# Delegation Heuristics for Diane — Design

> **Goal:** Give Diane's orchestrator explicit cost/speed/quality awareness when delegating to sub-agents, so `spawn_agents` calls are informed by structured metadata instead of static prompt text.

**Architecture:** Agent definition metadata → dynamic system prompt injection → orchestrator uses stats to route decisions. No runtime changes to the spawn mechanism itself.

**Primary inspiration:** oh-my-opencode-slim's orchestrator prompt pattern where each agent has embedded speed/cost/quality multipliers + explicit Delegate When / Don't Delegate When rules + Rule of Thumb.

---

## Problem

Diane's current `diane-default` system prompt has a static "ORCHESTRATION RULES" section:

```
- Use list_available_agents() before deciding to delegate.
- Only delegate when the task clearly requires a specialized agent's toolset.
```

This is generic — the orchestrator has **no data** to differentiate *which* agent to pick for *what*. It can't know that diane-researcher costs 1/2 as much but takes 2x longer, or that diane-codebase is 5x better at graph queries but 3x more expensive.

## Proposed Solution

### 1. Add Delegation Metadata to `BuiltInAgent`

New struct:

```go
// DelegationHeuristics contains cost/speed/quality metadata for orchestrator routing.
type DelegationHeuristics struct {
    // Relative performance compared to the default orchestrator agent.
    // 1.0 = same, >1 = better, <1 = worse.
    SpeedMultiplier   float64 `json:"speedMultiplier"`   // e.g. 2.0 = 2x faster
    CostMultiplier    float64 `json:"costMultiplier"`    // e.g. 0.5 = half the cost
    QualityMultiplier float64 `json:"qualityMultiplier"` // e.g. 5.0 = 5x better decisions

    // Conditions where delegation is beneficial (injected as bullet points).
    DelegateWhen []string `json:"delegateWhen,omitempty"`

    // Conditions where delegation is wasteful (injected as bullet points).
    DontDelegateWhen []string `json:"dontDelegateWhen,omitempty"`

    // Quick rule-of-thumb for fast routing decisions.
    RuleOfThumb string `json:"ruleOfThumb,omitempty"`
}
```

Added to `BuiltInAgent`:

```go
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
    // NEW:
    Delegation  *DelegationHeuristics `yaml:"delegation,omitempty"`
}
```

### 2. Annotate All Built-In Agents

| Agent | Speed | Cost | Quality | Delegate When | Don't Delegate When |
|-------|-------|------|---------|--------------|-------------------|
| diane-researcher | 1.5x | 1.0x | 3x better | Multi-source research, fact-checking, synthesis | Single-source lookup, quick answers |
| diane-codebase | 2x | 1.0x | 5x better | Graph analysis, competitive landscape, architecture | Simple file reads, basic queries |
| diane-agent-creator | 1.0x | 1.0x | 5x better | Agent/skill/schema creation or modification | Routine tasks, agent use (not creation) |
| diane-schema-designer | 0.8x | 1.0x | 10x better | New schema types, relationship design, complex evolution | Simple field additions |
| graph-query-agent | 3x | 0.5x | 2x better | Graph traversal, entity discovery, relationship queries | NL explanations |
| diane-dreamer | — | — | — | Scheduled nightly runs only | Never delegate manually |
| diane-session-extractor | — | — | — | Post-session extraction | Never delegate manually |

### 3. Dynamic System Prompt Generation

**Current:** The system prompt for `diane-default` has a static AGENT CATALOG section with plain descriptions.

**Proposed:** Replace the static AGENT CATALOG with a dynamically-generated section that uses the heuristics metadata. When an agent has `Delegation != nil`, render:

```
### @agent-name
- Role: <description>
- Stats: <speedMult>x faster, <costMult>x cost, <qualityMult>x better at expertise
- Delegate when: <delegateWhen bullet list>
- Don't delegate when: <dontDelegateWhen bullet list>
- Rule of thumb: <ruleOfThumb>
```

When it doesn't have heuristics, render the current plain description.

**Implementation location:** `server/internal/agents/registry.go` — add a function `BuildAgentCatalog(builtins []BuiltInAgent) string` that generates the catalog section. Call it from the system prompt definition.

### 4. Enhance `list_available_agents` via Config Map

The MP SDK's `AgentDefinition` has a `Config map[string]any` field that passes through to ADK agents. Store delegation heuristics there:

```go
config["delegation"] = map[string]any{
    "speedMultiplier":   2.0,
    "costMultiplier":    0.5,
    "qualityMultiplier": 5.0,
    "delegateWhen":      []string{"Graph-heavy analysis", "Codebase structure exploration"},
    "dontDelegateWhen":  []string{"Simple file reads"},
    "ruleOfThumb":       "Let diane-codebase handle graph work; do simple reads yourself",
}
```

This means the orchestrator LLM gets the data via TWO paths:
1. **System prompt** — readable text for the LLM to reason over
2. **Config map** — structured data the ADK runtime could surface

### 5. Sync Path

The `cmdAgentSeed` / `cmdAgentSync` functions already push `BuiltInAgent` → `AgentDefinition`. The sync logic needs to:

```go
// In syncAgentToMP:
if ba.Delegation != nil {
    if r.Config == nil {
        r.Config = make(map[string]any)
    }
    r.Config["delegation"] = ba.Delegation
}
```

---

## Deferred: Feedback Loop

oh-my-opencode-slim's stats are **static** — manually authored and never recalibrated. A feedback loop that tracks actual delegation outcomes and adjusts stats would be a Phase 2 feature:

- Log each `spawn_agents` call: agent chosen, task description, actual cost (tokens), actual time, quality score (0-1)
- Periodically compute running averages → update agent metadata
- Detect "don't delegate when" violations (delegated something that should have been done directly)

---

## Files to Modify

| File | Change |
|------|--------|
| `server/internal/agents/registry.go` | Add `DelegationHeuristics` struct + populate for each built-in agent + add `BuildAgentCatalog()` |
| `server/internal/config/config.go` | (Optional) Add `Delegation` field to `AgentConfig` for user-defined agents |
| `server/internal/memory/bridge.go` | Push `Delegation` into `Config` map on sync |
| `server/internal/discord/bot.go` | (Optional) Show delegation stats in agent list |

---

## Acceptance Criteria

1. Each built-in agent has populated delegation heuristics
2. `diane-default`'s system prompt shows AGENT CATALOG with per-agent stats, delegate-when/don't-delegate-when rules, and rules of thumb
3. `list_available_agents` output includes delegation stat information
4. User-defined agents can optionally specify delegation metadata in `diane.yml`
5. Existing behavior is preserved when no delegation metadata exists
