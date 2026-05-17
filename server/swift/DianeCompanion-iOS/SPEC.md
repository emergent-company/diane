# Diane iOS App — Chat Specification v1.0

> **Purpose**: Define every aspect of the chat experience — message lifecycle, network resilience, UI feedback, and error recovery — so any developer can implement, review, or extend the chat feature without ambiguity.

---

## 1. Message Lifecycle & State Machine

### 1.1 User Message States

Each **user message** goes through these states:

```
                    ┌──────────┐
                    │  QUEUED   │ ← offline, waiting for network
                    └────┬─────┘
                         │ prepared for send
                         ▼
                    ┌──────────┐
                    │ SENDING  │──────────────┐
                    └────┬─────┘              │
                         │ HTTP 200 received   │ failure
                         ▼                    │
                    ┌──────────┐              │
                    │  SENT    │  ✓ single     │
                    └────┬─────┘  gray check  │
                         │                    │
                         │ run.in-progress     │
                         │ or first           │
                         │ message.part       │
                         ▼                    ▼
                    ┌──────────┐        ┌──────────────┐
                    │  READ    │  ✓✓    │   FAILED     │
                    └──────────┘  blue  └──────┬───────┘
                         double check           │
                                                │ user taps Retry
                                                ▼
                                         goes back to SENDING

  RETRYING: temporary state during auto-retry, same as SENDING visually
```

| User Status | Indicator | Meaning | Trigger |
|-------------|-----------|---------|---------|
| `QUEUED` | ⏳ `clock.arrow.circlepath` orange | Waiting for network | Created while offline |
| `SENDING` | ⟳ spinner | Being sent right now | User tapped Send |
| `SENT` | ✓ single gray checkmark | Server received the message | HTTP 200 from POST or `run.created` SSE event |
| `READ` | ✓✓ double blue checkmark | AI started processing | `run.in-progress` or first `message.part` SSE event |
| `FAILED` | ⚠️ `exclamationmark.circle.fill` red + inline Retry button | All retries exhausted | Final retry failed |
| `RETRYING` | ⟳ spinner + "Retrying…" label (subtle) | Auto-retry in progress | Transient error during send |

### 1.2 Assistant Message States

**Assistant messages** are always created by the system (user never sends them). Their lifecycle:

```
                    ┌──────────────┐
                    │  STREAMING   │  ▍ blinking cursor
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐     ┌──────────────┐
                    │   SENT       │     │   FAILED     │
                    │ (completed)  │     │ (partial)    │
                    └──────────────┘     └──────────────┘

  - STREAMING: cursor shown, content grows
  - SENT: stream completed with "done" event
  - FAILED: stream ended with error mid-way — partial content preserved
```

Assistant messages do NOT have "delivered" or "read" states — the assistant IS the reader.

### 1.3 Model Changes

Add to `DianeMessage`:

```swift
public enum SendStatus: String, Codable, Sendable {
    case queued      // user messages: waiting for network
    case sending     // user messages: currently being sent
    case sent        // both: delivered/completed but not yet read
    case read        // user messages only: AI acknowledged/processing
    case failed      // both: all retries exhausted / stream error
    case retrying    // user messages: auto-retry in progress
    case streaming   // assistant messages: content actively streaming
}

// New fields on DianeMessage:
public let sendStatus: SendStatus?
public let errorMessage: String?
```

Existing `role == "error"` messages are replaced by `role == "user" || role == "assistant"` messages with `sendStatus == .failed` and appropriate error text in `errorMessage`.

---

## 2. Sending Flow (Detailed)

### 2.1 Send Blocking — One Message at a Time

**Principle:** While a message is being sent or a response is being received, the user CANNOT send another message. The send button becomes a **Stop** button. To send again, the user must either:
- **Wait** for the response to finish (auto-resolves)
- **Tap Stop** to cancel the current stream, then type and send a new message

This prevents the confusing scenario of stacking requests on the same session.

#### Guard Logic

The send flow checks `isStreaming` and `isSending` flags to block double-sends:

| State | Send Button Shows | Send Tappable? |
|-------|------------------|----------------|
| Idle (no message in flight) | ⟫ Send arrow (accent) | ✅ Yes |
| `.sending` / `.queued` / `.retrying` | ⏹ Stop icon (red) | ❌ No — must stop first |
| `.streaming` (assistant generating) | ⏹ Stop icon (red) | ❌ No — must stop first |
| `.failed` | ⟫ Send arrow (accent) | ✅ Yes — retries by re-sending |
| `.sent` / `.read` (completed) | ⟫ Send arrow (accent) | ✅ Yes |

The `isStreaming` flag is set to `true` immediately when `sendMessage()` is called (before the HTTP request), and remains `true` until the stream completes, fails, or the user taps Stop.

```swift
// Current guard — prevents any concurrent send:
guard !isStreaming else { return }

// Text field is disabled during streaming:
TextField(...).disabled(isStreaming)

// Send/Stop button swaps:
Button(action: isStreaming ? onStop : onSend) { ... }
```

### 2.2 Normal Path (Online)

```
User taps Send
  │
  ├─ Validate input (non-empty after trim)
  │
  ├─ 1. Create DianeMessage with sendStatus = .sending
  │      - id = "user-{UUID}"
  │      - role = "user"
  │      - content = trimmed text
  │      - sendStatus = .sending
  │      - createdAt = now
  │
  ├─ 2. Append to messages array → UI shows ⟳ spinner
  ├─ 3. Cache messages (so they survive interruption)
  ├─ 4. Start streaming task
  ├─ 5. Call streamACP()
  │
  ├─ 6a. HTTP 200 / run.created SSE event → ✓ single gray checkmark
  │         Update user message to .sent
  │         Create placeholder assistant message with .streaming
  │
  ├─ 6b. Connection fails → handle via retry logic (section 2.2)
  │
  ├─ 7. run.in-progress or first message.part → ✓✓ double blue checkmark
  │      Update user message to .read
  │
  ├─ 8. Process SSE events (streaming tokens, reasoning, tool calls)
  │      Assistant message accumulates content with ▍ cursor
  │
  ├─ 9. On stream event "done" → mark assistant as .sent
  │      Cache messages
  │
  └─ 10. On error → replace streaming placeholder with error message
```

### 2.2 Retry Logic (Network Failure)

When `streamACP()` throws an error:

```
Send failed
  │
  ├─ Check NetworkMonitor.isConnected
  │
  ├─ YES connected:
  │    └─ Retry immediately, up to 3 attempts
  │       ├─ Attempt 1: wait 1s, retry
  │       ├─ Attempt 2: wait 3s, retry
  │       ├─ Attempt 3: wait 5s, retry
  │       └─ All failed → mark message as .failed
  │
  └─ NO disconnected:
       └─ Mark message as .queued
          Register for NetworkMonitor.connectivityChanged
```

### 2.3 Offline Queue

```
User taps Send while offline
  │
  ├─ Create DianeMessage with sendStatus = .queued
  ├─ Append to messages → shows ⏳ "Queued" indicator
  ├─ Persist to SessionCache (messages already cached)
  └─ Register for NetworkMonitor.connectivityChanged
```

**On network reconnection:**

```
.connectivityChanged notification received, isConnected == true
  │
  ├─ Scan messages[] for any with sendStatus == .queued
  ├─ For each queued message (in order):
  │    ├─ Set sendStatus = .sending (spinner)
  │    ├─ Attempt send
  │    ├─ Success → .sent (run.created) → .read (processed)
  │    ├─ Failure → .failed
  │    └─ Continue to next queued message (1 msg/sec rate limit)
  └─ Show brief toast notification: "Queued messages sent"
```

**Rate limit for queue flush:** Send at most 1 message per second from the queue to avoid overwhelming the connection.

### 2.4 User-Initiated Retry

When a message has `sendStatus == .failed`:

- Show a **Retry** button (inline, subtle) on the failed message bubble
- Tapping retry:
  1. Set `sendStatus = .sending`
  2. Create a new `streamACP()` call
  3. Append a new placeholder assistant message
  4. Follow normal streaming flow
- The old placeholder assistant message (empty, from previous failed attempt) should be **removed** before starting the new stream

### 2.6 Message History Loading with Pagination

**Principle:** When opening a chat session, load only the most recent messages. Older messages load on demand as the user scrolls up. This prevents slow startup on sessions with thousands of messages.

#### Load Flow

```
ChatView appears
  │
  ├─ 1. Load from local cache (instant)
  │     ├─ Get ALL cached messages for session
  │     ├─ Take last PAGE_SIZE (default: 50) messages
  │     └─ Display them immediately — no spinner if cached
  │
  ├─ 2. Fetch from server (background)
  │     ├─ GET /acp/v1/sessions/{id}
  │     ├─ If server supports pagination:
  │     │    └─ GET ...?limit=PAGE_SIZE&offset=0  (newest first)
  │     ├─ Extract & merge with local messages
  │     ├─ Merge strategy:
  │     │    ├─ Remote messages replace cached (authoritative)
  │     │    ├─ Local-only messages (pending send) preserved
  │     │    └─ Sort by createdAt ascending
  │     └─ Cache full result locally
  │
  ├─ 3. User scrolls up past first visible message
  │     └─ "Load more" triggered
  │
  ├─ 4. Load more messages
  │     ├─ From local cache if available (most common)
  │     │    └─ Prepend next PAGE_SIZE messages to array
  │     ├─ From server if not cached:
  │     │    └─ GET ...?limit=PAGE_SIZE&offset={nextOffset}
  │     └─ Show subtle loading indicator at top of scroll
  │
  └─ 5. When all messages loaded (exhausted cache + server)
       └─ Show subtle "Beginning of conversation" indicator
```

#### Page Size

| Context | Page Size | Rationale |
|---------|-----------|-----------|
| Initial load (cache) | 50 messages | Instant, no network needed |
| Initial load (server fetch) | Full session (server doesn't support pagination yet) | One-time cost — then cached |
| Scroll-to-load-more (cache) | 30 messages | Smooth scrolling — prepend in batch |
| Scroll-to-load-more (server, future) | 30 messages | Network efficient |

#### Pagination State

```swift
@State private var allMessages: [DianeMessage] = []     // complete set (cached)
@State private var displayedMessages: [DianeMessage] = [] // visible subset
@State private var displayLimit: Int = PAGE_SIZE           // how many to show
@State private var isLoadingMore = false                   // loading spinner at top
@State private var hasMoreMessages = true                  // exhausted?
```

#### Server-Side Pagination (Future)

The ACP session detail endpoint should eventually support:

```
GET /acp/v1/sessions/{id}?offset=0&limit=50&order=desc

Response:
{
  "id": "...",
  "total_runs": 342,        // total available
  "history": [...]          // current page
}
```

This would make pagination efficient for sessions with 1000+ messages.

#### Scroll Detection

Use `GeometryReader` or ExyteChat's built-in `enableLoadMore` on the scroll view:

```swift
// In the ScrollView:
GeometryReader { geo in
    Color.clear
        .onChange(of: geo.frame(in: .global).minY) { _, newY in
            // When first visible message scrolls past threshold
            if newY > scrollThreshold && !isLoadingMore && hasMoreMessages {
                Task { await loadMoreMessages() }
            }
        }
}

// Or the loadMore function:
private func loadMoreMessages() async {
    guard !isLoadingMore && hasMoreMessages else { return }
    isLoadingMore = true
    
    // Get next batch from full cache
    let nextLimit = displayLimit + PAGE_SIZE
    let nextBatch = allMessages.suffix(nextLimit)
    
    await MainActor.run {
        withAnimation {
            displayedMessages = Array(nextBatch)
            displayLimit = nextLimit
            hasMoreMessages = nextBatch.count < allMessages.count
        }
        isLoadingMore = false
    }
}
```

#### Initial Loading Indicator

| State | UI | Duration |
|-------|-----|----------|
| Cache found + not empty | Instant — no loading state | 0ms |
| Cache found + empty | "Loading messages…" spinner, replaced when server fetch completes | <3s |
| No cache + server fetch | Full-screen "Loading…" | Until first response |
| Scroll loading more | Subtle spinner at top of scroll, inline | Until batch completes |

#### Caching Strategy

- **All messages** are cached to UserDefaults (existing behavior)
- On re-open, only last `PAGE_SIZE` are displayed — full cache is loaded but paginated
- Server fetch in background syncs the full set, then pagination is re-applied
- If local cache is large (>500 messages), consider moving to SQLite or file-based storage

#### Error Recovery

| Scenario | Behavior |
|----------|----------|
| Server fetch fails on open | Show cached messages only — show "Could not sync" banner |
| Load-more cache miss + server fails | Keep current messages, show "Failed to load more" inline |
| Server returns no pagination support | Fall back to loading all (current behavior), then paginate client-side |

### 2.5 Cancellation (Stop Streaming)

```
User taps stop button (while isStreaming == true)
  │
  ├─ streamingTask?.cancel()
  ├─ streamingTask = nil
  ├─ isStreaming = false
  ├─ Find placeholder assistant message:
  │    ├─ Has accumulated content? → Keep it, set sendStatus = .sent
  │    └─ Empty? → Remove it from messages
  └─ Cache messages
```

---

## 3. Message Status Indicators (UI)

### 3.1 User Message Status Indicators

Each user bubble gets **two rows** below the message content:

```
 ┌──────────────────────────────────────┐
 │ Hello, what can you do?              │
 └──────────────────────────────────────┘
 09:41 AM          [✓✓]     (SEND/READ)
```

The status indicator sits at the trailing edge, next to the timestamp:

| Status | Icon | Color | Behavior |
|--------|------|-------|----------|
| `.queued` | `clock.arrow.circlepath` | Orange 70% | Pulses gently |
| `.sending` | `ProgressView()` spinner | Accent color | Spins until resolved |
| `.sent` | ✓ single checkmark | Gray (secondary) | Single gray checkmark appears after HTTP 200 |
| `.read` | ✓✓ double checkmark | Blue (accent) | Double blue checkmark appears on `run.in-progress` or first `message.part` |
| `.failed` | `exclamationmark.circle.fill` | Red | Persistent — tap reveals error + Retry |
| `.retrying` | `arrow.triangle.2.circlepath` + "Retrying…" | Orange | Spins, subtle animation |

**Transition timing:**
- `.sending` → `.sent`: typically <1s on good network (HTTP round-trip)
- `.sent` → `.read`: typically 1-3s (server acknowledges, agent starts processing)
- `.sent` hover: minimum 200ms so user can see both states distinctly (no flicker)
- `.read` stays indefinitely on sent messages (shows the message was processed)

**Implementation:** A `MessageStatusView` component, shown only for `role == "user"` messages. Uses a 200ms minimum delay before transitioning from `.sent` to `.read` to prevent flicker on fast responses.

### 3.2 Assistant Response Header

Before the first token arrives (after user message is sent but before streaming starts):

- Show a subtle animated indicator: "Diane is thinking…" with three bouncing dots
- Duration: maximum 5 seconds before showing error
- Replaced by streaming content once first token arrives

### 3.3 Streaming Cursor

- During active streaming, show `▍` blinking cursor at end of content (already implemented)
- On completion, cursor disappears and message transitions from `.streaming` to `.sent`

### 3.4 Tool Calls — Detail Card Only (Not Inline)

**Principle:** Tool calls are NEVER shown inline inside the assistant response bubble. The bubble shows only:
- Reasoning section (if any, collapsible — stays inline)
- Content text (the assistant's actual response)
- Streaming cursor (during streaming)
- Timestamp

Tool calls are ONLY accessible by **tapping on the assistant message bubble**, which opens the `MessageDetailSheet` showing full expanded tool call details.

**Rationale:** Tool calls are implementation details (what the AI did internally to produce the response). They're useful for debugging/transparency but clutter the main chat view. Moving them to the detail card keeps the chat flow clean and focused on the conversation.

**Visual layout:**

```
Bubble (inline — NO tool calls):
┌──────────────────────────────────────┐
│ Reasoning ▼                          │  ← collapsible, stays inline
│ ┌─ reasoning text ─────────────────┐ │
│ │ The user asked about weather...  │ │
│ └──────────────────────────────────┘ │
│ Based on the data I found,         │
│ the weather today is sunny.        │  ← main response text
│ ▍                                    │  ← streaming cursor (if active)
│ 09:41 AM                            │  ← timestamp
└──────────────────────────────────────┘
        ↑ tap here → opens MessageDetailSheet

MessageDetailSheet (full detail):
┌──────────────────────────────────────┐
│  Role: assistant                     │
│  Time: 09:41 AM                      │
│  ID: stream-xxxxx                    │
├──────────────────────────────────────┤
│  Content                             │
│  Based on the data I found...       │
├──────────────────────────────────────┤
│  Reasoning                           │
│  The user asked about weather...    │
├──────────────────────────────────────┤
│  Tool Calls (2)                     │
│  ┌─ web_search ───────────────────┐ │
│  │ Arguments: {query: "weather"}  │ │
│  │ Result: Sunny, 72°F            │ │
│  └────────────────────────────────┘ │
│  ┌─ read_file ────────────────────┐ │
│  │ Arguments: {path: "/tmp/..."} │ │
│  │ Result: File contents: ...    │ │
│  └────────────────────────────────┘ │
├──────────────────────────────────────┤
│  [Copy Message Text]                │
└──────────────────────────────────────┘
```

**Current code to remove:**

The inline `ToolCallView` in `MessageBubbleContent` (lines 514-520 of ChatView.swift) must be removed:

```swift
// REMOVE this block from MessageBubbleContent:
if let toolCalls = message.toolCalls, !toolCalls.isEmpty {
    VStack(alignment: .leading, spacing: DesignTokens.spacingXXS) {
        ForEach(toolCalls, id: \\.name) { tc in
            ToolCallView(toolCall: tc)
        }
    }
}
```

The `ToolCallView` struct and `ExpandedToolCallView` struct remain in the codebase — `ExpandedToolCallView` is used inside `MessageDetailSheet`, and `ToolCallView` can be removed as dead code once the inline rendering is gone.

**Message Detail Sheet already has the correct structure** for showing tool calls in expanded view (see section 7.2). No changes needed there.
### 3.5 Toast / Banner Feedback

| Event | Toast/Banner | Duration |
|-------|-------------|----------|
| Message sent successfully | None (too noisy) | — |
| Message queued (offline) | "Message queued — will send when connected" | 3s |
| Queued messages flushed | "Queued messages sent" | 3s |
| Send failed (all retries) | "Message failed to send" inline on bubble | Persistent |
| Connection restored | (Cached data reloaded silently) | — |
| Upload failed | Inline error message in chat | Persistent |

---

## 4. Network Integration

### 4.1 Current State

- `NetworkMonitor`: NWPathMonitor, posts `.connectivityChanged` notifications
- `OfflineBanner`: Shows "No Connection" banner at top of content views
- SessionListView reloads sessions on reconnection

### 4.2 What Needs to Change

**NetworkMonitor** (no structural changes needed — already functional)

**ChatView** needs to:
1. Subscribe to `.connectivityChanged` notification
2. On reconnection: flush queued messages
3. On disconnection: stop any active streaming (show error), don't lose queued messages

**OfflineBanner** needs to:
- Also appear inside ChatView (currently only on SessionListView)
- Show a count: "No Connection — 2 messages queued" when messages are queued

### 4.3 Session Carry-While-Offline

- Messages created offline still belong to the current session
- The session must exist on the server (it was created online)
- On reconnection → send to existing session
- If session was deleted remotely while offline → show error, offer to create new session

---

## 5. Error Handling Matrix

| Error Scenario | User Message Status | Assistant Placeholder | Recovery |
|---------------|-------------------|----------------------|----------|
| No network at send time | `.queued` | None created | Auto-flush on reconnect |
| Network dies mid-stream | `.sent` (delivered) | `.failed` with partial content | Tap to retry — resend user msg |
| Server returns 4xx | `.failed` | None (or removed) | Show error, offer retry |
| Server returns 5xx | `.retrying` → `.failed` | None (or removed) | Auto retries x3, then show Retry |
| Stream timeout (300s) | `.sent` | Partial content kept, marked `.failed` | User sees partial, can ask again |
| File upload fails | `.failed` (error message) | N/A | User can re-attach file |
| Empty response (0 tokens) | `.sent` | Remove placeholder | Silent — no assistant msg shown |

### Error Message Display

When a message transitions to `.failed`:

```swift
// The message content becomes a user-facing error string
DianeMessage(
    id: "user-{UUID-original}",
    role: "user",
    content: originalContent,  // Keep original text
    sendStatus: .failed,
    errorMessage: "Could not reach the server. Tap to retry."
)
```

The `errorMessage` is displayed as a caption below the bubble, in red, with a Retry button.

**Important:** Never discard the user's original text. The failed message retains its original `content` so the user can see what they typed.

---

## 6. File Upload Integration

### 6.1 Sending with Attachment

```
User picks file → upload to MP
  ├─ Success: attach document ID to next message
  └─ Failure: show inline error message

User types text + has pending attachment → send()
  ├─ Message content = "[📎 filename]\n\n{user text}"
  ├─ Include document_id in streamACP() request body
  └─ Follow normal send flow (status indicators apply)
```

### 6.2 Upload Status

- While uploading: show `ProgressView()` + "Uploading…" (already implemented)
- Failed upload: show error message in chat (already implemented)
- Retry upload: user taps + button → re-pick file

---

## 7. Session Detail & Navigation

### 7.1 Session Detail Drawer

Already implemented: tapping the navigation title opens `SessionDetailSheet` with:
- Session info (title, agent)
- Stats grid (Run Count, Messages, Tokens, Cost)
- Status
- Timestamps
- Action buttons (Delete)
- Enriched with data from `GET /acp/v1/sessions/{id}` detail endpoint (total_tokens, total_cost_usd, run_count, last_run_status)

### 7.2 Message Detail Sheet

Already implemented: tapping a message bubble opens `MessageDetailSheet` with:
- Metadata header (role, time, ID)
- Full content with `.textSelection(.enabled)`
- Reasoning content
- Expanded tool calls (all arguments + results)
- Copy Message Text button

### 7.3 Session List — Status Dots with Motion

Each session row in the list shows a **10px colored circle** that communicates the session's run status from the ACP API.

#### SDK-Defined Run Statuses (from `emergent.memory/apps/server/domain/agents/entity.go`)

The ACP SDK defines exactly these `AgentRunStatus` values:

| Internal Constant | ACP Value String | Color | Animation | Meaning |
|-|-|-|-|-|
| `RunStatusQueued` | `"submitted"` | 🟠 Yellow | **Pulse** (2s) | Enqueued, waiting for a worker |
| `RunStatusRunning` | `"working"` | 🟢 Green | **Pulse** (2s) | Actively being processed |
| `RunStatusSuccess` | `"completed"` | 🟢 Green | Static | Finished successfully |
| `RunStatusSkipped` | `"skipped"` | ⚪ Gray | Static | Run was skipped (maps to `completed` in ACP output) |
| `RunStatusError` | `"failed"` | 🔴 Red | Static | Run ended with error |
| `RunStatusPaused` | `"input-required"` | 🟡 Orange | Static | Waiting for human input |
| `RunStatusCancelled` | `"cancelled"` | ⚪ Gray | Static | User cancelled the run |
| `RunStatusCancelling` | `"cancelling"` | 🟠 Yellow | **Pulse** (2s) | Two-step cancel: intent acknowledged, awaiting stop |
| `nil` (no runs) | absent | 🟠 Yellow | **Pulse** (2s) | Session created but never had a run |

#### Where These Values Appear

| Context | Status Source | Values Seen |
|---------|-------------|-------------|
| **Session list** (`GET /acp/v1/sessions`) | `last_run_status` on the session object | `completed`, `failed`, or `nil` |
| **Session detail** (`GET /acp/v1/sessions/{id}`) | `last_run_status` on session + `status` on each run in `history[]` | same + `submitted`, `working`, `cancelled` for in-progress runs |
| **SSE events** (active streaming) | `run.created` → `status: submitted`, `run.in-progress` → `status: working`, `run.completed` → `status: completed`, `run.failed` → `status: failed`, `run.cancelled` → `status: cancelled` | All 7 ACP statuses possible |

#### ACP SSE Status Constants (from `emergent.memory/apps/server/domain/agents/acp_dto.go`)

```go
ACPStatusSubmitted     = "submitted"
ACPStatusWorking       = "working"
ACPStatusInputRequired = "input-required"
ACPStatusCompleted     = "completed"
ACPStatusFailed        = "failed"
ACPStatusCancelling    = "cancelling"
ACPStatusCancelled     = "cancelled"
```

These are the raw string values the client will receive in SSE `run.*` events and in `last_run_status` on sessions.

#### Status Resolution Chain

The session row should use `lastRunStatus` directly from the ACP response:

```swift
// Use the last_run_status from server — no fallback to "active"
let displayStatus = session.lastRunStatus  // "completed" | "failed" | nil
```

The current code uses `lastRunStatus ?? status ?? "active"` which incorrectly shows new sessions (nil) as green "active". This should be changed to just `lastRunStatus` — nil gets its own state (yellow pulsing).

#### Current SessionResponse Fields from Server

| Field | Type | Example | Notes |
|-------|------|---------|-------|
| `id` | string | `65fc0785-...` | Session UUID |
| `agent_name` | string | `diane-default` | Agent used for the session |
| `created_at` | string (ISO8601) | `2026-05-17T08:38:40Z` | Creation timestamp |
| `updated_at` | string (ISO8601) | `2026-05-17T08:38:40Z` | Last update timestamp |
| `status` | string or null | `nil` (always absent) | Server does NOT send this field — only `last_run_status` exists |
| `last_run_status` | string or null | `"completed"` | Use this directly — exact values from SDK: `submitted`, `working`, `completed`, `failed`, `input-required`, `cancelling`, `cancelled`, or nil |
| `is_archived` | bool | `false` | Server-side archive flag (iOS ignores this) |
| `run_count` | int | `2` | Number of runs |
| `message_count` | int | `2` | Number of messages |
| `total_tokens` | int | `322256` | Total tokens used |
| `total_cost_usd` | double | `0.0322736` | Total cost in USD |

#### Animation Implementation

Add to `StatusColors`:

```swift
public enum StatusAnimation {
    case `static`
    case pulse      // submitted, working, cancelling, nil (no runs) — 2.0s cycle, scale 1.0→1.15
}

public static func statusAnimation(_ status: String?) -> StatusAnimation {
    guard let status = status?.lowercased(), !status.isEmpty else {
        return .pulse  // nil = no runs yet = waiting
    }
    switch status {
    case "submitted", "working", "cancelling":
        return .pulse
    default:
        return .static  // completed, failed, input-required, cancelled, skipped
    }
}
```

New `StatusPulseAnimation` view modifier in `SessionListView`:

```swift
struct StatusPulseAnimation: ViewModifier {
    let animation: StatusAnimation

    func body(content: Content) -> some View {
        switch animation {
        case .static: content
        case .pulse: content
            .scaleEffect(1.15)
            .animation(.easeInOut(duration: 1.0).repeatForever(autoreverses: true), value: animation)
        }
    }
}
```

Updated `SessionRow` status dot:

```swift
Circle()
    .fill(StatusColors.statusColor(session.lastRunStatus ?? session.status))
    .frame(width: 10, height: 10)
    .modifier(StatusPulseAnimation(
        animation: StatusColors.statusAnimation(session.lastRunStatus ?? session.status)
    ))
```

**Status resolution priority:** `lastRunStatus` takes precedence over `status` for both color and animation. This way, a session with `status: "active"` but `last_run_status: "running"` shows a **pulsing green** dot — giving real-time activity feedback.

### 7.4 Return Navigation

- If a session is deleted from the detail sheet → pop back to session list
- Archive is local-only (no server sync needed)
The session list has two display modes controlled by a toolbar button (archive icon):

- **Default view** (`showArchived = false`): Shows only non-archived sessions
- **Archived view** (`showArchived = true`): Shows all sessions with an archive badge

**Search behavior:** When the user types in the search bar, archived sessions are ALWAYS included in results regardless of the `showArchived` toggle. This ensures the user can find any session by searching, even if it's archived. The toggle only affects the unfiltered list view.

```
Search text empty:
  ├─ showArchived = false → only active sessions
  └─ showArchived = true  → all sessions

Search text non-empty (searching):
  └─ ALL sessions (active + archived) matched by title or agent name
```

**Implementation note:** The current code (line 288 of SessionListView.swift) filters archived sessions before applying the search predicate. This must be changed so the search operates on ALL sessions:

```swift
// Current (wrong — archived hidden from search):
let visible = archiveStore.showArchived
    ? sessions
    : sessions.filter { !archiveStore.isArchived($0.id) }
if searchText.isEmpty { return visible }

// Fixed (correct — search includes archived):
if !searchText.isEmpty {
    return sessions.filter {
        (($0.title ?? "").localizedCaseInsensitiveContains(searchText)
        || ($0.agentName ?? "").localizedCaseInsensitiveContains(searchText))
    }
}
let visible = archiveStore.showArchived
    ? sessions
    : sessions.filter { !archiveStore.isArchived($0.id) }
return visible
```

---

## 8. Data Persistence

### 8.1 Cache Strategy

| Data | Storage | Update Strategy |
|------|---------|----------------|
| Session list | UserDefaults (SessionCache) | Cache on load, merge remote |
| Messages per session | UserDefaults (SessionCache) | Cache after each stream completion, cache on reconnection flush |
| Send status | UserDefaults (SessionCache) | Persisted with message — survives app restart |
| Queued messages | UserDefaults (SessionCache) | Survives app restart → flush on next launch |
| Archived IDs | UserDefaults (ArchivedSessionsStore) | Local-only, no server sync |
| Last read timestamps | UserDefaults (SessionCache) | Updated on message load |

### 8.2 Unread Badge

- Computed from last-read timestamp vs message createdAt dates
- Unread count displayed on session list rows
- Marked read on entering ChatView (after load)

---

## 9. Existing UI Tests & Coverage

### 9.1 Current Tests (20 tests)

The existing `DianeUITests.swift` covers:

**Session List:**
- ✅ App launches to Chats screen
- ✅ New session button exists
- ✅ Search bar exists
- ✅ Agent picker opens and can be cancelled

**Mock Data Tests:**
- ✅ Mock session appears in list
- ✅ Mock session shows agent name
- ✅ Navigate to ChatView via tap
- ✅ Chat title button exists
- ✅ Messages display correctly
- ✅ Tool call names visible
- ✅ Tool call expands on tap
- ✅ Reasoning section expands

**Session Detail Sheet:**
- ✅ Opens on title tap
- ✅ Shows stats (run count, tokens)
- ✅ Shows agent name
- ✅ Shows delete button
- ✅ Can be dismissed

**Message Detail Sheet:**
- ✅ Opens on bubble tap
- ✅ Shows copy button
- ✅ Can be dismissed
- ✅ Shows tool calls in expanded view

**Settings:**
- ✅ Opens via gear button
- ✅ Shows connection fields
- ✅ Can type API Key and Project ID
- ✅ Test Connection button exists
- ✅ Save button exists and starts disabled
- ✅ Can be dismissed

**Swipe Actions:**
- ✅ Session row reveals Archive on swipe

**Edge Cases:**
- ✅ Tap empty area doesn't crash
- ✅ Tool call shows arguments after expand

### 9.2 Missing Test Coverage (Needs Writing)

After implementing the spec above, these tests must be added:

**Message States:**
- [ ] User message shows `.sending` indicator (⟳ spinner) immediately on send
- [ ] User message transitions to `.sent` (✓ single gray checkmark) on server acknowledgment
- [ ] User message transitions to `.read` (✓✓ double blue checkmark) when AI starts processing
- [ ] Assistant message shows no inline tool calls — bubble only has content + reasoning
- [ ] Tapping assistant bubble opens message detail sheet with full tool call details
- [ ] Failed message shows Retry button
- [ ] Tapping Retry re-sends and creates new assistant placeholder

**Offline Behavior:**
- [ ] Sending while offline shows `.queued` indicator
- [ ] Queued messages are flushed on reconnect (simulate with mock)

**Network Recovery:**
- [ ] Chat view shows queued message count in offline banner
- [ ] Partial stream content preserved on connection loss

**Edge Cases:**
- [ ] Double-tap send is prevented (guarded by `isStreaming`)
- [ ] Stop button removes empty placeholder
- [ ] Stop button keeps partial content
- [ ] Send after stop works correctly
- [ ] Session deleted while chat open → navigation pops back

**Pagination:**
- [ ] Chat opens showing only last PAGE_SIZE (50) messages from mock data
- [ ] Scrolling to top of displayed messages triggers load more
- [ ] Load more prepends older messages to the displayed set
- [ ] Loading indicator appears at top while loading more
- [ ] "Beginning of conversation" indicator shows when all messages loaded
- [ ] New messages sent while scrolled up → auto-scroll to bottom

**Archive & Search:**
- [ ] Archived sessions hidden by default when not searching
- [ ] Archived sessions appear in search results even when showArchived is false
- [ ] Tapping archive icon toggles to archived view

---

## 10. Implementation Phases

### Phase 1: Message State Machine + Sent/Read + Tool Call Relocation + Message History Pagination (Core)

**Model changes:**
1. Add `SendStatus` enum to `DianeMessage` — includes `queued`, `sending`, `sent`, `read`, `failed`, `retrying`, `streaming`
2. Add `sendStatus: SendStatus?` field
3. Add `errorMessage: String?` field
4. Update `SessionCache` to persist new fields (JSON encoder/decoder auto-handles Codable enums)

**ChatView load/pagination changes:**
1. Split `messages: [DianeMessage]` into `allMessages` (full cache) and `displayedMessages` (paginated subset)
2. On load: show last `PAGE_SIZE` (50) messages from cache immediately
3. Background fetch from server syncs full set, re-applies pagination
4. Add scroll-detection (`GeometryReader`) to trigger `loadMoreMessages()`
5. Prepend next batch (30) messages on scroll up
6. Show subtle spinner at top when `isLoadingMore`
7. Show "Beginning of conversation" when `hasMoreMessages == false`

**ChatView send/status changes:**
1. Create `MessageStatusView` component showing:
   - ⟳ spinner for `.sending`
   - ✓ single gray checkmark for `.sent`
   - ✓✓ double blue checkmark for `.read`
   - ⏳ orange clock for `.queued`
   - ⚠️ red exclamation for `.failed` (with Retry button)
   - ⟳ + "Retrying…" for `.retrying`
2. Update `sendMessage()` to set `.sending` on creation, `.sent` on `run.created`, `.read` on `run.in-progress`/first `message.part`
3. Add 200ms minimum hover time on `.sent` before transitioning to `.read` to prevent flicker
4. Handle catch-block to set `.failed` or `.queued` based on network state
5. Add retry button to `.failed` messages (on tap: remove old placeholder, create new stream)
6. Add "Diane is thinking…" indicator before first token arrives
7. **Remove inline `ToolCallView` from `MessageBubbleContent`** — tool calls only in detail sheet
8. Wire `streamACP()` event parsing to detect `run.created` and `run.in-progress` events for status transitions

**Dependencies:** None — pure SwiftUI + model changes

### Phase 2: Offline Queue & Auto-Retry

1. Subscribe to `NetworkMonitor.connectivityChanged` in `ChatView`
2. On reconnect: scan for `.queued` messages and flush
3. Update `OfflineBanner` to show queued count
4. Implement exponential backoff retry (1s, 3s, 5s) for transient errors
5. Persist sendStatus so queued messages survive app restart

**Dependencies:** Phase 1 complete (status fields exist)

### Phase 3: UI Polish

1. Add `StatusAnimation` enum + `statusAnimation()` to `StatusColors`
2. Create `StatusPulseAnimation` view modifier in `SessionListView`
3. Update `SessionRow` to apply pulse animation based on `lastRunStatus`
4. Toast/banner notifications for queue flush
5. "Sent" checkmark fade animation (visible for 30s)
6. Haptic feedback on send success/failure
7. Queued message count in offline banner

### Phase 4: Tests

1. Add UI test infrastructure for message states
2. Write offline + retry tests
3. Write edge case tests
4. Run full suite and fix regressions

---

## 11. Architecture Diagram

```
┌──────────────────────────────────────────────────────────┐
│                      ChatView                            │
│  ┌────────────────────────────────────────────────────┐  │
│  │  messages: [DianeMessage]                      │  │
│  │  ├─ allMessages (full cached set)                │  │
│  │  └─ displayedMessages (visible PAGE_SIZE subset) │  │
│  │  ┌─────────────────────┐                          │  │
│  │  │ MessageBubbleContent│  ← ForEach(messages)     │  │
│  │  │  ├─ MessageStatusView  (for user msgs)         │  │
│  │  │  ├─ ReasoningSection   (collapsible, inline)  │
│  │  │  └─ StreamingCursor    (when streaming)        │
│  │  │     NO inline tool calls — see detail sheet    │
│  │  └─────────────────────┘                          │  │
│  └────────────────────────────────────────────────────┘  │
│  ┌────────────────────────────────────────────────────┐  │
│  │  ChatInputBar                                      │  │
│  │  ├─ TextField (disabled when streaming)            │  │
│  │  ├─ Send/Stop button                               │  │
│  │  └─ File attachment indicator                       │  │
│  └────────────────────────────────────────────────────┘  │
│                                                           │
│  Subscriptions:                                            │
│  ├─ NetworkMonitor.connectivityChanged ─→ flush queue      │
│  └─ streamingTask ─→ cancellation onDisappear              │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────┐
│               EmergentAPIClient               │
│  ├─ streamACP() → AsyncThrowingStream        │
│  ├─ createACPSession() → session id          │
│  ├─ uploadDocument() → Document              │
│  └─ fetchACPSessions() → [ACPSessionItem]    │
└──────────────────────────────────────────────┘

┌──────────────────────────────────────────────┐
│              NetworkMonitor                   │
│  ├─ NWPathMonitor                             │
│  ├─ isConnected: Bool                         │
│  └─ connectivityChanged Notification          │
└──────────────────────────────────────────────┘

┌──────────────────────────────────────────────┐
│              SessionCache                     │
│  ├─ cacheMessages() → UserDefaults           │
│  ├─ cacheSessions() → UserDefaults           │
│  ├─ loadCachedMessages() → [DianeMessage]    │
│  └─ includes sendStatus persistence           │
└──────────────────────────────────────────────┘
```

---

## 12. Key Design Decisions

1. **No separate "pending messages" store.** Queued messages live alongside regular messages in the `messages` array with `sendStatus == .queued`. This means they're always visible to the user in the correct position, always persisted with the session, and always in-order.

2. **User text is never lost.** When a message fails, the original `content` is preserved. The user sees their text + an error message below it.

3. **Retry creates new assistant placeholder.** Tapping retry on a failed user message creates a *new* assistant placeholder. The old empty/failed placeholder is removed. This avoids confusion between old and new attempts.

4. **One queue flush at a time.** When the app comes back online, queued messages are flushed one-at-a-time with a 1-second delay between each. The `isStreaming` guard prevents concurrent sends naturally.

5. **No visual noise for successful sends.** The checkmark indicator is intentionally subtle and fades after 30 seconds. Successful sends are the expected path, not an achievement.

6. **Streaming cursor stays even on partial content.** If connection drops mid-stream, the partial content is preserved (marked as `.failed`). The user can see what the assistant had started to say and can retry or continue.

---

## 13. ExyteChat Gap Analysis

The project vendors ExyteChat as a local Swift Package (`server/swift/Packages/ExyteChat/`) but the iOS app does NOT use any of its built-in features. The chat is implemented entirely with custom SwiftUI components (ScrollView + LazyVStack + manual input bar + custom message bubbles). ExyteChat provides a mature chat framework with the following built-in features that are either missing or reimplemented from scratch.

### 13.1 Built-in ExyteChat Features We're Ignoring

| Feature | ExyteChat Provides | Diane iOS Currently | Recommendation |
|---------|-------------------|-------------------|----------------|
| **Message Status** | `Message.Status`: `.sending`, `.sent`, `.delivered`, `.read`, `.error` + built-in `MessageStatusView` with icons | Being spec'd from scratch in this doc (Phase 1) | **Use ExyteChat's status system** — maps 1:1 to our SendStatus enum |
| **Message Status UI** | `MessageStatusView` renders per-message status icon (spinner, ✓, ✓✓, ⚠️ retry) | Custom `MessageStatusView` in Phase 1 | **Use ExyteChat's** — saves ~60 lines of UI code |
| **Long-press Context Menu** | `MessageMenu` with reactions + custom actions (copy, reply, edit, delete, select) | Custom message detail sheet on tap | Evaluate: ExyteChat's menu handles reactions inline; we may want both |
| **Emoji Reactions** | `ReactionDelegate` + `ReactionSelectionView` with emoji search, custom reactions, reaction overview | Not implemented at all | **Low priority** — AI chat doesn't need reactions yet |
| **Swipe Actions (on messages)** | Leading/trailing `SwipeAction` per message with full-swipe support | Not implemented on messages (only on session list) | Consider: swipe to reply or delete a message |
| **Attachments/Media Picker** | Built-in photo/video/document picker, attachments grid, `AttachmentsEditor`, fullscreen media viewer | Custom `fileImporter` + manual upload flow | **Use ExyteChat's** — richer, handles multiple media types, upload status |
| **Voice Recording** | `Recorder` + `RecordWaveform` + `RecordingPlayer` — full voice message flow | Not implemented | Low priority |
| **Reply-to-Message** | `.quote` and `.answer` reply modes — renders quoted message above bubble | Not implemented | Would be useful for multi-turn context |
| **Date Headers** | Built-in section date separators (`showDateHeaders: true`) | Not implemented | **Easy win** — turn on `showDateHeaders` |
| **Avatars** | Avatar with URL loading, name fallback, tap handler, configurable size | Not shown | Not applicable for AI chat (both parties = user & system) |
| **Link Previews** | Built-in link preview rendering (limit configurable) | Custom `MessageContentView` with Textual | Could simplify: use ExyteChat's link preview |
| **Network Banner** | `showNetworkConnectionProblem: true` | Custom `OfflineBanner` | **Use ExyteChat's** — simpler, in-line with chat |
| **Scroll-to-Bottom** | Built-in floating button `showScrollToBottomButton: true` | Manual `scrollTo(lastId)` on count change | **Use ExyteChat's** — handles edge cases |
| **Pagination** | `enableLoadMore(offset:, handler:)` — triggers load when scrolling above pageSize | Not implemented | Useful for large sessions |
| **Keyboard Dismiss** | `.interactive` / `.onDrag` / `.none` modes | `scrollDismissesKeyboard(.immediately)` | Minor — ExyteChat's `.interactive` is better UX |
| **GIPHY Integration** | Built-in `AvailableInputType.giphy` | Not implemented | Low priority |
| **Theming** | `ChatTheme` with colors, images, styles for every component | Manual `DesignTokens` throughout | Could adopt for consistency |

### 13.2 Architectural Decision: Use ExyteChat or Keep Custom?

The current implementation is completely custom (not using ExyteChat's `ChatView` at all). Two options:

**Option A: Adopt ExyteChat (migration)**
- Pros: Get 16+ features for free, consistent UX, less code to maintain
- Cons: Migration cost — must convert `DianeMessage` to ExyteChat's `Message` model, must adapt custom bubble content to `messageBuilder` closure, must replace custom input bar with ExyteChat's input view, tool calls/reasoning need custom rendering inside ExyteChat cells
- Effort: ~2-3 days of refactoring

**Option B: Keep Custom + Cherry-Pick**
- Pros: Full control, no migration risk, we already have the spec
- Cons: We rebuild what ExyteChat already gives us, more code to maintain
- Effort: Already spec'd — ~4 phases

### 13.3 Cherry-Pick Items for Phase 1-2

If staying custom (Option B), at minimum use these ExyteChat pieces:

```swift
// Use ExyteChat's Message.Status enum instead of our own
import ExyteChat

// Already have: Message.Status.sending, .sent, .delivered, .read, .error

// Use the built-in status view in our custom bubbles:
MessageStatusView(status: .read) { /* retry handler */ }
```

Also enable ExyteChat's built-in features that require zero code changes if we switched to ExyteChat's `ChatView`:
- `.showScrollToBottomButton(true)`
- `.showNetworkConnectionProblem(true)`
- `.showDateHeaders(true)`
- `.keyboardDismissMode(.interactive)`

---

## 14. API Call Mapping & Memory Platform Gaps

### 14.1 Current API Calls (iOS → Memory Platform)

| Endpoint | Method | Used For | Status | Data Flow |
|----------|--------|----------|--------|-----------|
| `/acp/v1/sessions` | POST | Create new session | ✅ | Returns `{ id: "..." }` |
| `/acp/v1/agents/{name}/runs` | POST | Stream chat message (SSE) | ✅ | Returns SSE stream of events |
| `/acp/v1/sessions` | GET | List sessions | ✅ | Returns array/object of `ACPSessionItem` |
| `/acp/v1/sessions/{id}` | GET | Session detail + run history | ✅ | Returns single `ACPSessionItem` with `history[]` |
| `/acp/v1/sessions/{id}` | DELETE | Delete session | ✅ Best-effort | No response body expected |
| `/api/agent-definitions` | GET | List available agents | ✅ | Returns agent name, model, provider |
| `/api/projects` | GET | List projects | ✅ | Returns `[Project]` |
| `/api/stats` | GET | Project statistics | ✅ | Returns `ProjectStats` |
| `/api/documents/upload` | POST | Upload file | ✅ | Multipart form-data, returns document |
| `/api/documents` | GET | List documents | ✅ | Returns `[Document]` |
| `/api/schema` | GET | Schema viewer | ✅ | Returns schema response |

### 14.2 Missing Memory Platform Endpoints (from iOS Perspective)

These features would require new server endpoints:

| Feature | Required Endpoint(s) | Why We Need It | Workaround Without It |
|---------|---------------------|---------------|----------------------|
| **Mark message as read** | `PATCH /acp/v1/sessions/{id}/read` or message-level read receipts | `READ` status transitions require explicit ACK from server | Infer `.read` from `run.in-progress` SSE event — but this only works during active stream, not for past messages |
| **Delete message** | `DELETE /acp/v1/agents/{name}/runs/{runId}` or message-level delete | User wants to delete a single message, not the whole session | Workaround: delete entire session (current behavior) |
| **Edit message** | `PATCH /acp/v1/agents/{name}/runs/{runId}` or message-level edit | User wants to correct a message and re-send | Workaround: copy content, delete session, create new session |
| **Typing indicator** | `POST /acp/v1/sessions/{id}/typing` or WebSocket channel | Show "Diane is typing…" before first token | Already handled by SSE stream — but no way to show indicator before stream begins |
| **Search messages** | `GET /acp/v1/sessions/{id}/search?q=...` | Search within a session's messages | Client-side search through cached messages only |
| **Session archive (server)** | `PATCH /acp/v1/sessions/{id}` with `status: archived` | Archive sessions server-side | Local-only archive via `ArchivedSessionsStore` — session still visible to other clients |
| **Message reactions** | `POST /acp/v1/sessions/{id}/messages/{msgId}/reactions` | Emoji reactions on messages | Not implemented (reactions are low priority for AI chat) |
| **Paginated history** | `GET /acp/v1/sessions/{id}?limit=50&offset=0&order=desc` | Load session history in pages — prevent slow startup on large sessions | Client-side truncation to PAGE_SIZE messages; server returns full history
| **File attachment in stream** | Include `document_ids` in `/acp/v1/agents/{name}/runs` request body | Send document context to the agent | Workaround: embed document reference in message text `[📎 filename]\n\n{text}` |
| **Markdown content** | Server sends content_type hint in SSE | Proper markdown rendering vs plain text | Client auto-detects markdown with `DianeContentDetector` |

### 14.3 SSE Event Coverage (ACP Streaming)

Events the Memory Platform sends that the iOS app processes:

| SSE Event | iOS Handler | Status (from SDK) | Used For |
|-----------|-------------|------------------|----------|
| `run.created` | Not explicitly handled | `submitted` | Could drive `.sending → .sent` transition |
| `run.in-progress` | Not explicitly handled | `working` | Could drive `.sent → .read` transition |
| `message.part` (text/plain) | `.token` / `.text` | — | Accumulates assistant response content |
| `message.part` (application/json, trajectory kind) | `.tool_call` / `.tool_result` | — | Tracks tool usage by the AI |
| `message.created` | `.message` | — | Full message content |
| `run.completed` | `.done` | `completed` | Marks stream as complete |
| `run.failed` | `.error` | `failed` | Stream failure with error message |
| `run.cancelled` | `.error` | `cancelled` | Stream cancelled |
| `error` | `.error` | — | Generic error from server |

Status values defined by the ACP SDK (`emergent.memory/apps/server/domain/agents/acp_dto.go`):

```go
ACPStatusSubmitted     = "submitted"
ACPStatusWorking       = "working"
ACPStatusInputRequired = "input-required"
ACPStatusCompleted     = "completed"
ACPStatusFailed        = "failed"
ACPStatusCancelling    = "cancelling"
ACPStatusCancelled     = "cancelled"
```

The `cancelling` and `input-required` statuses are currently not handled by the iOS app. `cancelling` could drive a "stopping…" UI, and `input-required` could show a "waiting for you" state.

Events NOT currently handled but could be useful:
- `message.part` with `content_type: "reasoning"` — reasoning content is currently delivered as `text/plain` and parsed by the app. If the server sent it with a dedicated content type, parsing would be cleaner.
- `message.part` with status metadata — the server could include `status: "generating_tool_call"` to show the user the agent is making a tool call.

### 14.4 Authentication

| Auth Type | Token Prefix | iOS Implementation |
|-----------|-------------|-------------------|
| Bearer token | `emt_` | `Authorization: Bearer emt_xxx` |
| API Key | Any other | `X-API-Key: mp_xxx` |

The iOS app auto-detects token type in `EmergentAPIClient.apiKey` setter.

---

*Document version: 1.0 | Last updated: 2026-05-18*
*Author: DianeCoderTool*
