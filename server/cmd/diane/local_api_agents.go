// Package: main
// Agent CRUD and override HTTP handlers.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"github.com/Emergent-Comapny/diane/internal/agents"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
)

func (a *localAPIServer) handleListAgents(w http.ResponseWriter, r *http.Request) {
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

// handleCreateAgent creates a new user-defined agent definition.
func (a *localAPIServer) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	var req sdkagents.CreateAgentDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
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

	resp, err := a.bridge.CreateAgentDef(ctx, &req)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create agent: %v", err))
		return
	}

	jsonResponse(w, resp.Data)
}

// handleAgentRouter handles all /api/agents/{name} and /api/agents/{name}/... routes.
func (a *localAPIServer) handleAgentDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	path = strings.TrimSuffix(path, "/")

	// Parse the path: {name} or {name}/override or {name}/clone
	parts := strings.SplitN(path, "/", 2)
	agentName := parts[0]
	subPath := ""
	if len(parts) > 1 {
		subPath = parts[1]
	}

	if agentName == "" {
		// /api/agents/ with no name — handled by handleAgents above
		jsonError(w, http.StatusBadRequest, "agent name required")
		return
	}

	ctx := context.Background()

	switch subPath {
	case "override":
		a.handleAgentOverride(ctx, w, r, agentName)
	case "clone":
		a.handleCloneAgent(ctx, w, r, agentName)
	case "":
		a.handleSingleAgent(ctx, w, r, agentName)
	default:
		jsonError(w, http.StatusNotFound, "unknown path")
	}
}

// handleSingleAgent handles GET/PATCH/DELETE for /api/agents/{name}
func (a *localAPIServer) handleSingleAgent(ctx context.Context, w http.ResponseWriter, r *http.Request, name string) {
	switch r.Method {
	case http.MethodGet:
		a.getSingleAgentDetail(ctx, w, r, name)
	case http.MethodPatch:
		a.updateSingleAgent(ctx, w, r, name)
	case http.MethodDelete:
		a.deleteSingleAgent(ctx, w, r, name)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// getSingleAgentDetail returns full detail for a single agent.
func (a *localAPIServer) getSingleAgentDetail(ctx context.Context, w http.ResponseWriter, r *http.Request, name string) {
	// List all agent defs to find ID by name
	defs, err := a.bridge.ListAgentDefs(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list agents: %v", err))
		return
	}

	var defID string
	for _, d := range defs.Data {
		if d.Name == name {
			defID = d.ID
			break
		}
	}
	if defID == "" {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", name))
		return
	}

	// Fetch full detail
	detail, err := a.bridge.GetAgentDef(ctx, defID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("get agent %s: %v", name, err))
		return
	}

	d := detail.Data
	resp := map[string]any{
		"id":             d.ID,
		"name":           d.Name,
		"description":    d.Description,
		"flow_type":      d.FlowType,
		"visibility":     d.Visibility,
		"is_default":     d.IsDefault,
		"tool_count":     len(d.Tools),
		"tools":          d.Tools,
		"skills":         d.Skills,
		"system_prompt":  d.SystemPrompt,
		"created_at":     d.CreatedAt.Format(time.RFC3339),
		"updated_at":     d.UpdatedAt.Format(time.RFC3339),
	}

	if d.MaxSteps != nil {
		resp["max_steps"] = *d.MaxSteps
	}
	if d.DefaultTimeout != nil {
		resp["default_timeout"] = *d.DefaultTimeout
	}
	if d.Model != nil {
		model := map[string]any{
			"name": d.Model.Name,
		}
		if d.Model.Temperature != nil {
			model["temperature"] = *d.Model.Temperature
		}
		if d.Model.MaxTokens != nil {
			model["max_tokens"] = *d.Model.MaxTokens
		}
		resp["model"] = model
	}
	if d.ACPConfig != nil {
		resp["acp"] = d.ACPConfig
	}
	if d.Config != nil {
		resp["config"] = d.Config
	}
	if d.DispatchMode != "" {
		resp["dispatch_mode"] = d.DispatchMode
	}

	jsonResponse(w, resp)
}

// updateSingleAgent updates a user-defined agent definition.
func (a *localAPIServer) updateSingleAgent(ctx context.Context, w http.ResponseWriter, r *http.Request, name string) {
	// Find ID by name
	defs, err := a.bridge.ListAgentDefs(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list agents: %v", err))
		return
	}

	var defID string
	for _, d := range defs.Data {
		if d.Name == name {
			defID = d.ID
			break
		}
	}
	if defID == "" {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", name))
		return
	}

	var req sdkagents.UpdateAgentDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	_, err = a.bridge.UpdateAgentDef(ctx, defID, &req)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("update agent %s: %v", name, err))
		return
	}

	jsonResponse(w, map[string]string{"status": "updated"})
}

// deleteSingleAgent deletes or disables an agent.
// Built-in agents get disabled via override; user-defined agents are deleted from MP.
func (a *localAPIServer) deleteSingleAgent(ctx context.Context, w http.ResponseWriter, r *http.Request, name string) {
	// Check if it's a built-in agent by looking at the Go registry
	isBuiltIn := false
	for _, ba := range agents.BuiltInAgents() {
		if ba.Name == name {
			isBuiltIn = true
			break
		}
	}

	if isBuiltIn {
		// Built-in: set disabled=true override
		override := &agents.AgentOverrideConfig{
			AgentName: name,
			Disabled:  true,
		}
		if err := a.bridge.UpsertAgentOverride(ctx, override); err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("disable agent %s: %v", name, err))
			return
		}
		jsonResponse(w, map[string]string{
			"status": "disabled",
			"message": fmt.Sprintf("Built-in agent %q disabled. Re-enable by removing the override config.", name),
		})
		return
	}

	// User-defined: delete from MP
	defs, err := a.bridge.ListAgentDefs(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list agents: %v", err))
		return
	}

	var defID string
	for _, d := range defs.Data {
		if d.Name == name {
			defID = d.ID
			break
		}
	}
	if defID == "" {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", name))
		return
	}

	if err := a.bridge.DeleteAgentDef(ctx, defID); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("delete agent %s: %v", name, err))
		return
	}

	jsonResponse(w, map[string]string{"status": "deleted"})
}

// handleAgentOverride handles GET/PUT/DELETE for /api/agents/{name}/override
func (a *localAPIServer) handleAgentOverride(ctx context.Context, w http.ResponseWriter, r *http.Request, name string) {
	switch r.Method {
	case http.MethodGet:
		a.getAgentOverride(ctx, w, r, name)
	case http.MethodPut:
		a.putAgentOverride(ctx, w, r, name)
	case http.MethodDelete:
		a.deleteAgentOverride(ctx, w, r, name)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *localAPIServer) getAgentOverride(ctx context.Context, w http.ResponseWriter, r *http.Request, name string) {
	oc, err := a.bridge.GetAgentOverride(ctx, name)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("get override: %v", err))
		return
	}
	if oc == nil {
		jsonResponse(w, map[string]any{"agent_name": name, "overrides": nil})
		return
	}
	jsonResponse(w, oc)
}

func (a *localAPIServer) putAgentOverride(ctx context.Context, w http.ResponseWriter, r *http.Request, name string) {
	var oc agents.AgentOverrideConfig
	if err := json.NewDecoder(r.Body).Decode(&oc); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	oc.AgentName = name

	if err := a.bridge.UpsertAgentOverride(ctx, &oc); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("save override: %v", err))
		return
	}
	jsonResponse(w, map[string]string{"status": "saved"})
}

func (a *localAPIServer) deleteAgentOverride(ctx context.Context, w http.ResponseWriter, r *http.Request, name string) {
	if err := a.bridge.DeleteAgentOverride(ctx, name); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("delete override: %v", err))
		return
	}
	jsonResponse(w, map[string]string{"status": "deleted"})
}

// handleCloneAgent handles POST /api/agents/{name}/clone
func (a *localAPIServer) handleCloneAgent(ctx context.Context, w http.ResponseWriter, r *http.Request, sourceName string) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Find source agent by name
	defs, err := a.bridge.ListAgentDefs(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list agents: %v", err))
		return
	}

	var sourceDefID string
	for _, d := range defs.Data {
		if d.Name == sourceName {
			sourceDefID = d.ID
			break
		}
	}
	if sourceDefID == "" {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("source agent %q not found", sourceName))
		return
	}

	// Get full detail
	detail, err := a.bridge.GetAgentDef(ctx, sourceDefID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("get source agent: %v", err))
		return
	}

	src := detail.Data

	// Parse request for new name
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.Name == "" {
		req.Name = sourceName + "-copy"
	}

	// Create new agent with source's config
	createReq := &sdkagents.CreateAgentDefinitionRequest{
		Name:         req.Name,
		Description:  src.Description,
		SystemPrompt: src.SystemPrompt,
		Visibility:   src.Visibility,
		FlowType:     src.FlowType,
		Tools:        src.Tools,
		Skills:       src.Skills,
		MaxSteps:     src.MaxSteps,
		DefaultTimeout: src.DefaultTimeout,
		Model:         src.Model,
		DispatchMode:  src.DispatchMode,
	}

	if src.Config != nil {
		createReq.Config = src.Config
	}

	resp, err := a.bridge.CreateAgentDef(ctx, createReq)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create clone: %v", err))
		return
	}

	jsonResponse(w, map[string]any{
		"status":     "created",
		"id":         resp.Data.ID,
		"name":       resp.Data.Name,
	})
}

// handleAgentSeed handles POST /api/agents/seed
func (a *localAPIServer) handleAgentSeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := context.Background()
	builtIns, err := agents.BuildMergedAgents(ctx, a.bridge.Client().Graph)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("build merged agents: %v", err))
		return
	}

	if err := agents.SeedAgentList(ctx, a.bridge.Client(), builtIns); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("seed agents: %v", err))
		return
	}

	jsonResponse(w, map[string]any{
		"status":  "seeded",
		"count":   len(builtIns),
	})
}

// ─── MCP CRUD Handlers ────────────────────────────────────────

// POST /api/mcp-servers/toggle/{name} — toggle enabled/disabled
