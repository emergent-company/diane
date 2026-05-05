package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
)

// httpClient is a shared HTTP client with a 15-second timeout,
// used across all production API calls.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// cmdServe is the unified service command for Diane.
//
// It starts the Discord bot, the MCP relay, and the local companion API
// in a single process, eliminating the possibility of duplicate instances.
//
// Usage: diane serve [flags]
//
// Flags:
//
//	--pidfile <path>    PID lock file path (default: ~/.diane/serve.pid)
//	                    Set to "" to disable locking (force-start)
//	--instance <name>   MCP relay instance ID (from config if empty)
//	--api-port <port>   Local companion API port (default: 8890, set to 0 to disable)
func cmdServe() {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	pidfileDefault := filepath.Join(os.Getenv("HOME"), ".diane", "serve.pid")
	pidfilePtr := fs.String("pidfile", pidfileDefault, "PID lock file path (empty = disable)")
	instancePtr := fs.String("instance", "", "MCP relay instance ID (from config if empty)")
	apiPort := fs.Int("api-port", 8890, "Local companion API port (0 = disable)")
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
	startAPI := *apiPort > 0

	if !startBot && !startRelay {
		if startAPI {
			log.Printf("[SERVE] Local API running on port %d (no bot or relay configured)", *apiPort)
		} else {
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
			if !startBot && !startRelay && !startAPI {
				return
			}
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
	if startAPI {
		log.Printf("  LocalAPI: ✓ port=%d", *apiPort)
	}

	// ── Signal handler: catch SIGTERM/SIGINT for graceful shutdown ──
	// The relay's own signal handler also catches these, so both fire.
	// Whichever arrives first triggers the select below.
	shutdownCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("[SERVE] Received %v, shutting down...", sig)
		cancel()
	}()

	// ── Error collector ──
	errCh := make(chan error, 3)

	// ── Start local companion API ──
	var apiServer *localAPIServer
	if startAPI {
		as, err := startLocalAPI(pc, *apiPort)
		if err != nil {
			log.Printf("[SERVE] Failed to start local API: %v", err)
		} else {
			apiServer = as
			defer apiServer.close()
		}
	}

	// ── Start Discord bot ──
	if startBot {
		go func() {
			log.Printf("[SERVE] Starting Discord bot...")
			errCh <- runBotOnce(pc)
		}()
	}

	// ── Compute instance ID ──
	instanceID := resolveInstanceID(pc, *instancePtr)
	if startRelay {
		log.Printf("[SERVE] Relay:    ✓ instance=%s", instanceID)
	}

	// ── Start MCP relay ──
	if startRelay {
		go func() {
			relayURL := "wss://" + strings.TrimPrefix(pc.ServerURL, "https://") + "/api/mcp-relay/connect"

			log.Printf("[SERVE] Starting MCP relay (instance=%s)...", instanceID)

			relayCfg := MCPRelayConfig{
				RelayURL:     relayURL,
				ServerURL:    pc.ServerURL,
				ProjectID:    pc.ProjectID,
				InstanceID:   instanceID,
				ProjectToken: pc.Token,
			}

			// Sync config from graph and pass servers to relay
			servers := syncConfigFromGraph(pc.ServerURL, pc.Token, pc.ProjectID, instanceID)

			cmdMCPRelay(relayCfg, servers)
			errCh <- nil // relay exited cleanly
		}()
	}

	// ── Node status registration + heartbeat ──
	// Registers this node in the MP graph with version, provider, uptime, etc.
	// Runs on ALL serve instances (not just relay) so every node is discoverable.
	// Re-upserts every 5 minutes to keep last_seen fresh.
	go startNodeHeartbeat(shutdownCtx, pc, instanceID, startRelay, startBot)

	// ── Auto-upgrade background check ──
	// Runs on all nodes. Checks for new releases at startup and periodically.
	// If auto_upgrade is enabled (default true), downloads and installs automatically.
	if pc.AutoUpgrade || !hasAutoUpgradeConfig(pc) {
		interval := pc.UpgradeCheckInterval
		if interval <= 0 {
			interval = 6 * time.Hour
		}
		log.Printf("[SERVE] Auto-upgrade: ✓ (check every %v)", interval)
		go autoUpgradeLoop(shutdownCtx, interval)
	} else {
		log.Printf("[SERVE] Auto-upgrade: disabled (set auto_upgrade: true in config)")
	}

	// Watch for AgentToolConfig changes via SSE and auto-seed agents
	// Runs on any node with a valid project config (not relay-dependent)
	go agentToolConfigWatch(shutdownCtx, pc.ServerURL, pc.Token, pc.ProjectID, pc.OrgID)

	// ── Wait for first exit or shutdown signal ──
	select {
	case err = <-errCh:
		if err != nil {
			log.Printf("[SERVE] Service exited with error: %v", err)
		} else {
			log.Printf("[SERVE] Service exited cleanly")
		}
	case <-shutdownCtx.Done():
		log.Printf("[SERVE] Shutdown requested — stopping all services")
	}

	log.Printf("[SERVE] All services stopped")
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

// autoUpgradeLoop periodically checks for updates and applies them automatically.
func autoUpgradeLoop(ctx context.Context, interval time.Duration) {
	// Wait a few seconds for the relay to start before checking
	select {
	case <-time.After(10 * time.Second):
	case <-ctx.Done():
		return
	}

	// Run one check immediately at startup
	log.Printf("[UPGRADE] Checking for updates at startup...")
	runAutoUpgrade()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Printf("[UPGRADE] Checking for updates...")
			runAutoUpgrade()
		case <-ctx.Done():
			log.Printf("[UPGRADE] Stopped")
			return
		}
	}
}

// runAutoUpgrade performs a single auto-upgrade check and applies the update if available.
// Logs results but does NOT exit on failure (runs in background).
func runAutoUpgrade() {
	latestVer, currentVer, available := checkVersion()
	if !available {
		log.Printf("[UPGRADE] Already up to date (%s)", currentVer)
		return
	}

	// Don't auto-upgrade if current version is "dev" (local build)
	if currentVer == "vdev" || currentVer == "dev" {
		log.Printf("[UPGRADE] Skipping auto-upgrade: local build (dev)")
		return
	}

	log.Printf("[UPGRADE] Update available: %s → %s", currentVer, latestVer)

	// Download new binary
	if err := downloadAndStage(latestVer); err != nil {
		log.Printf("[UPGRADE] Download failed: %v", err)
		return
	}
	log.Printf("[UPGRADE] Downloaded %s, staging...", latestVer)

	// Backup current binary
	if err := backupCurrentBinary(currentVer); err != nil {
		log.Printf("[UPGRADE] Backup failed: %v", err)
	}

	// Resolve symlinks
	targetPath := upgradeBinaryPath()
	var symlinkTarget string
	if fi, err := os.Lstat(targetPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if resolved, err := os.Readlink(targetPath); err == nil && filepath.IsAbs(resolved) {
			symlinkTarget = resolved
			targetPath = resolved
		}
	}

	// Swap staged → active
	staged := stagedBinaryPath()
	if err := swapBinary(staged, targetPath); err != nil {
		log.Printf("[UPGRADE] Install failed: %v", err)
		tryRollback()
		return
	}

	if symlinkTarget != "" {
		os.Remove(upgradeBinaryPath())
		os.Symlink(targetPath, upgradeBinaryPath())
	}

	// Verify
	if _, err := verifyBinary(targetPath); err != nil {
		log.Printf("[UPGRADE] Verification failed: %v", err)
		tryRollback()
		return
	}

	// Update state
	st := readUpgradeState()
	st.Current = latestVer
	st.Previous = currentVer
	st.PrevPath = previousBinaryPath()
	writeUpgradeState(st)

	log.Printf("[UPGRADE] ✅ Auto-upgraded to %s — restarting serve", latestVer)

	// Restart serve to pick up the new binary
	if err := restartServeProcess(); err != nil {
		log.Printf("[UPGRADE] Restart failed: %v", err)
	}
}

// hasAutoUpgradeConfig returns true if the user has explicitly set auto_upgrade in config.
// The zero value of bool is 'false', which we interpret as "not set" vs "explicitly disabled".
func hasAutoUpgradeConfig(pc *config.ProjectConfig) bool {
	// Check if config was loaded from file with auto_upgrade explicitly set
	// We can check this by re-reading the raw YAML
	data, err := os.ReadFile(config.Path())
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "auto_upgrade:")
}
