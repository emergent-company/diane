// Package memorytest verifies that slave node guardrails are correctly enforced.
//
// Slave nodes should block all mutation commands (bot, agent seed, agent delete,
// agent sync, agent define) while allowing read-only commands (doctor, agent list).
//
// The test creates a temporary config with mode: slave and uses DIANE_CONFIG
// to point diane at it, then runs each command and checks the output.
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestSlaveGuardrails ./memorytest/
package memorytest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// =========================================================================
// TestSlaveGuardrails_BlockedCommands: Verifies that mutation commands are
// rejected on slave nodes with the correct error message.
// =========================================================================

func TestSlaveGuardrails_BlockedCommands(t *testing.T) {
	slaveConfig := setupSlaveConfig(t)
	defer slaveConfig.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dianeBin := findDianeBinary(t)
	env := []string{"DIANE_CONFIG=" + slaveConfig.path}
	args := []struct {
		cmd     []string
		keyword string // expected error keyword
	}{
		{[]string{"bot"}, "slave node"},
		{[]string{"agent", "seed"}, "not available on slave nodes"},
		{[]string{"agent", "delete", "dummy-test-agent"}, "not available on slave nodes"},
		{[]string{"agent", "sync"}, "not available on slave nodes"},
		{[]string{"agent", "define", "dummy-test-agent"}, "not available on slave nodes"},
		{[]string{"agent", "seed-db"}, "not available on slave nodes"},
		{[]string{"agent", "route", "dummy", "50"}, "not available on slave nodes"},
		{[]string{"agent", "tag", "dummy", "test"}, "not available on slave nodes"},
		{[]string{"agent", "prune"}, "not available on slave nodes"},
	}

	for _, a := range args {
		t.Run(strings.Join(a.cmd, " "), func(t *testing.T) {
			cmdCtx, cmdCancel := context.WithTimeout(ctx, 10*time.Second)
			defer cmdCancel()

			output, err := runCLIWithEnv(cmdCtx, t, dianeBin, env, a.cmd...)

			// bot exits 0 (clean return) but still prints the error via log.Printf
			// Other commands use os.Exit(1) via requireMaster
			if a.cmd[0] != "bot" {
				if err == nil {
					t.Errorf("Expected error for 'diane %s' on slave, but got exit code 0", strings.Join(a.cmd, " "))
				}
			}

			// Should contain the expected keyword
			if !strings.Contains(output, a.keyword) {
				t.Errorf("Output for 'diane %s' should contain %q, got:\n%s",
					strings.Join(a.cmd, " "), a.keyword, output)
			} else {
				t.Logf("✅ 'diane %s' blocked with: %s", strings.Join(a.cmd, " "), extractRelevantLine(output, a.keyword))
			}
		})
	}
}

// =========================================================================
// TestSlaveGuardrails_AllowedCommands: Verifies that read-only commands
// still work on slave nodes.
// =========================================================================

func TestSlaveGuardrails_AllowedCommands(t *testing.T) {
	slaveConfig := setupSlaveConfig(t)
	defer slaveConfig.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dianeBin := findDianeBinary(t)
	env := []string{"DIANE_CONFIG=" + slaveConfig.path}

	t.Run("doctor", func(t *testing.T) {
		cmdCtx, cmdCancel := context.WithTimeout(ctx, 15*time.Second)
		defer cmdCancel()

		output, err := runCLIWithEnv(cmdCtx, t, dianeBin, env, "doctor")
		if err != nil {
			t.Fatalf("doctor should work on slave: %v\nOutput: %s", err, output)
		}
		if !strings.Contains(output, "Slave") && !strings.Contains(output, "slave") {
			t.Logf("Doctor output should mention slave mode (may be worded differently)")
		}
		if strings.Contains(output, "✅") || strings.Contains(output, "✅") || strings.Contains(output, "Done") {
			t.Log("✅ doctor completed on slave node")
		}
		t.Logf("Doctor output starts with:\n%s", output[:min(len(output), 300)])
	})

	t.Run("agent list", func(t *testing.T) {
		cmdCtx, cmdCancel := context.WithTimeout(ctx, 15*time.Second)
		defer cmdCancel()

		output, err := runCLIWithEnv(cmdCtx, t, dianeBin, env, "agent", "list")
		if err != nil {
			t.Fatalf("agent list should work on slave: %v\nOutput: %s", err, output)
		}
		if strings.Contains(output, "diane-default") || strings.Contains(output, "diane-agent") {
			t.Log("✅ agent list returned agents on slave node")
		} else {
			t.Logf("agent list output: %s", output[:min(len(output), 200)])
		}
	})
}

// =========================================================================
// Helpers
// =========================================================================

type slaveTestConfig struct {
	path    string
	cleanup func()
}

// setupSlaveConfig creates a temporary config file with mode: slave
// using credentials from the real config. Returns the path and cleanup func.
func setupSlaveConfig(t *testing.T) *slaveTestConfig {
	t.Helper()

	// Read real config to reuse credentials
	realData, err := os.ReadFile("/root/.config/diane.yml")
	if err != nil {
		t.Skipf("No real config found at /root/.config/diane.yml: %v — 'diane init' required", err)
	}

	// Parse just the project fields we need (server_url, token, project_id)
	// using the same YAML structure but injecting mode: slave
	type proj struct {
		ServerURL string `yaml:"server_url"`
		Token     string `yaml:"token"`
		ProjectID string `yaml:"project_id"`
		Mode      string `yaml:"mode,omitempty"`
	}
	type cfg struct {
		Default  string           `yaml:"default"`
		Projects map[string]*proj `yaml:"projects"`
	}

	var realCfg cfg
	if err := yaml.Unmarshal(realData, &realCfg); err != nil {
		t.Fatalf("Failed to parse real config: %v", err)
	}
	projName := realCfg.Default
	if projName == "" {
		for name := range realCfg.Projects {
			projName = name
			break
		}
	}
	p, ok := realCfg.Projects[projName]
	if !ok || p == nil {
		t.Fatalf("No project %q found in real config", projName)
	}

	// Build minimal slave config
	slaveCfg := cfg{
		Default: projName,
		Projects: map[string]*proj{
			projName: {
				ServerURL: p.ServerURL,
				Token:     p.Token,
				ProjectID: p.ProjectID,
				Mode:      "slave",
			},
		},
	}

	slaveData, err := yaml.Marshal(&slaveCfg)
	if err != nil {
		t.Fatalf("Failed to marshal slave config: %v", err)
	}

	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "diane-slave.yml")
	if err := os.WriteFile(tmpPath, slaveData, 0644); err != nil {
		t.Fatalf("Failed to write slave config: %v", err)
	}

	t.Logf("Created slave config at %s", tmpPath)
	return &slaveTestConfig{
		path:    tmpPath,
		cleanup: func() { os.Remove(tmpPath) },
	}
}

// runCLIWithEnv is like runCLI but with additional environment variables.
func runCLIWithEnv(ctx context.Context, t *testing.T, dianeBin string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, dianeBin, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// extractRelevantLine returns the first line of output containing the keyword.
func extractRelevantLine(output, keyword string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, keyword) {
			return strings.TrimSpace(line)
		}
	}
	return "(not found)"
}
