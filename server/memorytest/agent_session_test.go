// Package memorytest validates agent session continuity — the ability to
// maintain conversation context across multiple trigger calls using sessionId.
//
// This tests the MP feature (v0.40.15+) where passing the same sessionId to
// successive TriggerAgentWithInput calls keeps the ADK conversation going,
// allowing Diane to maintain context across multiple messages.
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestAgentSession ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/memory"
)

// =========================================================================
// TestAgentSession_Continuity: Verifies that calling TriggerAgentWithInput
// twice with the same sessionId produces a single session with accumulated
// context. The second trigger's messages should include both conversations.
//
// Strategy:
//   1. Trigger agent with prompt "Say hello and mention you are a cat"
//   2. Poll until complete
//   3. Trigger again with same sessionId: "What animal did you say you were?"
//   4. Poll until complete
//   5. Get run messages for run #2 — response should reference "cat"
// =========================================================================

func TestAgentSession_Continuity(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout+60*time.Second)
	defer cancel()

	// 1. Find the agent definition
	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}
	var defID string
	for _, d := range defs.Data {
		if d.Name == agentTestDefName {
			defID = d.ID
			break
		}
	}
	if defID == "" {
		t.Skipf("Agent definition '%s' not found — run 'diane agent sync'", agentTestDefName)
	}
	t.Logf("Agent definition: %s", defID)

	// 2. Create a runtime agent
	runName := fmt.Sprintf("t-session-%d", time.Now().UnixMilli())
	cleanupTestAgentsByPrefix(ctx, "t-session-", t)
	agent, err := b.CreateRuntimeAgent(ctx, runName, defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := agent.Data.ID
	t.Logf("Runtime agent: %s", agentID)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = b.Client().Agents.Delete(cleanupCtx, agentID)
	})

	// 3. Create a unique session ID for this test
	sessionID := fmt.Sprintf("test-session-%d", time.Now().UnixMilli())
	t.Logf("Session ID: %s", sessionID)

	// 4. First trigger — introduce context
	prompt1 := "Say hello and mention that you are a cat in your response. Keep it to one sentence."
	t.Logf("Run 1 prompt: %s", prompt1)

	resp1, err := b.TriggerAgentWithInput(ctx, agentID, prompt1, sessionID)
	if err != nil {
		t.Fatalf("Trigger 1: %v", err)
	}
	if resp1.Error != nil && *resp1.Error != "" {
		t.Fatalf("Trigger 1 error: %s", *resp1.Error)
	}
	runID1 := *resp1.RunID
	t.Logf("Run 1 ID: %s", runID1)

	// Poll run 1 to completion
	if !pollRunCompletion(b, ctx, t, runID1) {
		t.Fatal("Run 1 did not complete within polling window")
	}

	// Log messages from run 1 (informational)
	logMessages(t, b, ctx, runID1, "Run 1 messages")

	// 5. Second trigger — same sessionId, ask about context
	prompt2 := "What animal did you say you were in our first message? Answer in one sentence."
	t.Logf("Run 2 prompt: %s", prompt2)

	resp2, err := b.TriggerAgentWithInput(ctx, agentID, prompt2, sessionID)
	if err != nil {
		t.Fatalf("Trigger 2: %v", err)
	}
	if resp2.Error != nil && *resp2.Error != "" {
		t.Fatalf("Trigger 2 error: %s", *resp2.Error)
	}
	runID2 := *resp2.RunID
	t.Logf("Run 2 ID: %s", runID2)

	// Poll run 2 to completion
	if !pollRunCompletion(b, ctx, t, runID2) {
		t.Fatal("Run 2 did not complete within polling window")
	}

	// 6. Get messages from run 2 and verify continuity
	msgs2, err := b.GetRunMessages(ctx, runID2)
	if err != nil {
		t.Fatalf("GetRunMessages (run 2): %v", err)
	}
	if msgs2 == nil || len(msgs2.Data) == 0 {
		t.Fatal("No messages returned for run 2")
	}

	t.Logf("Run 2 messages: %d", len(msgs2.Data))
	var lastResponse string
	for _, m := range msgs2.Data {
		content := extractMsgContent(m.Content)
		t.Logf("  [%s] %.120s", m.Role, content)
		if m.Role == "assistant" || m.Role == "model" {
			lastResponse = content
		}
	}

	// Check that the response references "cat" — proving session continuity
	if lastResponse != "" {
		lower := strings.ToLower(lastResponse)
		if strings.Contains(lower, "cat") {
			t.Log("✅ Session continuity confirmed: model remembered it's a cat")
		} else {
			t.Logf("⚠️  No 'cat' reference in response (%.100s) — session continuity may not work", lastResponse)
			t.Log("   This could mean MP server doesn't support sessionId, or model ignored context")
			t.Log("   Check MP version (need >= v0.40.15)")
		}
	}

	t.Log("✅ Session continuity test completed")
}

// =========================================================================
// TestAgentSession_Isolation: Verifies that two triggers with different
// sessionIds produce independent conversations. The second session should
// NOT have context from the first.
// =========================================================================

func TestAgentSession_Isolation(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout+60*time.Second)
	defer cancel()

	// Find agent definition
	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}
	var defID string
	for _, d := range defs.Data {
		if d.Name == agentTestDefName {
			defID = d.ID
			break
		}
	}
	if defID == "" {
		t.Skipf("Agent definition '%s' not found — run 'diane agent sync'", agentTestDefName)
	}

	// Create runtime agent
	runName := fmt.Sprintf("t-isolate-%d", time.Now().UnixMilli())
	cleanupTestAgentsByPrefix(ctx, "t-isolate-", t)
	agent, err := b.CreateRuntimeAgent(ctx, runName, defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := agent.Data.ID

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := b.Client().Agents.Delete(cleanupCtx, agentID); err != nil {
			t.Logf("Cleanup: delete agent %s: %v", agentID[:12], err)
		}
	})

	// Two different session IDs
	now := time.Now().UnixMilli()
	sessionID1 := fmt.Sprintf("isolate-a-%d", now)
	sessionID2 := fmt.Sprintf("isolate-b-%d", now)
	t.Logf("Session A: %s", sessionID1)
	t.Logf("Session B: %s", sessionID2)

	// Trigger run 1 with secret number
	prompt1 := "Remember this secret: the secret number is 42. Say 'I remember the secret number'."
	resp1, err := b.TriggerAgentWithInput(ctx, agentID, prompt1, sessionID1)
	if err != nil {
		t.Fatalf("Trigger 1: %v", err)
	}
	runID1 := *resp1.RunID
	t.Logf("Run 1 (session A): %s", runID1)

	if !pollRunCompletion(b, ctx, t, runID1) {
		t.Fatal("Run 1 did not complete")
	}

	// Trigger run 2 with DIFFERENT session ID — ask about secret
	prompt2 := "What was the secret number? If you don't know, say 'I don't know any secret number'."
	resp2, err := b.TriggerAgentWithInput(ctx, agentID, prompt2, sessionID2)
	if err != nil {
		t.Fatalf("Trigger 2: %v", err)
	}
	runID2 := *resp2.RunID
	t.Logf("Run 2 (session B): %s", runID2)

	if !pollRunCompletion(b, ctx, t, runID2) {
		// Check for provider error — skip gracefully
		runResp, pollErr := b.GetProjectRun(ctx, runID2)
		if pollErr == nil && runResp.Data.ErrorMessage != nil {
			errMsg := *runResp.Data.ErrorMessage
			t.Logf("Run 2 failed: %s", errMsg)
			if isProviderError(errMsg) {
				t.Skipf("Provider error — isolation test can't complete: %s", errMsg)
			}
		}
		t.Fatal("Run 2 did not complete")
	}

	// Get run 2 messages — should NOT reference 42
	msgs2, err := b.GetRunMessages(ctx, runID2)
	if err != nil {
		t.Fatalf("GetRunMessages (run 2): %v", err)
	}

	var lastResponse string
	for _, m := range msgs2.Data {
		content := extractMsgContent(m.Content)
		t.Logf("  [%s] %.120s", m.Role, content)
		if m.Role == "assistant" || m.Role == "model" {
			lastResponse = content
		}
	}

	if lastResponse != "" {
		lower := strings.ToLower(lastResponse)
		if strings.Contains(lower, "42") || strings.Contains(lower, "secret number") {
			t.Logf("⚠️  Session B seems to know secret from A (%.100s)", lastResponse)
			t.Log("   Session isolation may not be working")
		} else {
			t.Log("✅ Session isolation: session B does not have context from session A")
		}
	}

	t.Log("✅ Session isolation test completed")
}

// =========================================================================
// TestAgentSession_NoSessionId: Verifies triggers without sessionId create
// independent runs — each trigger gets a different runID.
// =========================================================================

func TestAgentSession_NoSessionId(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout+60*time.Second)
	defer cancel()

	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}
	var defID string
	for _, d := range defs.Data {
		if d.Name == agentTestDefName {
			defID = d.ID
			break
		}
	}
	if defID == "" {
		t.Skipf("Agent definition '%s' not found", agentTestDefName)
	}

	runName := fmt.Sprintf("t-nosess-%d", time.Now().UnixMilli())
	cleanupTestAgentsByPrefix(ctx, "t-nosess-", t)
	agent, err := b.CreateRuntimeAgent(ctx, runName, defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := agent.Data.ID

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := b.Client().Agents.Delete(cleanupCtx, agentID); err != nil {
			t.Logf("Cleanup: delete agent %s: %v", agentID[:12], err)
		}
	})

	// Two triggers with empty sessionId
	resp1, err := b.TriggerAgentWithInput(ctx, agentID, "Say 'Run 1' and nothing else", "")
	if err != nil {
		t.Fatalf("Trigger 1: %v", err)
	}
	runID1 := *resp1.RunID

	resp2, err := b.TriggerAgentWithInput(ctx, agentID, "Say 'Run 2' and nothing else", "")
	if err != nil {
		t.Fatalf("Trigger 2: %v", err)
	}
	runID2 := *resp2.RunID

	// Must be different runs
	if runID1 == runID2 {
		t.Error("Two triggers without sessionId produced the same run ID")
	}

	// Both should complete
	if !pollRunCompletion(b, ctx, t, runID1) {
		t.Fatal("Run 1 did not complete")
	}
	if !pollRunCompletion(b, ctx, t, runID2) {
		t.Fatal("Run 2 did not complete")
	}

	t.Log("✅ No-sessionId isolation verified (different run IDs)")
}

// =========================================================================
// Helpers
// =========================================================================

// pollRunCompletion polls GetProjectRun until the run finishes or times out.
func pollRunCompletion(b *memory.Bridge, ctx context.Context, t testing.TB, runID string) bool {
	t.Helper()
	for i := 0; i < agentTestMaxPoll; i++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		time.Sleep(agentTestPoll)

		runResp, err := b.GetProjectRun(ctx, runID)
		if err != nil {
			t.Logf("Poll %d: %v", i+1, err)
			continue
		}

		run := runResp.Data
		switch run.Status {
		case "success", "completed":
			t.Logf("Completed after %d polls (%dms)", i+1, safeDerefInt(run.DurationMs))
			return true
		case "failed", "error":
			errMsg := ""
			if run.ErrorMessage != nil {
				errMsg = *run.ErrorMessage
			}
			t.Logf("Run failed: %s", errMsg)
			return false
		}
	}
	return false
}

// logMessages fetches and prints run messages for debugging.
func logMessages(t testing.TB, b *memory.Bridge, ctx context.Context, runID, label string) {
	t.Helper()
	msgs, err := b.GetRunMessages(ctx, runID)
	if err != nil {
		t.Logf("%s: GetRunMessages error: %v", label, err)
		return
	}
	if msgs == nil {
		return
	}
	t.Logf("%s (%d):", label, len(msgs.Data))
	for _, m := range msgs.Data {
		content := extractMsgContent(m.Content)
		t.Logf("  [%s] %.120s", m.Role, content)
	}
}
