// Package config manages persistent Diane configuration.
//
// Config is stored at ~/.config/diane.yml (default path).
// Supports multiple named projects with their own Memory Platform credentials
// and Discord bot settings.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultDir is the default directory for Diane config files.
var DefaultDir = filepath.Join(os.Getenv("HOME"), ".config")

// Config is the top-level configuration structure.
type Config struct {
	Projects map[string]*ProjectConfig `yaml:"projects"`
	Default  string                    `yaml:"default,omitempty"` // default project name
}

// ProjectConfig holds credentials and settings for one Memory project.
type ProjectConfig struct {
	// Memory Platform
	ServerURL string `yaml:"server_url"`
	Token     string `yaml:"token"`
	ProjectID string `yaml:"project_id"`
	OrgID     string `yaml:"org_id,omitempty"`

	// Node mode: "master" (default, runs Discord + manages agents) or
	// "slave" (MCP relay only — no Discord, no agent management).
	// Empty defaults to "master" for backward compatibility.
	Mode string `yaml:"mode,omitempty"`

	// Discord Bot (optional, per project)
	DiscordBotToken      string   `yaml:"discord_bot_token,omitempty"`
	DiscordChannelIDs    []string `yaml:"discord_channel_ids,omitempty"`
	DiscordThreadChannelIDs []string `yaml:"discord_thread_channel_ids,omitempty"`

	// LLM Providers (optional, synced to Memory Platform)
	GenerativeProvider *ProviderConfig `yaml:"generative_provider,omitempty"`
	EmbeddingProvider  *ProviderConfig `yaml:"embedding_provider,omitempty"`

	// Tool API Keys (stored in config, not in graph — used by MCP tools)
	BraveAPIKey string `yaml:"brave_api_key,omitempty"`

	// Relay Instance
	InstanceID string `yaml:"instance_id,omitempty"` // stable relay instance ID, auto-generated if empty

	// Bot behavior
	SystemPrompt string `yaml:"system_prompt,omitempty"`

	// Agent Definitions (synced to Memory Platform as AgentDefinitions)
	Agents map[string]*AgentConfig `yaml:"agents,omitempty"`
}

// IsMaster returns true if this node is the master (default, backward-compatible).
// Master runs the Discord bot, manages agent definitions, and syncs to MP.
func (p *ProjectConfig) IsMaster() bool {
	return p.Mode == "" || p.Mode == "master"
}

// IsSlave returns true if this node is a slave (MCP relay only).
// Slaves provide tools via the MCP relay but don't run Discord or manage agents.
func (p *ProjectConfig) IsSlave() bool {
	return p.Mode == "slave"
}

// ModeLabel returns a human-readable label for the node mode.
func (p *ProjectConfig) ModeLabel() string {
	if p == nil {
		return "unknown"
	}
	if p.IsMaster() {
		return "master 🏰"
	}
	return "slave 🔧"
}

// ProviderConfig holds credentials for a single LLM provider.
// Maps directly to Memory Platform's UpsertProviderConfigRequest.
type ProviderConfig struct {
	Provider string `yaml:"provider"` // google, google-vertex, openai-compatible, deepseek
	APIKey   string `yaml:"api_key,omitempty"`

	// Google Vertex AI only
	ServiceAccountJSON string `yaml:"service_account_json,omitempty"`
	GCPProject         string `yaml:"gcp_project,omitempty"`
	Location           string `yaml:"location,omitempty"`

	// OpenAI-compatible / DeepSeek
	BaseURL  string `yaml:"base_url,omitempty"`
	Model    string `yaml:"model,omitempty"` // generative model name (GeminiModel in MP API)
}

// AgentConfig defines an agent profile that maps to a Memory Platform AgentDefinition.
type AgentConfig struct {
	// Required
	Description string `yaml:"description,omitempty"`

	// System prompt (optional — the agent's core instruction)
	SystemPrompt string `yaml:"system_prompt,omitempty"`

	// Model config (optional — uses project default if omitted)
	Model *AgentModelConfig `yaml:"model,omitempty"`

	// Tools the agent can use (MCP tool names)
	Tools []string `yaml:"tools,omitempty"`

	// Skills the agent has loaded
	Skills []string `yaml:"skills,omitempty"`

	// Flow type: standard, acp, tool_use, auto
	FlowType string `yaml:"flow_type,omitempty"`

	// Visibility: project, org, private
	Visibility string `yaml:"visibility,omitempty"`

	// Dispatch mode: auto, manual
	DispatchMode string `yaml:"dispatch_mode,omitempty"`

	// Execution limits
	MaxSteps       int `yaml:"max_steps,omitempty"`
	DefaultTimeout int `yaml:"default_timeout,omitempty"`

	// Sandbox config (optional — injected into Config)
	Sandbox *SandboxConfig `yaml:"sandbox,omitempty"`

	// ACP metadata for agent discovery
	ACP *ACPConfig `yaml:"acp,omitempty"`

	// Delegation heuristics for orchestrator routing (optional).
	// When set, the orchestrator sees cost/speed/quality stats and routing rules.
	Delegation *DelegationHeuristics `yaml:"delegation,omitempty"`
}

// AgentModelConfig specifies the model for an agent.
type AgentModelConfig struct {
	Provider    string  `yaml:"provider,omitempty"`   // deepseek, google, etc.
	Name        string  `yaml:"name,omitempty"`       // model name
	Temperature float32 `yaml:"temperature,omitempty"`
	MaxTokens   int     `yaml:"max_tokens,omitempty"`
}

// SandboxConfig defines the execution environment for agent runs.
type SandboxConfig struct {
	Enabled    bool              `yaml:"enabled"`
	BaseImage  string            `yaml:"base_image,omitempty"`
	PullPolicy string            `yaml:"pull_policy,omitempty"` // always, missing
	Env        map[string]string `yaml:"env,omitempty"`
}

// DelegationHeuristics contains cost/speed/quality metadata for orchestrator routing.
// Inspired by oh-my-opencode-slim's agent description pattern.
// Populated for delegatable agents so the orchestrator can make informed routing decisions.
// The orchestrator uses this metadata plus the agent's actual Tools and Skills
// (from BuiltInAgent) to perform solution path selection.
type DelegationHeuristics struct {
	// Relative performance compared to the default orchestrator agent.
	// 1.0 = same, >1 = better, <1 = worse.
	SpeedMultiplier   float64 `json:"speedMultiplier,omitempty" yaml:"speedMultiplier,omitempty"`
	CostMultiplier    float64 `json:"costMultiplier,omitempty" yaml:"costMultiplier,omitempty"`
	QualityMultiplier float64 `json:"qualityMultiplier,omitempty" yaml:"qualityMultiplier,omitempty"`

	// Broad problem categories this agent is designed to handle.
	// The orchestrator uses these to match task requirements against agent specializations.
	CapabilityAreas []string `json:"capabilityAreas,omitempty" yaml:"capabilityAreas,omitempty"`

	// Conditions where delegation is beneficial (injected as bullet points).
	DelegateWhen []string `json:"delegateWhen,omitempty" yaml:"delegateWhen,omitempty"`

	// Conditions where delegation is wasteful (injected as bullet points).
	DontDelegateWhen []string `json:"dontDelegateWhen,omitempty" yaml:"dontDelegateWhen,omitempty"`

	// Quick rule-of-thumb for fast routing decisions.
	RuleOfThumb string `json:"ruleOfThumb,omitempty" yaml:"ruleOfThumb,omitempty"`
}

// ACPConfig contains Agent Card Protocol metadata for agent discovery.
type ACPConfig struct {
	DisplayName  string   `yaml:"display_name,omitempty"`
	Description  string   `yaml:"description,omitempty"`
	Capabilities []string `yaml:"capabilities,omitempty"`
	InputModes   []string `yaml:"input_modes,omitempty"`
	OutputModes  []string `yaml:"output_modes,omitempty"`
}

// DefaultConfig returns a fresh config with empty projects map.
func DefaultConfig() *Config {
	return &Config{
		Projects: make(map[string]*ProjectConfig),
	}
}

// Path returns the path to the config file.
// If the DIANE_CONFIG environment variable is set, it is used directly.
// Otherwise defaults to ~/.config/diane.yml.
func Path() string {
	if p := os.Getenv("DIANE_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(DefaultDir, "diane.yml")
}

// Load reads the config file. Returns a default config if the file doesn't exist.
func Load() (*Config, error) {
	p := Path()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("read config %s: %w", p, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", p, err)
	}
	if cfg.Projects == nil {
		cfg.Projects = make(map[string]*ProjectConfig)
	}
	return &cfg, nil
}

// Save writes the config file.
func (c *Config) Save() error {
	p := Path()
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(p, data, 0644); err != nil {
		return fmt.Errorf("write config %s: %w", p, err)
	}
	return nil
}

// Active returns the active project config, or nil if none configured.
func (c *Config) Active() *ProjectConfig {
	if c.Default == "" {
		// Pick the first configured project
		for _, p := range c.Projects {
			return p
		}
		return nil
	}
	return c.Projects[c.Default]
}

// AddProject adds a project config and sets it as default if it's the first.
func (c *Config) AddProject(name string, pc *ProjectConfig) {
	c.Projects[name] = pc
	if c.Default == "" {
		c.Default = name
	}
}

// RemoveProject removes a project by name.
func (c *Config) RemoveProject(name string) {
	delete(c.Projects, name)
	if c.Default == name {
		c.Default = ""
	}
}
