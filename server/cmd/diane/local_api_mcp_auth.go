package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

// ── Pending auth state ──

type authStatus string

const (
	authPending   authStatus = "pending"
	authCompleted authStatus = "completed"
	authFailed    authStatus = "failed"
)

type pendingAuth struct {
	Status       authStatus `json:"status"`
	ServerName   string     `json:"server_name"`
	AuthURL      string     `json:"auth_url,omitempty"`
	Error        string     `json:"error,omitempty"`
	ExpiresAt    string     `json:"expires_at,omitempty"`
	CallbackPort int        `json:"callback_port,omitempty"`

	// Internal: PKCE verifier, code channel, cleanup
	verifier    string
	codeChan    chan string
	cleanupFunc func()
	createdAt   time.Time
}

var (
	pendingAuths   = make(map[string]*pendingAuth)
	pendingAuthsMu sync.Mutex
)

// startPendingAuth initiates an OAuth authorization code flow for an MCP server.
func startPendingAuth(serverName string, oauth *mcpproxy.OAuthConfig) (*pendingAuth, error) {
	if oauth == nil {
		return nil, fmt.Errorf("no OAuth configuration for server %q", serverName)
	}
	if oauth.AuthorizationURL == "" {
		return nil, fmt.Errorf("OAuth authorization URL not configured for server %q", serverName)
	}
	if oauth.TokenURL == "" {
		return nil, fmt.Errorf("OAuth token URL not configured for server %q", serverName)
	}
	if oauth.ClientID == "" {
		if oauth.RegistrationURL != "" {
			clientID, err := mcpproxy.DynamicClientRegistration(oauth.RegistrationURL)
			if err != nil {
				return nil, fmt.Errorf("dynamic client registration failed: %w", err)
			}
			oauth.ClientID = clientID
			_ = mcpproxy.SaveDiscoveredConfig(serverName, oauth)
		} else {
			return nil, fmt.Errorf("OAuth client ID not configured for server %q", serverName)
		}
	}

	// Generate PKCE parameters
	verifier := mcpproxy.GenerateCodeVerifier()
	challenge := mcpproxy.GenerateCodeChallenge(verifier)

	// Start a local callback server
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)
	callbackServer, port, err := mcpproxy.StartCallbackServer(codeChan, errChan)
	if err != nil {
		return nil, fmt.Errorf("cannot start callback server: %w", err)
	}

	// Build authorization URL
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)
	authURL, err := mcpproxy.BuildAuthURL(oauth.AuthorizationURL, oauth.ClientID, redirectURI, challenge, oauth.Scopes)
	if err != nil {
		callbackServer.Shutdown(nil)
		return nil, fmt.Errorf("build auth URL: %w", err)
	}

	pa := &pendingAuth{
		Status:     authPending,
		ServerName: serverName,
		AuthURL:    authURL,
		verifier:   verifier,
		codeChan:   codeChan,
		createdAt:  time.Now(),
		cleanupFunc: func() {
			_ = callbackServer.Shutdown(nil)
		},
	}

	// Store the pending auth state
	pendingAuthsMu.Lock()
	if existing, ok := pendingAuths[strings.ToLower(serverName)]; ok {
		if existing.cleanupFunc != nil {
			existing.cleanupFunc()
		}
	}
	pendingAuths[strings.ToLower(serverName)] = pa
	pendingAuthsMu.Unlock()

	// Background: wait for the callback, exchange code, save tokens
	go func() {
		select {
		case code := <-codeChan:
			stored, err := mcpproxy.ExchangeCodeForTokens(oauth.TokenURL, oauth.ClientID, code, redirectURI, verifier)
			if err != nil {
				pa.fail(fmt.Sprintf("token exchange failed: %v", err))
				return
			}
			if err := mcpproxy.SaveTokens(serverName, stored); err != nil {
				pa.fail(fmt.Sprintf("failed to save tokens: %v", err))
				return
			}
			expiresAt := ""
			if !stored.ExpiresAt.IsZero() {
				expiresAt = stored.ExpiresAt.Format(time.RFC3339)
			}
			pa.complete(expiresAt)

			log.Printf("[OAuth] Successfully authenticated MCP server: %s (expires: %s)", serverName, expiresAt)

		case err := <-errChan:
			pa.fail(fmt.Sprintf("callback server error: %v", err))

		case <-time.After(5 * time.Minute):
			pa.fail("authorization timed out after 5 minutes")
		}
	}()

	return pa, nil
}

func (pa *pendingAuth) complete(expiresAt string) {
	pendingAuthsMu.Lock()
	defer pendingAuthsMu.Unlock()
	if pa.cleanupFunc != nil {
		pa.cleanupFunc()
		pa.cleanupFunc = nil
	}
	pa.Status = authCompleted
	pa.ExpiresAt = expiresAt
}

func (pa *pendingAuth) fail(errMsg string) {
	pendingAuthsMu.Lock()
	defer pendingAuthsMu.Unlock()
	if pa.cleanupFunc != nil {
		pa.cleanupFunc()
		pa.cleanupFunc = nil
	}
	pa.Status = authFailed
	pa.Error = errMsg
	log.Printf("[OAuth] Auth failed for %s: %s", pa.ServerName, errMsg)
}

func getPendingAuth(serverName string) *pendingAuth {
	pendingAuthsMu.Lock()
	defer pendingAuthsMu.Unlock()
	return pendingAuths[strings.ToLower(serverName)]
}

func deletePendingAuth(serverName string) {
	pendingAuthsMu.Lock()
	defer pendingAuthsMu.Unlock()
	if pa, ok := pendingAuths[strings.ToLower(serverName)]; ok {
		if pa.cleanupFunc != nil {
			pa.cleanupFunc()
		}
		delete(pendingAuths, strings.ToLower(serverName))
	}
}

// ── HTTP Handlers ──

type mcpAuthResponse struct {
	Status  string `json:"status"`
	AuthURL string `json:"auth_url,omitempty"`
	Server  string `json:"server"`
	Error   string `json:"error,omitempty"`
}

type mcpAuthStatusResponse struct {
	Status    string `json:"status"`
	Server    string `json:"server"`
	AuthURL   string `json:"auth_url,omitempty"`
	Error     string `json:"error,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func (h *apiHandlers) handleMCPServerAuth(w http.ResponseWriter, r *http.Request, serverName string) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed (use POST)")
		return
	}

	// Check if there's already a pending auth for this server
	existing := getPendingAuth(serverName)
	if existing != nil && existing.Status == authPending {
		writeJSON(w, mcpAuthResponse{
			Status:  "pending",
			AuthURL: existing.AuthURL,
			Server:  serverName,
		})
		return
	}

	// Look up OAuth config: first discovered, then fallback
	oauth := mcpproxy.LoadDiscoveredConfig(serverName)
	if oauth == nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("no OAuth configuration found for server %q. Run diane serve first to auto-discover OAuth endpoints.", serverName))
		return
	}

	pa, err := startPendingAuth(serverName, oauth)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to start auth: %v", err))
		return
	}

	log.Printf("[LOCAL-API] Started OAuth flow for MCP server %s", serverName)

	writeJSON(w, mcpAuthResponse{
		Status:  "pending",
		AuthURL: pa.AuthURL,
		Server:  serverName,
	})
}

func (h *apiHandlers) handleMCPServerAuthStatus(w http.ResponseWriter, r *http.Request, serverName string) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed (use GET)")
		return
	}

	pa := getPendingAuth(serverName)
	if pa == nil {
		// No active auth flow — check if tokens already exist
		tokens, err := mcpproxy.LoadTokens(serverName)
		if err == nil && tokens.AccessToken != "" {
			expiresAt := ""
			if !tokens.ExpiresAt.IsZero() {
				expiresAt = tokens.ExpiresAt.Format(time.RFC3339)
			}
			writeJSON(w, mcpAuthStatusResponse{
				Status:    "completed",
				Server:    serverName,
				ExpiresAt: expiresAt,
			})
			return
		}
		writeJSON(w, mcpAuthStatusResponse{
			Status: "idle",
			Server: serverName,
		})
		return
	}

	resp := mcpAuthStatusResponse{
		Status:  string(pa.Status),
		Server:  serverName,
		AuthURL: pa.AuthURL,
		Error:   pa.Error,
	}
	if pa.Status == authCompleted {
		resp.ExpiresAt = pa.ExpiresAt
	}

	writeJSON(w, resp)

	// Clean up completed/failed auths on read
	if pa.Status != authPending {
		deletePendingAuth(serverName)
	}
}
