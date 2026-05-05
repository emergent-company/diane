// Package: main
// Chat/send and content value extraction.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func (a *localAPIServer) handleChatSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		SessionID string `json:"session_id,omitempty"`
		Content   string `json:"content"`
		AgentName string `json:"agent_name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.Content == "" {
		jsonError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.AgentName == "" {
		req.AgentName = "diane-default"
	}

	ctx := context.Background()

	// 1. Create or reuse session
	sessionID := req.SessionID
	if sessionID == "" {
		session, err := a.bridge.CreateSession(ctx, "Chat")
		if err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create session: %v", err))
			return
		}
		sessionID = session.ID
	}

	// 2. Append user message to session
	_, err := a.bridge.AppendMessage(ctx, sessionID, "user", req.Content, 0)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("append message: %v", err))
		return
	}

	// 3. Find agent definition by name
	defs, err := a.bridge.ListAgentDefs(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list agent defs: %v", err))
		return
	}
	var defID string
	if defs != nil {
		for _, d := range defs.Data {
			if d.Name == req.AgentName {
				defID = d.ID
				break
			}
		}
	}
	if defID == "" {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("agent definition %q not found on Memory Platform", req.AgentName))
		return
	}

	// 4. Create runtime agent
	runtimeName := fmt.Sprintf("chat-%s-%d", req.AgentName, time.Now().UnixMilli())
	agent, err := a.bridge.CreateRuntimeAgent(ctx, runtimeName, defID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create runtime agent: %v", err))
		return
	}
	agentID := agent.Data.ID

	// 5. Ensure cleanup
	defer func() {
		if delErr := a.bridge.Client().Agents.Delete(ctx, agentID); delErr != nil {
			log.Printf("[CHAT] Failed to clean up runtime agent %s: %v", agentID, delErr)
		} else {
			log.Printf("[CHAT] Cleaned up runtime agent %s", agentID)
		}
	}()

	// 6. Trigger agent with user's message as prompt
	triggerResp, err := a.bridge.TriggerAgentWithInput(ctx, agentID, req.Content, sessionID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("trigger agent: %v", err))
		return
	}
	if !triggerResp.Success || triggerResp.RunID == nil {
		errMsg := "unknown error"
		if triggerResp.Error != nil {
			errMsg = *triggerResp.Error
		}
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("trigger failed: %s", errMsg))
		return
	}
	runID := *triggerResp.RunID
	log.Printf("[CHAT] Agent triggered — run_id=%s session_id=%s", runID[:12], sessionID[:12])

	// 7. Poll for completion
	pollStart := time.Now()
	pollInterval := 2 * time.Second
	pollTimeout := 120 * time.Second
	var runStatus string

pollLoop:
	for {
		select {
		case <-ctx.Done():
			jsonError(w, http.StatusInternalServerError, "cancelled")
			return
		case <-time.After(pollInterval):
		}

		if time.Since(pollStart) >= pollTimeout {
			jsonError(w, http.StatusGatewayTimeout, fmt.Sprintf("run %s: timeout after %v (last status: %s)", runID[:12], pollTimeout, runStatus))
			return
		}

		runResp, pollErr := a.bridge.GetProjectRun(ctx, runID)
		if pollErr != nil {
			log.Printf("[CHAT] Poll error: %v", pollErr)
			continue
		}
		runStatus = runResp.Data.Status
		log.Printf("[CHAT] Poll — run=%s status=%s elapsed=%v", runID[:12], runStatus, time.Since(pollStart).Round(time.Second))

		switch runStatus {
		case "completed", "success", "completed_with_warnings":
			break pollLoop
		case "paused":
			log.Printf("[CHAT] Agent paused (asked a question) — continuing poll")
			continue
		case "error", "failed", "cancelled", "timeout":
			errMsg := ""
			if runResp.Data.ErrorMessage != nil {
				errMsg = *runResp.Data.ErrorMessage
			}
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("run %s: status=%s error=%s", runID[:12], runStatus, errMsg))
			return
		}
	}

	duration := time.Since(pollStart).Round(time.Millisecond)
	log.Printf("[CHAT] Run completed — run=%s session=%s duration=%v", runID[:12], sessionID[:12], duration)

	// 8. Fetch run messages and convert to flat format
	msgs, err := a.bridge.GetRunMessages(ctx, runID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("get run messages: %v", err))
		return
	}

	// 9. Convert SDK messages to flat messageJSON format
	type toolCallJSON struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments,omitempty"`
	}
	type messageJSON struct {
		ID               string         `json:"id"`
		Role             string         `json:"role"`
		Content          string         `json:"content"`
		SequenceNumber   int            `json:"sequence_number,omitempty"`
		TokenCount       int            `json:"token_count,omitempty"`
		ToolCalls        []toolCallJSON `json:"tool_calls,omitempty"`
		ReasoningContent string         `json:"reasoning_content,omitempty"`
		CreatedAt        string         `json:"created_at,omitempty"`
	}

	messages := make([]messageJSON, 0, len(msgs.Data))
	var assistantText string

	for i, msg := range msgs.Data {
		// Normalize role: MP returns the agent name as the role; we want "assistant"
		role := msg.Role
		switch role {
		case "user", "tool", "system":
			// keep as-is
		default:
			role = "assistant"
		}

		flatMsg := messageJSON{
			Role:           role,
			SequenceNumber: i,
			CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		}

		// Extract content from the SDK's Content map.
		// The "text" key can be a string, []string, or []any — handle all.
		if val, ok := msg.Content["reasoning"]; ok {
			if s := extractContentValue(val); s != "" {
				flatMsg.ReasoningContent = s
			}
		}
		if val, ok := msg.Content["text"]; ok {
			if s := extractContentValue(val); s != "" {
				flatMsg.Content = s
				if role != "user" && role != "tool" {
					assistantText = s
				}
			}
		}
		// If no text content but there's reasoning, show reasoning as content
		if flatMsg.Content == "" && flatMsg.ReasoningContent != "" {
			flatMsg.Content = flatMsg.ReasoningContent
			flatMsg.ReasoningContent = ""
		}

		messages = append(messages, flatMsg)
	}

	// 10. Store assistant response in session for cross-run context
	if assistantText != "" {
		go func() {
			storeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			a.bridge.AppendMessage(storeCtx, sessionID, "assistant", assistantText, 0)
		}()
	}

	// 11. Return response
	jsonResponse(w, map[string]any{
		"session_id": sessionID,
		"run_id":     runID,
		"messages":   messages,
		"success":    true,
	})
}

// extractContentValue extracts human-readable text from a value in an SDK
// Content map. The "text" key can be stored as string, []string, or []any
// (e.g., when the ADK executor joins multiple text parts). This handles all
// variants so content extraction never silently fails.
func extractContentValue(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case []string:
		if len(v) == 0 {
			return ""
		}
		return strings.Join(v, "\n")
	case []any:
		var parts []string
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	default:
		// Unknown type — try JSON stringification as last resort
		if b, err := json.Marshal(val); err == nil && len(b) > 2 {
			return string(b)
		}
		return ""
	}
}

// ─── Todo Handlers ───────────────────────────────────────

// handleSessionTodos handles todo list/create.
// GET /api/sessions/{id}/todos — list todos
// POST /api/sessions/{id}/todos — create a todo
