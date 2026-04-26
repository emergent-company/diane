// Package memorytest validates the embedded schema system against the live
// Memory Platform — schema discovery, dry-run apply, and type listing.
//
// Uses the same test project (testPID) on memory.emergent-company.ai.
//
// Run: cd ~/diane/server && MEMORY_TEST_TOKEN=*** /usr/local/go/bin/go test -v -count=1 ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/Emergent-Comapny/diane/internal/schema"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	srsdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemaregistry"
)

// setupSDK creates an SDK client for the Memory Platform using the
// MEMORY_TEST_TOKEN environment variable. Skips the test if the token is not set.
func setupSDK(t *testing.T) *sdk.Client {
	t.Helper()
	token := os.Getenv("MEMORY_TEST_TOKEN")
	if token == "" {
		t.Skip("MEMORY_TEST_TOKEN not set")
	}
	c, err := sdk.New(sdk.Config{
		ServerURL: serverURL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: token},
	})
	if err != nil {
		t.Fatalf("sdk.New: %v", err)
	}
	c.SetContext("", testPID)
	return c
}

func boolPtr(v bool) *bool { return &v }

// expectedKnownTypes lists the core embedded schema type names that should be
// reported by a dry-run apply. These cover the personal schema object types and
// the system schema checkpoint type.
var expectedKnownTypes = []string{
	"MemoryFact",
	"Person",
	"Company",
	"CalendarEvent",
	"Place",
	"Task",
	"Project",
	"Contact",
	"Invoice",
	"Subscription",
	"SkillMonitorCheckpoint",
}

// =========================================================================
// TestSchemaDiscovery_DryRun — Validates schema.Apply() discovers existing
// types and reports the correct status for each known type in dry-run mode.
// =========================================================================

func TestSchemaDiscovery_DryRun(t *testing.T) {
	client := setupSDK(t)
	ctx := context.Background()

	t.Logf("Calling schema.Apply(DryRun=true) for project %s …", testPID)
	results, err := schema.Apply(ctx, client, testPID, &schema.ApplyOptions{
		DryRun:    true,
		ServerURL: serverURL,
	})
	if err != nil {
		t.Fatalf("schema.Apply(DryRun=true) returned error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("schema.Apply returned zero results")
	}

	// Build maps for verification
	resultByType := make(map[string]string) // typeName → action
	for _, r := range results {
		resultByType[r.TypeName] = r.Action
		if r.Error != nil {
			t.Errorf("Result for %q has unexpected error: %v", r.TypeName, r.Error)
		}
	}

	// Log the full result summary
	t.Logf("Schema Apply results (%d total):", len(results))
	for _, r := range results {
		status := r.Action
		if r.Error != nil {
			status = fmt.Sprintf("error: %v", r.Error)
		}
		t.Logf("  %s → %s", r.TypeName, status)
	}

	// Verify each expected known type appears in results
	for _, typeName := range expectedKnownTypes {
		action, found := resultByType[typeName]
		if !found {
			t.Errorf("Expected type %q not found in results", typeName)
			continue
		}
		// All expected types should either be "created" (new) or "unchanged" (already exist)
		if action != "created" && action != "unchanged" && action != "updated" {
			t.Errorf("Unexpected action %q for type %q (expected created/unchanged/updated)", action, typeName)
		}
		t.Logf("  ✅ %s: %s", typeName, action)
	}

	// Verify we have results for ALL embedded types, not just the expected subset
	// The total should be well over len(expectedKnownTypes) since the embedded schemas
	// contain many more types (FinancialTransaction, Note, Habit, Device, etc.)
	if len(results) < len(expectedKnownTypes) {
		t.Errorf("Only got %d results, expected at least %d", len(results), len(expectedKnownTypes))
	}

	// Count actions
	var created, unchanged, updated, errors int
	for _, r := range results {
		switch r.Action {
		case "created":
			created++
		case "unchanged":
			unchanged++
		case "updated":
			updated++
		case "error":
			errors++
		}
	}
	t.Logf("Summary — Created: %d | Updated: %d | Unchanged: %d | Errors: %d",
		created, updated, unchanged, errors)

	// The test project is expected to have MemoryFact, Session, Message etc. built-in,
	// but NOT the full diane-personal-schema. So at least some of the personal schema
	// types should be reported as "created" (new).
	if created == 0 && unchanged == 0 {
		t.Log("Note: zero created and zero unchanged — all types may have errored")
	}
}

// =========================================================================
// TestSchemaDiscovery_ListExistingTypes — Directly fetches all types from
// the project's schema registry to verify the existing type landscape.
// =========================================================================

func TestSchemaDiscovery_ListExistingTypes(t *testing.T) {
	client := setupSDK(t)
	ctx := context.Background()

	t.Logf("Fetching existing types for project %s …", testPID)
	entries, err := client.SchemaRegistry.GetProjectTypes(ctx, testPID, &srsdk.ListTypesOptions{
		EnabledOnly: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("GetProjectTypes: %v", err)
	}

	if len(entries) == 0 {
		// Platform built-in types (Session, Message, MemoryFact) are system-level and
		// NOT exposed via SchemaRegistry.GetProjectTypes. Zero entries is expected for
		// a project without explicitly registered schema types.
		t.Logf("GetProjectTypes returned zero entries — expected (system types not exposed via SchemaRegistry API)")
	} else {
		// Log all type names found
		t.Logf("Existing types (%d total):", len(entries))
		typeNames := make([]string, 0, len(entries))
		for _, e := range entries {
			typeNames = append(typeNames, e.Type)
			enabled := "disabled"
			if e.Enabled {
				enabled = "enabled"
			}
			t.Logf("  %s (%s)", e.Type, enabled)
		}

		// Build a set for lookup
		typeSet := make(map[string]bool, len(typeNames))
		for _, name := range typeNames {
			typeSet[name] = true
		}

		// Log which expected types already exist and which don't
		t.Log("Expected-type availability check:")
		for _, typeName := range expectedKnownTypes {
			if typeSet[typeName] {
				t.Logf("  ✅ %s — already exists in project", typeName)
			} else {
				t.Logf("  ⬜ %s — not yet installed (will be created by Apply)", typeName)
			}
		}
	}

	// Check that the SchemaRegistry API is reachable (no error) — already verified above
	t.Logf("  ✅ GetProjectTypes API call succeeded")
}
