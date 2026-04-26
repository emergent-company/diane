package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/agents"
	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
)

func cmdAgent(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: diane agent <command>")
		fmt.Println("")
		fmt.Println("Commands:")
		fmt.Println("  list            List agent definitions (built-in + MP)")
		fmt.Println("  seed            Seed all built-in agents to Memory Platform")
		fmt.Println("  define <name>   Create or update a user-defined agent")
		fmt.Println("  show <name>     Show agent detail")
		fmt.Println("  sync [name]     Sync one or all user agents to Memory Platform")
		fmt.Println("  trigger <name> [prompt]  Trigger an agent run and show the result")
		fmt.Println("  delete <name>   Delete a user agent (local + MP)")
		fmt.Println("")
		fmt.Println("Built-in agents are immutable and ship with Diane. User agents")
		fmt.Println("are created dynamically and stored on Memory Platform.")
		return
	}

	switch args[0] {
	case "list":
		cmdAgentList()
	case "seed":
		cmdAgentSeed()
	case "define":
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
	case "sync":
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
		if len(args) < 2 {
			fmt.Println("Usage: diane agent delete <name>")
			return
		}
		cmdAgentDelete(args[1])
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
			fmt.Printf("       Tools: %d | Skills: %d | Flow: %s\n", toolCount, skillCount, orDefault(a.FlowType, "standard"))
			if a.Sandbox != nil && a.Sandbox.Enabled {
				fmt.Printf("       Sandbox: %s\n", orDefault(a.Sandbox.BaseImage, "default"))
			}
		}
	}

	fmt.Println()

	// Remote agents
	fmt.Println("🌐 Memory Platform (synced):")
	bridge, err := memory.New(memory.Config{
		ServerURL: pc.ServerURL,
		APIKey:    pc.Token,
		ProjectID: pc.ProjectID,
		OrgID:     pc.OrgID,
	})
	if err != nil {
		fmt.Printf("   ⚠️  Cannot connect: %v\n", err)
		return
	}
	defer bridge.Close()

	remoteAgents, err := bridge.ListAgentDefs(context.Background())
	if err != nil {
		fmt.Printf("   ⚠️  %v\n", err)
		return
	}
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
	prompt = fmt.Sprintf("Flow type (standard/acp/tool_use/auto) [%s]: ", orDefault(ac.FlowType, "standard"))
	fmt.Print(prompt)
	if ft := readLine(reader); ft != "" {
		ac.FlowType = ft
	} else if ac.FlowType == "" {
		ac.FlowType = "standard"
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
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	pc := cfg.Active()
	if pc == nil || pc.Agents == nil || pc.Agents[name] == nil {
		fmt.Printf("Agent '%s' not found in local config.\n", name)
		return
	}

	ac := pc.Agents[name]
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
		Name:        name,
		FlowType:    orDefault(ac.FlowType, "standard"),
		Visibility:  orDefault(ac.Visibility, "project"),
		DispatchMode: orDefault(ac.DispatchMode, "auto"),
		Description: strPtr(ac.Description),
		Tools:       ac.Tools,
		Skills:      ac.Skills,
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
	triggerResp, err := bridge.TriggerAgentWithInput(ctx, agentID, prompt)
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
	const maxPolls = 60  // 60 * 2s = 120s
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
