// Package session provides the session runner and context compression.
//
// The compressor is a Go port of Hermes Agent's context_compressor.py.
// It manages the context window by pruning old tool outputs and summarizing
// middle conversation turns when the token count approaches the model limit.
package session

// MessageRole represents the role of a message sender.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
	RoleTool      MessageRole = "tool"
)

// ToolCall represents a function call the assistant made.
type ToolCall struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Function  FunctionCall `json:"function"`
}

// FunctionCall represents the function name and arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message is a single turn in a conversation.
type Message struct {
	Role        MessageRole `json:"role"`
	Content     string      `json:"content"`
	TokenCount  int         `json:"token_count,omitempty"` // actual token count from graph; 0 = unknown
	ToolCalls   []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID  string      `json:"tool_call_id,omitempty"`
}

// CompressorConfig configures the context compressor.
type CompressorConfig struct {
	// ThresholdPercent triggers compression at this % of context window (0.0-1.0).
	// Default: 0.50 (50%).
	ThresholdPercent float64

	// ProtectFirstN messages in the head (system prompt + first exchange).
	// Default: 3.
	ProtectFirstN int

	// SummaryTargetRatio determines the token budget for the summary.
	// The summary gets target_ratio * threshold_tokens tokens.
	// Default: 0.20 (20%).
	SummaryTargetRatio float64

	// CompactModel is an optional model override for the summarizer LLM call.
	// If empty, the main model is used.
	CompactModel string

	// ContextLength is the model's context window token limit.
	ContextLength int

	// QuietMode suppresses logging noise.
	QuietMode bool
}

// CompressionStats tracks what happened during compression.
type CompressionStats struct {
	MessagesBefore   int
	MessagesAfter    int
	TokensBefore     int
	TokensAfter      int
	PrunedTools      int
	SummarizedTurns  int
	CompressionRatio float64
}

// CompressionResult contains the compressed message list and metadata.
type CompressionResult struct {
	Messages     []Message
	Summary      string
	Stats        CompressionStats
	WasCompacted bool
}

// defaultCompressorConfig returns sane defaults for the compressor.
func defaultCompressorConfig(contextLength int) CompressorConfig {
	return CompressorConfig{
		ThresholdPercent:   0.50,
		ProtectFirstN:      3,
		SummaryTargetRatio: 0.20,
		CompactModel:       "",
		ContextLength:      contextLength,
		QuietMode:          false,
	}
}
