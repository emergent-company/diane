// Package memorytest validates the agent lifecycle against the live Memory Platform.
//
// Unlike the bridge tests (which use MEMORY_TEST_TOKEN env var), these tests
// read credentials from ~/.config/diane.yml — Diane's canonical config file.
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestAgent ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
)

const (
	agentTestDefName = "diane-default"
	agentTestPrompt  = "Say hello in one sentence. Then list 3 tools you have access to."
	agentTestTimeout = 120 * time.Second
	agentTestPoll    = 2 * time.Second
	agentTestMaxPoll = 30
)

// setupBridgeFromConfig reads ~/.config/diane.yml and creates a bridge.
func setupBridgeFromConfig(t *testing.T) *memory.Bridge {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Cannot load config: %v", err)
	}
	pc := cfg.Active()
	if pc == nil {
		t.Skip("No active project in config")
	}
	if pc.Token == "" {
		t.Skip("No token in config — run 'diane init' first")
	}

	b, err := memory.New(memory.Config{
		ServerURL:          pc.ServerURL,
		APIKey:             pc.Token,
		ProjectID:          pc.ProjectID,
		HTTPClientTimeout:  30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}
	t.Cleanup(b.Close)
	return b
}

// TestAgentDefinitionExists checks that the named agent definition is synced to MP.
func TestAgentDefinitionExists(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx := context.Background()

	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}

	var found bool
	for _, d := range defs.Data {
		if d.Name == agentTestDefName {
			found = true
			t.Logf("Found agent definition: %s (%s)", d.Name, d.ID)
			t.Logf("  FlowType: %s", d.FlowType)
			t.Logf("  Tools: %d", d.ToolCount)
			t.Logf("  Visibility: %s", d.Visibility)
			break
		}
	}
	if !found {
		t.Fatalf("Agent definition '%s' not found. Run 'diane agent sync' first.", agentTestDefName)
	}
}

// TestAgentTriggerRun performs a full agent lifecycle:
// create runtime agent → trigger → poll → verify → cleanup.
func TestAgentTriggerRun(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout)
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

	// 2. Create a runtime agent (clean up afterwards)
	runName := fmt.Sprintf("test-run-%d", time.Now().UnixMilli())
	agent, err := b.CreateRuntimeAgent(ctx, runName, defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := agent.Data.ID
	t.Logf("Runtime agent: %s", agentID)

	// Cleanup: delete the runtime agent after test
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := b.Client().Agents.Delete(cleanupCtx, agentID); err != nil {
			t.Logf("Cleanup: delete agent %s: %v", agentID, err)
		}
	})

	// 3. Trigger the agent
	resp, err := b.TriggerAgentWithInput(ctx, agentID, agentTestPrompt)
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	if resp.Error != nil && *resp.Error != "" {
		t.Fatalf("Trigger error: %s", *resp.Error)
	}
	runID := *resp.RunID
	t.Logf("Run ID: %s", runID)
	t.Logf("Prompt: %s", agentTestPrompt)

	// 4. Poll for completion
	for i := 0; i < agentTestMaxPoll; i++ {
		time.Sleep(agentTestPoll)

		runResp, err := b.GetProjectRun(ctx, runID)
		if err != nil {
			t.Logf("Poll %d: %v", i+1, err)
			continue
		}

		run := runResp.Data

		switch run.Status {
		case "success", "completed":
			// Completed successfully
			t.Logf("Completed after %d polls (%dms)", i+1, safeDerefInt(run.DurationMs))
			t.Logf("Steps: %d", run.StepCount)

			// Verify summary exists
			summary, hasSummary := run.Summary["final_response"]
			if !hasSummary {
				t.Fatal("Run has no final_response summary")
			}
			response := fmt.Sprintf("%v", summary)
			t.Logf("Response length: %d chars", len(response))

			// Verify response contains greeting
			if !strings.Contains(strings.ToLower(response), "hello") &&
				!strings.Contains(strings.ToLower(response), "hi") &&
				!strings.Contains(strings.ToLower(response), "greeting") {
				t.Logf("Warning: response may not contain greeting: %.80s", response)
			}

			// Verify tool mentions
			toolCount := 0
			for _, line := range strings.Split(response, "\n") {
				if strings.Contains(line, "**") || strings.Contains(line, "1.") ||
					strings.Contains(line, "2.") || strings.Contains(line, "3.") {
					toolCount++
				}
			}
			if toolCount < 3 {
				t.Logf("Warning: expected ~3 tool mentions, found ~%d", toolCount)
			}

			// 5. Fetch messages
			msgs, err := b.GetRunMessages(ctx, runID)
			if err != nil {
				t.Logf("GetRunMessages: %v (non-fatal)", err)
			} else if msgs != nil {
				t.Logf("Messages: %d", len(msgs.Data))
				for _, m := range msgs.Data {
					content := extractMsgContent(m.Content)
					t.Logf("  [%s] %.100s", m.Role, content)
				}
				if len(msgs.Data) < 1 {
					t.Error("Expected at least 1 message (assistant response)")
				}
			}

			// 6. Fetch tool calls (informational)
			toolCalls, err := b.GetRunToolCalls(ctx, runID)
			if err != nil {
				t.Logf("GetRunToolCalls: %v (non-fatal)", err)
			} else if toolCalls != nil && len(toolCalls.Data) > 0 {
				t.Logf("Tool calls: %d", len(toolCalls.Data))
				for _, tc := range toolCalls.Data {
					t.Logf("  %s (status: %s, %dms)", tc.ToolName, tc.Status, safeDerefInt(tc.DurationMs))
				}
			}

			t.Logf("✅ Agent run completed successfully")
			return

		case "failed", "error":
			errMsg := ""
			if runResp.Data.ErrorMessage != nil {
				errMsg = *runResp.Data.ErrorMessage
			}
			t.Fatalf("Run failed: %s", errMsg)
		}
	}
	t.Fatal("Run timed out — did not complete within polling window")
}

// TestAgentList shows all synced agent definitions.
func TestAgentListDefinitions(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}

	t.Logf("Agent definitions (%d):", len(defs.Data))
	for _, d := range defs.Data {
		t.Logf("  • %s", d.Name)
		t.Logf("    ID: %s", d.ID)
		t.Logf("    Flow: %s | Tools: %d | Default: %v", d.FlowType, d.ToolCount, d.IsDefault)
		if d.Description != nil {
			t.Logf("    %s", *d.Description)
		}
	}

	if len(defs.Data) == 0 {
		t.Log("No agent definitions found — run 'diane agent sync'")
	}
}

// ─── Helpers ───

func safeDerefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func extractMsgContent(content map[string]any) string {
	if content == nil {
		return ""
	}
	for _, key := range []string{"content", "text", "message"} {
		if v, ok := content[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
			return fmt.Sprintf("%v", v)
		}
	}
	return fmt.Sprintf("%v", content)
}
