// Package: main
// Status, stats, and provider HTTP handlers.
package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/Emergent-Comapny/diane/internal/schema"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

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

// GET /api/stats/objects — graph object counts from the Memory Platform,
// covering all known schema types with their display names from the embedded schema.
func (a *localAPIServer) handleGraphObjectStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := context.Background()

	// Load schema types to get all type names + labels
	nodeTypes, _, err := schema.LoadDefinitions()
	typeNames := make([]string, len(nodeTypes))
	typeLabel := make(map[string]string, len(nodeTypes))
	if err == nil {
		for i, nt := range nodeTypes {
			typeNames[i] = nt.TypeName
			typeLabel[nt.TypeName] = nt.Label
		}
	}

	var byType []memory.TypeCount
	var total int

	if len(typeNames) > 0 {
		// Use schema types for accurate per-type counts
		counts, cErr := a.bridge.GetObjectCountsForSchema(ctx, typeNames)
		if cErr != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("query counts: %v", cErr))
			return
		}
		byType = make([]memory.TypeCount, 0, len(counts))
		typeTotal := 0
		for _, tn := range typeNames {
			c := counts[tn]
			if c > 0 {
				label := typeLabel[tn]
				if label == "" {
					label = tn
				}
				byType = append(byType, memory.TypeCount{TypeName: label, Count: c})
				typeTotal += c
			}
		}
		total = typeTotal

		// Sort by count descending
		sort.Slice(byType, func(i, j int) bool {
			return byType[i].Count > byType[j].Count
		})
	} else {
		// Fallback: use old method
		stats, sErr := a.bridge.GetGraphObjectStats(ctx)
		if sErr != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("query graph objects: %v", sErr))
			return
		}
		total = stats.Total
		byType = stats.ByType
	}

	jsonResponse(w, map[string]any{
		"total":   total,
		"by_type": byType,
	})
}

// ─── Agent CRUD Handlers ─────────────────────────────────────────

// GET /api/agents — list agent definitions
// POST /api/agents — create a new user-defined agent
func (a *localAPIServer) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.handleListAgents(w, r)
	case http.MethodPost:
		a.handleCreateAgent(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleListAgents lists all agent definitions via the bridge.
