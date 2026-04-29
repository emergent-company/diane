package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

// cmdMCPServe runs the MCP server that reads JSON-RPC from stdin and writes to stdout.
// This is used by 'diane mcp relay' as the MCP subprocess.
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

	// Initialize MCP proxy
	configPath := mcpproxy.GetDefaultConfigPath()
	proxy, err := mcpproxy.NewProxy(configPath)
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
				if err := proxy.Reload(); err != nil {
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
func buildMCPToolList() []map[string]interface{} {
	tools := []map[string]interface{}{
		{
			"name":        "node_status",
			"description": "Check if diane server is running",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}

	return tools
}
