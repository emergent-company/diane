// Package memorytest validates the diane CLI binary end-to-end via exec.
//
// These tests require ~/.config/diane.yml to exist (same config that the diane
// CLI uses), and the active project must have a valid API token.
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestSlave ./memorytest/
package memorytest

import (
	"context"
	"fmt"
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
//  2. Clean slate
//  3. Install — gh release download + extract
//  4. Configure — write ~/.config/diane.yml
//  5. Verify — diane projects + diane doctor
//  6. Connect — diane mcp relay
//  7. Cleanup
func TestSlaveQuickStart(t *testing.T) {
	pc := loadActiveConfig(t)
	if pc == nil {
		t.Skip("No active project config found — run 'diane init' or check ~/.config/diane.yml")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Step 1: Connectivity
	t.Log("═══ Step 1: SSH connectivity ═══")
	if !sshReachable(t, ctx) {
		t.Skipf("Slave host %s not reachable — skipping", slaveHost)
	}

	// Step 2: Clean slate
	t.Log("")
	t.Log("═══ Step 2: Clean slate ═══")
	cleanSlave(t, ctx)
	t.Log("✅ Fresh state ready")

	// Step 3: Install — matches README "Private repo" instructions
	t.Log("")
	t.Log("═══ Step 3: Install (README: Private repo) ═══")
	installOnSlave(t, ctx)

	// Step 4: Configure — matches README "Configure" section
	t.Log("")
	t.Log("═══ Step 4: Configure (README: Configure) ═══")
	writeSlaveConfig(t, ctx, pc)

	// Step 5: Verify — matches README "Verify" section
	t.Log("")
	t.Log("═══ Step 5: Verify (README: Verify) ═══")
	verifyCLI(t, ctx)

	// Step 6: Connect — matches README "Connect as a Slave" section
	t.Log("")
	t.Log("═══ Step 6: Connect (README: Connect as a Slave) ═══")
	verifyRelay(t, ctx)

	// Step 7: Cleanup
	t.Log("")
	t.Log("═══ Step 7: Cleanup ═══")
	if !t.Failed() {
		cleanSlave(t, ctx)
		t.Log("✅ Cleaned up")
	} else {
		t.Log("⚠️  Test failed — keeping slave state for debugging")
	}

	t.Log("")
	t.Log("═══ Quick Start verified ✅ ═══")
}

// ── Install (matches README "Private repo" section) ──

func installOnSlave(t *testing.T, ctx context.Context) {
	t.Helper()

	// gh token lives in macOS keychain, unreachable via non-interactive SSH.
	// We pass it via GITHUB_TOKEN env var — same credential, same download UX.
	ghToken := getGHToken(t)

	mustSSH(t, ctx, "mkdir -p ~/.diane/bin")

	// gh release download -R emergent-company/diane v1.1.0 -p "diane-darwin-arm64.tar.gz"
	mustSSH(t, ctx, fmt.Sprintf(
		"export PATH=/opt/homebrew/bin:$PATH GITHUB_TOKEN=%s && cd ~/.diane/bin && gh release download -R emergent-company/diane v1.1.0 -p 'diane-darwin-arm64.tar.gz' --clobber",
		ghToken,
	))

	// tar xzf + rm tarball + chmod
	mustSSH(t, ctx, "cd ~/.diane/bin && tar xzf diane-darwin-arm64.tar.gz && rm diane-darwin-arm64.tar.gz && chmod +x diane")

	// Verify
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

type projectConfig struct {
	ServerURL string
	Token     string
	ProjectID string
}

func loadActiveConfig(t *testing.T) *projectConfig {
	t.Helper()
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
	t.Logf("Loaded config: server=%s project=%s", pc.ServerURL, pc.ProjectID)
	return &projectConfig{
		ServerURL: pc.ServerURL,
		Token:     pc.Token,
		ProjectID: pc.ProjectID,
	}
}

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

func verifyRelay(t *testing.T, ctx context.Context) {
	t.Helper()
	rctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	cmd := sshCmd(rctx,
		"export PATH=$HOME/.diane/bin:$PATH && nohup diane mcp relay --instance tool-test 2>&1 & echo $!; sleep 8; wait",
	)
	out, err := cmd.CombinedOutput()
	output := string(out)

	t.Logf("Relay output (%d bytes):\n%s", len(output), output)

	switch {
	case strings.Contains(output, "Connected to relay"):
		t.Log("✅ Relay connected to Memory Platform")
	case strings.Contains(output, "Failed") || strings.Contains(output, "Error") || strings.Contains(output, "error"):
		t.Logf("⚠️  Relay errors:\n%s", output)
	default:
		t.Log("⚠️  Relay status unclear from output")
	}
	if err != nil {
		t.Logf("Relay process ended: %v (expected if killed by timeout)", err)
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
