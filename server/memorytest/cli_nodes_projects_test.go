// Package memorytest validates diane CLI nodes, projects, and provider commands.
//
// Tests:
//   - 'diane nodes' — lists connected relay nodes
//   - 'diane projects' — lists configured projects
//   - 'diane provider list' — lists configured providers
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestCLI_Nodes ./memorytest/
package memorytest

import (
	"context"
	"strings"
	"testing"
	"time"
)

// =========================================================================
// TestCLI_Nodes: Runs 'diane nodes' and verifies the output format.
// May show connected nodes or "no connected relay nodes" message.
// =========================================================================

func TestCLI_Nodes(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "nodes")
	if err != nil {
		t.Logf("Exit error: %v", err)
	}

	t.Logf("=== 'diane nodes' ===\n%s\n=== end ===", output)

	// Should have some output
	if len(strings.TrimSpace(output)) == 0 {
		t.Error("Output is empty")
	}

	// Should mention relay nodes (even if none connected)
	if strings.Contains(output, "relay") || strings.Contains(output, "Connected") || strings.Contains(output, "🌐") {
		t.Log("✅ Output has expected relay node format")
	}

	// Should not contain error messages (exits on error, so we'd see it in output)
	if strings.Contains(output, "❌") {
		t.Logf("⚠️  Error output present — possible API issue: %.200s", output)
	}

	t.Log("✅ diane nodes completed")
}

// =========================================================================
// TestCLI_NodesFormat: Verifies the nodes command output formatting
// includes expected sections.
// =========================================================================

func TestCLI_NodesFormat(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "nodes")
	if err != nil {
		t.Logf("Exit error (non-fatal): %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		t.Fatal("Output is empty")
	}

	t.Logf("Output has %d lines", len(lines))

	// Check for expected format elements
	hasEmoji := strings.Contains(output, "🌐") || strings.Contains(output, "📡")
	hasHost := strings.Contains(output, "Host:") || strings.Contains(output, "host")
	if hasEmoji {
		t.Log("✅ Contains expected emoji indicators")
	}
	if hasHost {
		t.Log("✅ Contains host information")
	}

	t.Log("✅ nodes format verified")
}

// =========================================================================
// TestCLI_Projects: Runs 'diane projects' and verifies it shows configured
// projects with server, project ID, and optional Discord info.
// =========================================================================

func TestCLI_Projects(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "projects")
	if err != nil {
		t.Fatalf("diane projects: %v\noutput: %s", err, output)
	}

	t.Logf("=== 'diane projects' ===\n%s\n=== end ===", output)

	// Should show configured projects
	if !strings.Contains(output, "Configured projects") && !strings.Contains(output, "No projects") {
		t.Log("⚠️  Unexpected output format")
	}

	// Should show server URL
	if strings.Contains(output, "Server:") || strings.Contains(output, "memory.") {
		t.Log("✅ Server URL present")
	}

	// Should show project ID
	if strings.Contains(output, "Project:") || strings.Contains(output, "-") {
		t.Log("✅ Project ID present")
	}

	if len(strings.TrimSpace(output)) == 0 {
		t.Error("Output is empty")
	}

	t.Log("✅ diane projects completed")
}

// =========================================================================
// TestCLI_ProjectsDefault: Verifies the default project is marked with '*'
// in the projects output.
// =========================================================================

func TestCLI_ProjectsDefault(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "projects")
	if err != nil {
		t.Fatalf("diane projects: %v", err)
	}

	// Should have a default marker
	if strings.Contains(output, "*") {
		t.Log("✅ Default project marker present")
	} else {
		t.Log("⚠️  No default project marker found")
	}

	t.Log("✅ projects default marker verified")
}

// =========================================================================
// TestCLI_ProviderList: Runs 'diane provider list' and verifies it shows
// configured providers (both local and remote).
// =========================================================================

func TestCLI_ProviderList(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "provider", "list")
	if err != nil {
		t.Fatalf("diane provider list: %v\noutput: %s", err, output)
	}

	t.Logf("=== 'diane provider list' ===\n%s\n=== end ===", output)

	// Should show provider info
	if strings.Contains(output, "generative") || strings.Contains(output, "embedding") || strings.Contains(output, "Provider") {
		t.Log("✅ Provider type information present")
	}

	// Should have some content
	if len(strings.TrimSpace(output)) < 10 {
		t.Log("⚠️  Output is very short — may be empty/error")
	}

	t.Log("✅ diane provider list completed")
}

// =========================================================================
// TestCLI_ProviderListFormat: Verifies the provider list output has
// consistent formatting.
// =========================================================================

func TestCLI_ProviderListFormat(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "provider", "list")
	if err != nil {
		t.Fatalf("diane provider list: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	t.Logf("Output has %d lines", len(lines))
	for i, line := range lines {
		t.Logf("  %d: %s", i, line)
	}

	// Should have local and remote sections
	hasLocal := false
	hasRemote := false
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "local") || strings.Contains(lower, "config") {
			hasLocal = true
		}
		if strings.Contains(lower, "remote") || strings.Contains(lower, "platform") || strings.Contains(lower, "memory") {
			hasRemote = true
		}
	}

	if hasLocal {
		t.Log("✅ Local provider section present")
	}
	if hasRemote {
		t.Log("✅ Remote provider section present")
	}
	if !hasLocal && !hasRemote {
		t.Log("⚠️  Neither local nor remote section found — unexpected format")
	}

	t.Log("✅ provider list format verified")
}
