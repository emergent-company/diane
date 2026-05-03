package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

// cmdMCPServe runs the MCP server that reads JSON-RPC from stdin and writes to stdout.
// This is used when invoked standalone as 'diane mcp serve'.
func cmdMCPServe() {
	// For JSON mode, acknowledge and exit (don't start the daemon)
	if jsonOutput {
		emitJSON("ok", map[string]interface{}{
			"message": "Starting MCP server",
			"pid":     os.Getpid(),
		})
		return
	}

	// Write PID file
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}
	pidFile := filepath.Join(home, ".diane", "mcp.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		log.Printf("Warning: Failed to write PID file: %v", err)
	}
	defer os.Remove(pidFile)

	// Initialize MCP proxy (servers loaded from graph, not local file)
	proxy, err := mcpproxy.NewProxy(nil)
	if err != nil {
		log.Printf("Warning: Failed to initialize MCP proxy: %v", err)
	}
	defer func() {
		if proxy != nil {
			proxy.Close()
		}
	}()

	// MCP servers communicate via stdin/stdout
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	// Setup signal handler for reload (SIGUSR1)
	if proxy != nil {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGUSR1)
		go func() {
			for range sigChan {
				log.Printf("Received SIGUSR1, reloading MCP configuration...")
				if err := proxy.Reload(nil); err != nil {
					log.Printf("Failed to reload MCP config: %v", err)
				}
			}
		}()
	}

	// Main MCP loop
	for {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      interface{}     `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			log.Printf("Failed to decode request: %v", err)
			break
		}

		resp := handleMCPServeRequest(req, proxy)
		resp.JSONRPC = "2.0"
		resp.ID = req.ID
		if err := encoder.Encode(resp); err != nil {
			log.Printf("Failed to encode response: %v", err)
			break
		}
	}
}

type mcpServeResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func handleMCPServeRequest(
	req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      interface{}     `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	},
	proxy *mcpproxy.Proxy,
) mcpServeResponse {
	switch req.Method {
	case "initialize":
		return mcpServeResponse{
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{
						"listChanged": true,
					},
				},
				"serverInfo": map[string]interface{}{
					"name":    "diane",
					"version": "dev",
				},
			},
		}
	case "tools/list":
		tools := buildMCPToolList()

		// Add proxied tools
		if proxy != nil {
			proxiedTools, err := proxy.ListAllTools()
			if err != nil {
				log.Printf("Failed to list proxied tools: %v", err)
			} else if proxiedTools != nil {
				tools = append(tools, proxiedTools...)
			}
		}

		return mcpServeResponse{
			Result: map[string]interface{}{
				"tools": tools,
			},
		}
	case "tools/call":
		if proxy == nil {
			return mcpServeResponse{
				Error: &struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}{
					Code:    -32603,
					Message: "proxy not initialized",
				},
			}
		}

		var params struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return mcpServeResponse{
				Error: &struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}{
					Code:    -32602,
					Message: fmt.Sprintf("invalid params: %v", err),
				},
			}
		}

		// Handle built-in MCP management tools
		switch params.Name {
		case "mcp_add":
			result, err := handleMCPAdd(params.Arguments)
			if err != nil {
				return mcpServeResponse{
					Error: &struct {
						Code    int    `json:"code"`
						Message string `json:"message"`
					}{
						Code:    -32603,
						Message: err.Error(),
					},
				}
			}
			return mcpServeResponse{Result: result}

		case "mcp_test":
			result, err := handleMCPTest(params.Arguments, proxy)
			if err != nil {
				return mcpServeResponse{
					Error: &struct {
						Code    int    `json:"code"`
						Message string `json:"message"`
					}{
						Code:    -32603,
						Message: err.Error(),
					},
				}
			}
			return mcpServeResponse{Result: result}

		case "mcp_status":
			result, err := handleMCPStatus(proxy)
			if err != nil {
				return mcpServeResponse{
					Error: &struct {
						Code    int    `json:"code"`
						Message string `json:"message"`
					}{
						Code:    -32603,
						Message: err.Error(),
					},
				}
			}
			return mcpServeResponse{Result: result}
		}

		// Forward to proxied MCP servers
		result, err := proxy.CallTool(params.Name, params.Arguments)
		if err != nil {
			return mcpServeResponse{
				Error: &struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}{
					Code:    -32603,
					Message: err.Error(),
				},
			}
		}

		var resultObj interface{}
		if err := json.Unmarshal(result, &resultObj); err != nil {
			return mcpServeResponse{
				Error: &struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}{
					Code:    -32603,
					Message: fmt.Sprintf("failed to parse tool result: %v", err),
				},
			}
		}

		return mcpServeResponse{
			Result: resultObj,
		}
	default:
		return mcpServeResponse{
			Error: &struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}{
				Code:    -32601,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

// buildMCPToolList returns the built-in MCP tools.
// Providers have been removed — all functionality comes from proxied MCP servers.
// MCP management tools (mcp_add, mcp_test, mcp_status) are available to all instances.
func buildMCPToolList() []map[string]interface{} {
	tools := []map[string]interface{}{
		{
			"name":        "node_status",
			"description": "Check if diane server is running",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		// ── MCP Management Tools ──
		{
			"name":        "mcp_add",
			"description": "Add or update an MCP server configuration. Syncs to Memory Platform graph with scope targeting for multi-node deployment.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Unique server name",
					},
					"scope": map[string]interface{}{
						"type":        "string",
						"description": "Node scope: 'all', 'instance:<id>', 'slave:*', 'master:*'",
						"default":     "all",
					},
					"type": map[string]interface{}{
						"type":        "string",
						"description": "Server type: stdio, http, streamable-http, sse",
						"default":     "stdio",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command path (for stdio type)",
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL endpoint (for http/sse type)",
					},
					"headers": map[string]interface{}{
						"type":        "string",
						"description": "HTTP headers as JSON object string",
					},
					"env": map[string]interface{}{
						"type":        "string",
						"description": "Environment variables as JSON object string",
					},
					"timeout": map[string]interface{}{
						"type":        "number",
						"description": "Tool call timeout in seconds",
						"default":     60,
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "mcp_test",
			"description": "Test connectivity to an MCP server. Runs initialize + tools/list + a quick tool call and reports status, tool count, and latency.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Server name to test (as configured in the Memory Platform graph)",
					},
					"type": map[string]interface{}{
						"type":        "string",
						"description": "Server type override (stdio, http)",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command override (for stdio)",
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL override (for http)",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "mcp_status",
			"description": "Show status of all configured MCP servers, registered relay instances, and tool counts. Overview of MCP infrastructure health.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	return tools
}

// ============================================================================
// MCP Management Tool Handlers
// ============================================================================

// handleMCPAdd adds or updates an MCP server configuration.
// Syncs to the MP graph (no local file write).
func handleMCPAdd(args map[string]interface{}) (map[string]interface{}, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	scope, _ := args["scope"].(string)
	if scope == "" {
		scope = "all"
	}
	srvType, _ := args["type"].(string)
	if srvType == "" {
		srvType = "stdio"
	}
	command, _ := args["command"].(string)
	url, _ := args["url"].(string)
	timeout := 60
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
	}

	// Parse headers (JSON string → map)
	var headers map[string]string
	if h, ok := args["headers"].(string); ok && h != "" {
		if err := json.Unmarshal([]byte(h), &headers); err != nil {
			return nil, fmt.Errorf("invalid headers JSON: %w", err)
		}
	}

	// Parse env (JSON string → map)
	var env map[string]string
	if e, ok := args["env"].(string); ok && e != "" {
		if err := json.Unmarshal([]byte(e), &env); err != nil {
			return nil, fmt.Errorf("invalid env JSON: %w", err)
		}
	}

	server := mcpproxy.ServerConfig{
		Name:    name,
		Enabled: true,
		Type:    srvType,
		Command: command,
		URL:     url,
		Headers: headers,
		Env:     env,
		Timeout: timeout,
	}

	// Sync to MP graph (single source of truth — no local file write)
	pc := loadActiveProjectConfig()
	if pc != nil && pc.Token != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		bridge, err := memory.New(memory.Config{
			ServerURL: pc.ServerURL, APIKey: pc.Token,
			ProjectID: pc.ProjectID, OrgID: pc.OrgID,
			HTTPClientTimeout: 15 * time.Second,
		})
		if err == nil {
			serverData, _ := json.Marshal(server)
			_ = bridge.UpsertMCPProxyConfig(ctx, &memory.MCPProxyConfigRequest{
				Scope: scope, Config: string(serverData),
				Version: int(time.Now().Unix()),
			})
			bridge.Close()
		}
	}

	return map[string]interface{}{
		"ok":      true,
		"name":    name,
		"scope":   scope,
		"type":    srvType,
		"message": fmt.Sprintf("MCP server %q added with scope %q", name, scope),
	}, nil
}

// handleMCPTest tests connectivity to an MCP server.
func handleMCPTest(args map[string]interface{}, proxy *mcpproxy.Proxy) (map[string]interface{}, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	// Build server config from arguments (no local file read)
	var serverCfg = &mcpproxy.ServerConfig{
		Name: name,
	}
	if t, ok := args["type"].(string); ok {
		serverCfg.Type = t
	}
	if c, ok := args["command"].(string); ok {
		serverCfg.Command = c
	}
	if u, ok := args["url"].(string); ok {
		serverCfg.URL = u
	}

	// Connect and test
	start := time.Now()

	// Try proxy first (if already connected), then direct
	if proxy != nil {
		tools, err := proxy.ListServerTools(name)
		if err == nil && tools != nil {
			latency := time.Since(start).Milliseconds()
			return map[string]interface{}{
				"ok":         true,
				"name":       name,
				"status":     "connected",
				"tool_count": len(tools),
				"latency_ms": latency,
				"via":        "proxy",
			}, nil
		}
	}

	// Direct connection
	var client mcpproxy.Client
	var err error
	if serverCfg.Type == "http" || serverCfg.Type == "sse" || serverCfg.Type == "streamable-http" {
		client, err = mcpproxy.NewHTTPMCPClient(serverCfg.Name, serverCfg.URL, serverCfg.Headers, serverCfg.OAuth, serverCfg.Timeout)
	} else {
		client, err = mcpproxy.NewMCPClient(serverCfg.Name, serverCfg.Command, serverCfg.Args, serverCfg.Env, serverCfg.Timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", name, err)
	}
	defer client.Close()

	tools, err := client.ListTools()
	if err != nil {
		return nil, fmt.Errorf("list tools from %s: %w", name, err)
	}

	latency := time.Since(start).Milliseconds()
	toolNames := make([]string, 0, len(tools))
	for _, t := range tools {
		if n, ok := t["name"].(string); ok {
			toolNames = append(toolNames, n)
		}
	}

	return map[string]interface{}{
		"ok":         true,
		"name":       name,
		"status":     "connected",
		"tool_count": len(tools),
		"tools":      toolNames,
		"latency_ms": latency,
		"via":        "direct",
	}, nil
}

// handleMCPStatus returns the status of all MCP servers and relay instances.
func handleMCPStatus(proxy *mcpproxy.Proxy) (map[string]interface{}, error) {
	result := map[string]interface{}{}

	// Connected proxy tools (servers loaded from graph, not local file)
	if proxy != nil {
		allTools, err := proxy.ListAllTools()
		if err == nil {
			// Group by server
			serverTools := map[string][]string{}
			for _, t := range allTools {
				if name, ok := t["name"].(string); ok {
					server, _ := t["_server"].(string)
					if server != "" {
						cleanName := strings.TrimPrefix(name, server+"_")
						serverTools[server] = append(serverTools[server], cleanName)
					}
				}
			}
			connected := []map[string]interface{}{}
			for srv, tools := range serverTools {
				connected = append(connected, map[string]interface{}{
					"name":       srv,
					"tool_count": len(tools),
					"tools":      tools,
				})
			}
			result["connected_servers"] = connected
			result["total_tools"] = len(allTools)
			result["configured_servers"] = connected
		}
	}

	if result["configured_servers"] == nil {
		result["configured_servers"] = []map[string]interface{}{}
	}

	return result, nil
}

// loadActiveProjectConfig loads the active project config for MP operations.
func loadActiveProjectConfig() *config.ProjectConfig {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	return cfg.Active()
}
