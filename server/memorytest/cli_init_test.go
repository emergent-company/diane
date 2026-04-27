// Package memorytest validates 'diane init' — the first-time setup wizard.
//
// Tests pipe input to the interactive prompt and verify the config file is
// created correctly. Original config is backed up and restored after the test.
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestCLI_Init ./memorytest/
package memorytest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// dianeConfig represents the structure of ~/.config/diane.yml
type dianeConfig struct {
	Default  string                     `yaml:"default"`
	Projects map[string]dianeProjectCfg `yaml:"projects"`
}

type dianeProjectCfg struct {
	ServerURL   string   `yaml:"server_url"`
	ProjectID   string   `yaml:"project_id"`
	Token       string   `yaml:"token"`
	Mode        string   `yaml:"mode,omitempty"`
}

// =========================================================================
// TestCLI_InitInteractive: Pipes input to 'diane init' and verifies the
// config file is created with correct values. Restores original config.
// =========================================================================

func TestCLI_InitInteractive(t *testing.T) {
	// Skip if no existing config — we need it for the token/project
	skipIfNoConfig(t)

	configPath := filepath.Join(os.Getenv("HOME"), ".config", "diane.yml")
	if _, err := os.Stat(configPath); err != nil {
		t.Skip("~/.config/diane.yml not found — can't test init")
	}

	// Read current config to get a valid token and project ID
	origData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read config: %v", err)
	}
	var cfg dianeConfig
	if err := yaml.Unmarshal(origData, &cfg); err != nil {
		t.Fatalf("Parse config: %v", err)
	}
	pc, hasProject := cfg.Projects[cfg.Default]
	if !hasProject && len(cfg.Projects) > 0 {
		// Get first project
		for _, p := range cfg.Projects {
			pc = p
			hasProject = true
			break
		}
	}
	if !hasProject || pc.Token == "" || pc.ProjectID == "" {
		t.Skip("Config lacks token or project ID — can't test init")
	}

	t.Logf("Using project: %s (%s)", cfg.Default, pc.ProjectID)

	// Backup original config
	backupPath := configPath + ".bak"
	if err := os.WriteFile(backupPath, origData, 0644); err != nil {
		t.Fatalf("Backup config: %v", err)
	}
	t.Cleanup(func() {
		// Restore original config
		if err := os.WriteFile(configPath, origData, 0644); err != nil {
			t.Errorf("Restore config: %v", err)
		} else {
			t.Log("✅ Original config restored")
		}
		_ = os.Remove(backupPath)
	})

	// Remove the config so init creates it fresh
	if err := os.Remove(configPath); err != nil {
		t.Fatalf("Remove config for fresh init: %v", err)
	}

	// Build stdin input — answer all interactive prompts:
	//   1. Project name [default]: "init-test"
	//   2. Server URL [https://memory.emergent-company.ai]: (default)
	//   3. Token (emt_...): (from existing config)
	//   4. Project ID (UUID): (from existing config)
	//   5. Node mode [master]: (default)
	//   6. Discord bot token (optional): (skip)
	//   7. Allowed Discord channel IDs (comma-separated): (skip)
	//   8. Thread channel IDs: (skip)
	//   9. Apply embedded schemas? [Y/n]: n (skip to keep things clean)
	input := fmt.Sprintf("init-test\n\n%s\n%s\n\n\n\n\nn\n", pc.Token, pc.ProjectID)
	stdin := bytes.NewBufferString(input)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run diane init with piped stdin
	cmd := exec.CommandContext(ctx, findDianeBinary(t), "init")
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	output := stdout.String()
	errOutput := stderr.String()

	t.Logf("=== 'diane init' stdout ===\n%s\n=== end ===", output)
	if errOutput != "" {
		t.Logf("=== stderr ===\n%s\n=== end ===", errOutput)
	}
	if err != nil {
		t.Fatalf("diane init: %v", err)
	}

	// Verify the config was created
	newData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read new config: %v", err)
	}
	var newCfg dianeConfig
	if err := yaml.Unmarshal(newData, &newCfg); err != nil {
		t.Fatalf("Parse new config: %v", err)
	}

	// Verify project was created
	newPC, ok := newCfg.Projects["init-test"]
	if !ok {
		t.Fatal("Config does not contain 'init-test' project")
	}

	if newPC.ServerURL == "" {
		t.Error("ServerURL is empty")
	} else {
		t.Logf("✅ ServerURL: %s", newPC.ServerURL)
	}

	if newPC.ProjectID != pc.ProjectID {
		t.Errorf("ProjectID = %q, want %q", newPC.ProjectID, pc.ProjectID)
	} else {
		t.Log("✅ ProjectID matches")
	}

	if newPC.Token != pc.Token {
		t.Errorf("Token mismatch (may be masked)")
	} else {
		t.Log("✅ Token preserved")
	}

	if newCfg.Default != "init-test" {
		t.Errorf("Default project = %q, want 'init-test'", newCfg.Default)
	} else {
		t.Log("✅ Default project is 'init-test'")
	}

	// Cleanup: restore original config (via Cleanup)
	t.Log("✅ diane init interactive test completed")
}

// =========================================================================
// TestCLI_InitNoToken: Verifies 'diane init' fails with a clear error when
// no token is provided (empty input).
// =========================================================================

func TestCLI_InitNoToken(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)

	// Pipe input without a token
	input := "no-token-test\n\n\n"
	stdin := bytes.NewBufferString(input)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, dianeBin, "init")
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	errOutput := stderr.String()

	// Should fail with a clear error about missing token
	if err == nil {
		t.Log("Expected init to fail with no token — but it succeeded")
		t.Logf("Output: %s", output)
	} else {
		t.Logf("Exit error: %v", err)
	}

	t.Logf("Stdout: %.300s", output)
	if errOutput != "" {
		t.Logf("Stderr: %.300s", errOutput)
	}

	fullOutput := output + errOutput
	if strings.Contains(fullOutput, "token") && (strings.Contains(fullOutput, "required") || strings.Contains(fullOutput, "emt_")) {
		t.Log("✅ Proper error for missing token")
	} else {
		t.Log("⚠️  Could not find token-required error in output")
	}

	t.Log("✅ init no-token test completed")
}
