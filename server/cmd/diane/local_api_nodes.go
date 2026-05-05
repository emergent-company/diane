// Package: main
// Node listing and detail HTTP handlers.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"github.com/Emergent-Comapny/diane/internal/memory"
)

func (a *localAPIServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// ── 1. Query registered nodes from the graph (DianeNodeConfig) ──
	ctx := r.Context()
	registeredNodes, graphErr := a.bridge.ListNodeConfigs(ctx)

	// ── 2. Query active MCP relay sessions (for online status) ──
	relayURL := strings.TrimSuffix(a.config.ServerURL, "/") + "/api/mcp-relay/sessions"
	req, err := http.NewRequest("GET", relayURL, nil)
	if err != nil {
		req = nil // proceed without relay data
	} else {
		req.Header.Set("Authorization", "Bearer "+a.config.Token)
	}

	var onlineSessions []relaySessionData
	if req != nil {
		resp, err2 := httpClient.Do(req)
		if err2 == nil && resp.StatusCode == 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			// Parse response formats
			if err := json.Unmarshal(body, &onlineSessions); err != nil {
				var wrapped struct {
					Items    []relaySessionData `json:"items"`
					Data     []relaySessionData `json:"data"`
					Sessions []relaySessionData `json:"sessions"`
				}
				if err2 := json.Unmarshal(body, &wrapped); err2 == nil {
					switch {
					case wrapped.Sessions != nil:
						onlineSessions = wrapped.Sessions
					case wrapped.Items != nil:
						onlineSessions = wrapped.Items
					case wrapped.Data != nil:
						onlineSessions = wrapped.Data
					}
				}
			}
		}
	}

	// Build online lookup: instance_id -> relay session data
	online := make(map[string]relaySessionData)
	for _, s := range onlineSessions {
		online[s.InstanceID] = s
	}

	// Build registered lookup: instance_id -> node config
	registered := make(map[string]memory.NodeConfig)
	if graphErr == nil {
		for _, nc := range registeredNodes {
			registered[nc.InstanceID] = nc
		}
	}

	// ── 3. Merge: use registered nodes as base, add online-only as fallback ──
	type nodeJSON struct {
		InstanceID  string `json:"instance_id"`
		Hostname    string `json:"hostname,omitempty"`
		Mode        string `json:"mode,omitempty"`
		Version     string `json:"version,omitempty"`
		ToolCount   int    `json:"tool_count,omitempty"`
		ConnectedAt string `json:"connected_at,omitempty"`
		Online      bool   `json:"online"`
		Uptime      string `json:"uptime,omitempty"`
		Provider    string `json:"provider,omitempty"`
		RelayActive bool   `json:"relay_active,omitempty"`
		BotActive   bool   `json:"bot_active,omitempty"`
		Healthy     bool   `json:"healthy,omitempty"`
	}

	// A node is online if it has an active relay session OR a heartbeat within the last 10 minutes.
	heartbeatGrace := 10 * time.Minute
	now := time.Now()
	isOnline := func(nc memory.NodeConfig) bool {
		if _, ok := online[nc.InstanceID]; ok {
			return true
		}
		if nc.LastSeen != "" {
			seenAt, err := time.Parse(time.RFC3339, nc.LastSeen)
			if err == nil && now.Sub(seenAt) <= heartbeatGrace {
				return true
			}
		}
		return false
	}

	seen := make(map[string]bool)
	nodes := make([]nodeJSON, 0)

	// First: all registered nodes from graph (with online status from relay)
	for _, nc := range registeredNodes {
		seen[nc.InstanceID] = true
		n := nodeJSON{
			InstanceID:  nc.InstanceID,
			Hostname:    nc.Hostname,
			Mode:        nc.Mode,
			Version:     nc.Version,
			Uptime:      nc.Uptime,
			Provider:    nc.Provider,
			RelayActive: nc.RelayActive,
			BotActive:   nc.BotActive,
			Healthy:     nc.Healthy,
		}
		if s, ok := online[nc.InstanceID]; ok {
			n.Online = true
			n.ToolCount = s.ToolCount
			if s.ToolCount == 0 && s.Tools != nil {
				if toolsMap, ok := s.Tools.(map[string]interface{}); ok {
					if tl, ok := toolsMap["tools"].([]interface{}); ok {
						n.ToolCount = len(tl)
					}
				}
			}
			n.ConnectedAt = s.ConnectedAt
			// Prefer relay version if more specific
			if s.Version != "" {
				n.Version = s.Version
			}
		}
		n.Online = isOnline(nc)
		nodes = append(nodes, n)
	}

	// Second: online-only nodes (registered in relay but not yet in graph — older nodes)
	for _, s := range onlineSessions {
		if seen[s.InstanceID] {
			continue
		}
		toolCount := s.ToolCount
		if toolCount == 0 && s.Tools != nil {
			if toolsMap, ok := s.Tools.(map[string]interface{}); ok {
				if tl, ok := toolsMap["tools"].([]interface{}); ok {
					toolCount = len(tl)
				}
			}
		}
		nodes = append(nodes, nodeJSON{
			InstanceID:  s.InstanceID,
			Hostname:    s.Hostname,
			Version:     s.Version,
			ToolCount:   toolCount,
			ConnectedAt: s.ConnectedAt,
			Online:      true,
		})
	}

	jsonResponse(w, map[string]any{
		"nodes": nodes,
		"total": len(nodes),
	})
}

// GET /api/nodes/{instanceId}/tools — get MCP tools for a specific relay node
func (a *localAPIServer) handleNodeByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract instance ID from path: /api/nodes/{instanceId}/tools
	path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] != "tools" {
		jsonError(w, http.StatusNotFound, "use /api/nodes/{instanceId}/tools")
		return
	}
	instanceID := parts[0]
	if instanceID == "" {
		jsonError(w, http.StatusBadRequest, "instance ID required")
		return
	}

	toolsURL := strings.TrimSuffix(a.config.ServerURL, "/") + "/api/mcp-relay/sessions/" + instanceID + "/tools"
	req, err := http.NewRequest("GET", toolsURL, nil)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("create request: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+a.config.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("query tools: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		jsonError(w, resp.StatusCode, fmt.Sprintf("tools API returned %d", resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("read tools: %v", err))
		return
	}

	// The tools response is the raw MCP tools/list result
	var tools struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(body, &tools); err != nil {
		// Try as a bare array
		var bareTools []map[string]any
		if err2 := json.Unmarshal(body, &bareTools); err2 != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("parse tools: %v", err))
			return
		}
		tools.Tools = bareTools
	}

	type toolJSON struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}

	items := make([]toolJSON, 0, len(tools.Tools))
	for _, t := range tools.Tools {
		name, _ := t["name"].(string)
		desc, _ := t["description"].(string)
		if name != "" {
			items = append(items, toolJSON{
				Name:        name,
				Description: desc,
			})
		}
	}

	jsonResponse(w, map[string]any{
		"tools": items,
		"total": len(items),
	})
}

// GET /api/status — health check with version and uptime
