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
		fmt.Println("  list          List configured providers (local + remote)")
		fmt.Println("  set           Configure a provider (generative or embedding)")
		fmt.Println("  test          Test a provider via Memory Platform")
		fmt.Println("  sync          Push local provider configs to Memory Platform")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  diane provider set generative")
		fmt.Println("  diane provider test generative")
		fmt.Println("  diane provider sync")
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
	case "sync":
		cmdProviderSync()
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

	// ── Local config ──
	fmt.Println("📋 Local config:")
	gen := pc.GenerativeProvider
	emb := pc.EmbeddingProvider

	if gen != nil {
		model := gen.Model
		if model == "" {
			model = "(auto)"
		}
		fmt.Printf("  Generative: %s → %s\n", gen.Provider, model)
	} else {
		fmt.Println("  Generative: not configured")
	}

	if emb != nil {
		fmt.Printf("  Embedding:  %s\n", emb.Provider)
	} else {
		fmt.Println("  Embedding:  not configured")
	}
	fmt.Println()

	// ── Remote (Memory Platform) ──
	fmt.Println("🌐 Memory Platform (synced):")
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
	fmt.Println("Run 'diane provider sync' to push local config to Memory Platform.")
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

	// Build provider config
	providerCfg := &config.ProviderConfig{
		Provider: pType,
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Model:    model,
	}

	// Save to local config
	if kind == "generative" {
		pc.GenerativeProvider = providerCfg
	} else {
		pc.EmbeddingProvider = providerCfg
	}

	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✅ Saved to %s\n", config.Path())

	// Offer to push to Memory Platform
	fmt.Print("\nSync to Memory Platform now? [Y/n]: ")
	sync := readLine(reader)
	if sync == "" || strings.ToLower(sync) == "y" || strings.ToLower(sync) == "yes" {
		doProviderSync(cfg, pc)
	}

	// Offer to test
	fmt.Print("\nTest provider now? [Y/n]: ")
	test := readLine(reader)
	if test == "" || strings.ToLower(test) == "y" || strings.ToLower(test) == "yes" {
		doProviderTest(kind, pc)
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

	doProviderTest(kind, pc)
}

func doProviderTest(kind string, pc *config.ProjectConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var providerCfg *config.ProviderConfig
	if kind == "generative" {
		providerCfg = pc.GenerativeProvider
	} else {
		providerCfg = pc.EmbeddingProvider
	}

	if providerCfg == nil {
		fmt.Printf("⚠️  %s provider not configured locally. Use 'diane provider set %s' first.\n", kind, kind)
		return
	}

	fmt.Printf("Testing %s provider (%s)...\n", kind, providerCfg.Provider)

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		fmt.Printf("❌ Connection failed: %v\n", err)
		return
	}
	defer bridge.Close()

	// Ensure provider is synced before testing
	orgID := pc.OrgID
	if orgID == "" {
		proj, err := bridge.Client().Projects.Get(ctx, pc.ProjectID, nil)
		if err != nil {
			fmt.Printf("❌ Cannot fetch org ID: %v\n", err)
			return
		}
		orgID = proj.OrgID
	}

	// First upsert to ensure credentials are on MP
	_, err = bridge.UpsertOrgProvider(ctx, orgID, providerCfg.Provider, providerCfg.APIKey, providerCfg.Model, providerCfg.BaseURL)
	if err != nil {
		fmt.Printf("❌ Sync failed: %v\n", err)
		fmt.Println("   Ensure API key is valid and Memory Platform can reach the provider.")
		return
	}

	// Now test
	result, err := bridge.TestProvider(ctx, orgID, providerCfg.Provider)
	if err != nil {
		fmt.Printf("❌ Test failed: %v\n", err)
		return
	}

	fmt.Printf("✅ %s → %s\n", result.Provider, result.Model)
	fmt.Printf("   Reply: \"%s\"\n", truncateStr(result.Reply, 80))
	fmt.Printf("   Latency: %dms\n", result.LatencyMs)
}

func cmdProviderSync() {
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

	doProviderSync(cfg, pc)
}

func doProviderSync(cfg *config.Config, pc *config.ProjectConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("═══ Sync Providers → Memory Platform ═══")
	fmt.Println()

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		fmt.Printf("❌ Connection failed: %v\n", err)
		return
	}
	defer bridge.Close()

	// Resolve org ID
	orgID := pc.OrgID
	if orgID == "" {
		proj, err := bridge.Client().Projects.Get(ctx, pc.ProjectID, nil)
		if err != nil {
			fmt.Printf("❌ Cannot fetch org ID: %v\n", err)
			return
		}
		orgID = proj.OrgID
	}

	synced := 0

	if pc.GenerativeProvider != nil {
		p := pc.GenerativeProvider
		fmt.Printf("Syncing generative (%s → %s)... ", p.Provider, p.Model)
		_, err := bridge.UpsertOrgProvider(ctx, orgID, p.Provider, p.APIKey, p.Model, p.BaseURL)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
		} else {
			fmt.Println("✅")
			synced++
		}
	}

	if pc.EmbeddingProvider != nil {
		p := pc.EmbeddingProvider
		fmt.Printf("Syncing embedding (%s)... ", p.Provider)
		_, err := bridge.UpsertOrgProvider(ctx, orgID, p.Provider, p.APIKey, p.Model, p.BaseURL)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
		} else {
			fmt.Println("✅")
			synced++
		}
	}

	if synced == 0 {
		fmt.Println("\n⚠️  No providers to sync. Configure with 'diane provider set' first.")
	} else {
		fmt.Printf("\n✅ Synced %d provider(s) to Memory Platform\n", synced)
	}
}
