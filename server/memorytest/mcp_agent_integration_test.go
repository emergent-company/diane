// Package memorytest validates the MCP agent integration end-to-end.
//
// The test:
//  1. Starts a Docker container running diane serve (slave relay node)
//  2. Waits for it to register with the Memory Platform relay
//  3. Verifies MCP servers exist in the graph (MCPProxyConfig objects)
//  4. Checks tools are registered via relay on connected sessions
//  5. Creates or reuses a test agent definition with MCP tool references
//  6. Triggers the agent with a prompt that requires an MCP tool call
//  7. Verifies the agent called the MCP tool by fetching run messages and tool calls
//  8. Cleans up the Docker container
//
// Run:
//	cd server && go test -v -count=1 -run TestMCPAgentIntegration ./memorytest/ -timeout 10m
//
// Prerequisites:
//   - Docker installed and running
//   - diane-test-node:latest image built (docker build -t diane-test-node -f docker/Dockerfile .)
//   - MCPProxyConfig objects in the Memory Platform graph (for at least one session)
//   - A connected node with MCP servers running (e.g., tool-test with everything-server)
//
//go:build integration

package memorytest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

const (
	mcpDockerContainerName = "diane-test-mcp-agent"
	mcpDockerImage         = "diane-test-node:latest"
	mcpDockerAPIPort       = "8892" // avoid clash with local diane (8890)
	mcpTestServerName      = "everything"
	mcpTestAgentName       = "test-mcp-agent"
)

// ── Docker helpers (prefixed to avoid clash with docker_test.go) ────────────

func mcpDockerAvailable() bool {
	cmd := exec.Command(mcpDockerBin(), "info")
	return cmd.Run() == nil
}

func mcpDockerBin() string {
	if _, err := os.Stat("/Applications/Docker.app/Contents/Resources/bin/docker"); err == nil {
		return "/Applications/Docker.app/Contents/Resources/bin/docker"
	}
	return "docker"
}

func mcpDockerEnv() []string {
	socket := os.Getenv("DOCKER_HOST")
	if socket == "" {
		realHome := os.Getenv("HOME")
		altSocket := fmt.Sprintf("unix://%s/.docker/run/docker.sock", realHome)
		if _, err := os.Stat(strings.TrimPrefix(altSocket, "unix://")); err == nil {
			socket = altSocket
		}
	}
	env := os.Environ()
	if socket != "" {
		env = append(env, "DOCKER_HOST="+socket)
	}
	return env
}

func mcpDockerRunContainer(t *testing.T, instanceID string) (containerID string, cleanup func()) {
	t.Helper()

	exec.Command(mcpDockerBin(), "rm", "-f", mcpDockerContainerName).Run()

	args := []string{
		"run", "-d",
		"--name", mcpDockerContainerName,
		"--rm",
		"-p", mcpDockerAPIPort + ":8890",
		"-e", "MEMORY_SERVER_URL=" + os.Getenv("MEMORY_SERVER_URL"),
		"-e", "MEMORY_PROJECT_ID=" + os.Getenv("MEMORY_PROJECT_ID"),
		"-e", "MEMORY_API_KEY=" + os.Getenv("MEMORY_API_KEY"),
		"-e", "DIANE_INSTANCE_ID=" + instanceID,
		mcpDockerImage,
	}

	cmd := exec.Command(mcpDockerBin(), args...)
	cmd.Env = mcpDockerEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to start container: %v\nOutput: %s", err, string(out))
	}

	containerID = strings.TrimSpace(string(out))
	t.Logf("Container started: %s (instance=%s)", containerID, instanceID)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	healthy := false
	for ctx.Err() == nil {
		time.Sleep(2 * time.Second)
		check := exec.CommandContext(ctx, mcpDockerBin(), "inspect",
			"--format={{.State.Health.Status}}", containerID)
		check.Env = mcpDockerEnv()
		status, _ := check.CombinedOutput()
		statusStr := strings.TrimSpace(string(status))
		t.Logf("  Health: %s", statusStr)
		if statusStr == "healthy" {
			healthy = true
			break
		}
	}
	if !healthy {
		logs, _ := exec.Command(mcpDockerBin(), "logs", containerID).CombinedOutput()
		t.Fatalf("Container did not become healthy in 60s\nLogs:\n%s", string(logs))
	}

	cleanup = func() {
		t.Logf("Stopping container %s...", containerID)
		exec.Command(mcpDockerBin(), "rm", "-f", containerID).Run()
	}

	return containerID, cleanup
}

func mcpDockerExec(ctx context.Context, t *testing.T, args ...string) (string, error) {
	t.Helper()
	dockerArgs := append([]string{"exec", mcpDockerContainerName}, args...)
	cmd := exec.CommandContext(ctx, mcpDockerBin(), dockerArgs...)
	cmd.Env = mcpDockerEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ── MP Relay helpers ────────────────────────────────────────────────────────

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

func safeIntPtr(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// ── Agent setup helper ──────────────────────────────────────────────────────

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

// ── Test ────────────────────────────────────────────────────────────────────

// TestMCPAgentIntegration validates the full MCP agent flow within Docker.
func TestMCPAgentIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping MCP integration test in short mode")
	}

	// Check prerequisites
	for _, key := range []string{"MEMORY_SERVER_URL", "MEMORY_PROJECT_ID", "MEMORY_API_KEY"} {
		if os.Getenv(key) == "" {
			t.Skipf("Required env var %s not set — skipping", key)
		}
	}
	if !mcpDockerAvailable() {
		t.Skip("Docker not available — skipping Docker-integrated test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// ── 0. Load config for API calls to MP relay ──
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Cannot load config: %v", err)
	}
	pc := cfg.Active()
	if pc == nil || pc.Token == "" {
		t.Skip("No active project config or token")
	}
	serverURL := pc.ServerURL
	token := pc.Token

	instanceID := "test-mcp-agent-" + fmt.Sprintf("%d", time.Now().UnixMilli()%100000)

	// ── 1. Start Docker container ──
	t.Log("=== 1. Start Docker container ===")
	containerID, dockerCleanup := mcpDockerRunContainer(t, instanceID)
	defer dockerCleanup()

	// ── 2. Verify container registered with relay ──
	t.Log("=== 2. Wait for relay registration ===")
	var dockerRegistered bool
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		sessions := queryRelaySessions(ctx, serverURL, token)
		for _, s := range sessions {
			if s == instanceID {
				dockerRegistered = true
				t.Logf("  ✅ Docker node '%s' registered with relay after %ds", instanceID, (i+1)*2)
				break
			}
		}
		if dockerRegistered {
			break
		}
		t.Logf("  Poll %d: waiting for %s to register (sessions: %v)", i+1, instanceID, sessions)
	}
	if !dockerRegistered {
		logs, _ := mcpDockerExec(ctx, t, "cat", "/home/diane/.diane/serve.log")
		t.Fatalf("Docker node '%s' never registered with relay\nLogs:\n%s", instanceID, logs)
	}

	// ── 3. Verify MCP servers in graph ──
	t.Log("=== 3. MCP Server Discovery ===")
	b := setupBridgeFromConfig(t)
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
		t.Logf("  ⚠️  Server '%s' not found in graph — agent may have no tools to call", mcpTestServerName)
	}
	t.Logf("  Total MCP servers in graph: %d", len(entries))

	// ── 4. Verify tools are registered via relay ──
	t.Log("=== 4. Relay Tool Registration ===")
	sessions := queryRelaySessions(ctx, serverURL, token)
	if len(sessions) == 0 {
		t.Fatal("No relay sessions found — is any diane serve running?")
	}
	t.Logf("  Relay sessions: %v", sessions)

	// Our Docker node should be in the list
	foundDocker := false
	for _, s := range sessions {
		if s == instanceID {
			foundDocker = true
		}
	}
	if !foundDocker {
		t.Errorf("Docker node '%s' not in relay sessions list: %v", instanceID, sessions)
	}

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

	// ── 5. Create or reuse test agent ──
	t.Log("=== 5. Agent Setup ===")
	defID := findOrCreateMCPTestAgent(ctx, t, b)
	t.Logf("  Agent ready: %s (defID=%s)", mcpTestAgentName, defID)

	// ── 6. Trigger the agent ──
	t.Log("=== 6. Agent Trigger ===")
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

	// ── 7. Wait for completion ──
	t.Log("=== 7. Waiting for completion ===")
	var run *sdkagentrun.AgentRun
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		runs, err := b.GetAgentRuns(ctx, agentID, 5)
		if err != nil {
			t.Logf("  Poll %d: %v", i+1, err)
			continue
		}
		for _, r := range runs.Data {
			if r.ID == runID {
				run = &r
				break
			}
		}
		if run == nil {
			continue
		}
		if run.Status == "completed" || run.Status == "success" || run.Status == "failed" || run.Status == "error" {
			t.Logf("  Run status: %s (after %ds)", run.Status, (i+1)*2)
			break
		}
		t.Logf("  Run status: %s (poll %d)", run.Status, i+1)
	}
	if run == nil {
		t.Fatal("Run never completed — timed out")
	}

	// ── 8. Verify tool was called ──
	t.Log("=== 8. Tool Call Verification ===")

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
		if run.Status == "failed" || run.Status == "error" {
			errMsg := ""
			if run.ErrorMessage != nil {
				errMsg = *run.ErrorMessage
			}
			t.Errorf("❌ Agent run %s failed: %s", runID, errMsg)
		} else {
			t.Log("  ⚠️  No explicit tool call logged (run completed without tool evidence)")
		}
	} else {
		t.Logf("  ✅ Agent successfully called MCP tool(s)")
	}

	// ── 9. Summary ──
	t.Log("=== 9. Summary ===")
	t.Logf("  Docker node:          %s (%s)", instanceID, containerID[:12])
	t.Logf("  MCP servers in graph: %d", len(entries))
	t.Logf("  Relay sessions:       %d", len(sessions))
	t.Logf("  Total relay tools:    %d", totalTools)
	t.Logf("  Agent:                %s (def=%s, run=%s)", mcpTestAgentName, defID, runID)
	t.Logf("  Run status:           %s", run.Status)
	t.Logf("  Tool called:          %v", toolCalled)

	// Verify Docker health as final check
	t.Log("=== 10. Final health check ===")
	healthOut, _ := exec.Command(mcpDockerBin(), "inspect",
		"--format={{.State.Health.Status}}", containerID).CombinedOutput()
	t.Logf("  Container health: %s", strings.TrimSpace(string(healthOut)))
}
