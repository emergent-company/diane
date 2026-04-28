package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Emergent-Comapny/diane/internal/config"
)

// cmdServe is the unified service command for Diane.
//
// It starts both the Discord bot and the MCP relay in a single process,
// eliminating the possibility of duplicate instances (one Gateway connection,
// one WebSocket relay, one PID file).
//
// Usage: diane serve [flags]
//
// Flags:
//
//	--pidfile <path>    PID lock file path (default: ~/.diane/serve.pid)
//	                    Set to "" to disable locking (force-start)
//	--instance <name>   MCP relay instance ID (from config if empty)
//
// What starts depends on config:
//   - Master nodes (mode: "" or "master") with DiscordBotToken set → Discord bot
//   - Master nodes with InstanceID set → MCP relay
//   - Slave nodes (mode: "slave") → MCP relay only
func cmdServe() {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	pidfileDefault := filepath.Join(os.Getenv("HOME"), ".diane", "serve.pid")
	pidfilePtr := fs.String("pidfile", pidfileDefault, "PID lock file path (empty = disable)")
	instancePtr := fs.String("instance", "", "MCP relay instance ID (from config if empty)")
	fs.Parse(os.Args[2:])

	// ── PID lock: atomic flock guards against duplicate instances ──
	acquirePIDLock(*pidfilePtr)
	defer releasePIDLock()

	// ── Load config ──
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[SERVE] Failed to load config: %v", err)
	}
	pc := cfg.Active()
	if pc == nil {
		log.Fatal("[SERVE] No active project. Run 'diane init' first.")
	}

	isMaster := pc.IsMaster()
	hasDiscord := pc.DiscordBotToken != ""
	hasInstance := instancePtr != nil && *instancePtr != "" || pc.InstanceID != ""
	startBot := isMaster && hasDiscord
	startRelay := hasInstance

	if !startBot && !startRelay {
		// On a master without any services configured, give a helpful message
		if isMaster && !hasDiscord && !hasInstance {
			log.Fatal(
				"[SERVE] Nothing to serve.\n" +
					"       Configure a Discord bot token or an instance ID in ~/.config/diane.yml\n" +
					"       Or run one of:\n" +
					"         diane bot          (Discord bot only)\n" +
					"         diane mcp relay    (MCP relay only)",
			)
		}
		if isMaster && !hasDiscord {
			log.Println("[SERVE] No Discord bot token configured — skipping Discord bot")
		}
		if !hasInstance {
			log.Println("[SERVE] No instance ID configured — skipping MCP relay")
		}
		if !startBot && !startRelay {
			return
		}
	}

	log.Printf("═══ Diane Serve ═══")
	log.Printf("  Mode:     %s", pc.ModeLabel())
	if startBot {
		log.Printf("  Discord:  ✓ bot enabled")
	}
	if startRelay {
		log.Printf("  Relay:    ✓ instance=%s", resolveInstanceID(pc, *instancePtr))
	}

	// ── Error collector ──
	errCh := make(chan error, 2)

	// ── Start Discord bot ──
	if startBot {
		go func() {
			log.Printf("[SERVE] Starting Discord bot...")
			errCh <- runBotOnce(pc)
		}()
	}

	// ── Start MCP relay ──
	if startRelay {
		go func() {
			instanceID := resolveInstanceID(pc, *instancePtr)
			relayURL := "wss://" + strings.TrimPrefix(pc.ServerURL, "https://") + "/api/mcp-relay/connect"

			log.Printf("[SERVE] Starting MCP relay (instance=%s)...", instanceID)

			relayCfg := MCPRelayConfig{
				RelayURL:     relayURL,
				InstanceID:   instanceID,
				ProjectToken: pc.Token,
			}

			// Sync config from graph
			syncConfigFromGraph(pc.ServerURL, pc.Token, pc.ProjectID, instanceID)

			cmdMCPRelay(relayCfg)
			errCh <- nil // relay exited cleanly
		}()
	}

	// ── Wait for first exit ──
	err = <-errCh
	if err != nil {
		log.Printf("[SERVE] Service exited with error: %v", err)
	} else {
		log.Printf("[SERVE] Service exited cleanly")
	}

	// Log what's still running for visibility
	if startBot && startRelay {
		log.Printf("[SERVE] Stopping all services")
	}
}

// resolveInstanceID returns the instance ID to use, preferring the CLI flag
// over the config value, and falling back to auto-generation.
func resolveInstanceID(pc *config.ProjectConfig, flagInstance string) string {
	if flagInstance != "" {
		return flagInstance
	}
	if pc.InstanceID != "" {
		return pc.InstanceID
	}
	return generateInstanceID()
}

func init() {
	// Make sure serve is listed in the help output
	_ = fmt.Sprintf("  serve           Start both Discord bot and MCP relay (unified service)")
}
