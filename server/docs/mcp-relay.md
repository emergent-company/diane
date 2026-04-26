# MCP Relay: Outbound WebSocket Bridge for Diane

## Why

emergent.memory (cloud agentic runner) needs to call tools on Diane Master (local machine behind NAT/firewall). Diane must initiate the connection outbound. The relay sits on the memory server and bridges the gap.

## Architecture

```
┌── Diane (local, ~/.diane/) ───────┐    ┌── memory.emergent-company.ai ───────────┐
│                                    │    │                                          │
│  diane mcp-relay ──outbound WS─────┼───►│  MCP Relay (WS server :9090)             │
│    ↕ MCP JSON-RPC over WS frames   │    │    ↕ internal                            │
│                                    │    │  agentic runner (LLM loop)              │
│  auto-reconnect (30/60/120s)       │    │  → calls Diane tools via relay          │
│  auth: project token               │    │                                          │
└────────────────────────────────────┘    └──────────────────────────────────────────┘
```

## Implementation (Very Lightweight)

### Part A: MCP Relay Server (~150 lines, on memory server)

```go
// mcp-relay/main.go
// Runs on memory.emergent-company.ai

package main

import (
    "net/http"
    "sync"
    "github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

// dianeSessions holds all connected Diane instances
var dianeSessions = struct {
    sync.RWMutex
    sessions map[string]*websocket.Conn  // keyed by project_id
}{sessions: make(map[string]*websocket.Conn)}

func main() {
    http.HandleFunc("/mcp-relay", handleDianeConnect)
    http.HandleFunc("/mcp-relay/{project_id}/call", handleToolCall)
    log.Fatal(http.ListenAndServe(":9090", nil))
}

// Diane connects here (outbound, persistent)
func handleDianeConnect(w http.ResponseWriter, r *http.Request) {
    conn, _ := upgrader.Upgrade(w, r, nil)
    // Auth: first message is project token
    _, msg, _ := conn.ReadMessage()
    token := string(msg)

    projectID := validateToken(token)
    dianeSessions.Lock()
    dianeSessions.sessions[projectID] = conn
    dianeSessions.Unlock()
    defer cleanup(projectID)

    // Keep connection alive: read pings/notifications from Diane
    for {
        _, _, err := conn.ReadMessage()
        if err != nil { break }  // Diane disconnected → cleanup
    }
}

// Agentic runner calls tools via this endpoint
func handleToolCall(w http.ResponseWriter, r *http.Request) {
    projectID := extractProjectID(r)
    call := parseToolCall(r.Body)

    dianeSessions.RLock()
    conn := dianeSessions.sessions[projectID]
    dianeSessions.RUnlock()

    conn.WriteJSON(call)                       // Forward to Diane
    result := readResponse(conn)               // Wait for response
    json.NewEncoder(w).Encode(result)
}
```

### Part B: Diane `mcp-relay` command (~120 lines)

```go
// cmd/diane/mcp_relay.go
// New subcommand: diane mcp-relay

package main

import (
    "encoding/json"
    "github.com/gorilla/websocket"
    "time"
    "log"
)

func cmdMCPRelay(relayURL string, token string) {
    // Run the MCP server in-process
    go runMCPServer()  // starts the existing handleRequest loop

    // Connect outbound to relay (persistent WS)
    for {
        conn, _, err := websocket.DefaultDialer.Dial(relayURL, nil)
        if err != nil {
            time.Sleep(30 * time.Second)  // retry
            continue
        }

        // Auth: send project token as first frame
        conn.WriteMessage(websocket.TextMessage, []byte(token))

        // Main loop: relay tool calls → handle → send response
        for {
            _, msg, err := conn.ReadMessage()
            if err != nil { break }  // disconnect → reconnect loop

            var call ToolCall
            json.Unmarshal(msg, &call)
            result := handleRequest(call)  // existing MCP handler
            conn.WriteJSON(result)
        }

        conn.Close()
        time.Sleep(30 * time.Second)
    }
}
```

### Part C: Invocation

```bash
# On Diane's local machine (e.g., systemd service or launchd plist)
diane mcp-relay \
  --relay wss://memory.emergent-company.ai/mcp-relay \
  --token emt_proj_xxx

# Auto-reconnects on network change, sleep wake, etc.
```

## Example Flow

```
Agentic runner needs to list GitHub issues:
  POST /mcp-relay/{project}/call
  {"method": "tools/call", "params": {"name": "github_list_issues", ...}}

Relay → WebSocket → Diane MCP Server
Diane executes → returns result → WebSocket → Relay → HTTP response

All standard MCP — nothing custom in the protocol.
```

## Requirements

| Side | What | Lines |
|------|------|-------|
| **Memory server** | Go HTTP server, gorilla/websocket, token validation | ~150 |
| **Diane** | ~50 lines in `cmd/diane/mcp_relay.go` | ~120 |
| **Deploy** | systemd unit or launchd plist for auto-start + reconnect | 1 file |

That's it. The MCP protocol stays completely unchanged — just the transport swaps from stdin/stdout to WebSocket.
