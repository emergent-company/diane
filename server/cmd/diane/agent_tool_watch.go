package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/agents"
	"github.com/Emergent-Comapny/diane/internal/memory"
)

// agentToolConfigWatch connects to the MP SSE stream and watches for
// AgentToolConfig and AgentOverrideConfig entity changes. On change,
// it automatically re-reads configs from the graph and re-seeds
// affected built-in agents.
//
// At startup it does an initial poll+seed to catch any changes made while
// the relay was offline, then subscribes to SSE for real-time updates.
func agentToolConfigWatch(ctx context.Context, serverURL, apiKey, projectID, orgID string) {
	memCfg := memory.Config{
		ServerURL: serverURL,
		APIKey:    apiKey,
		ProjectID: projectID,
		OrgID:     orgID,
	}

	graphClient, err := newGraphClient(serverURL, apiKey, projectID)
	if err != nil {
		log.Printf("[agent-watch] Failed to create graph client: %v (watching disabled)", err)
		return
	}

	// Cache: track last-seen hash of AgentToolConfig + AgentOverrideConfig entities
	var lastHash string

	// Read current state once at startup, seed if needed
	hash := pollAndSeed(ctx, graphClient, memCfg, lastHash)
	if hash != "" {
		lastHash = hash
	}
	if hash == lastHash {
		log.Printf("[agent-watch] Agent config unchanged since last check")
	}

	// SSE reconnect loop
	backoff := 5 * time.Second
	maxBackoff := 2 * time.Minute

	for {
		select {
		case <-ctx.Done():
			log.Printf("[agent-watch] Context cancelled, stopping")
			return
		default:
		}

		err := sseSubscribe(ctx, serverURL, apiKey, projectID, graphClient, &lastHash, memCfg)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[agent-watch] SSE disconnected: %v (reconnect in %v)", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		} else {
			backoff = 5 * time.Second // reset on clean reconnect
		}
	}
}

// sseHTTPClient is used exclusively for the SSE stream connection.
// It has no timeout to allow long-lived streaming.
var sseHTTPClient = &http.Client{}

// sseSubscribe connects to the MP SSE event stream and processes events
// until disconnect or context cancellation.
func sseSubscribe(ctx context.Context, serverURL, apiKey, projectID string, gc *graphClientWrapper, lastHash *string, memCfg memory.Config) error {
	sseURL := fmt.Sprintf("%s/api/events/stream?projectId=%s", serverURL, projectID)

	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		return fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := sseHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("SSE unexpected status: %d", resp.StatusCode)
	}

	log.Printf("[agent-watch] Connected to SSE stream")

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)

	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line = end of event
		if line == "" {
			currentEvent = ""
			continue
		}

		// Parse SSE event type
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		// Parse SSE data
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			switch currentEvent {
			case "entity.created", "entity.updated", "entity.deleted":
				var evt sseEntityEvent
				if err := json.Unmarshal([]byte(data), &evt); err != nil {
					log.Printf("[agent-watch] Failed to parse event data: %v", err)
					continue
				}
				handleEntityEvent(ctx, gc, evt, lastHash, memCfg)
			case "heartbeat":
				// no-op, connection is alive
			case "connected":
				log.Printf("[agent-watch] SSE connected: %s", truncateStr(data, 80))
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("SSE read: %w", err)
	}

	return io.EOF
}

// sseEntityEvent represents an entity event from the SSE stream.
type sseEntityEvent struct {
	Entity    string `json:"entity"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
}

// agentConfigWatcherTypes lists the graph entity types that trigger re-seeding.
var agentConfigWatcherTypes = []string{"AgentToolConfig", "AgentOverrideConfig"}

// handleEntityEvent checks if the changed entity is an watched config type
// and triggers a re-seed if so.
func handleEntityEvent(ctx context.Context, gc *graphClientWrapper, evt sseEntityEvent, lastHash *string, memCfg memory.Config) {
	if evt.Entity != "graph_object" || evt.ID == "" {
		return
	}

	// Quick type check via GetObject
	obj, err := gc.GetObject(ctx, evt.ID)
	if err != nil {
		// Object not found (deleted or error) — recheck all to be safe
		log.Printf("[agent-watch] Could not fetch entity %s (may be deleted): %v", truncateStr(evt.ID, 12), err)
	} else if !isWatchedConfigType(obj.Type) {
		return // not an agent config change, skip
	}

	log.Printf("[agent-watch] Agent config entity changed (%s, type=%s) — re-reading configs", evt.Entity, obj.Type)

	hash := pollAndSeed(ctx, gc, memCfg, *lastHash)
	if hash != "" {
		*lastHash = hash
	}
}

// isWatchedConfigType returns true if the given entity type is one that
// triggers auto-re-seeding of agent definitions.
func isWatchedConfigType(objType string) bool {
	for _, t := range agentConfigWatcherTypes {
		if objType == t {
			return true
		}
	}
	return false
}

// pollAndSeed reads AgentToolConfig and AgentOverrideConfig entities from
// the graph, computes a combined hash, and seeds if the hash differs
// from lastHash. Returns the new hash, or empty string if unchanged.
func pollAndSeed(ctx context.Context, gc *graphClientWrapper, memCfg memory.Config, lastHash string) string {
	// Read both config types
	toolConfigs, err := gc.ListObjects(ctx, "AgentToolConfig", 100)
	if err != nil {
		log.Printf("[agent-watch] Failed to list AgentToolConfig: %v", err)
		return ""
	}
	overrideConfigs, err := gc.ListObjects(ctx, "AgentOverrideConfig", 100)
	if err != nil {
		log.Printf("[agent-watch] Failed to list AgentOverrideConfig: %v", err)
		return ""
	}

	// Compute combined hash
	hashInput := ""
	for _, obj := range toolConfigs.Items {
		agentName, _ := obj.Properties["agent_name"].(string)
		patternsRaw, _ := obj.Properties["tool_patterns"].([]interface{})
		hashInput += "tool:" + agentName + ":" + strings.Join(toStringSlice(patternsRaw), ",") + ";"
	}
	for _, obj := range overrideConfigs.Items {
		agentName, _ := obj.Properties["agent_name"].(string)
		sp, _ := obj.Properties["system_prompt"].(string)
		hashInput += "override:" + agentName + ":" + sp + ";"
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(hashInput)))

	if hash == lastHash {
		return hash // unchanged
	}

	log.Printf("[agent-watch] Agent config changed — seeding built-in agents")
	if err := seedBuiltInAgentsFromGraph(ctx, memCfg); err != nil {
		log.Printf("[agent-watch] Seed failed: %v", err)
		return lastHash // keep old hash so we retry
	}

	log.Printf("[agent-watch] Built-in agents re-seeded successfully")
	return hash
}

// seedBuiltInAgentsFromGraph reads AgentToolConfig and AgentOverrideConfig
// from the graph, applies them to built-in agents in the correct order,
// and seeds to Memory Platform.
func seedBuiltInAgentsFromGraph(ctx context.Context, memCfg memory.Config) error {
	bridge, err := memory.New(memCfg)
	if err != nil {
		return fmt.Errorf("create bridge: %w", err)
	}
	defer bridge.Close()

	// Build merged agents: built-in → overrides → tool patterns
	builtIns, err := agents.BuildMergedAgents(ctx, bridge.Client().Graph)
	if err != nil {
		return fmt.Errorf("build merged agents: %w", err)
	}

	// Seed
	if err := agents.SeedAgentList(ctx, bridge.Client(), builtIns); err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	return nil
}

// graphClientWrapper wraps a raw HTTP client for graph API calls,
// avoiding the need for a full memory.Bridge just for simple queries.
type graphClientWrapper struct {
	serverURL string
	apiKey    string
	projectID string
	http      *http.Client
}

func newGraphClient(serverURL, apiKey, projectID string) (*graphClientWrapper, error) {
	return &graphClientWrapper{
		serverURL: serverURL,
		apiKey:    apiKey,
		projectID: projectID,
		http:      http.DefaultClient,
	}, nil
}

// GetObject fetches a single graph object by ID.
func (c *graphClientWrapper) GetObject(ctx context.Context, id string) (*struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
}, error) {
	url := fmt.Sprintf("%s/api/graph/objects/%s", c.serverURL, id)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-Project-ID", c.projectID)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("not found")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var obj struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// SearchObjectsResponse mirrors the SDK response for list queries.
type SearchObjectsResponse struct {
	Items []struct {
		ID         string         `json:"id"`
		CanonicalID string        `json:"canonical_id"`
		EntityID   string         `json:"entity_id"`
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
	} `json:"items"`
	Total int `json:"total"`
}

// ListObjects queries graph objects by type.
func (c *graphClientWrapper) ListObjects(ctx context.Context, objType string, limit int) (*SearchObjectsResponse, error) {
	url := fmt.Sprintf("%s/api/graph/objects/search?type=%s&limit=%d", c.serverURL, objType, limit)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-Project-ID", c.projectID)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var result SearchObjectsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateObject updates a graph object's properties.
func (c *graphClientWrapper) UpdateObject(ctx context.Context, id string, properties map[string]any) error {
	body := map[string]any{"properties": properties}
	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/api/graph/objects/%s", c.serverURL, id)
	req, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-Project-ID", c.projectID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error: %d", resp.StatusCode)
	}
	return nil
}

// toStringSlice converts []interface{} to []string for hash computation.
func toStringSlice(raw []interface{}) []string {
	result := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
