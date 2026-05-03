package mcpproxy

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

// OAuthConfig holds OAuth 2.0 configuration for MCP server authentication.
type OAuthConfig struct {
	// Device flow (GitHub Copilot style)
	ClientID      string   `json:"client_id,omitempty"`
	DeviceAuthURL string   `json:"device_auth_url,omitempty"`
	TokenURL      string   `json:"token_url,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`

	// Authorization code flow (infakt style, headless-friendly)
	AuthorizationURL string `json:"authorization_url,omitempty"`

	// Dynamic client registration (RFC 7591)
	RegistrationURL string `json:"registration_url,omitempty"`

	// Static bearer token (pre-authenticated)
	BearerToken string `json:"bearer_token,omitempty"`
}

// DefaultToolTimeout is the default per-server tool call timeout in seconds.
// Applied when ServerConfig.Timeout is 0 or not set.
const DefaultToolTimeout = 60

// ServerConfig represents configuration for an MCP server
type ServerConfig struct {
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Type    string            `json:"type"` // stdio, http, sse, streamable-http
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url,omitempty"`     // HTTP/SSE endpoint URL
	Headers map[string]string `json:"headers,omitempty"` // Static HTTP headers for auth
	OAuth   *OAuthConfig      `json:"oauth,omitempty"`   // OAuth 2.0 configuration
	Timeout int               `json:"timeout,omitempty"` // Tool call timeout in seconds (0 = DefaultToolTimeout)
}

// Config represents the MCP proxy configuration
type Config struct {
	Servers []ServerConfig `json:"servers"`
}

// Proxy manages multiple MCP clients
type Proxy struct {
	clients    map[string]Client
	mu         sync.RWMutex
	notifyChan chan string // Aggregated notifications channel
}

// NewProxy creates a new MCP proxy from a list of server configs.
func NewProxy(servers []ServerConfig) (*Proxy, error) {
	proxy := &Proxy{
		clients:    make(map[string]Client),
		notifyChan: make(chan string, 10), // Buffered channel for notifications
	}

	// Start enabled MCP servers
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		switch server.Type {
		case "stdio":
			if err := proxy.startClient(server); err != nil {
				log.Printf("Failed to start MCP server %s: %v", server.Name, err)
			}
		case "http", "streamable-http", "sse":
			client, err := NewHTTPMCPClient(server.Name, server.URL, server.Headers, server.OAuth, server.Timeout)
			if err != nil {
				log.Printf("Failed to connect to HTTP MCP server %s: %v", server.Name, err)
				continue
			}
			proxy.mu.Lock()
			proxy.clients[server.Name] = client
			proxy.mu.Unlock()
			log.Printf("Connected to HTTP MCP server: %s", server.Name)
		default:
			log.Printf("Unknown MCP server type: %s for server %s", server.Type, server.Name)
		}
	}

	// Start notification monitor
	go proxy.monitorNotifications()

	return proxy, nil
}

// startClient starts an MCP client
func (p *Proxy) startClient(config ServerConfig) error {
	client, err := NewMCPClient(config.Name, config.Command, config.Args, config.Env, config.Timeout)
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.clients[config.Name] = client
	p.mu.Unlock()

	log.Printf("Started MCP server: %s", config.Name)
	return nil
}

// ListAllTools aggregates tools from all MCP clients
func (p *Proxy) ListAllTools() ([]map[string]interface{}, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var allTools []map[string]interface{}

	for serverName, client := range p.clients {
		tools, err := client.ListTools()
		if err != nil {
			log.Printf("Failed to list tools from %s: %v", serverName, err)
			continue
		}

		// Prefix tool names with server name to avoid conflicts
		for _, tool := range tools {
			if name, ok := tool["name"].(string); ok {
				tool["name"] = serverName + "_" + name
				tool["_server"] = serverName // Track which server this tool belongs to
			}
			allTools = append(allTools, tool)
		}
	}

	return allTools, nil
}

// ListServerTools returns tools for a specific MCP server by name.
// Returns nil if the server is not connected.
func (p *Proxy) ListServerTools(serverName string) ([]map[string]interface{}, error) {
	p.mu.RLock()
	client, ok := p.clients[serverName]
	p.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("MCP server '%s' not connected", serverName)
	}
	return client.ListTools()
}

// ListServerPrompts returns prompts for a specific MCP server by name.
// Returns nil if the server is not connected.
func (p *Proxy) ListServerPrompts(serverName string) ([]map[string]interface{}, error) {
	p.mu.RLock()
	client, ok := p.clients[serverName]
	p.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("MCP server '%s' not connected", serverName)
	}
	return client.ListPrompts()
}

// CallTool routes a tool call to the appropriate MCP client
func (p *Proxy) CallTool(toolName string, arguments map[string]interface{}) (json.RawMessage, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Parse server name from tool name (format: server_toolname)
	var serverName, actualToolName string
	for sName := range p.clients {
		prefix := sName + "_"
		if len(toolName) > len(prefix) && toolName[:len(prefix)] == prefix {
			serverName = sName
			actualToolName = toolName[len(prefix):]
			break
		}
	}

	if serverName == "" {
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}

	client, ok := p.clients[serverName]
	if !ok {
		return nil, fmt.Errorf("server not found: %s", serverName)
	}

	return client.CallTool(actualToolName, arguments)
}

// monitorNotifications watches all client notification channels
func (p *Proxy) monitorNotifications() {
	p.mu.RLock()
	for name, client := range p.clients {
		if client.NotificationChan() != nil {
			go p.monitorClient(name, client)
		}
	}
	p.mu.RUnlock()
}

// NotificationChan returns the channel for receiving aggregated notifications
func (p *Proxy) NotificationChan() <-chan string {
	return p.notifyChan
}

// Reload replaces the full server list, stopping removed servers and starting new ones.
func (p *Proxy) Reload(servers []ServerConfig) error {
	log.Printf("Reloading MCP configuration with %d servers", len(servers))

	p.mu.Lock()
	defer p.mu.Unlock()

	// Build map of new enabled servers
	newServers := make(map[string]ServerConfig)
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		switch s.Type {
		case "stdio", "http", "streamable-http", "sse":
			newServers[s.Name] = s
		default:
			log.Printf("Unknown MCP server type in reload: %s for server %s", s.Type, s.Name)
		}
	}

	// Stop removed servers
	for name, client := range p.clients {
		if _, exists := newServers[name]; !exists {
			log.Printf("Stopping removed MCP server: %s", name)
			client.Close()
			delete(p.clients, name)
		}
	}

	// Start new servers
	for name, serverConfig := range newServers {
		if _, exists := p.clients[name]; !exists {
			log.Printf("Starting new MCP server: %s", name)
			if err := p.startClientUnlocked(serverConfig); err != nil {
				log.Printf("Failed to start %s: %v", name, err)
			}
		}
	}

	// Send notification that tools changed
	select {
	case p.notifyChan <- "config-reload":
		log.Printf("Sent config-reload notification")
	default:
		log.Printf("Notification channel full, dropping config-reload notification")
	}

	log.Printf("MCP configuration reload complete")
	return nil
}

// startClientUnlocked starts a client (assumes lock is held by caller)
func (p *Proxy) startClientUnlocked(config ServerConfig) error {
	var client Client
	var err error

	switch config.Type {
	case "stdio":
		client, err = NewMCPClient(config.Name, config.Command, config.Args, config.Env, config.Timeout)
	case "http", "streamable-http", "sse":
		client, err = NewHTTPMCPClient(config.Name, config.URL, config.Headers, config.OAuth, config.Timeout)
	default:
		return fmt.Errorf("unknown MCP server type: %s", config.Type)
	}

	if err != nil {
		return err
	}

	p.clients[config.Name] = client

	// Start monitoring this client's notifications (only if it supports them)
	if client.NotificationChan() != nil {
		go p.monitorClient(config.Name, client)
	}

	log.Printf("Started MCP server: %s", config.Name)
	return nil
}

// monitorClient monitors a single client for notifications
func (p *Proxy) monitorClient(clientName string, client Client) {
	for method := range client.NotificationChan() {
		if method == "notifications/tools/list_changed" {
			log.Printf("[%s] Tools changed, forwarding notification", clientName)
			select {
			case p.notifyChan <- clientName:
			default:
				log.Printf("Proxy notification channel full, dropping notification from %s", clientName)
			}
		}
	}
}

// Close shuts down all MCP clients
func (p *Proxy) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for name, client := range p.clients {
		log.Printf("Shutting down MCP server: %s", name)
		if err := client.Close(); err != nil {
			log.Printf("Error closing %s: %v", name, err)
		}
	}

	p.clients = make(map[string]Client)
	return nil
}

// MergeServerConfigs merges multiple ServerConfig lists, deduplicating by name.
// Later entries with the same name override earlier ones.
func MergeServerConfigs(lists ...[]ServerConfig) []ServerConfig {
	byName := make(map[string]ServerConfig)
	order := make([]string, 0)
	for _, list := range lists {
		for _, s := range list {
			if _, exists := byName[s.Name]; !exists {
				order = append(order, s.Name)
			}
			byName[s.Name] = s
		}
	}
	result := make([]ServerConfig, len(order))
	for i, name := range order {
		result[i] = byName[name]
	}
	return result
}
