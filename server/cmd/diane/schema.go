package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/Emergent-Comapny/diane/internal/schema"
)

func cmdSchema(args []string) {
	if len(args) == 0 || args[0] != "apply" {
		fmt.Println("Usage: diane schema apply [--dry-run]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  apply        Create or update all embedded schema definitions on Memory Platform")
		fmt.Println()
		fmt.Println("Flags:")
		fmt.Println("  --dry-run    Log what would be done without making API calls")
		return
	}

	// Parse --dry-run flag
	dryRun := false
	for _, arg := range args[1:] {
		if arg == "--dry-run" {
			dryRun = true
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintln(os.Stderr, "No project configured. Run 'diane init' first.")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer bridge.Close()

	label := "Applying"
	if dryRun {
		label = "Dry-run: would apply"
	}
	fmt.Printf("%s schema definitions from embedded schemas/ to project '%s'...\n", label, pc.ProjectID)
	fmt.Println()

	start := time.Now()
	results, err := schema.Apply(ctx, bridge.Client(), pc.ProjectID, &schema.ApplyOptions{
		DryRun: dryRun,
	})
	if err != nil {
		log.Fatalf("Schema apply failed: %v", err)
	}

	schema.PrintResults(results, time.Since(start))
}

func init() {
	// Register schema command — handled by cmdSchema() via main.go routing
}
