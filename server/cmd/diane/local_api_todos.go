// Package: main
// Session TODO HTTP handlers.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

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
