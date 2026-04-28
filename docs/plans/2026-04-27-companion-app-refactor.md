# Diane Companion App — Refactor Plan

> **Goal:** Fork and refactor the existing `emergent.memory.mac` Mac app into **Diane Companion** — a macOS SwiftUI app that bundles the `diane` binary, manages MCPs, views session logs from the Memory Platform, integrates with Apple apps (Calendar, Reminders, Contacts, Messages, Notes, Mail), and manages macOS permissions.

## Discovery

The repo `emergent-company/emergent.memory.mac` contains a **mature, fully functional** macOS app:

| Component | Status |
|-----------|--------|
| Pure Swift REST API client (EmergentAPIClient) | ✅ All endpoints: projects, stats, graph, agents, MCP, docs, providers, profile |
| Models (Projects, Agents, MCPServers, Documents, GraphObjects, etc.) | ✅ 30+ types with proper CodingKeys |
| SwiftUI MainWindow with NavigationSplitView + sidebar | ✅ 3-column layout, 11 sidebar items |
| Views (Query, Traces, Status, Workers, Objects, Docs, Account, Profile, Agents, MCPs, Providers) | ✅ All complete |
| Menu bar with server status + project list | ✅ |
| Settings (server URL, API key, launch at login, poll interval) | ✅ |
| CLIManager (bundles binary, creates symlink, detects conflicts) | ✅ |
| StatusMonitor (polls `/health` endpoint) | ✅ |
| UpdateChecker (GitHub releases comparison) | ✅ |
| Sparkle auto-update integration | ✅ SUFeedURL + SUPublicEDKey configured |
| XcodeGen project (macOS 13.0+) | ✅ |
| CI release pipeline (build → sign → notarize → DMG) | ✅ Fully working on macOS 14 runner |
| Reusable components (EmptyState, ErrorBanner, Loading, StatCard, etc.) | ✅ |

**Just need to rebrand, adapt for Diane binary, and add ~5 new features.**

## Decisions (No Questions — Made Authoritatively)

1. **Repo location:** ⮕ **In the diane repo** at `server/swift/DianeCompanion/`. Tied to diane's release cycle.
2. **EmergentKit:** ⮕ **Do not use.** Keep the existing pure-Swift `EmergentAPIClient`. Simpler, faster, no XCFramework dependency.
3. **Sparkle feed:** ⮕ Point to `emergent-company/diane` releases after move.
4. **Cert/org:** ⮕ Same Apple Developer team, same `com.emergent-company` identifier.
5. **macOS target:** ⮕ Keep macOS 13.0 (Ventura). Broad compatibility. Can bump later.

---

## Phase 1: Fork into Diane Repo

Copy `emergent.memory.mac` into the diane repo under `server/swift/DianeCompanion/`.

### 1.1 — Copy files

```bash
mkdir -p server/swift/DianeCompanion
cp -r ~/src/emergent.memory.mac/Emergent  server/swift/DianeCompanion/Sources
cp ~/src/emergent.memory.mac/project.yml   server/swift/DianeCompanion/
cp ~/src/emergent.memory.mac/Scripts/      server/swift/DianeCompanion/Scripts/
cp ~/src/emergent.memory.mac/EmergentTests/ server/swift/DianeCompanion/EmergentTests/
cp ~/src/emergent.memory.mac/EmergentUITests/ server/swift/DianeCompanion/EmergentUITests/
cp ~/src/emergent.memory.mac/Casks/        server/swift/DianeCompanion/Casks/
```

### 1.2 — Restructure source directories

```
Sources/
├── CompanionApp/        # ← was Emergent/App/
│   ├── DianeCompanionApp.swift
│   ├── CLIManager.swift
│   ├── SettingsView.swift
│   ├── StatusView.swift
│   └── UpdateChecker.swift
├── CompanionCore/       # ← was Emergent/Core/
│   ├── AppState.swift
│   ├── AppConstants.swift
│   ├── ConnectionState.swift
│   ├── EmergentAPIClient.swift
│   ├── Models.swift
│   ├── ServerConfiguration.swift
│   ├── StatusMonitor.swift
│   └── Components/
│       ├── JSONPropertyViewer.swift
│       ├── SearchableListView.swift
│       ├── StatCardView.swift
│       ├── StateFeedbackViews.swift
│       └── ThreeColumnDetailView.swift
├── Views/               # ← was Emergent/MainWindow/
│   ├── MainWindowView.swift
│   ├── AccountStatusView.swift
│   ├── AgentsView.swift
│   ├── DocumentBrowserView.swift
│   ├── DocumentContentView.swift
│   ├── DocumentDetailView.swift
│   ├── MCPServersView.swift
│   ├── ObjectsBrowserView.swift
│   ├── ProfileView.swift
│   ├── ProjectStatusView.swift
│   ├── ProvidersView.swift
│   ├── QueryView.swift
│   ├── TracesView.swift
│   └── WorkersView.swift
├── MenuBar/             # ← was Emergent/MenuBar/
│   └── MenuBarView.swift
├── AppleIntegration/    # NEW
│   ├── CalendarManager.swift
│   ├── RemindersManager.swift
│   ├── ContactsManager.swift
│   ├── AppleScriptRunner.swift
│   ├── MessagesManager.swift
│   ├── NotesManager.swift
│   └── MailManager.swift
└── Permissions/         # NEW
    ├── PermissionManager.swift
    ├── AccessibilityPermission.swift
    ├── AutomationPermission.swift
    └── NotificationsPermission.swift
```

Plus:
```
DianeCompanion.entitlements
DianeCompanion/Info.plist
DianeCompanion/Assets.xcassets/
```

---

## Phase 2: Rebrand (Emergent → Diane Companion)

### 2.1 — project.yml

| Key | Old Value | New Value |
|-----|-----------|-----------|
| `name` | `Emergent` | `DianeCompanion` |
| `bundleIdPrefix` | `com.emergent-company` | `com.emergent-company` |
| `PRODUCT_NAME` | `Emergent` | `DianeCompanion` |
| `INFOPLIST_FILE` | `Emergent/Info.plist` | `DianeCompanion/Info.plist` |
| `CODE_SIGN_ENTITLEMENTS` | `Emergent/Emergent.entitlements` | `DianeCompanion/DianeCompanion.entitlements` |
| `PRODUCT_BUNDLE_IDENTIFIER` | `com.emergent-company.emergent-mac` | `com.emergent-company.diane-companion` |
| Post-build script | `Copy emergent CLI` → `Emergent/bin/emergent` | `Copy diane CLI` → download from release |

### 2.2 — Info.plist

```xml
<key>CFBundleDisplayName</key>
<string>Diane Companion</string>
<key>CFBundleName</key>
<string>DianeCompanion</string>
<key>SUFeedURL</key>
<string>https://github.com/emergent-company/diane/releases/latest/download/appcast.xml</string>
<key>NSHumanReadableCopyright</key>
<string>Copyright © 2026 Emergent Company.</string>
```

### 2.3 — App entry point

`DianeCompanionApp.swift` (renamed from `EmergentApp.swift`):
- `@main struct DianeCompanionApp: App`
- Update all subsystem strings: `com.emergent-company.diane-companion`
- Update logger categories

### 2.4 — UI text replacements

All occurrences in SwiftUI views:
- `"Emergent"` → `"Diane Companion"`
- `Window("Emergent"` → `Window("Diane Companion"`
- Menu bar: `Text("Emergent.memory")` → `Text("Diane Companion")`
- Sidebar `navigationTitle("Emergent")` → `navigationTitle("Diane Companion")`
- build-dmg.sh: `SCHEME="DianeCompanion"`, `DMG_NAME="DianeCompanion"`

---

## Phase 3: Adapt CLIManager for Diane Binary

### 3.1 — AppConstants.CLIPaths

```swift
enum CLIPaths {
    static let candidates = [
        "/usr/local/bin/diane",
        "/opt/homebrew/bin/diane",
        "/usr/bin/diane",
        "\(NSHomeDirectory())/.local/bin/diane",
    ]
    static let installTarget = "\(NSHomeDirectory())/.local/bin/diane"
}
```

### 3.2 — Version parser

Diane CLI outputs `diane version <X.Y.Z>`. Update `parseVersion()`:
```swift
private func parseVersion(_ raw: String) -> String {
    for line in raw.components(separatedBy: .newlines) {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        // diane version 1.2.3 or diane version dev
        if trimmed.hasPrefix("diane version") {
            return trimmed.dropFirst("diane version".count).trimmingCharacters(in: .whitespaces)
        }
    }
    return raw.trimmingCharacters(in: .whitespacesAndNewlines)
}
```

### 3.3 — Build phase script

Replace `cp -f Emergent/bin/emergent` with a download + extract step:

```bash
# Copy diane CLI from downloaded artifact or download at build time
# For release builds, the binary is downloaded as a build artifact
# For local builds, download from GitHub:
RELEASE_URL="https://github.com/emergent-company/diane/releases/latest/download/diane-darwin-arm64.tar.gz"
OUTPUT_DIR="${TARGET_BUILD_DIR}/${PRODUCT_NAME}.app/Contents/Resources"
mkdir -p "$OUTPUT_DIR"
curl -sL --connect-timeout 10 "$RELEASE_URL" 2>/dev/null | tar xz -C "$OUTPUT_DIR" 2>/dev/null && \
    chmod +x "$OUTPUT_DIR/diane" 2>/dev/null || \
    echo "Warning: Could not download diane binary for bundling"
```

---

## Phase 4: New Features — Session Log Viewer

### 4.1 — New models in CompanionCore/Models.swift

```swift
// MARK: - Diane Session (for Companion session log viewing)
struct DianeSession: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let key: String?
    let title: String?
    let status: String?
    let messageCount: Int?
    let totalTokens: Int?
    let createdAt: String?
    
    enum CodingKeys: String, CodingKey {
        case id, key, title, status
        case messageCount = "message_count"
        case totalTokens = "total_tokens"
        case createdAt = "created_at"
    }
}

struct DianeMessage: Identifiable, Codable, Sendable {
    let id: String
    let role: String
    let content: String
    let sequenceNumber: Int?
    let tokenCount: Int?
    
    enum CodingKeys: String, CodingKey {
        case id, role, content
        case sequenceNumber = "sequence_number"
        case tokenCount = "token_count"
    }
}
```

### 4.2 — New API methods in EmergentAPIClient

```swift
func fetchSessions(projectID: String, status: String? = nil, limit: Int = 50) async throws -> [DianeSession] {
    var path = "/api/graph/objects?type=Session&limit=\(limit)&sort=-created_at"
    if let s = status { path += "&status=\(s)" }
    let data = try await get(path, projectID: projectID)
    struct Response: Decodable { let items: [DianeSession]? }
    if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
        return list
    }
    return (try? JSONDecoder().decode([DianeSession].self, from: data)) ?? []
}

func fetchSessionMessages(projectID: String, sessionID: String, limit: Int = 200) async throws -> [DianeMessage] {
    let data = try await get("/api/graph/objects/\(sessionID)/messages?limit=\(limit)", projectID: projectID)
    struct Response: Decodable { let items: [DianeMessage]? }
    if let resp = try? JSONDecoder().decode(Response.self, from: data), let list = resp.items {
        return list
    }
    return (try? JSONDecoder().decode([DianeMessage].self, from: data)) ?? []
}
```

### 4.3 — New views

**SessionsView.swift** — List of sessions with search/filter:
- Filter by status (All, Active, Completed)
- Search by title
- Show session: title, message count, token count, date, status
- Select → opens detail

**SessionDetailView.swift** — Message transcript:
- Chat-like scroll view
- Role badge (User, Assistant, System)
- Content rendered as text
- Sequence numbers

### 4.4 — Update SidebarItem

```swift
enum SidebarItem {
    case sessions    // NEW — add to .project section
    // ... existing
}
```

---

## Phase 5: MCP Relay Status

### 5.1 — API method

Add to EmergentAPIClient:
```swift
func fetchRelaySessions(projectID: String) async throws -> [RelaySession] {
    let data = try await get("/api/mcp-relay/sessions", projectID: projectID)
    struct Response: Decodable { let sessions: [RelaySession]? }
    // ...
}

struct RelaySession: Identifiable, Codable, Sendable {
    let id: String
    let instanceID: String?
    let nodeName: String?
    let toolCount: Int?
    let connectedAt: String?
    let lastSeenAt: String?
}
```

### 5.2 — Extend MCPServersView

Add a "Relay Nodes" section showing active relay connections:
- Node name, instance ID
- Tool count
- Connection time
- Last seen
- Online/offline status

---

## Phase 6: Apple Integration

### 6.1 — CalendarManager (EventKit)

```swift
@MainActor
class CalendarManager: ObservableObject {
    @Published private(set) var isAuthorized = false
    
    func requestPermission() async -> Bool {
        let store = EKEventStore()
        if #available(macOS 14.0, *) {
            let granted = try? await store.requestFullAccessToEvents()
            return granted != nil
        } else {
            let granted = try? await store.requestAccess(to: .event)
            return granted == true
        }
    }
    
    func fetchCalendars() -> [EKCalendar] {
        let store = EKEventStore()
        return store.calendars(for: .event)
    }
    
    func fetchEvents(in range: DateInterval) throws -> [EKEvent] {
        let store = EKEventStore()
        let cal = store.calendars(for: .event)
        let predicate = store.predicateForEvents(withStart: range.start, end: range.end, calendars: cal)
        return store.events(matching: predicate)
    }
}
```

### 6.2 — RemindersManager (EventKit)

```swift
@MainActor
class RemindersManager: ObservableObject {
    @Published private(set) var isAuthorized = false
    
    func requestPermission() async -> Bool { /* EKEventStore.requestAccess(to: .reminder) */ }
    func fetchLists() -> [EKCalendar] { /* calendars(for: .reminder) */ }
    func fetchReminders(in list: EKCalendar?) async throws -> [EKReminder] { /* ... */ }
    func createReminder(title: String, list: EKCalendar?, dueDate: Date?) async throws { /* ... */ }
}
```

### 6.3 — ContactsManager (Contacts framework)

```swift
@MainActor
class ContactsManager: ObservableObject {
    private let store = CNContactStore()
    
    func requestPermission() async -> Bool { /* CNContactStore.requestAccess */ }
    func searchContacts(query: String) throws -> [CNContact] { /* unifiedContacts */ }
    func listAllContacts() throws -> [CNContact] { /* enumerate */ }
}
```

### 6.4 — AppleScriptRunner + Messages/Notes/Mail

```swift
class AppleScriptRunner {
    static func run(_ script: String) async throws -> String {
        try await withCheckedThrowingContinuation { cont in
            let proc = Process()
            proc.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
            proc.arguments = ["-e", script]
            // ...
        }
    }
}
```

**MessagesManager:**
```swift
func sendMessage(text: String, to: String) async throws {
    let script = """
    tell application "Messages"
        set targetBuddy to buddy "\(to)" of service "E:\(to)"
        send "\(text)" to targetBuddy
    end tell
    """
    try await AppleScriptRunner.run(script)
}
```

**NotesManager:**
```swift
func createNote(title: String, body: String) async throws {
    let escapedBody = body.replacingOccurrences(of: "\"", with: "\\\"")
    let script = """
    tell application "Notes"
        tell account "iCloud"
            make new note with properties {name:"\(title)", body:"\(escapedBody)"}
        end tell
    end tell
    """
    try await AppleScriptRunner.run(script)
}
```

**MailManager:**
```swift
func sendEmail(to: String, subject: String, body: String) async throws {
    let script = """
    tell application "Mail"
        set newMessage to make new outgoing message with properties {subject:"\(subject)", content:"\(body)", visible:true}
        tell newMessage
            make new to recipient at end of recipients with properties {address:"\(to)"}
            send
        end tell
    end tell
    """
    try await AppleScriptRunner.run(script)
}
```

---

## Phase 7: Permissions Manager

### 7.1 — Permission model

```swift
enum PermissionType: String, CaseIterable, Identifiable {
    case accessibility
    case automation
    case notifications
    case calendar
    case reminders
    case contacts
    
    var id: String { rawValue }
    var displayName: String { /* human readable */ }
    var description: String { /* what this enables */ }
    var systemIcon: String { /* SF Symbol */ }
}

struct PermissionInfo: Identifiable {
    let type: PermissionType
    var status: PermissionStatus
    var id: String { type.rawValue }
}

enum PermissionStatus {
    case granted
    case denied
    case notDetermined
    case restricted
}
```

### 7.2 — Permission checks

```swift
@MainActor
class PermissionManager: ObservableObject {
    @Published var permissions: [PermissionInfo] = []
    
    func refresh() {
        permissions = PermissionType.allCases.map { type in
            PermissionInfo(type: type, status: check(type))
        }
    }
    
    private func check(_ type: PermissionType) -> PermissionStatus {
        switch type {
        case .accessibility:
            return AXIsProcessTrusted() ? .granted : .denied
        case .calendar:
            let status = EKEventStore.authorizationStatus(for: .event)
            return mapStatus(status)
        case .reminders:
            let status = EKEventStore.authorizationStatus(for: .reminder)
            return mapStatus(status)
        case .contacts:
            let status = CNContactStore.authorizationStatus(for: .contacts)
            return mapStatus(status)
        case .notifications:
            // Check via UNUserNotificationCenter
            return .notDetermined
        case .automation:
            // Check via AEDeterminePermissionToAutomateTarget
            return .notDetermined
        }
    }
}
```

### 7.3 — Permissions UI

Add a "Permissions" sidebar item (in Configuration section) or as a tab in Settings:

```swift
struct PermissionsView: View {
    @StateObject private var manager = PermissionManager()
    
    var body: some View {
        List(manager.permissions) { permission in
            HStack {
                Image(systemName: permission.type.systemIcon)
                VStack(alignment: .leading) {
                    Text(permission.type.displayName)
                    Text(permission.type.description).font(.caption)
                }
                Spacer()
                switch permission.status {
                case .granted:
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
                case .denied:
                    Button("Open Settings") { /* open pref pane */ }
                case .notDetermined:
                    Button("Request") { Task { await manager.request(permission.type) } }
                case .restricted:
                    Text("Restricted").foregroundStyle(.secondary)
                }
            }
        }
    }
}
```

---

## Phase 8: CI Workflow

Add companion build job to `diane/.github/workflows/release.yml`:

```yaml
companion:
  name: Build Companion App
  runs-on: macos-14
  needs: build
  steps:
    - uses: actions/checkout@v4
    - name: Select Xcode
      run: sudo xcode-select -s /Applications/Xcode_15.4.app
    - name: Install tools
      run: |
        brew install xcodegen create-dmg
    - name: Download diane binary
      uses: actions/download-artifact@v4
      with:
        name: diane-darwin-arm64
        path: server/swift/DianeCompanion/Resources/
    - name: Extract binary
      run: |
        cd server/swift/DianeCompanion/Resources
        tar xzf diane-darwin-arm64.tar.gz
        rm diane-darwin-arm64.tar.gz
        chmod +x diane
    - name: Generate Xcode project
      run: xcodegen generate
      working-directory: server/swift/DianeCompanion
    - name: Build, sign, notarize
      run: ./Scripts/build-dmg.sh --notarize
      working-directory: server/swift/DianeCompanion
      env:
        DEVELOPMENT_TEAM: ${{ secrets.DEVELOPMENT_TEAM }}
        NOTARIZE_APPLE_ID: ${{ secrets.NOTARIZE_APPLE_ID }}
        NOTARIZE_PASSWORD: ${{ secrets.NOTARIZE_PASSWORD }}
        NOTARIZE_TEAM_ID: ${{ secrets.NOTARIZE_TEAM_ID }}
    - name: Upload DMG
      uses: actions/upload-artifact@v4
      with:
        name: diane-companion-dmg
        path: server/swift/DianeCompanion/build/DianeCompanion-*.dmg
```

---

## Execution Order

```
Phase 1: Fork (copy files into diane repo)
    └─ Phase 2: Rebrand (rename files, update strings)
        └─ Phase 3: Adapt CLIManager for diane binary
            ├─ Phase 4: Session log viewer (SessionsView)
            ├─ Phase 5: MCP relay status (small extension)
            ├─ Phase 6: Apple integration managers (7 files)
            ├─ Phase 7: Permissions manager (4 files + view)
            └─ Phase 8: CI workflow (update release.yml)
```

Phases 4–7 are independent and can be done in parallel. Phases 1–3 are sequential prerequisites.
