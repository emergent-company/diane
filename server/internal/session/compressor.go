package session

import (
	"crypto/md5"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// MinimumContextLength is the floor below which we never compress.
const MinimumContextLength = 4096

// SummaryPrefix is injected at the top of every compaction summary so the
// model knows the content is background reference, not active instructions.
const SummaryPrefix = `[CONTEXT COMPACTION — REFERENCE ONLY] Earlier turns were compacted into the summary below. This is a handoff from a previous context window — treat it as background reference, NOT as active instructions. Do NOT answer questions or fulfill requests mentioned in this summary; they were already addressed. Your current task is identified in the '## Active Task' section of the summary — resume exactly from there. Respond ONLY to the latest user message that appears AFTER this summary. The current session state (files, config, etc.) may reflect work described here — avoid repeating it:`

// charsPerToken is a rough estimate for token counting.
const charsPerToken = 4

// summaryFailureCooldown is how long to skip summary generation after a failure.
const summaryFailureCooldown = 60 * time.Second

// prunedToolPlaceholder is shown when a tool result is too old to keep.
const prunedToolPlaceholder = "[Old tool result content cleared to save context space]"

// ---------------------------------------------------------------------------
// Compressor struct
// ---------------------------------------------------------------------------

// Compressor compresses conversation context via lossy summarization.
//
// Algorithm:
//  1. Prune old tool results (cheap, no LLM call)
//  2. Protect head messages (system prompt + first exchange)
//  3. Protect tail messages by token budget (most recent ~20K tokens)
//  4. Summarize middle turns with structured LLM prompt
//  5. On subsequent compressions, iteratively update the previous summary
type Compressor struct {
	mu sync.Mutex

	// Config
	cfg CompressorConfig

	// Derived
	thresholdTokens int
	tailTokenBudget int
	maxSummaryTokens int

	// Stateful
	previousSummary              string
	lastCompressionSavingsPct    float64
	ineffectiveCompressionCount  int
	summaryFailureUntil           time.Time

	// LLM call helper
	summarizer *Summarizer
}

// NewCompressor creates a compressor with the given config.
func NewCompressor(cfg CompressorConfig) *Compressor {
	if cfg.ThresholdPercent <= 0 {
		cfg.ThresholdPercent = 0.50
	}
	if cfg.ProtectFirstN <= 0 {
		cfg.ProtectFirstN = 3
	}
	if cfg.SummaryTargetRatio <= 0 {
		cfg.SummaryTargetRatio = 0.20
	}
	if cfg.ContextLength <= 0 {
		cfg.ContextLength = MinimumContextLength
	}

	// Floor: never compress below minimum
	threshold := int(float64(cfg.ContextLength) * cfg.ThresholdPercent)
	if threshold < MinimumContextLength {
		threshold = MinimumContextLength
	}

	targetTokens := int(float64(threshold) * cfg.SummaryTargetRatio)
	clamp := func(v, min, max int) int {
		if v < min { return min }
		if v > max { return max }
		return v
	}
	maxSummary := clamp(int(float64(cfg.ContextLength)*0.05), 512, 8192)

	return &Compressor{
		cfg:                  cfg,
		thresholdTokens:      threshold,
		tailTokenBudget:      targetTokens,
		maxSummaryTokens:     maxSummary,
		lastCompressionSavingsPct: 100.0,
		previousSummary:      "",
	}
}

// SetSummarizer attaches an LLM summarizer to the compressor.
func (c *Compressor) SetSummarizer(s *Summarizer) {
	c.summarizer = s
}

// SetPreviousSummary sets the summary from a previous compression cycle
// so the next compression can do an iterative update.
func (c *Compressor) SetPreviousSummary(summary string) {
	c.previousSummary = summary
}

// ShouldCompress checks if the context exceeds the compression threshold.
// Includes anti-thrashing: if the last 2 compressions each saved < 10%,
// skip compression to avoid infinite loops.
func (c *Compressor) ShouldCompress(promptTokens int) bool {
	if promptTokens < c.thresholdTokens {
		return false
	}
	// Anti-thrashing
	if c.ineffectiveCompressionCount >= 2 {
		return false
	}
	return true
}

// Compress compresses the message list by summarizing middle turns.
// Returns the compressed messages, or the original list if compression is
// not possible.
func (c *Compressor) Compress(messages []Message, currentTokens int, focusTopic string) CompressionResult {
	c.mu.Lock()
	c.mu.Unlock()

	n := len(messages)
	minForCompress := c.cfg.ProtectFirstN + 3 + 1
	if n <= minForCompress {
		return CompressionResult{
			Messages:     messages,
			WasCompacted: false,
		}
	}

	displayTokens := currentTokens
	if displayTokens <= 0 {
		displayTokens = estimateTokenCount(messages)
	}

	// Phase 1: Prune old tool results (cheap, no LLM call)
	pruned, prunedCount := c.pruneOldToolResults(messages)
	if prunedCount > 0 && !c.cfg.QuietMode {
		logf("Pre-compression: pruned %d old tool result(s)", prunedCount)
	}

	// Phase 2: Determine boundaries
	compressStart := c.cfg.ProtectFirstN
	compressStart = c.alignBoundaryForward(pruned, compressStart)

	compressEnd := c.findTailCutByTokens(pruned, compressStart, c.tailTokenBudget)

	if compressStart >= compressEnd {
		return CompressionResult{
			Messages:     messages,
			WasCompacted: false,
		}
	}

	turnsToSummarize := pruned[compressStart:compressEnd]

	if !c.cfg.QuietMode {
		tailMsgs := n - compressEnd
		logf("Context compression triggered (%d tokens >= %d threshold)",
			displayTokens, c.thresholdTokens)
		logf("Model context limit: %d tokens (%.0f%% = %d)",
			c.cfg.ContextLength, c.cfg.ThresholdPercent*100, c.thresholdTokens)
		logf("Summarizing turns %d-%d (%d turns), protecting %d head + %d tail messages",
			compressStart+1, compressEnd, len(turnsToSummarize),
			compressStart, tailMsgs)
	}

	// Phase 3: Generate structured summary
	summary := c.generateSummary(turnsToSummarize, focusTopic)

	// Phase 4: Assemble compressed message list
	compressed := make([]Message, 0, compressStart+2)

	// Copy head messages
	for i := 0; i < compressStart; i++ {
		msg := pruned[i]
		if i == 0 && msg.Role == RoleSystem {
			note := "[Note: Some earlier conversation turns have been compacted into a handoff summary to preserve context space. The current session state may still reflect earlier work, so build on that summary and state rather than re-doing work.]"
			if !strings.Contains(msg.Content, note) {
				msg.Content = msg.Content + "\n\n" + note
			}
		}
		compressed = append(compressed, msg)
	}

	// If LLM summary failed, insert a static fallback
	if summary == "" {
		fallback := fmt.Sprintf("[Conversation turns %d-%d compressed — summary unavailable due to LLM error]",
			compressStart+1, compressEnd)
		compressed = append(compressed, Message{Role: RoleUser, Content: "What did we do so far?"})
		compressed = append(compressed, Message{Role: RoleAssistant, Content: fallback})
	} else {
		compressed = append(compressed, Message{Role: RoleUser, Content: "What did we do so far?"})
		compressed = append(compressed, Message{Role: RoleAssistant, Content: summary})
	}

	// Append tail messages
	compressed = append(compressed, pruned[compressEnd:]...)

	// Phase 5: Sanitize tool pairs
	compressed = c.sanitizeToolPairs(compressed)

	// Compute stats
	tokensBefore := estimateTokenCount(messages)
	tokensAfter := estimateTokenCount(compressed)
	savingsPct := 0.0
	if tokensBefore > 0 {
		savingsPct = 100.0 * (1.0 - float64(tokensAfter)/float64(tokensBefore))
	}
	c.lastCompressionSavingsPct = savingsPct
	if savingsPct < 10.0 {
		c.ineffectiveCompressionCount++
	} else {
		c.ineffectiveCompressionCount = 0
	}

	return CompressionResult{
		Messages: compressed,
		Summary:  summary,
		Stats: CompressionStats{
			MessagesBefore:  n,
			MessagesAfter:   len(compressed),
			TokensBefore:    tokensBefore,
			TokensAfter:     tokensAfter,
			PrunedTools:     prunedCount,
			SummarizedTurns: len(turnsToSummarize),
			CompressionRatio: func() float64 {
				if tokensAfter > 0 {
					return float64(tokensBefore) / float64(tokensAfter)
				}
				return 0
			}(),
		},
		WasCompacted: true,
	}
}

// Reset resets per-session state for /new or /reset.
func (c *Compressor) Reset() {
	c.previousSummary = ""
	c.lastCompressionSavingsPct = 100.0
	c.ineffectiveCompressionCount = 0
	c.summaryFailureUntil = time.Time{}
}

// ---------------------------------------------------------------------------
// Phase 1: Tool output pruning
// ---------------------------------------------------------------------------

func (c *Compressor) pruneOldToolResults(messages []Message) ([]Message, int) {
	if len(messages) == 0 {
		return messages, 0
	}

	result := copyMessages(messages)
	pruned := 0

	// Build index: tool_call_id -> (tool_name, arguments_json)
	callIDToTool := make(map[string]struct{ name, args string })
	for _, msg := range result {
		if msg.Role == RoleAssistant {
			for _, tc := range msg.ToolCalls {
				callIDToTool[tc.ID] = struct{ name, args string }{
					name: tc.Function.Name,
					args: tc.Function.Arguments,
				}
			}
		}
	}

	// Determine the prune boundary using token budget
	protectTailTokens := c.tailTokenBudget
	minProtect := min(c.cfg.ProtectFirstN, len(result)-1)

	accumulated := 0
	boundary := len(result)
	for i := len(result) - 1; i >= 0; i-- {
		msg := result[i]
		msgTokens := estimateMessageTokens(msg)
		if accumulated+msgTokens > protectTailTokens && (len(result)-i) >= minProtect {
			boundary = i
			break
		}
		accumulated += msgTokens
		boundary = i
	}
	pruneBoundary := max(boundary, len(result)-minProtect)

	// Pass 1: Deduplicate identical tool results
	type hashEntry struct {
		index int
		callID string
	}
	contentHashes := make(map[string]hashEntry) // md5 hash -> entry
	for i := len(result) - 1; i >= 0; i-- {
		msg := result[i]
		if msg.Role != RoleTool {
			continue
		}
		if len(msg.Content) < 200 {
			continue
		}
		h := fmt.Sprintf("%x", md5.Sum([]byte(msg.Content)))[:12]
		if _, exists := contentHashes[h]; exists {
			result[i] = Message{Role: RoleTool, Content: "[Duplicate tool output — same content as a more recent call]", ToolCallID: msg.ToolCallID}
			pruned++
		} else {
			contentHashes[h] = hashEntry{index: i, callID: msg.ToolCallID}
		}
	}

	// Pass 2: Replace old tool results with informative summaries
	for i := 0; i < pruneBoundary; i++ {
		msg := result[i]
		if msg.Role != RoleTool {
			continue
		}
		if msg.Content == "" || msg.Content == prunedToolPlaceholder {
			continue
		}
		if strings.HasPrefix(msg.Content, "[Duplicate tool output") {
			continue
		}
		if len(msg.Content) > 200 {
			callID := msg.ToolCallID
			toolInfo := callIDToTool[callID]
			summary := SummarizeToolResult(toolInfo.name, toolInfo.args, msg.Content)
			result[i] = Message{Role: RoleTool, Content: summary, ToolCallID: msg.ToolCallID}
			pruned++
		}
	}

	// Pass 3: Truncate large tool_call arguments in assistant messages outside the tail
	for i := 0; i < pruneBoundary; i++ {
		msg := result[i]
		if msg.Role != RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		newTCs := make([]ToolCall, 0, len(msg.ToolCalls))
		modified := false
		for _, tc := range msg.ToolCalls {
			if len(tc.Function.Arguments) > 500 {
				newArgs := TruncateToolCallArgsJSON(tc.Function.Arguments, 200)
				if newArgs != tc.Function.Arguments {
					tc.Function.Arguments = newArgs
					modified = true
				}
			}
			newTCs = append(newTCs, tc)
		}
		if modified {
			result[i].ToolCalls = newTCs
		}
	}

	return result, pruned
}

// ---------------------------------------------------------------------------
// Phase 2: Boundary selection
// ---------------------------------------------------------------------------

func (c *Compressor) alignBoundaryForward(messages []Message, idx int) int {
	for idx < len(messages) && messages[idx].Role == RoleTool {
		idx++
	}
	return idx
}

func (c *Compressor) alignBoundaryBackward(messages []Message, idx int) int {
	if idx <= 0 || idx >= len(messages) {
		return idx
	}
	check := idx - 1
	for check >= 0 && messages[check].Role == RoleTool {
		check--
	}
	if check >= 0 && messages[check].Role == RoleAssistant && len(messages[check].ToolCalls) > 0 {
		idx = check
	}
	return idx
}

func (c *Compressor) findLastUserMessageIdx(messages []Message, headEnd int) int {
	for i := len(messages) - 1; i >= headEnd; i-- {
		if messages[i].Role == RoleUser {
			return i
		}
	}
	return -1
}

func (c *Compressor) ensureLastUserMessageInTail(messages []Message, cutIdx, headEnd int) int {
	lastUserIdx := c.findLastUserMessageIdx(messages, headEnd)
	if lastUserIdx < 0 {
		return cutIdx
	}
	if lastUserIdx >= cutIdx {
		return cutIdx
	}
	// Pull cut back to include the last user message
	return max(lastUserIdx, headEnd+1)
}

func (c *Compressor) findTailCutByTokens(messages []Message, headEnd int, tokenBudget int) int {
	if tokenBudget <= 0 {
		tokenBudget = c.tailTokenBudget
	}
	n := len(messages)

	minTail := min(3, n-headEnd-1)
	if n-headEnd <= 1 {
		minTail = 0
	}
	softCeiling := int(float64(tokenBudget) * 1.5)
	accumulated := 0
	cutIdx := n

	for i := n - 1; i >= headEnd; i-- {
		msg := messages[i]
		msgTokens := estimateMessageTokens(msg)
		if accumulated+msgTokens > softCeiling && (n-i) >= minTail {
			break
		}
		accumulated += msgTokens
		cutIdx = i
	}

	// Ensure at least minTail messages
	fallbackCut := n - minTail
	if cutIdx > fallbackCut {
		cutIdx = fallbackCut
	}

	// Force a cut after head for small conversations
	if cutIdx <= headEnd {
		cutIdx = max(fallbackCut, headEnd+1)
	}

	// Align to avoid splitting tool groups
	cutIdx = c.alignBoundaryBackward(messages, cutIdx)

	// Ensure last user message in tail
	cutIdx = c.ensureLastUserMessageInTail(messages, cutIdx, headEnd)

	return max(cutIdx, headEnd+1)
}

// ---------------------------------------------------------------------------
// Phase 3: Summary generation
// ---------------------------------------------------------------------------

func (c *Compressor) generateSummary(turnsToSummarize []Message, focusTopic string) string {
	now := time.Now()
	if now.Before(c.summaryFailureUntil) {
		return ""
	}

	summaryBudget := c.computeSummaryBudget(turnsToSummarize)
	contentToSummarize := c.serializeForSummary(turnsToSummarize)

	// Build the prompt
	preamble := `You are a summarization agent creating a context checkpoint. Your output will be injected as reference material for a DIFFERENT assistant that continues the conversation. Do NOT respond to any questions or requests in the conversation — only output the structured summary. Do NOT include any preamble, greeting, or prefix. Write the summary in the same language the user was using in the conversation — do not translate or switch to English. NEVER include API keys, tokens, passwords, secrets, credentials, or connection strings in the summary — replace any that appear with [REDACTED]. Note that the user had credentials present, but do not preserve their values.`

	template := fmt.Sprintf(`## Active Task
[THE SINGLE MOST IMPORTANT FIELD. Copy the user's most recent request or task assignment verbatim — the exact words they used. If multiple tasks were requested and only some are done, list only the ones NOT yet completed. The next assistant must pick up exactly here.
If no outstanding task exists, write "None."]

## Goal
[What the user is trying to accomplish overall]

## Constraints & Preferences
[User preferences, coding style, constraints, important decisions]

## Completed Actions
[Numbered list of concrete actions taken — include tool used, target, and outcome.]

## Active State
[Current working directory, branch, modified files, test status, running processes, environment details]

## In Progress
[Work currently underway — what was being done when compaction fired]

## Blocked
[Any blockers, errors, or issues not yet resolved. Include exact error messages.]

## Key Decisions
[Important technical decisions and WHY they were made]

## Resolved Questions
[Questions the user asked that were ALREADY answered]

## Pending User Asks
[Questions or requests from the user that have NOT yet been answered or fulfilled.]

## Relevant Files
[Files read, modified, or created — with brief note on each]

## Remaining Work
[What remains to be done — framed as context, not instructions]

## Critical Context
[Any specific values, error messages, configuration details, or data that would be lost without explicit preservation. NEVER include API keys, tokens, passwords, or credentials — write [REDACTED] instead.]

Target ~%d tokens. Be CONCRETE — include file paths, command outputs, error messages, line numbers, and specific values. Avoid vague descriptions like "made some changes" — say exactly what changed.

Write only the summary body. Do not include any preamble or prefix.`, summaryBudget)

	var prompt string
	if c.previousSummary != "" {
		// Iterative update
		prompt = fmt.Sprintf(`%s

You are updating a context compaction summary. A previous compaction produced the summary below. New conversation turns have occurred since then and need to be incorporated.

PREVIOUS SUMMARY:
%s

NEW TURNS TO INCORPORATE:
%s

Update the summary using this exact structure. PRESERVE all existing information that is still relevant. ADD new completed actions to the numbered list (continue numbering). Move items from "In Progress" to "Completed Actions" when done. Move answered questions to "Resolved Questions". Update "Active State" to reflect current state. Remove information only if it is clearly obsolete. CRITICAL: Update "## Active Task" to reflect the user's most recent unfulfilled request — this is the most important field for task continuity.

%s`, preamble, c.previousSummary, contentToSummarize, template)
	} else {
		// First compaction
		prompt = fmt.Sprintf(`%s

Create a structured handoff summary for a different assistant that will continue this conversation after earlier turns are compacted. The next assistant should be able to understand what happened without re-reading the original turns.

TURNS TO SUMMARIZE:
%s

Use this exact structure:

%s`, preamble, contentToSummarize, template)
	}

	// Inject focus topic
	if focusTopic != "" {
		prompt += fmt.Sprintf(`

FOCUS TOPIC: "%s"
The user has requested that this compaction PRIORITISE preserving all information related to the focus topic above. For content related to "%s", include full detail — exact values, file paths, command outputs, error messages, and decisions. For content NOT related to the focus topic, summarise more aggressively (brief one-liners or omit if truly irrelevant). The focus topic sections should receive roughly 60-70%% of the summary token budget. Even for the focus topic, NEVER preserve API keys, tokens, passwords, or credentials — use [REDACTED].`, focusTopic, focusTopic)
	}

	// Call the LLM
	if c.summarizer == nil {
		return ""
	}

	summary, err := c.summarizer.Summarize(prompt, summaryBudget)
	if err != nil {
		c.summaryFailureUntil = time.Now().Add(summaryFailureCooldown)
		logf("Failed to generate context summary: %v. Further summary attempts paused for %.0fs.", err, summaryFailureCooldown.Seconds())
		return ""
	}

	c.summaryFailureUntil = time.Time{}
	c.previousSummary = summary
	return withSummaryPrefix(summary)
}

func (c *Compressor) computeSummaryBudget(turns []Message) int {
	tokens := estimateTokenCount(turns)
	budget := int(float64(tokens) * 0.20)
	minSummary := 256
	if budget < minSummary {
		budget = minSummary
	}
	if budget > c.maxSummaryTokens {
		budget = c.maxSummaryTokens
	}
	return budget
}

// serializeForSummary converts conversation turns into labeled text for the summarizer.
// Ported from Hermes' _serialize_for_summary().
func (c *Compressor) serializeForSummary(turns []Message) string {
	const contentMax = 6000
	const contentHead = 4000
	const contentTail = 1500
	const toolArgsMax = 1500
	const toolArgsHead = 1200

	var parts []string
	for _, msg := range turns {
		content := msg.Content

		// Tool results
		if msg.Role == RoleTool {
			toolID := msg.ToolCallID
			if len(content) > contentMax {
				content = content[:contentHead] + "\n...[truncated]...\n" + content[len(content)-contentTail:]
			}
			parts = append(parts, fmt.Sprintf("[TOOL RESULT %s]: %s", toolID, content))
			continue
		}

		// Assistant messages: include tool call names AND arguments
		if msg.Role == RoleAssistant {
			if len(content) > contentMax {
				content = content[:contentHead] + "\n...[truncated]...\n" + content[len(content)-contentTail:]
			}
			if len(msg.ToolCalls) > 0 {
				tcParts := make([]string, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					args := tc.Function.Arguments
					if len(args) > toolArgsMax {
						args = args[:toolArgsHead] + "..."
					}
					tcParts = append(tcParts, fmt.Sprintf("  %s(%s)", tc.Function.Name, args))
				}
				content += "\n[Tool calls:\n" + strings.Join(tcParts, "\n") + "\n]"
			}
			parts = append(parts, fmt.Sprintf("[ASSISTANT]: %s", content))
			continue
		}

		// User and other roles
		if len(content) > contentMax {
			content = content[:contentHead] + "\n...[truncated]...\n" + content[len(content)-contentTail:]
		}
		parts = append(parts, fmt.Sprintf("[%s]: %s", strings.ToUpper(string(msg.Role)), content))
	}

	return strings.Join(parts, "\n\n")
}

// withSummaryPrefix normalizes and adds the compaction handoff prefix.
func withSummaryPrefix(summary string) string {
	text := strings.TrimSpace(summary)
	if strings.HasPrefix(text, SummaryPrefix) {
		return text
	}
	return SummaryPrefix + "\n" + text
}

// sanitizeToolPairs removes orphaned tool calls and inserts stub results.
// Ported from Hermes' _sanitize_tool_pairs().
func (c *Compressor) sanitizeToolPairs(messages []Message) []Message {
	// Collect surviving call IDs (from assistant tool_calls)
	survivingIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == RoleAssistant {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					survivingIDs[tc.ID] = true
				}
			}
		}
	}

	// Collect result call IDs (from tool messages)
	resultIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == RoleTool && msg.ToolCallID != "" {
			resultIDs[msg.ToolCallID] = true
		}
	}

	// 1. Remove tool results whose call_id has no matching assistant tool_call
	orphanedResults := make(map[string]bool)
	for cid := range resultIDs {
		if !survivingIDs[cid] {
			orphanedResults[cid] = true
		}
	}
	filtered := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == RoleTool && orphanedResults[msg.ToolCallID] {
			continue
		}
		filtered = append(filtered, msg)
	}

	// 2. Add stub results for assistant tool_calls whose results were dropped
	missingResults := make(map[string]bool)
	for cid := range survivingIDs {
		if !resultIDs[cid] {
			missingResults[cid] = true
		}
	}
	if len(missingResults) > 0 {
		patched := make([]Message, 0, len(filtered)+len(missingResults))
		for _, msg := range filtered {
			patched = append(patched, msg)
			if msg.Role == RoleAssistant {
				for _, tc := range msg.ToolCalls {
					if missingResults[tc.ID] {
						patched = append(patched, Message{
							Role:       RoleTool,
							Content:    "[Result from earlier conversation — see context summary above]",
							ToolCallID: tc.ID,
						})
					}
				}
			}
		}
		filtered = patched
	}

	return filtered
}

// ---------------------------------------------------------------------------
// Token counting (rough estimates)
// ---------------------------------------------------------------------------

// estimateMessageTokens returns a rough token count for a single message.
// Uses msg.TokenCount if > 0 (actual count from graph), otherwise estimates
// from content length (4 chars per token heuristic).
func estimateMessageTokens(msg Message) int {
	if msg.TokenCount > 0 {
		return msg.TokenCount
	}
	total := len(msg.Content) / charsPerToken
	for _, tc := range msg.ToolCalls {
		total += len(tc.Function.Arguments) / charsPerToken
	}
	return total + 10 // overhead for role + metadata
}

// estimateTokenCount returns a rough token count for a list of messages.
func estimateTokenCount(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateMessageTokens(msg)
	}
	return total
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func copyMessages(src []Message) []Message {
	dst := make([]Message, len(src))
	copy(dst, src)
	return dst
}

func logf(format string, args ...interface{}) {
	// In production, use a real logger. For now, we use fmt.
	fmt.Printf("[compressor] "+format+"\n", args...)
}

func min(a, b int) int {
	if a < b { return a }
	return b
}

func max(a, b int) int {
	if a > b { return a }
	return b
}
