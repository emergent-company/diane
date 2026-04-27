package mcpproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// HTTPMCPClient implements the Client interface for Streamable HTTP MCP servers.
// It sends JSON-RPC messages as HTTP POST requests to a configured URL.
type HTTPMCPClient struct {
	Name    string
	URL     string
	Headers map[string]string // Static HTTP headers (e.g., Authorization, X-API-Key)
	Token   string            // OAuth bearer token (set after authentication)
	OAuth   *OAuthConfig      // OAuth config for auto-authentication on 401
	client  *http.Client
	mu      sync.Mutex
	nextID  int
}

// Compile-time check that *HTTPMCPClient implements Client.
var _ Client = (*HTTPMCPClient)(nil)

// NewHTTPMCPClient creates a new HTTP MCP client and initializes the connection.
// It sends an initialize request to verify the server is reachable and speaks MCP.
func NewHTTPMCPClient(name string, url string, headers map[string]string, oauth *OAuthConfig) (*HTTPMCPClient, error) {
	c := &HTTPMCPClient{
		Name:    name,
		URL:     url,
		Headers: headers,
		OAuth:   oauth,
		client:  &http.Client{},
		nextID:  0,
	}

	// Initialize the MCP connection (verify server reachable and compatible)
	if err := c.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize HTTP MCP server %s: %w", name, err)
	}

	log.Printf("[%s] Connected to HTTP MCP server at %s", name, url)
	return c, nil
}

// initialize sends an initialize request to verify the HTTP MCP server.
func (c *HTTPMCPClient) initialize() error {
	params := json.RawMessage(`{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"diane","version":"1.0.0"}}`)
	_, err := c.sendRequest("initialize", params)
	return err
}

// sendRequest sends a JSON-RPC request to the HTTP MCP server and returns the result.
// If the server returns 401 and OAuth config is available, it auto-authenticates and retries.
func (c *HTTPMCPClient) sendRequest(method string, params json.RawMessage) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	reqID := c.nextID
	c.mu.Unlock()

	// Ensure we have a valid token (try loading stored tokens, refresh if needed)
	if err := c.ensureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication check failed for %s: %w", method, err)
	}

	reqBody := MCPRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s request: %w", method, err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, c.URL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Add static headers (e.g., Authorization, X-API-Key)
	for k, v := range c.Headers {
		httpReq.Header.Set(k, v)
	}

	// Add OAuth bearer token if set
	if c.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send %s: %w", method, err)
	}
	defer resp.Body.Close()

	// Handle 401 — trigger OAuth authentication if configured
	if resp.StatusCode == http.StatusUnauthorized {
		if c.OAuth == nil {
			return nil, fmt.Errorf("%s unauthorized (401): check authentication credentials", method)
		}

		// Run OAuth flow (device or auth code) interactively
		if err := c.reauthenticate(); err != nil {
			return nil, fmt.Errorf("%s unauthorized (401): %w", method, err)
		}

		// Save the new token
		_ = SaveTokens(c.Name, &StoredTokens{AccessToken: c.Token})

		// Retry the original request with the new token
		return c.sendRequest(method, params)
	}

	// Handle other HTTP-level errors
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%s forbidden (403): insufficient permissions", method)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s HTTP error %d: %s", method, resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s response: %w", method, err)
	}

	var mcpResp MCPResponse
	if err := json.Unmarshal(body, &mcpResp); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", method, err)
	}

	if mcpResp.Error != nil {
		return nil, fmt.Errorf("%s error: %s", method, mcpResp.Error.Message)
	}

	return mcpResp.Result, nil
}

// ListTools requests the list of tools from the HTTP MCP server.
func (c *HTTPMCPClient) ListTools() ([]map[string]interface{}, error) {
	result, err := c.sendRequest("tools/list", nil)
	if err != nil {
		return nil, err
	}

	var toolsResult struct {
		Tools []map[string]interface{} `json:"tools"`
	}
	if err := json.Unmarshal(result, &toolsResult); err != nil {
		return nil, fmt.Errorf("failed to parse tools: %w", err)
	}

	return toolsResult.Tools, nil
}

// CallTool calls a tool on the HTTP MCP server.
func (c *HTTPMCPClient) CallTool(toolName string, arguments map[string]interface{}) (json.RawMessage, error) {
	params, err := json.Marshal(map[string]interface{}{
		"name":      toolName,
		"arguments": arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	return c.sendRequest("tools/call", params)
}

// NotificationChan returns nil since HTTP MCP is stateless and does not support server-sent notifications.
func (c *HTTPMCPClient) NotificationChan() <-chan string {
	return nil
}

// ensureAuthenticated checks for stored tokens or runs OAuth flow on 401.
// Returns the token to use, or empty string if no auth is needed.
func (c *HTTPMCPClient) ensureAuthenticated() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Token != "" {
		return nil
	}
	if c.OAuth == nil {
		return nil // no auth configured
	}

	// Try loading stored tokens
	tokens, err := LoadTokens(c.Name)
	if err == nil && tokens.AccessToken != "" {
		// Check expiry
		if !tokens.ExpiresAt.IsZero() && time.Now().After(tokens.ExpiresAt) {
			// Expired — try to refresh
			if tokens.RefreshToken != "" && c.OAuth.TokenURL != "" {
				newTokens, refreshErr := RefreshTokens(c.OAuth.TokenURL, c.OAuth.ClientID, tokens.RefreshToken)
				if refreshErr == nil {
					c.Token = newTokens.AccessToken
					// Save the refreshed tokens
					_ = SaveTokens(c.Name, newTokens)
					return nil
				}
			}
			// Can't refresh, will need to re-authenticate on 401
			return nil
		}
		// Token is valid
		c.Token = tokens.AccessToken
		return nil
	}

	return nil // no stored token, will trigger reauth on 401
}

// reauthenticate runs the OAuth flow (device or auth code) interactively.
// Sets c.Token on success.
func (c *HTTPMCPClient) reauthenticate() error {
	if c.OAuth == nil {
		return fmt.Errorf("no OAuth configuration for %s", c.Name)
	}

	var token string
	var err error

	if c.OAuth.DeviceAuthURL != "" {
		// Device flow (GitHub Copilot) — print URL+code, poll
		token, err = AuthenticateDeviceFlow(c.Name, c.OAuth)
	} else if c.OAuth.AuthorizationURL != "" {
		// Auth code flow (infakt) — print URL, read paste
		token, err = AuthenticateAuthCodeFlow(c.Name, c.OAuth)
	} else {
		return fmt.Errorf("no OAuth flow configured for %s (need device_auth_url or authorization_url)", c.Name)
	}

	if err != nil {
		return fmt.Errorf("authentication failed for %s: %w", c.Name, err)
	}

	c.Token = token
	return nil
}

// SetToken sets the OAuth bearer token for this client.
// The token will be sent as an Authorization: Bearer header on all requests.
func (c *HTTPMCPClient) SetToken(token string) {
	c.Token = token
}

// Close is a no-op for HTTP clients (no subprocess to kill).
func (c *HTTPMCPClient) Close() error {
	return nil
}
