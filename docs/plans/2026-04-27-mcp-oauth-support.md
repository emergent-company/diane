# MCP OAuth & HTTP/SSE Transport Support for Diane

> **For Hermes:** Use subagent-driven-development to implement this plan task-by-task.

**Goal:** Enable Diane to connect to HTTP/SSE-based MCP servers that require OAuth authentication (like infakt, GitHub Copilot MCP, etc.)

**Architecture:** Extend the existing `mcpproxy` package with HTTP transport clients, OAuth flow handlers, and token storage. Servers with `type: "http"`, `"sse"`, or `"streamable-http"` will be started alongside stdio servers by the proxy, with OAuth handled transparently on first connection.

**Headless-first design:** Diane runs on both browser-equipped (macOS slaves) and headless machines (diane-lxc on Linux). OAuth flows **never assume a browser is available**. Instead:
- **Device flow** (GitHub Copilot): Print URL + user code, user visits from any machine → native headless support
- **Auth code + PKCE flow** (infakt): Diane prints the authorization URL with clear instructions. User opens URL on their desktop, authorizes, then **pastes the redirect URL back into the Diane CLI**. Diane extracts the code and completes the exchange.
- On macOS: Diane also offers to `open` the URL automatically as a convenience.

**Tech Stack:** Go standard library (`net/http`, `crypto/sha256`, `encoding/base64`), existing `mcpproxy` patterns, file-based token storage in `~/.diane/secrets/`

---

## Current State

- `ServerConfig` only has: `Name`, `Enabled`, `Type`, `Command`, `Args`, `Env`
- `NewProxy` only starts servers with `Type == "stdio"`
- HTTP/SSE servers are parsed but silently skipped
- No OAuth code exists anywhere in Diane
- `mcp-servers.json` on prod already has servers needing this:
  - `github` (type=http, device OAuth flow)
  - `emergent` (type=sse, static headers)
  - `infakt` (target: streamable-http, PKCE OAuth flow)

---

## Tasks

### Task 1: Extend ServerConfig with URL, Headers, and OAuthConfig

**Objective:** Add the fields needed for HTTP/SSE MCP servers and OAuth configuration to the existing ServerConfig struct.

**Files:**
- Modify: `server/internal/mcpproxy/proxy.go` (struct definitions)
- Modify: `server/internal/mcpproxy/proxy_test.go` (tests for new fields)

**Step 1: Add OAuthConfig struct and extend ServerConfig**

```go
// In proxy.go, add before ServerConfig:

// OAuthConfig holds OAuth 2.0 configuration for MCP server authentication.
type OAuthConfig struct {
    // Device flow (GitHub Copilot style)
    ClientID       string   `json:"client_id,omitempty"`
    DeviceAuthURL  string   `json:"device_auth_url,omitempty"`
    TokenURL       string   `json:"token_url,omitempty"`
    Scopes         []string `json:"scopes,omitempty"`

    // Authorization code flow (infakt style)
    AuthorizationURL string `json:"authorization_url,omitempty"`

    // Static bearer token (pre-authenticated)
    BearerToken string `json:"bearer_token,omitempty"`
}

// Extend ServerConfig:
type ServerConfig struct {
    Name    string            `json:"name"`
    Enabled bool              `json:"enabled"`
    Type    string            `json:"type"` // stdio, http, sse, streamable-http
    Command string            `json:"command"`
    Args    []string          `json:"args"`
    Env     map[string]string `json:"env"`
    URL     string            `json:"url,omitempty"`     // HTTP/SSE endpoint
    Headers map[string]string `json:"headers,omitempty"` // Static HTTP headers
    OAuth   *OAuthConfig      `json:"oauth,omitempty"`   // OAuth configuration
}
```

**Step 2: Write tests for new config fields**

```go
func TestLoadConfig_WithURLAndHeaders(t *testing.T) {
    // Parse config with url, headers, and verify they're preserved
}

func TestLoadConfig_WithOAuthDeviceFlow(t *testing.T) {
    // Parse config matching the existing github entry in mcp-servers.json
}

func TestLoadConfig_WithOAuthPKCE(t *testing.T) {
    // Parse config matching infakt's requirements
}
```

**Step 3: Build and test**

```bash
cd /root/diane/server && go build ./internal/mcpproxy/
cd /root/diane/server && go test ./internal/mcpproxy/ -run "TestLoadConfig" -v -count=1
```

Expected: All 8+ config parsing tests pass.

**Step 4: Commit**

```bash
git add server/internal/mcpproxy/proxy.go server/internal/mcpproxy/proxy_test.go
git commit -m "feat(mcpproxy): extend ServerConfig with URL, Headers, OAuthConfig"
```

---

### Task 2: Create HTTP MCP Client (Streamable HTTP Transport)

**Objective:** Implement an MCP client that connects to HTTP/Streamable HTTP MCP servers, implementing the same interface as the stdio MCPClient so the proxy can use both interchangeably.

**Files:**
- Create: `server/internal/mcpproxy/http_client.go`
- Modify: `server/internal/mcpproxy/proxy.go` (interface extraction if needed)

**Step 1: Design the HTTP MCP client**

The Streamable HTTP transport sends MCP JSON-RPC messages as HTTP POST requests. Key pattern:

```go
// Initialize: POST /mcp with initialize request
// tools/list: POST /mcp with tools/list request
// tools/call: POST /mcp with tools/call request

type HTTPMCPClient struct {
    Name    string
    URL     string
    Headers map[string]string
    client  *http.Client
}
```

**Step 2: Write tests first**

```go
// Test with an echo-style HTTP server or a mock
```

**Step 3: Implement HTTPMCPClient**

Key methods:
- `NewHTTPMCPClient(name, url, headers)` — creates client
- `Initialize()` — sends initialize via HTTP POST, checks response
- `ListTools()` — sends tools/list via HTTP POST, parses result
- `CallTool(name, args)` — sends tools/call via HTTP POST
- `Close()` — no-op for HTTP (no subprocess)
- `NotificationChan()` — returns nil channel (HTTP is stateless)

The MCP Streamable HTTP transport sends:
```json
POST /mcp HTTP/1.1
Content-Type: application/json

{"jsonrpc":"2.0","id":1,"method":"initialize","params":{...}}
```

Response:
```json
{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{...}}}
```

**Step 4: Integration with proxy**

Modify `NewProxy` to start HTTP/SSE clients alongside stdio:

```go
for _, server := range config.Servers {
    if !server.Enabled {
        continue
    }
    switch server.Type {
    case "stdio":
        // existing code
    case "http", "streamable-http":
        client, err := NewHTTPMCPClient(server.Name, server.URL, server.Headers)
        if err != nil {
            log.Printf("Failed to connect to %s: %v", server.Name, err)
            continue
        }
        p.clients[server.Name] = client
    }
}
```

**Step 5: Build and test**

```bash
cd /root/diane/server && go build ./internal/mcpproxy/
cd /root/diane/server && go test ./internal/mcpproxy/ -v -count=1
```

**Step 6: Commit**

```bash
git add server/internal/mcpproxy/http_client.go server/internal/mcpproxy/proxy.go
git commit -m "feat(mcpproxy): add HTTP Streamable MCP client and proxy integration"
```

---

### Task 3: Implement OAuth Device Authorization Flow

**Objective:** Support devices that use OAuth device authorization flow (used by GitHub Copilot MCP). The flow is CLI-friendly — no browser redirect needed.

**Files:**
- Create: `server/internal/mcpproxy/oauth.go`

**Step 1: Implement device flow**

The OAuth device flow:
1. POST to `device_auth_url` with `client_id` → get `device_code`, `user_code`, `verification_uri`
2. Display `verification_uri` and `user_code` to user (print to stderr)
3. Poll `token_url` with `device_code` and `grant_type=urn:ietf:params:oauth:grant-type:device_code`
4. On `authorization_pending` — wait and retry
5. On success — get `access_token` (and optionally `refresh_token`)

```go
type DeviceAuthResponse struct {
    DeviceCode      string `json:"device_code"`
    UserCode        string `json:"user_code"`
    VerificationURI string `json:"verification_uri"`
    Interval        int    `json:"interval"`
}

type TokenResponse struct {
    AccessToken  string `json:"access_token"`
    RefreshToken string `json:"refresh_token,omitempty"`
    ExpiresIn    int    `json:"expires_in,omitempty"`
    Scope        string `json:"scope,omitempty"`
}

func (c *HTTPMCPClient) AuthenticateDeviceFlow(ctx context.Context, oauth *OAuthConfig) error {
    // 1. Request device code
    // 2. Print user_code + verification_uri to stderr
    // 3. Poll for token
    // 4. Store token in ~/.diane/secrets/<name>-oauth.json
    // 5. Add Authorization: Bearer <token> header
}
```

**Step 2: Write tests**

```go
func TestParseDeviceAuthResponse(t *testing.T) {
    // Test JSON parsing of device auth responses
}

func TestParseTokenResponse(t *testing.T) {
    // Test JSON parsing of token responses
}
```

**Step 3: Add token storage**

```go
// TokenStorage manages OAuth tokens on disk
type TokenStorage struct {
    ServerName string
}

func (s *TokenStorage) Load() (*StoredTokens, error) {
    // Read from ~/.diane/secrets/<name>-oauth.json
}

func (s *TokenStorage) Save(tokens *StoredTokens) error {
    // Write to ~/.diane/secrets/<name>-oauth.json with 0600 permissions
}
```

**Step 4: Commit**

```bash
git add server/internal/mcpproxy/oauth.go
git commit -m "feat(mcpproxy): implement OAuth device authorization flow"
```

---

### Task 4: Implement OAuth Authorization Code + PKCE Flow

**Objective:** Support OAuth authorization code flow with PKCE (used by infakt). Must work on **both** browser-equipped machines (macOS) and headless servers (Diane LXC on zoidberg2).

**Design Decision — Headless-first:**
Diane runs on headless servers (diane-lxc on zoidberg2). The auth code flow must work without a browser:

1. Diane generates PKCE params, prints the authorization URL + instructions
2. User copies the URL, opens on their desktop browser (any machine)
3. After authorizing, the browser redirects to `http://localhost:PORT/callback?code=...`
4. User copies the **full redirect URL** and pastes it into the Diane CLI
5. Diane extracts the authorization code, exchanges for tokens
6. Tokens saved, ready to use

On browser-equipped machines (macOS), Diane also offers to `open` the URL automatically.

**Files:**
- Modify: `server/internal/mcpproxy/oauth.go` (add PKCE flow)

**Step 1: Implement PKCE helper functions**

```go
// Generate PKCE code verifier (random 43-128 char string of unreserved chars)
func generateCodeVerifier() string

// Generate PKCE code challenge (SHA256 base64url-encoded verifier, no padding)
func generateCodeChallenge(verifier string) string
```

**Step 2: Implement the interactive auth code flow**

```go
func (c *HTTPMCPClient) AuthenticateAuthCodeFlow(ctx context.Context, oauth *OAuthConfig) error {
    // 1. Generate PKCE verifier + challenge
    // 2. Build authorization URL:
    //    authorization_endpoint?
    //      response_type=code&
    //      client_id=<dynamic or "diane">&
    //      redirect_uri=http://localhost:PORT/callback&
    //      code_challenge=<challenge>&
    //      code_challenge_method=S256&
    //      state=<random>&
    //      scope=<scopes>
    // 3. Print to stderr:
    //
    //    ╔══════════════════════════════════════════════════════╗
    //    ║         MCP Authentication Required                 ║
    //    ║                                                    ║
    //    ║  Server: infakt                                     ║
    //    ║                                                    ║
    //    ║  1. Open this URL in any browser:                   ║
    //    ║     https://mcp.infakt.pl/authorize?...             ║
    //    ║                                                    ║
    //    ║  2. Authorize the application                       ║
    //    ║                                                    ║
    //    ║  3. After redirect, paste the full URL here:        ║
    //    ║     (the one starting with http://localhost:...)     ║
    //    ║                                                    ║
    //    ╚══════════════════════════════════════════════════════╝
    //
    // 4. If browser available (macOS), also run: open <url>
    // 5. Read redirect URL from stdin (user paste)
    // 6. Parse authorization code from query params
    // 7. POST to token_endpoint:
    //    grant_type=authorization_code&
    //    code=<code>&
    //    redirect_uri=http://localhost:PORT/callback&
    //    client_id=<id>&
    //    code_verifier=<verifier>
    // 8. Store access_token + refresh_token + expires_at
    // 9. Add Authorization: Bearer <token> header
}
```

**Step 3: Write tests**

```go
func TestGeneratePKCEVerifier(t *testing.T) {
    // Verify 43-128 chars, all unreserved URL chars
}

func TestGeneratePKCEChallenge(t *testing.T) {
    // Verify SHA256 base64url encoding without padding
}

func TestExtractAuthCodeFromURL(t *testing.T) {
    // Parse "http://localhost:3456/callback?code=abc123&state=xyz"
    // Should return "abc123"
}

func TestExchangeCodeForToken(t *testing.T) {
    // Mock HTTP server for token endpoint
    // Verify correct POST body + header
}
```

**Step 4: Commit**

```bash
git add server/internal/mcpproxy/oauth.go
git commit -m "feat(mcpproxy): implement OAuth authorization code + PKCE flow"
```

---

### Task 5: Auto-Trigger OAuth on 401 Response

**Objective:** When an HTTP MCP server returns 401 (unauthorized), automatically trigger the OAuth flow, store the token, and retry the request.

**Files:**
- Modify: `server/internal/mcpproxy/http_client.go` (add 401 handling)
- Modify: `server/internal/mcpproxy/oauth.go` (integration hook)

**Step 1: Detect 401 and trigger OAuth**

When `ListTools()` or `CallTool()` gets a 401 response:
1. Check if server has OAuth config
2. If device flow: call `AuthenticateDeviceFlow()`
3. If auth code flow: call `AuthenticateAuthCodeFlow()`
4. Retry the original request
5. If still 401: return error "authentication failed"

```go
func (c *HTTPMCPClient) sendRequest(method string, params json.RawMessage) (json.RawMessage, error) {
    resp, err := c.doRequest(method, params)
    if err != nil {
        return nil, err
    }

    // If 401 and we have OAuth config, try to authenticate
    if resp.StatusCode == http.StatusUnauthorized && c.oauth != nil {
        if err := c.authenticate(context.Background()); err != nil {
            return nil, fmt.Errorf("authentication failed: %w", err)
        }
        // Retry with auth header
        return c.doRequest(method, params)
    }

    return parseResponse(resp)
}
```

**Step 2: Token refresh on expiry**

If a request returns 401 after successful auth (token expired):
1. Check if refresh_token is available
2. POST to token endpoint with `grant_type=refresh_token` and `refresh_token`
3. Save new tokens
4. Retry request

**Step 3: Commit**

```bash
git add server/internal/mcpproxy/http_client.go server/internal/mcpproxy/oauth.go
git commit -m "feat(mcpproxy): auto-trigger OAuth on 401 with token refresh"
```

---

### Task 6: Add `diane mcp auth` CLI Command

**Objective:** Provide a CLI trigger for the OAuth flow so users can authenticate MCP servers interactively.

**Files:**
- Create: `server/cmd/diane/mcp_auth.go`
- Modify: `server/cmd/diane/main.go` (register command)

**Step 1: Implement the command**

```go
func cmdMCPAuth(args []string) {
    // Parse --server flag (required)
    // Load mcp-servers.json
    // Find the server config
    // If OAuth is configured:
    //   - For device flow: print URL, wait for completion
    //   - For auth code flow: open browser, wait for redirect
    // Save token on success
    // Print "✅ Authenticated as <name>"
}
```

**Step 2: Wire into main.go and design headless UX**

The `--server` flag is the server name from mcp-servers.json. The command:

1. Loads the server config
2. Detects the OAuth type (device vs auth code)
3. Device flow: prints URL + code, polls for token
4. Auth code flow: prints authorization URL + instructions, reads pasted redirect URL from stdin
5. On macOS with browser: offers to `open` the URL automatically
6. Saves the token to `~/.diane/secrets/<name>-oauth.json`
7. Prints success confirmation

```go
// In main.go command dispatch:
case "auth":
    cmdMCPAuth(args)
```

**Step 3: Test manually**

```bash
# Build
cd /root/diane/server && go build -o /tmp/diane ./cmd/diane/

# Test help
/tmp/diane mcp auth --help
# Expected: shows usage for --server flag
```

**Step 4: Commit**

```bash
git add server/cmd/diane/mcp_auth.go server/cmd/diane/main.go
git commit -m "feat(cli): add diane mcp auth command for OAuth flow"
```

---

### Task 7: Update `mcp list` for HTTP Servers

**Objective:** Extend the existing `diane mcp list` command to show HTTP/SSE server tools.

**Files:**
- Modify: `server/cmd/diane/mcp_list.go` (already uncommitted — update it)
- Modify: `server/cmd/diane/mcp_list_test.go` (tests)

**Step 1: Connect to HTTP servers in collectTools**

The `collectTools` function currently skips non-stdio servers. Update it to also try connecting to HTTP/SSE servers:

```go
// In collectTools, after trying proxy:
for _, s := range cfg.Servers {
    if s.Enabled && (s.Type == "http" || s.Type == "streamable-http") {
        // Try direct HTTP connection to list tools
        client := NewHTTPMCPClient(s.Name, s.URL, s.Headers)
        tools, err := client.ListTools()
        if err == nil {
            result[s.Name] = tools
        }
    }
}
```

**Step 2: Commit**

```bash
git add server/cmd/diane/mcp_list.go server/cmd/diane/mcp_list_test.go
git commit -m "feat(cli): extend mcp list to show HTTP server tools"
```

---

### Task 8: End-to-End Integration Test

**Objective:** Write an integration test that starts an HTTP MCP test server (with OAuth) and verifies the full flow.

**Files:**
- Add to: `server/memorytest/agent_mcp_tools_test.go`

**Step 1: Add a test HTTP MCP server**

```go
// In test setup, start a local HTTP server that:
// 1. Implements the MCP Streamable HTTP protocol
// 2. Returns 401 on first request (simulating OAuth)
// 3. Accepts Authorization header on subsequent requests
// 4. Provides echo_text + add_numbers tools
```

**Step 2: Test the full flow**

```go
func TestHTTPMCPClientAuth(t *testing.T) {
    // Start test HTTP MCP server
    // Create HTTPMCPClient with OAuth config
    // Call ListTools()
    // Verify: 401 -> authenticates -> retries -> succeeds
    // Verify: tools include "echo_text" and "add_numbers"
}
```

**Step 3: Commit**

```bash
git add server/memorytest/agent_mcp_tools_test.go
git commit -m "test: add HTTP MCP client integration test with OAuth"
```

---

## Config Format — Before & After

### Before (current — HTTP servers silently skipped)
```json
{
  "servers": [
    {
      "name": "infakt",
      "enabled": true,
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@infakt/mcp-bridge"]
    }
  ]
}
```

### After (working)
```json
{
  "servers": [
    {
      "name": "infakt",
      "enabled": true,
      "type": "streamable-http",
      "url": "https://mcp.infakt.pl/mcp",
      "oauth": {
        "authorization_url": "https://mcp.infakt.pl/authorize",
        "token_url": "https://mcp.infakt.pl/token"
      }
    },
    {
      "name": "github",
      "enabled": true,
      "type": "http",
      "url": "https://api.githubcopilot.com/mcp/",
      "oauth": {
        "client_id": "Ov23liWQjrRGUIT0yY1M",
        "device_auth_url": "https://github.com/login/device/code",
        "token_url": "https://github.com/login/oauth/access_token",
        "scopes": ["repo", "read:user"]
      }
    },
    {
      "name": "emergent",
      "enabled": true,
      "type": "sse",
      "url": "http://localhost:3002/api/mcp/sse/...",
      "headers": {
        "X-API-Key": "***"
      }
    }
  ]
}
```

---

## Token Storage

Tokens stored at `~/.diane/secrets/<server-name>-oauth.json`:

```json
{
  "access_token": "ya29...",
  "refresh_token": "1//0g...",
  "expires_at": "2026-04-28T07:00:00Z",
  "scope": "public api:invoices:read"
}
```

Updated automatically on token refresh. 0600 permissions.

---

## Verification

After implementation:

1. `go test ./internal/mcpproxy/ -v -count=1` — all unit tests pass
2. `go build ./cmd/diane/` — compiles cleanly
3. `diane mcp list --tools` — shows tools from HTTP servers
4. `diane mcp auth --server infakt` — triggers OAuth flow
5. `diane doctor` — shows connected MCP servers with tool counts
6. Relay registers all tools (including HTTP server tools) with Memory Platform
