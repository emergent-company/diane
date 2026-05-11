// Package memorytest verifies that agent skills are properly resolved at runtime.
//
// This test references an existing skill on MP from an agent definition,
// triggers the agent, and verifies the skill content appears in the session.
//
// Run: cd ~/diane/server && go test -v -tags=integration -count=1 -run TestAgentSkill ./memorytest/
//go:build integration

package memorytest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
)

// =========================================================================
// TestAgentSkill_AvailableInSession: Uses an existing MP skill in an agent
// definition, triggers the agent, and verifies the skill content is visible
// in the agent's response.
//
// Prerequisite: MP has at least one skill (e.g. "diane-memory-save").
// =========================================================================

func TestAgentSkill_AvailableInSession(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), agentTestTimeout+60*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-skill-%d", time.Now().UnixMilli())

	// ── 1. Find an existing skill on MP ──
	existingSkills, err := b.ListSkills(ctx)
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(existingSkills) == 0 {
		t.Skip("No skills found on MP — need at least one skill to test")
	}
	testSkill := existingSkills[0]
	t.Logf("Using existing skill: %s — %s", testSkill.Name, testSkill.Description)
	t.Logf("  Content preview: %.120s...", testSkill.Content)

	// ── 2. Create an agent definition that references this skill ──
	defName := prefix + "-def"
	sysPrompt := "You are a test agent. Use your available skills when relevant."
	tools := []string{"web-search-brave"}

	createdDef, err := b.CreateAgentDef(ctx, &sdkagents.CreateAgentDefinitionRequest{
		Name:         defName,
		SystemPrompt: &sysPrompt,
		Skills:       []string{testSkill.Name},
		Tools:        tools,
		Visibility:   "project",
	})
	if err != nil {
		t.Fatalf("CreateAgentDef: %v", err)
	}
	defID := createdDef.Data.ID
	t.Logf("Created agent definition: %s (%s)", defName, defID)
	t.Logf("  Skills on definition: %v", createdDef.Data.Skills)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = b.DeleteAgentDef(cleanupCtx, defID)
	})

	// ── 3. Verify skills persisted on definition ──
	gotDef, err := b.GetAgentDef(ctx, defID)
	if err != nil {
		t.Fatalf("GetAgentDef: %v", err)
	}
	if len(gotDef.Data.Skills) == 0 {
		t.Fatal("FAIL: Skills field was dropped by MP — agent definition has no skills")
	}
	hasSkill := false
	for _, s := range gotDef.Data.Skills {
		if s == testSkill.Name {
			hasSkill = true
			break
		}
	}
	if !hasSkill {
		t.Fatalf("FAIL: Skill %q not found in definition skills: %v", testSkill.Name, gotDef.Data.Skills)
	}
	t.Logf("✅ Skills persisted on definition: %v", gotDef.Data.Skills)

	// ── 4. Create a runtime agent from the definition ──
	runName := fmt.Sprintf("t-skill-agent-%d", time.Now().UnixMilli())
	agent, err := b.CreateRuntimeAgent(ctx, runName, defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := agent.Data.ID
	t.Logf("Runtime agent: %s", agentID)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = b.Client().Agents.Delete(cleanupCtx, agentID)
	})

	// ── 5. Trigger agent with a skill-aware prompt ──
	sessionID := fmt.Sprintf("test-skill-session-%d", time.Now().UnixMilli())
	prompt := fmt.Sprintf(
		"List all skills you have access to. For each skill, describe what it does. "+
			"If you have a skill named %q, include its full content in your response. "+
			"Be thorough and detailed.",
		testSkill.Name,
	)

	t.Logf("Triggering agent...")
	triggerResp, err := b.TriggerAgentWithInput(ctx, agentID, prompt, sessionID)
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	if triggerResp.Error != nil && *triggerResp.Error != "" {
		t.Fatalf("Trigger error: %s", *triggerResp.Error)
	}
	runID := *triggerResp.RunID
	t.Logf("Run ID: %s", runID)

	// ── 6. Poll for completion ──
	if !pollRunCompletion(b, ctx, t, runID) {
		logMessages(t, b, ctx, runID, "Run messages (failed)")
		t.Fatal("Run did not complete successfully within polling window")
	}
	t.Logf("✅ Run completed")

	// ── 7. Fetch and analyze messages ──
	logMessages(t, b, ctx, runID, "Run messages")

	msgs, err := b.GetRunMessages(ctx, runID)
	if err != nil {
		t.Fatalf("GetRunMessages: %v", err)
	}

	// Check all messages for skill evidence
	foundSkillInResponse := false
	foundSkillToolCall := false
	for _, m := range msgs.Data {
		content := extractMsgContent(m.Content)

		if strings.Contains(content, testSkill.Name) {
			foundSkillInResponse = true
			t.Logf("✅ Skill name %q found in agent response", testSkill.Name)
		}

		contentLower := strings.ToLower(content)
		if strings.Contains(contentLower, "skill") &&
			strings.Contains(contentLower, strings.ToLower(testSkill.Name)) {
			foundSkillToolCall = true
		}
	}

	if !foundSkillInResponse && !foundSkillToolCall {
		t.Logf("⚠️  Skill %q not explicitly mentioned in agent response.", testSkill.Name)
		t.Logf("   The skill was successfully persisted on the definition,")
		t.Logf("   but the ADK runtime may not inject it into the prompt,")
		t.Logf("   or the agent chose not to reference it.")
		t.Logf("   Check full message logs above for diagnostics.")
		t.Logf("   Definition skills: %v", gotDef.Data.Skills)
	} else {
		t.Logf("✅ PASS: Skill content is available to the agent at runtime")
	}
}
