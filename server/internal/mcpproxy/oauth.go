package mcpproxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
