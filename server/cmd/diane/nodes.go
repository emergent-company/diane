package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Emergent-Comapny/diane/internal/config"
)

// relaySession represents a connected MCP relay instance from the MP API.
type relaySession struct {
	InstanceID  string `json:"instance_id"`
	Hostname    string `json:"hostname,omitempty"`
	Version     string `json:"version,omitempty"`
	Tools       any    `json:"tools,omitempty"`
	ConnectedAt string `json:"connected_at,omitempty"`
}

func cmdNodes() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintln(os.Stderr, "❌ No active project configured. Run 'diane init' first.")
		os.Exit(1)
	}

	relayURL := strings.TrimSuffix(pc.ServerURL, "/") + "/api/mcp-relay/sessions"

	req, err := http.NewRequest("GET", relayURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to create request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+pc.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to query relay sessions: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to read response: %v\n", err)
		os.Exit(1)
	}

	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "❌ Relay API returned %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	// Parse response — could be array, {"items":[...]}, {"data":[...]}, or {"sessions":[...]}
	var sessions []relaySession
	if err := json.Unmarshal(body, &sessions); err != nil {
		// Try wrapped response formats
		var wrapped struct {
			Items    []relaySession `json:"items"`
			Data     []relaySession `json:"data"`
			Sessions []relaySession `json:"sessions"`
		}
		if err2 := json.Unmarshal(body, &wrapped); err2 == nil {
			switch {
			case wrapped.Sessions != nil:
				sessions = wrapped.Sessions
			case wrapped.Items != nil:
				sessions = wrapped.Items
			case wrapped.Data != nil:
				sessions = wrapped.Data
			}
		}
		if sessions == nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to parse relay sessions response\n   Raw: %s\n", string(body))
			os.Exit(1)
		}
	}

	if len(sessions) == 0 {
		fmt.Println("🌐 No connected relay nodes")
		return
	}

	fmt.Printf("🌐 Connected Relay Nodes (%d)\n", len(sessions))
	fmt.Println(strings.Repeat("─", 60))
	for _, s := range sessions {
		id := s.InstanceID
		if id == "" {
			id = "(unknown)"
		}
		host := s.Hostname
		if host == "" {
			host = "(unknown)"
		}
		ver := s.Version
		if ver == "" {
			ver = "-"
		}

		// Count tools if available
		toolCount := ""
		if s.Tools != nil {
			switch t := s.Tools.(type) {
			case map[string]interface{}:
				if tl, ok := t["tools"].([]interface{}); ok {
					toolCount = fmt.Sprintf(" (%d tools)", len(tl))
				}
			}
		}

		fmt.Printf("  📡 %s%s\n", id, toolCount)
		fmt.Printf("     Host:     %s\n", host)
		fmt.Printf("     Version:  %s\n", ver)
		if s.ConnectedAt != "" {
			fmt.Printf("     Since:    %s\n", s.ConnectedAt)
		}
		fmt.Println()
	}
}
