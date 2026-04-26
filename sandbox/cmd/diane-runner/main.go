// diane-runner — MCP server for Diane sandbox containers
//
// Runs as a stdio-based MCP server inside the sandbox container.
// Exposes memory operation tools that wrap the `memory` CLI.
// Gets auth from environment variables:
//   MEMORY_SERVER_URL   — Memory Platform URL
//   MEMORY_PROJECT_ID   — Project ID
//   MEMORY_API_KEY      — Project API token (emt_*)
//
// No connection to Diane Master needed — fully self-contained.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ── Tool Definitions ──────────────────────────────────────────────────────────

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

var dianeTools = []Tool{
	{
		Name:        "diane_memory_save",
		Description: "Save a fact to the memory graph as a MemoryFact object. Creates a durable memory that future agent runs can recall.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"content"},
			"properties": map[string]any{
				"content":  map[string]any{"type": "string", "description": "The fact text to save"},
				"category": map[string]any{"type": "string", "description": "Fact category: user-preference, decision, pattern, entity, action-item, observation", "default": "observation"},
				"confidence": map[string]any{"type": "number", "description": "Confidence 0.0-1.0 (0.9=stated, 0.7=inferred, 0.5=speculative)", "default": 0.7},
				"source_session": map[string]any{"type": "string", "description": "Session/run ID this fact was extracted from"},
				"source_agent":   map[string]any{"type": "string", "description": "Name of the agent that extracted this fact", "default": "diane-default"},
				"memory_tier":    map[string]any{"type": "integer", "description": "1=per-turn, 2=session-end, 3=dreamed/consolidated", "default": 1},
			},
		},
	},
	{
		Name:        "diane_memory_recall",
		Description: "Search MemoryFact objects using hybrid semantic+keyword search. Returns facts sorted by relevance. Use BEFORE answering questions to recall past context.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query":          map[string]any{"type": "string", "description": "Natural language query"},
				"limit":          map[string]any{"type": "integer", "description": "Max results (default 10, max 50)", "default": 10},
				"min_confidence": map[string]any{"type": "number", "description": "Min confidence filter (0.0-1.0)", "default": 0.0},
				"category":       map[string]any{"type": "string", "description": "Optional: filter by category"},
				"tier":           map[string]any{"type": "integer", "description": "Optional: filter by memory_tier"},
			},
		},
	},
	{
		Name:        "diane_memory_decay",
		Description: "Apply confidence decay to unaccessed MemoryFacts. Uses a half-life model — facts not accessed within the half-life period lose half their confidence. Facts below threshold are archived.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"half_life_days":   map[string]any{"type": "integer", "description": "Days after which confidence halves", "default": 30},
				"delete_threshold": map[string]any{"type": "number", "description": "Confidence below which facts are archived", "default": 0.05},
				"dry_run":          map[string]any{"type": "boolean", "description": "Report only, don't modify", "default": false},
			},
		},
	},
	{
		Name:        "diane_memory_patterns",
		Description: "Find similar/overlapping MemoryFacts via semantic similarity search. Clusters similar facts together. Can merge weaker facts into the strongest one when merge=true.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"similarity_threshold": map[string]any{"type": "number", "description": "Score threshold for clustering (0.0-1.0)", "default": 0.85},
				"merge":                map[string]any{"type": "boolean", "description": "Merge weaker facts into the strongest", "default": false},
				"dry_run":              map[string]any{"type": "boolean", "description": "Report only, don't modify", "default": false},
			},
		},
	},
}

// ── Memory CLI Wrapper ────────────────────────────────────────────────────────

var serverURL, projectID, apiKey string

func init() {
	serverURL = os.Getenv("MEMORY_SERVER_URL")
	projectID = os.Getenv("MEMORY_PROJECT_ID")
	apiKey = os.Getenv("MEMORY_API_KEY")
}

func mem(args ...string) (map[string]any, error) {
	cliPath := findMemoryCLI()
	cmd := exec.Command(cliPath, args...)
	cmd.Args = append(cmd.Args, "--project", projectID, "--json")

	env := os.Environ()
	if serverURL != "" {
		env = append(env, "MEMORY_SERVER_URL="+serverURL)
	} else {
		env = append(env, "MEMORY_SERVER_URL=https://memory.emergent-company.ai")
	}
	if apiKey != "" {
		env = append(env, "MEMORY_PROJECT_TOKEN="+apiKey)
	}
	cmd.Env = env

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("memory CLI: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("memory CLI: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		return map[string]any{"raw": string(out)}, nil
	}
	return result, nil
}

func findMemoryCLI() string {
	// Check common locations
	paths := []string{
		"/usr/local/bin/memory",
		"/usr/bin/memory",
		"/root/.memory/bin/memory",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fall back to PATH
	return "memory"
}

// ── MCP Server ────────────────────────────────────────────────────────────────

type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type CallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func main() {
	// Log to stderr to avoid polluting MCP stdout protocol
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime | log.Lshortfile)
	log.Printf("diane-runner starting — server=%s project=%s", serverURL, projectID)

	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		var req MCPRequest
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("decode error: %v", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		resp := handle(req)
		resp.JSONRPC = "2.0"
		resp.ID = req.ID

		if err := encoder.Encode(resp); err != nil {
			log.Printf("encode error: %v", err)
			break
		}
	}
}

func handle(req MCPRequest) MCPResponse {
	switch req.Method {
	case "initialize":
		return MCPResponse{
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "diane-runner",
					"version": "1.0.0",
				},
			},
		}

	case "tools/list":
		tools := make([]map[string]any, 0, len(dianeTools))
		for _, t := range dianeTools {
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		return MCPResponse{Result: map[string]any{"tools": tools}}

	case "tools/call":
		var params CallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return MCPResponse{Error: &Error{Code: -32602, Message: "invalid params: " + err.Error()}}
		}
		return callTool(params.Name, params.Arguments)

	default:
		return MCPResponse{
			Error: &Error{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func callTool(name string, args map[string]any) MCPResponse {
	switch name {
	case "diane_memory_save":
		return handleSave(args)
	case "diane_memory_recall":
		return handleRecall(args)
	case "diane_memory_decay":
		return handleDecay(args)
	case "diane_memory_patterns":
		return handlePatterns(args)
	default:
		return MCPResponse{
			Error: &Error{Code: -32602, Message: fmt.Sprintf("unknown tool: %s", name)},
		}
	}
}

// ── Tool Handlers ─────────────────────────────────────────────────────────────

func handleSave(args map[string]any) MCPResponse {
	content := getString(args, "content")
	if content == "" {
		return MCPResponse{Error: &Error{Code: -32602, Message: "content is required"}}
	}

	props := map[string]any{
		"content":     content,
		"confidence":  getFloat(args, "confidence", 0.7),
		"memory_tier": getInt(args, "memory_tier", 1),
		"category":    getStringDefault(args, "category", "observation"),
		"source_agent": getStringDefault(args, "source_agent", "diane-default"),
		"status":       "active",
		"access_count": 1,
		"created_at":   time.Now().UTC().Format(time.RFC3339),
	}
	if ss := getString(args, "source_session"); ss != "" {
		props["source_session"] = ss
	}

	key := fmt.Sprintf("fact-%d", time.Now().UnixMilli())
	propsJSON := mustJSON(props)

	_, err := mem("graph", "objects", "create",
		"--type", "MemoryFact",
		"--key", key,
		"--properties", propsJSON,
	)
	if err != nil {
		return MCPResponse{Error: &Error{Code: 1, Message: fmt.Sprintf("save failed: %v", err)}}
	}

	return MCPResponse{
		Result: map[string]any{
			"status":     "saved",
			"key":        key,
			"content":    content,
			"confidence": props["confidence"],
			"category":   props["category"],
		},
	}
}

func handleRecall(args map[string]any) MCPResponse {
	query := getString(args, "query")
	if query == "" {
		return MCPResponse{Error: &Error{Code: -32602, Message: "query is required"}}
	}

	limit := getInt(args, "limit", 10)
	if limit > 50 {
		limit = 50
	}
	minConf := getFloat(args, "min_confidence", 0.0)
	category := getString(args, "category")
	tier := getInt(args, "tier", 0)

	// Use hybrid search via memory CLI
	result, err := mem("query", "--mode=search", "--limit", fmt.Sprintf("%d", limit), query)
	if err != nil {
		return MCPResponse{Error: &Error{Code: 1, Message: fmt.Sprintf("recall failed: %v", err)}}
	}

	items := extractItems(result)
	filtered := make([]map[string]any, 0)

	for _, item := range items {
		props := getMap(item, "properties")
		if props == nil {
			props = item
		}

		objType := getString(props, "type")
		if objType == "" {
			objType = getString(item, "type")
		}
		if objType != "MemoryFact" && objType != "" {
			continue
		}

		conf := getFloat(props, "confidence", 1.0)
		if conf < minConf {
			continue
		}
		if category != "" && getString(props, "category") != category {
			continue
		}
		if tier > 0 && getInt(props, "memory_tier", 0) != tier {
			continue
		}

		filtered = append(filtered, map[string]any{
			"content":    getString(props, "content"),
			"confidence": conf,
			"category":   getString(props, "category"),
			"tier":       props["memory_tier"],
			"agent":      getString(props, "source_agent"),
			"session":    getString(props, "source_session"),
			"created":    getString(props, "created_at"),
			"score":      getFloat(item, "score", 0),
		})
	}

	return MCPResponse{
		Result: map[string]any{
			"results": filtered,
			"count":   len(filtered),
		},
	}
}

func handleDecay(args map[string]any) MCPResponse {
	halfLife := getInt(args, "half_life_days", 30)
	threshold := getFloat(args, "delete_threshold", 0.05)
	dryRun := getBool(args, "dry_run")

	result, err := mem("graph", "objects", "list", "--type", "MemoryFact", "--limit", "5000")
	if err != nil {
		return MCPResponse{Error: &Error{Code: 1, Message: fmt.Sprintf("list facts failed: %v", err)}}
	}

	items := extractItems(result)
	now := time.Now().UTC()
	var decayed, archived int
	details := make([]map[string]any, 0)

	for _, fact := range items {
		id := getString(fact, "id")
		props := getMap(fact, "properties")
		if props == nil {
			props = fact
		}

		status := getString(props, "status")
		if status == "archived" || status == "merged" {
			continue
		}

		conf := getFloat(props, "confidence", 1.0)
		if conf <= 0 {
			continue
		}

		lastStr := getString(props, "last_accessed")
		if lastStr == "" {
			lastStr = getString(props, "created_at")
		}
		content := getString(props, "content")
		accessCount := getInt(props, "access_count", 0)

		lastTime, err := time.Parse(time.RFC3339, lastStr)
		if err != nil {
			continue
		}
		days := int(now.Sub(lastTime).Hours() / 24)

		if days > halfLife && accessCount == 0 {
			periods := days / halfLife
			newConf := conf
			for i := 0; i < periods; i++ {
				newConf /= 2
			}

			entry := map[string]any{
				"id":             id,
				"content":        truncate(content, 60),
				"old_confidence": conf,
				"new_confidence": newConf,
				"days_unused":    days,
			}

			if newConf < threshold {
				entry["action"] = "archive"
				archived++
				if !dryRun {
					mem("graph", "objects", "update", id,
						"--properties", mustJSON(map[string]any{
							"confidence": newConf,
							"status":     "archived",
						}),
					)
				}
			} else {
				entry["action"] = "decay"
				decayed++
				if !dryRun {
					mem("graph", "objects", "update", id,
						"--properties", mustJSON(map[string]any{"confidence": newConf}),
					)
				}
			}
			details = append(details, entry)
		}
	}

	return MCPResponse{
		Result: map[string]any{
			"status":      "ok",
			"dry_run":     dryRun,
			"total_facts": len(items),
			"decayed":     decayed,
			"archived":    archived,
			"details":     details,
		},
	}
}

func handlePatterns(args map[string]any) MCPResponse {
	threshold := getFloat(args, "similarity_threshold", 0.85)
	shouldMerge := getBool(args, "merge")
	dryRun := getBool(args, "dry_run")

	result, err := mem("graph", "objects", "list", "--type", "MemoryFact", "--limit", "5000")
	if err != nil {
		return MCPResponse{Error: &Error{Code: 1, Message: fmt.Sprintf("list facts failed: %v", err)}}
	}

	items := extractItems(result)
	processed := make(map[string]bool)
	clusters := make([]map[string]any, 0)
	merged := 0

	for _, fact := range items {
		key := getString(fact, "key")
		if key == "" || processed[key] {
			continue
		}
		props := getMap(fact, "properties")
		if props == nil {
			props = fact
		}
		content := getString(props, "content")
		if len(content) < 10 {
			processed[key] = true
			continue
		}

		// Search for similar content via memory query
		simResult, simErr := mem("query", "--mode=search", "--limit", "5", content)
		if simErr != nil {
			processed[key] = true
			continue
		}

		simItems := extractItems(simResult)
		var similar []map[string]any

		for _, item := range simItems {
			simProps := getMap(item, "properties")
			if simProps == nil {
				simProps = item
			}
			simKey := getString(item, "key")
			if simKey == "" || simKey == key || processed[simKey] {
				continue
			}
			score := getFloat(item, "score", 0)
			if score >= threshold {
				similar = append(similar, map[string]any{
					"key":     simKey,
					"content": truncate(getString(simProps, "content"), 80),
					"score":   score,
				})
				processed[simKey] = true
			}
		}

		processed[key] = true

		if len(similar) > 0 {
			cluster := map[string]any{
				"primary": map[string]any{
					"key":     key,
					"content": truncate(content, 80),
				},
				"similar": similar,
				"size":    1 + len(similar),
			}
			clusters = append(clusters, cluster)

			if shouldMerge && !dryRun {
				for _, sim := range similar {
					sk := sim["key"].(string)
					mem("graph", "objects", "update", sk,
						"--properties", mustJSON(map[string]any{
							"status":      "merged",
							"merged_into": key,
						}),
					)
					merged++
				}
			}
		}
	}

	return MCPResponse{
		Result: map[string]any{
			"status":              "ok",
			"dry_run":             dryRun,
			"clusters_found":      len(clusters),
			"merged":              merged,
			"total_facts":         len(items),
			"similarity_threshold": threshold,
			"clusters":            clusters,
		},
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func extractItems(result map[string]any) []map[string]any {
	if result == nil {
		return nil
	}

	// `memory query --mode=search` returns {results: [{type: "graph", fields: {...}, ...}]}
	if results, ok := result["results"].([]any); ok {
		out := make([]map[string]any, 0, len(results))
		for _, item := range results {
			if m, ok := item.(map[string]any); ok {
				// Convert query result to unified format
				objType := getString(m, "object_type")
				if objType == "" {
					objType = getString(m, "type")
				}
				fields := getMap(m, "fields")
				unified := map[string]any{
					"id":         getString(m, "object_id"),
					"key":        getString(m, "key"),
					"type":       objType,
					"score":      getFloat(m, "score", 0),
					"properties": fields,
				}
				// Copy each field to root level (some consumers expect them there)
				if fields != nil {
					for k, v := range fields {
						unified[k] = v
					}
				}
				out = append(out, unified)
			}
		}
		return out
	}

	// `memory graph objects list` returns {items: [...]}
	if items, ok := result["items"].([]any); ok {
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}

	// Some endpoints return {data: [...]}
	if items, ok := result["data"].([]any); ok {
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func getStringDefault(m map[string]any, key, def string) string {
	if v := getString(m, key); v != "" {
		return v
	}
	return def
}

func getFloat(m map[string]any, key string, def float64) float64 {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return def
}

func getInt(m map[string]any, key string, def int) int {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return def
}

func getBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func getMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	result, _ := v.(map[string]any)
	return result
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
