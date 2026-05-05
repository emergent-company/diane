// Package: main
// Bug report submission HTTP handler.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *localAPIServer) handleBugReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Title       string `json:"title"`
		Body        string `json:"body"`
		Labels      string `json:"labels,omitempty"`      // comma-separated
		Severity    string `json:"severity,omitempty"`    // critical|high|medium|low
		AppVersion  string `json:"app_version,omitempty"` // companion app version
		OSVersion   string `json:"os_version,omitempty"`
		LogSnippet  string `json:"log_snippet,omitempty"` // recent log lines
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.Title == "" {
		jsonError(w, http.StatusBadRequest, "title is required")
		return
	}

	// Build a structured issue body
	var body strings.Builder
	body.WriteString("## Bug Report\n\n")
	if req.Severity != "" {
		fmt.Fprintf(&body, "**Severity:** %s\n\n", req.Severity)
	}
	body.WriteString("### Details\n\n")
	body.WriteString(req.Body)
	body.WriteString("\n\n")
	if req.AppVersion != "" || req.OSVersion != "" {
		body.WriteString("### Environment\n\n")
		if req.AppVersion != "" {
			fmt.Fprintf(&body, "- **App Version:** %s\n", req.AppVersion)
		}
		if req.OSVersion != "" {
			fmt.Fprintf(&body, "- **macOS:** %s\n", req.OSVersion)
		}
		body.WriteString("\n")
	}
	if req.LogSnippet != "" {
		fmt.Fprintf(&body, "### Logs\n\n```\n%s\n```\n\n", req.LogSnippet)
	}
	body.WriteString("---\n_Reported automatically by Diane Companion App_")

	// Create GitHub issue via REST API
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		// Fallback: read token from gh CLI config
		token = readGHTokenFromConfig()
	}
	if token == "" {
		jsonError(w, http.StatusInternalServerError, "GH_TOKEN not set and no gh config found — cannot create GitHub issue")
		return
	}

	apiURL := "https://api.github.com/repos/emergent-company/diane/issues"

	// Default labels
	allLabels := "bug"
	if req.Labels != "" {
		allLabels = "bug," + req.Labels
	}

	payload := map[string]any{
		"title":  req.Title,
		"body":   body.String(),
		"labels": strings.Split(allLabels, ","),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("marshal payload: %v", err))
		return
	}

	httpReq, err := http.NewRequest("POST", apiURL, bytes.NewReader(payloadJSON))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create request: %v", err))
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept", "application/vnd.github.v3+json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "diane-companion-app")

	client := &http.Client{Timeout: 30 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("GitHub API request failed: %v", err))
		return
	}
	defer httpResp.Body.Close()

	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode != http.StatusCreated {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("GitHub API error (HTTP %d): %s", httpResp.StatusCode, string(respBody)))
		return
	}

	var result struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("parse GitHub response: %v", err))
		return
	}

	log.Printf("Bug report created: #%d — %s", result.Number, result.HTMLURL)

	jsonResponse(w, map[string]any{
		"issue_url":    result.HTMLURL,
		"issue_number": result.Number,
	})
}

// readGHTokenFromConfig reads the GitHub token from gh CLI's config file.
// Checks ~/.config/gh/hosts.yml and ~/.config/gh/config.yml for oauth_token
// values under the github.com host. Uses tab/space indentation to track nesting.
func readGHTokenFromConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	paths := []string{
		filepath.Join(home, ".config", "gh", "hosts.yml"),
		filepath.Join(home, ".config", "gh", "config.yml"),
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		inGitHubSection := false
		githubIndent := -1
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			// Count leading whitespace for indentation tracking
			indent := len(line) - len(strings.TrimLeft(line, " \t"))

			// Detect github.com host section start
			if !inGitHubSection && (trimmed == "github.com:" || trimmed == "github.com") {
				inGitHubSection = true
				githubIndent = indent
				continue
			}
			if !inGitHubSection {
				continue
			}
			// If we're back to the same or lesser indent level as github.com,
			// we've left the github.com section
			if indent <= githubIndent {
				inGitHubSection = false
				continue
			}
			// Look for oauth_token inside the github.com section
			if strings.HasPrefix(trimmed, "oauth_token:") {
				parts := strings.SplitN(trimmed, ":", 2)
				if len(parts) == 2 {
					token := strings.TrimSpace(parts[1])
					token = strings.Trim(token, "'\"")
					if token != "" && strings.HasPrefix(token, "gh") {
						return token
					}
				}
			}
		}
	}

	return ""
}
