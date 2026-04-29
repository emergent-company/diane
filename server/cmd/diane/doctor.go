package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/Emergent-Comapny/diane/internal/agents"
	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
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
	modeLabel := pc.ModeLabel()
	fmt.Printf("   Mode:    %s\n", modeLabel)

	isSlave := pc.IsSlave()

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
		ServerURL:         pc.ServerURL,
		APIKey:            pc.Token,
		ProjectID:         pc.ProjectID,
		OrgID:             pc.OrgID,
		HTTPClientTimeout: 10 * time.Second,
	})
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	defer bridge.Close()
	fmt.Println("✅ SDK initialized")

	// ── 6. Project info from auth/me ──
	fmt.Print("\n🏷️  Project name... ")
	authInfo, err := bridge.GetProjectInfo(ctx)
	if err != nil {
		fmt.Printf("⚠️  %v\n", err)
	} else {
		fmt.Printf("✅ \"%s\"\n", authInfo.ProjectName)
	}

	// ── 7. LLM provider / provider config (skipped in slave mode) ──
	if isSlave {
		fmt.Println("\n🤖 LLM provider... ⏭️  Slave mode — agents run on master/cloud")
		fmt.Println("📋 Provider config (local)... ⏭️  Not applicable in slave mode")
	} else {
		// ── 7. LLM provider — try org-level first, fall back gracefully ──
		fmt.Print("\n🤖 LLM provider... ")
		var orgID string
		if authInfo != nil && authInfo.OrgID != "" {
			orgID = authInfo.OrgID
		} else {
			orgID = pc.OrgID
		}
		if orgID == "" {
			fmt.Println("✅ Using project-level provider (run 'diane provider sync' to configure)")
		} else {
			sdkClient := bridge.Client()
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
	}

	// ── 7c. Agent Definitions ──
	remoteDefs, err := bridge.ListAgentDefs(ctx)
	remoteNameSet := map[string]*sdkagents.AgentDefinitionSummary{}
	if err == nil && remoteDefs != nil {
		for i := range remoteDefs.Data {
			d := remoteDefs.Data[i]
			remoteNameSet[d.Name] = &d
		}
	}

	totalRemote := len(remoteNameSet)
	totalLocal := len(pc.Agents)
	deployed := 0
	for name := range pc.Agents {
		if remoteNameSet[name] != nil {
			deployed++
		}
	}

	// Build set of built-in agent names (seeded from Go code)
	builtInSet := map[string]bool{}
	for _, ba := range agents.BuiltInAgents() {
		builtInSet[ba.Name] = true
	}

	fmt.Print("\n🧠 Agent Definitions")
	if totalLocal == 0 && totalRemote == 0 {
		fmt.Println(" — none configured")
		if !isSlave {
			fmt.Println("   Run 'diane agent define <name>' or 'diane agent seed' to get started.")
		}
	} else if isSlave {
		// Slave: only show MP agents, skip local config comparison
		if err != nil {
			fmt.Printf(" — ⚠️  %v\n", err)
		} else {
			fmt.Printf(" — %d on MP", totalRemote)
			builtInOnMP := 0
			for name := range remoteNameSet {
				if builtInSet[name] {
					builtInOnMP++
				}
			}
			if builtInOnMP > 0 {
				orphaned := totalRemote - builtInOnMP
				if orphaned > 0 {
					fmt.Printf(" (%d built-in, %d custom)\n", builtInOnMP, orphaned)
				} else {
					fmt.Printf(" (%d built-in)\n", builtInOnMP)
				}
			} else {
				fmt.Println("")
			}
		}
		// Show all MP agents
		mpOnlyNames := make([]string, 0, totalRemote)
		for name := range remoteNameSet {
			mpOnlyNames = append(mpOnlyNames, name)
		}
		sort.Strings(mpOnlyNames)

		for _, name := range mpOnlyNames {
			d := remoteNameSet[name]
			desc := ""
			if d.Description != nil {
				desc = *d.Description
				if len(desc) > 50 {
					desc = desc[:50] + "..."
				}
			}
			toolInfo := ""
			if d.ToolCount > 0 {
				toolInfo = fmt.Sprintf(" [%d tools]", d.ToolCount)
			}
			label := "🔧 built-in"
			if !builtInSet[name] {
				label = "📦 custom"
			}
			fmt.Printf("   %-25s %s%s", name, label, toolInfo)
			if desc != "" {
				fmt.Printf(" — %s", desc)
			}
			fmt.Println()
		}
	} else {
		fmt.Printf(" — %d in config", totalLocal)
		if err != nil {
			fmt.Printf(", ⚠️  (MP: %v)", err)
		} else {
			builtInOnMP := 0
			orphaned := 0
			for name := range remoteNameSet {
				if _, local := pc.Agents[name]; !local {
					if builtInSet[name] {
						builtInOnMP++
					} else {
						orphaned++
					}
				}
			}
			fmt.Printf(", %d on MP", totalRemote)
			if builtInOnMP > 0 {
				fmt.Printf(" (%d built-in", builtInOnMP)
				if orphaned > 0 {
					fmt.Printf(", %d orphaned", orphaned)
				}
				fmt.Printf(")")
			} else if orphaned > 0 {
				fmt.Printf(" (%d orphaned)", orphaned)
			}
		}
		if totalLocal > 0 {
			if deployed == totalLocal {
				fmt.Printf(" — all deployed ✅\n")
			} else {
				fmt.Printf(" — %d deployed, %d pending 🕐\n", deployed, totalLocal-deployed)
			}
		} else {
			fmt.Println("")
		}

		// Local agents first (config-defined), with deploy status
		if totalLocal > 0 {
			localNames := make([]string, 0, totalLocal)
			for name := range pc.Agents {
				localNames = append(localNames, name)
			}
			sort.Strings(localNames)

			for _, name := range localNames {
				a := pc.Agents[name]
				rd := remoteNameSet[name]

				status := "🕐  Not deployed"
				if rd != nil {
					status = "✅ Deployed"
				}

				desc := a.Description
				if len(desc) > 55 {
					desc = desc[:55] + "..."
				}
				toolCount := len(a.Tools)
				fmt.Printf("   📄 %-25s %s  — %s", name, status, desc)
				if toolCount > 0 {
					fmt.Printf(" [%d tools]", toolCount)
				}
				fmt.Println()

				if rd != nil {
					fmt.Printf("       Flow: %s  Visibility: %s  Default: %v\n", rd.FlowType, rd.Visibility, rd.IsDefault)
				}
				if a.Sandbox != nil && a.Sandbox.Enabled {
					fmt.Printf("       Sandbox: %s\n", a.Sandbox.BaseImage)
				}
			}
		}

		// Then remaining agents on MP (not in local config)
		if err == nil && totalRemote > deployed {
			mpOnlyCount := totalRemote - deployed
			mpOnlyNames := make([]string, 0, mpOnlyCount)
			builtInAmongMpOnly := 0
			for name := range remoteNameSet {
				if _, local := pc.Agents[name]; !local {
					mpOnlyNames = append(mpOnlyNames, name)
					if builtInSet[name] {
						builtInAmongMpOnly++
					}
				}
			}
			sort.Strings(mpOnlyNames)

			if mpOnlyCount <= 5 || totalLocal == 0 {
				for _, name := range mpOnlyNames {
					d := remoteNameSet[name]
					desc := ""
					if d.Description != nil {
						desc = *d.Description
						if len(desc) > 50 {
							desc = desc[:50] + "..."
						}
					}
					toolInfo := ""
					if d.ToolCount > 0 {
						toolInfo = fmt.Sprintf(" [%d tools]", d.ToolCount)
					}
					label := "🔧 built-in"
					if !builtInSet[name] {
						label = "☁️  orphaned"
					}
					fmt.Printf("   %-25s %s%s", name, label, toolInfo)
					if desc != "" {
						fmt.Printf(" — %s", desc)
					}
					fmt.Println()
				}
			} else {
				limit := 3
				for _, name := range mpOnlyNames[:limit] {
					d := remoteNameSet[name]
					desc := ""
					if d.Description != nil {
						desc = *d.Description
						if len(desc) > 50 {
							desc = desc[:50] + "..."
						}
					}
					toolInfo := ""
					if d.ToolCount > 0 {
						toolInfo = fmt.Sprintf(" [%d tools]", d.ToolCount)
					}
					label := "🔧 built-in"
					if !builtInSet[name] {
						label = "☁️  orphaned"
					}
					fmt.Printf("   %-25s %s%s", name, label, toolInfo)
					if desc != "" {
						fmt.Printf(" — %s", desc)
					}
					fmt.Println()
				}
				remaining := mpOnlyCount - limit
				builtInRemaining := builtInAmongMpOnly - min(builtInAmongMpOnly, limit)
				fmt.Printf("   … and %d more (%d built-in, %d orphaned — run 'diane agent list' for all)\n",
					remaining, builtInRemaining, remaining-builtInRemaining)
			}
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
	if isSlave {
		fmt.Println("⏭️  N/A (slave mode — master runs the bot)")
	} else if pc.DiscordBotToken != "" {
		fmt.Printf("✅ configured (%d channel(s))\n", len(pc.DiscordChannelIDs))
	} else {
		fmt.Println("⚠️  Not configured (optional)")
	}

	fmt.Println("\n═══ Done ═══")

	if jsonOutput {
		emitJSON("ok", map[string]interface{}{
			"config": map[string]string{
				"path":       config.Path(),
				"project_id": pc.ProjectID,
				"server_url": pc.ServerURL,
				"mode":       pc.ModeLabel(),
			},
			"token_valid":     true,
			"sdk_connected":   true,
			"discord_checked": true,
		})
	}
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// emitJSON prints a structured JSON response to stdout.
// status is "ok" or "error". data is the payload (map, slice, string, etc.).
func emitJSON(status string, data interface{}) {
	resp := map[string]interface{}{
		"status": status,
	}
	if status == "error" {
		switch v := data.(type) {
		case string:
			resp["error"] = v
		case map[string]string:
			if msg, ok := v["message"]; ok {
				resp["error"] = msg
			}
			resp["data"] = v
		default:
			resp["error"] = data
		}
	} else {
		resp["data"] = data
	}
	b, _ := json.Marshal(resp)
	fmt.Println(string(b))
}
