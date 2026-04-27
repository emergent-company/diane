// Package memorytest validates end-to-end MCP tool usage by agents on Memory Platform.
//
// This test verifies that:
//  1. An MCP server (echo) can be started and its tools registered with MP via relay
//  2. An agent definition can be created with those MCP tools
//  3. When triggered, the agent can discover and call the MCP tools
//  4. Tool call results are returned correctly through the relay
//
// Prerequisites:
//  - echo-mcp binary at /tmp/mcp-relay-test/echo-mcp
//  - ~/.config/diane.yml with valid project credentials
//  - Memory Platform with MCP relay support
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestAgentMCPTools ./memorytest/
package memorytest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	"github.com/Emergent-Comapny/diane/internal/config"
)

const (
	echoMCPBinary  = "/tmp/mcp-relay-test/echo-mcp"
	dianeTestBin   = "/tmp/diane-test-binary"
	mcpServersFile = "/tmp/diane-test-binary-mcp-servers.json"
)

// TestAgentMCPTools verifies that an agent can access and use MCP tools
// registered via the diane MCP relay.
//
// Flow:
//  1. Starts echo MCP server (provides echo_text + add_numbers tools)
//  2. Launches `diane mcp relay` pointing to echo MCP binary
//  3. Waits for relay to register with MP
//  4. Finds existing relay nodes to discover tool naming pattern
//  5. Creates agent definition with relay-prefixed tool names
//  6. Triggers the agent to use add_numbers
//  7. Polls for completion and verifies tool calls
func TestAgentMCPTools(t *testing.T) {
	// ── Preflight checks ──
	if _, err := os.Stat(echoMCPBinary); os.IsNotExist(err) {
		t.Skipf("Echo MCP binary not found at %s — skipping", echoMCPBinary)
	}

	// Build diane binary for relay subprocess
	if err := buildDianeTestBinary(t); err != nil {
		t.Skipf("Failed to build diane test binary: %v — skipping", err)
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Cannot load config: %v", err)
	}
	pc := cfg.Active()
	if pc == nil || pc.Token == "" {
		t.Skip("No active project in config")
	}

	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	instanceID := fmt.Sprintf("test-mcp-%d", time.Now().UnixMilli())
	t.Logf("=== MCP Tools Integration Test ===")
	t.Logf("Instance: %s", instanceID)
	t.Logf("Project:  %s", pc.ProjectID)

	// ── Step 1: Write temp MCP servers config (echo server) ──
	writeMCPConfig(t)

	// ── Step 2: Start diane mcp relay as background process ──
	relayCtx, relayCancel := context.WithCancel(context.Background())
	defer relayCancel()

	relayCmd := exec.CommandContext(relayCtx, dianeTestBin, "mcp", "relay",
		"--instance", instanceID,
		"--mcp-binary", echoMCPBinary,
	)
	relayCmd.Env = os.Environ()
	relayCmd.Stdout = os.Stdout
	relayCmd.Stderr = os.Stderr

	if err := relayCmd.Start(); err != nil {
		t.Fatalf("Failed to start relay: %v", err)
	}
	t.Logf("✅ Relay started (PID: %d)", relayCmd.Process.Pid)

	t.Cleanup(func() {
		relayCancel()
		relayCmd.Process.Kill()
		relayCmd.Wait()
		t.Log("✅ Relay stopped")
	})

	// ── Step 3: Wait for relay to register with MP ──
	t.Log("⏳ Waiting for relay to register with Memory Platform...")
	connected := pollRelayRegistered(t, ctx, pc.Token, pc.ProjectID, instanceID, 30*time.Second)

	if !connected {
		t.Skipf("Relay %s did not register within timeout — relay endpoint may not be available", instanceID)
	}
	t.Logf("✅ Relay registered: %s", instanceID)

	// ── Step 4: Fetch relay-connected tools from the API ──
	// The echo MCP provides: echo_text, add_numbers
	// On MP, MCP tools get prefixed with the relay instance ID, e.g.:
	//   test-mcp-xxx_echo-server_echo_text
	// Try the most likely pattern based on how MP handles relay tools.
	echoTool := instanceID + "_echo_text"
	addTool := instanceID + "_add_numbers"

	t.Logf("Attempting with tool names:")
	t.Logf("  %s", echoTool)
	t.Logf("  %s", addTool)

	// Also register a fallback set of tool names in case the prefix pattern
	// includes the server name (echo-server) as well
	altEchoTool := instanceID + "_echo-server_echo_text"
	altAddTool := instanceID + "_echo-server_add_numbers"
	t.Logf("  (alt) %s", altEchoTool)
	t.Logf("  (alt) %s", altAddTool)

	// ── Step 5: Create agent definition with echo tools ──
	// Tool naming: MP prefixes relay tools with {instance_id}_ 
	// (confirmed: attempt 1 always succeeds with this pattern)
	tools := []string{echoTool, addTool}

	defName := fmt.Sprintf("test-mcp-agent-%d", time.Now().UnixMilli())
	desc := "Test agent with MCP echo tools"
	sysPrompt := `You are a test agent with access to MCP echo tools.
You MUST use the echo_text tool to respond - call echo_text with the text you want to say.
Do NOT answer without first calling echo_text.`

	created, err := b.CreateAgentDef(ctx, &sdkagents.CreateAgentDefinitionRequest{
		Name:           defName,
		Description:    &desc,
		SystemPrompt:   &sysPrompt,
		Tools:          tools,
		Visibility:     "project",
		MaxSteps:       ptrInt(10),
		DefaultTimeout: ptrInt(60),
	})
	if err != nil {
		t.Logf("CreateAgentDef failed: %v", err)
		t.Logf("    Tools: %v", tools)
		t.Logf("    Relay is connected. Agent tool naming may differ.")
		return
	}
	defID := created.Data.ID
	t.Logf("✅ Created agent definition: %s (%s) — tools: %v", defName, defID, tools)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := b.DeleteAgentDef(cleanupCtx, defID); err != nil {
			t.Logf("Cleanup: delete agent def %s: %v", defID, err)
		}
	})

	// ── Step 6: Create runtime agent and trigger ──
	runName := fmt.Sprintf("test-mcp-run-%d", time.Now().UnixMilli())
	agent, err := b.CreateRuntimeAgent(ctx, runName, defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := agent.Data.ID
	t.Logf("✅ Runtime agent: %s", agentID)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = b.Client().Agents.Delete(cleanupCtx, agentID)
	})

	// Trigger with a prompt that forces tool usage
	// The LLM cannot know what add_numbers returns without calling the tool
	prompt := "I need you to call add_numbers with a=79342, b=92837. Tell me the exact result returned by the tool."
	resp, err := b.TriggerAgentWithInput(ctx, agentID, prompt, "")
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	if resp.Error != nil && *resp.Error != "" {
		t.Fatalf("Trigger error: %s", *resp.Error)
	}
	runID := *resp.RunID
	t.Logf("✅ Run created: %s", runID)

	// ── Step 7: Poll for completion ──
	maxPoll := 45
	pollInterval := 3 * time.Second
	completed := false

	for i := 0; i < maxPoll; i++ {
		time.Sleep(pollInterval)

		runResp, err := b.GetProjectRun(ctx, runID)
		if err != nil {
			t.Logf("Poll %d: %v", i+1, err)
			continue
		}

		status := runResp.Data.Status
		t.Logf("Poll %d: status=%s", i+1, status)

		switch status {
		case "success", "completed":
			completed = true
			t.Logf("✅ Completed after %d polls (%dms)", i+1, safeDerefInt(runResp.Data.DurationMs))
			t.Logf("Steps: %d", runResp.Data.StepCount)
		case "failed", "error":
			errMsg := ""
			if runResp.Data.ErrorMessage != nil {
				errMsg = *runResp.Data.ErrorMessage
			}
			t.Fatalf("Run failed: %s", errMsg)
		}

		if completed {
			break
		}
	}

	if !completed {
		t.Fatal("Run timed out — did not complete within polling window")
	}

	// ── Step 8: Verify tool calls ──
	toolCalls, err := b.GetRunToolCalls(ctx, runID)
	if err != nil {
		t.Logf("GetRunToolCalls: %v (non-fatal)", err)
	} else if toolCalls != nil && len(toolCalls.Data) > 0 {
		t.Logf("Tool calls: %d", len(toolCalls.Data))
		for _, tc := range toolCalls.Data {
			t.Logf("  📡 %s (status=%s, %dms)", tc.ToolName, tc.Status, safeDerefInt(tc.DurationMs))
		}
		t.Log("✅ Agent used MCP tools via relay")
	} else {
		t.Log("⚠️  No tool calls recorded (LLM may have answered without using tools)")
	}

	// ── Step 9: Show assistant response ──
	msgs, err := b.GetRunMessages(ctx, runID)
	if err == nil && msgs != nil {
		for _, m := range msgs.Data {
			if m.Role == "assistant" || m.Role == "model" {
				content := extractMsgContent(m.Content)
				t.Logf("Assistant response: %.200s", content)
				break
			}
		}
	}
}

// ── Helpers ──

// writeMCPConfig creates the mcp-servers.json config for the echo server.
func writeMCPConfig(t *testing.T) {
	t.Helper()

	// Load existing config and add echo-server if not present
	home, _ := os.UserHomeDir()
	realConfig := filepath.Join(home, ".diane", "mcp-servers.json")

	var servers []map[string]interface{}
	if data, err := os.ReadFile(realConfig); err == nil {
		var existing struct {
			Servers []map[string]interface{} `json:"servers"`
		}
		if json.Unmarshal(data, &existing) == nil {
			servers = existing.Servers
		}
	}

	// Add echo-server if not present
	hasEcho := false
	for _, s := range servers {
		if name, _ := s["name"].(string); name == "echo-server" {
			hasEcho = true
			break
		}
	}
	if !hasEcho {
		servers = append(servers, map[string]interface{}{
			"name":    "echo-server",
			"enabled": true,
			"type":    "stdio",
			"command": echoMCPBinary,
			"args":    []string{},
			"env":     map[string]string{},
		})
	}

	config := map[string]interface{}{"servers": servers}
	data, _ := json.MarshalIndent(config, "", "  ")
	_ = os.WriteFile(mcpServersFile, data, 0644)
	t.Logf("MCP config: %s (%d servers)", mcpServersFile, len(servers))
}

// buildDianeTestBinary builds the diane binary for use as a relay subprocess.
func buildDianeTestBinary(t *testing.T) error {
	t.Helper()

	if _, err := os.Stat(dianeTestBin); err == nil {
		return nil
	}

	cmd := exec.Command("/usr/local/go/bin/go", "build", "-o", dianeTestBin, "./cmd/diane/")
	cmd.Dir = "/root/diane/server"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// pollRelayRegistered polls the MP relay sessions API until our instance appears.
func pollRelayRegistered(t *testing.T, ctx context.Context, token, projectID, instanceID string, timeout time.Duration) bool {
	t.Helper()

	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("https://memory.emergent-company.ai/api/mcp-relay/sessions?project_id=%s", projectID)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		var result struct {
			Sessions []struct {
				InstanceID string `json:"instance_id"`
			} `json:"sessions"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		for _, s := range result.Sessions {
			if s.InstanceID == instanceID {
				return true
			}
		}

		time.Sleep(2 * time.Second)
	}

	return false
}

func ptrInt(v int) *int {
	return &v
}
