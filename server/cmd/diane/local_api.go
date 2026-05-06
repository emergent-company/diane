package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
	"github.com/Emergent-Comapny/diane/internal/memory"
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
type localAPIServer struct {
	server *http.Server
}

// startLocalAPI creates and starts the local companion API server on the given port.
// Returns immediately — the server runs in its own goroutine.
func startLocalAPI(pc *config.ProjectConfig, port int) (*localAPIServer, error) {
	mux := http.NewServeMux()

	api := &apiHandlers{pc: pc}
	mux.HandleFunc("/api/status", api.handleStatus)
	mux.HandleFunc("/api/stats", api.handleStats)
	mux.HandleFunc("/api/stats/providers", api.handleProviderStats)
	mux.HandleFunc("/api/stats/objects", api.handleGraphObjectStats)
	mux.HandleFunc("/api/sessions", api.handleSessions)
	mux.HandleFunc("/api/sessions/", api.handleSessionMessages)
	mux.HandleFunc("/api/mcp-servers", api.handleMCPServers)
	mux.HandleFunc("/api/nodes", api.handleNodes)

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

// handleStatus returns a simple health check.
func (h *apiHandlers) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"ok": true})
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

// handleSessionMessages returns messages for a session.
func (h *apiHandlers) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from path: /api/sessions/{id}/messages
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	sessionID := strings.TrimSuffix(path, "/messages")
	if sessionID == "" || strings.Contains(sessionID, "/") {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
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

// handleMCPServers returns the list of configured MCP servers.
func (h *apiHandlers) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	cfg, err := mcpproxy.LoadConfig(mcpproxy.GetDefaultConfigPath())
	if err != nil {
		// No config file is normal — return empty list
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
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.pc.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, map[string]any{"nodes": []any{}, "error": err.Error()})
		return
	}
	defer resp.Body.Close()

	var nodes any
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		writeJSON(w, map[string]any{"nodes": []any{}})
		return
	}

	writeJSON(w, map[string]any{"nodes": nodes})
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

// GET /api/stats/providers — provider/model usage summary.
func (h *apiHandlers) handleProviderStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, map[string]any{
		"providers":           []any{},
		"total_runs":          0,
		"total_success":       0,
		"total_errors":        0,
		"total_input_tokens":  0,
		"total_output_tokens": 0,
		"total_cost_usd":      0.0,
		"hours":               0,
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

// writeJSON marshals v as JSON and writes it to w with Content-Type header.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[LOCAL-API] JSON encode error: %v", err)
	}
}
