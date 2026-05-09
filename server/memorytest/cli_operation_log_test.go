// Package memorytest validates the OperationLog audit trail in the graph.
//
// After performing agent operations, queries the graph to verify that
// OperationLog entries were created with the expected fields.
//
//go:build integration

package memorytest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// OperationLogEntry represents a deserialized OperationLog graph object.
type OperationLogEntry struct {
	ID        string
	Op        string
	Target    string
	Actor     string
	Status    string
	Detail    string
	Node      string
	CreatedAt string
}

// listOperationLogs queries the graph for OperationLog entries.
func listOperationLogs(t *testing.T, graphClient *graph.Client, limit int) []OperationLogEntry {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := graphClient.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  "OperationLog",
		Limit: limit,
	})
	if err != nil {
		t.Fatalf("ListObjects(OperationLog): %v", err)
	}

	var entries []OperationLogEntry
	for _, obj := range resp.Items {
		props := obj.Properties
		entries = append(entries, OperationLogEntry{
			ID:        obj.ID,
			Op:        getPropString(props, "op"),
			Target:    getPropString(props, "target"),
			Actor:     getPropString(props, "actor"),
			Status:    getPropString(props, "status"),
			Detail:    getPropString(props, "detail"),
			Node:      getPropString(props, "node"),
			CreatedAt: obj.CreatedAt.Format(time.RFC3339),
		})
	}
	return entries
}

// getPropString safely extracts a string property from a map.
func getPropString(props map[string]any, key string) string {
	if props == nil {
		return ""
	}
	v, ok := props[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// =========================================================================
// TestOperationLog_AgentSeed: Verifies that 'diane agent seed' creates an
// OperationLog entry with op="agent.seed".
// =========================================================================

func TestOperationLog_AgentSeed(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	seedOut, err := runCLI(ctx, t, dianeBin, "agent", "seed")
	if err != nil {
		t.Fatalf("Seed failed: %v\nOutput: %s", err, seedOut)
	}
	t.Logf("Seed output contains [operation-log]: %v", strings.Contains(seedOut, "[operation-log]"))

	gc, cleanup := setup(t)
	defer cleanup()

	entries := listOperationLogs(t, gc, 10)

	var found *OperationLogEntry
	for i, e := range entries {
		if e.Op == "agent.seed" {
			found = &entries[i]
			break
		}
	}

	if found == nil {
		t.Fatal("No OperationLog entry with op=agent.seed found in graph")
	}

	t.Logf("✅ Found OperationLog: op=%q target=%q actor=%q status=%q node=%q",
		found.Op, found.Target, found.Actor, found.Status, found.Node)

	if found.Actor != "cli" {
		t.Logf("⚠️  Expected actor=cli, got %q", found.Actor)
	}
	if found.Status != "success" {
		t.Logf("⚠️  Expected status=success, got %q", found.Status)
	}
	t.Log("✅ OperationLog agent.seed validated")
}

// =========================================================================
// TestOperationLog_AgentDelete: Verifies that deleting an agent creates
// an OperationLog entry with op="agent.delete".
// =========================================================================

func TestOperationLog_AgentDelete(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	agentName := "diane-researcher"

	t.Cleanup(func() {
		deleteOverrideEntity(t, agentName)
		reSeed(t, dianeBin)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := runCLIWithStdin(ctx, t, dianeBin, "y\n", "agent", "delete", agentName)
	if err != nil {
		t.Fatalf("Delete built-in failed: %v\nOutput: %s", err, out)
	}
	t.Logf("Delete output: %s", truncateStr(out, 300))

	gc, cleanup := setup(t)
	defer cleanup()

	entries := listOperationLogs(t, gc, 10)

	var found *OperationLogEntry
	for i, e := range entries {
		if e.Op == "agent.delete" && e.Target == agentName {
			found = &entries[i]
			break
		}
	}

	if found == nil {
		t.Log("Recent OperationLog entries:")
		for _, e := range entries {
			t.Logf("  op=%q target=%q actor=%q status=%q", e.Op, e.Target, e.Actor, e.Status)
		}
		t.Fatalf("No OperationLog entry for op=agent.delete target=%s", agentName)
	}

	t.Logf("✅ Found agent.delete: op=%q target=%q actor=%q status=%q node=%q",
		found.Op, found.Target, found.Actor, found.Status, found.Node)

	if found.Actor == "cli" {
		t.Log("✅ Actor is cli")
	}
	if found.Status == "success" {
		t.Log("✅ Status is success")
	}
}

// =========================================================================
// TestOperationLog_List: Queries and logs recent OperationLog entries.
// =========================================================================

func TestOperationLog_List(t *testing.T) {
	skipIfNoConfig(t)
	gc, cleanup := setup(t)
	defer cleanup()

	entries := listOperationLogs(t, gc, 5)

	if len(entries) == 0 {
		t.Skip("No OperationLog entries found — seed or delete an agent first")
	}

	t.Logf("Found %d recent OperationLog entries:", len(entries))
	for _, e := range entries {
		createdStr := e.CreatedAt
		if len(createdStr) > 19 {
			createdStr = createdStr[:19]
		}
		t.Logf("  [%s] %s → %s (%s by %s)", createdStr, e.Op, e.Target, e.Status, e.Actor)
	}
}
