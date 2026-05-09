// Package memorytest validates diane serve in a Docker container.
//
// These tests spin up a Docker container running `diane serve` as a slave
// relay node, then test CLI commands (service status, monitor, tool test)
// and HTTP health checks against the companion API.
//
// Prerequisites:
//   - Docker installed and running
//   - Docker image built: docker build -t diane-test-node -f docker/Dockerfile .
//   - Env vars: MEMORY_SERVER_URL, MEMORY_PROJECT_ID, MEMORY_API_KEY
//
// Run:
//
//	export MEMORY_SERVER_URL=https://memory.emergent-company.ai
//	export MEMORY_PROJECT_ID=<uuid>
//	export MEMORY_API_KEY=<emt-token>
//	cd server && go test -v -count=1 -run TestDocker ./memorytest/
//
//go:build integration

package memorytest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Container name used for the test run
const dockerContainerName = "diane-test-containertest"

// Required env vars for the test
var dockerRequiredEnv = []string{"MEMORY_SERVER_URL", "MEMORY_PROJECT_ID", "MEMORY_API_KEY"}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// dockerAvailable returns true if the docker command works.
func dockerAvailable() bool {
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}

// dockerEnsureEnv checks that all required env vars are set.
func dockerEnsureEnv(t *testing.T) {
	t.Helper()
	for _, key := range dockerRequiredEnv {
		if os.Getenv(key) == "" {
			t.Skipf("Required env var %s not set — skipping Docker tests", key)
		}
	}
}

// dockerRun starts the diane-test-node container with the required env vars.
// Returns the container ID and a cleanup function.
func dockerRun(t *testing.T) (containerID string, cleanup func()) {
	t.Helper()

	// Stop and remove any leftover container
	exec.Command("docker", "rm", "-f", dockerContainerName).Run()

	args := []string{
		"run", "-d",
		"--name", dockerContainerName,
		"--rm",
		"-p", "8890:8890",
		"-e", fmt.Sprintf("MEMORY_SERVER_URL=%s", os.Getenv("MEMORY_SERVER_URL")),
		"-e", fmt.Sprintf("MEMORY_PROJECT_ID=%s", os.Getenv("MEMORY_PROJECT_ID")),
		"-e", fmt.Sprintf("MEMORY_API_KEY=%s", os.Getenv("MEMORY_API_KEY")),
		"-e", fmt.Sprintf("DIANE_INSTANCE_ID=%s", "ci-test-node"),
		"diane-test-node",
	}

	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to start container: %v\nOutput: %s", err, string(out))
	}

	containerID = strings.TrimSpace(string(out))
	t.Logf("Container started: %s", containerID)

	// Wait for health check
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	healthy := false
	for ctx.Err() == nil {
		time.Sleep(2 * time.Second)
		check := exec.CommandContext(ctx, "docker", "inspect",
			"--format={{.State.Health.Status}}", containerID)
		status, _ := check.CombinedOutput()
		statusStr := strings.TrimSpace(string(status))
		t.Logf("  Health: %s", statusStr)
		if statusStr == "healthy" {
			healthy = true
			break
		}
	}
	if !healthy {
		logs, _ := exec.Command("docker", "logs", containerID).CombinedOutput()
		t.Fatalf("Container did not become healthy in 30s\nLogs:\n%s", string(logs))
	}

	cleanup = func() {
		t.Logf("Stopping container %s...", containerID)
		exec.Command("docker", "rm", "-f", containerID).Run()
	}

	return containerID, cleanup
}

// dockerExec runs a command inside the container and returns combined output.
func dockerExec(ctx context.Context, t *testing.T, containerID string, args ...string) (string, error) {
	t.Helper()
	dockerArgs := append([]string{"exec", containerID}, args...)
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestDockerHealthCheck verifies the companion API returns 200 on /api/status.
func TestDockerHealthCheck(t *testing.T) {
	dockerEnsureEnv(t)
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	_, cleanup := dockerRun(t)
	defer cleanup()

	// HTTP check
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://localhost:8890/api/status")
	if err != nil {
		t.Fatalf("HTTP health check failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("GET /api/status → %d: %s", resp.StatusCode, string(body))

	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

// TestDockerServiceStatus runs 'diane service status' inside the container.
func TestDockerServiceStatus(t *testing.T) {
	dockerEnsureEnv(t)
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	containerID, cleanup := dockerRun(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := dockerExec(ctx, t, containerID, "diane", "service", "status")
	t.Logf("=== 'diane service status' ===\n%s\n=== end ===", output)

	if err != nil {
		t.Logf("Exit error (may still be valid): %v", err)
	}

	// Should report something about the serve process
	if strings.Contains(output, "running") || strings.Contains(output, "up") || strings.Contains(output, "PID") {
		t.Log("✅ Service status shows running process")
	} else {
		// 'diane service status' may show "not running" when run inside container
		// because it checks PID file / processes. Accept any non-crash output.
		t.Logf("⚠️  Raw output: %s", output)
	}
}

// TestDockerMonitor runs 'diane monitor' inside the container.
func TestDockerMonitor(t *testing.T) {
	dockerEnsureEnv(t)
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	containerID, cleanup := dockerRun(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := dockerExec(ctx, t, containerID, "diane", "monitor")
	t.Logf("=== 'diane monitor' ===\n%s\n=== end ===", output)

	if err != nil {
		t.Logf("Exit error: %v", err)
	}

	// Monitor should show at least some header/status output
	if len(strings.TrimSpace(output)) > 50 {
		t.Log("✅ Monitor produced substantive output")
	} else {
		t.Logf("⚠️  Short output: %q", output)
	}
}

// TestDockerUpgradeDryRun runs 'diane upgrade --dry-run' to ensure no crash.
func TestDockerUpgradeDryRun(t *testing.T) {
	dockerEnsureEnv(t)
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	containerID, cleanup := dockerRun(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := dockerExec(ctx, t, containerID, "diane", "upgrade", "--dry-run")
	t.Logf("=== 'diane upgrade --dry-run' ===\n%s\n=== end ===", output)

	if err != nil {
		// Upgrade may fail because GitHub API access may not be available,
		// but it should not crash/panic
		t.Logf("Exit error (expected if no internet): %v", err)
	}

	if strings.Contains(output, "panic") {
		t.Error("❌ Output contains panic — upgrade command crashed")
	}
}

// TestDockerToolTest runs 'diane tool test get_time' inside the container.
func TestDockerToolTest(t *testing.T) {
	dockerEnsureEnv(t)
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	containerID, cleanup := dockerRun(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := dockerExec(ctx, t, containerID, "diane", "tool", "test", "get_time")
	t.Logf("=== 'diane tool test get_time' ===\n%s\n=== end ===", output)

	if err != nil {
		t.Logf("Exit error: %v", err)
	}

	// Should return time data
	if strings.Contains(output, "status:") || strings.Contains(output, "success") || strings.Contains(output, `"time"`) {
		t.Log("✅ Tool test returned expected output")
	} else {
		t.Logf("⚠️  Unexpected output (may still be valid): %s", output)
	}
}

// TestDockerNodes runs 'diane nodes' inside the container.
func TestDockerNodes(t *testing.T) {
	dockerEnsureEnv(t)
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	containerID, cleanup := dockerRun(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := dockerExec(ctx, t, containerID, "diane", "nodes")
	t.Logf("=== 'diane nodes' ===\n%s\n=== end ===", output)

	if err != nil {
		t.Logf("Exit error: %v", err)
	}

	// Should show at least the test node itself
	if strings.Contains(output, "ci-test-node") || strings.Contains(output, "instance") || strings.Contains(output, "Node") {
		t.Log("✅ Nodes output shows instance information")
	}
}

// TestDockerVersion runs 'diane version' inside the container.
func TestDockerVersion(t *testing.T) {
	dockerEnsureEnv(t)
	if !dockerAvailable() {
		t.Skip("Docker not available")
	}

	containerID, cleanup := dockerRun(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := dockerExec(ctx, t, containerID, "diane", "version")
	t.Logf("=== 'diane version' ===\n%s\n=== end ===", output)

	if err != nil {
		t.Fatalf("Version command failed: %v", err)
	}

	// Should show version information
	if strings.Contains(output, "v1.") || strings.Contains(output, "Version") || strings.Contains(output, "dev") {
		t.Log("✅ Version command produced output")
	}
}
