package schema

import (
	"encoding/json"
	"fmt"
	"sort"
)

// EnrichedProperty describes a single property on a graph node type.
type EnrichedProperty struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	EnumValues  []string `json:"enum_values,omitempty"`
}

// EnrichedSchemaType is the structured representation of a graph object type,
// with its JSON schema parsed into individual properties.
type EnrichedSchemaType struct {
	TypeName           string             `json:"type_name"`
	Label              string             `json:"label"`
	Description        string             `json:"description"`
	Namespace          string             `json:"namespace,omitempty"`
	Properties         []EnrichedProperty `json:"properties"`
	ObjectCount        int                `json:"object_count,omitempty"`
	RelationshipCount  int                `json:"relationship_count,omitempty"`
}

// EnrichedRelationship is the structured representation of a relationship type.
type EnrichedRelationship struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	InverseLabel string `json:"inverse_label"`
	Description  string `json:"description"`
	SourceType   string `json:"source_type"`
	TargetType   string `json:"target_type"`
}

// LoadDefinitions reads all embedded schema JSON files and returns parsed
// object types and relationships suitable for display/API consumption.
func LoadDefinitions() ([]EnrichedSchemaType, []EnrichedRelationship, error) {
	defs, err := parseObjectTypes()
	if err != nil {
		return nil, nil, fmt.Errorf("parse object types: %w", err)
	}

	nodes := make([]EnrichedSchemaType, len(defs))
	for i, def := range defs {
		nodes[i] = schemaDefToType(def)
	}

	rels, err := parseRelationshipTypes()
	if err != nil {
		return nil, nil, fmt.Errorf("parse relationships: %w", err)
	}

	relationships := make([]EnrichedRelationship, len(rels))
	for i, rel := range rels {
		relationships[i] = EnrichedRelationship{
			Name:         rel.Name,
			Label:        rel.Label,
			InverseLabel: rel.InverseLabel,
			Description:  rel.Description,
			SourceType:   rel.SourceType,
			TargetType:   rel.TargetType,
		}
	}

	return nodes, relationships, nil
}

// schemaDefToType parses a SchemaDefinition's raw JSON schema into
// structured EnrichedSchemaType with extracted properties.
func schemaDefToType(def SchemaDefinition) EnrichedSchemaType {
	var raw struct {
		Label       string          `json:"label"`
		Type        string          `json:"type"`
		Description string          `json:"description"`
		Properties  json.RawMessage `json:"properties"`
	}
	json.Unmarshal(def.JSONSchema, &raw)

	label := raw.Label
	if label == "" {
		label = def.TypeName
	}

	desc := raw.Description
	if desc == "" {
		desc = def.Description
	}

	node := EnrichedSchemaType{
		TypeName:    def.TypeName,
		Label:       label,
		Description: desc,
		Namespace:   def.Namespace,
	}

	// Parse properties map into ordered list
	if raw.Properties != nil {
		var propsMap map[string]json.RawMessage
		if err := json.Unmarshal(raw.Properties, &propsMap); err == nil {
			// Sort property names for stable output
			names := make([]string, 0, len(propsMap))
			for name := range propsMap {
				names = append(names, name)
			}
			sort.Strings(names)

			for _, name := range names {
				prop := parseProperty(name, propsMap[name])
				node.Properties = append(node.Properties, prop)
			}
		}
	}

	return node
}

// parseProperty extracts a single property from its raw JSON schema object.
func parseProperty(name string, raw json.RawMessage) EnrichedProperty {
	prop := EnrichedProperty{Name: name}

	var basic struct {
		Type        string   `json:"type"`
		Description string   `json:"description"`
		Enum        []string `json:"enum"`
	}
	if err := json.Unmarshal(raw, &basic); err != nil {
		prop.Type = "unknown"
		return prop
	}

	prop.Type = basic.Type
	prop.Description = basic.Description
	if len(basic.Enum) > 0 {
		prop.EnumValues = basic.Enum
	}
	return prop
}
