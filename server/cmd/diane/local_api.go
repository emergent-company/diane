package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// localAPIServer serves the local companion API on 127.0.0.1.
// Endpoints consumed by DianeCompanion (DianeAPIClient.swift):
//
//	GET /api/status              → {"ok": true}
//	GET /api/sessions?status=…   → {"items": [GraphObject...]}
//	GET /api/sessions/{id}/messages → {"items": [GraphObject...]}
//	GET /api/mcp-servers         → {"servers": [...]}
//	GET /api/nodes               → {"nodes": [...]}
type localAPIServer struct {
	server *http.Server
}

// startLocalAPI creates and starts the local companion API server on the given port.
// Returns immediately — the server runs in its own goroutine.
func startLocalAPI(pc *config.ProjectConfig, port int) (*localAPIServer, error) {
	mux := http.NewServeMux()

	api := &apiHandlers{pc: pc}
	mux.HandleFunc("/api/status", api.handleStatus)
	mux.HandleFunc("/api/sessions", api.handleSessions)
	mux.HandleFunc("/api/sessions/", api.handleSessionMessages)
	mux.HandleFunc("/api/mcp-servers", api.handleMCPServers)
	mux.HandleFunc("/api/nodes", api.handleNodes)

	srv := &localAPIServer{
		server: &http.Server{
			Addr:    fmt.Sprintf("127.0.0.1:%d", port),
			Handler: mux,
		},
	}

	go func() {
		log.Printf("[LOCAL-API] Listening on http://127.0.0.1:%d", port)
		if err := srv.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[LOCAL-API] Server error: %v", err)
		}
	}()

	return srv, nil
}

// close shuts down the local API server.
func (s *localAPIServer) close() {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.server.Shutdown(ctx)
	}
}

// apiHandlers holds shared state for HTTP handlers.
type apiHandlers struct {
	pc *config.ProjectConfig
}

// bridge creates and returns a memory bridge from the project config.
func (h *apiHandlers) bridge(ctx context.Context) (*memory.Bridge, error) {
	return memory.New(memory.Config{
		ServerURL:         h.pc.ServerURL,
		APIKey:            h.pc.Token,
		ProjectID:         h.pc.ProjectID,
		OrgID:             h.pc.OrgID,
		HTTPClientTimeout: 10 * time.Second,
	})
}

// handleStatus returns a simple health check.
func (h *apiHandlers) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSessions lists sessions, optionally filtered by status.
func (h *apiHandlers) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	status := r.URL.Query().Get("status")
	resp, err := bridge.Client().Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: "Session",
	})
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	// Filter by status client-side if requested
	items := resp.Items
	if status != "" {
		var filtered []*graph.GraphObject
		for _, obj := range items {
			if obj.Properties != nil {
				if s, ok := obj.Properties["status"].(string); ok && s == status {
					filtered = append(filtered, obj)
				}
			}
		}
		items = filtered
	}

	if items == nil {
		items = []*graph.GraphObject{}
	}

	writeJSON(w, map[string]any{"items": items})
}

// handleSessionMessages returns messages for a session.
func (h *apiHandlers) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from path: /api/sessions/{id}/messages
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	sessionID := strings.TrimSuffix(path, "/messages")
	if sessionID == "" || strings.Contains(sessionID, "/") {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	bridge, err := h.bridge(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer bridge.Close()

	resp, err := bridge.Client().Graph.ListMessages(ctx, sessionID, 100, "")
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	items := resp.Items
	if items == nil {
		items = []*graph.GraphObject{}
	}

	writeJSON(w, map[string]any{"items": items})
}

// handleMCPServers returns the list of configured MCP servers.
func (h *apiHandlers) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	cfg, err := mcpproxy.LoadConfig(mcpproxy.GetDefaultConfigPath())
	if err != nil {
		// No config file is normal — return empty list
		writeJSON(w, map[string]any{"servers": []any{}})
		return
	}

	type serverEntry struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Enabled bool   `json:"enabled"`
		Command string `json:"command,omitempty"`
		URL     string `json:"url,omitempty"`
	}

	servers := make([]serverEntry, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		entry := serverEntry{
			Name:    s.Name,
			Type:    s.Type,
			Enabled: s.Enabled,
		}
		if s.Type == "stdio" {
			entry.Command = s.Command
		} else {
			entry.URL = s.URL
		}
		servers = append(servers, entry)
	}

	writeJSON(w, map[string]any{"servers": servers})
}

// handleNodes returns connected relay nodes from the Memory Platform.
func (h *apiHandlers) handleNodes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	relayURL := strings.TrimSuffix(h.pc.ServerURL, "/") + "/api/mcp-relay/sessions"

	req, err := http.NewRequestWithContext(ctx, "GET", relayURL, nil)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.pc.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, map[string]any{"nodes": []any{}, "error": err.Error()})
		return
	}
	defer resp.Body.Close()

	var nodes any
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		writeJSON(w, map[string]any{"nodes": []any{}})
		return
	}

	writeJSON(w, map[string]any{"nodes": nodes})
}

// writeJSON marshals v as JSON and writes it to w with Content-Type header.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[LOCAL-API] JSON encode error: %v", err)
	}
}
