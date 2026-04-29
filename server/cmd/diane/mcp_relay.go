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
//	diane mcp relay (this process)
//	    │ stdin/stdout
//	    ▼
//	Diane MCP Server (mcp/server.go subprocess)
//
// Each Diane instance connects with a unique instance ID,
// registered with its tool list for discovery.

package main

import (
	"bufio"
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
	"github.com/gorilla/websocket"
)

// MCPRelayConfig holds the configuration for the relay connection.
type MCPRelayConfig struct {
	// RelayURL is the WebSocket endpoint on the Memory Platform.
	// e.g., wss://memory.emergent-company.ai/mcp/relay
	RelayURL string

	// InstanceID is a unique identifier for this Diane instance.
	// e.g., "laptop-mac", "server-linux", "companion-mac"
	// Used by the agentic runner to route tool calls.
	InstanceID string

	// ProjectToken authenticates this instance against the relay.
	ProjectToken string

	// MCPBinary is the path to the MCP server binary plus arguments.
	// Defaults to "diane mcp serve" (spawns itself as MCP subprocess).
	MCPBinary string

	// ReconnectDelay is the initial delay between reconnection attempts.
	// Increases with exponential backoff (30s, 60s, 120s, capped at 300s).
	ReconnectDelay time.Duration
}

// MCPSession manages the MCP server subprocess and WS relay connection.
type MCPSession struct {
	cfg     MCPRelayConfig
	mcpCmd  *exec.Cmd
	mcpIn   *bufio.Writer
	mcpOut  *bufio.Scanner
	wsConn  *websocket.Conn
	wsMutex sync.Mutex
	mcpMu   sync.Mutex // protects mcpIn writes from tool watch vs forward loops
	pending sync.Map   // map[requestID]chan response
	done    chan struct{}

	// Dynamic tool registration: register immediately on connect, then
	// re-register when new MCP servers appear (slow starters like AirMCP
	// get picked up via periodic tool watch).
	toolWatchID int64 // incrementing request ID for tool watch polls
}

func cmdMCPRelay(cfg MCPRelayConfig) {
	// Resolve defaults
	if cfg.MCPBinary == "" {
		exe, _ := os.Executable()
		cfg.MCPBinary = exe + " mcp serve"
	}
	if cfg.ReconnectDelay == 0 {
		cfg.ReconnectDelay = 5 * time.Second
	}

	log.Printf("[mcp-relay] Starting relay for instance: %s", cfg.InstanceID)
	log.Printf("[mcp-relay] Relay server: %s", cfg.RelayURL)
	log.Printf("[mcp-relay] MCP binary: %s", cfg.MCPBinary)

	session := &MCPSession{cfg: cfg, done: make(chan struct{})}

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("[mcp-relay] Shutting down...")
		close(session.done)
		session.stopMCP()
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
	// 1. Start MCP server subprocess
	if err := s.startMCP(); err != nil {
		return fmt.Errorf("start MCP: %w", err)
	}
	defer s.stopMCP()

	// 2. Connect to relay
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

	// 3. Forward loop: WS → MCP stdin
	errCh := make(chan error, 2)

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
				s.forwardToMCP(frame)
			case "ping":
				s.sendWS(json.RawMessage(`{"type":"pong"}`))
			}
		}
	}()

	// 4. Forward loop: MCP stdout → WS, with tool-change detection and
	//    ResponseFrame wrapping for relay-proxied tool calls.
	go func() {
		for s.mcpOut.Scan() {
			line := s.mcpOut.Bytes()

			// Check if this is a tools/list response from our watch loop
			// (watch IDs start at 1000000). If tools changed, re-register.
			var respMeta struct {
				ID float64 `json:"id"`
			}
			if err := json.Unmarshal(line, &respMeta); err == nil && respMeta.ID >= 999999 {
				s.checkToolChangeAndReregister(line)
			}

			// Extract response ID for relay request matching.
			var respID struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(line, &respID); err != nil {
				log.Printf("[mcp-relay] Failed to parse response ID from MCP output: %v", err)
			}

			// If this response matches a pending relay tool call, wrap it in
			// a ResponseFrame so the MP server's relay handler can route it.
			if respID.ID != "" {
				if _, ok := s.pending.LoadAndDelete(respID.ID); ok {
					wrapped := map[string]interface{}{
						"type":    "response",
						"id":      respID.ID,
						"payload": json.RawMessage(line),
					}
					wrappedData, _ := json.Marshal(wrapped)
					s.sendWS(wrappedData)
					continue
				}
			}

			// All other MCP output: forward raw (tool list updates, etc.)
			var resp json.RawMessage
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}
			s.sendWS(resp)
		}
		if err := s.mcpOut.Err(); err != nil {
			errCh <- fmt.Errorf("mcp read: %w", err)
		} else {
			errCh <- fmt.Errorf("mcp process exited")
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

func (s *MCPSession) startMCP() error {
	// Split binary path and args (supports "diane mcp serve" etc.)
	parts := strings.Fields(s.cfg.MCPBinary)
	cmd := exec.Command(parts[0], parts[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	s.mcpCmd = cmd
	s.mcpIn = bufio.NewWriter(stdin)
	s.mcpOut = bufio.NewScanner(bufio.NewReader(stdout))
	s.mcpOut.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	log.Printf("[mcp-relay] MCP server started (PID: %d)", cmd.Process.Pid)
	return nil
}

func (s *MCPSession) stopMCP() {
	if s.mcpCmd != nil && s.mcpCmd.Process != nil {
		s.mcpCmd.Process.Signal(syscall.SIGTERM)
		go func() {
			time.Sleep(5 * time.Second)
			if s.mcpCmd != nil && s.mcpCmd.Process != nil {
				s.mcpCmd.Process.Kill()
			}
		}()
		s.mcpCmd.Wait()
		s.mcpCmd = nil
	}
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

func (s *MCPSession) forwardToMCP(frame RelayFrame) {
	// Extract the request ID from the JSON-RPC payload so we can match
	// the response when it comes back from the MCP subprocess.
	var payloadMeta struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(frame.Payload, &payloadMeta); err == nil && payloadMeta.ID != nil {
		// json.RawMessage for a string contains JSON quotes — unmarshal to string.
		var idStr string
		if err := json.Unmarshal(payloadMeta.ID, &idStr); err == nil && idStr != "" {
			s.pending.Store(idStr, true)
		}
	}

	// Send the MCP JSON-RPC request to the subprocess
	s.mcpMu.Lock()
	if _, err := s.mcpIn.Write(frame.Payload); err != nil {
		log.Printf("[mcp-relay] Error writing to MCP stdin: %v", err)
		s.mcpMu.Unlock()
		return
	}
	if err := s.mcpIn.WriteByte('\n'); err != nil {
		log.Printf("[mcp-relay] Error writing newline to MCP stdin: %v", err)
		s.mcpMu.Unlock()
		return
	}
	if err := s.mcpIn.Flush(); err != nil {
		log.Printf("[mcp-relay] Error flushing MCP stdin: %v", err)
	}
	s.mcpMu.Unlock()
}

func (s *MCPSession) sendRegister() {
	// Single tools/list poll — register immediately with whatever is available.
	// Slow-starting servers (AirMCP) get picked up later via toolWatchLoop.
	initMsg := json.RawMessage(`{"jsonrpc":"2.0","id":0,"method":"tools/list","params":{}}`)
	s.mcpMu.Lock()
	s.mcpIn.Write(initMsg)
	s.mcpIn.WriteByte('\n')
	s.mcpIn.Flush()
	s.mcpMu.Unlock()

	if s.mcpOut.Scan() {
		s.doRegister(s.mcpOut.Bytes())
	} else {
		// MCP process didn't respond — register with empty tools
		s.doRegister(json.RawMessage(`{"tools":[]}`))
	}
}

// doRegister registers (or re-registers) tools with the relay. Stores the
// tool list for comparison on subsequent updates.
func (s *MCPSession) doRegister(data []byte) {
	var mcpResponse struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	toolsData := data
	if err := json.Unmarshal(data, &mcpResponse); err == nil {
		if mcpResponse.Error != nil {
			log.Printf("[mcp-relay] Failed to list tools: %s (code %d)", mcpResponse.Error.Message, mcpResponse.Error.Code)
			toolsData = json.RawMessage(`{"tools":[]}`)
		} else if mcpResponse.Result != nil {
			toolsData = mcpResponse.Result
		}
	}

	// Send register frame to relay
	hostname, _ := os.Hostname()
	reg := RegisterFrame{
		Type:       "register",
		InstanceID: s.cfg.InstanceID,
		Hostname:   hostname,
		Version:    "1.0",
		Tools:      json.RawMessage(toolsData),
	}
	data, _ = json.Marshal(reg)
	s.sendWS(data)

	log.Printf("[mcp-relay] Registered with relay: %s", s.cfg.InstanceID)
}

// startToolWatch starts a background loop that periodically checks for new or
// changed MCP tools and re-registers them with the relay. This picks up
// slow-starting MCP servers (like AirMCP) without blocking initial registration.
func (s *MCPSession) startToolWatch() {
	s.toolWatchID = 999999
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.toolWatchID++
				reqID := s.toolWatchID
				msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/list","params":{}}`, reqID)
				s.mcpMu.Lock()
				s.mcpIn.WriteString(msg)
				s.mcpIn.WriteByte('\n')
				s.mcpIn.Flush()
				s.mcpMu.Unlock()
			case <-s.done:
				return
			}
		}
	}()
}

// checkToolChangeAndReregister re-registers tools with the relay when the
// tool watch loop detects a tools/list response. Called from the MCP stdout
// forwarding goroutine. We always re-register on watch responses — the
// overhead is negligible (one WS frame per 20s), and it ensures slow-starting
// servers like AirMCP get registered as soon as their tools appear.
func (s *MCPSession) checkToolChangeAndReregister(line []byte) {
	// Verify the response has tools data
	var mcpResponse struct {
		Result *struct {
			Tools []json.RawMessage `json:"tools"`
		} `json:"result,omitempty"`
		Tools []json.RawMessage `json:"tools,omitempty"`
	}
	if err := json.Unmarshal(line, &mcpResponse); err != nil {
		return
	}
	if mcpResponse.Result == nil && mcpResponse.Tools == nil {
		return
	}

	log.Printf("[mcp-relay] Tools update detected, re-registering...")
	s.doRegister(line)
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

// init registers the "relay" subcommand
func init() {
	// This runs when the diane CLI starts — registers the relay command
	// via the command routing in main.go
}

// RelayFrame is the wire format for WS messages.

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
	memoryCLI := findMemoryCLI()
	if memoryCLI == "" {
		log.Printf("[mcp-relay] memory CLI not found, skipping graph config sync")
		return
	}

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
			merged := mergeProxyConfigs(matched)
			configPath := filepath.Join(dianeDir, "mcp-servers.json")
			if err := os.WriteFile(configPath, []byte(merged), 0644); err != nil {
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
	if scope == instanceID {
		return 3
	}
	if strings.HasPrefix(instanceID, scope) || strings.HasPrefix(scope, instanceID) {
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
			continue
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

// ── Actual CLI integration ──

// generateInstanceID creates a random instance ID like "diane-a4fd".
func generateInstanceID() string {
	bytes := make([]byte, 2)
	if _, err := rand.Read(bytes); err != nil {
		log.Printf("[mcp-relay] Failed to generate random instance ID: %v", err)
		return "diane-" + fmt.Sprintf("%04x", time.Now().UnixNano()&0xFFFF)
	}
	return "diane-" + hex.EncodeToString(bytes)
}

// Actual CLI integration — called from main.go's command switch
func runMCPRelay(args []string) {
	// Parse optional flags
	instanceID := ""
	relayURL := ""
	mcpBinary := ""
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
		case "--mcp-binary":
			if i+1 < len(args) {
				mcpBinary = args[i+1]
				i++
			}
		case "--help", "-h":
			fmt.Println("Usage: diane mcp relay [--instance <name>] [--relay <url>] [--mcp-binary <path>]")
			fmt.Println("")
			fmt.Println("Connects Diane's MCP tools to the Memory Platform relay.")
			fmt.Println("")
			fmt.Println("Flags:")
			fmt.Println("  --instance     Unique instance name (default: from config or auto-generated)")
			fmt.Println("  --relay        Relay WebSocket URL")
			fmt.Println("                 (default: from config, derived from server URL)")
			fmt.Println("  --mcp-binary   Path to MCP server binary (default: self with \"mcp serve\" subcommand)")
			os.Exit(0)
		}
	}

	// For JSON mode, acknowledge and exit (don't start the daemon)
	if jsonOutput {
		emitJSON("ok", map[string]interface{}{
			"message":    "Starting relay",
			"instance":   instanceID,
			"relay_url":  relayURL,
			"mcp_binary": mcpBinary,
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
		InstanceID:   instanceID,
		ProjectToken: pc.Token,
		MCPBinary:    mcpBinary,
	}

	// Sync MCP proxy config and secrets from the MP graph
	// This lets one node define config centrally and other nodes auto-pull it
	syncConfigFromGraph(pc.ServerURL, pc.Token, pc.ProjectID, instanceID)

	cmdMCPRelay(relayCfg)
}
