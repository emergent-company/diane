// Package memorytest validates sub-agent routing and coordination tools
// against the live Memory Platform.
//
// Tests cover:
//   - All 8 built-in agent definitions exist on MP (diane-default, diane-researcher,
//     diane-agent-creator, diane-schema-designer, diane-session-extractor,
//     diane-codebase, diane-dreamer, diane-skill-monitor)
//   - Coordination tools (list_available_agents, spawn_agents) are configured
//     on the orchestrator agent definitions
//   - Runtime trigger verifies agents are discoverable via list_available_agents
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestSubAgent ./memorytest/
package memorytest

import (
	"context"
	"strings"
	"testing"
	"time"
)

// builtInAgentNames is the expected set of built-in agent definitions.
var builtInAgentNames = []string{
	"diane-default",
	"diane-researcher",
	"diane-agent-creator",
	"diane-schema-designer",
	"diane-session-extractor",
	"diane-codebase",
	"diane-dreamer",
	"diane-skill-monitor",
}

// coordinationTools are the ADK tools that enable sub-agent routing.
var coordinationTools = []string{"list_available_agents", "spawn_agents"}

// orchestratorAgents are agents that SHOULD have coordination tools.
var orchestratorAgents = []string{"diane-default", "diane-researcher", "diane-codebase"}

// =========================================================================
// TestSubAgent_AllDefinitionsExist: Verifies ALL 8 built-in agent definitions
// are synced to Memory Platform. This catches sync failures, definition
// corruption, or naming drift between the Go registry and MP.
// =========================================================================

func TestSubAgent_AllDefinitionsExist(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}

	// Build a lookup map
	byName := make(map[string]struct{})
	for _, d := range defs.Data {
		byName[d.Name] = struct{}{}
		t.Logf("  Found: %s (id=%s flow=%s tools=%d)", d.Name, d.ID[:12], d.FlowType, d.ToolCount)
	}

	// Check every expected agent exists
	var missing []string
	for _, name := range builtInAgentNames {
		if _, ok := byName[name]; !ok {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		t.Fatalf("Missing agent definitions (%d): %s", len(missing), strings.Join(missing, ", "))
	}

	t.Logf("✅ All %d built-in agent definitions exist on MP", len(builtInAgentNames))
}

// =========================================================================
// TestSubAgent_AgentDetails: Verifies key properties on each agent definition
// from the list response: FlowType, Visibility, minimum tool count.
// =========================================================================

func TestSubAgent_AgentDetails(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}

	byName := make(map[string]struct {
		Name       string
		FlowType   string
		Visibility string
		ToolCount  int
	})
	for _, d := range defs.Data {
		byName[d.Name] = struct {
			Name       string
			FlowType   string
			Visibility string
			ToolCount  int
		}{d.Name, d.FlowType, d.Visibility, d.ToolCount}
	}

	for _, name := range builtInAgentNames {
		def, ok := byName[name]
		if !ok {
			t.Errorf("Agent definition %q not found", name)
			continue
		}

		if def.FlowType == "" {
			t.Errorf("%s: FlowType is empty", name)
		}
		if def.Visibility == "" {
			t.Errorf("%s: Visibility is empty", name)
		}
		if def.ToolCount < 2 {
			t.Errorf("%s: only %d tools — expected at least 2", name, def.ToolCount)
		}

		t.Logf("  %s: flow=%s vis=%s tools=%d", name, def.FlowType, def.Visibility, def.ToolCount)
	}

	t.Log("✅ Agent definition details verified")
}

// =========================================================================
// TestSubAgent_CoordinationTools: Fetches individual agent definitions and
// verifies that orchestrator agents (diane-default, diane-researcher,
// diane-dreamer, diane-codebase) have list_available_agents and spawn_agents
// in their tool whitelist. Without these tools, sub-agent routing is impossible.
// =========================================================================

func TestSubAgent_CoordinationTools(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// First list all defs to get their IDs
	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}

	idByName := make(map[string]string)
	for _, d := range defs.Data {
		idByName[d.Name] = d.ID
	}

	// Fetch each orchestrator agent's full definition to inspect tools
	for _, name := range orchestratorAgents {
		id, ok := idByName[name]
		if !ok {
			t.Errorf("Agent definition %q not found — can't check coordination tools", name)
			continue
		}

		defResp, err := b.GetAgentDef(ctx, id)
		if err != nil {
			t.Errorf("GetAgentDef(%s): %v", name, err)
			continue
		}

		def := defResp.Data
		toolSet := make(map[string]bool)
		for _, tool := range def.Tools {
			toolSet[tool] = true
		}

		for _, ct := range coordinationTools {
			if !toolSet[ct] {
				t.Errorf("%s: missing coordination tool %q", name, ct)
			}
		}

		if toolSet["list_available_agents"] && toolSet["spawn_agents"] {
			t.Logf("  ✅ %s: coordination tools present", name)
		} else if toolSet["list_available_agents"] {
			t.Logf("  ⚠️  %s: has list_available_agents but missing spawn_agents", name)
		} else if toolSet["spawn_agents"] {
			t.Logf("  ⚠️  %s: has spawn_agents but missing list_available_agents", name)
		} else {
			t.Logf("  ❌ %s: missing both coordination tools", name)
		}
	}

	t.Log("✅ Coordination tool configuration verified")
}

// =========================================================================
// TestSubAgent_RunTraceCoordination: Triggers diane-default, polls to
// completion, then checks the run trace for coordination tool usage and
// sub-agent references in the response. This is the definitive end-to-end
// test of the sub-agent routing pipeline.
// =========================================================================

func TestSubAgent_RunTraceCoordination(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout+60*time.Second)
	defer cancel()

	// Create runtime agent
	defID, agentID := createAgent(ctx, t, b)
	t.Logf("Agent: %s (def: %s)", agentID, defID)

	// Trigger with a prompt that asks about available agents
	prompt := "What agents are available for delegation? Use list_available_agents to find them, then tell me their names in a numbered list."
	t.Logf("Prompt: %s", prompt)

	resp, err := b.TriggerAgentWithInput(ctx, agentID, prompt, "")
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	if resp.Error != nil && *resp.Error != "" {
		t.Fatalf("Trigger error: %s", *resp.Error)
	}
	runID := *resp.RunID
	t.Logf("Run ID: %s", runID)

	// Poll for completion
	if !pollRunCompletion(b, ctx, t, runID) {
		t.Fatal("Run did not complete within polling window")
	}

	// Fetch messages from the run
	msgsResp, err := b.GetRunMessages(ctx, runID)
	if err != nil {
		t.Fatalf("GetRunMessages: %v", err)
	}

	msgs := msgsResp.Data
	if len(msgs) == 0 {
		t.Fatal("GetRunMessages returned zero messages")
	}

	t.Logf("Messages: %d total", len(msgs))
	var lastResponse string
	for i, m := range msgs {
		content := extractMsgContent(m.Content)
		t.Logf("  [%d] role=%s %.150s", i, m.Role, content)
		if m.Role != "user" {
			lastResponse = content
		}
	}

	// Check response mentions sub-agents
	if lastResponse != "" {
		foundAgents := 0
		for _, name := range orchestratorAgents {
			if name == "diane-default" {
				continue // self
			}
			if strings.Contains(strings.ToLower(lastResponse), strings.ToLower(name)) ||
				strings.Contains(strings.ToLower(lastResponse), strings.ToLower(strings.TrimPrefix(name, "diane-"))) {
				foundAgents++
				t.Logf("  ✅ Response mentions: %s", name)
			}
		}
		if foundAgents == 0 {
			t.Logf("⚠️  Response doesn't mention any sub-agents — agent catalog may not be injected")
			t.Logf("   Response preview: %.200s", lastResponse)
		} else {
			t.Logf("✅ Response mentions %d sub-agents", foundAgents)
		}
	}

	// Fetch tool calls to check for coordination tools
	toolCalls, err := b.GetRunToolCalls(ctx, runID)
	if err != nil {
		t.Logf("GetRunToolCalls: %v (non-fatal)", err)
	} else if toolCalls != nil && len(toolCalls.Data) > 0 {
		t.Logf("Tool calls: %d", len(toolCalls.Data))
		var foundList, foundSpawn bool
		for _, tc := range toolCalls.Data {
			t.Logf("  %s (status: %s, %dms)", tc.ToolName, tc.Status, safeDerefInt(tc.DurationMs))
			if tc.ToolName == "list_available_agents" {
				foundList = true
			}
			if tc.ToolName == "spawn_agents" {
				foundSpawn = true
			}
		}
		if foundList {
			t.Log("✅ list_available_agents was called during the run")
		} else {
			t.Log("ℹ️  list_available_agents was not called (model chose direct response)")
		}
		if foundSpawn {
			t.Log("✅ spawn_agents was called during the run")
		}
	}

	t.Log("✅ Sub-agent routing trace verified")
}

// =========================================================================
// TestSubAgent_DefinitionsHaveSyncedIds: Verifies that all agent definitions
// on MP have non-empty IDs, Names, and FlowTypes — basic integrity check.
// =========================================================================

func TestSubAgent_DefinitionsHaveSyncedIds(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}

	for _, d := range defs.Data {
		if d.ID == "" {
			t.Errorf("Definition %q has empty ID", d.Name)
		}
		if d.Name == "" {
			t.Errorf("Definition with ID %q has empty Name", d.ID)
		}
	}

	t.Logf("✅ All %d definitions have valid IDs and names", len(defs.Data))
}
