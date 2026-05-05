// Package: main
// Core: struct, constructor, middleware, helpers, route registration.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/Emergent-Comapny/diane/internal/schema"
)

// localAPIServer manages the local HTTP API for the companion app.
type localAPIServer struct {
	server    *http.Server
	config    *config.ProjectConfig
	bridge    *memory.Bridge
	port      int
	proxy     *mcpproxy.Proxy
	proxyOnce sync.Once
	startedAt time.Time
}

// startLocalAPI starts a local HTTP API server on 127.0.0.1:port.
// It serves the companion app with session data, MCP server config, and node info.
func startLocalAPI(pc *config.ProjectConfig, port int) (*localAPIServer, error) {
	// Create bridge for session operations
	bridge, err := memory.New(memory.Config{
		ServerURL:         pc.ServerURL,
		APIKey:            pc.Token,
		ProjectID:         pc.ProjectID,
		OrgID:             pc.OrgID,
		HTTPClientTimeout: 15 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("local api: bridge: %w", err)
	}

	api := &localAPIServer{
		config:    pc,
		bridge:    bridge,
		port:      port,
		startedAt: time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", api.handleSessions)
	mux.HandleFunc("/api/sessions/", api.handleSessionByID)
	mux.HandleFunc("/api/mcp-servers", api.handleMCPServers)
	mux.HandleFunc("/api/mcp-servers/", api.handleMCPServerByID)
	mux.HandleFunc("/api/mcp-servers/store", api.handleMCPSave)
	mux.HandleFunc("/api/mcp-servers/toggle/", api.handleMCPToggle)
	mux.HandleFunc("/api/mcp-servers/delete/", api.handleMCPDelete)
	mux.HandleFunc("/api/agents", api.handleAgents)
	mux.HandleFunc("/api/agents/", api.handleAgentDetail)
	mux.HandleFunc("/api/agents/seed", api.handleAgentSeed)
	mux.HandleFunc("/api/nodes", api.handleNodes)
	mux.HandleFunc("/api/nodes/", api.handleNodeByID)
	mux.HandleFunc("/api/schema", api.handleSchema)
	mux.HandleFunc("/api/schema/objects/", api.handleSchemaObjects)
	mux.HandleFunc("/api/providers", api.handleProjectProviders)
	mux.HandleFunc("/api/status", api.handleStatus)
	mux.HandleFunc("/api/stats", api.handleStats)
	mux.HandleFunc("/api/stats/providers", api.handleProviderStats)
	mux.HandleFunc("/api/stats/objects", api.handleGraphObjectStats)
	mux.HandleFunc("/api/chat/send", api.handleChatSend)
	mux.HandleFunc("/api/doctor", api.handleDoctor)
	mux.HandleFunc("/api/bugreport", api.handleBugReport)

	api.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: corsMiddleware(mux),
	}

	go func() {
		log.Printf("[LOCAL-API] Listening on 127.0.0.1:%d", port)
		if err := api.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[LOCAL-API] Server error: %v", err)
		}
	}()

	return api, nil
}

// close shuts down the local API server.
func (a *localAPIServer) close() {
	if a.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		a.server.Shutdown(ctx)
	}
	if a.bridge != nil {
		a.bridge.Close()
	}
	if a.proxy != nil {
		a.proxy.Close()
	}
}

// ensureProxy lazily starts the MCP proxy for tool/prompt discovery.
// Safe to call multiple times — only initializes once.
// Servers are loaded from the graph, not a local config file.
func (a *localAPIServer) ensureProxy() {
	a.proxyOnce.Do(func() {
		p, err := mcpproxy.NewProxy(nil)
		if err != nil {
			log.Printf("[LOCAL-API] Failed to start MCP proxy: %v", err)
			return
		}
		a.proxy = p
		log.Printf("[LOCAL-API] MCP proxy started")
	})
}

// ─── Handlers ────────────────────────────────────────────────

// GET /api/sessions — list all sessions
// POST /api/sessions — create a new session

func jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// corsMiddleware adds permissive CORS headers for localhost access.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// safeStrAny safely extracts a string from a map by key.
func safeStrAny(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return ""
	}
}

// safeIntAny safely extracts an int from a map by key.
func safeIntAny(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

// safeBoolAny safely extracts a bool from a map by key.
func safeBoolAny(m map[string]any, key string) bool {
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

// SchemaAPIResponse is the JSON response for GET /api/schema.
type SchemaAPIResponse struct {
	NodeTypes     []schema.EnrichedSchemaType  `json:"node_types"`
	Relationships []schema.EnrichedRelationship `json:"relationships"`
}

// GET /api/schema — returns embedded graph schema definitions (object types + relationships),
// enriched with per-type object counts from the project's Memory Platform.
