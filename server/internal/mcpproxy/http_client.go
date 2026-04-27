package mcpproxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	timeout time.Duration     // Per-request HTTP timeout
	client  *http.Client
	mu      sync.Mutex
	nextID  int
}

// Compile-time check that *HTTPMCPClient implements Client.
var _ Client = (*HTTPMCPClient)(nil)

// NewHTTPMCPClient creates a new HTTP MCP client and initializes the connection.
// It sends an initialize request to verify the server is reachable and speaks MCP.
func NewHTTPMCPClient(name string, url string, headers map[string]string, oauth *OAuthConfig, timeoutSec int) (*HTTPMCPClient, error) {
	if timeoutSec <= 0 {
		timeoutSec = DefaultToolTimeout
	}
	c := &HTTPMCPClient{
		Name:    name,
		URL:     url,
		Headers: headers,
		OAuth:   oauth,
		timeout: time.Duration(timeoutSec) * time.Second,
		client:  &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
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
			// No OAuth config — try to discover from www-authenticate header
			discovered, discErr := c.discoverOAuthFromHeader(resp.Header)
			if discErr == nil && discovered != nil {
				c.OAuth = discovered
				_ = SaveDiscoveredConfig(c.Name, discovered)
				log.Printf("[OAuth] Discovered and saved OAuth config for %s. Run 'diane mcp auth --server %s' to authenticate.",
					c.Name, c.Name)
			}
			// Whether discovery succeeded or not, return a clean error
			// — don't attempt interactive auth here (blocks on stdin in relay context)
			return nil, fmt.Errorf("%s unauthorized (401): run 'diane mcp auth --server %s' to authenticate",
				method, c.Name)
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

// WWWAuthenticate holds the parsed structure of a WWW-Authenticate header
type WWWAuthenticate struct {
	Scheme           string
	Error            string
	ErrorDescription string
	ResourceMetadata string // URL for OAuth protected resource metadata
}

// discoverOAuthFromHeader parses the WWW-Authenticate header from a 401 response.
// It follows the OAuth metadata discovery chain:
// 1. Parse www-authenticate header → get resource_metadata URL
// 2. Fetch resource_metadata → get authorization_servers
// 3. Fetch auth server's .well-known/oauth-authorization-server → get endpoints
func (c *HTTPMCPClient) discoverOAuthFromHeader(headers http.Header) (*OAuthConfig, error) {
	authHeader := headers.Get("www-authenticate")
	if authHeader == "" {
		return nil, fmt.Errorf("no www-authenticate header in 401 response")
	}

	// Parse the header value
	wwwAuth := parseWWWAuthenticate(authHeader)
	if wwwAuth.ResourceMetadata == "" {
		return nil, fmt.Errorf("no resource_metadata in www-authenticate header")
	}

	// Fetch the resource metadata to get the authorization server URL
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(wwwAuth.ResourceMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OAuth resource metadata from %s: %w", wwwAuth.ResourceMetadata, err)
	}
	defer resp.Body.Close()

	var metadata struct {
		Resource            string   `json:"resource"`
		AuthorizationServer []string `json:"authorization_servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to parse resource metadata: %w", err)
	}
	if len(metadata.AuthorizationServer) == 0 {
		return nil, fmt.Errorf("no authorization_servers in resource metadata")
	}

	authServer := metadata.AuthorizationServer[0]
	if !strings.HasPrefix(authServer, "http") {
		return nil, fmt.Errorf("invalid authorization server URL: %s", authServer)
	}

	// Fetch the OAuth authorization server metadata
	wellKnownURL := strings.TrimRight(authServer, "/") + "/.well-known/oauth-authorization-server"
	resp2, err := client.Get(wellKnownURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OAuth server metadata from %s: %w", wellKnownURL, err)
	}
	defer resp2.Body.Close()

	var oauthMeta struct {
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
		TokenEndpoint        string   `json:"token_endpoint"`
		RegistrationEndpoint *string  `json:"registration_endpoint,omitempty"`
		ScopesSupported      []string `json:"scopes_supported,omitempty"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&oauthMeta); err != nil {
		return nil, fmt.Errorf("failed to parse OAuth server metadata: %w", err)
	}
	if oauthMeta.AuthorizationEndpoint == "" || oauthMeta.TokenEndpoint == "" {
		return nil, fmt.Errorf("incomplete OAuth server metadata: missing authorization or token endpoint")
	}

	log.Printf("[OAuth] Discovered OAuth endpoints for %s: authorize=%s token=%s",
		c.Name, oauthMeta.AuthorizationEndpoint, oauthMeta.TokenEndpoint)

	cfg := &OAuthConfig{
		AuthorizationURL: oauthMeta.AuthorizationEndpoint,
		TokenURL:         oauthMeta.TokenEndpoint,
		Scopes:           oauthMeta.ScopesSupported,
	}
	if oauthMeta.RegistrationEndpoint != nil {
		cfg.RegistrationURL = *oauthMeta.RegistrationEndpoint
	}

	return cfg, nil
}

// parseWWWAuthenticate parses a WWW-Authenticate header value into its components.
// Supports: Bearer scheme with key=value parameters (comma-separated, quoted values).
func parseWWWAuthenticate(header string) WWWAuthenticate {
	result := WWWAuthenticate{}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) > 0 {
		result.Scheme = parts[0]
	}
	if len(parts) < 2 {
		return result
	}

	// Parse key="value" pairs
	rest := parts[1]
	scanner := bufio.NewScanner(strings.NewReader(rest))
	scanner.Split(bufio.ScanWords)
	for scanner.Scan() {
		token := scanner.Text()
		eqIdx := strings.IndexByte(token, '=')
		if eqIdx < 0 {
			continue
		}
		key := token[:eqIdx]
		val := strings.Trim(token[eqIdx+1:], "\",")
		switch key {
		case "error":
			result.Error = val
		case "error_description":
			result.ErrorDescription = val
		case "resource_metadata":
			result.ResourceMetadata = strings.Trim(val, "\"")
		}
	}
	return result
}

// SaveDiscoveredConfig saves an auto-discovered OAuth config for a server.
// This allows subsequent runs to use the discovered config without re-discovery.
func SaveDiscoveredConfig(serverName string, config *OAuthConfig) error {
	path := TokenPath(serverName + "-oauth-config")
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// LoadDiscoveredConfig loads a previously auto-discovered OAuth config for a server.
// Returns nil if no discovered config exists.
func LoadDiscoveredConfig(serverName string) *OAuthConfig {
	path := TokenPath(serverName + "-oauth-config")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg OAuthConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// ensureAuthenticated checks for stored tokens or runs OAuth flow on 401.
// Always checks for stored tokens first, regardless of whether OAuth is
// configured in the server config — allows auto-discovered OAuth tokens
// (e.g., infakt) to work on subsequent connections.
func (c *HTTPMCPClient) ensureAuthenticated() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Token != "" {
		return nil
	}

	// Try loading stored tokens
	tokens, err := LoadTokens(c.Name)
	if err == nil && tokens.AccessToken != "" {
		// Check expiry
		if !tokens.ExpiresAt.IsZero() && time.Now().After(tokens.ExpiresAt) {
			// Expired — try to refresh
			oauth := c.OAuth
			if oauth == nil || oauth.TokenURL == "" {
				// Try loading discovered OAuth config for refresh
				oauth = LoadDiscoveredConfig(c.Name)
			}
			if oauth != nil && tokens.RefreshToken != "" && oauth.TokenURL != "" {
				clientID := oauth.ClientID
				newTokens, refreshErr := RefreshTokens(oauth.TokenURL, clientID, tokens.RefreshToken)
				if refreshErr == nil {
					c.Token = newTokens.AccessToken
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

	// No stored token — try to load discovered OAuth config for later use
	if c.OAuth == nil {
		discovered := LoadDiscoveredConfig(c.Name)
		if discovered != nil {
			c.OAuth = discovered
		}
	}

	return nil // will trigger reauth on 401
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
