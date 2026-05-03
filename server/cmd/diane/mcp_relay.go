// Command: diane mcp relay
//
// Connects Diane's MCP server to the Memory Platform's MCP Relay,
// allowing the cloud agentic runner to call local tools.
//
// Architecture:
//
//	emergent.memory (agentic runner)
//	    │ HTTP /api/v1/mcp/relay/{session}/call
//	    ▼
//	MCP Relay Server (on memory.emergent-company.ai)
//	    │ WebSocket (persistent, outbound)
//	    ▼
// diane mcp relay (in-process)
//     │ mcpproxy.Proxy + handleMCPServeRequest()
//     ▼
// Diane MCP Tools (built-in + proxied MCP servers)
//
// Each Diane instance connects with a unique instance ID,
// registered with its tool list for discovery.

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/gorilla/websocket"
)

// MCPRelayConfig holds the configuration for the relay connection.
type MCPRelayConfig struct {
	// RelayURL is the WebSocket endpoint on the Memory Platform.
	// e.g., wss://memory.emergent-company.ai/mcp/relay
	RelayURL string

	// ServerURL is the Memory Platform HTTP endpoint for API calls.
	// e.g., https://memory.emergent-company.ai
	// Used for syncing config from the graph on config_changed notifications.
	ServerURL string

	// ProjectID is the Memory Platform project ID.
	// Used for syncing config from the graph on config_changed notifications.
	ProjectID string

	// InstanceID is a unique identifier for this Diane instance.
	// e.g., "laptop-mac", "server-linux", "companion-mac"
	// Used by the agentic runner to route tool calls.
	InstanceID string

	// ProjectToken authenticates this instance against the relay.
	ProjectToken string

	// ReconnectDelay is the initial delay between reconnection attempts.
	// Increases with exponential backoff (30s, 60s, 120s, capped at 300s).
	ReconnectDelay time.Duration
}

// MCPSession manages the WebSocket relay connection and in-process MCP handler.
type MCPSession struct {
	cfg     MCPRelayConfig
	proxy   *mcpproxy.Proxy // shared MCP proxy (manages sub-MCP servers)
	wsConn  *websocket.Conn
	wsMutex sync.Mutex
	done    chan struct{}
	version string // build version for registration
}

// upsertNodeConfigInGraph registers this node's config in the Memory Platform graph.
// This is a best-effort operation — failures are logged but not fatal.
func upsertNodeConfigInGraph(pc *config.ProjectConfig, instanceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bridge, err := memory.New(memory.Config{
		ServerURL:         pc.ServerURL,
		APIKey:            pc.Token,
		ProjectID:         pc.ProjectID,
		OrgID:             pc.OrgID,
		HTTPClientTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Printf("[mcp-relay] Warning: cannot create bridge for node config: %v", err)
		return
	}
	defer bridge.Close()

	hostname, _ := os.Hostname()
	mode := "master"
	if pc.IsSlave() {
		mode = "slave"
	}

	nc := &memory.NodeConfig{
		InstanceID: instanceID,
		Hostname:   hostname,
		Mode:       mode,
		Version:    Version,
		LastSeen:   time.Now().UTC().Format(time.RFC3339),
	}

	if _, err := bridge.UpsertNodeConfig(ctx, nc); err != nil {
		log.Printf("[mcp-relay] Warning: failed to upsert node config: %v", err)
	} else {
		log.Printf("[mcp-relay] Node config registered in graph (instance=%s, mode=%s)", instanceID, mode)
	}
}

func cmdMCPRelay(cfg MCPRelayConfig) {
	// Resolve defaults
	if cfg.ReconnectDelay == 0 {
		cfg.ReconnectDelay = 5 * time.Second
	}

	log.Printf("[mcp-relay] Starting relay for instance: %s", cfg.InstanceID)
	log.Printf("[mcp-relay] Relay server: %s", cfg.RelayURL)

	// Create MCP proxy in-process (no subprocess)
	configPath := mcpproxy.GetDefaultConfigPath()
	proxy, err := mcpproxy.NewProxy(configPath)
	if err != nil {
		log.Printf("[mcp-relay] Warning: failed to create MCP proxy: %v", err)
	}
	defer func() {
		if proxy != nil {
			proxy.Close()
		}
	}()

	session := &MCPSession{
		cfg:     cfg,
		proxy:   proxy,
		done:    make(chan struct{}),
		version: Version,
	}

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("[mcp-relay] Shutting down...")
		close(session.done)
		session.disconnectWS()
	}()

	// Main reconnect loop
	backoff := cfg.ReconnectDelay
	for {
		select {
		case <-session.done:
			return
		default:
		}

		err := session.run()
		if err != nil {
			log.Printf("[mcp-relay] Connection error: %v (reconnecting in %v)", err, backoff)
			select {
			case <-session.done:
				return
			case <-time.After(backoff):
			}
			// Exponential backoff, cap at 5 minutes
			backoff *= 2
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}
		} else {
			// Clean exit (shutdown requested or successful reconnect cycle)
			backoff = cfg.ReconnectDelay
		}
	}
}

func (s *MCPSession) run() error {
	// 1. Connect to relay
	u, _ := url.Parse(s.cfg.RelayURL)
	query := u.Query()
	query.Set("instance", s.cfg.InstanceID)
	u.RawQuery = query.Encode()

	header := make(http.Header)
	header.Set("Authorization", "Bearer "+s.cfg.ProjectToken)

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	conn, _, err := dialer.Dial(u.String(), header)
	if err != nil {
		return fmt.Errorf("connect to relay: %w", err)
	}
	s.wsConn = conn
	log.Printf("[mcp-relay] Connected to relay: %s (instance: %s)", s.cfg.RelayURL, s.cfg.InstanceID)

	// Set up WebSocket keepalive: send pings every 25s to prevent
	// server-side proxy (Traefik) from killing idle connections.
	// Use binary-level ping/pong (RFC 6455) so Traefik forwards them.
	const (
		pingPeriod = 25 * time.Second
		pongWait   = 50 * time.Second
	)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					return
				}
			case <-s.done:
				return
			}
		}
	}()

	// Send initial register message with tool list
	s.sendRegister()

	// Start background tool watch — periodically checks for new/changed MCP
	// tools and re-registers. This picks up slow-starting servers (AirMCP)
	// without blocking initial registration.
	s.startToolWatch()

	defer s.disconnectWS()

	// Forward loop: WS → in-process handler → WS response
	errCh := make(chan error, 1)
	go func() {
		for {
			select {
			case <-s.done:
				return
			default:
			}

			_, msg, err := conn.ReadMessage()
			if err != nil {
				errCh <- fmt.Errorf("ws read: %w", err)
				return
			}

			var frame RelayFrame
			if err := json.Unmarshal(msg, &frame); err != nil {
				log.Printf("[mcp-relay] Invalid WS frame: %v", err)
				continue
			}

			switch frame.Type {
			case "request":
				// Parse JSON-RPC request from payload
				var req struct {
					JSONRPC string          `json:"jsonrpc"`
					ID      interface{}     `json:"id"`
					Method  string          `json:"method"`
					Params  json.RawMessage `json:"params,omitempty"`
				}
				if err := json.Unmarshal(frame.Payload, &req); err != nil {
					log.Printf("[mcp-relay] Invalid request payload: %v", err)
					continue
				}

				// Handle in-process via shared handler
				resp := handleMCPServeRequest(req, s.proxy)
				resp.JSONRPC = "2.0"
				resp.ID = req.ID

				// Wrap in response frame for relay routing
				respData, _ := json.Marshal(resp)
				wrapped := map[string]interface{}{
					"type":    "response",
					"id":      req.ID,
					"payload": json.RawMessage(respData),
				}
				wrappedData, _ := json.Marshal(wrapped)
				s.sendWS(wrappedData)

			case "ping":
				s.sendWS(json.RawMessage(`{"type":"pong"}`))

			case "config_changed":
				log.Printf("[mcp-relay] Received config_changed notification — reloading MCP config from graph")
				go func() {
					syncConfigFromGraph(s.cfg.ServerURL, s.cfg.ProjectToken, s.cfg.ProjectID, s.cfg.InstanceID)
					if s.proxy != nil {
						if err := s.proxy.Reload(); err != nil {
							log.Printf("[mcp-relay] Config reload failed: %v", err)
						} else {
							log.Printf("[mcp-relay] MCP config reloaded successfully")
							// Re-register with updated tool list
							s.sendRegister()
						}
					}
				}()
			}
		}
	}()

	// Wait for first error or clean shutdown
	select {
	case err := <-errCh:
		return err
	case <-s.done:
		return nil
	}
}

// sendRegister registers tools with the relay immediately on connection.
func (s *MCPSession) sendRegister() {
	// Build tool list directly from proxy (no subprocess)
	tools := buildMCPToolList()
	if s.proxy != nil {
		proxiedTools, err := s.proxy.ListAllTools()
		if err != nil {
			log.Printf("[mcp-relay] Failed to list proxied tools: %v", err)
		} else if proxiedTools != nil {
			tools = append(tools, proxiedTools...)
		}
	}

	toolsData, _ := json.Marshal(map[string]interface{}{"tools": tools})
	s.doRegister(toolsData)
}

// doRegister sends a register frame to the relay with the current tool list.
func (s *MCPSession) doRegister(data []byte) {
	hostname, _ := os.Hostname()
	reg := RegisterFrame{
		Type:       "register",
		InstanceID: s.cfg.InstanceID,
		Hostname:   hostname,
		Version:    s.version,
		Tools:      json.RawMessage(data),
	}
	data, _ = json.Marshal(reg)
	s.sendWS(data)

	log.Printf("[mcp-relay] Registered with relay: %s", s.cfg.InstanceID)
}

// startToolWatch periodically checks for new/changed tools and re-registers.
// This picks up slow-starting MCP servers (like AirMCP) without blocking.
func (s *MCPSession) startToolWatch() {
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Check tools directly (no subprocess)
				tools := buildMCPToolList()
				if s.proxy != nil {
					proxiedTools, err := s.proxy.ListAllTools()
					if err == nil && proxiedTools != nil {
						tools = append(tools, proxiedTools...)
					}
				}
				toolsData, _ := json.Marshal(map[string]interface{}{"tools": tools})
				log.Printf("[mcp-relay] Tools watch: re-registering...")
				s.doRegister(toolsData)
			case <-s.done:
				return
			}
		}
	}()
}

func (s *MCPSession) disconnectWS() {
	s.wsMutex.Lock()
	defer s.wsMutex.Unlock()
	if s.wsConn != nil {
		s.wsConn.Close()
		s.wsConn = nil
	}
}

func (s *MCPSession) sendWS(msg json.RawMessage) {
	s.wsMutex.Lock()
	defer s.wsMutex.Unlock()
	if s.wsConn != nil {
		if err := s.wsConn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("[mcp-relay] WS write error: %v", err)
		}
	}
}

// RelayFrame is the wire format for WS messages.
type RelayFrame struct {
	Type    string          `json:"type"`
	ID      json.RawMessage `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RegisterFrame is sent by Diane on connection.
type RegisterFrame struct {
	Type       string          `json:"type"`
	InstanceID string          `json:"instance_id"`
	Hostname   string          `json:"hostname"` // machine hostname for identification
	Version    string          `json:"version"`
	Tools      json.RawMessage `json:"tools"`
}

// ── Graph Config Sync ──

// scoredConfig represents a matched proxy config with a scope match score.
type scoredConfig struct {
	config  string
	version int
	score   int // higher = more specific match
}

// syncConfigFromGraph pulls MCP proxy config and secrets from the MP graph
// before starting the relay. This lets one node define config centrally
// and other nodes auto-pull it without SSH or shared filesystem.
func syncConfigFromGraph(serverURL, token, projectID, instanceID string) {
	// Try memory CLI first (works on master where it's installed)
	memoryCLI := findMemoryCLI()
	if memoryCLI != "" {
		syncConfigFromGraphViaCLI(memoryCLI, serverURL, token, projectID, instanceID)
		return
	}

	// Fall back to SDK bridge (works everywhere the relay can connect to MP)
	syncConfigFromGraphViaSDK(serverURL, token, projectID, instanceID)
}

// syncConfigFromGraphViaCLI uses the memory CLI to sync MCP config and secrets.
func syncConfigFromGraphViaCLI(memoryCLI, serverURL, token, projectID, instanceID string) {
	dianeDir := filepath.Join(os.Getenv("HOME"), ".diane")
	os.MkdirAll(dianeDir, 0755)
	os.MkdirAll(filepath.Join(dianeDir, "secrets"), 0755)

	// ── Query MCPProxyConfig objects ──
	configs, err := queryGraphObjects(memoryCLI, serverURL, token, projectID, "MCPProxyConfig")
	if err != nil {
		log.Printf("[mcp-relay] Failed to query MCPProxyConfig: %v", err)
	} else {
		// Match by scope: collect all configs that apply to this instance
		var matched []scoredConfig
		for _, obj := range configs {
			scope := getPropString(obj, "scope")
			cfgStr := getPropString(obj, "config")
			ver := getPropInt(obj, "version")
			score := scopeMatchScore(scope, instanceID)
			if score > 0 && cfgStr != "" {
				matched = append(matched, scoredConfig{config: cfgStr, version: ver, score: score})
			}
		}

		if len(matched) > 0 {
			// Merge configs (highest score wins on conflict)
			mergedConfig := mergeProxyConfigsWithLocal(matched, dianeDir)
			configPath := filepath.Join(dianeDir, "mcp-servers.json")
			if err := os.WriteFile(configPath, []byte(mergedConfig), 0644); err != nil {
				log.Printf("[mcp-relay] Failed to write mcp-servers.json: %v", err)
			} else {
				log.Printf("[mcp-relay] Synced MCP proxy config from graph (%d matching, merged to %s)", len(matched), configPath)
			}
		} else {
			log.Printf("[mcp-relay] No MCPProxyConfig objects found for scope matching '%s'", instanceID)
		}
	}

	// ── Query MCPSecret objects ──
	secrets, err := queryGraphObjects(memoryCLI, serverURL, token, projectID, "MCPSecret")
	if err != nil {
		log.Printf("[mcp-relay] Failed to query MCPSecret: %v", err)
	} else {
		written := 0
		for _, obj := range secrets {
			scope := getPropString(obj, "scope")
			name := getPropString(obj, "name")
			value := getPropString(obj, "value")
			if scopeMatchScore(scope, instanceID) > 0 && name != "" && value != "" {
				secretPath := filepath.Join(dianeDir, "secrets", name)
				// If filename doesn't end in .json, add it
				if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
					secretPath += ".json"
				}
				if err := os.WriteFile(secretPath, []byte(value), 0600); err != nil {
					log.Printf("[mcp-relay] Failed to write secret %s: %v", name, err)
				} else {
					written++
				}
			}
		}
		if written > 0 {
			log.Printf("[mcp-relay] Synced %d secrets from graph to %s/secrets/", written, dianeDir)
		}
	}
}

// syncConfigFromGraphViaSDK uses the SDK bridge to sync MCP config and secrets.
// This works without the memory CLI installed.
func syncConfigFromGraphViaSDK(serverURL, token, projectID, instanceID string) {
	dianeDir := filepath.Join(os.Getenv("HOME"), ".diane")
	os.MkdirAll(dianeDir, 0755)
	os.MkdirAll(filepath.Join(dianeDir, "secrets"), 0755)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Load full config to get OrgID
	cfg, err := config.Load()
	var orgID string
	if err == nil {
		pc := cfg.Active()
		if pc != nil {
			orgID = pc.OrgID
		}
	}

	bridge, err := memory.New(memory.Config{
		ServerURL:         serverURL,
		APIKey:            token,
		ProjectID:         projectID,
		OrgID:             orgID,
		HTTPClientTimeout: 15 * time.Second,
	})
	if err != nil {
		log.Printf("[mcp-relay] Failed to create bridge for graph sync: %v", err)
		return
	}
	defer bridge.Close()

	client := bridge.Client()

	// ── Query MCPProxyConfig objects ──
	configResp, err := client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  "MCPProxyConfig",
		Limit: 100,
	})
	if err != nil {
		log.Printf("[mcp-relay] Failed to query MCPProxyConfig via SDK: %v", err)
	} else if configResp != nil {
		var matched []scoredConfig
		for _, obj := range configResp.Items {
			if obj == nil || obj.Properties == nil {
				continue
			}
			scope := getPropAnyString(obj.Properties, "scope")
			cfgStr := getPropAnyString(obj.Properties, "config")
			ver := getPropAnyInt(obj.Properties, "version")
			score := scopeMatchScore(scope, instanceID)
			if score > 0 && cfgStr != "" {
				matched = append(matched, scoredConfig{config: cfgStr, version: ver, score: score})
			}
		}
		if len(matched) > 0 {
			mergedConfig := mergeProxyConfigsWithLocal(matched, dianeDir)
			if mergedConfig != "" {
				configPath := filepath.Join(dianeDir, "mcp-servers.json")
				if err := os.WriteFile(configPath, []byte(mergedConfig), 0644); err != nil {
					log.Printf("[mcp-relay] Failed to write mcp-servers.json: %v", err)
				} else {
					log.Printf("[mcp-relay] Synced MCP proxy config via SDK (%d matching)", len(matched))
				}
			}
		} else {
			log.Printf("[mcp-relay] No MCPProxyConfig objects found via SDK for scope matching '%s'", instanceID)
		}
	}

	// ── Query MCPSecret objects ──
	secretResp, err := client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  "MCPSecret",
		Limit: 100,
	})
	if err != nil {
		log.Printf("[mcp-relay] Failed to query MCPSecret via SDK: %v", err)
	} else if secretResp != nil {
		written := 0
		for _, obj := range secretResp.Items {
			if obj == nil || obj.Properties == nil {
				continue
			}
			scope := getPropAnyString(obj.Properties, "scope")
			name := getPropAnyString(obj.Properties, "name")
			value := getPropAnyString(obj.Properties, "value")
			if scopeMatchScore(scope, instanceID) > 0 && name != "" && value != "" {
				secretPath := filepath.Join(dianeDir, "secrets", name)
				if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
					secretPath += ".json"
				}
				if err := os.WriteFile(secretPath, []byte(value), 0600); err != nil {
					log.Printf("[mcp-relay] Failed to write secret %s: %v", name, err)
				} else {
					written++
				}
			}
		}
		if written > 0 {
			log.Printf("[mcp-relay] Synced %d secrets from graph via SDK to %s/secrets/", written, dianeDir)
		}
	}
}

// getPropAnyString extracts a string from a map[string]any (the SDK type).
func getPropAnyString(props map[string]any, key string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getPropAnyInt extracts an int from a map[string]any.
func getPropAnyInt(props map[string]any, key string) int {
	if v, ok := props[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case json.Number:
			i, _ := n.Int64()
			return int(i)
		}
	}
	return 0
}

// findMemoryCLI locates the memory CLI binary.
func findMemoryCLI() string {
	paths := []string{
		filepath.Join(os.Getenv("HOME"), ".memory", "bin", "memory"),
		"/root/.memory/bin/memory",
		"/usr/local/bin/memory",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fall back to PATH
	if _, err := exec.LookPath("memory"); err == nil {
		return "memory"
	}
	return ""
}

// queryGraphObjects queries the Memory Platform graph for objects of a given type.
// Returns parsed JSON objects.
func queryGraphObjects(memoryCLI, serverURL, token, projectID, objectType string) ([]map[string]interface{}, error) {
	cmd := exec.Command(memoryCLI, "graph", "objects", "list",
		"--type", objectType,
		"--server", serverURL,
		"--project-token", token,
		"--project", projectID,
		"--output", "json",
		"--limit", "100",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %s", err, string(out))
	}

	var objects []map[string]interface{}
	if err := json.Unmarshal(out, &objects); err != nil {
		// Try single object response
		var single map[string]interface{}
		if err2 := json.Unmarshal(out, &single); err2 == nil {
			// Wrap single object in array
			objects = []map[string]interface{}{single}
		} else {
			return nil, fmt.Errorf("parse %s list: %w", objectType, err)
		}
	}
	return objects, nil
}

// scopeMatchScore returns how well a scope matches an instance ID.
// 3 = exact match, 2 = scope starts with instance prefix, 1 = scope=all, 0 = no match
func scopeMatchScore(scope, instanceID string) int {
	if scope == "" || scope == "all" {
		return 1
	}
	// Strip "instance:" prefix from scope for comparison
	scopeID := scope
	if strings.HasPrefix(scope, "instance:") {
		scopeID = strings.TrimPrefix(scope, "instance:")
	}
	if scopeID == instanceID {
		return 3
	}
	if strings.HasPrefix(instanceID, scopeID) || strings.HasPrefix(scopeID, instanceID) {
		return 2
	}
	return 0
}

// getPropString extracts a string property from a graph object.
func getPropString(obj map[string]interface{}, key string) string {
	props, _ := obj["properties"].(map[string]interface{})
	if props == nil {
		return ""
	}
	v, _ := props[key].(string)
	return v
}

// getPropInt extracts an integer property from a graph object.
func getPropInt(obj map[string]interface{}, key string) int {
	props, _ := obj["properties"].(map[string]interface{})
	if props == nil {
		return 0
	}
	v, _ := props[key].(float64)
	return int(v)
}

// mergeProxyConfigs merges multiple scored MCP proxy configs into one.
// Higher-scored configs override lower-scored ones at the server level.
func mergeProxyConfigs(configs []scoredConfig) string {
	if len(configs) == 0 {
		return `{"servers":[]}`
	}
	if len(configs) == 1 {
		return configs[0].config
	}

	// Parse all configs and merge servers
	type serverDef struct {
		Name    string            `json:"name"`
		Enabled bool              `json:"enabled"`
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		URL     string            `json:"url,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
		Timeout int               `json:"timeout,omitempty"`
	}
	type proxyCfg struct {
		Servers []serverDef `json:"servers"`
	}

	merged := proxyCfg{Servers: []serverDef{}}
	seen := map[string]int{} // server name → index in merged.Servers

	// Sort by score ascending so higher scores override lower
	sort.Slice(configs, func(i, j int) bool {
		return configs[i].score < configs[j].score
	})

	for _, sc := range configs {
		var cfg proxyCfg
		if err := json.Unmarshal([]byte(sc.config), &cfg); err != nil {
			// Try as a single server config (not wrapped in {"servers":[...]})
			var single serverDef
			if err2 := json.Unmarshal([]byte(sc.config), &single); err2 == nil && single.Name != "" {
				cfg.Servers = append(cfg.Servers, single)
			} else {
				continue
			}
		}
		for _, s := range cfg.Servers {
			if idx, ok := seen[s.Name]; ok {
				merged.Servers[idx] = s // override
			} else {
				seen[s.Name] = len(merged.Servers)
				merged.Servers = append(merged.Servers, s)
			}
		}
	}

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		log.Printf("[mcp-relay] Failed to marshal merged config: %v", err)
		return `{"servers":[]}`
	}
	return string(data)
}

// mergeProxyConfigsWithLocal merges graph-synced MCP proxy configs with the
// existing local config. Graph servers override local servers, but local
// servers not in the graph result are preserved.
func mergeProxyConfigsWithLocal(configs []scoredConfig, dianeDir string) string {
	merged := mergeProxyConfigs(configs)
	if merged == "" || merged == `{"servers":[]}` {
		// Nothing useful from graph — preserve local config
		localPath := filepath.Join(dianeDir, "mcp-servers.json")
		if data, err := os.ReadFile(localPath); err == nil {
			return string(data)
		}
		return merged
	}

	// Parse merged graph config
	var graphCfg struct {
		Servers []struct {
			Name string `json:"name"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(merged), &graphCfg); err != nil {
		return merged
	}

	// Build set of graph server names
	graphNames := make(map[string]bool)
	for _, s := range graphCfg.Servers {
		graphNames[s.Name] = true
	}

	// Read local config
	localPath := filepath.Join(dianeDir, "mcp-servers.json")
	localData, err := os.ReadFile(localPath)
	if err != nil {
		return merged // no local config, just use graph
	}

	type serverDef struct {
		Name    string            `json:"name"`
		Enabled bool              `json:"enabled"`
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		URL     string            `json:"url,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
		Timeout int               `json:"timeout,omitempty"`
	}
	type proxyCfg struct {
		Servers []serverDef `json:"servers"`
	}

	var localCfg proxyCfg
	if err := json.Unmarshal(localData, &localCfg); err != nil {
		return merged
	}

	// Parse graph config fully
	var fullGraphCfg proxyCfg
	json.Unmarshal([]byte(merged), &fullGraphCfg)

	// Build final list: graph servers first, then local servers not in graph
	seen := make(map[string]bool)
	var result proxyCfg
	for _, s := range fullGraphCfg.Servers {
		seen[s.Name] = true
		result.Servers = append(result.Servers, s)
	}
	for _, s := range localCfg.Servers {
		if !seen[s.Name] {
			result.Servers = append(result.Servers, s)
		}
	}

	data, _ := json.Marshal(result)
	return string(data)
}

// generateInstanceID creates a random instance ID like "diane-a4fd".
func generateInstanceID() string {
	bytes := make([]byte, 2)
	if _, err := rand.Read(bytes); err != nil {
		log.Printf("[mcp-relay] Failed to generate random instance ID: %v", err)
		return "diane-" + fmt.Sprintf("%04x", time.Now().UnixNano()&0xFFFF)
	}
	return "diane-" + hex.EncodeToString(bytes)
}

// ── Actual CLI integration ──

// Actual CLI integration — called from main.go's command switch
func runMCPRelay(args []string) {
	// Parse optional flags
	instanceID := ""
	relayURL := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--instance":
			if i+1 < len(args) {
				instanceID = args[i+1]
				i++
			}
		case "--relay":
			if i+1 < len(args) {
				relayURL = args[i+1]
				i++
			}
		case "--help", "-h":
			fmt.Println("Usage: diane mcp relay [--instance <name>] [--relay <url>]")
			fmt.Println("")
			fmt.Println("Connects Diane's MCP tools to the Memory Platform relay.")
			fmt.Println("")
			fmt.Println("Flags:")
			fmt.Println("  --instance     Unique instance name (default: from config or auto-generated)")
			fmt.Println("  --relay        Relay WebSocket URL")
			fmt.Println("                 (default: from config, derived from server URL)")
			os.Exit(0)
		}
	}

	// For JSON mode, acknowledge and exit (don't start the daemon)
	if jsonOutput {
		emitJSON("ok", map[string]interface{}{
			"message":   "Starting relay",
			"instance":  instanceID,
			"relay_url": relayURL,
		})
		return
	}

	// Load config for token & relay URL
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	pc := cfg.Active()
	if pc == nil {
		log.Fatal("No project configured. Run 'diane init' first.")
	}

	// Resolve instance ID: --instance flag > config > auto-generate
	if instanceID == "" {
		if pc.InstanceID != "" {
			instanceID = pc.InstanceID
		} else {
			// Auto-generate: diane-<4 random hex chars>
			instanceID = generateInstanceID()
			pc.InstanceID = instanceID
			if err := cfg.Save(); err != nil {
				log.Printf("[mcp-relay] Warning: failed to save instance ID: %v", err)
			} else {
				log.Printf("[mcp-relay] Generated instance ID: %s (saved to config)", instanceID)
			}
		}
	}

	if relayURL == "" {
		// Default: derive from server URL
		// https://memory.emergent-company.ai → wss://memory.emergent-company.ai/api/mcp-relay/connect
		relayURL = "wss://" + strings.TrimPrefix(pc.ServerURL, "https://") + "/api/mcp-relay/connect"
	}

	relayCfg := MCPRelayConfig{
		RelayURL:     relayURL,
		ServerURL:    pc.ServerURL,
		ProjectID:    pc.ProjectID,
		InstanceID:   instanceID,
		ProjectToken: pc.Token,
	}

	// Sync MCP proxy config and secrets from the MP graph
	// This lets one node define config centrally and other nodes auto-pull it
	syncConfigFromGraph(pc.ServerURL, pc.Token, pc.ProjectID, instanceID)

	// Register this node's config in the graph so other nodes can discover it
	upsertNodeConfigInGraph(pc, instanceID)

	cmdMCPRelay(relayCfg)
}
