// Package agents provides the agent definition system.
//
// This file implements graph-based agent tool configuration:
// AgentToolConfig entities in the graph store tool glob patterns
// that are merged into built-in agent definitions at seed time.
// This allows adding new MCP relay tool access without code changes.
package agents

import (
	"context"
	"fmt"
	"log"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// ReadAgentToolConfigs queries the project graph for AgentToolConfig entities
// and returns a map of agent name → tool glob patterns.
func ReadAgentToolConfigs(ctx context.Context, graphClient *graph.Client) (map[string][]string, error) {
	resp, err := graphClient.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  "AgentToolConfig",
		Limit: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("list AgentToolConfig objects: %w", err)
	}

	result := make(map[string][]string)
	for _, obj := range resp.Items {
		agentName := propString(obj.Properties, "agent_name")
		if agentName == "" {
			continue
		}
		patterns := propStringSlice(obj.Properties, "tool_patterns")
		if len(patterns) == 0 {
			continue
		}
		existing := result[agentName]
		existing = append(existing, patterns...)
		result[agentName] = existing
		log.Printf("[toolconfig] Agent %q: %d tool pattern(s) from graph", agentName, len(patterns))
	}

	return result, nil
}

// MergeToolPatterns merges graph-stored tool patterns into built-in agents.
// Returns a new slice — the original is not modified.
func MergeToolPatterns(agents []BuiltInAgent, configs map[string][]string) []BuiltInAgent {
	merged := make([]BuiltInAgent, len(agents))
	copy(merged, agents)

	for i, ba := range merged {
		patterns, ok := configs[ba.Name]
		if !ok || len(patterns) == 0 {
			continue
		}
		// Deduplicate: skip patterns already in the core list
		existingSet := make(map[string]bool, len(ba.Tools))
		for _, t := range ba.Tools {
			existingSet[t] = true
		}
		for _, p := range patterns {
			if !existingSet[p] {
				merged[i].Tools = append(merged[i].Tools, p)
				existingSet[p] = true
			}
		}
		log.Printf("[toolconfig] Agent %q: merged %d graph pattern(s) → %d total tools",
			ba.Name, len(patterns), len(merged[i].Tools))
	}

	return merged
}

// ---------------------------------------------------------------------------
// Property extraction helpers (mirrors bridge.go patterns)
// ---------------------------------------------------------------------------

func propString(props map[string]any, key string) string {
	if props == nil {
		return ""
	}
	v, ok := props[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func propStringSlice(props map[string]any, key string) []string {
	if props == nil {
		return nil
	}
	v, ok := props[key]
	if !ok {
		return nil
	}
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
