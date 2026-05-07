package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/Emergent-Comapny/diane/internal/schema"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/acp"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// localAPIServer serves the local companion API on 127.0.0.1.
// Endpoints consumed by DianeCompanion (DianeAPIClient.swift):
//
//	GET /api/status              → {"ok": true}
//	GET /api/sessions?status=…   → {"items": [GraphObject...]}
//	GET /api/sessions/{id}/messages → {"items": [GraphObject...]}
//	GET /api/mcp-servers         → {"servers": [...]}
//	GET /api/nodes               → {"nodes": [...]}
//	GET /api/doctor              → DoctorResponse
type localAPIServer struct {
	server *http.Server
}

// startLocalAPI creates and starts the local companion API server on the given port.
// Returns immediately — the server runs in its own goroutine.
func startLocalAPI(pc *config.ProjectConfig, port int) (*localAPIServer, error) {
	mux := http.NewServeMux()

	api := &apiHandlers{pc: pc}
	registered := 0
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleStatus(w, r) })
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleStats(w, r) })
	mux.HandleFunc("/api/stats/providers", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleProviderStats(w, r) })
	mux.HandleFunc("/api/stats/objects", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleGraphObjectStats(w, r) })
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleSessions(w, r) })
	mux.HandleFunc("/api/sessions/", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleSessionMessages(w, r) })
	mux.HandleFunc("/api/chat/send", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleChatSend(w, r) })
	mux.HandleFunc("/api/chat/stream", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleChatStream(w, r) })
	mux.HandleFunc("/api/mcp-servers", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleMCPServers(w, r) })
	mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleNodes(w, r) })
	mux.HandleFunc("/api/providers", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleProviders(w, r) })
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleAgents(w, r) })
	mux.HandleFunc("/api/agents/", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleAgentSubRoutes(w, r) })
	mux.HandleFunc("/api/schema", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleSchema(w, r) })
	mux.HandleFunc("/api/schema/objects/", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleSchemaObjects(w, r) })
	mux.HandleFunc("/api/doctor", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleDoctor(w, r) })

	expected := 16
	if registered != expected {
		log.Printf("[LOCAL-API] WARNING: registered %d routes, expected %d — check for missing handlers", registered, expected)
	}

	srv := &localAPIServer{
		server: &http.Server{
			Addr:    fmt.Sprintf("127.0.0.1:%d", port),
			Handler: mux,
		},
	}

	go func() {
		log.Printf("[LOCAL-API] Listening on http://127.0.0.1:%d", port)
		if err := srv.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[LOCAL-API] Server error: %v", err)
		}
	}()

	return srv, nil
}

// close shuts down the local API server.
func (s *localAPIServer) close() {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.server.Shutdown(ctx)
	}
}

// apiHandlers holds shared state for HTTP handlers.
type apiHandlers struct {
	pc *config.ProjectConfig
}

// bridge creates and returns a memory bridge from the project config.
func (h *apiHandlers) bridge(ctx context.Context) (*memory.Bridge, error) {
	return memory.New(memory.Config{
		ServerURL:         h.pc.ServerURL,
		APIKey:            h.pc.Token,
		ProjectID:         h.pc.ProjectID,
		OrgID:             h.pc.OrgID,
		HTTPClientTimeout: 10 * time.Second,
	})
}

// ── Doctor handler ──

// DoctorResult is a single diagnostic check result in the API response.
type DoctorResult struct {
	Check   string `json:"check"`
	Status  string `json:"status"` // "ok", "warning", "error"
	Message string `json:"message"`
	Details any    `json:"details"`
}

// DoctorResponse is the JSON response for GET /api/doctor.
type DoctorResponse struct {
	Ok      bool           `json:"ok"`
	Version string         `json:"version"`
	Results []DoctorResult `json:"results"`
}

// handleDoctor runs diagnostic checks and returns them as JSON.
func (h *apiHandlers) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	results := []DoctorResult{}
	addResult := func(check, status, message string) {
		results = append(results, DoctorResult{
			Check:   check,
			Status:  status,
			Message: message,
			Details: map[string]any{},
		})
	}

	pc := h.pc

	// ── 1. Config file ──
	if pc == nil {
		addResult("config_file", "error", "No project config loaded")
		writeJSON(w, DoctorResponse{Ok: true, Version: Version, Results: results})
		return
	}
	addResult("config_file", "ok", "Project config loaded")

	// ── 2. API token ──
	if pc.Token == "" {
		addResult("api_token", "error", "Not set")
		writeJSON(w, DoctorResponse{Ok: true, Version: Version, Results: results})
		return
	}
	if len(pc.Token) >= 10 {
		addResult("api_token", "ok", "Token is present")
	} else {
		addResult("api_token", "warning", "Token too short to be valid")
	}

	// ── 3. Memory SDK connection ──
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		addResult("sdk_connection", "error", err.Error())
		writeJSON(w, DoctorResponse{Ok: true, Version: Version, Results: results})
		return
	}
	defer bridge.Close()
	addResult("sdk_connection", "ok", "SDK initialized")

	// ── 4. Project info ──
	sdkClient := bridge.Client()
	proj, err := sdkClient.Projects.Get(ctx, pc.ProjectID, nil)
	if err != nil {
		addResult("project_info", "warning", err.Error())
	} else {
		addResult("project_info", "ok", fmt.Sprintf("Project: %q", proj.Name))
		// Set org ID from project if not already set
		if pc.OrgID == "" && proj.OrgID != "" {
			sdkClient.SetContext(proj.OrgID, pc.ProjectID)
		}
	}

	// ── 5. LLM provider ──
	orgID := pc.OrgID
	if orgID == "" {
		if proj == nil {
			if p2, err2 := sdkClient.Projects.Get(ctx, pc.ProjectID, nil); err2 == nil {
				orgID = p2.OrgID
			}
		} else {
			orgID = proj.OrgID
		}
	}
	if orgID == "" {
		addResult("llm_provider", "warning", "Could not determine org ID")
	} else {
		providers, err := sdkClient.Provider.ListOrgConfigs(ctx, orgID)
		if err != nil {
			addResult("llm_provider", "warning", err.Error())
		} else if len(providers) == 0 {
			addResult("llm_provider", "warning", "No org providers configured")
		} else {
			var descs []string
			for _, p := range providers {
				model := p.GenerativeModel
				if model == "" {
					model = "(auto)"
				}
				descs = append(descs, fmt.Sprintf("%s → %s", p.Provider, model))
			}
			addResult("llm_provider", "ok", strings.Join(descs, ", "))
		}
	}

	// ── 6. Agent definitions ──
	remoteDefs, err := bridge.ListAgentDefs(ctx)
	remoteNameSet := map[string]*sdkagents.AgentDefinitionSummary{}
	if err == nil && remoteDefs != nil {
		for i := range remoteDefs.Data {
			d := remoteDefs.Data[i]
			remoteNameSet[d.Name] = &d
		}
	}
	totalRemote := len(remoteNameSet)
	totalLocal := len(pc.Agents)
	deployed := 0
	for name := range pc.Agents {
		if remoteNameSet[name] != nil {
			deployed++
		}
	}

	if err != nil && totalLocal == 0 {
		addResult("agent_definitions", "warning", "Could not fetch agent definitions: "+err.Error())
	} else if totalLocal == 0 && totalRemote == 0 {
		addResult("agent_definitions", "ok", "None configured")
	} else {
		detail := fmt.Sprintf("%d in config, %d on server", totalLocal, totalRemote)
		if totalLocal > 0 && deployed == totalLocal {
			detail += " — all deployed"
			addResult("agent_definitions", "ok", detail)
		} else if totalLocal > 0 {
			detail += fmt.Sprintf(" — %d deployed, %d pending", deployed, totalLocal-deployed)
			addResult("agent_definitions", "warning", detail)
		} else {
			addResult("agent_definitions", "ok", detail)
		}
	}

	writeJSON(w, DoctorResponse{Ok: true, Version: Version, Results: results})
}

// ── Agent handlers ──

// handleAgents routes /api/agents requests.
func (h *apiHandlers) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleListAgents(w, r)
	case http.MethodPost:
		h.handleCreateAgent(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleListAgents lists all agent definitions via the bridge.
func (h *apiHandlers) handleListAgents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	defs, err := bridge.ListAgentDefs(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list agents: "+err.Error())
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

	writeJSON(w, map[string]any{
		"agents": items,
		"total":  len(items),
	})
}

// handleCreateAgent creates a new user-defined agent definition.
func (h *apiHandlers) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var req sdkagents.CreateAgentDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Name == "" {
		jsonError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Apply defaults
	if req.Visibility == "" {
		req.Visibility = "project"
	}
	if req.FlowType == "" {
		req.FlowType = "standard"
	}

	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	resp, err := bridge.CreateAgentDef(ctx, &req)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "create agent: "+err.Error())
		return
	}

	writeJSON(w, resp.Data)
}

// ── Agent Sub-Route Handlers (seed, override) ──

// handleAgentSubRoutes dispatches /api/agents/* requests.
// Routes:
//
//	POST /api/agents/seed                  → trigger re-seed
//	GET  /api/agents/{name}/override       → fetch override config
//	PUT  /api/agents/{name}/override       → save/upsert override config
//	DELETE /api/agents/{name}/override     → delete override config (restore defaults)
func (h *apiHandlers) handleAgentSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		jsonError(w, http.StatusBadRequest, "invalid agent path")
		return
	}

	first := parts[0]
	if first == "seed" {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleSeedAgents(w, r)
		return
	}

	// /api/agents/{name}/override
	if len(parts) < 2 || parts[1] != "override" {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	agentName := parts[0]

	switch r.Method {
	case http.MethodGet:
		h.handleGetAgentOverride(w, r, agentName)
	case http.MethodPut:
		h.handleSaveAgentOverride(w, r, agentName)
	case http.MethodDelete:
		h.handleDeleteAgentOverride(w, r, agentName)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleSeedAgents triggers a re-seed of built-in agents.
func (h *apiHandlers) handleSeedAgents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	memCfg := memory.Config{
		ServerURL:         h.pc.ServerURL,
		APIKey:            h.pc.Token,
		ProjectID:         h.pc.ProjectID,
		OrgID:             h.pc.OrgID,
		HTTPClientTimeout: 30 * time.Second,
	}

	if err := seedBuiltInAgentsFromGraph(ctx, memCfg); err != nil {
		jsonError(w, http.StatusInternalServerError, "seed: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"ok": true})
}

// handleGetAgentOverride returns the override config for a built-in agent.
func (h *apiHandlers) handleGetAgentOverride(w http.ResponseWriter, r *http.Request, agentName string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	gc := bridge.Client().Graph
	resp, err := gc.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  "AgentOverrideConfig",
		Limit: 50,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list overrides: "+err.Error())
		return
	}

	// Find the matching entity
	for _, obj := range resp.Items {
		if obj.Properties == nil {
			continue
		}
		name, _ := obj.Properties["agent_name"].(string)
		if name == agentName {
			// Build a clean override config from properties
			override := buildOverrideFromProperties(obj.Properties)
			writeJSON(w, map[string]any{"overrides": override})
			return
		}
	}

	// No override found — return empty
	writeJSON(w, map[string]any{"overrides": nil})
}

// handleSaveAgentOverride upserts an override config for a built-in agent.
func (h *apiHandlers) handleSaveAgentOverride(w http.ResponseWriter, r *http.Request, agentName string) {
	var req struct {
		SystemPrompt     string   `json:"system_prompt,omitempty"`
		Skills           []string `json:"skills,omitempty"`
		ModelProvider    string   `json:"model_provider,omitempty"`
		ModelName        string   `json:"model_name,omitempty"`
		ModelTemperature float64  `json:"model_temperature,omitempty"`
		ModelMaxTokens   int      `json:"model_max_tokens,omitempty"`
		MaxSteps         int      `json:"max_steps,omitempty"`
		Timeout          int      `json:"timeout,omitempty"`
		Visibility       string   `json:"visibility,omitempty"`
		SandboxEnabled   *bool    `json:"sandbox_enabled,omitempty"`
		Disabled         bool     `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	gc := bridge.Client().Graph

	// Build properties map
	props := map[string]any{
		"agent_name": agentName,
	}
	if req.SystemPrompt != "" {
		props["system_prompt"] = req.SystemPrompt
	}
	if len(req.Skills) > 0 {
		props["skills"] = req.Skills
	}
	if req.ModelProvider != "" {
		props["model_provider"] = req.ModelProvider
	}
	if req.ModelName != "" {
		props["model_name"] = req.ModelName
	}
	if req.ModelTemperature != 0 {
		props["model_temperature"] = req.ModelTemperature
	}
	if req.ModelMaxTokens != 0 {
		props["model_max_tokens"] = req.ModelMaxTokens
	}
	if req.MaxSteps > 0 {
		props["max_steps"] = req.MaxSteps
	}
	if req.Timeout > 0 {
		props["timeout"] = req.Timeout
	}
	if req.Visibility != "" {
		props["visibility"] = req.Visibility
	}
	if req.SandboxEnabled != nil {
		props["sandbox_enabled"] = *req.SandboxEnabled
	}
	props["disabled"] = req.Disabled

	// Look for existing entity to update
	resp, err := gc.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  "AgentOverrideConfig",
		Limit: 50,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list overrides: "+err.Error())
		return
	}

	for _, obj := range resp.Items {
		if obj.Properties == nil {
			continue
		}
		name, _ := obj.Properties["agent_name"].(string)
		if name == agentName {
			// Update existing
			_, err := gc.UpdateObject(ctx, obj.EntityID, &graph.UpdateObjectRequest{
				Properties: props,
			})
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "update override: "+err.Error())
				return
			}
			writeJSON(w, map[string]any{"ok": true, "action": "updated"})
			return
		}
	}

	// Create new
	_, err = gc.CreateObject(ctx, &graph.CreateObjectRequest{
		Type:       "AgentOverrideConfig",
		Properties: props,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "create override: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"ok": true, "action": "created"})
}

// handleDeleteAgentOverride removes the override config for a built-in agent.
func (h *apiHandlers) handleDeleteAgentOverride(w http.ResponseWriter, r *http.Request, agentName string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	gc := bridge.Client().Graph
	resp, err := gc.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  "AgentOverrideConfig",
		Limit: 50,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list overrides: "+err.Error())
		return
	}

	for _, obj := range resp.Items {
		if obj.Properties == nil {
			continue
		}
		name, _ := obj.Properties["agent_name"].(string)
		if name == agentName {
			if err := gc.DeleteObject(ctx, obj.EntityID, nil); err != nil {
				jsonError(w, http.StatusInternalServerError, "delete override: "+err.Error())
				return
			}
			writeJSON(w, map[string]any{"ok": true})
			return
		}
	}

	// No override found — nothing to delete, still success
	writeJSON(w, map[string]any{"ok": true, "note": "no override to delete"})
}

// buildOverrideFromProperties reads an AgentOverrideConfig from graph properties.
func buildOverrideFromProperties(props map[string]any) map[string]any {
	out := map[string]any{
		"agent_name": propStr(props, "agent_name"),
	}
	if v := propStr(props, "system_prompt"); v != "" {
		out["system_prompt"] = v
	}
	if v := propStr(props, "model_provider"); v != "" {
		out["model_provider"] = v
	}
	if v := propStr(props, "model_name"); v != "" {
		out["model_name"] = v
	}
	if v := propStr(props, "visibility"); v != "" {
		out["visibility"] = v
	}
	if v := propFloat(props, "model_temperature"); v != 0 {
		out["model_temperature"] = v
	}
	if v := propInt(props, "model_max_tokens"); v != 0 {
		out["model_max_tokens"] = v
	}
	if v := propInt(props, "max_steps"); v != 0 {
		out["max_steps"] = v
	}
	if v := propInt(props, "timeout"); v != 0 {
		out["timeout"] = v
	}
	if v := propBool(props, "disabled"); v {
		out["disabled"] = true
	}
	if v := propBoolPtr(props, "sandbox_enabled"); v != nil {
		out["sandbox_enabled"] = *v
	}
	// Skills array
	if raw, ok := props["skills"]; ok {
		if arr, ok := raw.([]any); ok {
			skills := make([]string, 0, len(arr))
			for _, s := range arr {
				if str, ok := s.(string); ok {
					skills = append(skills, str)
				}
			}
			if len(skills) > 0 {
				out["skills"] = skills
			}
		}
	}
	return out
}

// ── Property extraction helpers for graph object properties ──

func propStr(props map[string]any, key string) string {
	if props == nil {
		return ""
	}
	v, _ := props[key].(string)
	return v
}

func propFloat(props map[string]any, key string) float64 {
	if props == nil {
		return 0
	}
	v, ok := props[key]
	if !ok {
		return 0
	}
	f, _ := v.(float64)
	return f
}

func propInt(props map[string]any, key string) int {
	if props == nil {
		return 0
	}
	v, ok := props[key]
	if !ok {
		return 0
	}
	f, _ := v.(float64)
	return int(f)
}

func propBool(props map[string]any, key string) bool {
	if props == nil {
		return false
	}
	v, _ := props[key].(bool)
	return v
}

func propBoolPtr(props map[string]any, key string) *bool {
	if props == nil {
		return nil
	}
	v, ok := props[key]
	if !ok {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		return nil
	}
	return &b
}

// handleStatus returns server status including version and config info.
func (h *apiHandlers) handleStatus(w http.ResponseWriter, r *http.Request) {
	cleanVersion := strings.TrimPrefix(Version, "v")
	writeJSON(w, map[string]any{
		"ok":         true,
		"version":    cleanVersion,
		"started_at": startedAt,
		"server_url": h.pc.ServerURL,
		"project_id": h.pc.ProjectID,
	})
}

// handleSessions lists sessions, optionally filtered by status.
func (h *apiHandlers) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	status := r.URL.Query().Get("status")
	resp, err := bridge.Client().Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: "Session",
	})
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	// Filter by status client-side if requested
	items := resp.Items
	if status != "" {
		var filtered []*graph.GraphObject
		for _, obj := range items {
			if obj.Properties != nil {
				if s, ok := obj.Properties["status"].(string); ok && s == status {
					filtered = append(filtered, obj)
				}
			}
		}
		items = filtered
	}

	if items == nil {
		items = []*graph.GraphObject{}
	}

	writeJSON(w, map[string]any{"items": items})
}

// handleSessionMessages dispatches sub-resource operations on a session.
// Paths handled:
//
//	/api/sessions/{id}              — GET: detail, DELETE: close, PATCH: update
//	/api/sessions/{id}/messages     — GET: list messages, POST: append message
//	/api/sessions/{id}/todos        — GET: list todos, POST: create todo
//	/api/sessions/{id}/todos/{tid}  — PATCH: update todo, DELETE: delete todo
func (h *apiHandlers) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	// Extract the session ID from path and determine sub-resource
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")

	// Split path into segments: ["{id}"] or ["{id}", "messages"] or ["{id}", "todos"] or ["{id}", "todos", "{todoID}"]
	parts := strings.SplitN(path, "/", 3)
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return
	}
	sessionID := parts[0]

	// No sub-resource path — operate on the session itself
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.handleGetSession(w, r, sessionID)
		case http.MethodDelete:
			h.handleCloseSession(w, r, sessionID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	sub := parts[1]
	switch sub {
	case "messages":
		switch r.Method {
		case http.MethodGet:
			h.handleGetSessionMessages(w, r, sessionID)
		case http.MethodPost:
			h.handleAppendSessionMessage(w, r, sessionID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "runs":
		if r.Method == http.MethodGet {
			h.handleListSessionRuns(w, r, sessionID)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "todos":
		todoID := ""
		if len(parts) == 3 && parts[2] != "" {
			todoID = parts[2]
		}
		if todoID != "" {
			switch r.Method {
			case http.MethodPatch:
				h.handleUpdateSessionTodo(w, r, sessionID, todoID)
			case http.MethodDelete:
				h.handleDeleteSessionTodo(w, r, sessionID, todoID)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		} else {
			switch r.Method {
			case http.MethodGet:
				h.handleListSessionTodos(w, r, sessionID)
			case http.MethodPost:
				h.handleCreateSessionTodo(w, r, sessionID)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		}
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// ─── Session Sub-Resource Handlers ──────────────────────────

// handleGetSession returns session details (aggregates + metadata).
func (h *apiHandlers) handleGetSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	session, err := bridge.GetSession(ctx, sessionID)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	// Fetch session runs to compute aggregates
	runs, _ := bridge.ListSessionRuns(ctx, sessionID, 200)
	agg := buildSessionAggregates(runs)

	// Format timestamps
	var updatedAt string
	if !session.UpdatedAt.IsZero() {
		updatedAt = session.UpdatedAt.Format(time.RFC3339)
	}

	writeJSON(w, map[string]any{
		"id":            session.ID,
		"key":           session.Key,
		"title":         session.Title,
		"status":        session.Status,
		"message_count": session.MessageCount,
		"total_tokens":  session.TotalTokens,
		"created_at":    session.CreatedAt.Format(time.RFC3339),
		"updated_at":    updatedAt,
		"aggregates":    agg,
	})
}

// buildSessionAggregates computes aggregate run stats from a list of session runs.
func buildSessionAggregates(runs []sdkagentrun.AgentRun) map[string]any {
	totalRuns := len(runs)
	agentNames := make([]string, 0, totalRuns)
	seen := make(map[string]bool)
	var totalCost float64
	var totalInput, totalOutput int64

	for _, run := range runs {
		name := run.AgentName
		if name == "" {
			name = run.AgentID
		}
		if name != "" && !seen[name] {
			seen[name] = true
			agentNames = append(agentNames, name)
		}
		// TokenUsage is available from the list endpoint in newer MP versions
		if run.TokenUsage != nil {
			totalCost += run.TokenUsage.EstimatedCostUSD
			totalInput += run.TokenUsage.TotalInputTokens
			totalOutput += run.TokenUsage.TotalOutputTokens
		}
	}

	agg := map[string]any{
		"total_runs":          totalRuns,
		"agent_names":         agentNames,
		"estimated_cost_usd":  totalCost,
		"total_input_tokens":  totalInput,
		"total_output_tokens": totalOutput,
	}
	return agg
}

// handleListSessionRuns lists agent runs associated with a session.
// GET /api/sessions/{id}/runs
func (h *apiHandlers) handleListSessionRuns(w http.ResponseWriter, r *http.Request, sessionID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	runs, err := bridge.ListSessionRuns(ctx, sessionID, 200)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	if runs == nil {
		runs = []sdkagentrun.AgentRun{}
	}

	// Return simplified run summaries
	type runSummary struct {
		ID          string  `json:"id"`
		AgentName   string  `json:"agent_name"`
		Status      string  `json:"status"`
		StartedAt   string  `json:"started_at"`
		DurationMs  *int    `json:"duration_ms,omitempty"`
		Model       *string `json:"model,omitempty"`
		Provider    *string `json:"provider,omitempty"`
		ErrorMsg    *string `json:"error_message,omitempty"`
	}
	items := make([]runSummary, 0, len(runs))
	for _, run := range runs {
		var startedAt string
		if !run.StartedAt.IsZero() {
			startedAt = run.StartedAt.Format(time.RFC3339)
		}
		items = append(items, runSummary{
			ID:         run.ID,
			AgentName:  run.AgentName,
			Status:     run.Status,
			StartedAt:  startedAt,
			DurationMs: run.DurationMs,
			Model:      run.Model,
			Provider:   run.Provider,
			ErrorMsg:   run.ErrorMessage,
		})
	}

	writeJSON(w, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// handleCloseSession marks a session as completed.
func (h *apiHandlers) handleCloseSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	if err := bridge.CloseSession(ctx, sessionID); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]any{"ok": true})
}

// handleGetSessionMessages returns messages for a session.
func (h *apiHandlers) handleGetSessionMessages(w http.ResponseWriter, r *http.Request, sessionID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	resp, err := bridge.Client().Graph.ListMessages(ctx, sessionID, 100, "")
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	items := resp.Items
	if items == nil {
		items = []*graph.GraphObject{}
	}

	writeJSON(w, map[string]any{"items": items})
}

// handleAppendSessionMessage appends a message to a session.
func (h *apiHandlers) handleAppendSessionMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Role == "" || req.Content == "" {
		jsonError(w, http.StatusBadRequest, "role and content are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	msg, err := bridge.AppendMessage(ctx, sessionID, req.Role, req.Content, 0)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]any{"ok": true, "id": msg.ID})
}

// ─── Session TODO Handlers ──────────────────────────────────

// handleListSessionTodos lists all todos for a session.
func (h *apiHandlers) handleListSessionTodos(w http.ResponseWriter, r *http.Request, sessionID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	todos, err := bridge.ListSessionTodos(ctx, sessionID)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	if todos == nil {
		todos = []memory.SessionTodo{}
	}

	writeJSON(w, map[string]any{"items": todos})
}

// handleCreateSessionTodo creates a new todo for a session.
func (h *apiHandlers) handleCreateSessionTodo(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req struct {
		Content string `json:"content"`
		Order   int    `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Content == "" {
		jsonError(w, http.StatusBadRequest, "content is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	todo, err := bridge.CreateSessionTodo(ctx, sessionID, req.Content, req.Order)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]any{"ok": true, "todo": todo})
}

// handleUpdateSessionTodo updates a specific todo item.
func (h *apiHandlers) handleUpdateSessionTodo(w http.ResponseWriter, r *http.Request, sessionID, todoID string) {
	var req struct {
		Content *string `json:"content,omitempty"`
		Status  *string `json:"status,omitempty"`
		Order   *int    `json:"order,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	todo, err := bridge.UpdateSessionTodo(ctx, todoID, req.Content, req.Status, req.Order)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]any{"ok": true, "todo": todo})
}

// handleDeleteSessionTodo deletes a specific todo item.
func (h *apiHandlers) handleDeleteSessionTodo(w http.ResponseWriter, r *http.Request, sessionID, todoID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	if err := bridge.DeleteSessionTodo(ctx, todoID); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]any{"ok": true})
}

// ─── Chat Send Handler ────────────────────────────────────

// POST /api/chat/send — send a message to a session and run it through the agent pipeline.
// If session_id is empty, creates a new session.
func (h *apiHandlers) handleChatSend(w http.ResponseWriter, r *http.Request) {
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
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
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

	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	// 1. Create or reuse session
	sessionID := req.SessionID
	if sessionID == "" {
		session, err := bridge.CreateSession(ctx, "Chat")
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "create session: "+err.Error())
			return
		}
		sessionID = session.ID
	}

	// 2. Append user message to session
	_, err = bridge.AppendMessage(ctx, sessionID, "user", req.Content, 0)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "append message: "+err.Error())
		return
	}

	// 3. Find agent definition by name
	defs, err := bridge.ListAgentDefs(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list agent defs: "+err.Error())
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
		jsonError(w, http.StatusNotFound, "agent definition "+req.AgentName+" not found")
		return
	}

	// 4. Create runtime agent
	runtimeName := fmt.Sprintf("chat-%s-%d", req.AgentName, time.Now().UnixMilli())
	agent, err := bridge.CreateRuntimeAgent(ctx, runtimeName, defID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "create runtime agent: "+err.Error())
		return
	}
	agentID := agent.Data.ID

	// 5. Ensure cleanup
	defer func() {
		if delErr := bridge.Client().Agents.Delete(ctx, agentID); delErr != nil {
			log.Printf("[CHAT] Failed to clean up runtime agent %s: %v", agentID, delErr)
		} else {
			log.Printf("[CHAT] Cleaned up runtime agent %s", agentID)
		}
	}()

	// 6. Trigger agent with user's message as prompt
	triggerResp, err := bridge.TriggerAgentWithInput(ctx, agentID, req.Content, sessionID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "trigger agent: "+err.Error())
		return
	}
	if !triggerResp.Success || triggerResp.RunID == nil {
		errMsg := "unknown error"
		if triggerResp.Error != nil {
			errMsg = *triggerResp.Error
		}
		jsonError(w, http.StatusInternalServerError, "trigger failed: "+errMsg)
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

		runResp, pollErr := bridge.GetProjectRun(ctx, runID)
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
	msgs, err := bridge.GetRunMessages(ctx, runID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "get run messages: "+err.Error())
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
			bridge.AppendMessage(storeCtx, sessionID, "assistant", assistantText, 0)
		}()
	}

	// 11. Return response
	writeJSON(w, map[string]any{
		"session_id": sessionID,
		"run_id":     runID,
		"messages":   messages,
		"success":    true,
	})
}

// extractContentValue extracts human-readable text from a value in an SDK
// Content map. The "text" key can be stored as string, []string, or []any.
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
		if b, err := json.Marshal(val); err == nil && len(b) > 2 {
			return string(b)
		}
		return ""
	}
}

// handleMCPServers returns the list of configured MCP servers.
func (h *apiHandlers) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	cfg, err := mcpproxy.LoadConfig(mcpproxy.GetDefaultConfigPath())
	if err != nil {
		// No config file is normal — return empty list.
		// A corrupted config file also reaches here, so log a warning.
		log.Printf("[LOCAL-API] MCP config load: %v (returning empty list)", err)
		writeJSON(w, map[string]any{"servers": []any{}})
		return
	}

	type serverEntry struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Enabled bool   `json:"enabled"`
		Command string `json:"command,omitempty"`
		URL     string `json:"url,omitempty"`
	}

	servers := make([]serverEntry, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		entry := serverEntry{
			Name:    s.Name,
			Type:    s.Type,
			Enabled: s.Enabled,
		}
		if s.Type == "stdio" {
			entry.Command = s.Command
		} else {
			entry.URL = s.URL
		}
		servers = append(servers, entry)
	}

	writeJSON(w, map[string]any{"servers": servers})
}

// handleNodes returns connected relay nodes from the Memory Platform.
func (h *apiHandlers) handleNodes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	relayURL := strings.TrimSuffix(h.pc.ServerURL, "/") + "/api/mcp-relay/sessions"

	req, err := http.NewRequestWithContext(ctx, "GET", relayURL, nil)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "nodes: build request: "+err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.pc.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "nodes: query relay: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("nodes: MP returned %d: %s", resp.StatusCode, string(body)))
		return
	}

	var raw any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		jsonError(w, http.StatusInternalServerError, "nodes: decode response: "+err.Error())
		return
	}

	// The MP relay returns nodes as either a flat array or {"items": [...]}
	var nodes []any
	switch v := raw.(type) {
	case []any:
		nodes = v
	case map[string]any:
		// Check known key patterns first.
		if items, ok := v["items"].([]any); ok {
			nodes = items
		} else if items, ok := v["nodes"].([]any); ok {
			nodes = items
		} else if items, ok := v["data"].([]any); ok {
			nodes = items
		} else if items, ok := v["sessions"].([]any); ok {
			nodes = items
		} else {
			// Last resort: scan for any key whose value is an array.
			found := false
			for key, val := range v {
				if arr, ok := val.([]any); ok {
					log.Printf("[LOCAL-API] handleNodes: falling back to map key %q (type %T) for nodes array (len=%d)", key, val, len(arr))
					nodes = arr
					found = true
					break
				}
			}
			if !found {
				// Serialize a prefix of the response for debugging.
				rawJSON, _ := json.Marshal(raw)
				snippet := string(rawJSON)
				if len(snippet) > 500 {
					snippet = snippet[:500] + "..."
				}
				log.Printf("[LOCAL-API] handleNodes: unexpected relay response format — body=%s", snippet)
				jsonError(w, http.StatusInternalServerError, fmt.Sprintf("nodes: unexpected relay response format: %s", snippet))
				return
			}
		}
	default:
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("nodes: unexpected relay response type %T", raw))
		return
	}

	if len(nodes) == 0 {
		log.Printf("[LOCAL-API] WARNING: /api/nodes returned 0 nodes — expected at least the local instance")
	}

	writeJSON(w, map[string]any{"nodes": nodes})
}

// handleProviders returns configured LLM providers from the project config.
func (h *apiHandlers) handleProviders(w http.ResponseWriter, r *http.Request) {
	providers := make([]map[string]any, 0, 2)

	if gp := h.pc.GenerativeProvider; gp != nil {
		providers = append(providers, map[string]any{
			"provider":        gp.Provider,
			"baseUrl":         gp.BaseURL,
			"generativeModel": gp.Model,
		})
	}
	if ep := h.pc.EmbeddingProvider; ep != nil {
		providers = append(providers, map[string]any{
			"provider":        ep.Provider,
			"baseUrl":         ep.BaseURL,
			"generativeModel": ep.Model,
		})
	}

	writeJSON(w, map[string]any{"providers": providers})
}

// GET /api/stats — agent run statistics from the Memory Platform.
func (h *apiHandlers) handleStats(w http.ResponseWriter, r *http.Request) {
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
	opts := &sdkagentrun.RunStatsOptions{Since: &since}

	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("bridge: %v", err))
		return
	}
	defer bridge.Close()

	statsResp, err := bridge.GetProjectRunStats(ctx, opts)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("query stats: %v", err))
		return
	}
	stats := statsResp.Data

	// Fetch agent definitions to enrich stats
	defs, defsErr := bridge.ListAgentDefs(ctx)
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

	merged := make(map[string]*mergedStat)

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

	// Build JSON response types
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
			return items[i].TotalRuns > items[j].TotalRuns
		}
		return items[i].AgentName < items[j].AgentName
	})

	writeJSON(w, map[string]any{
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

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}

// GET /api/stats/providers — provider/model usage summary from recent runs.
func (h *apiHandlers) handleProviderStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 720 {
		hours = 24
	}

	ctx := context.Background()
	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("bridge: %v", err))
		return
	}
	defer bridge.Close()

	client := bridge.Client()
	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	type providerStat struct {
		totalRuns       int
		successRuns     int
		errorRuns       int
		totalInput      int64
		totalOutput     int64
		totalCostUSD    float64
	}

	allStats := make(map[string]*providerStat)
	var totals struct {
		runs    int
		success int
		errors  int
		input   int64
		output  int64
		costUSD float64
	}

	// Paginate through recent runs
	offset := 0
	const pageSize = 200
	for {
		runsResp, err := client.Agents.ListProjectRuns(ctx, h.pc.ProjectID, &sdkagentrun.ListRunsOptions{
			Limit:  pageSize,
			Offset: offset,
		})
		if err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list runs: %v", err))
			return
		}
		if runsResp == nil || len(runsResp.Data.Items) == 0 {
			break
		}

		for _, run := range runsResp.Data.Items {
			if run.StartedAt.Before(since) {
				// Runs are ordered newest-first; once we hit ones before the window, stop
				continue
			}

			provider := "<unknown>"
			if run.Provider != nil && *run.Provider != "" {
				provider = *run.Provider
			}
			model := "<unknown>"
			if run.Model != nil && *run.Model != "" {
				model = *run.Model
			}

			key := provider + "/" + model
			stat, ok := allStats[key]
			if !ok {
				stat = &providerStat{}
				allStats[key] = stat
			}
			stat.totalRuns++
			totals.runs++

			switch run.Status {
			case "success", "completed":
				stat.successRuns++
				totals.success++
			default:
				stat.errorRuns++
				totals.errors++
			}
		}

		if len(runsResp.Data.Items) < pageSize {
			break
		}
		offset += pageSize
	}

	// Build response
	type providerJSON struct {
		ProviderName      string  `json:"provider_name"`
		ModelName         string  `json:"model_name"`
		TotalRuns         int     `json:"total_runs"`
		SuccessRuns       int     `json:"success_runs"`
		ErrorRuns         int     `json:"error_runs"`
		TotalInputTokens  int64   `json:"total_input_tokens"`
		TotalOutputTokens int64   `json:"total_output_tokens"`
		TotalCostUSD      float64 `json:"total_cost_usd"`
	}

	providers := make([]providerJSON, 0, len(allStats))
	for key, stat := range allStats {
		parts := strings.SplitN(key, "/", 2)
		providers = append(providers, providerJSON{
			ProviderName:      parts[0],
			ModelName:         parts[1],
			TotalRuns:         stat.totalRuns,
			SuccessRuns:       stat.successRuns,
			ErrorRuns:         stat.errorRuns,
			TotalInputTokens:  stat.totalInput,
			TotalOutputTokens: stat.totalOutput,
			TotalCostUSD:      stat.totalCostUSD,
		})
	}

	// Sort by runs descending
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].TotalRuns > providers[j].TotalRuns
	})

	writeJSON(w, map[string]any{
		"providers":           providers,
		"total_runs":          totals.runs,
		"total_success":       totals.success,
		"total_errors":        totals.errors,
		"total_input_tokens":  totals.input,
		"total_output_tokens": totals.output,
		"total_cost_usd":      totals.costUSD,
		"hours":               hours,
	})
}

// GET /api/stats/objects — graph object counts from the Memory Platform.
func (h *apiHandlers) handleGraphObjectStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := context.Background()
	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("bridge: %v", err))
		return
	}
	defer bridge.Close()

	stats, err := bridge.GetGraphObjectStats(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("query stats: %v", err))
		return
	}

	writeJSON(w, map[string]any{
		"total":   stats.Total,
		"by_type": stats.ByType,
	})
}

// GET /api/schema — returns embedded graph schema definitions enriched with counts.
func (h *apiHandlers) handleSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	nodeTypes, rels, err := schema.LoadSchemaDefinitions()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "load schema: "+err.Error())
		return
	}

	// Build relationship counts per type
	relCountByType := make(map[string]int)
	for _, rel := range rels {
		relCountByType[rel.SourceType]++
		relCountByType[rel.TargetType]++
	}

	// Query object counts from Memory Platform
	ctx := context.Background()
	typeNames := make([]string, len(nodeTypes))
	for i, nt := range nodeTypes {
		typeNames[i] = nt.TypeName
	}

	bridge, err := h.bridge(ctx)
	objCounts := make(map[string]int)
	if err == nil {
		counts, countErr := bridge.GetObjectCountsForSchema(ctx, typeNames)
		if countErr == nil {
			objCounts = counts
		}
		bridge.Close()
	} else {
		log.Printf("[LOCAL-API] bridge for schema counts: %v", err)
	}

	// Build response
	apiTypes := make([]schema.SchemaNodeTypeJSON, 0, len(nodeTypes))
	for _, nt := range nodeTypes {
		t := nt.ToSchemaNodeTypeJSON()
		t.ObjectCount = objCounts[nt.TypeName]
		t.RelationshipCount = relCountByType[nt.TypeName]
		apiTypes = append(apiTypes, t)
	}

	// Sort by object count descending
	sort.Slice(apiTypes, func(i, j int) bool {
		return apiTypes[i].ObjectCount > apiTypes[j].ObjectCount
	})

	apiRels := make([]map[string]any, 0, len(rels))
	for _, r := range rels {
		apiRels = append(apiRels, r.ToRelationshipJSON())
	}

	if len(apiTypes) == 0 {
		log.Printf("[LOCAL-API] WARNING: /api/schema returned 0 node types — embedded schema definitions may be missing or corrupted")
	}

	writeJSON(w, map[string]any{
		"node_types":    apiTypes,
		"relationships": apiRels,
	})
}

// GET /api/schema/objects/{typeName} — returns recent objects of a given schema type.
func (h *apiHandlers) handleSchemaObjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	typeName := strings.TrimPrefix(r.URL.Path, "/api/schema/objects/")
	if typeName == "" || strings.Contains(typeName, "/") {
		jsonError(w, http.StatusBadRequest, "type name required")
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	ctx := context.Background()
	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	resp, err := bridge.Client().Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  typeName,
		Limit: limit,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list objects: "+err.Error())
		return
	}

	items := resp.Items
	if items == nil {
		items = []*graph.GraphObject{}
	}

	if len(items) == 0 {
		log.Printf("[LOCAL-API] WARNING: /api/schema/objects/%s returned 0 objects — type may not exist in this project", typeName)
	}

	writeJSON(w, map[string]any{
		"type_name": typeName,
		"total":     len(items),
		"objects":   items,
	})
}

// ── ACP SSE Chat Stream ──

// POST /api/chat/stream — sends a message via ACP SSE streaming.
// Request body: {"message": "...", "agent_name": "...", "session_id": "..."}
// Response: text/event-stream with events:
//
//	data: {"type":"token","content":"..."}
//	data: {"type":"tool_call","name":"..."}
//	data: {"type":"tool_result","name":"..."}
//	data: {"type":"message","role":"assistant","content":"..."}
//	data: {"type":"done","session_id":"...","run_id":"..."}
//	data: {"type":"error","message":"..."}
func (h *apiHandlers) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Message   string `json:"message"`
		AgentName string `json:"agent_name"`
		SessionID string `json:"session_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Message == "" {
		jsonError(w, http.StatusBadRequest, "message is required")
		return
	}
	if req.AgentName == "" {
		req.AgentName = "diane-default"
	}

	ctx := context.Background()

	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	acpClient := bridge.ACP()

	// 1. Create or reuse ACP session
	sessionID := req.SessionID
	if sessionID == "" {
		session, err := acpClient.CreateSession(ctx, acp.CreateSessionRequest{
			AgentName: &req.AgentName,
		})
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "create session: "+err.Error())
			return
		}
		sessionID = session.ID
		log.Printf("[CHAT-STREAM] Created ACP session: %s", sessionID[:12])
	}

	// 2. Build message parts from the user input
	message := []acp.MessagePart{
		{ContentType: "text/plain", Content: req.Message},
	}

	// 3. Create streaming run
	stream, err := acpClient.CreateRunStream(ctx, req.AgentName, acp.CreateRunRequest{
		Message:   message,
		SessionID: &sessionID,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "create run stream: "+err.Error())
		return
	}
	defer stream.Close()

	// 4. Set up SSE response headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		jsonError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// 5. Read ACP SSE events and forward to client
	var runID string
	var sawRunCreated, sawRunComplete bool
	var tokenCount, toolCallCount int
	writeEvent := func(evt map[string]any) {
		data, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for {
		event, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("[CHAT-STREAM] ACP stream read error: %v", err)
			writeEvent(map[string]any{"type": "error", "message": err.Error()})
			break
		}

		switch event.Type {
		case "run.created":
			sawRunCreated = true
			if run, ok := event.Data["run"].(map[string]any); ok {
				if id, ok := run["id"].(string); ok && id != "" {
					runID = id
				}
			}
			if runID == "" {
				log.Printf("[CHAT-STREAM] WARNING: run.created event had no run.id — runID empty")
			}

		case "run.in-progress":
			if runID == "" {
				if run, ok := event.Data["run"].(map[string]any); ok {
					if id, ok := run["id"].(string); ok && id != "" {
						runID = id
					}
				}
			}

		case "message.part":
			part, ok := event.Data["part"].(map[string]any)
			if !ok {
				log.Printf("[CHAT-STREAM] WARNING: message.part event had no 'part' in data — %+v", event.Data)
				continue
			}
			contentType, _ := part["content_type"].(string)
			if contentType == "" {
				log.Printf("[CHAT-STREAM] WARNING: message.part had empty content_type — %+v", part)
				continue
			}
			switch contentType {
			case "text/plain":
				content, _ := part["content"].(string)
				if content != "" {
					tokenCount++
					writeEvent(map[string]any{
						"type":    "token",
						"content": content,
					})
				}
			case "application/json":
				if meta, ok := part["metadata"].(map[string]any); ok {
					kind, _ := meta["kind"].(string)
					if kind == "trajectory" {
						toolName, _ := meta["tool_name"].(string)
						if toolName == "" {
							log.Printf("[CHAT-STREAM] WARNING: trajectory part had empty tool_name")
						}
						eventType := "tool_call"
						if _, hasOutput := meta["tool_output"]; hasOutput {
							eventType = "tool_result"
						}
						toolCallCount++
						writeEvent(map[string]any{
							"type": eventType,
							"name": toolName,
						})
					}
				}
			default:
				log.Printf("[CHAT-STREAM] WARNING: unexpected message.part content_type %q", contentType)
			}

		case "message.created":
			if msg, ok := event.Data["message"].(map[string]any); ok {
				role, _ := msg["role"].(string)
				if role == "" {
					log.Printf("[CHAT-STREAM] WARNING: message.created had empty role")
				}
				var textContent string
				if parts, ok := msg["parts"].([]any); ok {
					for _, p := range parts {
						if pm, ok := p.(map[string]any); ok {
							if ct, _ := pm["content_type"].(string); ct == "text/plain" {
								if c, _ := pm["content"].(string); c != "" {
									textContent += c
								}
							}
						}
					}
				}
				writeEvent(map[string]any{
					"type":    "message",
					"role":    role,
					"content": textContent,
				})
			} else {
				log.Printf("[CHAT-STREAM] WARNING: message.created had no 'message' in data — %+v", event.Data)
			}

		case "run.completed":
			sawRunComplete = true
			if runID == "" {
				log.Printf("[CHAT-STREAM] WARNING: run.completed with no runID captured — stream may have missed run.created")
			}
			writeEvent(map[string]any{
				"type":       "done",
				"session_id": sessionID,
				"run_id":     runID,
			})

		case "run.failed", "run.cancelled":
			errMsg := "run " + strings.TrimPrefix(event.Type, "run.")
			if run, ok := event.Data["run"].(map[string]any); ok {
				if e, ok := run["error"].(map[string]any); ok {
					if m, ok := e["message"].(string); ok {
						errMsg = m
					}
				}
			}
			log.Printf("[CHAT-STREAM] Run %s: %s (session=%s)", runID[:min(len(runID), 12)], event.Type, sessionID[:min(len(sessionID), 12)])
			writeEvent(map[string]any{
				"type":    "error",
				"message": errMsg,
			})

		case "run.awaiting":
			log.Printf("[CHAT-STREAM] Agent paused, awaiting input (session=%s run=%s)", sessionID[:min(len(sessionID), 12)], runID[:min(len(runID), 12)])

		case "error":
			errMsg := "stream error"
			if e, ok := event.Data["error"].(map[string]any); ok {
				if m, ok := e["message"].(string); ok {
					errMsg = m
				}
			}
			log.Printf("[CHAT-STREAM] ACP error event: %s", errMsg)
			writeEvent(map[string]any{
				"type":    "error",
				"message": errMsg,
			})

		default:
			log.Printf("[CHAT-STREAM] WARNING: unexpected ACP event type %q — data: %+v", event.Type, event.Data)
		}
	}

	// Post-stream diagnostics
	if !sawRunCreated {
		log.Printf("[CHAT-STREAM] WARNING: stream ended without run.created event — empty or failed run (session=%s)", sessionID[:min(len(sessionID), 12)])
	}
	if !sawRunComplete && tokenCount == 0 {
		log.Printf("[CHAT-STREAM] WARNING: stream ended with no run.completed and 0 tokens — possible silent failure (session=%s)", sessionID[:min(len(sessionID), 12)])
	}
	log.Printf("[CHAT-STREAM] Stream complete: session=%s run=%s tokens=%d tool_calls=%d complete=%v",
		sessionID[:min(len(sessionID), 12)],
		runID[:min(len(runID), 12)],
		tokenCount, toolCallCount, sawRunComplete)
}

// writeJSON marshals v as JSON and writes it to w with Content-Type header.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[LOCAL-API] JSON encode error: %v", err)
	}
}
