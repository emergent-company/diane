package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
	"github.com/Emergent-Comapny/diane/internal/memory"
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
			fmt.Printf("   Syncing token to MP graph...")
			if err := syncTokenToGraph(*serverName, ""); err != nil {
				fmt.Printf(" ⚠️  %v\n", err)
			} else {
				fmt.Println(" ✅")
			}
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

	// Determine flow type: device flow or auth code flow
	if oauth.DeviceAuthURL != "" {
		// Device flow (e.g., GitHub Copilot)
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
	} else if oauth.AuthorizationURL != "" {
		// Auth code flow (e.g., infakt)
		token, err := mcpproxy.AuthenticateAuthCodeFlow(*serverName, oauth)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Authentication failed: %v\n", err)
			os.Exit(1)
		}
		_ = token
	} else {
		fmt.Fprintf(os.Stderr, "Error: no OAuth flow configured for server %q (need device_auth_url or authorization_url)\n", *serverName)
		os.Exit(1)
	}

	// Sync token to MP graph so other nodes can pull it
	if err := syncTokenToGraph(*serverName, ""); err != nil {
		fmt.Printf("⚠️  Token saved locally, but graph sync failed: %v\n", err)
	}

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
	// Sync token to MP graph
	if err := syncTokenToGraph(*serverName, ""); err != nil {
		log.Printf("⚠️ Token synced locally but graph sync failed: %v", err)
	}
}

func loadMCPConfig() *mcpproxy.Config {
	// Check if we have a discovered OAuth config for this server
	// that wasn't registered via 'diane mcp add' (common for HTTP MCP servers
	// like infakt where the relay auto-discovers OAuth endpoints).
	cfg := &mcpproxy.Config{
		Servers: []mcpproxy.ServerConfig{},
	}

	// Scan for discovered OAuth configs in ~/.diane/secrets/*-oauth-config.json
	secretDir := filepath.Join(os.Getenv("HOME"), ".diane", "secrets")
	entries, err := os.ReadDir(secretDir)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, "-oauth-config.json") || name == "test-server-oauth-config.json" {
				continue
			}
			serverName := strings.TrimSuffix(name, "-oauth-config.json")
			if serverName == "" {
				continue
			}
			discovered := mcpproxy.LoadDiscoveredConfig(serverName)
			if discovered == nil {
				continue
			}
			// If registration URL available but no client_id, auto-register
			if discovered.ClientID == "" && discovered.RegistrationURL != "" {
				log.Printf("[mcp-auth] No client_id for %s, attempting dynamic registration...", serverName)
				if clientID, regErr := mcpproxy.DynamicClientRegistration(discovered.RegistrationURL); regErr == nil {
					discovered.ClientID = clientID
					mcpproxy.SaveDiscoveredConfig(serverName, discovered)
					log.Printf("[mcp-auth] Registered client_id=%s for %s", clientID, serverName)
				}
			}
			cfg.Servers = append(cfg.Servers, mcpproxy.ServerConfig{
				Name:  serverName,
				Type:  "http",
				OAuth: discovered,
			})
		}
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

// syncTokenToGraph syncs an OAuth token to the MP graph as an MCPSecret
// so that other nodes matching the scope can pull it automatically.
func syncTokenToGraph(serverName, scope string) error {
	tokens, err := mcpproxy.LoadTokens(serverName)
	if err != nil {
		return fmt.Errorf("load token: %w", err)
	}
	if tokens.AccessToken == "" {
		return fmt.Errorf("no token found for %s", serverName)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	pc := cfg.Active()
	if pc == nil || pc.Token == "" {
		return fmt.Errorf("no active project config")
	}

	if scope == "" {
		scope = "instance:" + getInstanceID()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	value, _ := json.Marshal(tokens)

	bridge, err := memory.New(memory.Config{
		ServerURL:         pc.ServerURL,
		APIKey:            pc.Token,
		ProjectID:         pc.ProjectID,
		OrgID:             pc.OrgID,
		HTTPClientTimeout: 15 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("connect to MP: %w", err)
	}
	defer bridge.Close()

	if err := bridge.UpsertMCPSecret(ctx, &memory.MCPSecretRequest{
		Name:  serverName + ".json",
		Scope: scope,
		Value: string(value),
	}); err != nil {
		return fmt.Errorf("upsert mcp secret: %w", err)
	}

	return nil
}

// getInstanceID returns the instance ID from config or hostname.
func getInstanceID() string {
	cfg, err := config.Load()
	if err == nil {
		pc := cfg.Active()
		if pc != nil && pc.InstanceID != "" {
			return pc.InstanceID
		}
	}
	hostname, _ := os.Hostname()
	return hostname
}
