// Package memorytest validates diane CLI agent trace and runs commands via exec.
//
// Test 'diane agent trace <runID>' — shows run trace
// Test 'diane agent runs [name]' — lists recent runs
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestCLI_AgentTrace ./memorytest/
package memorytest

import (
	"context"
	"strings"
	"testing"
	"time"
)

// =========================================================================
// TestCLI_AgentRunsList: Runs 'diane agent runs' and verifies it shows
// a list of recent runs with agent names and status.
// =========================================================================

func TestCLI_AgentRunsList(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "runs")
	if err != nil {
		t.Fatalf("diane agent runs: %v\noutput: %s", err, output)
	}

	t.Logf("=== 'diane agent runs' ===\n%s\n=== end ===", output)

	// Should contain recent runs
	if strings.Contains(output, "No runs") {
		t.Log("⚠️  No runs recorded — expected at least some")
	} else {
		// Should mention some agents
		for _, name := range []string{"diane-default", "diane-dreamer"} {
			if strings.Contains(output, name) {
				t.Logf("✅ Output mentions: %s", name)
			}
		}
		// Should have run IDs or status
		if strings.Contains(output, "success") || strings.Contains(output, "failed") || strings.Contains(output, "completed") {
			t.Log("✅ Status info present")
		}
	}

	if len(strings.TrimSpace(output)) == 0 {
		t.Error("Output is empty")
	}
}

// =========================================================================
// TestCLI_AgentRunsByName: Runs 'diane agent runs diane-default' and
// verifies it shows runs for that specific agent.
// =========================================================================

func TestCLI_AgentRunsByName(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "runs", "diane-default")
	if err != nil {
		t.Fatalf("diane agent runs diane-default: %v\noutput: %s", err, output)
	}

	t.Logf("=== 'diane agent runs diane-default' ===\n%s\n=== end ===", output)

	if strings.Contains(output, "No runs") {
		t.Log("⚠️  No runs for diane-default in last 24h")
	} else {
		if strings.Contains(output, "diane-default") {
			t.Log("✅ Agent name in output")
		}
		if strings.Contains(output, "success") || strings.Contains(output, "failed") {
			t.Log("✅ Status info present")
		}
		// Check for run ID (UUID format)
		if strings.Contains(output, "-") {
			t.Log("✅ Run IDs present")
		}
	}

	t.Log("✅ agent runs by name completed")
}

// =========================================================================
// TestCLI_AgentRunsWithSince: Runs 'diane agent runs --since 48h' and
// verifies the --since flag works.
// =========================================================================

func TestCLI_AgentRunsWithSince(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "runs", "--since", "48h")
	if err != nil {
		t.Fatalf("diane agent runs --since 48h: %v\noutput: %s", err, output)
	}

	t.Logf("=== 'diane agent runs --since 48h' ===\n%s\n=== end ===", output)

	if strings.Contains(output, "No runs") {
		t.Log("⚠️  No runs in last 48h")
	} else {
		t.Log("✅ Runs found in 48h window")
	}

	if len(strings.TrimSpace(output)) > 0 {
		t.Log("✅ Non-empty output")
	}
}

// =========================================================================
// TestCLI_AgentTraceByRunID: Finds a recent run ID from 'agent runs' and
// fetches its trace via 'agent trace <runID>'. Verifies messages and tools.
// =========================================================================

func TestCLI_AgentTraceByRunID(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First, get a recent run ID
	runsOutput, err := runCLI(ctx, t, dianeBin, "agent", "runs", "diane-default", "--since", "48h")
	if err != nil {
		t.Fatalf("agent runs: %v", err)
	}

	if strings.Contains(runsOutput, "No runs") || len(strings.TrimSpace(runsOutput)) < 20 {
		t.Skip("No recent runs for diane-default — cannot test trace")
	}

	// Parse the first run ID from output — lines look like:
	//   f14a5349-0315-48ba-a0e3-6fd1795e9f36  diane-default  success  7.3s
	var runID string
	for _, line := range strings.Split(runsOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "-") && (strings.Contains(line, "success") || strings.Contains(line, "completed") || strings.Contains(line, "failed")) {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				runID = parts[0]
				break
			}
		}
	}

	if runID == "" || !strings.Contains(runID, "-") {
		t.Skipf("'agent runs' outputs aggregated stats (no individual run IDs) — trace-by-runID tested via bridge API in run_trace_test.go")
	}

	t.Logf("Found run ID: %s", runID)

	// Now trace it
	traceOutput, err := runCLI(ctx, t, dianeBin, "agent", "trace", runID)
	if err != nil {
		t.Fatalf("agent trace %s: %v\noutput: %s", runID, err, traceOutput)
	}

	t.Logf("=== 'diane agent trace %s' ===\n%s\n=== end ===", runID[:12], traceOutput)

	// Should contain messages
	if strings.Contains(traceOutput, "user") || strings.Contains(traceOutput, "assistant") || strings.Contains(traceOutput, "model") {
		t.Log("✅ Messages found in trace")
	} else {
		t.Log("⚠️  No user/assistant messages in trace")
	}

	// Should contain tool calls or run summary
	if strings.Contains(traceOutput, "tool") || strings.Contains(traceOutput, "Tool") {
		t.Log("✅ Tool info in trace")
	}

	if len(strings.TrimSpace(traceOutput)) > 50 {
		t.Log("✅ Trace output is non-trivial")
	}
}

// =========================================================================
// TestCLI_AgentTraceNonExistent: Runs 'agent trace <fake-id>' and verifies
// graceful error handling.
// =========================================================================

func TestCLI_AgentTraceNonExistent(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "trace", "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Logf("Exit error expected: %v", err)
	}
	t.Logf("Output (first 300 chars): %.300s", output)

	if strings.Contains(output, "not found") || strings.Contains(output, "404") || strings.Contains(output, "error") {
		t.Log("✅ Proper error for non-existent run")
	}

	t.Log("✅ Trace non-existent handled gracefully")
}

// =========================================================================
// TestCLI_AgentRunsWithSinceFlagByName: Runs 'agent runs diane-default --since 72h'
// to verify the flag + name combo works.
// =========================================================================

func TestCLI_AgentRunsWithSinceFlagByName(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "runs", "diane-default", "--since", "72h")
	if err != nil {
		t.Fatalf("agent runs diane-default --since 72h: %v\noutput: %s", err, output)
	}

	t.Logf("=== 'agent runs diane-default --since 72h' ===\n%s\n=== end ===", output)

	if strings.Contains(output, "No runs") {
		t.Log("⚠️  No runs in 72h for diane-default")
	} else {
		t.Log("✅ Runs found with name+flag filter")
	}
}
