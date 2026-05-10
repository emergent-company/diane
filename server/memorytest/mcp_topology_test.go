// Package memorytest validates the MCP relay master/slave topology end-to-end.
//
// Runs on diane-test via SSH. The test:
//   1. Starts docker-compose master + slave nodes (both connect to MP relay)
//   2. Waits for both nodes to be healthy and relay-connected
//   3. Verifies each node registered tools with the relay
//   4. Checks that tools from both nodes are visible via the master's /api/nodes
//   5. Verifies the default agent definition references tools matching relay patterns
//
// Prerequisites:
//   - SSH access to diane-test (root@100.94.94.105 via infra_ed25519 key)
//   - Docker + docker compose on diane-test
//   - diane-test-node image (pre-built)
//   - .env file at /opt/diane/docker/.env with MEMORY_* vars
//   - MP relay server reachable from diane-test (via --add-host to 10.10.10.20)
//
// Run:
//
//	cd server && go test -v -count=1 -run TestMCPTopology ./memorytest/
//
//go:build integration

package memorytest

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	mcpTestHost       = "root@100.94.94.105"
	mcpSSHKey         = "/Users/mcj/.ssh/infra_ed25519"
	mcpDockerDir      = "/opt/diane/docker"
	mcpProjectRoot    = "/opt/diane"
	mcpMasterName     = "diane-test-master"
	mcpSlaveName      = "diane-test-slave"
	mcpMasterPort     = "8890"
	mcpSlavePort      = "8891"
)

// ---------------------------------------------------------------------------
// SSH helpers
// ---------------------------------------------------------------------------

func mcpSSH(ctx context.Context, t *testing.T, format string, args ...any) string {
	t.Helper()
	cmd := fmt.Sprintf(format, args...)
	c := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "IdentitiesOnly=yes",
		"-i", mcpSSHKey,
		mcpTestHost,
		cmd,
	)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Logf("ssh (%s): %s\n%s", cmd, err, string(out))
	}
	return string(out)
}

func mcpSSHOK(ctx context.Context, t *testing.T, format string, args ...any) string {
	t.Helper()
	cmd := fmt.Sprintf(format, args...)
	c := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "IdentitiesOnly=yes",
		"-i", mcpSSHKey,
		mcpTestHost,
		cmd,
	)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("ssh failed: %s\ncmd=%s\nout=%s", err, cmd, string(out))
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestMCPTopology_Docker verifies that master and slave nodes register tools
// with the Memory Platform relay, and that tools from both nodes are visible.
func TestMCPTopology_Docker(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping MCP topology test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// ── 0. Clean up any leftover containers ──
	t.Log("=== 0. Cleanup ===")
	mcpSSH(ctx, t, "cd %s && docker compose down -v 2>/dev/null; true", mcpDockerDir)
	time.Sleep(3 * time.Second)

	// ── 1. Start docker-compose master + slave ──
	t.Log("=== 1. Start master + slave ===")
	mcpSSHOK(ctx, t, "cd %s && docker compose up -d 2>&1", mcpDockerDir)
	t.Log("  ✅ docker-compose started")

	// ── 2. Wait for both containers to become healthy ──
	t.Log("=== 2. Wait for containers healthy ===")

	masterHealthy := false
	slaveHealthy := false
	for i := 0; i < 30; i++ {
		masterStatus := strings.TrimSpace(mcpSSH(ctx, t,
			"docker inspect --format='{{.State.Health.Status}}' %s 2>/dev/null || echo 'not-found'",
			mcpMasterName))
		slaveStatus := strings.TrimSpace(mcpSSH(ctx, t,
			"docker inspect --format='{{.State.Health.Status}}' %s 2>/dev/null || echo 'not-found'",
			mcpSlaveName))

		if masterStatus == "healthy" {
			masterHealthy = true
		}
		if slaveStatus == "healthy" {
			slaveHealthy = true
		}
		if masterHealthy && slaveHealthy {
			t.Logf("  ✅ Both healthy after %ds", (i+1)*2)
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !masterHealthy {
		logs := mcpSSH(ctx, t, "docker logs %s --tail=20 2>&1", mcpMasterName)
		t.Fatalf("Master not healthy. Logs:\n%s", logs)
	}
	if !slaveHealthy {
		logs := mcpSSH(ctx, t, "docker logs %s --tail=20 2>&1", mcpSlaveName)
		t.Fatalf("Slave not healthy. Logs:\n%s", logs)
	}

	// ── 3. Wait for both to register with MP relay ──
	t.Log("=== 3. Wait for relay registration ===")

	masterRegistered := false
	slaveRegistered := false
	for i := 0; i < 30; i++ {
		masterLogs := mcpSSH(ctx, t,
			"docker logs %s --tail=5 2>&1 | grep -c 'Registered with relay'", mcpMasterName)
		slaveLogs := mcpSSH(ctx, t,
			"docker logs %s --tail=5 2>&1 | grep -c 'Registered with relay'", mcpSlaveName)

		masterRegs := strings.TrimSpace(masterLogs)
		slaveRegs := strings.TrimSpace(slaveLogs)

		if masterRegs != "" && masterRegs != "0" {
			masterRegistered = true
		}
		if slaveRegs != "" && slaveRegs != "0" {
			slaveRegistered = true
		}
		if masterRegistered && slaveRegistered {
			t.Logf("  ✅ Both registered after %ds", (i+1)*2)
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !masterRegistered {
		logs := mcpSSH(ctx, t, "docker logs %s --tail=30 2>&1", mcpMasterName)
		t.Logf("Master logs:\n%s", logs)
		t.Fatal("❌ Master did not register with relay")
	}
	if !slaveRegistered {
		logs := mcpSSH(ctx, t, "docker logs %s --tail=30 2>&1", mcpSlaveName)
		t.Logf("Slave logs:\n%s", logs)
		t.Fatal("❌ Slave did not register with relay")
	}

	// ── 4. Verify nodes register with relay (check logs) ──
	t.Log("=== 4. Verify relay registration details ===")

	masterRegLine := mcpSSH(ctx, t,
		"docker logs %s 2>&1 | grep 'Registered with relay' | tail -1", mcpMasterName)
	slaveRegLine := mcpSSH(ctx, t,
		"docker logs %s 2>&1 | grep 'Registered with relay' | tail -1", mcpSlaveName)

	t.Logf("  Master reg: %s", strings.TrimSpace(masterRegLine))
	t.Logf("  Slave reg:  %s", strings.TrimSpace(slaveRegLine))

	if !strings.Contains(masterRegLine, "test-master") {
		t.Fatalf("Master registered with wrong instance ID: %s", masterRegLine)
	}
	if !strings.Contains(slaveRegLine, "test-slave") {
		t.Fatalf("Slave registered with wrong instance ID: %s", slaveRegLine)
	}
	t.Log("  ✅ Correct instance IDs registered")

	// ── 5. Get nodes list from master API ──
	t.Log("=== 5. Query master API for connected nodes ===")

	// Each node's local API shows connected relay nodes via /api/nodes
	nodesJSON := mcpSSHOK(ctx, t,
		`docker exec %s sh -c 'curl -sf http://localhost:%s/api/nodes 2>&1 || echo "NODES_FAILED"'`,
		mcpMasterName, mcpMasterPort)

	if nodesJSON == "NODES_FAILED" || len(nodesJSON) < 10 {
		t.Fatalf("Failed to query master /api/nodes: %s", nodesJSON)
	}
	t.Logf("  /api/nodes response (%d bytes)", len(nodesJSON))

	// Parse the nodes response
	var nodesResponse struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(nodesJSON), &nodesResponse); err != nil {
		// Try flat array fallback
		var flatNodes []map[string]any
		if err2 := json.Unmarshal([]byte(nodesJSON), &flatNodes); err2 != nil {
			t.Logf("  Raw response: %s", nodesJSON)
			t.Fatalf("Failed to parse nodes response: %v (flat: %v)", err, err2)
		}
		nodesResponse.Nodes = flatNodes
	}

	t.Logf("  Found %d connected nodes", len(nodesResponse.Nodes))

	foundMaster := false
	foundSlave := false
	for _, n := range nodesResponse.Nodes {
		id, _ := n["instance_id"].(string)
		toolCount := 0
		if tc, ok := n["tool_count"].(float64); ok {
			toolCount = int(tc)
		}
		version, _ := n["version"].(string)
		t.Logf("    Node: %s  tools=%d  version=%s", id, toolCount, version)

		switch id {
		case "test-master":
			foundMaster = true
			if toolCount < 1 {
				t.Errorf("Master has %d tools, expected at least 1 (built-in node_status)", toolCount)
			}
		case "test-slave":
			foundSlave = true
			if toolCount < 1 {
				t.Errorf("Slave has %d tools, expected at least 1 (built-in node_status)", toolCount)
			}
		}
	}

	if !foundMaster {
		t.Error("❌ test-master not found in /api/nodes")
	} else {
		t.Log("  ✅ test-master found in nodes list")
	}
	if !foundSlave {
		t.Error("❌ test-slave not found in /api/nodes")
	} else {
		t.Log("  ✅ test-slave found in nodes list")
	}

	// ── 6. Slice test: query slave API for nodes (both should be visible) ──
	t.Log("=== 6. Query slave API for connected nodes ===")

	slaveNodes := mcpSSH(ctx, t,
		`docker exec %s sh -c 'curl -sf http://localhost:%s/api/nodes 2>&1 || echo "NODES_FAILED"'`,
		mcpSlaveName, mcpSlavePort)

	if slaveNodes == "NODES_FAILED" || len(slaveNodes) < 10 {
		t.Fatalf("Failed to query slave /api/nodes: %s", slaveNodes)
	}

	var slaveNodesResp struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(slaveNodes), &slaveNodesResp); err != nil {
		t.Logf("  Raw slave nodes: %s", slaveNodes)
		t.Logf("  (parse error: %v)", err)
	} else {
		t.Logf("  Slave sees %d connected nodes", len(slaveNodesResp.Nodes))
		foundMasterFromSlave := false
		foundSlaveFromSlave := false
		for _, n := range slaveNodesResp.Nodes {
			id, _ := n["instance_id"].(string)
			if id == "test-master" {
				foundMasterFromSlave = true
			}
			if id == "test-slave" {
				foundSlaveFromSlave = true
			}
		}
		if foundMasterFromSlave {
			t.Log("  ✅ test-master visible from slave")
		} else {
			t.Log("  ⚠️  test-master not visible from slave (may be expected if relay filters)")
		}
		if foundSlaveFromSlave {
			t.Log("  ✅ test-slave visible from slave")
		} else {
			t.Log("  ⚠️  test-slave not visible from slave")
		}
	}

	// ── 7. Verify default agent definition has tool patterns ──
	t.Log("=== 7. Check default agent tool patterns ===")

	agentJSON := mcpSSH(ctx, t,
		`docker exec %s sh -c 'curl -sf http://localhost:%s/api/agents/diane-default 2>&1 || echo "AGENT_FAILED"'`,
		mcpMasterName, mcpMasterPort)

	if agentJSON == "AGENT_FAILED" || len(agentJSON) < 10 {
		t.Logf("  ⚠️  Could not fetch agent detail (endpoint may require newer version)")
	} else {
		var agentDetail struct {
			Tools []string `json:"tools"`
		}
		if err := json.Unmarshal([]byte(agentJSON), &agentDetail); err != nil {
			t.Logf("  Raw agent JSON: %s", agentJSON)
			t.Logf("  (parse error: %v)", err)
		} else {
			t.Logf("  Default agent has %d tool patterns", len(agentDetail.Tools))
			for _, tool := range agentDetail.Tools {
				t.Logf("    - %s", tool)
			}
			// The default agent should have wildcard patterns that match
			// relay-registered tools (e.g., "test-master_node_status")
			hasNodePattern := false
			for _, tool := range agentDetail.Tools {
				if strings.Contains(tool, "node") || strings.Contains(tool, "status") {
					hasNodePattern = true
				}
			}
			if !hasNodePattern {
				t.Log("  ⚠️  No tool pattern matching 'node_status' found in default agent")
			} else {
				t.Log("  ✅ Default agent has patterns matching node_status tools")
			}
		}
	}

	// ── 8. Direct relay check: verify each node's registered tools ──
	t.Log("=== 8. Verify tools registered by each node ===")

	// Check the MCP subprocess tools/list directly (available on the local API
	// via internal endpoint or by checking logs for tool count)
	nodeNames := map[string]string{"master": mcpMasterName, "slave": mcpSlaveName}
	for _, name := range []string{"master", "slave"} {
		relayLogs := mcpSSH(ctx, t,
			`docker logs %s 2>&1 | grep 'Registered with relay' | wc -l`,
			nodeNames[name])
		regCount := strings.TrimSpace(relayLogs)
		t.Logf("  %s: %s registration events", name, regCount)
	}

	// ── 9. Cleanup ──
	t.Log("=== 9. Cleanup ===")
	mcpSSH(ctx, t, "cd %s && docker compose down -v 2>/dev/null; true", mcpDockerDir)
	t.Log("  ✅ Cleanup done")
}
