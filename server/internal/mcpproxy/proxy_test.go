package mcpproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =========================================================================
// ServerConfig and Config structs — parsing and validation
// =========================================================================

func TestLoadConfig_StdioServer(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{
		"servers": [
			{
				"name": "filesystem",
				"enabled": true,
				"type": "stdio",
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
				"env": {"ALLOWED_DIR": "/tmp"}
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Servers = %d, want 1", len(cfg.Servers))
	}

	s := cfg.Servers[0]
	if s.Name != "filesystem" {
		t.Errorf("Name = %q, want %q", s.Name, "filesystem")
	}
	if s.Enabled != true {
		t.Errorf("Enabled = %v, want true", s.Enabled)
	}
	if s.Type != "stdio" {
		t.Errorf("Type = %q, want %q", s.Type, "stdio")
	}
	if s.Command != "npx" {
		t.Errorf("Command = %q, want %q", s.Command, "npx")
	}
	if len(s.Args) != 3 || s.Args[0] != "-y" || s.Args[1] != "@modelcontextprotocol/server-filesystem" {
		t.Errorf("Args = %v, want [\"-y\" \"@modelcontextprotocol/server-filesystem\" \"/tmp\"]", s.Args)
	}
	if s.Env["ALLOWED_DIR"] != "/tmp" {
		t.Errorf("Env = %v, want {ALLOWED_DIR: /tmp}", s.Env)
	}

	t.Logf("✅ Loaded stdio server: %s (type=%s, command=%s)", s.Name, s.Type, s.Command)
}

func TestLoadConfig_RemoteServer(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{
		"servers": [
			{
				"name": "remote-api",
				"enabled": true,
				"type": "http",
				"command": "",
				"args": [],
				"env": {
					"API_KEY": "sk-test123"
				}
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Servers = %d, want 1", len(cfg.Servers))
	}

	s := cfg.Servers[0]
	if s.Name != "remote-api" {
		t.Errorf("Name = %q, want %q", s.Name, "remote-api")
	}
	if s.Enabled != true {
		t.Errorf("Enabled = %v, want true", s.Enabled)
	}
	if s.Type != "http" {
		t.Errorf("Type = %q, want %q", s.Type, "http")
	}

	t.Logf("✅ Loaded remote server: %s (type=%s)", s.Name, s.Type)
}

func TestLoadConfig_MultipleServers(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{
		"servers": [
			{
				"name": "time",
				"enabled": true,
				"type": "stdio",
				"command": "uvx",
				"args": ["mcp-server-time"]
			},
			{
				"name": "github",
				"enabled": false,
				"type": "stdio",
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-github"],
				"env": {"GITHUB_TOKEN": "ghp_test"}
			},
			{
				"name": "internal-api",
				"enabled": true,
				"type": "http",
				"command": ""
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if len(cfg.Servers) != 3 {
		t.Fatalf("Servers = %d, want 3", len(cfg.Servers))
	}

	// Verify names
	names := make([]string, len(cfg.Servers))
	for i, s := range cfg.Servers {
		names[i] = s.Name
	}
	expected := []string{"time", "github", "internal-api"}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("Server[%d].Name = %q, want %q", i, n, expected[i])
		}
	}

	// Verify distinct types
	types := map[string]bool{}
	for _, s := range cfg.Servers {
		types[s.Type] = true
	}
	if !types["stdio"] {
		t.Error("Expected at least one stdio server")
	}
	if !types["http"] {
		t.Error("Expected at least one http server")
	}

	t.Logf("✅ Loaded %d servers with types: stdio=%v, http=%v", len(cfg.Servers), types["stdio"], types["http"])
}

func TestLoadConfig_DisabledServer(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{
		"servers": [
			{
				"name": "disabled-server",
				"enabled": false,
				"type": "stdio",
				"command": "npx",
				"args": ["-y", "some-package"]
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Servers = %d, want 1", len(cfg.Servers))
	}

	if cfg.Servers[0].Enabled != false {
		t.Errorf("Enabled = %v, want false", cfg.Servers[0].Enabled)
	}

	t.Log("✅ Disabled server parsed correctly")
}

func TestLoadConfig_EmptyServersList(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{"servers": []}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if len(cfg.Servers) != 0 {
		t.Errorf("Servers = %d, want 0", len(cfg.Servers))
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "does-not-exist.json")

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("LoadConfig should return error for missing file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("Expected os.IsNotExist error, got: %v", err)
	}
	t.Logf("✅ Missing file returns: %v", err)
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	if err := os.WriteFile(configPath, []byte(`{invalid json}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("LoadConfig should return error for invalid JSON")
	}
	t.Logf("✅ Invalid JSON returns: %v", err)
}

func TestLoadConfig_MinimalServer(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	// A server with only name and type — minimal fields
	configJSON := `{
		"servers": [
			{
				"name": "minimal",
				"enabled": false,
				"type": "stdio",
				"command": ""
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Servers = %d, want 1", len(cfg.Servers))
	}

	s := cfg.Servers[0]
	if s.Name != "minimal" {
		t.Errorf("Name = %q, want %q", s.Name, "minimal")
	}
	if s.Command != "" {
		t.Errorf("Command = %q, want empty", s.Command)
	}
}

// =========================================================================
// GetDefaultConfigPath
// =========================================================================

func TestGetDefaultConfigPath(t *testing.T) {
	path := GetDefaultConfigPath()
	if path == "" {
		t.Fatal("GetDefaultConfigPath returned empty path")
	}
	if !strings.HasSuffix(path, ".diane/mcp-servers.json") {
		t.Errorf("Path = %q, want suffix '.diane/mcp-servers.json'", path)
	}
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "~") {
		// On most systems home dir is absolute
		t.Logf("Default config path: %s", path)
	}
}

// =========================================================================
// Proxy initialization with config
// =========================================================================

func TestNewProxy_WithValidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	// A config with only disabled servers — should start without error
	// but not launch any actual subprocesses
	configJSON := `{
		"servers": [
			{
				"name": "disabled-test",
				"enabled": false,
				"type": "stdio",
				"command": "nonexistent"
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	proxy, err := NewProxy(configPath)
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

func TestNewProxy_MissingConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "does-not-exist.json")

	_, err := NewProxy(configPath)
	if err == nil {
		t.Fatal("NewProxy should return error for missing config file")
	}
	t.Logf("✅ Missing config returns: %v", err)
}

func TestNewProxy_InvalidConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	if err := os.WriteFile(configPath, []byte(`not json`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := NewProxy(configPath)
	if err == nil {
		t.Fatal("NewProxy should return error for invalid config")
	}
	t.Logf("✅ Invalid config returns: %v", err)
}

// =========================================================================
// Config with sensitive data — env var values
// =========================================================================

func TestLoadConfig_WithSensitiveEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	// Config with API keys in env
	configJSON := `{
		"servers": [
			{
				"name": "github-api",
				"enabled": true,
				"type": "stdio",
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-github"],
				"env": {
					"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_xxxxxxxxxxxxxxxxxxxx",
					"API_KEY": "sk-test-key-12345"
				}
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	s := cfg.Servers[0]
	if s.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] != "ghp_xxxxxxxxxxxxxxxxxxxx" {
		t.Error("GITHUB_TOKEN env not preserved")
	}
	if s.Env["API_KEY"] != "sk-test-key-12345" {
		t.Error("API_KEY env not preserved")
	}

	t.Log("✅ Sensitive env vars loaded correctly")
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

// =========================================================================
// Config struct default values
// =========================================================================

// =========================================================================
// ServerConfig with URL, Headers, and OAuth
// =========================================================================

func TestLoadConfig_WithURLAndHeaders(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{
		"servers": [
			{
				"name": "remote-api",
				"enabled": true,
				"type": "streamable-http",
				"url": "https://api.example.com/mcp",
				"headers": {
					"X-API-Key": "test-key-123"
				}
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Servers = %d, want 1", len(cfg.Servers))
	}

	s := cfg.Servers[0]
	if s.Name != "remote-api" {
		t.Errorf("Name = %q, want %q", s.Name, "remote-api")
	}
	if s.Enabled != true {
		t.Errorf("Enabled = %v, want true", s.Enabled)
	}
	if s.Type != "streamable-http" {
		t.Errorf("Type = %q, want %q", s.Type, "streamable-http")
	}
	if s.URL != "https://api.example.com/mcp" {
		t.Errorf("URL = %q, want %q", s.URL, "https://api.example.com/mcp")
	}
	if s.Headers == nil {
		t.Fatal("Headers is nil")
	}
	if s.Headers["X-API-Key"] != "test-key-123" {
		t.Errorf("Headers[X-API-Key] = %q, want %q", s.Headers["X-API-Key"], "test-key-123")
	}

	t.Logf("✅ Loaded remote API server with URL and headers: %s", s.Name)
}

func TestLoadConfig_WithOAuthDeviceFlow(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{
		"servers": [
			{
				"name": "github",
				"enabled": true,
				"type": "streamable-http",
				"url": "https://api.github.com/mcp",
				"oauth": {
					"client_id": "Iv23li1234567890abcdef",
					"device_auth_url": "https://github.com/login/device/code",
					"token_url": "https://github.com/login/oauth/access_token",
					"scopes": ["repo", "user"]
				}
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Servers = %d, want 1", len(cfg.Servers))
	}

	s := cfg.Servers[0]
	if s.Name != "github" {
		t.Errorf("Name = %q, want %q", s.Name, "github")
	}
	if s.Type != "streamable-http" {
		t.Errorf("Type = %q, want %q", s.Type, "streamable-http")
	}
	if s.URL != "https://api.github.com/mcp" {
		t.Errorf("URL = %q, want %q", s.URL, "https://api.github.com/mcp")
	}

	// Verify OAuth fields
	if s.OAuth == nil {
		t.Fatal("OAuth is nil")
	}
	if s.OAuth.ClientID != "Iv23li1234567890abcdef" {
		t.Errorf("OAuth.ClientID = %q, want %q", s.OAuth.ClientID, "Iv23li1234567890abcdef")
	}
	if s.OAuth.DeviceAuthURL != "https://github.com/login/device/code" {
		t.Errorf("OAuth.DeviceAuthURL = %q, want %q", s.OAuth.DeviceAuthURL, "https://github.com/login/device/code")
	}
	if s.OAuth.TokenURL != "https://github.com/login/oauth/access_token" {
		t.Errorf("OAuth.TokenURL = %q, want %q", s.OAuth.TokenURL, "https://github.com/login/oauth/access_token")
	}
	if len(s.OAuth.Scopes) != 2 {
		t.Fatalf("OAuth.Scopes = %v, want [repo user]", s.OAuth.Scopes)
	}
	if s.OAuth.Scopes[0] != "repo" || s.OAuth.Scopes[1] != "user" {
		t.Errorf("OAuth.Scopes = %v, want [repo user]", s.OAuth.Scopes)
	}
	// BearerToken should be empty (not set in device flow)
	if s.OAuth.BearerToken != "" {
		t.Errorf("OAuth.BearerToken = %q, want empty", s.OAuth.BearerToken)
	}

	t.Logf("✅ Loaded GitHub OAuth device flow config: client_id=%s", s.OAuth.ClientID[:12])
}

func TestLoadConfig_WithOAuthPKCE(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{
		"servers": [
			{
				"name": "infakt",
				"enabled": true,
				"type": "streamable-http",
				"url": "https://api.infakt.pl/mcp",
				"oauth": {
					"client_id": "infakt-client-abc123",
					"authorization_url": "https://app.infakt.pl/oauth/authorize",
					"token_url": "https://app.infakt.pl/oauth/token",
					"scopes": ["invoices", "clients"]
				}
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("Servers = %d, want 1", len(cfg.Servers))
	}

	s := cfg.Servers[0]
	if s.Name != "infakt" {
		t.Errorf("Name = %q, want %q", s.Name, "infakt")
	}
	if s.Type != "streamable-http" {
		t.Errorf("Type = %q, want %q", s.Type, "streamable-http")
	}
	if s.URL != "https://api.infakt.pl/mcp" {
		t.Errorf("URL = %q, want %q", s.URL, "https://api.infakt.pl/mcp")
	}

	// Verify OAuth fields
	if s.OAuth == nil {
		t.Fatal("OAuth is nil")
	}
	if s.OAuth.ClientID != "infakt-client-abc123" {
		t.Errorf("OAuth.ClientID = %q, want %q", s.OAuth.ClientID, "infakt-client-abc123")
	}
	if s.OAuth.AuthorizationURL != "https://app.infakt.pl/oauth/authorize" {
		t.Errorf("OAuth.AuthorizationURL = %q, want %q", s.OAuth.AuthorizationURL, "https://app.infakt.pl/oauth/authorize")
	}
	if s.OAuth.TokenURL != "https://app.infakt.pl/oauth/token" {
		t.Errorf("OAuth.TokenURL = %q, want %q", s.OAuth.TokenURL, "https://app.infakt.pl/oauth/token")
	}
	if len(s.OAuth.Scopes) != 2 {
		t.Fatalf("OAuth.Scopes = %v, want [invoices clients]", s.OAuth.Scopes)
	}
	if s.OAuth.Scopes[0] != "invoices" || s.OAuth.Scopes[1] != "clients" {
		t.Errorf("OAuth.Scopes = %v, want [invoices clients]", s.OAuth.Scopes)
	}
	// BearerToken should be empty (not set in PKCE flow)
	if s.OAuth.BearerToken != "" {
		t.Errorf("OAuth.BearerToken = %q, want empty", s.OAuth.BearerToken)
	}

	t.Logf("✅ Loaded infakt OAuth PKCE config: client_id=%s", s.OAuth.ClientID)
}

func TestServerConfigDefaults(t *testing.T) {
	// When loading JSON, empty fields should get zero values
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{
		"servers": [
			{
				"name": "no-args"
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	s := cfg.Servers[0]
	if s.Name != "no-args" {
		t.Errorf("Name = %q", s.Name)
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
}
