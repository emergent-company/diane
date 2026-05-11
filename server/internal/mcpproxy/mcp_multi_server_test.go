package mcpproxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// =========================================================================
// Mock MCP servers with distinct toolsets
// =========================================================================

// newMockServer creates a mock MCP HTTP server that responds to
// initialize, tools/list, and tools/call with the given server info and tools.
func newMockServer(t *testing.T, name, version string, tools []map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					"serverInfo":      map[string]interface{}{"name": name, "version": version},
				},
			})
		case "tools/list":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{"tools": tools},
			})
		case "tools/call":
			var params struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			json.Unmarshal(req.Params, &params)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": name + " executed " + params.Name},
					},
				},
			})
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]interface{}{
					"code": -32601, "message": "method not found",
				},
			})
		}
	}))
}

// =========================================================================
// Test: Proxy.MultiServer_ListAllTools
// =========================================================================

func TestProxy_MultiServer_ListAllTools(t *testing.T) {
	// ── Create 3 mock MCP servers with distinct toolsets ──
	serverATools := []map[string]interface{}{
		{"name": "echo", "description": "Echo input", "inputSchema": map[string]interface{}{"type": "object"}},
		{"name": "add", "description": "Add two numbers", "inputSchema": map[string]interface{}{"type": "object"}},
	}
	serverBTools := []map[string]interface{}{
		{"name": "search", "description": "Search documents", "inputSchema": map[string]interface{}{"type": "object"}},
		{"name": "read", "description": "Read a document", "inputSchema": map[string]interface{}{"type": "object"}},
	}
	serverCTools := []map[string]interface{}{
		{"name": "list", "description": "List resources", "inputSchema": map[string]interface{}{"type": "object"}},
	}

	mockA := newMockServer(t, "server-A", "1.0.0", serverATools)
	defer mockA.Close()
	mockB := newMockServer(t, "server-B", "2.0.0", serverBTools)
	defer mockB.Close()
	mockC := newMockServer(t, "server-C", "3.0.0", serverCTools)
	defer mockC.Close()

	// ── Create Proxy with all 3 servers ──
	dir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string { return dir }
	defer func() { secretsDir = origSecretsDir }()

	servers := []ServerConfig{
		{Name: "server-A", Enabled: true, Type: "http", URL: mockA.URL},
		{Name: "server-B", Enabled: true, Type: "http", URL: mockB.URL},
		{Name: "server-C", Enabled: true, Type: "http", URL: mockC.URL},
	}

	proxy, err := NewProxy(servers)
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}
	defer proxy.Close()

	// ── ListAllTools returns all 5 tools ──
	tools, err := proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools failed: %v", err)
	}

	if len(tools) != 5 {
		t.Fatalf("ListAllTools returned %d tools, want 5", len(tools))
	}
	t.Logf("✅ ListAllTools returned %d tools", len(tools))

	// ── Verify tool names are prefixed with server name ──
	expectedNames := map[string]string{
		"server-A_echo":   "server-A",
		"server-A_add":    "server-A",
		"server-B_search": "server-B",
		"server-B_read":   "server-B",
		"server-C_list":   "server-C",
	}

	found := make(map[string]bool)
	for _, tool := range tools {
		name, ok := tool["name"].(string)
		if !ok {
			t.Errorf("tool missing 'name' field: %v", tool)
			continue
		}
		wantServer, ok := expectedNames[name]
		if !ok {
			t.Errorf("unexpected tool name: %s", name)
			continue
		}
		found[name] = true

		// Verify _server field
		gotServer, ok := tool["_server"].(string)
		if !ok || gotServer != wantServer {
			t.Errorf("tool %q _server=%q, want %q", name, gotServer, wantServer)
		}

		// Verify description preserved
		desc, ok := tool["description"].(string)
		if !ok || desc == "" {
			t.Errorf("tool %q missing description", name)
		}

		t.Logf("  ✅ %s  (server=%s, desc=%s)", name, gotServer, desc)
	}

	for name := range expectedNames {
		if !found[name] {
			t.Errorf("missing expected tool: %s", name)
		}
	}

	t.Log("✅ Multi-server ListAllTools: all tools present with correct prefixes")
}

// =========================================================================
// Test: Proxy.MultiServer_CallTool routes to correct server
// =========================================================================

func TestProxy_MultiServer_CallTool_Routing(t *testing.T) {
	mockA := newMockServer(t, "server-A", "1.0.0", []map[string]interface{}{
		{"name": "echo", "description": "Echo", "inputSchema": map[string]interface{}{"type": "object"}},
	})
	defer mockA.Close()
	mockB := newMockServer(t, "server-B", "2.0.0", []map[string]interface{}{
		{"name": "search", "description": "Search", "inputSchema": map[string]interface{}{"type": "object"}},
	})
	defer mockB.Close()

	dir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string { return dir }
	defer func() { secretsDir = origSecretsDir }()

	servers := []ServerConfig{
		{Name: "server-A", Enabled: true, Type: "http", URL: mockA.URL},
		{Name: "server-B", Enabled: true, Type: "http", URL: mockB.URL},
	}

	proxy, err := NewProxy(servers)
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}
	defer proxy.Close()

	// ── Call tool on server-A ──
	result, err := proxy.CallTool("server-A_echo", map[string]interface{}{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool server-A_echo failed: %v", err)
	}
	var resultMap map[string]interface{}
	if err := json.Unmarshal(result, &resultMap); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	content, _ := resultMap["content"].([]interface{})
	if len(content) == 0 {
		t.Fatal("response missing content")
	}
	first := content[0].(map[string]interface{})
	if first["text"] != "server-A executed echo" {
		t.Errorf("server-A response text = %q, want %q", first["text"], "server-A executed echo")
	}
	t.Log("  ✅ server-A_echo routed correctly")

	// ── Call tool on server-B ──
	result, err = proxy.CallTool("server-B_search", map[string]interface{}{"query": "test"})
	if err != nil {
		t.Fatalf("CallTool server-B_search failed: %v", err)
	}
	if err := json.Unmarshal(result, &resultMap); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	content, _ = resultMap["content"].([]interface{})
	if len(content) == 0 {
		t.Fatal("response missing content")
	}
	first = content[0].(map[string]interface{})
	if first["text"] != "server-B executed search" {
		t.Errorf("server-B response text = %q, want %q", first["text"], "server-B executed search")
	}
	t.Log("  ✅ server-B_search routed correctly")

	t.Log("✅ Multi-server CallTool routing: tools routed to correct servers")
}

// =========================================================================
// Test: Proxy.MultiServer_CallTool_UnknownTool
// =========================================================================

func TestProxy_MultiServer_CallTool_UnknownTool(t *testing.T) {
	dir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string { return dir }
	defer func() { secretsDir = origSecretsDir }()

	// Proxy with no clients
	proxy, err := NewProxy([]ServerConfig{})
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}
	defer proxy.Close()

	_, err = proxy.CallTool("nonexistent_tool", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}

	errStr := err.Error()
	if errStr != "unknown tool: nonexistent_tool" {
		t.Errorf("error message = %q, want %q", errStr, "unknown tool: nonexistent_tool")
	}
	t.Logf("  ✅ unknown tool error: %s", errStr)

	// ── Non-existent server within known prefix pattern ──
	// When no servers exist at all, any tool is unknown
	t.Log("✅ Unknown tool correctly returns error")
}

// =========================================================================
// Test: Proxy.MultiServer_Reload_AddAndRemoveServers
// =========================================================================

func TestProxy_MultiServer_Reload(t *testing.T) {
	mockA := newMockServer(t, "alpha", "1.0.0", []map[string]interface{}{
		{"name": "ping", "description": "Ping", "inputSchema": map[string]interface{}{"type": "object"}},
	})
	defer mockA.Close()
	mockB := newMockServer(t, "beta", "2.0.0", []map[string]interface{}{
		{"name": "pong", "description": "Pong", "inputSchema": map[string]interface{}{"type": "object"}},
	})
	defer mockB.Close()
	mockC := newMockServer(t, "gamma", "3.0.0", []map[string]interface{}{
		{"name": "status", "description": "Status", "inputSchema": map[string]interface{}{"type": "object"}},
	})
	defer mockC.Close()

	dir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string { return dir }
	defer func() { secretsDir = origSecretsDir }()

	// ── Start with alpha + beta ──
	servers := []ServerConfig{
		{Name: "alpha", Enabled: true, Type: "http", URL: mockA.URL},
		{Name: "beta", Enabled: true, Type: "http", URL: mockB.URL},
	}

	proxy, err := NewProxy(servers)
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}
	defer proxy.Close()

	// Verify 2 tools
	tools, err := proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("after init: got %d tools, want 2", len(tools))
	}
	t.Logf("✅ Initial tools: %d (alpha_ping, beta_pong)", len(tools))

	// ── Reload: remove beta, add gamma ──
	newServers := []ServerConfig{
		{Name: "alpha", Enabled: true, Type: "http", URL: mockA.URL},
		{Name: "gamma", Enabled: true, Type: "http", URL: mockC.URL},
	}

	if err := proxy.Reload(newServers); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Verify 2 tools (alpha_ping + gamma_status)
	tools, err = proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools after reload failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("after reload: got %d tools, want 2", len(tools))
	}
	t.Logf("✅ After reload (removed beta, added gamma): %d tools", len(tools))

	// Check specific tools
	hasAlpha := false
	hasGamma := false
	hasBeta := false
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		switch name {
		case "alpha_ping":
			hasAlpha = true
		case "gamma_status":
			hasGamma = true
		case "beta_pong":
			hasBeta = true
		}
	}
	if !hasAlpha {
		t.Error("alpha_ping missing after reload")
	}
	if !hasGamma {
		t.Error("gamma_status missing after reload")
	}
	if hasBeta {
		t.Error("beta_pong still present after removal")
	}
	t.Logf("  ✅ alpha_ping present: %v", hasAlpha)
	t.Logf("  ✅ gamma_status present: %v", hasGamma)
	t.Logf("  ✅ beta_pong removed: %v", !hasBeta)

	// ── Reload: all servers removed ──
	if err := proxy.Reload([]ServerConfig{}); err != nil {
		t.Fatalf("Reload to empty failed: %v", err)
	}

	tools, err = proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools after empty reload failed: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("after empty reload: got %d tools, want 0", len(tools))
	}
	t.Log("✅ After reload to empty: 0 tools")

	// ── Add gamma back via Reload ──
	if err := proxy.Reload([]ServerConfig{
		{Name: "gamma", Enabled: true, Type: "http", URL: mockC.URL},
	}); err != nil {
		t.Fatalf("Reload add gamma back failed: %v", err)
	}

	tools, err = proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0]["name"] != "gamma_status" {
		t.Errorf("expected gamma_status only, got %d tools: %v", len(tools), toolNames(tools))
	}
	t.Log("✅ Gamma re-added via Reload")

	t.Log("✅ Multi-server Reload: add, remove, clear, re-add all working")
}

// =========================================================================
// Test: Proxy.MultiServer_DisabledServersSkipped
// =========================================================================

func TestProxy_MultiServer_DisabledServersSkipped(t *testing.T) {
	mockA := newMockServer(t, "enabled-server", "1.0.0", []map[string]interface{}{
		{"name": "status", "description": "Status", "inputSchema": map[string]interface{}{"type": "object"}},
	})
	defer mockA.Close()
	mockB := newMockServer(t, "disabled-server", "2.0.0", []map[string]interface{}{
		{"name": "secret", "description": "Secret", "inputSchema": map[string]interface{}{"type": "object"}},
	})
	defer mockB.Close()

	dir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string { return dir }
	defer func() { secretsDir = origSecretsDir }()

	servers := []ServerConfig{
		{Name: "enabled-server", Enabled: true, Type: "http", URL: mockA.URL},
		{Name: "disabled-server", Enabled: false, Type: "http", URL: mockB.URL},
	}

	proxy, err := NewProxy(servers)
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}
	defer proxy.Close()

	tools, err := proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools failed: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("ListAllTools returned %d tools, want 1 (disabled server should be skipped)", len(tools))
	}

	if tools[0]["name"] != "enabled-server_status" {
		t.Errorf("tool name = %q, want %q", tools[0]["name"], "enabled-server_status")
	}

	t.Log("✅ Disabled server correctly skipped, only enabled server's tools returned")
	t.Log("✅ Proxy.MultiServer_DisabledServersSkipped passed")
}

// =========================================================================
// Test: Proxy.MultiServer_Reload_DisabledToggle
// =========================================================================

func TestProxy_MultiServer_Reload_DisabledToggle(t *testing.T) {
	mock := newMockServer(t, "toggle", "1.0.0", []map[string]interface{}{
		{"name": "flip", "description": "Flip", "inputSchema": map[string]interface{}{"type": "object"}},
	})
	defer mock.Close()

	dir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string { return dir }
	defer func() { secretsDir = origSecretsDir }()

	// Start disabled
	proxy, err := NewProxy([]ServerConfig{
		{Name: "toggle", Enabled: false, Type: "http", URL: mock.URL},
	})
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}
	defer proxy.Close()

	tools, err := proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools failed: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("disabled: got %d tools, want 0", len(tools))
	}
	t.Log("✅ Disabled: 0 tools")

	// Enable via Reload
	if err := proxy.Reload([]ServerConfig{
		{Name: "toggle", Enabled: true, Type: "http", URL: mock.URL},
	}); err != nil {
		t.Fatalf("Reload enable failed: %v", err)
	}

	tools, err = proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0]["name"] != "toggle_flip" {
		t.Errorf("enabled: expected toggle_flip, got %v", toolNames(tools))
	}
	t.Log("✅ Enabled via Reload: toggle_flip present")

	// Disable again via Reload
	if err := proxy.Reload([]ServerConfig{
		{Name: "toggle", Enabled: false, Type: "http", URL: mock.URL},
	}); err != nil {
		t.Fatalf("Reload disable failed: %v", err)
	}

	tools, err = proxy.ListAllTools()
	if err != nil {
		t.Fatalf("ListAllTools failed: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("disabled again: got %d tools, want 0", len(tools))
	}
	t.Log("✅ Disabled again via Reload: 0 tools")

	t.Log("✅ Proxy.MultiServer_Reload_DisabledToggle passed")
}

// =========================================================================
// Helpers
// =========================================================================

func toolNames(tools []map[string]interface{}) []string {
	names := make([]string, len(tools))
	for i, tool := range tools {
		name, _ := tool["name"].(string)
		names[i] = name
	}
	return names
}
