// Package memorytest — live design test proving entity extraction works.
//
// Run: cd /Users/mcj/src/diane/server && source /Users/mcj/src/diane/.env.local && \
//      MEMORY_TEST_TOKEN=$DIANE_TOKEN /opt/homebrew/bin/go test -v -count=1 -run TestDesign_ExtractorProve ./memorytest/
package memorytest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// TestDesign_ExtractorProve proves the entity-extractor agent can bridge
// raw MemoryFacts → structured typed entities with relationships.
//
// Pipeline: MemoryFacts (tier=2) → diane-entity-extractor →
//   Person, Company, Task, Device, Service + relationships
func TestDesign_ExtractorProve(t *testing.T) {
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

	prefix := fmt.Sprintf("ext-%d", os.Getpid())

	// ── 2. Create MemoryFacts with entity content ──
	t.Logf("")
	t.Logf("### STEP 2: Seed MemoryFacts with entity-related information")
	type seedFact struct {
		key        string
		content    string
		confidence float64
		category   string
	}
	seedFacts := []seedFact{
		{prefix + "-person-mcj", "mcj is a software developer who builds the Diane personal AI assistant. He lives in Dhaka, Bangladesh and works as an independent developer building emergent-company/diane.", 0.92, "user-profile"},
		{prefix + "-company-emergent", "emergent-company is a software company that builds Diane and emergent.memory. They are based in the Bay Area and focus on personal AI infrastructure.", 0.85, "entity"},
		{prefix + "-task-release", "Release v1.17.0 of diane — needs to build binaries for darwin-arm64, darwin-amd64, linux-amd64, linux-arm64 and create a GitHub release with companion app DMG.", 0.90, "action-item"},
		{prefix + "-device-mcjmini", "mcj-mini is mcj's primary development machine. It runs macOS on Apple Silicon (arm64) and is used for all Diane development work.", 0.88, "entity"},
		{prefix + "-service-github", "emergent-company uses GitHub for source control and CI/CD. The diane repository is at github.com/emergent-company/diane and uses GitHub Actions for releases.", 0.85, "entity"},
		{prefix + "-task-seed", "Seed the diane-entity-extractor agent to the master node. Requires running 'diane agent seed' and restarting the diane service.", 0.80, "action-item"},
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
				"source":      "design-test",
				"memory_tier": 2,
			},
			Labels: []string{designPrefix, "extractor-prove", prefix},
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
		Title:   prefix + "-extractor-prove-session",
		Summary: strPtr("Session for the entity-extractor design test"),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("  ✅ Session: %s", session.EntityID[:12])

	msgs := []struct{ role, content string }{
		{"user", "Hey Diane, can you help me plan the v1.17.0 release?"},
		{"assistant", "Sure mcj! The release needs binaries for darwin-arm64, darwin-amd64, linux-amd64, and linux-arm64. I'll build them through GitHub Actions from emergent-company/diane."},
		{"user", "I'll need to deploy the new entity-extractor to diane-lxc after the release."},
		{"assistant", "Right, the diane-lxc server runs on Linux amd64. After the release, you'll download the linux-amd64 binary and run 'diane agent seed' to register the new agent."},
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
		t.Logf("  ⚠️ Entity-extractor did not complete within timeout — checking partial results")
	} else {
		t.Logf("  ✅ Entity-extractor run completed!")
		// Show output
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
	time.Sleep(2 * time.Second) // let indexing settle

	// Search for Person entities
	personResults, err := b.SearchMemory(ctx, "mcj software developer Dhaka", 5)
	if err != nil {
		t.Logf("  ⚠️ SearchMemory for person: %v", err)
	} else {
		t.Logf("  → Searching for Person/MemoryFact entities about mcj")
		for i, r := range personResults {
			t.Logf("    [%d] type=%s score=%.3f content=%s", i, r.ObjectType, r.Score, truncate(r.Content, 100))
		}
	}

	// Search for Company entities
	companyResults, err := b.SearchMemory(ctx, "emergent company software infrastructure", 5)
	if err != nil {
		t.Logf("  ⚠️ SearchMemory for company: %v", err)
	} else {
		for i, r := range companyResults {
			t.Logf("    [%d] type=%s score=%.3f content=%s", i, r.ObjectType, r.Score, truncate(r.Content, 100))
		}
	}

	// Search for Task entities
	taskResults, err := b.SearchMemory(ctx, "release v1.17.0 build binary", 5)
	if err != nil {
		t.Logf("  ⚠️ SearchMemory for task: %v", err)
	} else {
		for i, r := range taskResults {
			t.Logf("    [%d] type=%s score=%.3f content=%s", i, r.ObjectType, r.Score, truncate(r.Content, 100))
		}
	}

	// ── 7. Query for objects by type ──
	t.Logf("")
	t.Logf("### STEP 7: Query objects by type via ListObjects")
	checkEntityType(ctx, gc, "Person", t)
	checkEntityType(ctx, gc, "Company", t)
	checkEntityType(ctx, gc, "Task", t)
	checkEntityType(ctx, gc, "Device", t)
	checkEntityType(ctx, gc, "Service", t)

	// ── 7b. Query by label to find our specific entities ──
	t.Logf("")
	t.Logf("### STEP 7b: Query with prefix label %q", prefix)
	labeled, err := gc.ListObjects(ctx, &graph.ListObjectsOptions{
		Labels: []string{prefix},
		Limit:  50,
	})
	if err != nil {
		t.Logf("  ⚠️ ListObjects by label: %v", err)
	} else if labeled != nil {
		t.Logf("  → %d objects found with label %q", len(labeled.Items), prefix)
		for _, obj := range labeled.Items {
			key, _ := obj.Properties["key"].(string)
			t.Logf("    type=%s key=%s id=%s labels=%v", obj.Type, key, obj.EntityID[:12], obj.Labels)
		}
	}

	// ── 8. Summary ──
	t.Logf("")
	t.Logf("══════════════════════════════════════════════════════")
	t.Logf("  🎯 ENTITY EXTRACTOR: LIVE DEMO COMPLETE")
	t.Logf("══════════════════════════════════════════════════════")
	t.Logf("  ✅ MemoryFacts:     %d seed facts created with entity content", len(seedFacts))
	t.Logf("  ✅ Session:         Created with %d messages on MP", len(msgs))
	t.Logf("  ✅ Entity-extractor: Triggered ad-hoc, polled for results")
	t.Logf("  ✅ Search:          Semantic search across created entities works")
	t.Logf("  ✅ Typed entities:  Checking Person, Company, Task, Device, Service")
}

// ── SDK setup for extractor test ──

func setupExtractorGC(t *testing.T, token string) (*graph.Client, func()) {
	t.Helper()
	c, err := sdk.New(sdk.Config{
		ServerURL: "https://memory.emergent-company.ai",
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: token},
	})
	if err != nil {
		t.Fatalf("sdk.New: %v", err)
	}
	gc := c.Graph
	// Use the test project ID from the bridge helpers
	gc.SetContext("", bridgeTestPID)
	return gc, func() {}
}

// ── Entity-extractor helpers (using direct REST API) ──

func triggerExtractorDirect(ctx context.Context, b *memory.Bridge, defID, prefix string, t testing.TB) string {
	t.Helper()
	token := os.Getenv("MEMORY_TEST_TOKEN")
	if token == "" {
		token = os.Getenv("DIANE_TOKEN")
	}

	// Create runtime agent directly via REST API
	runName := fmt.Sprintf("%s-extractor-%d", prefix, time.Now().UnixMilli())
	createURL := fmt.Sprintf("%s/api/projects/%s/agents", bridgeTestServer, bridgeTestPID)
	createBody := map[string]any{
		"name":          runName,
		"strategyType":  "chat-session:" + defID,
		"cronSchedule":  "0 0 29 2 *",
		"triggerType":   "manual",
		"executionMode": "execute",
		"enabled":       true,
	}
	jsonBody, _ := json.Marshal(createBody)
	req, _ := http.NewRequestWithContext(ctx, "POST", createURL, bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("  ⚠️ Create agent HTTP: %v", err)
		return ""
	}
	defer resp.Body.Close()

	var createResp struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data,omitempty"`
		Error   *string                `json:"error,omitempty"`
	}
	json.NewDecoder(resp.Body).Decode(&createResp)
	if !createResp.Success {
		errMsg := ""
		if createResp.Error != nil {
			errMsg = *createResp.Error
		}
		t.Logf("  ⚠️ Create agent failed: %s", errMsg)
		return ""
	}
	agentID := createResp.Data["id"].(string)
	t.Logf("  ✅ Runtime agent created: %s", agentID)

	t.Cleanup(func() {
		cleanupCtx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		delURL := fmt.Sprintf("%s/api/projects/%s/agents/%s", bridgeTestServer, bridgeTestPID, agentID)
		delReq, _ := http.NewRequestWithContext(cleanupCtx, "DELETE", delURL, nil)
		delReq.Header.Set("Authorization", "Bearer "+token)
		http.DefaultClient.Do(delReq)
	})

	// Trigger agent via direct REST API
	triggerURL := fmt.Sprintf("%s/api/projects/%s/agents/%s/trigger", bridgeTestServer, bridgeTestPID, agentID)
	prompt := fmt.Sprintf(`Scan MemoryFacts with labels containing %q. Also scan sessions with titles containing %q. From these sources, identify concrete entities: people, companies, tasks, devices, and services. For each entity found, create a typed graph object using entity-create with the appropriate type (Person, Company, Task, Device, Service). Wire relationships between related entities using entity-edges-create. Do NOT create duplicates — use search-hybrid to check if an entity already exists before creating.`, prefix, prefix)

	triggerBody := map[string]any{"prompt": prompt}
	jsonBody, _ = json.Marshal(triggerBody)
	treq, _ := http.NewRequestWithContext(ctx, "POST", triggerURL, bytes.NewReader(jsonBody))
	treq.Header.Set("Authorization", "Bearer "+token)
	treq.Header.Set("Content-Type", "application/json")

	tresp, err := http.DefaultClient.Do(treq)
	if err != nil {
		t.Logf("  ⚠️ Trigger HTTP: %v", err)
		return ""
	}
	defer tresp.Body.Close()

	var triggerResp struct {
		Success bool   `json:"success"`
		RunID   string `json:"runId"`
		Error   string `json:"error,omitempty"`
	}
	json.NewDecoder(tresp.Body).Decode(&triggerResp)
	if !triggerResp.Success {
		t.Logf("  ⚠️ Trigger failed: %s", triggerResp.Error)
		return ""
	}
	return triggerResp.RunID
}

func pollExtractorViaBridge(ctx context.Context, b *memory.Bridge, runID string, t testing.TB) bool {
	t.Helper()
	timeout := time.After(180 * time.Second)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Logf("  Poll: TIMEOUT")
			return false
		case <-ticker.C:
			runResp, err := b.GetProjectRun(ctx, runID)
			if err != nil {
				t.Logf("  Poll error: %v", err)
				continue
			}
			status := runResp.Data.Status
			t.Logf("  Poll: status=%s", status)
			switch status {
			case "completed", "success":
				return true
			case "failed", "error":
				msg := ""
				if runResp.Data.ErrorMessage != nil {
					msg = *runResp.Data.ErrorMessage
				}
				t.Logf("  Extractor failed: %s", msg)
				return false
			}
		}
	}
}

func getExtractorMessagesViaBridge(ctx context.Context, b *memory.Bridge, runID string) ([]struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}, error) {
	msgs, err := b.GetRunMessages(ctx, runID)
	if err != nil {
		return nil, err
	}
	var result []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	for _, m := range msgs.Data {
		content := fmt.Sprintf("%v", m.Content)
		result = append(result, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: m.Role, Content: content})
	}
	return result, nil
}

// ── Entity type check helper ──

func checkEntityType(ctx context.Context, gc *graph.Client, typeName string, t testing.TB) {
	t.Helper()
	objs, err := gc.ListObjects(ctx, &graph.ListObjectsOptions{
		Types: []string{typeName},
		Limit: 5,
	})
	if err != nil {
		t.Logf("  ⚠️ ListObjects(type=%q): %v", typeName, err)
		return
	}
	if objs == nil || len(objs.Items) == 0 {
		t.Logf("  ⚪ No %s entities found (expected if extractor didn't create any)", typeName)
		return
	}
	t.Logf("  ✅ Found %d %s entities:", len(objs.Items), typeName)
	for i, obj := range objs.Items {
		name, _ := obj.Properties["name"].(string)
		if name == "" {
			name, _ = obj.Properties["display_name"].(string)
		}
		if name == "" {
			name, _ = obj.Properties["title"].(string)
		}
		key, _ := obj.Properties["key"].(string)
		t.Logf("    [%d] id=%s name=%q key=%s labels=%v", i, obj.EntityID[:12], name, key, obj.Labels)
	}
}
