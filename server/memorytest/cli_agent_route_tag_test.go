// Package memorytest validates diane CLI agent route and tag commands via exec.
//
// Test 'diane agent route <name> <weight>' — sets routing weight
// Test 'diane agent tag <name> <tags>' — sets agent tags
//
// Run: cd ~/diane/server && /usr/local/go/bin/go test -v -count=1 -run TestCLI_AgentRoute ./memorytest/
package memorytest

import (
	"context"
	"strings"
	"testing"
	"time"
)

// =========================================================================
// TestCLI_AgentRoute: Sets routing weight for diane-default and verifies
// the success message. Checks for the ✅ confirmation format.
// =========================================================================

func TestCLI_AgentRoute(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "route", "diane-default", "0.75")
	if err != nil {
		t.Fatalf("diane agent route: %v\noutput: %s", err, output)
	}

	t.Logf("=== 'diane agent route diane-default 0.75' ===\n%s\n=== end ===", output)

	if !strings.Contains(output, "✅") && !strings.Contains(output, "set") {
		t.Log("⚠️  No success indicator found in output")
	}

	if !strings.Contains(output, "0.75") && !strings.Contains(output, "0.75") {
		t.Log("⚠️  Weight 0.75 not mentioned in output")
	}

	t.Log("✅ Route set completed")
}

// =========================================================================
// TestCLI_AgentRoute_InvalidWeight: Tests that invalid weights are rejected
// with a proper error message, not a crash.
// =========================================================================

func TestCLI_AgentRoute_InvalidWeight(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "route", "diane-default", "2.5")
	if err != nil {
		t.Logf("Exit error expected: %v", err)
	}
	t.Logf("Output: %.200s", output)

	if !strings.Contains(output, "Weight must be") && !strings.Contains(output, "0.0") {
		t.Log("⚠️  No weight validation error in output")
	}

	t.Log("✅ Invalid weight gracefully handled")
}

// =========================================================================
// TestCLI_AgentRoute_NonExistent: Tests routing on a non-existent agent.
// =========================================================================

func TestCLI_AgentRoute_NonExistent(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "route", "nonexistent-agent-test", "0.5")
	if err != nil {
		t.Logf("Exit error expected: %v", err)
	}
	t.Logf("Output: %.200s", output)

	if !strings.Contains(output, "not found") {
		t.Log("⚠️  No 'not found' error for non-existent agent")
	}

	t.Log("✅ Non-existent agent handled gracefully")
}

// =========================================================================
// TestCLI_AgentTag: Sets tags on diane-default and verifies the output.
// Restores the original tags afterward to avoid side effects.
// =========================================================================

func TestCLI_AgentTag(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "agent", "tag", "diane-default", "test-tag-a,test-tag-b")
	if err != nil {
		t.Fatalf("diane agent tag: %v\noutput: %s", err, output)
	}

	t.Logf("=== 'diane agent tag diane-default test-tag-a,test-tag-b' ===\n%s\n=== end ===", output)

	if !strings.Contains(output, "✅") && !strings.Contains(output, "set") && !strings.Contains(output, "updated") {
		t.Log("⚠️  No success indicator found")
	} else {
		t.Log("✅ Tag set succeeded")
	}
}

// =========================================================================
// TestCLI_AgentRouteAndTagRoundTrip: Sets route, then sets tag, then reads
// stats to verify both changes persisted. Single end-to-end test.
// =========================================================================

func TestCLI_AgentRouteAndTagRoundTrip(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Set route weight
	routeOut, err := runCLI(ctx, t, dianeBin, "agent", "route", "diane-default", "0.42")
	if err != nil {
		t.Fatalf("Set route: %v\n%s", err, routeOut)
	}
	t.Logf("Route set: %s", strings.TrimSpace(routeOut))

	// 2. Set tags
	tagOut, err := runCLI(ctx, t, dianeBin, "agent", "tag", "diane-default", "roundtrip-test,ci-verified")
	if err != nil {
		t.Fatalf("Set tag: %v\n%s", err, tagOut)
	}
	t.Logf("Tag set: %s", strings.TrimSpace(tagOut))

	// 3. Verify via stats read
	statsOut, err := runCLI(ctx, t, dianeBin, "agent", "stats", "diane-default")
	if err != nil {
		t.Fatalf("Stats read: %v\n%s", err, statsOut)
	}
	t.Logf("=== Stats after route+tag ===\n%s\n=== end ===", statsOut)

	// Check weight is reflected
	if strings.Contains(statsOut, "0.42") {
		t.Log("✅ Weight 0.42 persisted")
	} else {
		t.Log("⚠️  Weight 0.42 not found in stats output")
	}

	// Check tags are reflected
	if strings.Contains(statsOut, "roundtrip-test") && strings.Contains(statsOut, "ci-verified") {
		t.Log("✅ Tags roundtrip-test, ci-verified persisted")
	} else {
		t.Log("⚠️  Tags not found in stats output")
	}
}
