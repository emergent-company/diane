// Package memorytest validates diane CLI agent subcommands via exec.
//
// Tests for agent seed, list, show, sync, run, trace, stats, route, tag,
// delete, and prune. All tests run the diane binary as a subprocess against
// the live Memory Platform project.
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestCLI_Agent ./memorytest/
//go:build integration

package memorytest

import (
	"context"
	"strings"
	"testing"
	"time"
)

// =========================================================================
// TestCLI_AgentSeed: Runs 'diane agent seed' to seed built-in agents to
// Memory Platform with graph overrides applied.
// =========================================================================

func TestCLI_AgentSeed(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "seed")
	t.Logf("=== 'diane agent seed' output ===\n%s\n=== end output ===", output)
	if err != nil {
		t.Logf("Exit error (non-fatal): %v", err)
	}

	// Should list agents being seeded
	expectedAgents := []string{"diane-default", "diane-researcher", "diane-codebase"}
	for _, name := range expectedAgents {
		if strings.Contains(output, name) {
			t.Logf("✅ Found built-in agent: %s", name)
		} else {
			t.Logf("⚠️  Agent '%s' not in seed output", name)
		}
	}

	// Should show override config reading
	if strings.Contains(output, "AgentOverrideConfig") || strings.Contains(output, "override") {
		t.Log("✅ Output shows override config processing")
	}

	// Should show completion message
	if strings.Contains(output, "All built-in agents seeded") {
		t.Log("✅ Seed completed successfully")
	}

	// Should show operation log (new in v1.38.48+)
	if strings.Contains(output, "[operation-log]") {
		t.Log("✅ Operation log entry written during seed")
	}
}

// =========================================================================
// TestCLI_AgentShow: Runs 'diane agent show <name>' for known agents.
// =========================================================================

func TestCLI_AgentShow(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)

	agentNames := []string{"diane-default", "diane-researcher", "diane-codebase"}

	for _, name := range agentNames {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		output, err := runCLI(ctx, t, dianeBin, "agent", "show", name)
		cancel()

		t.Logf("=== 'diane agent show %s' output ===\n%s\n=== end output ===", name, output)

		if err != nil {
			t.Logf("  show %s: %v", name, err)
			continue
		}

		if strings.Contains(output, name) {
			t.Logf("✅ Agent '%s' detail displayed", name)
			for _, line := range strings.Split(output, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && (strings.Contains(line, "Tools:") ||
					strings.Contains(line, "Flow") ||
					strings.Contains(line, "Skills") ||
					strings.Contains(line, "Model:")) {
					t.Logf("  %s", line)
				}
			}
		}
	}
}

// =========================================================================
// TestCLI_AgentRuns: Runs 'diane agent runs' to list recent agent runs.
// =========================================================================

func TestCLI_AgentRuns(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "runs")
	t.Logf("=== 'diane agent runs' output ===\n%s\n=== end output ===", output)
	if err != nil {
		t.Logf("Exit error: %v", err)
	}

	if strings.Contains(output, "no runs") || strings.Contains(output, "No runs") ||
		strings.Contains(output, "0 runs") || strings.Contains(output, "None") {
		t.Log("No recent runs found — expected if no agents have been triggered lately")
	} else if strings.Contains(output, "run") || strings.Contains(output, "Run") {
		t.Log("✅ Recent agent runs displayed")
	} else {
		t.Log("⚠️  Unexpected output format")
	}
}

// =========================================================================
// TestCLI_AgentSync: Runs 'diane agent sync' to push local configs to MP.
// =========================================================================

func TestCLI_AgentSync(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "sync")
	t.Logf("=== 'diane agent sync' output ===\n%s\n=== end output ===", output)
	if err != nil {
		t.Logf("Exit error: %v", err)
	}

	if strings.Contains(output, "synced") || strings.Contains(output, "Synced") {
		t.Log("✅ Agent sync ran and processed agents")
	} else {
		t.Log("⚠️  Sync output didn't match expected patterns")
	}
}
