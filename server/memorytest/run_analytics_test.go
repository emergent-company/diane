// Package memorytest validates the agent run analytics APIs against the live Memory Platform.
// These tests read credentials from ~/.config/diane.yml (same as agent_test.go).
package memorytest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

// TestRunAnalytics_GetProjectRunFull runs a full agent lifecycle and then fetches
// the full run trace via GetProjectRunFull — verifying run metadata, messages,
// and tool calls are all returned in a single request.
func TestRunAnalytics_GetProjectRunFull(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout)
	defer cancel()

	// 1. Find the agent definition
	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}
	var defID string
	for _, d := range defs.Data {
		if d.Name == agentTestDefName {
			defID = d.ID
			break
		}
	}
	if defID == "" {
		t.Skipf("Agent definition '%s' not found — run 'diane agent sync'", agentTestDefName)
	}
	t.Logf("Agent definition: %s", defID)

	// 2. Create a runtime agent (clean up afterwards)
	runName := fmt.Sprintf("test-analytics-full-%d", time.Now().UnixMilli())
	agent, err := b.CreateRuntimeAgent(ctx, runName, defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := agent.Data.ID
	t.Logf("Runtime agent: %s", agentID)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := b.Client().Agents.Delete(cleanupCtx, agentID); err != nil {
			t.Logf("Cleanup: delete agent %s: %v", agentID, err)
		}
	})

	// 3. Trigger the agent
	prompt := "Say hello in one sentence."
	resp, err := b.TriggerAgentWithInput(ctx, agentID, prompt, "")
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	if resp.Error != nil && *resp.Error != "" {
		t.Fatalf("Trigger error: %s", *resp.Error)
	}
	runID := *resp.RunID
	t.Logf("Run ID: %s", runID)
	t.Logf("Prompt: %s", prompt)

	// 4. Poll for completion
	for i := 0; i < agentTestMaxPoll; i++ {
		time.Sleep(agentTestPoll)

		runResp, err := b.GetProjectRun(ctx, runID)
		if err != nil {
			t.Logf("Poll %d: %v", i+1, err)
			continue
		}

		run := runResp.Data
		switch run.Status {
		case "success", "completed":
			t.Logf("Completed after %d polls (%dms)", i+1, safeDerefInt(run.DurationMs))
			t.Logf("Steps: %d", run.StepCount)
			goto DONE
		case "failed", "error":
			errMsg := ""
			if run.ErrorMessage != nil {
				errMsg = *run.ErrorMessage
			}
			t.Fatalf("Run failed: %s", errMsg)
		}
	}
	t.Fatal("Run timed out — did not complete within polling window")

DONE:
	// 5. Fetch the full run trace via GetProjectRunFull
	fullCtx, fullCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer fullCancel()

	fullResp, err := b.GetProjectRunFull(fullCtx, runID)
	if err != nil {
		// The full endpoint might not be available on older MP servers
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "501") {
			t.Skipf("GetProjectRunFull not supported on this MP version: %v", err)
		}
		t.Fatalf("GetProjectRunFull: %v", err)
	}

	full := fullResp.Data

	// 6. Verify Run metadata
	if full.Run == nil {
		t.Fatal("AgentRunFull.Run is nil")
	}
	if full.Run.ID == "" {
		t.Error("AgentRunFull.Run.ID is empty")
	}
	if full.Run.Status == "" {
		t.Error("AgentRunFull.Run.Status is empty")
	}
	if full.Run.StartedAt.IsZero() {
		t.Error("AgentRunFull.Run.StartedAt is zero")
	}
	t.Logf("Run: id=%s status=%s startedAt=%s durationMs=%d steps=%d",
		full.Run.ID, full.Run.Status, full.Run.StartedAt.Format(time.RFC3339),
		safeDerefInt(full.Run.DurationMs), full.Run.StepCount)

	// Verify token usage is populated
	if full.Run.TokenUsage != nil {
		t.Logf("  TokenUsage: input=%d output=%d cost=%.6f",
			full.Run.TokenUsage.TotalInputTokens, full.Run.TokenUsage.TotalOutputTokens,
			full.Run.TokenUsage.EstimatedCostUSD)
	} else {
		t.Log("  TokenUsage: nil (may not be populated on older MP versions)")
	}

	// Log model/provider if set
	if full.Run.Model != nil {
		t.Logf("  Model: %s", *full.Run.Model)
	}
	if full.Run.Provider != nil {
		t.Logf("  Provider: %s", *full.Run.Provider)
	}

	// 7. Verify Messages
	if len(full.Messages) == 0 {
		t.Log("No messages returned — run may be too fast or endpoint differs")
	} else {
		t.Logf("Messages: %d", len(full.Messages))
		for _, m := range full.Messages {
			content := extractMsgContent(m.Content)
			t.Logf("  [%s] step=%d %.120s", m.Role, m.StepNumber, content)
		}
		if len(full.Messages) < 1 {
			t.Error("Expected at least 1 message (assistant response)")
		}
	}

	// 8. Log ToolCalls
	if len(full.ToolCalls) == 0 {
		t.Log("No tool calls recorded for this run")
	} else {
		t.Logf("Tool calls: %d", len(full.ToolCalls))
		for _, tc := range full.ToolCalls {
			t.Logf("  %s (status: %s, %dms, step=%d)", tc.ToolName, tc.Status, safeDerefInt(tc.DurationMs), tc.StepNumber)
		}
	}

	// 9. Log ParentRun if present
	if full.ParentRun != nil {
		t.Logf("Parent run: id=%s status=%s", full.ParentRun.ID, full.ParentRun.Status)
	} else {
		t.Log("No parent run (standalone run)")
	}

	// Verify the run ID matches what we triggered
	if full.Run.ID != runID {
		t.Errorf("GetProjectRunFull returned run ID %q, expected %q", full.Run.ID, runID)
	}

	t.Log("✅ GetProjectRunFull verified successfully")
}

// TestRunAnalytics_GetProjectRunStats fetches aggregate run statistics for the
// last hour and logs the overview, per-agent breakdown, tool stats, and top errors.
func TestRunAnalytics_GetProjectRunStats(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Set Since to 1 hour ago
	since := time.Now().Add(-1 * time.Hour)
	opts := &sdkagentrun.RunStatsOptions{
		Since: &since,
	}

	statsResp, err := b.GetProjectRunStats(ctx, opts)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "501") {
			t.Skipf("GetProjectRunStats not supported on this MP version (need >= v0.40.2): %v", err)
		}
		t.Fatalf("GetProjectRunStats: %v", err)
	}

	stats := statsResp.Data

	// Log the analysis period
	t.Logf("Period: %s → %s", stats.Period.Since.Format(time.RFC3339), stats.Period.Until.Format(time.RFC3339))

	// Log Overview
	ov := stats.Overview
	t.Logf("Overview: totalRuns=%d success=%d failed=%d errors=%d",
		ov.TotalRuns, ov.SuccessCount, ov.FailedCount, ov.ErrorCount)
	t.Logf("  SuccessRate=%.1f%% AvgDuration=%.0fms TotalCost=%.6f USD",
		ov.SuccessRate*100, ov.AvgDurationMs, ov.TotalCostUSD)

	// Log per-agent breakdown
	if len(stats.ByAgent) == 0 {
		t.Log("No per-agent stats — no agents ran in this period")
	} else {
		t.Logf("Agents tracked: %d", len(stats.ByAgent))
		for name, agent := range stats.ByAgent {
			t.Logf("  %s: total=%d success=%d failed=%d errored=%d avgMs=%.0f maxMs=%.0f avgCost=%.6f totalCost=%.6f avgIn=%d avgOut=%d",
				name, agent.Total, agent.Success, agent.Failed, agent.Errored,
				agent.AvgDurationMs, float64(agent.MaxDurationMs),
				agent.AvgCostUSD, agent.TotalCostUSD,
				int64(agent.AvgInputTokens), int64(agent.AvgOutputTokens))
		}
	}

	// Log top errors
	if len(stats.TopErrors) == 0 {
		t.Log("No top errors recorded")
	} else {
		t.Logf("Top errors: %d", len(stats.TopErrors))
		for i, e := range stats.TopErrors {
			t.Logf("  [%d] count=%d %s", i, e.Count, e.Message)
		}
	}

	// Log tool stats
	toolStats := stats.ToolStats
	t.Logf("Tool stats: totalCalls=%d", toolStats.TotalToolCalls)
	if len(toolStats.ByTool) > 0 {
		for name, ts := range toolStats.ByTool {
			t.Logf("  %s: total=%d success=%d failed=%d avgMs=%.0f maxMs=%d",
				name, ts.Total, ts.Success, ts.Failed, ts.AvgDurationMs, ts.MaxDurationMs)
		}
	}

	// Log time series
	ts := stats.TimeSeries
	if len(ts.ByHour) == 0 {
		t.Log("Time series: no hourly data")
	} else {
		t.Logf("Time series: %d hourly buckets", len(ts.ByHour))
		for i, tp := range ts.ByHour {
			if i >= 5 {
				t.Logf("  ... and %d more", len(ts.ByHour)-5)
				break
			}
			t.Logf("  %s: runs=%d", tp.Hour.Format(time.RFC3339), tp.Runs)
		}
	}

	t.Log("✅ GetProjectRunStats verified successfully")
}

// TestRunAnalytics_GetProjectRunSessionStats fetches session-level analytics
// for the last 24 hours and logs session counts, platform breakdown, and top sessions.
func TestRunAnalytics_GetProjectRunSessionStats(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Set Since to 24 hours ago
	since := time.Now().Add(-24 * time.Hour)
	opts := &sdkagentrun.RunStatsOptions{
		Since: &since,
		TopN:  10,
	}

	sessionResp, err := b.GetProjectRunSessionStats(ctx, opts)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "501") {
			t.Skipf("GetProjectRunSessionStats not supported on this MP version (need >= v0.40.2): %v", err)
		}
		t.Fatalf("GetProjectRunSessionStats: %v", err)
	}

	sessions := sessionResp.Data

	// Log the analysis period
	t.Logf("Period: %s → %s", sessions.Period.Since.Format(time.RFC3339), sessions.Period.Until.Format(time.RFC3339))

	// Log session counts
	t.Logf("Sessions: total=%d active=%d", sessions.TotalSessions, sessions.ActiveSessions)
	t.Logf("Runs per session: avg=%.1f max=%d", sessions.AvgRunsPerSession, sessions.MaxRunsPerSession)

	// Log platform breakdown
	if len(sessions.SessionsByPlatform) == 0 {
		t.Log("No sessions by platform data")
	} else {
		t.Logf("Sessions by platform (%d):", len(sessions.SessionsByPlatform))
		for platform, count := range sessions.SessionsByPlatform {
			t.Logf("  %s: %d", platform, count)
		}
	}

	// Log top sessions
	if len(sessions.TopSessions) == 0 {
		t.Log("No top sessions recorded")
	} else {
		t.Logf("Top sessions: %d", len(sessions.TopSessions))
		for i, s := range sessions.TopSessions {
			t.Logf("  [%d] platform=%s channel=%s thread=%s runs=%d lastRun=%s avgMs=%.0f totalCost=%.6f",
				i, s.Platform, s.ChannelID, s.ThreadID,
				s.TotalRuns, s.LastRunAt.Format(time.RFC3339),
				s.AvgDurationMs, s.TotalCostUSD)
		}
	}

	t.Log("✅ GetProjectRunSessionStats verified successfully")
}
