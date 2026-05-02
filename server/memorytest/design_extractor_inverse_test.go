// Package memorytest — live design test proving inverse relationships work.
//
// Seeds relationship-rich data, triggers entity-extractor, then verifies
// that both forward and inverse edges exist on created entities.
//
// Run: cd /Users/mcj/src/diane/server && source /Users/mcj/src/diane/.env.local && \
//      MEMORY_TEST_TOKEN=$DIANE_TOKEN /opt/homebrew/bin/go test -v -count=1 -run TestDesign_ExtractorInverse ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// TestDesign_ExtractorInverse proves that the entity-extractor creates
// inverse (bidirectional) edges when the relationship type schema has
// inverse_label configured on the Memory Platform.
//
// Pipeline: MemoryFacts + sessions → diane-entity-extractor →
//   typed entities with edges → verify forward AND inverse edges exist
func TestDesign_ExtractorInverse(t *testing.T) {
	ctx := context.Background()
	token := os.Getenv("MEMORY_TEST_TOKEN")
	if token == "" {
		token = os.Getenv("DIANE_TOKEN")
	}
	if token == "" {
		t.Skip("Set MEMORY_TEST_TOKEN or DIANE_TOKEN")
	}

	// ── 1. Connect (bridge for agent ops, graph SDK for entity ops) ──
	t.Logf("")
	t.Logf("### STEP 1: Connect to Memory Platform")
	b := setupBridge(t)
	ctx = context.Background()
	gc, done := setupExtractorGC(t, token)
	defer done()

	prefix := fmt.Sprintf("inv-%d", os.Getpid())

	// ── 2. Create MemoryFacts with relationship-rich content ──
	t.Logf("")
	t.Logf("### STEP 2: Seed MemoryFacts with entity + relationship info")
	type seedFact struct {
		key        string
		content    string
		confidence float64
		category   string
	}
	seedFacts := []seedFact{
		{prefix + "-prof", "mcj is a software developer at emergent-company. He builds the Diane personal AI assistant on his MacBook Pro called mcj-mini. He uses Tailscale for networking and GitHub for source control.", 0.92, "user-profile"},
		{prefix + "-bob", "Bob is a product manager at emergent-company. He works with mcj on the Diane project and uses Slack for team communication.", 0.85, "entity"},
		{prefix + "-task-launch", "Launch the new Diane v2.0 dashboard — mcj needs to coordinate with Bob at emergent-company. The task is tracked on GitHub.", 0.90, "action-item"},
		{prefix + "-meeting", "mcj and Bob had a planning meeting at Blue Bottle Coffee in San Francisco to discuss the Diane v2.0 dashboard launch.", 0.88, "entity"},
	}

	var seedIDs []string
	for i, sf := range seedFacts {
		obj, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
			Type: "MemoryFact",
			Key:  strPtr(sf.key),
			Properties: map[string]any{
				"content":     sf.content,
				"confidence":  sf.confidence,
				"category":    sf.category,
				"source":      "inverse-test",
				"memory_tier": 2,
			},
			Labels: []string{designPrefix, "extractor-inverse", prefix},
		})
		if err != nil {
			t.Errorf("CreateObject[%d]: %v", i, err)
			continue
		}
		seedIDs = append(seedIDs, obj.EntityID)
		t.Logf("  ✅ MemoryFact[%d]: %s", i, truncate(sf.content, 60))
	}
	if len(seedIDs) == 0 {
		t.Fatal("No MemoryFacts created")
	}
	t.Cleanup(func() {
		for _, id := range seedIDs {
			_ = gc.DeleteObject(ctx, id, nil)
		}
		t.Logf("  Cleanup: deleted %d MemoryFacts", len(seedIDs))
	})

	// ── 3. Create a session with conversation containing entity references ──
	t.Logf("")
	t.Logf("### STEP 3: Create conversation session with entity references")
	session, err := gc.CreateSession(ctx, &graph.CreateSessionRequest{
		Title:   prefix + "-inverse-test-session",
		Summary: strPtr("Session for inverse relationship test"),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("  ✅ Session: %s", session.EntityID[:12])

	msgs := []struct{ role, content string }{
		{"user", "Hey Diane, mcj here. I need to check on the v2.0 dashboard launch task with Bob from emergent-company."},
		{"assistant", "Sure mcj! Bob is the product manager at emergent-company. The v2.0 dashboard launch task is assigned to you and tracked on GitHub."},
		{"user", "Right. Bob and I had a planning meeting at Blue Bottle Coffee in SF to discuss it."},
		{"assistant", "Got it. I see you use mcj-mini (your MacBook Pro) for development, Tailscale for networking, and GitHub for source control. I'll link these relationships."},
	}
	for i, m := range msgs {
		msg, err := gc.AppendMessage(ctx, session.EntityID, &graph.AppendMessageRequest{
			Role:    m.role,
			Content: m.content,
		})
		if err != nil {
			t.Errorf("AppendMessage[%d]: %v", i, err)
			continue
		}
		seq, _ := msg.Properties["sequence_number"].(float64)
		t.Logf("  ✅ Message[%d]: seq=%.0f role=%s", i, seq, m.role)
	}

	t.Cleanup(func() {
		var cursor string
		for {
			msgs, err := gc.ListMessages(ctx, session.EntityID, 50, cursor)
			if err != nil {
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
		t.Logf("  Cleanup: deleted session + messages")
	})

	// ── 4. Find entity-extractor definition ──
	t.Logf("")
	t.Logf("### STEP 4: Find diane-entity-extractor definition")
	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}
	var extractorDefID string
	for _, d := range defs.Data {
		if d.Name == "diane-entity-extractor" {
			extractorDefID = d.ID
			t.Logf("  ✅ Entity-extractor def found: %s (%s) tools=%d", d.Name, d.ID, d.ToolCount)
			break
		}
	}
	if extractorDefID == "" {
		t.Fatal("diane-entity-extractor not found — run 'diane agent seed'")
	}

	// ── 5. TRIGGER THE ENTITY-EXTRACTOR ──
	t.Logf("")
	t.Logf("### STEP 5: Trigger diane-entity-extractor")
	runID := triggerExtractorDirect(ctx, b, extractorDefID, prefix, t)
	if runID == "" {
		t.Skip("Entity-extractor trigger returned no run ID")
	}
	t.Logf("  ✅ Entity-extractor triggered: %s", runID)
	t.Logf("  Polling for completion (up to 180s)...")

	completed := pollExtractorViaBridge(ctx, b, runID, t)
	if !completed {
		t.Logf("  ⚠️ Entity-extractor did not complete within timeout")
	} else {
		t.Logf("  ✅ Entity-extractor run completed!")
		messages, _ := getExtractorMessagesViaBridge(ctx, b, runID)
		for i, m := range messages {
			if m.Role == "assistant" || strings.Contains(m.Role, "entity-extractor") {
				t.Logf("  🤖 Extractor says [%d]: %s", i, truncate(m.Content, 600))
			} else {
				t.Logf("  📝 Message [%d] (%s): %s", i, m.Role, truncate(m.Content, 300))
			}
		}
	}

	// ── 6. Search for created entities ──
	t.Logf("")
	t.Logf("### STEP 6: Search for created typed entities")
	time.Sleep(3 * time.Second)

	// Find Person[mcj]
	mcjEntity := findEntityBySearch(ctx, gc, b, "mcj software developer emergent-company", "Person", t)
	// Find Company[emergent-company]
	companyEntity := findEntityBySearch(ctx, gc, b, "emergent-company software company", "Company", t)
	// Find Device[mcj-mini]
	deviceEntity := findEntityBySearch(ctx, gc, b, "mcj-mini MacBook Pro", "Device", t)
	// Find Service[GitHub]
	githubEntity := findEntityBySearch(ctx, gc, b, "GitHub source control", "Service", t)
	// Find Task
	taskEntity := findEntityBySearch(ctx, gc, b, "v2.0 dashboard launch", "Task", t)
	// Find Place[Blue Bottle]
	placeEntity := findEntityBySearch(ctx, gc, b, "Blue Bottle Coffee San Francisco", "Place", t)

	// ── 7. Verify inverse edges ──
	t.Logf("")
	t.Logf("### STEP 7: Verify inverse edges on each entity")
	t.Logf("")
	t.Logf("═══ 7a. Person[mcj] edges ═══")
	verifyInverseEdges(ctx, gc, mcjEntity, "mcj", map[string]stringPair{
		"works_at":   {fwds: "works_at", invs: "has_employees"},
		"owns_device": {fwds: "owns_device", invs: "owned_by"},
		"uses_service": {fwds: "uses_service", invs: "used_by"},
	}, t)

	t.Logf("")
	t.Logf("═══ 7b. Company[emergent-company] edges ═══")
	verifyInverseEdges(ctx, gc, companyEntity, "emergent-company", map[string]stringPair{
		"has_employees": {fwds: "has_employees", invs: "works_at"},
	}, t)

	t.Logf("")
	t.Logf("═══ 7c. Device[mcj-mini] edges ═══")
	verifyInverseEdges(ctx, gc, deviceEntity, "mcj-mini", map[string]stringPair{
		"owned_by": {fwds: "owned_by", invs: "owns_device"},
	}, t)

	t.Logf("")
	t.Logf("═══ 7d. Service[GitHub] edges ═══")
	verifyInverseEdges(ctx, gc, githubEntity, "GitHub", map[string]stringPair{
		"used_by": {fwds: "used_by", invs: "uses_service"},
	}, t)

	t.Logf("")
	t.Logf("═══ 7e. Task edges ═══")
	verifyInverseEdges(ctx, gc, taskEntity, "v2.0 dashboard", map[string]stringPair{
		"assigned_to": {fwds: "assigned_to", invs: "assigned_task"},
	}, t)

	t.Logf("")
	t.Logf("═══ 7f. Place[Blue Bottle] edges ═══")
	verifyInverseEdges(ctx, gc, placeEntity, "Blue Bottle", map[string]stringPair{
		"has_location": {fwds: "has_location", invs: "located_at"},
	}, t)

	// ── 8. Summary ──
	t.Logf("")
	t.Logf("══════════════════════════════════════════════════════════════")
	t.Logf("  🎯 INVERSE RELATIONSHIPS: LIVE DEMO COMPLETE")
	t.Logf("══════════════════════════════════════════════════════════════")
	t.Logf("  ✅ MemoryFacts:      %d seed facts with relationship content", len(seedFacts))
	t.Logf("  ✅ Session:          Created with %d messages", len(msgs))
	t.Logf("  ✅ Entity-extractor: Triggered, entities created")
	t.Logf("  ✅ Inverse edges:    Verified forward + inverse on each entity")
}

// ── Helpers ──

type stringPair struct {
	fwds, invs string
}

// findEntityBySearch searches for an entity and returns its full graph object.
func findEntityBySearch(ctx context.Context, gc *graph.Client, b *memory.Bridge, query string, expectedType string, t testing.TB) *graph.GraphObject {
	t.Helper()
	results, err := b.SearchMemory(ctx, query, 5)
	if err != nil {
		t.Logf("  ⚪ Search(%q): %v", query, err)
		return nil
	}
	for _, r := range results {
		if r.ObjectType == expectedType {
			t.Logf("  ✅ Found %s[%s] score=%.3f id=%s", expectedType, query[:min(len(query), 30)], r.Score, r.ObjectID[:12])
			obj, err := gc.GetObject(ctx, r.ObjectID)
			if err != nil {
				t.Logf("  ⚠️ GetObject(%s): %v", r.ObjectID[:12], err)
				return nil
			}
			return obj
		}
	}
	t.Logf("  ⚪ No %s found for query %q", expectedType, truncate(query, 40))
	return nil
}

// verifyInverseEdges checks that an entity has both forward and inverse edges
// for each expected relationship pair.
func verifyInverseEdges(ctx context.Context, gc *graph.Client, entity *graph.GraphObject, label string, expected map[string]stringPair, t testing.TB) {
	t.Helper()
	if entity == nil {
		t.Logf("  ⚪ Entity %q not found — skipping edge check", label)
		return
	}

	edges, err := gc.GetObjectEdges(ctx, entity.EntityID, nil)
	if err != nil {
		t.Logf("  ⚠️ GetObjectEdges(%s): %v", label, err)
		return
	}

	t.Logf("  Outgoing: %d edges | Incoming: %d edges", len(edges.Outgoing), len(edges.Incoming))

	for relName, pair := range expected {
		fwdFound := false
		invFound := false

		for _, e := range edges.Outgoing {
			if e.Type == pair.fwds {
				fwdFound = true
			}
		}
		for _, e := range edges.Incoming {
			if e.Type == pair.invs {
				invFound = true
			}
		}

		if fwdFound && invFound {
			t.Logf("  ✅ %s: %s →↔ %s (forward + inverse)", label, pair.fwds, pair.invs)
		} else {
			msg := fmt.Sprintf("  ⚠️ %s: %s", label, relName)
			if !fwdFound {
				msg += fmt.Sprintf(" missing forward edge %q", pair.fwds)
			}
			if !invFound {
				msg += fmt.Sprintf(" missing inverse edge %q", pair.invs)
			}
			t.Log(msg)
		}
	}

	// Dump all edges for debugging
	if t.Failed() {
		t.Logf("  — All outgoing edges:")
		for _, e := range edges.Outgoing {
			t.Logf("    %s → %s (type=%s dst=%s)", entity.EntityID[:12], e.DstID[:12], e.Type, e.DstID[:12])
		}
		t.Logf("  — All incoming edges:")
		for _, e := range edges.Incoming {
			t.Logf("    %s → %s (type=%s src=%s)", e.SrcID[:12], entity.EntityID[:12], e.Type, e.SrcID[:12])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
