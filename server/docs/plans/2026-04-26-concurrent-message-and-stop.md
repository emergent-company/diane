# Concurrent Message Handling & Stop Functionality

**Goal:** Add per-channel message queuing for concurrent messages and a `/stop` command to cancel active agent runs.

**Architecture:** Per-channel active session guard with message queue + cancellable polling loop using `context.WithCancel`. Mirrors Hermes' gateway pattern (`_active_sessions` + `_pending_messages` + `asyncio.Event`).

**Key idea:** `triggerAgentWithContext` already has a synchronous 2s polling loop. We make it honour a `context.Context`'s cancellation. A `/stop` message cancels the context from another goroutine, the poll loop exits, and the handler sends "🛑 Stopped". Concurrent messages queue behind the active guard and drain automatically after each response.

**Tech Stack:** Go 1.25, discordgo, `client.Agents.CancelRun(ctx, agentID, runID)`

**Files touched:** `~/diane/server/internal/discord/bot.go`

---

### Task 1: Add ActiveChannel struct and Bot fields

**Objective:** Add the data structures for per-channel concurrency control.

**Files:**
- Modify: `~/diane/server/internal/discord/bot.go`

**Step 1: Add ActiveChannel struct** (after `ChannelSession`, around line 96):

```go
// ActiveChannel tracks an in-progress agent run for a channel.
// Used for concurrency guard, interrupt support, and /stop.
type ActiveChannel struct {
    Cancel  context.CancelFunc // cancels the poll loop in triggerAgentWithContext
    AgentID string             // runtime agent ID (for CancelRun API)
    RunID   string             // current run ID (for CancelRun API)
    Pending []*discordgo.Message // queued messages waiting to be processed
}
```

**Step 2: Add Bot fields** (in `Bot` struct around line 79, after `dedupCache`):

```go
activeMu     sync.Mutex
activeChans  map[string]*ActiveChannel // responseChannel → active processing
```

**Step 3: Initialize in New()** (around line 159):

```go
activeChans: make(map[string]*ActiveChannel),
```

**Step 4: Verify build**

```bash
cd ~/diane/server && /usr/local/go/bin/go build ./internal/discord/
```

**Step 5: Commit**

```bash
git add ~/diane/server/internal/discord/bot.go
git commit -m "feat: add ActiveChannel struct for per-channel concurrency"
```

---

### Task 2: Make poll loop cancellable via context

**Objective:** The 2s poll loop in `triggerAgentWithContext` uses a fixed timeout. Switch to honouring a `context.Context` so it exits when cancelled.

**Files:**
- Modify: `~/diane/server/internal/discord/bot.go:850-1083`

**Step 1: Add `"errors"` to imports**

```go
import (
    // ...
    "errors"
    // ...
)
```

**Step 2: Replace fixed-timeout loop** (lines 934-958).

**Old code:**
```go
// 4. Poll for completion (max 120s, poll every 2s)
pollStart := time.Now()
timeout := 120 * time.Second
pollInterval := 2 * time.Second
var runStatus string
for time.Since(pollStart) < timeout {
    time.Sleep(pollInterval)
    runResp, err := globalBridge.GetProjectRun(ctx, runID)
    if err != nil {
        dlog("POLL", "err", err.Error(), "elapsed", time.Since(pollStart).Round(time.Second).String())
        continue
    }
    runStatus = runResp.Data.Status
    dlog("POLL", "status", runStatus, "elapsed", time.Since(pollStart).Round(time.Second).String(), "run", runID[:12])

    switch runStatus {
    case "completed", "success", "completed_with_warnings":
        // Done!
        goto fetchResponse
    case "error", "failed", "cancelled", "timeout":
        errMsg := ""
        if runResp.Data.ErrorMessage != nil {
            errMsg = *runResp.Data.ErrorMessage
        }
        dlog("AGT", "err", "run_"+runStatus, "run", runID[:12], "error", errMsg)
        return "", fmt.Errorf("run %s: status=%s, error=%s", runID[:12], runStatus, errMsg)
    }
    // "pending", "running", "queued" → keep polling
}
return "", fmt.Errorf("run %s: timeout after %v (last status: %s)", runID[:12], timeout, runStatus)
```

**New code:**
```go
// 4. Poll for completion (cancellable via ctx)
pollStart := time.Now()
pollInterval := 2 * time.Second
pollTimeout := 120 * time.Second
var runStatus string
pollLoop:
for {
    select {
    case <-ctx.Done():
        // Cancelled by /stop or interrupt
        dlog("POLL", "event", "cancelled", "elapsed", time.Since(pollStart).Round(time.Second).String())
        return "", fmt.Errorf("run %s: cancelled by user", runID[:12])
    case <-time.After(pollInterval):
    }

    // Check timeout separately
    if time.Since(pollStart) >= pollTimeout {
        dlog("POLL", "event", "timeout", "elapsed", pollTimeout.String())
        return "", fmt.Errorf("run %s: timeout after %v (last status: %s)", runID[:12], pollTimeout, runStatus)
    }

    runResp, err := globalBridge.GetProjectRun(ctx, runID)
    if err != nil {
        if errors.Is(ctx.Err(), context.Canceled) {
            return "", fmt.Errorf("run %s: cancelled by user", runID[:12])
        }
        dlog("POLL", "err", err.Error(), "elapsed", time.Since(pollStart).Round(time.Second).String())
        continue
    }
    runStatus = runResp.Data.Status
    dlog("POLL", "status", runStatus, "elapsed", time.Since(pollStart).Round(time.Second).String(), "run", runID[:12])

    switch runStatus {
    case "completed", "success", "completed_with_warnings":
        break pollLoop
    case "error", "failed", "cancelled", "timeout":
        errMsg := ""
        if runResp.Data.ErrorMessage != nil {
            errMsg = *runResp.Data.ErrorMessage
        }
        dlog("AGT", "err", "run_"+runStatus, "run", runID[:12], "error", errMsg)
        return "", fmt.Errorf("run %s: status=%s, error=%s", runID[:12], runStatus, errMsg)
    }
}
```

**Step 3: Store agentID and runID on active channel** (after `runID := *triggerResp.RunID` at line 927):

```go
// Store agent/run IDs for CancelRun (used by /stop and interrupt)
b.setActiveAgentRun(cs.ChannelID, agentID, runID)
```

**Step 4: Verify build**

```bash
cd ~/diane/server && /usr/local/go/bin/go build ./internal/discord/
```

**Step 5: Commit**

```bash
git add ~/diane/server/internal/discord/bot.go
git commit -m "feat: make agent run poll loop cancellable via context"
```

---

### Task 3: Add channel guard helpers

**Objective:** Create the helper methods for acquiring/releasing channels and managing the pending message queue.

**Files:**
- Modify: `~/diane/server/internal/discord/bot.go` (add methods after `saveSession` around line 686)

**Step 1: Add helper methods after `saveSession`:**

```go
// ─────────────────────────────────────────────────────────────────────
// Channel Concurrency Guard
// ─────────────────────────────────────────────────────────────────────

// acquireChannel marks a channel as busy and returns a cancellable context.
// Returns nil, nil, false if the channel is already busy.
func (b *Bot) acquireChannel(channelID string) (context.Context, context.CancelFunc, bool) {
    b.activeMu.Lock()
    defer b.activeMu.Unlock()
    if _, exists := b.activeChans[channelID]; exists {
        return nil, nil, false
    }
    ctx, cancel := context.WithCancel(context.Background())
    b.activeChans[channelID] = &ActiveChannel{Cancel: cancel}
    return ctx, cancel, true
}

// releaseChannel removes the channel from active state.
// Must be called when the processing loop exits.
func (b *Bot) releaseChannel(channelID string) {
    b.activeMu.Lock()
    defer b.activeMu.Unlock()
    delete(b.activeChans, channelID)
}

// setActiveAgentRun stores the runtime agent and run IDs for CancelRun.
// Called from triggerAgentWithContext after the run starts.
func (b *Bot) setActiveAgentRun(channelID, agentID, runID string) {
    b.activeMu.Lock()
    defer b.activeMu.Unlock()
    if ac, exists := b.activeChans[channelID]; exists {
        ac.AgentID = agentID
        ac.RunID = runID
    }
}

// queueMessage adds a message to the pending queue for a busy channel.
func (b *Bot) queueMessage(channelID string, msg *discordgo.Message) {
    b.activeMu.Lock()
    defer b.activeMu.Unlock()
    if ac, exists := b.activeChans[channelID]; exists {
        ac.Pending = append(ac.Pending, msg)
        log.Printf("[QUEUE] Queued msg %s for channel %s (size=%d)", msg.ID[:8], channelID, len(ac.Pending))
    }
}

// popPending removes and returns the next queued message, or nil if empty.
func (b *Bot) popPending(channelID string) *discordgo.Message {
    b.activeMu.Lock()
    defer b.activeMu.Unlock()
    ac, exists := b.activeChans[channelID]
    if !exists || len(ac.Pending) == 0 {
        return nil
    }
    msg := ac.Pending[0]
    ac.Pending = ac.Pending[1:]
    return msg
}

// stopActiveRun cancels the current agent run for a channel.
// Called from the /stop handler on a different goroutine.
func (b *Bot) stopActiveRun(channelID string) {
    b.activeMu.Lock()
    ac, exists := b.activeChans[channelID]
    b.activeMu.Unlock()
    if !exists {
        log.Printf("[STOP] No active run for channel %s", channelID)
        return
    }

    log.Printf("[STOP] Cancelling run %s for channel %s", truncateStr(ac.RunID, 12), channelID)

    // Cancel the context first — the poll loop picks this up
    ac.Cancel()

    // Cancel the run on MP (best-effort, non-blocking)
    if ac.AgentID != "" && ac.RunID != "" {
        go func(aID, rID string) {
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            if err := globalBridge.Client().Agents.CancelRun(ctx, aID, rID); err != nil {
                log.Printf("[STOP] CancelRun error: %v", err)
            } else {
                log.Printf("[STOP] Run %s cancelled on MP", rID[:12])
            }
        }(ac.AgentID, ac.RunID)
    }
}
```

**Step 2: Verify build**

```bash
cd ~/diane/server && /usr/local/go/bin/go build ./internal/discord/
```

**Step 3: Commit**

```bash
git add ~/diane/server/internal/discord/bot.go
git commit -m "feat: add channel guard helpers (acquire/release/queue/stop)"
```

---

### Task 4: Thread cancellable context through buildAndSendResponse

**Objective:** `buildAndSendResponse` currently creates its own `context.Background()`. Change it to accept a `context.Context` from the caller so the cancellable context from the active guard flows through to `triggerAgentWithContext`.

**Files:**
- Modify: `~/diane/server/internal/discord/bot.go:580-611`

**Step 1: Update `sendResponse` to accept context** (line 580-585):

```go
// sendResponse handles the full response flow (fallback path, no active guard).
// Uses a plain background context since this path doesn't support /stop.
func (b *Bot) sendResponse(s *discordgo.Session, channelID string, m *discordgo.Message, start time.Time) {
    response := b.buildAndSendResponse(context.Background(), m, channelID)
    b.sendMessage(s, channelID, response)
    log.Printf("[RES] channel=%s duration=%v chars=%d", channelID, time.Since(start).Round(time.Millisecond), len(response))
}
```

**Step 2: Update `buildAndSendResponse` signature and body** (line 590-611):

```go
// buildAndSendResponse does the actual work: session management + MP agent call.
// ctx must be a cancellable context passed down from the active guard.
func (b *Bot) buildAndSendResponse(ctx context.Context, m *discordgo.Message, responseChannel string) string {
    // Get or create session for this response channel (thread or parent channel)
    cs := b.getOrCreateSession(responseChannel, b.detectAgentType(m.Content))
    log.Printf("[SES] response_channel=%s session=%s agent=%s", responseChannel, cs.SessionID, cs.AgentType)

    // Determine which MP agent to use
    agentName := cs.AgentType
    if agentName == AgentTypeDefault {
        agentName = "diane-default"
    }

    log.Printf("[AGT] Routing to agent: %s", agentName)
    response, err := b.triggerAgentWithContext(ctx, cs, m.Content, agentName)
    if err != nil {
        // Check if cancelled — suppress noisy error message for stop
        if ctx.Err() != nil {
            return "" // caller handles the "Stopped" message
        }
        errMsg := fmt.Sprintf("❌ Agent %s failed: %v", agentName, err)
        log.Printf("[AGT] %s", errMsg)
        return errMsg
    }
    return response
}
```

**Step 3: Verify build**

```bash
cd ~/diane/server && /usr/local/go/bin/go build ./internal/discord/
```

**Step 4: Commit**

```bash
git add ~/diane/server/internal/discord/bot.go
git commit -m "refactor: pass cancellable context through buildAndSendResponse"
```

---

### Task 5: Rewrite handleMessage with queue + drain loop

**Objective:** Modify `handleMessage` to use the active guard, queue messages when busy, drain the queue after each response, and handle `/stop`.

**Files:**
- Modify: `~/diane/server/internal/discord/bot.go:474-578`

**Step 1: Replace the entire `handleMessage` method:**

```go
func (b *Bot) handleMessage(s *discordgo.Session, m *discordgo.Message) {
    start := time.Now()
    channelID := m.ChannelID
    botID := s.State.User.ID

    // ── Determine response channel (thread or inline) ──
    ch, err := s.Channel(channelID)
    isThread := err == nil && ch.IsThread()
    var responseChannel string
    var createdNewThread bool

    if isThread {
        responseChannel = channelID
        log.Printf("[THR] Continuing in existing thread %s", channelID)
    } else {
        shouldThread := len(b.config.ThreadChannels) == 0 // empty = thread everywhere
        if !shouldThread {
            for _, id := range b.config.ThreadChannels {
                if id == channelID {
                    shouldThread = true
                    break
                }
            }
        }
        if !shouldThread {
            log.Printf("[THR] Responding inline (no thread config)")
            responseChannel = channelID
        } else {
            emoji, category := categorizeMessage(m.Content)
            cleanMsg := strings.TrimSpace(m.Content)
            cleanMsg = regexp.MustCompile(`^[\p{So}\p{Sk}\p{Sc}\p{Sm}]\s*`).ReplaceAllString(cleanMsg, "")
            threadName := emoji + " " + category + ": " + truncateStr(cleanMsg, 40)
            if len(threadName) > 100 {
                threadName = threadName[:100]
            }
            if threadName == "" || threadName == emoji+" "+category+": " {
                threadName = emoji + " " + category
            }
            thread, err := s.MessageThreadStart(channelID, m.ID, threadName, 60*24)
            if err != nil {
                log.Printf("[WARN] Thread creation failed: %v", err)
                b.sendResponse(s, channelID, m, start)
                return
            }
            responseChannel = thread.ID
            createdNewThread = true
            log.Printf("[THR] Created thread %s (%s)", thread.ID, threadName)
        }
    }

    // ── Handle /stop when nothing is active ──
    if strings.TrimSpace(m.Content) == "/stop" {
        b.activeMu.Lock()
        _, active := b.activeChans[responseChannel]
        b.activeMu.Unlock()
        if !active {
            s.MessageReactionAdd(channelID, m.ID, "👀")
            b.sendMessage(s, responseChannel, "Nothing is currently running.")
            s.MessageReactionRemove(channelID, m.ID, "👀", botID)
            s.MessageReactionAdd(channelID, m.ID, "✅")
            log.Printf("[STOP] Nothing running — replied idle")
            return
        }
        // Active — fall through to acquire guard below
    }

    // ── Acquire channel (active guard) ──
    ctx, cancel, acquired := b.acquireChannel(responseChannel)
    if !acquired {
        // Channel is busy with another message
        if strings.TrimSpace(m.Content) == "/stop" {
            // /stop bypasses the queue — cancel the active run
            s.MessageReactionAdd(channelID, m.ID, "👀")
            b.stopActiveRun(responseChannel)
            s.MessageReactionRemove(channelID, m.ID, "👀", botID)
            s.MessageReactionAdd(channelID, m.ID, "✅")
            b.sendMessage(s, responseChannel, "🛑 **Stopped**")
            log.Printf("[STOP] Stopped active run for channel %s", responseChannel)
            return
        }

        // Non-stop message while busy — queue it
        s.MessageReactionAdd(channelID, m.ID, "👀")
        b.queueMessage(responseChannel, m.Message)
        log.Printf("[QUEUE] Channel %s busy, queued msg %s", responseChannel, m.ID[:8])
        return
    }

    // ── Process loop: drain messages until queue empty or cancelled ──
    processingOK := false
    currentMsg := m

    for {
        // Log queue state
        queueSize := 0
        b.activeMu.Lock()
        if ac, ok := b.activeChans[responseChannel]; ok {
            queueSize = len(ac.Pending)
        }
        b.activeMu.Unlock()
        dlog("PRC", "channel", responseChannel, "msg", currentMsg.ID[:8], "queue_size", queueSize)

        b.startTyping(s, responseChannel)
        response := b.buildAndSendResponse(ctx, currentMsg, responseChannel)
        b.stopTyping(responseChannel)

        // Check if cancelled by /stop
        if ctx.Err() != nil {
            b.sendMessage(s, responseChannel, "🛑 **Stopped**")
            processingOK = true
            break
        }

        // Send response
        if response != "" {
            b.sendMessage(s, responseChannel, response)
            processingOK = true

            // For new threads, check for session title update
            if createdNewThread && !isThread {
                b.mu.RLock()
                cs, exists := b.sessions[responseChannel]
                b.mu.RUnlock()
                if exists && cs.SessionID != "" {
                    sd, sdErr := globalBridge.GetSession(context.Background(), cs.SessionID)
                    if sdErr == nil && sd.Title != "" && !strings.HasPrefix(sd.Title, "Discord #") {
                        if _, editErr := s.ChannelEdit(responseChannel, &discordgo.ChannelEdit{
                            Name: sd.Title,
                        }); editErr != nil {
                            log.Printf("[THR] Title update failed: %v", editErr)
                        } else {
                            log.Printf("[THR] Renamed thread to %q", sd.Title)
                        }
                    }
                }
            }
        }

        // Check queue for next message
        nextMsg := b.popPending(responseChannel)
        if nextMsg == nil {
            break // queue empty, done
        }
        currentMsg = nextMsg
        log.Printf("[QUEUE] Processing queued msg %s (channel %s)", currentMsg.ID[:8], responseChannel)
    }

    // ── Cleanup ──
    cancel()
    b.releaseChannel(responseChannel)

    // Swap reactions on ORIGINAL message only
    s.MessageReactionRemove(channelID, m.ID, "👀", botID)
    if processingOK {
        s.MessageReactionAdd(channelID, m.ID, "✅")
    } else {
        s.MessageReactionAdd(channelID, m.ID, "❌")
    }

    log.Printf("[RES] channel=%s duration=%v chars=%d", responseChannel, time.Since(start).Round(time.Millisecond), len(m.Content))
}
```

**Step 2: Verify build**

```bash
cd ~/diane/server && /usr/local/go/bin/go build ./internal/discord/
```

**Step 3: Commit**

```bash
git add ~/diane/server/internal/discord/bot.go
git commit -m "feat: rewrite handleMessage with active guard, queue, and /stop"
```

---

### Summary

After these 5 tasks, the bot will:

1. **Queue concurrent messages**: If two messages arrive for the same channel/thread, the second gets queued. 👀 stays visible. After the first response, the queue automatically drains.

2. **Support `/stop`**: Typing `/stop` in a channel where a run is active immediately cancels the poll loop + cancels the run on MP + shows "🛑 Stopped". Typing `/stop` when idle shows "Nothing is currently running."

3. **Cancel on MP**: The `CancelRun` API call ensures the MP server stops expensive LLM calls/tool executions.

4. **Hermes-style clean drain**: All queued messages are processed sequentially in the same goroutine, avoiding race conditions.

**Testing:**

```bash
# Quick build check
cd ~/diane/server && /usr/local/go/bin/go build ./cmd/diane/

# Unit tests
cd ~/diane/server && /usr/local/go/bin/go test ./internal/discord/ -v -count=1

# Integration tests against live MP
cd ~/diane/server && MEMORY_TEST_TOKEN=emt_... /usr/local/go/bin/go test -v -count=1 ./memorytest/

# Live Discord test: run bot and send messages rapidly + /stop
diane bot --pidfile /tmp/bot.pid
```
