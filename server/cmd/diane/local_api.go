package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/agents"
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
	api    *apiHandlers  // exposed so callers can inject references (e.g. mcpProxy) after creation
}

// startLocalAPI creates and starts the local companion API server on the given port.
// Returns immediately — the server runs in its own goroutine.
func startLocalAPI(pc *config.ProjectConfig, port int, callbackHost string, mcpProxy *mcpproxy.Proxy) (*localAPIServer, error) {
	mux := http.NewServeMux()

	api := &apiHandlers{pc: pc, callbackHost: callbackHost, mcpProxy: mcpProxy}
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
	mux.HandleFunc("/api/mcp-servers/", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleMCPServerSubRoutes(w, r) })
	mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleNodes(w, r) })
	mux.HandleFunc("/api/nodes/", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleNodeSubRoutes(w, r) })
	mux.HandleFunc("/api/push/send", func(w http.ResponseWriter, r *http.Request) { registered++; api.handlePushSend(w, r) })
	mux.HandleFunc("/api/providers", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleProviders(w, r) })
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleAgents(w, r) })
	mux.HandleFunc("/api/agents/", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleAgentSubRoutes(w, r) })
	mux.HandleFunc("/api/schema", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleSchema(w, r) })
	mux.HandleFunc("/api/schema/objects/", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleSchemaObjects(w, r) })
	mux.HandleFunc("/api/doctor", func(w http.ResponseWriter, r *http.Request) { registered++; api.handleDoctor(w, r) })

	expected := 20
	if registered != expected {
		log.Printf("[LOCAL-API] WARNING: registered %d routes, expected %d — check for missing handlers", registered, expected)
	}

	srv := &localAPIServer{
		server: &http.Server{
			Addr:    fmt.Sprintf("127.0.0.1:%d", port),
			Handler: mux,
		},
		api: api,
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
	pc           *config.ProjectConfig
	callbackHost string             // hostname for OAuth redirect URI ("localhost" = local machine)
	mcpProxy     *mcpproxy.Proxy    // optional reference for log/status queries (may be nil)
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

	if err != nil && totalRemote == 0 {
		addResult("agent_definitions", "warning", "Could not fetch agent definitions: "+err.Error())
	} else if totalRemote == 0 {
		addResult("agent_definitions", "ok", "None configured — run 'diane agent seed'")
	} else {
		builtInSet := map[string]bool{}
		for _, ba := range agents.BuiltInAgents() {
			builtInSet[ba.Name] = true
		}
		builtInCount := 0
		for name := range remoteNameSet {
			if builtInSet[name] {
				builtInCount++
			}
		}
		addResult("agent_definitions", "ok", fmt.Sprintf("%d on MP (%d built-in, %d user-defined)", totalRemote, builtInCount, totalRemote-builtInCount))
	}

	// ── 7. CLI / App version match ──
	appVer := readInstalledAppVersion()
	vmStatus := "ok"
	vmMsg := "Diane.app not installed"
	if appVer != "" {
		cliVer := strings.TrimPrefix(Version, "v")
		appVerClean := strings.TrimPrefix(appVer, "v")
		if cliVer == appVerClean || (cliVer == "dev" && strings.HasPrefix(appVerClean, "dev")) {
			vmStatus = "ok"
			vmMsg = fmt.Sprintf("CLI=%s, App=%s — match", Version, appVer)
		} else {
			vmStatus = "warning"
			vmMsg = fmt.Sprintf("CLI=%s, App=%s — MISMATCH", Version, appVer)
		}
	}
	addResult("version_match", vmStatus, vmMsg)

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
	if len(parts) >= 2 && parts[1] == "override" {
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
		return
	}

	// /api/agents/{name} — single agent detail or delete
	if r.Method == http.MethodDelete {
		h.handleDeleteAgent(w, r, first)
		return
	}
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	h.handleGetAgentDetail(w, r, first)
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

// handleGetAgentDetail returns detail for a single agent by name.
// GET /api/agents/{name}
func (h *apiHandlers) handleGetAgentDetail(w http.ResponseWriter, r *http.Request, agentName string) {
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

	// Find the agent by name
	var found *sdkagents.AgentDefinitionSummary
	for i, d := range defs.Data {
		if d.Name == agentName {
			found = &defs.Data[i]
			break
		}
	}
	if found == nil {
		// Fallback: check built-in registry for known agents
		for _, builtIn := range agents.BuiltInAgents() {
			if builtIn.Name == agentName {
				toolCount := len(builtIn.Tools)
				if builtIn.Tools == nil {
					builtIn.Tools = []string{}
				}
				writeJSON(w, map[string]any{
					"name":        builtIn.Name,
					"description": builtIn.Description,
					"system_prompt": builtIn.SystemPrompt,
					"flow_type":   builtIn.FlowType,
					"visibility":  builtIn.Visibility,
					"is_default":  builtIn.Name == "diane-default",
					"tool_count":  toolCount,
					"tools":       builtIn.Tools,
					"built_in":    true,
				})
				return
			}
		}
		jsonError(w, http.StatusNotFound, "agent not found")
		return
	}

	// Fetch full definition for tools
	fullDef, err := bridge.GetAgentDef(ctx, found.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "get agent: "+err.Error())
		return
	}

	desc := ""
	if found.Description != nil {
		desc = *found.Description
	}

	tools := fullDef.Data.Tools
	if tools == nil {
		tools = []string{}
	}

	writeJSON(w, map[string]any{
		"id":          found.ID,
		"name":        found.Name,
		"description": desc,
		"flow_type":   found.FlowType,
		"visibility":  found.Visibility,
		"is_default":  found.IsDefault,
		"tool_count":  found.ToolCount,
		"tools":       tools,
		"created_at":  found.CreatedAt.Format(time.RFC3339),
		"updated_at":  found.UpdatedAt.Format(time.RFC3339),
	})
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

			// Log the operation
			if logErr := agents.WriteAgentOp(ctx, gc, "override.update", agentName, "local-api", "success", "Agent override config updated"); logErr != nil {
				log.Printf("[operation-log] Failed to write override.update for %s: %v", agentName, logErr)
			}
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

	// Log the operation
	if logErr := agents.WriteAgentOp(ctx, gc, "override.create", agentName, "local-api", "success", "Agent override config created"); logErr != nil {
		log.Printf("[operation-log] Failed to write override.create for %s: %v", agentName, logErr)
	}
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

			// Log the operation
			if logErr := agents.WriteAgentOp(ctx, gc, "override.delete", agentName, "local-api", "success", "Agent override config deleted (restored to defaults)"); logErr != nil {
				log.Printf("[operation-log] Failed to write override.delete for %s: %v", agentName, logErr)
			}
			return
		}
	}

	// No override found — nothing to delete, still success
	writeJSON(w, map[string]any{"ok": true, "note": "no override to delete"})
}

// handleDeleteAgent handles DELETE /api/agents/{name}.
// Built-in agents → disable via graph override + re-seed.
// User-defined agents → delete from MP API + local config.
func (h *apiHandlers) handleDeleteAgent(w http.ResponseWriter, r *http.Request, agentName string) {
	// Check if built-in
	isBuiltIn := false
	for _, ba := range agents.BuiltInAgents() {
		if ba.Name == agentName {
			isBuiltIn = true
			break
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	if isBuiltIn {
		// Create/update override with disabled=true
		if err := agents.UpsertDisableOverride(ctx, bridge.Client().Graph, agentName); err != nil {
			jsonError(w, http.StatusInternalServerError, "disable override: "+err.Error())
			return
		}

		// Re-seed (skip disabled agents)
		builtIns, buildErr := agents.BuildMergedAgents(ctx, bridge.Client().Graph)
		if buildErr != nil {
			jsonError(w, http.StatusInternalServerError, "build merged agents: "+buildErr.Error())
			return
		}
		allBuiltIns := agents.BuiltInAgents()
		allNames := make([]string, len(allBuiltIns))
		for i, a := range allBuiltIns {
			allNames[i] = a.Name
		}
		if err := agents.SeedAgentList(ctx, bridge.Client(), builtIns, allNames); err != nil {
			jsonError(w, http.StatusInternalServerError, "re-seed: "+err.Error())
			return
		}

		writeJSON(w, map[string]any{"status": "disabled"})

		// Log the operation
		if logErr := agents.WriteAgentOp(ctx, bridge.Client().Graph, "agent.delete", agentName, "local-api", "success", "Built-in agent disabled via graph override"); logErr != nil {
			log.Printf("[operation-log] Failed to write agent.delete for %s: %v", agentName, logErr)
		}
		return
	}

	// User-defined: delete from MP
	defs, err := bridge.ListAgentDefs(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list defs: "+err.Error())
		return
	}
	for _, def := range defs.Data {
		if def.Name == agentName {
			if delErr := bridge.DeleteAgentDef(ctx, def.ID); delErr != nil {
				jsonError(w, http.StatusInternalServerError, "delete: "+delErr.Error())
				return
			}
			break
		}
	}

	// (config no longer stores agent definitions — all agents live on MP)
	writeJSON(w, map[string]any{"status": "deleted"})

	// Log the operation
	if logErr := agents.WriteAgentOp(ctx, bridge.Client().Graph, "agent.delete", agentName, "local-api", "success", "User-defined agent deleted from config and MP"); logErr != nil {
		log.Printf("[operation-log] Failed to write agent.delete for %s: %v", agentName, logErr)
	}
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
	exe, _ := os.Executable()
	writeJSON(w, map[string]any{
		"ok":          true,
		"version":     cleanVersion,
		"binary_path": exe,
		"started_at":  startedAt,
		"server_url":  h.pc.ServerURL,
		"project_id":  h.pc.ProjectID,
	})
}

// handleSessions lists sessions (GET) or creates a new session (POST).
func (h *apiHandlers) handleSessions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	switch r.Method {
	case http.MethodGet:
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

	case http.MethodPost:
		var body struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
			writeJSON(w, map[string]any{"error": "invalid request body: " + err.Error()})
			return
		}

		session, err := bridge.CreateSession(ctx, body.Title)
		if err != nil {
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}

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
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
		"id":           session.ID,
		"entity_id":    session.ID,
		"canonical_id": session.ID,
		"key":          session.Key,
		"title":        session.Title,
		"status":       session.Status,
		"message_count": session.MessageCount,
		"total_tokens": session.TotalTokens,
		"created_at":   session.CreatedAt.Format(time.RFC3339),
		"updated_at":   updatedAt,
		"aggregates":   agg,
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

// handleMCPServers returns the list of MCP servers from the graph.
func (h *apiHandlers) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Query MCP proxy configs from the graph
	bridge, err := h.bridge(ctx)
	if err != nil {
		log.Printf("[LOCAL-API] handleMCPServers: bridge: %v", err)
		writeJSON(w, map[string]any{"servers": []any{}})
		return
	}
	defer bridge.Close()

	entries, err := bridge.ListMCPProxyConfigs(ctx)
	if err != nil {
		log.Printf("[LOCAL-API] list MCP proxy configs: %v", err)
		writeJSON(w, map[string]any{"servers": []any{}})
		return
	}

		type serverEntry struct {
			Name         string `json:"name"`
			Type         string `json:"type"`
			Enabled      bool   `json:"enabled"`
			Scope        string `json:"scope"`
			Version      int    `json:"version"`
			Status       string `json:"status"`
			ErrorMessage string `json:"error_message,omitempty"`
		}

		servers := make([]serverEntry, 0, len(entries))
		seen := make(map[string]int) // server name → index in servers (dedup by highest version)
		for _, e := range entries {
			var fullConfig mcpproxy.ServerConfig
			if err := json.Unmarshal([]byte(e.Config), &fullConfig); err != nil {
				log.Printf("[LOCAL-API] mcp server config unmarshal (%s): %v", e.Scope, err)
				continue
			}

			// Compute runtime status
			status := "running"
			var errorMsg string
			if !fullConfig.Enabled {
				status = "disabled"
			} else if fullConfig.Type == "http" || fullConfig.Type == "sse" || fullConfig.Type == "streamable-http" {
			// Check OAuth status from graph config OR locally discovered config
			// (graph config may lack oauth; locally discovered config covers that case)
			oauth := fullConfig.OAuth
			if oauth == nil {
				oauth = mcpproxy.LoadDiscoveredConfig(fullConfig.Name)
			}
			if oauth != nil {
				tokens, err := mcpproxy.LoadTokens(fullConfig.Name)
				if err != nil {
					if os.IsNotExist(err) {
						status = "auth_required"
						errorMsg = "OAuth required — no valid tokens found. Re-authenticate in companion app."
					} else {
						status = "error"
						errorMsg = fmt.Sprintf("failed to load auth tokens: %v", err)
					}
			} else if !tokens.ExpiresAt.IsZero() && time.Now().After(tokens.ExpiresAt) {
				// Token expired — try to refresh before reporting expired
				if oauth != nil && oauth.TokenURL != "" && oauth.ClientID != "" && tokens.RefreshToken != "" {
					newTokens, refreshErr := mcpproxy.RefreshTokens(oauth.TokenURL, oauth.ClientID, tokens.RefreshToken)
					if refreshErr == nil {
						log.Printf("[LOCAL-API] Auto-refreshed tokens for %s (new expiry: %s)", fullConfig.Name, newTokens.ExpiresAt.Format(time.RFC3339))
						_ = mcpproxy.SaveTokens(fullConfig.Name, newTokens)
						status = "running"
					} else {
						log.Printf("[LOCAL-API] Token refresh failed for %s: %v", fullConfig.Name, refreshErr)
						status = "auth_expired"
						errorMsg = fmt.Sprintf("auth tokens expired at %s — re-authenticate in companion app", tokens.ExpiresAt.Format(time.RFC3339))
					}
				} else {
					log.Printf("[LOCAL-API] Token expired for %s but cannot refresh: oauth=%v hasTokenURL=%t hasClientID=%t hasRefreshToken=%t",
						fullConfig.Name, oauth != nil, oauth != nil && oauth.TokenURL != "",
						oauth != nil && oauth.ClientID != "", tokens.RefreshToken != "")
					status = "auth_expired"
					errorMsg = fmt.Sprintf("auth tokens expired at %s — re-authenticate in companion app", tokens.ExpiresAt.Format(time.RFC3339))
				}
				}
			}
			}

			if idx, exists := seen[fullConfig.Name]; exists {
				// Keep the entry with the higher version (newer config wins)
				if e.Version > servers[idx].Version {
					servers[idx] = serverEntry{
						Name:         fullConfig.Name,
						Type:         fullConfig.Type,
						Enabled:      fullConfig.Enabled,
						Scope:        e.Scope,
						Version:      e.Version,
						Status:       status,
						ErrorMessage: errorMsg,
					}
				}
				continue
			}
			seen[fullConfig.Name] = len(servers)
			servers = append(servers, serverEntry{
				Name:         fullConfig.Name,
				Type:         fullConfig.Type,
				Enabled:      fullConfig.Enabled,
				Scope:        e.Scope,
				Version:      e.Version,
				Status:       status,
				ErrorMessage: errorMsg,
			})
		}

	// ── Relay tools check: override status for enabled servers with no tools ──
	// Queries ALL connected relay sessions' tools, not just the local instance,
	// so servers running on other nodes (e.g., infakt on tool-test) report
	// correctly instead of always showing "no_tools".
	if len(servers) > 0 && h.pc.ServerURL != "" {
		// 1. Get all active sessions
		relayURL := strings.TrimSuffix(h.pc.ServerURL, "/") + "/api/mcp-relay/sessions"
		sessions := h.queryRelaySessions(r.Context(), relayURL)

		// 2. Collect tools from all sessions, grouped by instance ID
		type instanceTools struct {
			InstanceID string
			Tools      []map[string]any
		}
		var allInstanceTools []instanceTools
		for _, instID := range sessions {
			if instID == "" {
				continue
			}
			tools := h.queryInstanceTools(r.Context(), instID)
			if len(tools) > 0 {
				allInstanceTools = append(allInstanceTools, instanceTools{InstanceID: instID, Tools: tools})
			}
		}

		// 3. Count tools per server across all instances
		toolCounts := make(map[string]int)
		for _, it := range allInstanceTools {
			for _, t := range it.Tools {
				name, _ := t["name"].(string)
				if name == "" {
					continue
				}
				for _, s := range servers {
					prefix := it.InstanceID + "_" + s.Name + "_"
					if strings.HasPrefix(name, prefix) {
						toolCounts[s.Name]++
						break
					}
				}
			}
		}

	// 4. Override status for enabled servers with no tools.
	// Preserve auth_required/auth_expired — they're actionable.
	for i, s := range servers {
		if s.Status == "running" && toolCounts[s.Name] == 0 {
			servers[i].Status = "no_tools"
			msg := "server connected but no tools registered"
			// Add scope context when the server is bound to a different instance
			if s.Scope != "" && !strings.HasSuffix(s.Scope, h.pc.InstanceID) {
				msg = fmt.Sprintf("server bound to %s — tools not visible from this node", s.Scope)
			}
			servers[i].ErrorMessage = msg
		}
	}
	}

	writeJSON(w, map[string]any{"servers": servers})
}

// ── MCP server sub-routes ──

func (h *apiHandlers) handleMCPServerSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mcp-servers/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		jsonError(w, http.StatusBadRequest, "invalid MCP server name")
		return
	}
	serverName, err := url.PathUnescape(parts[0])
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid server name encoding")
		return
	}

	if len(parts) == 2 && parts[1] == "tools" {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleMCPServerTools(w, r, serverName)
		return
	}

	if len(parts) == 2 && parts[1] == "prompts" {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		// Prompts not yet supported at proxy level — return empty list
		writeJSON(w, map[string]any{"prompts": []any{}})
		return
	}

	if len(parts) == 1 && parts[0] == "auth" {
		// POST /api/mcp-servers/{name}/auth
		// Starts or resumes the OAuth authorization flow.
		// name is already extracted in serverName above (before the tools/prompts check)
		// BUT: this branch handles the case where path is just "auth" — need the name
		jsonError(w, http.StatusNotFound, "not found")
		return
	}

	if len(parts) == 2 && parts[1] == "auth" {
		h.handleMCPServerAuth(w, r, serverName)
		return
	}

	if len(parts) == 2 && parts[1] == "auth-status" {
		h.handleMCPServerAuthStatus(w, r, serverName)
		return
	}

	if len(parts) == 2 && parts[1] == "scope" {
		// PUT /api/mcp-servers/{name}/scope
		// Updates the node binding scope for this MCP server.
		if r.Method != http.MethodPut {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed (use PUT)")
			return
		}
		h.handleMCPServerUpdateScope(w, r, serverName)
		return
	}

	if len(parts) == 2 && parts[1] == "logs" {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleMCPServerLogs(w, r, serverName)
		return
	}

	jsonError(w, http.StatusNotFound, "not found")
}

// handleMCPServerUpdateScope updates the node binding scope for an MCP server.
// PUT /api/mcp-servers/{name}/scope  body: {"scope": "instance:mcj-mini"}
func (h *apiHandlers) handleMCPServerUpdateScope(w http.ResponseWriter, r *http.Request, serverName string) {
	ctx := r.Context()

	var req struct {
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Scope == "" {
		jsonError(w, http.StatusBadRequest, "scope is required")
		return
	}

	// Validate scope format
	if req.Scope != "all" && !strings.HasPrefix(req.Scope, "instance:") && !strings.HasPrefix(req.Scope, "slave:") {
		jsonError(w, http.StatusBadRequest, "scope must be 'all', 'instance:<id>', or 'slave:*'")
		return
	}

	bridge, err := h.bridge(ctx)
	if err != nil {
		log.Printf("[LOCAL-API] handleMCPServerUpdateScope: bridge: %v", err)
		jsonError(w, http.StatusInternalServerError, "failed to connect to Memory Platform")
		return
	}
	defer bridge.Close()

	// Find the entity ID by listing configs and matching server name in config JSON
	entries, err := bridge.ListMCPProxyConfigs(ctx)
	if err != nil {
		log.Printf("[LOCAL-API] handleMCPServerUpdateScope: list configs: %v", err)
		jsonError(w, http.StatusInternalServerError, "failed to list MCP configs")
		return
	}

	var targetEntityID string
	for _, e := range entries {
		var nameCheck struct {
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(e.Config), &nameCheck) == nil && nameCheck.Name == serverName {
			targetEntityID = e.EntityID
			break
		}
	}

	if targetEntityID == "" {
		jsonError(w, http.StatusNotFound, "MCP server not found: "+serverName)
		return
	}

	if err := bridge.UpdateMCPProxyConfigScope(ctx, targetEntityID, req.Scope); err != nil {
		log.Printf("[LOCAL-API] handleMCPServerUpdateScope: update: %v", err)
		jsonError(w, http.StatusInternalServerError, "failed to update scope")
		return
	}

	log.Printf("[LOCAL-API] Updated MCP server %s scope to %s", serverName, req.Scope)
	writeJSON(w, map[string]any{"status": "ok", "server": serverName, "scope": req.Scope})
}

// handleMCPServerLogs returns recent log entries for a specific MCP server.
// GET /api/mcp-servers/{name}/logs → {"logs": [...]}
func (h *apiHandlers) handleMCPServerLogs(w http.ResponseWriter, r *http.Request, serverName string) {
	if h.mcpProxy == nil {
		writeJSON(w, map[string]any{"logs": []any{}, "message": "MCP proxy not available (logs only available on the relay node)"})
		return
	}

	entries := h.mcpProxy.GetLogs(serverName)
	if entries == nil {
		entries = []mcpproxy.LogEntry{}
	}
	writeJSON(w, map[string]any{"logs": entries})
}

// handleMCPServerTools returns tools exposed by a specific MCP server.
// Instead of creating a temp proxy (which can't handle OAuth), it queries
// ALL connected relay sessions' tools via the MP relay API and filters by server name.
// This allows viewing tools from servers running on any node (not just the local instance).
// GET /api/mcp-servers/{name}/tools → {"tools": [...]}
func (h *apiHandlers) handleMCPServerTools(w http.ResponseWriter, r *http.Request, serverName string) {
	ctx := r.Context()

	if h.pc.ServerURL == "" {
		writeJSON(w, map[string]any{"tools": []any{}})
		return
	}

	// Get all connected relay sessions
	relayURL := strings.TrimSuffix(h.pc.ServerURL, "/") + "/api/mcp-relay/sessions"
	sessions := h.queryRelaySessions(ctx, relayURL)

	// Query each session's tools, looking for ones matching {instanceId}_{serverName}_
	for _, instID := range sessions {
		if instID == "" {
			continue
		}
		tools := h.queryInstanceTools(ctx, instID)
		if len(tools) == 0 {
			continue
		}

		// Check if any tool in this session matches the server name prefix
		prefix := instID + "_" + serverName + "_"
		var result []any
		for _, t := range tools {
			name, ok := t["name"]
			if !ok {
				continue
			}
			nameStr, ok := name.(string)
			if !ok || !strings.HasPrefix(nameStr, prefix) {
				continue
			}
			// Convert to map[string]any for manipulation
			tool := make(map[string]any)
			for k, v := range t {
				tool[k] = v
			}
			tool["name"] = strings.TrimPrefix(nameStr, prefix)
			tool["id"] = tool["name"] // Swift companion app requires Identifiable.id
			delete(tool, "_server")
			result = append(result, tool)
		}

		if len(result) > 0 {
			writeJSON(w, map[string]any{"tools": result})
			return
		}
	}

	// No tools found on any session
	writeJSON(w, map[string]any{"tools": []any{}})
}

// handleNodes returns connected relay nodes from the Memory Platform.
// GET: lists relay sessions + registered phone nodes
// POST: registers a phone node (iOS push-enabled device)
func (h *apiHandlers) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		h.handlePhoneNodeRegister(w, r)
		return
	}

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

	// Read the raw body first so we can log it for debugging before decoding.
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "nodes: read response: "+err.Error())
		return
	}

	// Log the first 500 characters of the raw relay response body for debugging.
	bodySnippet := string(rawBody)
	if len(bodySnippet) > 500 {
		log.Printf("[LOCAL-API] handleNodes: raw relay response (first 500 chars): %s...", bodySnippet[:500])
	} else {
		log.Printf("[LOCAL-API] handleNodes: raw relay response: %s", bodySnippet)
	}

	var raw any
	if err := json.Unmarshal(rawBody, &raw); err != nil {
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
				log.Printf("[LOCAL-API] handleNodes: unexpected relay response format — body=%s", bodySnippet)
				jsonError(w, http.StatusInternalServerError, fmt.Sprintf("nodes: unexpected relay response format"))
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

	// Enrich each node with metadata from the graph (DianeNodeConfig objects).
	// The MP relay sessions API returns basic live fields (instance_id, version,
	// tool_count, connected_at) but NOT mode, hostname, provider, etc.
	// The graph stores these as DianeNodeConfig objects.
	bridge, bridgeErr := h.bridge(ctx)
	graphConfigs := make(map[string]map[string]any) // instance_id → properties
	if bridgeErr == nil {
		defer bridge.Close()
		if graphResp, err := bridge.Client().Graph.ListObjects(ctx, &graph.ListObjectsOptions{
			Type: "DianeNodeConfig",
		}); err == nil && graphResp != nil {
			for _, obj := range graphResp.Items {
				if obj.Properties != nil {
					if id, ok := obj.Properties["instance_id"].(string); ok && id != "" {
						graphConfigs[id] = obj.Properties
					}
				}
			}
		}
	}

	seen := make(map[string]bool)
	for i, node := range nodes {
		m, ok := node.(map[string]any)
		if !ok {
			continue
		}

		// All nodes from the relay sessions list are online by definition.
		if _, exists := m["online"]; !exists {
			m["online"] = true
		}

		// Track seen instance IDs for orphan detection below.
		id, _ := m["instance_id"].(string)
		if id != "" {
			seen[id] = true
		}

		// Enrich with graph config fields (mode, hostname, provider, etc.)
		// Only set fields the relay session doesn't already have.
		if cfg, ok := graphConfigs[id]; ok {
			for _, key := range []string{"mode", "hostname", "provider",
				"uptime", "relay_active", "bot_active", "healthy"} {
				if _, exists := m[key]; !exists {
					if val, ok := cfg[key]; ok {
						m[key] = val
					}
				}
			}
			// Use graph config version if relay version is empty or "1.0".
			if v, ok := m["version"]; (!ok || v == "" || v == "1.0") && cfg["version"] != nil {
				m["version"] = cfg["version"]
			}
		}

		nodes[i] = m
	}

	// Append registered nodes that aren't currently connected via relay.
	// These show as offline in the companion UI but preserve their config.
	for id, cfg := range graphConfigs {
		if !seen[id] {
			node := map[string]any{
				"instance_id": id,
				"online":      false,
			}
			for _, key := range []string{"mode", "hostname", "version",
				"provider", "uptime", "relay_active", "bot_active", "healthy"} {
				if val, ok := cfg[key]; ok {
					node[key] = val
				}
			}
			nodes = append(nodes, node)
		}
	}

	if len(nodes) == 0 {
		log.Printf("[LOCAL-API] WARNING: /api/nodes returned 0 nodes — expected at least the local instance")
	}

	writeJSON(w, map[string]any{"nodes": nodes})
}

// handleNodeSubRoutes dispatches sub-resource requests under /api/nodes/.
// Paths handled:
//
//	/api/nodes/{instanceID}/tools    — GET: list tools for a relay node
//	/api/nodes/{instanceID}/heartbeat — PUT: update node heartbeat
//	/api/nodes/{instanceID}           — PUT: update node (push token)
//	                                 — DELETE: de-register node
func (h *apiHandlers) handleNodeSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		jsonError(w, http.StatusBadRequest, "invalid node instance ID")
		return
	}
	instanceID := parts[0]

	if len(parts) == 2 && parts[1] == "tools" {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleNodeTools(w, r, instanceID)
		return
	}

	if len(parts) == 2 && parts[1] == "heartbeat" {
		h.handleNodeHeartbeat(w, r, instanceID)
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodPut:
			h.handleNodeUpdate(w, r, instanceID)
			return
		case http.MethodDelete:
			h.handleNodeDeregister(w, r, instanceID)
			return
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
	}

	jsonError(w, http.StatusNotFound, "not found")
}

// handleNodeTools returns the MCP tools for a specific relay node.
// GET /api/nodes/{instanceID}/tools → {"tools": [...]}
func (h *apiHandlers) handleNodeTools(w http.ResponseWriter, r *http.Request, instanceID string) {
	ctx := r.Context()

	// Query the per-instance tools endpoint on the MP relay.
	// The sessions list endpoint (/api/mcp-relay/sessions) does NOT embed tools;
	// tools are available at /api/mcp-relay/sessions/{instanceID}/tools.
	toolsURL := strings.TrimSuffix(h.pc.ServerURL, "/") + "/api/mcp-relay/sessions/" + url.PathEscape(instanceID) + "/tools"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, toolsURL, nil)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "node tools: build request: "+err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.pc.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "node tools: query relay: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// If the node isn't found (404) or there's another error, return an empty tool list.
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, map[string]any{"tools": []any{}})
		return
	}

	var result struct {
		Tools []any `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[LOCAL-API] handleNodeTools: decode response: %v", err)
		writeJSON(w, map[string]any{"tools": []any{}})
		return
	}

	if result.Tools == nil {
		result.Tools = []any{}
	}
	writeJSON(w, map[string]any{"tools": result.Tools})
}

// handleProviders returns configured LLM providers from the Memory Platform.
func (h *apiHandlers) handleProviders(w http.ResponseWriter, r *http.Request) {
	providers := make([]map[string]any, 0, 2)

	// Fetch from Memory Platform
	ctx := context.Background()
	bridge, err := h.bridge(ctx)
	if err != nil {
		// No bridge — return empty list
		writeJSON(w, map[string]any{"providers": providers})
		return
	}
	defer bridge.Close()

	orgID := h.pc.OrgID
	if orgID == "" {
		proj, err := bridge.Client().Projects.Get(ctx, h.pc.ProjectID, nil)
		if err != nil {
			writeJSON(w, map[string]any{"providers": providers})
			return
		}
		orgID = proj.OrgID
	}

	mpProviders, err := bridge.ListOrgProviders(ctx, orgID)
	if err != nil {
		writeJSON(w, map[string]any{"providers": providers})
		return
	}

	for _, p := range mpProviders {
		model := p.GenerativeModel
		providers = append(providers, map[string]any{
			"provider":        p.Provider,
			"baseUrl":         p.BaseURL,
			"generativeModel": model,
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

	// 3. Create streaming run via raw HTTP (ACP SDK's SSE parser doesn't handle event: headers)
	reqBody, err := json.Marshal(map[string]any{
		"message":    message,
		"session_id": sessionID,
		"mode":       "stream",
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "json: "+err.Error())
		return
	}

	acpURL := fmt.Sprintf("%s/acp/v1/agents/%s/runs", h.pc.ServerURL, url.PathEscape(req.AgentName))
	httpReq, err := http.NewRequestWithContext(ctx, "POST", acpURL, bytes.NewReader(reqBody))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "request: "+err.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+h.pc.Token)

	streamClient := &http.Client{
		Timeout: 0, // no timeout for SSE streaming
	}
	acpResp, err := streamClient.Do(httpReq)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "acp connect: "+err.Error())
		return
	}
	defer acpResp.Body.Close()

	if acpResp.StatusCode >= 400 {
		body, _ := io.ReadAll(acpResp.Body)
		jsonError(w, http.StatusInternalServerError, "acp: "+string(body))
		return
	}

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

	// 5. Read ACP SSE events (with proper event: header parsing) and forward to client
	var runID string
	var sawRunCreated, sawRunComplete bool
	var tokenCount, toolCallCount int
	writeEvent := func(evt map[string]any) {
		data, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	scanner := bufio.NewScanner(acpResp.Body)
	var currentEventType string
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")
			if dataStr == "[DONE]" {
				break
			}

			var rawData map[string]any
			if err := json.Unmarshal([]byte(dataStr), &rawData); err != nil {
				log.Printf("[CHAT-STREAM] WARNING: failed to parse SSE data: %v", err)
				continue
			}

			eventType := currentEventType
			currentEventType = "" // reset after consuming

			if eventType == "" {
				log.Printf("[CHAT-STREAM] WARNING: SSE event with no event: header — data: %+v", rawData)
				continue
			}

			switch eventType {
			case "run.created":
				sawRunCreated = true
				if run, ok := rawData["run"].(map[string]any); ok {
					if id, ok := run["run_id"].(string); ok && id != "" {
						runID = id
					}
				}
				if runID == "" {
					log.Printf("[CHAT-STREAM] WARNING: run.created had no run_id")
				}

			case "run.in-progress":
				if runID == "" {
					if run, ok := rawData["run"].(map[string]any); ok {
						if id, ok := run["run_id"].(string); ok && id != "" {
							runID = id
						}
					}
				}

			case "message.part":
				part, ok := rawData["part"].(map[string]any)
				if !ok {
					log.Printf("[CHAT-STREAM] WARNING: message.part had no 'part' — %+v", rawData)
					continue
				}
				contentType, _ := part["content_type"].(string)
				if contentType == "" {
					log.Printf("[CHAT-STREAM] WARNING: message.part empty content_type — %+v", part)
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
							evtType := "tool_call"
							if _, hasOutput := meta["tool_output"]; hasOutput {
								evtType = "tool_result"
							}
							toolCallCount++
							writeEvent(map[string]any{
								"type": evtType,
								"name": toolName,
							})
						}
					}
				default:
					log.Printf("[CHAT-STREAM] WARNING: unexpected message.part content_type %q", contentType)
				}

			case "message.created":
				if msg, ok := rawData["message"].(map[string]any); ok {
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
					log.Printf("[CHAT-STREAM] WARNING: message.created had no 'message' in data — %+v", rawData)
				}

			case "run.completed":
				sawRunComplete = true
				if runID == "" {
					log.Printf("[CHAT-STREAM] WARNING: run.completed with no runID")
				}
				writeEvent(map[string]any{
					"type":       "done",
					"session_id": sessionID,
					"run_id":     runID,
				})

			case "run.failed", "run.cancelled":
				errMsg := "run " + strings.TrimPrefix(eventType, "run.")
				if run, ok := rawData["run"].(map[string]any); ok {
					if e, ok := run["error"].(map[string]any); ok {
						if m, ok := e["message"].(string); ok {
							errMsg = m
						}
					}
				}
				log.Printf("[CHAT-STREAM] Run %s: %s (session=%s)", safePrefix(runID, 12), eventType, safePrefix(sessionID, 12))
				writeEvent(map[string]any{
					"type":    "error",
					"message": errMsg,
				})

			case "run.awaiting":
				log.Printf("[CHAT-STREAM] Agent paused, awaiting input (session=%s run=%s)", safePrefix(sessionID, 12), safePrefix(runID, 12))

			case "error":
				errMsg := "stream error"
				if e, ok := rawData["error"].(map[string]any); ok {
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
				log.Printf("[CHAT-STREAM] WARNING: unexpected ACP event type %q — data: %+v", eventType, rawData)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[CHAT-STREAM] SSE scanner error: %v", err)
	}

	// ── Agent Push Notification ──────────────────────────────
	// After a successful run, fire a goroutine to push notifications
	// to registered phone nodes. This runs in the background so the
	// SSE stream closes immediately.
	if sawRunComplete && sessionID != "" {
		go h.sendAgentNotification(sessionID, req.AgentName, tokenCount)
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

// queryRelaySessions fetches the list of active relay instance IDs from the MP relay.
func (h *apiHandlers) queryRelaySessions(ctx context.Context, relayURL string) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, relayURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+h.pc.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	// Parse sessions from various response formats
	var sessions []string
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if id, ok := m["instance_id"].(string); ok && id != "" {
					sessions = append(sessions, id)
				}
			}
		}
	case map[string]any:
		for _, key := range []string{"sessions", "items", "data", "nodes"} {
			if arr, ok := v[key].([]any); ok {
				for _, item := range arr {
					if m, ok := item.(map[string]any); ok {
						if id, ok := m["instance_id"].(string); ok && id != "" {
							sessions = append(sessions, id)
						}
					}
				}
				if len(sessions) > 0 {
					break
				}
			}
		}
	}
	return sessions
}

// queryInstanceTools fetches the tool list for a specific relay instance.
func (h *apiHandlers) queryInstanceTools(ctx context.Context, instanceID string) []map[string]any {
	toolsURL := strings.TrimSuffix(h.pc.ServerURL, "/") + "/api/mcp-relay/sessions/" + url.PathEscape(instanceID) + "/tools"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, toolsURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+h.pc.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var toolsResp struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&toolsResp); err != nil {
		return nil
	}
	return toolsResp.Tools
}

// writeJSON marshals v as JSON and writes it to w with Content-Type header.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[LOCAL-API] JSON encode error: %v", err)
	}
}

// safePrefix returns the first n characters of s, or the whole string if shorter.
func safePrefix(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
