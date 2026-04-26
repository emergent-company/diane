package main

import (
	"fmt"
	"log"
	"time"

	"github.com/Emergent-Comapny/diane/internal/agents"
	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/db"
	"github.com/Emergent-Comapny/diane/internal/discord"
	"github.com/Emergent-Comapny/diane/internal/memory"
)

// startBot starts the Discord bot and blocks until shutdown.
// On setup errors, it logs and exits (user-facing convenience).
func startBot(pc *config.ProjectConfig) {
	if err := runBotOnce(pc); err != nil {
		log.Fatalf("Bot error: %v", err)
	}
}

// runBotOnce starts the Discord bot once and returns when it exits.
// Unlike startBot, it returns errors instead of calling log.Fatalf,
// so the caller can decide whether to restart.
func runBotOnce(pc *config.ProjectConfig) error {
	// Seed built-in agents to local SQLite database on every startup.
	// This ensures the local DB is always in sync with the Go code.
	if localDB, err := db.New(""); err == nil {
		if err := agents.SeedToDB(localDB); err != nil {
			log.Printf("[WARN] Failed to seed agents to local DB: %v", err)
		} else {
			log.Printf("[DB] Seeded built-in agents to local SQLite database")
		}
		localDB.Close()
	} else {
		log.Printf("[WARN] Cannot open local DB: %v", err)
	}
	// Build Discord config
	dc := discord.DefaultConfig()
	dc.BotToken = pc.DiscordBotToken
	dc.AllowedChannels = pc.DiscordChannelIDs
	if pc.SystemPrompt != "" {
		dc.SystemPrompt = pc.SystemPrompt
	}
	dc.MemoryServerURL = pc.ServerURL
	dc.MemoryAPIKey = pc.Token
	dc.MemoryProjectID = pc.ProjectID
	dc.MemoryOrgID = pc.OrgID
	dc.SSEEventStream = true

	if dc.BotToken == "" {
		return fmt.Errorf("discord bot token not configured — run 'diane init'")
	}

	// Build Memory bridge
	memCfg := memory.Config{
		ServerURL:          pc.ServerURL,
		APIKey:             pc.Token,
		ProjectID:          pc.ProjectID,
		OrgID:              pc.OrgID,
		HTTPClientTimeout:  120 * time.Second,
	}

	bridge, err := memory.New(memCfg)
	if err != nil {
		return fmt.Errorf("create memory bridge: %w", err)
	}

	bot, err := discord.New(dc)
	if err != nil {
		return fmt.Errorf("create discord bot: %w", err)
	}
	bot.AttachBridge(bridge)

	log.Printf("Starting Diane bot for project '%s'...", pc.ServerURL+"/project/"+pc.ProjectID)
	log.Printf("  Discord bot token: configured")
	log.Printf("  Memory server:     %s", pc.ServerURL)
	log.Printf("  Memory project:    %s", pc.ProjectID)
	if len(dc.AllowedChannels) > 0 {
		log.Printf("  Discord channels:  %v", dc.AllowedChannels)
	}

	return bot.Start()
}
