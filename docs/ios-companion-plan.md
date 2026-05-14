# iOS Companion App — Specification & Implementation Plan

> **Status:** Draft
> **Date:** 2026-05-13
> **Author:** DianeCoder (Hermes Agent)

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [Shared Swift Package: DianeShared](#2-shared-swift-package-dianeshared)
3. [iOS App Structure](#3-ios-app-structure)
4. [iOS as a Passive Node](#4-ios-as-a-passive-node)
5. [Notification Flow](#5-notification-flow)
6. [Chat UI Strategy](#6-chat-ui-strategy)
7. [Feature Matrix](#7-feature-matrix)
8. [Implementation Phases](#8-implementation-phases)
9. [Gap Analysis — Decisions Made](#9-gap-analysis--decisions-made)
10. [Updated Spec Summary](#10-updated-spec-summary-decisions-applied)
11. [Appendix: API Surface](#11-appendix-api-surface)
12. [Third-Pass Gaps — Decisions Made](#12-third-pass-gaps--decisions-made)
13. [iOS project.yml Specification](#13-ios-projectyml-specification)
14. [Info.plist & Entitlements Specification](#14-infoplist--entitlements-specification)
15. [iOS App Startup Sequence](#15-ios-app-startup-sequence)
16. [Key Data Types](#16-key-data-types)
17. [Total File & LOC Estimate](#17-total-file--loc-estimate)
18. [UI & Navigation Design](#18-ui--navigation-design)
19. [Implementation Notes for Phase 1](#19-implementation-notes-for-phase-1)

---

## 1. Architecture Overview

### 1.1 Problem

The macOS companion app runs `diane serve` locally and connects to `127.0.0.1:8890`. iOS cannot run the server process, and the connection pattern is fundamentally different — the phone connects remotely, with intermittent connectivity.

### 1.2 Solution

The iOS app connects **directly to the Memory Platform cloud API** (`https://memory.emergent-company.ai`) as its primary data source, and optionally to a user-configured remote Diane server.

The phone registers itself as a **passive node** in the Memory Platform node registry, enabling:
- Push notifications routed through the platform
- Visibility in the node topology
- Agent → phone communication channel

### 1.3 High-Level Architecture

```
┌──────────────────────────────────────────────────┐
│  Memory Platform (cloud)                          │
│  ┌─────────────────────────────────────────────┐ │
│  │  Node Registry                              │ │
│  │  ├─ mcj-iphone  (phone, passive, token=xxx) │ │
│  │  ├─ tool-test   (slave, active)            │ │
│  │  └─ diane-master (master, active)          │ │
│  │                                             │ │
│  │  Push Router (NEW)                          │ │
│  │  ┌─────────────────────────────────────┐    │ │
│  │  │  Agent → Message → PushPayload →    │    │ │
│  │  │  → Apple Push API → APNs → Device   │    │ │
│  │  └─────────────────────────────────────┘    │ │
│  └─────────────────────────────────────────────┘ │
└─────────────▲──────────────────────▲──────────────┘
              │ HTTP API            │ APNs
              │                     ▼
┌─────────────────────────┐ ┌──────────────────┐
│ Diane iOS App            │ │ Apple Push       │
│ POST/GET /api/sessions   │ │ Notification     │
│ POST /api/chat/stream    │ │ Service (APNs)   │
│ POST /api/nodes          │ └──────────────────┘
│ GET /api/agents          │
│ registerForPushNotifications()              │
└─────────────────────────┘
```

---

## 2. Shared Swift Package: DianeShared

### 2.1 Motivation

The macOS app currently has ~3,700 lines of Swift code that are **purely about data models, networking, and formatting** — zero platform dependencies. Duplicating this for iOS is unacceptable.

### 2.2 Package Structure

```
server/swift/
├── Packages/DianeShared/
│   ├── Package.swift
│   ├── Sources/
│   │   ├── API/
│   │   │   ├── EmergentAPIClient.swift    ← cloud API client (MOVED)
│   │   │   └── HTTPClient.swift           ← shared HTTP methods (EXTRA CTED)
│   │   ├── Models/
│   │   │   ├── DianeSession.swift         ← EXTRACTED from Models.swift
│   │   │   ├── DianeMessage.swift         ← EXTRACTED
│   │   │   ├── StreamChatEvent.swift      ← EXTRACTED
│   │   │   ├── AgentDef.swift             ← EXTRACTED
│   │   │   ├── RelayNode.swift            ← EXTRACTED
│   │   │   ├── MCPServer.swift            ← EXTRACTED
│   │   │   ├── SchemaTypes.swift          ← EXTRACTED
│   │   │   ├── ProviderStats.swift        ← EXTRACTED
│   │   │   ├── SystemStatus.swift         ← EXTRACTED
│   │   │   └── ... (20+ model files)
│   │   ├── Configuration/
│   │   │   └── ServerConfiguration.swift  ← MOVED
│   │   └── Utilities/
│   │       ├── DesignTokens.swift         ← MOVED
│   │       ├── DateUtils.swift            ← MOVED
│   │       ├── StatusColors.swift         ← MOVED
│   │       ├── NumberFormatting.swift     ← MOVED
│   │       └── ViewFormatting.swift       ← MOVED
│   └── Tests/
│       └── DianeSharedTests/
│           └── ModelDecodingTests.swift   ← ported from macOS
│
├── DianeCompanion/                        ← macOS app (EXISTING)
│   ├── project.yml → depends on DianeShared
│   └── Sources/
│       ├── CompanionApp/                  ← macOS lifecycle, menus, update
│       │   ├── DianeCompanionApp.swift
│       │   ├── SettingsView.swift
│       │   ├── SelfTestManager.swift
│       │   ├── StatusView.swift
│       │   ├── UpdateChecker.swift
│       │   └── CLIManager.swift
│       ├── CompanionCore/                 ← macOS-specific
│       │   ├── DianeAPIClient.swift       ← localhost:8890 (KEEPS)
│       │   ├── APIServerManager.swift     ← launchd management (KEEPS)
│       │   ├── ConnectionState.swift      ← KEEPS
│       │   ├── StatusMonitor.swift        ← KEEPS
│       │   ├── ErrorReporter.swift        ← KEEPS
│       │   ├── ViewTracker.swift          ← KEEPS
│       │   ├── AppLogger.swift            ← KEEPS
│       │   └── Components/               ← KEEPS
│       └── Views/                         ← all macOS views (KEEPS)
│
└── DianeCompanion-iOS/                    ← NEW
    ├── project.yml → depends on DianeShared
    ├── DianeCompanion/Info.plist
    └── Sources/
        ├── App/
        │   └── DianeCompanionApp.swift    ← iOS lifecycle
        ├── Views/
        │   ├── ContentView.swift          ← TabView + NavigationStack
        │   ├── SessionListView.swift      ← chats list
        │   ├── ChatView.swift             ← ExyteChat + streaming
        │   ├── ChatViewModel.swift        ← SSE state management
        │   ├── AgentsView.swift           ← read-only agents list
        │   ├── MCPServersView.swift       ← read-only MCP servers
        │   ├── SchemaView.swift           ← schema browser
        │   ├── SystemView.swift           ← server status
        │   └── SettingsView.swift         ← URL, API key, theme
        └── Services/
            ├── NodeRegistrationService.swift  ← register as node
            └── PushNotificationService.swift  ← APNs registration + handling
```

### 2.3 Package.swift

```swift
// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "DianeShared",
    platforms: [
        .macOS(.v15),
        .iOS(.v18)
    ],
    products: [
        .library(name: "DianeShared", targets: ["DianeShared"]),
    ],
    dependencies: [
        .package(url: "https://github.com/getsentry/sentry-cocoa", from: "8.45.0"),
    ],
    targets: [
        .target(
            name: "DianeShared",
            dependencies: [
                .product(name: "Sentry", package: "sentry-cocoa"),
            ]
        ),
        .testTarget(
            name: "DianeSharedTests",
            dependencies: ["DianeShared"]
        ),
    ]
)
```

### 2.4 Migration Plan (macOS)

1. Create `Packages/DianeShared/` with Package.swift
2. Extract models into individual files under `Sources/Models/`
3. Create `HTTPClient.swift` — shared GET/POST/stream primitives
4. Move `EmergentAPIClient.swift` → `Sources/API/` (drops `ObservableObject`, uses `@Observable` or plain actor)
5. Move utilities: `DesignTokens`, `DateUtils`, `StatusColors`, `NumberFormatting`, `ViewFormatting`
6. Move `ServerConfiguration.swift` → `Sources/Configuration/`
7. Update macOS `project.yml`: add local package dep, remove duplicate sources
8. Add `import DianeShared` to all macOS files that reference moved types
9. Verify build + tests pass

### 2.5 Files Moved Out of macOS App (Removed from macOS target)

The following files are **deleted from the macOS app target** and replaced by `import DianeShared`:

From `CompanionCore/`:
- `EmergentAPIClient.swift`
- `ServerConfiguration.swift`
- `DesignTokens.swift`
- `DateUtils.swift`

From `Utils/`:
- `StatusColors.swift`
- `NumberFormatting.swift`
- `ViewFormatting.swift`

From `Models.swift`: All model types extracted. The macOS `Models.swift` becomes an empty file or is deleted entirely (models imported from package).

**Net reduction in macOS code:** ~2,500 lines removed from app target.

---

## 3. iOS App Structure

### 3.1 Navigation

iOS uses `TabView` with `NavigationStack` (not the macOS `NavigationSplitView` sidebar):

```
TabView {
    NavigationStack { SessionListView() }   .tab("Chats", chat-bubble)
    NavigationStack { AgentsListView() }    .tab("Agents", robot)
    NavigationStack { SystemStatusView() }  .tab("Status", server-rack)
    NavigationStack { SettingsView() }      .tab("Settings", gear)
}
```

### 3.2 App Launcher (App.swift)

```swift
@main
struct DianeCompanionApp: App {
    @State private var apiClient = EmergentAPIClient()
    @State private var config = ServerConfiguration()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environment(apiClient)
                .environment(config)
                .task { await startup() }
        }
    }

    private func startup() async {
        // 1. Register as a phone node
        try? await NodeRegistrationService.shared.register(apiClient: apiClient)

        // 2. Register for push notifications
        PushNotificationService.shared.register()

        // 3. Check reachability
        _ = await apiClient.checkReachability()
    }
}
```

### 3.3 Chat View (the core differentiator)

**Why ExyteChat over Stream:**

| Factor | ExyteChat | Stream Chat SDK |
|--------|-----------|-----------------|
| Backend dependency | None | Designed for Stream backend |
| Streaming | ✅ DraftMessage.append | Requires custom channel adapter |
| SwiftUI native | ✅ Pure SwiftUI | Mix of UIKit + SwiftUI |
| Source size | ~3 files | ~200 files |
| License | MIT | Proprietary (free tier) |
| Maintenance | Active, smaller | Active, large team |

**Integration pattern with our SSE streaming:**

```swift
// ChatViewModel.swift
@MainActor
@Observable
final class ChatViewModel {
    let apiClient: EmergentAPIClient

    var sessions: [DianeSession] = []
    var messages: [DianeMessage] = []
    var streamingMessageIndex: Int? = nil  // index of partial message

    func sendMessage(sessionID: String, content: String) async {
        // 1. Append user message immediately
        messages.append(userMessage)

        // 2. Add empty assistant message for streaming
        messages.append(assistantPlaceholder)
        streamingMessageIndex = messages.count - 1

        // 3. Stream response
        let stream = apiClient.streamACP(agentName: "diane-default",
                                          sessionID: sessionID,
                                          content: content)
        for try await event in stream {
            switch event.type {
            case "token":
                // Append to the last assistant message's content
                messages[streamingMessageIndex!].content += event.content ?? ""
            case "done":
                streamingMessageIndex = nil
            case "error":
                // Show error state
                streamingMessageIndex = nil
            default:
                break
            }
        }
    }
}
```

**ExyteChat integration:**

```swift
// ChatView.swift
import ExyteChat

struct ChatView: View {
    @Environment(ChatViewModel.self) private var vm
    let session: DianeSession

    var body: some View {
        ChatView(
            messages: vm.messages.map { $0.toExyteMessage() },
            didSendMessage: { draft in
                Task { await vm.sendMessage(sessionID: session.id, content: draft.text) }
            }
        )
    }
}
```

ExyteChat's `DraftMessage` type will be mapped to our `DianeMessage` model. The streaming works because ExyteChat supports updating messages by ID — as tokens arrive, we update the message's text in the view model, and ExyteChat re-renders the bubble.

### 3.4 Settings View

```swift
struct SettingsView: View {
    @Environment(ServerConfiguration.self) private var config
    @State private var serverURL: String = ""
    @State private var apiKey: String = ""

    var body: some View {
        Form {
            Section("Connection") {
                TextField("Server URL", text: $serverURL)
                    .textContentType(.URL)
                    .autocapitalization(.none)
                    .onChange(of: serverURL) { _, new in config.serverURL = new }

                SecureField("API Key", text: $apiKey)
                    .onChange(of: apiKey) { _, new in config.apiKey = new }

                Button("Test Connection") {
                    // EmergentAPIClient.checkReachability()
                }
            }
            Section("Device") {
                LabeledContent("Node ID", value: NodeRegistrationService.shared.instanceID)
                LabeledContent("Push Token", value: PushNotificationService.shared.token ?? "Not registered")
            }
            Section("About") {
                LabeledContent("Version", value: appVersion)
                LabeledContent("Build", value: buildNumber)
            }
        }
        .navigationTitle("Settings")
    }
}
```

---

## 4. iOS as a Passive Node

### 4.1 Node Registration

Every iOS device registers itself with the Memory Platform on first launch:

```http
POST /api/nodes
Content-Type: application/json
X-API-Key: <api_key>
X-Project-ID: <project_id>

{
    "instance_id": "ios-<device-uuid>",
    "node_type": "phone",
    "mode": "passive",
    "capabilities": ["push_notifications"],
    "metadata": {
        "device_name": "Mcj's iPhone",
        "device_model": "iPhone 17 Pro",
        "os_version": "iOS 18.5",
        "app_version": "1.0.0"
    }
}
```

Response:

```json
{
    "ok": true,
    "node_id": "ios-abc123",
    "push_endpoint": "https://memory.emergent-company.ai/api/push/register"
}
```

### 4.2 Node Types (Updated Schema)

| `node_type` | `mode` | Description | Examples |
|-------------|--------|-------------|---------|
| `master` | `active` | Primary agent node | `diane-master` |
| `slave` | `active` | Compute/relay node | `tool-test`, `production-relay` |
| `phone` | `passive` | iOS device (not always reachable) | `ios-abc123` |

**`passive` mode semantics:**
- Node is not expected to be continuously reachable
- Heartbeats are accepted but not required
- The node receives outbound pushes via APNs
- Inbound requests buffer until the node reconnects (or are delivered via push)

### 4.3 APNs Token Registration

After obtaining a device token from Apple, the app sends it:

```http
PUT /api/nodes/ios-<uuid>
Content-Type: application/json

{
    "push_token": "<apns-device-token>",
    "push_sandbox": false   // true for TestFlight
}
```

Heartbeat renewal (app in foreground):

```http
PUT /api/nodes/ios-<uuid>/heartbeat
{
    "status": "online",
    "last_seen": "2026-05-13T22:00:00Z",
    "push_token_valid": true
}
```

### 4.4 Server-Side: Push Router (NEW)

A new Go component in `server/cmd/diane/` or the Memory Platform itself:

```
PushRouter
├── NodePushTokenStore (SQLite or Memory Platform graph)
├── ApplePushSender (HTTP/2 + APNs)
│
├── HandlePushRequest(message, targetNodeID)
│   ├── Lookup node → get push token + environment (sandbox/prod)
│   ├── Build APS payload: { "alert": ..., "badge": N, "data": { "type": "...", ... } }
│   └── Send via Apple HTTP/2 API → return delivery status
│
└── PushDelegate for ADK agent runtime
    └── Agent.sendNotification(userID, text, data)
        └── → routes through PushRouter
```

**Apple Push API** is a single HTTP/2 POST per notification:

```
:method = POST
:scheme = https
:path = /3/device/<device-token>
authorization = bearer <provider-auth-token>
apns-topic = com.emergent-company.diane-companion

{
    "aps": {
        "alert": {
            "title": "Diane",
            "subtitle": "New message from Agent",
            "body": "Task completed: 150k tokens processed"
        },
        "badge": 1,
        "sound": "default",
        "mutable-content": 1
    },
    "data": {
        "session_id": "sess-abc",
        "type": "new_message"
    }
}
```

### 4.5 Node Lifecycle

```
App Install
  ↓
First Launch
  ├── Create UUID (persist in Keychain)
  ├── POST /api/nodes → register as phone node
  ├── Register for remote notifications (APNs)
  └── PUT /api/nodes/<id> → push_token + sandbox flag
  ↓
(App runs in foreground)
  ├── Heartbeat every 5 minutes
  ├── Normal API usage (fetch sessions, chat, etc.)
  └── Receive pushes → tap → deep link to session
  ↓
(App enters background)
  └── Final heartbeat → status: "background"
  ↓
(Terminated)
  └── No more heartbeats → status: "offline" (stale detection)
  ↓
(Push received while offline)
  └── APNs wakes app → badge increment
  ↓
(App opened from push)
  └── Deep link to session → auto-connect
```

---

## 5. Notification Flow

### 5.1 Agent-Initiated Push

```
┌─────────┐     ┌──────────────┐     ┌──────────┐     ┌───────┐
│ Agent    │     │ Memory       │     │ Apple    │     │ iOS   │
│ (ADK)    │     │ Platform     │     │ Push API │     │ Device│
└────┬────┘     └──────┬───────┘     └─────┬────┘     └───┬───┘
     │                  │                   │              │
     │  1. sendMessage  │                   │              │
     │  (userId, text)  │                   │              │
     ├─────────────────►│                   │              │
     │                  │                   │              │
     │  2. Lookup node  │                   │              │
     │  for user        │                   │              │
     │                  │                   │              │
     │  3. Build APS    │                   │              │
     │  payload         │                   │              │
     │                  │                   │              │
     │  4. POST /3/device/{token}          │              │
     │                  ├──────────────────►│              │
     │                  │                   │              │
     │  5. 200 OK       │                   │   6. Deliver │
     │                  │◄──────────────────┤  to device   │
     │                  │                   ├─────────────►│
     │                  │                   │              │
     │                  │                   │  7. Tap push │
     │                  │                   │  → Open app  │
     │                  │                   │  → Deep link │
     │                  │                   │  to session  │
```

### 5.2 Push Payload Types

| `type` in `data` | When | Action on tap |
|------------------|------|---------------|
| `new_message` | Agent sent a message in a session | Open ChatView for the session |
| `task_complete` | Long-running task finished | Open SystemView or show notification |
| `error` | Agent error needs attention | Open error details |
| `session_request` | Someone started a new session | Open SessionListView |
| `system_alert` | Server health issue | Open SystemView |

### 5.3 Badge Management

The platform tracks an unread counter per node. When a notification is delivered:
- `badge = unread_count`
- When the app opens and fetches sessions → `PUT /api/nodes/<id>/badge-read` → reset to 0
- Next push carries `badge: 0` to clear the badge

---

## 6. Chat UI Strategy

### 6.1 Library Recommendation: ExyteChat

**Swift Package:** `https://github.com/exyte/Chat` (MIT, ~3k stars)

**Why ExyteChat:**
- Pure SwiftUI — no UIViewRepresentable bridges
- Built-in streaming support via `DraftMessage.append(text:)`
- Typing indicator support — can show "Diane is thinking..."
- Configurable bubble styles (user vs assistant colors)
- Input bar with text + send button built in
- Lightweight: ~3 source files
- No backend dependency — we control all networking

**Alternative considered — custom SwiftUI:**
- More code to write (~500 lines for a basic chat UI with streaming)
- No typing indicator, no attachment support, no scroll management
- Better to use ExyteChat for the complex parts, customize the rest

### 6.2 Streaming Integration

```
ExyteChat's data model:
  ChatMessage(id: String, text: String, user: ChatUser, status: MessageStatus, ...)

Our data model:
  DianeMessage(id: String, role: String, content: String, ...)

Mapping:
  DianeMessage.role == "user"     → ChatUser.current
  DianeMessage.role == "assistant" → ChatUser(name: "Diane")
  DianeMessage.content             → ChatMessage.text
```

**During streaming:**
1. Insert a `ChatMessage` with `status: .sending` and empty text
2. As SSE tokens arrive, update the message's text in-place
3. ExyteChat re-renders the bubble with the updated content
4. On `type: "done"`, set `status: .sent`
5. On `type: "error"`, set `status: .failed` and show retry button

### 6.3 Session List (Stub/Preview)

Before the session list loads from the API, show:

```swift
struct SessionListView: View {
    @State private var sessions: [DianeSession] = []
    @State private var isLoading = true

    var body: some View {
        List {
            if isLoading {
                // Show 5 redacted placeholder rows
                ForEach(0..<5) { _ in
                    SessionListRow(session: redactedSession)
                        .redacted(reason: .placeholder)
                }
            } else {
                ForEach(sessions) { session in
                    NavigationLink(value: session) {
                        SessionListRow(session: session)
                    }
                }
            }
        }
        .navigationTitle("Chats")
        .task { await load() }
    }
}
```

---

## 7. Feature Matrix

### 7.1 v1.0 (Phase 1 + 2)

| Feature | macOS | iOS | Notes |
|---------|-------|-----|-------|
| Session list | ✅ | ✅ | Same data, different UI |
| Chat with streaming | ✅ | ✅ | Core feature — SSE via ExyteChat |
| Agent list (read-only) | ✅ | ✅ | List + status |
| MCP servers (read-only) | ✅ | ✅ | List + status badges |
| Schema browser (read-only) | ✅ | ✅ | Type tree + details |
| System status | ✅ | ✅ | Server info, reachability |
| Node registration (phone) | ❌ | ✅ | **NEW** |
| Push notifications | ❌ | ✅ | **NEW** |
| Server URL config | ✅ | ✅ | Simplified |
| API key config | ✅ | ✅ | Via Keychain |

### 7.2 v1.1 (Phase 3+)

| Feature | macOS | iOS | Notes |
|---------|-------|-----|-------|
| Agent → Phone push routing | ❌ | ✅ | Server-side work |
| Deep linking from push | ❌ | ✅ | Open session from notification |
| Badge management | ❌ | ✅ | Unread counts synced |
| iMessage/Share sheet integration | ❌ | ✅ | iOS-native sharing |
| Widget (session glance) | ❌ | ✅ | Home Screen widget |
| Apple Watch companion | ❌ | Future | Minimal — check notifications |
| iPad layout optimization | ❌ | ✅ | Sidebar + detail on larger screens |

### 7.3 What iOS Does NOT Do

- Run `diane serve` (no process management)
- Bundle the diane binary
- Auto-update via Sparkle (App Store handles this)
- AppleScript/Mac automation integration
- Local MCP server management
- Doctor self-test (no binary to test)
- Sentry error reporting from iOS (maybe later)
- Desktop-only views (RelayNodes, Onboarding, etc.)

---

## 8. Implementation Phases

### Phase 1: Foundation + Chat (estimate: ~4h)

**Goal:** Working iOS app that can list sessions and chat with streaming.

| Task | Description | Files |
|------|-------------|-------|
| 1.1 | Create `Packages/DianeShared/` with Package.swift | `Package.swift` |
| 1.2 | Extract models to `Sources/Models/` | 20+ model files |
| 1.3 | Extract `EmergentAPIClient.swift` → `Sources/API/` | `EmergentAPIClient.swift` |
| 1.4 | Create `HTTPClient.swift` with GET/POST/stream methods | `HTTPClient.swift` |
| 1.5 | Move utilities to `Sources/Utilities/` | 5 utility files |
| 1.6 | Move `ServerConfiguration.swift` | `ServerConfiguration.swift` |
| 1.7 | Update macOS `project.yml` to depend on DianeShared | `project.yml` |
| 1.8 | Add `import DianeShared` to macOS files | ~30 files |
| 1.9 | Create iOS `project.yml` | `DianeCompanion-iOS/project.yml` |
| 1.10 | Add ExyteChat dependency | `project.yml` |
| 1.11 | Build `DianeCompanionApp.swift` (iOS lifecycle) | `App/DianeCompanionApp.swift` |
| 1.12 | Build `ContentView.swift` (TabView) | `Views/ContentView.swift` |
| 1.13 | Build `SessionListView.swift` | `Views/SessionListView.swift` |
| 1.14 | Build `ChatViewModel.swift` + `ChatView.swift` (ExyteChat + SSE) | 2 files |
| 1.15 | Build `SettingsView.swift` (URL + API key) | `Views/SettingsView.swift` |
| 1.16 | Verify build + test chat flow end-to-end | — |

**Deliverable:** iOS app that connects to Memory Platform, lists sessions, and sends/receives messages via SSE streaming.

### Phase 2: Read-Only Views + Navigation (estimate: ~2h)

| Task | Description |
|------|-------------|
| 2.1 | Port AgentsView (read-only list) |
| 2.2 | Port MCPServersView (status badges) |
| 2.3 | Port SchemaView + SchemaTypeDetailView |
| 2.4 | Port SystemView (simplified) |
| 2.5 | Wire TabView with all 4 tabs |
| 2.6 | Add pull-to-refresh on list views |
| 2.7 | Error states: offline banner, retry buttons |

### Phase 3: Node Registration + Push (estimate: ~3h)

| Task | Description | Where |
|------|-------------|-------|
| 3.1 | Implement NodeRegistrationService (register, heartbeat) | iOS app |
| 3.2 | Keychain-based device UUID persistence | iOS app |
| 3.3 | Implement PushNotificationService (APNs registration + token send) | iOS app |
| 3.4 | Handle push payload → deep link to session | iOS app |
| 3.5 | Server-side: PushRouter component (Apple HTTP/2 client) | Go server |
| 3.6 | Server-side: node → push token lookup by user | Go server |
| 3.7 | Agent SDK integration route (`agent.sendNotification`) | Go/ADK |

### Phase 4: Polish (estimate: ~2h)

| Task | Description |
|------|-------------|
| 4.1 | Dark mode support |
| 4.2 | Dynamic type support |
| 4.3 | Offline state (no connection, show cached sessions) |
| 4.4 | Badge management (unread count sync) |
| 4.5 | iPad: sidebar + detail layout |
| 4.6 | Launch screen + app icon |

### Phase 5: Release (estimate: ~1h)

| Task | Description |
|------|-------------|
| 5.1 | TestFlight distribution (Xcode Cloud or manual) |
| 5.2 | CI: GitHub Actions build iOS target |
| 5.3 | Sentry SDK for iOS crash reporting |
| 5.4 | App Store submission |

---

## 9. Gap Analysis — Decisions Made

The following gaps were identified and resolved with default decisions based on commonly shipped AI assistant patterns (ChatGPT, Claude), latest iOS 18 APIs, and pragmatic simplicity for v1.

### 🔴 Critical — Resolved

| # | Gap | Decision | Rationale |
|---|-----|----------|-----------|
| **G1** | **Chat streaming endpoint** — ACP vs HTTP SSE | **HTTP SSE via configurable remote Diane server URL.** The iOS app connects to the user's Diane server (same as macOS, just remote instead of localhost). The existing `POST /api/chat/stream` SSE endpoint on the Diane server handles streaming. `EmergentAPIClient` is only used for read-only cloud data. This matches the macOS pattern of dual API clients. | Most AI chat apps use HTTP SSE — simpler, works on cellular, reconnects naturally. No WebSocket complexity. The Diane server already has this endpoint (used by macOS). |
| **G2** | **APNs provider authentication** | **Token-based auth with a `.p8` key.** Generated once from Apple Developer Console by the developer. Stored as an environment variable (`APNS_P8_KEY`) on the master node, managed via Ansible. The server reads it at startup and uses it for both sandbox (TestFlight) and production (App Store). | Apple recommends token auth over certificates — never expires, one key for all environments. Environment variable avoids secrets in code. |
| **G3** | **Multi-device push routing** | **Push to ALL registered phone nodes.** If you have iPhone + iPad, both receive the notification. No "primary device" concept in v1. Remove a device manually from Settings if unwanted. | Matches ChatGPT/Claude behavior — they deliver to every signed-in device. |
| **G4** | **Session space: shared or separate?** | **Fully shared.** A chat started on iPhone appears on macOS and vice versa. Messages from desktop agents render as read-only on iOS. | Matches ChatGPT/Claude — conversations sync across all devices. Enables seamless handoff. |
| **G5** | **APNs 4KB payload limit** | **Truncate to 150 characters.** Push body = first 150 chars of the message, `"…"` appended if truncated. `data.session_id` = the session ID for deep linking. User taps to open app → fetch full message list. No Notification Service Extension in v1. | 150 chars is enough context to decide if the notification matters. Full content is always available in-app. Standard AI assistant pattern. |
| **G6** | **Dual-format decoder in shared package** | **Keep the dual decoder.** The `DianeSession`/`DianeMessage` `init(from:)` decoder handles both flat (local Diane API) and graph (Memory Platform relay) formats. iOS connects via Diane API so it receives flat format; dual decoder costs nothing. | A few lines of `do/try/catch` in the decoder. Zero runtime cost on the happy path. Avoids maintaining a second decoder. |

### 🟡 Important — Resolved

| # | Gap | Decision | Rationale |
|---|-----|----------|-----------|
| **G7** | **Phone node authentication** | **Same API key as the master.** User types their Diane API key in iOS Settings (same key `diane serve` uses for Memory Platform auth). Stored in Keychain with Secure Enclave protection. | Simplest pattern. Matches every API-based mobile app (Termius, TablePlus, etc.). No QR code or per-device registration needed for v1. |
| **G8** | **New chat agent selection** | **Hardcode `diane-default` for v1.** No agent picker in the initial release. The user's default Diane agent handles all iOS-initiated chats. | Matches the macOS `SessionsView` default. Agent picker is a future addition after the core chat experience ships. |
| **G9** | **Push token re-registration** | **Compare old vs new token on every registration callback.** In `application(_:didRegisterForRemoteNotificationsWithDeviceToken:)`, hash the token and compare with Keychain-stored value. If different → `PUT /api/nodes/<id>` with new token. | Simple, reliable, handles all token invalidation cases automatically. |
| **G10** | **API key error recovery** | **Two-tier: (1) On empty/no key → modal overlay "Configure API Key" sheet blocks the app until set. (2) On 401 during use → inline banner with "Update Key" button → opens Settings.** | Common app pattern (Slack, WhatsApp login gating). Prevents launching into a useless empty state. |
| **G11** | **Deep linking URL scheme** | **`diane://session/<id>` for v1.** Register `diane://` as a custom URL scheme in Info.plist. Parse `session/<id>` from push tap → navigate to ChatView. Universal Links in a future phase. | Zero server-side setup. Works immediately. Deliver notifications as silent pushes that contain the session ID in the payload. |

### 🔵 Should-Specify — Resolved

| # | Gap | Decision | Rationale |
|---|-----|----------|-----------|
| **G12** | **Background heartbeat reliability** | **Foreground-only heartbeats.** Send heartbeat in `scenePhase.active`. On entering background, send final heartbeat with `status: "background"`. Server marks `stale` after 7 days of no heartbeat. | No point fighting iOS background limits. Foreground-only is reliable, simple, and matches how most mobile apps report presence. |
| **G13** | **Test strategy** | **Two tiers: (1) DianeShared package unit tests — model decoding, API client mock tests (same pattern as macOS, share `ModelDecodingTests.swift`). (2) iOS app unit tests — `ChatViewModel` tests with mock API, `SessionListViewModel` tests. SwiftUI Previews for visual iteration. No XCUITest in v1.** | Proven pattern from macOS (367 tests). Preview-based development is fast. XCUITest adds complexity without CI runner benefit. |
| **G14** | **Keychain access group** | **Define App Group `group.com.emergent-company.diane-companion` from day one.** Use it in all Keychain queries (`SecAttrAccessGroup`). The group is registered in the app's entitlements file. | Extensions (widgets, Notification Service) need access to the same Keychain items. Adding it later requires migration. |
| **G15** | **Stale node cleanup** | **Server-side: no heartbeat for 7 days → `status: stale`. After 30 days → `status: inactive`. On APNs 410 (Unregistered) → auto-remove push token. iOS Settings has "De-register Device" button → `DELETE /api/nodes/<id>`.** | No clean uninstall detection on iOS. TTL-based cleanup is standard. Manual de-registration for lost/stolen devices. |
| **G16** | **ExyteChat min iOS version** | **Verify before Phase 1.** ExyteChat is actively maintained and likely supports iOS 16+. Given our target iOS 18.0+ for Swift 6, compatibility is virtually certain. | Quick verification step before starting implementation. |
| **G17** | **Notification Service Extension** | **Skip for v1.** Push notifications will contain basic text only. `mutable-content: 1` flag is NOT set. Rich media / content modification is a Phase 4+ addition. | Adding an extension target requires its own `project.yml` entry and code signing setup. Not worth the complexity for basic text notifications. |

---

## 10. Updated Spec Summary (Decisions Applied)

### Platform Target

| Key | Value |
|-----|-------|
| Minimum iOS | **18.0** (Swift 6, latest APIs) |
| Minimum macOS | 15.0 (unchanged) |

### Connection Architecture

```
iOS App
  ├─ DianeAPIClient → https://<user-diane-server>/api/...  (chat, sessions, agents, MCP)
  └─ EmergentAPIClient → https://memory.emergent-company.ai/api/...  (read-only cloud data)
```

The user configures their Diane server URL in Settings (e.g. `https://diane.emergent-company.ai`). The Memory Platform URL is hardcoded as the secondary data source for schema/system data.

### Auth

- **API key**: User's existing Diane Memory Platform key, typed into Settings
- **Storage**: Keychain with Secure Enclave protection, App Group access
- **Server side**: Same key authenticates both phone node registration and all API calls

### Chat

- **Protocol**: HTTP SSE (`POST /api/chat/stream`)
- **Agent**: `diane-default` hardcoded for v1
- **Session space**: Fully shared with macOS/desktop
- **Library**: ExyteChat with default bubble styling (user = system blue, assistant = secondary)

### Push Notifications

- **Auth**: p8 key → env variable → Ansible-managed
- **Routing**: Push to all registered phone nodes
- **Payload**: 150-char truncated body + session_id for deep linking
- **Deep link**: `diane://session/<id>` custom URL scheme
- **Extension**: No Notification Service Extension in v1
- **Heartbeat**: Foreground-only, server staleness at 7d / 30d

### Architecture Decisions

- **Dual-format model decoder**: Keep in shared package
- **Keychain Access Group**: Defined from day one
- **iOS target**: 18.0, Swift 6, strict concurrency
- **iPad**: TabView-only for v1, NavigationSplitView later
- **Testing**: Shared package unit tests + iOS ViewModel tests + Previews. No XCUITest v1.

---

## 11. Appendix: API Surface

### 11.1 Existing API Endpoints Consumed by iOS

| Method | Path | Source | Used For |
|--------|------|--------|----------|
| GET | `/api/status` | DianeAPIClient | Reachability + server info |
| GET | `/api/sessions` | DianeAPIClient | Session list |
| GET | `/api/sessions/:id/messages` | DianeAPIClient | Chat history |
| POST | `/api/chat/stream` (SSE) | DianeAPIClient | Streaming chat |
| POST | `/api/sessions/create` | DianeAPIClient | New chat |
| GET | `/api/agents` | DianeAPIClient | Agents list (read-only) |
| GET | `/api/mcp-servers` | DianeAPIClient | MCP servers list |
| GET | `/api/schema` | EmergentAPIClient | Schema browser |
| GET | `/api/providers/stats` | EmergentAPIClient | Provider stats |

### 11.2 New API Endpoints (for Node + Push)

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/nodes` | Register iOS device as phone node |
| PUT | `/api/nodes/:id` | Update push token + metadata |
| PUT | `/api/nodes/:id/heartbeat` | Node status heartbeat |
| PUT | `/api/nodes/:id/badge-read` | Reset unread badge count |
| POST | `/api/push/send` | (server-internal) Send push to a registered node |

### 11.3 iOS App Info

| Key | Value |
|-----|-------|
| Bundle ID | `com.emergent-company.diane-companion` |
| Deployment target | iOS 18.0 |
| Swift version | 6.0 (strict concurrency) |
| Primary framework | SwiftUI |
| Chat library | [ExyteChat](https://github.com/exyte/Chat) |
| Push notifications | Apple Push Notification Service |
| Build system | xcodegen + Xcode 16+ |
| Distribution | TestFlight → App Store |

---

## 12. Third-Pass Gaps — Decisions Made

### 🔴 Critical (Blocks Phase 1)

| # | Gap | Decision | Rationale |
|---|-----|----------|-----------|
| **M1** | **Remote DianeAPIClient — where does it live?** | **`RemoteDianeAPIClient` in the iOS target.** Extract an `HTTPClient` base (GET/POST/SSE stream primitives) into the shared package — both `DianeAPIClient` (macOS) and `RemoteDianeAPIClient` (iOS) use it. The iOS client reads its base URL from `ServerConfiguration.baseURL`. The macOS `DianeAPIClient` stays hardcoded to `localhost:8890` and keeps launchd lifecycle management in the macOS target. | Keeps the shared package clean (only `HTTPClient` primitives + `EmergentAPIClient`). iOS gets its own client with mobile-specific retry logic. No risk of breaking macOS. |
| **M2** | **iOS project.yml missing** | See full specification below. | — |
| **M3** | **Info.plist and entitlements unspecified** | See full specification below. | — |
| **M4** | **DianeAPIClient streaming method undefined for iOS** | **`RemoteDianeAPIClient.streamChatMessage()`** — POST to `{baseURL}/api/chat/stream`, read SSE `data:` lines, yield `StreamChatEvent` via `AsyncThrowingStream`. Same pattern as macOS `EmergentAPIClient.streamACP()` but over plain HTTP SSE, not ACP/WebSocket. This method is the iOS counterpart of the macOS `DianeAPIClient.sendChatMessage()` but for streaming. | The Diane server already has `POST /api/chat/stream` returning SSE. The client just needs to read it. Standard URLSession bytes streaming on iOS 18. |

### 🟡 Important

| # | Gap | Decision | Rationale |
|---|-----|----------|-----------|
| **M5** | **Sentry for iOS — DSN + initialization** | **New `apple-ios` project in the existing `diane-6t` org (de.sentry.io).** `SentrySDK.start()` in `DianeCompanionApp.swift` with iOS-specific DSN. `tracesSampleRate: 0.5` (mobile data is less forgiving than desktop 1.0). `sendDefaultPii: true`. Same Sentry dependency from shared package — only the DSN and start call are app-level. | Single org for all Diane errors. Lower sample rate to respect mobile data. Shared package already has Sentry as a dep. |
| **M6** | **ServerConfiguration App Group UserDefaults** | **Add optional `userDefaults:` parameter to `ServerConfiguration.init()`.** Default: `UserDefaults.standard`. iOS passes `UserDefaults(suiteName: "group.com.emergent-company.diane-companion")`. macOS stays unchanged (`.standard`). The `DianeCompanionApp.swift` creates the config with the suite parameter. | Zero risk to macOS. Enables Keychain sharing for future extensions. |
| **M7** | **SSE reconnection on mobile networks** | **Manual retry only.** On SSE disconnect mid-stream: show partial message + inline "Connection interrupted — tap to retry" button. User taps → re-POSTs the message → resumes from scratch (new SSE stream). Max 2 automatic retries for transient network errors before showing the retry UI. No retry on app backgrounding. | ChatGPT mobile pattern. Automatic retry is unreliable on cellular (same connectivity that caused the drop). Let the user decide when to retry. |
| **M8** | **Loading states per view** | **SessionList**: 5 redacted placeholder rows (specified). **Chat**: progress view → message list. **Agents/MCP/Schema/System**: standard `ProgressView` on first load, then content + pull-to-refresh. **No pagination** in v1 — load all sessions/messages. Add lazy loading in Phase 4. | Redacted placeholders feel native. No pagination keeps v1 simple (most users have <50 sessions). |
| **M9** | **Read-only UX detail for agents/MCP servers** | **Agents**: list (name + flow type + status badge). Tap → detail view (name, description, model config — all read-only text labels). **MCP Servers**: list (name + status badge). Tap → detail view (tools list with names + descriptions, no invocation). No edit/create/delete anywhere. | Matches macOS read-only display pattern. Mobile "view only" is standard for companion apps. |

### 🔵 Should-Specify

| # | Gap | Decision | Rationale |
|---|-----|----------|-----------|
| **M10** | **iOS Bundle ID** | **`com.emergent-company.diane-companion.ios`** | Apple requires unique bundle IDs per platform within the same developer account. |
| **M11** | **Push: user permission flow** | Prompt once on first launch after API key is configured: `requestAuthorization([.alert, .badge, .sound])`. If denied → show "Notifications off — enable in Settings" in the Settings view (with link to iOS Settings). If authorized → `registerForRemoteNotifications()` → `PUT /api/nodes/<id>` with token. | Standard iOS permission flow. Apple doesn't allow re-prompting. |
| **M12** | **Notification triggering logic** | **Push is sent when a new message arrives in a session AND the app is not in the foreground.** The Diane server tracks the app's last heartbeat. If `status: active` (foreground) → no push (user sees it live via SSE). If `status: background` or no recent heartbeat → send push. Future: also push for explicit `agent.sendNotification()`. | Avoids duplicate notifications when the user is actively chatting. Simple threshold-based logic. |
| **M13** | **Multi-agent sessions in shared list** | All sessions appear in the list regardless of agent. Tapping a session from a non-default agent: messages render as read-only (same bubble styling), no send bar. The session detail header shows the agent name. User can read but not reply. | Transparent — user sees all their conversations. Read-only for non-default agents avoids confusion. |
| **M14** | **Phone de-registration endpoint** | **`DELETE /api/nodes/:id`** — new Go handler on the Diane server. Removes node from registry, clears push token. Returns 200 on success, 404 if node not found. Part of Phase 3 server-side work. | Small handler, standard REST pattern. |
| **M15** | **Test coverage target** | **v1 target: 30-40 unit tests.** Breakdown: 15 model decoding tests (shared package), 10 `ChatViewModel` tests (with mock API), 5 `SessionListViewModel` tests, 5 formatting/utility tests. No coverage percentage target — ship what's tested. | Pragmatic. macOS has 367 tests accumulated over months. 30-40 is realistic for the initial iOS release. |
| **M16** | **iOS build tool** | **xcodegen** — same as macOS. `project.yml` in `DianeCompanion-iOS/`. `xcodegen generate` creates `Diane.xcodeproj` or a separate iOS project. | Consistent toolchain. User already has xcodegen installed. |

---

## 13. iOS project.yml Specification

```yaml
name: Diane
options:
  bundleIdPrefix: com.emergent-company.diane-companion
  deploymentTarget:
    iOS: "18.0"
  xcodeVersion: "16.0"
  generateEmptyDirectories: true
  indentWidth: 4
  tabWidth: 4

settings:
    base:
        SWIFT_VERSION: "6.0"
        IPHONEOS_DEPLOYMENT_TARGET: "18.0"
        PRODUCT_BUNDLE_IDENTIFIER: com.emergent-company.diane-companion.ios
        DEVELOPMENT_TEAM: ""
        CODE_SIGN_STYLE: Manual
        CODE_SIGN_IDENTITY: "Apple Development"
        ENABLE_HARDENED_RUNTIME: NO
        MARKETING_VERSION: 1.0.0
        CURRENT_PROJECT_VERSION: 1

packages:
  DianeShared:
    path: ../Packages/DianeShared
  ExyteChat:
    url: https://github.com/exyte/Chat
    from: 0.0.0  # resolve to latest compatible

targets:
  Diane:
    type: application
    platform: iOS
    deploymentTarget: "18.0"
    sources:
      - path: Sources
    dependencies:
      - package: DianeShared
        product: DianeShared
      - package: ExyteChat
        product: Chat
    settings:
      base:
        PRODUCT_NAME: Diane
        INFOPLIST_FILE: DianeCompanion/Info.plist
        CODE_SIGN_ENTITLEMENTS: DianeCompanion/DianeCompanion.entitlements
        ASSETCATALOG_COMPILER_APPICON_NAME: AppIcon
        SUPPORTED_PLATFORMS: "iphoneos iphonesimulator"
        TARGETED_DEVICE_FAMILY: "1,2"  # iPhone + iPad

  DianeTests:
    type: bundle.unit-test
    platform: iOS
    deploymentTarget: "18.0"
    sources:
      - path: DianeTests
    dependencies:
      - target: Diane
    settings:
      base:
        INFOPLIST_FILE: DianeTests/Info.plist
        GENERATE_INFOPLIST_FILE: YES
```

**Note:** ExyteChat version `from: 0.0.0` should be pinned to the actual latest release during Phase 1 after verifying compatibility.

---

## 14. Info.plist & Entitlements Specification

### Info.plist

```xml
<key>CFBundleDisplayName</key>
<string>Diane</string>

<key>CFBundleURLTypes</key>
<array>
    <dict>
        <key>CFBundleURLSchemes</key>
        <array>
            <string>diane</string>
        </array>
    </dict>
</array>

<key>UILaunchScreen</key>
<dict/>

<key>UISupportedInterfaceOrientations</key>
<array>
    <string>UIInterfaceOrientationPortrait</string>
    <string>UIInterfaceOrientationLandscapeLeft</string>
    <string>UIInterfaceOrientationLandscapeRight</string>
</array>

<key>UISupportedInterfaceOrientations~ipad</key>
<array>
    <string>UIInterfaceOrientationPortrait</string>
    <string>UIInterfaceOrientationLandscapeLeft</string>
    <string>UIInterfaceOrientationLandscapeRight</string>
</array>
```

**ATS note:** No arbitrary loads exception. iOS app requires the user's Diane server to be HTTPS. If users need HTTP for local development, they can add an exception manually or the app can detect this and show a warning.

### Entitlements (`DianeCompanion.entitlements`)

```xml
<key>keychain-access-groups</key>
<array>
    <string>$(AppIdentifierPrefix)com.emergent-company.diane-companion.ios</string>
</array>

<key>aps-environment</key>
<string>production</string>
```

**Note:** `aps-environment: production` works for both TestFlight (sandbox APNs) and App Store (production APNs) — Apple determines the environment from the provisioning profile.

---

## 15. iOS App Startup Sequence

```
1. SentrySDK.start() with iOS DSN
2. Initialize ServerConfiguration(userDefaults: AppGroupUserDefaults)
3. Initialize RemoteDianeAPIClient(baseURL: config.serverURL)
4. Check API key exists:
   ├─ No key → show "Configure API Key" modal sheet
   │           sheet blocks UI until key is entered
   │           test connection → save to Keychain
   └─ Key exists → proceed
5. Register as phone node:
   ├─ Generate UUID on first launch (persist in Keychain)
   ├─ POST /api/nodes → register
   └─ On subsequent launches: PUT /api/nodes/:id/heartbeat
6. Push notifications:
   ├─ requestAuthorization([.alert, .badge, .sound])
   ├─ If authorized: registerForRemoteNotifications()
   └─ Send token to server: PUT /api/nodes/:id
7. Show ContentView (TabView with all tabs) — start loading data
```

---

## 16. Key Data Types

| Class/Struct | Location | Purpose |
|-------------|----------|---------|
| `HTTPClient` | DianeShared/Sources/API | URLSession GET/POST/SSE stream primitives. No Diane-specific logic. |
| `EmergentAPIClient` | DianeShared/Sources/API | Cloud Memory Platform API (schema, providers, system). Uses `HTTPClient`. |
| `RemoteDianeAPIClient` | iOS/Sources/Services | User's Diane server API (chat streaming, sessions, agents, MCP). Uses `HTTPClient`. |
| `ServerConfiguration` | DianeShared/Sources/Configuration | UserDefaults-backed store for serverURL + apiKey. Accepts custom `UserDefaults` suite. |
| `ChatViewModel` | iOS/Sources/Views | `@Observable` managing sessions list + messages + SSE streaming state. |
| `NodeRegistrationService` | iOS/Sources/Services | Singleton: node registration, heartbeat, de-registration. UUID in Keychain. |
| `PushNotificationService` | iOS/Sources/Services | Singleton: APNs registration, token management, deep link parsing. |

---

## 17. Total File & LOC Estimate

| Bucket | Files | LOC (approx) | Type |
|--------|-------|--------------|------|
| **DianeShared package** (extracted from macOS) | 25+ | ~2,500 | Existing, moved |
| **New iOS Swift files** | 15 | ~1,500 | New code |
| **project.yml, Info.plist, entitlements** | 3 | ~150 | Config |
| **Total** | **~43** | **~4,150** | — |

Key new iOS files:
4 views (ContentView, SessionListView, ChatView, SettingsView) = ~800 LOC
2 ViewModels (ChatViewModel, SessionListViewModel) = ~300 LOC
2 services (NodeRegistration, PushNotification) = ~250 LOC
1 API client (RemoteDianeAPIClient) = ~150 LOC
Total new: ~1,500 LOC

---

## 18. UI & Navigation Design

### 18.1 Design Language

| Element | Choice | Rationale |
|---------|--------|-----------|
| **Tone** | Clean, native iOS — system fonts, SF Symbols, standard navigation patterns | Feels like a first-class Apple app, not a port |
| **Background** | `.systemGroupedBackground` (light) / `.systemGroupedBackground` (dark) | Standard iOS grouping for list-heavy apps |
| **Accent color** | System accent (iOS blue) — no custom brand color in v1 | Native feel, free dark mode adaptation |
| **Typography** | SF Pro (default), `.body` for content, `.caption` for metadata, `.footnote` for timestamps | System default = best readability |
| **Borders** | `.separator` color, hairline (0.33pt) where needed | Minimal chrome |
| **Card style** | `.background` (secondary) + rounded corners (12pt) + subtle shadow | For metadata panels and status cards |

### 18.2 Navigation Architecture

```
TabView (bottom tab bar, 4 tabs)
│
├── Tab 1: Chats 💬
│   └── NavigationStack
│       ├── SessionListView (root)
│       │   ├── tap → ChatView(session)
│       │   ├── swipe → delete session
│       │   └── + button → NewSessionView (inline)
│       └── ChatView(session)
│           ├── messages list (scrollable, newest at bottom)
│           ├── input bar (pinned to bottom)
│           └── session metadata header (collapsible)
│
├── Tab 2: Agents 🤖
│   └── NavigationStack
│       ├── AgentsListView (root)
│       └── AgentDetailView(agent) — read-only labels
│
├── Tab 3: Status 📡
│   └── NavigationStack
│       ├── StatusView (root) — server info, reachability
│       ├── MCPServersView → MCPDetailView
│       └── SchemaView → SchemaTypeDetailView
│
└── Tab 4: Settings ⚙️
    └── NavigationStack
        └── SettingsView (root, single scrolling form)
```

### 18.3 Tab Bar

| Tab | Title | SF Symbol | Badge |
|-----|-------|-----------|-------|
| 1 | Chats | `message.fill` | Unread count |
| 2 | Agents | `brain.head.profile` | None |
| 3 | Status | `antenna.radiowaves.left.and.right` | Connection indicator |
| 4 | Settings | `gearshape.fill` | None |

**Note:** `brain.head.profile` is an iOS 18+ SF Symbol. If targeting earlier, fallback to `robot` or `person.circle`.

```swift
TabView(selection: $selectedTab) {
    NavigationStack { SessionListView() }
        .tabItem { Label("Chats", systemImage: "message.fill") }
        .tag(Tab.chats)

    NavigationStack { AgentsListView() }
        .tabItem { Label("Agents", systemImage: "brain.head.profile") }
        .tag(Tab.agents)

    NavigationStack { StatusView() }
        .tabItem { Label("Status", systemImage: "antenna.radiowaves.left.and.right") }
        .tag(Tab.status)

    NavigationStack { SettingsView() }
        .tabItem { Label("Settings", systemImage: "gearshape.fill") }
        .tag(Tab.settings)
}
```

### 18.4 Screen-by-Screen

#### 18.4.1 SessionListView

```
┌────────────────────────────────────────┐
│  ← Chats                     [+]      │  ← nav title + new chat button
├────────────────────────────────────────┤
│  Search sessions...                    │  ← search bar (optional)
├────────────────────────────────────────┤
│  ○ "How to deploy the new..." 2m ago   │  ← session row
│    diane-master · 12 messages          │
├────────────────────────────────────────┤
│  ○ "Fix the database migra..." 1h ago  │
│    diane-master · 5 messages           │
├────────────────────────────────────────┤
│  ○ "Review PR #423"           3h ago   │
│    tool-test · 8 messages              │
├────────────────────────────────────────┤
│  ○ "Set up monitoring fo..."  yesterday│
│    diane-master · 23 messages          │
├────────────────────────────────────────┤
│            [No more sessions]           │
└────────────────────────────────────────┘
```

**Session row layout:**
```
[status dot] [title (1 line, truncated)] [relative timestamp]
             [agent name · message count]                ← .caption, secondary
```

- **Status dot:** Circle.fill — green (active), secondary (archived), orange (error)
- **Tap:** push ChatView
- **Swipe left:** Delete confirmation (with haptic)
- **Long press:** Copy session ID, share session

**Empty state:**
```
          💬
    No Conversations Yet
    Start a chat with your Diane agent
    to see your sessions here.

        [ New Chat ]
```

**Loading state (preview):**
```
[──────────────]  ← 5 redacted pill-shaped rows
[──────────────]     shimmer animation
[──────────────]
[──────────────]
[──────────────]
```

#### 18.4.2 ChatView

```
┌────────────────────────────────────────┐
│  ← Chats           How to deploy...  │  ← session title (nav bar)
│                     diane-master      │  ← agent name (subtitle)
├────────────────────────────────────────┤
│                                        │
│   Today                               │  ← section header
│                                        │
│  ┌──────────────────────┐             │
│  │ How do I deploy the  │             │  ← user bubble (right)
│  │ new monitoring stack?│             │     blue bg, white text
│  └──────────────────────┘             │     .body, rounded 16pt
│                                        │
│  ┌────────────────────────────────┐   │
│  │ Let me check the current      │   │  ← assistant bubble (left)
│  │ deployment status...          │   │     secondary bg, primary text
│  │                                │   │
│  │  Checking running services   │   │  ← tool call (monospaced, .footnote)
│  │  Analyzing config files      │   │
│  │                                │   │
│  │ Here's what I found:          │   │
│  │ The monitoring stack needs    │   │
│  │ Prometheus + Grafana...       │   │
│  └────────────────────────────────┘   │
│                                        │
│  ┌──────────────────────┐             │
│  │ Can you set it up?   │             │
│  └──────────────────────┘             │
│                                        │
│  ┌───────────────────────────────┐    │
│  │ Deploying monitoring...      │    │
│  │ ⚪⚪⚪⚪⚪⚪⚪⚪⚪⚪ 60%         │    │  ← streaming (pulsing cursor)
│  └───────────────────────────────┘    │
│                                        │
├────────────────────────────────────────┤
│  💬 Message Diane...           ▶️  │  ← input bar (pinned to bottom)
│  ───────────────────────────────       │
└────────────────────────────────────────┘
```

**Chat bubble rules:**

| Bubble | Alignment | Background | Text color | Corner radius |
|--------|-----------|-----------|------------|---------------|
| User | `.trailing` | `.accentColor` (blue) | `.white` | 16pt top/leading, 4pt bottom/trailing |
| Assistant | `.leading` | `.secondarySystemBackground` | `.primary` | 16pt top/trailing, 4pt bottom/leading |
| Error | `.leading` | `.systemRed.opacity(0.1)` | `.red` | Same as assistant |
| System (tool call) | `.leading` | `.clear` (monospaced) | `.secondary` | No bubble, `.footnote.monospaced` |

**Streaming indicator:**
- While SSE is active: assistant bubble shows a **pulsing vertical cursor** (`▍`) at the end of the text
- Animation: opacity 1.0 → 0.3 → 1.0, 0.8s cycle
- On `type: "done"`: cursor disappears, message finalized

**Reasoning / thinking sections** (when agent exposes reasoning):
```
┌─────────────────────────────────────────┐
│ 💭 Analyzing deployment topology...     │  ← .footnote, secondary text,
│                                         │     collapsible on tap
│ The user's stack runs on 3 nodes...     │
│ Let me check each service...            │
└─────────────────────────────────────────┘
```

**Tool call display:**
```
🔧 FetchDeploymentStatus("monitoring")    ← .footnote.monospaced
```
- Shows tool name + truncated arguments
- Tap to expand full tool call/response

**Input bar:**
```
┌──────────────────────────────────┬────┐
│ 💬 Message Diane...              │ ▶️ │
└──────────────────────────────────┴────┘
```
- `.body` text field, no border (inset background)
- Send button: `arrow.up.circle.fill`, disabled when text is empty
- When streaming is active: input bar shows "Waiting for response..." and send button is replaced by a stop button (`stop.circle.fill`)
- Disables autocapitalization for quick commands (but keeps spellcheck)

#### 18.4.3 AgentsListView

```
┌────────────────────────────────────────┐
│  ← Agents                              │
├────────────────────────────────────────┤
│                                        │
│  diane-master                          │  ← agent name (title)
│  Agent · Running            🟢 Active  │  ← flow type + status badge
│  Default agent for all general tasks   │  ← description (.caption)
├────────────────────────────────────────┤
│                                        │
│  tool-test                             │
│  Agent · Running            🟢 Active  │
│  Compute node for tool execution       │
├────────────────────────────────────────┤
│                                        │
│  production-relay                      │
│  Slave · Idle              🟡 Idle     │
│  Relays MCP calls to production infra  │
├────────────────────────────────────────┤
│                                        │
│  mcj-iphone (NEW)                      │
│  Phone · Passive          🔵 Online    │
│  iOS companion — push notifications    │
└────────────────────────────────────────┘
```

**Agent detail view (tap to push):**

```
┌────────────────────────────────────────┐
│  ← Agents                diane-master  │
├────────────────────────────────────────┤
│                                        │
│  Flow Type          Agent              │
│  Status             🟢 Running         │
│  Model              gpt-4o             │   ← all read-only labels
│  Provider           openai             │
│  Created            2026-01-15         │
│                                        │
│  Description                           │
│  ─────────────────────────────────     │
│  Default agent for all general         │
│  tasks including chat, analysis,       │
│  and tool orchestration.              │
│                                        │
│  System Prompt                         │
│  ─────────────────────────────────     │
│  You are Diane, a helpful AI          │
│  assistant... (truncated)             │
│                                        │
│  Capabilities                          │   ← list of badge pills
│  ┌──────┐ ┌──────┐ ┌──────┐          │
│  │ Chat │ │ Tools │ │ Code │          │
│  └──────┘ └──────┘ └──────┘          │
└────────────────────────────────────────┘
```

#### 18.4.4 StatusView

```
┌────────────────────────────────────────┐
│  ← Status                              │
├────────────────────────────────────────┤
│  📡 Connection                         │
│  ─────────────────────────────────     │
│  Server      🟢 Online                 │
│  URL         https://diane.emergent... │
│  Version     1.38.68                   │
│  Started     2h ago                    │
│                                        │
│  📱 Device                             │
│  ─────────────────────────────────     │
│  Node ID     ios-abc123                │
│  Status      🟢 Registered             │
│  Push Token  ✅ Active                 │
│                                        │
│  🔌 MCP Servers                        │  ← tappable row → MCP list
│  ─────────────────────────────────     │
│  infakt       ✅ Connected             │
│  sentry       ✅ Connected             │
│  github       ❌ Auth Required         │
│  ─────────────────────────────────     │
│  Total: 3 servers, 2 ready             │
│                                        │
│  🧠 Schema                             │  ← tappable row → schema browser
│  ─────────────────────────────────     │
│  47 types · 12 relationships           │
│                                        │
│  ⚡ Providers                           │
│  ─────────────────────────────────     │
│  OpenAI        1.2M tokens today       │
│  Anthropic     850K tokens today       │
│  DeepSeek      300K tokens today       │
└────────────────────────────────────────┘
```

#### 18.4.5 SettingsView

```
┌────────────────────────────────────────┐
│  ⚙️ Settings                           │
├────────────────────────────────────────┤
│  CONNECTION                            │
│                                        │
│  Server URL                            │
│  ┌──────────────────────────────────┐  │
│  │ https://diane.emergent-company.ai│  │  ← text field
│  └──────────────────────────────────┘  │
│                                        │
│  API Key                               │
│  ┌──────────────────────────────────┐  │
│  │ ●●●●●●●●●●●●●●●●                │  │  ← secure field
│  └──────────────────────────────────┘  │
│                                        │
│  [ Test Connection ]                   │  ← button → spinner + checkmark
│                                        │
│  DEVICE                                │
│                                        │
│  Device Name      Mcj's iPhone         │  ← read-only labels
│  Push Token       ABC123...DEF         │
│  Node Status      🟢 Registered        │
│                                        │
│  [ De-register Device ]                │  ← destructive → confirmation alert
│                                        │
│  NOTIFICATIONS                         │
│                                        │
│  Push Notifications          🟢 On     │  ← toggle
│  Sound                      Default    │
│                                        │
│  ABOUT                                 │
│                                        │
│  Version                    1.0.0      │
│  Build                       1         │
│  Platform              iOS 18.5        │
│                                        │
│  [ Send Bug Report ]                   │
└────────────────────────────────────────┘
```

### 18.5 Shared Components

#### Status Badge

A compact pill indicating status across all views:

```swift
struct StatusBadge: View {
    enum Status { case active, idle, error, disabled, pending }
    let status: Status
    let label: String

    var body: some View {
        HStack(spacing: 4) {
            Circle().fill(color).frame(width: 6, height: 6)
            Text(label).font(.caption2)
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(color.opacity(0.12))
        .cornerRadius(4)
    }
}
```

| Status | Color | Label |
|--------|-------|-------|
| `.active` | `.green` | Active/Running |
| `.idle` | `.orange` | Idle/Pending |
| `.error` | `.red` | Error/Failed |
| `.disabled` | `.secondary` | Disabled/Offline |
| `.pending` | `.blue` | Pending/Auth Required |

#### Loading / Empty / Error States

**Two patterns, same as macOS:**

**Pattern A — Inline (for lists):**
```swift
if items.isEmpty && isLoading {
    PlaceholderRows(count: 5)
} else if items.isEmpty && !isLoading {
    EmptyStateView(title: "No Items", icon: "tray", action: ...)
} else {
    List { ForEach(items) { ... } }
}
```

**Pattern B — Full screen (for single-item views):**
```swift
Group {
    if isLoading { ProgressView("Loading...") }
    else if let error { ErrorStateView(message: error, retry: ...) }
    else { contentView }
}
```

#### Message Row Components

**Tool call row** (inside assistant bubble):
```
🔧 tool_name(arg1="val1", arg2="val2")    ← .footnote.monospaced
                                           ← tap to expand → DetailView
```
- Chevron indicator when collapsible
- Expanded view shows full tool input + output in monospaced block

**Thinking/reasoning section** (inside assistant bubble):
```
💭 Reasoning title...                      ← .footnote, secondary
                                           ← tap to expand
```
- Collapsed by default
- Shows reasoning content indented when expanded

### 18.6 Dark Mode

Fully automatic — no custom colors, use system colors throughout:

| Element | Light | Dark |
|---------|-------|------|
| Background | `.systemGroupedBackground` | `.systemGroupedBackground` |
| Card surface | `.background` (secondary) | `.background` (secondary) |
| User bubble | `.accentColor` (blue) | `.accentColor` (blue) |
| Assistant bubble | `.secondarySystemBackground` | `.tertiarySystemBackground` |
| Separator | `.separator` | `.separator` |
| Text | `.primary` / `.secondary` | `.primary` / `.secondary` |

No manual color definitions — `Color` adapts automatically.

### 18.7 Transitions & Animations

| Action | Animation | Duration |
|--------|-----------|----------|
| Push navigation | iOS default slide | 0.35s |
| Message appears | `.push(from: .bottom)` + opacity | 0.25s |
| Streaming token | No animation (text just appears) | — |
| Streaming cursor | Opacity pulse | 0.8s cycle |
| Send button tap | Scale bounce (1.0 → 0.8 → 1.0) | 0.2s |
| Status change | Color crossfade | 0.3s |
| Redacted shimmer | Gradient sweep | 1.5s cycle |

### 18.8 Launch Screen

System default launch screen (`.storyboard` or SwiftUI `UILaunchScreen` with empty dict). No branding — just the system background color with the app icon centered.

```xml
<!-- Info.plist -->
<key>UILaunchScreen</key>
<dict/>
```

This gives a clean, fast transition to the first loaded view.

### 18.9 Haptics

| Action | Haptic type |
|--------|-------------|
| Send message | `.lightImpact` |
| Error | `.warningFeedback` |
| Connection restored | `.successFeedback` |
| Swipe delete | `.mediumImpact` |
| Pull to refresh | `.selectionChanged` |

---

## 19. Implementation Notes for Phase 1

### ExyteChat Integration Gotchas

1. **Bubble differentiation:** ExyteChat uses `ChatUser.id` to determine alignment. Set user ID to `"user"` for the current user and `"diane"` for the assistant. Configure separate colors per user.

2. **Streaming updates:** ExyteChat's `ChatMessage` uses `id` for identity. When updating a message mid-stream, ensure the `id` stays the same — ExyteChat re-renders the bubble with the new text. Do NOT insert new messages for each token.

3. **Input bar:** ExyteChat's built-in `ChatView` has an `InputView` component. Configure the placeholder text and send button. Override the send action to call our `sendMessage()` method.

4. **Typing indicator:** ExyteChat supports `showTypingIndicator`. Set to `true` while SSE is streaming. The indicator auto-removes when streaming stops.

### SwiftUI Previews

Every view gets a preview provider with mock data:

```swift
#Preview("Session List — Loaded") {
    SessionListView()
        .environment(mockAPIClient)
}

#Preview("Session List — Empty") {
    SessionListView()
        .environment(mockEmptyAPIClient)
}

#Preview("Chat — Messages") {
    ChatView(session: mockSession)
        .environment(mockAPIClient)
}

#Preview("Chat — Streaming") {
    ChatView(session: mockSession)
        .environment(mockStreamingAPIClient)
}
```

---

## Revision History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 3.0 | 2026-05-13 | DianeCoder | Added UI & navigation design (section 18): full screen mockups, tab bar, chat bubble rules, component library, dark mode, animations, haptics, launch screen, Phase 1 implementation notes |
| 2.0 | 2026-05-13 | DianeCoder | Resolved 17 gaps (G1–G17) + 16 third-pass gaps (M1–M16). Added project.yml, Info.plist, entitlements, startup sequence, key types, LOC estimate |
| 1.0 | 2026-05-13 | DianeCoder | Initial draft |
