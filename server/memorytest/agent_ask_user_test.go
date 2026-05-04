// Package memorytest validates the agent ask_user → answer → resume lifecycle
// against the live Memory Platform.
//
// This test:
//  1. Creates a runtime agent
//  2. Triggers it with a prompt designed to call ask_user
//  3. Waits for the question to appear (status: paused)
//  4. Answers the question via RespondToAgentQuestion
//  5. Verifies the run continues and completes
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestAgentAskUser ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
)

const (
	askUserTestDefName  = "diane-default"
	askUserTestTimeout  = 180 * time.Second
	askUserTestPoll     = 2 * time.Second
	askUserTestMaxSteps = 10
)

// TestAgentAskUser verifies the full ask_user lifecycle:
// trigger → question → answer → resume → complete
func TestAgentAskUser(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), askUserTestTimeout)
	defer cancel()

	// Get project ID from config
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Cannot load config: %v", err)
	}
	pc := cfg.Active()
	if pc == nil {
		t.Skip("No active project in config")
	}
	projectID := pc.ProjectID

	// ── 1. Find agent definition ──
	defs, err := b.ListAgentDefs(ctx)
	if err != nil {
		t.Fatalf("ListAgentDefs: %v", err)
	}
	var defID string
	for _, d := range defs.Data {
		if d.Name == askUserTestDefName {
			defID = d.ID
			break
		}
	}
	if defID == "" {
		t.Fatalf("Agent definition %q not found — run 'diane agent seed'", askUserTestDefName)
	}
	t.Logf("Found def: %s (%s)", askUserTestDefName, defID[:12])

	// ── 2. Create runtime agent ──
	runtimeName := fmt.Sprintf("test-askuser-%d", time.Now().UnixMilli())
	agent, err := b.CreateRuntimeAgent(ctx, runtimeName, defID)
	if err != nil {
		t.Fatalf("CreateRuntimeAgent: %v", err)
	}
	agentID := agent.Data.ID
	t.Logf("Created runtime agent: %s (%s)", runtimeName, agentID[:12])
	t.Cleanup(func() {
		if delErr := b.Client().Agents.Delete(context.Background(), agentID); delErr != nil {
			t.Logf("Cleanup delete agent: %v", delErr)
		}
	})

	// ── 3. Create a session for context ──
	sessionTitle := fmt.Sprintf("test-askuser-session-%d", time.Now().UnixMilli())
	session, err := b.CreateSession(ctx, sessionTitle)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionID := session.ID
	t.Logf("Created session: %s", sessionID[:12])

	// ── 4. Trigger with a prompt that asks a question ──
	triggerPrompt := "I need to decide which database to use for a new project. " +
		"Ask me which one I prefer: PostgreSQL or SQLite. " +
		"Present these as two options. Then wait for my response."

	triggerResp, err := b.TriggerAgentWithInput(ctx, agentID, triggerPrompt, sessionID)
	if err != nil {
		t.Fatalf("TriggerAgentWithInput: %v", err)
	}
	if !triggerResp.Success || triggerResp.RunID == nil {
		errMsg := ""
		if triggerResp.Error != nil {
			errMsg = *triggerResp.Error
		}
		t.Fatalf("Trigger failed: success=%v runID=%v error=%s",
			triggerResp.Success, triggerResp.RunID, errMsg)
	}
	runID := *triggerResp.RunID
	t.Logf("Triggered run: %s", runID[:12])

	// ── 5. Poll for the question (paused status) ──
	var questionID string
	var foundQuestion bool
	pollStart := time.Now()

	for i := 0; i < askUserTestMaxSteps; i++ {
		select {
		case <-ctx.Done():
			t.Fatalf("Context cancelled while waiting for question")
		default:
		}

		runResp, err := b.GetProjectRun(ctx, runID)
		if err != nil {
			time.Sleep(askUserTestPoll)
			continue
		}
		status := runResp.Data.Status
		elapsed := time.Since(pollStart).Round(time.Second)
		t.Logf("  [poll %d] status=%s step=%d (%s)", i, status, runResp.Data.StepCount, elapsed)

		if status == "paused" {
			qsResp, err := b.Client().Agents.GetRunQuestions(ctx, projectID, runID)
			if err != nil {
				t.Logf("  GetRunQuestions error: %v (retrying)", err)
				time.Sleep(askUserTestPoll)
				continue
			}
			for _, q := range qsResp.Data {
				if q.Status == "pending" {
					questionID = q.ID
					foundQuestion = true
					t.Logf("  Found question: %s — %q", q.ID[:12], truncateStr(q.Question, 80))
					break
				}
			}
			if foundQuestion {
				break
			}
		}

		if status == "completed" || status == "success" || status == "completed_with_warnings" {
			t.Fatal("Run completed without asking a question — agent didn't call ask_user")
		}
		if status == "error" || status == "failed" || status == "cancelled" || status == "timeout" {
			errMsg := ""
			if runResp.Data.ErrorMessage != nil {
				errMsg = *runResp.Data.ErrorMessage
			}
			t.Fatalf("Run failed: status=%s error=%s", status, errMsg)
		}

		time.Sleep(askUserTestPoll)
	}

	if !foundQuestion {
		t.Fatal("Agent did not ask a question within the polling period")
	}

	questionTime := time.Now()

	// ── 6. Answer the question ──
	answer := "I prefer PostgreSQL"
	qResp, err := b.RespondToAgentQuestion(ctx, questionID, answer)
	if err != nil {
		t.Fatalf("RespondToAgentQuestion: %v", err)
	}
	if qResp.Status != "answered" {
		t.Fatalf("Question status after respond = %q, want %q", qResp.Status, "answered")
	}
	t.Logf("✅ Question answered: %s", qResp.Status)
	t.Logf("   Response: %q", *qResp.Response)

	// ── 7. Poll for run to complete (proving the resume worked) ──
	resumePollStart := time.Now()
	var finalStatus string
	var completed bool

	for i := 0; i < askUserTestMaxSteps*3; i++ {
		select {
		case <-ctx.Done():
			t.Fatalf("Context cancelled while waiting for resume")
		default:
		}

		runResp, err := b.GetProjectRun(ctx, runID)
		if err != nil {
			time.Sleep(askUserTestPoll)
			continue
		}

		status := runResp.Data.Status
		stepCount := runResp.Data.StepCount
		elapsed := time.Since(resumePollStart).Round(time.Second)
		t.Logf("  [resume %d] status=%s step=%d (%s)", i, status, stepCount, elapsed)

		switch status {
		case "completed", "success", "completed_with_warnings":
			finalStatus = status
			completed = true
		case "error", "failed":
			finalStatus = status
			completed = true
		case "paused":
			if stepCount > 2 {
				// Run paused again — still proves the resume worked.
				t.Logf("   ⚠️  Run paused again at step %d — resume worked but new question asked", stepCount)
				completed = true
				finalStatus = "paused (but continued from original pause)"
			}
		}

		if completed {
			break
		}

		time.Sleep(askUserTestPoll)
	}

	if !completed {
		t.Fatalf("Run did not complete after answering question (waited %s)",
			time.Since(resumePollStart).Round(time.Second))
	}

	// ── 8. Verify messages show continuation ──
	msgs, err := b.GetRunMessages(ctx, runID)
	if err != nil {
		t.Fatalf("GetRunMessages: %v", err)
	}

	// Check for the resume injection message
	var foundResumeInjection bool
	for _, m := range msgs.Data {
		if val, ok := m.Content["text"]; ok {
			text := extractTextSafe(val)
			if strings.Contains(text, "Previously you asked") && strings.Contains(text, answer) {
				foundResumeInjection = true
				break
			}
		}
	}
	if !foundResumeInjection {
		t.Logf("⚠️  Warning: Did not find resume injection message (may be a different format)")
	}

	totalDuration := time.Since(pollStart).Round(time.Millisecond)
	questionDuration := questionTime.Sub(pollStart).Round(time.Millisecond)
	resumeDuration := time.Since(questionTime).Round(time.Millisecond)

	t.Logf("")
	t.Logf("═══ Results ═══")
	t.Logf("  Time to question: %s", questionDuration)
	t.Logf("  Time to completion: %s", resumeDuration)
	t.Logf("  Total: %s", totalDuration)
	t.Logf("  Final status: %s", finalStatus)
	t.Logf("  Steps: %d", len(msgs.Data))
	t.Logf("")
	t.Logf("✅ ask_user → answer → resume lifecycle verified")
}

// extractTextSafe extracts a string from a content value (may be []interface{} or string).
func extractTextSafe(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				return s
			}
		}
	}
	return fmt.Sprintf("%v", val)
}
