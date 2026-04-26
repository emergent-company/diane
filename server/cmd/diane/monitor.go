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
				sdkClient := bridge.Client()

				// Get latest runs across all agents
				runsResp, err := sdkClient.Agents.ListProjectRuns(ctx, pc.ProjectID, nil)
				if err != nil {
					fmt.Printf("⚠️  %v\n", err)
				} else if len(runsResp.Data.Items) == 0 {
					fmt.Println("None")
				} else {
					total := len(runsResp.Data.Items)
					success := 0
					failed := 0
					var totalDur int
					agents := map[string]int{}

					for _, r := range runsResp.Data.Items {
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
					fmt.Printf("%d total, %d✅ %d❌ (%.0f%% success), avg %v\n",
						total, success, failed, successRate, avgDur)

					// Show agent breakdown
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
			}
		}
	}

	fmt.Println()
	fmt.Println("═══ End ═══")
}
