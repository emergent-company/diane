package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
)

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

// ── Update node version in graph ──
	// Run regardless of relay/bot so the companion shows the real version even for
	// master-only nodes or nodes that haven't connected to relay yet.
	if hasInstance {
		instanceID := resolveInstanceID(pc, *instancePtr)
		go updateNodeVersionInGraph(pc.ServerURL, pc.Token, pc.ProjectID, instanceID)
	} else {
		// Try all known DianeNodeConfig instances when no instance_id is configured
		// (e.g., master-only configs that still want version display)
		go updateNodeVersionInGraph(pc.ServerURL, pc.Token, pc.ProjectID, "")
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

	// ── Start auto-upgrade loop ──
	if startAPI {
		startAutoUpgrade(pc)
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

// startAutoUpgrade launches a background goroutine that periodically checks
// GitHub for new releases and auto-upgrades the binary if auto_upgrade is enabled.
func startAutoUpgrade(pc *config.ProjectConfig) {
	// Check if auto-upgrade is explicitly disabled
	if pc.AutoUpgrade != nil && !*pc.AutoUpgrade {
		log.Println("[UPGRADE] Auto-upgrade disabled by config")
		return
	}

	// Skip for dev builds
	if Version == "dev" {
		log.Println("[UPGRADE] Dev build — skipping auto-upgrade")
		return
	}

	// Parse check interval (default 3m)
	interval := 3 * time.Minute
	if pc.UpgradeCheckInterval != "" {
		if d, err := time.ParseDuration(pc.UpgradeCheckInterval); err == nil {
			interval = d
		} else {
			log.Printf("[UPGRADE] Invalid upgrade_check_interval %q: %v — using default 3m", pc.UpgradeCheckInterval, err)
		}
	}

	log.Printf("[UPGRADE] Auto-upgrade enabled (check every %v)", interval)

	go func() {
		// Initial check after a short delay (let services stabilize)
		time.Sleep(30 * time.Second)
		runAutoUpgradeCheck()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			runAutoUpgradeCheck()
		}
	}()
}

// runAutoUpgradeCheck performs one cycle: check for update → download → swap → restart.
func runAutoUpgradeCheck() {
	home, _ := os.UserHomeDir()
	dianeDir := filepath.Join(home, ".diane")
	binDir := filepath.Join(dianeDir, "bin")
	symlinkPath := filepath.Join(binDir, "diane")

	tagName := checkForUpdate()
	if tagName == "" {
		return // up to date or check failed
	}

	log.Printf("[UPGRADE] New version %s available — starting auto-upgrade", tagName)

	// Fetch release assets
	repo := "emergent-company/diane"
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("[UPGRADE] Failed to fetch release: %v", err)
		return
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		log.Printf("[UPGRADE] Failed to fetch release: %v", err)
		return
	}
	defer resp.Body.Close()

	var release struct {
		TagName string         `json:"tag_name"`
		Assets  []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		log.Printf("[UPGRADE] Failed to parse release: %v", err)
		return
	}

	autoUpgrade(release.TagName, release.Assets, symlinkPath, binDir)

	// On macOS, write DMG trigger for companion app
	if runtime.GOOS == "darwin" {
		writeDMGTrigger(release.TagName)
	}
}
