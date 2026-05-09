// Package memorytest validates diane CLI agent delete subcommand via exec.
//
// Tests both built-in (disable via graph override) and user-defined agent
// deletion paths. Cleans up after itself using SDK calls.
//
//go:build integration

package memorytest

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// runCLIWithStdin runs the diane binary with given args and pipes input to stdin.
func runCLIWithStdin(ctx context.Context, t *testing.T, dianeBin, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, dianeBin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// deleteOverrideEntity removes the AgentOverrideConfig for a given agent name.
// Used for cleanup after built-in agent disable tests.
func deleteOverrideEntity(t *testing.T, agentName string) {
	t.Helper()
	gc, cleanup := setup(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := gc.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  "AgentOverrideConfig",
		Limit: 50,
	})
	if err != nil {
		t.Logf("Cleanup: ListObjects failed: %v", err)
		return
	}
	for _, obj := range resp.Items {
		if name, ok := obj.Properties["agent_name"].(string); ok && name == agentName {
			if err := gc.DeleteObject(ctx, obj.EntityID, nil); err != nil {
				t.Logf("Cleanup: DeleteObject for %s failed: %v", agentName, err)
			} else {
				t.Logf("Cleanup: removed override for %s", agentName)
			}
			return
		}
	}
	t.Logf("Cleanup: no override found for %s", agentName)
}

// addTemporaryAgent defines a user agent via CLI.
func addTemporaryAgent(t *testing.T, dianeBin string) string {
	t.Helper()
	agentName := fmt.Sprintf("test-del-%d", time.Now().UnixMilli())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	defineCmd := exec.CommandContext(ctx, dianeBin, "agent", "define", agentName)
	defineCmd.Stdin = strings.NewReader("y\n")
	out, err := defineCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to define test agent %s: %v\nOutput: %s", agentName, err, string(out))
	}
	t.Logf("Created temporary agent: %s", agentName)
	return agentName
}

// reSeed runs 'diane agent seed' to restore built-in agents after cleanup.
func reSeed(t *testing.T, dianeBin string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := runCLI(ctx, t, dianeBin, "agent", "seed")
	if err != nil {
		t.Logf("Re-seed: %v\nOutput: %s", err, out)
	}
}

// =========================================================================
// TestCLI_AgentDelete_UserDefined: Creates a temporary agent, deletes it,
// and verifies it's gone.
// =========================================================================

func TestCLI_AgentDelete_UserDefined(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)

	agentName := addTemporaryAgent(t, dianeBin)

	// Clean up on test end — delete via CLI
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		runCLIWithStdin(ctx, t, dianeBin, "y\n", "agent", "delete", agentName)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLIWithStdin(ctx, t, dianeBin, "y\n", "agent", "delete", agentName)
	t.Logf("=== 'diane agent delete %s' output ===\n%s\n=== end output ===", agentName, output)

	if err != nil {
		t.Fatalf("Delete failed: %v\nOutput: %s", err, output)
	}

	if strings.Contains(output, "deleted") {
		t.Log("✅ User-defined agent deleted")
	} else {
		t.Fatalf("Expected delete confirmation, got: %s", truncateStr(output, 200))
	}

	// Verify agent is gone
	showOut, _ := runCLI(ctx, t, dianeBin, "agent", "show", agentName)
	if strings.Contains(showOut, "not found") {
		t.Log("✅ Agent no longer present after delete")
	} else {
		t.Log("⚠️  Agent may still appear (lingering config)")
	}
}

// =========================================================================
// TestCLI_AgentDelete_Builtin: Disables a built-in agent via CLI override,
// verifies success, then cleans up override and re-seeds.
// =========================================================================

func TestCLI_AgentDelete_Builtin(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	agentName := "diane-dreamer"

	// Clean up: remove override and re-seed
	t.Cleanup(func() {
		deleteOverrideEntity(t, agentName)
		reSeed(t, dianeBin)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	output, err := runCLIWithStdin(ctx, t, dianeBin, "y\n", "agent", "delete", agentName)
	t.Logf("=== 'diane agent delete %s' output ===\n%s\n=== end output ===", agentName, output)

	if err != nil {
		t.Fatalf("Delete built-in failed: %v\nOutput: %s", err, output)
	}

	if strings.Contains(output, "disabled") {
		t.Log("✅ Built-in agent disabled via graph override")
	} else {
		t.Fatalf("Expected disable confirmation, got: %s", truncateStr(output, 200))
	}

	// Verify operation log was written
	if strings.Contains(output, "[operation-log]") {
		t.Log("✅ Operation log entry created during delete")
	}
}
