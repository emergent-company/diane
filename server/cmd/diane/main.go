// Command diane is the main Diane CLI.
//
// Subcommands:
//
//	init    Set up a new project (creates ~/.config/diane.yml)
//	bot     Start the Discord bot
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/Emergent-Comapny/diane/internal/schema"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: diane <command>")
		fmt.Println("")
		fmt.Println("Commands:")
		fmt.Println("  init            Set up a new Memory project")
		fmt.Println("  bot             Start the Discord bot")
		fmt.Println("  projects        List configured projects")
		fmt.Println("  provider        Configure LLM providers (list, set, test, sync)")
		fmt.Println("  agent           Manage agent definitions (list, define, show, sync, delete)")
		fmt.Println("  doctor          Check connection with Memory Platform and run diagnostics")
		fmt.Println("  monitor         Show bot status, sessions, recent activity")
		fmt.Println("  mcp relay       Connect MCP server to Memory Platform relay")
		fmt.Println("  mcp serve       Run MCP JSON-RPC server (stdin/stdout)")
		fmt.Println("  schema apply    Apply embedded schema definitions to Memory Platform")
		fmt.Println()
	}

	switch os.Args[1] {
	case "init":
		cmdInit()
	case "bot":
		cmdBot()
	case "projects":
		cmdProjects()
	case "agent":
		cmdAgent(os.Args[2:])
	case "provider":
		cmdProvider(os.Args[2:])
	case "doctor":
		cmdDoctor()
	case "monitor":
		cmdMonitor()
	case "schema":
		cmdSchema(os.Args[2:])
	case "mcp":
		if len(os.Args) < 3 {
			fmt.Println("Usage: diane mcp relay [--instance <name>] [--relay <url>]")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "relay":
			runMCPRelay(os.Args[3:])
		case "serve":
			cmdMCPServe()
		default:
			fmt.Fprintf(os.Stderr, "Unknown mcp command: %s\n", os.Args[2])
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// cmdInit walks the user through setting up a new project.
// It discovers the project ID from the token automatically.
func cmdInit() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("=== Diane Init ===")
	fmt.Println("This will create ~/.config/diane.yml with your Memory Platform credentials.")
	fmt.Println()

	// Load existing config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// ── Project name ──
	fmt.Print("Project name [default]: ")
	name := readLine(reader)
	if name == "" {
		name = "default"
	}

	// ── Memory server URL ──
	fmt.Print("Memory server URL [https://memory.emergent-company.ai]: ")
	serverURL := readLine(reader)
	if serverURL == "" {
		serverURL = "https://memory.emergent-company.ai"
	}

	// ── Project token ──
	fmt.Print("Memory project token (emt_...): ")
	token := readLine(reader)
	token = strings.TrimSpace(token)
	if token == "" {
		log.Fatal("Project token is required. Create one at the Memory Platform UI or use 'memory tokens create'.")
	}

	// ── Discover project ID from the token ──
	// We create a lightweight SDK call — a simple objects list with limit=0
	// validates the token. If it fails, we ask for the project ID manually.
	projectID := ""
	fmt.Print("Discovering project from token...\n")

	bridge, err := memory.New(memory.Config{
		ServerURL: serverURL,
		APIKey:    token,
	})
	if err != nil {
		fmt.Printf("  ⚠️  Could not create bridge: %v\n", err)
	} else {
		defer bridge.Close()
		// Try listing sessions — works with data:read scope which every token has
		sessions, err := bridge.ListSessions(context.Background(), "")
		if err != nil {
			fmt.Printf("  ⚠️  Token validation failed: %v\n", err)
			fmt.Println("  The token may be invalid or lack required permissions.")
		} else {
			fmt.Printf("  ✅ Token valid — project has %d sessions\n", len(sessions))
		}
	}

	// Ask for project ID (required)
	fmt.Print("Memory project ID (UUID): ")
	projectID = readLine(reader)
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		log.Fatal("Project ID is required. Find it via 'memory projects list' or the Memory Platform UI.")
	}

	// ── Discord bot (optional) ──
	fmt.Print("\nDiscord bot token (optional, press Enter to skip): ")
	discordToken := readLine(reader)

	var channelIDs []string
	var threadChannelIDs []string
	if discordToken != "" {
		fmt.Print("Allowed Discord channel IDs (comma-separated, or empty for all): ")
		ch := readLine(reader)
		for _, id := range strings.Split(ch, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				channelIDs = append(channelIDs, id)
			}
		}

		fmt.Print("Thread channel IDs for auto-threading (comma-separated, empty = thread everywhere): ")
		th := readLine(reader)
		for _, id := range strings.Split(th, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				threadChannelIDs = append(threadChannelIDs, id)
			}
		}
	}

	// ── Create project config ──
	pc := &config.ProjectConfig{
		ServerURL: serverURL,
		Token:     token,
		ProjectID: projectID,
	}
	if discordToken != "" {
		pc.DiscordBotToken = discordToken
		pc.DiscordChannelIDs = channelIDs
		pc.DiscordThreadChannelIDs = threadChannelIDs
	}

	cfg.AddProject(name, pc)

	if err := cfg.Save(); err != nil {
		log.Fatalf("Failed to save config: %v", err)
	}

	fmt.Printf("\n✅ Configuration saved to %s\n", config.Path())
	fmt.Printf("   Project: %s\n", name)
	fmt.Printf("   Server:  %s\n", serverURL)
	fmt.Printf("   Project: %s\n", projectID)
	if discordToken != "" {
		fmt.Printf("   Discord: bot configured, %d channel(s)\n", len(channelIDs))
	}

	// Offer to apply embedded schemas
	fmt.Print("\n📦 Apply embedded schema definitions to this project? [Y/n]: ")
	applySchemas := readLine(reader)
	if applySchemas == "" || strings.ToLower(applySchemas) == "y" || strings.ToLower(applySchemas) == "yes" {
		doApplySchemas(pc)
	}

	fmt.Println("\nRun 'diane bot' to start the Discord bot.")
}

// cmdBot starts the Discord bot using config.
// Flags:
//
//	--pidfile <path>    Write PID to this file (default: ~/.diane/bot.pid)
//	                    Set to "" to skip PID file
//	--restart           Auto-restart on crash (not on SIGTERM/SIGINT)
//	--restart-delay <d> Delay between restarts (default: 5s)
func cmdBot() {
	fs := flag.NewFlagSet("bot", flag.ExitOnError)
	pidfileDefault := filepath.Join(os.Getenv("HOME"), ".diane", "bot.pid")
	pidfilePtr := fs.String("pidfile", pidfileDefault, "Path to PID file (empty = none)")
	restartPtr := fs.Bool("restart", false, "Auto-restart on crash")
	restartDelay := fs.Duration("restart-delay", 5*time.Second, "Restart delay")
	fs.Parse(os.Args[2:])

	// Write PID file
	if *pidfilePtr != "" {
		dir := filepath.Dir(*pidfilePtr)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Cannot create PID dir %s: %v", dir, err)
		}
		if err := os.WriteFile(*pidfilePtr, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
			log.Fatalf("Cannot write PID file %s: %v", *pidfilePtr, err)
		}
		defer func() {
			os.Remove(*pidfilePtr)
			log.Printf("[PROC] Removed PID file %s", *pidfilePtr)
		}()
		log.Printf("[PROC] PID: %d → %s", os.Getpid(), *pidfilePtr)
	}

	// ── Restart loop ──
	first := true
	for {
		if !first {
			log.Printf("[PROC] Restarting in %v...", *restartDelay)
			time.Sleep(*restartDelay)
		}
		first = false

		// Reload config each iteration (picks up changes)
		cfg, err := config.Load()
		if err != nil {
			log.Printf("[PROC] Config load error: %v — retrying...", err)
			continue
		}
		pc := cfg.Active()
		if pc == nil {
			log.Printf("[PROC] No active project — retrying...")
			continue
		}

		// Run bot in goroutine with panic recovery
		exitCh := make(chan error, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					exitCh <- fmt.Errorf("PANIC: %v", r)
				}
			}()
			exitCh <- runBotOnce(pc)
		}()

		err = <-exitCh
		if err == nil {
			log.Printf("[PROC] Bot exited cleanly (SIGTERM/SIGINT)")
			if !*restartPtr {
				return
			}
			log.Printf("[PROC] Clean exit — not restarting (use --restart for crash-only restart)")
			return
		}

		log.Printf("[PROC] Bot exited: %v", err)
		if !*restartPtr {
			return
		}
		// With --restart: only restart crashes, not clean exits.
		// Since Start() returns nil on SIGTERM, any non-nil error is a crash.
		log.Printf("[PROC] Restarting due to error...")
	}
}

// cmdProjects lists configured projects.
func cmdProjects() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if len(cfg.Projects) == 0 {
		fmt.Println("No projects configured. Run 'diane init'.")
		return
	}
	fmt.Println("Configured projects:")
	for name, pc := range cfg.Projects {
		mark := " "
		if name == cfg.Default {
			mark = "*"
		}
		fmt.Printf("  %s %s\n", mark, name)
		fmt.Printf("      Server:  %s\n", pc.ServerURL)
		fmt.Printf("      Project: %s\n", pc.ProjectID)
		if pc.DiscordBotToken != "" {
			fmt.Printf("      Discord: ✓ (%d channel(s))\n", len(pc.DiscordChannelIDs))
		}
	}
}

// doApplySchemas applies the embedded schemas to the configured project.
func doApplySchemas(pc *config.ProjectConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		fmt.Printf("  ⚠️  Cannot apply schemas: %v\n", err)
		return
	}
	defer bridge.Close()

	fmt.Print("  Applying schemas... ")
	results, err := schema.Apply(ctx, bridge.Client(), pc.ProjectID, nil)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}

	var created, updated int
	for _, r := range results {
		switch r.Action {
		case "created":
			created++
		case "updated":
			updated++
		case "error":
			fmt.Printf("\n  ❌ %s: %v", r.TypeName, r.Error)
		}
	}
	fmt.Printf("✅ %d created, %d updated\n", created, updated)
}

// readLine reads a trimmed line from stdin.
func readLine(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
}
