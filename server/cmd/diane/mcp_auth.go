package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

// osExit allows tests to override os.Exit for exit code verification.
var osExit = os.Exit

func cmdMCPAuth(args []string) {
	fs := flag.NewFlagSet("auth", flag.ExitOnError)
	serverName := fs.String("server", "", "Name of the MCP server to authenticate (required)")
	configPath := fs.String("config", "", "Path to MCP servers config (default: ~/.diane/mcp-servers.json)")
	fs.Parse(args)

	if *serverName == "" {
		fmt.Fprintf(os.Stderr, "Error: --server flag is required\n\n")
		fs.Usage()
		osExit(1)
	}

	// Load config
	path := *configPath
	if path == "" {
		path = mcpproxy.GetDefaultConfigPath()
	}

	cfg, err := mcpproxy.LoadConfig(path)
	if err != nil {
		if jsonOutput {
			emitJSON("error", map[string]string{"message": "Error loading config: " + err.Error()})
		} else {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		}
		osExit(1)
	}

	// Find the server
	var server *mcpproxy.ServerConfig
	for i, s := range cfg.Servers {
		if s.Name == *serverName {
			server = &cfg.Servers[i]
			break
		}
	}
	if server == nil {
		if jsonOutput {
			emitJSON("error", map[string]string{"message": fmt.Sprintf("Server %q not found in %s", *serverName, path)})
		} else {
			fmt.Fprintf(os.Stderr, "Error: server %q not found in %s\n", *serverName, path)
		}
		osExit(1)
	}

	// Check if OAuth is configured in the server config, or auto-discovered
	oauth := server.OAuth
	if oauth == nil {
		// Try loading auto-discovered OAuth config
		oauth = mcpproxy.LoadDiscoveredConfig(*serverName)
	}

	// If we have a discovered config (or config with endpoints) but no client_id,
	// check if dynamic client registration is available
	if oauth != nil && oauth.RegistrationURL != "" && oauth.ClientID == "" {
		if !jsonOutput {
			fmt.Printf("🔄 Registering client with authorization server...\n")
		}
		clientID, err := mcpproxy.DynamicClientRegistration(oauth.RegistrationURL)
		if err != nil {
			if jsonOutput {
				emitJSON("error", map[string]string{"message": "Dynamic client registration failed: " + err.Error()})
			} else {
				fmt.Fprintf(os.Stderr, "❌ Dynamic client registration failed: %v\n", err)
			}
			osExit(1)
		}
		oauth.ClientID = clientID
		// Save the updated config with the client_id back to disk
		_ = mcpproxy.SaveDiscoveredConfig(*serverName, oauth)
		if !jsonOutput {
			fmt.Printf("✅ Registered client: %s\n", clientID)
		}
	}

	if oauth == nil {
		// Check if tokens already exist (pre-authenticated)
		tokens, err := mcpproxy.LoadTokens(*serverName)
		if err == nil && tokens.AccessToken != "" {
			if jsonOutput {
				emitJSON("ok", map[string]interface{}{
					"server":     *serverName,
					"message":    "Already authenticated",
					"expires_at": tokens.ExpiresAt.Format(time.RFC3339),
				})
			} else {
				fmt.Printf("✅ %s is already authenticated (token expires at %s)\n", *serverName, tokens.ExpiresAt.Format(time.RFC3339))
			}
			return
		}
		if jsonOutput {
			emitJSON("error", map[string]string{"message": fmt.Sprintf("No OAuth configuration for server %q", *serverName)})
		} else {
			fmt.Fprintf(os.Stderr, "Error: no OAuth configuration for server %q\n", *serverName)
			fmt.Fprintf(os.Stderr, "Tip: run diane mcp relay first to auto-discover OAuth endpoints for HTTP servers\n")
		}
		osExit(1)
	}

	if !jsonOutput {
		fmt.Printf("🔐 Authenticating MCP server: %s\n\n", *serverName)
	}

	var token string
	if oauth.DeviceAuthURL != "" {
		token, err = mcpproxy.AuthenticateDeviceFlow(*serverName, oauth)
	} else if oauth.AuthorizationURL != "" {
		token, err = mcpproxy.AuthenticateAuthCodeFlow(*serverName, oauth)
	} else {
		if jsonOutput {
			emitJSON("error", map[string]string{"message": "No OAuth flow configured (need device_auth_url or authorization_url)"})
		} else {
			fmt.Fprintf(os.Stderr, "Error: no OAuth flow configured (need device_auth_url or authorization_url)\n")
		}
		osExit(1)
	}

	if err != nil {
		if jsonOutput {
			emitJSON("error", map[string]interface{}{
				"server":  *serverName,
				"message": "Authentication failed",
				"error":   err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "❌ Authentication failed: %v\n", err)
		}
		osExit(1)
	}

	_ = token

	if jsonOutput {
		emitJSON("ok", map[string]interface{}{
			"server":     *serverName,
			"message":    "Successfully authenticated",
			"token_path": mcpproxy.TokenPath(*serverName),
		})
	} else {
		fmt.Printf("\n✅ Successfully authenticated %s\n", *serverName)
		fmt.Printf("   Token saved to: %s\n", mcpproxy.TokenPath(*serverName))
	}
}
