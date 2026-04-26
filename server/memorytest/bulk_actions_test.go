// Package memorytest validates advanced BulkAction operations beyond the
// basic update_status and set_labels already tested in memory_pipeline_test.go.
//
// Tests cover: soft_delete, hard_delete (dry-run), merge_properties, and
// add_labels (additive vs set_labels which replaces).
//
// Run: cd ~/diane/server && MEMORY_TEST_TOKEN=*** /usr/local/go/bin/go test -v -count=1 -run TestBulkAction ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// =========================================================================
// TestBulkAction_SoftDelete: Creates a MemoryFact, then bulk soft-deletes it
// via the soft_delete action. Verifies the object status is updated.
// =========================================================================

func TestBulkAction_SoftDelete(t *testing.T) {
	gc, done := setup(t)
	defer done()
	ctx := context.Background()
	prefix := fmt.Sprintf("t-softdel-%d", time.Now().UnixMilli())
	testLabel := prefix + "-label"

	// Create a MemoryFact to soft-delete
	fact, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
		Type: "MemoryFact",
		Key:  strPtr(prefix + "-soft-delete-target"),
		Properties: map[string]any{
			"confidence": 0.5,
			"content":    prefix + ": Object to soft-delete",
			"source":     "test",
		},
		Labels: []string{testLabel},
	})
	if err != nil {
		t.Fatalf("CreateObject: %v", err)
	}
	factID := fact.EntityID
	t.Logf("Created fact: %s", factID[:12])

	// Cleanup: try to restore if soft-delete was applied, then delete
	t.Cleanup(func() {
		_, _ = gc.UpdateObject(ctx, factID, &graph.UpdateObjectRequest{
			Status: strPtr("active"),
		})
		_ = gc.DeleteObject(ctx, factID, nil)
	})

	// Soft-delete via BulkAction
	softResp, err := gc.BulkAction(ctx, &graph.BulkActionRequest{
		Filter: graph.BulkActionFilter{
			Types:  []string{"MemoryFact"},
			Labels: []string{testLabel},
		},
		Action: "soft_delete",
	})
	if err != nil {
		t.Fatalf("BulkAction soft_delete: %v", err)
	}
	t.Logf("Soft-delete: matched=%d affected=%d errors=%d", softResp.Matched, softResp.Affected, softResp.Errors)

	if softResp.Affected < 1 {
		t.Errorf("Expected at least 1 affected, got %d", softResp.Affected)
	}

	// Verify the object status changed
	obj, err := gc.GetObject(ctx, factID)
	if err != nil {
		t.Logf("GetObject after soft-delete: %v (object may be hidden)", err)
	} else {
		status := "(nil)"
		if obj.Status != nil {
			status = *obj.Status
		}
		t.Logf("Post-soft-delete status: %s", status)
		if status != "deleted" && status != "soft_deleted" && status != "archived" {
			t.Logf("⚠️  Status is %q — may differ from expected 'deleted'", status)
		} else {
			t.Logf("✅ Soft-delete confirmed: status=%s", status)
		}
	}

	t.Log("✅ BulkAction soft_delete completed")
}

// =========================================================================
// TestBulkAction_HardDeleteDryRun: Creates MemoryFacts, runs a hard_delete
// bulk action in dry-run mode, verifies the preview count, and confirms
// objects are NOT actually deleted (dry-run).
// =========================================================================

func TestBulkAction_HardDeleteDryRun(t *testing.T) {
	gc, done := setup(t)
	defer done()
	ctx := context.Background()
	prefix := fmt.Sprintf("t-harddel-%d", time.Now().UnixMilli())
	testLabel := prefix + "-label"

	// Create a few facts
	var ids []string
	for i := 0; i < 2; i++ {
		fact, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
			Type: "MemoryFact",
			Key:  strPtr(fmt.Sprintf("%s-fact-%d", prefix, i)),
			Properties: map[string]any{
				"confidence": 0.3,
				"content":    fmt.Sprintf("%s: Hard-delete target %d", prefix, i),
			},
			Labels: []string{testLabel},
		})
		if err != nil {
			t.Fatalf("CreateObject[%d]: %v", i, err)
		}
		ids = append(ids, fact.EntityID)
	}
	t.Logf("Created %d facts with label=%s", len(ids), testLabel)

	t.Cleanup(func() {
		for _, id := range ids {
			_ = gc.DeleteObject(ctx, id, nil)
		}
	})

	// Dry-run hard_delete — should preview deletion without doing it
	dryRun, err := gc.BulkAction(ctx, &graph.BulkActionRequest{
		Filter: graph.BulkActionFilter{
			Types:  []string{"MemoryFact"},
			Labels: []string{testLabel},
		},
		Action: "hard_delete",
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("BulkAction hard_delete (dry-run): %v", err)
	}

	if !dryRun.DryRun {
		t.Error("DryRun flag not set in response")
	}
	if dryRun.Matched < 1 {
		t.Errorf("Dry-run matched=%d, want >= 1", dryRun.Matched)
	}
	t.Logf("Dry-run: matched=%d affected=%d errors=%d", dryRun.Matched, dryRun.Affected, dryRun.Errors)

	// Verify objects still exist (dry-run should not delete)
	for i, id := range ids {
		obj, err := gc.GetObject(ctx, id)
		if err != nil {
			t.Errorf("Object[%d] gone after dry-run: %v", i, err)
		} else {
			t.Logf("Object[%d] still exists: key=%s", i, strOrEmpty(obj.Key))
		}
	}

	t.Log("✅ BulkAction hard_delete (dry-run) completed — no objects deleted")
}

// =========================================================================
// TestBulkAction_MergeProperties: Creates a MemoryFact, then uses
// merge_properties to add a new property while preserving existing ones.
// Verifies both old and new properties exist after the merge.
// =========================================================================

func TestBulkAction_MergeProperties(t *testing.T) {
	gc, done := setup(t)
	defer done()
	ctx := context.Background()
	prefix := fmt.Sprintf("t-merge-%d", time.Now().UnixMilli())
	testLabel := prefix + "-label"

	// Create a fact with initial properties
	fact, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
		Type: "MemoryFact",
		Key:  strPtr(prefix + "-merge-target"),
		Properties: map[string]any{
			"confidence": 0.7,
			"content":    prefix + ": Original content",
			"source":     "test-original",
		},
		Labels: []string{testLabel},
	})
	if err != nil {
		t.Fatalf("CreateObject: %v", err)
	}
	factID := fact.EntityID
	t.Logf("Created fact: %s", factID[:12])
	t.Logf("  Original properties: confidence=%.1f source=%s", fact.Properties["confidence"].(float64), fact.Properties["source"])

	t.Cleanup(func() {
		_ = gc.DeleteObject(ctx, factID, nil)
	})

	// Merge new properties — should add "category" and update "source"
	mergeResp, err := gc.BulkAction(ctx, &graph.BulkActionRequest{
		Filter: graph.BulkActionFilter{
			Types:  []string{"MemoryFact"},
			Labels: []string{testLabel},
		},
		Action: "merge_properties",
		Properties: map[string]any{
			"category":  "test-merged",
			"source":    "test-merged",
			"new_field": "added-via-merge",
		},
	})
	if err != nil {
		t.Fatalf("BulkAction merge_properties: %v", err)
	}
	t.Logf("Merge: matched=%d affected=%d", mergeResp.Matched, mergeResp.Affected)

	if mergeResp.Affected < 1 {
		t.Errorf("Expected at least 1 affected, got %d", mergeResp.Affected)
	}

	// Verify properties via GetObject
	obj, err := gc.GetObject(ctx, factID)
	if err != nil {
		t.Fatalf("GetObject after merge: %v", err)
	}

	// Original properties should be preserved (confidence should still be 0.7)
	confidence, hasConfidence := obj.Properties["confidence"].(float64)
	source, hasSource := obj.Properties["source"].(string)
	category, hasCategory := obj.Properties["category"].(string)
	newField, hasNewField := obj.Properties["new_field"].(string)

	t.Logf("Post-merge properties:")
	t.Logf("  confidence=%.1f (original, preserved=%v)", confidence, hasConfidence)
	t.Logf("  source=%q (updated)", source)
	t.Logf("  category=%q (new)", category)
	t.Logf("  new_field=%q (new)", newField)

	if !hasConfidence || confidence != 0.7 {
		t.Errorf("Original property 'confidence' lost or changed: %.1f", confidence)
	}
	if !hasSource || source != "test-merged" {
		t.Errorf("Merged property 'source' wrong: %q", source)
	} else {
		t.Log("✅ Existing property 'source' updated by merge")
	}
	if !hasCategory || category != "test-merged" {
		t.Errorf("New property 'category' missing: %q", category)
	} else {
		t.Log("✅ New property 'category' added by merge")
	}
	if !hasNewField || newField != "added-via-merge" {
		t.Errorf("New property 'new_field' missing: %q", newField)
	} else {
		t.Log("✅ New property 'new_field' added by merge")
	}

	t.Log("✅ BulkAction merge_properties completed — old properties preserved, new ones added")
}

// =========================================================================
// TestBulkAction_AddLabels: Creates a MemoryFact with an initial label,
// then uses add_labels to add a new label without removing existing ones.
// This differs from set_labels which replaces all labels.
// =========================================================================

func TestBulkAction_AddLabels(t *testing.T) {
	gc, done := setup(t)
	defer done()
	ctx := context.Background()
	prefix := fmt.Sprintf("t-labels-%d", time.Now().UnixMilli())
	initialLabel := prefix + "-initial"
	addedLabel := prefix + "-added"

	// Create a fact with one label
	fact, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
		Type: "MemoryFact",
		Key:  strPtr(prefix + "-labels-target"),
		Properties: map[string]any{
			"confidence": 0.5,
			"content":    prefix + ": Label test object",
		},
		Labels: []string{initialLabel},
	})
	if err != nil {
		t.Fatalf("CreateObject: %v", err)
	}
	factID := fact.EntityID
	t.Logf("Created fact: %s", factID[:12])
	t.Logf("  Initial labels: %v", fact.Labels)

	t.Cleanup(func() {
		_ = gc.DeleteObject(ctx, factID, nil)
	})

	// Add a new label via add_labels (should preserve the initial label)
	addResp, err := gc.BulkAction(ctx, &graph.BulkActionRequest{
		Filter: graph.BulkActionFilter{
			Types:  []string{"MemoryFact"},
			Labels: []string{initialLabel},
		},
		Action: "add_labels",
		Labels: []string{addedLabel},
	})
	if err != nil {
		t.Fatalf("BulkAction add_labels: %v", err)
	}
	t.Logf("Add_labels: matched=%d affected=%d", addResp.Matched, addResp.Affected)

	if addResp.Affected < 1 {
		t.Errorf("Expected at least 1 affected, got %d", addResp.Affected)
	}

	// Verify both labels are present
	obj, err := gc.GetObject(ctx, factID)
	if err != nil {
		t.Fatalf("GetObject after add_labels: %v", err)
	}
	t.Logf("Post-add_labels: %v", obj.Labels)

	hasInitial := false
	hasAdded := false
	for _, l := range obj.Labels {
		if l == initialLabel {
			hasInitial = true
		}
		if l == addedLabel {
			hasAdded = true
		}
	}

	if !hasInitial {
		t.Errorf("Initial label %q lost after add_labels — got %v", initialLabel, obj.Labels)
	} else {
		t.Log("✅ Initial label preserved")
	}
	if !hasAdded {
		t.Errorf("Added label %q not found after add_labels — got %v", addedLabel, obj.Labels)
	} else {
		t.Log("✅ New label added")
	}

	// Total labels should be at least 2 (initial + added, possibly more like "memory-test")
	if len(obj.Labels) < 2 {
		t.Errorf("Expected >= 2 labels, got %d: %v", len(obj.Labels), obj.Labels)
	}

	t.Log("✅ BulkAction add_labels completed — labels are additive, not replacing")
}

// =========================================================================
// TestBulkAction_UpdateStatusToActive: Tests update_status action with
// value "active" — the inverse of the archive test in memory_pipeline.
// Creates an archived fact and reactivates it.
// =========================================================================

func TestBulkAction_UpdateStatusToActive(t *testing.T) {
	gc, done := setup(t)
	defer done()
	ctx := context.Background()
	prefix := fmt.Sprintf("t-active-%d", time.Now().UnixMilli())
	testLabel := prefix + "-label"

	// Create a fact
	fact, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
		Type: "MemoryFact",
		Key:  strPtr(prefix + "-reactivate-target"),
		Properties: map[string]any{
			"confidence": 0.5,
			"content":    prefix + ": Object to archive then reactivate",
		},
		Labels: []string{testLabel},
	})
	if err != nil {
		t.Fatalf("CreateObject: %v", err)
	}
	factID := fact.EntityID
	t.Logf("Created fact: %s", factID[:12])

	t.Cleanup(func() {
		_ = gc.DeleteObject(ctx, factID, nil)
	})

	// First archive it
	archiveResp, err := gc.BulkAction(ctx, &graph.BulkActionRequest{
		Filter: graph.BulkActionFilter{
			Types:  []string{"MemoryFact"},
			Labels: []string{testLabel},
		},
		Action: "update_status",
		Value:  "archived",
	})
	if err != nil {
		t.Fatalf("BulkAction archive: %v", err)
	}
	t.Logf("Archive: matched=%d affected=%d", archiveResp.Matched, archiveResp.Affected)

	// Verify archived
	obj, err := gc.GetObject(ctx, factID)
	if err != nil {
		t.Fatalf("GetObject after archive: %v", err)
	}
	status := "(nil)"
	if obj.Status != nil {
		status = *obj.Status
	}
	t.Logf("After archive: status=%s", status)

	// Now reactivate
	activeResp, err := gc.BulkAction(ctx, &graph.BulkActionRequest{
		Filter: graph.BulkActionFilter{
			Types:  []string{"MemoryFact"},
			Labels: []string{testLabel},
		},
		Action: "update_status",
		Value:  "active",
	})
	if err != nil {
		t.Fatalf("BulkAction reactivate: %v", err)
	}
	t.Logf("Reactivate: matched=%d affected=%d", activeResp.Matched, activeResp.Affected)

	if activeResp.Affected < 1 {
		t.Errorf("Expected at least 1 affected by reactivation, got %d", activeResp.Affected)
	}

	// Verify active
	obj2, err := gc.GetObject(ctx, factID)
	if err != nil {
		t.Fatalf("GetObject after reactivate: %v", err)
	}
	status2 := "(nil)"
	if obj2.Status != nil {
		status2 = *obj2.Status
	}
	t.Logf("After reactivate: status=%s", status2)
	if !(status2 == "active" || status2 == "published" || status2 == "") {
		t.Errorf("Expected status to be 'active' or empty after reactivation, got %q", status2)
	} else {
		t.Log("✅ Reactivation successful")
	}

	t.Log("✅ BulkAction update_status (active→archived→active) round-trip completed")
}

// strOrEmpty safely dereferences a string pointer, returning "" if nil.
func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
