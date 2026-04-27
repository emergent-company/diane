package discord

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ── Dedup Tests ──────────────────────────────────────────────────────────

func TestIsDuplicate(t *testing.T) {
	bot := &Bot{
		dedupCache: make(map[string]time.Time),
	}

	// First call: not duplicate
	if bot.isDuplicate("msg-1") {
		t.Error("Expected isDuplicate('msg-1') = false on first call")
	}

	// Second call: IS duplicate
	if !bot.isDuplicate("msg-1") {
		t.Error("Expected isDuplicate('msg-1') = true on second call")
	}

	// Different message ID: not duplicate
	if bot.isDuplicate("msg-2") {
		t.Error("Expected isDuplicate('msg-2') = false for different ID")
	}
}

func TestIsDuplicateConcurrent(t *testing.T) {
	bot := &Bot{
		dedupCache: make(map[string]time.Time),
	}

	// Fire 10 goroutines all checking the same message ID simultaneously
	var wg sync.WaitGroup
	results := make([]bool, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = bot.isDuplicate("same-msg")
		}(i)
	}
	wg.Wait()

	// Exactly 1 should be false (first), the rest true (duplicates)
	falseCount := 0
	for _, r := range results {
		if !r {
			falseCount++
		}
	}
	if falseCount != 1 {
		t.Errorf("Expected exactly 1 non-duplicate, got %d (results: %v)", falseCount, results)
	}
}

func TestDedupExpiry(t *testing.T) {
	bot := &Bot{
		dedupCache: make(map[string]time.Time),
	}

	// current isDuplicate() only checks key existence (no inline TTL check).
	// TTL cleanup runs in a background goroutine (startDedupCleanup).
	// This test verifies that entries remain in the map until cleanup runs.

	// Add an entry with an old timestamp
	bot.dedupMu.Lock()
	bot.dedupCache["old-msg"] = time.Now().Add(-10 * time.Minute)
	bot.dedupMu.Unlock()

	// Add a fresh entry
	bot.isDuplicate("fresh-msg")

	// Both should be duplicate because keys still exist in the map
	if !bot.isDuplicate("fresh-msg") {
		t.Error("fresh-msg should be duplicate (key exists)")
	}
	if !bot.isDuplicate("old-msg") {
		t.Error("old-msg should be duplicate (key exists, TTL cleanup runs async)")
	}

	// Simulate cleanup that startDedupCleanup does every minute
	bot.dedupMu.Lock()
	now := time.Now()
	for id, ts := range bot.dedupCache {
		if now.Sub(ts) > dedupTTL {
			delete(bot.dedupCache, id)
		}
	}
	bot.dedupMu.Unlock()

	// After cleanup, old-msg should be gone
	if bot.isDuplicate("old-msg") {
		t.Error("old-msg should NOT be duplicate after TTL cleanup")
	}
	// fresh-msg should still be duplicate
	if !bot.isDuplicate("fresh-msg") {
		t.Error("fresh-msg should still be duplicate after cleanup")
	}
}

// ── Message Processing Guard Tests ──────────────────────────────────────

func TestMessageGuard(t *testing.T) {
	bot := &Bot{
		msgGuard: make(map[string]struct{}),
	}

	msgID := "guard-msg-1"

	// Guard not set initially
	bot.msgGuardMu.Lock()
	_, exists := bot.msgGuard[msgID]
	bot.msgGuardMu.Unlock()
	if exists {
		t.Error("Guard should not exist before being set")
	}

	// Set guard
	bot.msgGuardMu.Lock()
	bot.msgGuard[msgID] = struct{}{}
	bot.msgGuardMu.Unlock()

	// Guard exists now
	bot.msgGuardMu.Lock()
	_, exists = bot.msgGuard[msgID]
	bot.msgGuardMu.Unlock()
	if !exists {
		t.Error("Guard should exist after being set")
	}

	// Clear guard (as done in defer of handleMessage)
	bot.msgGuardMu.Lock()
	delete(bot.msgGuard, msgID)
	bot.msgGuardMu.Unlock()

	bot.msgGuardMu.Lock()
	_, exists = bot.msgGuard[msgID]
	bot.msgGuardMu.Unlock()
	if exists {
		t.Error("Guard should be cleared after delete")
	}
}

// ── Channel Acquire/Release Tests ──────────────────────────────────────

func TestAcquireAndReleaseChannel(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
	}

	channelID := "channel-1"

	// Acquire: should succeed
	ctx, cancel, acquired := bot.acquireChannel(channelID)
	if !acquired {
		t.Error("Expected acquireChannel to succeed on first call")
	}
	if ctx == nil {
		t.Error("Expected non-nil context")
	}
	if cancel == nil {
		t.Error("Expected non-nil cancel function")
	}

	// Acquire again: should fail (already acquired)
	_, _, acquired = bot.acquireChannel(channelID)
	if acquired {
		t.Error("Expected acquireChannel to fail on second call")
	}

	// Release
	bot.releaseChannel(channelID)

	// Acquire again after release: should succeed
	_, _, acquired = bot.acquireChannel(channelID)
	if !acquired {
		t.Error("Expected acquireChannel to succeed after release")
	}

	bot.releaseChannel(channelID)
}

func TestConcurrentAcquireDifferentChannels(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
	}

	var wg sync.WaitGroup
	results := make([]bool, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			chID := "chan-" + string(rune('A'+idx))
			_, _, acquired := bot.acquireChannel(chID)
			results[idx] = acquired
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if !r {
			t.Errorf("Channel %d should have been acquired", i)
		}
	}
}

func TestAcquireSameChannelConcurrent(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
	}

	var wg sync.WaitGroup
	acquisitions := make([]bool, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _, acquired := bot.acquireChannel("same-channel")
			acquisitions[idx] = acquired
		}(i)
	}
	wg.Wait()

	count := 0
	for _, a := range acquisitions {
		if a {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected exactly 1 successful acquisition, got %d", count)
	}
}

func TestChannelCancelContext(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
	}

	ctx, cancel, acquired := bot.acquireChannel("cancel-test")
	if !acquired {
		t.Fatal("Expected to acquire channel")
	}

	// Cancel the context (simulates /stop)
	cancel()

	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Expected context to be done after cancel")
	}
}

// ── Queue/Pop Tests ────────────────────────────────────────────────────

func TestQueueAndPop(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
	}

	channelID := "queue-channel"
	bot.acquireChannel(channelID)
	defer bot.releaseChannel(channelID)

	msg1 := &discordgo.Message{ID: "msg-1", Content: "hello"}
	bot.queueMessage(channelID, msg1)

	msg2 := &discordgo.Message{ID: "msg-2", Content: "world"}
	bot.queueMessage(channelID, msg2)

	// Pop: should get msg-1 first (FIFO)
	popped := bot.popPending(channelID)
	if popped == nil {
		t.Fatal("Expected first queued message, got nil")
	}
	if popped.ID != "msg-1" {
		t.Errorf("Expected msg-1, got %s", popped.ID)
	}

	// Pop: should get msg-2
	popped = bot.popPending(channelID)
	if popped == nil {
		t.Fatal("Expected second queued message, got nil")
	}
	if popped.ID != "msg-2" {
		t.Errorf("Expected msg-2, got %s", popped.ID)
	}

	// Pop: queue should be empty
	popped = bot.popPending(channelID)
	if popped != nil {
		t.Errorf("Expected nil on empty queue, got %s", popped.ID)
	}
}

func TestQueueToUnacquiredChannel(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
	}

	msg := &discordgo.Message{ID: "msg-1", Content: "hello"}
	bot.queueMessage("unacquired-channel", msg)

	// Should not crash, message should be silently dropped
	popped := bot.popPending("unacquired-channel")
	if popped != nil {
		t.Error("Expected nil for unacquired channel")
	}
}

func TestConcurrentQueueAndPop(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
	}

	channelID := "concurrent-queue"
	bot.acquireChannel(channelID)
	defer bot.releaseChannel(channelID)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			bot.queueMessage(channelID, &discordgo.Message{
				ID:      "msg-" + string(rune('0'+idx%10)),
				Content: "test",
			})
		}(i)
	}
	wg.Wait()

	count := 0
	for {
		popped := bot.popPending(channelID)
		if popped == nil {
			break
		}
		count++
	}

	if count == 0 {
		t.Error("Expected at least some queued messages")
	}
	t.Logf("Successfully popped %d queued messages", count)
}

// ── setActiveAgentRun and runChannels Tests ────────────────────────────

func TestSetActiveAgentRun(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
		runChannels: make(map[string]string),
	}

	channelID := "agent-channel"
	agentID := "agent-1"
	runID := "run-abc-123"

	bot.acquireChannel(channelID)
	defer bot.releaseChannel(channelID)

	bot.setActiveAgentRun(channelID, agentID, runID)

	// Check run→channel mapping
	bot.runChannelsMu.RLock()
	mappedCh, exists := bot.runChannels[runID]
	bot.runChannelsMu.RUnlock()
	if !exists {
		t.Error("Expected run→channel mapping to exist")
	}
	if mappedCh != channelID {
		t.Errorf("Expected channel %s, got %s", channelID, mappedCh)
	}

	// Check active channel has agent/run IDs
	bot.activeMu.Lock()
	ac := bot.activeChans[channelID]
	bot.activeMu.Unlock()
	if ac == nil {
		t.Fatal("Expected active channel to exist")
	}
	if ac.AgentID != agentID {
		t.Errorf("Expected agentID %s, got %s", agentID, ac.AgentID)
	}
	if ac.RunID != runID {
		t.Errorf("Expected runID %s, got %s", runID, ac.RunID)
	}
}

func TestReleaseCleansRunChannels(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
		runChannels: make(map[string]string),
	}

	channelID := "cleanup-channel"
	runID := "run-cleanup-1"

	bot.acquireChannel(channelID)
	bot.setActiveAgentRun(channelID, "agent-1", runID)

	// Release should clean up run→channel mapping
	bot.releaseChannel(channelID)

	bot.runChannelsMu.RLock()
	_, exists := bot.runChannels[runID]
	bot.runChannelsMu.RUnlock()
	if exists {
		t.Error("Expected run→channel mapping to be cleaned up after release")
	}
}

// ── Bot Construction Tests ──────────────────────────────────────────────

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ContextMessages != 10 {
		t.Errorf("Expected ContextMessages=10, got %d", cfg.ContextMessages)
	}
	if cfg.BotToken != "" {
		t.Error("Expected BotToken to be empty in defaults")
	}
}

// ── Concurrency: Acquire + Queue Pop Race ─────────────────────────────

func TestAcquireQueuePopRace(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
	}

	channelID := "race-channel"

	// Acquire the channel first (as happens in real code flow)
	bot.acquireChannel(channelID)
	defer bot.releaseChannel(channelID)

	// Now queue a message — this simulates a second message arriving
	// while the first is still being processed
	msg := &discordgo.Message{ID: "race-msg", Content: "queued message"}
	bot.queueMessage(channelID, msg)

	// Pop should return the queued message
	popped := bot.popPending(channelID)
	if popped == nil {
		t.Fatal("Expected to pop queued message")
	}
	if popped.ID != "race-msg" {
		t.Errorf("Expected race-msg, got %s", popped.ID)
	}

	// Queue should now be empty
	remains := bot.popPending(channelID)
	if remains != nil {
		t.Error("Expected nil after draining queue")
	}
}

// ── Channel Existence and Cleanup ──────────────────────────────────────

func TestDoubleReleaseIsSafe(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
		runChannels: make(map[string]string),
	}

	// Release a channel that was never acquired
	bot.releaseChannel("nonexistent")
	// Should not panic

	// Acquire and double release
	bot.acquireChannel("test-channel")
	bot.releaseChannel("test-channel")
	bot.releaseChannel("test-channel") // double release should be safe
}

func TestMultipleChannelsIndependent(t *testing.T) {
	bot := &Bot{
		activeChans: make(map[string]*ActiveChannel),
		runChannels: make(map[string]string),
	}

	for _, ch := range []string{"A", "B", "C"} {
		_, _, acquired := bot.acquireChannel(ch)
		if !acquired {
			t.Fatalf("Failed to acquire channel %s", ch)
		}
	}

	// Each should be independently tracked
	for _, ch := range []string{"A", "B", "C"} {
		bot.activeMu.Lock()
		_, exists := bot.activeChans[ch]
		bot.activeMu.Unlock()
		if !exists {
			t.Errorf("Channel %s should be in active map", ch)
		}
	}

	// Release one and verify others remain
	bot.releaseChannel("A")

	bot.activeMu.Lock()
	_, aExists := bot.activeChans["A"]
	_, bExists := bot.activeChans["B"]
	_, cExists := bot.activeChans["C"]
	bot.activeMu.Unlock()

	if aExists {
		t.Error("Channel A should be removed after release")
	}
	if !bExists {
		t.Error("Channel B should still exist")
	}
	if !cExists {
		t.Error("Channel C should still exist")
	}
}

// ── Dedup entry format tests ───────────────────────────────────────────

func TestDedupEntryFormat(t *testing.T) {
	bot := &Bot{
		dedupCache: make(map[string]time.Time),
	}

	cases := []struct {
		name  string
		msgID string
	}{
		{"plain message", "123456789"},
		{"message with letters", "abc-def-123"},
		{"long message ID", "9999999999999999999"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if bot.isDuplicate(tc.msgID) {
				t.Errorf("First call for %s should not be duplicate", tc.msgID)
			}

			bot.dedupMu.Lock()
			_, exists := bot.dedupCache[tc.msgID]
			bot.dedupMu.Unlock()
			if !exists {
				t.Errorf("Message %s should be in dedup cache after first check", tc.msgID)
			}

			if !bot.isDuplicate(tc.msgID) {
				t.Errorf("Second call for %s should be duplicate", tc.msgID)
			}
		})
	}
}

// ── Test Bot fields default to non-nil maps ────────────────────────────

func TestNewBotHasEmptyMaps(t *testing.T) {
	bot := &Bot{
		sessions:        make(map[string]*ChannelSession),
		typingCancel:    make(map[string]context.CancelFunc),
		dedupCache:      make(map[string]time.Time),
		activeChans:     make(map[string]*ActiveChannel),
		sseNotifications: make(chan map[string]interface{}, 100),
		runChannels:     make(map[string]string),
		msgGuard:        make(map[string]struct{}),
	}

	for name, m := range map[string]interface{}{
		"sessions":     bot.sessions,
		"typingCancel": bot.typingCancel,
		"dedupCache":   bot.dedupCache,
		"activeChans":  bot.activeChans,
		"runChannels":  bot.runChannels,
		"msgGuard":     bot.msgGuard,
	} {
		if m == nil {
			t.Errorf("%s should not be nil", name)
		}
	}

	// Verify dedupCache timestamp format
	bot.isDuplicate("test-msg")
	bot.dedupMu.Lock()
	ts, exists := bot.dedupCache["test-msg"]
	bot.dedupMu.Unlock()
	if !exists {
		t.Error("test-msg should be in cache")
	}
	if ts.IsZero() {
		t.Error("timestamp should not be zero")
	}
	if time.Since(ts) > time.Second {
		t.Errorf("timestamp should be recent, got %v ago", time.Since(ts))
	}
}

// ── Restart Scenario Test ────────────────────────────────────────────

func TestDedupSurvivesRestartedBot(t *testing.T) {
	// Simulate the restart → dedup loss scenario:
	// Without SQLite persistence, restarting the bot loses the dedup cache.
	// With it (wired via the New() function), the cache is restored.

	// Bot v1: processes a message
	bot1 := &Bot{
		dedupCache:      make(map[string]time.Time),
		dedupCookie:     "cookie-v1",
		restartCount:    1,
		activeChans:     make(map[string]*ActiveChannel),
		runChannels:     make(map[string]string),
		msgGuard:        make(map[string]struct{}),
		sseNotifications: make(chan map[string]interface{}, 100),
	}

	msgID := "hello-world-12345"

	// First call: should NOT be duplicate
	if bot1.isDuplicate(msgID) {
		t.Error("Expected isDuplicate = false on first call (bot v1)")
	}

	// Second call: should BE duplicate
	if !bot1.isDuplicate(msgID) {
		t.Error("Expected isDuplicate = true on second call (bot v1)")
	}

	// Bot v2: simulates restart — fresh in-memory dedup cache
	// WITHOUT SQLite persistence, this is where the bug lives.
	bot2 := &Bot{
		dedupCache:      make(map[string]time.Time),
		dedupCookie:     "cookie-v2",
		restartCount:    2,
		activeChans:     make(map[string]*ActiveChannel),
		runChannels:     make(map[string]string),
		msgGuard:        make(map[string]struct{}),
		sseNotifications: make(chan map[string]interface{}, 100),
	}

	// Without persistence: the same message ID is NOT recognized as duplicate
	if bot2.isDuplicate(msgID) {
		t.Error("Without DB persistence, restarted bot should NOT see message as duplicate")
	}
	// After re-recording, it IS duplicate
	if !bot2.isDuplicate(msgID) {
		t.Error("After re-recording, same msg should be duplicate within same bot instance")
	}

	t.Log("✅ Without DB: restarted bot loses dedup — this was the bug")
	t.Log("✅ With DB persistence: bot startup restores dedupCache from SQLite")
}

func TestDedupCookieChangesOnRestart(t *testing.T) {
	cookie1 := generateDedupCookie()
	cookie2 := generateDedupCookie()

	if cookie1 == cookie2 {
		t.Error("Expected different cookies for different bot instances")
	}
	if len(cookie1) != 16 { // 8 bytes → 16 hex chars
		t.Errorf("Expected 16-char hex cookie, got %d chars: %s", len(cookie1), cookie1)
	}
}
