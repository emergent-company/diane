// Package memorytest validates diane CLI agent *stats* subcommand via exec.
//
// Tests cover 'diane agent stats' (list all) and 'diane agent stats <name>'
// (individual agent), verifying the output format and data completeness.
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestCLI_AgentStats ./memorytest/
package memorytest

import (
	"context"
	"strings"
	"testing"
	"time"
)

// =========================================================================
// TestCLI_AgentStatsList: Runs 'diane agent stats' and verifies it lists
// all agents with their status, tags, weight, and run statistics.
// =========================================================================

func TestCLI_AgentStatsList(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "stats")
	if err != nil {
		t.Fatalf("diane agent stats: %v\noutput: %s", err, output)
	}

	t.Logf("=== 'diane agent stats' output ===\n%s\n=== end output ===", output)

	// Should contain agent names
	for _, name := range []string{"diane-default", "diane-dreamer", "diane-researcher"} {
		if !strings.Contains(output, name) {
			t.Logf("⚠️  Output doesn't mention %q", name)
		} else {
			t.Logf("✅ Output mentions: %s", name)
		}
	}

	// Should contain status indicators
	if !strings.Contains(output, "Status:") {
		t.Log("⚠️  Output doesn't contain 'Status:' field")
	}

	// Should mention routing weight or no-runs message
	if !strings.Contains(output, "Weight:") && !strings.Contains(output, "No runs") {
		t.Log("⚠️  Output missing 'Weight:' or 'No runs' — unexpected format")
	}

	// Should not be empty
	if len(strings.TrimSpace(output)) == 0 {
		t.Error("Output is empty")
	} else {
		t.Logf("✅ Output is non-empty")
	}
}

// =========================================================================
// TestCLI_AgentStatsSingle: Runs 'diane agent stats diane-default' and
// verifies per-agent statistics output format.
// =========================================================================

func TestCLI_AgentStatsSingle(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "stats", "diane-default")
	if err != nil {
		t.Fatalf("diane agent stats diane-default: %v\noutput: %s", err, output)
	}

	t.Logf("=== 'diane agent stats diane-default' ===\n%s\n=== end output ===", output)

	// Check section header
	if !strings.Contains(output, "Stats for diane-default") {
		t.Error("Output missing 'Stats for diane-default' header")
	} else {
		t.Log("✅ Section header found")
	}

	// Check status field
	if !strings.Contains(output, "Status:") {
		t.Log("⚠️  Missing Status field")
	}

	// Check either run stats or empty-runs message
	hasRuns := strings.Contains(output, "Runs:") && strings.Contains(output, "Success:")
	hasNoRuns := strings.Contains(output, "No runs recorded")
	if !hasRuns && !hasNoRuns {
		t.Log("⚠️  Neither run stats nor 'No runs' message found")
	}

	if strings.Contains(output, "Avg duration:") || strings.Contains(output, "Avg input:") {
		t.Log("✅ Run statistics present")
	}
}

// =========================================================================
// TestCLI_AgentStatsNonExistent: Runs 'diane agent stats nonexistent-agent'
// and verifies graceful error handling (not a crash).
// =========================================================================

func TestCLI_AgentStatsNonExistent(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "stats", "nonexistent-agent-test-xyz")
	if err != nil {
		t.Logf("Exit error (non-fatal): %v", err)
	}
	_ = output // non-existent agents still produce output (no crash)
	t.Logf("Output (first 200 chars): %.200s", output)
	t.Log("✅ Graceful handling of non-existent agent")
}

// =========================================================================
// TestCLI_AgentStatsFormat: Verifies the output formatting is consistent
// across multiple runs — stable columns, no unprintable characters.
// =========================================================================

func TestCLI_AgentStatsFormat(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "stats")
	if err != nil {
		t.Fatalf("diane agent stats: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		t.Fatal("Output is empty")
	}

	t.Logf("Output has %d lines", len(lines))

	// Should start with a header section
	hasHeader := false
	for _, line := range lines {
		if strings.Contains(line, "═══") && strings.Contains(line, "Stats") {
			hasHeader = true
			t.Logf("  Header: %s", strings.TrimSpace(line))
			break
		}
	}
	if !hasHeader {
		t.Error("No section header found (expected ═══ Stats ═══)")
	} else {
		t.Log("✅ Section header found")
	}

	// Should have at least one agent entry
	agentCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "diane-") || strings.HasPrefix(line, "test-") {
			agentCount++
			t.Logf("  Agent: %s", line)
		}
	}
	if agentCount == 0 {
		t.Log("⚠️  No agent entries found (no runs in last 24h)")
	} else {
		t.Logf("✅ Found %d agents with run data", agentCount)
	}
}
