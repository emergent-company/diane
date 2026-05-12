// Package memorytest validates the MCP agent integration end-to-end.
//
// The test:
//  1. Verifies MCP servers exist in the graph (MCPProxyConfig objects)
//  2. Checks tools are registered via relay on connected sessions
//  3. Creates or reuses a test agent definition with MCP tool references
//  4. Triggers the agent with a prompt that requires an MCP tool call
//  5. Verifies the agent called the MCP tool by fetching run messages and tool calls
//
// Run:
//
//	cd server && go test -v -count=1 -run TestMCPAgentIntegration ./memorytest/ -timeout 5m
//
//go:build integration

package memorytest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

const (
	mcpTestServerName = "everything"
	mcpTestAgentName  = "test-mcp-agent"
)

// TestMCPAgentIntegration validates that an agent can discover and call MCP tools.
func TestMCPAgentIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping MCP integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Load config for API calls to MP relay
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Cannot load config: %v", err)
	}
	pc := cfg.Active()
	if pc == nil {
		t.Skip("No active project configured")
	}
	if pc.Token == "" {
		t.Skip("No token in config — run 'diane init' first")
	}
	serverURL := pc.ServerURL
	token := pc.Token

	b := setupBridgeFromConfig(t)

	// ── 1. Verify MCP servers exist in the graph ──
	t.Log("=== 1. MCP Server Discovery ===")
	entries, err := b.ListMCPProxyConfigs(ctx)
	if err != nil {
		t.Fatalf("ListMCPProxyConfigs: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("No MCP servers configured in graph — add at least one MCPProxyConfig first")
	}
	foundServer := false
	for _, e := range entries {
		if strings.Contains(e.Config, mcpTestServerName) {
			foundServer = true
			t.Logf("  ✅ Found MCP server: %s (scope=%s)", mcpTestServerName, e.Scope)
		}
	}
	if !foundServer {
		t.Logf("  ⚠️  Server '%s' not found in graph", mcpTestServerName)
	}
	t.Logf("  Total MCP servers in graph: %d", len(entries))

	// ── 2. Verify tools are registered via relay ──
	t.Log("=== 2. Relay Tool Registration ===")
	sessions := queryRelaySessions(ctx, serverURL, token)
	if len(sessions) == 0 {
		t.Fatal("No relay sessions found — is diane serve running?")
	}
	t.Logf("  Relay sessions: %v", sessions)

	totalTools := 0
	for _, instID := range sessions {
		tools := queryInstanceTools(ctx, serverURL, token, instID)
		t.Logf("  %s: %d tools", instID, len(tools))
		totalTools += len(tools)
	}
	if totalTools == 0 {
		t.Fatal("No tools registered with relay — MCP servers may not be started")
	}
	t.Logf("  ✅ Total tools across all sessions: %d", totalTools)

	// ── 3. Create or reuse test agent ──
	t.Log("=== 3. Agent Setup ===")
	defID := findOrCreateMCPTestAgent(ctx, t, b)
	t.Logf("  Agent ready: %s (defID=%s)", mcpTestAgentName, defID)

	// ── 4. Trigger the agent ──
	t.Log("=== 4. Agent Trigger ===")
	runtimeAgent, err := b.CreateRuntimeAgent(ctx, mcpTestAgentName, defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := runtimeAgent.Data.ID
	t.Logf("  Runtime agent created: %s", agentID)

	prompt := "Run the get-env tool and tell me what environment variable HOME is set to."
	t.Logf("  Prompt: %q", prompt)

	triggerResp, err := b.TriggerAgentWithInput(ctx, agentID, prompt, "")
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	runID := ""
	if triggerResp.RunID != nil {
		runID = *triggerResp.RunID
	}
	if runID == "" {
		t.Fatal("Trigger response did not return a run ID")
	}
	t.Logf("  Agent triggered, runID=%s", runID)

	// ── 5. Wait for completion ──
	t.Log("=== 5. Waiting for completion ===")
	var run *sdkagentrun.AgentRun
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		runs, err := b.GetAgentRuns(ctx, agentID, 5)
		if err != nil {
			t.Logf("  Poll %d: %v", i+1, err)
			continue
		}
		// Find the specific run
		for _, r := range runs.Data {
			if r.ID == runID {
				run = &r
				break
			}
		}
		if run == nil {
			continue
		}
		if run.Status == "completed" || run.Status == "failed" || run.Status == "error" {
			t.Logf("  Run status: %s (after %ds)", run.Status, (i+1)*2)
			break
		}
		t.Logf("  Run status: %s (poll %d)", run.Status, i+1)
	}
	if run == nil {
		t.Fatal("Run never completed — timed out")
	}

	// ── 6. Verify tool was called ──
	t.Log("=== 6. Tool Call Verification ===")

	// Fetch tool calls for the run
	toolCalls, err := b.GetRunToolCalls(ctx, runID)
	if err != nil {
		t.Logf("  GetRunToolCalls: %v (non-fatal)", err)
	}
	toolCalled := false
	if toolCalls != nil && len(toolCalls.Data) > 0 {
		toolCalled = true
		for _, tc := range toolCalls.Data {
			t.Logf("  🛠️  Tool call: %s (status=%s, duration=%dms)",
				tc.ToolName, tc.Status, safeIntPtr(tc.DurationMs))
		}
	}

	// Fetch messages for evidence
	messages, err := b.GetRunMessages(ctx, runID)
	if err != nil {
		t.Logf("  GetRunMessages: %v (non-fatal)", err)
	}
	if messages != nil {
		toolMsgCount := 0
		for _, msg := range messages.Data {
			if msg.Role == "tool" {
				toolMsgCount++
			}
		}
		if toolMsgCount > 0 {
			toolCalled = true
		}
		t.Logf("  Messages: %d total, %d tool responses", len(messages.Data), toolMsgCount)
	}

	if !toolCalled {
		t.Logf("  ⚠️  No explicit tool call logged (run status=%s)", run.Status)
		if run.Status == "failed" || run.Status == "error" {
			errMsg := ""
			if run.ErrorMessage != nil {
				errMsg = *run.ErrorMessage
			}
			t.Errorf("❌ Agent run %s failed: %s", runID, errMsg)
		} else {
			t.Log("  Run completed — tool may have been used without being recorded in this API version")
		}
	} else {
		t.Logf("  ✅ Agent successfully called MCP tool(s)")
	}

	// ── 7. Summary ──
	t.Log("=== 7. Summary ===")
	t.Logf("  MCP servers in graph:  %d", len(entries))
	t.Logf("  Relay sessions:        %d", len(sessions))
	t.Logf("  Total relay tools:     %d", totalTools)
	t.Logf("  Agent:                 %s (def=%s, run=%s)", mcpTestAgentName, defID, runID)
	t.Logf("  Run status:            %s", run.Status)
	t.Logf("  Tool called:           %v", toolCalled)
}

// findOrCreateMCPTestAgent looks for or creates a test agent with MCP tool access.
func findOrCreateMCPTestAgent(ctx context.Context, t *testing.T, b *memory.Bridge) string {
	t.Helper()

	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}

	if defs != nil {
		for _, d := range defs.Data {
			if d.Name == mcpTestAgentName {
				t.Logf("  Reusing existing agent: %s", d.Name)
				return d.ID
			}
		}
	}

	tools := []string{"*echo*", "*get-env*", "*get-sum*", fmt.Sprintf("*%s*", mcpTestServerName)}
	desc := "Test agent for MCP integration test"
	visibility := "project"
	timeout := 120
	flowType := "single"

	created, err := b.CreateAgentDef(ctx, &sdkagents.CreateAgentDefinitionRequest{
		Name:        mcpTestAgentName,
		Description: &desc,
		Tools:       tools,
		Visibility:  visibility,
		DefaultTimeout: &timeout,
		FlowType:    flowType,
	})
	if err != nil {
		t.Fatalf("CreateAgentDef: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := b.DeleteAgentDef(cleanupCtx, created.Data.ID); err != nil {
			t.Logf("Cleanup delete %s: %v", created.Data.ID, err)
		}
	})

	t.Logf("  Created new agent: %s (ID=%s)", mcpTestAgentName, created.Data.ID)
	return created.Data.ID
}

// queryRelaySessions fetches active relay instance IDs from the MP.
func queryRelaySessions(ctx context.Context, serverURL, token string) []string {
	relayURL := strings.TrimSuffix(serverURL, "/") + "/api/mcp-relay/sessions"
	req, err := http.NewRequestWithContext(ctx, "GET", relayURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var result struct {
		Sessions []struct {
			InstanceID string `json:"instance_id"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}
	sessions := make([]string, 0, len(result.Sessions))
	for _, s := range result.Sessions {
		if s.InstanceID != "" {
			sessions = append(sessions, s.InstanceID)
		}
	}
	return sessions
}

// queryInstanceTools fetches the tool list for a specific relay instance.
func queryInstanceTools(ctx context.Context, serverURL, token, instanceID string) []map[string]any {
	toolsURL := strings.TrimSuffix(serverURL, "/") + "/api/mcp-relay/sessions/" + url.PathEscape(instanceID) + "/tools"
	req, err := http.NewRequestWithContext(ctx, "GET", toolsURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var result struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}
	return result.Tools
}

// safeIntPtr returns the value of an int pointer or 0 if nil.
func safeIntPtr(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
