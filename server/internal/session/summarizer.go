package session

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Summarizer handles LLM calls for generating context summaries.
// The actual LLM call is abstracted behind an interface so different
// backends (Memory Platform chat API, direct LLM, fake for testing)
// can be plugged in.
type Summarizer struct {
	// CallLLM is the function that calls the LLM with a prompt and returns
	// the response text. It must respect the maxTokens budget.
	CallLLM func(ctx context.Context, prompt string, maxTokens int) (string, error)

	// Model is the model name/ID used for summarization.
	Model string
}

// NewSummarizer creates a summarizer that delegates to the given CallLLM.
func NewSummarizer(model string, callLLM func(ctx context.Context, prompt string, maxTokens int) (string, error)) *Summarizer {
	return &Summarizer{
		CallLLM: callLLM,
		Model:   model,
	}
}

// Summarize calls the LLM to generate a summary of the given content.
func (s *Summarizer) Summarize(prompt string, maxTokens int) (string, error) {
	if s.CallLLM == nil {
		return "", fmt.Errorf("summarizer: no LLM call function configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	response, err := s.CallLLM(ctx, prompt, maxTokens)
	if err != nil {
		return "", fmt.Errorf("summarizer: LLM call failed: %w", err)
	}

	return strings.TrimSpace(response), nil
}

// ---------------------------------------------------------------------------
// Default LLM caller using the Memory Platform's Chat Stream API
// ---------------------------------------------------------------------------

// MemorySummarizerCaller creates a CallLLM function that uses the Memory
// Platform's chat streaming endpoint. It reads the full stream response
// and returns it as a single string.
//
// This is the simplest way to make a non-streaming LLM call via Memory
// Platform — we stream and collect.
func MemorySummarizerCaller(
	streamFn func(ctx context.Context, message string) (<-chan string, error),
) func(ctx context.Context, prompt string, maxTokens int) (string, error) {
	return func(ctx context.Context, prompt string, maxTokens int) (string, error) {
		ch, err := streamFn(ctx, prompt)
		if err != nil {
			return "", fmt.Errorf("stream start: %w", err)
		}

		var parts []string
		for chunk := range ch {
			parts = append(parts, chunk)
		}

		result := strings.Join(parts, "")
		if result == "" {
			return "", io.ErrUnexpectedEOF
		}
		return result, nil
	}
}
