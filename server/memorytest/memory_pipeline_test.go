// Package memorytest tests Diane's Tier 3 memory pipeline building blocks
// against the live Memory Platform at the SDK level.
//
// These tests exercise the same primitives that Diane's memory algorithm
// relies on: MemoryFact CRUD, bulk lifecycle (dreaming/decay pipeline),
// large sessions with pagination, and extracted-from relationships.
//
// Run: cd server && MEMORY_TEST_TOKEN=*** /usr/local/go/bin/go test -v -count=1 ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// =========================================================================
// Tier 3 Pipeline Test 1: MemoryFact CRUD Lifecycle
// =========================================================================
// Tests full create → read → update → verify cycle for MemoryFact objects,
// which are the core unit of Diane's declarative memory.

func TestMemoryPipeline_MemoryFactCRUD(t *testing.T) {
	gc, done := setup(t)
	defer done()
	ctx := context.Background()
	prefix := fmt.Sprintf("t-fact-%d", os.Getpid())

	// R1: Create 3 MemoryFact objects with different confidence levels
	confidences := []float64{0.15, 0.55, 0.92}
	contents := []string{
		"User prefers dark mode in all applications",
		"User's timezone is UTC+2 (Eastern European)",
		"User has admin access to the analytics dashboard",
	}
	var factIDs []string

	for i := range confidences {
		obj, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
			Type: "MemoryFact",
			Key:  strPtr(fmt.Sprintf("%s-fact-%d", prefix, i)),
			Properties: map[string]any{
				"confidence": confidences[i],
				"content":    contents[i],
			},
			Labels: []string{"memory-test", "memory-fact-crud"},
		})
		if err != nil {
			t.Fatalf("CreateObject[%d]: %v", i, err)
		}
		if obj.EntityID == "" {
			t.Fatalf("CreateObject[%d] returned empty EntityID", i)
		}
		factIDs = append(factIDs, obj.EntityID)

		// Verify confidence value was stored correctly
		gotConf, _ := obj.Properties["confidence"].(float64)
		if gotConf != confidences[i] {
			t.Errorf("fact[%d] confidence=%.2f, want %.2f", i, gotConf, confidences[i])
		}

		t.Logf("  R1a ✅ Created MemoryFact[%d]: id=%s confidence=%.2f",
			i, obj.EntityID[:12], confidences[i])
	}

	// Cleanup all facts at end
	t.Cleanup(func() {
		for _, id := range factIDs {
			_ = gc.DeleteObject(ctx, id, nil)
		}
		t.Logf("  R1z ✅ Cleanup: deleted %d MemoryFacts", len(factIDs))
	})

	// R1b: GetObject each one to verify properties
	for i, id := range factIDs {
		obj, err := gc.GetObject(ctx, id)
		if err != nil {
			t.Fatalf("GetObject[%d] %s: %v", i, id, err)
		}
		if obj.EntityID != id {
			t.Errorf("GetObject[%d] EntityID mismatch: %s vs %s", i, obj.EntityID, id)
		}
		gotConf, _ := obj.Properties["confidence"].(float64)
		gotContent, _ := obj.Properties["content"].(string)
		if gotConf != confidences[i] {
			t.Errorf("GetObject[%d] confidence=%.2f, want %.2f", i, gotConf, confidences[i])
		}
		if gotContent != contents[i] {
			t.Errorf("GetObject[%d] content=%q, want %q", i, gotContent, contents[i])
		}
		t.Logf("  R1b ✅ GetObject[%d]: confidence=%.2f content=%.40s...",
			i, gotConf, gotContent)
	}

	// R1c: Update one — change confidence
	newConf := 0.99
	updated, err := gc.UpdateObject(ctx, factIDs[0], &graph.UpdateObjectRequest{
		Properties: map[string]any{
			"confidence": newConf,
		},
	})
	if err != nil {
		t.Fatalf("UpdateObject[0]: %v", err)
	}
	t.Logf("  R1c ✅ UpdateObject[0]: new EntityID=%s version=%d",
		updated.EntityID[:12], updated.Version)

	// R1d: Verify update persisted via GetObject
	// Use canonical EntityID (stable) for re-fetch
	verified, err := gc.GetObject(ctx, factIDs[0])
	if err != nil {
		t.Fatalf("GetObject after update: %v", err)
	}
	gotConf, _ := verified.Properties["confidence"].(float64)
	if gotConf != newConf {
		t.Errorf("Verified confidence after update=%.2f, want %.2f", gotConf, newConf)
	}
	t.Logf("  R1d ✅ Update verified: confidence=%.2f (was %.2f)", gotConf, confidences[0])
}

// =========================================================================
// Tier 3 Pipeline Test 2: Bulk Action / Dreaming Decay Pipeline
// =========================================================================
// Simulates the dreaming decay pipeline where low-confidence MemoryFacts
// are batched and archived, while high-confidence facts are promoted.

func TestMemoryPipeline_BulkActionDecay(t *testing.T) {
	gc, done := setup(t)
	defer done()
	ctx := context.Background()
	prefix := fmt.Sprintf("t-decay-%d", os.Getpid())
	decayLabels := []string{"test-decay", prefix}

	// R2a: Create 3 MemoryFact objects with confidences [0.9, 0.5, 0.1]
	confidences := []float64{0.9, 0.5, 0.1}
	contents := []string{
		"High-confidence fact about user preferences",
		"Medium-confidence fact from partial observation",
		"Low-confidence speculation that should be archived",
	}
	var factIDs []string

	for i := range confidences {
		obj, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
			Type: "MemoryFact",
			Key:  strPtr(fmt.Sprintf("%s-fact-%d", prefix, i)),
			Properties: map[string]any{
				"confidence": confidences[i],
				"content":    contents[i],
			},
			Labels: decayLabels,
		})
		if err != nil {
			t.Fatalf("R2a ❌ CreateObject[%d]: %v", i, err)
		}
		factIDs = append(factIDs, obj.EntityID)
	}
	t.Logf("  R2a ✅ Created %d MemoryFacts with labels=%v confidences=%v",
		len(confidences), decayLabels, confidences)

	// Cleanup all at end
	t.Cleanup(func() {
		for _, id := range factIDs {
			_ = gc.DeleteObject(ctx, id, nil)
		}
		t.Logf("  R2z ✅ Cleanup: deleted %d MemoryFacts", len(factIDs))
	})

	// R2b: Dry-run — preview which objects match confidence < 0.3
	// Only the 0.1 fact should match
	dryRun, err := gc.BulkAction(ctx, &graph.BulkActionRequest{
		Filter: graph.BulkActionFilter{
			Types:  []string{"MemoryFact"},
			Labels: decayLabels,
			PropertyFilters: []graph.PropertyFilter{
				{Path: "confidence", Op: "lt", Value: 0.3},
			},
		},
		Action: "update_status",
		Value:  "archived",
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("R2b ❌ BulkAction dry-run: %v", err)
	}
	if dryRun.Matched < 1 {
		t.Errorf("R2b ❌ dry-run matched=%d (want >= 1 low-confidence fact)", dryRun.Matched)
	}
	if !dryRun.DryRun {
		t.Error("R2b ❌ DryRun flag not set in response")
	}
	t.Logf("  R2b ✅ BulkAction dry-run (confidence<0.3 → archive): matched=%d", dryRun.Matched)

	// R2c: Execute archive for real
	result, err := gc.BulkAction(ctx, &graph.BulkActionRequest{
		Filter: graph.BulkActionFilter{
			Types:  []string{"MemoryFact"},
			Labels: decayLabels,
			PropertyFilters: []graph.PropertyFilter{
				{Path: "confidence", Op: "lt", Value: 0.3},
			},
		},
		Action: "update_status",
		Value:  "archived",
	})
	if err != nil {
		t.Fatalf("R2c ❌ BulkAction execute: %v", err)
	}
	if result.Affected < 1 {
		t.Errorf("R2c ❌ bulk-archive affected=%d (want >= 1)", result.Affected)
	}
	t.Logf("  R2c ✅ BulkAction archive: matched=%d affected=%d errors=%d",
		result.Matched, result.Affected, result.Errors)

	// R2d: Verify the low-confidence fact is now archived
	for i, id := range factIDs {
		obj, err := gc.GetObject(ctx, id)
		if err != nil {
			t.Errorf("R2d ❌ GetObject[%d]: %v", i, err)
			continue
		}
		status := "(nil)"
		if obj.Status != nil {
			status = *obj.Status
		}
		conf, _ := obj.Properties["confidence"].(float64)
		t.Logf("  R2d ✅ fact[%d] confidence=%.1f status=%s", i, conf, status)

		if conf < 0.3 && status != "archived" {
			t.Errorf("R2d ❌ fact[%d] confidence=%.1f should be archived, got %q",
				i, conf, status)
		}
	}

	// R2e: Run BulkAction set_labels as another action type — promote high-confidence
	// Add a "promoted" label to facts with confidence >= 0.5
	promotedLabels := []string{"promoted", "high-confidence"}
	promoteResult, err := gc.BulkAction(ctx, &graph.BulkActionRequest{
		Filter: graph.BulkActionFilter{
			Types:  []string{"MemoryFact"},
			Labels: decayLabels,
			PropertyFilters: []graph.PropertyFilter{
				{Path: "confidence", Op: "gte", Value: 0.5},
			},
		},
		Action: "set_labels",
		Labels: promotedLabels,
	})
	if err != nil {
		t.Fatalf("R2e ❌ BulkAction set_labels: %v", err)
	}
	t.Logf("  R2e ✅ BulkAction set_labels→%v: matched=%d affected=%d",
		promotedLabels, promoteResult.Matched, promoteResult.Affected)

	// R2f: Verify labels were applied
	highFact, err := gc.GetObject(ctx, factIDs[0]) // 0.9 confidence
	if err != nil {
		t.Fatalf("R2f ❌ GetObject high-fact: %v", err)
	}
	hasPromoted := false
	for _, l := range highFact.Labels {
		if l == "promoted" {
			hasPromoted = true
			break
		}
	}
	if !hasPromoted {
		t.Errorf("R2f ❌ high-confidence fact missing 'promoted' label, got %v", highFact.Labels)
	} else {
		t.Logf("  R2f ✅ High-confidence fact labels=%v (includes 'promoted')", highFact.Labels)
	}

	// R2g: Verify low-confidence fact does NOT have promoted label
	lowFact, err := gc.GetObject(ctx, factIDs[2]) // 0.1 confidence
	if err != nil {
		t.Fatalf("R2g ❌ GetObject low-fact: %v", err)
	}
	for _, l := range lowFact.Labels {
		if l == "promoted" {
			t.Error("R2g ❌ Low-confidence fact should NOT have 'promoted' label")
		}
	}
	t.Logf("  R2g ✅ Low-confidence fact labels=%v (no 'promoted')", lowFact.Labels)

	// R2h: Restore archived fact status so normal cleanup works
	// (DeleteObject on an archived object may produce an error)
	for _, id := range factIDs {
		obj, err := gc.GetObject(ctx, id)
		if err == nil && obj.Status != nil && *obj.Status == "archived" {
			_, _ = gc.UpdateObject(ctx, id, &graph.UpdateObjectRequest{
				Status: strPtr("active"),
			})
		}
	}
	t.Logf("  R2h ✅ Restored archived facts to active for cleanup")
}

// =========================================================================
// Tier 3 Pipeline Test 3: Large Session (50 messages)
// =========================================================================
// Validates that sessions can hold 50 messages with correct sequencing
// and that paginated ListMessages retrieves all of them.

func TestMemoryPipeline_LargeSession(t *testing.T) {
	gc, done := setup(t)
	defer done()
	ctx := context.Background()
	prefix := fmt.Sprintf("t-large-%d", os.Getpid())
	const msgCount = 50

	// R3a: Create a session
	session, err := gc.CreateSession(ctx, &graph.CreateSessionRequest{
		Title:   prefix + "-large-session",
		Summary: strPtr("50-message session for pipeline test"),
	})
	if err != nil {
		t.Fatalf("R3a ❌ CreateSession: %v", err)
	}
	if session.EntityID == "" {
		t.Fatal("R3a ❌ CreateSession returned empty EntityID")
	}
	t.Logf("  R3a ✅ Session created: %s (%s)", session.EntityID[:12], session.Properties["title"])

	// Cleanup session and all messages at end
	t.Cleanup(func() {
		// List all messages first to clean them up
		var cursor string
		for {
			msgs, err := gc.ListMessages(ctx, session.EntityID, 100, cursor)
			if err != nil {
				t.Logf("  R3z ⚠️ ListMessages during cleanup: %v", err)
				break
			}
			for _, m := range msgs.Items {
				_ = gc.DeleteObject(ctx, m.EntityID, nil)
			}
			if msgs.NextCursor == nil || *msgs.NextCursor == "" {
				break
			}
			cursor = *msgs.NextCursor
		}
		_ = gc.DeleteObject(ctx, session.EntityID, nil)
		t.Logf("  R3z ✅ Cleanup: deleted session + %d messages", msgCount)
	})

	// R3b: Append 50 messages with alternating roles
	var msgIDs []string
	for i := 1; i <= msgCount; i++ {
		role := "user"
		content := fmt.Sprintf("%s: message %d with some padding content for uniqueness", prefix, i)
		if i%2 == 0 {
			role = "assistant"
			content = fmt.Sprintf("%s: This is assistant response number %d with context", prefix, i)
		}

		msg, err := gc.AppendMessage(ctx, session.EntityID, &graph.AppendMessageRequest{
			Role:    role,
			Content: content,
		})
		if err != nil {
			t.Fatalf("R3b ❌ AppendMessage[%d]: %v", i, err)
		}
		msgIDs = append(msgIDs, msg.EntityID)

		// Verify sequence number
		seq, _ := msg.Properties["sequence_number"].(float64)
		if int(seq) != i {
			t.Errorf("R3b ❌ Message[%d] seq=%.0f, want %d", i, seq, i)
		}
	}
	t.Logf("  R3b ✅ Appended %d messages, sequence numbers verified (1..%d)", msgCount, msgCount)

	// R3c: Verify each message's sequence via GetObject
	for i, id := range msgIDs {
		obj, err := gc.GetObject(ctx, id)
		if err != nil {
			t.Errorf("R3c ❌ GetObject msg[%d] %s: %v", i, id, err)
			continue
		}
		seq, _ := obj.Properties["sequence_number"].(float64)
		if int(seq) != i+1 {
			t.Errorf("R3c ❌ Message[%d] seq=%.0f, want %d", i, seq, i+1)
		}
	}
	t.Logf("  R3c ✅ All %d messages verified via GetObject with correct sequence numbers", msgCount)

	// R3d: Verify ListMessages returns all 50 with pagination
	var allFetched []*graph.GraphObject
	var cursor string
	page := 0
	for {
		page++
		resp, err := gc.ListMessages(ctx, session.EntityID, 10, cursor)
		if err != nil {
			t.Fatalf("R3d ❌ ListMessages page %d: %v", page, err)
		}
		allFetched = append(allFetched, resp.Items...)
		t.Logf("  R3d ✅ Page %d: fetched %d messages (total so far: %d)",
			page, len(resp.Items), len(allFetched))

		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}

	if len(allFetched) != msgCount {
		t.Errorf("R3d ❌ ListMessages returned %d total, want %d", len(allFetched), msgCount)
	} else {
		t.Logf("  R3d ✅ ListMessages pagination returned all %d messages across %d pages",
			msgCount, page)
	}

	// R3e: Verify ordering by sequence_number
	for i, m := range allFetched {
		seq, _ := m.Properties["sequence_number"].(float64)
		if int(seq) != i+1 {
			t.Errorf("R3e ❌ Message[%d] in list has seq=%.0f, want %d", i, seq, i+1)
		}
	}
	t.Logf("  R3e ✅ All %d messages in correct order (sequence 1..%d)", len(allFetched), msgCount)
}

// =========================================================================
// Tier 3 Pipeline Test 4: Extracted-From Relationships
// =========================================================================
// Tests wiring MemoryFacts to a session via extracted_from relationships,
// which is how Diane's memory algorithm links extracted knowledge back
// to the conversation it came from.

func TestMemoryPipeline_ExtractedFromRelationships(t *testing.T) {
	gc, done := setup(t)
	defer done()
	ctx := context.Background()
	prefix := fmt.Sprintf("t-extr-%d", os.Getpid())

	// R4a: Create a session with a few messages
	session, err := gc.CreateSession(ctx, &graph.CreateSessionRequest{
		Title:   prefix + "-extracted-from",
		Summary: strPtr("Session for extracted_from relationship test"),
	})
	if err != nil {
		t.Fatalf("R4a ❌ CreateSession: %v", err)
	}
	t.Cleanup(func() {
		_ = gc.DeleteObject(ctx, session.EntityID, nil)
	})
	t.Logf("  R4a ✅ Session created: %s", session.EntityID[:12])

	// Append 2 messages
	msgContents := []string{
		prefix + ": Tell me about the user's preferences",
		prefix + ": The user prefers dark mode and has admin access",
	}
	var msgIDs []string
	for i, content := range msgContents {
		role := "user"
		if i == 1 {
			role = "assistant"
		}
		msg, err := gc.AppendMessage(ctx, session.EntityID, &graph.AppendMessageRequest{
			Role:    role,
			Content: content,
		})
		if err != nil {
			t.Fatalf("R4a ❌ AppendMessage[%d]: %v", i, err)
		}
		msgIDs = append(msgIDs, msg.EntityID)
		t.Logf("  R4a ✅ Message[%d] created: %s", i, msg.EntityID[:12])
	}

	t.Cleanup(func() {
		for _, id := range msgIDs {
			_ = gc.DeleteObject(ctx, id, nil)
		}
	})

	// R4b: Create 2 MemoryFact objects
	factContents := []string{
		"User prefers dark mode in all applications",
		"User has admin access to the analytics dashboard",
	}
	var factIDs []string
	for i, content := range factContents {
		fact, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
			Type: "MemoryFact",
			Key:  strPtr(fmt.Sprintf("%s-fact-%d", prefix, i)),
			Properties: map[string]any{
				"confidence": 0.85,
				"content":    content,
				"source":     "conversation_extraction",
			},
			Labels: []string{"extracted", "memory-test"},
		})
		if err != nil {
			t.Fatalf("R4b ❌ CreateObject fact[%d]: %v", i, err)
		}
		factIDs = append(factIDs, fact.EntityID)
		t.Logf("  R4b ✅ MemoryFact[%d] created: %s — %s", i, fact.EntityID[:12], content)
	}

	t.Cleanup(func() {
		for _, id := range factIDs {
			_ = gc.DeleteObject(ctx, id, nil)
		}
	})

	// R4c: Wire extracted_from relationships from each fact to the session
	var relIDs []string
	for i, factID := range factIDs {
		rel, err := gc.CreateRelationship(ctx, &graph.CreateRelationshipRequest{
			Type:  "extracted_from",
			SrcID: factID,           // fact is the source
			DstID: session.EntityID, // session is the destination
			Properties: map[string]any{
				"source_message_id": msgIDs[i%len(msgIDs)],
				"confidence":        0.85,
			},
		})
		if err != nil {
			t.Fatalf("R4c ❌ CreateRelationship extracted_from[%d]: %v", i, err)
		}
		relIDs = append(relIDs, rel.EntityID)
		t.Logf("  R4c ✅ extracted_from[%d]: fact=%s -> session=%s",
			i, factID[:12], session.EntityID[:12])
	}

	t.Cleanup(func() {
		for _, id := range relIDs {
			_ = gc.DeleteRelationship(ctx, id)
		}
	})

	// R4d: Verify edges from fact side (outgoing extracted_from)
	for i, factID := range factIDs {
		edges, err := gc.GetObjectEdges(ctx, factID, &graph.GetObjectEdgesOptions{
			Type: "extracted_from",
		})
		if err != nil {
			t.Fatalf("R4d ❌ GetObjectEdges fact[%d]: %v", i, err)
		}
		if len(edges.Outgoing) == 0 {
			t.Errorf("R4d ❌ fact[%d] has no outgoing extracted_from edges", i)
		} else {
			t.Logf("  R4d ✅ fact[%d] has %d outgoing extracted_from edges",
				i, len(edges.Outgoing))
			for _, e := range edges.Outgoing {
				if e.DstID != session.EntityID {
					t.Errorf("R4d ❌ fact[%d] edge dst=%s, want session=%s",
						i, e.DstID[:12], session.EntityID[:12])
				}
			}
		}
	}

	// R4e: Verify edges from session side (incoming extracted_from)
	sessionEdges, err := gc.GetObjectEdges(ctx, session.EntityID, &graph.GetObjectEdgesOptions{
		Type: "extracted_from",
	})
	if err != nil {
		t.Fatalf("R4e ❌ GetObjectEdges session: %v", err)
	}
	if len(sessionEdges.Incoming) == 0 {
		t.Errorf("R4e ❌ session has no incoming extracted_from edges")
	} else {
		t.Logf("  R4e ✅ session has %d incoming extracted_from edges", len(sessionEdges.Incoming))
		for _, e := range sessionEdges.Incoming {
			t.Logf("       ← fact %s (type=%s)", e.SrcID[:12], e.Type)
		}
	}

	// R4f: Verify relationship properties are accessible
	for i, relID := range relIDs {
		rel, err := gc.GetRelationship(ctx, relID)
		if err != nil {
			t.Fatalf("R4f ❌ GetRelationship[%d]: %v", i, err)
		}
		conf, hasConf := rel.Properties["confidence"]
		srcMsgID, hasSrcMsg := rel.Properties["source_message_id"]
		t.Logf("  R4f ✅ Relationship[%d]: type=%s confidence=%v source_message_id=%v",
			i, rel.Type, conf, srcMsgID)
		if !hasConf {
			t.Errorf("R4f ❌ Relationship[%d] missing 'confidence' property", i)
		}
		if !hasSrcMsg {
			t.Errorf("R4f ❌ Relationship[%d] missing 'source_message_id' property", i)
		}
	}

	t.Logf("  R4z ✅ Extracted-from relationships fully verified: %d facts → session", len(factIDs))
}
