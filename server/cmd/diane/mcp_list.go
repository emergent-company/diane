package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

func cmdMCPList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to MCP servers config (default: ~/.diane/mcp-servers.json)")
	showTools := fs.Bool("tools", false, "Connect and list available tools from each enabled server")
	fs.Parse(args)

	path := *configPath
	if path == "" {
		path = mcpproxy.GetDefaultConfigPath()
	}

	// Load config
	cfg, err := mcpproxy.LoadConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("📋 MCP Servers  (%s)\n", path)
			fmt.Println("  (no config file — create one to configure MCP servers)")
			return
		}
		fmt.Fprintf(os.Stderr, "❌ Failed to load config: %v\n", err)
		return
	}

	relPath := shortenHome(path)
	fmt.Printf("📋 MCP Servers  (%s)\n", relPath)
	fmt.Println()

	if len(cfg.Servers) == 0 {
		fmt.Println("  (no servers configured)")
		return
	}

	// Collect tools if --tools is set
	var toolsPerServer map[string][]string
	if *showTools {
		toolsPerServer = collectTools(path, cfg)
	}

	// Print each server
	for _, s := range cfg.Servers {
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
		case "http", "remote", "ws":
			cmdDisplay = "(remote)"
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
		if *showTools && s.Enabled {
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
	for _, s := range cfg.Servers {
		if s.Enabled {
			enabled++
		}
	}
	fmt.Println()
	fmt.Printf("  %d server%s total — %d enabled, %d disabled\n",
		len(cfg.Servers), plural(len(cfg.Servers)), enabled, len(cfg.Servers)-enabled)
}

// collectTools starts a temporary proxy to discover tools from enabled servers.
func collectTools(configPath string, cfg *mcpproxy.Config) map[string][]string {
	result := make(map[string][]string)

	proxy, err := mcpproxy.NewProxy(configPath)
	if err != nil {
		// For tool discovery, graceful failure per server
		for _, s := range cfg.Servers {
			if s.Enabled {
				result[s.Name] = nil // mark as failed
			}
		}
		return result
	}
	defer proxy.Close()

	allTools, err := proxy.ListAllTools()
	if err != nil {
		for _, s := range cfg.Servers {
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

	for _, s := range cfg.Servers {
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

// shortenHome replaces the home directory prefix with ~ for display.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return strings.Replace(path, home, "~", 1)
}

// plural returns "s" if n != 1, empty string otherwise.
func plural(n int) string {
	if n != 1 {
		return "s"
	}
	return ""
}

// getMCPServersConfigPath returns the default MCP servers config path,
// or the path from --config flag if present in args.
func getMCPServersConfigPath(args []string) string {
	for i, a := range args {
		if a == "--config" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, "--config=") {
			return strings.TrimPrefix(a, "--config=")
		}
	}
	return mcpproxy.GetDefaultConfigPath()
}

// ensureDianeDir creates ~/.diane if it doesn't exist.
func ensureDianeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".diane")
	os.MkdirAll(dir, 0755)
	return dir
}
