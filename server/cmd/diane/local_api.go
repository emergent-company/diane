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
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
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
	mux.HandleFunc("/api/mcp-servers", api.handleMCPServersRoot)
	mux.HandleFunc("/api/mcp-servers/", api.handleMCPServersSub)
	mux.HandleFunc("/api/nodes", api.handleNodes)
	mux.HandleFunc("/api/status", api.handleStatus)

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

// ─── Response Types ───────────────────────────────────────────

type sessionJSON struct {
	ID           string `json:"id"`
	Key          string `json:"key,omitempty"`
	Title        string `json:"title,omitempty"`
	Status       string `json:"status,omitempty"`
	MessageCount int    `json:"message_count,omitempty"`
	TotalTokens  int    `json:"total_tokens,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
}

type messageJSON struct {
	ID             string `json:"id"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	SequenceNumber int    `json:"sequence_number,omitempty"`
	TokenCount     int    `json:"token_count,omitempty"`
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

// GET /api/sessions/{id} — get session metadata
// GET /api/sessions/{id}/messages?limit=100&offset=0 — get paginated messages
func (a *localAPIServer) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract session ID from path: /api/sessions/{id} or /api/sessions/{id}/messages
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.SplitN(path, "/", 2)
	sessionID := parts[0]
	if sessionID == "" {
		jsonError(w, http.StatusBadRequest, "session ID required")
		return
	}

	ctx := context.Background()

	// /api/sessions/{id}/messages — return messages for a session
	if len(parts) == 2 && parts[1] == "messages" {
		a.handleSessionMessages(ctx, w, r, sessionID)
		return
	}

	// /api/sessions/{id} — return single session metadata
	session, err := a.bridge.GetSession(ctx, sessionID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("get session: %v", err))
		return
	}

	jsonResponse(w, map[string]any{
		"session": sessionJSON{
			ID:           session.ID,
			Key:          session.Key,
			Title:        session.Title,
			Status:       session.Status,
			MessageCount: session.MessageCount,
			TotalTokens:  session.TotalTokens,
			CreatedAt:    session.CreatedAt.Format(time.RFC3339),
		},
	})
}

// handleSessionMessages returns paginated messages for a session.
func (a *localAPIServer) handleSessionMessages(ctx context.Context, w http.ResponseWriter, r *http.Request, sessionID string) {
	q := r.URL.Query()
	limit, _ := parseIntParam(q.Get("limit"), 0)
	offset, _ := parseIntParam(q.Get("offset"), 0)

	messages, err := a.bridge.GetMessages(ctx, sessionID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("get messages: %v", err))
		return
	}

	total := len(messages)

	// Apply offset/limit
	var slice []memory.Message
	if offset >= total {
		slice = nil
	} else {
		end := offset + limit
		if limit <= 0 || end > total {
			end = total
		}
		slice = messages[offset:end]
	}

	items := make([]messageJSON, 0, len(slice))
	for _, m := range slice {
		items = append(items, messageJSON{
			ID:             m.ID,
			Role:           m.Role,
			Content:        m.Content,
			SequenceNumber: m.Seq,
			TokenCount:     m.TokenCount,
		})
	}

	jsonResponse(w, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// parseIntParam parses an integer query parameter. Returns 0 on empty or invalid.
func parseIntParam(s string, defaultVal int) (int, error) {
	if s == "" {
		return defaultVal, nil
	}
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// GET /api/mcp-servers — list MCP servers from local config
func (a *localAPIServer) handleMCPServersRoot(w http.ResponseWriter, r *http.Request) {
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

// handleMCPServersSub dispatches sub-paths under /api/mcp-servers/.
// Routes:
//   POST /api/mcp-servers/toggle/{name}  — enable/disable
//   POST /api/mcp-servers/save            — add or update
//   DELETE /api/mcp-servers/{name}        — delete
//   GET  /api/mcp-servers/{name}/tools    — list tools
//   GET  /api/mcp-servers/{name}/prompts  — list prompts
func (a *localAPIServer) handleMCPServersSub(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mcp-servers/")

	// POST /api/mcp-servers/save — add or update an MCP server
	if path == "save" {
		a.handleMCPSave(w, r)
		return
	}

	// POST /api/mcp-servers/toggle/{name}
	if strings.HasPrefix(path, "toggle/") {
		name := strings.TrimPrefix(path, "toggle/")
		a.handleMCPToggle(w, r, name)
		return
	}

	// All remaining routes: /api/mcp-servers/{name}[/action]
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		jsonError(w, http.StatusNotFound, "use /api/mcp-servers/{name} or /api/mcp-servers/{name}/tools or /api/mcp-servers/{name}/prompts")
		return
	}
	serverName := parts[0]

	if len(parts) == 2 {
		// /api/mcp-servers/{name}/tools or /{name}/prompts
		switch parts[1] {
		case "tools":
			a.handleMCPTools(w, r, serverName)
		case "prompts":
			a.handleMCPPrompts(w, r, serverName)
		default:
			jsonError(w, http.StatusNotFound, "use tools or prompts: /api/mcp-servers/{name}/tools")
		}
		return
	}

	// DELETE /api/mcp-servers/{name}
	if r.Method == http.MethodDelete {
		a.handleMCPDelete(w, r, serverName)
		return
	}

	jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// GET /api/mcp-servers/{name}/tools — list tools for a specific MCP server
func (a *localAPIServer) handleMCPTools(w http.ResponseWriter, r *http.Request, serverName string) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg, err := a.findMCPServer(serverName)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}

	client, err := connectToMCPServer(cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("connect to %s: %v", serverName, err))
		return
	}
	defer client.Close()

	tools, err := client.ListTools()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list tools for %s: %v", serverName, err))
		return
	}

	jsonResponse(w, map[string]any{
		"tools": tools,
		"total": len(tools),
	})
}

// GET /api/mcp-servers/{name}/prompts — list prompts for a specific MCP server
func (a *localAPIServer) handleMCPPrompts(w http.ResponseWriter, r *http.Request, serverName string) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg, err := a.findMCPServer(serverName)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}

	client, err := connectToMCPServer(cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("connect to %s: %v", serverName, err))
		return
	}
	defer client.Close()

	prompts, err := client.ListPrompts()
	if err != nil {
		// Many MCP servers don't implement prompts/list — treat as empty
		log.Printf("[LOCAL-API] prompts/list for %s failed (non-fatal): %v", serverName, err)
		jsonResponse(w, map[string]any{
			"prompts": []any{},
			"total":   0,
			"note":    "prompts/list not supported by this server",
		})
		return
	}

	jsonResponse(w, map[string]any{
		"prompts": prompts,
		"total":   len(prompts),
	})
}

// POST /api/mcp-servers/toggle/{name} — toggle enabled/disabled
func (a *localAPIServer) handleMCPToggle(w http.ResponseWriter, r *http.Request, serverName string) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
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

	jsonResponse(w, map[string]any{"ok": true, "name": serverName, "enabled": found})
}

// POST /api/mcp-servers/save — add or update an MCP server
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
		if incoming.Enabled {
			incoming.Enabled = true
		}
		cfg.Servers = append(cfg.Servers, incoming)
	}

	if err := writeMCPServersConfig(configPath, cfg); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("write config: %v", err))
		return
	}

	jsonResponse(w, map[string]any{"ok": true, "name": incoming.Name})
}

// DELETE /api/mcp-servers/{name} — remove an MCP server
func (a *localAPIServer) handleMCPDelete(w http.ResponseWriter, r *http.Request, serverName string) {
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

// ─── MCP Config Helpers ───────────────────────────────────────

// findMCPServer loads the config and returns the server config by name.
func (a *localAPIServer) findMCPServer(name string) (*mcpproxy.ServerConfig, error) {
	configPath := mcpproxy.GetDefaultConfigPath()
	cfg, err := mcpproxy.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == name {
			return &cfg.Servers[i], nil
		}
	}
	return nil, fmt.Errorf("server %q not found in MCP config", name)
}

// connectToMCPServer creates a temporary MCP client for a server config.
func connectToMCPServer(cfg *mcpproxy.ServerConfig) (mcpproxy.Client, error) {
	switch cfg.Type {
	case "stdio":
		return mcpproxy.NewMCPClient(cfg.Name, cfg.Command, cfg.Args, cfg.Env, cfg.Timeout)
	case "http", "streamable-http", "sse":
		return mcpproxy.NewHTTPMCPClient(cfg.Name, cfg.URL, cfg.Headers, cfg.OAuth, cfg.Timeout)
	default:
		return nil, fmt.Errorf("unknown MCP server type: %s", cfg.Type)
	}
}

// writeMCPServersConfig writes a Config to the JSON file.
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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
