package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/db"
	"github.com/Emergent-Comapny/diane/internal/memory"
	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

func cmdMonitor() {
	ctx := context.Background()
	home, _ := os.UserHomeDir()
	dianeDir := filepath.Join(home, ".diane")

	fmt.Println("═══ Diane Monitor ═══")
	fmt.Println()

	// ── 1. Bot process health ──
	pidFile := filepath.Join(dianeDir, "bot.pid")
	fmt.Print("🟢 Bot process... ")
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Println("❌ Not running (no PID file)")
	} else {
		pid := strings.TrimSpace(string(pidData))
		cmd := exec.Command("ps", "-p", pid, "-o", "pid,rss,args", "--no-headers")
		output, err := cmd.Output()
		if err != nil {
			fmt.Printf("⚠️  PID %s but process not found (stale PID)\n", pid)
		} else {
			fields := strings.Fields(string(output))
			if len(fields) >= 3 {
				fmt.Printf("✅ Running (PID %s, %s KB)\n", fields[0], fields[1])
			} else {
				fmt.Printf("✅ Running (PID %s)\n", pid)
			}
		}
	}

	// ── 2. Service status ──
	fmt.Print("🔧 Systemd service... ")
	cmd := exec.Command("systemctl", "is-active", "diane")
	output, err := cmd.Output()
	if err == nil {
		status := strings.TrimSpace(string(output))
		if status == "active" {
			fmt.Println("✅ active")
		} else {
			fmt.Printf("⚠️  %s\n", status)
		}
	} else {
		fmt.Println("⚠️  Not available")
	}

	// ── 3. Active sessions from SQLite ──
	fmt.Print("\n📋 Active sessions... ")
	sqliteDB, err := db.New("")
	if err != nil {
		fmt.Printf("⚠️  SQLite: %v\n", err)
	} else {
		all, err := sqliteDB.GetAllDiscordSessions()
		sqliteDB.Close()
		if err != nil {
			fmt.Printf("⚠️  %v\n", err)
		} else if len(all) == 0 {
			fmt.Println("None (waiting for first message)")
		} else {
			fmt.Printf("%d session(s)\n", len(all))
			for _, s := range all {
				sessionID := s.SessionID
				if len(sessionID) > 12 {
					sessionID = sessionID[:12] + "..."
				}
				if sessionID == "" {
					sessionID = "(pending)"
				}
				channelShort := s.ChannelID
				if len(channelShort) > 12 {
					channelShort = channelShort[:12] + "..."
				}
				age := time.Since(s.UpdatedAt).Round(time.Second)
				fmt.Printf("   📌 #%s → session %s (agent: %s, %v ago)\n",
					channelShort, sessionID, s.AgentType, age)
			}
		}
	}

	// ── 4. Recent activity from debug log ──
	fmt.Print("\n📊 Recent activity (last 10 lines)...\n")
	logFile := filepath.Join(dianeDir, "debug.log")
	logData, err := os.ReadFile(logFile)
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
		start := 0
		if len(lines) > 10 {
			start = len(lines) - 10
		}
		for _, line := range lines[start:] {
			if line == "" {
				continue
			}
			// Trim timestamp for readability
			display := line
			if len(display) > 100 {
				display = display[:100] + "..."
			}
			fmt.Printf("   %s\n", display)
		}
	} else {
		fmt.Printf("   ⚠️  Cannot read log: %v\n", err)
	}

	// ── 5. Agent run stats from MP ──
	fmt.Print("\n🤖 Agent runs (last 24h)... ")
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("⚠️  Config: %v\n", err)
	} else {
		pc := cfg.Active()
		if pc == nil {
			fmt.Println("⚠️  No active project")
		} else {
			bridge, err := memory.New(memory.Config{
				ServerURL:          pc.ServerURL,
				APIKey:             pc.Token,
				ProjectID:          pc.ProjectID,
				OrgID:              pc.OrgID,
				HTTPClientTimeout:  15 * time.Second,
			})
			if err != nil {
				fmt.Printf("⚠️  Bridge: %v\n", err)
			} else {
				defer bridge.Close()

				// Use the new aggregate stats endpoint (requires MP ≥v0.40.2)
				since := time.Now().Add(-24 * time.Hour)
				runsResp, err := bridge.GetProjectRunStats(ctx, &sdkagentrun.RunStatsOptions{
					Since: &since,
				})
				if err != nil {
					// Fallback to legacy ListProjectRuns for older MP servers
					fmt.Printf("⚠️  %v\n", err)
					fmt.Println("   ↳ Falling back to legacy run list...")

					legacyRuns, legacyErr := bridge.Client().Agents.ListProjectRuns(ctx, pc.ProjectID, nil)
					if legacyErr != nil {
						fmt.Printf("   ⚠️  Legacy fallback also failed: %v\n", legacyErr)
					} else if len(legacyRuns.Data.Items) == 0 {
						fmt.Println("   (no runs found)")
					} else {
						total := len(legacyRuns.Data.Items)
						success := 0
						failed := 0
						var totalDur int
						agents := map[string]int{}

						for _, r := range legacyRuns.Data.Items {
							agents[r.AgentName]++
							if r.Status == "completed" || r.Status == "success" {
								success++
							} else if r.Status == "error" || r.Status == "failed" {
								failed++
							}
							if r.DurationMs != nil {
								totalDur += *r.DurationMs
							}
						}

						avgDur := "0s"
						if total > 0 {
							avg := time.Duration(totalDur/total) * time.Millisecond
							avg = avg.Round(time.Millisecond * 100)
							avgDur = avg.String()
						}

						successRate := float64(success) / float64(total) * 100
						fmt.Printf("   %d total, %d✅ %d❌ (%.0f%% success), avg %v (legacy)\n",
							total, success, failed, successRate, avgDur)

						if len(agents) > 0 {
							var names []string
							for name := range agents {
								names = append(names, name)
							}
							sort.Strings(names)
							for _, name := range names {
								fmt.Printf("   %s: %d runs\n", name, agents[name])
							}
						}
					}
				} else {
					stats := runsResp.Data
					o := stats.Overview

				fmt.Printf("%d total, %d✅ %d❌ %d⚠️  (%.0f%% success), avg %v, $%.4f\n",
					o.TotalRuns, o.SuccessCount, o.FailedCount, o.ErrorCount,
					o.SuccessRate*100,
					time.Duration(o.AvgDurationMs)*time.Millisecond,
					o.TotalCostUSD)

					// Per-agent breakdown
					if len(stats.ByAgent) > 0 {
						var names []string
						for name := range stats.ByAgent {
							names = append(names, name)
						}
						sort.Strings(names)
						for _, name := range names {
							a := stats.ByAgent[name]
							fmt.Printf("   %s: %d runs (%d✅ %d❌, avg %v, $%.4f)\n",
								name, a.Total, a.Success, a.Failed,
								time.Duration(a.AvgDurationMs)*time.Millisecond,
								a.TotalCostUSD)
						}
					}

					// Top errors
					if len(stats.TopErrors) > 0 {
						fmt.Println("\n   Top errors:")
						for _, e := range stats.TopErrors {
							fmt.Printf("     • %d× %s\n", e.Count, e.Message)
						}
					}

					// Tool stats (top 5)
					if stats.ToolStats.TotalToolCalls > 0 {
						fmt.Printf("\n   Tool calls: %d total\n", stats.ToolStats.TotalToolCalls)
						type toolEntry struct {
							name string
							stat sdkagentrun.RunStatsTool
						}
						var toolList []toolEntry
						for name, stat := range stats.ToolStats.ByTool {
							toolList = append(toolList, toolEntry{name, stat})
						}
						sort.Slice(toolList, func(i, j int) bool {
							return toolList[i].stat.Total > toolList[j].stat.Total
						})
						limit := 5
						if len(toolList) < limit {
							limit = len(toolList)
						}
						for _, t := range toolList[:limit] {
							rate := float64(t.stat.Success) / float64(t.stat.Total) * 100
							fmt.Printf("     %s: %d calls (%d✅ %d❌, %.0f%%, avg %v)\n",
								t.name, t.stat.Total, t.stat.Success, t.stat.Failed, rate,
								time.Duration(t.stat.AvgDurationMs)*time.Millisecond)
						}
					}

					// Session analytics
					fmt.Println("\n   📊 Sessions (last 24h):")
					sessionStats, err := bridge.GetProjectRunSessionStats(ctx, &sdkagentrun.RunStatsOptions{
						Since: &since,
						TopN:  5,
					})
					if err != nil {
						fmt.Printf("   ⚠️  %v\n", err)
					} else {
						ss := sessionStats.Data
						fmt.Printf("   %d total, %d active, avg %.1f runs/session, max %d\n",
							ss.TotalSessions, ss.ActiveSessions,
							ss.AvgRunsPerSession, ss.MaxRunsPerSession)

						// Platform breakdown
						if len(ss.SessionsByPlatform) > 0 {
							var platforms []string
							for p := range ss.SessionsByPlatform {
								platforms = append(platforms, p)
							}
							sort.Strings(platforms)
							for _, p := range platforms {
								fmt.Printf("     %s: %d sessions\n", p, ss.SessionsByPlatform[p])
							}
						}

						// Top sessions
						if len(ss.TopSessions) > 0 {
							fmt.Println("   Top sessions:")
							for _, s := range ss.TopSessions {
								var channelDisplay string
								if s.ThreadID != "" {
									channelDisplay = s.ThreadID
								} else {
									channelDisplay = s.ChannelID
								}
								if len(channelDisplay) > 10 {
									channelDisplay = channelDisplay[:10] + "..."
								}
								lastRun := time.Since(s.LastRunAt).Round(time.Second)
								fmt.Printf("     %s/%s: %d runs, $%.4f, last %v ago\n",
									s.Platform, channelDisplay, s.TotalRuns, s.TotalCostUSD, lastRun)
							}
						}
					}
				}
			}
		}
	}

	// ── 6. Version info ──
	fmt.Print("\n📦 Version info... ")
	versionData, err := os.ReadFile(filepath.Join(dianeDir, "version"))
	if err == nil {
		fmt.Printf("v%s\n", strings.TrimSpace(string(versionData)))
	} else {
		fmt.Println("(unknown)")
	}

	fmt.Println()
	fmt.Println("═══ End ═══")
}
