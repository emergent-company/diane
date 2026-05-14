package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

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
	"github.com/Emergent-Comapny/diane/mcp/tools/weather"
)

// MCP Server for Diane
// Provides tools for managing cron jobs and proxies other MCP servers

// Version is set at build time via ldflags
var Version = "dev"

// MCPServer encapsulates all server state for testability.
type MCPServer struct {
	proxy     *mcpproxy.Proxy
	encoder   *json.Encoder
	providers []tools.ToolProvider

	// In-memory job store (replaces SQLite cron.db)
	jobMu     sync.Mutex
	jobs      []*Job
	nextJobID int64
}

// Job represents a scheduled cron job.
type Job struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	Schedule  string    `json:"schedule"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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

// providerFactory creates a ToolProvider, possibly with an error.
type providerFactory func() (tools.ToolProvider, error)

type providerEntry struct {
	name    string
	factory providerFactory
}

func main() {
	srv := &MCPServer{}
	srv.run()
}

func (s *MCPServer) run() {
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

	// Initialize MCP proxy (optional — config comes from relay, not file)
	s.proxy, err = mcpproxy.NewProxy([]mcpproxy.ServerConfig{})
	if err != nil {
		log.Printf("Warning: Failed to initialize MCP proxy: %v", err)
		// Continue without proxy - built-in tools will still work
	}
	defer func() {
		if s.proxy != nil {
			s.proxy.Close()
		}
	}()

	// Initialize all tool providers
	s.initProviders()

	// MCP servers communicate via stdin/stdout
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	s.encoder = encoder // Store for notification forwarding

	// Start notification forwarder if proxy is available
	if s.proxy != nil {
		go s.forwardProxiedNotifications(s.proxy)
	}

	// Setup signal handler for reload (SIGUSR1)
	if s.proxy != nil {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGUSR1)
		go func() {
			for range sigChan {
				log.Printf("Received SIGUSR1, reloading MCP configuration...")
				if err := s.proxy.Reload([]mcpproxy.ServerConfig{}); err != nil {
					log.Printf("Failed to reload MCP config: %v", err)
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

		resp := s.handleRequest(req)
		resp.JSONRPC = "2.0"
		resp.ID = req.ID
		if err := encoder.Encode(resp); err != nil {
			log.Printf("Failed to encode response: %v", err)
			break
		}
	}
}

func (s *MCPServer) handleRequest(req MCPRequest) MCPResponse {
	switch req.Method {
	case "initialize":
		return s.initialize()
	case "tools/list":
		return s.listTools()
	case "tools/call":
		return s.callTool(req.Params)
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
func (s *MCPServer) forwardProxiedNotifications(p *mcpproxy.Proxy) {
	for serverName := range p.NotificationChan() {
		log.Printf("Received tools/list_changed notification from proxied server: %s", serverName)

		// Send notification to stdout (to the MCP client)
		notification := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "notifications/tools/list_changed",
		}

		if err := s.encoder.Encode(notification); err != nil {
			log.Printf("Failed to send notification: %v", err)
		} else {
			log.Printf("Forwarded tools/list_changed notification to MCP client")
		}
	}
}

// initProviders initializes all built-in tool providers.
// Providers whose dependencies are not met are skipped with a warning.
func (s *MCPServer) initProviders() {
	entries := []providerEntry{
		{"Apple", func() (tools.ToolProvider, error) { return apple.NewProvider(), nil }},
		{"Google", func() (tools.ToolProvider, error) { return google.NewProvider(), nil }},
		{"Infrastructure", func() (tools.ToolProvider, error) { return infrastructure.NewProvider(), nil }},
		{"Notifications", func() (tools.ToolProvider, error) { return notifications.NewProvider(), nil }},
		{"Finance", func() (tools.ToolProvider, error) { return finance.NewProvider(), nil }},
		{"Google Places", func() (tools.ToolProvider, error) { return places.NewProvider(), nil }},
		{"Weather", func() (tools.ToolProvider, error) { return weather.NewProvider(), nil }},
		{"GitHub Bot", func() (tools.ToolProvider, error) { return githubbot.NewProvider() }},
		{"Memory", func() (tools.ToolProvider, error) { return memorytools.NewProvider(), nil }},
	}

	for _, e := range entries {
		p, err := e.factory()
		if err != nil {
			log.Printf("Warning: %s tools not available: %v", e.name, err)
			continue
		}
		if err := p.CheckDependencies(); err != nil {
			log.Printf("Warning: %s tools not available: %v", e.name, err)
			continue
		}
		s.providers = append(s.providers, p)
		log.Printf("%s tools initialized successfully", e.name)
	}
}

func (s *MCPServer) initialize() MCPResponse {
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

func (s *MCPServer) listTools() MCPResponse {
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

	// Add provider tools
	for _, p := range s.providers {
		for _, tool := range p.Tools() {
			tools = append(tools, map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			})
		}
	}

	// Add proxied tools from other MCP servers
	if s.proxy != nil {
		proxiedTools, err := s.proxy.ListAllTools()
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

func (s *MCPServer) callTool(params json.RawMessage) MCPResponse {
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
		return s.jobList(call.Arguments)
	case "job_add":
		return s.jobAdd(call.Arguments)
	case "job_enable":
		return s.jobEnable(call.Arguments)
	case "job_disable":
		return s.jobDisable(call.Arguments)
	case "job_delete":
		return s.jobDelete(call.Arguments)
	case "job_pause":
		return s.pauseAll()
	case "job_resume":
		return s.resumeAll()
	case "job_logs":
		return s.getLogs(call.Arguments)
	case "server_status":
		return s.getStatus()
	default:
		// Try each provider
		for _, p := range s.providers {
			for _, t := range p.Tools() {
				if t.Name == call.Name {
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
		}

		// Try proxied tools
		if s.proxy != nil {
			result, err := s.proxy.CallTool(call.Name, call.Arguments)
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

func (s *MCPServer) jobList(args map[string]interface{}) MCPResponse {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	enabledOnly := false
	if val, ok := args["enabled_only"].(bool); ok {
		enabledOnly = val
	}

	var filtered []*Job
	for _, j := range s.jobs {
		if enabledOnly && !j.Enabled {
			continue
		}
		filtered = append(filtered, j)
	}

	if filtered == nil {
		filtered = []*Job{}
	}

	jobsJSON, _ := json.MarshalIndent(filtered, "", "  ")
	return mcpTextResponse(string(jobsJSON))
}

func (s *MCPServer) jobAdd(args map[string]interface{}) MCPResponse {
	name, _ := args["name"].(string)
	schedule, _ := args["schedule"].(string)
	command, _ := args["command"].(string)

	if name == "" || schedule == "" || command == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "name, schedule, and command are required"}}
	}

	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	for _, j := range s.jobs {
		if j.Name == name {
			return MCPResponse{Error: &MCPError{Code: -1, Message: fmt.Sprintf("job '%s' already exists", name)}}
		}
	}

	now := time.Now()
	s.nextJobID++
	job := &Job{
		ID:        s.nextJobID,
		Name:      name,
		Command:   command,
		Schedule:  schedule,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.jobs = append(s.jobs, job)

	jobJSON, _ := json.MarshalIndent(job, "", "  ")
	message := fmt.Sprintf("Job '%s' created successfully\n\n%s", name, string(jobJSON))
	return mcpTextResponse(message)
}

func (s *MCPServer) jobEnable(args map[string]interface{}) MCPResponse {
	jobIdentifier, _ := args["job"].(string)
	if jobIdentifier == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "job identifier is required"}}
	}

	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	job := s.findJob(jobIdentifier)
	if job == nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: fmt.Sprintf("job '%s' not found", jobIdentifier)}}
	}

	job.Enabled = true
	job.UpdatedAt = time.Now()
	return mcpTextResponse(fmt.Sprintf("Job '%s' enabled", jobIdentifier))
}

func (s *MCPServer) jobDisable(args map[string]interface{}) MCPResponse {
	jobIdentifier, _ := args["job"].(string)
	if jobIdentifier == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "job identifier is required"}}
	}

	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	job := s.findJob(jobIdentifier)
	if job == nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: fmt.Sprintf("job '%s' not found", jobIdentifier)}}
	}

	job.Enabled = false
	job.UpdatedAt = time.Now()
	return mcpTextResponse(fmt.Sprintf("Job '%s' disabled", jobIdentifier))
}

func (s *MCPServer) jobDelete(args map[string]interface{}) MCPResponse {
	jobIdentifier, _ := args["job"].(string)
	if jobIdentifier == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "job identifier is required"}}
	}

	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	for i, j := range s.jobs {
		if j.Name == jobIdentifier || fmt.Sprintf("%d", j.ID) == jobIdentifier {
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			return mcpTextResponse(fmt.Sprintf("Job '%s' deleted", jobIdentifier))
		}
	}
	return MCPResponse{Error: &MCPError{Code: -1, Message: fmt.Sprintf("job '%s' not found", jobIdentifier)}}
}

func (s *MCPServer) pauseAll() MCPResponse {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	count := 0
	for _, j := range s.jobs {
		if j.Enabled {
			j.Enabled = false
			j.UpdatedAt = time.Now()
			count++
		}
	}
	return mcpTextResponse(fmt.Sprintf("Paused %d jobs", count))
}

func (s *MCPServer) resumeAll() MCPResponse {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	count := 0
	for _, j := range s.jobs {
		if !j.Enabled {
			j.Enabled = true
			j.UpdatedAt = time.Now()
			count++
		}
	}
	return mcpTextResponse(fmt.Sprintf("Resumed %d jobs", count))
}

func (s *MCPServer) getLogs(args map[string]interface{}) MCPResponse {
	// SQLite-backed job_executions log was never written to in production.
	return mcpTextResponse("[]")
}

// findJob looks up a job by name or ID string. Must be called with s.jobMu held.
func (s *MCPServer) findJob(identifier string) *Job {
	for _, j := range s.jobs {
		if j.Name == identifier || fmt.Sprintf("%d", j.ID) == identifier {
			return j
		}
	}
	return nil
}

func (s *MCPServer) getStatus() MCPResponse {
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
