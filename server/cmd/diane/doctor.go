package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
)

func cmdDoctor() {
	ctx := context.Background()
	fmt.Println("═══ Diane Doctor ═══")
	fmt.Println()

	// ── 1. Config file ──
	fmt.Print("📁 Config file... ")
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Println("⚠️  No project configured")
		fmt.Println("\n   Run 'diane init' to set up a project.")
		return
	}
	fmt.Printf("✅ %s\n", config.Path())
	fmt.Printf("   Project: %s\n", pc.ProjectID)
	fmt.Printf("   Server:  %s\n", pc.ServerURL)
	fmt.Printf("   Mode:    %s\n", pc.ModeLabel())

	// ── 3. Project ID format ──
	fmt.Print("\n🔢 Project ID... ")
	if len(pc.ProjectID) == 36 {
		fmt.Println("✅", pc.ProjectID)
	} else {
		fmt.Printf("⚠️  Not a UUID (got %d chars)\n", len(pc.ProjectID))
	}

	// ── 4. Token present ──
	fmt.Print("\n🔑 API token... ")
	if pc.Token == "" {
		fmt.Println("❌ Not set")
		return
	}
	if len(pc.Token) >= 10 {
		fmt.Printf("✅ %s...%s (%d chars)\n", pc.Token[:8], pc.Token[len(pc.Token)-4:], len(pc.Token))
	} else {
		fmt.Println("⚠️  Too short to be valid")
	}

	// ── 5. SDK connection ──
	fmt.Print("\n🔌 Memory SDK connection... ")
	bridge, err := memory.New(memory.Config{
		ServerURL:          pc.ServerURL,
		APIKey:             pc.Token,
		ProjectID:          pc.ProjectID,
		OrgID:              pc.OrgID,
		HTTPClientTimeout:  10 * time.Second,
	})
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	defer bridge.Close()
	fmt.Println("✅ SDK initialized")

	// ── 6. Project name from Memory Platform ──
	fmt.Print("\n🏷️  Project name... ")
	sdkClient := bridge.Client()
	proj, err := sdkClient.Projects.Get(ctx, pc.ProjectID, nil)
	if err != nil {
		fmt.Printf("⚠️  %v\n", err)
	} else {
		fmt.Printf("✅ \"%s\"\n", proj.Name)
		// Store org ID for provider lookup
		if pc.OrgID == "" && proj.OrgID != "" {
			sdkClient.SetContext(proj.OrgID, pc.ProjectID)
		}
	}

	// ── 7. LLM provider used by Memory Platform ──
	fmt.Print("\n🤖 LLM provider... ")
	orgID := pc.OrgID
	if orgID == "" {
		// Fetch org ID if we didn't already
		if proj == nil {
			p2, err2 := sdkClient.Projects.Get(ctx, pc.ProjectID, nil)
			if err2 == nil {
				orgID = p2.OrgID
			}
		} else {
			orgID = proj.OrgID
		}
	}
	if orgID == "" {
		fmt.Println("⚠️  Could not determine org ID")
	} else {
		providers, err := sdkClient.Provider.ListOrgConfigs(ctx, orgID)
		if err != nil {
			fmt.Printf("⚠️  %v\n", err)
		} else if len(providers) == 0 {
			fmt.Println("⚠️  No org providers configured")
		} else {
			for _, p := range providers {
				model := p.GenerativeModel
				if model == "" {
					model = "(auto)"
				}
				fmt.Printf("✅ %s → %s\n", p.Provider, model)
			}
		}
	}

	// ── 7b. Local provider config ──
	fmt.Print("\n📋 Provider config (local)... ")
	if pc.GenerativeProvider == nil && pc.EmbeddingProvider == nil {
		fmt.Println("⚠️  None configured")
		fmt.Println("   Run 'diane provider set generative' or 'diane provider set embedding'")
	} else {
		fmt.Println()
		if pc.GenerativeProvider != nil {
			p := pc.GenerativeProvider
			model := p.Model
			if model == "" {
				model = "(auto)"
			}
			fmt.Printf("   Generative: %s → %s\n", p.Provider, model)
		} else {
			fmt.Println("   Generative: not configured")
		}
		if pc.EmbeddingProvider != nil {
			p := pc.EmbeddingProvider
			fmt.Printf("   Embedding:  %s\n", p.Provider)
		} else {
			fmt.Println("   Embedding:  not configured")
		}
	}

	// ── 7c. Local agent definitions ──
	fmt.Print("\n🧠 Agent definitions (local)... ")
	if len(pc.Agents) == 0 {
		fmt.Println("⚠️  None configured")
		fmt.Println("   Run 'diane agent define <name>' to create one")
	} else {
		fmt.Printf("✅ %d agent(s)\n", len(pc.Agents))
		for name, a := range pc.Agents {
			fmt.Printf("   %s — %s\n", name, a.Description)
			if len(a.Tools) > 0 {
				fmt.Printf("       Tools: %d\n", len(a.Tools))
			}
			if a.Sandbox != nil && a.Sandbox.Enabled {
				fmt.Printf("       Sandbox: %s\n", a.Sandbox.BaseImage)
			}
		}
	}

	// ── 7d. Agents on Memory Platform ──
	fmt.Print("\n🧠 Agents on Memory Platform... ")
	remoteDefs, err := bridge.ListAgentDefs(ctx)
	if err != nil {
		fmt.Printf("⚠️  %v\n", err)
	} else if len(remoteDefs.Data) == 0 {
		fmt.Println("⚠️  None")
		fmt.Println("   Run 'diane agent seed' to sync local agents to the platform.")
	} else {
		fmt.Printf("✅ %d agent(s)\n", len(remoteDefs.Data))
		for _, d := range remoteDefs.Data {
			desc := ""
			if d.Description != nil {
				desc = *d.Description
				if len(desc) > 60 {
					desc = desc[:60] + "..."
				}
			}
			toolInfo := ""
			if d.ToolCount > 0 {
				toolInfo = fmt.Sprintf(" (%d tools)", d.ToolCount)
			}
			fmt.Printf("   %s%s\n", d.Name, toolInfo)
			if desc != "" {
				fmt.Printf("       %s\n", desc)
			}
			fmt.Printf("       Flow: %s  Visibility: %s  Default: %v\n", d.FlowType, d.Visibility, d.IsDefault)
		}
	}

	// ── 7e. Run stats from Memory Platform ──
	fmt.Print("\n📊 Run stats... ")
	stats, err := bridge.GetProjectRunStats(ctx, nil)
	if err != nil {
		fmt.Printf("⚠️  %v\n", err)
	} else {
		s := stats.Data
		fmt.Printf("✅ %d runs total | %.1f%% success | $%.4f total\n", s.Overview.TotalRuns, s.Overview.SuccessRate*100, s.Overview.TotalCostUSD)
		if len(s.ByAgent) > 0 {
			// Show top 5 agents by run count
			type agentStat struct {
				name  string
				total int64
				succ  int64
				fail  int64
				avgMs float64
			}
			var sorted []agentStat
			for name, a := range s.ByAgent {
				sorted = append(sorted, agentStat{name, a.Total, a.Success, a.Failed + a.Errored, a.AvgDurationMs})
			}
			// Sort by total desc (simple bubble sort, small set)
			for i := 0; i < len(sorted); i++ {
				for j := i + 1; j < len(sorted); j++ {
					if sorted[j].total > sorted[i].total {
						sorted[i], sorted[j] = sorted[j], sorted[i]
					}
				}
			}
			limit := 5
			if len(sorted) < limit {
				limit = len(sorted)
			}
			for _, a := range sorted[:limit] {
				rate := 0.0
				if a.total > 0 {
					rate = float64(a.succ) / float64(a.total) * 100
				}
				fmt.Printf("   %s — %d runs, %.0f%% ok, avg %.0fms\n", a.name, a.total, rate, a.avgMs)
			}
			if len(sorted) > limit {
				fmt.Printf("   ... and %d more\n", len(sorted)-limit)
			}
		}
	}

	// ── 8. Session CRUD ──
	fmt.Print("\n📋 Session CRUD... ")
	session, err := bridge.CreateSession(ctx, "diane-doctor-check")
	if err != nil {
		fmt.Printf("❌ CreateSession: %v\n", err)
		return
	}
	fmt.Print("✅ created ")

	_, err = bridge.AppendMessage(ctx, session.ID, "user", "doctor test message", 0)
	if err != nil {
		fmt.Printf("❌ AppendMessage: %v\n", err)
		_ = bridge.CloseSession(ctx, session.ID)
		return
	}
	fmt.Print("✅ wrote ")

	msgs, err := bridge.GetMessages(ctx, session.ID)
	if err != nil {
		fmt.Printf("❌ GetMessages: %v\n", err)
		_ = bridge.CloseSession(ctx, session.ID)
		return
	}
	fmt.Printf("✅ read %d msgs ", len(msgs))

	err = bridge.CloseSession(ctx, session.ID)
	if err != nil {
		fmt.Printf("❌ CloseSession: %v\n", err)
		return
	}
	fmt.Println("✅ closed")

	// ── 9. Hybrid search ──
	fmt.Print("\n🔍 Memory search... ")
	results, err := bridge.SearchMemory(ctx, "doctor test", 3)
	if err != nil {
		fmt.Printf("⚠️  %v (non-fatal)\n", err)
	} else {
		fmt.Printf("✅ %d results\n", len(results))
	}

	// ── 10. Discord config ──
	fmt.Print("\n🤖 Discord bot... ")
	if pc.DiscordBotToken != "" {
		fmt.Printf("✅ configured (%d channel(s))\n", len(pc.DiscordChannelIDs))
	} else {
		fmt.Println("⚠️  Not configured (optional)")
	}

	fmt.Println("\n═══ Done ═══")
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
