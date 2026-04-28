// Package memorytools provides Diane's custom memory MCP tools.
// These wrap the Memory Platform's graph API for memory-specific operations.
// Uses the `memory` CLI (/root/.memory/bin/memory) for all graph operations.
package memorytools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ── Config ──

type dianeConfig struct {
	Projects []struct {
		ServerURL string `yaml:"server_url"`
		Token     string `yaml:"token"`
		ProjectID string `yaml:"project_id"`
	} `yaml:"projects"`
}

func loadConfig() (token, projectID string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("home dir: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "diane.yml"))
	if err != nil {
		return "", "", fmt.Errorf("read diane.yml: %w", err)
	}
	var cfg dianeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", "", fmt.Errorf("parse diane.yml: %w", err)
	}
	if len(cfg.Projects) == 0 {
		return "", "", fmt.Errorf("no projects in diane.yml")
	}
	p := cfg.Projects[0]
	return p.Token, p.ProjectID, nil
}

// findMemoryCLI locates the memory CLI binary.
func findMemoryCLI() string {
	paths := []string{
		"/root/.memory/bin/memory",
		filepath.Join(os.Getenv("HOME"), ".memory", "bin", "memory"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fall back to PATH
	return "memory"
}

// ── Tool Defs ──

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// Provider implements memory MCP tools.
type Provider struct {
	token     string
	projectID string
	memoryCLI string
}

// NewProvider creates a new memory tools provider.
func NewProvider() *Provider {
	return &Provider{}
}

// CheckDependencies loads config and finds the memory CLI.
func (p *Provider) CheckDependencies() error {
	token, projectID, err := loadConfig()
	if err != nil {
		return fmt.Errorf("memory config load failed: %w", err)
	}
	p.token = token
	p.projectID = projectID
	p.memoryCLI = findMemoryCLI()
	return nil
}

// Tools returns the list of memory tools.
func (p *Provider) Tools() []Tool {
	return []Tool{
		{
			Name:        "memory_save",
			Description: "Save a fact to the memory graph as a MemoryFact object. Automatically timestamps and links context. Stores user preferences, decisions, learned patterns, and entities discovered during conversation.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"content"},
				"properties": map[string]interface{}{
					"content":        strProp("The fact text to save"),
					"confidence":     numProp("Confidence 0.0-1.0 (0.9=stated, 0.7=inferred)", 0.7),
					"category":       strPropDef("Fact category", "user-preference"),
					"source_session": strProp("Session/run ID this fact was extracted from"),
					"source_agent":   strPropDef("Creating agent name", "diane-default"),
					"memory_tier":    intProp("Memory tier (1=per-turn, 2=session-end, 3=dreamed)", 1),
					"derived_from":   strProp("Key of the source fact this was derived from (for dreamed/hallucinated facts)"),
				},
			},
		},
		{
			Name:        "memory_recall",
			Description: "Search MemoryFact objects using hybrid semantic+keyword search. Returns facts sorted by relevance with confidence scores. Use BEFORE answering questions to recall past context. Automatically tracks retrieval scores.",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]interface{}{
					"query":          strProp("Natural language query"),
					"limit":          intProp("Max results (default 5, max 20)", 5),
					"min_confidence": numProp("Min confidence threshold (default 0.3)", 0.3),
					"category":       strProp("Optional: filter by category"),
					"dry_run":        boolProp("Report without updating retrieval scores", false),
				},
			},
		},
		{
			Name:        "memory_apply_decay",
			Description: "Apply confidence decay to unaccessed MemoryFacts. Uses 30-day half-life. Facts below threshold are archived. Reports changes without modifying when dry_run=true.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"half_life_days":   intProp("Days after which confidence halves", 30),
					"delete_threshold": numProp("Confidence below which facts are archived", 0.05),
					"dry_run":          boolProp("Report without modifying", false),
				},
			},
		},
		{
			Name:        "memory_detect_patterns",
			Description: "Find similar/overlapping MemoryFacts via vector similarity. Clusters similar facts. Can merge weaker facts into strongest when merge=true.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"similarity_threshold": numProp("Cosine similarity threshold for clustering", 0.85),
					"merge":                boolProp("Merge weaker facts into strongest", false),
					"dry_run":              boolProp("Report without modifying", false),
				},
			},
		},
	}
}

// HasTool checks if a tool belongs to this provider.
func (p *Provider) HasTool(name string) bool {
	return name == "memory_save" || name == "memory_recall" ||
		name == "memory_apply_decay" || name == "memory_detect_patterns"
}

// Call dispatches to the right tool implementation.
func (p *Provider) Call(name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "memory_save":
		return p.memorySave(args)
	case "memory_recall":
		return p.memoryRecall(args)
	case "memory_apply_decay":
		return p.memoryApplyDecay(args)
	case "memory_detect_patterns":
		return p.memoryDetectPatterns(args)
	}
	return nil, fmt.Errorf("unknown tool: %s", name)
}

// ── Memory CLI Helper ──

func (p *Provider) mem(args ...string) (map[string]interface{}, error) {
	cmd := exec.Command(p.memoryCLI, args...)
	// Always add --project and --json
	cmd.Args = append(cmd.Args, "--project", p.projectID, "--json")
	cmd.Env = append(os.Environ(),
		"MEMORY_API_KEY="+p.token,
		"HOME="+os.Getenv("HOME"),
	)

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("memory CLI: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("memory CLI: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		return map[string]interface{}{"raw": string(out)}, nil
	}
	return result, nil
}

// ── Tool Implementations ──

func (p *Provider) memorySave(args map[string]interface{}) (interface{}, error) {
	content := getString(args, "content", "")
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	props := map[string]interface{}{
		"content":       content,
		"confidence":    getFloat(args, "confidence", 0.7),
		"memory_tier":   getInt(args, "memory_tier", 1),
		"category":      getString(args, "category", "user-preference"),
		"source_agent":  getString(args, "source_agent", "diane-default"),
		"status":        "active",
		"access_count":  1,
		"created_at":    time.Now().UTC().Format(time.RFC3339),
		"last_accessed": time.Now().UTC().Format(time.RFC3339),
	}
	if ss := getString(args, "source_session", ""); ss != "" {
		props["source_session"] = ss
	}
	if df := getString(args, "derived_from", ""); df != "" {
		props["derived_from"] = df
	}

	key := fmt.Sprintf("fact-%d", time.Now().UnixMilli())
	propsJSON := mustJSON(props)

	result, err := p.mem("graph", "objects", "create",
		"--type", "MemoryFact",
		"--key", key,
		"--properties", propsJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("memory_save: %w", err)
	}

	return map[string]interface{}{
		"status":     "saved",
		"key":        key,
		"id":         result["id"],
		"content":    content,
		"confidence": props["confidence"],
		"category":   props["category"],
	}, nil
}

func (p *Provider) memoryRecall(args map[string]interface{}) (interface{}, error) {
	query := getString(args, "query", "")
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	limit := getInt(args, "limit", 5)
	minConf := getFloat(args, "min_confidence", 0.3)
	category := getString(args, "category", "")

	// Use memory query for hybrid semantic+keyword search
	result, err := p.mem("query", "--mode=search", "--limit", fmt.Sprintf("%d", limit), query)
	if err != nil {
		return nil, fmt.Errorf("memory_recall: %w", err)
	}

	allResults := extractResults(result)

	filtered := make([]map[string]interface{}, 0)

	for _, item := range allResults {
		objType := getString(item, "object_type", "")
		if objType != "MemoryFact" {
			continue
		}

		// Fields are nested under "fields" sub-map
		fields := getMap(item, "fields")
		if fields == nil {
			continue
		}

		conf := getFloat(fields, "confidence", 1.0)
		if conf < minConf {
			continue
		}
		cat := getString(fields, "category", "")
		if category != "" && cat != category {
			continue
		}

		objID := getString(item, "object_id", "")

		filtered = append(filtered, map[string]interface{}{
			"id":         objID,
			"content":    getString(fields, "content", ""),
			"confidence": conf,
			"category":   cat,
			"tier":       getInt(fields, "memory_tier", 1),
			"agent":      getString(fields, "source_agent", ""),
			"created":    getString(fields, "created_at", ""),
			"score":      getFloat(item, "score", 0),
		})

		// ── Persist retrieval score ──
		// Update avg_retrieval_score, retrieval_count, last_retrieval_score
		if objID != "" {
			score := getFloat(item, "score", 0)
			curRetCount := getInt(fields, "retrieval_count", 0)
			curAvgScore := getFloat(fields, "avg_retrieval_score", 0)
			curDivCount := getInt(fields, "query_diversity_count", 0)

			newRetCount := curRetCount + 1
			newAvg := score
			if curRetCount > 0 && curAvgScore > 0 {
				newAvg = ((curAvgScore * float64(curRetCount)) + score) / float64(newRetCount)
			}

			// Track query diversity: compare this query hash against stored queries
			newDivCount := curDivCount
			lastQuery := getString(fields, "last_query_hash", "")
			queryHash := simpleHash(query)
			if queryHash != lastQuery {
				newDivCount = curDivCount + 1
			}

			updateProps := map[string]interface{}{
				"avg_retrieval_score":   newAvg,
				"retrieval_count":       newRetCount,
				"last_retrieval_score":  score,
				"last_accessed":         time.Now().UTC().Format(time.RFC3339),
				"last_query_hash":       queryHash,
				"query_diversity_count": newDivCount,
			}

			if !isDryRun(args) {
				p.mem("graph", "objects", "update", objID,
					"--properties", mustJSON(updateProps),
				)
			}
		}
	}

	return map[string]interface{}{
		"results": filtered,
		"count":   len(filtered),
	}, nil
}

func (p *Provider) memoryApplyDecay(args map[string]interface{}) (interface{}, error) {
	halfLife := getInt(args, "half_life_days", 30)
	threshold := getFloat(args, "delete_threshold", 0.05)
	dryRun := getBool(args, "dry_run", false)

	result, err := p.mem("graph", "objects", "list", "--type", "MemoryFact",
		"--limit", "5000", "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("list facts: %w", err)
	}

	items := getItems(result)
	now := time.Now().UTC()
	var decayed, archived int
	details := make([]map[string]interface{}, 0)

	for _, fact := range items {
		props := getMap(fact, "properties")
		if props == nil {
			props = fact
		}
		factID := getString(fact, "id", "")
		conf := getFloat(props, "confidence", 1.0)
		lastStr := getString(props, "last_accessed", getString(fact, "created_at", ""))
		accessCount := getInt(props, "access_count", 0)
		status := getString(props, "status", "active")

		if status == "archived" || status == "merged" || conf <= 0 {
			continue
		}

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

			entry := map[string]interface{}{
				"id":             factID,
				"content":        getString(props, "content", "")[:min(60, len(getString(props, "content", "")))],
				"old_confidence": conf,
				"new_confidence": newConf,
				"days_unused":    days,
			}

			if newConf < threshold {
				entry["action"] = "archive"
				archived++
				if !dryRun {
					p.mem("graph", "objects", "update", factID,
						"--properties", mustJSON(map[string]interface{}{
							"confidence": newConf,
							"status":     "archived",
						}),
					)
				}
			} else {
				entry["action"] = "decay"
				decayed++
				if !dryRun {
					p.mem("graph", "objects", "update", factID,
						"--properties", mustJSON(map[string]interface{}{"confidence": newConf}),
					)
				}
			}
			details = append(details, entry)
		}
	}

	return map[string]interface{}{
		"status":      "ok",
		"dry_run":     dryRun,
		"total_facts": len(items),
		"decayed":     decayed,
		"archived":    archived,
		"details":     details,
	}, nil
}

func (p *Provider) memoryDetectPatterns(args map[string]interface{}) (interface{}, error) {
	threshold := getFloat(args, "similarity_threshold", 0.85)
	shouldMerge := getBool(args, "merge", false)
	dryRun := getBool(args, "dry_run", false)

	result, err := p.mem("graph", "objects", "list", "--type", "MemoryFact",
		"--limit", "5000", "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("list facts: %w", err)
	}

	facts := getItems(result)
	processed := make(map[string]bool)
	var clusters []map[string]interface{}
	merged := 0

	for _, fact := range facts {
		key := getString(fact, "key", "")
		if key == "" || processed[key] {
			continue
		}
		props := getMap(fact, "properties")
		if props == nil {
			props = fact
		}
		content := getString(props, "content", "")
		if len(content) < 10 {
			processed[key] = true
			continue
		}

		// Search for similar content via memory query
		simResult, err := p.mem("query", "--mode=search", "--limit", "5", content)
		if err != nil {
			processed[key] = true
			continue
		}

		simItems := extractResults(simResult)
		var similar []map[string]interface{}

		for _, item := range simItems {
			simKey := getString(item, "key", "")
			if simKey == key || processed[simKey] {
				continue
			}
			score := getFloat(item, "score", 0)
			if score >= threshold {
				similar = append(similar, map[string]interface{}{
					"key":     simKey,
					"content": getString(item, "content", "")[:min(80, len(getString(item, "content", "")))],
					"score":   score,
				})
				processed[simKey] = true
			}
		}

		processed[key] = true

		if len(similar) > 0 {
			cluster := map[string]interface{}{
				"primary": map[string]interface{}{
					"key":     key,
					"content": content[:min(80, len(content))],
				},
				"similar": similar,
				"size":    1 + len(similar),
			}
			clusters = append(clusters, cluster)

			if shouldMerge && !dryRun {
				for _, sim := range similar {
					sk := sim["key"].(string)
					p.mem("graph", "objects", "update", sk,
						"--properties", mustJSON(map[string]interface{}{
							"status":      "merged",
							"merged_into": key,
						}),
					)
					merged++
				}
			}
		}
	}

	return map[string]interface{}{
		"status":               "ok",
		"dry_run":              dryRun,
		"clusters_found":       len(clusters),
		"merged":               merged,
		"total_facts":          len(facts),
		"similarity_threshold": threshold,
		"clusters":             clusters,
	}, nil
}

// ── Schema Helpers ──

func strProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}
func strPropDef(desc, def string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc, "default": def}
}
func numProp(desc string, def float64) map[string]interface{} {
	return map[string]interface{}{"type": "number", "description": desc, "default": def}
}
func intProp(desc string, def int) map[string]interface{} {
	return map[string]interface{}{"type": "integer", "description": desc, "default": def}
}
func boolProp(desc string, def bool) map[string]interface{} {
	return map[string]interface{}{"type": "boolean", "description": desc, "default": def}
}

// ── Data Helpers ──

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func getString(m map[string]interface{}, key, def string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return def
}
func getFloat(m map[string]interface{}, key string, def float64) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return def
}
func getInt(m map[string]interface{}, key string, def int) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return def
}
func getBool(m map[string]interface{}, key string, def bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return def
}
func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}
func extractResults(m map[string]interface{}) []map[string]interface{} {
	if r, ok := m["results"].([]interface{}); ok {
		out := make([]map[string]interface{}, 0)
		for _, item := range r {
			if im, ok := item.(map[string]interface{}); ok {
				out = append(out, im)
			}
		}
		return out
	}
	if r, ok := m["results"].([]map[string]interface{}); ok {
		return r
	}
	return nil
}
func getItems(m map[string]interface{}) []map[string]interface{} {
	if items, ok := m["items"].([]interface{}); ok {
		out := make([]map[string]interface{}, 0)
		for _, item := range items {
			if im, ok := item.(map[string]interface{}); ok {
				out = append(out, im)
			}
		}
		return out
	}
	if items, ok := m["items"].([]map[string]interface{}); ok {
		return items
	}
	return nil
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// isDryRun checks if the caller requested a dry run (no mutations).
func isDryRun(args map[string]interface{}) bool {
	if v, ok := args["dry_run"].(bool); ok {
		return v
	}
	if v, ok := args["dryRun"].(bool); ok {
		return v
	}
	return false
}

// simpleHash produces a deterministic short hash for query diversity tracking.
// Uses a simplified approach: lowercases and trims the input, takes first 40 chars.
func simpleHash(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
