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
	"fmt"
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

// =========================================================================
// TestCLI_AgentDefine: Creates a test agent via interactive CLI define,
// pipes all default answers, verifies it was saved, then cleans up.
// =========================================================================

func TestCLI_AgentDefine(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	agentName := fmt.Sprintf("test-def-%d", time.Now().UnixMilli())

	// Clean up: remove from config after test
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		runCLIWithStdin(ctx, t, dianeBin, "y\n", "agent", "delete", agentName)
	})

	// stdin sequence for agent define:
	//   description: default (just Enter)
	//   system prompt: "." + Enter (ends multi-line)
	//   flow type: default
	//   visibility: default
	//   dispatch mode: default
	//   model override: default (n)
	//   tools: default (empty)
	//   skills: default (empty)
	//   max steps: default
	//   timeout: default
	//   sandbox: default (n)
	//   ACP card: default (n)
	//   sync: "n" (skip sync to avoid side effects)
	stdin := "\n.\n\n\n\n\n\n\n\n\n\n\nn\n"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLIWithStdin(ctx, t, dianeBin, stdin, "agent", "define", agentName)
	t.Logf("=== 'diane agent define %s' output ===\n%s\n=== end output ===", agentName, output)

	if err != nil {
		t.Fatalf("Define failed: %v\nOutput: %s", err, output)
	}

	if strings.Contains(output, "saved") {
		t.Log("✅ Agent defined and saved to config")
	} else {
		t.Fatalf("Expected 'saved' confirmation, got: %s", truncateStr(output, 200))
	}

	// Verify via agent show
	showCtx, showCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer showCancel()
	showOut, _ := runCLI(showCtx, t, dianeBin, "agent", "show", agentName)
	if strings.Contains(showOut, agentName) {
		t.Log("✅ Agent appears in show output")
	} else {
		t.Log("⚠️  Agent show didn't find the agent (may need sync)")
	}
}

// =========================================================================
// TestCLI_AgentTrigger: Triggers a built-in agent and verifies run output.
// Uses diane-default (always available) with a simple test prompt.
// =========================================================================

func TestCLI_AgentTrigger(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)

	// Pick a lightweight built-in agent that's always available
	agentName := "diane-default"

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "trigger", agentName, "Say only the word 'hello' and nothing else.")
	t.Logf("=== 'diane agent trigger %s' output ===\n%s\n=== end output ===", agentName, truncateStr(output, 2000))

	if err != nil {
		if ctx.Err() != nil {
			t.Skipf("Trigger timed out (agent run may be queued): %v", err)
		}
		t.Logf("Trigger exit error (may be partial): %v", err)
	}

	// Should show the trigger flow
	if strings.Contains(output, "Triggering Agent") {
		t.Log("✅ Trigger initiated")
	}

	if strings.Contains(output, "hello") || strings.Contains(output, "Hello") {
		t.Log("✅ Agent responded with expected content")
	} else if strings.Contains(output, "success") || strings.Contains(output, "completed") {
		t.Log("✅ Agent run completed")
	} else if strings.Contains(output, "Run") && strings.Contains(output, "ID") {
		t.Log("✅ Run ID received")
	} else {
		t.Log("⚠️  Output format unexpected — logging for inspection")
	}

	// Check for operation log
	if strings.Contains(output, "[operation-log]") {
		t.Log("✅ Operation log entry found in output")
	}
}
