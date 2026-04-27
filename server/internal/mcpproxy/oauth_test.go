package mcpproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// =========================================================================
// PKCE tests
// =========================================================================

// TestGenerateCodeVerifier verifies the verifier is 43-128 chars with valid characters.
func TestGenerateCodeVerifier(t *testing.T) {
	verifier := GenerateCodeVerifier()
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Errorf("verifier length = %d, want between 43 and 128", len(verifier))
	}

	// Verify all characters are unreserved URL characters
	for _, c := range verifier {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~') {
			t.Errorf("invalid character in verifier: %c (0x%x)", c, c)
		}
	}

	// Verify consecutive calls produce different values
	verifier2 := GenerateCodeVerifier()
	if verifier == verifier2 {
		t.Error("verifier should be random, got identical values")
	}

	t.Logf("✅ Code verifier (len=%d) generated correctly", len(verifier))
}

// TestGenerateCodeChallenge verifies PKCE challenge generation using RFC 7636 test vectors.
func TestGenerateCodeChallenge(t *testing.T) {
	// RFC 7636 Appendix B test vectors
	// Verifier (S256): dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk
	// Challenge: E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expectedChallenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	challenge := GenerateCodeChallenge(verifier)
	if challenge != expectedChallenge {
		t.Errorf("challenge = %q, want %q", challenge, expectedChallenge)
	}

	t.Log("✅ PKCE challenge generated correctly (RFC 7636 test vectors)")
}

// TestExtractAuthCodeFromURL tests extracting auth code from redirect URLs.
func TestExtractAuthCodeFromURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expected    string
		expectError bool
	}{
		{
			name:        "valid redirect URL",
			url:         "http://localhost:3456/callback?code=abc123&state=xyz",
			expected:    "abc123",
			expectError: false,
		},
		{
			name:        "missing code param",
			url:         "http://localhost:3456/callback?state=xyz",
			expected:    "",
			expectError: true,
		},
		{
			name:        "invalid URL",
			url:         "http://%%invalid%%url",
			expected:    "",
			expectError: true,
		},
		{
			name:        "code with special characters",
			url:         "http://localhost:9999/callback?code=auth%2Fcode%2Btest&state=abc",
			expected:    "auth/code+test",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := ExtractAuthCodeFromRedirectURL(tt.url)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				t.Logf("Got expected error: %v", err)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if code != tt.expected {
				t.Errorf("code = %q, want %q", code, tt.expected)
			}
		})
	}

	t.Log("✅ ExtractAuthCodeFromRedirectURL works correctly")
}

// TestExchangeCodeForTokens tests exchanging an auth code for tokens using a mock server.
func TestExchangeCodeForTokens(t *testing.T) {
	var receivedForm url.Values

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method and content type
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("expected Content-Type application/x-www-form-urlencoded, got %s", ct)
		}

		// Read and decode form body
		body, _ := io.ReadAll(r.Body)
		receivedForm, _ = url.ParseQuery(string(body))

		// Return mock success response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "mock_access_token_123",
			"refresh_token": "mock_refresh_token_456",
			"expires_in":    3600,
			"scope":         "read write",
		})
	}))
	defer ts.Close()

	// Create test stdin with a redirect URL for AuthenticateAuthCodeFlow
	tokens, err := ExchangeCodeForTokens(ts.URL, "test-client", "auth_code_xyz", "http://localhost:9999/callback", "test_verifier")
	if err != nil {
		t.Fatalf("ExchangeCodeForTokens failed: %v", err)
	}

	// Verify the form body
	if receivedForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q, want %q", receivedForm.Get("grant_type"), "authorization_code")
	}
	if receivedForm.Get("code") != "auth_code_xyz" {
		t.Errorf("code = %q, want %q", receivedForm.Get("code"), "auth_code_xyz")
	}
	if receivedForm.Get("redirect_uri") != "http://localhost:9999/callback" {
		t.Errorf("redirect_uri = %q, want %q", receivedForm.Get("redirect_uri"), "http://localhost:9999/callback")
	}
	if receivedForm.Get("client_id") != "test-client" {
		t.Errorf("client_id = %q, want %q", receivedForm.Get("client_id"), "test-client")
	}
	if receivedForm.Get("code_verifier") != "test_verifier" {
		t.Errorf("code_verifier = %q, want %q", receivedForm.Get("code_verifier"), "test_verifier")
	}

	// Verify returned tokens
	if tokens.AccessToken != "mock_access_token_123" {
		t.Errorf("AccessToken = %q, want %q", tokens.AccessToken, "mock_access_token_123")
	}
	if tokens.RefreshToken != "mock_refresh_token_456" {
		t.Errorf("RefreshToken = %q, want %q", tokens.RefreshToken, "mock_refresh_token_456")
	}
	if tokens.Scope != "read write" {
		t.Errorf("Scope = %q, want %q", tokens.Scope, "read write")
	}
	if tokens.ExpiresAt.Before(time.Now().Add(30 * time.Minute)) {
		t.Error("ExpiresAt should be in the future (3600s from now)")
	}

	t.Log("✅ ExchangeCodeForTokens works correctly")
}

// TestExchangeCodeForTokens_Error tests that HTTP errors from the token endpoint are propagated.
func TestExchangeCodeForTokens_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "Authorization code expired",
		})
	}))
	defer ts.Close()

	_, err := ExchangeCodeForTokens(ts.URL, "test-client", "bad_code", "http://localhost:9999/callback", "verifier")
	if err == nil {
		t.Fatal("expected error for bad request, got nil")
	}
	t.Logf("✅ Token exchange error correctly handled: %v", err)
}

// TestRefreshTokens tests refreshing tokens using a mock server.
func TestRefreshTokens(t *testing.T) {
	var receivedForm url.Values

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		receivedForm, _ = url.ParseQuery(string(body))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new_access_token_789",
			"refresh_token": "new_refresh_token_012",
			"expires_in":    7200,
			"scope":         "read",
		})
	}))
	defer ts.Close()

	tokens, err := RefreshTokens(ts.URL, "test-client", "old_refresh_token")
	if err != nil {
		t.Fatalf("RefreshTokens failed: %v", err)
	}

	// Verify form body
	if receivedForm.Get("grant_type") != "refresh_token" {
		t.Errorf("grant_type = %q, want %q", receivedForm.Get("grant_type"), "refresh_token")
	}
	if receivedForm.Get("refresh_token") != "old_refresh_token" {
		t.Errorf("refresh_token = %q, want %q", receivedForm.Get("refresh_token"), "old_refresh_token")
	}
	if receivedForm.Get("client_id") != "test-client" {
		t.Errorf("client_id = %q, want %q", receivedForm.Get("client_id"), "test-client")
	}

	// Verify returned tokens
	if tokens.AccessToken != "new_access_token_789" {
		t.Errorf("AccessToken = %q, want %q", tokens.AccessToken, "new_access_token_789")
	}
	if tokens.RefreshToken != "new_refresh_token_012" {
		t.Errorf("RefreshToken = %q, want %q", tokens.RefreshToken, "new_refresh_token_012")
	}

	t.Log("✅ RefreshTokens works correctly")
}

// TestRefreshTokens_PreservesOldRefreshToken tests that when the server doesn't
// issue a new refresh token, the old one is preserved.
func TestRefreshTokens_PreservesOldRefreshToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "new_access_token_999",
			"expires_in":   3600,
			// No refresh_token in response
		})
	}))
	defer ts.Close()

	tokens, err := RefreshTokens(ts.URL, "test-client", "original_refresh_token")
	if err != nil {
		t.Fatalf("RefreshTokens failed: %v", err)
	}

	if tokens.RefreshToken != "original_refresh_token" {
		t.Errorf("RefreshToken = %q, want %q (original preserved)", tokens.RefreshToken, "original_refresh_token")
	}

	t.Log("✅ Old refresh token preserved when server doesn't issue a new one")
}

// TestRefreshTokens_EmptyRefreshToken tests that empty refresh token returns error.
func TestRefreshTokens_EmptyRefreshToken(t *testing.T) {
	_, err := RefreshTokens("http://example.com/token", "test-client", "")
	if err == nil {
		t.Fatal("expected error for empty refresh token, got nil")
	}
	t.Logf("✅ Empty refresh token handled: %v", err)
}

// TestAuthenticateAuthCodeFlow_Validation tests validation in AuthenticateAuthCodeFlow.
func TestAuthenticateAuthCodeFlow_Validation(t *testing.T) {
	tests := []struct {
		name    string
		oauth   *OAuthConfig
	}{
		{
			name:    "nil config",
			oauth:   nil,
		},
		{
			name:    "missing client_id",
			oauth:   &OAuthConfig{AuthorizationURL: "https://auth.example.com", TokenURL: "https://token.example.com"},
		},
		{
			name:    "missing authorization URL",
			oauth:   &OAuthConfig{ClientID: "test-client", TokenURL: "https://token.example.com"},
		},
		{
			name:    "missing token URL",
			oauth:   &OAuthConfig{ClientID: "test-client", AuthorizationURL: "https://auth.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AuthenticateAuthCodeFlow("test-server", tt.oauth)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			t.Logf("✅ Validation error: %v", err)
		})
	}
}
