// Package schema manages embedded schema definitions and applies them
// to the Memory Platform's Schema Registry.
//
// Schema files are stored in internal/schema/schemas/ as JSON arrays of
// SchemaDefinition, embedded into the binary at build time via //go:embed. This makes them
// part of the Diane release — always available, always in sync.
//
// Usage:
//
//	Apply(ctx, sdkClient, projectID)  // create or update all embedded schemas
package schema

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	srsdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemaregistry"
)

// ---------------------------------------------------------------------------
// Embedded schema definitions
// ---------------------------------------------------------------------------

//go:embed schemas/*.json
var schemaFiles embed.FS // keyed by filename, e.g. "schemas/diane-personal-schema.json"

// SchemaDefinition maps exactly to one entry in a schema JSON file.
type SchemaDefinition struct {
	TypeName    string          `json:"type_name"`
	Description string          `json:"description"`
	JSONSchema  json.RawMessage `json:"json_schema"`
	Enabled     bool            `json:"enabled"`
}

// schemaFile holds the parsed contents of one schema JSON file.
type schemaFile struct {
	Name  string              // filename without extension
	Types []SchemaDefinition
}

// ---------------------------------------------------------------------------
// Apply — main entry point
// ---------------------------------------------------------------------------

// ApplyOptions controls how Apply behaves.
type ApplyOptions struct {
	DryRun bool
}

// Result describes the outcome of applying a single schema type.
type Result struct {
	TypeName string // name of the schema type
	Action   string // "created", "updated", "unchanged", "error"
	Error    error  // nil unless Action is "error"
}

// Apply creates or updates all embedded schema types on the Memory Platform.
// It is idempotent — existing types are updated in-place if their definition changed.
func Apply(ctx context.Context, client *sdk.Client, projectID string, opts *ApplyOptions) ([]Result, error) {
	if opts == nil {
		opts = &ApplyOptions{}
	}

	files, err := parseSchemaFiles()
	if err != nil {
		return nil, fmt.Errorf("parse embedded schemas: %w", err)
	}

	existing, err := fetchExistingTypes(ctx, client, projectID)
	if err != nil {
		return nil, fmt.Errorf("fetch existing schemas: %w", err)
	}
	existingByName := make(map[string]*srsdk.SchemaRegistryEntry, len(existing))
	for _, e := range existing {
		entry := e
		existingByName[e.Type] = &entry
	}

	var results []Result
	for _, file := range files {
		for _, def := range file.Types {
			r := applyOne(ctx, client, projectID, def, existingByName[def.TypeName], opts)
			results = append(results, r)
		}
	}

	return results, nil
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func applyOne(
	ctx context.Context,
	client *sdk.Client,
	projectID string,
	def SchemaDefinition,
	existing *srsdk.SchemaRegistryEntry,
	opts *ApplyOptions,
) Result {
	enabled := def.Enabled

	if existing == nil {
		if opts.DryRun {
			log.Printf("[schema] DRY-RUN: would create type %q", def.TypeName)
			return Result{TypeName: def.TypeName, Action: "created"}
		}

		_, err := client.SchemaRegistry.CreateType(ctx, projectID, &srsdk.CreateTypeRequest{
			TypeName:    def.TypeName,
			Description: &def.Description,
			JSONSchema:  def.JSONSchema,
			Enabled:     &enabled,
		})
		if err != nil {
			return Result{TypeName: def.TypeName, Action: "error", Error: fmt.Errorf("create %q: %w", def.TypeName, err)}
		}
		log.Printf("[schema] Created type %q", def.TypeName)
		return Result{TypeName: def.TypeName, Action: "created"}
	}

	if !typeHasChanged(existing, &def) {
		return Result{TypeName: def.TypeName, Action: "unchanged"}
	}

	if opts.DryRun {
		log.Printf("[schema] DRY-RUN: would update type %q", def.TypeName)
		return Result{TypeName: def.TypeName, Action: "updated"}
	}

	_, err := client.SchemaRegistry.UpdateType(ctx, projectID, def.TypeName, &srsdk.UpdateTypeRequest{
		Description: &def.Description,
		JSONSchema:  def.JSONSchema,
		Enabled:     &enabled,
	})
	if err != nil {
		return Result{TypeName: def.TypeName, Action: "error", Error: fmt.Errorf("update %q: %w", def.TypeName, err)}
	}
	log.Printf("[schema] Updated type %q", def.TypeName)
	return Result{TypeName: def.TypeName, Action: "updated"}
}

func parseSchemaFiles() ([]schemaFile, error) {
	dirEntries, err := schemaFiles.ReadDir("schemas")
	if err != nil {
		return nil, fmt.Errorf("read embedded schemas dir: %w", err)
	}

	var filenames []string
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		filenames = append(filenames, name)
	}
	sort.Strings(filenames)

	var files []schemaFile
	for _, name := range filenames {
		data, err := schemaFiles.ReadFile("schemas/" + name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		var defs []SchemaDefinition
		if err := json.Unmarshal(data, &defs); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}

		baseName := strings.TrimSuffix(name, ".json")
		files = append(files, schemaFile{
			Name:  baseName,
			Types: defs,
		})
		log.Printf("[schema] Loaded %s: %d type(s)", name, len(defs))
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no JSON schema files found in embedded schemas/")
	}

	return files, nil
}

func fetchExistingTypes(ctx context.Context, client *sdk.Client, projectID string) ([]srsdk.SchemaRegistryEntry, error) {
	types, err := client.SchemaRegistry.GetProjectTypes(ctx, projectID, &srsdk.ListTypesOptions{
		EnabledOnly: boolPtr(false),
	})
	if err != nil {
		return nil, err
	}
	return types, nil
}

func typeHasChanged(existing *srsdk.SchemaRegistryEntry, def *SchemaDefinition) bool {
	if existing == nil {
		return true
	}

	if def.Description != "" {
		if existing.Description == nil || *existing.Description != def.Description {
			return true
		}
	}

	if existing.Enabled != def.Enabled {
		return true
	}

	existingJSON := normalizeJSON(existing.JSONSchema)
	localJSON := normalizeJSON(def.JSONSchema)
	return existingJSON != localJSON
}

func normalizeJSON(data []byte) string {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	normalized, err := json.Marshal(v)
	if err != nil {
		return string(data)
	}
	return string(normalized)
}

func boolPtr(v bool) *bool {
	return &v
}

// ── CLI display helpers ──

// PrintResults formats schema apply results for CLI output.
func PrintResults(results []Result, duration time.Duration) {
	var created, updated, unchanged, errors int
	for _, r := range results {
		switch r.Action {
		case "created":
			created++
			fmt.Printf("  ✅ %s (created)\n", r.TypeName)
		case "updated":
			updated++
			fmt.Printf("  🔄 %s (updated)\n", r.TypeName)
		case "unchanged":
			unchanged++
		case "error":
			errors++
			fmt.Printf("  ❌ %s: %v\n", r.TypeName, r.Error)
		}
	}
	fmt.Println()
	fmt.Printf("  Created: %d | Updated: %d | Unchanged: %d | Errors: %d (%v)\n",
		created, updated, unchanged, errors, duration.Round(time.Millisecond))
}
