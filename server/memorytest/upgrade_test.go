// Package memorytest validates the auto-upgrade mechanism end-to-end.
//
// Runs on diane-test via SSH. The test:
//   1. Builds a mock "v999.0.0-test" diane binary (the "new" release)
//   2. Packages it as the release tar.gz
//   3. Starts a mock HTTP server serving fake GitHub release API
//   4. Builds a Docker image with an old version (v1.0.0-old) using Dockerfile.local
//   5. Deploys master node with DIANE_UPGRADE_API_URL pointing at mock server
//   6. Waits for auto-upgrade loop to fire
//   7. Verifies the binary at ~/.diane/bin/diane was replaced with new version
//
// Prerequisites:
//   - SSH access to diane-test (root@diane-test via Tailscale)
//   - Docker + docker compose on diane-test
//   - diane-test-node image or Go toolchain on the host to build mock binaries
//   - .env file at /opt/diane/docker/.env with MEMORY_* vars
//
// Run:
//
//	cd server && go test -v -count=1 -run TestAutoUpgrade ./memorytest/
//
//go:build integration

package memorytest

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	testHost          = "root@diane-test"
	dockerDir         = "/opt/diane/docker"
	projectRoot       = "/opt/diane"
	mockVersionTag    = "v999.0.0-test"
	oldVersionTag     = "v1.0.0-old"
	dockerImageTag    = "diane-upgrade-test"
	testContainerName = "diane-upgrade-test"
	mockServerPort    = "19999"
)

// ---------------------------------------------------------------------------
// SSH helpers
// ---------------------------------------------------------------------------

func sshOut(ctx context.Context, t *testing.T, format string, args ...any) string {
	t.Helper()
	cmd := fmt.Sprintf(format, args...)
	c := exec.CommandContext(ctx, "ssh", testHost, cmd)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Logf("ssh: %s\n%s", cmd, string(out))
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// Test
// ---------------------------------------------------------------------------

func TestAutoUpgrade_Docker(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Docker upgrade test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	mockDir := "/tmp/diane-upgrade-test"

	// ── 0. Clean up any leftovers ──
	t.Log("=== 0. Cleanup ===")
	sshOut(ctx, t, "docker rm -f %s 2>/dev/null; true", testContainerName)
	sshOut(ctx, t, "pkill -f 'python3.*mock-server' 2>/dev/null; true")
	sshOut(ctx, t, "rm -rf %s /opt/diane/diane 2>/dev/null; true", mockDir)

	// ── 1. Build mock binaries ──
	t.Log("=== 1. Build mock binaries ===")

	// Old-version binary (what the container will run initially)
	sshOut(ctx, t,
		`mkdir -p %[1]s && cat > %[1]s/build-old.go << 'GOEOF'
package main

import "fmt"

func main() {
	fmt.Println("diane CLI: %[2]s")
}
GOEOF
go build -o %[1]s/diane-old %[1]s/build-old.go`,
		mockDir, oldVersionTag)

	// New-version binary (the "upgrade" target)
	sshOut(ctx, t,
		`cat > %[1]s/build-new.go << 'GOEOF'
package main

import "fmt"

func main() {
	fmt.Println("diane CLI: %[2]s")
}
GOEOF
go build -o %[1]s/diane-new %[1]s/build-new.go`,
		mockDir, mockVersionTag)

	t.Log("  ✅ Mock binaries built")

	// Create tar.gz for the release
	sshOut(ctx, t, `cd %[1]s && cp diane-new diane && tar czf diane-linux-arm64.tar.gz diane && rm diane`, mockDir)
	t.Log("  ✅ Release tar.gz created")

	// ── 2. Start mock HTTP server ──
	t.Log("=== 2. Start mock HTTP server ===")

	mockScript := fmt.Sprintf(`import http.server, json, os

RELEASE_DATA = json.dumps({
    "tag_name": "%[1]s",
    "assets": [{"name": "diane-linux-arm64.tar.gz",
                 "browser_download_url": "http://%%s:%%s/download/diane-linux-arm64.tar.gz"}]
})

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.endswith("/releases/latest"):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(RELEASE_DATA.encode())
        elif self.path.endswith("diane-linux-arm64.tar.gz"):
            self.send_response(200)
            self.send_header("Content-Type", "application/gzip")
            self.end_headers()
            with open("%[2]s/diane-linux-arm64.tar.gz", "rb") as f:
                self.wfile.write(f.read())
        else:
            self.send_response(404)
            self.end_headers()

http.server.HTTPServer(("0.0.0.0", %[3]s), Handler).serve_forever()
`, mockVersionTag, mockDir, mockServerPort)

	sshOut(ctx, t,
		`cat > %s/mock-server.py << 'PYEOF'
%s
PYEOF
nohup python3 %s/mock-server.py > %s/mock-server.log 2>&1 &
echo "PID: $!"`,
		mockDir, mockScript, mockDir, mockDir)

	time.Sleep(1 * time.Second)

	// Verify mock server responds
	resp := sshOut(ctx, t, "curl -sf http://localhost:%s/repos/emergent-company/diane/releases/latest 2>&1", mockServerPort)
	if !strings.Contains(resp, mockVersionTag) {
		t.Fatalf("Mock server not responding. Got: %s", resp)
	}
	t.Log("  ✅ Mock server running")

	// ── 3. Build Docker image with old version ──
	t.Log("=== 3. Build Docker image ===")

	// Copy old binary to where Dockerfile.local expects it
	sshOut(ctx, t, "cp %s/diane-old %s/diane", mockDir, projectRoot)

	// Build from /opt/diane with -f docker/Dockerfile.local
	buildOut := sshOut(ctx, t,
		`cd %s && docker build -t %s \
			-f docker/Dockerfile.local \
			. 2>&1`,
		projectRoot, dockerImageTag)
	if strings.Contains(buildOut, "ERROR") || strings.Contains(buildOut, "failed") {
		t.Fatalf("Docker build failed:\n%s", buildOut)
	}
	t.Log("  ✅ Docker image built")

	// ── 4. Start the container ──
	t.Log("=== 4. Start container ===")

	// Read env vars from .env
	envOut := sshOut(ctx, t,
		`cd %s && grep -E '^MEMORY_' .env | sed 's/^/-e /' | tr '\n' ' '`, dockerDir)
	envFlags := strings.TrimSpace(envOut)
	t.Logf("  Env flags: %s", envFlags)

	// Get the Docker bridge gateway IP (host machine from container's perspective)
	gwIP := strings.TrimSpace(sshOut(ctx, t,
		`ip route show default | awk '{print $3}'`))
	t.Logf("  Docker gateway IP: %s", gwIP)

	// Start the container
	sshOut(ctx, t,
		`docker rm -f %s 2>/dev/null; true && \
		docker run -d --name %s --rm \
			%s \
			-e DIANE_UPGRADE_API_URL=http://%s:%s \
			-e UPGRADE_CHECK_INTERVAL=5s \
			-e DIANE_MODE=master \
			-e DIANE_INSTANCE_ID=upgrade-test \
			-e DIANE_API_PORT=8890 \
			-p 18890:8890 \
			--add-host host.docker.internal:%s \
			%s 2>&1`,
		testContainerName, testContainerName,
		envFlags,
		gwIP, mockServerPort,
		gwIP,
		dockerImageTag)

	// Wait for container health
	t.Log("  Waiting for container to become healthy...")
	healthy := false
	for i := 0; i < 30; i++ {
		status := strings.TrimSpace(sshOut(ctx, t,
			`docker inspect --format='{{.State.Health.Status}}' %s 2>/dev/null || echo "not-found"`,
			testContainerName))
		if status == "healthy" {
			healthy = true
			break
		}
		if status == "not-found" {
			// Check logs
			logs := sshOut(ctx, t, `docker logs %s --tail=10 2>&1`, testContainerName)
			t.Fatalf("Container not found. Logs:\n%s", logs)
		}
		time.Sleep(2 * time.Second)
	}
	if !healthy {
		logs := sshOut(ctx, t, `docker logs %s --tail=30 2>&1`, testContainerName)
		t.Fatalf("Container didn't become healthy. Logs:\n%s", logs)
	}
	t.Log("  ✅ Container healthy")

	// ── 5. Verify initial version ──
	ver := strings.TrimSpace(sshOut(ctx, t,
		`docker exec %s diane version 2>&1`, testContainerName))
	t.Logf("  Initial version: %s", ver)

	if !strings.Contains(ver, oldVersionTag) {
		t.Fatalf("Expected old version %q, got %q", oldVersionTag, ver)
	}
	t.Log("  ✅ Running old version")

	// ── 6. Wait for auto-upgrade ──
	t.Log("=== 5. Wait for auto-upgrade ===")
	t.Log("  Checking every 5s for binary swap...")

	var upgraded bool
	for i := 0; i < 24; i++ { // 2 minutes max
		time.Sleep(5 * time.Second)

		// Check if ~/.diane/bin/diane exists (upgrade downloads from mock)
		exists := strings.TrimSpace(sshOut(ctx, t,
			`docker exec %s sh -c 'test -f ~diane/.diane/bin/diane && echo "EXISTS" || echo "NOT_FOUND"' 2>&1`,
			testContainerName))

		if exists == "EXISTS" {
			// Verify version of swapped binary
			newVer := strings.TrimSpace(sshOut(ctx, t,
				`docker exec %s sh -c '~diane/.diane/bin/diane version' 2>&1`,
				testContainerName))
			t.Logf("  Swapped binary version: %s", newVer)

			if strings.Contains(newVer, mockVersionTag) {
				t.Log("  ✅ Auto-upgrade completed — binary swapped with new version!")
				upgraded = true
				break
			}
		}

		// Check logs for upgrade progress
		logCheck := sshOut(ctx, t,
			`docker logs %s --tail=3 2>&1 | grep -i 'UPGRADE\|download\|new' || echo "(no upgrade log)"`,
			testContainerName)
		t.Logf("  [%ds] %s", (i+1)*5, strings.TrimSpace(logCheck))
	}

	if !upgraded {
		fullLogs := sshOut(ctx, t, `docker logs %s --tail=50 2>&1`, testContainerName)
		t.Fatalf("❌ Auto-upgrade did not complete.\n\nContainer logs:\n%s", fullLogs)
	}

	// ── 7. Cleanup ──
	t.Log("=== 6. Cleanup ===")
	sshOut(ctx, t, "docker rm -f %s 2>/dev/null; true", testContainerName)
	sshOut(ctx, t, "pkill -f 'python3.*mock-server' 2>/dev/null; true")
	sshOut(ctx, t, "rm -rf %s /opt/diane/diane 2>/dev/null; true", mockDir)
	t.Log("  ✅ Cleanup done")
}
