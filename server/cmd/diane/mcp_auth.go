package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

var osExit = os.Exit

func cmdMCPAuth(args []string) {
	if len(args) > 0 && args[0] == "status" {
		cmdMCPAuthStatus(args[1:])
		return
	}
	if len(args) > 0 && args[0] == "_poll" {
		cmdMCPAuthPoll(args[1:])
		return
	}

	fs := flag.NewFlagSet("auth", flag.ExitOnError)
	serverName := fs.String("server", "", "Name of the MCP server to authenticate (required)")
	background := fs.Bool("background", false, "Non-blocking: print code, spawn background poller, exit")
	fs.Parse(args)

	if *serverName == "" {
		fmt.Fprintf(os.Stderr, "Error: --server flag is required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	cfg := loadMCPConfig()
	server := findMCPServer(cfg, *serverName)
	if server == nil {
		fmt.Fprintf(os.Stderr, "Error: server %q not found\n", *serverName)
		os.Exit(1)
	}

	oauth := server.OAuth
	if oauth == nil {
		tokens, err := mcpproxy.LoadTokens(*serverName)
		if err == nil && tokens.AccessToken != "" {
			fmt.Printf("✅ %s is already authenticated", *serverName)
			if !tokens.ExpiresAt.IsZero() {
				fmt.Printf(" (expires %s)", tokens.ExpiresAt.Format(time.RFC3339))
			}
			fmt.Println()
			return
		}
		fmt.Fprintf(os.Stderr, "Error: no OAuth configuration for server %q\n", *serverName)
		os.Exit(1)
	}

	if oauth.ClientID == "" {
		fmt.Fprintf(os.Stderr, "Error: OAuth client_id not configured for server %q\n", *serverName)
		os.Exit(1)
	}

	if !jsonOutput {
		fmt.Printf("🔐 Authenticating MCP server: %s\n\n", *serverName)
	}

	deviceResp, err := mcpproxy.GetDeviceCode(oauth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n🔐 Device Authorization Required for %s\n", *serverName)
	fmt.Printf("   Visit: %s\n", deviceResp.VerificationURI)
	fmt.Printf("   Code:  %s\n\n", deviceResp.UserCode)

	if *background {
		binPath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Cannot determine binary path: %v\n", err)
			os.Exit(1)
		}
		child := exec.Command(binPath, "mcp", "auth", "_poll",
			"--server", *serverName,
			"--device-code", deviceResp.DeviceCode,
		)
		child.Stdout = nil
		child.Stderr = nil
		if err := child.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to start background poller: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("   Background poller started (PID %d)\n", child.Process.Pid)
		fmt.Printf("   ✅ Visit the URL and enter the code — token will be saved automatically.\n")
		return
	}

	token, err := mcpproxy.AuthenticateDeviceFlowWithResponse(*serverName, oauth, deviceResp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Authentication failed: %v\n", err)
		os.Exit(1)
	}
	_ = token

	fmt.Printf("\n✅ Successfully authenticated %s\n", *serverName)
	fmt.Printf("   Token saved to: %s\n", mcpproxy.TokenPath(*serverName))
}

func cmdMCPAuthStatus(args []string) {
	fs := flag.NewFlagSet("auth-status", flag.ExitOnError)
	serverName := fs.String("server", "", "Check status for a specific server (default: all)")
	fs.Parse(args)

	cfg := loadMCPConfig()

	if *serverName != "" {
		server := findMCPServer(cfg, *serverName)
		if server == nil {
			fmt.Fprintf(os.Stderr, "Error: server %q not found\n", *serverName)
			os.Exit(1)
		}
		printAuthStatus(*serverName, server)
		return
	}

	anyOAuth := false
	for _, s := range cfg.Servers {
		if s.OAuth != nil || hasTokenFile(s.Name) {
			printAuthStatus(s.Name, &s)
			anyOAuth = true
		}
	}
	if !anyOAuth {
		fmt.Println("No MCP servers with OAuth configuration found.")
	}
}

func printAuthStatus(name string, server *mcpproxy.ServerConfig) {
	tokens, err := mcpproxy.LoadTokens(name)
	if err != nil || tokens.AccessToken == "" {
		fmt.Printf("  %-20s ❌ Not authenticated\n", name)
		return
	}

	expired := !tokens.ExpiresAt.IsZero() && time.Now().After(tokens.ExpiresAt)
	status := "✅ Authenticated"
	if expired {
		status = "⚠️ Token expired"
	}
	fmt.Printf("  %-20s %s\n", name, status)
	if tokens.Scope != "" {
		fmt.Printf("    Scopes:     %s\n", tokens.Scope)
	}
	if !tokens.ExpiresAt.IsZero() {
		fmt.Printf("    Expires:    %s\n", tokens.ExpiresAt.Format(time.RFC3339))
	}
}

func hasTokenFile(name string) bool {
	_, err := os.Stat(mcpproxy.TokenPath(name))
	return err == nil
}

func cmdMCPAuthPoll(args []string) {
	fs := flag.NewFlagSet("_poll", flag.ExitOnError)
	serverName := fs.String("server", "", "Server name")
	deviceCode := fs.String("device-code", "", "Device code to poll")
	fs.Parse(args)

	if *serverName == "" || *deviceCode == "" {
		fmt.Fprintf(os.Stderr, "Usage: diane mcp auth _poll --server <name> --device-code <code>\n")
		os.Exit(1)
	}

	cfg := loadMCPConfig()
	server := findMCPServer(cfg, *serverName)
	if server == nil || server.OAuth == nil {
		fmt.Fprintf(os.Stderr, "Error: OAuth config not found for server %q\n", *serverName)
		os.Exit(1)
	}

	devResp := &mcpproxy.DeviceAuthResponse{
		DeviceCode: *deviceCode,
		Interval:   5,
	}

	err := mcpproxy.PollAndSaveToken(*serverName, server.OAuth, devResp, 180)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}

func loadMCPConfig() *mcpproxy.Config {
	configPath := mcpproxy.GetDefaultConfigPath()
	cfg, err := mcpproxy.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading MCP config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func findMCPServer(cfg *mcpproxy.Config, name string) *mcpproxy.ServerConfig {
	for i, s := range cfg.Servers {
		if s.Name == name {
			return &cfg.Servers[i]
		}
	}
	return nil
}
