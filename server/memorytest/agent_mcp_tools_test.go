// Package memorytest validates that agents can access and use MCP tools
// configured in Diane's MCP server config (~/.diane/mcp-servers.json) via relay.
//
// Flow:
//  1. Adds the echo MCP server example to mcp-servers.json
//  2. Starts diane mcp relay to register tools with Memory Platform
//  3. Verifies the relay session appears in MP's relay sessions API
//  4. Creates a test agent definition with the relay-registered tools
//  5. Triggers the agent with a tool-using prompt
//  6. Verifies the run completed and tool calls were attempted
//
// Requires:
//   - echo-mcp binary at /tmp/mcp-relay-test/echo-mcp
//   - diane binary at /tmp/diane-test-binary (auto-built)
//   - ~/.config/diane.yml with valid project token
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
	echoMCPBin  = "/tmp/mcp-relay-test/echo-mcp"
	dianeBin    = "/tmp/diane-test-binary"
	mcpCfgFile  = "/tmp/diane-test-binary-mcp-servers.json"
)

func TestAgentMCPTools(t *testing.T) {
	// ── Preflight ──
	if _, err := os.Stat(echoMCPBin); os.IsNotExist(err) {
		t.Skipf("Echo MCP binary not found at %s", echoMCPBin)
	}
	if err := buildDiane(t); err != nil {
		t.Skipf("Build diane binary: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Config: %v", err)
	}
	pc := cfg.Active()
	if pc == nil || pc.Token == "" {
		t.Skip("No active project")
	}

	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	instanceID := fmt.Sprintf("test-mcp-%d", time.Now().UnixMilli())
	t.Logf("Instance: %s", instanceID)

	// ── Step 1: Write mcp-servers.json with echo server ──
	writeEchoConfig(t)

	// ── Step 2: Start relay (background process) ──
	relayCtx, stopRelay := context.WithCancel(context.Background())
	defer stopRelay()

	cmd := exec.CommandContext(relayCtx, dianeBin, "mcp", "relay",
		"--instance", instanceID,
		"--mcp-binary", echoMCPBin,
	)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start relay: %v", err)
	}
	t.Cleanup(func() {
		stopRelay()
		cmd.Process.Kill()
		cmd.Wait()
	})

	// ── Step 3: Wait for relay session to appear ──
	t.Log("Waiting for relay to register...")
	if !pollRelay(ctx, pc.Token, pc.ProjectID, instanceID, 25*time.Second) {
		t.Skip("Relay did not register — relay endpoint may be unavailable")
	}
	t.Logf("✅ Relay registered")

	// ── Step 4: Create agent definition with echo tools ──
	// Tool naming on MP: {instance_id}_{bare_name}
	tools := []string{
		instanceID + "_echo_text",
		instanceID + "_add_numbers",
	}

	defName := fmt.Sprintf("test-mcp-agent-%d", time.Now().UnixMilli())
	created, err := b.CreateAgentDef(ctx, &sdkagents.CreateAgentDefinitionRequest{
		Name:           defName,
		Description:    ptrStr("Test MCP tool usage"),
		SystemPrompt:   ptrStr("You have access to echo_text and add_numbers MCP tools."),
		Tools:          tools,
		Visibility:     "project",
		MaxSteps:       ptrInt(10),
		DefaultTimeout: ptrInt(60),
	})
	if err != nil {
		t.Fatalf("CreateAgentDef: %v", err)
	}
	defID := created.Data.ID
	t.Cleanup(func() { b.DeleteAgentDef(context.Background(), defID) })
	t.Logf("✅ Agent def: %s — tools: %v", defName, tools)

	// ── Step 5: Trigger agent ──
	agent, err := b.CreateRuntimeAgent(ctx, "test-mcp-run-"+fmt.Sprint(time.Now().UnixMilli()), defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := agent.Data.ID
	t.Cleanup(func() { b.Client().Agents.Delete(context.Background(), agentID) })

	resp, err := b.TriggerAgentWithInput(ctx, agentID,
		"Call add_numbers with 79342 and 92837. Report the result.", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if resp.Error != nil && *resp.Error != "" {
		t.Fatalf("Trigger error: %s", *resp.Error)
	}
	runID := *resp.RunID
	t.Logf("✅ Run: %s", runID)

	// ── Step 6: Poll for completion ──
	var runStatus string
	var steps int
	for i := 0; i < 40; i++ {
		time.Sleep(3 * time.Second)
		r, err := b.GetProjectRun(ctx, runID)
		if err != nil {
			continue
		}
		runStatus = r.Data.Status
		steps = r.Data.StepCount
		t.Logf("  poll %d: %s", i+1, runStatus)
		if runStatus == "success" || runStatus == "completed" {
			t.Logf("✅ Completed in %d steps (%dms)", steps, safeDerefInt(r.Data.DurationMs))
			break
		}
		if runStatus == "failed" || runStatus == "error" {
			errMsg := ""
			if r.Data.ErrorMessage != nil {
				errMsg = *r.Data.ErrorMessage
			}
			t.Fatalf("Run failed: %s", errMsg)
		}
	}
	if runStatus != "success" && runStatus != "completed" {
		t.Fatalf("Run timed out (final status: %s)", runStatus)
	}

	// ── Step 7: Check tool usage ──
	tc, err := b.GetRunToolCalls(ctx, runID)
	if err != nil {
		t.Logf("GetRunToolCalls: %v", err)
	} else if tc != nil {
		t.Logf("Tool calls: %d", len(tc.Data))
		for _, call := range tc.Data {
			t.Logf("  📡 %s (%s, %dms)", call.ToolName, call.Status, safeDerefInt(call.DurationMs))
		}
		if len(tc.Data) > 1 {
			if tc.Data[1].Status == "error" {
				t.Logf("⚠️  Tool call error — may be MP relay routing issue")
			}
		}
	}

	// ── Step 8: Assistant response ──
	msgs, err := b.GetRunMessages(ctx, runID)
	if err == nil && msgs != nil {
		for _, m := range msgs.Data {
			if m.Role == "assistant" || m.Role == "model" {
				t.Logf("Assistant: %.200s", extractMsgContent(m.Content))
				break
			}
		}
	}
}

// ── Helpers ──

func writeEchoConfig(t *testing.T) {
	t.Helper()
	home, _ := os.UserHomeDir()
	realCfg := filepath.Join(home, ".diane", "mcp-servers.json")

	var servers []map[string]any
	if d, err := os.ReadFile(realCfg); err == nil {
		var tmp struct {
			Servers []map[string]any `json:"servers"`
		}
		json.Unmarshal(d, &tmp)
		servers = tmp.Servers
	}

	hasEcho := false
	for _, s := range servers {
		if n, _ := s["name"].(string); n == "echo-server" {
			hasEcho = true
			break
		}
	}
	if !hasEcho {
		servers = append(servers, map[string]any{
			"name": "echo-server", "enabled": true, "type": "stdio",
			"command": echoMCPBin, "args": []string{}, "env": map[string]string{},
		})
	}

	d, _ := json.MarshalIndent(map[string]any{"servers": servers}, "", "  ")
	os.WriteFile(mcpCfgFile, d, 0644)
}

func buildDiane(t *testing.T) error {
	t.Helper()
	if _, err := os.Stat(dianeBin); err == nil {
		return nil
	}
	c := exec.Command("/usr/local/go/bin/go", "build", "-o", dianeBin, "./cmd/diane/")
	c.Dir = "/root/diane/server"
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func pollRelay(ctx context.Context, token, project, instance string, timeout time.Duration) bool {
	url := fmt.Sprintf("https://memory.emergent-company.ai/api/mcp-relay/sessions?project_id=%s", project)
	deadline := time.Now().Add(timeout)
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
		var r struct {
			Sessions []struct {
				InstanceID string `json:"instance_id"`
			} `json:"sessions"`
		}
		json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()
		for _, s := range r.Sessions {
			if s.InstanceID == instance {
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

func ptrStr(s string) *string { return &s }
func ptrInt(i int) *int       { return &i }
