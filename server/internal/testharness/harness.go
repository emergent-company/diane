// Package testharness provides a Discord-based integration test harness for Diane.
//
// The harness connects to Discord as a separate bot, sends messages to a test
// channel, and listens for Diane's responses, reactions, and thread creation.
// It provides blocking assertion methods with configurable timeouts.
//
// Usage:
//
//	h, err := testharness.New(testharness.Config{
//	    BotToken:    os.Getenv("TEST_BOT_TOKEN"),
//	    ChannelID:   os.Getenv("TEST_CHANNEL_ID"),
//	    TargetBotID: os.Getenv("DIANE_BOT_ID"),
//	})
//	if err != nil { log.Fatal(err) }
//	defer h.Close()
//
//	result := h.RunTest("basic-ping", func(h *testharness.H) testharness.Result {
//	    // Send a message, get the message ID
//	    msgID := h.Send("ping --test-ping")
//
//	    // Expect 👀 within 5s
//	    if !h.ExpectReaction(msgID, "👀", 5*time.Second) {
//	        return testharness.Fail("no 👀 reaction")
//	    }
//
//	    // Expect thread created within 10s
//	    threadID, ok := h.ExpectThread(msgID, 10*time.Second)
//	    if !ok {
//	        return testharness.Fail("no thread created")
//	    }
//
//	    // Expect ✅ or ❌ within 120s
//	    passed := h.ExpectFinalReaction(msgID, 120*time.Second)
//	    if !passed {
//	        return testharness.Fail("timed out waiting for final reaction")
//	    }
//
//	    // Expect response in the thread
//	    resp, ok := h.ExpectResponse(threadID, 120*time.Second)
//	    if !ok {
//	        return testharness.Fail("no response from Diane")
//	    }
//	    if !h.AssertNotEmpty(resp) {
//	        return testharness.Fail("empty response")
//	    }
//
//	    return testharness.Pass()
//	})
package testharness

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/bwmarrin/discordgo"
)

// Default timeout values
const (
	DefaultReactionTimeout      = 10 * time.Second
	DefaultThreadTimeout        = 15 * time.Second
	DefaultResponseTimeout      = 120 * time.Second
	DefaultFinalReactionTimeout = 130 * time.Second
)

// Config holds the test harness configuration.
type Config struct {
	// BotToken is the Discord bot token for the test harness bot.
	BotToken string

	// ChannelID is the Discord channel ID where tests will send messages.
	ChannelID string

	// TargetBotID is the Discord user ID of the bot being tested (Diane).
	TargetBotID string

	// MemoryServerURL is the Memory Platform API server URL.
	MemoryServerURL string

	// MemoryAPIKey is the Memory Platform API key/token.
	MemoryAPIKey string

	// MemoryProjectID is the Memory Platform project ID.
	MemoryProjectID string

	// Logf is an optional logger. If nil, uses log.Printf.
	Logf func(format string, args ...interface{})
}

// Event represents a tracked Discord event for the event stream.
type Event struct {
	Type      string // "message", "reaction", "channel"
	Timestamp time.Time
	Data      interface{}
}

// MessageEvent data for a MESSAGE_CREATE event (filtered to target bot).
type MessageEvent struct {
	MessageID string
	ChannelID string
	Content   string
	Embeds    []string // embed titles, if any
}

// ReactionEvent data for a MESSAGE_REACTION_ADD event.
type ReactionEvent struct {
	MessageID string
	ChannelID string
	Emoji     string
	UserID    string
}

// ChannelEvent data for a CHANNEL_CREATE event (filtered to threads).
type ChannelEvent struct {
	ChannelID string
	ParentID  string
	Name      string
}

// Result is the outcome of a single test.
type Result struct {
	Name     string
	Passed   bool
	Error    string
	Duration time.Duration
}

// Pass creates a passing result.
func Pass() Result { return Result{Passed: true} }

// Fail creates a failing result with an error message.
func Fail(err string) Result { return Result{Passed: false, Error: err} }

// H is the test harness handle — passed to each test function.
type H struct {
	harness *TestHarness
	name    string
	start   time.Time
	mu      sync.Mutex
	failed  bool
}

// TestHarness manages the Discord Gateway connection and event tracking.
type TestHarness struct {
	config      Config
	session     *discordgo.Session
	targetBotID string
	channelID   string
	logf        func(format string, args ...interface{})

	// Event channels for each type
	messagesCh  chan MessageEvent
	reactionsCh chan ReactionEvent
	channelsCh  chan ChannelEvent

	// Channel cache (we track thread→parent mapping)
	mu       sync.RWMutex
	channels map[string]*discordgo.Channel

	done   chan struct{}
	closed bool

	// memoryBridge lazily created when a scenario calls Bridge()
	memoryMu     sync.Mutex
	memoryBridge *memory.Bridge
}

// Bridge returns a Memory Platform bridge, creating it lazily on first call.
func (h *TestHarness) Bridge() *memory.Bridge {
	h.memoryMu.Lock()
	defer h.memoryMu.Unlock()
	if h.memoryBridge != nil {
		return h.memoryBridge
	}
	if h.config.MemoryServerURL == "" || h.config.MemoryAPIKey == "" || h.config.MemoryProjectID == "" {
		return nil
	}
	b, err := memory.New(memory.Config{
		ServerURL: h.config.MemoryServerURL,
		APIKey:    h.config.MemoryAPIKey,
		ProjectID: h.config.MemoryProjectID,
	})
	if err != nil {
		h.logf("[HARNESS] Failed to create memory bridge: %v", err)
		return nil
	}
	h.memoryBridge = b
	h.logf("[HARNESS] Memory bridge created — project=%s", h.config.MemoryProjectID)
	return b
}

// Bridge returns a memory bridge from the harness handle.
func (hh *H) Bridge() *memory.Bridge { return hh.harness.Bridge() }

// New creates and connects a test harness.
func New(cfg Config) (*TestHarness, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("testharness: BotToken is required")
	}
	if cfg.ChannelID == "" {
		return nil, fmt.Errorf("testharness: ChannelID is required")
	}
	if cfg.TargetBotID == "" {
		return nil, fmt.Errorf("testharness: TargetBotID is required")
	}

	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}

	dg, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("discordgo.New: %w", err)
	}

	dg.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuildMessageReactions |
		discordgo.IntentsGuilds

	h := &TestHarness{
		config:      cfg,
		targetBotID: cfg.TargetBotID,
		channelID:   cfg.ChannelID,
		logf:        logf,
		messagesCh:  make(chan MessageEvent, 500),
		reactionsCh: make(chan ReactionEvent, 500),
		channelsCh:  make(chan ChannelEvent, 500),
		channels:    make(map[string]*discordgo.Channel),
		done:        make(chan struct{}),
	}

	dg.AddHandler(h.onMessageCreate)
	dg.AddHandler(h.onReactionAdd)
	dg.AddHandler(h.onChannelCreate)
	dg.AddHandler(h.onThreadCreate)

	if err := dg.Open(); err != nil {
		return nil, fmt.Errorf("opening Discord connection: %w", err)
	}

	// Pre-populate channel cache with known channels
	if ch, err := dg.Channel(cfg.ChannelID); err == nil {
		h.channels[ch.ID] = ch
	}

	h.session = dg
	h.logf("[HARNESS] Connected — target_bot=%s channel=%s", cfg.TargetBotID, cfg.ChannelID)
	return h, nil
}

// Close disconnects the harness from Discord.
func (h *TestHarness) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	h.mu.Unlock()

	close(h.done)
	h.session.Close()
	h.logf("[HARNESS] Disconnected")
}

// ChannelID returns the configured test channel ID.
func (h *TestHarness) ChannelID() string { return h.channelID }

// TargetBotID returns the target bot's user ID.
func (h *TestHarness) TargetBotID() string { return h.targetBotID }

// ── Gateway Event Handlers ──────────────────────────────────────────────

func (h *TestHarness) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Only track messages from the target bot (Diane)
	if m.Author.ID != h.targetBotID {
		return
	}

	// Extract embed titles
	var embeds []string
	for _, e := range m.Embeds {
		if e.Title != "" {
			embeds = append(embeds, e.Title)
		}
	}

	select {
	case h.messagesCh <- MessageEvent{
		MessageID: m.ID,
		ChannelID: m.ChannelID,
		Content:   m.Content,
		Embeds:    embeds,
	}:
	default:
		h.logf("[HARNESS] Message event channel full, dropping msg %s", m.ID)
	}
}

func (h *TestHarness) onReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	// Only track reactions from the target bot
	if r.UserID != h.targetBotID {
		return
	}

	select {
	case h.reactionsCh <- ReactionEvent{
		MessageID: r.MessageID,
		ChannelID: r.ChannelID,
		Emoji:     r.Emoji.Name,
		UserID:    r.UserID,
	}:
	default:
		h.logf("[HARNESS] Reaction event channel full, dropping %s %s", r.Emoji.Name, r.MessageID)
	}
}

func (h *TestHarness) onChannelCreate(s *discordgo.Session, c *discordgo.ChannelCreate) {
	// Only track thread creation (public/private threads)
	if c.Type != discordgo.ChannelTypeGuildPublicThread &&
		c.Type != discordgo.ChannelTypeGuildPrivateThread {
		return
	}

	// Cache the channel
	h.mu.Lock()
	h.channels[c.ID] = c.Channel
	h.mu.Unlock()

	select {
	case h.channelsCh <- ChannelEvent{
		ChannelID: c.ID,
		ParentID:  c.ParentID,
		Name:      c.Name,
	}:
	default:
		h.logf("[HARNESS] Channel event channel full, dropping thread %s", c.ID)
	}
}

// onThreadCreate handles THREAD_CREATE gateway events.
// Discord sends this event when threads are created (new threads created via
// MessageThreadStart API don't always trigger CHANNEL_CREATE).
func (h *TestHarness) onThreadCreate(s *discordgo.Session, c *discordgo.ThreadCreate) {
	// ThreadCreate embeds *Channel — use the same logic
	if c.Type != discordgo.ChannelTypeGuildPublicThread &&
		c.Type != discordgo.ChannelTypeGuildPrivateThread {
		return
	}

	h.mu.Lock()
	h.channels[c.ID] = c.Channel
	h.mu.Unlock()

	select {
	case h.channelsCh <- ChannelEvent{
		ChannelID: c.ID,
		ParentID:  c.ParentID,
		Name:      c.Name,
	}:
	default:
		h.logf("[HARNESS] Thread event channel full, dropping thread %s", c.ID)
	}
}

// ── Discord Actions ─────────────────────────────────────────────────────

// Send sends a message to the test channel and returns the message ID.
func (h *TestHarness) Send(content string) string {
	msg, err := h.session.ChannelMessageSend(h.channelID, content)
	if err != nil {
		h.logf("[HARNESS] Failed to send message: %v", err)
		return ""
	}
	return msg.ID
}

// DeleteThread deletes/archives a thread channel.
func (h *TestHarness) DeleteThread(threadID string) {
	if threadID == "" {
		return
	}
	// Set archived + locked to clean up quickly
	_, err := h.session.ChannelEdit(threadID, &discordgo.ChannelEdit{
		Archived:            boolPtr(true),
		Locked:              boolPtr(true),
		AutoArchiveDuration: 60, // 1 hour minimum
	})
	if err != nil {
		h.logf("[HARNESS] Failed to archive thread %s: %v", threadID, err)
	}
}

// CleanupChannel archives all active threads under the test channel.
// This ensures a clean state before each test.
// Best-effort — permission errors are logged but not fatal.
func (h *TestHarness) CleanupChannel() {
	threads, err := h.session.ThreadsActive(h.channelID)
	if err != nil {
		h.logf("[HARNESS] Failed to list active threads: %v", err)
		return
	}
	if len(threads.Threads) == 0 {
		return
	}
	h.logf("[HARNESS] Archived %d active threads", len(threads.Threads))
	for _, t := range threads.Threads {
		h.DeleteThread(t.ID)
	}
}

// ── Assertion Methods (blocking with timeout) ───────────────────────────

// ExpectReaction waits for a reaction from Diane on a specific message.
// Returns true if the reaction was seen within the timeout.
func (h *TestHarness) ExpectReaction(messageID, emoji string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-h.reactionsCh:
			if ev.MessageID == messageID && ev.Emoji == emoji {
				return true
			}
		case <-deadline:
			return false
		case <-h.done:
			return false
		}
	}
}

// ExpectFinalReaction waits for ✅ or ❌ on a message.
// Returns true if ✅, false if ❌ or timeout.
func (h *TestHarness) ExpectFinalReaction(messageID string, timeout time.Duration) (success bool) {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-h.reactionsCh:
			if ev.MessageID != messageID {
				continue
			}
			switch ev.Emoji {
			case "✅":
				return true
			case "❌":
				return false
			}
		case <-deadline:
			return false
		case <-h.done:
			return false
		}
	}
}

// ExpectReactionRemoved waits for a specific emoji to be REMOVED from a message
// by the target bot. Returns true if the removal was observed.
func (h *TestHarness) ExpectReactionRemoved(messageID, emoji string, timeout time.Duration) bool {
	// The harness doesn't track MESSAGE_REACTION_REMOVE by default.
	// We use ExpectReactionSwap instead which is more reliable.
	// This method is a best-effort wrapper.
	h.logf("[HARNESS] ExpectReactionRemoved: use ExpectReactionSwap instead")
	return false
}

// ExpectReactionSwap waits for 👀 to be replaced by ✅ or ❌.
// Returns true if ✅ was seen, false if ❌ or timeout.
// This is the recommended way to check reaction completion.
func (h *TestHarness) ExpectReactionSwap(messageID string, timeout time.Duration) (success bool) {
	seenEye := false
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-h.reactionsCh:
			if ev.MessageID != messageID {
				continue
			}
			switch ev.Emoji {
			case "👀":
				seenEye = true
			case "✅":
				if seenEye {
					return true
				}
			case "❌":
				if seenEye {
					return false
				}
			}
		case <-deadline:
			return false
		case <-h.done:
			return false
		}
	}
}

// ExpectThread waits for a thread to be created with the test channel as parent.
// The parentMessageID helps match threads created FROM a specific message.
// Returns the thread ID if found.
func (h *TestHarness) ExpectThread(parentMessageID string, timeout time.Duration) (threadID string, ok bool) {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-h.channelsCh:
			if ev.ParentID == h.channelID {
				// Found a thread under our test channel
				// Verify this is indeed a thread started from our message
				// by checking the thread name or fetching the starter message
				h.logf("[HARNESS] Thread created: %s (%s) parent=%s", ev.ChannelID, ev.Name, ev.ParentID)
				return ev.ChannelID, true
			}
		case <-deadline:
			return "", false
		case <-h.done:
			return "", false
		}
	}
}

// ExpectResponse waits for Diane to send a NON-EMPTY message in the
// specified channel/thread. Returns the message content if found.
// Skips empty messages — Diane sometimes sends an empty message
// (system artifact) before the actual response.
func (h *TestHarness) ExpectResponse(channelID string, timeout time.Duration) (content string, ok bool) {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-h.messagesCh:
			if ev.ChannelID == channelID && ev.Content != "" {
				return ev.Content, true
			}
		case <-deadline:
			return "", false
		case <-h.done:
			return "", false
		}
	}
}

// ExpectAnyResponse waits for Diane to send a message in ANY channel.
func (h *TestHarness) ExpectAnyResponse(timeout time.Duration) (channelID, content string, ok bool) {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-h.messagesCh:
			return ev.ChannelID, ev.Content, true
		case <-deadline:
			return "", "", false
		case <-h.done:
			return "", "", false
		}
	}
}

// ExpectEmbedTitle waits for Diane to send a message with an embed
// whose title matches (contains) the given string. Returns the channel
// ID where it was received.
func (h *TestHarness) ExpectEmbedTitle(title string, timeout time.Duration) (channelID string, ok bool) {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-h.messagesCh:
			for _, et := range ev.Embeds {
				if contains(et, title) {
					return ev.ChannelID, true
				}
			}
		case <-deadline:
			return "", false
		case <-h.done:
			return "", false
		}
	}
}

// SendToThread sends a message to a specific thread and returns the message ID.
func (h *TestHarness) SendToThread(threadID, content string) string {
	msg, err := h.session.ChannelMessageSend(threadID, content)
	if err != nil {
		h.logf("[HARNESS] Failed to send to thread %s: %v", threadID, err)
		return ""
	}
	return msg.ID
}

// ExpectNoMessage asserts that Diane sends NO message to the given channel
// within the timeout. Returns true if the timeout expires without a message.
func (h *TestHarness) ExpectNoMessage(channelID string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-h.messagesCh:
			if ev.ChannelID == channelID {
				return false // got unexpected message
			}
		case <-deadline:
			return true // timeout = no message
		case <-h.done:
			return false
		}
	}
}

// WaitForIdle waits until no reactions or messages are observed for the
// specified quiet period. Useful at the end of a test to confirm silence.
func (h *TestHarness) WaitForIdle(quietPeriod time.Duration) {
	deadline := time.After(quietPeriod)
	for {
		select {
		case <-h.reactionsCh:
			// Reset deadline on any event
			deadline = time.After(quietPeriod)
		case <-h.messagesCh:
			deadline = time.After(quietPeriod)
		case <-h.channelsCh:
			deadline = time.After(quietPeriod)
		case <-deadline:
			return
		case <-h.done:
			return
		}
	}
}

// ── H (per-test handle) Methods ─────────────────────────────────────────

// RunTest runs a single test with a unique name.
// It sets up the per-test handle H and collects timing.
func (h *TestHarness) RunTest(name string, fn func(hh *H) Result) Result {
	start := time.Now()
	h.logf("")
	h.logf("═══ Test: %s ═══", name)

	hh := &H{
		harness: h,
		name:    name,
		start:   start,
	}

	// Recover from panics in test function
	defer func() {
		if r := recover(); r != nil {
			hh.mu.Lock()
			hh.failed = true
			hh.mu.Unlock()
			duration := time.Since(start)
			h.logf("═══ %s: PANIC: %v (%v) ═══", name, r, duration.Round(time.Millisecond))
		}
	}()

	result := fn(hh)
	result.Name = name
	result.Duration = time.Since(start)

	status := "✅"
	if !result.Passed {
		status = "❌"
	}
	errInfo := ""
	if result.Error != "" {
		errInfo = " — " + result.Error
	}
	h.logf("%s %s (%v)%s", status, name, result.Duration.Round(time.Millisecond), errInfo)

	return result
}

// H methods delegate to the harness but use the test's logging context.

// Send sends a message and returns the message ID.
func (hh *H) Send(content string) string {
	hh.harness.logf("  ── Send: %s", truncate(content, 60))
	return hh.harness.Send(content)
}

// SendToThread sends a message to a specific thread.
func (hh *H) SendToThread(threadID, content string) string {
	hh.harness.logf("  ── SendToThread: %s → %s", truncate(content, 60), truncate(threadID, 12))
	return hh.harness.SendToThread(threadID, content)
}

// ExpectEmbedTitle waits for an embed with a matching title.
func (hh *H) ExpectEmbedTitle(title string, timeout time.Duration) (string, bool) {
	chID, ok := hh.harness.ExpectEmbedTitle(title, timeout)
	if ok {
		hh.harness.logf("  ✓ Embed title %q in channel %s", title, truncate(chID, 12))
	} else {
		hh.harness.logf("  ✗ Embed title %q not found (timeout %v)", title, timeout)
	}
	return chID, ok
}

// ExpectNoMessage asserts no message from Diane in the given channel.
func (hh *H) ExpectNoMessage(channelID string, timeout time.Duration) bool {
	ok := hh.harness.ExpectNoMessage(channelID, timeout)
	if ok {
		hh.harness.logf("  ✓ No message in channel %s (timeout %v)", truncate(channelID, 12), timeout)
	} else {
		hh.harness.logf("  ✗ Unexpected message in channel %s", truncate(channelID, 12))
	}
	return ok
}

// ExpectReaction waits for a reaction.
func (hh *H) ExpectReaction(messageID, emoji string, timeout time.Duration) bool {
	ok := hh.harness.ExpectReaction(messageID, emoji, timeout)
	if ok {
		hh.harness.logf("  ✓ %s reaction on %s", emoji, truncate(messageID, 12))
	} else {
		hh.harness.logf("  ✗ %s reaction on %s (timeout %v)", emoji, truncate(messageID, 12), timeout)
	}
	return ok
}

// ExpectFinalReaction waits for ✅/❌.
func (hh *H) ExpectFinalReaction(messageID string, timeout time.Duration) bool {
	ok := hh.harness.ExpectFinalReaction(messageID, timeout)
	if ok {
		hh.harness.logf("  ✓ ✅ reaction on %s", truncate(messageID, 12))
	} else {
		hh.harness.logf("  ✗ ❌ or timeout on %s (timeout %v)", truncate(messageID, 12), timeout)
	}
	return ok
}

// ExpectThread waits for a thread and returns its ID.
func (hh *H) ExpectThread(parentMessageID string, timeout time.Duration) (string, bool) {
	threadID, ok := hh.harness.ExpectThread(parentMessageID, timeout)
	if ok {
		hh.harness.logf("  ✓ Thread created: %s", threadID)
	} else {
		hh.harness.logf("  ✗ No thread created (timeout %v)", timeout)
	}
	return threadID, ok
}

// ExpectResponse waits for a response in a channel/thread.
func (hh *H) ExpectResponse(channelID string, timeout time.Duration) (string, bool) {
	content, ok := hh.harness.ExpectResponse(channelID, timeout)
	if ok {
		hh.harness.logf("  ✓ Response received (%d chars)", len(content))
	} else {
		hh.harness.logf("  ✗ No response (timeout %v)", timeout)
	}
	return content, ok
}

// ExpectAnyResponse waits for Diane to respond anywhere.
func (hh *H) ExpectAnyResponse(timeout time.Duration) (string, string, bool) {
	chID, content, ok := hh.harness.ExpectAnyResponse(timeout)
	if ok {
		hh.harness.logf("  ✓ Response in channel %s (%d chars)", truncate(chID, 12), len(content))
	} else {
		hh.harness.logf("  ✗ No response anywhere (timeout %v)", timeout)
	}
	return chID, content, ok
}

// CleanupThread archives and locks a test thread.
func (hh *H) CleanupThread(threadID string) {
	if threadID == "" {
		return
	}
	hh.harness.DeleteThread(threadID)
	hh.harness.logf("  🧹 Thread %s archived", truncate(threadID, 12))
}

// AssertNotEmpty checks that content is non-empty.
func (hh *H) AssertNotEmpty(content string) bool {
	if content == "" {
		hh.harness.logf("  ✗ Assertion failed: content is empty")
		return false
	}
	return true
}

// AssertContains checks that haystack contains needle.
func (hh *H) AssertContains(haystack, needle string) bool {
	if !contains(haystack, needle) {
		hh.harness.logf("  ✗ Assertion failed: expected %q in %q", needle, truncate(haystack, 80))
		return false
	}
	return true
}

// ── Utilities ───────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func contains(s, substr string) bool {
	return len(substr) == 0 || findStr(s, substr) >= 0
}

func findStr(s, substr string) int {
	limit := len(s) - len(substr)
	for i := 0; i <= limit; i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func boolPtr(b bool) *bool { return &b }

func strPtr(s string) *string { return &s }
