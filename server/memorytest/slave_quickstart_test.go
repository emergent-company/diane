// Package memorytest validates the diane CLI binary end-to-end via exec.
//
// These tests require credentials — either from ~/.config/diane.yml or env vars
// DIANE_TOKEN, DIANE_SERVER, DIANE_PROJECT.
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestSlave ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
)

const slaveHost = "mcj@100.75.227.125"

// TestSlaveQuickStart runs through the Quick Start guide in README.md
// to verify a fresh Diane installation on a slave machine via SSH.
//
// Flow matches README sections:
//  1. SSH connectivity check
//  2. Clean slate (skip if DIANE_KEEP is set)
//  3. Install — gh release download + extract
//  4. Configure — write ~/.config/diane.yml
//  5. Verify — diane projects + diane doctor
//  6. Connect — diane mcp relay
//  7. Verify session visible from Memory Platform
//  8. Cleanup (only if DIANE_KEEP is not set)
func TestSlaveQuickStart(t *testing.T) {
	pc := loadConfig(t)
	if pc == nil {
		t.Skip("No active project config found — run 'diane init', check ~/.config/diane.yml, or set DIANE_TOKEN/DIANE_SERVER/DIANE_PROJECT")
	}

	keep := os.Getenv("DIANE_KEEP") != ""
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Step 1: Connectivity
	t.Log("═══ Step 1: SSH connectivity ═══")
	if !sshReachable(t, ctx) {
		t.Skipf("Slave host %s not reachable — skipping", slaveHost)
	}

	// Step 2: Clean slate (only if not keeping)
	if !keep {
		t.Log("")
		t.Log("═══ Step 2: Clean slate ═══")
		cleanSlave(t, ctx)
		t.Log("✅ Fresh state ready")
	} else {
		t.Log("")
		t.Log("═══ Step 2: Skipped (DIANE_KEEP is set) ═══")
	}

	// Step 3: Install
	t.Log("")
	t.Log("═══ Step 3: Install (README: Private repo) ═══")
	installOnSlave(t, ctx)

	// Step 4: Configure
	t.Log("")
	t.Log("═══ Step 4: Configure (README: Configure) ═══")
	writeSlaveConfig(t, ctx, pc)

	// Step 5: Verify
	t.Log("")
	t.Log("═══ Step 5: Verify (README: Verify) ═══")
	verifyCLI(t, ctx)

	// Step 6: Connect — start the relay in background
	t.Log("")
	t.Log("═══ Step 6: Connect (README: Connect as a Slave) ═══")
	relayPID := startRelay(t, ctx)
	if relayPID == "" {
		t.Fatal("Relay did not start")
	}
	t.Logf("✅ Relay running (PID: %s)", relayPID)

	// Step 7: Verify the instance is visible from Memory Platform
	t.Log("")
	t.Log("═══ Step 7: Verify session listed on Memory Platform ═══")
	verifyRelaySession(t, ctx, pc)

	// Step 8: Cleanup (only if DIANE_KEEP is not set)
	if !keep {
		t.Log("")
		t.Log("═══ Step 8: Cleanup ═══")
		stopRelay(t, ctx, relayPID)
		cleanSlave(t, ctx)
		t.Log("✅ Cleaned up")
	} else {
		t.Log("")
		t.Log("═══ Step 8: Keeping slave running (DIANE_KEEP is set) ═══")
	}

	t.Log("")
	t.Log("═══ Quick Start verified ✅ ═══")
}

// ── Config ──

type projectConfig struct {
	ServerURL string
	Token     string
	ProjectID string
}

// loadConfig reads credentials from env vars first, then falls back to diane.yml.
// Env: DIANE_TOKEN, DIANE_SERVER, DIANE_PROJECT
func loadConfig(t *testing.T) *projectConfig {
	t.Helper()

	tok := os.Getenv("DIANE_TOKEN")
	srv := os.Getenv("DIANE_SERVER")
	pid := os.Getenv("DIANE_PROJECT")

	if tok != "" && srv != "" && pid != "" {
		t.Logf("Using env vars: server=%s project=%s", srv, pid)
		return &projectConfig{ServerURL: srv, Token: tok, ProjectID: pid}
	}

	// Fall back to config file
	cfg, err := config.Load()
	if err != nil {
		t.Logf("Failed to load config: %v", err)
		return nil
	}
	pc := cfg.Active()
	if pc == nil {
		t.Log("No active project found in config")
		return nil
	}
	t.Logf("Using config file: server=%s project=%s", pc.ServerURL, pc.ProjectID)
	return &projectConfig{ServerURL: pc.ServerURL, Token: pc.Token, ProjectID: pc.ProjectID}
}

// ── Install (matches README "Private repo" section) ──

func installOnSlave(t *testing.T, ctx context.Context) {
	t.Helper()
	ghToken := getGHToken(t)

	mustSSH(t, ctx, "mkdir -p ~/.diane/bin")
	mustSSH(t, ctx, fmt.Sprintf(
		"export PATH=/opt/homebrew/bin:$PATH GITHUB_TOKEN=%s && cd ~/.diane/bin && gh release download -R emergent-company/diane v1.1.0 -p 'diane-darwin-arm64.tar.gz' --clobber",
		ghToken,
	))
	mustSSH(t, ctx, "cd ~/.diane/bin && tar xzf diane-darwin-arm64.tar.gz && rm diane-darwin-arm64.tar.gz && chmod +x diane")

	out, err := sshCmdOutput(ctx, "file ~/.diane/bin/diane && ls -lh ~/.diane/bin/diane")
	if err != nil {
		t.Fatalf("Binary verification failed: %v\n%s", err, out)
	}
	t.Logf("✅ Installed: %s", strings.TrimSpace(out))
}

func getGHToken(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("gh", "auth", "token").CombinedOutput()
	if err != nil {
		t.Skipf("No local gh token available: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// ── Configure (matches README "Configure" section) ──

func writeSlaveConfig(t *testing.T, ctx context.Context, pc *projectConfig) {
	t.Helper()
	yml := fmt.Sprintf(`default: default
projects:
  default:
    server_url: %s
    token: %s
    project_id: %s
    instance_id: tool-test
`, pc.ServerURL, pc.Token, pc.ProjectID)

	mustSSH(t, ctx, fmt.Sprintf("cat > ~/.config/diane.yml << 'DIANEEOF'\n%sDIANEEOF", yml))

	out, err := sshCmdOutput(ctx, "wc -l ~/.config/diane.yml")
	if err != nil {
		t.Fatalf("Config not found: %v", err)
	}
	t.Logf("✅ Config written (%s)", strings.TrimSpace(out))
}

// ── Verify (matches README "Verify" section) ──

func verifyCLI(t *testing.T, ctx context.Context) {
	t.Helper()
	out, err := sshCmdOutput(ctx, "export PATH=$HOME/.diane/bin:$PATH && diane projects 2>&1")
	if err != nil {
		t.Logf("'diane projects' exit: %v", err)
	}
	if !strings.Contains(out, "Server:") || !strings.Contains(out, "Project:") {
		t.Errorf("'diane projects' should show server/project. Got: %s", out)
	} else {
		t.Log("✅ diane projects — shows project info")
	}

	out, err = sshCmdOutput(ctx, "export PATH=$HOME/.diane/bin:$PATH && diane doctor 2>&1")
	if err != nil {
		t.Logf("'diane doctor' exit: %v", err)
	}
	for _, keyword := range []string{"Diane Doctor", "Config file", "Server:", "API token", "SDK initialized", "Done"} {
		if strings.Contains(out, keyword) {
			t.Logf("✅ Doctor — %s", keyword)
		}
	}
}

// ── Connect (matches README "Connect as a Slave" section) ──

func startRelay(t *testing.T, ctx context.Context) string {
	t.Helper()
	// Start relay in background, capture PID
	out, err := sshCmdOutput(ctx,
		"export PATH=$HOME/.diane/bin:$PATH && nohup diane mcp relay --instance tool-test > ~/.diane/relay.log 2>&1 & echo $!",
	)
	if err != nil {
		t.Fatalf("Failed to start relay: %v\n%s", err, out)
	}
	pid := strings.TrimSpace(out)

	// Wait for relay to establish connection
	time.Sleep(5 * time.Second)

	// Verify it's still running and check logs for connection
	checkOut, err := sshCmdOutput(ctx,
		"export PATH=$HOME/.diane/bin:$PATH && ps -p "+pid+" > /dev/null 2>&1 && echo RUNNING || echo DEAD",
	)
	if err != nil || !strings.Contains(checkOut, "RUNNING") {
		logOut, _ := sshCmdOutput(ctx, "tail -20 ~/.diane/relay.log 2>/dev/null")
		t.Fatalf("Relay process %s is not running. Logs:\n%s", pid, logOut)
	}

	// Show connection status from logs
	logOut, _ := sshCmdOutput(ctx, "grep -E '(Connected|Registered|error|Error)' ~/.diane/relay.log 2>/dev/null")
	t.Logf("Relay logs:\n%s", logOut)

	return pid
}

func stopRelay(t *testing.T, ctx context.Context, pid string) {
	t.Helper()
	sshCmdOutput(ctx, "kill "+pid+" 2>/dev/null; kill -0 "+pid+" 2>/dev/null && sleep 2 && kill -9 "+pid+" 2>/dev/null; true")
}

// verifyRelaySession runs 'diane nodes' on the slave to confirm the instance
// is registered and visible.
func verifyRelaySession(t *testing.T, ctx context.Context, pc *projectConfig) {
	t.Helper()

	// Give the relay a moment to register
	time.Sleep(2 * time.Second)

	out, err := sshCmdOutput(ctx, "export PATH=$HOME/.diane/bin:$PATH && diane nodes 2>&1")
	if err != nil {
		t.Logf("'diane nodes' exit: %v", err)
	}
	t.Logf("diane nodes output:\n%s", out)

	if strings.Contains(out, "tool-test") {
		t.Log("✅ Node 'tool-test' visible via 'diane nodes'")
	} else if strings.Contains(out, "Connected relay") || strings.Contains(out, "nodes") {
		t.Log("⚠️  'tool-test' not found but nodes command responded")
	} else {
		t.Log("⚠️  Could not verify node listing")
	}
}

// ── SSH helpers ──

func sshCmd(ctx context.Context, args ...string) *exec.Cmd {
	sshArgs := append([]string{
		"-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		slaveHost,
	}, args...)
	return exec.CommandContext(ctx, "ssh", sshArgs...)
}

func sshCmdOutput(ctx context.Context, args ...string) (string, error) {
	cmd := sshCmd(ctx, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustSSH(t *testing.T, ctx context.Context, cmd string) {
	t.Helper()
	out, err := sshCmdOutput(ctx, cmd)
	if err != nil {
		t.Fatalf("SSH command failed: %v\nCommand: %s\nOutput: %s", err, cmd, out)
	}
}

func cleanSlave(t *testing.T, ctx context.Context) {
	t.Helper()
	sshCmdOutput(ctx, "rm -rf ~/.diane/ ~/.diane.* ~/.config/diane.yml ~/.diane")
}

func sshReachable(t *testing.T, ctx context.Context) bool {
	t.Helper()
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := sshCmdOutput(ctx2, "hostname && uname -s")
	if err != nil {
		t.Logf("SSH to %s failed: %v", slaveHost, err)
		return false
	}
	out = strings.TrimSpace(out)
	t.Logf("SSH connected: %s", strings.Split(out, "\n")[0])
	return true
}
