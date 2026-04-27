package mcpproxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSaveAndLoadTokens tests saving tokens to a temp directory and loading them back.
func TestSaveAndLoadTokens(t *testing.T) {
	tmpDir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string {
		return tmpDir
	}
	defer func() { secretsDir = origSecretsDir }()

	serverName := "test-server"

	tokens := &StoredTokens{
		AccessToken:  "gho_test_access_token_12345",
		RefreshToken: "ghr_test_refresh_token_67890",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		Scope:        "repo,read:org",
	}

	// Save tokens
	if err := SaveTokens(serverName, tokens); err != nil {
		t.Fatalf("SaveTokens failed: %v", err)
	}

	// Verify the file exists with correct permissions
	tokenPath := TokenPath(serverName)
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("token file not found: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %#o", info.Mode().Perm())
	}

	// Load tokens back
	loaded, err := LoadTokens(serverName)
	if err != nil {
		t.Fatalf("LoadTokens failed: %v", err)
	}

	if loaded.AccessToken != tokens.AccessToken {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, tokens.AccessToken)
	}
	if loaded.RefreshToken != tokens.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", loaded.RefreshToken, tokens.RefreshToken)
	}
	if loaded.Scope != tokens.Scope {
		t.Errorf("Scope = %q, want %q", loaded.Scope, tokens.Scope)
	}
	if !loaded.ExpiresAt.Equal(tokens.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", loaded.ExpiresAt, tokens.ExpiresAt)
	}

	t.Log("✅ Save and load tokens works correctly")
}

// TestLoadTokens_MissingFile tests that LoadTokens returns error for missing file.
func TestLoadTokens_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string {
		return tmpDir
	}
	defer func() { secretsDir = origSecretsDir }()

	_, err := LoadTokens("nonexistent-server")
	if err == nil {
		t.Fatal("expected error for missing token file, got nil")
	}
	t.Logf("✅ Missing token file returns error: %v", err)
}

// TestSaveTokens_NilTokens tests SaveTokens with nil.
func TestSaveTokens_NilTokens(t *testing.T) {
	tmpDir := t.TempDir()
	origSecretsDir := secretsDir
	secretsDir = func() string {
		return tmpDir
	}
	defer func() { secretsDir = origSecretsDir }()

	err := SaveTokens("server-name", nil)
	if err == nil {
		t.Fatal("expected error for nil tokens, got nil")
	}
	t.Logf("✅ Nil tokens returns error: %v", err)
}

// TestParseDeviceAuthResponse tests parsing the JSON device authorization response.
func TestParseDeviceAuthResponse(t *testing.T) {
	jsonData := `{
		"device_code": "3584d83530557fdd1f46af8289938c8ef79f9dc5",
		"user_code": "WDJB-MJHT",
		"verification_uri": "https://github.com/login/device",
		"interval": 5
	}`

	var resp DeviceAuthResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to parse DeviceAuthResponse: %v", err)
	}

	if resp.DeviceCode != "3584d83530557fdd1f46af8289938c8ef79f9dc5" {
		t.Errorf("DeviceCode = %q, want %q", resp.DeviceCode, "3584d83530557fdd1f46af8289938c8ef79f9dc5")
	}
	if resp.UserCode != "WDJB-MJHT" {
		t.Errorf("UserCode = %q, want %q", resp.UserCode, "WDJB-MJHT")
	}
	if resp.VerificationURI != "https://github.com/login/device" {
		t.Errorf("VerificationURI = %q, want %q", resp.VerificationURI, "https://github.com/login/device")
	}
	if resp.Interval != 5 {
		t.Errorf("Interval = %d, want 5", resp.Interval)
	}

	t.Log("✅ DeviceAuthResponse parsed correctly")
}

// TestParseTokenResponse_Success tests parsing a successful token response.
func TestParseTokenResponse_Success(t *testing.T) {
	jsonData := `{
		"access_token": "gho_actual_access_token",
		"refresh_token": "ghr_actual_refresh_token",
		"expires_in": 3600,
		"scope": "repo read:org"
	}`

	var resp TokenResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to parse TokenResponse: %v", err)
	}

	if resp.AccessToken != "gho_actual_access_token" {
		t.Errorf("AccessToken = %q, want %q", resp.AccessToken, "gho_actual_access_token")
	}
	if resp.RefreshToken != "ghr_actual_refresh_token" {
		t.Errorf("RefreshToken = %q, want %q", resp.RefreshToken, "ghr_actual_refresh_token")
	}
	if resp.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn = %d, want 3600", resp.ExpiresIn)
	}
	if resp.Scope != "repo read:org" {
		t.Errorf("Scope = %q, want %q", resp.Scope, "repo read:org")
	}

	t.Log("✅ TokenResponse (success) parsed correctly")
}

// TestParseTokenResponse_Pending tests parsing a pending response (authorization_pending).
func TestParseTokenResponse_Pending(t *testing.T) {
	jsonData := `{
		"error": "authorization_pending"
	}`

	// TokenResponse doesn't have an Error field — the error comes as a top-level field
	// We'll parse this as a generic map to check the error value
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("failed to parse pending response: %v", err)
	}

	if errStr, ok := result["error"].(string); !ok || errStr != "authorization_pending" {
		t.Errorf("error = %v, want %q", result["error"], "authorization_pending")
	}

	t.Log("✅ Pending response detected correctly")
}

// TestTokenPath verifies TokenPath ends with correct suffix.
func TestTokenPath(t *testing.T) {
	origSecretsDir := secretsDir
	secretsDir = func() string {
		return "/fake/secrets/dir"
	}
	defer func() { secretsDir = origSecretsDir }()

	path := TokenPath("my-server")
	if path == "" {
		t.Fatal("TokenPath returned empty")
	}
	if !strings.HasSuffix(path, "my-server.json") {
		t.Errorf("TokenPath = %q, want suffix 'my-server.json'", path)
	}
	if !strings.HasPrefix(path, "/fake/secrets/dir/") {
		t.Errorf("TokenPath = %q, want prefix '/fake/secrets/dir/'", path)
	}
	t.Logf("✅ TokenPath = %s", path)
}

// TestTokenPath_SpecialChars tests TokenPath with special characters in server name.
func TestTokenPath_SpecialChars(t *testing.T) {
	origSecretsDir := secretsDir
	secretsDir = func() string {
		return "/tmp/secrets"
	}
	defer func() { secretsDir = origSecretsDir }()

	// Server names should be sanitized for safe filenames
	path := TokenPath("github.com/path")
	if !strings.HasSuffix(path, ".json") {
		t.Errorf("TokenPath = %q, want .json suffix", path)
	}
	t.Logf("✅ TokenPath with special chars = %s", path)
}

// TestHTTPMCPClient_TokenInjection tests that when Token is set, Authorization header is injected.
func TestHTTPMCPClient_TokenInjection(t *testing.T) {
	var authHeader string
	ts := newMockMCPServer(t)
	defer ts.Close()

	// Override the handler on the mock to capture the Authorization header
	// We need a custom server for this test
	client := &HTTPMCPClient{
		Name:   "token-test",
		URL:    ts.URL(),
		client: &http.Client{},
		Token:  "test-bearer-token-value",
		nextID: 0,
		Headers: map[string]string{},
	}

	// Build a request manually and check the header gets injected
	// We can test this through the existing mock which doesn't check auth headers
	// Create a quick custom test server instead
	customTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer customTS.Close()

	client.URL = customTS.URL

	// Trigger sendRequest via initialize
	err := client.initialize()
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if authHeader != "Bearer test-bearer-token-value" {
		t.Errorf("Authorization header = %q, want %q", authHeader, "Bearer test-bearer-token-value")
	}
	t.Log("✅ OAuth bearer token injected correctly")
}

// TestHTTPMCPClient_SetToken tests the SetToken method.
func TestHTTPMCPClient_SetToken(t *testing.T) {
	client := &HTTPMCPClient{
		Name: "test",
	}

	if client.Token != "" {
		t.Errorf("initial Token = %q, want empty", client.Token)
	}

	client.SetToken("new-token-value")
	if client.Token != "new-token-value" {
		t.Errorf("after SetToken, Token = %q, want %q", client.Token, "new-token-value")
	}
	t.Log("✅ SetToken works correctly")
}
