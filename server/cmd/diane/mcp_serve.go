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
	"github.com/Emergent-Comapny/diane/mcp/tools/apple"
	"github.com/Emergent-Comapny/diane/mcp/tools/finance"
	githubbot "github.com/Emergent-Comapny/diane/mcp/tools/github"
	"github.com/Emergent-Comapny/diane/mcp/tools/google"
	"github.com/Emergent-Comapny/diane/mcp/tools/infrastructure"
	"github.com/Emergent-Comapny/diane/mcp/tools/memorytools"
	"github.com/Emergent-Comapny/diane/mcp/tools/notifications"
	"github.com/Emergent-Comapny/diane/mcp/tools/places"
	"github.com/Emergent-Comapny/diane/mcp/tools/weather"
)

// cmdMCPServe runs the MCP server that reads JSON-RPC from stdin and writes to stdout.
// This is used by 'diane mcp relay' as the MCP subprocess.
func cmdMCPServe() {
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

	// Initialize providers
	appleProvider := apple.NewProvider()
	if err := appleProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Apple tools not available: %v", err)
		appleProvider = nil
	}

	googleProvider := google.NewProvider()
	if err := googleProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Google tools not available: %v", err)
		googleProvider = nil
	}

	infrastructureProvider := infrastructure.NewProvider()
	if err := infrastructureProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Infrastructure tools not available: %v", err)
		infrastructureProvider = nil
	}

	notificationsProvider := notifications.NewProvider()
	if err := notificationsProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Notifications tools not available: %v", err)
		notificationsProvider = nil
	}

	financeProvider := finance.NewProvider()
	if err := financeProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Finance tools not available: %v", err)
		financeProvider = nil
	}

	placesProvider := places.NewProvider()
	if err := placesProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Google Places tools not available: %v", err)
		placesProvider = nil
	}

	weatherProvider := weather.NewProvider()
	if err := weatherProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Weather tools not available: %v", err)
		weatherProvider = nil
	}

	githubProvider, githubErr := githubbot.NewProvider()
	if githubErr != nil {
		log.Printf("Warning: GitHub Bot tools not available: %v", githubErr)
		githubProvider = nil
	}

	memoryProvider := memorytools.NewProvider()
	if err := memoryProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Memory tools not available: %v", err)
		memoryProvider = nil
	}

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

		resp := handleMCPServeRequest(req, proxy, appleProvider, googleProvider,
			infrastructureProvider, notificationsProvider, financeProvider,
			placesProvider, weatherProvider, githubProvider, memoryProvider)
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
	appleProvider *apple.Provider,
	googleProvider *google.Provider,
	infrastructureProvider *infrastructure.Provider,
	notificationsProvider *notifications.Provider,
	financeProvider *finance.Provider,
	placesProvider *places.Provider,
	weatherProvider *weather.Provider,
	githubProvider *githubbot.Provider,
	memoryProvider *memorytools.Provider,
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
		tools := buildMCPToolList(appleProvider, googleProvider, infrastructureProvider,
			notificationsProvider, financeProvider, placesProvider,
			weatherProvider, githubProvider, memoryProvider)

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
		return mcpServeResponse{
			Error: &struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}{
				Code:    -32603,
				Message: "tools/call not implemented in serve mode (use relay for tool execution)",
			},
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

// buildMCPToolList returns all available MCP tools from the registered providers.
func buildMCPToolList(
	appleProvider *apple.Provider,
	googleProvider *google.Provider,
	infrastructureProvider *infrastructure.Provider,
	notificationsProvider *notifications.Provider,
	financeProvider *finance.Provider,
	placesProvider *places.Provider,
	weatherProvider *weather.Provider,
	githubProvider *githubbot.Provider,
	memoryProvider *memorytools.Provider,
) []map[string]interface{} {
	tools := []map[string]interface{}{
		{
			"name":        "job_list",
			"description": "List all cron jobs with their schedules and enabled status",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"enabled_only": map[string]interface{}{
						"type":        "boolean",
						"description": "Filter to show only enabled jobs",
					},
				},
			},
		},
		{
			"name":        "job_add",
			"description": "Add a new cron job with schedule and command",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":     map[string]interface{}{"type": "string", "description": "Unique name for the job"},
					"schedule": map[string]interface{}{"type": "string", "description": "Cron schedule expression"},
					"command":  map[string]interface{}{"type": "string", "description": "Shell command to execute"},
				},
				"required": []string{"name", "schedule", "command"},
			},
		},
		{
			"name":        "job_enable",
			"description": "Enable a cron job by name or ID",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"job": map[string]interface{}{"type": "string", "description": "Job name or ID"}},
				"required":   []string{"job"},
			},
		},
		{
			"name":        "job_disable",
			"description": "Disable a cron job by name or ID",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"job": map[string]interface{}{"type": "string", "description": "Job name or ID"}},
				"required":   []string{"job"},
			},
		},
		{
			"name":        "job_delete",
			"description": "Delete a cron job by name or ID (removes permanently)",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"job": map[string]interface{}{"type": "string", "description": "Job name or ID"}},
				"required":   []string{"job"},
			},
		},
		{
			"name":        "job_pause",
			"description": "Pause all cron jobs (disables all enabled jobs)",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "job_resume",
			"description": "Resume all cron jobs (enables all disabled jobs)",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			"name":        "job_logs",
			"description": "View execution logs for cron jobs",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"job_name": map[string]interface{}{"type": "string", "description": "Filter logs by job name"},
					"limit":    map[string]interface{}{"type": "number", "description": "Maximum number of logs to return (default 10)"},
				},
			},
		},
		{
			"name":        "server_status",
			"description": "Check if diane server is running",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}

	// Add provider tools
	if appleProvider != nil {
		for _, tool := range appleProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema,
			})
		}
	}
	if googleProvider != nil {
		for _, tool := range googleProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema,
			})
		}
	}
	if infrastructureProvider != nil {
		for _, tool := range infrastructureProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema,
			})
		}
	}
	if notificationsProvider != nil {
		for _, tool := range notificationsProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema,
			})
		}
	}
	if financeProvider != nil {
		for _, tool := range financeProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema,
			})
		}
	}
	if placesProvider != nil {
		for _, tool := range placesProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema,
			})
		}
	}
	if weatherProvider != nil {
		for _, tool := range weatherProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema,
			})
		}
	}
	if githubProvider != nil {
		for _, tool := range githubProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema,
			})
		}
	}
	if memoryProvider != nil {
		for _, tool := range memoryProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema,
			})
		}
	}

	return tools
}
