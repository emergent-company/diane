package mcpproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// =========================================================================
// Mock MCP HTTP server helpers
// =========================================================================

// newMockMCPServer creates a minimal Streamable HTTP MCP server that supports
// initialize, tools/list, and tools/call methods.
// The echo tool echoes back the "message" argument.
func newMockMCPServerWithEcho() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      interface{}     `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "initialize":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{
					"protocolVersion": "2025-11-25",
					"serverInfo": map[string]interface{}{
						"name": "test-mcp", "version": "1.0.0",
					},
				},
			})
		case "tools/list":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{
					"tools": []map[string]interface{}{
						{
							"name": "echo", "description": "Echo back input",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"message": map[string]interface{}{"type": "string"},
								},
							},
						},
					},
				},
			})
		case "tools/call":
			var params struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			json.Unmarshal(req.Params, &params)
			result := map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": fmt.Sprintf("echo: %v", params.Arguments["message"])},
				},
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": result,
			})
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]interface{}{
					"code":    -32601,
					"message": "method not found",
				},
			})
		}
	})
	return httptest.NewServer(mux)
}

// newMockOAuthMCPServer creates a mock HTTP MCP server that requires
// authentication. The first request without an Authorization header receives
// a 401. Subsequent requests (and any request with a valid Authorization header)
// succeed normally.
func newMockOAuthMCPServer() *httptest.Server {
	var mu sync.Mutex
	callCount := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		count := callCount
		mu.Unlock()

		auth := r.Header.Get("Authorization")

		// First call with no auth gets 401
		if count == 1 && auth == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_token"})
			return
		}

		// Normal MCP response for all subsequent calls
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      interface{}     `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "initialize":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{
					"protocolVersion": "2025-11-25",
					"serverInfo": map[string]interface{}{
						"name": "oauth-mcp", "version": "1.0.0",
					},
				},
			})
		case "tools/list":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{
					"tools": []map[string]interface{}{
						{
							"name": "echo", "description": "Echo back input",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"message": map[string]interface{}{"type": "string"},
								},
							},
						},
					},
				},
			})
		case "tools/call":
			var params struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			json.Unmarshal(req.Params, &params)
			result := map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": fmt.Sprintf("echo: %v", params.Arguments["message"])},
				},
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": result,
			})
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]interface{}{
					"code":    -32601,
					"message": "method not found",
				},
			})
		}
	})
	return httptest.NewServer(mux)
}

// =========================================================================
// Test 1: HTTPMCPClient ListTools
// =========================================================================

func TestHTTPMCPClient_ListTools_Integration(t *testing.T) {
	mock := newMockMCPServerWithEcho()
	defer mock.Close()

	client, err := NewHTTPMCPClient("test-server", mock.URL+"/mcp", nil, nil, 0)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient failed: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("ListTools returned %d tools, want 1", len(tools))
	}

	name, ok := tools[0]["name"].(string)
	if !ok {
		t.Fatalf("tool missing 'name' field: %v", tools[0])
	}
	if name != "echo" {
		t.Errorf("tool name = %q, want %q", name, "echo")
	}

	desc, ok := tools[0]["description"].(string)
	if !ok {
		t.Fatalf("tool missing 'description' field: %v", tools[0])
	}
	if desc != "Echo back input" {
		t.Errorf("tool description = %q, want %q", desc, "Echo back input")
	}

	t.Log("✅ ListTools integration test passed")
}

// =========================================================================
// Test 2: HTTPMCPClient CallTool
// =========================================================================

func TestHTTPMCPClient_CallTool_Integration(t *testing.T) {
	mock := newMockMCPServerWithEcho()
	defer mock.Close()

	client, err := NewHTTPMCPClient("test-server", mock.URL+"/mcp", nil, nil, 0)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient failed: %v", err)
	}
	defer client.Close()

	result, err := client.CallTool("echo", map[string]interface{}{"message": "hello world"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal(result, &resultMap); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	content, ok := resultMap["content"].([]interface{})
	if !ok {
		t.Fatalf("result missing 'content' field: %v", resultMap)
	}
	if len(content) != 1 {
		t.Fatalf("content length = %d, want 1", len(content))
	}

	first, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("content[0] is not a map: %v", content[0])
	}

	text, ok := first["text"].(string)
	if !ok {
		t.Fatalf("content[0] missing 'text' field: %v", first)
	}
	if text != "echo: hello world" {
		t.Errorf("response text = %q, want %q", text, "echo: hello world")
	}

	t.Log("✅ CallTool integration test passed")
}

// =========================================================================
// Test 3: HTTPMCPClient with pre-set token and 401-challenged server
// =========================================================================

func TestHTTPMCPClient_401WithToken(t *testing.T) {
	mock := newMockOAuthMCPServer()
	defer mock.Close()

	// Create a client with a pre-set Authorization header.
	// The mock server returns 401 on the first call without auth, but
	// with this header present, the initialize request will carry the token
	// and succeed immediately.
	headers := map[string]string{
		"Authorization": "Bearer valid-test-token",
	}

	client, err := NewHTTPMCPClient("auth-server", mock.URL+"/mcp", headers, nil, 0)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient with pre-set token failed: %v", err)
	}
	defer client.Close()

	// Verify we can list tools
	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools failed with pre-set token: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("ListTools returned %d tools, want 1", len(tools))
	}

	name, ok := tools[0]["name"].(string)
	if !ok || name != "echo" {
		t.Errorf("tool name = %q, want %q", name, "echo")
	}

	// Verify we can call a tool
	result, err := client.CallTool("echo", map[string]interface{}{"message": "authed"})
	if err != nil {
		t.Fatalf("CallTool failed with pre-set token: %v", err)
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal(result, &resultMap); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	content, ok := resultMap["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("result missing content: %v", resultMap)
	}

	first, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("content[0] is not a map: %v", content[0])
	}

	text, ok := first["text"].(string)
	if !ok || text != "echo: authed" {
		t.Errorf("response text = %q, want %q", text, "echo: authed")
	}

	t.Log("✅ 401 with pre-set token integration test passed")
}

// =========================================================================
// Test 4: Proxy with HTTP MCP server via config
// =========================================================================

func TestProxy_WithHTTPServer(t *testing.T) {
	mock := newMockMCPServerWithEcho()
	defer mock.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp-servers.json")

	// Create config JSON pointing to the mock HTTP MCP server
	configJSON := fmt.Sprintf(`{
		"servers": [
			{
				"name": "remote-api",
				"enabled": true,
				"type": "http",
				"url": "%s/mcp"
			}
		]
	}`, mock.URL)

	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Override secretsDir to use temp dir so token loading doesn't pollute
	// the real secrets directory or fail on missing home dir
	origSecretsDir := secretsDir
	secretsDir = func() string {
		return filepath.Join(dir, "secrets")
	}
	defer func() { secretsDir = origSecretsDir }()

	proxy, err := NewProxy(configPath)
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}
	defer proxy.Close()

	tools, err := proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools failed: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("ListAllTools returned %d tools, want 1", len(tools))
	}

	// Tool name should be prefixed with server name
	name, ok := tools[0]["name"].(string)
	if !ok {
		t.Fatalf("tool missing 'name' field: %v", tools[0])
	}
	expectedName := "remote-api_echo"
	if name != expectedName {
		t.Errorf("tool name = %q, want %q", name, expectedName)
	}

	// Check _server field
	server, ok := tools[0]["_server"].(string)
	if !ok {
		t.Fatalf("tool missing '_server' field: %v", tools[0])
	}
	if server != "remote-api" {
		t.Errorf("tool _server = %q, want %q", server, "remote-api")
	}

	// Check description preserved
	desc, ok := tools[0]["description"].(string)
	if !ok {
		t.Fatalf("tool missing 'description' field: %v", tools[0])
	}
	if desc != "Echo back input" {
		t.Errorf("tool description = %q, want %q", desc, "Echo back input")
	}

	t.Log("✅ Proxy with HTTP server integration test passed")
}
