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

	"github.com/Emergent-Comapny/diane/internal/db"
	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
	"github.com/Emergent-Comapny/diane/mcp/tools"
	"github.com/Emergent-Comapny/diane/mcp/tools/apple"
	"github.com/Emergent-Comapny/diane/mcp/tools/finance"
	githubbot "github.com/Emergent-Comapny/diane/mcp/tools/github"
	"github.com/Emergent-Comapny/diane/mcp/tools/google"
	"github.com/Emergent-Comapny/diane/mcp/tools/infrastructure"
	"github.com/Emergent-Comapny/diane/mcp/tools/memorytools"
	"github.com/Emergent-Comapny/diane/mcp/tools/notifications"
	"github.com/Emergent-Comapny/diane/mcp/tools/places"
	"github.com/Emergent-Comapny/diane/mcp/tools/skills"
	"github.com/Emergent-Comapny/diane/mcp/tools/weather"
)

// MCP Server for Diane
// Provides tools for managing cron jobs and proxies other MCP servers

// Version is set at build time via ldflags
var Version = "dev"

var proxy *mcpproxy.Proxy
var globalEncoder *json.Encoder // For sending notifications
var toolProviders []tools.ToolProvider

// registerProvider adds a provider to the registry if it's not nil
func registerProvider(p tools.ToolProvider) {
	if p == nil {
		return
	}
	toolProviders = append(toolProviders, p)
	log.Printf("%s tools initialized successfully", p.Name())
}

type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	// Write PID file for reload command
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}
	pidFile := filepath.Join(home, ".diane", "mcp.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		log.Printf("Warning: Failed to write PID file: %v", err)
	}
	defer os.Remove(pidFile)

	// Initialize MCP proxy with inline config path (no graph dependency for standalone MCP server)
	configPath := mcpproxy.GetDefaultConfigPath()
	cfg, cfgErr := mcpproxy.LoadConfig(configPath)
	if cfgErr != nil {
		log.Printf("Warning: Failed to load MCP config: %v", cfgErr)
		// Continue without proxy - built-in tools will still work
	} else {
		proxy, err = mcpproxy.NewProxy(cfg.Servers)
		if err != nil {
			log.Printf("Warning: Failed to initialize MCP proxy: %v", err)
			// Continue without proxy - built-in tools will still work
		}
	}
	defer func() {
		if proxy != nil {
			proxy.Close()
		}
	}()

	// Initialize tool providers via registry
	initProviders := []struct {
		name string
		p    tools.ToolProvider
	}{
		{"Apple", apple.NewProvider()},
		{"Google", google.NewProvider()},
		{"Infrastructure", infrastructure.NewProvider()},
		{"Notifications", notifications.NewProvider()},
		{"Finance", finance.NewProvider()},
		{"Places", places.NewProvider()},
		{"Weather", weather.NewProvider()},
		{"GitHub", githubbot.NewProvider()},
		{"Memory", memorytools.NewProvider()},
		{"Skills", skills.NewProvider()},
	}

	for _, ip := range initProviders {
		if err := ip.p.CheckDependencies(); err != nil {
			log.Printf("Warning: %s tools not available: %v", ip.name, err)
		} else {
			registerProvider(ip.p)
		}
	}

	// MCP servers communicate via stdin/stdout
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	globalEncoder = encoder // Store for notification forwarding

	// Start notification forwarder if proxy is available
	if proxy != nil {
		go forwardProxiedNotifications(proxy)
	}

	// Setup signal handler for reload (SIGUSR1)
	if proxy != nil {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGUSR1)
		go func() {
			for range sigChan {
				log.Printf("Received SIGUSR1, reloading MCP configuration...")
				if cfg, cfgErr := mcpproxy.LoadConfig(configPath); cfgErr == nil {
					if reloadErr := proxy.Reload(cfg.Servers); reloadErr != nil {
						log.Printf("Failed to reload MCP config: %v", reloadErr)
					}
				} else {
					log.Printf("Failed to reload MCP config: %v", cfgErr)
				}
			}
		}()
	}

	for {
		var req MCPRequest
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				// stdin not ready yet or closed temporarily
				// Wait briefly and continue listening
				time.Sleep(50 * time.Millisecond)
				continue
			}
			log.Printf("Failed to decode request: %v", err)
			break
		}

		resp := handleRequest(req)
		resp.JSONRPC = "2.0"
		resp.ID = req.ID
		if err := encoder.Encode(resp); err != nil {
			log.Printf("Failed to encode response: %v", err)
			break
		}
	}
}

func handleRequest(req MCPRequest) MCPResponse {
	switch req.Method {
	case "initialize":
		return initialize()
	case "tools/list":
		return listTools()
	case "tools/call":
		return callTool(req.Params)
	default:
		return MCPResponse{
			Error: &MCPError{
				Code:    -32601,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

// forwardProxiedNotifications monitors the proxy for tool list changes
// and forwards them to the MCP client
func forwardProxiedNotifications(p *mcpproxy.Proxy) {
	for serverName := range p.NotificationChan() {
		log.Printf("Received tools/list_changed notification from proxied server: %s", serverName)

		// Send notification to stdout (to the MCP client)
		notification := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "notifications/tools/list_changed",
		}

		if err := globalEncoder.Encode(notification); err != nil {
			log.Printf("Failed to send notification: %v", err)
		} else {
			log.Printf("Forwarded tools/list_changed notification to MCP client")
		}
	}
}

func initialize() MCPResponse {
	return MCPResponse{
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": true, // Diane supports dynamic tool list updates from proxied servers
				},
			},
			"serverInfo": map[string]interface{}{
				"name":    "diane",
				"version": Version,
			},
		},
	}
}

func listTools() MCPResponse {
	// Built-in tools
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
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Unique name for the job",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "Cron schedule expression (e.g., '* * * * *' for every minute)",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Shell command to execute",
					},
				},
				"required": []string{"name", "schedule", "command"},
			},
		},
		{
			"name":        "job_enable",
			"description": "Enable a cron job by name or ID",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"job": map[string]interface{}{
						"type":        "string",
						"description": "Job name or ID",
					},
				},
				"required": []string{"job"},
			},
		},
		{
			"name":        "job_disable",
			"description": "Disable a cron job by name or ID",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"job": map[string]interface{}{
						"type":        "string",
						"description": "Job name or ID",
					},
				},
				"required": []string{"job"},
			},
		},
		{
			"name":        "job_delete",
			"description": "Delete a cron job by name or ID (removes permanently)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"job": map[string]interface{}{
						"type":        "string",
						"description": "Job name or ID",
					},
				},
				"required": []string{"job"},
			},
		},
		{
			"name":        "job_pause",
			"description": "Pause all cron jobs (disables all enabled jobs)",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "job_resume",
			"description": "Resume all cron jobs (enables all disabled jobs)",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "job_logs",
			"description": "View execution logs for cron jobs",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"job_name": map[string]interface{}{
						"type":        "string",
						"description": "Filter logs by job name",
					},
					"limit": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of logs to return (default 10)",
					},
				},
			},
		},
		{
			"name":        "server_status",
			"description": "Check if diane server is running",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	// Add tools from all registered providers
	for _, p := range toolProviders {
		for _, tool := range p.Tools() {
			tools = append(tools, map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			})
		}
	}

	// Add proxied tools from other MCP servers
	if proxy != nil {
		proxiedTools, err := proxy.ListAllTools()
		if err != nil {
			log.Printf("Failed to list proxied tools: %v", err)
		} else {
			tools = append(tools, proxiedTools...)
		}
	}

	return MCPResponse{
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

func callTool(params json.RawMessage) MCPResponse {
	var call struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}

	if err := json.Unmarshal(params, &call); err != nil {
		return MCPResponse{
			Error: &MCPError{
				Code:    -32602,
				Message: fmt.Sprintf("Invalid params: %v", err),
			},
		}
	}

	switch call.Name {
	case "job_list":
		return jobList(call.Arguments)
	case "job_add":
		return jobAdd(call.Arguments)
	case "job_enable":
		return jobEnable(call.Arguments)
	case "job_disable":
		return jobDisable(call.Arguments)
	case "job_delete":
		return jobDelete(call.Arguments)
	case "job_pause":
		return pauseAll()
	case "job_resume":
		return resumeAll()
	case "job_logs":
		return getLogs(call.Arguments)
	case "server_status":
		return getStatus()
	default:
		// Try all registered providers
		for _, p := range toolProviders {
			if p.HasTool(call.Name) {
				result, err := p.Call(call.Name, call.Arguments)
				if err != nil {
					return MCPResponse{
						Error: &MCPError{
							Code:    -1,
							Message: err.Error(),
						},
					}
				}
				return MCPResponse{Result: result}
			}
		}

		// Try proxied tools
		if proxy != nil {
			result, err := proxy.CallTool(call.Name, call.Arguments)
			if err == nil {
				return MCPResponse{Result: result}
			}
		}
		return MCPResponse{
			Error: &MCPError{
				Code:    -32601,
				Message: fmt.Sprintf("Tool not found: %s", call.Name),
			},
		}
	}
}

func getDB() (*db.DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(home, ".diane", "diane.db")
	return db.New(dbPath)
}

// Helper to format tool response in MCP content format
func mcpTextResponse(text string) MCPResponse {
	return MCPResponse{
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": text,
				},
			},
		},
	}
}

func jobList(args map[string]interface{}) MCPResponse {
	database, err := getDB()
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}
	defer database.Close()

	enabledOnly := false
	if val, ok := args["enabled_only"].(bool); ok {
		enabledOnly = val
	}

	jobs, err := database.ListJobs(enabledOnly)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	// Format as JSON string for text response
	jobsJSON, _ := json.MarshalIndent(jobs, "", "  ")

	return MCPResponse{
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(jobsJSON),
				},
			},
		},
	}
}

func jobAdd(args map[string]interface{}) MCPResponse {
	name, _ := args["name"].(string)
	schedule, _ := args["schedule"].(string)
	command, _ := args["command"].(string)

	if name == "" || schedule == "" || command == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "name, schedule, and command are required"}}
	}

	database, err := getDB()
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}
	defer database.Close()

	job, err := database.CreateJob(name, command, schedule)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	jobJSON, _ := json.MarshalIndent(job, "", "  ")
	message := fmt.Sprintf("Job '%s' created successfully\n\n%s", name, string(jobJSON))
	return mcpTextResponse(message)
}

func jobEnable(args map[string]interface{}) MCPResponse {
	jobIdentifier, _ := args["job"].(string)
	if jobIdentifier == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "job identifier is required"}}
	}

	database, err := getDB()
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}
	defer database.Close()

	job, err := database.GetJobByName(jobIdentifier)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	enabled := true
	if err := database.UpdateJob(job.ID, nil, nil, &enabled); err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	return mcpTextResponse(fmt.Sprintf("Job '%s' enabled", jobIdentifier))
}

func jobDisable(args map[string]interface{}) MCPResponse {
	jobIdentifier, _ := args["job"].(string)
	if jobIdentifier == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "job identifier is required"}}
	}

	database, err := getDB()
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}
	defer database.Close()

	job, err := database.GetJobByName(jobIdentifier)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	enabled := false
	if err := database.UpdateJob(job.ID, nil, nil, &enabled); err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	return mcpTextResponse(fmt.Sprintf("Job '%s' disabled", jobIdentifier))
}

func jobDelete(args map[string]interface{}) MCPResponse {
	jobIdentifier, _ := args["job"].(string)
	if jobIdentifier == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "job identifier is required"}}
	}

	database, err := getDB()
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}
	defer database.Close()

	job, err := database.GetJobByName(jobIdentifier)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	if err := database.DeleteJob(job.ID); err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	return mcpTextResponse(fmt.Sprintf("Job '%s' deleted", jobIdentifier))
}

func pauseAll() MCPResponse {
	database, err := getDB()
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}
	defer database.Close()

	jobs, err := database.ListJobs(true)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	count := 0
	enabled := false
	for _, job := range jobs {
		if err := database.UpdateJob(job.ID, nil, nil, &enabled); err != nil {
			return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
		}
		count++
	}

	return mcpTextResponse(fmt.Sprintf("Paused %d jobs", count))
}

func resumeAll() MCPResponse {
	database, err := getDB()
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}
	defer database.Close()

	allJobs, err := database.ListJobs(false)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	count := 0
	enabled := true
	for _, job := range allJobs {
		if !job.Enabled {
			if err := database.UpdateJob(job.ID, nil, nil, &enabled); err != nil {
				return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
			}
			count++
		}
	}

	return mcpTextResponse(fmt.Sprintf("Resumed %d jobs", count))
}

func getLogs(args map[string]interface{}) MCPResponse {
	database, err := getDB()
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}
	defer database.Close()

	limit := 10
	if val, ok := args["limit"].(float64); ok {
		limit = int(val)
	}

	var jobName string
	if val, ok := args["job_name"].(string); ok {
		jobName = val
	}

	// Get executions
	var executions []*db.JobExecution
	if jobName != "" {
		job, jobErr := database.GetJobByName(jobName)
		if jobErr != nil {
			return MCPResponse{Error: &MCPError{Code: -1, Message: jobErr.Error()}}
		}
		var execErr error
		executions, execErr = database.ListJobExecutions(&job.ID, limit, 0)
		if execErr != nil {
			return MCPResponse{Error: &MCPError{Code: -1, Message: execErr.Error()}}
		}
	} else {
		var execErr error
		executions, execErr = database.ListJobExecutions(nil, limit, 0)
		if execErr != nil {
			return MCPResponse{Error: &MCPError{Code: -1, Message: execErr.Error()}}
		}
	}

	logsJSON, _ := json.MarshalIndent(executions, "", "  ")
	return mcpTextResponse(string(logsJSON))
}

func getStatus() MCPResponse {
	home, err := os.UserHomeDir()
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	pidFile := filepath.Join(home, ".diane", "server.pid")
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		return mcpTextResponse("Server is not running")
	}

	return mcpTextResponse(fmt.Sprintf("Server is running (PID: %s)", string(pidBytes)))
}
