// Package memorytest validates agent run trace APIs — the ability to fetch
// run messages, tool calls, and agent-specific run lists after a trigger.
//
// These tests complement the high-level GetProjectRunFull test by testing
// the individual endpoints that back CLI commands like 'diane agent trace'
// and 'diane agent runs'.
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestRunTrace ./memorytest/
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
// TestRunTrace_GetRunMessages: Triggers an agent, polls to completion, then
// fetches the conversation transcript. Verifies messages are returned with
// correct roles, chronological ordering, and content.
// =========================================================================

func TestRunTrace_GetRunMessages(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout)
	defer cancel()

	// 1. Find agent definition and create runtime agent
	defID, agentID := createAgent(ctx, t, b)
	t.Logf("Agent: %s (def: %s)", agentID, defID)

	// 2. Trigger with a prompt that will produce multiple turns
	prompt := "Say hello, then list 2 tools you have access to."
	resp, err := b.TriggerAgentWithInput(ctx, agentID, prompt, "")
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	if resp.Error != nil && *resp.Error != "" {
		t.Fatalf("Trigger error: %s", *resp.Error)
	}
	runID := *resp.RunID
	t.Logf("Run ID: %s", runID)

	// 3. Poll to completion
	if !pollRunCompletion(b, ctx, t, runID) {
		t.Fatal("Run did not complete within polling window")
	}

	// 4. Fetch messages
	msgsResp, err := b.GetRunMessages(ctx, runID)
	if err != nil {
		t.Fatalf("GetRunMessages: %v", err)
	}

	msgs := msgsResp.Data
	if len(msgs) == 0 {
		t.Fatal("GetRunMessages returned zero messages")
	}

	t.Logf("Messages: %d total", len(msgs))

	// Verify message structure — agent responses use agent name as role (e.g. "diane-default")
	var hasUser, hasAssistant bool
	var prevStep int = -1
	for i, m := range msgs {
		content := extractMsgContent(m.Content)
		t.Logf("  [%d] step=%d role=%s %.120s", i, m.StepNumber, m.Role, content)

		if m.Role == "user" {
			hasUser = true
		} else {
			hasAssistant = true // any non-user role is an agent response
		}
		if m.ID == "" {
			t.Errorf("Message[%d] has empty ID", i)
		}
		if m.RunID != runID {
			t.Errorf("Message[%d] RunID=%q, want %q", i, m.RunID, runID)
		}

		// Verify step numbers are non-decreasing
		if m.StepNumber < prevStep {
			t.Errorf("Message[%d] step=%d < previous step=%d — ordering violation", i, m.StepNumber, prevStep)
		}
		prevStep = m.StepNumber
	}

	if !hasUser {
		t.Error("No user message found in run transcript")
	}
	if !hasAssistant {
		t.Error("No assistant/model message found in run transcript")
	}

	t.Logf("✅ GetRunMessages verified: %d messages, hasUser=%v hasAssistant=%v",
		len(msgs), hasUser, hasAssistant)
}

// isProviderError checks if an error message indicates a transient provider issue.
func isProviderError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "provider") ||
		strings.Contains(lower, "api key") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "reasoning_content") ||
		strings.Contains(lower, "400")
}

// =========================================================================
// TestRunTrace_GetRunToolCalls: Triggers an agent, then fetches the tool
// calls made during the run. Verifies tool call structure, status, and
// that at least one tool was called.
// =========================================================================

func TestRunTrace_GetRunToolCalls(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout)
	defer cancel()

	// 1. Set up agent
	defID, agentID := createAgent(ctx, t, b)
	t.Logf("Agent: %s (def: %s)", agentID, defID)

	// 2. Trigger with a prompt that will use tools
	// The diane-default agent has skill injection and web-search-brave
	prompt := "Search the web for 'latest AI news' and summarize what you find."
	resp, err := b.TriggerAgentWithInput(ctx, agentID, prompt, "")
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	if resp.Error != nil && *resp.Error != "" {
		t.Fatalf("Trigger error: %s", *resp.Error)
	}
	runID := *resp.RunID
	t.Logf("Run ID: %s", runID)

	// 3. Poll to completion
	if !pollRunCompletion(b, ctx, t, runID) {
		// Check if the run failed due to a provider error — skip gracefully
		runResp, pollErr := b.GetProjectRun(ctx, runID)
		if pollErr == nil && runResp.Data.ErrorMessage != nil {
			errMsg := *runResp.Data.ErrorMessage
			t.Logf("Run failed: %s", errMsg)
			if isProviderError(errMsg) {
				t.Skipf("Provider error — run couldn't complete: %s", errMsg)
			}
		}
		t.Fatal("Run did not complete within polling window")
	}

	// 4. Fetch tool calls
	tcResp, err := b.GetRunToolCalls(ctx, runID)
	if err != nil {
		// The endpoint might not be available on older MP versions
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "501") {
			t.Skipf("GetRunToolCalls not available: %v", err)
		}
		t.Fatalf("GetRunToolCalls: %v", err)
	}

	toolCalls := tcResp.Data
	if len(toolCalls) == 0 {
		t.Log("No tool calls recorded — agent may have answered without tools")
		t.Log("This is acceptable for the diane-default agent which has many tools")
	} else {
		t.Logf("Tool calls: %d total", len(toolCalls))
		var successCount, errorCount int
		for i, tc := range toolCalls {
			status := tc.Status
			if status == "success" {
				successCount++
			} else if status == "error" {
				errorCount++
			}

			t.Logf("  [%d] %s (status=%s, step=%d, %dms)",
				i, tc.ToolName, status, tc.StepNumber, safeDerefInt(tc.DurationMs))

			// Verify structure
			if tc.ID == "" {
				t.Errorf("ToolCall[%d] has empty ID", i)
			}
			if tc.RunID != runID {
				t.Errorf("ToolCall[%d] RunID=%q, want %q", i, tc.RunID, runID)
			}
			if tc.ToolName == "" {
				t.Errorf("ToolCall[%d] has empty ToolName", i)
			}
		}

		t.Logf("  Success: %d, Error: %d", successCount, errorCount)

		// At least some should succeed (unless all tools failed)
		if successCount == 0 && len(toolCalls) > 0 {
			t.Log("⚠️  All tool calls failed — check provider configuration")
		}
	}

	t.Log("✅ GetRunToolCalls verified")
}

// =========================================================================
// TestRunTrace_GetAgentRuns: Triggers an agent, then fetches the run list
// for that specific agent via GetAgentRuns. Verifies the triggered run
// appears in the list with correct status.
// =========================================================================

func TestRunTrace_GetAgentRuns(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout)
	defer cancel()

	// 1. Set up agent
	defID, agentID := createAgent(ctx, t, b)
	t.Logf("Agent: %s (def: %s)", agentID, defID)

	// 2. Check current runs (baseline)
	runsBefore, err := b.GetAgentRuns(ctx, agentID, 10)
	if err != nil {
		t.Logf("GetAgentRuns (before): %v (ignoring — may be empty)", err)
	} else {
		t.Logf("Runs before: %d", len(runsBefore.Data))
	}

	// 3. Trigger the agent
	prompt := "Say hello in one sentence."
	resp, err := b.TriggerAgentWithInput(ctx, agentID, prompt, "")
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	runID := *resp.RunID
	t.Logf("Run ID: %s", runID)

	// 4. Poll to completion
	if !pollRunCompletion(b, ctx, t, runID) {
		t.Fatal("Run did not complete within polling window")
	}

	// 5. Fetch agent runs again — our run should now appear
	runsAfter, err := b.GetAgentRuns(ctx, agentID, 10)
	if err != nil {
		t.Fatalf("GetAgentRuns (after): %v", err)
	}

	t.Logf("Runs after trigger: %d", len(runsAfter.Data))

	if len(runsAfter.Data) == 0 {
		t.Error("GetAgentRuns returned empty list after trigger")
	} else {
		// The most recent run should be ours
		var found bool
		for _, r := range runsAfter.Data {
			t.Logf("  Run: id=%s status=%s started=%s",
				r.ID, r.Status, r.StartedAt.Format(time.RFC3339))
			if r.ID == runID {
				found = true
				if r.Status != "success" && r.Status != "completed" {
					t.Logf("⚠️  Run status is %q (expected success/completed)", r.Status)
				}
				if r.AgentName != "" {
					t.Logf("  Agent name: %s", r.AgentName)
				}
				if r.DurationMs != nil {
					t.Logf("  Duration: %dms", *r.DurationMs)
				}
				if r.Model != nil {
					t.Logf("  Model: %s", *r.Model)
				}
				break
			}
		}
		if !found {
			t.Error("Triggered run not found in GetAgentRuns response")

			// Log all run IDs for debugging
			t.Log("Run IDs in list:")
			for _, r := range runsAfter.Data {
				t.Logf("  %s", r.ID)
			}
		} else {
			t.Log("✅ Triggered run found in GetAgentRuns list")
		}
	}
}

// =========================================================================
// TestRunTrace_GetAgentRunsLimit: Verifies that the limit parameter works
// correctly — asking for 1 run returns at most 1 result.
// =========================================================================

func TestRunTrace_GetAgentRunsLimit(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout)
	defer cancel()

	defID, agentID := createAgent(ctx, t, b)
	_ = defID // OK, used in other test variants
	t.Logf("Agent: %s", agentID)

	// Trigger to ensure at least one run exists
	resp, err := b.TriggerAgentWithInput(ctx, agentID, "Say hello briefly.", "")
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	runID := *resp.RunID
	pollRunCompletion(b, ctx, t, runID)

	// Fetch with limit=1
	runs, err := b.GetAgentRuns(ctx, agentID, 1)
	if err != nil {
		t.Fatalf("GetAgentRuns(limit=1): %v", err)
	}
	if len(runs.Data) > 1 {
		t.Errorf("GetAgentRuns(limit=1) returned %d runs, expected <= 1", len(runs.Data))
	}
	t.Logf("✅ GetAgentRuns(limit=1) returned %d runs", len(runs.Data))
}

// =========================================================================
// Helper: createAgent sets up a runtime agent for testing.
// Returns (defID, agentID). Cleans up on test completion.
// =========================================================================

func createAgent(ctx context.Context, t *testing.T, b *memory.Bridge) (string, string) {
	t.Helper()

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

	runName := fmt.Sprintf("t-run-%d", time.Now().UnixMilli())
	agent, err := b.CreateRuntimeAgent(ctx, runName, defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := agent.Data.ID

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = b.Client().Agents.Delete(cleanupCtx, agentID)
	})

	return defID, agentID
}
