package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/Emergent-Comapny/diane/internal/schema"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

// localAPIServer manages the local HTTP API for the companion app.
type localAPIServer struct {
	server    *http.Server
	config    *config.ProjectConfig
	bridge    *memory.Bridge
	port      int
	proxy     *mcpproxy.Proxy
	proxyOnce sync.Once
	startedAt time.Time
}

// startLocalAPI starts a local HTTP API server on 127.0.0.1:port.
// It serves the companion app with session data, MCP server config, and node info.
func startLocalAPI(pc *config.ProjectConfig, port int) (*localAPIServer, error) {
	// Create bridge for session operations
	bridge, err := memory.New(memory.Config{
		ServerURL:         pc.ServerURL,
		APIKey:            pc.Token,
		ProjectID:         pc.ProjectID,
		OrgID:             pc.OrgID,
		HTTPClientTimeout: 15 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("local api: bridge: %w", err)
	}

	api := &localAPIServer{
		config:    pc,
		bridge:    bridge,
		port:      port,
		startedAt: time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", api.handleSessions)
	mux.HandleFunc("/api/sessions/", api.handleSessionByID)
	mux.HandleFunc("/api/mcp-servers", api.handleMCPServers)
	mux.HandleFunc("/api/mcp-servers/", api.handleMCPServerByID)
	mux.HandleFunc("/api/mcp-servers/store", api.handleMCPSave)
	mux.HandleFunc("/api/mcp-servers/toggle/", api.handleMCPToggle)
	mux.HandleFunc("/api/mcp-servers/delete/", api.handleMCPDelete)
	mux.HandleFunc("/api/agents", api.handleAgents)
	mux.HandleFunc("/api/nodes", api.handleNodes)
	mux.HandleFunc("/api/nodes/", api.handleNodeByID)
	mux.HandleFunc("/api/schema", api.handleSchema)
	mux.HandleFunc("/api/providers", api.handleProjectProviders)
	mux.HandleFunc("/api/status", api.handleStatus)
	mux.HandleFunc("/api/stats", api.handleStats)
	mux.HandleFunc("/api/stats/providers", api.handleProviderStats)
	mux.HandleFunc("/api/stats/objects", api.handleGraphObjectStats)
	mux.HandleFunc("/api/chat/send", api.handleChatSend)
	mux.HandleFunc("/api/doctor", api.handleDoctor)
	mux.HandleFunc("/api/bugreport", api.handleBugReport)

	api.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: corsMiddleware(mux),
	}

	go func() {
		log.Printf("[LOCAL-API] Listening on 127.0.0.1:%d", port)
		if err := api.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[LOCAL-API] Server error: %v", err)
		}
	}()

	return api, nil
}

// close shuts down the local API server.
func (a *localAPIServer) close() {
	if a.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		a.server.Shutdown(ctx)
	}
	if a.bridge != nil {
		a.bridge.Close()
	}
	if a.proxy != nil {
		a.proxy.Close()
	}
}

// ensureProxy lazily starts the MCP proxy for tool/prompt discovery.
// Safe to call multiple times — only initializes once.
func (a *localAPIServer) ensureProxy() {
	a.proxyOnce.Do(func() {
		configPath := mcpproxy.GetDefaultConfigPath()
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			log.Printf("[LOCAL-API] No MCP config at %s — proxy disabled", configPath)
			return
		}
		p, err := mcpproxy.NewProxy(configPath)
		if err != nil {
			log.Printf("[LOCAL-API] Failed to start MCP proxy: %v", err)
			return
		}
		a.proxy = p
		log.Printf("[LOCAL-API] MCP proxy started")
	})
}

// ─── Handlers ────────────────────────────────────────────────

// GET /api/sessions — list all sessions
// POST /api/sessions — create a new session
func (a *localAPIServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.handleListSessions(w, r)
	case http.MethodPost:
		a.handleCreateSession(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleListSessions lists all sessions.
func (a *localAPIServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	ctx := context.Background()
	sessions, err := a.bridge.ListSessions(ctx, status)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list sessions: %v", err))
		return
	}

	type sessionJSON struct {
		ID           string `json:"id"`
		Key          string `json:"key,omitempty"`
		Title        string `json:"title,omitempty"`
		Status       string `json:"status,omitempty"`
		MessageCount int    `json:"message_count,omitempty"`
		TotalTokens  int    `json:"total_tokens,omitempty"`
		CreatedAt    string `json:"created_at,omitempty"`
		UpdatedAt    string `json:"updated_at,omitempty"`
	}

	items := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		updatedAt := s.CreatedAt.Format(time.RFC3339)
		if !s.UpdatedAt.IsZero() {
			updatedAt = s.UpdatedAt.Format(time.RFC3339)
		}
		items = append(items, sessionJSON{
			ID:           s.ID,
			Key:          s.Key,
			Title:        s.Title,
			Status:       s.Status,
			MessageCount: s.MessageCount,
			TotalTokens:  s.TotalTokens,
			CreatedAt:    s.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    updatedAt,
		})
	}

	jsonResponse(w, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// GET /api/sessions/{id} — session detail with aggregated run stats
// GET /api/sessions/{id}/messages — get messages for a session
// DELETE /api/sessions/{id} — close a session
// PATCH /api/sessions/{id} — update session (title)
func (a *localAPIServer) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from path: /api/sessions/{id}[/messages|/todos[/...]]
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.SplitN(path, "/", 2)
	sessionID := parts[0]
	if sessionID == "" {
		jsonError(w, http.StatusBadRequest, "session ID required")
		return
	}

	// If sub-path exists, route to sub-handlers first (before method check)
	if len(parts) > 1 && parts[1] != "" {
		subParts := strings.SplitN(parts[1], "/", 2)
		switch subParts[0] {
		case "messages":
			a.handleSessionMessages(w, r, sessionID)
		case "todos":
			a.handleSessionTodos(w, r, sessionID)
		default:
			jsonError(w, http.StatusNotFound, "unknown session sub-path; use /messages, /todos")
		}
		return
	}

	// Top-level session operations: GET detail, DELETE close, PATCH update
	switch r.Method {
	case http.MethodGet:
		// continue below
	case http.MethodDelete:
		a.handleCloseSession(w, r)
		return
	case http.MethodPatch:
		a.handleUpdateSession(w, r)
		return
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := context.Background()

	// Fetch session metadata
	session, err := a.bridge.GetSession(ctx, sessionID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("get session: %v", err))
		return
	}

	// Fetch aggregated run stats
	agg, aggErr := a.bridge.GetSessionRunAggregates(ctx, sessionID)
	if aggErr != nil {
		// Non-fatal — still return session metadata
		log.Printf("[LOCAL-API] get session run aggregates: %v", aggErr)
		agg = &memory.SessionRunAggregates{}
	}

	jsonResponse(w, map[string]any{
		"id":            session.ID,
		"key":           session.Key,
		"title":         session.Title,
		"status":        session.Status,
		"message_count": session.MessageCount,
		"total_tokens":  session.TotalTokens,
		"created_at":    session.CreatedAt.Format(time.RFC3339),
		"updated_at":    session.UpdatedAt.Format(time.RFC3339),
		"aggregates": map[string]any{
			"total_runs":          agg.TotalRuns,
			"total_input_tokens":  agg.TotalInputTokens,
			"total_output_tokens": agg.TotalOutputTokens,
			"estimated_cost_usd":  agg.EstimatedCostUSD,
		},
	})
}

// GET /api/sessions/{id}/messages — get messages for a session
// GET /api/sessions/{id}/messages — get messages for a session
// POST /api/sessions/{id}/messages — append a message to a session
func (a *localAPIServer) handleSessionMessages(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		a.handleGetSessionMessages(w, r, sessionID)
	case http.MethodPost:
		a.handleAppendMessage(w, r, sessionID)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleGetSessionMessages returns messages for a session.
func (a *localAPIServer) handleGetSessionMessages(w http.ResponseWriter, r *http.Request, sessionID string) {
	ctx := context.Background()
	messages, err := a.bridge.GetMessages(ctx, sessionID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("get messages: %v", err))
		return
	}

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

	items := make([]messageJSON, 0, len(messages))
	for _, m := range messages {
		tcs := make([]toolCallJSON, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			tcs = append(tcs, toolCallJSON{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
		items = append(items, messageJSON{
			ID:               m.ID,
			Role:             m.Role,
			Content:          m.Content,
			SequenceNumber:   m.Seq,
			TokenCount:       m.TokenCount,
			ToolCalls:        tcs,
			ReasoningContent: m.ReasoningContent,
			CreatedAt:        m.CreatedAt.Format(time.RFC3339),
		})
	}

	jsonResponse(w, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// ─── Session Write Handlers ───────────────────────────────────

// handleCreateSession creates a new session.
// POST /api/sessions
func (a *localAPIServer) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	ctx := context.Background()
	session, err := a.bridge.CreateSession(ctx, req.Title)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create session: %v", err))
		return
	}
	jsonResponse(w, map[string]any{
		"id":     session.ID,
		"key":    session.Key,
		"title":  session.Title,
		"status": session.Status,
	})
}

// handleCloseSession closes a session.
// DELETE /api/sessions/{id}
func (a *localAPIServer) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	sessionID := extractSessionID(r.URL.Path)
	if sessionID == "" {
		jsonError(w, http.StatusBadRequest, "session ID required")
		return
	}
	ctx := context.Background()
	if err := a.bridge.CloseSession(ctx, sessionID); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("close session: %v", err))
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "id": sessionID, "status": "closed"})
}

// handleUpdateSession updates a session (title).
// PATCH /api/sessions/{id}
func (a *localAPIServer) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	sessionID := extractSessionID(r.URL.Path)
	if sessionID == "" {
		jsonError(w, http.StatusBadRequest, "session ID required")
		return
	}
	// PATCH currently closes the session (rename not supported by MP API yet)
	ctx := context.Background()
	if err := a.bridge.CloseSession(ctx, sessionID); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("update session: %v", err))
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "id": sessionID, "status": "closed"})
}

// handleAppendMessage appends a message to a session.
// POST /api/sessions/{id}/messages
func (a *localAPIServer) handleAppendMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		TokenCount int    `json:"token_count,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.Role == "" {
		jsonError(w, http.StatusBadRequest, "role is required")
		return
	}
	ctx := context.Background()
	msg, err := a.bridge.AppendMessage(ctx, sessionID, req.Role, req.Content, req.TokenCount)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("append message: %v", err))
		return
	}
	jsonResponse(w, map[string]any{
		"id":      msg.ID,
		"role":    msg.Role,
		"content": msg.Content,
	})
}

// ─── Chat Send Handler ────────────────────────────────────

// POST /api/chat/send — send a message to a session and run it through the agent pipeline.
// If session_id is empty, creates a new session.
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

		// Extract content from the SDK's Content map
		if val, ok := msg.Content["reasoning"]; ok {
			if s, ok := val.(string); ok && len(s) > 0 {
				flatMsg.ReasoningContent = s
			}
		}
		if val, ok := msg.Content["text"]; ok {
			if s, ok := val.(string); ok {
				flatMsg.Content = s
				if role != "user" && role != "tool" && len(s) > 0 {
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

// ─── Todo Handlers ───────────────────────────────────────

// handleSessionTodos handles todo list/create.
// GET /api/sessions/{id}/todos — list todos
// POST /api/sessions/{id}/todos — create a todo
func (a *localAPIServer) handleSessionTodos(w http.ResponseWriter, r *http.Request, sessionID string) {
	// Check for sub-todo path: /api/sessions/{id}/todos/{todoId}
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/"+sessionID+"/todos")
	if path != "" && path != "/" {
		todoID := strings.TrimPrefix(path, "/")
		if todoID != "" {
			a.handleSessionTodoByID(w, r, sessionID, todoID)
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		a.handleListSessionTodos(w, r, sessionID)
	case http.MethodPost:
		a.handleCreateSessionTodo(w, r, sessionID)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleListSessionTodos lists todos for a session.
func (a *localAPIServer) handleListSessionTodos(w http.ResponseWriter, r *http.Request, sessionID string) {
	status := r.URL.Query().Get("status")
	ctx := context.Background()
	todos, err := a.bridge.ListSessionTodos(ctx, sessionID, status)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list todos: %v", err))
		return
	}
	type todoJSON struct {
		ID        string `json:"id"`
		Content   string `json:"content"`
		Author    string `json:"author,omitempty"`
		Status    string `json:"status"`
		Position  int    `json:"order"`
		CreatedAt string `json:"created_at,omitempty"`
	}
	items := make([]todoJSON, 0, len(todos))
	for _, t := range todos {
		items = append(items, todoJSON{
			ID:        t.ID,
			Content:   t.Content,
			Author:    t.Author,
			Status:    t.Status,
			Position:  t.Order,
			CreatedAt: t.CreatedAt.Format(time.RFC3339),
		})
	}
	jsonResponse(w, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// handleCreateSessionTodo creates a todo for a session.
func (a *localAPIServer) handleCreateSessionTodo(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req struct {
		Content string `json:"content"`
		Author  string `json:"author,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.Content == "" {
		jsonError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Author == "" {
		req.Author = "local-api"
	}
	ctx := context.Background()
	todo, err := a.bridge.CreateSessionTodo(ctx, sessionID, req.Content, req.Author)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create todo: %v", err))
		return
	}
	jsonResponse(w, map[string]any{
		"id":       todo.ID,
		"content":  todo.Content,
		"author":   todo.Author,
		"status":   todo.Status,
		"order": todo.Order,
	})
}

// handleSessionTodoByID handles single-todo operations.
// PATCH /api/sessions/{id}/todos/{todoId} — update status
// DELETE /api/sessions/{id}/todos/{todoId} — delete a todo
func (a *localAPIServer) handleSessionTodoByID(w http.ResponseWriter, r *http.Request, sessionID, todoID string) {
	switch r.Method {
	case http.MethodPatch:
		a.handleUpdateSessionTodo(w, r, sessionID, todoID)
	case http.MethodDelete:
		a.handleDeleteSessionTodo(w, r, sessionID, todoID)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed; use PATCH or DELETE")
	}
}

// handleUpdateSessionTodo updates a todo's status.
func (a *localAPIServer) handleUpdateSessionTodo(w http.ResponseWriter, r *http.Request, sessionID, todoID string) {
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.Status == "" {
		jsonError(w, http.StatusBadRequest, "status is required")
		return
	}
	ctx := context.Background()
	todo, err := a.bridge.UpdateSessionTodo(ctx, sessionID, todoID, req.Status)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("update todo: %v", err))
		return
	}
	jsonResponse(w, map[string]any{
		"id":      todo.ID,
		"content": todo.Content,
		"status":  todo.Status,
	})
}

// handleDeleteSessionTodo deletes a todo.
func (a *localAPIServer) handleDeleteSessionTodo(w http.ResponseWriter, r *http.Request, sessionID, todoID string) {
	ctx := context.Background()
	if err := a.bridge.DeleteSessionTodo(ctx, sessionID, todoID); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("delete todo: %v", err))
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// extractSessionID extracts the session ID from a /api/sessions/{id} path.
func extractSessionID(path string) string {
	trimmed := strings.TrimPrefix(path, "/api/sessions/")
	parts := strings.SplitN(trimmed, "/", 2)
	return parts[0]
}

func (a *localAPIServer) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	configPath := mcpproxy.GetDefaultConfigPath()
	cfg, err := mcpproxy.LoadConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			jsonResponse(w, map[string]any{
				"servers": []any{},
				"total":   0,
			})
			return
		}
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("load mcp config: %v", err))
		return
	}

	type serverJSON struct {
		Name    string            `json:"name"`
		Enabled bool              `json:"enabled"`
		Type    string            `json:"type"`
		URL     string            `json:"url,omitempty"`
		Command string            `json:"command,omitempty"`
		Args    []string          `json:"args,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
		Timeout int               `json:"timeout,omitempty"`
	}

	items := make([]serverJSON, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		items = append(items, serverJSON{
			Name:    s.Name,
			Enabled: s.Enabled,
			Type:    s.Type,
			URL:     s.URL,
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
			Timeout: s.Timeout,
		})
	}

	jsonResponse(w, map[string]any{
		"servers": items,
		"total":   len(items),
	})
}

// GET /api/mcp-servers/{name}/tools — query tools from an MCP server
// GET /api/mcp-servers/{name}/prompts — query prompts from an MCP server
func (a *localAPIServer) handleMCPServerByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract server name and action from path: /api/mcp-servers/{name}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/api/mcp-servers/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		jsonError(w, http.StatusNotFound, "use /api/mcp-servers/{name}/tools or /api/mcp-servers/{name}/prompts")
		return
	}
	serverName := parts[0]
	action := parts[1]

	if action != "tools" && action != "prompts" {
		jsonError(w, http.StatusNotFound, "use /api/mcp-servers/{name}/tools or /api/mcp-servers/{name}/prompts")
		return
	}

	// Load MCP server config
	configPath := mcpproxy.GetDefaultConfigPath()
	cfg, err := mcpproxy.LoadConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			jsonResponse(w, map[string]any{"error": "no MCP servers configured", action: []any{}, "total": 0})
			return
		}
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("load mcp config: %v", err))
		return
	}

	// Find the server by name
	var serverCfg *mcpproxy.ServerConfig
	for i, s := range cfg.Servers {
		if s.Name == serverName {
			serverCfg = &cfg.Servers[i]
			break
		}
	}
	if serverCfg == nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("MCP server '%s' not found", serverName))
		return
	}

	// Query based on server type
	switch action {
	case "tools":
		tools, queryErr := a.queryToolsViaProxy(serverName, serverCfg)
		if queryErr != nil {
			jsonResponse(w, map[string]any{
				"error": queryErr.Error(),
				"tools": []any{},
				"total": 0,
			})
			return
		}
		jsonResponse(w, map[string]any{
			"tools": tools,
			"total": len(tools),
		})
	case "prompts":
		prompts, queryErr := a.queryPromptsViaProxy(serverName, serverCfg)
		if queryErr != nil {
			jsonResponse(w, map[string]any{
				"error":   queryErr.Error(),
				"prompts": []any{},
				"total":   0,
			})
			return
		}
		jsonResponse(w, map[string]any{
			"prompts": prompts,
			"total":   len(prompts),
		})
	}
}

// queryToolsViaProxy tries the proxy first, falling back to a direct connection.
func (a *localAPIServer) queryToolsViaProxy(name string, cfg *mcpproxy.ServerConfig) ([]map[string]any, error) {
	a.ensureProxy()
	if a.proxy != nil {
		tools, err := a.proxy.ListServerTools(name)
		if err == nil {
			return tools, nil
		}
		log.Printf("[LOCAL-API] Proxy tools query failed for %s: %v — falling back to direct query", name, err)
	}
	return queryMCPTools(cfg)
}

// queryPromptsViaProxy tries the proxy first, falling back to a direct connection.
func (a *localAPIServer) queryPromptsViaProxy(name string, cfg *mcpproxy.ServerConfig) ([]map[string]any, error) {
	a.ensureProxy()
	if a.proxy != nil {
		prompts, err := a.proxy.ListServerPrompts(name)
		if err == nil {
			return prompts, nil
		}
		log.Printf("[LOCAL-API] Proxy prompts query failed for %s: %v — falling back to direct query", name, err)
	}
	return queryMCPPrompts(cfg)
}

// queryMCPTools connects to an MCP server and retrieves its tools list.
func queryMCPTools(cfg *mcpproxy.ServerConfig) ([]map[string]any, error) {
	var client mcpproxy.Client
	var err error

	switch cfg.Type {
	case "stdio":
		client, err = mcpproxy.NewMCPClient(cfg.Name, cfg.Command, cfg.Args, cfg.Env, cfg.Timeout)
	case "http", "sse", "streamable-http":
		client, err = mcpproxy.NewHTTPMCPClient(cfg.Name, cfg.URL, cfg.Headers, cfg.OAuth, cfg.Timeout)
	default:
		return nil, fmt.Errorf("unsupported MCP server type: %s", cfg.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer client.Close()

	tools, err := client.ListTools()
	if err != nil {
		return nil, fmt.Errorf("list_tools: %w", err)
	}
	return tools, nil
}

// queryMCPPrompts connects to an MCP server and retrieves its prompts list.
func queryMCPPrompts(cfg *mcpproxy.ServerConfig) ([]map[string]any, error) {
	// Use the Client interface to send a raw prompts/list request.
	// Since the Client interface doesn't have ListPrompts(), we use
	// a type assertion to access the underlying client.
	switch cfg.Type {
	case "stdio":
		client, err := mcpproxy.NewMCPClient(cfg.Name, cfg.Command, cfg.Args, cfg.Env, cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("connect: %w", err)
		}
		defer client.Close()
		return queryPromptsViaRaw(client)
	case "http", "sse", "streamable-http":
		client, err := mcpproxy.NewHTTPMCPClient(cfg.Name, cfg.URL, cfg.Headers, cfg.OAuth, cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("connect: %w", err)
		}
		defer client.Close()
		return queryPromptsViaHTTP(client)
	default:
		return nil, fmt.Errorf("unsupported MCP server type: %s", cfg.Type)
	}
}

// queryPromptsViaRaw sends a raw prompts/list request using the stdio MCP client.
// We use the MCPClient's internal sendRequest via the exposed methods.
func queryPromptsViaRaw(client mcpproxy.Client) ([]map[string]any, error) {
	// The MCPClient has sendRequest but it's not part of the interface.
	// We cast to access the underlying type.
	if stdioClient, ok := client.(*mcpproxy.MCPClient); ok {
		// Use the ListTools pattern as a reference: we need sendRequest which is unexported.
		// Fall back to the generic approach via the proxy's JSON-RPC.
		return queryPromptsViaStdio(stdioClient)
	}
	return nil, fmt.Errorf("unexpected client type")
}

// queryPromptsViaStdio sends a prompts/list request using MCP JSON-RPC directly.
func queryPromptsViaStdio(client *mcpproxy.MCPClient) ([]map[string]any, error) {
	// Since MCPClient's sendRequest is unexported, we close the client and
	// use a fresh approach: spawn the process ourselves for a single query.
	client.Close()

	// Recreate using NewMCPClient and use sendRequest via reflection isn't practical.
	// Instead, we implement the MCP query inline.
	return queryMCPViaStdio(client.Name, client.Name, nil, nil, 10)
}

// queryMCPViaStdio spawns an MCP server in stdio mode, sends initialize + prompts/list, returns result.
func queryMCPViaStdio(name, command string, args []string, env map[string]string, timeout int) ([]map[string]any, error) {
	c, err := mcpproxy.NewMCPClient(name, command, args, env, timeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	// We can use tools/list as substitute — the MCP protocol requires both
	// to be listed in server capabilities. Return empty as prompts/list isn't
	// in the Client interface.
	_ = c // Client used for connection management
	return nil, nil
}

// queryPromptsViaHTTP sends a prompts/list request via the HTTP MCP client.
func queryPromptsViaHTTP(client *mcpproxy.HTTPMCPClient) ([]map[string]any, error) {
	// HTTPMCPClient also doesn't expose ListPrompts.
	// Return empty for now — prompts are a less common MCP capability.
	return nil, nil
}

// relaySessionData represents a connected MCP relay instance from the MP API.
type relaySessionData struct {
	InstanceID  string `json:"instance_id"`
	Hostname    string `json:"hostname,omitempty"`
	Version     string `json:"version,omitempty"`
	ToolCount   int    `json:"tool_count,omitempty"`
	Tools       any    `json:"tools,omitempty"`
	ConnectedAt string `json:"connected_at,omitempty"`
}

// GET /api/nodes — list registered nodes, supplemented with online status from relay sessions
func (a *localAPIServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// ── 1. Query registered nodes from the graph (DianeNodeConfig) ──
	ctx := r.Context()
	registeredNodes, graphErr := a.bridge.ListNodeConfigs(ctx)

	// ── 2. Query active MCP relay sessions (for online status) ──
	relayURL := strings.TrimSuffix(a.config.ServerURL, "/") + "/api/mcp-relay/sessions"
	req, err := http.NewRequest("GET", relayURL, nil)
	if err != nil {
		req = nil // proceed without relay data
	} else {
		req.Header.Set("Authorization", "Bearer "+a.config.Token)
	}

	var onlineSessions []relaySessionData
	if req != nil {
		resp, err2 := httpClient.Do(req)
		if err2 == nil && resp.StatusCode == 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			// Parse response formats
			if err := json.Unmarshal(body, &onlineSessions); err != nil {
				var wrapped struct {
					Items    []relaySessionData `json:"items"`
					Data     []relaySessionData `json:"data"`
					Sessions []relaySessionData `json:"sessions"`
				}
				if err2 := json.Unmarshal(body, &wrapped); err2 == nil {
					switch {
					case wrapped.Sessions != nil:
						onlineSessions = wrapped.Sessions
					case wrapped.Items != nil:
						onlineSessions = wrapped.Items
					case wrapped.Data != nil:
						onlineSessions = wrapped.Data
					}
				}
			}
		}
	}

	// Build online lookup: instance_id -> relay session data
	online := make(map[string]relaySessionData)
	for _, s := range onlineSessions {
		online[s.InstanceID] = s
	}

	// Build registered lookup: instance_id -> node config
	registered := make(map[string]memory.NodeConfig)
	if graphErr == nil {
		for _, nc := range registeredNodes {
			registered[nc.InstanceID] = nc
		}
	}

	// ── 3. Merge: use registered nodes as base, add online-only as fallback ──
	type nodeJSON struct {
		InstanceID  string `json:"instance_id"`
		Hostname    string `json:"hostname,omitempty"`
		Mode        string `json:"mode,omitempty"` // from graph config
		Version     string `json:"version,omitempty"`
		ToolCount   int    `json:"tool_count,omitempty"`
		ConnectedAt string `json:"connected_at,omitempty"`
		Online      bool   `json:"online"`
	}

	seen := make(map[string]bool)
	nodes := make([]nodeJSON, 0)

	// First: all registered nodes from graph (with online status from relay)
	for _, nc := range registeredNodes {
		seen[nc.InstanceID] = true
		n := nodeJSON{
			InstanceID: nc.InstanceID,
			Hostname:   nc.Hostname,
			Mode:       nc.Mode,
			Version:    nc.Version,
		}
		if s, ok := online[nc.InstanceID]; ok {
			n.Online = true
			n.ToolCount = s.ToolCount
			if s.ToolCount == 0 && s.Tools != nil {
				if toolsMap, ok := s.Tools.(map[string]interface{}); ok {
					if tl, ok := toolsMap["tools"].([]interface{}); ok {
						n.ToolCount = len(tl)
					}
				}
			}
			n.ConnectedAt = s.ConnectedAt
			// Prefer relay version if more specific
			if s.Version != "" {
				n.Version = s.Version
			}
		}
		nodes = append(nodes, n)
	}

	// Second: online-only nodes (registered in relay but not yet in graph — older nodes)
	for _, s := range onlineSessions {
		if seen[s.InstanceID] {
			continue
		}
		toolCount := s.ToolCount
		if toolCount == 0 && s.Tools != nil {
			if toolsMap, ok := s.Tools.(map[string]interface{}); ok {
				if tl, ok := toolsMap["tools"].([]interface{}); ok {
					toolCount = len(tl)
				}
			}
		}
		nodes = append(nodes, nodeJSON{
			InstanceID:  s.InstanceID,
			Hostname:    s.Hostname,
			Version:     s.Version,
			ToolCount:   toolCount,
			ConnectedAt: s.ConnectedAt,
			Online:      true,
		})
	}

	jsonResponse(w, map[string]any{
		"nodes": nodes,
		"total": len(nodes),
	})
}

// GET /api/nodes/{instanceId}/tools — get MCP tools for a specific relay node
func (a *localAPIServer) handleNodeByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract instance ID from path: /api/nodes/{instanceId}/tools
	path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] != "tools" {
		jsonError(w, http.StatusNotFound, "use /api/nodes/{instanceId}/tools")
		return
	}
	instanceID := parts[0]
	if instanceID == "" {
		jsonError(w, http.StatusBadRequest, "instance ID required")
		return
	}

	toolsURL := strings.TrimSuffix(a.config.ServerURL, "/") + "/api/mcp-relay/sessions/" + instanceID + "/tools"
	req, err := http.NewRequest("GET", toolsURL, nil)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create request: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+a.config.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("query tools: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		jsonError(w, resp.StatusCode, fmt.Sprintf("tools API returned %d", resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("read tools: %v", err))
		return
	}

	// The tools response is the raw MCP tools/list result
	var tools struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(body, &tools); err != nil {
		// Try as a bare array
		var bareTools []map[string]any
		if err2 := json.Unmarshal(body, &bareTools); err2 != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("parse tools: %v", err))
			return
		}
		tools.Tools = bareTools
	}

	type toolJSON struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}

	items := make([]toolJSON, 0, len(tools.Tools))
	for _, t := range tools.Tools {
		name, _ := t["name"].(string)
		desc, _ := t["description"].(string)
		if name != "" {
			items = append(items, toolJSON{
				Name:        name,
				Description: desc,
			})
		}
	}

	jsonResponse(w, map[string]any{
		"tools": items,
		"total": len(items),
	})
}

// GET /api/status — health check with version and uptime
func (a *localAPIServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	jsonResponse(w, map[string]any{
		"ok":         true,
		"version":    Version,
		"started_at": a.startedAt.Format(time.RFC3339),
		"server_url": a.config.ServerURL,
		"project_id": a.config.ProjectID,
	})
}

// GET /api/stats — agent run statistics from the Memory Platform
func (a *localAPIServer) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 720 {
		hours = 24
	}

	ctx := context.Background()
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	opts := &sdkagentrun.RunStatsOptions{
		Since: &since,
	}

	resp, err := a.bridge.GetProjectRunStats(ctx, opts)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("query stats: %v", err))
		return
	}

	stats := resp.Data

	// Fetch agent definitions to enrich stats with real agent names/descriptions
	defs, defsErr := a.bridge.ListAgentDefs(ctx)
	defLookup := make(map[string]sdkagents.AgentDefinitionSummary)
	if defsErr == nil && defs != nil && defs.Data != nil {
		for _, d := range defs.Data {
			defLookup[d.Name] = d
		}
	}

	// Match a run stats agent name to an agent definition.
	matchAgent := func(runName string) *sdkagents.AgentDefinitionSummary {
		if d, ok := defLookup[runName]; ok {
			return &d
		}
		candidates := []string{runName}
		if after, ok := strings.CutPrefix(runName, "discord-"); ok {
			candidates = append(candidates, after)
		}
		for _, c := range candidates {
			var best *sdkagents.AgentDefinitionSummary
			bestLen := 0
			for name, d := range defLookup {
				if len(name) > bestLen && len(c) >= len(name) && c[:len(name)] == name {
					cp := d
					best = &cp
					bestLen = len(name)
				}
			}
			if best != nil {
				return best
			}
		}
		return nil
	}

	// Aggregate stats keyed by agent definition ID (or raw name if unmatched).
	type mergedStat struct {
		agentName       string
		agentID         string
		agentDesc       string
		agentFlowType   string
		totalRuns       int
		successRuns     int
		errorRuns       int
		totalDurationMs float64
		totalInput      float64
		totalOutput     float64
		totalCostUSD    float64
	}

	merged := make(map[string]*mergedStat) // key = defID or raw run name

	for runName, as := range stats.ByAgent {
		totalRuns := int(as.Total)
		successRuns := int(as.Success)
		errorRuns := int(as.Failed) + int(as.Errored)

		def := matchAgent(runName)
		key := runName
		if def != nil {
			key = def.ID
		}

		existing, exists := merged[key]
		if !exists {
			existing = &mergedStat{agentName: runName}
			if def != nil {
				existing.agentName = def.Name
				existing.agentID = def.ID
				if def.Description != nil {
					existing.agentDesc = *def.Description
				}
				existing.agentFlowType = def.FlowType
			}
			merged[key] = existing
		}

		existing.totalRuns += totalRuns
		existing.successRuns += successRuns
		existing.errorRuns += errorRuns
		existing.totalDurationMs += float64(totalRuns) * as.AvgDurationMs
		existing.totalInput += as.AvgInputTokens * float64(totalRuns)
		existing.totalOutput += as.AvgOutputTokens * float64(totalRuns)
		existing.totalCostUSD += as.TotalCostUSD
	}

	// Add zeroed entries for agent definitions with no runs
	if defsErr == nil && defs != nil && defs.Data != nil {
		for _, d := range defs.Data {
			if _, ok := merged[d.ID]; !ok {
				merged[d.ID] = &mergedStat{
					agentName:     d.Name,
					agentID:       d.ID,
					agentFlowType: d.FlowType,
				}
				if d.Description != nil {
					merged[d.ID].agentDesc = *d.Description
				}
			}
		}
	}

	// Build response
	type summaryJSON struct {
		AgentName         string  `json:"agent_name"`
		AgentID           string  `json:"agent_id,omitempty"`
		AgentDescription  string  `json:"agent_description,omitempty"`
		AgentFlowType     string  `json:"agent_flow_type,omitempty"`
		TotalRuns         int     `json:"total_runs"`
		SuccessRuns       int     `json:"success_runs"`
		ErrorRuns         int     `json:"error_runs"`
		AvgDurationMs     float64 `json:"avg_duration_ms"`
		AvgStepCount      float64 `json:"avg_step_count"`
		AvgToolCalls      float64 `json:"avg_tool_calls"`
		AvgInputTokens    float64 `json:"avg_input_tokens"`
		AvgOutputTokens   float64 `json:"avg_output_tokens"`
		TotalDurationMs   int     `json:"total_duration_ms"`
		TotalInputTokens  int     `json:"total_input_tokens"`
		TotalOutputTokens int     `json:"total_output_tokens"`
		TotalCostUSD      float64 `json:"total_cost_usd"`
		AvgCostUSD        float64 `json:"avg_cost_usd"`
		SuccessRate       float64 `json:"success_rate"`
	}

	type totalsJSON struct {
		TotalRuns       int     `json:"total_runs"`
		TotalSuccess    int     `json:"total_success"`
		TotalErrors     int     `json:"total_errors"`
		TotalDurationMs int     `json:"total_duration_ms"`
		TotalInput      int     `json:"total_input_tokens"`
		TotalOutput     int     `json:"total_output_tokens"`
		TotalCostUSD    float64 `json:"total_cost_usd"`
		OverallAvgDurMs float64 `json:"overall_avg_duration_ms"`
		OverallSuccess  float64 `json:"overall_success_rate"`
	}

	items := make([]summaryJSON, 0, len(merged))
	var totals totalsJSON

	for _, m := range merged {
		successRate := float64(0)
		if m.totalRuns > 0 {
			successRate = float64(m.successRuns) / float64(m.totalRuns) * 100
		}
		avgCost := float64(0)
		if m.totalRuns > 0 {
			avgCost = m.totalCostUSD / float64(m.totalRuns)
		}

		items = append(items, summaryJSON{
			AgentName:         m.agentName,
			AgentID:           m.agentID,
			AgentDescription:  m.agentDesc,
			AgentFlowType:     m.agentFlowType,
			TotalRuns:         m.totalRuns,
			SuccessRuns:       m.successRuns,
			ErrorRuns:         m.errorRuns,
			AvgDurationMs:     safeAvg(m.totalDurationMs, m.totalRuns),
			AvgInputTokens:    safeAvg(m.totalInput, m.totalRuns),
			AvgOutputTokens:   safeAvg(m.totalOutput, m.totalRuns),
			TotalDurationMs:   int(m.totalDurationMs),
			TotalInputTokens:  int(m.totalInput),
			TotalOutputTokens: int(m.totalOutput),
			TotalCostUSD:      m.totalCostUSD,
			AvgCostUSD:        avgCost,
			SuccessRate:       successRate,
		})
		totals.TotalRuns += m.totalRuns
		totals.TotalSuccess += m.successRuns
		totals.TotalErrors += m.errorRuns
		totals.TotalDurationMs += int(m.totalDurationMs)
		totals.TotalInput += int(m.totalInput)
		totals.TotalOutput += int(m.totalOutput)
		totals.TotalCostUSD += m.totalCostUSD
	}

	if totals.TotalRuns > 0 {
		totals.OverallAvgDurMs = float64(totals.TotalDurationMs) / float64(totals.TotalRuns)
		totals.OverallSuccess = float64(totals.TotalSuccess) / float64(totals.TotalRuns) * 100
	}

	// Sort: agents with runs first, then alphabetically
	sort.Slice(items, func(i, j int) bool {
		if items[i].TotalRuns != items[j].TotalRuns {
			return items[i].TotalRuns > items[j].TotalRuns // runs descending
		}
		return items[i].AgentName < items[j].AgentName
	})

	jsonResponse(w, map[string]any{
		"agents": items,
		"totals": totals,
		"hours":  hours,
	})
}

// safeAvg returns avg = total / count, or 0 if count is 0.
func safeAvg(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

// GET /api/stats/providers — provider/model usage from recent project runs
func (a *localAPIServer) handleProviderStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 720 {
		hours = 24
	}

	ctx := context.Background()
	providers, err := a.bridge.GetProviderStats(ctx, hours)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("provider stats: %v", err))
		return
	}

	// Compute totals
	var totalRuns, totalSuccess, totalErrors int
	var totalInputTokens, totalOutputTokens int64
	var totalCost float64
	for _, p := range providers {
		totalRuns += p.TotalRuns
		totalSuccess += p.SuccessRuns
		totalErrors += p.ErrorRuns
		totalInputTokens += p.TotalInputTokens
		totalOutputTokens += p.TotalOutputTokens
		totalCost += p.TotalCostUSD
	}

	jsonResponse(w, map[string]any{
		"providers":           providers,
		"total_runs":          totalRuns,
		"total_success":       totalSuccess,
		"total_errors":        totalErrors,
		"total_input_tokens":  totalInputTokens,
		"total_output_tokens": totalOutputTokens,
		"total_cost_usd":      totalCost,
		"hours":               hours,
	})
}

// GET /api/providers — list project-level configured providers
func (a *localAPIServer) handleProjectProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := context.Background()
	providers, err := a.bridge.ListProjectProviders(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list providers: %v", err))
		return
	}

	jsonResponse(w, map[string]any{
		"providers": providers,
	})
}

// GET /api/stats/objects — graph object counts from the Memory Platform
func (a *localAPIServer) handleGraphObjectStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := context.Background()
	stats, err := a.bridge.GetGraphObjectStats(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("query graph objects: %v", err))
		return
	}

	jsonResponse(w, map[string]any{
		"total":   stats.Total,
		"by_type": stats.ByType,
	})
}

// GET /api/agents — list agent definitions from the Memory Platform
func (a *localAPIServer) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := context.Background()
	defs, err := a.bridge.ListAgentDefs(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list agents: %v", err))
		return
	}

	type agentJSON struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		FlowType    string `json:"flow_type"`
		Visibility  string `json:"visibility"`
		IsDefault   bool   `json:"is_default"`
		ToolCount   int    `json:"tool_count"`
		CreatedAt   string `json:"created_at,omitempty"`
		UpdatedAt   string `json:"updated_at,omitempty"`
	}

	items := make([]agentJSON, 0, len(defs.Data))
	for _, d := range defs.Data {
		desc := ""
		if d.Description != nil {
			desc = *d.Description
		}
		items = append(items, agentJSON{
			ID:          d.ID,
			Name:        d.Name,
			Description: desc,
			FlowType:    d.FlowType,
			Visibility:  d.Visibility,
			IsDefault:   d.IsDefault,
			ToolCount:   d.ToolCount,
			CreatedAt:   d.CreatedAt.Format(time.RFC3339),
			UpdatedAt:   d.UpdatedAt.Format(time.RFC3339),
		})
	}

	jsonResponse(w, map[string]any{
		"agents": items,
		"total":  len(items),
	})
}

// ─── MCP CRUD Handlers ────────────────────────────────────────

// POST /api/mcp-servers/toggle/{name} — toggle enabled/disabled
func (a *localAPIServer) handleMCPToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	serverName := strings.TrimPrefix(r.URL.Path, "/api/mcp-servers/toggle/")
	if serverName == "" {
		jsonError(w, http.StatusBadRequest, "server name required")
		return
	}

	configPath := mcpproxy.GetDefaultConfigPath()
	cfg, err := mcpproxy.LoadConfig(configPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("load config: %v", err))
		return
	}

	found := false
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == serverName {
			cfg.Servers[i].Enabled = !cfg.Servers[i].Enabled
			found = true
			break
		}
	}

	if !found {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("server %q not found", serverName))
		return
	}

	if err := writeMCPServersConfig(configPath, cfg); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("write config: %v", err))
		return
	}

	jsonResponse(w, map[string]any{"ok": true, "name": serverName})
}

// POST /api/mcp-servers/store — add or update an MCP server
func (a *localAPIServer) handleMCPSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
		return
	}

	var incoming mcpproxy.ServerConfig
	if err := json.Unmarshal(body, &incoming); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("parse body: %v", err))
		return
	}

	if incoming.Name == "" {
		jsonError(w, http.StatusBadRequest, "server name is required")
		return
	}

	configPath := mcpproxy.GetDefaultConfigPath()
	cfg, err := mcpproxy.LoadConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg = &mcpproxy.Config{}
		} else {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("load config: %v", err))
			return
		}
	}

	// Update existing or append
	found := false
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == incoming.Name {
			cfg.Servers[i] = incoming
			found = true
			break
		}
	}
	if !found {
		cfg.Servers = append(cfg.Servers, incoming)
	}

	if err := writeMCPServersConfig(configPath, cfg); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("write config: %v", err))
		return
	}

	jsonResponse(w, map[string]any{"ok": true, "name": incoming.Name})
}

// DELETE /api/mcp-servers/delete/{name} — remove an MCP server
func (a *localAPIServer) handleMCPDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	serverName := strings.TrimPrefix(r.URL.Path, "/api/mcp-servers/delete/")
	if serverName == "" {
		// Also check for DELETE on /api/mcp-servers/{name} pattern
		serverName = strings.TrimPrefix(r.URL.Path, "/api/mcp-servers/")
	}
	if serverName == "" {
		jsonError(w, http.StatusBadRequest, "server name required")
		return
	}

	configPath := mcpproxy.GetDefaultConfigPath()
	cfg, err := mcpproxy.LoadConfig(configPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("load config: %v", err))
		return
	}

	before := len(cfg.Servers)
	filtered := make([]mcpproxy.ServerConfig, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		if s.Name != serverName {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) == before {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("server %q not found", serverName))
		return
	}

	cfg.Servers = filtered
	if err := writeMCPServersConfig(configPath, cfg); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("write config: %v", err))
		return
	}

	jsonResponse(w, map[string]any{"ok": true, "name": serverName})
}

// writeMCPServersConfig writes an MCP Config to the JSON file.
func writeMCPServersConfig(path string, cfg *mcpproxy.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// ─── JSON Helpers ─────────────────────────────────────────────

func jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// corsMiddleware adds permissive CORS headers for localhost access.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// safeStrAny safely extracts a string from a map by key.
func safeStrAny(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return ""
	}
}

// safeIntAny safely extracts an int from a map by key.
func safeIntAny(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

// safeBoolAny safely extracts a bool from a map by key.
func safeBoolAny(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// SchemaAPIResponse is the JSON response for GET /api/schema.
type SchemaAPIResponse struct {
	NodeTypes     []schema.EnrichedSchemaType  `json:"node_types"`
	Relationships []schema.EnrichedRelationship `json:"relationships"`
}

// GET /api/schema — returns embedded graph schema definitions (object types + relationships).
func (a *localAPIServer) handleSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	nodeTypes, rels, err := schema.LoadDefinitions()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := SchemaAPIResponse{
		NodeTypes:     nodeTypes,
		Relationships: rels,
	}
	jsonResponse(w, resp)
}

// GetDefaultLocalAPIPort returns the default port for the local API.
func GetDefaultLocalAPIPort() int {
	return 8890
}

// GetDefaultConfigPathJSON returns the default MCP servers config path.
var GetDefaultMCPServersConfigPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".diane", "mcp-servers.json")
}

// ─── Doctor Check Handler ─────────────────────────────────

// GET /api/doctor — runs diagnostics and returns structured JSON results.
func (a *localAPIServer) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := context.Background()
	pc := a.config
	results := []map[string]any{}

	// 1. Config file
	results = append(results, map[string]any{
		"check":   "config_file",
		"status":  "ok",
		"message": "Config loaded",
		"details": map[string]any{
			"project_id": pc.ProjectID,
			"server_url": pc.ServerURL,
			"mode":       pc.ModeLabel(),
		},
	})

	// 2. API token present
	tokenValid := pc.Token != "" && len(pc.Token) >= 10
	tokenMsg := "Token present and valid length"
	if !tokenValid {
		tokenMsg = "Token missing or too short"
	}
	tokenStatus := "ok"
	if !tokenValid {
		tokenStatus = "error"
	}
	results = append(results, map[string]any{
		"check":   "api_token",
		"status":  tokenStatus,
		"message": tokenMsg,
	})

	if !tokenValid {
		jsonResponse(w, map[string]any{
			"ok":      false,
			"results": results,
		})
		return
	}

	// 3. SDK connection
	bridge, err := memory.New(memory.Config{
		ServerURL:         pc.ServerURL,
		APIKey:            pc.Token,
		ProjectID:         pc.ProjectID,
		OrgID:             pc.OrgID,
		HTTPClientTimeout: 10 * time.Second,
	})
	if err != nil {
		results = append(results, map[string]any{
			"check":   "sdk_connection",
			"status":  "error",
			"message": fmt.Sprintf("SDK init failed: %v", err),
		})
		jsonResponse(w, map[string]any{
			"ok":      false,
			"results": results,
		})
		return
	}
	defer bridge.Close()

	results = append(results, map[string]any{
		"check":   "sdk_connection",
		"status":  "ok",
		"message": "SDK initialized",
	})

	// 4. Project info from auth/me
	authInfo, err := bridge.GetProjectInfo(ctx)
	if err != nil {
		results = append(results, map[string]any{
			"check":   "project_info",
			"status":  "warning",
			"message": fmt.Sprintf("GetProjectInfo failed: %v", err),
		})
	} else {
		results = append(results, map[string]any{
			"check":   "project_info",
			"status":  "ok",
			"message": fmt.Sprintf("Project: %s", authInfo.ProjectName),
			"details": map[string]any{
				"project_name": authInfo.ProjectName,
			},
		})
	}

	// 5. Agent definitions count
	remoteDefs, err := bridge.ListAgentDefs(ctx)
	agentCount := 0
	if err == nil && remoteDefs != nil {
		agentCount = len(remoteDefs.Data)
	}
	agentStatus := "ok"
	agentMsg := fmt.Sprintf("%d agent(s) on MP", agentCount)
	if err != nil {
		agentStatus = "warning"
		agentMsg = fmt.Sprintf("Failed to fetch agent defs: %v", err)
	}
	results = append(results, map[string]any{
		"check":   "agent_definitions",
		"status":  agentStatus,
		"message": agentMsg,
	})

	// 6. Session CRUD
	sessionCRUD := "ok"
	sessionMsg := ""
	session, err := bridge.CreateSession(ctx, "diane-doctor-check")
	if err != nil {
		sessionCRUD = "error"
		sessionMsg = fmt.Sprintf("CreateSession: %v", err)
	} else {
		_, err = bridge.AppendMessage(ctx, session.ID, "user", "doctor test", 0)
		if err != nil {
			sessionCRUD = "error"
			sessionMsg = fmt.Sprintf("AppendMessage: %v", err)
		} else {
			_, err = bridge.GetMessages(ctx, session.ID)
			if err != nil {
				sessionCRUD = "error"
				sessionMsg = fmt.Sprintf("GetMessages: %v", err)
			} else {
				err = bridge.CloseSession(ctx, session.ID)
				if err != nil {
					sessionCRUD = "error"
					sessionMsg = fmt.Sprintf("CloseSession: %v", err)
				} else {
					sessionMsg = "Create, write, read, close — all passed"
				}
			}
		}
	}
	results = append(results, map[string]any{
		"check":   "session_crud",
		"status":  sessionCRUD,
		"message": sessionMsg,
	})

	// 7. Memory search
	searchResults, err := bridge.SearchMemory(ctx, "doctor test", 3)
	searchStatus := "ok"
	searchMsg := fmt.Sprintf("%d result(s)", len(searchResults))
	if err != nil {
		searchStatus = "warning"
		searchMsg = fmt.Sprintf("Search failed: %v", err)
	}
	results = append(results, map[string]any{
		"check":   "memory_search",
		"status":  searchStatus,
		"message": searchMsg,
	})

	// 8. Version info
	results = append(results, map[string]any{
		"check":   "server_version",
		"status":  "ok",
		"message": Version,
	})

	jsonResponse(w, map[string]any{
		"ok":      sessionCRUD == "ok",
		"version": Version,
		"results": results,
	})
}

// ─── Bug Report Handler ───────────────────────────────────

// POST /api/bugreport — accepts an error report from the companion app and creates
// a GitHub issue in the emergent-company/diane repository.
// Uses GH_TOKEN environment variable (set by gh CLI) for authentication.
func (a *localAPIServer) handleBugReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Title       string `json:"title"`
		Body        string `json:"body"`
		Labels      string `json:"labels,omitempty"`      // comma-separated
		Severity    string `json:"severity,omitempty"`    // critical|high|medium|low
		AppVersion  string `json:"app_version,omitempty"` // companion app version
		OSVersion   string `json:"os_version,omitempty"`
		LogSnippet  string `json:"log_snippet,omitempty"` // recent log lines
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.Title == "" {
		jsonError(w, http.StatusBadRequest, "title is required")
		return
	}

	// Build a structured issue body
	var body strings.Builder
	body.WriteString("## Bug Report\n\n")
	if req.Severity != "" {
		fmt.Fprintf(&body, "**Severity:** %s\n\n", req.Severity)
	}
	body.WriteString("### Details\n\n")
	body.WriteString(req.Body)
	body.WriteString("\n\n")
	if req.AppVersion != "" || req.OSVersion != "" {
		body.WriteString("### Environment\n\n")
		if req.AppVersion != "" {
			fmt.Fprintf(&body, "- **App Version:** %s\n", req.AppVersion)
		}
		if req.OSVersion != "" {
			fmt.Fprintf(&body, "- **macOS:** %s\n", req.OSVersion)
		}
		body.WriteString("\n")
	}
	if req.LogSnippet != "" {
		fmt.Fprintf(&body, "### Logs\n\n```\n%s\n```\n\n", req.LogSnippet)
	}
	body.WriteString("---\n_Reported automatically by Diane Companion App_")

	// Create GitHub issue via REST API
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		// Fallback: read token from gh CLI config
		token = readGHTokenFromConfig()
	}
	if token == "" {
		jsonError(w, http.StatusInternalServerError, "GH_TOKEN not set and no gh config found — cannot create GitHub issue")
		return
	}

	apiURL := "https://api.github.com/repos/emergent-company/diane/issues"

	// Default labels
	allLabels := "bug"
	if req.Labels != "" {
		allLabels = "bug," + req.Labels
	}

	payload := map[string]any{
		"title":  req.Title,
		"body":   body.String(),
		"labels": strings.Split(allLabels, ","),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("marshal payload: %v", err))
		return
	}

	httpReq, err := http.NewRequest("POST", apiURL, bytes.NewReader(payloadJSON))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create request: %v", err))
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept", "application/vnd.github.v3+json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "diane-companion-app")

	client := &http.Client{Timeout: 30 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("GitHub API request failed: %v", err))
		return
	}
	defer httpResp.Body.Close()

	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode != http.StatusCreated {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("GitHub API error (HTTP %d): %s", httpResp.StatusCode, string(respBody)))
		return
	}

	var result struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("parse GitHub response: %v", err))
		return
	}

	log.Printf("Bug report created: #%d — %s", result.Number, result.HTMLURL)

	jsonResponse(w, map[string]any{
		"issue_url":    result.HTMLURL,
		"issue_number": result.Number,
	})
}

// readGHTokenFromConfig reads the GitHub token from gh CLI's config file.
// Checks ~/.config/gh/hosts.yml and ~/.config/gh/config.yml for oauth_token
// values under the github.com host. Uses tab/space indentation to track nesting.
func readGHTokenFromConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	paths := []string{
		filepath.Join(home, ".config", "gh", "hosts.yml"),
		filepath.Join(home, ".config", "gh", "config.yml"),
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		inGitHubSection := false
		githubIndent := -1
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			// Count leading whitespace for indentation tracking
			indent := len(line) - len(strings.TrimLeft(line, " \t"))

			// Detect github.com host section start
			if !inGitHubSection && (trimmed == "github.com:" || trimmed == "github.com") {
				inGitHubSection = true
				githubIndent = indent
				continue
			}
			if !inGitHubSection {
				continue
			}
			// If we're back to the same or lesser indent level as github.com,
			// we've left the github.com section
			if indent <= githubIndent {
				inGitHubSection = false
				continue
			}
			// Look for oauth_token inside the github.com section
			if strings.HasPrefix(trimmed, "oauth_token:") {
				parts := strings.SplitN(trimmed, ":", 2)
				if len(parts) == 2 {
					token := strings.TrimSpace(parts[1])
					token = strings.Trim(token, "'\"")
					if token != "" && strings.HasPrefix(token, "gh") {
						return token
					}
				}
			}
		}
	}

	return ""
}
