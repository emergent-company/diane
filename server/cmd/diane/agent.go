package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/agents"
	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/db"
	"github.com/Emergent-Comapny/diane/internal/memory"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

func cmdAgent(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: diane agent <command>")
		fmt.Println("")
		fmt.Println("Commands:")
		fmt.Println("  list            List agent definitions (built-in + MP)")
		fmt.Println("  seed            Seed all built-in agents to Memory Platform")
		fmt.Println("  seed-db         Seed all built-in agents to local SQLite database")
		fmt.Println("  list-db         List agents from local SQLite database")
		fmt.Println("  stats [name]    Show run stats for agents (from Memory Platform)")
		fmt.Println("  trace <runID>    Fetch full trace of an agent run (messages, tools, parent)")
		fmt.Println("  runs [name] [--since <duration>]  List recent agent runs from Memory Platform")
		fmt.Println("  define <name>   Create or update a user-defined agent  [master only]")
		fmt.Println("  show <name>     Show agent detail (from local DB)")
		fmt.Println("  route <name> <weight>  Set routing weight for A/B testing  [master only]")
		fmt.Println("  tag <name> <tags>      Set tags for agent (comma-separated)  [master only]")
		fmt.Println("  sync [name]     Sync one or all user agents to Memory Platform  [master only]")
		fmt.Println("  trigger <name> [prompt]  Trigger an agent run and show the result")
		fmt.Println("  delete <name>   Delete a user agent (local + MP)  [master only]")
		fmt.Println("  prune [--force] Remove orphaned agents from MP (dry-run without --force)  [master only]")
		fmt.Println("")
		fmt.Println("Local SQLite database (~/.diane/cron.db) is the single source of truth.")
		fmt.Println("Built-in agents are immutable and seeded from Go code on every startup.")
		return
	}

	switch args[0] {
	case "list":
		cmdAgentList()
	case "seed":
		requireMaster("agent seed")
		cmdAgentSeed()
	case "seed-db":
		requireMaster("agent seed-db")
		cmdAgentSeedDB()
	case "list-db":
		cmdAgentListDB()
	case "stats":
		name := ""
		if len(args) >= 2 {
			name = args[1]
		}
		cmdAgentStats(name)
	case "trace":
		runID := ""
		if len(args) >= 2 {
			runID = args[1]
		}
		cmdAgentTrace(runID)
	case "runs":
		name := ""
		sinceDur := "24h"
		for i := 1; i < len(args); i++ {
			if args[i] == "--since" && i+1 < len(args) {
				sinceDur = args[i+1]
				i++
			} else if !strings.HasPrefix(args[i], "--") {
				name = args[i]
			}
		}
		cmdAgentRuns(name, sinceDur)
	case "define":
		requireMaster("agent define")
		if len(args) < 2 {
			fmt.Println("Usage: diane agent define <name>")
			return
		}
		cmdAgentDefine(args[1])
	case "show":
		if len(args) < 2 {
			fmt.Println("Usage: diane agent show <name>")
			return
		}
		cmdAgentShow(args[1])
	case "route":
		requireMaster("agent route")
		if len(args) < 3 {
			fmt.Println("Usage: diane agent route <name> <weight>")
			return
		}
		cmdAgentRoute(args[1], args[2])
	case "tag":
		requireMaster("agent tag")
		if len(args) < 3 {
			fmt.Println("Usage: diane agent tag <name> <tag1,tag2,...>")
			return
		}
		cmdAgentTag(args[1], args[2])
	case "sync":
		requireMaster("agent sync")
		name := ""
		if len(args) >= 2 {
			name = args[1]
		}
		cmdAgentSync(name)
	case "trigger":
		prompt := "Say hello and describe your capabilities."
		name := ""
		if len(args) >= 2 {
			name = args[1]
		}
		if len(args) >= 3 {
			prompt = strings.Join(args[2:], " ")
		}
		cmdAgentTrigger(name, prompt)
	case "delete":
		requireMaster("agent delete")
		if len(args) < 2 {
			fmt.Println("Usage: diane agent delete <name>")
			return
		}
		cmdAgentDelete(args[1])
	case "prune":
		requireMaster("agent prune")
		force := false
		for _, a := range args[1:] {
			if a == "--force" {
				force = true
			}
		}
		cmdAgentPrune(force)
	default:
		fmt.Fprintf(os.Stderr, "Unknown agent command: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdAgentList() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Println("No project configured.")
		return
	}

	// Fetch remote agents for display
	var remoteAgents *sdkagents.APIResponse[[]sdkagents.AgentDefinitionSummary]
	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err == nil {
		defer bridge.Close()
		remoteAgents, _ = bridge.ListAgentDefs(context.Background())
	}

	if !jsonOutput {
		fmt.Println("═══ Agent Definitions ═══")
		fmt.Println()

		// Local agents
		fmt.Println("📋 Local config:")
		if len(pc.Agents) == 0 {
			fmt.Println("   No agents configured")
		} else {
			for name, a := range pc.Agents {
				toolCount := len(a.Tools)
				skillCount := len(a.Skills)
				fmt.Printf("   %s\n", name)
				if a.Description != "" {
					fmt.Printf("       %s\n", a.Description)
				}
				fmt.Printf("       Tools: %d | Skills: %d | Flow: %s\n", toolCount, skillCount, orDefault(a.FlowType, ""))
				if a.Sandbox != nil && a.Sandbox.Enabled {
					fmt.Printf("       Sandbox: %s\n", orDefault(a.Sandbox.BaseImage, "default"))
				}
			}
		}

		fmt.Println()

		// Remote agents
		fmt.Println("🌐 Memory Platform (synced):")
		if remoteAgents == nil || len(remoteAgents.Data) == 0 {
			fmt.Println("   No agents synced yet")
			fmt.Println("   Run 'diane agent sync' to push local agents")
		} else {
			for _, a := range remoteAgents.Data {
				def := ""
				if a.IsDefault {
					def = " [default]"
				}
				fmt.Printf("   %s — %s%s\n", a.Name, a.FlowType, def)
				if a.Description != nil {
					fmt.Printf("       %s\n", *a.Description)
				}
				fmt.Printf("       Tools: %d | Visibility: %s\n", a.ToolCount, a.Visibility)
			}
		}
	}
}

func cmdAgentDefine(name string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "No project configured.\n")
		os.Exit(1)
	}

	if pc.Agents == nil {
		pc.Agents = make(map[string]*config.AgentConfig)
	}

	existing := pc.Agents[name]
	if existing != nil {
		fmt.Printf("Agent '%s' already exists. Edit it? [y/N]: ", name)
		answer := readLine(bufio.NewReader(os.Stdin))
		if strings.ToLower(answer) != "y" && strings.ToLower(answer) != "yes" {
			fmt.Println("Aborted.")
			return
		}
	}

	reader := bufio.NewReader(os.Stdin)
	ac := &config.AgentConfig{}
	if existing != nil {
		ac = existing
	}

	fmt.Printf("=== Define Agent: %s ===\n", name)
	fmt.Println()

	// Description
	prompt := fmt.Sprintf("Description [%s]: ", orDefault(ac.Description, "General purpose AI assistant"))
	fmt.Print(prompt)
	if desc := readLine(reader); desc != "" {
		ac.Description = desc
	} else if ac.Description == "" {
		ac.Description = "General purpose AI assistant"
	}

	// System prompt
	fmt.Println("System prompt (multi-line, end with '.' on its own line):")
	if ac.SystemPrompt != "" {
		fmt.Printf("  Current: %s\n", truncateStr(ac.SystemPrompt, 80))
		fmt.Print("  New (or '.' to keep): ")
	} else {
		fmt.Println("  (press '.' on a blank line to skip)")
	}
	var promptLines []string
	for {
		line := readLine(reader)
		if line == "." {
			break
		}
		promptLines = append(promptLines, line)
	}
	if len(promptLines) > 0 {
		ac.SystemPrompt = strings.Join(promptLines, "\n")
	}

	// Flow type
	prompt = fmt.Sprintf("Flow type (single/sequential/loop) [%s]: ", ac.FlowType)
	fmt.Print(prompt)
	if ft := readLine(reader); ft != "" {
		ac.FlowType = ft
	}

	// Visibility
	prompt = fmt.Sprintf("Visibility (project/org/private) [%s]: ", orDefault(ac.Visibility, "project"))
	fmt.Print(prompt)
	if vis := readLine(reader); vis != "" {
		ac.Visibility = vis
	} else if ac.Visibility == "" {
		ac.Visibility = "project"
	}

	// Dispatch mode
	prompt = fmt.Sprintf("Dispatch mode (auto/manual) [%s]: ", orDefault(ac.DispatchMode, "auto"))
	fmt.Print(prompt)
	if dm := readLine(reader); dm != "" {
		ac.DispatchMode = dm
	} else if ac.DispatchMode == "" {
		ac.DispatchMode = "auto"
	}

	// Model config
	fmt.Print("\nOverride model? (y/N): ")
	if yn := readLine(reader); strings.ToLower(yn) == "y" || strings.ToLower(yn) == "yes" {
		if ac.Model == nil {
			ac.Model = &config.AgentModelConfig{}
		}
		fmt.Printf("  Provider [%s]: ", orDefault(ac.Model.Provider, "deepseek"))
		if p := readLine(reader); p != "" {
			ac.Model.Provider = p
		} else if ac.Model.Provider == "" {
			ac.Model.Provider = "deepseek"
		}
		fmt.Printf("  Model [%s]: ", orDefault(ac.Model.Name, "deepseek-chat"))
		if m := readLine(reader); m != "" {
			ac.Model.Name = m
		} else if ac.Model.Name == "" {
			ac.Model.Name = "deepseek-chat"
		}
	}

	// Tools
	fmt.Print("\nTools (comma-separated MCP tool names, e.g. github,gmail) ")
	if len(ac.Tools) > 0 {
		fmt.Printf("[%s]: ", strings.Join(ac.Tools, ","))
	} else {
		fmt.Print("[]: ")
	}
	if tools := readLine(reader); tools != "" {
		parts := strings.Split(tools, ",")
		ac.Tools = make([]string, 0, len(parts))
		for _, t := range parts {
			if t = strings.TrimSpace(t); t != "" {
				ac.Tools = append(ac.Tools, t)
			}
		}
	}

	// Skills
	fmt.Print("\nSkills (comma-separated) ")
	if len(ac.Skills) > 0 {
		fmt.Printf("[%s]: ", strings.Join(ac.Skills, ","))
	} else {
		fmt.Print("[]: ")
	}
	if skills := readLine(reader); skills != "" {
		parts := strings.Split(skills, ",")
		ac.Skills = make([]string, 0, len(parts))
		for _, s := range parts {
			if s = strings.TrimSpace(s); s != "" {
				ac.Skills = append(ac.Skills, s)
			}
		}
	}

	// Execution limits
	fmt.Printf("\nMax steps [%d]: ", orDefaultInt(ac.MaxSteps, 50))
	if ms := readLine(reader); ms != "" {
		fmt.Sscanf(ms, "%d", &ac.MaxSteps)
	} else if ac.MaxSteps == 0 {
		ac.MaxSteps = 50
	}
	fmt.Printf("Default timeout (seconds) [%d]: ", orDefaultInt(ac.DefaultTimeout, 300))
	if dt := readLine(reader); dt != "" {
		fmt.Sscanf(dt, "%d", &ac.DefaultTimeout)
	} else if ac.DefaultTimeout == 0 {
		ac.DefaultTimeout = 300
	}

	// Sandbox
	sandboxEnabled := ac.Sandbox != nil && ac.Sandbox.Enabled
	fmt.Printf("\nEnable sandbox? [%s]: ", ynStr(sandboxEnabled))
	if yn := readLine(reader); yn != "" {
		sandboxEnabled = strings.ToLower(yn) == "y" || strings.ToLower(yn) == "yes"
	}
	if sandboxEnabled {
		if ac.Sandbox == nil {
			ac.Sandbox = &config.SandboxConfig{}
		}
		ac.Sandbox.Enabled = true
		baseImage := ac.Sandbox.BaseImage
		if baseImage == "" {
			baseImage = "debian:bookworm-slim"
		}
		fmt.Printf("  Base image [%s]: ", baseImage)
		if bi := readLine(reader); bi != "" {
			ac.Sandbox.BaseImage = bi
		} else {
			ac.Sandbox.BaseImage = baseImage
		}
		fmt.Printf("  Pull policy (always/missing) [%s]: ", orDefault(ac.Sandbox.PullPolicy, "missing"))
		if pp := readLine(reader); pp != "" {
			ac.Sandbox.PullPolicy = pp
		} else if ac.Sandbox.PullPolicy == "" {
			ac.Sandbox.PullPolicy = "missing"
		}
		fmt.Print("  Env vars (KEY=VALUE, comma-separated, empty to skip): ")
		if env := readLine(reader); env != "" {
			if ac.Sandbox.Env == nil {
				ac.Sandbox.Env = make(map[string]string)
			}
			for _, pair := range strings.Split(env, ",") {
				pair = strings.TrimSpace(pair)
				if k, v, ok := strings.Cut(pair, "="); ok {
					ac.Sandbox.Env[strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
			}
		}
	} else {
		ac.Sandbox = nil
	}

	// ACP metadata
	fmt.Print("\nConfigure ACP card? (for agent discovery) [y/N]: ")
	if yn := readLine(reader); strings.ToLower(yn) == "y" || strings.ToLower(yn) == "yes" {
		if ac.ACP == nil {
			ac.ACP = &config.ACPConfig{}
		}
		fmt.Printf("  Display name [%s]: ", orDefault(ac.ACP.DisplayName, name))
		if dn := readLine(reader); dn != "" {
			ac.ACP.DisplayName = dn
		} else if ac.ACP.DisplayName == "" {
			ac.ACP.DisplayName = name
		}
		fmt.Printf("  Description [%s]: ", orDefault(ac.ACP.Description, ac.Description))
		if ad := readLine(reader); ad != "" {
			ac.ACP.Description = ad
		} else if ac.ACP.Description == "" {
			ac.ACP.Description = ac.Description
		}
		fmt.Print("  Capabilities (comma-separated) [text-generation]: ")
		if caps := readLine(reader); caps != "" {
			ac.ACP.Capabilities = splitTrim(caps)
		} else if len(ac.ACP.Capabilities) == 0 {
			ac.ACP.Capabilities = []string{"text-generation"}
		}
		fmt.Print("  Input modes (comma-separated) [text]: ")
		if im := readLine(reader); im != "" {
			ac.ACP.InputModes = splitTrim(im)
		} else if len(ac.ACP.InputModes) == 0 {
			ac.ACP.InputModes = []string{"text"}
		}
		fmt.Print("  Output modes (comma-separated) [text]: ")
		if om := readLine(reader); om != "" {
			ac.ACP.OutputModes = splitTrim(om)
		} else if len(ac.ACP.OutputModes) == 0 {
			ac.ACP.OutputModes = []string{"text"}
		}
	}

	pc.Agents[name] = ac
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\n✅ Agent '%s' saved to %s\n", name, config.Path())

	// Offer sync
	fmt.Print("\nSync to Memory Platform now? [Y/n]: ")
	if yn := readLine(reader); yn == "" || strings.ToLower(yn) == "y" || strings.ToLower(yn) == "yes" {
		doAgentSync(name, cfg, pc)
	}
}

func cmdAgentShow(name string) {
	cfg, err := config.Load()
	if err != nil {
		if jsonOutput {
			emitJSON("error", map[string]string{"message": fmt.Sprintf("Failed to load config: %v", err)})
		}
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil || pc.Agents == nil || pc.Agents[name] == nil {
		if jsonOutput {
			emitJSON("error", map[string]string{"message": fmt.Sprintf("Agent '%s' not found in local config.", name)})
		}
		fmt.Printf("Agent '%s' not found in local config.\n", name)
		return
	}

	ac := pc.Agents[name]

	if jsonOutput {
		emitJSON("ok", map[string]interface{}{
			"name":           name,
			"description":    ac.Description,
			"system_prompt":  ac.SystemPrompt,
			"flow_type":      orDefault(ac.FlowType, "standard"),
			"visibility":     orDefault(ac.Visibility, "project"),
			"dispatch_mode":  orDefault(ac.DispatchMode, "auto"),
			"max_steps":      orDefaultInt(ac.MaxSteps, 50),
			"default_timeout": orDefaultInt(ac.DefaultTimeout, 300),
			"tools":          ac.Tools,
			"skills":         ac.Skills,
			"model":          ac.Model,
			"sandbox":        ac.Sandbox,
			"acp":            ac.ACP,
		})
		return
	}

	fmt.Printf("═══ Agent: %s ═══\n", name)
	fmt.Printf("  Description:     %s\n", ac.Description)
	if ac.SystemPrompt != "" {
		fmt.Printf("  System prompt:   %d chars\n", len(ac.SystemPrompt))
	}
	fmt.Printf("  Flow type:       %s\n", orDefault(ac.FlowType, "standard"))
	fmt.Printf("  Visibility:      %s\n", orDefault(ac.Visibility, "project"))
	fmt.Printf("  Dispatch mode:   %s\n", orDefault(ac.DispatchMode, "auto"))
	fmt.Printf("  Max steps:       %d\n", orDefaultInt(ac.MaxSteps, 50))
	fmt.Printf("  Timeout:         %ds\n", orDefaultInt(ac.DefaultTimeout, 300))
	if ac.Model != nil {
		fmt.Printf("  Model provider:  %s\n", ac.Model.Provider)
		fmt.Printf("  Model name:      %s\n", orDefault(ac.Model.Name, "(auto)"))
	}
	if len(ac.Tools) > 0 {
		fmt.Printf("  Tools:           %s\n", strings.Join(ac.Tools, ", "))
	}
	if len(ac.Skills) > 0 {
		fmt.Printf("  Skills:          %s\n", strings.Join(ac.Skills, ", "))
	}
	if ac.Sandbox != nil && ac.Sandbox.Enabled {
		fmt.Printf("  Sandbox:         %s\n", orDefault(ac.Sandbox.BaseImage, "default"))
		fmt.Printf("  Pull policy:     %s\n", orDefault(ac.Sandbox.PullPolicy, "missing"))
		if len(ac.Sandbox.Env) > 0 {
			fmt.Printf("  Env vars:        %d\n", len(ac.Sandbox.Env))
		}
	}
	if ac.ACP != nil {
		fmt.Printf("  ACP:             %s\n", orDefault(ac.ACP.DisplayName, name))
		fmt.Printf("    Capabilities:  %s\n", strings.Join(ac.ACP.Capabilities, ", "))
	}

	fmt.Println()
	fmt.Println("Run 'diane agent sync' to push this agent to Memory Platform.")
}

func cmdAgentSync(name string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "No project configured.\n")
		os.Exit(1)
	}

	doAgentSync(name, cfg, pc)
}

func doAgentSync(name string, cfg *config.Config, pc *config.ProjectConfig) {
	// First seed built-in agents (ensure immutable agents are up to date)
	seedCtx, seedCancel := context.WithTimeout(context.Background(), 60*time.Second)
	seedBridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
	})
	if err == nil {
		fmt.Print("📦 Seeding built-in agents... ")
		if err := agents.SeedBuiltInAgents(seedCtx, seedBridge.Client()); err != nil {
			fmt.Printf("⚠️  %v\n", err)
		} else {
			fmt.Println("✅")
		}
		seedBridge.Close()
	}
	seedCancel()

	// Then sync user-defined agents
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	if name != "" {
		// Sync single agent
		ac := pc.Agents[name]
		if ac == nil {
			fmt.Printf("Agent '%s' not found in local config.\n", name)
			fmt.Println("Use 'diane agent define' to create it first.")
			return
		}
		syncOneAgent(ctx, bridge, name, ac)
		return
	}

	// Sync all agents
	if len(pc.Agents) == 0 {
		fmt.Println("⚠️  No agents configured locally. Use 'diane agent define' first.")
		return
	}

	fmt.Printf("═══ Syncing %d agent(s) to Memory Platform ═══\n", len(pc.Agents))
	fmt.Println()

	synced := 0
	for name, ac := range pc.Agents {
		fmt.Printf("  %s... ", name)
		if err := syncOneAgent(ctx, bridge, name, ac); err != nil {
			fmt.Printf("❌ %v\n", err)
		} else {
			fmt.Println("✅")
			synced++
		}
	}
	fmt.Printf("\n✅ Synced %d/%d agent(s)\n", synced, len(pc.Agents))
}

func cmdAgentSeed() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Config: %v\n", err)
		return
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "No project configured.\n")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Println("═══ Seeding Built-in Agents ═══")
	fmt.Println()

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
	})
	if err != nil {
		fmt.Printf("❌ Connection: %v\n", err)
		return
	}
	defer bridge.Close()

	builtIns := agents.BuiltInAgents()
	fmt.Printf("Found %d built-in agent(s):\n", len(builtIns))
	for _, ba := range builtIns {
		fmt.Printf("  • %s — %s\n", ba.Name, ba.Description)
	}
	fmt.Println()

	if err := agents.SeedBuiltInAgents(ctx, bridge.Client()); err != nil {
		fmt.Printf("❌ Seeding failed: %v\n", err)
		return
	}

	fmt.Println("✅ All built-in agents seeded to Memory Platform")
	fmt.Println("   They are immutable: cannot be deleted or renamed via CLI/API.")
}

func syncOneAgent(ctx context.Context, bridge *memory.Bridge, name string, ac *config.AgentConfig) error {
	// Build the SDK create request
	req := &sdkagents.CreateAgentDefinitionRequest{
		Name:         name,
		FlowType:     orDefault(ac.FlowType, "standard"),
		Visibility:   orDefault(ac.Visibility, "project"),
		DispatchMode: orDefault(ac.DispatchMode, "auto"),
		Description:  strPtr(ac.Description),
		Tools:        ac.Tools,
		Skills:       ac.Skills,
	}

	if ac.SystemPrompt != "" {
		req.SystemPrompt = strPtr(ac.SystemPrompt)
	}

	if ac.Model != nil {
		req.Model = &sdkagents.ModelConfig{
			Name:        ac.Model.Name,
			Temperature: fl32Ptr(ac.Model.Temperature),
			MaxTokens:   intPtr(ac.Model.MaxTokens),
		}
	}

	if ac.MaxSteps > 0 {
		req.MaxSteps = intPtr(ac.MaxSteps)
	}
	if ac.DefaultTimeout > 0 {
		req.DefaultTimeout = intPtr(ac.DefaultTimeout)
	}

	if ac.ACP != nil {
		req.ACPConfig = &sdkagents.ACPConfig{
			DisplayName:  ac.ACP.DisplayName,
			Description:  ac.ACP.Description,
			Capabilities: ac.ACP.Capabilities,
			InputModes:   ac.ACP.InputModes,
			OutputModes:  ac.ACP.OutputModes,
		}
	}

	// Check if this agent already exists on MP by listing
	existingList, err := bridge.ListAgentDefs(ctx)
	if err != nil {
		return fmt.Errorf("list existing: %w", err)
	}

	var existingID string
	if existingList != nil {
		for _, a := range existingList.Data {
			if a.Name == name {
				existingID = a.ID
				break
			}
		}
	}

	var defResp *sdkagents.APIResponse[sdkagents.AgentDefinition]
	if existingID != "" {
		// Update
		updReq := &sdkagents.UpdateAgentDefinitionRequest{
			Name:         &name,
			Description:  strPtr(ac.Description),
			SystemPrompt: strPtr(ac.SystemPrompt),
			FlowType:     strPtr(orDefault(ac.FlowType, "standard")),
			Visibility:   strPtr(orDefault(ac.Visibility, "project")),
			DispatchMode: strPtr(orDefault(ac.DispatchMode, "auto")),
			Tools:        ac.Tools,
			Skills:       ac.Skills,
		}
		if ac.Model != nil {
			updReq.Model = &sdkagents.ModelConfig{
				Name:        ac.Model.Name,
				Temperature: fl32Ptr(ac.Model.Temperature),
				MaxTokens:   intPtr(ac.Model.MaxTokens),
			}
		}
		if ac.MaxSteps > 0 {
			updReq.MaxSteps = intPtr(ac.MaxSteps)
		}
		if ac.DefaultTimeout > 0 {
			updReq.DefaultTimeout = intPtr(ac.DefaultTimeout)
		}
		if ac.ACP != nil {
			updReq.ACPConfig = &sdkagents.ACPConfig{
				DisplayName:  ac.ACP.DisplayName,
				Description:  ac.ACP.Description,
				Capabilities: ac.ACP.Capabilities,
				InputModes:   ac.ACP.InputModes,
				OutputModes:  ac.ACP.OutputModes,
			}
		}
		defResp, err = bridge.UpdateAgentDef(ctx, existingID, updReq)
	} else {
		defResp, err = bridge.CreateAgentDef(ctx, req)
	}
	if err != nil {
		return fmt.Errorf("upsert agent def: %w", err)
	}
	defID := defResp.Data.ID

	// Sync workspace config (sandbox)
	if ac.Sandbox != nil && ac.Sandbox.Enabled {
		sbConfig := map[string]any{
			"enabled":     true,
			"baseImage":   ac.Sandbox.BaseImage,
			"pull_policy": orDefault(ac.Sandbox.PullPolicy, "missing"),
		}
		if ac.Sandbox.Env != nil {
			sbConfig["env"] = ac.Sandbox.Env
		}
		if _, err := bridge.SetAgentWorkspaceConfig(ctx, defID, sbConfig); err != nil {
			return fmt.Errorf("set workspace config: %w", err)
		}
	}

	return nil
}

func cmdAgentTrigger(name, prompt string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Config: %v\n", err)
		return
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "No project configured.\n")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	fmt.Printf("═══ Triggering Agent: %s ═══\n\n", name)

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
	})
	if err != nil {
		fmt.Printf("❌ Connection: %v\n", err)
		return
	}
	defer bridge.Close()

	// 1. Find the agent definition on MP directly (not from local config)
	fmt.Print("🔍 Finding agent definition... ")
	defs, err := bridge.ListAgentDefs(ctx)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	var defID string
	for _, d := range defs.Data {
		if d.Name == name {
			defID = d.ID
			break
		}
	}
	if defID == "" {
		fmt.Println("❌ Not found on MP. Run 'diane agent sync' first.")
		return
	}
	fmt.Printf("✅ %s\n", defID)

	// 2. Create a runtime agent from the definition
	fmt.Print("🚀 Creating runtime agent... ")
	runtimeAgent, err := bridge.CreateRuntimeAgent(ctx, name, defID)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	agentID := runtimeAgent.Data.ID
	fmt.Printf("✅ %s\n", agentID)

	// 3. Trigger the agent
	fmt.Printf("💬 Triggering with: \"%s\"...\n", truncateStr(prompt, 60))
	triggerResp, err := bridge.TriggerAgentWithInput(ctx, agentID, prompt, "")
	if err != nil {
		fmt.Printf("❌ Trigger: %v\n", err)
		return
	}
	if triggerResp.Error != nil && *triggerResp.Error != "" {
		fmt.Printf("❌ Trigger error: %s\n", *triggerResp.Error)
		return
	}
	runID := *triggerResp.RunID
	fmt.Printf("   Run ID: %s\n", runID)

	// 4. Poll for completion
	fmt.Print("⏳ Waiting for result")
	const maxPolls = 60 // 60 * 2s = 120s
	for i := 0; i < maxPolls; i++ {
		time.Sleep(2 * time.Second)
		fmt.Print(".")

		run, err := bridge.GetProjectRun(ctx, runID)
		if err != nil {
			continue
		}
		if run.Data.Status == "success" || run.Data.Status == "completed" {
			fmt.Println(" ✅")
			fmt.Printf("\n📊 Duration: %dms\n", safeDeref(run.Data.DurationMs))
			fmt.Printf("📝 Steps: %d/%d\n", run.Data.StepCount, safeDeref(run.Data.MaxSteps))

			// Show summary
			if summary, ok := run.Data.Summary["final_response"]; ok {
				if s, ok := summary.(string); ok {
					fmt.Printf("\n📋 Response:\n%s\n", s)
				}
			}

			// Show messages
			msgs, err := bridge.GetRunMessages(ctx, runID)
			if err == nil && msgs != nil {
				fmt.Printf("\n💬 Messages (%d):\n", len(msgs.Data))
				for _, m := range msgs.Data {
					content := ""
					if text, ok := m.Content["content"]; ok {
						content = fmt.Sprintf("%v", text)
					} else if text, ok := m.Content["text"]; ok {
						content = fmt.Sprintf("%v", text)
					}
					prefix := "🤖"
					if m.Role == "user" {
						prefix = "👤"
					} else if m.Role == "tool" {
						prefix = "🔧"
					}
					fmt.Printf("   %s [%s]: %s\n", prefix, m.Role, truncateStr(content, 120))
				}
			}

			// Show tool calls
			toolCalls, err := bridge.GetRunToolCalls(ctx, runID)
			if err == nil && toolCalls != nil && len(toolCalls.Data) > 0 {
				fmt.Printf("\n🔧 Tool Calls (%d):\n", len(toolCalls.Data))
				for _, tc := range toolCalls.Data {
					fmt.Printf("   %s(%v)\n", tc.ToolName, truncateStr(fmt.Sprintf("%v", tc.Input), 80))
				}
			}

			// Record run stats to local DB
			func() {
				localDB, err := db.New("")
				if err != nil {
					return
				}
				defer localDB.Close()

				toolCallCount := 0
				if toolCalls != nil {
					toolCallCount = len(toolCalls.Data)
				}

				durMs := 0
				if run.Data.DurationMs != nil {
					durMs = *run.Data.DurationMs
				}
				inTokens := 0
				outTokens := 0
				if run.Data.TokenUsage != nil {
					inTokens = int(run.Data.TokenUsage.TotalInputTokens)
					outTokens = int(run.Data.TokenUsage.TotalOutputTokens)
				}

				if err := localDB.RecordRunStat(&db.AgentRunStat{
					AgentName:     name,
					RunID:         runID,
					DurationMs:    durMs,
					StepCount:     run.Data.StepCount,
					ToolCallCount: toolCallCount,
					InputTokens:   inTokens,
					OutputTokens:  outTokens,
					Status:        "success",
				}); err != nil {
					fmt.Fprintf(os.Stderr, "\n⚠️  Stats recording: %v\n", err)
				}
			}()
			return
		}
		if run.Data.Status == "failed" || run.Data.Status == "error" {
			fmt.Println(" ❌")
			errMsg := ""
			if run.Data.ErrorMessage != nil {
				errMsg = *run.Data.ErrorMessage
			}
			fmt.Printf("\n❌ Run failed: %s\n", errMsg)
			return
		}
	}
	fmt.Println(" ⏰ Timeout")
	fmt.Println("The run is taking longer than expected. Check Memory Platform UI for details.")
}

func cmdAgentDelete(name string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil || pc.Agents == nil || pc.Agents[name] == nil {
		fmt.Printf("Agent '%s' not found in local config.\n", name)
		return
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Delete agent '%s' from local config AND Memory Platform? [y/N]: ", name)
	if yn := readLine(reader); strings.ToLower(yn) != "y" && strings.ToLower(yn) != "yes" {
		fmt.Println("Aborted.")
		return
	}

	// Delete from MP if synced
	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err == nil {
		existingList, listErr := bridge.ListAgentDefs(context.Background())
		if listErr == nil && existingList != nil {
			for _, a := range existingList.Data {
				if a.Name == name {
					if delErr := bridge.DeleteAgentDef(context.Background(), a.ID); delErr != nil {
						fmt.Printf("⚠️  MP delete failed: %v\n", delErr)
					} else {
						fmt.Println("🗑️  Deleted from Memory Platform")
					}
					break
				}
			}
		}
		bridge.Close()
	}

	// Delete from config
	delete(pc.Agents, name)
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Agent '%s' deleted from local config\n", name)
}

// cmdAgentPrune removes orphaned agent definitions from Memory Platform.
// Orphaned = on MP but not in local config and not a built-in agent.
// Without --force, runs in dry-run mode (lists only).
func cmdAgentPrune(force bool) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Println("No project configured.")
		return
	}

	ctx := context.Background()
	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		fmt.Printf("❌ Connection: %v\n", err)
		return
	}
	defer bridge.Close()

	// Fetch all agents on MP
	defs, err := bridge.ListAgentDefs(ctx)
	if err != nil {
		fmt.Printf("❌ Failed to list agent definitions: %v\n", err)
		return
	}

	// Build built-in name set
	builtInSet := map[string]bool{}
	for _, ba := range agents.BuiltInAgents() {
		builtInSet[ba.Name] = true
	}

	// Find orphans
	type orphan struct {
		ID          string
		Name        string
		Description string
		ToolCount   int
	}
	var orphans []orphan
	for _, d := range defs.Data {
		_, inConfig := pc.Agents[d.Name]
		if !inConfig && !builtInSet[d.Name] {
			desc := ""
			if d.Description != nil {
				desc = *d.Description
			}
			orphans = append(orphans, orphan{
				ID:          d.ID,
				Name:        d.Name,
				Description: desc,
				ToolCount:   d.ToolCount,
			})
		}
	}

	if len(orphans) == 0 {
		fmt.Println("✅ No orphaned agents found — MP is clean.")
		return
	}

	fmt.Printf("🧹 Found %d orphaned agent(s) on Memory Platform:\n\n", len(orphans))
	for _, o := range orphans {
		fmt.Printf("   %s", o.Name)
		if o.ToolCount > 0 {
			fmt.Printf(" (%d tools)", o.ToolCount)
		}
		if o.Description != "" {
			desc := o.Description
			if len(desc) > 55 {
				desc = desc[:55] + "..."
			}
			fmt.Printf(" — %s", desc)
		}
		fmt.Println()
	}

	if !force {
		fmt.Println("\n⚠️  This is a dry-run. Nothing was deleted.")
		fmt.Println("   Run 'diane agent prune --force' to delete these agents.")
		return
	}

	if !force {
		fmt.Println()
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Delete %d orphaned agent(s) from Memory Platform? [y/N]: ", len(orphans))
		if yn := readLine(reader); strings.ToLower(yn) != "y" && strings.ToLower(yn) != "yes" {
			fmt.Println("Aborted.")
			return
		}
	}

	deleted := 0
	for _, o := range orphans {
		if delErr := bridge.DeleteAgentDef(ctx, o.ID); delErr != nil {
			fmt.Printf("   ❌ %s — %v\n", o.Name, delErr)
		} else {
			fmt.Printf("   🗑️  %s\n", o.Name)
			deleted++
		}
	}
	fmt.Printf("\n✅ Deleted %d/%d orphaned agents.\n", deleted, len(orphans))
}

// ============================================================================
// Local SQLite Database Commands
// ============================================================================

// cmdAgentSeedDB seeds all built-in agents to the local SQLite database.
func cmdAgentSeedDB() {
	d, err := db.New("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Database: %v\n", err)
		return
	}
	defer d.Close()

	if err := agents.SeedToDB(d); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Seed: %v\n", err)
		return
	}
	fmt.Println("✅ Built-in agents seeded to local SQLite database")

	// Show what was seeded
	all, err := d.ListAgentDefinitions("", nil)
	if err != nil {
		return
	}
	for _, a := range all {
		tools, _ := db.ToolsFromJSON(a.ToolsJSON)
		tags, _ := db.TagsFromJSON(a.TagsJSON)
		def := ""
		if a.IsDefault {
			def = " [default]"
		}
		fmt.Printf("  • %s%s — %s (%d tools, tags: %v)\n",
			a.Name, def, a.Status, len(tools), tags)
	}
}

// cmdAgentListDB lists agents from the local SQLite database.
func cmdAgentListDB() {
	d, err := db.New("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Database: %v\n", err)
		return
	}
	defer d.Close()

	all, err := d.ListAgentDefinitions("", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ List: %v\n", err)
		return
	}
	if len(all) == 0 {
		fmt.Println("No agents in local database. Run 'diane agent seed-db' first.")
		return
	}

	fmt.Println("═══ Local Agent Definitions ═══")
	for _, a := range all {
		tools, _ := db.ToolsFromJSON(a.ToolsJSON)
		tags, _ := db.TagsFromJSON(a.TagsJSON)
		def := ""
		if a.IsDefault {
			def = " 👑 default"
		}
		exp := ""
		if a.IsExperimental {
			exp = " 🧪 experimental"
		}
		fmt.Printf("  %s%s%s\n", a.Name, def, exp)
		fmt.Printf("    Source: %s | Status: %s | Weight: %.2f\n", a.Source, a.Status, a.RoutingWeight)
		fmt.Printf("    Tools: %d | Flow: %s | Attempts: %d\n", len(tools), a.FlowType, a.MaxSteps)
		if len(tags) > 0 {
			fmt.Printf("    Tags: %v\n", tags)
		}
		if a.Description != "" {
			fmt.Printf("    %s\n", a.Description)
		}
		fmt.Println()
	}
}

// cmdAgentStats shows run statistics for agents from the Memory Platform.
func cmdAgentStats(name string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Config: %v\n", err)
		return
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintln(os.Stderr, "❌ No active project. Run 'diane init' or configure ~/.config/diane.yml")
		return
	}

	ctx := context.Background()
	since := time.Now().Add(-24 * time.Hour)
	opts := &sdkagentrun.RunStatsOptions{
		Since: &since,
	}

	if name != "" {
		opts.AgentID = name
	}

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Memory Platform: %v\n", err)
		return
	}
	defer bridge.Close()

	resp, err := bridge.GetProjectRunStats(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Stats: %v\n", err)
		return
	}

	stats := resp.Data

	if name != "" {
		// Show individual agent stats
		as, ok := stats.ByAgent[name]
		if !ok {
			fmt.Printf("No runs recorded for %q in the last 24 hours.\n", name)
			return
		}
		successRate := float64(0)
		if as.Total > 0 {
			successRate = float64(as.Success) / float64(as.Total) * 100
		}
		fmt.Printf("═══ Stats for %s ═══\n", name)
		fmt.Printf("  Runs: %d | Success: %d | Failures: %d\n", as.Total, as.Success, as.Failed+as.Errored)
		fmt.Printf("  Success rate: %.0f%% | Avg duration: %.0fms\n", successRate, as.AvgDurationMs)
		fmt.Printf("  Avg input: %.0f | Avg output: %.0f\n", as.AvgInputTokens, as.AvgOutputTokens)
	} else {
		// Show summary for all agents
		if len(stats.ByAgent) == 0 {
			fmt.Println("No runs recorded in the last 24 hours.")
			return
		}
		fmt.Println("═══ Agent Stats (last 24h) ═══")
		for name, as := range stats.ByAgent {
			successRate := float64(0)
			if as.Total > 0 {
				successRate = float64(as.Success) / float64(as.Total) * 100
			}
			fmt.Printf("  %s\n", name)
			fmt.Printf("    Runs: %d | Success: %.0f%% | Avg: %.0fms | Avg tokens: %.0f in / %.0f out\n",
				as.Total, successRate, as.AvgDurationMs,
				as.AvgInputTokens, as.AvgOutputTokens)
		}
	}
}

// cmdAgentRoute sets the routing weight for an agent.
func cmdAgentRoute(name, weightStr string) {
	var weight float64
	if _, err := fmt.Sscanf(weightStr, "%f", &weight); err != nil || weight < 0 || weight > 1 {
		fmt.Fprintf(os.Stderr, "❌ Weight must be a float between 0.0 and 1.0\n")
		return
	}

	d, err := db.New("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Database: %v\n", err)
		return
	}
	defer d.Close()

	a, err := d.GetAgentDefinition(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Lookup: %v\n", err)
		return
	}
	if a == nil {
		fmt.Fprintf(os.Stderr, "❌ Agent '%s' not found\n", name)
		return
	}

	a.RoutingWeight = weight
	if err := d.UpsertAgentDefinition(a); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Update: %v\n", err)
		return
	}
	fmt.Printf("✅ %s routing weight set to %.2f\n", name, weight)
}

// cmdAgentTag sets tags for an agent.
func cmdAgentTag(name, tagsStr string) {
	tagList := splitTrim(tagsStr)
	tagsJSON, err := json.Marshal(tagList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Marshal tags: %v\n", err)
		return
	}

	d, err := db.New("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Database: %v\n", err)
		return
	}
	defer d.Close()

	a, err := d.GetAgentDefinition(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Lookup: %v\n", err)
		return
	}
	if a == nil {
		fmt.Fprintf(os.Stderr, "❌ Agent '%s' not found\n", name)
		return
	}

	a.TagsJSON = string(tagsJSON)
	if err := d.UpsertAgentDefinition(a); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Update: %v\n", err)
		return
	}
	fmt.Printf("✅ %s tags set to: %v\n", name, tagList)
}

// ============================================================================
// Agent Run Trace & Inspect
// ============================================================================

// cmdAgentTrace fetches and displays the full trace of an agent run.
func cmdAgentTrace(runID string) {
	if runID == "" {
		fmt.Println("Usage: diane agent trace <runID>")
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Config: %v\n", err)
		return
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "No project configured.\n")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Bridge: %v\n", err)
		return
	}
	defer bridge.Close()

	fmt.Println("═══ Agent Run Trace ═══")
	fmt.Printf("Run ID: %s\n\n", runID)

	// Fetch full trace
	full, err := bridge.GetProjectRunFull(ctx, runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Fetch: %v\n", err)
		return
	}

	run := full.Data.Run
	if run == nil {
		fmt.Println("⚠️  No run data returned.")
		return
	}

	// Run metadata
	fmt.Println("── Run Metadata ──")
	fmt.Printf("  Agent:        %s (%s)\n", run.AgentName, run.AgentID)
	fmt.Printf("  Status:       %s\n", run.Status)
	duration := "N/A"
	if run.DurationMs != nil {
		duration = fmt.Sprintf("%dms (%s)", *run.DurationMs, time.Duration(*run.DurationMs)*time.Millisecond)
	}
	fmt.Printf("  Duration:     %s\n", duration)
	fmt.Printf("  Steps:        %d\n", run.StepCount)
	fmt.Printf("  Started:      %s\n", run.StartedAt.Format(time.RFC3339))
	if run.CompletedAt != nil {
		fmt.Printf("  Completed:    %s\n", run.CompletedAt.Format(time.RFC3339))
	}
	if run.ErrorMessage != nil {
		fmt.Printf("  Error:        %s\n", *run.ErrorMessage)
	}
	modelName := "N/A"
	if run.Model != nil {
		modelName = *run.Model
	}
	providerName := "N/A"
	if run.Provider != nil {
		providerName = *run.Provider
	}
	fmt.Printf("  Model:        %s\n", modelName)
	fmt.Printf("  Provider:     %s\n", providerName)
	if run.TraceID != nil {
		fmt.Printf("  Trace ID:     %s\n", *run.TraceID)
	}
	if run.RootRunID != nil {
		fmt.Printf("  Root Run ID:  %s\n", *run.RootRunID)
	}
	if run.ParentRunID != nil {
		fmt.Printf("  Parent Run:   %s\n", *run.ParentRunID)
	}
	if run.TokenUsage != nil {
		fmt.Printf("  Input Tokens: %d\n", run.TokenUsage.TotalInputTokens)
		fmt.Printf("  Output Tokens: %d\n", run.TokenUsage.TotalOutputTokens)
		fmt.Printf("  Cost:         $%.6f\n", run.TokenUsage.EstimatedCostUSD)
	}
	if len(run.Summary) > 0 {
		fmt.Println("\n── Summary ──")
		for k, v := range run.Summary {
			fmt.Printf("  %s: %v\n", k, truncateStr(fmt.Sprintf("%v", v), 200))
		}
	}

	// Messages
	if len(full.Data.Messages) > 0 {
		fmt.Printf("\n── Messages (%d) ──\n", len(full.Data.Messages))
		for _, m := range full.Data.Messages {
			content := ""
			for _, key := range []string{"content", "text", "response"} {
				if v, ok := m.Content[key]; ok {
					content = truncateStr(fmt.Sprintf("%v", v), 200)
					break
				}
			}
			prefix := ""
			switch m.Role {
			case "user":
				prefix = "👤"
			case "assistant", "model":
				prefix = "🤖"
			case "tool", "function":
				prefix = "🔧"
			default:
				prefix = "💬"
			}
			fmt.Printf("  %s [%s] (step %d): %s\n", prefix, m.Role, m.StepNumber, content)
		}
	}

	// Tool calls
	if len(full.Data.ToolCalls) > 0 {
		fmt.Printf("\n── Tool Calls (%d) ──\n", len(full.Data.ToolCalls))
		for _, tc := range full.Data.ToolCalls {
			inputPreview := ""
			for _, key := range []string{"query", "question", "name", "message", "url", "text"} {
				if v, ok := tc.Input[key]; ok {
					inputPreview = truncateStr(fmt.Sprintf("%v", v), 100)
					break
				}
			}
			status := "✅"
			if tc.Status == "error" || tc.Status == "failed" {
				status = "❌"
			}
			dur := ""
			if tc.DurationMs != nil {
				dur = fmt.Sprintf(" [%dms]", *tc.DurationMs)
			}
			fmt.Printf("  %s %s(%s)%s\n", status, tc.ToolName, inputPreview, dur)

			// Show output for interesting tool calls
			if tc.ToolName == "search-knowledge" && len(tc.Output) > 0 {
				if rid, ok := tc.Output["run_id"]; ok {
					fmt.Printf("       → run_id: %v\n", rid)
				}
				if answer, ok := tc.Output["answer"]; ok {
					fmt.Printf("       → answer: %s\n", truncateStr(fmt.Sprintf("%v", answer), 150))
				}
				if sessionID, ok := tc.Output["session_id"]; ok {
					fmt.Printf("       → session_id: %v\n", sessionID)
				}
			}
		}

		// Search for search-knowledge calls and suggest trace inspection
		for _, tc := range full.Data.ToolCalls {
			if tc.ToolName == "search-knowledge" {
				fmt.Println()
				fmt.Println("  🔗 This run called search-knowledge.")
				fmt.Println("     Each search-knowledge call creates an internal graph-query-agent run.")
				fmt.Println("     To trace the internal cost, run: diane agent runs 'Chat session for graph-query-agent'")
			}
		}
	}

	// Parent run
	if full.Data.ParentRun != nil {
		pr := full.Data.ParentRun
		fmt.Printf("\n── Parent Run ──\n")
		fmt.Printf("  ID:       %s\n", pr.ID)
		fmt.Printf("  Agent:    %s\n", pr.AgentName)
		fmt.Printf("  Status:   %s\n", pr.Status)
		if pr.DurationMs != nil {
			fmt.Printf("  Duration: %dms\n", *pr.DurationMs)
		}
		if pr.TokenUsage != nil {
			fmt.Printf("  Cost:     $%.6f\n", pr.TokenUsage.EstimatedCostUSD)
		}
		fmt.Printf("\n  To view parent run: diane agent trace %s\n", pr.ID)
	}
}

// cmdAgentRuns lists recent agent runs from Memory Platform.
func cmdAgentRuns(agentName, sinceStr string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Config: %v\n", err)
		return
	}
	pc := cfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "No project configured.\n")
		return
	}

	since, err := time.ParseDuration(sinceStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Invalid duration: %s (use e.g. '24h', '1h', '7d')\n", sinceStr)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Bridge: %v\n", err)
		return
	}
	defer bridge.Close()

	// Try aggregate stats endpoint first
	sinceTime := time.Now().Add(-since)
	runsResp, err := bridge.GetProjectRunStats(ctx, &sdkagentrun.RunStatsOptions{
		Since: &sinceTime,
	})
	if err == nil && runsResp != nil {
		stats := runsResp.Data
		fmt.Printf("═══ Agent Runs (last %s) ═══\n\n", sinceStr)

		// Overview
		o := stats.Overview
		fmt.Printf("Total: %d | ✅ %d | ❌ %d | ⚠️  %d | Avg: %v | Cost: $%.4f\n\n",
			o.TotalRuns, o.SuccessCount, o.FailedCount, o.ErrorCount,
			time.Duration(o.AvgDurationMs)*time.Millisecond, o.TotalCostUSD)

		// Per-agent breakdown
		if len(stats.ByAgent) > 0 {
			var names []string
			for name := range stats.ByAgent {
				names = append(names, name)
			}
			sort.Strings(names)

			filtered := names
			if agentName != "" {
				filtered = nil
				for _, n := range names {
					if strings.Contains(n, agentName) {
						filtered = append(filtered, n)
					}
				}
			}

			for _, name := range filtered {
				a := stats.ByAgent[name]
				fmt.Printf("  %s\n", name)
				fmt.Printf("    Runs: %d | ✅ %d | ❌ %d | Avg: %v | Cost: $%.4f | Tokens: %.0f in / %.0f out\n",
					a.Total, a.Success, a.Failed,
					time.Duration(a.AvgDurationMs)*time.Millisecond,
					a.TotalCostUSD, a.AvgInputTokens, a.AvgOutputTokens)
			}
			// Hint that different agents may use different models
			if len(filtered) > 0 && agentName == "" {
				fmt.Println("\nℹ️  Agents can use different models — check with: diane agent trace <runID>")
			}
		}

		// Tool stats
		if stats.ToolStats.TotalToolCalls > 0 {
			fmt.Printf("\n── Tool Calls: %d total ──\n", stats.ToolStats.TotalToolCalls)
			type toolEntry struct {
				name string
				stat sdkagentrun.RunStatsTool
			}
			var tools []toolEntry
			for name, stat := range stats.ToolStats.ByTool {
				tools = append(tools, toolEntry{name, stat})
			}
			sort.Slice(tools, func(i, j int) bool {
				return tools[i].stat.Total > tools[j].stat.Total
			})
			for _, t := range tools {
				rate := float64(0)
				if t.stat.Total > 0 {
					rate = float64(t.stat.Success) / float64(t.stat.Total) * 100
				}
				fmt.Printf("  %s: %d calls (%d ✅, %.0f%%, avg %v)\n",
					t.name, t.stat.Total, t.stat.Success, rate,
					time.Duration(t.stat.AvgDurationMs)*time.Millisecond)
			}
		}

		// Top errors
		if len(stats.TopErrors) > 0 {
			fmt.Println("\n── Top Errors ──")
			for _, e := range stats.TopErrors {
				fmt.Printf("  %d× %s\n", e.Count, e.Message)
			}
		}

		// Hint to dig deeper
		if agentName != "" {
			fmt.Printf("\n💡 To see full details of a run: diane agent trace <runID>\n")
		} else {
			fmt.Printf("\n💡 To filter by agent: diane agent runs <agent-name>\n")
			fmt.Printf("💡 To trace a specific run: diane agent trace <runID>\n")
		}
		return
	}

	// Fallback: list all project runs directly
	fmt.Printf("═══ Recent Runs (last %s) — Legacy API ═══\n\n", sinceStr)
	runs, err := bridge.Client().Agents.ListProjectRuns(ctx, pc.ProjectID, &sdkagentrun.ListRunsOptions{
		Limit: 20,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ List runs: %v\n", err)
		return
	}

	filtered := runs.Data.Items
	if agentName != "" {
		var match []sdkagentrun.AgentRun
		for _, r := range filtered {
			if strings.Contains(r.AgentName, agentName) {
				match = append(match, r)
			}
		}
		filtered = match
	}

	if len(filtered) == 0 {
		fmt.Println("No runs found.")
		return
	}

	for _, r := range filtered {
		dur := "N/A"
		if r.DurationMs != nil {
			dur = fmt.Sprintf("%dms", *r.DurationMs)
		}
		fmt.Printf("  %s\n", r.ID)
		fmt.Printf("    Agent: %s | Status: %s | Duration: %s | Steps: %d\n",
			r.AgentName, r.Status, dur, r.StepCount)
		if r.TokenUsage != nil {
			fmt.Printf("    Cost: $%.6f | Tokens: %d in / %d out\n",
				r.TokenUsage.EstimatedCostUSD, r.TokenUsage.TotalInputTokens, r.TokenUsage.TotalOutputTokens)
		}
		fmt.Printf("    Started: %s\n", r.StartedAt.Format(time.RFC3339))
		if r.ErrorMessage != nil {
			fmt.Printf("    Error: %s\n", *r.ErrorMessage)
		}
		fmt.Println()
	}
	fmt.Printf("💡 Full trace: diane agent trace <runID>\n")
}

// ─── Helpers ───

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func orDefaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func ynStr(v bool) string {
	if v {
		return "Y/n"
	}
	return "y/N"
}

func splitTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(v int) *int {
	return &v
}

func fl32Ptr(v float32) *float32 {
	return &v
}

func safeDeref[T int | int64](p *T) T {
	if p == nil {
		return 0
	}
	return *p
}

// requireMaster checks that the active project is a master node.
// Slaves cannot manage agent definitions — this is the master's job.
// Prints an error and exits if called on a slave node.
func requireMaster(command string) {
	cfg, err := config.Load()
	if err != nil {
		return // let the actual command handle config errors
	}
	pc := cfg.Active()
	if pc != nil && pc.IsSlave() {
		fmt.Fprintf(os.Stderr, "❌ 'diane %s' is not available on slave nodes.\n", command)
		fmt.Fprintf(os.Stderr, "   Agent management is the master node's responsibility.\n")
		fmt.Fprintf(os.Stderr, "   Run this command on the master node, or change mode in ~/.config/diane.yml.\n")
		os.Exit(1)
	}
}
