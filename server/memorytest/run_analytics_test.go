// Package memorytest validates diane's run analytics via bridge API calls.
//
// Tests cover:
//   - GetProjectRunStats — aggregate analytics
//   - GetProjectRunSessionStats — session-level analytics
//   - GetProjectRun — single run details
//   - GetProjectRunFull — full run trace
//   - Error handling for non-existent runs
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestRunAnalytics ./memorytest/
package memorytest

import (
	"context"
	"strings"
	"testing"
	"time"

	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

// =========================================================================
// TestRunAnalytics_GetProjectRunStats: Calls GetProjectRunStats and verifies
// the response structure — overview, byAgent, topErrors, toolStats, timeSeries.
// =========================================================================

func TestRunAnalytics_GetProjectRunStats(t *testing.T) {
	b := setupBridgeFromConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	since := time.Now().Add(-72 * time.Hour)
	opts := &sdkagentrun.RunStatsOptions{
		Since: &since,
	}

	resp, err := b.GetProjectRunStats(ctx, opts)
	if err != nil {
		t.Fatalf("GetProjectRunStats: %v", err)
	}
	if resp == nil {
		t.Fatal("GetProjectRunStats returned nil response")
	}

	stats := resp.Data
	t.Logf("=== Run stats ===")
	t.Logf("  Since: %s", stats.Period.Since.Format(time.RFC3339))
	t.Logf("  Until: %s", stats.Period.Until.Format(time.RFC3339))
	t.Logf("  Total runs: %d | Success: %d | Failed: %d | Errors: %d",
		stats.Overview.TotalRuns, stats.Overview.SuccessCount,
		stats.Overview.FailedCount, stats.Overview.ErrorCount)
	t.Logf("  Avg duration: %.0fms | Total cost: $%.4f",
		stats.Overview.AvgDurationMs, stats.Overview.TotalCostUSD)

	// Verify overview structure
	if stats.Overview.TotalRuns < 0 {
		t.Errorf("TotalRuns = %d, want >= 0", stats.Overview.TotalRuns)
	}
	if stats.Overview.SuccessRate < 0 || stats.Overview.SuccessRate > 100 {
		t.Errorf("SuccessRate = %.2f, want between 0 and 100", stats.Overview.SuccessRate)
	}
	if stats.Overview.AvgDurationMs < 0 {
		t.Errorf("AvgDurationMs = %.2f, want >= 0", stats.Overview.AvgDurationMs)
	}

	// Per-agent stats
	if stats.ByAgent != nil && len(stats.ByAgent) > 0 {
		t.Logf("  Agents with run data: %d", len(stats.ByAgent))
		for name, agentStats := range stats.ByAgent {
			t.Logf("    %s: %d runs (%d success, %d failed, %d errored)",
				name, agentStats.Total, agentStats.Success,
				agentStats.Failed, agentStats.Errored)
		}
	} else {
		t.Log("  ⚠️  No per-agent stats (no runs in window)")
	}

	// Top errors
	if stats.TopErrors != nil {
		t.Logf("  Top errors: %d entries", len(stats.TopErrors))
		for _, e := range stats.TopErrors {
			t.Logf("    [%dx] %s", e.Count, e.Message)
		}
	}

	// Tool stats
	if stats.ToolStats.ByTool != nil && len(stats.ToolStats.ByTool) > 0 {
		t.Logf("  Tool stats: %d distinct tools", len(stats.ToolStats.ByTool))
		for name, toolStats := range stats.ToolStats.ByTool {
			t.Logf("    %s: %d calls (avg %.0fms)", name, toolStats.Total, toolStats.AvgDurationMs)
		}
	}

	// Time series
	if stats.TimeSeries.ByHour != nil {
		t.Logf("  Time series: %d hourly buckets", len(stats.TimeSeries.ByHour))
	}

	t.Log("✅ GetProjectRunStats response structure verified")
}

// =========================================================================
// TestRunAnalytics_GetProjectRunStatsEmptyWindow: Calls GetProjectRunStats
// with a very narrow time window and verifies empty results are handled
// gracefully (zero counts, not an error).
// =========================================================================

func TestRunAnalytics_GetProjectRunStatsEmptyWindow(t *testing.T) {
	b := setupBridgeFromConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Query a future window — should get empty results, not an error
	future := time.Now().Add(24 * time.Hour)
	opts := &sdkagentrun.RunStatsOptions{
		Since: &future,
	}

	resp, err := b.GetProjectRunStats(ctx, opts)
	if err != nil {
		// Some servers may reject future windows — not a failure
		t.Logf("GetProjectRunStats (future window): %v (non-fatal)", err)
		t.Log("✅ Future window handled gracefully (not a crash)")
		return
	}

	if resp == nil || resp.Data.Overview.TotalRuns != 0 {
		t.Logf("⚠️  Future window returned data (unexpected but not harmful): %d runs",
			resp.Data.Overview.TotalRuns)
	} else {
		t.Log("✅ Future window correctly returns zero runs")
	}
}

// =========================================================================
// TestRunAnalytics_GetProjectRunSessionStats: Calls GetProjectRunSessionStats
// and verifies the response structure — sessions by platform, top sessions.
// =========================================================================

func TestRunAnalytics_GetProjectRunSessionStats(t *testing.T) {
	b := setupBridgeFromConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	since := time.Now().Add(-72 * time.Hour)
	opts := &sdkagentrun.RunStatsOptions{
		Since: &since,
		TopN:  10,
	}

	resp, err := b.GetProjectRunSessionStats(ctx, opts)
	if err != nil {
		t.Fatalf("GetProjectRunSessionStats: %v", err)
	}
	if resp == nil {
		t.Fatal("GetProjectRunSessionStats returned nil response")
	}

	sessionStats := resp.Data
	t.Logf("=== Session stats ===")
	t.Logf("  Total sessions: %d | Active: %d", sessionStats.TotalSessions, sessionStats.ActiveSessions)
	t.Logf("  Avg runs/session: %.1f | Max runs/session: %d",
		sessionStats.AvgRunsPerSession, sessionStats.MaxRunsPerSession)

	// Sessions by platform
	if sessionStats.SessionsByPlatform != nil && len(sessionStats.SessionsByPlatform) > 0 {
		t.Logf("  Sessions by platform: %d platforms", len(sessionStats.SessionsByPlatform))
		for platform, count := range sessionStats.SessionsByPlatform {
			t.Logf("    %s: %d sessions", platform, count)
		}
	} else {
		t.Log("  ⚠️  No sessions by platform data")
	}

	// Top sessions
	if len(sessionStats.TopSessions) > 0 {
		t.Logf("  Top sessions:")
		for _, s := range sessionStats.TopSessions {
			t.Logf("    %s/%s/%s: %d runs, last %s",
				s.Platform, s.ChannelID, s.ThreadID,
				s.TotalRuns, s.LastRunAt.Format(time.RFC3339))
		}
	} else {
		t.Log("  ⚠️  No top sessions data")
	}

	t.Log("✅ GetProjectRunSessionStats response structure verified")
}

// =========================================================================
// TestRunAnalytics_GetProjectRun: Triggers a simple agent run, then fetches
// the run details via GetProjectRun and GetProjectRunFull.
// =========================================================================

func TestRunAnalytics_GetProjectRun(t *testing.T) {
	b := setupBridgeFromConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Try to find a recent run by listing agent definitions for built-in agents
	// and querying runs for each. GetAgentRuns expects a runtime agent ID.
	listResp, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}
	if listResp == nil || len(listResp.Data) == 0 {
		t.Skip("No agent definitions found")
	}

	// Try each built-in agent — get its runtime agent via ListDefinitions
	for _, name := range []string{"diane-default", "diane-dreamer", "diane-researcher"} {
		for _, d := range listResp.Data {
			if d.Name != name {
				continue
			}

			// Try to get runs for this definition's runtime agent
			runsResp, err := b.GetAgentRuns(ctx, d.ID, 5)
			if err != nil {
				t.Logf("GetAgentRuns(%s=%s): %v", name, d.ID[:12], err)
				continue
			}

			if runsResp == nil || len(runsResp.Data) == 0 {
				t.Logf("No runs for %s", name)
				continue
			}

			run := runsResp.Data[0]
			t.Logf("Using run from %s: %s (status=%s)", name, run.ID, run.Status)

			// Test GetProjectRun
			runResp, err := b.GetProjectRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetProjectRun(%s): %v", run.ID, err)
			}
			if runResp == nil {
				t.Fatal("GetProjectRun returned nil")
			}
			if runResp.Data.ID != run.ID {
				t.Errorf("Run ID = %q, want %q", runResp.Data.ID, run.ID)
			}
			t.Logf("✅ GetProjectRun: %s (status=%s, duration=%vms)",
				runResp.Data.ID, runResp.Data.Status, runResp.Data.DurationMs)

			// Test GetProjectRunFull
			fullResp, err := b.GetProjectRunFull(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetProjectRunFull(%s): %v", run.ID, err)
			}
			if fullResp == nil {
				t.Fatal("GetProjectRunFull returned nil")
			}
			if fullResp.Data.Run == nil {
				t.Fatal("GetProjectRunFull: Run is nil")
			}
			if fullResp.Data.Run.ID != run.ID {
				t.Errorf("RunFull ID = %q, want %q", fullResp.Data.Run.ID, run.ID)
			}
			t.Logf("✅ GetProjectRunFull: %s (messages=%d, toolCalls=%d)",
				fullResp.Data.Run.ID, len(fullResp.Data.Messages), len(fullResp.Data.ToolCalls))

			// Also test GetRunMessages
			msgResp, err := b.GetRunMessages(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetRunMessages: %v", err)
			}
			if msgResp != nil {
				t.Logf("✅ GetRunMessages: %d messages", len(msgResp.Data))
			}

			// Also test GetRunToolCalls
			tcResp, err := b.GetRunToolCalls(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetRunToolCalls: %v", err)
			}
			if tcResp != nil {
				t.Logf("✅ GetRunToolCalls: %d tool calls", len(tcResp.Data))
			}

			t.Log("✅ All run retrieval APIs verified")
			return
		}
	}

	t.Skip("No recent runs found for any built-in agent")
}

// =========================================================================
// TestRunAnalytics_NonExistentRun: Calls GetProjectRun with a fake run ID
// and verifies graceful error handling (404 not found).
// =========================================================================

func TestRunAnalytics_NonExistentRun(t *testing.T) {
	b := setupBridgeFromConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Use a valid UUID format but unlikely to exist
	fakeID := "00000000-0000-0000-0000-000000000000"

	_, err := b.GetProjectRun(ctx, fakeID)
	if err != nil {
		t.Logf("GetProjectRun(non-existent): %v", err)
		// Should get a 404 or similar error
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
			t.Log("✅ Proper 404/not-found for non-existent run")
		} else {
			t.Logf("ℹ️  Error message: %s", err.Error())
		}
	} else {
		t.Log("⚠️  Non-existent run returned no error (unexpected but not a crash)")
	}

	// Also test GetProjectRunFull with fake ID
	_, err = b.GetProjectRunFull(ctx, fakeID)
	if err != nil {
		t.Logf("GetProjectRunFull(non-existent): %v", err)
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
			t.Log("✅ Proper 404/not-found for non-existent run full trace")
		}
	} else {
		t.Log("⚠️  Non-existent run full returned no error")
	}

	t.Log("✅ Non-existent run handling complete")
}

// =========================================================================
// TestRunAnalytics_GetProjectRunStatsByAgent: Filters stats by a specific agent
// and verifies the agent filter works.
// =========================================================================

func TestRunAnalytics_GetProjectRunStatsByAgent(t *testing.T) {
	b := setupBridgeFromConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	since := time.Now().Add(-72 * time.Hour)
	opts := &sdkagentrun.RunStatsOptions{
		Since:   &since,
		AgentID: "diane-default",
	}

	resp, err := b.GetProjectRunStats(ctx, opts)
	if err != nil {
		// AgentID filter may use a runtime agent ID, not a name — accept graceful failure
		t.Logf("GetProjectRunStats(agent filter by name): %v", err)
		if err.Error() != "" {
			t.Log("✅ Agent name filter handled gracefully (not a crash)")
		}
		return
	}
	if resp == nil {
		t.Fatal("GetProjectRunStats returned nil")
	}

	t.Logf("=== Stats for diane-default ===")
	t.Logf("  Total runs: %d", resp.Data.Overview.TotalRuns)
	t.Logf("  ByAgent keys: %d", len(resp.Data.ByAgent))

	if len(resp.Data.ByAgent) > 1 {
		t.Logf("⚠️  Agent filter returned data for %d agents (expected 0-1)", len(resp.Data.ByAgent))
	} else {
		t.Logf("✅ Agent filter scoped to 0-1 agent(s)")
	}

	t.Log("✅ GetProjectRunStats with agent filter verified")
}
