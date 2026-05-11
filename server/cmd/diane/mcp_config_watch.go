package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// mcpWatchCredentials stores the connection parameters needed to re-query
// the graph for MCPProxyConfig changes. Set once by startMCPConfigWatch.
var mcpWatchCreds struct {
	mu         sync.Mutex
	serverURL  string
	token      string
	projectID  string
	instanceID string
}

// mcpWatchHash tracks the last-seen combined hash of MCPProxyConfig objects,
// so we only push config/set when something actually changed.
var mcpWatchHash string

// startMCPConfigWatch connects to the MP SSE stream and watches for
// MCPProxyConfig entity changes. On change, it re-queries the graph,
// re-merges by scope, and pushes the updated config to the running relay
// session via pushMCPConfigFunc.
//
// This auto-reloads MCP servers (infakt, etc.) when config changes in the
// graph without requiring a full diane serve restart.
func startMCPConfigWatch(ctx context.Context, serverURL, token, projectID, instanceID string) {
	mcpWatchCreds.mu.Lock()
	mcpWatchCreds.serverURL = serverURL
	mcpWatchCreds.token = token
	mcpWatchCreds.projectID = projectID
	mcpWatchCreds.instanceID = instanceID
	mcpWatchCreds.mu.Unlock()

	// Initial poll to establish baseline hash
	hash := pollMCPConfigAndPush(serverURL, token, projectID, instanceID, "")
	if hash != "" {
		mcpWatchHash = hash
		log.Printf("[mcp-config-watch] Initial MCP config sync complete (hash: %.12s)", hash)
	}

	// SSE reconnect loop
	backoff := 5 * time.Second
	maxBackoff := 2 * time.Minute

	for {
		select {
		case <-ctx.Done():
			log.Printf("[mcp-config-watch] Context cancelled, stopping")
			return
		default:
		}

		err := mcpSSESubscribe(ctx, serverURL, token, projectID, instanceID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[mcp-config-watch] SSE disconnected: %v (reconnect in %v)", err, backoff)
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
			backoff = 5 * time.Second
		}
	}
}

// mcpSSESubscribe connects to the MP SSE event stream and processes events
// for MCPProxyConfig changes.
func mcpSSESubscribe(ctx context.Context, serverURL, token, projectID, instanceID string) error {
	sseURL := fmt.Sprintf("%s/api/events/stream?projectId=%s", serverURL, projectID)

	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		return fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := sseHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("SSE unexpected status: %d", resp.StatusCode)
	}

	log.Printf("[mcp-config-watch] Connected to SSE stream")

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)

	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			currentEvent = ""
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			switch currentEvent {
			case "entity.created", "entity.updated", "entity.deleted":
				var evt sseEntityEvent
				if err := json.Unmarshal([]byte(data), &evt); err != nil {
					log.Printf("[mcp-config-watch] Failed to parse event data: %v", err)
					continue
				}
				handleMCPConfigEvent(ctx, evt, serverURL, token, projectID, instanceID)
			case "heartbeat":
				// no-op
			case "connected":
				log.Printf("[mcp-config-watch] SSE connected: %s", data[:min(len(data), 80)])
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

// handleMCPConfigEvent checks if the changed entity is an MCPProxyConfig
// and triggers a re-sync+push if so.
func handleMCPConfigEvent(ctx context.Context, evt sseEntityEvent, serverURL, token, projectID, instanceID string) {
	if evt.Entity != "graph_object" || evt.ID == "" {
		return
	}

	// Quick type check via graph client
	gc, err := newGraphClient(serverURL, token, projectID)
	if err != nil {
		log.Printf("[mcp-config-watch] Failed to create graph client for type check: %v", err)
		return
	}

	objRaw, err := gc.GetObject(ctx, evt.ID)
	if err != nil {
		log.Printf("[mcp-config-watch] Could not fetch entity %s (may be deleted): %v", evt.ID[:min(len(evt.ID), 12)], err)
		// Deleted entity — re-sync to be safe (might affect scope matching)
	} else if obj, ok := objRaw.(*graph.GraphObject); ok {
		if obj.Type != "" && obj.Type != "MCPProxyConfig" {
			return // not an MCP config change, skip
		}
		log.Printf("[mcp-config-watch] MCPProxyConfig entity changed (type=%s) — re-syncing config", obj.Type)
	} else {
		log.Printf("[mcp-config-watch] Unexpected entity type %T for %s — re-syncing to be safe", objRaw, evt.ID[:min(len(evt.ID), 12)])
	}

	// Re-query, re-merge, and push
	mcpWatchCreds.mu.Lock()
	sURL, tok, pID, iID := mcpWatchCreds.serverURL, mcpWatchCreds.token, mcpWatchCreds.projectID, mcpWatchCreds.instanceID
	mcpWatchCreds.mu.Unlock()

	if sURL == "" {
		return
	}

	hash := pollMCPConfigAndPush(sURL, tok, pID, iID, mcpWatchHash)
	if hash != "" {
		mcpWatchHash = hash
	}
}

// pollMCPConfigAndPush queries MCPProxyConfig objects from the graph,
// merges by scope, and if the hash changed, pushes the merged config to
// the running relay session. Returns the new hash (or empty if unchanged/failed).
func pollMCPConfigAndPush(serverURL, token, projectID, instanceID, lastHash string) string {
	memoryCLI := findMemoryCLI()
	if memoryCLI == "" {
		log.Printf("[mcp-config-watch] memory CLI not found, cannot sync")
		return ""
	}

	// Query MCPProxyConfig objects
	configs, err := queryGraphObjects(memoryCLI, serverURL, token, projectID, "MCPProxyConfig")
	if err != nil {
		log.Printf("[mcp-config-watch] Failed to query MCPProxyConfig: %v", err)
		return ""
	}

	// Match by scope
	type scoredCfg struct {
		config string
		score  int
	}
	var matched []scoredCfg
	for _, obj := range configs {
		scope := getPropString(obj, "scope")
		cfgStr := getPropString(obj, "config")
		score := scopeMatchScore(scope, instanceID)
		if score > 0 && cfgStr != "" {
			matched = append(matched, scoredCfg{config: cfgStr, score: score})
		}
	}

	if len(matched) == 0 {
		log.Printf("[mcp-config-watch] No MCPProxyConfig objects found for scope matching '%s'", instanceID)
		return ""
	}

	// Compute hash of matched configs
	hashInput := ""
	for _, mc := range matched {
		hashInput += mc.config + fmt.Sprintf(":%d;", mc.score)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(hashInput)))

	if hash == lastHash {
		return hash // unchanged
	}

	// Merge configs (highest score wins on conflict)
	var scoredConfigs []scoredConfig
	for _, mc := range matched {
		scoredConfigs = append(scoredConfigs, scoredConfig{config: mc.config, score: mc.score})
	}
	merged := mergeProxyConfigs(scoredConfigs)

	log.Printf("[mcp-config-watch] MCP config changed (hash: %.12s), pushing to relay session", hash)

	// Push to running relay session
	if pushMCPConfigFunc != nil {
		pushMCPConfigFunc(merged)
	} else {
		log.Printf("[mcp-config-watch] No active relay session to push to")
		graphMCPConfig = merged // store for next session
	}

	return hash
}
