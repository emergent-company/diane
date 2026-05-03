package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

func cmdMCPAdd(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	name := fs.String("name", "", "MCP server name (required)")
	scope := fs.String("scope", "all", "Node scope (all, instance:<id>, slave:*, master:*)")
	srvType := fs.String("type", "stdio", "Server type (stdio, http, streamable-http, sse)")
	command := fs.String("command", "", "Command path (for stdio type)")
	argsRaw := fs.String("args", "", "Command arguments, comma-separated (for stdio type)")
	url := fs.String("url", "", "URL (for http/sse type)")
	enabled := fs.Bool("enabled", true, "Enable on start")
	timeout := fs.Int("timeout", 60, "Tool call timeout in seconds")
	headerPairs := fs.String("headers", "", "HTTP headers as key=value pairs, comma-separated")
	envPairs := fs.String("env", "", "Environment variables as key=value pairs, comma-separated")
	fs.Parse(args)

	if *name == "" {
		fmt.Fprintf(os.Stderr, "Error: --name is required\n\n")
		fs.Usage()
		osExit(1)
	}

	// Parse --headers (key=val,key=val)
	headers := make(map[string]string)
	if *headerPairs != "" {
		for _, pair := range splitCommaPairs(*headerPairs) {
			k, v, ok := splitKeyVal(pair)
			if !ok {
				fmt.Fprintf(os.Stderr, "Error: invalid header pair: %q (use key=value)\n", pair)
				osExit(1)
			}
			headers[k] = v
		}
	}

	// Parse --env (key=val,key=val)
	env := make(map[string]string)
	if *envPairs != "" {
		for _, pair := range splitCommaPairs(*envPairs) {
			k, v, ok := splitKeyVal(pair)
			if !ok {
				fmt.Fprintf(os.Stderr, "Error: invalid env pair: %q (use key=value)\n", pair)
				osExit(1)
			}
			env[k] = v
		}
	}

	// Parse --args (comma-separated)
	var serverArgs []string
	if *argsRaw != "" {
		for _, a := range splitCommaPairs(*argsRaw) {
			serverArgs = append(serverArgs, strings.TrimSpace(a))
		}
	}

	// Build ServerConfig
	server := mcpproxy.ServerConfig{
		Name:    *name,
		Enabled: *enabled,
		Type:    *srvType,
		Command: *command,
		Args:    serverArgs,
		URL:     *url,
		Headers: headers,
		Env:     env,
		Timeout: *timeout,
	}

	// Serialize to JSON for the config field
	serverData, err := json.Marshal(server)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to serialize server config: %v\n", err)
		osExit(1)
	}

	if jsonOutput {
		emitJSON("ok", map[string]interface{}{
			"message": fmt.Sprintf("Added MCP server %q with scope %q", *name, *scope),
			"name":    *name,
			"scope":   *scope,
		})
		return
	}

	// Write to MP graph
	fmt.Printf("📦 Adding MCP server: %s\n", *name)
	fmt.Printf("   Scope:  %s\n", *scope)
	fmt.Printf("   Type:   %s\n", *srvType)
	if *command != "" {
		fmt.Printf("   Cmd:    %s\n", *command)
	}
	if len(serverArgs) > 0 {
		fmt.Printf("   Args:   %v\n", serverArgs)
	}
	if *url != "" {
		fmt.Printf("   URL:    %s\n", *url)
	}

	// Upsert to MP graph
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Failed to load config for graph sync: %v\n", err)
		fmt.Println("   (server not added)")
		osExit(1)
	}
	pc := cfg.Active()
	if pc == nil || pc.Token == "" {
		fmt.Fprintf(os.Stderr, "⚠️  No active project config for graph sync\n")
		fmt.Println("   (server not added)")
		osExit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	bridge, err := memory.New(memory.Config{
		ServerURL:         pc.ServerURL,
		APIKey:            pc.Token,
		ProjectID:         pc.ProjectID,
		OrgID:             pc.OrgID,
		HTTPClientTimeout: 15 * time.Second,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Failed to connect to MP: %v\n", err)
		fmt.Println("   (server not added)")
		osExit(1)
	}
	defer bridge.Close()

	err = bridge.UpsertMCPProxyConfig(ctx, &memory.MCPProxyConfigRequest{
		Scope:   *scope,
		Config:  string(serverData),
		Version: int(time.Now().Unix()),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Failed to sync to graph: %v\n", err)
		fmt.Println("   (server not added)")
		osExit(1)
	}

	fmt.Println("✅ Synced to Memory Platform graph")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  - All nodes matching scope will pick this up on next sync")
	fmt.Println("  - Run 'diane mcp reload' to reload local relay immediately")
	fmt.Println("  - Run 'diane mcp auth --server <name>' if OAuth is needed")
}

// splitCommaPairs splits a comma-separated string, trimming whitespace.
func splitCommaPairs(s string) []string {
	var pairs []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			pairs = append(pairs, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		pairs = append(pairs, s[start:])
	}
	return pairs
}

// splitKeyVal splits "key=value" into key and value.
func splitKeyVal(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
