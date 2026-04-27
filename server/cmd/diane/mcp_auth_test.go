package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdMCPAuthNoServerFlag verifies that the --server flag is required.
func TestCmdMCPAuthNoServerFlag(t *testing.T) {
	// Override osExit to capture the exit code instead of actually exiting
	origExit := osExit
	defer func() { osExit = origExit }()

	var exitCode int
	osExit = func(code int) {
		exitCode = code
		// Panic to stop execution immediately (like os.Exit would)
		panic("os.Exit called")
	}

	func() {
		defer func() {
			recover()
		}()
		cmdMCPAuth([]string{})
	}()

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

// TestCmdMCPAuthUnknownServer verifies that an unknown server name shows an error.
func TestCmdMCPAuthUnknownServer(t *testing.T) {
	// Create a temporary config file with a known server
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "mcp-servers.json")
	configContent := `{
		"servers": [
			{
				"name": "test-server",
				"type": "stdio",
				"command": "echo",
				"enabled": true
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	origExit := osExit
	defer func() { osExit = origExit }()

	var exitCode int
	osExit = func(code int) {
		exitCode = code
		panic("os.Exit called")
	}

	func() {
		defer func() {
			recover()
		}()
		cmdMCPAuth([]string{"--server", "nonexistent", "--config", configPath})
	}()

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

// TestCmdMCPAuthNoOAuthConfig verifies error when server has no OAuth config and no tokens.
func TestCmdMCPAuthNoOAuthConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "mcp-servers.json")
	configContent := `{
		"servers": [
			{
				"name": "noauth-server",
				"type": "stdio",
				"command": "echo",
				"enabled": true
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	origExit := osExit
	defer func() { osExit = origExit }()

	var exitCode int
	osExit = func(code int) {
		exitCode = code
		panic("os.Exit called")
	}

	func() {
		defer func() {
			recover()
		}()
		cmdMCPAuth([]string{"--server", "noauth-server", "--config", configPath})
	}()

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

// TestHelperContains tests the contains helper.
func TestHelperContains(t *testing.T) {
	if !strings.Contains("hello world", "world") {
		t.Error("expected contains to find 'world'")
	}
	if strings.Contains("hello world", "xyz") {
		t.Error("expected contains to NOT find 'xyz'")
	}
}
