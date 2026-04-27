// Package config_test tests the config package — YAML parsing, project management,
// mode detection, and path resolution.
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Emergent-Comapny/diane/internal/config"
)

// =========================================================================
// Path Resolution
// =========================================================================

func TestPathDefault(t *testing.T) {
	// PATH is not set, should use home + .config
	p := config.Path()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "diane.yml")
	if p != expected {
		t.Errorf("Path() = %q, want %q", p, expected)
	}
}

func TestPathWithEnvVar(t *testing.T) {
	// Set DIANE_CONFIG to custom path
	t.Setenv("DIANE_CONFIG", "/tmp/test-diane-config.yml")
	p := config.Path()
	if p != "/tmp/test-diane-config.yml" {
		t.Errorf("Path() = %q, want /tmp/test-diane-config.yml", p)
	}
}

// =========================================================================
// DefaultConfig
// =========================================================================

func TestDefaultConfig(t *testing.T) {
	c := config.DefaultConfig()
	if c == nil {
		t.Fatal("DefaultConfig() returned nil")
	}
	if c.Projects == nil {
		t.Error("DefaultConfig().Projects is nil — expected empty map")
	}
	if len(c.Projects) != 0 {
		t.Errorf("DefaultConfig().Projects has %d entries, want 0", len(c.Projects))
	}
	if c.Default != "" {
		t.Errorf("DefaultConfig().Default = %q, want empty", c.Default)
	}
}

// =========================================================================
// Load / Save
// =========================================================================

func TestLoadMissingFile(t *testing.T) {
	// Point to a non-existent file
	dir := t.TempDir()
	p := filepath.Join(dir, "does-not-exist.yml")
	t.Setenv("DIANE_CONFIG", p)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() on missing file: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil on missing file")
	}
	if cfg.Projects == nil {
		t.Error("Projects map is nil after Load() of missing file")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "diane.yml")
	t.Setenv("DIANE_CONFIG", p)

	// Create a config with two projects
	cfg := config.DefaultConfig()
	cfg.AddProject("default", &config.ProjectConfig{
		ServerURL: "https://memory.example.com",
		Token:     "emt_test_token_123",
		ProjectID: "proj-123-uuid",
		Mode:      "master",
	})
	cfg.AddProject("secondary", &config.ProjectConfig{
		ServerURL: "https://memory2.example.com",
		Token:     "emt_test_token_456",
		ProjectID: "proj-456-uuid",
		Mode:      "slave",
	})

	// Save
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	// Check file exists
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("Config file not created: %v", err)
	}

	// Load back
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load() after save: %v", err)
	}

	// Verify content
	if len(loaded.Projects) != 2 {
		t.Errorf("Loaded %d projects, want 2", len(loaded.Projects))
	}

	pc, ok := loaded.Projects["default"]
	if !ok {
		t.Fatal("Loaded config missing 'default' project")
	}
	if pc.ServerURL != "https://memory.example.com" {
		t.Errorf("ServerURL = %q, want https://memory.example.com", pc.ServerURL)
	}
	if pc.Token != "emt_test_token_123" {
		t.Errorf("Token = %q, want emt_test_token_123", pc.Token)
	}
	if pc.ProjectID != "proj-123-uuid" {
		t.Errorf("ProjectID = %q, want proj-123-uuid", pc.ProjectID)
	}
	if !pc.IsMaster() {
		t.Error("default project should be master")
	}

	// Check secondary project
	pc2, ok := loaded.Projects["secondary"]
	if !ok {
		t.Fatal("Loaded config missing 'secondary' project")
	}
	if !pc2.IsSlave() {
		t.Error("secondary project should be slave")
	}

	// Check default was set
	if loaded.Default != "default" {
		t.Errorf("Default = %q, want 'default'", loaded.Default)
	}
}

// =========================================================================
// Active Project Selection
// =========================================================================

func TestActiveNoDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Projects["alpha"] = &config.ProjectConfig{ServerURL: "https://alpha.com", Token: "tok1", ProjectID: "p1"}
	cfg.Projects["beta"] = &config.ProjectConfig{ServerURL: "https://beta.com", Token: "tok2", ProjectID: "p2"}

	// No default set — should return first project (map iteration)
	active := cfg.Active()
	if active == nil {
		t.Fatal("Active() returned nil when projects exist")
	}
}

func TestActiveWithDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Projects["alpha"] = &config.ProjectConfig{ServerURL: "https://alpha.com", Token: "tok1", ProjectID: "p1"}
	cfg.Projects["beta"] = &config.ProjectConfig{ServerURL: "https://beta.com", Token: "tok2", ProjectID: "p2"}
	cfg.Default = "beta"

	active := cfg.Active()
	if active == nil {
		t.Fatal("Active() returned nil")
	}
	if active.Token != "tok2" {
		t.Errorf("Active() returned token %q, want 'tok2'", active.Token)
	}
}

func TestActiveNoProjects(t *testing.T) {
	cfg := config.DefaultConfig()
	active := cfg.Active()
	if active != nil {
		t.Error("Active() should return nil when no projects configured")
	}
}

// =========================================================================
// AddProject / RemoveProject
// =========================================================================

func TestAddProjectSetsDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddProject("first", &config.ProjectConfig{ServerURL: "https://first.com", Token: "t1", ProjectID: "p1"})
	if cfg.Default != "first" {
		t.Errorf("Default = %q, want 'first'", cfg.Default)
	}
}

func TestAddProjectDoesNotOverrideDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddProject("first", &config.ProjectConfig{ServerURL: "https://first.com", Token: "t1", ProjectID: "p1"})
	cfg.AddProject("second", &config.ProjectConfig{ServerURL: "https://second.com", Token: "t2", ProjectID: "p2"})
	if cfg.Default != "first" {
		t.Errorf("Default = %q, want 'first' (should not be overridden)", cfg.Default)
	}
}

func TestRemoveProject(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddProject("alpha", &config.ProjectConfig{Token: "t1", ProjectID: "p1"})
	cfg.AddProject("beta", &config.ProjectConfig{Token: "t2", ProjectID: "p2"})

	cfg.RemoveProject("beta")
	if _, ok := cfg.Projects["beta"]; ok {
		t.Error("beta project should be removed")
	}
	if len(cfg.Projects) != 1 {
		t.Errorf("Projects has %d entries after removal, want 1", len(cfg.Projects))
	}
	// Default should remain 'alpha'
	if cfg.Default != "alpha" {
		t.Errorf("Default = %q, want 'alpha'", cfg.Default)
	}
}

func TestRemoveDefaultProject(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AddProject("only", &config.ProjectConfig{Token: "t1", ProjectID: "p1"})
	cfg.RemoveProject("only")
	if cfg.Default != "" {
		t.Errorf("Default should be empty after removing the only project, got %q", cfg.Default)
	}
}

// =========================================================================
// Mode Detection
// =========================================================================

func TestIsMaster(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"", true},     // empty = master (backward compat)
		{"master", true},
		{"slave", false},
	}
	for _, tt := range tests {
		pc := &config.ProjectConfig{Mode: tt.mode}
		got := pc.IsMaster()
		if got != tt.want {
			t.Errorf("IsMaster(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestIsSlave(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"", false},
		{"master", false},
		{"slave", true},
	}
	for _, tt := range tests {
		pc := &config.ProjectConfig{Mode: tt.mode}
		got := pc.IsSlave()
		if got != tt.want {
			t.Errorf("IsSlave(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestModeLabel(t *testing.T) {
	tests := []struct {
		pc   *config.ProjectConfig
		want string
	}{
		{nil, "unknown"},
		{&config.ProjectConfig{Mode: ""}, "master"},
		{&config.ProjectConfig{Mode: "master"}, "master"},
		{&config.ProjectConfig{Mode: "slave"}, "slave"},
	}
	for _, tt := range tests {
		label := tt.pc.ModeLabel()
		if tt.want == "master" && label != "master 🏰" {
			t.Errorf("ModeLabel(master) = %q, want 'master 🏰'", label)
		}
		if tt.want == "slave" && label != "slave 🔧" {
			t.Errorf("ModeLabel(slave) = %q, want 'slave 🔧'", label)
		}
		if tt.pc == nil && label != tt.want {
			t.Errorf("ModeLabel(nil) = %q, want %q", label, tt.want)
		}
	}
}

// =========================================================================
// ProviderConfig
// =========================================================================

func TestProviderConfigDefaults(t *testing.T) {
	pc := &config.ProviderConfig{}
	if pc.Provider != "" {
		t.Errorf("Default Provider = %q, want empty", pc.Provider)
	}
}

func TestProviderConfigFull(t *testing.T) {
	pc := &config.ProviderConfig{
		Provider: "deepseek",
		APIKey:   "sk-test-key",
		BaseURL:  "https://api.deepseek.com",
		Model:    "deepseek-v4-flash",
	}
	if pc.Provider != "deepseek" {
		t.Errorf("Provider = %q", pc.Provider)
	}
	if pc.APIKey != "sk-test-key" {
		t.Errorf("APIKey = %q", pc.APIKey)
	}
}

// =========================================================================
// AgentConfig
// =========================================================================

func TestAgentConfigDefaults(t *testing.T) {
	ac := &config.AgentConfig{}
	if ac.MaxSteps != 0 {
		t.Errorf("Default MaxSteps = %d, want 0", ac.MaxSteps)
	}
	if ac.DispatchMode != "" {
		t.Errorf("Default DispatchMode = %q, want empty", ac.DispatchMode)
	}
}

func TestAgentConfigFull(t *testing.T) {
	ac := &config.AgentConfig{
		Description: "Test agent",
		Tools:       []string{"search-hybrid", "web-search-brave"},
		Skills:      []string{"diane-coding"},
		MaxSteps:    25,
	}
	if len(ac.Tools) != 2 {
		t.Errorf("Tools count = %d, want 2", len(ac.Tools))
	}
	if len(ac.Skills) != 1 {
		t.Errorf("Skills count = %d, want 1", len(ac.Skills))
	}
}

// =========================================================================
// DelegationHeuristics
// =========================================================================

func TestDelegationHeuristicsDefaults(t *testing.T) {
	d := &config.DelegationHeuristics{}
	if d.SpeedMultiplier != 0 {
		t.Errorf("Default SpeedMultiplier = %f, want 0", d.SpeedMultiplier)
	}
	if len(d.DelegateWhen) != 0 {
		t.Errorf("Default DelegateWhen has %d entries, want 0", len(d.DelegateWhen))
	}
}

func TestDelegationHeuristicsFull(t *testing.T) {
	d := &config.DelegationHeuristics{
		SpeedMultiplier:   2.0,
		CostMultiplier:    1.0,
		QualityMultiplier: 3.0,
		CapabilityAreas:   []string{"Web research", "Data analysis"},
		DelegateWhen:      []string{"Multi-source research", "Complex queries"},
		DontDelegateWhen:  []string{"Simple lookups"},
		RuleOfThumb:       "Deep research → delegate",
	}
	if d.SpeedMultiplier != 2.0 {
		t.Errorf("SpeedMultiplier = %f", d.SpeedMultiplier)
	}
	if len(d.CapabilityAreas) != 2 {
		t.Errorf("CapabilityAreas = %d, want 2", len(d.CapabilityAreas))
	}
}

// =========================================================================
// SandboxConfig
// =========================================================================

func TestSandboxConfigDefaults(t *testing.T) {
	s := &config.SandboxConfig{}
	if s.Enabled {
		t.Error("Default Sandbox.Enabled should be false")
	}
	if s.BaseImage != "" {
		t.Errorf("Default BaseImage = %q", s.BaseImage)
	}
}

func TestSandboxConfigFull(t *testing.T) {
	s := &config.SandboxConfig{
		Enabled:   true,
		BaseImage: "python:3.12",
		Env:       map[string]string{"PATH": "/usr/bin"},
	}
	if !s.Enabled {
		t.Error("Enabled should be true")
	}
	if s.BaseImage != "python:3.12" {
		t.Errorf("BaseImage = %q", s.BaseImage)
	}
}

// =========================================================================
// ACPConfig
// =========================================================================

func TestACPConfig(t *testing.T) {
	acp := &config.ACPConfig{
		DisplayName:  "Test Agent",
		Description:  "An agent for testing",
		Capabilities: []string{"code", "search"},
		InputModes:   []string{"text"},
		OutputModes:  []string{"text"},
	}
	if acp.DisplayName != "Test Agent" {
		t.Errorf("DisplayName = %q", acp.DisplayName)
	}
	if len(acp.Capabilities) != 2 {
		t.Errorf("Capabilities count = %d", len(acp.Capabilities))
	}
}

// =========================================================================
// Complex Config (full YAML round-trip with nested structs)
// =========================================================================

func TestComplexConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "diane.yml")
	t.Setenv("DIANE_CONFIG", p)

	// Build a complex config
	cfg := config.DefaultConfig()
	cfg.AddProject("prod", &config.ProjectConfig{
		ServerURL:          "https://memory.prod.com",
		Token:              "emt_prod_token",
		ProjectID:          "prodid-123",
		Mode:               "master",
		DiscordBotToken:    "discord_token_here",
		DiscordChannelIDs:  []string{"123", "456"},
		SystemPrompt:       "You are a test agent.",
		BraveAPIKey:        "brave_key_here",
		InstanceID:         "inst-001",
		GenerativeProvider: &config.ProviderConfig{
			Provider: "deepseek",
			APIKey:   "sk-deepseek",
			Model:    "deepseek-v4-flash",
		},
		EmbeddingProvider: &config.ProviderConfig{
			Provider: "deepseek",
			APIKey:   "sk-embedding",
		},
		Agents: map[string]*config.AgentConfig{
			"test-agent": {
				Description: "Test agent",
				Tools:       []string{"search-hybrid"},
				MaxSteps:    25,
			},
		},
	})

	// Save
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	// Load back
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}

	pc, ok := loaded.Projects["prod"]
	if !ok {
		t.Fatal("Missing 'prod' project after round-trip")
	}

	// Verify all fields survived
	if pc.ServerURL != "https://memory.prod.com" {
		t.Errorf("ServerURL = %q", pc.ServerURL)
	}
	if pc.Token != "emt_prod_token" {
		t.Errorf("Token = %q", pc.Token)
	}
	if pc.ProjectID != "prodid-123" {
		t.Errorf("ProjectID = %q", pc.ProjectID)
	}
	if pc.Mode != "master" {
		t.Errorf("Mode = %q", pc.Mode)
	}
	if pc.DiscordBotToken != "discord_token_here" {
		t.Errorf("DiscordBotToken = %q", pc.DiscordBotToken)
	}
	if len(pc.DiscordChannelIDs) != 2 {
		t.Errorf("DiscordChannelIDs has %d, want 2", len(pc.DiscordChannelIDs))
	}
	if pc.BraveAPIKey != "brave_key_here" {
		t.Errorf("BraveAPIKey = %q", pc.BraveAPIKey)
	}
	if pc.InstanceID != "inst-001" {
		t.Errorf("InstanceID = %q", pc.InstanceID)
	}
	if pc.GenerativeProvider == nil {
		t.Fatal("GenerativeProvider is nil")
	}
	if pc.GenerativeProvider.Provider != "deepseek" {
		t.Errorf("GenerativeProvider.Provider = %q", pc.GenerativeProvider.Provider)
	}
	if pc.EmbeddingProvider == nil {
		t.Fatal("EmbeddingProvider is nil")
	}
	if pc.Agents == nil || len(pc.Agents) != 1 {
		t.Errorf("Agents count = %d, want 1", len(pc.Agents))
	}
	if pc.SystemPrompt != "You are a test agent." {
		t.Errorf("SystemPrompt = %q", pc.SystemPrompt)
	}

	t.Log("✅ Complex config round-tripped all fields")
}
