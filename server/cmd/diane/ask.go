package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
)

// --- Types (mirrored from emement.memory domain) ---

type agentQuestionStatus string

const (
	questionStatusPending   agentQuestionStatus = "pending"
	questionStatusAnswered  agentQuestionStatus = "answered"
	questionStatusExpired   agentQuestionStatus = "expired"
	questionStatusCancelled agentQuestionStatus = "cancelled"
)

type agentQuestionInteractionType string

const (
	interactionButtons      agentQuestionInteractionType = "buttons"
	interactionSelect       agentQuestionInteractionType = "select"
	interactionMultiSelect  agentQuestionInteractionType = "multi_select"
	interactionText         agentQuestionInteractionType = "text"
)

type agentQuestionOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type agentQuestionDTO struct {
	ID              string                       `json:"id"`
	RunID           string                       `json:"runId"`
	AgentID         string                       `json:"agentId"`
	ProjectID       string                       `json:"projectId"`
	Question        string                       `json:"question"`
	Options         []agentQuestionOption         `json:"options"`
	InteractionType agentQuestionInteractionType  `json:"interactionType"`
	Placeholder     string                       `json:"placeholder,omitempty"`
	MaxLength       int                          `json:"maxLength,omitempty"`
	Response        *string                      `json:"response,omitempty"`
	RespondedBy     *string                      `json:"respondedBy,omitempty"`
	RespondedAt     *string                      `json:"respondedAt,omitempty"`
	Status          agentQuestionStatus           `json:"status"`
	NotificationID  *string                      `json:"notificationId,omitempty"`
	CreatedAt       string                       `json:"createdAt"`
	UpdatedAt       string                       `json:"updatedAt"`
}

type questionListResponse struct {
	Data []agentQuestionDTO `json:"data"`
}

type respondRequest struct {
	Response string `json:"response"`
}

// cmdAsk implements the "diane ask" CLI command.
func cmdAsk(args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Config: %v\n", err)
		return
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "No project configured. Run 'diane init' first.\n")
		return
	}

	if len(args) == 0 {
		listQuestions(pc)
	} else {
		switch args[0] {
		case "list":
			listQuestions(pc)
		case "respond":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "Usage: diane ask respond <questionID> [response]\n")
				return
			}
			respondToQuestion(pc, args[1], strings.Join(args[2:], " "))
		default:
			fmt.Fprintf(os.Stderr, "Usage: diane ask [list|respond]\n")
		}
	}
}

// --- List Questions ---

func listQuestions(pc *config.ProjectConfig) {
	fmt.Printf("Fetching pending agent questions for project %s…\n\n", pc.ProjectID)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	questions, err := fetchQuestions(ctx, pc, questionStatusPending)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		return
	}

	if len(questions) == 0 {
		fmt.Println("No pending agent questions.")
		return
	}

	fmt.Printf("Found %d pending question(s):\n\n", len(questions))
	for i, q := range questions {
		printQuestion(i+1, q)
	}
}

func printQuestion(n int, q agentQuestionDTO) {
	fmt.Printf("  \033[1m#%d\033[0m  %s\n", n, q.Question)
	fmt.Printf("      ID:        %s\n", q.ID)
	fmt.Printf("      Run:       %s\n", truncate(q.RunID, 12))
	fmt.Printf("      Created:   %s\n", relTime(q.CreatedAt))
	fmt.Printf("      Type:      %s\n", q.InteractionType)
	if len(q.Options) > 0 {
		fmt.Printf("      Options:   %d choices\n", len(q.Options))
		for _, opt := range q.Options {
			desc := ""
			if opt.Description != "" {
				desc = " — " + opt.Description
			}
			fmt.Printf("                   • %s (%s)%s\n", opt.Label, opt.Value, desc)
		}
	}
	if q.Placeholder != "" {
		fmt.Printf("      Placeholder: %s\n", q.Placeholder)
	}
	fmt.Println()
}

// --- Respond to Question ---

func respondToQuestion(pc *config.ProjectConfig, questionID string, response string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// If no inline response, fetch the question first and prompt interactively
	if response == "" {
		questions, err := fetchQuestions(ctx, pc, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			return
		}
		var q *agentQuestionDTO
		for i := range questions {
			if questions[i].ID == questionID {
				q = &questions[i]
				break
			}
		}
		if q == nil {
			fmt.Fprintf(os.Stderr, "❌ Question %s not found.\n", questionID)
			return
		}
		if q.Status != questionStatusPending {
			fmt.Fprintf(os.Stderr, "❌ Question is already %s.\n", q.Status)
			return
		}

		fmt.Printf("Question: %s\n\n", q.Question)
		response = promptForResponse(q)
		if response == "" {
			fmt.Println("Cancelled.")
			return
		}
	}

	// Send the response
	reqBody, _ := json.Marshal(respondRequest{Response: response})
	serverURL := strings.TrimRight(pc.ServerURL, "/")
	url := fmt.Sprintf("%s/api/projects/%s/agent-questions/%s/respond", serverURL, pc.ProjectID, questionID)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to create request: %v\n", err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setAuthHeader(httpReq, pc.Token)

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Request failed: %v\n", err)
		return
	}
	defer httpResp.Body.Close()

	body, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode != 200 && httpResp.StatusCode != 202 {
		fmt.Fprintf(os.Stderr, "❌ Server responded %d: %s\n", httpResp.StatusCode, string(body))
		return
	}

	fmt.Println("✅ Response submitted. Agent is resuming…")
}

func promptForResponse(q *agentQuestionDTO) string {
	reader := bufio.NewReader(os.Stdin)

	switch q.InteractionType {
	case interactionButtons:
		return promptButtons(reader, q.Options)
	case interactionSelect:
		return promptSelect(reader, q.Options)
	case interactionMultiSelect:
		return promptMultiSelect(reader, q.Options)
	case interactionText:
		return promptText(reader, q.Placeholder, q.MaxLength)
	default:
		return promptText(reader, q.Placeholder, q.MaxLength)
	}
}

func promptButtons(reader *bufio.Reader, options []agentQuestionOption) string {
	fmt.Println("Choose an option:")
	for i, opt := range options {
		fmt.Printf("  %d) %s\n", i+1, opt.Label)
	}
	fmt.Print("Enter number [1]: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	idx := 0
	if input != "" {
		fmt.Sscanf(input, "%d", &idx)
		idx-- // zero-based
	}
	if idx < 0 || idx >= len(options) {
		idx = 0
	}
	fmt.Printf("→ %s\n\n", options[idx].Label)
	return options[idx].Value
}

func promptSelect(reader *bufio.Reader, options []agentQuestionOption) string {
	fmt.Println("Select an option:")
	for i, opt := range options {
		fmt.Printf("  %d) %s\n", i+1, opt.Label)
	}
	fmt.Print("Enter number: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	idx := 0
	fmt.Sscanf(input, "%d", &idx)
	idx--
	if idx < 0 || idx >= len(options) {
		fmt.Println("Invalid selection, using first option.")
		idx = 0
	}
	fmt.Printf("→ %s\n\n", options[idx].Label)
	return options[idx].Value
}

func promptMultiSelect(reader *bufio.Reader, options []agentQuestionOption) string {
	fmt.Println("Select options (comma-separated numbers, e.g. 1,3):")
	for i, opt := range options {
		fmt.Printf("  %d) %s\n", i+1, opt.Label)
	}
	fmt.Print("Enter: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return ""
	}

	var selected []string
	for _, part := range strings.Split(input, ",") {
		var idx int
		fmt.Sscanf(strings.TrimSpace(part), "%d", &idx)
		idx--
		if idx >= 0 && idx < len(options) {
			selected = append(selected, options[idx].Value)
		}
	}
	result := strings.Join(selected, ", ")
	fmt.Printf("→ %s\n\n", result)
	return result
}

func promptText(reader *bufio.Reader, placeholder string, maxLength int) string {
	if placeholder != "" {
		fmt.Printf("Enter response (%s):\n", placeholder)
	} else {
		fmt.Println("Enter response (Ctrl+D or empty line to finish, Ctrl+C to cancel):")
	}
	if maxLength > 0 {
		fmt.Printf("Max %d characters.\n", maxLength)
	}

	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(line, "\n\r")
		if line == "" {
			break
		}
		lines = append(lines, line)
		// Stop after 1 line for single-line input
		if len(lines) >= 1 {
			break
		}
	}
	result := strings.Join(lines, "\n")
	if maxLength > 0 && len(result) > maxLength {
		result = result[:maxLength]
	}
	return result
}

// --- HTTP Helpers ---

func fetchQuestions(ctx context.Context, pc *config.ProjectConfig, status agentQuestionStatus) ([]agentQuestionDTO, error) {
	serverURL := strings.TrimRight(pc.ServerURL, "/")
	url := fmt.Sprintf("%s/api/projects/%s/agent-questions", serverURL, pc.ProjectID)
	if status != "" {
		url += "?status=" + string(status)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	setAuthHeader(req, pc.Token)
	req.Header.Set("X-Project-ID", pc.ProjectID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("server responded %d: %s", resp.StatusCode, string(body))
	}

	var listResp questionListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		// Try plain array
		var arr []agentQuestionDTO
		if err2 := json.Unmarshal(body, &arr); err2 == nil {
			return arr, nil
		}
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if listResp.Data == nil {
		return nil, nil
	}

	// Sort by created_at descending
	sort.Slice(listResp.Data, func(i, j int) bool {
		return listResp.Data[i].CreatedAt > listResp.Data[j].CreatedAt
	})

	return listResp.Data, nil
}

func setAuthHeader(req *http.Request, token string) {
	if strings.HasPrefix(token, "emt_") {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set("X-API-Key", token)
	}
}

// --- Display helpers ---

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func relTime(iso string) string {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339, iso)
		if err != nil {
			return iso
		}
	}
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	}
}
