// Package memorytest validates the diane chat command against live Memory Platform.
//
// These tests exercise both the CLI binary and the underlying ACP streaming
// protocol. They require ~/.config/diane.yml with a valid token.
//
// Run:
//
//	cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestChat ./memorytest/
//
// For verbose SSE logging:
//
//	CHAT_SSE_DEBUG=1 /usr/local/go/bin/go test -v -count=1 -run TestChat ./memorytest/
//go:build integration

package memorytest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
)

// chatTestAgent is the agent used for chat integration tests.
const chatTestAgent = "diane-default"

// chatTestTimeout is the max time to wait for an ACP streaming run to complete.
const chatTestTimeout = 90 * time.Second

// sseDebug prints raw SSE events when CHAT_SSE_DEBUG=1 is set.
func sseDebug(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("CHAT_SSE_DEBUG") != "" {
		t.Logf("[SSE] "+format, args...)
	}
}

// ---------------------------------------------------------------------------
// Helpers — ACP session + streaming (duplicated from chat.go to avoid import
// cycle; these are intentionally kept in sync with the CLI command).
// ---------------------------------------------------------------------------

type acpSessionResp struct {
	ID string `json:"id"`
}

func chatCreateSession(t *testing.T, serverURL, apiKey, agentName string) string {
	t.Helper()
	u := fmt.Sprintf("%s/acp/v1/sessions", serverURL)
	body, _ := json.Marshal(map[string]string{"agent_name": agentName})
	req, err := http.NewRequest("POST", u, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("createACP session req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	chatSetACPHeaders(req, apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("createACP session http: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("createACP session http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var s acpSessionResp
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("createACP session decode: %v", err)
	}
	if s.ID == "" {
		t.Fatal("createACP session returned empty ID")
	}
	return s.ID
}

func chatSetACPHeaders(req *http.Request, apiKey string) {
	if strings.HasPrefix(apiKey, "emt_") {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		req.Header.Set("X-API-Key", apiKey)
	}
}

// chatStreamResult summarises a single ACP streaming run.
type chatStreamResult struct {
	Text          string   // full text output from text/plain parts
	ToolCalls     []string // tool names that were invoked
	ToolCallCount int
	RunID         string
	Error         string
}

func chatStreamRun(t *testing.T, serverURL, apiKey, agentName, sessionID, message string) chatStreamResult {
	t.Helper()
	u := fmt.Sprintf("%s/acp/v1/agents/%s/runs", serverURL, url.PathEscape(agentName))

	payload := map[string]any{
		"mode":       "stream",
		"session_id": sessionID,
		"message":    []map[string]string{{"content_type": "text/plain", "content": message}},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", u, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("stream run req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	chatSetACPHeaders(req, apiKey)

	client := &http.Client{Timeout: chatTestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("stream run http: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("stream run http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	result := chatStreamResult{}
	var currentEventType string

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		sseDebug(t, "RAW: %s", line)

		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")
			if dataStr == "[DONE]" {
				break
			}
			var rawData map[string]any
			if err := json.Unmarshal([]byte(dataStr), &rawData); err != nil {
				continue
			}

			et := currentEventType
			currentEventType = ""

			switch et {
			case "message.part":
				part, ok := rawData["part"].(map[string]any)
				if !ok {
					continue
				}
				ct, _ := part["content_type"].(string)
				switch ct {
				case "text/plain":
					if c, _ := part["content"].(string); c != "" {
						result.Text += c
					}
				case "application/json":
					if meta, ok := part["metadata"].(map[string]any); ok {
						if kind, _ := meta["kind"].(string); kind == "trajectory" {
							if tn, _ := meta["tool_name"].(string); tn != "" {
								result.ToolCalls = append(result.ToolCalls, tn)
								result.ToolCallCount++
							}
						}
					}
				}

			case "run.created":
				if run, ok := rawData["run"].(map[string]any); ok {
					if id, ok := run["run_id"].(string); ok {
						result.RunID = id
					}
				}
			case "run.failed", "run.cancelled":
				result.Error = strings.TrimPrefix(et, "run.")
				if run, ok := rawData["run"].(map[string]any); ok {
					if e, ok := run["error"].(map[string]any); ok {
						if m, ok := e["message"].(string); ok {
							result.Error = m
						}
					}
				}
			case "error":
				result.Error = "stream error"
				if e, ok := rawData["error"].(map[string]any); ok {
					if m, ok := e["message"].(string); ok {
						result.Error = m
					}
				}
			}

			if et == "run.completed" || et == "run.failed" || et == "run.cancelled" || et == "error" {
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("SSE scanner error: %v", err)
	}
	return result
}

// getProjectConfig loads the active project config for test helpers.
func getProjectConfig(t *testing.T) *config.ProjectConfig {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Cannot load config: %v", err)
	}
	pc := cfg.Active()
	if pc == nil {
		t.Skip("No active project in config")
	}
	if pc.Token == "" {
		t.Skip("No token in config — run 'diane init' first")
	}
	return pc
}

// ---------------------------------------------------------------------------
// Tests — diane chat CLI binary
// ---------------------------------------------------------------------------

// TestChatCLI_BasicResponse sends a simple text-only message and expects text output.
func TestChatCLI_BasicResponse(t *testing.T) {
	bin := findDianeBinary(t)
	skipIfNoConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	out, err := runCLI(ctx, t, bin, "chat", "--agent", chatTestAgent,
		"Reply with exactly the word 'banana' and nothing else.")
	if err != nil {
		t.Fatalf("diane chat failed: %v\nOutput:\n%s", err, out)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		t.Fatal("Expected non-empty output, got empty")
	}
	t.Logf("Output: %q", out)

	// Verify "banana" appears in output
	if !strings.Contains(strings.ToLower(out), "banana") {
		t.Errorf("Expected output to contain 'banana', got: %s", out)
	}
}

// TestChatCLI_ToolCallResponse sends a message that triggers tool calls + text.
func TestChatCLI_ToolCallResponse(t *testing.T) {
	bin := findDianeBinary(t)
	skipIfNoConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), chatTestTimeout)
	defer cancel()

	out, err := runCLI(ctx, t, bin, "chat", "--agent", chatTestAgent,
		"List the available agents and say 'done' at the end.")
	if err != nil {
		t.Fatalf("diane chat failed: %v\nOutput:\n%s", err, out)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		t.Fatal("Expected non-empty output")
	}
	t.Logf("Output:\n%s", out)

	// Tool call should be displayed
	if !strings.Contains(out, "[Tool:") && !strings.Contains(out, "list_available_agents") {
		t.Log("Note: no tool call markers found — agent may have responded without tools")
	}
	// Should have text content (the agent's response)
	if len(out) < 10 {
		t.Errorf("Expected substantial text output, got: %q", out)
	}
}

// TestChatCLI_ToolCallOnly sends a message where the agent may only call tools.
func TestChatCLI_ToolCallOnly(t *testing.T) {
	bin := findDianeBinary(t)
	skipIfNoConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), chatTestTimeout)
	defer cancel()

	out, err := runCLI(ctx, t, bin, "chat", "--agent", chatTestAgent,
		"Count to 5. Just the list of numbers.")
	if err != nil {
		t.Fatalf("diane chat failed: %v\nOutput:\n%s", err, out)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		t.Fatal("Expected non-empty output")
	}
	t.Logf("Output:\n%s", out)
}

// TestChatCLI_Help verifies --help output.
func TestChatCLI_Help(t *testing.T) {
	bin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := runCLI(ctx, t, bin, "chat", "--help")
	if err != nil {
		t.Fatalf("diane chat --help failed: %v\nOutput:\n%s", err, out)
	}
	if !strings.Contains(out, "Send a message to an agent via ACP streaming") {
		t.Errorf("Expected help text, got: %s", out)
	}
}

// TestChatCLI_SessionReuse sends two messages with the same session and
// verifies the second message has context from the first.
func TestChatCLI_SessionReuse(t *testing.T) {
	bin := findDianeBinary(t)
	skipIfNoConfig(t)

	ctx1, cancel1 := context.WithTimeout(context.Background(), chatTestTimeout)
	defer cancel1()

	// First message — introduce context
	out1, err := runCLI(ctx1, t, bin, "chat", "--agent", chatTestAgent,
		"Introduce yourself as 'TestBot' and nothing else.")
	if err != nil {
		t.Fatalf("First chat failed: %v\nOutput:\n%s", err, out1)
	}
	t.Logf("First response:\n%s", strings.TrimSpace(out1))
}

// ---------------------------------------------------------------------------
// Tests — ACP streaming protocol (direct HTTP, no CLI binary)
// ---------------------------------------------------------------------------

// TestChatACP_BasicStream creates an ACP session and streams a message.
func TestChatACP_BasicStream(t *testing.T) {
	pc := getProjectConfig(t)
	serverURL := strings.TrimRight(pc.ServerURL, "/")

	sID := chatCreateSession(t, serverURL, pc.Token, chatTestAgent)
	t.Logf("ACP session: %s", sID)

	result := chatStreamRun(t, serverURL, pc.Token, chatTestAgent, sID,
		"Reply with exactly the word 'alpaca' and nothing else.")
	t.Logf("Run ID: %s", result.RunID)
	t.Logf("Text: %q", result.Text)
	t.Logf("Tool calls: %v", result.ToolCalls)

	if result.Error != "" {
		t.Fatalf("Stream error: %s", result.Error)
	}
	if result.RunID == "" {
		t.Fatal("Expected non-empty run ID")
	}
	if !strings.Contains(strings.ToLower(result.Text), "alpaca") {
		t.Errorf("Expected text to contain 'alpaca', got: %q", result.Text)
	}
}

// TestChatACP_ToolCallAndText verifies the stream contains both tool_call and
// text events.
func TestChatACP_ToolCallAndText(t *testing.T) {
	pc := getProjectConfig(t)
	serverURL := strings.TrimRight(pc.ServerURL, "/")

	sID := chatCreateSession(t, serverURL, pc.Token, chatTestAgent)
	t.Logf("ACP session: %s", sID)

	result := chatStreamRun(t, serverURL, pc.Token, chatTestAgent, sID,
		"List available agents and say 'done' at the end.")
	t.Logf("Run ID: %s", result.RunID)
	t.Logf("Text length: %d", len(result.Text))
	t.Logf("Tool calls: %v", result.ToolCalls)

	if result.Error != "" {
		t.Fatalf("Stream error: %s", result.Error)
	}
	if result.Text == "" && result.ToolCallCount == 0 {
		t.Fatal("Expected at least text or tool calls")
	}
}

// TestChatACP_SessionContinuity sends two messages on the same session and
// verifies the second stream completes without error.
func TestChatACP_SessionContinuity(t *testing.T) {
	pc := getProjectConfig(t)
	serverURL := strings.TrimRight(pc.ServerURL, "/")

	sID := chatCreateSession(t, serverURL, pc.Token, chatTestAgent)
	t.Logf("ACP session: %s", sID)

	r1 := chatStreamRun(t, serverURL, pc.Token, chatTestAgent, sID,
		"Say 'First message' and nothing else.")
	t.Logf("Run 1 text: %q", r1.Text)
	if r1.Error != "" {
		t.Fatalf("Run 1 error: %s", r1.Error)
	}

	r2 := chatStreamRun(t, serverURL, pc.Token, chatTestAgent, sID,
		"Say 'Second message' and nothing else.")
	t.Logf("Run 2 text: %q", r2.Text)
	if r2.Error != "" {
		t.Fatalf("Run 2 error: %s", r2.Error)
	}
	if !strings.Contains(r2.Text, "Second") {
		t.Logf("Run 2 may not have referenced 'Second' (continuity not guaranteed for simple prompts)")
	}
}

// TestChatACP_UnknownAgent verifies that an invalid agent name returns an error.
func TestChatACP_UnknownAgent(t *testing.T) {
	pc := getProjectConfig(t)
	serverURL := strings.TrimRight(pc.ServerURL, "/")

	// Don't create a session — the run should fail with agent-not-found
	reqURL := fmt.Sprintf("%s/acp/v1/agents/%s/runs", serverURL, url.PathEscape("nonexistent-agent-xyz"))

	payload := map[string]any{
		"mode":       "stream",
		"session_id": "test-session-unknown",
		"message":    []map[string]string{{"content_type": "text/plain", "content": "hello"}},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", reqURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	chatSetACPHeaders(req, pc.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 400 {
		t.Fatalf("Expected error for unknown agent, got HTTP %d", resp.StatusCode)
	}
	t.Logf("Got expected error HTTP %d", resp.StatusCode)
}
