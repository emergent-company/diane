# Diane Companion App — Deep Refactor Plan

> **Goal:** Transform the companion macOS SwiftUI app from a read-only dashboard into an interactive MCP management console, a full session inspector, and a deeper Apple ecosystem integration hub — all while keeping the `diane` binary bundled, the process architecture intact, and the CI pipeline clean.

## Current Architecture

```
Diane.app
├── diane serve (child process, :8890)
│   ├── Local API (GET /api/sessions, /api/mcp-servers, /api/nodes, /api/status)
│   ├── MCP Relay (goroutine, WebSocket → MP)
│   └── Discord Bot (if master)
├── Swift UI
│   ├── SessionsView — chat bubbles, tool calls, thinking blocks
│   ├── MCPServersView — read-only list + relay node status
│   ├── PermissionsView — status + request buttons
│   ├── AppleIntegration/ — Calendar, Reminders, Contacts, Notes
│   └── MenuBar — status, update, quick launch
└── Extras
    ├── CLIManager — dual symlinks to bundled binary
    ├── UpdateChecker — DMG download + ditto install
    └── AppleScriptRunner — osascript for Notes
```

## What Exists vs What's Needed

| Area | Status | Gap |
|------|--------|-----|
| Binary bundling | ✅ `APIServerManager` spawns process | — |
| Dual symlinks | ✅ `CLIManager` creates `~/.local/bin` + `~/.diane/bin` | — |
| MCP server listing | ✅ Read-only list | ❌ No enable/disable, no add/edit/delete |
| MCP tools/prompts | ❌ Not in Go API or Swift UI | Need endpoints + views |
| Session logs | ✅ Chat bubbles, status, tokens | ❌ No search/filter, no export, no thinking/tool-call granularity |
| Apple Calendar | ✅ `CalendarManager` | ❌ No Calendar UI view |
| Apple Reminders | ✅ `RemindersManager` | ❌ No Reminders UI view |
| Apple Contacts | ✅ `ContactsManager` | ❌ No Contacts UI view |
| Apple Notes | ✅ `NotesManager` | — |
| Apple Messages | ❌ `MessagesManager.swift` doesn't exist | Need to create |
| Apple Mail | ❌ `MailManager.swift` doesn't exist | Need to create |
| Permissions | ✅ `PermissionManager` + `PermissionsView` | ❌ No accessibility check prompt, no automation persistence |
| Auto-update | ✅ DMG-based | — |
| CI/release | ✅ GitHub Actions DMG | — |
| RelayNodesView | ⚠️ Section in MCPServersView | Should be its own sidebar item |
| EmergentAPIClient logger | ⚠️ `emergent-mac` subsystem | Needs renaming to `diane-companion` |
| View state | ⚠️ `HSplitView` for most panels | Better to use `NavigationSplitView` consistently so panels are collapsible |

## Phase 1: Go API — Add MCP Tools/Prompts + CRUD Endpoints

### 1.1 — Add `/api/mcp-servers/{name}/tools` and `/{name}/prompts`

**File:** `server/cmd/diane/local_api.go`

These endpoints use the MCP proxy's `MCPClient` (which already exists in `internal/mcpproxy/`) to connect to a running MCP server and list its available tools and prompts.

**Route registration** (in `startLocalAPI`):
```go
mux.HandleFunc("/api/mcp-servers/", api.handleMCPServerByName)
```

**Handler logic:**
```go
// GET /api/mcp-servers/{name}/tools — list tools for a server
// GET /api/mcp-servers/{name}/prompts — list prompts for a server
func (a *localAPIServer) handleMCPServerByName(w http.ResponseWriter, r *http.Request) {
    path := strings.TrimPrefix(r.URL.Path, "/api/mcp-servers/")
    parts := strings.SplitN(path, "/", 2)
    if len(parts) < 2 { /* error */ }
    
    serverName := parts[0]
    action := parts[1] // "tools" or "prompts"
    
    // Load MCP config to find the server definition
    cfg := loadMCPServerConfig(serverName)
    
    // Connect to the MCP server
    client := mcpproxy.NewMCPClient(cfg)
    defer client.Close()
    
    switch action {
    case "tools":
        tools, err := client.ListTools(ctx)
        jsonResponse(w, map[string]any{"tools": tools})
    case "prompts":
        prompts, err := client.ListPrompts(ctx)
        jsonResponse(w, map[string]any{"prompts": prompts})
    }
}
```

### 1.2 — Add MCP CRUD endpoints

```go
// POST /api/mcp-servers/toggle/{name} — toggle enabled/disabled
func (a *localAPIServer) handleMCPToggle(w http.ResponseWriter, r *http.Request) { ... }

// POST /api/mcp-servers/save — add or update an MCP server
func (a *localAPIServer) handleMCPSave(w http.ResponseWriter, r *http.Request) { ... }

// DELETE /api/mcp-servers/{name} — remove an MCP server
func (a *localAPIServer) handleMCPDelete(w http.ResponseWriter, r *http.Request) { ... }
```

**Implementation pattern:** Read `mcp-servers.json`, mutate in memory, write back with `json.MarshalIndent`. For toggle, just flip `Enabled` field. For add/edit, merge by name.

### 1.3 — Update CORS middleware

Current middleware only allows `GET, OPTIONS`. Need to add `POST, PUT, DELETE`:
```go
w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
```

## Phase 2: Swift API Client — Extend DianeAPIClient

### 2.1 — Add MCP tool/prompt methods

**File:** `Sources/CompanionCore/DianeAPIClient.swift`

```swift
func fetchMCPTools(serverName: String) async throws -> [MCPTool]
func fetchMCPPrompts(serverName: String) async throws -> [MCPPrompt]
func toggleMCPServer(serverName: String) async throws
func saveMCPServer(_ server: MCPServer) async throws
func deleteMCPServer(serverName: String) async throws
```

New models:
```swift
struct MCPTool: Identifiable, Codable, Sendable {
    let name: String
    let description: String?
    let inputSchema: AnyCodable?
    var id: String { name }
}

struct MCPPrompt: Identifiable, Codable, Sendable {
    let name: String
    let description: String?
    let arguments: [MCPPromptArgument]?
    var id: String { name }
}

struct MCPPromptArgument: Codable, Sendable {
    let name: String
    let description: String?
    let required: Bool
}
```

### 2.2 — Fix EmergentAPIClient logger subsystem

**File:** `Sources/CompanionCore/EmergentAPIClient.swift`

```swift
// Change from:
private let logger = Logger(subsystem: "com.emergent-company.emergent-mac", category: "APIClient")
// To:
private let logger = Logger(subsystem: "com.emergent-company.diane-companion", category: "APIClient")
```

## Phase 3: Swift UI — MCPServersView Overhaul

### 3.1 — Server Detail Panel: Add Tools & Prompts Tabs

**File:** `Sources/Views/MCPServersView.swift`

Replace the current read-only `serverDetailPanel` with a tabbed detail view:

```
┌────────────────────────────────────────────┐
│  Server: chrome-devtools                   │
│  ● Enabled  │  Type: STDIO                 │
├──────────┬──────────┬──────────────────────┤
│ Config   │ Tools ◄  │ Prompts              │
├──────────┴──────────┴──────────────────────┤
│                                           │
│  ┌──────────────────────────────────┐     │
│  │ 🔧 navigate_page                 │     │
│  │   Navigate to a URL              │     │
│  ├──────────────────────────────────┤     │
│  │ 🔧 take_screenshot               │     │
│  │   Capture page screenshot        │     │
│  ├──────────────────────────────────┤     │
│  │ 🔧 click_element                 │     │
│  │   Click on page element          │     │
│  └──────────────────────────────────┘     │
│                                           │
└────────────────────────────────────────────┘
```

**Toolbar actions per server:**
- Toggle enable/disable (instant, writes to config)
- Test connection (calls tools endpoint, shows result)
- Edit server config (inline editor)
- Delete server (with confirmation)

### 3.2 — Add "Add MCP Server" Sheet

A form sheet for adding/editing MCP servers:

| Field | Type | Notes |
|-------|------|-------|
| Name | Text | Required, unique |
| Type | Picker | stdio / http / sse / streamable-http |
| Command | Text | For stdio type |
| Args | Text (comma-separated) | Optional |
| URL | Text | For http/sse types |
| Env | Key-Value editor | Dynamic list |
| Timeout | Stepper | 0-300 seconds |

## Phase 4: Swift UI — RelayNodesView as Independent View

### 4.1 — Extract from MCPServersView

Create `Sources/Views/RelayNodesView.swift`:

- Full table: hostname, instance ID, version, tool count, connected at, last seen
- Status indicators (green/yellow/red based on last seen recency)
- Click to see detailed info (tools registered, connection uptime)

### 4.2 — Add to Sidebar

**File:** `Sources/CompanionCore/AppState.swift`

```swift
enum SidebarItem: String, CaseIterable, Identifiable, Hashable {
    case sessions    = "Sessions"
    case mcpServers  = "MCP Servers"
    case relayNodes  = "Relay Nodes"  // NEW
    case permissions = "Permissions"
}
```

**File:** `Sources/Views/MainWindowView.swift`

```swift
case .relayNodes:
    RelayNodesView()
```

Icon: `"antenna.radiowaves.left.and.right"`

## Phase 5: SessionsView — Add Search, Filter, Export

### 5.1 — Search bar

Add a search field at the top of the sessions list that filters by title client-side:
```swift
@State private var searchText = ""
```

Filter logic:
```swift
var filteredSessions: [DianeSession] {
    if searchText.isEmpty { return sessions }
    return sessions.filter { ($0.title ?? "").localizedCaseInsensitiveContains(searchText) }
}
```

### 5.2 — Status filter

Add a Picker/Segmented control:
- All
- Active
- Completed

Union with search.

### 5.3 — Export session as JSON

Add button in session detail header: "Export JSON" → saves selected session's messages to a `.json` file via NSSavePanel.

### 5.4 — Thinking/tool-call granularity

**File:** `Sources/CompanionCore/Models.swift`

Extend `DianeMessage` model to support:
```swift
struct DianeMessage: Identifiable, Codable, Sendable {
    let id: String
    let role: String
    let content: String
    // NEW fields:
    let reasoningContent: String?      // <thinking> blocks
    let toolCalls: [MessageToolCall]?  // function/tool calls
    let toolResults: [ToolResult]?     // results from tool execution
    // ...
}

struct MessageToolCall: Identifiable, Codable, Sendable {
    let id: String
    let name: String
    let arguments: String  // JSON string of args
}

struct ToolResult: Codable, Sendable {
    let callID: String?
    let content: String?
    let isError: Bool?
}
```

The Go API already returns these fields from `bridge.GetMessages()`. The Swift decoder just needs to extract them from `properties`:
```swift
self.reasoningContent = graph.properties?["reasoning_content"]?.stringValue
self.toolCalls = graph.properties?["tool_calls"]?.arrayValue  // need AnyValue extension
```

**Add `arrayValue` to `AnyValue`:**
```swift
var arrayValue: [Any]? {
    switch self { case .array(let a): return a; default: return nil }
}
// Need new case:
case array([Any])
```

In the SessionsView, render:
- **Thinking blocks**: Orange background, collapsible (like the existing design)
- **Tool calls**: Purple background, show name + args, collapsible
- **Tool results**: Gray background, truncated with "Show more"

## Phase 6: Apple Ecosystem Integration Views

### 6.1 — Calendar View

**File:** `Sources/Views/CalendarView.swift`

- Month calendar view (or simple list for macOS)
- Shows event count per calendar
- Button to create a new event
- Permission status at top

### 6.2 — Reminders View

**File:** `Sources/Views/RemindersView.swift`

- List of reminder lists (work, personal, etc.)
- Expandable reminder items with due dates
- Create reminder button
- Quick add inline

### 6.3 — Contacts View

**File:** `Sources/Views/ContactsView.swift`

- Search bar with debounce
- Results list: name, email, phone
- Click to see detail

### 6.4 — Implement MessagesManager + MailManager

**File:** `Sources/AppleIntegration/MessagesManager.swift`

```swift
@MainActor
final class MessagesManager: ObservableObject {
    @Published private(set) var isAuthorized = false
    
    func sendMessage(to: String, text: String) async throws {
        let safeTo = to.replacingOccurrences(of: "\"", with: "\\\"")
        let safeText = text.replacingOccurrences(of: "\"", with: "\\\"")
        let script = """
        tell application "Messages"
            set targetService to 1st service whose service type = iMessage
            set targetBuddy to buddy "\(safeTo)" of targetService
            send "\(safeText)" to targetBuddy
        end tell
        """
        try await AppleScriptRunner.run(script)
    }
}
```

**File:** `Sources/AppleIntegration/MailManager.swift`

```swift
@MainActor
final class MailManager: ObservableObject {
    func sendEmail(to: String, subject: String, body: String) async throws { ... }
    func fetchInbox(count: Int = 20) async throws -> [MailMessage] { ... }
}

struct MailMessage: Identifiable, Sendable {
    let id: String
    let subject: String?
    let sender: String?
    let date: Date?
}
```

### 6.5 — Add Apple Integration views to sidebar

```swift
enum SidebarItem: String, CaseIterable, Identifiable, Hashable {
    // ... existing
    case calendar   = "Calendar"    // icon: "calendar"
    case reminders  = "Reminders"   // icon: "checklist"
    case contacts   = "Contacts"    // icon: "person.crop.circle"
    case mail       = "Mail"        // icon: "envelope"
    case messages   = "Messages"    // icon: "message"
    case notes      = "Notes"       // icon: "note.text"
}
```

**Trade-off:** This adds 6 sidebar items. Consider grouping under a "Apple Services" section with a disclosure group in the sidebar:

```swift
Section("Apple Services") {
    Label("Calendar", systemImage: "calendar").tag(SidebarItem.calendar)
    Label("Reminders", systemImage: "checklist").tag(SidebarItem.reminders)
    Label("Contacts", systemImage: "person.crop.circle").tag(SidebarItem.contacts)
    Label("Mail", systemImage: "envelope").tag(SidebarItem.mail)
    Label("Messages", systemImage: "message").tag(SidebarItem.messages)
    Label("Notes", systemImage: "note.text").tag(SidebarItem.notes)
}
```

## Phase 7: Permissions View Improvements

### 7.1 — Accessibility prompt enhancement

Current: just opens System Settings. Better: show a step-by-step guide with instructions:
```swift
// After opening settings, show a popover:
"1. Click the lock to make changes
 2. Check 'Diane.app' in the list
 3. If not listed, click + and add Diane.app from Applications"
```

### 7.2 — Automation persistence

macOS 14+ can programmatically request automation permission:
```swift
// Create an AppleScript process that triggers the TCC prompt
let script = """
tell application "System Events"
    -- Trigger automation prompt for Notes, Mail, Messages
end tell
"""
```

### 7.3 — Permission status auto-refresh

Add a timer that refreshes permission status every 30 seconds, so the user sees changes without manually clicking Refresh.

## Phase 8: Go Local API — Session Log Endpoint

### 8.1 — Add `/api/sessions/{id}` (single session detail)

```go
// GET /api/sessions/{id} — get session metadata only
func (a *localAPIServer) handleSingleSession(w http.ResponseWriter, r *http.Request) {
    // ... extract session ID, call bridge.GetSession(id)
}
```

### 8.2 — Add pagination to message endpoint

```go
// GET /api/sessions/{id}/messages?limit=100&offset=0
```

Currently `bridge.GetMessages()` returns all messages. Add limit/offset support.

## Phase 9: CI & Build Updates

### 9.1 — Fix version in DMG

**File:** `Scripts/build-dmg.sh`

The version is still hardcoded to `1.0`. Ensure `VERSION` env var from git tag is properly applied:
- CI passes `VERSION` from `${{ github.ref_name }}`
- Use `plutil -replace CFBundleShortVersionString`
- Pass to `hdiutil` for DMG name

### 9.2 — Add Messages/Mail entitlements

**File:** `DianeCompanion/DianeCompanion.entitlements`

If using AppleScript for Messages/Mail, no additional entitlements needed — they use Automation permission. If switching to native APIs:
- `com.apple.security.automation.appleevents`
- For Mail: could use `EMMail` (no native framework)

**Recommendation:** Keep AppleScript for Messages/Mail. No entitlement changes needed.

## Implementation Order

### Tier 1 (Core MCP Management) — Do First
1. Phase 1: Go API — tools/prompts + CRUD endpoints
2. Phase 2: Swift DianeAPIClient — new methods + fix logger
3. Phase 3: MCPServersView — add tools/prompts tabs, toggle, add/edit/delete

### Tier 2 (Session Improvements + Relay Nodes)
4. Phase 4: RelayNodesView — extract to own view + sidebar item
5. Phase 5: SessionsView — search, filter, export, thinking/tool-call granularity
6. Phase 8: Go API — single session + pagination

### Tier 3 (Apple Integration UIs)
7. Phase 6: Calendar, Reminders, Contacts views
8. Phase 6: MessagesManager + MailManager implementations
9. Phase 6: Apple sidebar section with disclosure group

### Tier 4 (Polish)
10. Phase 7: Permissions improvements
11. Phase 9: CI version fix

## Appendix: File Change Summary

| File | Action | Phase |
|------|--------|-------|
| `server/cmd/diane/local_api.go` | Add routes + handlers for tools/prompts/CRUD | 1 |
| `Sources/CompanionCore/DianeAPIClient.swift` | Add fetchMCPTools/Prompts, save/delete/toggle | 2 |
| `Sources/CompanionCore/EmergentAPIClient.swift` | Fix logger subsystem string | 2 |
| `Sources/CompanionCore/Models.swift` | Add MCPTool, MCPPrompt models; extend DianeMessage with thinking/tool-call; extend AnyValue | 2, 5 |
| `Sources/Views/MCPServersView.swift` | Tabbed detail, add sheet, toggle button, delete | 3 |
| `Sources/Views/RelayNodesView.swift` | New file — standalone relay node browser | 4 |
| `Sources/CompanionCore/AppState.swift` | Add relayNodes, calendar, reminders, contacts, mail, messages, notes to SidebarItem | 4, 6 |
| `Sources/Views/MainWindowView.swift` | Add switch cases for new sidebar items; add Apple Services section | 4, 6 |
| `Sources/Views/SessionsView.swift` | Add search, filter, export, thinking-block/tool-call rendering | 5 |
| `Sources/Views/CalendarView.swift` | New file | 6 |
| `Sources/Views/RemindersView.swift` | New file | 6 |
| `Sources/Views/ContactsView.swift` | New file | 6 |
| `Sources/AppleIntegration/MessagesManager.swift` | New file — create | 6 |
| `Sources/AppleIntegration/MailManager.swift` | New file — create | 6 |
| `Sources/Permissions/PermissionsView.swift` | Add step-by-step guide, auto-refresh | 7 |
| `Scripts/build-dmg.sh` | Fix version from git tag | 9 |
