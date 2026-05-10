package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/agents"
	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
)

// DoctorCheck represents a single diagnostic check result.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "pass", "fail", "warn", "skip"
	Detail string `json:"detail,omitempty"`
}

// DoctorReport is the complete JSON output of diane doctor --json.
type DoctorReport struct {
	Version  string        `json:"version"`
	Passed   int           `json:"passed"`
	Failed   int           `json:"failed"`
	Warnings int           `json:"warnings"`
	Total    int           `json:"total"`
	Checks   []DoctorCheck `json:"checks"`
}

func cmdDoctor(args []string) {
	jsonOutput := false
	for _, a := range args {
		if a == "--json" {
			jsonOutput = true
		}
	}

	if jsonOutput {
		runDoctorJSON()
	} else {
		runDoctorText()
	}
}

func runDoctorText() {
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

	// ── 6. Project name from Memory Platform ──
	fmt.Print("\n🏷️  Project name... ")
	sdkClient := bridge.Client()
	proj, err := sdkClient.Projects.Get(ctx, pc.ProjectID, nil)
	if err != nil {
		fmt.Printf("⚠️  %v\n", err)
	} else {
		fmt.Printf("✅ \"%s\"\n", proj.Name)
		if pc.OrgID == "" && proj.OrgID != "" {
			sdkClient.SetContext(proj.OrgID, pc.ProjectID)
		}
	}

	// ── 7. LLM provider ──
	fmt.Print("\n🤖 LLM provider... ")
	orgID := pc.OrgID
	if orgID == "" {
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
	builtInSet := map[string]bool{}
	for _, ba := range agents.BuiltInAgents() {
		builtInSet[ba.Name] = true
	}

	fmt.Print("\n🧠 Agent Definitions")
	if totalRemote == 0 {
		if err != nil {
			fmt.Printf(" — ⚠️  %v\n", err)
		} else {
			fmt.Println(" — none configured")
			fmt.Println("   Run 'diane agent define <name>' or 'diane agent seed' to get started.")
		}
	} else {
		fmt.Printf(" — %d on Memory Platform\n", totalRemote)
		names := make([]string, 0, totalRemote)
		for name := range remoteNameSet {
			names = append(names, name)
		}
		sort.Strings(names)
		builtInCount := 0
		for _, name := range names {
			if builtInSet[name] {
				builtInCount++
			}
			d := remoteNameSet[name]
			label := "🔧"
			if !builtInSet[name] {
				label = "🧑"
			}
			desc := ""
			if d.Description != nil {
				desc = *d.Description
				if len(desc) > 55 {
					desc = desc[:55] + "..."
				}
			}
			fmt.Printf("   %s %-25s", label, name)
			if d.ToolCount > 0 {
				fmt.Printf(" [%d tools]", d.ToolCount)
			}
			if desc != "" {
				fmt.Printf("  — %s", desc)
			}
			fmt.Printf("  Flow: %s  Vis: %s", orDefault(d.FlowType, "standard"), d.Visibility)
			if d.IsDefault {
				fmt.Print("  [default]")
			}
			fmt.Println()
		}
		orphaned := totalRemote - builtInCount
		if orphaned > 0 {
			fmt.Printf("   (%d built-in, %d user-defined)\n", builtInCount, orphaned)
		}
	}

	// ── 7e. Run stats ──
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
	if pc.DiscordBotToken != "" {
		fmt.Printf("✅ configured (%d channel(s))\n", len(pc.DiscordChannelIDs))
	} else {
		fmt.Println("⚠️  Not configured (optional)")
	}

	fmt.Println("\n═══ Done ═══")
}

func runDoctorJSON() {
	ctx := context.Background()
	checks := []DoctorCheck{}
	add := func(name, status, detail string) {
		checks = append(checks, DoctorCheck{Name: name, Status: status, Detail: detail})
	}

	// ── 1. Config file ──
	cfg, err := config.Load()
	if err != nil {
		add("Config file", "fail", err.Error())
		emitJSON(checks)
		return
	}
	pc := cfg.Active()
	if pc == nil {
		add("Config file", "fail", "No project configured — run 'diane init'")
		emitJSON(checks)
		return
	}
	add("Config file", "pass", fmt.Sprintf("Project: %s, Server: %s, Mode: %s", pc.ProjectID, pc.ServerURL, pc.ModeLabel()))

	// ── 2. Project ID format ──
	if len(pc.ProjectID) == 36 {
		add("Project ID", "pass", pc.ProjectID)
	} else {
		add("Project ID", "warn", fmt.Sprintf("Not a UUID (got %d chars)", len(pc.ProjectID)))
	}

	// ── 3. API token ──
	switch {
	case pc.Token == "":
		add("API token", "fail", "Not set")
		emitJSON(checks)
		return
	case len(pc.Token) >= 10:
		add("API token", "pass", fmt.Sprintf("%s...%s (%d chars)", pc.Token[:8], pc.Token[len(pc.Token)-4:], len(pc.Token)))
	default:
		add("API token", "warn", "Too short to be valid")
	}

	// ── 4. SDK connection ──
	bridge, err := memory.New(memory.Config{
		ServerURL:         pc.ServerURL,
		APIKey:            pc.Token,
		ProjectID:         pc.ProjectID,
		OrgID:             pc.OrgID,
		HTTPClientTimeout: 10 * time.Second,
	})
	if err != nil {
		add("Memory SDK connection", "fail", err.Error())
		emitJSON(checks)
		return
	}
	defer bridge.Close()
	add("Memory SDK connection", "pass", "SDK initialized")

	// ── 5. Project name ──
	sdkClient := bridge.Client()
	proj, err := sdkClient.Projects.Get(ctx, pc.ProjectID, nil)
	if err != nil {
		add("Project name", "warn", err.Error())
	} else {
		add("Project name", "pass", fmt.Sprintf("\"%s\"", proj.Name))
		if pc.OrgID == "" && proj.OrgID != "" {
			sdkClient.SetContext(proj.OrgID, pc.ProjectID)
		}
	}

	// ── 6. LLM provider ──
	orgID := pc.OrgID
	if orgID == "" {
		if proj == nil {
			if p2, err2 := sdkClient.Projects.Get(ctx, pc.ProjectID, nil); err2 == nil {
				orgID = p2.OrgID
			}
		} else {
			orgID = proj.OrgID
		}
	}
	if orgID == "" {
		add("LLM provider", "warn", "Could not determine org ID")
	} else {
		providers, err := sdkClient.Provider.ListOrgConfigs(ctx, orgID)
		if err != nil {
			add("LLM provider", "warn", err.Error())
		} else if len(providers) == 0 {
			add("LLM provider", "warn", "No org providers configured")
		} else {
			var descs []string
			for _, p := range providers {
				model := p.GenerativeModel
				if model == "" {
					model = "(auto)"
				}
				descs = append(descs, fmt.Sprintf("%s → %s", p.Provider, model))
			}
			add("LLM provider", "pass", strings.Join(descs, ", "))
		}
	}

	// ── 7. Agent definitions ──
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
	builtInSet := map[string]bool{}
	for _, ba := range agents.BuiltInAgents() {
		builtInSet[ba.Name] = true
	}

	if totalLocal == 0 && totalRemote == 0 {
		add("Agent definitions", "skip", "None configured")
	} else {
		detail := fmt.Sprintf("%d in config, %d on MP", totalLocal, totalRemote)
		if totalLocal > 0 && deployed == totalLocal {
			detail += " — all deployed"
			add("Agent definitions", "pass", detail)
		} else if totalLocal > 0 {
			detail += fmt.Sprintf(" — %d deployed, %d pending", deployed, totalLocal-deployed)
			add("Agent definitions", "warn", detail)
		} else {
			add("Agent definitions", "pass", detail)
		}
	}

	// ── 8. Run stats ──
	stats, err := bridge.GetProjectRunStats(ctx, nil)
	if err != nil {
		add("Run stats", "warn", err.Error())
	} else {
		s := stats.Data
		add("Run stats", "pass", fmt.Sprintf("%d runs, %.1f%% success, $%.4f total", s.Overview.TotalRuns, s.Overview.SuccessRate*100, s.Overview.TotalCostUSD))
	}

	// ── 9. Session CRUD ──
	session, err := bridge.CreateSession(ctx, "diane-doctor-check")
	if err != nil {
		add("Session CRUD", "fail", fmt.Sprintf("CreateSession: %v", err))
		emitJSON(checks)
		return
	}
	_, err = bridge.AppendMessage(ctx, session.ID, "user", "doctor test message", 0)
	if err != nil {
		add("Session CRUD", "fail", fmt.Sprintf("AppendMessage: %v", err))
		_ = bridge.CloseSession(ctx, session.ID)
		emitJSON(checks)
		return
	}
	msgs, err := bridge.GetMessages(ctx, session.ID)
	if err != nil {
		add("Session CRUD", "fail", fmt.Sprintf("GetMessages: %v", err))
		_ = bridge.CloseSession(ctx, session.ID)
		emitJSON(checks)
		return
	}
	err = bridge.CloseSession(ctx, session.ID)
	if err != nil {
		add("Session CRUD", "fail", fmt.Sprintf("CloseSession: %v", err))
		emitJSON(checks)
		return
	}
	add("Session CRUD", "pass", fmt.Sprintf("created → wrote → read %d msgs → closed", len(msgs)))

	// ── 10. Memory search ──
	results, err := bridge.SearchMemory(ctx, "doctor test", 3)
	if err != nil {
		add("Memory search", "warn", err.Error())
	} else {
		add("Memory search", "pass", fmt.Sprintf("%d results", len(results)))
	}

	// ── 10. CLI / App version match ──
	appVer := readInstalledAppVersion()
	if appVer == "" {
		add("CLI / App version", "skip", "Diane.app not installed")
	} else {
		cliVer := strings.TrimPrefix(Version, "v")
		appVerClean := strings.TrimPrefix(appVer, "v")
		if cliVer == appVerClean || (cliVer == "dev" && strings.HasPrefix(appVerClean, "dev")) {
			add("CLI / App version", "pass", fmt.Sprintf("CLI=%s, App=%s — match", Version, appVer))
		} else {
			add("CLI / App version", "warn", fmt.Sprintf("CLI=%s, App=%s — MISMATCH", Version, appVer))
		}
	}

	// ── 11. Discord config ──
	if pc.DiscordBotToken != "" {
		add("Discord bot", "pass", fmt.Sprintf("configured (%d channels)", len(pc.DiscordChannelIDs)))
	} else {
		add("Discord bot", "skip", "Not configured (optional)")
	}

	emitJSON(checks)
}

func emitJSON(checks []DoctorCheck) {
	report := DoctorReport{
		Version: Version,
		Checks:  checks,
		Total:   len(checks),
	}
	for _, c := range checks {
		switch c.Status {
		case "pass":
			report.Passed++
		case "fail":
			report.Failed++
		case "warn":
			report.Warnings++
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
