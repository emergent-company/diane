// Package agents provides the agent definition system.
//
// This file implements graph-based agent config overrides:
// AgentOverrideConfig entities in the graph store partial overrides
// for built-in agent definitions. At seed time, these overrides are
// applied on top of the Go-code defaults — only specified fields
// replace the built-in values.
package agents

import (
	"context"
	"fmt"
	"log"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// AgentOverrideConfig holds partial overrides for a built-in agent.
// Only non-zero/non-nil fields override the Go-code defaults.
type AgentOverrideConfig struct {
	AgentName        string   `json:"agent_name"`
	SystemPrompt     string   `json:"system_prompt,omitempty"`
	Skills           []string `json:"skills,omitempty"`
	ModelProvider    string   `json:"model_provider,omitempty"`
	ModelName        string   `json:"model_name,omitempty"`
	ModelTemperature float64  `json:"model_temperature,omitempty"`
	ModelMaxTokens   int      `json:"model_max_tokens,omitempty"`
	MaxSteps         int      `json:"max_steps,omitempty"`
	Timeout          int      `json:"timeout,omitempty"`
	Visibility       string   `json:"visibility,omitempty"`

	// SandboxEnabled uses *bool to distinguish "not set" (nil) from "set to false".
	SandboxEnabled *bool `json:"sandbox_enabled,omitempty"`

	// Disabled marks the agent as disabled. When true, the agent is skipped
	// during seeding and will not appear in the active agent list.
	Disabled bool `json:"disabled,omitempty"`
}

// HasOverrides returns true if any override field is set (beyond agent_name).
func (o *AgentOverrideConfig) HasOverrides() bool {
	return o.SystemPrompt != "" ||
		len(o.Skills) > 0 ||
		o.ModelProvider != "" ||
		o.ModelName != "" ||
		o.ModelTemperature != 0 ||
		o.ModelMaxTokens != 0 ||
		o.MaxSteps != 0 ||
		o.Timeout != 0 ||
		o.Visibility != "" ||
		o.SandboxEnabled != nil ||
		o.Disabled
}

// ReadAgentOverrideConfigs queries the project graph for AgentOverrideConfig entities
// and returns a map of agent name → override config.
func ReadAgentOverrideConfigs(ctx context.Context, graphClient *graph.Client) (map[string]*AgentOverrideConfig, error) {
	resp, err := graphClient.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  "AgentOverrideConfig",
		Limit: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("list AgentOverrideConfig objects: %w", err)
	}

	result := make(map[string]*AgentOverrideConfig)
	for _, obj := range resp.Items {
		agentName := propString(obj.Properties, "agent_name")
		if agentName == "" {
			continue
		}

		oc := &AgentOverrideConfig{
			AgentName:        agentName,
			SystemPrompt:     propString(obj.Properties, "system_prompt"),
			Skills:           propStringSlice(obj.Properties, "skills"),
			ModelProvider:    propString(obj.Properties, "model_provider"),
			ModelName:        propString(obj.Properties, "model_name"),
			ModelTemperature: propFloat64(obj.Properties, "model_temperature"),
			ModelMaxTokens:   propInt(obj.Properties, "model_max_tokens"),
			MaxSteps:         propInt(obj.Properties, "max_steps"),
			Timeout:          propInt(obj.Properties, "timeout"),
			Visibility:       propString(obj.Properties, "visibility"),
			SandboxEnabled:   propBoolPtr(obj.Properties, "sandbox_enabled"),
		}

		if !oc.HasOverrides() {
			log.Printf("[override] Agent %q: override entity exists but no fields set, skipping", agentName)
			continue
		}

		result[agentName] = oc
		log.Printf("[override] Agent %q: loaded override config from graph", agentName)
	}

	return result, nil
}

// ApplyOverrides applies graph-stored overrides to built-in agents.
// Returns a new slice — the original is not modified.
// Only fields explicitly set in the override config replace the built-in values.
func ApplyOverrides(agents []BuiltInAgent, overrides map[string]*AgentOverrideConfig) []BuiltInAgent {
	merged := make([]BuiltInAgent, len(agents))
	copy(merged, agents)

	for i, ba := range merged {
		oc, ok := overrides[ba.Name]
		if !ok || oc == nil {
			continue
		}

		// System prompt
		if oc.SystemPrompt != "" {
			merged[i].SystemPrompt = oc.SystemPrompt
			log.Printf("[override] Agent %q: overrode system prompt", ba.Name)
		}

		// Skills — replace entire list if provided
		if len(oc.Skills) > 0 {
			merged[i].Skills = make([]string, len(oc.Skills))
			copy(merged[i].Skills, oc.Skills)
			log.Printf("[override] Agent %q: overrode skills (%d)", ba.Name, len(oc.Skills))
		}

		// Model config
		if oc.ModelProvider != "" || oc.ModelName != "" || oc.ModelTemperature != 0 || oc.ModelMaxTokens != 0 {
			if merged[i].Model == nil {
				merged[i].Model = &config.AgentModelConfig{}
			}
		}
		if oc.ModelProvider != "" {
			merged[i].Model.Provider = oc.ModelProvider
		}
		if oc.ModelName != "" {
			merged[i].Model.Name = oc.ModelName
		}
		if oc.ModelTemperature != 0 {
			merged[i].Model.Temperature = float32(oc.ModelTemperature)
		}
		if oc.ModelMaxTokens != 0 {
			merged[i].Model.MaxTokens = oc.ModelMaxTokens
		}

		// Max steps
		if oc.MaxSteps > 0 {
			merged[i].MaxSteps = oc.MaxSteps
			log.Printf("[override] Agent %q: overrode max_steps → %d", ba.Name, oc.MaxSteps)
		}

		// Timeout
		if oc.Timeout > 0 {
			merged[i].Timeout = oc.Timeout
			log.Printf("[override] Agent %q: overrode timeout → %ds", ba.Name, oc.Timeout)
		}

		// Visibility
		if oc.Visibility != "" {
			merged[i].Visibility = oc.Visibility
			log.Printf("[override] Agent %q: overrode visibility → %s", ba.Name, oc.Visibility)
		}

		// Sandbox enabled
		if oc.SandboxEnabled != nil {
			if merged[i].Sandbox == nil {
				merged[i].Sandbox = &config.SandboxConfig{}
			}
			merged[i].Sandbox.Enabled = *oc.SandboxEnabled
			log.Printf("[override] Agent %q: overrode sandbox_enabled → %v", ba.Name, *oc.SandboxEnabled)
		}
	}

	return merged
}

// ---------------------------------------------------------------------------
// Extra property extraction helpers
// ---------------------------------------------------------------------------

func propFloat64(props map[string]any, key string) float64 {
	if props == nil {
		return 0
	}
	v, ok := props[key]
	if !ok {
		return 0
	}
	// JSON unmarshals numbers as float64
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return f
}

func propInt(props map[string]any, key string) int {
	if props == nil {
		return 0
	}
	v, ok := props[key]
	if !ok {
		return 0
	}
	// JSON unmarshals integers as float64
	f, ok := v.(float64)
	if ok {
		return int(f)
	}
	i, ok := v.(int)
	if ok {
		return i
	}
	return 0
}

func propBoolPtr(props map[string]any, key string) *bool {
	if props == nil {
		return nil
	}
	v, ok := props[key]
	if !ok {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		return nil
	}
	return &b
}

// FilterDisabled removes agents from the list that have a disabled override
// in the given overrides map. Returns a new slice — the original is not modified.
func FilterDisabled(agents []BuiltInAgent, overrides map[string]*AgentOverrideConfig) []BuiltInAgent {
	filtered := make([]BuiltInAgent, 0, len(agents))
	for _, a := range agents {
		oc, ok := overrides[a.Name]
		if ok && oc != nil && oc.Disabled {
			log.Printf("[override] Agent %q: filtered out (disabled via override)", a.Name)
			continue
		}
		filtered = append(filtered, a)
	}
	return filtered
}

// UpsertDisableOverride creates or updates an AgentOverrideConfig entity
// in the graph with disabled=true for the given agent name. If an entity
// already exists, it adds disabled=true to the existing properties.
func UpsertDisableOverride(ctx context.Context, graphClient *graph.Client, agentName string) error {
	// Check if an override entity already exists
	resp, err := graphClient.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  "AgentOverrideConfig",
		Limit: 100,
	})
	if err != nil {
		return fmt.Errorf("list existing overrides: %w", err)
	}

	for _, obj := range resp.Items {
		name := propString(obj.Properties, "agent_name")
		if name == agentName {
			// Update existing — merge disabled=true
			props := make(map[string]any)
			for k, v := range obj.Properties {
				props[k] = v
			}
			props["disabled"] = true
			_, err := graphClient.UpdateObject(ctx, obj.ID, &graph.UpdateObjectRequest{Properties: props})
			if err != nil {
				return fmt.Errorf("update override for %s: %w", agentName, err)
			}
			log.Printf("[override] Agent %q: disabled=true added to existing override", agentName)
			return nil
		}
	}

	// Create new override entity
	_, err = graphClient.CreateObject(ctx, &graph.CreateObjectRequest{
		Type: "AgentOverrideConfig",
		Properties: map[string]any{
			"agent_name": agentName,
			"disabled":   true,
		},
	})
	if err != nil {
		return fmt.Errorf("create disable override for %s: %w", agentName, err)
	}
	log.Printf("[override] Agent %q: override entity created with disabled=true", agentName)
	return nil
}
