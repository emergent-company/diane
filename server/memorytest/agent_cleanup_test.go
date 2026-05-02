// Package memorytest — shared agent cleanup utility for tests.
//
// Provides a pre-cleanup helper that deletes existing runtime agents with a
// given name prefix before a test creates new ones. This ensures interrupted
// test runs don't leave orphan agents behind on the Memory Platform.
package memorytest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
)

// cleanupTestAgentsByPrefix deletes any existing runtime agents whose name
// starts with the given prefix. Works with both env-var-based auth
// (setupBridge) and config-file-based auth (setupBridgeFromConfig).
//
// Call this BEFORE creating a new runtime agent so that agents left behind
// by interrupted test runs are cleaned up.
func cleanupTestAgentsByPrefix(ctx context.Context, prefix string, t testing.TB) {
	t.Helper()

	token := resolveToken()
	if token == "" {
		t.Logf("  Cleanup: no token available — skipping pre-cleanup for %q", prefix)
		return
	}

	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// List all runtime agents via REST API
	listURL := fmt.Sprintf("%s/api/projects/%s/agents", bridgeTestServer, bridgeTestPID)
	req, err := http.NewRequestWithContext(cleanupCtx, "GET", listURL, nil)
	if err != nil {
		t.Logf("  Cleanup: create list request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("  Cleanup: list agents: %v", err)
		return
	}
	defer resp.Body.Close()

	var listResp struct {
		Success bool `json:"success"`
		Data    []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Logf("  Cleanup: decode list: %v", err)
		return
	}
	if !listResp.Success {
		t.Logf("  Cleanup: list agents returned success=false")
		return
	}

	// Find agents matching prefix
	var toDelete []string
	for _, a := range listResp.Data {
		if len(a.Name) >= len(prefix) && a.Name[:len(prefix)] == prefix {
			toDelete = append(toDelete, a.ID)
		}
	}

	if len(toDelete) == 0 {
		return
	}

	t.Logf("  🧹 Pre-cleanup: deleting %d orphan agent(s) with prefix %q", len(toDelete), prefix)

	for _, aid := range toDelete {
		delURL := fmt.Sprintf("%s/api/projects/%s/agents/%s", bridgeTestServer, bridgeTestPID, aid)
		delReq, err := http.NewRequestWithContext(cleanupCtx, "DELETE", delURL, nil)
		if err != nil {
			t.Logf("  Cleanup: delete %s: %v", aid[:12], err)
			continue
		}
		delReq.Header.Set("Authorization", "Bearer "+token)

		delResp, err := http.DefaultClient.Do(delReq)
		if err != nil {
			t.Logf("  Cleanup: delete %s: %v", aid[:12], err)
			continue
		}
		delResp.Body.Close()
	}
}

// resolveToken tries to find an API token from env var or config file.
func resolveToken() string {
	if tok := os.Getenv("MEMORY_TEST_TOKEN"); tok != "" {
		return tok
	}
	if tok := os.Getenv("DIANE_TOKEN"); tok != "" {
		return tok
	}
	// Try config file
	cfg, err := config.Load()
	if err == nil {
		if pc := cfg.Active(); pc != nil && pc.Token != "" {
			return pc.Token
		}
	}
	return ""
}

// checkErrBody reads the response body and returns it as string for debugging.
func checkErrBody(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	return buf.String()
}
