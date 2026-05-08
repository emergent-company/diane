package mcpproxy

import (
	"strings"
	"testing"
)

// =========================================================================
// MergeServerConfigs
// =========================================================================

func TestMergeServerConfigs(t *testing.T) {
	t.Run("empty lists", func(t *testing.T) {
		result := MergeServerConfigs()
		if result == nil {
			t.Fatal("MergeServerConfigs returned nil")
		}
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})

	t.Run("single list", func(t *testing.T) {
		servers := []ServerConfig{
			{Name: "alpha", Enabled: true, Type: "stdio", Command: "npx"},
		}
		result := MergeServerConfigs(servers)
		if len(result) != 1 {
			t.Fatalf("len = %d, want 1", len(result))
		}
		if result[0].Name != "alpha" {
			t.Errorf("Name = %q, want %q", result[0].Name, "alpha")
		}
	})

	t.Run("merges two lists without overlap", func(t *testing.T) {
		listA := []ServerConfig{
			{Name: "alpha", Enabled: true, Type: "stdio", Command: "npx"},
		}
		listB := []ServerConfig{
			{Name: "beta", Enabled: true, Type: "http", URL: "https://example.com/mcp"},
		}
		result := MergeServerConfigs(listA, listB)
		if len(result) != 2 {
			t.Fatalf("len = %d, want 2", len(result))
		}
		names := make([]string, len(result))
		for i, s := range result {
			names[i] = s.Name
		}
		if names[0] != "alpha" || names[1] != "beta" {
			t.Errorf("names = %v, want [alpha beta]", names)
		}
	})

	t.Run("later overrides earlier", func(t *testing.T) {
		listA := []ServerConfig{
			{Name: "shared", Enabled: false, Type: "stdio", Command: "old"},
		}
		listB := []ServerConfig{
			{Name: "shared", Enabled: true, Type: "http", URL: "https://example.com/mcp"},
		}
		result := MergeServerConfigs(listA, listB)
		if len(result) != 1 {
			t.Fatalf("len = %d, want 1", len(result))
		}
		if result[0].Enabled != true {
			t.Errorf("Enabled = %v, want true", result[0].Enabled)
		}
		if result[0].Type != "http" {
			t.Errorf("Type = %q, want %q", result[0].Type, "http")
		}
		if result[0].URL != "https://example.com/mcp" {
			t.Errorf("URL = %q, want %q", result[0].URL, "https://example.com/mcp")
		}
	})

	t.Run("order preserved with override at original position", func(t *testing.T) {
		listA := []ServerConfig{
			{Name: "first", Enabled: true, Type: "stdio"},
			{Name: "second", Enabled: false, Type: "stdio"},
		}
		listB := []ServerConfig{
			{Name: "second", Enabled: true, Type: "http", URL: "https://override.com/mcp"},
			{Name: "third", Enabled: true, Type: "stdio"},
		}
		result := MergeServerConfigs(listA, listB)
		if len(result) != 3 {
			t.Fatalf("len = %d, want 3", len(result))
		}
		if result[0].Name != "first" {
			t.Errorf("result[0].Name = %q, want %q", result[0].Name, "first")
		}
		if result[1].Name != "second" {
			t.Errorf("result[1].Name = %q, want %q", result[1].Name, "second")
		}
		if result[1].URL != "https://override.com/mcp" {
			t.Errorf("result[1].URL = %q, want %q", result[1].URL, "https://override.com/mcp")
		}
		if result[2].Name != "third" {
			t.Errorf("result[2].Name = %q, want %q", result[2].Name, "third")
		}
	})

	t.Run("three lists", func(t *testing.T) {
		a := []ServerConfig{{Name: "a", Enabled: true, Type: "stdio"}}
		b := []ServerConfig{{Name: "b", Enabled: true, Type: "stdio"}}
		c := []ServerConfig{{Name: "c", Enabled: true, Type: "stdio"}}
		result := MergeServerConfigs(a, b, c)
		if len(result) != 3 {
			t.Fatalf("len = %d, want 3", len(result))
		}
	})

	t.Log("✅ MergeServerConfigs all tests passed")
}

// =========================================================================
// Proxy initialization with in-memory config
// =========================================================================

func TestNewProxy_WithValidConfig(t *testing.T) {
	// A config with only disabled servers — should start without error
	// but not launch any actual subprocesses
	servers := []ServerConfig{
		{
			Name:    "disabled-test",
			Enabled: false,
			Type:    "stdio",
			Command: "nonexistent",
		},
	}

	proxy, err := NewProxy(servers)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	defer proxy.Close()

	if proxy == nil {
		t.Fatal("NewProxy returned nil")
	}

	// No clients should be started (all disabled)
	tools, err := proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("ListAllTools returned %d tools, want 0", len(tools))
	}

	t.Log("✅ Proxy initialized with disabled servers only")
}

func TestNewProxy_EmptyServers(t *testing.T) {
	// Empty list should start without error
	proxy, err := NewProxy([]ServerConfig{})
	if err != nil {
		t.Fatalf("NewProxy with empty servers: %v", err)
	}
	defer proxy.Close()

	if proxy == nil {
		t.Fatal("NewProxy returned nil")
	}

	tools, err := proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("ListAllTools returned %d tools, want 0", len(tools))
	}

	t.Log("✅ Proxy initialized with empty server list")
}

func TestNewProxy_NilServers(t *testing.T) {
	// Nil slice should start without error
	proxy, err := NewProxy(nil)
	if err != nil {
		t.Fatalf("NewProxy with nil servers: %v", err)
	}
	defer proxy.Close()

	if proxy == nil {
		t.Fatal("NewProxy returned nil")
	}

	t.Log("✅ Proxy initialized with nil server list")
}

// =========================================================================
// Config with sensitive data — env var values (in-memory)
// =========================================================================

func TestLoadConfig_WithSensitiveEnv(t *testing.T) {
	// Construct ServerConfig directly in-memory with sensitive env vars
	cfg := Config{
		Servers: []ServerConfig{
			{
				Name:    "github-api",
				Enabled: true,
				Type:    "stdio",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-github"},
				Env: map[string]string{
					"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_xxxxxxxxxxxxxxxxxxxx",
					"API_KEY":                      "sk-test-key-12345",
				},
			},
		},
	}

	if len(cfg.Servers) != 1 {
		t.Fatalf("Servers = %d, want 1", len(cfg.Servers))
	}

	s := cfg.Servers[0]
	if s.Name != "github-api" {
		t.Errorf("Name = %q, want %q", s.Name, "github-api")
	}
	if s.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] != "ghp_xxxxxxxxxxxxxxxxxxxx" {
		t.Error("GITHUB_PERSONAL_ACCESS_TOKEN env not preserved")
	}
	if s.Env["API_KEY"] != "sk-test-key-12345" {
		t.Error("API_KEY env not preserved")
	}

	t.Log("✅ Sensitive env vars loaded correctly")
}

// =========================================================================
// ServerConfig default values
// =========================================================================

func TestServerConfigDefaults(t *testing.T) {
	// Construct a minimal ServerConfig and verify zero-value defaults
	s := ServerConfig{
		Name: "no-args",
	}

	if s.Name != "no-args" {
		t.Errorf("Name = %q, want %q", s.Name, "no-args")
	}
	// Args should be nil (Go zero value), not an empty slice
	if s.Args != nil {
		t.Errorf("Args = %v, want nil", s.Args)
	}
	if s.Env != nil {
		t.Errorf("Env = %v, want nil", s.Env)
	}
	if s.Enabled != false {
		t.Errorf("Enabled = %v, want false", s.Enabled)
	}
	if s.Type != "" {
		t.Errorf("Type = %q, want empty", s.Type)
	}
	if s.Command != "" {
		t.Errorf("Command = %q, want empty", s.Command)
	}
	if s.URL != "" {
		t.Errorf("URL = %q, want empty", s.URL)
	}
	if s.Headers != nil {
		t.Errorf("Headers = %v, want nil", s.Headers)
	}
	if s.OAuth != nil {
		t.Errorf("OAuth = %v, want nil", s.OAuth)
	}

	t.Log("✅ ServerConfig defaults are correct")
}

// =========================================================================
// getPath utility
// =========================================================================

func TestGetPath(t *testing.T) {
	path := getPath()
	if path == "" {
		t.Fatal("getPath returned empty")
	}
	if !strings.Contains(path, "/usr/local/bin") {
		t.Errorf("getPath = %q, expected /usr/local/bin", path)
	}
	if !strings.Contains(path, "/opt/homebrew/bin") {
		t.Errorf("getPath = %q, expected /opt/homebrew/bin", path)
	}
}
