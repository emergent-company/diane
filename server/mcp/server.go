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
	proxy          *mcpproxy.Proxy
	encoder        *json.Encoder
	appleProvider  *apple.Provider
	googleProvider *google.Provider
	infrastructureProvider *infrastructure.Provider
	notificationsProvider  *notifications.Provider
	financeProvider        *finance.Provider
	placesProvider         *places.Provider
	weatherProvider        *weather.Provider
	githubProvider         *githubbot.Provider
	memoryProvider         *memorytools.Provider
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

	// Initialize Apple tools provider (only on macOS)
	s.appleProvider = apple.NewProvider()
	if err := s.appleProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Apple tools not available: %v", err)
		s.appleProvider = nil
	} else {
		log.Printf("Apple tools initialized successfully")
	}

	// Initialize Google tools provider
	s.googleProvider = google.NewProvider()
	if err := s.googleProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Google tools not available: %v", err)
		s.googleProvider = nil
	} else {
		log.Printf("Google tools initialized successfully")
	}

	// Initialize Infrastructure tools provider (Cloudflare DNS)
	s.infrastructureProvider = infrastructure.NewProvider()
	if err := s.infrastructureProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Infrastructure tools not available: %v", err)
		s.infrastructureProvider = nil
	} else {
		log.Printf("Infrastructure tools initialized successfully")
	}

	// Initialize Notifications tools provider (Discord, Home Assistant)
	s.notificationsProvider = notifications.NewProvider()
	if err := s.notificationsProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Notifications tools not available: %v", err)
		s.notificationsProvider = nil
	} else {
		log.Printf("Notifications tools initialized successfully")
	}

	// Initialize Finance tools provider (Enable Banking, Actual Budget, Bank Sync)
	s.financeProvider = finance.NewProvider()
	if err := s.financeProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Finance tools not available: %v", err)
		s.financeProvider = nil
	} else {
		log.Printf("Finance tools initialized successfully")
	}

	// Initialize Google Places tools provider
	s.placesProvider = places.NewProvider()
	if err := s.placesProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Google Places tools not available: %v", err)
		s.placesProvider = nil
	} else {
		log.Printf("Google Places tools initialized successfully")
	}

	// Initialize Weather tools provider
	s.weatherProvider = weather.NewProvider()
	if err := s.weatherProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Weather tools not available: %v", err)
		s.weatherProvider = nil
	} else {
		log.Printf("Weather tools initialized successfully")
	}

	// Initialize GitHub Bot tools provider
	var githubErr error
	s.githubProvider, githubErr = githubbot.NewProvider()
	if githubErr != nil {
		log.Printf("Warning: GitHub Bot tools not available: %v", githubErr)
		s.githubProvider = nil
	} else {
		log.Printf("GitHub Bot tools initialized successfully")
	}

	// Initialize Memory tools provider (wraps MP SDK for memory ops)
	s.memoryProvider = memorytools.NewProvider()
	if err := s.memoryProvider.CheckDependencies(); err != nil {
		log.Printf("Warning: Memory tools not available: %v", err)
		s.memoryProvider = nil
	} else {
		log.Printf("Memory tools initialized successfully")
	}

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

	// Add Apple tools (reminders + contacts)
	if s.appleProvider != nil {
		for _, tool := range s.appleProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			})
		}
	}

	// Add Google tools (gmail, drive, sheets, calendar)
	if s.googleProvider != nil {
		for _, tool := range s.googleProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			})
		}
	}

	// Add Infrastructure tools (Cloudflare DNS)
	if s.infrastructureProvider != nil {
		for _, tool := range s.infrastructureProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			})
		}
	}

	// Add Notifications tools (Discord, Home Assistant)
	if s.notificationsProvider != nil {
		for _, tool := range s.notificationsProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			})
		}
	}

	// Add Finance tools (Enable Banking, Actual Budget, Bank Sync)
	if s.financeProvider != nil {
		for _, tool := range s.financeProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			})
		}
	}

	// Add Google Places tools
	if s.placesProvider != nil {
		for _, tool := range s.placesProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			})
		}
	}

	// Add Weather tools
	if s.weatherProvider != nil {
		for _, tool := range s.weatherProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			})
		}
	}

	// Add GitHub Bot tools
	if s.githubProvider != nil {
		for _, tool := range s.githubProvider.Tools() {
			tools = append(tools, map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			})
		}
	}

	// Add Memory tools (memory_save, memory_recall, memory_apply_decay, memory_detect_patterns)
	if s.memoryProvider != nil {
		for _, tool := range s.memoryProvider.Tools() {
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
		// Try Apple tools first
		if s.appleProvider != nil && s.appleProvider.HasTool(call.Name) {
			result, err := s.appleProvider.Call(call.Name, call.Arguments)
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

		// Try Google tools
		if s.googleProvider != nil && s.googleProvider.HasTool(call.Name) {
			result, err := s.googleProvider.Call(call.Name, call.Arguments)
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

		// Try Infrastructure tools (Cloudflare DNS)
		if s.infrastructureProvider != nil && s.infrastructureProvider.HasTool(call.Name) {
			result, err := s.infrastructureProvider.Call(call.Name, call.Arguments)
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

		// Try Notifications tools (Discord, Home Assistant)
		if s.notificationsProvider != nil && s.notificationsProvider.HasTool(call.Name) {
			result, err := s.notificationsProvider.Call(call.Name, call.Arguments)
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

		// Try Finance tools (Enable Banking, Actual Budget, Bank Sync)
		if s.financeProvider != nil && s.financeProvider.HasTool(call.Name) {
			result, err := s.financeProvider.Call(call.Name, call.Arguments)
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

		// Try Google Places tools
		if s.placesProvider != nil && s.placesProvider.HasTool(call.Name) {
			result, err := s.placesProvider.Call(call.Name, call.Arguments)
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

		// Try Weather tools
		if s.weatherProvider != nil && s.weatherProvider.HasTool(call.Name) {
			result, err := s.weatherProvider.Call(call.Name, call.Arguments)
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

		// Try GitHub Bot tools
		if s.githubProvider != nil && s.githubProvider.HasTool(call.Name) {
			result, err := s.githubProvider.Call(call.Name, call.Arguments)
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

		// Try Memory tools
		if s.memoryProvider != nil && s.memoryProvider.HasTool(call.Name) {
			result, err := s.memoryProvider.Call(call.Name, call.Arguments)
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

func getDB() (*db.DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(home, ".diane", "cron.db")
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

// withDB opens the database and passes it to the callback.
func withDB(f func(*db.DB) MCPResponse) MCPResponse {
	database, err := getDB()
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}
	defer database.Close()
	return f(database)
}

func (s *MCPServer) jobList(args map[string]interface{}) MCPResponse {
	return withDB(func(database *db.DB) MCPResponse {

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
	})
}

func (s *MCPServer) jobAdd(args map[string]interface{}) MCPResponse {
	name, _ := args["name"].(string)
	schedule, _ := args["schedule"].(string)
	command, _ := args["command"].(string)

	if name == "" || schedule == "" || command == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "name, schedule, and command are required"}}
	}

	return withDB(func(database *db.DB) MCPResponse {

	job, err := database.CreateJob(name, command, schedule)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	jobJSON, _ := json.MarshalIndent(job, "", "  ")
	message := fmt.Sprintf("Job '%s' created successfully\n\n%s", name, string(jobJSON))
	return mcpTextResponse(message)
	})
}

func (s *MCPServer) jobEnable(args map[string]interface{}) MCPResponse {
	jobIdentifier, _ := args["job"].(string)
	if jobIdentifier == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "job identifier is required"}}
	}

	return withDB(func(database *db.DB) MCPResponse {

	job, err := database.GetJobByName(jobIdentifier)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	enabled := true
	if err := database.UpdateJob(job.ID, nil, nil, &enabled); err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	return mcpTextResponse(fmt.Sprintf("Job '%s' enabled", jobIdentifier))
	})
}

func (s *MCPServer) jobDisable(args map[string]interface{}) MCPResponse {
	jobIdentifier, _ := args["job"].(string)
	if jobIdentifier == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "job identifier is required"}}
	}

	return withDB(func(database *db.DB) MCPResponse {

	job, err := database.GetJobByName(jobIdentifier)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	enabled := false
	if err := database.UpdateJob(job.ID, nil, nil, &enabled); err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	return mcpTextResponse(fmt.Sprintf("Job '%s' disabled", jobIdentifier))
	})
}

func (s *MCPServer) jobDelete(args map[string]interface{}) MCPResponse {
	jobIdentifier, _ := args["job"].(string)
	if jobIdentifier == "" {
		return MCPResponse{Error: &MCPError{Code: -1, Message: "job identifier is required"}}
	}

	return withDB(func(database *db.DB) MCPResponse {

	job, err := database.GetJobByName(jobIdentifier)
	if err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	if err := database.DeleteJob(job.ID); err != nil {
		return MCPResponse{Error: &MCPError{Code: -1, Message: err.Error()}}
	}

	return mcpTextResponse(fmt.Sprintf("Job '%s' deleted", jobIdentifier))
	})
}

func (s *MCPServer) pauseAll() MCPResponse {
	return withDB(func(database *db.DB) MCPResponse {

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
	})
}

func (s *MCPServer) resumeAll() MCPResponse {
	return withDB(func(database *db.DB) MCPResponse {

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
	})
}

func (s *MCPServer) getLogs(args map[string]interface{}) MCPResponse {
	return withDB(func(database *db.DB) MCPResponse {

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
	})
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
