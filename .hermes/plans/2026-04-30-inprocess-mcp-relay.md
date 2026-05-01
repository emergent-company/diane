# In-Process MCP Relay + Clean Shutdown Plan

> **Goal:** Eliminate `diane mcp serve` subprocess from the MCP relay, and ensure `diane serve` cleans up all services when killed.

**Current problem:**
```
diane serve (PID X)            ← companion app manages this
├── MCP relay goroutine
│   └── exec("diane mcp serve")  ← SUBPROCESS — becomes orphan on SIGTERM
└── Local API goroutine
```
When companion app sends SIGTERM (deinit → `process?.terminate()`), `diane serve` dies immediately without propagating the signal. The MCP subprocess (and its own child MCP servers) become orphans — hence the 15+ `diane mcp serve` zombies.

**Target:**
```
diane serve (PID X)
├── MCP relay goroutine → mcpproxy.Proxy + handleMCPServeRequest()  ← IN-PROCESS
├── Local API goroutine
└── Signal handler → closes done channel → kills all goroutines cleanly
```

---

## Task 1: Add signal handling to serve.go

**Objective:** `diane serve` catches SIGTERM/SIGINT and cleanly shuts down all services before exiting.

**Files:**
- Modify: `server/cmd/diane/serve.go`

**Changes:**
Add a `done` channel and signal handler in `cmdServe()` before the goroutines start:

```go
// ── Signal handler for graceful shutdown ──
shutdownCh := make(chan os.Signal, 1)
signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)
shutdownCtx, cancel := context.WithCancel(context.Background())
defer cancel()
go func() {
    sig := <-shutdownCh
    log.Printf("[SERVE] Received %v, shutting down...", sig)
    cancel()
}()
```

Then in the wait loop at the bottom, also select on `shutdownCtx.Done()`:

```go
// ── Wait for first exit or shutdown signal ──
select {
case err = <-errCh:
    // service exited
case <-shutdownCtx.Done():
    log.Printf("[SERVE] Shutdown requested")
    err = nil
}
// On either path, the cancel() defer runs and services should stop.
```

**Add imports:** `"os/signal"`, `"context"`, `"syscall"` (syscall is already imported via lock.go's use — need to add in serve.go).

**Verification:**
```bash
cd ~/src/diane/server && go build -o /dev/null ./cmd/diane/
```

---

## Task 2: Refactor MCPSession to remove subprocess

**Objective:** `MCPSession.run()` no longer starts/reads/writes a subprocess. Instead, it creates `mcpproxy.Proxy` directly and routes WS messages through `handleMCPServeRequest()`.

**Files:**
- Modify: `server/cmd/diane/mcp_relay.go`

### Step 2a: Remove subprocess fields from MCPSession struct

Remove these fields from `MCPSession`:
- `mcpCmd *exec.Cmd`
- `mcpIn *bufio.Writer`
- `mcpOut *bufio.Scanner`
- `mcpMu sync.Mutex`
- `pending sync.Map` — no longer async, we get responses directly from function calls

Add:
- `proxy *mcpproxy.Proxy` — created by the relay, shared with the handler

Also add `version string` field to carry the build version for registration.

### Step 2b: Remove MCPBinary from MCPRelayConfig

Remove the `MCPBinary string` field from `MCPRelayConfig` — no longer needed.

### Step 2c: Rewrite cmdMCPRelay()

Current creates a `MCPSession` with binary config. New version creates `mcpproxy.Proxy`:

```go
func cmdMCPRelay(cfg MCPRelayConfig) {
    if cfg.ReconnectDelay == 0 {
        cfg.ReconnectDelay = 5 * time.Second
    }

    log.Printf("[mcp-relay] Starting relay for instance: %s", cfg.InstanceID)
    log.Printf("[mcp-relay] Relay server: %s", cfg.RelayURL)

    // Create MCP proxy in-process (no subprocess)
    configPath := mcpproxy.GetDefaultConfigPath()
    proxy, err := mcpproxy.NewProxy(configPath)
    if err != nil {
        log.Printf("[mcp-relay] Warning: failed to create MCP proxy: %v", err)
    }
    defer func() {
        if proxy != nil {
            proxy.Close()
        }
    }()

    session := &MCPSession{
        cfg:     cfg,
        proxy:   proxy,
        done:    make(chan struct{}),
        version: Version,
    }

    // Signal handling for relay shutdown
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-sigCh
        log.Printf("[mcp-relay] Shutting down...")
        close(session.done)
        session.disconnectWS()
    }()

    // Main reconnect loop
    backoff := cfg.ReconnectDelay
    for {
        select {
        case <-session.done:
            return
        default:
        }

        err := session.run()
        if err != nil {
            log.Printf("[mcp-relay] Connection error: %v (reconnecting in %v)", err, backoff)
            select {
            case <-session.done:
                return
            case <-time.After(backoff):
            }
            backoff *= 2
            if backoff > 5*time.Minute {
                backoff = 5 * time.Minute
            }
        } else {
            backoff = cfg.ReconnectDelay
        }
    }
}
```

### Step 2d: Rewrite run() method

Replace subprocess-based communication with direct handler calls:

```go
func (s *MCPSession) run() error {
    // 1. Connect to relay
    u, _ := url.Parse(s.cfg.RelayURL)
    query := u.Query()
    query.Set("instance", s.cfg.InstanceID)
    u.RawQuery = query.Encode()

    header := make(http.Header)
    header.Set("Authorization", "Bearer "+s.cfg.ProjectToken)

    dialer := *websocket.DefaultDialer
    dialer.HandshakeTimeout = 10 * time.Second
    conn, _, err := dialer.Dial(u.String(), header)
    if err != nil {
        return fmt.Errorf("connect to relay: %w", err)
    }
    s.wsConn = conn
    log.Printf("[mcp-relay] Connected to relay: %s (instance: %s)", s.cfg.RelayURL, s.cfg.InstanceID)

    // Set up WebSocket keepalive (same as before)
    // ... ping/pong unchanged ...

    // Send initial register message with tool list
    s.sendRegister()

    // Start background tool watch
    s.startToolWatch()

    defer s.disconnectWS()

    // Forward loop: WS → in-process handler → WS response
    errCh := make(chan error, 1)
    go func() {
        for {
            select {
            case <-s.done:
                return
            default:
            }

            _, msg, err := conn.ReadMessage()
            if err != nil {
                errCh <- fmt.Errorf("ws read: %w", err)
                return
            }

            var frame RelayFrame
            if err := json.Unmarshal(msg, &frame); err != nil {
                log.Printf("[mcp-relay] Invalid WS frame: %v", err)
                continue
            }

            switch frame.Type {
            case "request":
                // Parse JSON-RPC request from payload
                var req struct {
                    JSONRPC string          `json:"jsonrpc"`
                    ID      interface{}     `json:"id"`
                    Method  string          `json:"method"`
                    Params  json.RawMessage `json:"params,omitempty"`
                }
                if err := json.Unmarshal(frame.Payload, &req); err != nil {
                    log.Printf("[mcp-relay] Invalid request payload: %v", err)
                    continue
                }

                // Handle in-process
                resp := handleMCPServeRequest(req, s.proxy)
                resp.JSONRPC = "2.0"
                resp.ID = req.ID

                // Wrap in response frame for relay routing
                respData, _ := json.Marshal(resp)
                wrapped := map[string]interface{}{
                    "type":    "response",
                    "id":      req.ID,
                    "payload": json.RawMessage(respData),
                }
                wrappedData, _ := json.Marshal(wrapped)
                s.sendWS(wrappedData)

            case "ping":
                s.sendWS(json.RawMessage(`{"type":"pong"}`))
            }
        }
    }()

    // Wait for first error or clean shutdown
    select {
    case err := <-errCh:
        return err
    case <-s.done:
        return nil
    }
}
```

### Step 2e: Rewrite sendRegister()

Replace subprocess stdin/stdout tool listing with direct call:

```go
func (s *MCPSession) sendRegister() {
    // Build tool list directly from proxy
    tools := buildMCPToolList()
    if s.proxy != nil {
        proxiedTools, err := s.proxy.ListAllTools()
        if err != nil {
            log.Printf("[mcp-relay] Failed to list proxied tools: %v", err)
        } else if proxiedTools != nil {
            tools = append(tools, proxiedTools...)
        }
    }

    toolsData, _ := json.Marshal(map[string]interface{}{"tools": tools})
    s.doRegister(toolsData)
}
```

### Step 2f: Rewrite startToolWatch()

Replace subprocess tool listing with direct call:

```go
func (s *MCPSession) startToolWatch() {
    go func() {
        ticker := time.NewTicker(20 * time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                // Build tool list directly
                tools := buildMCPToolList()
                if s.proxy != nil {
                    proxiedTools, err := s.proxy.ListAllTools()
                    if err == nil && proxiedTools != nil {
                        tools = append(tools, proxiedTools...)
                    }
                }
                toolsData, _ := json.Marshal(map[string]interface{}{"tools": tools})
                log.Printf("[mcp-relay] Tools watch: re-registering...")
                s.doRegister(toolsData)
            case <-s.done:
                return
            }
        }
    }()
}
```

### Step 2g: Remove stopMCP() and forwardToMCP()

Delete these entire methods — no longer needed.

### Step 2h: Remove unused WatchPatterns field from RelayFrame if present

Check and remove `WatchPatterns string` field from `RelayFrame` if unused.

**Verification:**
```bash
cd ~/src/diane/server && go build -o /dev/null ./cmd/diane/
```

---

## Task 3: Remove unused imports and dead code

**Objective:** Clean up unused imports in `mcp_relay.go` after removing subprocess code.

**Files:**
- Modify: `server/cmd/diane/mcp_relay.go`

Remove these imports (check `go build` for exact list):
- `"bufio"` — no longer used (no more pipes)
- `exec` — no longer used (no more subprocesses)
- `"os/exec"` — removed

**Verification:**
```bash
cd ~/src/diane/server && go build -o /dev/null ./cmd/diane/
```

---

## Task 4: Verify companion app cleanup

**Objective:** Confirm that the companion app's `deinit` → `process?.terminate()` → SIGTERM → `serve.go` signal handler → clean shutdown chain works.

**Files to inspect (no changes needed):**
- `server/swift/DianeCompanion/Sources/CompanionCore/APIServerManager.swift`

The deinit at line 83 calls `process?.terminate()` which sends SIGTERM. After task 1, `serve.go` catches SIGTERM, cancels the context, and the goroutines (bot, relay, API) stop cleanly. The relay's `cmdMCPRelay` signal handler also fires for the SIGTERM, closing `session.done`, which cleanly disconnects the WS.

**Verification:**
```bash
# Build and install
cd ~/src/diane && bash scripts/dev-build.sh

# Open the companion app, then quit it
# Check no orphan diane processes remain:
sleep 3 && pgrep -af "diane mcp serve"  # should be 0
```

---

## Task 5: Remove stale `diane mcp serve` invocation

**Objective:** Clean up any remaining references that assume a separate `diane mcp serve` process exists.

**Files to check:**
- `server/cmd/diane/mcp_relay.go:64` — Remove `MCPBinary` comment about subprocess

No other files reference `MCPBinary` (checked via `search_files`).

---

## Rollback Plan

If the refactor breaks the relay:
1. The WS connection fails — relay reconnects with backoff (up to 5min)
2. Companion app shows "Could not connect to the server" for nodes
3. The `proxy.CallTool()` change is the riskiest — if proxy is nil, tools return errors
4. Fix: `git checkout -- server/cmd/diane/mcp_relay.go server/cmd/diane/serve.go`

## Files Changed

| File | Change |
|------|--------|
| `server/cmd/diane/serve.go` | Add signal handler (SIGTERM/SIGINT context cancellation) |
| `server/cmd/diane/mcp_relay.go` | Major refactor: remove subprocess, add proxy, rewrite run/sendRegister/startToolWatch |
