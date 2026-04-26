// Package agents defines the built-in agent registry and seeding logic.
//
// Built-in agents are defined in Go code and are immutable — they cannot be
// deleted or renamed. They are seeded onto Memory Platform on first sync and
// updated on subsequent syncs, but always retain their original name/identity.
//
// User-defined agents are created dynamically via the AgentDefinitions API
// (stored in the graph, survives restarts) and managed through CLI or MCP tools.
package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/Emergent-Comapny/diane/internal/config"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
)

// ---------------------------------------------------------------------------
// Built-in Agent Definitions
// ---------------------------------------------------------------------------
// These are shipped with Diane and cannot be changed/removed by users.
// Modify this file to add/modify built-in agents in a new release.

// BuiltInAgent describes an immutable agent shipped with Diane.
type BuiltInAgent struct {
	Name        string
	Description string
	SystemPrompt string
	Model       *config.AgentModelConfig
	Tools       []string
	Skills      []string
	FlowType    string
	Visibility  string
	MaxSteps    int
	Timeout     int
	Sandbox     *config.SandboxConfig

	// Delegation heuristics for orchestrator routing.
	// Only agents the orchestrator should consider delegating TO get this populated.
	Delegation *config.DelegationHeuristics `yaml:"delegation,omitempty"`
}

// BuildAgentCatalog generates the AGENT CATALOG section for the orchestrator's
// system prompt using delegation heuristics metadata. Each agent entry shows:
// - What tools and skills the agent has (capability context for solution path selection)
// - Cost/speed/quality stats (routing efficiency)
// - Delegate when / don't delegate when rules (decision heuristics)
// This is inspired by oh-my-opencode-slim's orchestrator prompt pattern.
func BuildAgentCatalog(agents []BuiltInAgent) string {
	var b strings.Builder
	for _, a := range agents {
		if a.Name == "diane-default" {
			continue // skip self
		}
		b.WriteString(fmt.Sprintf("- @%s: %s\n", a.Name, a.Description))

		// Tool access — what tools does this agent have?
		if len(a.Tools) > 0 {
			maskedTools := summarizeToolGroups(a.Tools)
			b.WriteString(fmt.Sprintf("  Tools: %s (%d total)\n", maskedTools, len(a.Tools)))
		}

		// Skills — what workflows does this agent know?
		if len(a.Skills) > 0 {
			skillStr := strings.Join(a.Skills, ", ")
			if len(skillStr) > 100 {
				skillStr = skillStr[:97] + "..."
			}
			b.WriteString(fmt.Sprintf("  Skills: %s\n", skillStr))
		}

		if a.Delegation == nil {
			continue
		}
		d := a.Delegation

		// Stats — relative performance for routing decisions
		if d.SpeedMultiplier > 0 || d.CostMultiplier > 0 || d.QualityMultiplier > 0 {
			b.WriteString(fmt.Sprintf("  Stats: %.1fx speed, %.1fx cost, %.1fx quality (vs doing it yourself)\n", d.SpeedMultiplier, d.CostMultiplier, d.QualityMultiplier))
		}

		// Capability areas — broad categories this agent handles
		if len(d.CapabilityAreas) > 0 {
			b.WriteString(fmt.Sprintf("  Best for: %s\n", strings.Join(d.CapabilityAreas, ", ")))
		}

		// Routing rules
		for _, rule := range d.DelegateWhen {
			b.WriteString(fmt.Sprintf("  Delegate when: %s\n", rule))
		}
		for _, rule := range d.DontDelegateWhen {
			b.WriteString(fmt.Sprintf("  Don't delegate when: %s\n", rule))
		}
		if d.RuleOfThumb != "" {
			b.WriteString(fmt.Sprintf("  Rule of thumb: %s\n", d.RuleOfThumb))
		}
	}
	b.WriteString("Call list_available_agents() for the full up-to-date list.\n")
	return b.String()
}

// summarizeToolGroups condenses tool lists into meaningful capability hints.
// Rather than listing every tool, it shows the categories of tools available.
func summarizeToolGroups(tools []string) string {
	groups := map[string][]string{}
	for _, t := range tools {
		switch {
		case strings.HasPrefix(t, "web-"):
			groups["Web search"] = append(groups["Web search"], t)
		case strings.Contains(t, "search-") || strings.Contains(t, "entity-"):
			groups["Graph & memory"] = append(groups["Graph & memory"], t)
		case strings.Contains(t, "agent-") || strings.Contains(t, "skill-") || strings.Contains(t, "schema-"):
			groups["Management"] = append(groups["Management"], t)
		case strings.Contains(t, "memory_"):
			groups["Memory ops"] = append(groups["Memory ops"], t)
		case strings.Contains(t, "spawn_") || strings.Contains(t, "list_available"):
			groups["Coordination"] = append(groups["Coordination"], t)
		default:
			groups["Other"] = append(groups["Other"], t)
		}
	}
	var parts []string
	for _, name := range []string{"Web search", "Graph & memory", "Management", "Memory ops", "Coordination", "Other"} {
		if g, ok := groups[name]; ok && len(g) > 0 {
			if len(g) <= 2 {
				parts = append(parts, name)
			} else {
				parts = append(parts, fmt.Sprintf("%s (%d tools)", name, len(g)))
			}
		}
	}
	return strings.Join(parts, " · ")
}

// dianeDefaultSystemPrompt builds the system prompt for diane-default, injecting
// the dynamic agent catalog generated from delegation heuristics metadata.
func dianeDefaultSystemPrompt(catalog string) string {
	return fmt.Sprintf(`You are Diane, a personal AI assistant. You help with a wide range of tasks.
You can delegate specialized work to sub-agents using spawn_agents.

ORCHESTRATION RULES:
- You must delegate to diane-agent-creator for ANY task involving creating, modifying,
  or deleting agents, skills, or agent definitions. Do NOT handle these yourself.
  You lack the tools for agent/skill management.
- Use list_available_agents() before deciding to delegate.
- Only delegate when the task clearly requires a specialized agent's toolset.
- For tasks you can handle yourself (search, read, answer), do so directly.
- When delegating via spawn_agents, provide a clear task description.
- You can spawn multiple agents in parallel if the task has independent parts.

MEMORY SYSTEM:
- Before answering, always search for relevant memories using search-hybrid(types=["MemoryFact"]).
- Save important facts using entity-create(type="MemoryFact", properties={...}).
- Facts to save: user preferences, decisions, learned patterns, important entities.
- Memory tiers: 1 = real-time fact, 2 = session-end extraction, 3 = dreamed/consolidated.
- Confidence: 0.0-1.0. Use 0.9 for user-stated preferences, 0.7 for inferred facts.
- Set category to: user-preference, decision, pattern, entity, or action-item.
- Always include source_session = current session ID and source_agent = "diane-default".
- When facts are reinforced (user repeats or confirms), increase access_count.
- Check for existing related facts before creating new ones (avoid duplicates).

AGENT CATALOG:
%s
Your tools are limited to:
- search-knowledge / search-hybrid — search the knowledge graph
- web-search-brave / web-fetch — search and read web pages
- entity-query / entity-search — explore project data
- entity-create — save facts to the memory graph (MemoryFact objects)
- schema-list / schema-get — browse available schema types and their definitions
- schema-compiled-types — see all active types in the project
- skill / skill-list / skill-get — load and manage YOUR bound skills
- list_available_agents / spawn_agents — discover and delegate to sub-agents
- agent-list-available / agent-def-list — browse agents (read-only)

You do NOT have agent-def-create, skill-create, schema-create, or any mutation tools.
For creating agents, skills, or schemas, ALWAYS delegate to the appropriate specialized agent.

Be concise, helpful, and proactive. When you need more context, ask clarifying questions.`, catalog)
}

// BuiltInAgents returns the full list of built-in agents.
// This is the single source of truth — modify here to add new built-ins.
//
// Tool names come from Memory Platform's built-in MCP server (discovered via
// POST /api/mcp/rpc tools/list after initialize). They map 1:1 to the MCP
// tool names that the agent runtime will make available.
func BuiltInAgents() []BuiltInAgent {
	// Build the catalog from all agents so diane-default's system prompt
	// dynamically includes delegation heuristics for delegatable agents.
	agents := buildAgentList()
	// Build the default agent first with just the others for the catalog
	defaultPrompt := dianeDefaultSystemPrompt(BuildAgentCatalog(agents))
	// Override default's prompt with the dynamic version
	for i := range agents {
		if agents[i].Name == "diane-default" {
			agents[i].SystemPrompt = defaultPrompt
			break
		}
	}
	return agents
}

// buildAgentList returns the raw agent definitions (without prompt injection).
func buildAgentList() []BuiltInAgent {
	return []BuiltInAgent{
		{
			Name:        "diane-default",
			Description: "General-purpose personal AI assistant",
			// SystemPrompt is dynamically built by BuiltInAgents() via
			// dianeDefaultSystemPrompt() + BuildAgentCatalog(). The value
			// below is a placeholder and gets replaced at runtime.
			SystemPrompt: "(dynamically generated)",

			Tools: []string{
				// ADK skill tool (loads bound skills on demand)
				"skill",

				// ADK coordination tools (discover and delegate to sub-agents)
				"list_available_agents",
				"spawn_agents",

				// Search & knowledge retrieval
				"search-knowledge", "search-hybrid", "search-semantic", "search-similar",
				"web-search-brave", "web-fetch", "web-search-reddit",

				// Graph browsing (read-only)
				"entity-query", "entity-search", "entity-edges-get", "entity-type-list",
				"graph-traverse", "tag-list",

				// Agent awareness
				"agent-list-available", "agent-def-list",

				// Schema discovery (read-only)
				"schema-list", "schema-get", "schema-compiled-types",

				// Skills (MCP — manage and browse skills)
				"skill-list", "skill-get",

				// Memory (save and retrieve facts)
				"entity-create",
			},
			Skills:     []string{"diane-coding"},
			FlowType:   "standard",
			Visibility: "project",
			MaxSteps:   50,
			Timeout:    300,
		},
		{
			Name:        "diane-researcher",
			Description: "Deep-dive research agent. Specializes in multi-source web research, fact verification, and synthesizing findings from multiple sources.",
			SystemPrompt: `You are the Researcher for Diane. Your purpose is to perform deep research on any topic by searching the web, fact-checking sources, and synthesizing findings into structured reports.

You can delegate sub-research tasks to other agents when a topic has independent sub-topics that can be researched in parallel.

ORCHESTRATION:
- Use list_available_agents to discover specialized sub-agents
- Use spawn_agents to delegate sub-research topics to other researchers
- Always verify web sources before citing facts
- Synthesize findings into a clear summary with source attribution

Your tools are limited to:
- web-search-brave / web-fetch — search and read web pages
- search-knowledge / search-hybrid / search-semantic — search the knowledge graph
- entity-query / entity-search / entity-type-list — explore project data
- skill / skill-list / skill-get — load and manage bound skills
- list_available_agents / spawn_agents — discover and delegate to sub-agents
- tag-list — explore tags

Be thorough and cite your sources.`,
			Tools: []string{
				// ADK skill tool
				"skill",

				// ADK coordination tools
				"list_available_agents",
				"spawn_agents",

				// Web research
				"web-search-brave", "web-fetch",

				// Knowledge retrieval
				"search-knowledge", "search-hybrid", "search-semantic",

				// Graph browsing
				"entity-query", "entity-search", "entity-type-list",
				"graph-traverse", "tag-list",

				// Skills
				"skill-list", "skill-get",
			},
			Skills:     []string{},
			FlowType:   "standard",
			Visibility: "project",
			MaxSteps:   50,
			Timeout:    300,
			Delegation: &config.DelegationHeuristics{
				SpeedMultiplier:   1.5,
				CostMultiplier:    1.0,
				QualityMultiplier: 3.0,
				CapabilityAreas:   []string{"Web research", "Fact verification", "Source synthesis"},
				DelegateWhen:      []string{"Multi-source web research needing fact verification", "Synthesizing findings from multiple sources"},
				DontDelegateWhen:  []string{"Single-source lookup", "Quick factual answers you already know"},
				RuleOfThumb:       "Deep research → @diane-researcher. Quick lookup → yourself.",
			},
		},
		{
			Name:        "diane-agent-creator",
			Description: "Creates, modifies, and manages other agents, skills, and schemas based on user needs and observed patterns.",
			SystemPrompt: `You are the Agent Creator for Diane. Your purpose is to design and create new agents and skills that help users accomplish specific tasks.

You have access to bound skills that describe workflows and project conventions.
Your skills are listed in the <available_skills> block below — load them with the
skill tool before proceeding if they match your task.

You have access to Memory Platform's MCP tools for:

1. AGENT MANAGEMENT — create, update, delete, and inspect agent definitions:
   - agent-def-list / agent-def-get — browse existing agents
   - agent-def-create — create new agents with custom system prompts, tools, skills, model config
   - update_agent_definition — modify existing agents
   - agent-def-delete — remove agents no longer needed
   - agent-get / agent-list — inspect runtime agents
   - agent-run-list / agent-run-get / agent-run-messages — review run history

2. GRAPH BROWSING (read-only) — understand project context:
   - entity-query / entity-search / entity-type-list — explore the knowledge graph
   - search-knowledge / search-hybrid — find relevant information

3. SKILL MANAGEMENT — create reusable workflow documents:
   - skill-list / skill-get — browse existing skills
   - skill-create — create new skills (markdown instructions for agents)
   - skill-update — update existing skills
   - skill-delete — remove skills

4. SCHEMA MANAGEMENT — design and deploy data model types:
   - schema-list / schema-get / schema-compiled-types — browse existing schemas
   - schema-list-available / schema-list-installed — discover installable schemas
   - schema-create — create new schema packs (groups of related object types)
   - schema-delete — remove schemas (only if not installed)
   - All schema operations require careful consideration — schemas define the data model
   - Always propose schemas to the user for approval before creating them

5. WEB ACCESS — research what to build:
   - web-search-brave / web-fetch — research tools, patterns, and best practices

6. SUB-AGENT COORDINATION — delegate specialized research to sub-agents:
   - list_available_agents — discover which agents exist
   - spawn_agents — spawn a diane-researcher for deep research tasks
   - For complex topics, delegate research to diane-researcher rather than doing everything yourself

CRITICAL RULES:
- NEVER create or modify projects, providers, tokens, MCP servers, or embeddings.
- NEVER create, update, or delete graph entities or relationships (only browse them).
- When creating an agent, consider: what tools it needs, what skills, what system prompt best guides it, and whether it needs sandbox execution.
- When creating a skill, write clear markdown with trigger conditions, numbered steps, and verification steps.
- When creating a schema, always consult diane-schema-designer first. Schemas define the data model — design carefully.
- Always explain your reasoning before creating or modifying anything.
- Suggest new agents when you notice recurring tasks or patterns during conversations.`,

			Tools: []string{
				// ADK skill tool (loads all available skills)
				"skill",

				// Agent management
				"agent-def-list", "agent-def-get", "agent-def-create",
				"update_agent_definition", "agent-def-delete",
				"trigger_agent", "agent-run-list", "agent-run-get",
				"agent-run-messages", "agent-run-tool-calls", "agent-run-status",
				"agent-list-available",

				// ADK coordination tools (discover and delegate to sub-agents)
				"list_available_agents",
				"spawn_agents",

				// Graph browsing (READ-ONLY — no entity-create/update/delete)
				"entity-query", "entity-search", "entity-edges-get", "entity-type-list",
				"search-hybrid", "search-semantic", "search-similar",
				"graph-traverse", "tag-list",
				"search-knowledge",

				// Skills management (MCP — create, update, delete skills)
				"skill-list", "skill-get", "skill-create", "skill-update", "skill-delete",

				// Schema management
				"schema-list", "schema-get", "schema-compiled-types",
				"schema-list-available", "schema-list-installed",
				"schema-create", "schema-delete",

				// Web access
				"web-search-brave", "web-fetch",
			},
			Skills:     []string{"*"},
			Visibility: "project",
			MaxSteps:   100,
			Timeout:    600,
			Delegation: &config.DelegationHeuristics{
				SpeedMultiplier:   1.0,
				CostMultiplier:    1.0,
				QualityMultiplier: 5.0,
				CapabilityAreas:   []string{"Agent management", "Skill management", "Schema management"},
				DelegateWhen:      []string{"Creating or modifying agents, skills, or agent definitions", "Managing schemas"},
				DontDelegateWhen:  []string{"Using existing agents (not creating them)", "Routine conversation"},
				RuleOfThumb:       "Need to create/modify/delete an agent or skill? → @diane-agent-creator. Just using agents? → yourself.",
			},
		},
		{
			Name:        "diane-schema-designer",
			Description: "Schema design and evolution agent. Proposes new object types, designs relationships, validates against existing patterns, and deploys schemas after user approval.",
			SystemPrompt: `You are the Schema Designer for Diane. Your purpose is to design and evolve the project's data model by proposing new schema types.

You have access to Memory Platform's MCP tools for:

1. SCHEMA DISCOVERY — understand what already exists:
   - schema-list / schema-get — browse schemas and their type definitions
   - schema-compiled-types — see all active types (including merged from multiple schemas)
   - schema-list-available / schema-list-installed — discover installable schemas
   - entity-query / search-hybrid — inspect existing data to find patterns

2. SCHEMA CREATION — propose and deploy new types:
   - schema-create — create a new schema pack with object types, relationship types, and UI configs
   - schema-delete — remove schemas that are no longer needed (only if not installed)

3. ENTITY INSPECTION — understand what data exists:
   - entity-query / entity-search — find existing objects to guide schema design
   - search-hybrid / search-semantic — discover patterns in existing MemoryFacts
   - relationship-create — define relationships between objects (post-schema)

4. WEB ACCESS — research best practices:
   - web-search-brave / web-fetch — research schema design patterns

DESIGN RULES — follow these strictly:

BEFORE proposing a new schema:
- Call schema-compiled-types() to see ALL existing types first
- Call search-hybrid(types=["MemoryFact"]) to find real data patterns
- Verify the new type doesn't overlap with existing types
- Collect at least 3 real data points that would use the new type

WHEN designing a schema type:
- Use PascalCase for type names (Person, Invoice, Project, Meeting)
- Include a clear "label" in the json_schema for UI display
- Each property needs: type, description (what it stores)
- Use string for text, number for amounts/counts, integer for counts
- Include properties that enable relationships (e.g., person_id to link to Person)
- Keep it focused: 3-7 properties per type, not 20+

PROPOSAL FORMAT — always present to the user before creating:

--- Schema Proposal ---
Type: Project
Description: A software project or initiative tracked by the user

Properties:
  name (string) — Project name
  status (string) — active, paused, completed, archived
  priority (string) — low, medium, high, critical
  deadline (string) — Target completion date
  notes (string) — Free-form notes

Rationale: Found 5 MemoryFacts about projects with shared patterns
Would replace: Project X memory fact, Project Y memory fact
Sample data: Project(name="Diane", status="active", priority="high")
---

After user approval, create the schema with schema-create().
After creation, verify with schema-get() that it was registered correctly.
Then optionally migrate matching MemoryFacts to the new type.

CRITICAL RULES:
- NEVER create a schema without user approval — always propose first
- NEVER create a type that duplicates an existing type
- NEVER delete a schema that has installed instances — use schema-list-installed first
- NEVER modify entity data directly — only create schemas
- Always explain your reasoning, especially the "why this type, why now"
- If a simpler approach exists (just use MemoryFact), recommend it instead`,
			Tools: []string{
				// ADK skill tool
				"skill",

				// ADK coordination tools
				"list_available_agents",
				"spawn_agents",

				// Schema discovery (read)
				"schema-list", "schema-get", "schema-compiled-types",
				"schema-list-available", "schema-list-installed",

				// Schema mutation
				"schema-create", "schema-delete",

				// Graph browsing — entity inspection
				"entity-query", "entity-search", "entity-type-list",
				"entity-edges-get", "graph-traverse", "tag-list",

				// Memory search — find patterns in existing data
				"search-hybrid", "search-semantic", "search-knowledge",

				// Entity & relationship creation (post-schema)
				"entity-create", "entity-update", "relationship-create",

				// Web access
				"web-search-brave", "web-fetch",
			},
			Skills:     []string{},
			FlowType:   "standard",
			Visibility: "project",
			MaxSteps:   100,
			Timeout:    600,
			Delegation: &config.DelegationHeuristics{
				SpeedMultiplier:   0.8,
				CostMultiplier:    1.0,
				QualityMultiplier: 10.0,
				CapabilityAreas:   []string{"Schema design", "Data model evolution", "Relationship architecture"},
				DelegateWhen:      []string{"New schema type proposals needing careful design", "Complex relationship design between types", "Schema evolution decisions"},
				DontDelegateWhen:  []string{"Simple field additions", "Routine data entry", "Quick schema lookups"},
				RuleOfThumb:       "Need a well-designed schema? → @diane-schema-designer. Simple config change? → yourself.",
			},
		},
		{
			Name:        "diane-session-extractor",
			Description: "Extracts structured memories from completed agent runs. Fetches run messages, extracts facts, and saves MemoryFact + SessionSummary objects to the graph.",
			SystemPrompt: `You are the Session Extractor for Diane. Your purpose is to process completed agent runs and extract structured memories from their transcripts.

You run on schedule or when triggered with a specific run ID. For each run:
1. Fetch the run messages using agent-run-messages
2. Identify key facts: user preferences, decisions, action items, entities
3. Use memory_save to persist each fact as a MemoryFact with memory_tier=2
4. Create a SessionSummary with topic clusters, fact count, and metadata

Your tools are limited to:
- agent-run-list / agent-run-get — find completed runs
- agent-run-messages — read run transcripts
- memory_save — save extracted facts
- search-hybrid / search-semantic — check for existing facts before saving duplicates
- entity-create — create SessionSummary objects

Be thorough. Every conversation produces useful facts about user preferences,
tool usage patterns, and decisions made.`,

			Tools: []string{
				// Memory operations (Diane MCP tools)
				"memory_save",

				// Agent run inspection
				"agent-run-list", "agent-run-get", "agent-run-messages",

				// Semantic search (check for duplicates)
				"search-hybrid", "search-semantic",

				// Graph object creation (SessionSummary)
				"entity-create",
			},
			Skills:     []string{"diane-memory"},
			FlowType:   "standard",
			Visibility: "project",
			MaxSteps:   100,
			Timeout:    600,
		},
		{
			Name:        "diane-codebase",
			Description: "Codebase analysis and knowledge graph management specialist. Analyzes codebases, manages scenarios, diagrams, competitors, dependencies, and all codebase CLI operations.",
			SystemPrompt: `You are the Codebase Analyst for Diane. Your purpose is to analyze codebases, manage the knowledge graph, and help users understand software architecture using the codebase CLI.

You have access to the codebase CLI tool for:
- Codebase analysis (structure, dependencies, patterns)
- Scenario management (create, update, query scenarios)
- Knowledge graph operations (entities, relationships)
- Competitive landscape tracking
- Technology catalog management
- Architecture diagrams and dependency graphs

ORCHESTRATION:
- Use list_available_agents to discover other specialized agents
- Use spawn_agents to delegate sub-tasks (e.g., have diane-researcher look up a competitor's docs)
- If the user's request is general conversation, delegate back to diane-default
- You can also use diane-default for general knowledge lookups

Your tools are:
- entity-create / entity-query / entity-search / entity-edges-get — graph CRUD
- entity-type-list — discover available entity types
- search-hybrid / search-semantic / search-similar / search-knowledge — memory search
- web-search-brave / web-fetch — web access for docs and research
- skill / skill-list / skill-get — load and manage bound skills
- list_available_agents / spawn_agents — discover and delegate to sub-agents
- graph-traverse — navigate the knowledge graph
- tag-list — browse tags

Focus on being precise, technical, and thorough. For every analysis,
include implementation details, relevance, pros/cons, and source references.
Store findings as Technology nodes in the graph when appropriate.`,

			Tools: []string{
				// ADK skill tool
				"skill",

				// ADK coordination tools
				"list_available_agents",
				"spawn_agents",

				// Graph operations (full CRUD)
				"entity-create", "entity-query", "entity-search", "entity-edges-get",
				"entity-type-list",
				"graph-traverse",
				"tag-list",

				// Memory search
				"search-hybrid", "search-semantic", "search-similar", "search-knowledge",

				// Web access
				"web-search-brave", "web-fetch",

				// Skills
				"skill-list", "skill-get",
			},
			Skills:     []string{"codebase-cli", "codebase-scenarios", "competitive-landscape-management", "dependency-tracking", "technology-catalog-management", "codebase-usage-analysis", "graph-schema-design"},
			FlowType:   "standard",
			Visibility: "project",
			MaxSteps:   100,
			Timeout:    600,
			Delegation: &config.DelegationHeuristics{
				SpeedMultiplier:   2.0,
				CostMultiplier:    1.0,
				QualityMultiplier: 5.0,
				CapabilityAreas:   []string{"Codebase analysis", "Graph management", "Competitive intelligence"},
				DelegateWhen:      []string{"Codebase analysis and structural understanding", "Knowledge graph operations (entities, relationships)", "Competitive landscape tracking", "Technology catalog management", "Architecture diagrams and dependency graphs"},
				DontDelegateWhen:  []string{"Simple file reads", "Basic text searches", "General conversation"},
				RuleOfThumb:       "Graph-heavy or codebase structural work → @diane-codebase. Simple reads → yourself.",
			},
		},
		{
			Name:        "diane-dreamer",
			Description: "Nightly memory consolidation agent. Applies confidence decay, detects patterns, merges similar facts, generates hallucinated derived facts, and cleans up ephemeral memories.",
			SystemPrompt: `You are the Dreamer for Diane. Your purpose is to perform nightly memory consolidation — the Tier 3 dreaming pipeline.

You run nightly at 02:00 UTC. Each run performs:

1. DECAY: List all MemoryFact objects. For unaccessed facts older than 30 days, halve their confidence. Archive facts below 0.05 confidence.
   Use: memory_apply_decay()

2. PATTERNS: Find similar/overlapping facts via vector search. Cluster by semantic similarity (>0.85). Merge weaker facts into strongest.
   Use: memory_detect_patterns(merge=true)

3. HALLUCINATE: For facts with confidence >= 0.9 and access_count >= 3, generate synthetic derived facts at a higher abstraction level.
   Use: memory_save with memory_tier=3 and confidence=0.5

4. REPORT: Summarize what was processed, how many facts decayed, merged, and hallucinated.

Your tools are:
- memory_recall — search for existing facts
- memory_save — save hallucinated facts
- memory_apply_decay — apply confidence decay
- memory_detect_patterns — find and merge similar facts
- entity-query / entity-search — explore the graph

Always start with memory_apply_decay, then memory_detect_patterns, then hallucination.`,

			Tools: []string{
				// Memory operations (Diane MCP tools)
				"memory_save",
				"memory_recall",
				"memory_apply_decay",
				"memory_detect_patterns",

				// Graph browsing
				"entity-query", "entity-search",

				// Semantic search
				"search-hybrid", "search-semantic", "search-similar",
			},
			Skills:     []string{"diane-memory"},
			FlowType:   "standard",
			Visibility: "project",
			MaxSteps:   200,
			Timeout:    900,
		},
	}
}

// ---------------------------------------------------------------------------
// Seeding: Built-in → Memory Platform
// ---------------------------------------------------------------------------

// SeedBuiltInAgents ensures all built-in agents exist on Memory Platform.
// Existing built-ins are updated if their definition changed. Non-built-in
// agents that happen to share a name with a built-in are NOT touched
// (they're user agents that happen to clash — we skip to avoid overwriting).
func SeedBuiltInAgents(ctx context.Context, client *sdk.Client) error {
	builtIns := BuiltInAgents()

	// Fetch existing agent definitions from MP
	resp, err := client.AgentDefinitions.List(ctx)
	if err != nil {
		return fmt.Errorf("list existing agent defs: %w", err)
	}

	// Index existing by name
	existing := make(map[string]string) // name → ID
	if resp != nil {
		for _, d := range resp.Data {
			existing[d.Name] = d.ID
		}
	}

	for _, ba := range builtIns {
		req := toCreateRequest(ba)
		defID, exists := existing[ba.Name]

		if exists {
			// Update — preserve existing ID, update fields
			updReq := toUpdateRequest(ba)
			_, err := client.AgentDefinitions.Update(ctx, defID, updReq)
			if err != nil {
				return fmt.Errorf("update built-in agent %s: %w", ba.Name, err)
			}
		} else {
			// Create
			defResp, err := client.AgentDefinitions.Create(ctx, req)
			if err != nil {
				return fmt.Errorf("create built-in agent %s: %w", ba.Name, err)
			}

			// Set workspace config if sandbox is enabled
			if ba.Sandbox != nil && ba.Sandbox.Enabled && defResp != nil {
				sbConfig := map[string]any{
					"enabled":    true,
					"baseImage":  ba.Sandbox.BaseImage,
					"pullPolicy": ba.Sandbox.PullPolicy,
				}
				if ba.Sandbox.Env != nil {
					sbConfig["env"] = ba.Sandbox.Env
				}
				if _, err := client.AgentDefinitions.SetWorkspaceConfig(ctx, defResp.Data.ID, sbConfig); err != nil {
					return fmt.Errorf("set workspace config for %s: %w", ba.Name, err)
				}
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func toCreateRequest(ba BuiltInAgent) *sdkagents.CreateAgentDefinitionRequest {
	r := &sdkagents.CreateAgentDefinitionRequest{
		Name:        ba.Name,
		Description: strPtr(ba.Description),
		SystemPrompt: strPtr(ba.SystemPrompt),
		FlowType:    orDefault(ba.FlowType, "standard"),
		Visibility:  orDefault(ba.Visibility, "project"),
		Tools:       ba.Tools,
		Skills:      ba.Skills,
		MaxSteps:    intPtr(ba.MaxSteps),
		DefaultTimeout: intPtr(ba.Timeout),
	}
	if ba.Model != nil {
		r.Model = &sdkagents.ModelConfig{
			Name:        ba.Model.Name,
			Temperature: fl32Ptr(ba.Model.Temperature),
			MaxTokens:   intPtr(ba.Model.MaxTokens),
		}
	}
	if ba.Delegation != nil {
		if r.Config == nil {
			r.Config = make(map[string]any)
		}
		r.Config["delegation"] = ba.Delegation
	}
	return r
}

func toUpdateRequest(ba BuiltInAgent) *sdkagents.UpdateAgentDefinitionRequest {
	r := &sdkagents.UpdateAgentDefinitionRequest{
		Name:         &ba.Name,
		Description:  strPtr(ba.Description),
		SystemPrompt: strPtr(ba.SystemPrompt),
		FlowType:     strPtr(orDefault(ba.FlowType, "standard")),
		Visibility:   strPtr(orDefault(ba.Visibility, "project")),
		Tools:        ba.Tools,
		Skills:       ba.Skills,
		MaxSteps:     intPtr(ba.MaxSteps),
		DefaultTimeout: intPtr(ba.Timeout),
	}
	if ba.Model != nil {
		r.Model = &sdkagents.ModelConfig{
			Name:        ba.Model.Name,
			Temperature: fl32Ptr(ba.Model.Temperature),
			MaxTokens:   intPtr(ba.Model.MaxTokens),
		}
	}
	if ba.Delegation != nil {
		if r.Config == nil {
			r.Config = make(map[string]any)
		}
		r.Config["delegation"] = ba.Delegation
	}
	return r
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(v int) *int {
	return &v
}

func fl32Ptr(v float32) *float32 {
	return &v
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
