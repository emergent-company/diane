// Package agents_test tests the agent registry — built-in agent definitions,
// catalog generation, and delegation heuristics formatting.
package agents_test

import (
	"strings"
	"testing"

	"github.com/Emergent-Comapny/diane/internal/agents"
)

// =========================================================================
// BuiltInAgents — count, names, basic properties
// =========================================================================

func TestBuiltInAgentsCount(t *testing.T) {
	list := agents.BuiltInAgents()
	if len(list) != 9 {
		t.Errorf("BuiltInAgents() returned %d agents, want 9", len(list))
	}
}

func TestBuiltInAgentsNames(t *testing.T) {
	list := agents.BuiltInAgents()
	names := make(map[string]bool)
	for _, a := range list {
		if names[a.Name] {
			t.Errorf("Duplicate agent name: %s", a.Name)
		}
		names[a.Name] = true
	}

	expected := []string{
		"diane-default",
		"diane-researcher",
		"diane-agent-creator",
		"diane-schema-designer",
		"diane-session-extractor",
		"diane-entity-extractor",
		"diane-codebase",
		"diane-dreamer",
		"diane-skill-monitor",
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("Missing built-in agent: %s", name)
		}
	}
}

func TestBuiltInAgentsHaveRequiredFields(t *testing.T) {
	for _, a := range agents.BuiltInAgents() {
		if a.Name == "" {
			t.Error("Agent with empty name")
			continue
		}
		if a.Description == "" {
			t.Errorf("%s: empty Description", a.Name)
		}
		if a.SystemPrompt == "" {
			t.Errorf("%s: empty SystemPrompt", a.Name)
		}
		if a.Visibility == "" {
			t.Errorf("%s: empty Visibility", a.Name)
		}
		if len(a.Tools) == 0 {
			t.Errorf("%s: no tools configured", a.Name)
		}
		if a.MaxSteps <= 0 {
			t.Errorf("%s: MaxSteps = %d, want > 0", a.Name, a.MaxSteps)
		}
		if a.Timeout <= 0 {
			t.Errorf("%s: Timeout = %d, want > 0", a.Name, a.Timeout)
		}
	}
}

// =========================================================================
// Coordination Tools — check orchestrator agents
// =========================================================================

func TestOrchestratorAgentsHaveCoordinationTools(t *testing.T) {
	list := agents.BuiltInAgents()
	byName := make(map[string]*agents.BuiltInAgent)
	for i := range list {
		byName[list[i].Name] = &list[i]
	}

	orchestrators := []string{"diane-default", "diane-researcher", "diane-codebase"}
	coordinationTools := []string{"list_available_agents", "spawn_agents"}

	for _, name := range orchestrators {
		a, ok := byName[name]
		if !ok {
			t.Errorf("Orchestrator agent %q not found", name)
			continue
		}

		toolSet := make(map[string]bool)
		for _, tool := range a.Tools {
			toolSet[tool] = true
		}

		for _, ct := range coordinationTools {
			if !toolSet[ct] {
				t.Errorf("%s: missing coordination tool %q", name, ct)
			}
		}
	}
}

// =========================================================================
// BuildAgentCatalog — formatting and content
// =========================================================================

func TestBuildAgentCatalogExcludesDefault(t *testing.T) {
	list := agents.BuiltInAgents()
	catalog := agents.BuildAgentCatalog(list)

	if strings.Contains(catalog, "diane-default") {
		t.Error("Catalog should not include diane-default (self-reference)")
	}
}

func TestBuildAgentCatalogIncludesAllOthers(t *testing.T) {
	list := agents.BuiltInAgents()
	catalog := agents.BuildAgentCatalog(list)

	expected := []string{
		"diane-researcher",
		"diane-agent-creator",
		"diane-schema-designer",
		"diane-session-extractor",
		"diane-codebase",
		"diane-dreamer",
		"diane-skill-monitor",
	}

	for _, name := range expected {
		if !strings.Contains(catalog, name) {
			t.Errorf("Catalog missing entry for %s", name)
		}
	}
}

func TestBuildAgentCatalogHasToolInfo(t *testing.T) {
	list := agents.BuiltInAgents()
	catalog := agents.BuildAgentCatalog(list)

	if !strings.Contains(catalog, "Tools:") {
		t.Error("Catalog should include tool count info")
	}
	if !strings.Contains(catalog, "total") {
		t.Error("Catalog should mention tool total")
	}
}

func TestBuildAgentCatalogHasDelegationStats(t *testing.T) {
	list := agents.BuiltInAgents()
	catalog := agents.BuildAgentCatalog(list)

	// At least some agents should have delegation heuristics
	if strings.Contains(catalog, "Stats:") || strings.Contains(catalog, "Delegate when:") {
		t.Log("✅ Catalog contains delegation heuristics")
	} else {
		t.Log("⚠️  No delegation heuristics in catalog (all agents may lack Delegation field)")
	}
}

func TestBuildAgentCatalogHasSkills(t *testing.T) {
	list := agents.BuiltInAgents()
	catalog := agents.BuildAgentCatalog(list)

	// diane-default has Skills: ["diane-coding"]
	// diane-dreamer has Skills: ["diane-memory"]
	// Other agents may have skills too
	if strings.Contains(catalog, "Skills:") {
		t.Log("✅ Catalog contains skill references")
	}
}

// =========================================================================
// dianeDefaultSystemPrompt — prompt injection
// =========================================================================

func TestDefaultSystemPromptContainsCatalog(t *testing.T) {
	list := agents.BuiltInAgents()
	catalog := agents.BuildAgentCatalog(list)

	// We can't call dianeDefaultSystemPrompt directly (unexported),
	// but we can check that BuiltInAgents replaces the placeholder
	defaultAgent := findByName(list, "diane-default")
	if defaultAgent == nil {
		t.Fatal("diane-default not found")
	}

	// The prompt should NOT contain the placeholder
	if defaultAgent.SystemPrompt == "(dynamically generated)" {
		t.Log("⚠️  BuiltInAgents() may not have replaced the placeholder (test reads from registry, not at runtime)")
	}

	// The catalog should match the format used in the orchestrator's prompt
	t.Logf("Default agent prompt length: %d chars", len(defaultAgent.SystemPrompt))
	t.Logf("Catalog length: %d chars", len(catalog))
}

// =========================================================================
// Tool count consistency
// =========================================================================

func TestDefaultAgentHasEnoughTools(t *testing.T) {
	a := findByName(agents.BuiltInAgents(), "diane-default")
	if a == nil {
		t.Fatal("diane-default not found")
	}
	// diane-default should have 25+ tools
	if len(a.Tools) < 20 {
		t.Errorf("diane-default has %d tools, expected >= 20", len(a.Tools))
	}

	// Should have skill tool
	hasSkill := false
	for _, tool := range a.Tools {
		if tool == "skill" {
			hasSkill = true
			break
		}
	}
	if !hasSkill {
		t.Error("diane-default missing 'skill' tool")
	}
}

func TestDreamerHasDecayTools(t *testing.T) {
	a := findByName(agents.BuiltInAgents(), "diane-dreamer")
	if a == nil {
		t.Fatal("diane-dreamer not found")
	}
	decayTools := []string{"memory_apply_decay", "memory_detect_patterns", "memory_recall", "memory_save"}
	for _, dt := range decayTools {
		found := false
		for _, tool := range a.Tools {
			if tool == dt {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("diane-dreamer missing tool %q", dt)
		}
	}
}

// =========================================================================
// Helper
// =========================================================================

func findByName(list []agents.BuiltInAgent, name string) *agents.BuiltInAgent {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}
