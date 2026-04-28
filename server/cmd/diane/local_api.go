package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/db"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
	"github.com/Emergent-Comapny/diane/internal/memory"
)

// localAPIServer manages the local HTTP API for the companion app.
type localAPIServer struct {
	server *http.Server
	config *config.ProjectConfig
	bridge *memory.Bridge
	port   int
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
		config: pc,
		bridge: bridge,
		port:   port,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", api.handleSessions)
	mux.HandleFunc("/api/sessions/", api.handleSessionByID)
	mux.HandleFunc("/api/mcp-servers", api.handleMCPServers)
	mux.HandleFunc("/api/nodes", api.handleNodes)
	mux.HandleFunc("/api/status", api.handleStatus)
	mux.HandleFunc("/api/stats", api.handleStats)

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
}

// ─── Handlers ────────────────────────────────────────────────

// GET /api/sessions — list all sessions
func (a *localAPIServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

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
	}

	items := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, sessionJSON{
			ID:           s.ID,
			Key:          s.Key,
			Title:        s.Title,
			Status:       s.Status,
			MessageCount: s.MessageCount,
			TotalTokens:  s.TotalTokens,
			CreatedAt:    s.CreatedAt.Format(time.RFC3339),
		})
	}

	jsonResponse(w, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// GET /api/sessions/{id}/messages — get messages for a session
func (a *localAPIServer) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract session ID from path: /api/sessions/{id}/messages
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] != "messages" {
		jsonError(w, http.StatusNotFound, "use /api/sessions/{id}/messages")
		return
	}
	sessionID := parts[0]
	if sessionID == "" {
		jsonError(w, http.StatusBadRequest, "session ID required")
		return
	}

	ctx := context.Background()
	messages, err := a.bridge.GetMessages(ctx, sessionID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("get messages: %v", err))
		return
	}

	type messageJSON struct {
		ID             string `json:"id"`
		Role           string `json:"role"`
		Content        string `json:"content"`
		SequenceNumber int    `json:"sequence_number,omitempty"`
		TokenCount     int    `json:"token_count,omitempty"`
	}

	items := make([]messageJSON, 0, len(messages))
	for _, m := range messages {
		items = append(items, messageJSON{
			ID:             m.ID,
			Role:           m.Role,
			Content:        m.Content,
			SequenceNumber: m.Seq,
			TokenCount:     m.TokenCount,
		})
	}

	jsonResponse(w, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// GET /api/mcp-servers — list MCP servers from local config
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

// relaySessionData represents a connected MCP relay instance from the MP API.
type relaySessionData struct {
	InstanceID  string `json:"instance_id"`
	Hostname    string `json:"hostname,omitempty"`
	Version     string `json:"version,omitempty"`
	Tools       any    `json:"tools,omitempty"`
	ConnectedAt string `json:"connected_at,omitempty"`
}

// GET /api/nodes — list connected MCP relay nodes
func (a *localAPIServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	relayURL := strings.TrimSuffix(a.config.ServerURL, "/") + "/api/mcp-relay/sessions"
	req, err := http.NewRequest("GET", relayURL, nil)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create request: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+a.config.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("query nodes: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		jsonError(w, resp.StatusCode, fmt.Sprintf("relay API returned %d", resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("read response: %v", err))
		return
	}

	// Parse response — could be array, {"items":[...]}, {"data":[...]}, or {"sessions":[...]}
	var sessions []relaySessionData
	if err := json.Unmarshal(body, &sessions); err != nil {
		// Try wrapped response formats
		var wrapped struct {
			Items    []relaySessionData `json:"items"`
			Data     []relaySessionData `json:"data"`
			Sessions []relaySessionData `json:"sessions"`
		}
		if err2 := json.Unmarshal(body, &wrapped); err2 == nil {
			switch {
			case wrapped.Sessions != nil:
				sessions = wrapped.Sessions
			case wrapped.Items != nil:
				sessions = wrapped.Items
			case wrapped.Data != nil:
				sessions = wrapped.Data
			}
		}
		if sessions == nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to parse relay sessions: %s", string(body)))
			return
		}
	}

	type nodeJSON struct {
		InstanceID  string `json:"instance_id"`
		Hostname    string `json:"hostname,omitempty"`
		Version     string `json:"version,omitempty"`
		ToolCount   int    `json:"tool_count,omitempty"`
		ConnectedAt string `json:"connected_at,omitempty"`
	}

	nodes := make([]nodeJSON, 0, len(sessions))
	for _, s := range sessions {
		toolCount := 0
		if s.Tools != nil {
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
		})
	}

	jsonResponse(w, map[string]any{
		"nodes": nodes,
		"total": len(nodes),
	})
}

// GET /api/status — simple health check
func (a *localAPIServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	jsonResponse(w, map[string]any{
		"ok":         true,
		"server_url": a.config.ServerURL,
		"project_id": a.config.ProjectID,
	})
}

// GET /api/stats — agent run statistics (mirrors `diane stats`)
func (a *localAPIServer) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 720 {
		hours = 24
	}

	database, err := db.New("")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("open db: %v", err))
		return
	}
	defer database.Close()

	summaries, err := database.GetAgentStatsSummary(hours)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("query stats: %v", err))
		return
	}

	type summaryJSON struct {
		AgentName         string  `json:"agent_name"`
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
		SuccessRate       float64 `json:"success_rate"`
	}

	type totalsJSON struct {
		TotalRuns       int     `json:"total_runs"`
		TotalSuccess    int     `json:"total_success"`
		TotalErrors     int     `json:"total_errors"`
		TotalDurationMs int     `json:"total_duration_ms"`
		TotalInput      int     `json:"total_input_tokens"`
		TotalOutput     int     `json:"total_output_tokens"`
		OverallAvgDurMs float64 `json:"overall_avg_duration_ms"`
		OverallSuccess  float64 `json:"overall_success_rate"`
	}

	items := make([]summaryJSON, 0, len(summaries))
	var totals totalsJSON
	for _, s := range summaries {
		successRate := float64(0)
		if s.TotalRuns > 0 {
			successRate = float64(s.SuccessRuns) / float64(s.TotalRuns) * 100
		}
		items = append(items, summaryJSON{
			AgentName:         s.AgentName,
			TotalRuns:         s.TotalRuns,
			SuccessRuns:       s.SuccessRuns,
			ErrorRuns:         s.ErrorRuns,
			AvgDurationMs:     s.AvgDurationMs,
			AvgStepCount:      s.AvgStepCount,
			AvgToolCalls:      s.AvgToolCalls,
			AvgInputTokens:    s.AvgInputTokens,
			AvgOutputTokens:   s.AvgOutputTokens,
			TotalDurationMs:   s.TotalDurationMs,
			TotalInputTokens:  s.TotalInputTokens,
			TotalOutputTokens: s.TotalOutputTokens,
			SuccessRate:       successRate,
		})
		totals.TotalRuns += s.TotalRuns
		totals.TotalSuccess += s.SuccessRuns
		totals.TotalErrors += s.ErrorRuns
		totals.TotalDurationMs += s.TotalDurationMs
		totals.TotalInput += s.TotalInputTokens
		totals.TotalOutput += s.TotalOutputTokens
	}
	if totals.TotalRuns > 0 {
		totals.OverallAvgDurMs = float64(totals.TotalDurationMs) / float64(totals.TotalRuns)
		totals.OverallSuccess = float64(totals.TotalSuccess) / float64(totals.TotalRuns) * 100
	}

	jsonResponse(w, map[string]any{
		"agents": items,
		"totals": totals,
		"hours":  hours,
	})
}

// ─── Helpers ─────────────────────────────────────────────────

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
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
