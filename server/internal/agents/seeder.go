package agents

import (
	"encoding/json"
	"fmt"

	"github.com/Emergent-Comapny/diane/internal/db"
)

// SeedToDB writes all built-in agents to the SQLite database.
// Existing entries are updated; new ones are inserted.
// This ensures the local DB is always in sync with the Go code.
func SeedToDB(d *db.DB) error {
	builtIns := BuiltInAgents()

	for _, ba := range builtIns {
		toolsJSON, err := json.Marshal(ba.Tools)
		if err != nil {
			return fmt.Errorf("marshal tools for %s: %w", ba.Name, err)
		}
		skillsJSON, err := json.Marshal(ba.Skills)
		if err != nil {
			return fmt.Errorf("marshal skills for %s: %w", ba.Name, err)
		}

		tags := []string{}
		switch ba.Name {
		case "diane-default":
			tags = []string{"default", "general-purpose"}
		case "diane-codebase":
			tags = []string{"code", "graph"}
		case "diane-researcher":
			tags = []string{"research", "web"}
		case "diane-agent-creator":
			tags = []string{"admin", "management"}
		case "diane-dreamer":
			tags = []string{"memory", "consolidation"}
		case "diane-schema-designer":
			tags = []string{"admin", "schema"}
		case "diane-session-extractor":
			tags = []string{"memory", "extraction"}
		}

		tagsJSON, err := json.Marshal(tags)
		if err != nil {
			return fmt.Errorf("marshal tags for %s: %w", ba.Name, err)
		}

		// Visibility
		visibility := ba.Visibility
		if visibility == "" {
			visibility = "project"
		}

		maxSteps := ba.MaxSteps
		if maxSteps == 0 {
			maxSteps = 50
		}

		timeout := ba.Timeout
		if timeout == 0 {
			timeout = 300
		}

		ad := &db.AgentDefinition{
			Name:           ba.Name,
			Description:    ba.Description,
			SystemPrompt:   ba.SystemPrompt,
			ToolsJSON:      string(toolsJSON),
			SkillsJSON:     string(skillsJSON),
			ModelConfigJSON: "",
			FlowType:       ba.FlowType,
			Visibility:     visibility,
			MaxSteps:       maxSteps,
			DefaultTimeout: timeout,
			TagsJSON:       string(tagsJSON),
			RoutingWeight:  1.0,
			IsDefault:      ba.Name == "diane-default",
			IsExperimental: false,
			Status:         "active",
			Source:         "built-in",
		}

		if err := d.UpsertAgentDefinition(ad); err != nil {
			return fmt.Errorf("seed %s to DB: %w", ba.Name, err)
		}
	}

	return d.EnsureDefaultAgent()
}
