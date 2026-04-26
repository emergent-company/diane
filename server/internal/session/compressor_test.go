package session

import (
	"testing"
)

func TestNewCompressor(t *testing.T) {
	c := NewCompressor(CompressorConfig{ContextLength: 128000})
	if c == nil {
		t.Fatal("NewCompressor returned nil")
	}
	if c.thresholdTokens != 64000 {
		t.Errorf("expected threshold 64000, got %d", c.thresholdTokens)
	}
	if c.tailTokenBudget == 0 {
		t.Error("tailTokenBudget should not be zero")
	}
	if c.maxSummaryTokens == 0 {
		t.Error("maxSummaryTokens should not be zero")
	}
}

func TestShouldCompress(t *testing.T) {
	c := NewCompressor(CompressorConfig{ContextLength: 128000})

	// Should NOT compress below threshold
	if c.ShouldCompress(1000) {
		t.Error("should not compress at 1000 tokens")
	}

	// Should compress at threshold
	if !c.ShouldCompress(64000) {
		t.Error("should compress at 64000 tokens")
	}

	// Anti-thrashing: simulate 2 bad compressions
	c.ineffectiveCompressionCount = 2
	if c.ShouldCompress(64000) {
		t.Error("should skip compression after 2 ineffective ones")
	}
}

func TestCompressSmallConversation(t *testing.T) {
	c := NewCompressor(CompressorConfig{ContextLength: 128000})

	// Only 5 messages — need > 7 for compression to be possible
	messages := []Message{
		{Role: RoleSystem, Content: "You are a helpful assistant."},
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi there!"},
		{Role: RoleUser, Content: "How are you?"},
		{Role: RoleAssistant, Content: "I'm doing great, thanks!"},
	}

	result := c.Compress(messages, 2000, "")
	if result.WasCompacted {
		t.Error("should not compact conversation with < 7 messages")
	}
	if len(result.Messages) != 5 {
		t.Errorf("expected 5 messages, got %d", len(result.Messages))
	}
}

func TestPruneOldToolResults(t *testing.T) {
	c := NewCompressor(CompressorConfig{ContextLength: 128000})

	messages := []Message{
		{Role: RoleSystem, Content: "System prompt"},
		{Role: RoleUser, Content: "Search for something"},
		{Role: RoleAssistant, Content: "Let me search", ToolCalls: []ToolCall{
			{ID: "call_1", Function: FunctionCall{Name: "web_search", Arguments: `{"query":"test"}`}},
		}},
		{Role: RoleTool, Content: "This is a very long tool result with lots of content that should be pruned because it exceeds the 200 character threshold for pruning. " + repeatString("A", 300), ToolCallID: "call_1"},
		{Role: RoleAssistant, Content: "Here are the results"},
		{Role: RoleUser, Content: "Great, let's continue"},
		{Role: RoleAssistant, Content: "Sure thing"},
		{Role: RoleUser, Content: "One more thing"},
		{Role: RoleAssistant, Content: "What is it?"},
	}

	pruned, count := c.pruneOldToolResults(messages)

	if count == 0 {
		t.Error("expected at least 1 tool result to be pruned")
	}
	if pruned[3].Content == "" || pruned[3].Content == messages[3].Content {
		t.Error("tool result content should have been replaced with summary")
	}
	if !stringsHasPrefix(pruned[3].Content, "[web_search]") {
		t.Errorf("expected web_search summary prefix, got: %s", pruned[3].Content)
	}
}

func TestFindTailCutByTokens(t *testing.T) {
	c := NewCompressor(CompressorConfig{ContextLength: 128000})

	messages := []Message{
		{Role: RoleSystem, Content: "System"},
		{Role: RoleUser, Content: "Hi"},
		{Role: RoleAssistant, Content: "Hello"},
		{Role: RoleUser, Content: "Search something"},
		{Role: RoleAssistant, Content: "Result A"},
		{Role: RoleUser, Content: "Do more"},
		{Role: RoleAssistant, Content: "Result B"},
		{Role: RoleUser, Content: "Final question"},
		{Role: RoleAssistant, Content: "Final answer"},
	}

	// Head = 3 (system + first exchange)
	// Budget = small — should cut somewhere in the middle
	cutIdx := c.findTailCutByTokens(messages, 3, 100)

	if cutIdx <= 3 {
		t.Errorf("cut index %d should be after head (3)", cutIdx)
	}
	if cutIdx >= len(messages) {
		t.Errorf("cut index %d should be before end (%d)", cutIdx, len(messages))
	}

	// The last user message must be in the tail
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleUser {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx >= 0 && cutIdx > lastUserIdx {
		t.Errorf("last user message at %d should be in tail (cut=%d)", lastUserIdx, cutIdx)
	}
}

func TestAlignBoundaryForward(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: "System"},
		{Role: RoleTool, Content: "result", ToolCallID: "c1"},
		{Role: RoleTool, Content: "result2", ToolCallID: "c2"},
		{Role: RoleAssistant, Content: "Response"},
	}

	idx := 1
	result := (&Compressor{}).alignBoundaryForward(messages, idx)
	if result != 3 {
		t.Errorf("expected 3, got %d", result)
	}
}

func TestAlignBoundaryBackward(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: "System"},
		{Role: RoleAssistant, Content: "Let me search", ToolCalls: []ToolCall{{ID: "c1"}}},
		{Role: RoleTool, Content: "result 1", ToolCallID: "c1"},
		{Role: RoleTool, Content: "result 2", ToolCallID: "c1"},
		{Role: RoleUser, Content: "Thanks"},
	}

	// Boundary at 4 — should pull back to 1 (before the assistant with tool_calls)
	idx := 4
	result := (&Compressor{}).alignBoundaryBackward(messages, idx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

func TestSanitizeToolPairs(t *testing.T) {
	c := NewCompressor(CompressorConfig{ContextLength: 128000})

	// Case 1: Orphaned tool result (no matching assistant tool_call)
	messages := []Message{
		{Role: RoleUser, Content: "Hi"},
		{Role: RoleAssistant, Content: "Hello"},
		{Role: RoleTool, Content: "orphaned result", ToolCallID: "call_orphan"},
	}

	result := c.sanitizeToolPairs(messages)
	if len(result) != 2 {
		t.Errorf("expected 2 messages after removing orphaned tool result, got %d", len(result))
	}

	// Case 2: Missing tool results (assistant has tool_calls but no matching tool message)
	messages2 := []Message{
		{Role: RoleUser, Content: "Search"},
		{Role: RoleAssistant, Content: "Calling", ToolCalls: []ToolCall{
			{ID: "call_1", Function: FunctionCall{Name: "search"}},
		}},
	}

	result2 := c.sanitizeToolPairs(messages2)
	if len(result2) != 3 {
		t.Errorf("expected 3 messages after inserting stub, got %d", len(result2))
	}
	if result2[2].Role != RoleTool {
		t.Errorf("expected third message to be tool role, got %s", result2[2].Role)
	}
}

func TestSummarizeToolResult(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     string
		content  string
		expected string
	}{
		{
			name:     "terminal",
			toolName: "terminal",
			args:     `{"command":"npm test"}`,
			content:  `{"exit_code": 0, "stdout": "PASS"}`,
			expected: "[terminal] ran `npm test` -> exit 0, 1 lines output",
		},
		{
			name:     "read_file",
			toolName: "read_file",
			args:     `{"path":"main.go","offset":1}`,
			content:  "package main\n\nfunc main() {}",
			expected: "[read_file] read main.go from line 1 (28 chars)",
		},
		{
			name:     "write_file",
			toolName: "write_file",
			args:     `{"path":"/tmp/test.txt","content":"line1\nline2\n"}`,
			content:  "success",
			expected: "[write_file] wrote to /tmp/test.txt (3 lines)",
		},
		{
			name:     "search_files",
			toolName: "search_files",
			args:     `{"pattern":"test","path":".","target":"content"}`,
			content:  `{"total_count": 42}`,
			expected: "[search_files] content search for 'test' in . -> 42 matches",
		},
		{
			name:     "web_search",
			toolName: "web_search",
			args:     `{"query":"golang context"}`,
			content:  "some results...",
			expected: "[web_search] query='golang context' (15 chars result)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SummarizeToolResult(tt.toolName, tt.args, tt.content)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestTruncateToolCallArgsJSON(t *testing.T) {
	// Should truncate long string values while keeping JSON valid
	args := `{"path":"config.yaml","content":"` + repeatString("A", 500) + `"}`
	result := TruncateToolCallArgsJSON(args, 50)
	if len(result) >= len(args) {
		t.Error("truncation did not reduce length")
	}
	if !stringsHasPrefix(result, `{"content":`) || !stringsContains(result, "...[truncated]") {
		t.Errorf("unexpected truncation result: %s", result)
	}

	// Non-JSON should be returned unchanged
	invalid := `{not valid json}`
	result2 := TruncateToolCallArgsJSON(invalid, 200)
	if result2 != invalid {
		t.Error("non-JSON should be returned unchanged")
	}

	// Short args should be unchanged
	short := `{"name":"test"}`
	result3 := TruncateToolCallArgsJSON(short, 200)
	if result3 != short {
		t.Error("short args should be unchanged")
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	msg := Message{
		Role:    RoleUser,
		Content: repeatString("hello ", 100), // ~600 chars / 4 = ~150 tokens
	}
	tokens := estimateMessageTokens(msg)
	if tokens <= 0 {
		t.Error("expected positive token count")
	}

	msgWithTC := Message{
		Role:    RoleAssistant,
		Content: "Here are the results",
		ToolCalls: []ToolCall{
			{Function: FunctionCall{Name: "search", Arguments: `{"q":"test"}`}},
		},
	}
	tokens2 := estimateMessageTokens(msgWithTC)
	if tokens2 <= 10 {
		t.Error("expected more than 10 tokens for assistant with tool call")
	}
}

func TestEstimateMessageTokensUsesProvidedCount(t *testing.T) {
	// When TokenCount > 0, it should be used instead of estimating from content
	msg := Message{
		Role:       RoleUser,
		Content:    "This is a very long message that would normally estimate to many tokens",
		TokenCount: 42,
	}
	tokens := estimateMessageTokens(msg)
	if tokens != 42 {
		t.Errorf("expected 42 (provided token count), got %d", tokens)
	}
}

func TestEstimateTotalTokensWithMixedSources(t *testing.T) {
	// Messages with known token counts + messages without → should use provided where available
	messages := []Message{
		{Role: RoleUser, Content: "short", TokenCount: 3},
		{Role: RoleAssistant, Content: repeatString("hello ", 50), TokenCount: 0}, // ~250 chars/4 = ~62 + 10 = ~72
		{Role: RoleUser, Content: "hi", TokenCount: 2},
	}

	total := estimateTokenCount(messages)
	// First: 3, Second: ~85 (300 chars/4 + 10), Third: 2 = ~90
	if total < 85 || total > 95 {
		t.Errorf("expected total around 90, got %d", total)
	}
}

func TestSerialization(t *testing.T) {
	c := NewCompressor(CompressorConfig{ContextLength: 128000})

	turns := []Message{
		{Role: RoleUser, Content: "Help me find info about X"},
		{Role: RoleAssistant, Content: "Sure, let me look that up", ToolCalls: []ToolCall{
			{ID: "c1", Function: FunctionCall{Name: "web_search", Arguments: `{"query":"X info"}`}},
		}},
		{Role: RoleTool, Content: "Found 5 results about X", ToolCallID: "c1"},
		{Role: RoleAssistant, Content: "Here's what I found about X..."},
	}

	result := c.serializeForSummary(turns)
	if result == "" {
		t.Fatal("serialization produced empty result")
	}
	if !stringsContains(result, "[USER]:") {
		t.Error("missing [USER] section")
	}
	if !stringsContains(result, "[ASSISTANT]:") {
		t.Error("missing [ASSISTANT] section")
	}
	if !stringsContains(result, "[TOOL RESULT") {
		t.Error("missing [TOOL RESULT] section")
	}
	if !stringsContains(result, "web_search") {
		t.Error("missing tool call name in serialization")
	}
}

func TestWithSummaryPrefix(t *testing.T) {
	result := withSummaryPrefix("Here is the summary")
	if !stringsHasPrefix(result, SummaryPrefix) {
		t.Error("result should start with summary prefix")
	}
	if !stringsContains(result, "Here is the summary") {
		t.Error("result should contain the original summary text")
	}

	// Already has prefix — should not double it
	result2 := withSummaryPrefix(SummaryPrefix + "\nExisting summary")
	if stringsCount(result2, SummaryPrefix) != 1 {
		t.Error("should not double the prefix")
	}
}

func TestDeepCompaction(t *testing.T) {
	c := NewCompressor(CompressorConfig{ContextLength: 128000})

	// Build a conversation big enough to be compressible
	messages := buildTestConversation(20)

	result := c.Compress(messages, 65000, "")

	// Without a summarizer, it'll return the original messages with fallback
	if !result.WasCompacted {
		t.Error("should trigger compaction for 20 messages")
	}
	if result.Summary != "" {
		// With summarizer, summary would be non-empty
		t.Logf("Compression ratio: %.2fx", result.Stats.CompressionRatio)
	}
}

func TestDeepCompactionWithFocusTopic(t *testing.T) {
	c := NewCompressor(CompressorConfig{ContextLength: 128000})
	messages := buildTestConversation(15)

	// Without summarizer, focus topic shouldn't crash
	result := c.Compress(messages, 65000, "database performance")
	if result.WasCompacted {
		t.Logf("Compressed %d → %d messages", result.Stats.MessagesBefore, result.Stats.MessagesAfter)
	}
}

func TestMultipleCompressions(t *testing.T) {
	c := NewCompressor(CompressorConfig{ContextLength: 128000})
	messages := buildTestConversation(25)

	// First compression
	result1 := c.Compress(messages, 70000, "")
	if !result1.WasCompacted {
		t.Skip("could not compact — may need more messages")
	}

	// Simulate more conversation after first compaction
	// (previous summary should be set for iterative update)
	c.SetPreviousSummary(result1.Summary)

	// Add some new messages
	extended := append(result1.Messages,
		Message{Role: RoleUser, Content: "Let's refine this further"},
		Message{Role: RoleAssistant, Content: "Here are the refinements..."},
	)

	// Second compression should use iterative update
	result2 := c.Compress(extended, 75000, "")
	_ = result2 // just check no panic
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func repeatString(s string, count int) string {
	result := make([]byte, len(s)*count)
	for i := 0; i < count; i++ {
		copy(result[i*len(s):], s)
	}
	return string(result)
}

func buildTestConversation(n int) []Message {
	msgs := []Message{
		{Role: RoleSystem, Content: "You are Diane, a helpful personal AI assistant."},
	}
	phrases := []string{
		"Search the web for info about X",
		"Read the file config.yaml",
		"Update the configuration with new settings",
		"Run the test suite",
		"Check the build status",
		"Deploy to staging",
		"Review the pull request",
		"Debug the connection issue",
		"Write unit tests for the auth module",
		"Refactor the database layer",
	}
	for i := 0; i < n && i < len(phrases)*2; i++ {
		phrase := phrases[i%len(phrases)]
		msgs = append(msgs,
			Message{Role: RoleUser, Content: phrase},
			Message{Role: RoleAssistant, Content: "I'll handle that for you. Let me start by looking into it."},
		)
	}
	return msgs
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func stringsContains(s, substr string) bool {
	return len(substr) == 0 || findString(s, substr) >= 0
}

func stringsCount(s, substr string) int {
	if len(substr) == 0 {
		return len(s) + 1
	}
	count := 0
	for {
		i := findString(s, substr)
		if i < 0 {
			break
		}
		count++
		s = s[i+len(substr):]
	}
	return count
}

func findString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
