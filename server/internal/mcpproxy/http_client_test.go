package mcpproxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

	client, err := NewHTTPMCPClient("test-server", mock.URL(), nil, nil)
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

	client, err := NewHTTPMCPClient("test-server", mock.URL(), nil, nil)
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

	client, err := NewHTTPMCPClient("test-server", mock.URL(), nil, nil)
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
	client, err := NewHTTPMCPClient("auth-server", ts.URL, headers, nil)
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

	_, err := NewHTTPMCPClient("unauth-server", ts.URL, nil, nil)
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
	t.Logf("✅ Unauthorized correctly returns error: %v", err)
}

// TestHTTPMCPClient_NotificationChan tests that NotificationChan returns nil (HTTP is stateless)
func TestHTTPMCPClient_NotificationChan(t *testing.T) {
	mock := newMockMCPServer(t)
	defer mock.Close()

	client, err := NewHTTPMCPClient("test-server", mock.URL(), nil, nil)
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
	client, err := NewHTTPMCPClient("remote-api", mock.URL(), nil, nil)
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
	_, err := NewHTTPMCPClient("bad-server", "http://127.0.0.1:1", nil, nil)
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}
	t.Logf("✅ URL error correctly returns: %v", err)
}

// TestHTTPMCPClient_AutoLoadStoredToken verifies that stored tokens are auto-loaded
// when creating a client with OAuth config.
func TestHTTPMCPClient_AutoLoadStoredToken(t *testing.T) {
	tmpDir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string {
		return tmpDir
	}
	defer func() { secretsDir = origSecretsDir }()

	serverName := "auto-load-test"

	// Save tokens to disk first
	tokens := &StoredTokens{
		AccessToken:  "gho_pre_stored_token",
		RefreshToken: "ghr_refresh_token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}
	if err := SaveTokens(serverName, tokens); err != nil {
		t.Fatalf("SaveTokens failed: %v", err)
	}

	// Create a mock server that checks the Authorization header
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

	oauth := &OAuthConfig{
		ClientID: "test-client",
		TokenURL: "https://token.example.com",
	}

	client, err := NewHTTPMCPClient(serverName, ts.URL, nil, oauth)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient failed: %v", err)
	}
	defer client.Close()

	if client.Token != "gho_pre_stored_token" {
		t.Errorf("client.Token = %q, want %q", client.Token, "gho_pre_stored_token")
	}
	if authHeader != "Bearer gho_pre_stored_token" {
		t.Errorf("Authorization header = %q, want %q", authHeader, "Bearer gho_pre_stored_token")
	}
	t.Log("✅ Auto-load stored token works correctly")
}

// TestHTTPMCPClient_RefreshExpiredToken verifies that expired tokens are auto-refreshed
// using the refresh endpoint.
func TestHTTPMCPClient_RefreshExpiredToken(t *testing.T) {
	tmpDir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string {
		return tmpDir
	}
	defer func() { secretsDir = origSecretsDir }()

	serverName := "refresh-test"

	// Create a mock refresh token endpoint
	refreshCalled := false
	refreshTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new_token_after_refresh",
			"refresh_token": "new_refresh_token",
			"expires_in":    3600,
		})
	}))
	defer refreshTS.Close()

	// Save an expired token with a refresh token
	tokens := &StoredTokens{
		AccessToken:  "old_expired_token",
		RefreshToken: "valid_refresh_token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour), // expired 1 hour ago
	}
	if err := SaveTokens(serverName, tokens); err != nil {
		t.Fatalf("SaveTokens failed: %v", err)
	}

	// Create a mock MCP server
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

	oauth := &OAuthConfig{
		ClientID: "test-client",
		TokenURL: refreshTS.URL, // Point to our mock refresh endpoint
	}

	client, err := NewHTTPMCPClient(serverName, ts.URL, nil, oauth)
	if err != nil {
		t.Fatalf("NewHTTPMCPClient failed: %v", err)
	}
	defer client.Close()

	if !refreshCalled {
		t.Error("expected refresh endpoint to be called for expired token")
	}
	if client.Token != "new_token_after_refresh" {
		t.Errorf("client.Token = %q, want %q", client.Token, "new_token_after_refresh")
	}
	if authHeader != "Bearer new_token_after_refresh" {
		t.Errorf("Authorization header = %q, want %q", authHeader, "Bearer new_token_after_refresh")
	}

	// Also verify the refreshed token was saved to disk
	loaded, err := LoadTokens(serverName)
	if err != nil {
		t.Fatalf("LoadTokens failed: %v", err)
	}
	if loaded.AccessToken != "new_token_after_refresh" {
		t.Errorf("saved AccessToken = %q, want %q", loaded.AccessToken, "new_token_after_refresh")
	}

	t.Log("✅ Expired token auto-refresh works correctly")
}

// TestHTTPMCPClient_401WithOAuthTriggersReauth verifies that a 401 response triggers
// the OAuth re-authentication flow when OAuth config is present.
func TestHTTPMCPClient_401WithOAuthTriggersReauth(t *testing.T) {
	tmpDir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string {
		return tmpDir
	}
	defer func() { secretsDir = origSecretsDir }()

	serverName := "reauth-test-401"

	// Mock server that always returns 401
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	defer ts.Close()

	// With OAuth config but no flow configured (missing both DeviceAuthURL and AuthorizationURL),
	// reauthenticate should return an error about missing flow config.
	oauth := &OAuthConfig{
		ClientID: "test-client",
		TokenURL: "https://token.example.com",
		// No DeviceAuthURL or AuthorizationURL — will trigger "no flow configured" error
	}

	_, err := NewHTTPMCPClient(serverName, ts.URL, nil, oauth)
	if err == nil {
		t.Fatal("expected error for 401 with incomplete OAuth config, got nil")
	}

	errStr := err.Error()
	if !containsSubstring(errStr, "no OAuth flow configured") &&
		!containsSubstring(errStr, "unauthorized") {
		t.Errorf("error message should mention OAuth flow or unauthorized, got: %s", errStr)
	}
	t.Logf("✅ 401 with OAuth config correctly returns error: %v", err)
}

// TestHTTPMCPClient_401WithNilOAuth tests that 401 without OAuth config returns
// the standard auth error (no reauth attempted).
func TestHTTPMCPClient_401WithNilOAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	defer ts.Close()

	// No OAuth config — should get standard error
	_, err := NewHTTPMCPClient("no-oauth-server", ts.URL, nil, nil)
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !containsSubstring(err.Error(), "unauthorized") {
		t.Errorf("error should mention unauthorized, got: %v", err)
	}
	t.Logf("✅ 401 without OAuth returns standard error: %v", err)
}

// TestHTTPMCPClient_EnsureAuthenticated_EmptyOAuth tests that ensureAuthenticated
// returns nil when no OAuth config is present (no auth needed).
func TestHTTPMCPClient_EnsureAuthenticated_EmptyOAuth(t *testing.T) {
	client := &HTTPMCPClient{
		Name:   "test",
		Token:  "",
		OAuth:  nil,
		client: &http.Client{},
	}

	err := client.ensureAuthenticated()
	if err != nil {
		t.Fatalf("ensureAuthenticated with nil OAuth should return nil, got: %v", err)
	}
	if client.Token != "" {
		t.Errorf("Token should still be empty, got: %q", client.Token)
	}
	t.Log("✅ ensureAuthenticated with nil OAuth returns nil")
}

// containsSubstring is a test helper that checks if a string contains a substring.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

// containsStr is a simple contains check that doesn't use strings.Contains
// to avoid import issues.
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
