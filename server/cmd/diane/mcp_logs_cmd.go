package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// cmdMCPLogs displays MCP server logs from the local log HTTP server or local API.
// Usage: diane mcp logs [server-name] [--tail N] [--json]
func cmdMCPLogs(args []string) {
	tailN := 50
	showJSON := false
	serverNames := []string{}

	for _, a := range args {
		if a == "--json" || a == "-json" {
			showJSON = true
			continue
		}
		if strings.HasPrefix(a, "--tail=") {
			fmt.Sscanf(a, "--tail=%d", &tailN)
			continue
		}
		if strings.HasPrefix(a, "-tail=") {
			fmt.Sscanf(a, "-tail=%d", &tailN)
			continue
		}
		// Assume it's a server name
		if !strings.HasPrefix(a, "-") {
			serverNames = append(serverNames, a)
		}
	}

	if len(serverNames) == 0 {
		// No server name — list available servers with log counts
		listLogServers(showJSON)
		return
	}

	for _, name := range serverNames {
		entries := fetchLogs(name)
		if entries == nil {
			fmt.Fprintf(os.Stderr, "No log entries for %s (server not found or log server unreachable)\n", name)
			continue
		}
		if len(entries) == 0 {
			fmt.Printf("[%s] No log entries yet\n", name)
			continue
		}

		// Show last N entries
		start := 0
		if len(entries) > tailN {
			start = len(entries) - tailN
		}
		display := entries[start:]

		if showJSON {
			out, _ := json.MarshalIndent(display, "", "  ")
			if len(serverNames) > 1 {
				fmt.Printf("--- %s ---\n", name)
			}
			fmt.Println(string(out))
			continue
		}

		if len(serverNames) > 1 {
			fmt.Printf("═══ %s (%d entries) ═══\n", name, len(entries))
		} else {
			fmt.Printf("[%s] %d entries (showing last %d)\n", name, len(entries), len(display))
		}
		for _, e := range display {
			t := formatLogTime(e.Time)
			fmt.Printf("%s  %s\n", t, e.Message)
		}
	}
}

// logEntry mirrors mcpproxy.LogEntry for JSON decoding
type logEntry struct {
	Time    string `json:"time"`
	Message string `json:"message"`
}

// fetchLogs tries to get logs from the log HTTP server first, then local API
func fetchLogs(serverName string) []logEntry {
	// Try the dedicated MCP log HTTP server on :18990
	entries := tryFetch(fmt.Sprintf("http://127.0.0.1:18990/logs/%s", serverName))
	if entries != nil {
		return entries
	}
	// Fallback to local API
	entries = tryFetch(fmt.Sprintf("http://127.0.0.1:8890/api/mcp-servers/%s/logs", serverName))
	if entries != nil {
		return entries
	}
	return nil
}

func tryFetch(url string) []logEntry {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var result struct {
		Logs []logEntry `json:"logs"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	return result.Logs
}

func listLogServers(showJSON bool) {
	// Query the :18990 log server for all servers that have entries
	// Since there's no list endpoint, query the local API's MCP server list
	resp, err := http.Get("http://127.0.0.1:8890/api/mcp-servers")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to local API: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result struct {
		Servers []struct {
			Name   string `json:"name"`
			Status string `json:"status,omitempty"`
		} `json:"servers"`
	}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &result)

	if len(result.Servers) == 0 {
		fmt.Println("No MCP servers configured")
		return
	}

	// For each server, check log counts
	fmt.Printf("%-30s %-15s %s\n", "SERVER", "STATUS", "LOG LINES")
	fmt.Println(strings.Repeat("─", 65))
	for _, s := range result.Servers {
		entries := fetchLogs(s.Name)
		count := 0
		if entries != nil {
			count = len(entries)
		}
		fmt.Printf("%-30s %-15s %d\n", s.Name, s.Status, count)
	}
	fmt.Println()
	fmt.Println("Usage: diane mcp logs <server-name> [--tail N] [--json]")
}

func formatLogTime(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, iso)
		if err != nil {
			return iso
		}
	}
	return t.Format("15:04:05")
}
