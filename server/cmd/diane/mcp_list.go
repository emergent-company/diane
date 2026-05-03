package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

func cmdMCPList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	showTools := fs.Bool("tools", false, "Connect and list available tools from each enabled server")
	fs.Parse(args)

	if jsonOutput {
		emitJSON("ok", map[string]interface{}{
			"source":  "graph",
			"servers": []interface{}{},
			"message": "MCP servers are configured via the Memory Platform graph. Use the dashboard or 'diane mcp add' to manage them.",
		})
		return
	}

	fmt.Println("📋 MCP Servers  (configured via Memory Platform graph)")
	fmt.Println()
	fmt.Println("  MCP server configurations are now managed through the Memory Platform graph.")
	fmt.Println("  Use 'diane mcp add' to add a server, or visit the dashboard to manage them.")
	fmt.Println()

	// If --tools is set, try to connect via a proxy with no servers loaded
	if *showTools {
		proxy, err := mcpproxy.NewProxy(nil)
		if err != nil {
			fmt.Println("  (proxy not available)")
			return
		}
		defer proxy.Close()

		allTools, err := proxy.ListAllTools()
		if err != nil {
			fmt.Println("  (no connected servers)")
			return
		}
		if len(allTools) == 0 {
			fmt.Println("  (no connected servers)")
			return
		}
		// Group tools by server
		serverTools := make(map[string][]string)
		for _, t := range allTools {
			name, _ := t["name"].(string)
			server, _ := t["_server"].(string)
			if server != "" && name != "" {
				cleanName := strings.TrimPrefix(name, server+"_")
				serverTools[server] = append(serverTools[server], cleanName)
			}
		}
		for srv, tools := range serverTools {
			fmt.Printf("  ✓ %-25s %d tool%s\n", srv, len(tools), plural(len(tools)))
		}
	}
}

// plural returns "s" if n != 1, empty string otherwise.
func plural(n int) string {
	if n != 1 {
		return "s"
	}
	return ""
}
