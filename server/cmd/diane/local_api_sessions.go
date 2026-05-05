// Package: main
// Session HTTP handlers.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"github.com/Emergent-Comapny/diane/internal/memory"
)

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
