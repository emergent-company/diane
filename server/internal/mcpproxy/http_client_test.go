package mcpproxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockMCPServer is a test helper that implements a minimal Streamable HTTP MCP server.
type mockMCPServer struct {
	server     *httptest.Server
	tools      []map[string]interface{}
	methods    map[string]func(params json.RawMessage) (interface{}, int) // method -> handler, returns (result, statusCode)
	statusCode int                                                         // override status for error testing
}

func newMockMCPServer(t *testing.T) *mockMCPServer {
	m := &mockMCPServer{
		tools: []map[string]interface{}{
			{"name": "echo", "description": "Echo input", "inputSchema": map[string]interface{}{"type": "object"}},
			{"name": "add", "description": "Add two numbers", "inputSchema": map[string]interface{}{"type": "object"}},
		},
		methods: make(map[string]func(params json.RawMessage) (interface{}, int)),
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Check static headers if configured
		if m.statusCode > 0 {
			w.WriteHeader(m.statusCode)
			return
		}

		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      interface{}     `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var result interface{}
		var errCode int
		var errMsg string

		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": "2025-11-25",
				"serverInfo": map[string]interface{}{
					"name":    "test-server",
					"version": "1.0.0",
				},
			}
		case "tools/list":
			result = map[string]interface{}{
				"tools": m.tools,
			}
		case "tools/call":
			if handler, ok := m.methods["tools/call"]; ok {
				result, errCode = handler(req.Params)
				if errCode != 0 {
					w.Header().Set("Content-Type", "application/json")
					resp := map[string]interface{}{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"error": map[string]interface{}{
							"code":    errCode,
							"message": "tool error",
						},
					}
					json.NewEncoder(w).Encode(resp)
					return
				}
			} else {
				result = map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": "tool executed"},
					},
				}
			}
		default:
			errMsg = "method not found"
			errCode = -32601
		}

		w.Header().Set("Content-Type", "application/json")
		if errCode != 0 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error": map[string]interface{}{
					"code":    errCode,
					"message": errMsg,
				},
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
	return m
}

func (m *mockMCPServer) Close() {
	m.server.Close()
}

func (m *mockMCPServer) URL() string {
	return m.server.URL
}

// TestHTTPMCPClient_Initialize tests that NewHTTPMCPClient successfully initializes
func TestHTTPMCPClient_Initialize(t *testing.T) {
	mock := newMockMCPServer(t)
	defer mock.Close()

	client, err := NewHTTPMCPClient("test-server", mock.URL(), nil)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient failed: %v", err)
	}
	defer client.Close()

	if client.Name != "test-server" {
		t.Errorf("client.Name = %q, want %q", client.Name, "test-server")
	}
	if client.URL != mock.URL() {
		t.Errorf("client.URL = %q, want %q", client.URL, mock.URL())
	}
	t.Log("✅ HTTP MCP client initialized successfully")
}

// TestHTTPMCPClient_ListTools tests listing tools from an HTTP MCP server
func TestHTTPMCPClient_ListTools(t *testing.T) {
	mock := newMockMCPServer(t)
	defer mock.Close()

	client, err := NewHTTPMCPClient("test-server", mock.URL(), nil)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient failed: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("ListTools returned %d tools, want 2", len(tools))
	}

	// Check tool names
	expectedNames := map[string]bool{"echo": true, "add": true}
	for _, tool := range tools {
		name, ok := tool["name"].(string)
		if !ok {
			t.Errorf("tool missing 'name' field: %v", tool)
			continue
		}
		if !expectedNames[name] {
			t.Errorf("unexpected tool name: %s", name)
		}
		delete(expectedNames, name)
	}
	if len(expectedNames) > 0 {
		t.Errorf("missing tools: %v", expectedNames)
	}

	t.Log("✅ ListTools returned expected tools")
}

// TestHTTPMCPClient_CallTool tests calling a tool on an HTTP MCP server
func TestHTTPMCPClient_CallTool(t *testing.T) {
	mock := newMockMCPServer(t)
	defer mock.Close()

	client, err := NewHTTPMCPClient("test-server", mock.URL(), nil)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient failed: %v", err)
	}
	defer client.Close()

	result, err := client.CallTool("echo", map[string]interface{}{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	// Result should be JSON with content
	var resultMap map[string]interface{}
	if err := json.Unmarshal(result, &resultMap); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	content, ok := resultMap["content"].([]interface{})
	if !ok {
		t.Fatalf("result missing 'content' field: %v", resultMap)
	}
	if len(content) > 0 {
		first := content[0].(map[string]interface{})
		if first["text"] != "tool executed" {
			t.Errorf("content text = %q, want %q", first["text"], "tool executed")
		}
	}

	t.Log("✅ CallTool executed successfully")
}

// TestHTTPMCPClient_WithHeaders tests that custom headers are sent
func TestHTTPMCPClient_WithHeaders(t *testing.T) {
	var authHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"protocolVersion": "2025-11-25",
				"serverInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
			},
		})
	}))
	defer ts.Close()

	headers := map[string]string{
		"Authorization": "Bearer test-token-123",
		"X-API-Key":     "my-api-key",
	}
	client, err := NewHTTPMCPClient("auth-server", ts.URL, headers)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient failed: %v", err)
	}
	defer client.Close()

	if authHeader != "Bearer test-token-123" {
		t.Errorf("Authorization header = %q, want %q", authHeader, "Bearer test-token-123")
	}
	t.Log("✅ Custom headers sent correctly")
}

// TestHTTPMCPClient_Unauthorized tests that 401 responses are handled gracefully
func TestHTTPMCPClient_Unauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	defer ts.Close()

	_, err := NewHTTPMCPClient("unauth-server", ts.URL, nil)
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
	t.Logf("✅ Unauthorized correctly returns error: %v", err)
}

// TestHTTPMCPClient_NotificationChan tests that NotificationChan returns nil (HTTP is stateless)
func TestHTTPMCPClient_NotificationChan(t *testing.T) {
	mock := newMockMCPServer(t)
	defer mock.Close()

	client, err := NewHTTPMCPClient("test-server", mock.URL(), nil)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient failed: %v", err)
	}
	defer client.Close()

	ch := client.NotificationChan()
	if ch != nil {
		t.Error("expected nil notification channel for HTTP client")
	}
	t.Log("✅ NotificationChan returns nil for HTTP client")
}

// TestHTTPMCPClient_ListAllTools_WithProxy tests integration with Proxy.ListAllTools
func TestHTTPMCPClient_ListAllTools_WithProxy(t *testing.T) {
	mock := newMockMCPServer(t)
	defer mock.Close()

	// Create HTTP client and add it to a proxy
	client, err := NewHTTPMCPClient("remote-api", mock.URL(), nil)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient failed: %v", err)
	}
	defer client.Close()

	// Create proxy with just this client
	proxy := &Proxy{
		clients:    make(map[string]Client),
		notifyChan: make(chan string, 10),
	}
	proxy.clients["remote-api"] = client

	tools, err := proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools failed: %v", err)
	}

	// Tools should have server name prefix
	if len(tools) != 2 {
		t.Fatalf("ListAllTools returned %d tools, want 2", len(tools))
	}

	expectedNames := map[string]bool{"remote-api_echo": true, "remote-api_add": true}
	for _, tool := range tools {
		name, ok := tool["name"].(string)
		if !ok {
			t.Errorf("tool missing 'name' field: %v", tool)
			continue
		}
		if !expectedNames[name] {
			t.Errorf("unexpected tool name: %s", name)
		}
		delete(expectedNames, name)
		// Check _server field
		server, ok := tool["_server"].(string)
		if !ok || server != "remote-api" {
			t.Errorf("expected _server='remote-api', got %v", tool["_server"])
		}
	}
	if len(expectedNames) > 0 {
		t.Errorf("missing tools: %v", expectedNames)
	}

	t.Log("✅ HTTP client integrated with proxy ListAllTools")
}

// TestHTTPMCPClient_URLError tests connection refused / invalid URL handling
func TestHTTPMCPClient_URLError(t *testing.T) {
	_, err := NewHTTPMCPClient("bad-server", "http://127.0.0.1:1", nil)
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}
	t.Logf("✅ URL error correctly returns: %v", err)
}
