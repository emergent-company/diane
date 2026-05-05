// Package: main
// Schema listing and object detail HTTP handlers.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/Emergent-Comapny/diane/internal/schema"
)

func (a *localAPIServer) handleSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	nodeTypes, rels, err := schema.LoadDefinitions()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build relationship counts per type
	relCountByType := make(map[string]int)
	for _, rel := range rels {
		relCountByType[rel.SourceType]++
		relCountByType[rel.TargetType]++
	}

	// Query object counts from Memory Platform
	typeNames := make([]string, len(nodeTypes))
	for i, nt := range nodeTypes {
		typeNames[i] = nt.TypeName
	}
	objCounts, _ := a.bridge.GetObjectCountsForSchema(context.Background(), typeNames)

	// Enrich each type with counts
	for i := range nodeTypes {
		nodeTypes[i].ObjectCount = objCounts[nodeTypes[i].TypeName]
		nodeTypes[i].RelationshipCount = relCountByType[nodeTypes[i].TypeName]
	}

	// Sort by object count descending for convenience
	sort.Slice(nodeTypes, func(i, j int) bool {
		return nodeTypes[i].ObjectCount > nodeTypes[j].ObjectCount
	})

	resp := SchemaAPIResponse{
		NodeTypes:     nodeTypes,
		Relationships: rels,
	}
	jsonResponse(w, resp)
}

// SchemaObjectsResponse is the JSON response for GET /api/schema/objects/{typeName}.
type SchemaObjectsResponse struct {
	TypeName string                      `json:"type_name"`
	Total    int                         `json:"total"`
	Objects  []memory.GraphObjectSummary `json:"objects"`
}

// GET /api/schema/objects/{typeName} — returns recent objects of a given schema type.
// Supports ?limit=N to control the number of objects returned (default 20, max 50).
func (a *localAPIServer) handleSchemaObjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	typeName := strings.TrimPrefix(r.URL.Path, "/api/schema/objects/")
	if typeName == "" {
		jsonError(w, http.StatusBadRequest, "type name required")
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	objects, err := a.bridge.ListRecentObjectsByType(context.Background(), typeName, limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list objects: %v", err))
		return
	}

	// Get total count
	total := len(objects)
	if len(objects) == limit {
		// Try to get a more accurate total count (best-effort)
		if count, err := a.bridge.CountObjectsByType(context.Background(), typeName); err == nil {
			total = count
		}
	}

	jsonResponse(w, SchemaObjectsResponse{
		TypeName: typeName,
		Total:    total,
		Objects:  objects,
	})
}

// GetDefaultLocalAPIPort returns the default port for the local API.
func GetDefaultLocalAPIPort() int {
	return 8890
}

// GetDefaultConfigPathJSON returns the default MCP servers config path.
var GetDefaultMCPServersConfigPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".diane", "mcp-servers.json")
}

// ─── Doctor Check Handler ─────────────────────────────────

// GET /api/doctor — runs diagnostics and returns structured JSON results.
