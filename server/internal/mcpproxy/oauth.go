package mcpproxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// =========================================================================
// Token types and storage
// =========================================================================

// StoredTokens holds OAuth tokens persisted on disk.
type StoredTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scope        string    `json:"scope,omitempty"`
}

// secretsDir is a package-level variable so tests can override it.
var secretsDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if home dir can't be determined
		return filepath.Join(".", ".diane", "secrets")
	}
	return filepath.Join(home, ".diane", "secrets")
}

// TokenPath returns the token file path for a server.
// The server name is sanitized to be safe as a filename component.
func TokenPath(serverName string) string {
	// Sanitize server name: replace path separators and spaces with underscores
	safeName := strings.ReplaceAll(serverName, "/", "_")
	safeName = strings.ReplaceAll(safeName, "\\", "_")
	safeName = strings.ReplaceAll(safeName, " ", "_")
	return filepath.Join(secretsDir(), safeName+".json")
}

// LoadTokens loads stored tokens from disk for the given server name.
// Returns an error if the token file does not exist or cannot be parsed.
func LoadTokens(serverName string) (*StoredTokens, error) {
	path := TokenPath(serverName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file %s: %w", path, err)
	}

	var tokens StoredTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("failed to parse token file %s: %w", path, err)
	}

	return &tokens, nil
}

// SaveTokens saves tokens to disk for the given server name with 0600 permissions.
// The secrets directory is created if it does not exist.
func SaveTokens(serverName string, tokens *StoredTokens) error {
	if tokens == nil {
		return fmt.Errorf("cannot save nil tokens")
	}

	dir := secretsDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create secrets directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tokens: %w", err)
	}

	path := TokenPath(serverName)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file %s: %w", path, err)
	}

	return nil
}

// =========================================================================
// OAuth device authorization flow types
// =========================================================================

// DeviceAuthResponse from the device authorization endpoint.
type DeviceAuthResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
}

// TokenResponse from the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// tokenErrorResponse represents an error response from the token endpoint
// (e.g., authorization_pending, slow_down, expired_token).
type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// =========================================================================
// Device authorization flow
// =========================================================================

// AuthenticateDeviceFlow performs OAuth device authorization flow.
//  1. POST to DeviceAuthURL with client_id
//  2. Print user_code + verification_uri to the user
//  3. Poll TokenURL with device_code
//  4. On success: save tokens
//
// Returns the access token.
func AuthenticateDeviceFlow(serverName string, oauth *OAuthConfig) (string, error) {
	if oauth == nil {
		return "", fmt.Errorf("OAuthConfig is nil")
	}
	if oauth.ClientID == "" {
		return "", fmt.Errorf("OAuthConfig.ClientID is required")
	}
	if oauth.DeviceAuthURL == "" {
		return "", fmt.Errorf("OAuthConfig.DeviceAuthURL is required")
	}
	if oauth.TokenURL == "" {
		return "", fmt.Errorf("OAuthConfig.TokenURL is required")
	}

	// Step 1: Request device code
	deviceResp, err := requestDeviceCode(oauth)
	if err != nil {
		return "", fmt.Errorf("failed to request device code: %w", err)
	}

	// Step 2: Print user_code and verification_uri to the user
	fmt.Printf("\n🔐 Device Authorization Required for %s\n", serverName)
	fmt.Printf("   Visit: %s\n", deviceResp.VerificationURI)
	fmt.Printf("   Code:  %s\n\n", deviceResp.UserCode)

	// Step 3: Poll for token
	tokenResp, err := pollForToken(oauth, deviceResp)
	if err != nil {
		return "", fmt.Errorf("device authorization failed: %w", err)
	}

	// Step 4: Save tokens
	stored := &StoredTokens{
		AccessToken: tokenResp.AccessToken,
		Scope:       tokenResp.Scope,
	}
	if tokenResp.RefreshToken != "" {
		stored.RefreshToken = tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		stored.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	if err := SaveTokens(serverName, stored); err != nil {
		log.Printf("Warning: failed to save OAuth tokens for %s: %v", serverName, err)
	}

	fmt.Printf("✅ OAuth authorization complete for %s\n", serverName)
	return tokenResp.AccessToken, nil
}

// requestDeviceCode sends a POST request to the device authorization endpoint
// and returns the device code response.
func requestDeviceCode(oauth *OAuthConfig) (*DeviceAuthResponse, error) {
	form := url.Values{}
	form.Set("client_id", oauth.ClientID)
	if len(oauth.Scopes) > 0 {
		form.Set("scope", strings.Join(oauth.Scopes, " "))
	}

	resp, err := http.PostForm(oauth.DeviceAuthURL, form)
	if err != nil {
		return nil, fmt.Errorf("device auth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read device auth response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("device auth endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var deviceResp DeviceAuthResponse
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		return nil, fmt.Errorf("failed to parse device auth response: %w", err)
	}

	if deviceResp.DeviceCode == "" {
		return nil, fmt.Errorf("device auth response missing device_code")
	}

	// Default interval to 5 seconds if not provided
	if deviceResp.Interval <= 0 {
		deviceResp.Interval = 5
	}

	return &deviceResp, nil
}

// pollForToken polls the token endpoint until authorization is granted or expires.
func pollForToken(oauth *OAuthConfig, deviceResp *DeviceAuthResponse) (*TokenResponse, error) {
	httpClient := &http.Client{}

	for {
		time.Sleep(time.Duration(deviceResp.Interval) * time.Second)

		form := url.Values{}
		form.Set("client_id", oauth.ClientID)
		form.Set("device_code", deviceResp.DeviceCode)
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

		resp, err := httpClient.PostForm(oauth.TokenURL, form)
		if err != nil {
			return nil, fmt.Errorf("token poll request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read token response: %w", err)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Check for success
			var tokenResp TokenResponse
			if err := json.Unmarshal(body, &tokenResp); err != nil {
				return nil, fmt.Errorf("failed to parse token response: %w", err)
			}

			if tokenResp.AccessToken != "" {
				return &tokenResp, nil
			}

			// If no access token, check for error fields
			var errResp tokenErrorResponse
			if err := json.Unmarshal(body, &errResp); err != nil {
				return nil, fmt.Errorf("unexpected token response: %s", string(body))
			}

			switch errResp.Error {
			case "authorization_pending":
				// Continue polling
				continue
			case "slow_down":
				// Increase interval by 5 seconds
				deviceResp.Interval += 5
				continue
			case "expired_token":
				return nil, fmt.Errorf("device code expired, please restart the authorization process")
			case "access_denied":
				return nil, fmt.Errorf("authorization denied by user")
			default:
				return nil, fmt.Errorf("token endpoint error: %s", errResp.Error)
			}
		} else {
			// Non-200 response
			var errResp tokenErrorResponse
			if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
				switch errResp.Error {
				case "authorization_pending":
					continue
				case "slow_down":
					deviceResp.Interval += 5
					continue
				case "expired_token":
					return nil, fmt.Errorf("device code expired, please restart the authorization process")
				case "access_denied":
					return nil, fmt.Errorf("authorization denied by user")
				default:
					return nil, fmt.Errorf("token endpoint error: %s", errResp.Error)
				}
			}
			return nil, fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
		}
	}
}

// =========================================================================
// PKCE helpers (RFC 7636)
// =========================================================================

// GenerateCodeVerifier creates a random PKCE code verifier (43-128 chars,
// using unreserved URL characters: A-Z, a-z, 0-9, -, ., _, ~).
// Uses crypto/rand for secure randomness.
func GenerateCodeVerifier() string {
	// Generate 64 random bytes → base64url encoded = 86 characters (within 43-128 range)
	buf := make([]byte, 64)
	if _, err := rand.Read(buf); err != nil {
		// Fallback to 32 bytes if crypto/rand fails (extremely unlikely)
		buf = make([]byte, 32)
		rand.Read(buf)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// GenerateCodeChallenge creates the S256 PKCE code challenge from a verifier.
// SHA256 hash → base64url encoding (no padding).
func GenerateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// =========================================================================
// Dynamic client registration (RFC 7591)
// =========================================================================

// DynamicClientRegistration registers a new OAuth client with an authorization server
// that supports RFC 7591. It POSTs a client metadata document to the registration
// endpoint and returns the assigned client_id.
//
// The redirect_uri defaults to http://localhost:28561/callback which Diane uses
// as its standard OAuth callback port.
func DynamicClientRegistration(registrationURL string) (string, error) {
	body := map[string]interface{}{
		"client_name":                  "Diane AI Assistant",
		"redirect_uris":                []string{"http://localhost:28561/callback"},
		"grant_types":                  []string{"authorization_code", "refresh_token"},
		"response_types":               []string{"code"},
		"token_endpoint_auth_method":   "none",
		"application_type":             "native",
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal registration request: %w", err)
	}

	resp, err := http.Post(registrationURL, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read registration response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("registration endpoint returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse registration response: %w", err)
	}
	if result.ClientID == "" {
		return "", fmt.Errorf("registration response missing client_id: %s", string(respBody))
	}

	log.Printf("[OAuth] Registered new client: client_id=%s", result.ClientID)
	return result.ClientID, nil
}

// =========================================================================
// Authorization code flow with PKCE
// =========================================================================

// ExtractAuthCodeFromRedirectURL parses the authorization code from a redirect URL.
// Expected format: http://localhost:PORT/callback?code=AUTH_CODE&state=STATE
func ExtractAuthCodeFromRedirectURL(redirectURL string) (string, error) {
	parsed, err := url.Parse(redirectURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse redirect URL: %w", err)
	}

	code := parsed.Query().Get("code")
	if code == "" {
		return "", fmt.Errorf("redirect URL missing 'code' query parameter")
	}

	return code, nil
}

// ExchangeCodeForTokens POSTs to the token endpoint to exchange an auth code for tokens.
// Content-Type: application/x-www-form-urlencoded
// grant_type=authorization_code&code={code}&redirect_uri={redirectURI}&client_id={clientID}&code_verifier={verifier}
func ExchangeCodeForTokens(tokenURL, clientID, code, redirectURI, verifier string) (*StoredTokens, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)

	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token exchange response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token: %s", string(body))
	}

	stored := &StoredTokens{
		AccessToken: tokenResp.AccessToken,
		Scope:       tokenResp.Scope,
	}
	if tokenResp.RefreshToken != "" {
		stored.RefreshToken = tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		stored.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return stored, nil
}

// RefreshTokens uses a refresh token to get new access tokens.
// POST {TokenURL}
// grant_type=refresh_token&refresh_token={refreshToken}&client_id={clientID}
func RefreshTokens(tokenURL, clientID, refreshToken string) (*StoredTokens, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh token is empty")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)

	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token refresh response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token refresh response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token refresh response missing access_token: %s", string(body))
	}

	stored := &StoredTokens{
		AccessToken: tokenResp.AccessToken,
		Scope:       tokenResp.Scope,
	}
	if tokenResp.RefreshToken != "" {
		stored.RefreshToken = tokenResp.RefreshToken
	} else {
		// Preserve the existing refresh token if the server didn't issue a new one
		stored.RefreshToken = refreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		stored.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return stored, nil
}

// AuthenticateAuthCodeFlow performs OAuth authorization code flow with PKCE.
// Designed for headless environments:
//  1. Generates PKCE params
//  2. Prints authorization URL + instructions to stderr
//  3. Reads redirect URL from stdin (user paste)
//  4. Extracts auth code from the URL
//  5. Exchanges code for tokens
//  6. Saves tokens to ~/.diane/secrets/<name>.json
//
// On macOS, also calls exec.Command("open", url) to open the browser automatically.
// The redirect_uri is derived from the redirect URL that the user pastes.
func AuthenticateAuthCodeFlow(serverName string, oauth *OAuthConfig) (string, error) {
	if oauth == nil {
		return "", fmt.Errorf("OAuthConfig is nil")
	}
	if oauth.ClientID == "" {
		return "", fmt.Errorf("OAuthConfig.ClientID is required")
	}
	if oauth.AuthorizationURL == "" {
		return "", fmt.Errorf("OAuthConfig.AuthorizationURL is required")
	}
	if oauth.TokenURL == "" {
		return "", fmt.Errorf("OAuthConfig.TokenURL is required")
	}

	// Step 1: Generate PKCE parameters
	verifier := GenerateCodeVerifier()
	challenge := GenerateCodeChallenge(verifier)

	// Build authorization URL
	authURL, err := url.Parse(oauth.AuthorizationURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse authorization URL: %w", err)
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", oauth.ClientID)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if len(oauth.Scopes) > 0 {
		q.Set("scope", strings.Join(oauth.Scopes, " "))
	}
	authURL.RawQuery = q.Encode()

	// Step 2: Print the prompt to stderr (so it doesn't interfere with stdout output)
	prompt := fmt.Sprintf(`
╔══════════════════════════════════════════════════════╗
║         MCP Authentication Required                 ║
║  Server: %-39s║
║                                                     ║
║  1. Open this URL in any browser:                    ║
║     %s
║                                                     ║
║  2. Authorize the application                        ║
║                                                     ║
║  3. After redirect, paste the full URL here:         ║
║     (starting with http://localhost:...)              ║
╚══════════════════════════════════════════════════════╝
`, serverName, authURL.String())

	fmt.Fprint(os.Stderr, prompt)

	// On macOS, try to open the browser automatically
	if runtime.GOOS == "darwin" {
		exec.Command("open", authURL.String()).Start()
	}

	// Step 3: Read redirect URL from stdin
	fmt.Fprint(os.Stderr, "Paste redirect URL: ")
	reader := bufio.NewReader(os.Stdin)
	redirectURL, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read redirect URL from stdin: %w", err)
	}
	redirectURL = strings.TrimSpace(redirectURL)

	// Step 4: Extract auth code
	code, err := ExtractAuthCodeFromRedirectURL(redirectURL)
	if err != nil {
		return "", fmt.Errorf("failed to extract auth code from redirect URL: %w", err)
	}

	// Derive redirect_uri from the pasted URL (strip the query params)
	parsedRedirect, err := url.Parse(redirectURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse redirect URL: %w", err)
	}
	// Reconstruct the base redirect URI (scheme + host + path, no query)
	redirectURI := (&url.URL{
		Scheme: parsedRedirect.Scheme,
		Host:   parsedRedirect.Host,
		Path:   parsedRedirect.Path,
	}).String()

	// Step 5: Exchange code for tokens
	stored, err := ExchangeCodeForTokens(oauth.TokenURL, oauth.ClientID, code, redirectURI, verifier)
	if err != nil {
		return "", fmt.Errorf("token exchange failed: %w", err)
	}

	// Step 6: Save tokens
	if err := SaveTokens(serverName, stored); err != nil {
		log.Printf("Warning: failed to save OAuth tokens for %s: %v", serverName, err)
	}

	fmt.Fprintf(os.Stderr, "✅ OAuth authorization complete for %s\n", serverName)
	return stored.AccessToken, nil
}
