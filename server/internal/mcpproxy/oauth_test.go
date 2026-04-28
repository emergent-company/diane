package mcpproxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =========================================================================
// Tests: DynamicClientRegistration
// =========================================================================

// newMockRegistrationServer creates a test server that implements RFC 7591
// dynamic client registration. It accepts a POST with client metadata and
// returns a client_id.
func newMockRegistrationServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Verify required fields
		if _, ok := body["client_name"]; !ok {
			http.Error(w, "missing client_name", http.StatusBadRequest)
			return
		}
		if _, ok := body["redirect_uris"]; !ok {
			http.Error(w, "missing redirect_uris", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"client_id":           "test-client-id-12345",
			"client_id_issued_at": 1700000000,
			"client_name":         body["client_name"],
		})
	}))
}

// TestDynamicClientRegistration verifies that a valid registration endpoint
// returns a client_id.
func TestDynamicClientRegistration(t *testing.T) {
	mock := newMockRegistrationServer()
	defer mock.Close()

	clientID, err := DynamicClientRegistration(mock.URL)
	if err != nil {
		t.Fatalf("DynamicClientRegistration failed: %v", err)
	}

	if clientID != "test-client-id-12345" {
		t.Errorf("clientID = %q, want %q", clientID, "test-client-id-12345")
	}

	t.Logf("✅ DynamicClientRegistration returned client_id: %s", clientID)
}

// TestDynamicClientRegistration_ErrorResponse verifies that a registration
// endpoint returning an error status code produces an error.
func TestDynamicClientRegistration_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_redirect_uri"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := DynamicClientRegistration(server.URL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	t.Logf("✅ Got expected error: %v", err)
}

// TestDynamicClientRegistration_MissingClientID verifies that a registration
// response without a client_id produces an error.
func TestDynamicClientRegistration_MissingClientID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"client_name": "test",
			// no client_id
		})
	}))
	defer server.Close()

	_, err := DynamicClientRegistration(server.URL)
	if err == nil {
		t.Fatal("expected error for missing client_id, got nil")
	}
	t.Logf("✅ Got expected error: %v", err)
}

// TestDynamicClientRegistration_ConnectionError verifies that a connection
// failure produces an error.
func TestDynamicClientRegistration_ConnectionError(t *testing.T) {
	_, err := DynamicClientRegistration("http://127.0.0.1:1/register")
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
	t.Logf("✅ Got expected error: %v", err)
}

// =========================================================================
// Tests: SaveDiscoveredConfig / LoadDiscoveredConfig
// =========================================================================

// TestSaveAndLoadDiscoveredConfig verifies that a saved discovered config
// can be loaded back.
func TestSaveAndLoadDiscoveredConfig(t *testing.T) {
	// Set up a temp secrets dir
	tmpDir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string { return filepath.Join(tmpDir, ".diane", "secrets") }
	defer func() { secretsDir = origSecretsDir }()

	cfg := &OAuthConfig{
		AuthorizationURL: "https://auth.example.com/authorize",
		TokenURL:         "https://auth.example.com/token",
		RegistrationURL:  "https://auth.example.com/register",
		Scopes:           []string{"read", "write"},
	}

	if err := SaveDiscoveredConfig("test-server", cfg); err != nil {
		t.Fatalf("SaveDiscoveredConfig failed: %v", err)
	}

	loaded := LoadDiscoveredConfig("test-server")
	if loaded == nil {
		t.Fatal("LoadDiscoveredConfig returned nil")
	}

	if loaded.AuthorizationURL != cfg.AuthorizationURL {
		t.Errorf("AuthorizationURL = %q, want %q", loaded.AuthorizationURL, cfg.AuthorizationURL)
	}
	if loaded.TokenURL != cfg.TokenURL {
		t.Errorf("TokenURL = %q, want %q", loaded.TokenURL, cfg.TokenURL)
	}
	if loaded.RegistrationURL != cfg.RegistrationURL {
		t.Errorf("RegistrationURL = %q, want %q", loaded.RegistrationURL, cfg.RegistrationURL)
	}
	if len(loaded.Scopes) != 2 || loaded.Scopes[0] != "read" || loaded.Scopes[1] != "write" {
		t.Errorf("Scopes = %v, want %v", loaded.Scopes, cfg.Scopes)
	}

	t.Logf("✅ Save/LoadDiscoveredConfig round-trip successful")
}

// TestLoadDiscoveredConfig_NoFile verifies that LoadDiscoveredConfig returns
// nil when no config file exists.
func TestLoadDiscoveredConfig_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string { return filepath.Join(tmpDir, ".diane", "secrets") }
	defer func() { secretsDir = origSecretsDir }()

	loaded := LoadDiscoveredConfig("nonexistent-server")
	if loaded != nil {
		t.Fatal("expected nil for missing file")
	}
}

// TestLoadDiscoveredConfig_InvalidJSON verifies that LoadDiscoveredConfig
// returns nil for corrupted files.
func TestLoadDiscoveredConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string { return filepath.Join(tmpDir, ".diane", "secrets") }
	defer func() { secretsDir = origSecretsDir }()

	// Write invalid JSON
	secretsPath := filepath.Join(tmpDir, ".diane", "secrets")
	os.MkdirAll(secretsPath, 0700)
	os.WriteFile(filepath.Join(secretsPath, "test-server-oauth-config.json"), []byte("{invalid}"), 0600)

	loaded := LoadDiscoveredConfig("test-server")
	if loaded != nil {
		t.Fatal("expected nil for invalid JSON")
	}
}

// =========================================================================
// Tests: discoverOAuthFromHeader with registration_endpoint
// =========================================================================

// TestDiscoverOAuthFromHeader_WithRegistrationEndpoint verifies that
// the discovery chain captures the registration_endpoint from the well-known
// OAuth metadata.
func TestDiscoverOAuthFromHeader_WithRegistrationEndpoint(t *testing.T) {
	// Start the well-known metadata server
	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                                "https://mcp.example.com/",
			"authorization_endpoint":                "https://mcp.example.com/authorize",
			"token_endpoint":                        "https://mcp.example.com/token",
			"registration_endpoint":                 "https://mcp.example.com/register",
			"scopes_supported":                      []string{"read", "write", "admin"},
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"token_endpoint_auth_methods_supported": []string{"none"},
			"code_challenge_methods_supported":      []string{"S256"},
		})
	}))
	defer metaSrv.Close()

	// Start the resource metadata server (points to the well-known auth server)
	resSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"resource":              "https://mcp.example.com/",
			"authorization_servers": []string{metaSrv.URL},
		})
	}))
	defer resSrv.Close()

	// Construct a WWW-Authenticate header value pointing to our resource metadata
	header := http.Header{}
	header.Set("www-authenticate",
		`Bearer error="invalid_token", resource_metadata="`+resSrv.URL+`"`)

	// Create a client and call discoverOAuthFromHeader
	client := &HTTPMCPClient{Name: "test-server"}
	cfg, err := client.discoverOAuthFromHeader(header)
	if err != nil {
		t.Fatalf("discoverOAuthFromHeader failed: %v", err)
	}

	if cfg.AuthorizationURL != "https://mcp.example.com/authorize" {
		t.Errorf("AuthorizationURL = %q, want %q", cfg.AuthorizationURL, "https://mcp.example.com/authorize")
	}
	if cfg.TokenURL != "https://mcp.example.com/token" {
		t.Errorf("TokenURL = %q, want %q", cfg.TokenURL, "https://mcp.example.com/token")
	}
	if cfg.RegistrationURL != "https://mcp.example.com/register" {
		t.Errorf("RegistrationURL = %q, want %q", cfg.RegistrationURL, "https://mcp.example.com/register")
	}
	if len(cfg.Scopes) != 3 || cfg.Scopes[0] != "read" {
		t.Errorf("Scopes = %v, want [read write admin]", cfg.Scopes)
	}

	t.Logf("✅ discoverOAuthFromHeader captured registration_endpoint: %s", cfg.RegistrationURL)
}

// TestDiscoverOAuthFromHeader_NoRegistrationEndpoint verifies that discovery
// works even when the well-known metadata has no registration_endpoint.
func TestDiscoverOAuthFromHeader_NoRegistrationEndpoint(t *testing.T) {
	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                 "https://mcp.example.com/",
			"authorization_endpoint": "https://mcp.example.com/authorize",
			"token_endpoint":         "https://mcp.example.com/token",
			"scopes_supported":       []string{"read"},
		})
	}))
	defer metaSrv.Close()

	resSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"resource":              "https://mcp.example.com/",
			"authorization_servers": []string{metaSrv.URL},
		})
	}))
	defer resSrv.Close()

	header := http.Header{}
	header.Set("www-authenticate",
		`Bearer error="invalid_token", resource_metadata="`+resSrv.URL+`"`)

	client := &HTTPMCPClient{Name: "test-server"}
	cfg, err := client.discoverOAuthFromHeader(header)
	if err != nil {
		t.Fatalf("discoverOAuthFromHeader failed: %v", err)
	}

	if cfg.RegistrationURL != "" {
		t.Errorf("expected empty RegistrationURL, got %q", cfg.RegistrationURL)
	}
	if cfg.AuthorizationURL != "https://mcp.example.com/authorize" {
		t.Errorf("AuthorizationURL = %q, want %q", cfg.AuthorizationURL, "https://mcp.example.com/authorize")
	}

	t.Logf("✅ discoverOAuthFromHeader without registration_endpoint works correctly")
}

// =========================================================================
// Tests: 401 handling with auto-discovery (no interactive auth in relay)
// =========================================================================

// newMock401WithDiscoveryServer creates a mock MCP HTTP server that:
// 1. Returns 401 with a Bearer www-authenticate header (simulating OAuth challenge)
// 2. Has a well-known metadata endpoint
// The result is that the client discovers OAuth config and reports the error
// without trying interactive auth.
func newMock401WithDiscoveryServer() *httptest.Server {
	mux := http.NewServeMux()

	// Well-known OAuth metadata endpoint (discovery chain step 3)
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                 "https://mcp.example.com/",
			"authorization_endpoint": "https://mcp.example.com/authorize",
			"token_endpoint":         "https://mcp.example.com/token",
			"scopes_supported":       []string{"read"},
		})
	})

	// Resource metadata endpoint (discovery chain step 2)
	mux.HandleFunc("/resource-metadata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return the base server URL — production code appends /.well-known/oauth-authorization-server
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL := scheme + "://" + r.Host
		json.NewEncoder(w).Encode(map[string]interface{}{
			"resource":              baseURL,
			"authorization_servers": []string{baseURL},
		})
	})

	// MCP endpoint — always returns 401 with www-authenticate header
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		// Build the resource-metadata URL using the same server
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		resMetaURL := scheme + "://" + r.Host + "/resource-metadata"

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("www-authenticate",
			`Bearer error="invalid_token", resource_metadata="`+resMetaURL+`"`)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_token"})
	})

	return httptest.NewServer(mux)
}

// TestHTTPMCPClient_401WithDiscovery_ReturnsActionableError verifies that
// when the server returns 401 with a www-authenticate header, the client
// auto-discovers the OAuth config and returns a clean error (does not block
// on stdin).
func TestHTTPMCPClient_401WithDiscovery_ReturnsActionableError(t *testing.T) {
	mock := newMock401WithDiscoveryServer()
	defer mock.Close()

	// Use a temp dir for secrets so we can check the saved config
	tmpDir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string { return filepath.Join(tmpDir, ".diane", "secrets") }
	defer func() { secretsDir = origSecretsDir }()

	// NewHTTPMCPClient calls initialize() which gets the 401.
	// The client should auto-discover OAuth from the header, save it,
	// and return the actionable error without blocking on stdin.
	client, err := NewHTTPMCPClient("discovery-test", mock.URL+"/mcp", nil, nil, 0)
	if err == nil {
		client.Close()
		t.Fatal("expected error from 401 on initialize, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "run 'diane mcp auth") {
		t.Errorf("expected actionable error containing \"run 'diane mcp auth\", got: %v", err)
	}
	t.Logf("✅ Got actionable error without blocking on stdin: %v", err)

	// Verify the discovered config was saved to disk
	loaded := LoadDiscoveredConfig("discovery-test")
	if loaded == nil {
		t.Fatal("expected discovered config to be saved to disk")
	}
	if loaded.AuthorizationURL == "" || loaded.TokenURL == "" {
		t.Errorf("discovered config missing endpoints: %+v", loaded)
	}
	t.Logf("✅ Discovered config saved: auth=%s token=%s", loaded.AuthorizationURL, loaded.TokenURL)
}
