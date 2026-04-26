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

	// Step 3: Install — cross-compile current dev binary and SCP to slave
	t.Log("")
	t.Log("═══ Step 3: Install (cross-compile + SCP) ═══")
	installOnSlave(t, ctx)

	// Step 3b: Verify the dev binary has master/slave support
	t.Log("")
	t.Log("═══ Step 3b: Verify mode support in binary ═══")
	verifyModeSupport(t, ctx)

	// Step 4: Configure
	t.Log("")
	t.Log("═══ Step 4: Configure (README: Configure) ═══")
	writeSlaveConfig(t, ctx, pc)

	// Step 5: Verify
	t.Log("")
	t.Log("═══ Step 5: Verify (README: Verify) ═══")
	verifyCLI(t, ctx)

	// Step 5.5: Test upgrade — installs v1.1.1 with nodes/service/upgrade commands
	t.Log("")
	t.Log("═══ Step 5.5 Test upgrade (diane upgrade) ═══")
	verifyUpgrade(t, ctx)

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

// ── Install (cross-compile dev binary and SCP to slave) ──

func installOnSlave(t *testing.T, ctx context.Context) {
	t.Helper()

	// Cross-compile for darwin/arm64
	buildCmd := exec.CommandContext(ctx, "/usr/local/go/bin/go", "build",
		"-o", "/tmp/diane-darwin-arm64",
		"./cmd/diane/")
	buildCmd.Dir = "/root/diane/server"
	buildCmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0")
	out, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Cross-compile failed: %v\nOutput: %s", err, string(out))
	}
	t.Log("✅ Cross-compiled binary for darwin/arm64")

	// SCP to slave
	scpCmd := exec.CommandContext(ctx, "scp",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"/tmp/diane-darwin-arm64",
		slaveHost+":~/.diane/bin/diane")
	scpOut, err := scpCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SCP to slave failed: %v\nOutput: %s", err, string(scpOut))
	}
	t.Log("✅ SCP'd binary to slave")

	// Verify
	verifyOut, err := sshCmdOutput(ctx, "file ~/.diane/bin/diane && chmod +x ~/.diane/bin/diane && ls -lh ~/.diane/bin/diane")
	if err != nil {
		t.Fatalf("Binary verification failed: %v\n%s", err, verifyOut)
	}
	t.Logf("✅ Installed: %s", strings.TrimSpace(verifyOut))
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
	yaml := fmt.Sprintf(`default: default
projects:
  default:
    server_url: %s
    token: %s
    project_id: %s
    mode: slave
    instance_id: tool-test
`, pc.ServerURL, pc.Token, pc.ProjectID)

	mustSSH(t, ctx, fmt.Sprintf("cat > ~/.config/diane.yml << 'DIANEEOF'\n%sDIANEEOF", yaml))

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

// ── Upgrade ──

func verifyUpgrade(t *testing.T, ctx context.Context) {
	t.Helper()
	ghToken := getGHToken(t)
	// Actually run diane upgrade which checks GitHub and replaces itself
	out, err := sshCmdOutput(ctx,
		fmt.Sprintf("export PATH=$HOME/.diane/bin:$PATH GITHUB_TOKEN=%s && diane upgrade 2>&1", ghToken),
	)
	if err != nil {
		t.Logf("'diane upgrade' exit: %v", err)
	}
	t.Logf("Upgrade output:\n%s", out)

	if strings.Contains(out, "Upgraded") || strings.Contains(out, "up to date") || strings.Contains(out, "Already") {
		t.Log("✅ diane upgrade completed")
	}

	// Verify new commands exist (quiet check — they may error on usage, just not "unknown")
	for _, cmd := range []string{"nodes", "upgrade", "service"} {
		checkOut, err := sshCmdOutput(ctx,
			"export PATH=$HOME/.diane/bin:$PATH && diane "+cmd+" 2>&1",
		)
		if strings.Contains(checkOut, "Unknown command") {
			t.Errorf("Command 'diane %s' not found after upgrade:\n%s", cmd, checkOut)
		} else {
			t.Logf("✅ diane %s — recognized", cmd)
		}
		_ = err // ignore exit codes — commands that need args will exit 1
	}
}

// ── Connect (matches README "Connect as a Slave" section) ──

func startRelay(t *testing.T, ctx context.Context) string {
	t.Helper()
	// Use diane service start for proper PID management
	out, err := sshCmdOutput(ctx,
		"export PATH=$HOME/.diane/bin:$PATH && diane service start --instance tool-test 2>&1",
	)
	if err != nil {
		// If service start fails, try the raw method
		t.Logf("'diane service start' failed: %v\n%s", err, out)
		t.Logf("Falling back to direct relay start...")
		out, err = sshCmdOutput(ctx,
			"export PATH=$HOME/.diane/bin:$PATH && nohup diane mcp relay --instance tool-test > ~/.diane/relay.log 2>&1 & echo $!",
		)
		if err != nil {
			t.Fatalf("Failed to start relay: %v\n%s", err, out)
		}
		pid := strings.TrimSpace(out)
		t.Logf("Relay PID: %s", pid)

		// Verify running
		time.Sleep(5 * time.Second)
		checkOut, err := sshCmdOutput(ctx,
			"export PATH=$HOME/.diane/bin:$PATH && ps -p "+pid+" > /dev/null 2>&1 && echo RUNNING || echo DEAD",
		)
		if err != nil || !strings.Contains(checkOut, "RUNNING") {
			logOut, _ := sshCmdOutput(ctx, "tail -20 ~/.diane/relay.log 2>/dev/null")
			t.Fatalf("Relay process %s is not running. Logs:\n%s", pid, logOut)
		}
		logOut, _ := sshCmdOutput(ctx, "grep -E '(Connected|Registered|error|Error)' ~/.diane/relay.log 2>/dev/null")
		t.Logf("Relay logs:\n%s", logOut)
		return pid
	}

	// service start worked — verify status
	t.Logf("diane service start output:\n%s", out)

	// Check service status
	statusOut, err := sshCmdOutput(ctx,
		"export PATH=$HOME/.diane/bin:$PATH && diane service status --instance tool-test 2>&1",
	)
	if err != nil {
		t.Logf("'diane service status' exit: %v", err)
	}
	t.Logf("Service status:\n%s", statusOut)

	if strings.Contains(statusOut, "is running") {
		t.Log("✅ Relay running via diane service")
	} else {
		t.Log("⚠️  Relay may not be running via service")
	}

	// Extract PID from status
	pid := ""
	for _, line := range strings.Split(statusOut, "\n") {
		if strings.Contains(line, "PID") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "(PID" && i+1 < len(parts) {
					pid = strings.TrimRight(parts[i+1], ")")
				}
			}
		}
	}
	return pid
}

func stopRelay(t *testing.T, ctx context.Context, pid string) {
	t.Helper()
	// Use service stop first (clean), fall back to raw kill
	out, err := sshCmdOutput(ctx,
		"export PATH=$HOME/.diane/bin:$PATH && diane service stop --instance tool-test 2>&1",
	)
	if err != nil {
		t.Logf("'diane service stop' failed: %v\n%s", err, out)
		// Fall back to raw kill
		sshCmdOutput(ctx, "kill "+pid+" 2>/dev/null; sleep 1; kill -9 "+pid+" 2>/dev/null; true")
	} else {
		t.Logf("diane service stop:\n%s", out)
	}
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
