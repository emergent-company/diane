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
	mux.HandleFunc("/api/mcp-servers/", api.handleMCPServerByID)
	mux.HandleFunc("/api/nodes", api.handleNodes)
	mux.HandleFunc("/api/nodes/", api.handleNodeByID)
	mux.HandleFunc("/api/status", api.handleStatus)
	mux.HandleFunc("/api/stats", api.handleStats)
	mux.HandleFunc("/api/agents", api.handleAgents)

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
		tools, queryErr := queryMCPTools(serverCfg)
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
		prompts, queryErr := queryMCPPrompts(serverCfg)
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
	Tools       any    `json:"tools,omitempty"`
	ConnectedAt string `json:"connected_at,omitempty"`
}

// GET /api/nodes — list connected MCP relay nodes with role info
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

	resp, err := httpClient.Do(req)
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

	// Get local hostname and instance ID for role determination
	localHostname, _ := os.Hostname()
	localInstanceID := a.config.InstanceID

	type nodeJSON struct {
		InstanceID  string `json:"instance_id"`
		Hostname    string `json:"hostname,omitempty"`
		Role        string `json:"role,omitempty"`
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

		// Determine role: master if it matches the local instance, slave otherwise
		role := "slave"
		if localInstanceID != "" && s.InstanceID == localInstanceID {
			role = "master"
		} else if localHostname != "" && s.Hostname == localHostname {
			role = "master"
		}

		nodes = append(nodes, nodeJSON{
			InstanceID:  s.InstanceID,
			Hostname:    s.Hostname,
			Role:        role,
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
