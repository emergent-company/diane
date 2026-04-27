package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =========================================================================
// Helper function tests
// =========================================================================

func TestPlural(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{100, "s"},
	}
	for _, tt := range tests {
		got := plural(tt.n)
		if got != tt.want {
			t.Errorf("plural(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestShortenHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	tests := []struct {
		path string
	}{
		{filepath.Join(home, ".diane", "mcp-servers.json")},
		{filepath.Join(home, ".config", "diane.yml")},
		{"/etc/some/file"},
		{"/tmp/test"},
	}
	for _, tt := range tests {
		short := shortenHome(tt.path)
		if !strings.HasPrefix(tt.path, home) {
			// Path outside home — should be unchanged
			if short != tt.path {
				t.Errorf("shortenHome(%q) = %q, want %q (outside home)", tt.path, short, tt.path)
			}
		} else {
			if !strings.HasPrefix(short, "~") {
				t.Errorf("shortenHome(%q) = %q, want '~...' prefix", tt.path, short)
			}
			if strings.Contains(short, home) {
				t.Errorf("shortenHome(%q) still contains home dir: %q", tt.path, short)
			}
		}
	}
}

func TestGetMCPServersConfigPath(t *testing.T) {
	// Default path when no --config
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	defaultPath := filepath.Join(home, ".diane", "mcp-servers.json")

	got := getMCPServersConfigPath([]string{})
	if got != defaultPath {
		t.Errorf("No args: got %q, want %q", got, defaultPath)
	}

	// With --config flag
	got = getMCPServersConfigPath([]string{"--config", "/tmp/custom.json"})
	if got != "/tmp/custom.json" {
		t.Errorf("--config flag: got %q, want %q", got, "/tmp/custom.json")
	}

	// With --config=value
	got = getMCPServersConfigPath([]string{"--config=/tmp/equals.json"})
	if got != "/tmp/equals.json" {
		t.Errorf("--config=value: got %q, want %q", got, "/tmp/equals.json")
	}

	// Other flags don't interfere
	got = getMCPServersConfigPath([]string{"--tools", "--config", "/tmp/other.json", "--verbose"})
	if got != "/tmp/other.json" {
		t.Errorf("Mixed flags: got %q, want %q", got, "/tmp/other.json")
	}
}

func TestEnsureDianeDir(t *testing.T) {
	// Save original and restore
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	// Set a temp home
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dir := ensureDianeDir()
	expected := filepath.Join(tmpHome, ".diane")
	if dir != expected {
		t.Errorf("ensureDianeDir() = %q, want %q", dir, expected)
	}
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Error("~/.diane directory was not created")
	}
}

// =========================================================================
// cmdMCPList integration tests (capture stdout)
// =========================================================================

func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	fn()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	var bufOut, bufErr strings.Builder
	outBytes, _ := io.ReadAll(rOut)
	bufOut.Write(outBytes)
	errBytes, _ := io.ReadAll(rErr)
	bufErr.Write(errBytes)
	return bufOut.String(), bufErr.String()
}

func TestCmdMCPList_NoConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "nonexistent.json")

	output, _ := captureOutput(t, func() {
		cmdMCPList([]string{"--config", configPath})
	})

	if !strings.Contains(output, "no config file") {
		t.Errorf("Expected 'no config file' message, got:\n%s", output)
	}
	if strings.Contains(output, "❌") {
		t.Errorf("Should not show error for missing config file, got:\n%s", output)
	}
}

func TestCmdMCPList_EmptyConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")
	if err := os.WriteFile(configPath, []byte(`{"servers": []}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	output, _ := captureOutput(t, func() {
		cmdMCPList([]string{"--config", configPath})
	})

	if !strings.Contains(output, "no servers configured") {
		t.Errorf("Expected 'no servers configured' message, got:\n%s", output)
	}
}

func TestCmdMCPList_WithServers(t *testing.T) {
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
				"name": "filesystem",
				"enabled": false,
				"type": "stdio",
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
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

	output, _ := captureOutput(t, func() {
		cmdMCPList([]string{"--config", configPath})
	})

	// Should show all three servers
	if !strings.Contains(output, "time") {
		t.Error("Expected 'time' server in output")
	}
	if !strings.Contains(output, "filesystem") {
		t.Error("Expected 'filesystem' server in output")
	}
	if !strings.Contains(output, "internal-api") {
		t.Error("Expected 'internal-api' server in output")
	}

	// Should show status indicators
	if !strings.Contains(output, "✓") {
		t.Error("Expected checkmark for enabled servers")
	}
	if !strings.Contains(output, "✗") {
		t.Error("Expected X for disabled servers")
	}

	// Show command for stdio servers
	if !strings.Contains(output, "uvx mcp-server-time") {
		t.Error("Expected 'uvx mcp-server-time' command in output")
	}
	if !strings.Contains(output, "npx") {
		t.Error("Expected 'npx' command in output")
	}

	// Show "(remote)" for http servers
	if !strings.Contains(output, "(remote)") {
		t.Error("Expected '(remote)' indicator for http server")
	}

	// Should show status labels
	if !strings.Contains(output, "enabled") {
		t.Error("Expected 'enabled' status label in output")
	}
	if !strings.Contains(output, "disabled") {
		t.Error("Expected 'disabled' status label in output")
	}

	// Should show summary
	if !strings.Contains(output, "3 servers total") {
		t.Error("Expected '3 servers total' summary")
	}
	if !strings.Contains(output, "2 enabled") {
		t.Error("Expected '2 enabled' summary")
	}
}

func TestCmdMCPList_WithEnvVars(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{
		"servers": [
			{
				"name": "github",
				"enabled": true,
				"type": "stdio",
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-github"],
				"env": {
					"GITHUB_TOKEN": "ghp_test123"
				}
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	output, _ := captureOutput(t, func() {
		cmdMCPList([]string{"--config", configPath})
	})

	if !strings.Contains(output, "1 env var") {
		t.Errorf("Expected '1 env var' indicator, got:\n%s", output)
	}
}

func TestCmdMCPList_InvalidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(configPath, []byte(`not json`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, errOutput := captureOutput(t, func() {
		cmdMCPList([]string{"--config", configPath})
	})

	if !strings.Contains(errOutput, "Failed to load config") {
		t.Errorf("Expected error on stderr, got:\n%s", errOutput)
	}
}

func TestCmdMCPList_RemoteServer(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	configJSON := `{
		"servers": [
			{
				"name": "remote-api",
				"enabled": true,
				"type": "http",
				"command": ""
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	output, _ := captureOutput(t, func() {
		cmdMCPList([]string{"--config", configPath})
	})

	if !strings.Contains(output, "(remote)") {
		t.Errorf("Expected '(remote)' indicator for http server, got:\n%s", output)
	}
}

func TestCmdMCPList_MissingConfigFileError(t *testing.T) {
	// Test that missing config file is handled gracefully (not a crash)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "nonexistent.json")

	output, _ := captureOutput(t, func() {
		cmdMCPList([]string{"--config", configPath})
	})

	if !strings.Contains(output, "no config file") {
		t.Errorf("Expected graceful handling of missing config, got:\n%s", output)
	}
}
