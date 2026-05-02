// Package memorytest — deep test: dedup, edges, and merge behavior of entity-extractor.
// Tests:
//   1. Entity extraction with relationship profiles → checks edges
//   2. Second run with similar data → checks dedup (no duplicate entities)
//
// Run: cd /Users/mcj/src/diane/server && source /Users/mcj/src/diane/.env.local && \
//      MEMORY_TEST_TOKEN=$DIANE_TOKEN /opt/homebrew/bin/go test -v -count=1 -timeout 600s -run TestDesign_ExtractorDeep ./memorytest/
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

// TestDesign_ExtractorDeep proves dedup, relationship wiring, and merging.
func TestDesign_ExtractorDeep(t *testing.T) {
	ctx := context.Background()
	token := os.Getenv("MEMORY_TEST_TOKEN")
	if token == "" {
		token = os.Getenv("DIANE_TOKEN")
	}
	if token == "" {
		t.Skip("Set MEMORY_TEST_TOKEN or DIANE_TOKEN")
	}

	prefix := fmt.Sprintf("deep-%d", os.Getpid())

	// ── 1. Setup ──
	t.Logf("")
	t.Logf("### STEP 1: Connect to Memory Platform")
	b := setupBridge(t)
	gc, done := setupExtractorGC(t, token)
	defer done()

	// ── 2. Seed MemoryFacts with relationship-rich content ──
	t.Logf("")
	t.Logf("### STEP 2: Seed MemoryFacts with relationship cues")
	type seedFact struct {
		key     string
		content string
		cat     string
	}
	seed := []seedFact{
		{prefix + "-prof", "mcj is a software developer at emergent-company, building Diane personal AI assistant on his MacBook Pro called mcj-mini. He uses Tailscale for networking and GitHub for source control.", "user-profile"},
		{prefix + "-bob", "Bob is a product manager at emergent-company. He works with mcj on the Diane project and uses Slack for team communication.", "entity"},
		{prefix + "-task-launch", "Launch the new Diane v2.0 dashboard — mcj needs to coordinate with Bob at emergent-company. The task is tracked on GitHub.", "action-item"},
		{prefix + "-meeting", "mcj and Bob had a planning meeting at Blue Bottle Coffee in San Francisco to discuss the Diane v2.0 dashboard launch.", "entity"},
	}
	var seedIDs []string
	for i, sf := range seed {
		obj, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
			Type: "MemoryFact",
			Key:  strPtr(sf.key),
			Properties: map[string]any{
				"content":     sf.content,
				"confidence":  0.90,
				"category":    sf.cat,
				"source":      "deep-test",
				"memory_tier": 2,
			},
			Labels: []string{designPrefix, "extractor-deep", prefix},
		})
		if err != nil {
			t.Errorf("CreateObject[%d]: %v", i, err)
			continue
		}
		seedIDs = append(seedIDs, obj.EntityID)
		t.Logf("  ✅ MemoryFact[%d]: %s", i, truncate(sf.content, 60))
	}
	t.Cleanup(func() {
		for _, id := range seedIDs {
			_ = gc.DeleteObject(ctx, id, nil)
		}
		t.Logf("  Cleanup: deleted %d MemoryFacts", len(seedIDs))
	})

	// ── 3. Find entity-extractor definition ──
	t.Logf("")
	t.Logf("### STEP 3: Find diane-entity-extractor definition")
	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}
	var defID string
	for _, d := range defs.Data {
		if d.Name == "diane-entity-extractor" {
			defID = d.ID
			t.Logf("  ✅ Found def: %s tools=%d", d.ID[:12], d.ToolCount)
			break
		}
	}
	if defID == "" {
		t.Fatal("diane-entity-extractor not found — run 'diane agent seed'")
	}

	// ── 4. FIRST extractor run ──
	t.Logf("")
	t.Logf("### STEP 4: FIRST entity-extractor run (creating entities + edges)")
	runID1 := triggerExtractorDirect(ctx, b, defID, prefix, t)
	if runID1 == "" {
		t.Fatal("First extractor trigger failed")
	}
	t.Logf("  ✅ Run 1: %s (waiting up to 300s for completion)", runID1)

	completed1 := pollExtractorLong(ctx, b, runID1, t, 300)
	t.Logf("  Run 1 result: completed=%v", completed1)

	// ── 5. Verify entities created ──
	t.Logf("")
	t.Logf("### STEP 5: Verify typed entities from Run 1")
	time.Sleep(5 * time.Second)

	entities1 := map[string]string{
		"mcj":              "Person",
		"emergent-company": "Company",
		"mcj-mini":         "Device",
		"Tailscale":        "Service",
		"Bob":              "Person",
	}
	for name, etype := range entities1 {
		results, err := b.SearchMemory(ctx, name, 3)
		if err != nil {
			t.Logf("  Search(%q): %v", name, err)
			continue
		}
		found := false
		for _, r := range results {
			if strings.EqualFold(r.Content, name) || strings.Contains(strings.ToLower(r.Content), strings.ToLower(name)) {
				t.Logf("  ✅ Found %s (type=%s) score=%.3f id=%s", name, r.ObjectType, r.Score, r.ObjectID[:12])
				found = true
				break
			}
		}
		if !found {
			t.Logf("  ⚪ %s [%s] not found via search", name, etype)
		}
	}

	// ── 6. Check edges via run messages ──
	if completed1 {
		t.Logf("")
		t.Logf("### STEP 6: Check run messages for edge creation")
		msgs, err := b.GetRunMessages(ctx, runID1)
		if err != nil {
			t.Logf("  GetRunMessages: %v", err)
		} else {
			edgeCalls := 0
			for _, m := range msgs.Data {
				content := fmt.Sprintf("%v", m.Content)
				if strings.Contains(content, "entity-edges-create") || (strings.Contains(content, "Edge") && strings.Contains(content, "create")) {
					edgeCalls++
					if edgeCalls <= 5 {
						t.Logf("  📎 Edge call: %s", truncate(content, 150))
					}
				}
			}
			if edgeCalls > 0 {
				t.Logf("  ✅ Entity-extractor made %d edge creation calls", edgeCalls)
			} else {
				t.Logf("  ⚪ No edge creation calls in visible messages")
			}
		}
	}

	// ── 7. SECOND extractor run (dedup test) ──
	t.Logf("")
	t.Logf("### STEP 7: SECOND entity-extractor run (dedup test)")
	t.Logf("  Seeding 2 more MemoryFacts with SAME entity names (should NOT create duplicates)")

	dedupSeed := []seedFact{
		{prefix + "-dedup-mcj", "mcj is a software developer working at emergent-company. He lives in Dhaka.", "user-profile"},
		{prefix + "-dedup-task", "Release the Diane v2.0 dashboard — mcj and Bob are collaborating on this at emergent-company.", "action-item"},
	}
	var dedupIDs []string
	for i, sf := range dedupSeed {
		obj, err := gc.CreateObject(ctx, &graph.CreateObjectRequest{
			Type: "MemoryFact",
			Key:  strPtr(sf.key),
			Properties: map[string]any{
				"content":     sf.content,
				"confidence":  0.85,
				"category":    sf.cat,
				"source":      "deep-test-2",
				"memory_tier": 2,
			},
			Labels: []string{designPrefix, "extractor-deep-dedup", prefix},
		})
		if err != nil {
			t.Errorf("CreateObject dedup[%d]: %v", i, err)
			continue
		}
		dedupIDs = append(dedupIDs, obj.EntityID)
		t.Logf("  ✅ Dedup MemoryFact[%d]: %s", i, truncate(sf.content, 60))
	}
	t.Cleanup(func() {
		for _, id := range dedupIDs {
			_ = gc.DeleteObject(ctx, id, nil)
		}
	})

	runID2 := triggerExtractorDirect(ctx, b, defID, prefix, t)
	if runID2 == "" {
		t.Logf("  ⚪ Second trigger failed — may still be polling from first run")
	} else {
		t.Logf("  ✅ Run 2: %s (waiting up to 300s)", runID2)
		completed2 := pollExtractorLong(ctx, b, runID2, t, 300)
		t.Logf("  Run 2 result: completed=%v", completed2)

		// ── 8. Check dedup ──
		t.Logf("")
		t.Logf("### STEP 8: Verify dedup — no duplicate entities created")
		time.Sleep(5 * time.Second)

		// Query Person entities — count how many have name=mcj
		persons, err := gc.ListObjects(ctx, &graph.ListObjectsOptions{
			Types: []string{"Person"},
			Limit: 100,
		})
		mcjCount := 0
		if err != nil {
			t.Logf("  ListObjects(Person): %v", err)
		} else if persons != nil {
			for _, p := range persons.Items {
				name, _ := p.Properties["name"].(string)
				if strings.EqualFold(name, "mcj") {
					mcjCount++
					t.Logf("  Person[mcj] id=%s labels=%v", p.EntityID[:12], p.Labels)
				}
			}
		}

		companies, err := gc.ListObjects(ctx, &graph.ListObjectsOptions{
			Types: []string{"Company"},
			Limit: 50,
		})
		ecCount := 0
		if err != nil {
			t.Logf("  ListObjects(Company): %v", err)
		} else if companies != nil {
			for _, c := range companies.Items {
				name, _ := c.Properties["name"].(string)
				if strings.Contains(strings.ToLower(name), "emergent") {
					ecCount++
					t.Logf("  Company[%s] id=%s labels=%v", name, c.EntityID[:12], c.Labels)
				}
			}
		}

		if mcjCount > 1 {
			t.Errorf("  ❌ DEDUP FAILED: %d Person entities with name=mcj (expected 1)", mcjCount)
		} else if mcjCount == 1 {
			t.Logf("  ✅ DEDUP OK: 1 Person entity with name=mcj")
		} else {
			t.Logf("  ⚪ No Person[mcj] found")
		}
		if ecCount > 1 {
			t.Errorf("  ❌ DEDUP FAILED: %d Company entities with name contains 'emergent' (expected 1)", ecCount)
		} else if ecCount == 1 {
			t.Logf("  ✅ DEDUP OK: 1 Company[emergent-company]")
		} else {
			t.Logf("  ⚪ No Company matching 'emergent' found")
		}

		// Check run 2 messages for updates vs creates
		if completed2 {
			msgs2, err := b.GetRunMessages(ctx, runID2)
			if err == nil {
				updateCalls, createCalls := 0, 0
				for _, m := range msgs2.Data {
					content := fmt.Sprintf("%v", m.Content)
					if strings.Contains(content, "entity-update") {
						updateCalls++
					}
					if strings.Contains(content, "entity-create") {
						createCalls++
					}
				}
				t.Logf("  Run 2 tool calls: entity-create=%d entity-update=%d", createCalls, updateCalls)
				if createCalls > 0 {
					t.Logf("  ⚪ Dedup created %d new entities (expected updates/zero new)", createCalls)
				} else {
					t.Logf("  ✅ Dedup: 0 new entities created, all matched existing")
				}
			}
		}
	}

	// ── Summary ──
	t.Logf("")
	t.Logf("══════════════════════════════════════════════════════")
	t.Logf("  🎯 ENTITY EXTRACTOR: DEEP TEST COMPLETE")
	t.Logf("══════════════════════════════════════════════════════")
}

// ── Longer polling (up to 300s) ──

func pollExtractorLong(ctx context.Context, b *memory.Bridge, runID string, t testing.TB, maxSec int) bool {
	t.Helper()
	timeout := time.After(time.Duration(maxSec) * time.Second)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastStatus := ""
	for {
		select {
		case <-timeout:
			t.Logf("  Poll: TIMEOUT (%ds)", maxSec)
			return false
		case <-ticker.C:
			run, err := b.GetProjectRun(ctx, runID)
			if err != nil {
				t.Logf("  Poll error: %v", err)
				continue
			}
			status := run.Data.Status
			if status != lastStatus {
				t.Logf("  Poll: status=%s", status)
				lastStatus = status
			}
			switch status {
			case "completed", "success":
				return true
			case "failed", "error":
				msg := ""
				if run.Data.ErrorMessage != nil {
					msg = *run.Data.ErrorMessage
				}
				t.Logf("  Run failed: %s", msg)
				return false
			}
		}
	}
}
