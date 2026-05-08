package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

func cmdMCPList(args []string) {
	// Parse simple flags
	showTools := false
	for _, a := range args {
		if a == "--tools" || a == "-tools" {
			showTools = true
		}
	}

	// Load project config
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to load config: %v\n", err)
		return
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "❌ No active project configured — run: diane project init\n")
		return
	}

	// Create bridge to query graph
	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to connect to Memory Platform: %v\n", err)
		return
	}
	defer bridge.Close()

	ctx := context.Background()

	// Get MCP proxy configs from the graph
	entries, err := bridge.ListMCPProxyConfigs(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to list MCP servers from graph: %v\n", err)
		return
	}

	// Parse entries into ServerConfig list
	servers := make([]mcpproxy.ServerConfig, 0, len(entries))
	for _, e := range entries {
		var sc mcpproxy.ServerConfig
		if err := json.Unmarshal([]byte(e.Config), &sc); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Skipping invalid config for %s: %v\n", e.Scope, err)
			continue
		}
		servers = append(servers, sc)
	}

	fmt.Println("📋 MCP Servers")
	fmt.Println()

	if len(servers) == 0 {
		fmt.Println("  (no servers configured)")
		fmt.Println()
		fmt.Println("  Add MCP servers to your project's graph as MCPProxyConfig objects.")
		return
	}

	// Collect tools if --tools is set
	var toolsPerServer map[string][]string
	if showTools {
		toolsPerServer = collectTools(servers)
	}

	// Print each server
	for _, s := range servers {
		status := "✓"
		if !s.Enabled {
			status = "✗"
		}

		// Build command display
		var cmdDisplay string
		switch s.Type {
		case "stdio":
			parts := []string{s.Command}
			parts = append(parts, s.Args...)
			cmdDisplay = strings.Join(parts, " ")
			if len(s.Env) > 0 {
				cmdDisplay += fmt.Sprintf("  [%d env vars]", len(s.Env))
			}
		case "http", "sse", "streamable-http", "remote":
			cmdDisplay = s.URL
			if cmdDisplay == "" {
				cmdDisplay = "(remote)"
			}
		default:
			if s.Command != "" {
				cmdDisplay = s.Command
			} else {
				cmdDisplay = "(no command)"
			}
		}

		label := "enabled"
		if !s.Enabled {
			label = "disabled"
		}

		fmt.Printf("  %s %-25s %s → %s\n", status, s.Name, label, cmdDisplay)

		// Show tools if available
		if showTools && s.Enabled {
			if tools, ok := toolsPerServer[s.Name]; ok {
				if len(tools) > 0 {
					if s.Type != "stdio" {
						// HTTP/remote server with tools — show auth status
						if _, err := mcpproxy.LoadTokens(s.Name); err == nil {
							fmt.Printf("     └ 🔐 Authenticated (%d tool%s available)\n", len(tools), plural(len(tools)))
						} else {
							fmt.Printf("     └ %d tool%s: %s\n", len(tools), plural(len(tools)), strings.Join(tools, ", "))
						}
					} else {
						fmt.Printf("     └ %d tool%s: %s\n", len(tools), plural(len(tools)), strings.Join(tools, ", "))
					}
				} else if s.Type != "stdio" {
					// HTTP/remote server with no tools — check auth status
					if _, err := mcpproxy.LoadTokens(s.Name); err == nil {
						fmt.Println("     └ 🔐 Authenticated")
					} else {
						fmt.Printf("     └ ⚠️  Not authenticated — run: diane mcp auth --server %s\n", s.Name)
					}
				} else {
					fmt.Println("     └ (no tools reported)")
				}
			} else {
				fmt.Println("     └ ❌ Failed to connect")
			}
		}
	}

	// Summary
	enabled := 0
	for _, s := range servers {
		if s.Enabled {
			enabled++
		}
	}
	fmt.Println()
	fmt.Printf("  %d server%s total — %d enabled, %d disabled\n",
		len(servers), plural(len(servers)), enabled, len(servers)-enabled)
}

// collectTools starts a temporary proxy to discover tools from enabled servers.
func collectTools(servers []mcpproxy.ServerConfig) map[string][]string {
	result := make(map[string][]string)

	proxy, err := mcpproxy.NewProxy(servers)
	if err != nil {
		// For tool discovery, graceful failure per server
		for _, s := range servers {
			if s.Enabled {
				result[s.Name] = nil // mark as failed
			}
		}
		return result
	}
	defer proxy.Close()

	allTools, err := proxy.ListAllTools()
	if err != nil {
		for _, s := range servers {
			if s.Enabled {
				result[s.Name] = nil
			}
		}
		return result
	}

	// Group tools by server (the prefix is serverName_)
	serverTools := make(map[string][]string)
	for _, t := range allTools {
		name, _ := t["name"].(string)
		server, _ := t["_server"].(string)
		if server != "" && name != "" {
			// Strip the server prefix to show clean tool names
			cleanName := strings.TrimPrefix(name, server+"_")
			serverTools[server] = append(serverTools[server], cleanName)
		}
	}

	for _, s := range servers {
		if s.Enabled {
			if tools, ok := serverTools[s.Name]; ok {
				result[s.Name] = tools
			} else if s.Type != "stdio" {
				// Remote/HTTP servers won't appear in proxy tools if not authenticated.
				// Check OAuth token status for better user feedback.
				if _, err := mcpproxy.LoadTokens(s.Name); err == nil {
					// Has stored tokens — authenticated but no tools returned
					result[s.Name] = []string{}
				} else {
					// No stored tokens — needs user to run auth command
					result[s.Name] = nil
				}
			} else {
				result[s.Name] = nil // failed
			}
		}
	}

	return result
}

// plural returns "s" if n != 1, empty string otherwise.
func plural(n int) string {
	if n != 1 {
		return "s"
	}
	return ""
}
