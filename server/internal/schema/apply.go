// Package schema manages embedded schema definitions and applies them
// to the Memory Platform.
//
// Schema files are stored in internal/schema/schemas/ as JSON arrays of
// SchemaDefinition (object types) or RelationshipDefinition (relationship types),
// embedded into the binary at build time via //go:embed.
//
// Two paths are used:
//  1. Update existing object types via SDK SchemaRegistry.UpdateType (works)
//  2. Create new object types + all relationship types via Memory Schema pack API
//
// Usage:
//
//	Apply(ctx, sdkClient, projectID, opts)
package schema

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
var schemaFiles embed.FS

// SchemaDefinition maps to one object type entry (schema registry format).
type SchemaDefinition struct {
	TypeName    string          `json:"type_name"`
	Description string          `json:"description"`
	Namespace   string          `json:"namespace,omitempty"`
	JSONSchema  json.RawMessage `json:"json_schema"`
	Enabled     bool            `json:"enabled"`
}

// RelationshipDefinition maps to one relationship type entry.
type RelationshipDefinition struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	InverseLabel string `json:"inverse_label"`
	Description  string `json:"description,omitempty"`
	SourceType   string `json:"source_type"`
	TargetType   string `json:"target_type"`
}

// packObjectType is the format the Memory Schema pack API expects.
type packObjectType struct {
	Name        string          `json:"name"`
	Label       string          `json:"label,omitempty"`
	Description string          `json:"description,omitempty"`
	Properties  json.RawMessage `json:"properties,omitempty"`
}

// packRelationshipType is the format the Memory Schema pack API expects for relationship types.
type packRelationshipType struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	SourceType  string `json:"sourceType,omitempty"`
	TargetType  string `json:"targetType,omitempty"`
}

// packPayload is the complete Memory Schema pack request body.
type packPayload struct {
	Name                     string                `json:"name"`
	Version                  string                `json:"version"`
	Description              string                `json:"description,omitempty"`
	ObjectTypeSchemas        []packObjectType      `json:"object_type_schemas"`
	RelationshipTypeSchemas  []packRelationshipType `json:"relationship_type_schemas,omitempty"`
}

func (d *SchemaDefinition) toPackType() packObjectType {
	var schema struct {
		Label       string          `json:"label"`
		Properties  json.RawMessage `json:"properties"`
	}
	json.Unmarshal(d.JSONSchema, &schema)
	return packObjectType{
		Name:        d.TypeName,
		Label:       schema.Label,
		Description: d.Description,
		Properties:  schema.Properties,
	}
}

// ---------------------------------------------------------------------------
// ApplyOptions and Result
// ---------------------------------------------------------------------------

type ApplyOptions struct {
	DryRun    bool
	ServerURL string
}

type Result struct {
	TypeName string
	Action   string
	Error    error
}

// ---------------------------------------------------------------------------
// Apply — main entry point
// ---------------------------------------------------------------------------

const packName = "diane-personal-schema"
const packVersion = "2.0.0"

func Apply(ctx context.Context, client *sdk.Client, projectID string, opts *ApplyOptions) ([]Result, error) {
	if opts == nil {
		opts = &ApplyOptions{}
	}
	var results []Result

	allDefs, err := parseObjectTypes()
	if err != nil {
		return nil, fmt.Errorf("parse object types: %w", err)
	}
	allRels, err := parseRelationshipTypes()
	if err != nil {
		return nil, fmt.Errorf("parse relationship types: %w", err)
	}

	existing, err := fetchExistingTypes(ctx, client, projectID)
	if err != nil {
		return nil, fmt.Errorf("fetch existing types: %w", err)
	}
	existingByName := make(map[string]*srsdk.SchemaRegistryEntry, len(existing))
	for _, e := range existing {
		entry := e
		existingByName[e.Type] = &entry
	}

	// Step 1: Update existing types (SDK UpdateType endpoint works)
	for _, def := range allDefs {
		existingEntry := existingByName[def.TypeName]
		if existingEntry == nil {
			continue
		}
		r := updateExistingType(ctx, client, projectID, def, existingEntry, opts)
		results = append(results, r)
	}

	// Step 2: Identify new types needing creation
	var newDefs []SchemaDefinition
	for _, def := range allDefs {
		if existingByName[def.TypeName] == nil {
			newDefs = append(newDefs, def)
		}
	}

	// Step 3: Create new types + relationships via Memory Schema pack
	if len(newDefs) > 0 || len(allRels) > 0 {
		packResults := applyViaPack(ctx, client, projectID, newDefs, allRels, opts)
		results = append(results, packResults...)
	}

	// Step 4: Report unchanged
	for _, def := range allDefs {
		existingEntry := existingByName[def.TypeName]
		if existingEntry != nil && !typeHasChanged(existingEntry, &def) {
			results = append(results, Result{TypeName: def.TypeName, Action: "unchanged"})
		}
	}

	return results, nil
}

// ---------------------------------------------------------------------------
// Update existing types via SDK
// ---------------------------------------------------------------------------

func updateExistingType(
	ctx context.Context, client *sdk.Client, projectID string,
	def SchemaDefinition, existing *srsdk.SchemaRegistryEntry, opts *ApplyOptions,
) Result {
	if !typeHasChanged(existing, &def) {
		return Result{TypeName: def.TypeName, Action: "unchanged"}
	}
	if opts.DryRun {
		log.Printf("[schema] DRY-RUN: would update type %q", def.TypeName)
		return Result{TypeName: def.TypeName, Action: "updated"}
	}
	enabled := def.Enabled
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

// ---------------------------------------------------------------------------
// Create new types via Memory Schema pack
// ---------------------------------------------------------------------------

func applyViaPack(
	ctx context.Context, client *sdk.Client, projectID string,
	newDefs []SchemaDefinition, allRels []RelationshipDefinition, opts *ApplyOptions,
) []Result {
	if opts.DryRun {
		var results []Result
		for _, def := range newDefs {
			log.Printf("[schema] DRY-RUN: would create type %q via pack", def.TypeName)
			results = append(results, Result{TypeName: def.TypeName, Action: "created"})
		}
		for _, rel := range allRels {
			log.Printf("[schema] DRY-RUN: would create relationship type %q via pack", rel.Name)
			results = append(results, Result{TypeName: rel.Name, Action: "created"})
		}
		return results
	}

	if opts.ServerURL == "" {
		log.Println("[schema] ServerURL not set, skipping pack creation")
		var results []Result
		err := fmt.Errorf("ServerURL not set")
		for _, def := range newDefs {
			results = append(results, Result{TypeName: def.TypeName, Action: "error", Error: err})
		}
		for _, rel := range allRels {
			results = append(results, Result{TypeName: rel.Name, Action: "error", Error: err})
		}
		return results
	}

	baseURL := strings.TrimRight(opts.ServerURL, "/")

	// Build pack payload
	packObjTypes := make([]packObjectType, len(newDefs))
	for i, def := range newDefs {
		packObjTypes[i] = def.toPackType()
	}
	packRelTypes := make([]packRelationshipType, len(allRels))
	for i, rel := range allRels {
		packRelTypes[i] = packRelationshipType{
			Name: rel.Name, Label: rel.Label, Description: rel.Description,
			SourceType: rel.SourceType, TargetType: rel.TargetType,
		}
	}

	payload := packPayload{
		Name:                    packName,
		Version:                 packVersion,
		Description:             "Diane personal knowledge graph schema",
		ObjectTypeSchemas:       packObjTypes,
		RelationshipTypeSchemas: packRelTypes,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		var results []Result
		err = fmt.Errorf("marshal pack: %w", err)
		for _, def := range newDefs {
			results = append(results, Result{TypeName: def.TypeName, Action: "error", Error: err})
		}
		for _, rel := range allRels {
			results = append(results, Result{TypeName: rel.Name, Action: "error", Error: err})
		}
		return results
	}

	// Post the pack
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/schemas", bytes.NewReader(body))
	if err != nil {
		var results []Result
		err = fmt.Errorf("create pack request: %w", err)
		for _, def := range newDefs {
			results = append(results, Result{TypeName: def.TypeName, Action: "error", Error: err})
		}
		for _, rel := range allRels {
			results = append(results, Result{TypeName: rel.Name, Action: "error", Error: err})
		}
		return results
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(ctx, req)
	if err != nil {
		var results []Result
		err = fmt.Errorf("http post pack: %w", err)
		for _, def := range newDefs {
			results = append(results, Result{TypeName: def.TypeName, Action: "error", Error: err})
		}
		for _, rel := range allRels {
			results = append(results, Result{TypeName: rel.Name, Action: "error", Error: err})
		}
		return results
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 201 {
		var results []Result
		err = fmt.Errorf("create pack: HTTP %d: %s", resp.StatusCode, string(respBody))
		for _, def := range newDefs {
			results = append(results, Result{TypeName: def.TypeName, Action: "error", Error: err})
		}
		for _, rel := range allRels {
			results = append(results, Result{TypeName: rel.Name, Action: "error", Error: err})
		}
		return results
	}

	var packResp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(respBody, &packResp)
	log.Printf("[schema] Created pack %q (id=%s)", packName, packResp.ID)

	// Assign the pack to the project
	assignPayload := map[string]interface{}{
		"schema_id": packResp.ID,
		"merge":     true,
		"force":     true,
	}
	assignBody, _ := json.Marshal(assignPayload)

	assignReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/schemas/projects/"+projectID+"/assign",
		bytes.NewReader(assignBody))
	assignReq.Header.Set("Content-Type", "application/json")

	assignResp, err := client.Do(ctx, assignReq)
	if err != nil {
		var results []Result
		err = fmt.Errorf("assign pack: %w", err)
		for _, def := range newDefs {
			results = append(results, Result{TypeName: def.TypeName, Action: "error", Error: err})
		}
		for _, rel := range allRels {
			results = append(results, Result{TypeName: rel.Name, Action: "error", Error: err})
		}
		return results
	}
	defer assignResp.Body.Close()
	assignRespBody, _ := io.ReadAll(assignResp.Body)

	if assignResp.StatusCode >= 400 {
		var results []Result
		err = fmt.Errorf("assign pack: HTTP %d: %s", assignResp.StatusCode, string(assignRespBody))
		for _, def := range newDefs {
			results = append(results, Result{TypeName: def.TypeName, Action: "error", Error: err})
		}
		for _, rel := range allRels {
			results = append(results, Result{TypeName: rel.Name, Action: "error", Error: err})
		}
		return results
	}

	log.Printf("[schema] Pack %q assigned successfully", packName)
	return parseAssignResponse(assignRespBody, newDefs, allRels)
}

func parseAssignResponse(body []byte, newDefs []SchemaDefinition, allRels []RelationshipDefinition) []Result {
	var assignResult struct {
		InstalledTypes []string `json:"installed_types"`
		MergedTypes    []string `json:"merged_types"`
	}
	json.Unmarshal(body, &assignResult)

	created := make(map[string]bool)
	for _, name := range assignResult.InstalledTypes {
		created[name] = true
	}

	var results []Result
	for _, def := range newDefs {
		if created[def.TypeName] {
			results = append(results, Result{TypeName: def.TypeName, Action: "created"})
		} else {
			results = append(results, Result{TypeName: def.TypeName, Action: "updated"})
		}
	}
	for _, rel := range allRels {
		results = append(results, Result{TypeName: rel.Name, Action: "created"})
	}
	return results
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

func parseObjectTypes() ([]SchemaDefinition, error) {
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
		if !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, "relationships") {
			continue
		}
		filenames = append(filenames, name)
	}
	sort.Strings(filenames)
	var all []SchemaDefinition
	for _, name := range filenames {
		data, err := schemaFiles.ReadFile("schemas/" + name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		var defs []SchemaDefinition
		if err := json.Unmarshal(data, &defs); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		all = append(all, defs...)
		log.Printf("[schema] Loaded %s: %d object type(s)", name, len(defs))
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no object type schema files found")
	}
	return all, nil
}

func parseRelationshipTypes() ([]RelationshipDefinition, error) {
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
		if !strings.HasSuffix(name, ".json") || !strings.HasPrefix(name, "relationships") {
			continue
		}
		filenames = append(filenames, name)
	}
	sort.Strings(filenames)
	var all []RelationshipDefinition
	for _, name := range filenames {
		data, err := schemaFiles.ReadFile("schemas/" + name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		var defs []RelationshipDefinition
		if err := json.Unmarshal(data, &defs); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		all = append(all, defs...)
		log.Printf("[schema] Loaded %s: %d relationship type(s)", name, len(defs))
	}
	return all, nil
}

// ---------------------------------------------------------------------------
// Other helpers
// ---------------------------------------------------------------------------

func fetchExistingTypes(ctx context.Context, client *sdk.Client, projectID string) ([]srsdk.SchemaRegistryEntry, error) {
	return client.SchemaRegistry.GetProjectTypes(ctx, projectID, &srsdk.ListTypesOptions{
		EnabledOnly: boolPtr(false),
	})
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
	return normalizeJSON(existing.JSONSchema) != normalizeJSON(def.JSONSchema)
}

func normalizeJSON(data []byte) string {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	normalized, _ := json.Marshal(v)
	return string(normalized)
}

func boolPtr(v bool) *bool {
	return &v
}

// ── CLI display ──

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
