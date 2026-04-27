package schema

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	srsdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemaregistry"
)

// =========================================================================
// Embedded schema files — validate they parse correctly
// =========================================================================

func TestParseObjectTypes_Success(t *testing.T) {
	defs, err := parseObjectTypes()
	if err != nil {
		t.Fatalf("parseObjectTypes: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("parseObjectTypes returned 0 definitions")
	}
	// Check required fields on every def
	for _, d := range defs {
		if d.TypeName == "" {
			t.Errorf("found def with empty TypeName: %+v", d)
		}
		if d.JSONSchema == nil || len(d.JSONSchema) == 0 {
			t.Errorf("%s: empty JSONSchema", d.TypeName)
		}
	}
	t.Logf("Loaded %d object type(s)", len(defs))
	for _, d := range defs {
		t.Logf("  %s (enabled=%v)", d.TypeName, d.Enabled)
	}
}

func TestParseRelationshipTypes_Success(t *testing.T) {
	defs, err := parseRelationshipTypes()
	if err != nil {
		t.Fatalf("parseRelationshipTypes: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("parseRelationshipTypes returned 0 definitions")
	}
	// Check required fields on every def
	for _, d := range defs {
		if d.Name == "" {
			t.Errorf("found rel with empty Name: %+v", d)
		}
		if d.SourceType == "" {
			t.Errorf("%s: empty SourceType", d.Name)
		}
		if d.TargetType == "" {
			t.Errorf("%s: empty TargetType", d.Name)
		}
	}
	t.Logf("Loaded %d relationship type(s)", len(defs))
	for _, d := range defs {
		t.Logf("  %s (%s → %s)", d.Name, d.SourceType, d.TargetType)
	}
}

// =========================================================================
// toPackType
// =========================================================================

func TestToPackType_ExtractsLabelAndProperties(t *testing.T) {
	def := SchemaDefinition{
		TypeName:    "test_type",
		Description: "A test type",
		JSONSchema:  json.RawMessage(`{"label":"Test Type","properties":{"name":{"type":"string"}},"description":"A test type"}`),
		Enabled:     true,
	}
	pt := def.toPackType()
	if pt.Name != "test_type" {
		t.Errorf("Name = %q, want %q", pt.Name, "test_type")
	}
	if pt.Label != "Test Type" {
		t.Errorf("Label = %q, want %q", pt.Label, "Test Type")
	}
	if pt.Description != "A test type" {
		t.Errorf("Description = %q, want %q", pt.Description, "A test type")
	}
	if pt.Properties == nil {
		t.Fatal("Properties is nil")
	}
	var props map[string]any
	if err := json.Unmarshal(pt.Properties, &props); err != nil {
		t.Fatalf("unmarshal properties: %v", err)
	}
	if props["name"] == nil {
		t.Error("properties missing 'name' field")
	}
}

func TestToPackType_NoLabel(t *testing.T) {
	def := SchemaDefinition{
		TypeName:   "no_label_type",
		JSONSchema: json.RawMessage(`{"properties":{"val":{"type":"integer"}}}`),
	}
	pt := def.toPackType()
	if pt.Label != "" {
		t.Errorf("Label = %q, want empty", pt.Label)
	}
	if pt.Properties == nil {
		t.Fatal("Properties is nil")
	}
}

// =========================================================================
// normalizeJSON
// =========================================================================

func TestNormalizeJSON_EqualAfterFormatting(t *testing.T) {
	a := normalizeJSON(json.RawMessage(`{"a":1,"b":2}`))
	b := normalizeJSON(json.RawMessage(`{"b": 2, "a": 1}`))
	if a != b {
		t.Errorf("normalized JSON should be equal: %q vs %q", a, b)
	}
}

func TestNormalizeJSON_DifferentContent(t *testing.T) {
	a := normalizeJSON(json.RawMessage(`{"a":1}`))
	b := normalizeJSON(json.RawMessage(`{"a":2}`))
	if a == b {
		t.Error("normalized JSON should differ for different content")
	}
}

func TestNormalizeJSON_InvalidReturnsRaw(t *testing.T) {
	raw := `{invalid json}`
	result := normalizeJSON(json.RawMessage(raw))
	if result != raw {
		t.Errorf("invalid JSON should return raw string: got %q", result)
	}
}

func TestNormalizeJSON_EmptyInput(t *testing.T) {
	result := normalizeJSON(json.RawMessage{})
	if result != "" {
		t.Errorf("empty JSON should return empty string, got %q", result)
	}
}

// =========================================================================
// typeHasChanged
// =========================================================================

func TestTypeHasChanged_NilExisting(t *testing.T) {
	if !typeHasChanged(nil, &SchemaDefinition{}) {
		t.Error("nil existing should be considered changed")
	}
}

func TestTypeHasChanged_DescriptionChanged(t *testing.T) {
	existing := &srsdk.SchemaRegistryEntry{
		Description: strPtr("old desc"),
		Enabled:     true,
		JSONSchema:  json.RawMessage(`{"properties":{}}`),
	}
	def := &SchemaDefinition{
		Description: "new desc",
		Enabled:     true,
		JSONSchema:  json.RawMessage(`{"properties":{}}`),
	}
	if !typeHasChanged(existing, def) {
		t.Error("different description should be detected as changed")
	}
}

func TestTypeHasChanged_NilExistingDescription(t *testing.T) {
	existing := &srsdk.SchemaRegistryEntry{
		Description: nil,
		Enabled:     true,
		JSONSchema:  json.RawMessage(`{"properties":{}}`),
	}
	def := &SchemaDefinition{
		Description: "new desc",
		Enabled:     true,
		JSONSchema:  json.RawMessage(`{"properties":{}}`),
	}
	if !typeHasChanged(existing, def) {
		t.Error("nil → non-empty description should be changed")
	}
}

func TestTypeHasChanged_EnabledChanged(t *testing.T) {
	existing := &srsdk.SchemaRegistryEntry{
		Description: strPtr("test"),
		Enabled:     false,
		JSONSchema:  json.RawMessage(`{"properties":{}}`),
	}
	def := &SchemaDefinition{
		Description: "test",
		Enabled:     true,
		JSONSchema:  json.RawMessage(`{"properties":{}}`),
	}
	if !typeHasChanged(existing, def) {
		t.Error("different Enabled should be detected as changed")
	}
}

func TestTypeHasChanged_SameReturnsFalse(t *testing.T) {
	schema := json.RawMessage(`{"properties":{"name":{"type":"string"}}}`)
	existing := &srsdk.SchemaRegistryEntry{
		Description: strPtr("test"),
		Enabled:     true,
		JSONSchema:  schema,
	}
	def := &SchemaDefinition{
		Description: "test",
		Enabled:     true,
		JSONSchema:  schema,
	}
	if typeHasChanged(existing, def) {
		t.Error("identical definitions should not be changed")
	}
}

func TestTypeHasChanged_SchemaFormattingDifference(t *testing.T) {
	// Different formatting but same content — should NOT be changed
	existing := &srsdk.SchemaRegistryEntry{
		Description: strPtr("test"),
		Enabled:     true,
		JSONSchema:  json.RawMessage(`{"properties":{"name":{"type":"string"}}}`),
	}
	def := &SchemaDefinition{
		Description: "test",
		Enabled:     true,
		JSONSchema:  json.RawMessage(`{"properties": {"name": {"type": "string"}}}`),
	}
	if typeHasChanged(existing, def) {
		t.Error("formatting-only differences should not be detected as changed")
	}
}

// =========================================================================
// parseAssignResponse
// =========================================================================

func TestParseAssignResponse_AllInstalled(t *testing.T) {
	body := json.RawMessage(`{"installed_types":["type_a","type_b"],"merged_types":[]}`)
	newDefs := []SchemaDefinition{
		{TypeName: "type_a"},
		{TypeName: "type_b"},
	}
	rels := []RelationshipDefinition{
		{Name: "rel_x"},
	}

	results := parseAssignResponse(body, newDefs, rels)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Action != "created" || results[0].TypeName != "type_a" {
		t.Errorf("result[0] = %+v, want created type_a", results[0])
	}
	if results[1].Action != "created" || results[1].TypeName != "type_b" {
		t.Errorf("result[1] = %+v, want created type_b", results[1])
	}
	if results[2].Action != "created" || results[2].TypeName != "rel_x" {
		t.Errorf("result[2] = %+v, want created rel_x", results[2])
	}
}

func TestParseAssignResponse_SomeMerged(t *testing.T) {
	body := json.RawMessage(`{"installed_types":["type_b"],"merged_types":["type_a"]}`)
	newDefs := []SchemaDefinition{
		{TypeName: "type_a"},
		{TypeName: "type_b"},
	}
	results := parseAssignResponse(body, newDefs, nil)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Action != "updated" || results[0].TypeName != "type_a" {
		t.Errorf("result[0] = %+v, want updated type_a", results[0])
	}
	if results[1].Action != "created" || results[1].TypeName != "type_b" {
		t.Errorf("result[1] = %+v, want created type_b", results[1])
	}
}

func TestParseAssignResponse_EmptyBody(t *testing.T) {
	body := json.RawMessage(`{}`)
	newDefs := []SchemaDefinition{
		{TypeName: "type_a"},
	}
	results := parseAssignResponse(body, newDefs, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "updated" {
		t.Errorf("result = %+v, want updated type_a", results[0])
	}
}

// =========================================================================
// PrintResults — capture stdout
// =========================================================================

func TestPrintResults_AllActions(t *testing.T) {
	results := []Result{
		{TypeName: "CreatedType", Action: "created"},
		{TypeName: "UpdatedType", Action: "updated"},
		{TypeName: "UnchangedType", Action: "unchanged"},
		{TypeName: "ErrorType", Action: "error", Error: errors.New("something broke")},
	}
	// Just ensure it doesn't panic
	PrintResults(results, 1*time.Second)
}

// No need to test empty results — PrintResults handles it via zero counts.

// =========================================================================
// Helpers
// =========================================================================

func strPtr(s string) *string {
	return &s
}
