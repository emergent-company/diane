// Package memorytest validates the 'diane agent prune' command against the
// live Memory Platform — checks that orphan detection works correctly
// and that --force actually deletes orphaned agent definitions.
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestCLI_AgentPrune ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/memory"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
)

// =========================================================================
// TestCLI_AgentPrune_DryRun: Creates orphan agents on MP, runs 'diane agent prune'
// (dry-run), and verifies they're listed but NOT deleted.
// =========================================================================

func TestCLI_AgentPrune_DryRun(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-prune-dry-%d", time.Now().UnixMilli())
	orphan1 := prefix + "-orphan-1"
	orphan2 := prefix + "-orphan-2"

	// Create orphan agents on MP (not in config, not built-in)
	id1 := createPruneAgentDef(t, ctx, b, orphan1)
	id2 := createPruneAgentDef(t, ctx, b, orphan2)

	// Cleanup: delete orphans if test fails partway
	defer deletePruneAgentDef(t, ctx, b, id1, orphan1)
	defer deletePruneAgentDef(t, ctx, b, id2, orphan2)

	dianeBin := findDianeBinary(t)
	output, err := runCLI(ctx, t, dianeBin, "agent", "prune")
	if err != nil {
		t.Logf("prune exited with error (still checking output): %v", err)
	}

	t.Logf("=== prune dry-run output ===\n%s\n=== end ===", output)

	// Must show the orphan agents
	if !strings.Contains(output, orphan1) {
		t.Errorf("Dry-run output should contain orphan agent %q", orphan1)
	} else {
		t.Logf("✅ Found orphan: %s", orphan1)
	}
	if !strings.Contains(output, orphan2) {
		t.Errorf("Dry-run output should contain orphan agent %q", orphan2)
	} else {
		t.Logf("✅ Found orphan: %s", orphan2)
	}

	// Must indicate dry-run
	if !strings.Contains(output, "dry-run") && !strings.Contains(output, "dry run") {
		t.Error("Dry-run output should indicate nothing was deleted (contains 'dry-run')")
	} else {
		t.Log("✅ Output indicates dry-run")
	}

	// Verify orphans still exist on MP
	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs after dry-run: %v", err)
	}
	found1 := false
	found2 := false
	for _, d := range defs.Data {
		if d.Name == orphan1 {
			found1 = true
		}
		if d.Name == orphan2 {
			found2 = true
		}
	}
	if !found1 {
		t.Errorf("Orphan %q was deleted during dry-run (should still exist)", orphan1)
	} else {
		t.Logf("✅ Orphan %q still exists after dry-run", orphan1)
	}
	if !found2 {
		t.Errorf("Orphan %q was deleted during dry-run (should still exist)", orphan2)
	} else {
		t.Logf("✅ Orphan %q still exists after dry-run", orphan2)
	}
}

// =========================================================================
// TestCLI_AgentPrune_Force: Creates orphan agents, runs 'diane agent prune --force',
// and verifies they are actually deleted from MP.
// =========================================================================

func TestCLI_AgentPrune_Force(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-prune-frc-%d", time.Now().UnixMilli())
	orphan1 := prefix + "-orphan-a"
	orphan2 := prefix + "-orphan-b"

	// Create orphan agents
	id1 := createPruneAgentDef(t, ctx, b, orphan1)
	id2 := createPruneAgentDef(t, ctx, b, orphan2)

	// Safety cleanup: if --force fails, still try to remove these
	defer deletePruneAgentDef(t, ctx, b, id1, orphan1)
	defer deletePruneAgentDef(t, ctx, b, id2, orphan2)

	dianeBin := findDianeBinary(t)
	output, err := runCLI(ctx, t, dianeBin, "agent", "prune", "--force")
	if err != nil {
		t.Fatalf("prune --force failed: %v\nOutput: %s", err, output)
	}

	t.Logf("=== prune --force output ===\n%s\n=== end ===", output)

	// Must confirm deletion
	if !strings.Contains(output, "Deleted") && !strings.Contains(output, "deleted") {
		t.Error("Force output should indicate agents were deleted")
	} else {
		t.Log("✅ Output indicates deletion occurred")
	}

	// Verify orphans are actually gone from MP
	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs after prune: %v", err)
	}
	for _, d := range defs.Data {
		if d.Name == orphan1 {
			t.Errorf("Orphan %q still exists on MP after prune --force", orphan1)
		}
		if d.Name == orphan2 {
			t.Errorf("Orphan %q still exists on MP after prune --force", orphan2)
		}
	}
	t.Logf("✅ Both orphan agents successfully removed from MP")
}

// =========================================================================
// TestCLI_AgentPrune_Protected: Verifies that config agents and built-in
// agents are NOT listed as orphans.
// =========================================================================

func TestCLI_AgentPrune_Protected(t *testing.T) {
	skipIfNoConfig(t)

	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "prune")
	if err != nil {
		t.Logf("prune exited with error (still checking output): %v", err)
	}

	t.Logf("=== prune protected check output ===\n%s\n=== end ===", output)

	// Built-in agents should NOT appear in the orphan list
	protectedBuiltIns := []string{"diane-default", "diane-agent-creator", "diane-codebase"}
	for _, name := range protectedBuiltIns {
		if strings.Contains(output, name) {
			t.Errorf("Built-in agent %q should not appear in prune output", name)
		} else {
			t.Logf("✅ Built-in agent %q correctly excluded from orphans", name)
		}
	}

	// Config agents (test-search, test-knowledge) should NOT appear
	protectedConfig := []string{"test-search", "test-knowledge"}
	for _, name := range protectedConfig {
		if strings.Contains(output, name) {
			t.Errorf("Config agent %q should not appear in prune output", name)
		} else {
			t.Logf("✅ Config agent %q correctly excluded from orphans", name)
		}
	}
}

// =========================================================================
// Helpers
// =========================================================================

// createPruneAgentDef creates a minimal agent definition on MP for prune testing.
func createPruneAgentDef(t *testing.T, ctx context.Context, b *memory.Bridge, name string) string {
	t.Helper()
	desc := "Prune test agent — will be cleaned up"
	sysPrompt := "You are a test agent for prune testing."
	visibility := "project"

	created, err := b.CreateAgentDef(ctx, &sdkagents.CreateAgentDefinitionRequest{
		Name:         name,
		Description:  &desc,
		SystemPrompt: &sysPrompt,
		Visibility:   visibility,
	})
	if err != nil {
		t.Fatalf("CreateAgentDef(%q): %v", name, err)
	}
	defID := created.Data.ID
	t.Logf("Created test agent: %s (%s)", name, defID)
	return defID
}

// deletePruneAgentDef attempts to delete an agent definition for cleanup.
// Handles 404 gracefully (already deleted by prune).
func deletePruneAgentDef(t *testing.T, ctx context.Context, b *memory.Bridge, id, name string) {
	t.Helper()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.DeleteAgentDef(cleanupCtx, id); err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
			t.Logf("Cleanup: %q already deleted (expected after prune)", name)
		} else {
			t.Logf("Cleanup: DeleteAgentDef(%q): %v", name, err)
		}
	} else {
		t.Logf("Cleanup: deleted %q (%s)", name, id)
	}
}
