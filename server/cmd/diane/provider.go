package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
)

func cmdProvider(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: diane provider <command>")
		fmt.Println("")
		fmt.Println("Commands:")
		fmt.Println("  list          List configured providers on Memory Platform")
		fmt.Println("  set           Configure a provider (generative or embedding)")
		fmt.Println("  test          Test a provider via Memory Platform")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  diane provider set generative")
		fmt.Println("  diane provider test generative")
		return
	}

	switch args[0] {
	case "list":
		cmdProviderList()
	case "set":
		if len(args) < 2 {
			fmt.Println("Usage: diane provider set <generative|embedding>")
			return
		}
		cmdProviderSet(args[1])
	case "test":
		if len(args) < 2 {
			fmt.Println("Usage: diane provider test <generative|embedding>")
			return
		}
		cmdProviderTest(args[1])
	default:
		fmt.Fprintf(os.Stderr, "Unknown provider command: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdProviderList() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Println("No project configured. Run 'diane init' first.")
		return
	}

	fmt.Println("═══ Provider Configuration ═══")
	fmt.Println()
	fmt.Printf("Project: %s (%s)\n", cfg.Default, pc.ProjectID)
	fmt.Println()

	// ── Remote (Memory Platform) ──
	fmt.Println("🌐 Memory Platform:")
	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		fmt.Printf("  ⚠️  Cannot connect: %v\n", err)
		return
	}
	defer bridge.Close()

	// Resolve org ID
	orgID := pc.OrgID
	if orgID == "" {
		proj, err := bridge.Client().Projects.Get(ctx, pc.ProjectID, nil)
		if err != nil {
			fmt.Printf("  ⚠️  Cannot fetch org ID: %v\n", err)
		} else {
			orgID = proj.OrgID
		}
	}

	if orgID == "" {
		fmt.Println("  ⚠️  Cannot determine org ID")
		return
	}

	providers, err := bridge.ListOrgProviders(ctx, orgID)
	if err != nil {
		fmt.Printf("  ⚠️  %v\n", err)
		return
	}

	if len(providers) == 0 {
		fmt.Println("  No providers configured on Memory Platform")
	} else {
		for _, p := range providers {
			model := p.GenerativeModel
			if model == "" {
				model = "(auto)"
			}
			fmt.Printf("  %s → %s\n", p.Provider, model)
		}
	}
	fmt.Println()
	fmt.Println("Run 'diane provider set generative' or 'diane provider set embedding' to configure.")
}

func cmdProviderSet(kind string) {
	if kind != "generative" && kind != "embedding" {
		fmt.Fprintf(os.Stderr, "Provider kind must be 'generative' or 'embedding', got '%s'\n", kind)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "No project configured. Run 'diane init' first.\n")
		os.Exit(1)
	}

	fmt.Printf("=== Configure %s provider ===\n", kind)
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// Provider type
	fmt.Println("Supported providers:")
	fmt.Println("  google            - Google AI (Gemini) - API key")
	fmt.Println("  deepseek          - DeepSeek - API key (generative only)")
	fmt.Println("  openai-compatible - OpenAI-compatible API - API key + base URL")
	fmt.Println()
	fmt.Print("Provider [google]: ")
	pType := readLine(reader)
	if pType == "" {
		pType = "google"
	}

	fmt.Print("API key: ")
	apiKey := readLine(reader)
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "API key is required")
		os.Exit(1)
	}

	baseURL := ""
	if pType == "openai-compatible" || pType == "deepseek" {
		fmt.Print("Base URL [https://api.deepseek.com]: ")
		baseURL = readLine(reader)
		if baseURL == "" {
			if pType == "deepseek" {
				baseURL = "https://api.deepseek.com"
			}
		}
	}

	model := ""
	if kind == "generative" {
		if pType == "deepseek" {
			fmt.Print("Model [deepseek-chat]: ")
			model = readLine(reader)
			if model == "" {
				model = "deepseek-chat"
			}
		} else if pType == "openai-compatible" {
			fmt.Print("Model: ")
			model = readLine(reader)
		}
		// google auto-selects
	} else {
		// embedding — google auto-selects, others need model
		if pType != "google" {
			fmt.Print("Model: ")
			model = readLine(reader)
		}
	}

	// Write directly to Memory Platform
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Memory Platform: %v\n", err)
		os.Exit(1)
	}
	defer bridge.Close()

	// Resolve org ID
	orgID := pc.OrgID
	if orgID == "" {
		proj, err := bridge.Client().Projects.Get(ctx, pc.ProjectID, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot fetch org ID: %v\n", err)
			os.Exit(1)
		}
		orgID = proj.OrgID
	}

	fmt.Printf("\nWriting to Memory Platform... ")
	_, err = bridge.UpsertOrgProvider(ctx, orgID, pType, apiKey, model, baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅")

	// Offer to test
	fmt.Print("\nTest provider now? [Y/n]: ")
	test := readLine(reader)
	if test == "" || strings.ToLower(test) == "y" || strings.ToLower(test) == "yes" {
		doProviderTestWithProvider(ctx, bridge, orgID, pType, kind)
	}
}

func cmdProviderTest(kind string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "No project configured. Run 'diane init' first.\n")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Memory Platform: %v\n", err)
		os.Exit(1)
	}
	defer bridge.Close()

	// Resolve org ID
	orgID := pc.OrgID
	if orgID == "" {
		proj, err := bridge.Client().Projects.Get(ctx, pc.ProjectID, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot fetch org ID: %v\n", err)
			os.Exit(1)
		}
		orgID = proj.OrgID
	}

	// Look up providers from MP
	providers, err := bridge.ListOrgProviders(ctx, orgID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list providers: %v\n", err)
		os.Exit(1)
	}

	if len(providers) == 0 {
		fmt.Printf("⚠️  No provider configured on Memory Platform. Use 'diane provider set %s' first.\n", kind)
		return
	}

	// Use the first provider (MP doesn't distinguish generative/embedding at config level)
	provider := providers[0].Provider
	doProviderTestWithProvider(ctx, bridge, orgID, provider, kind)
}

func doProviderTestWithProvider(ctx context.Context, bridge *memory.Bridge, orgID, provider, kind string) {
	fmt.Printf("Testing %s provider (%s)...\n", kind, provider)

	result, err := bridge.TestProvider(ctx, orgID, provider)
	if err != nil {
		fmt.Printf("❌ Test failed: %v\n", err)
		return
	}

	fmt.Printf("✅ %s → %s\n", result.Provider, result.Model)
	fmt.Printf("   Reply: \"%s\"\n", truncateStr(result.Reply, 80))
	fmt.Printf("   Latency: %dms\n", result.LatencyMs)
}
