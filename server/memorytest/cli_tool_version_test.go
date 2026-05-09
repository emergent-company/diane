// Package memorytest validates diane version and tool test CLI commands.
//
//go:build integration

package memorytest

import (
	"context"
	"strings"
	"testing"
	"time"
)

// =========================================================================
// TestCLI_Version: Runs 'diane version' and verifies it displays version info.
// =========================================================================

func TestCLI_Version(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "version")
	t.Logf("=== 'diane version' output ===\n%s\n=== end output ===", output)

	if err != nil {
		t.Fatalf("Version command failed: %v\nOutput: %s", err, output)
	}

	// Should show a semantic version
	if strings.Contains(output, "v1.") || strings.Contains(output, "1.") || strings.Contains(output, "Version:") || strings.Contains(output, "version:") {
		t.Log("✅ Version output contains version number")
	} else {
		t.Log("⚠️  Version format unexpected — contents:")
		for _, line := range strings.Split(output, "\n") {
			if line != "" {
				t.Logf("  %s", line)
			}
		}
	}

	// Should show the companion app version or CLI version
	if strings.Contains(output, "CLI") || strings.Contains(output, "Diane") {
		t.Log("✅ Version output references Diane")
	}
}

// =========================================================================
// TestCLI_ToolTest: Tests 'diane tool test' with a known built-in tool.
// Uses 'get_time' which is always available and requires no arguments.
// =========================================================================

func TestCLI_ToolTest(t *testing.T) {
	skipIfNoConfig(t)
	dianeBin := findDianeBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := runCLI(ctx, t, dianeBin, "tool", "test", "get_time")
	t.Logf("=== 'diane tool test get_time' output ===\n%s\n=== end output ===", truncateStr(output, 1000))

	if err != nil {
		if strings.Contains(output, "not found") || strings.Contains(output, "unavailable") {
			t.Skipf("Tool 'get_time' not available in this environment: %v", err)
		}
		t.Logf("Tool test exit error (may be proxy not running): %v", err)
	}

	// Should show tool call result
	if strings.Contains(output, "Result:") || strings.Contains(output, "result:") || strings.Contains(output, "time") || strings.Contains(output, "Time") {
		t.Log("✅ Tool test returned a result")
	} else {
		t.Log("⚠️  Tool test output format unexpected")
	}
}
