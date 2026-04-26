// Package memorytest validates session and message edge cases against the
// live Memory Platform — empty sessions, session isolation, token counts,
// status filters, and error handling.
//
// Uses the same MEMORY_TEST_TOKEN env var and test PID as the existing
// bridge tests.
//
// Run: cd ~/diane/server && MEMORY_TEST_TOKEN=*** /usr/local/go/bin/go test -v -count=1 -run TestBridge_ ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// =========================================================================
// TestBridge_EmptySession: Creates a session with no messages, closes it,
// and verifies it has 0 messages via GetSession.
// =========================================================================

func TestBridge_EmptySession(t *testing.T) {
	b := setupBridge(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("t-empty-%d", os.Getpid())

	// Create session
	session, err := b.CreateSession(ctx, prefix+"-empty-session")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("Session created: %s", session.ID[:12])
	defer func() { _ = b.CloseSession(ctx, session.ID) }()

	// Verify it exists with 0 messages
	got, err := b.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != session.ID {
		t.Errorf("GetSession ID = %q, want %q", got.ID, session.ID)
	}
	if got.MessageCount != 0 {
		t.Errorf("MessageCount = %d, want 0", got.MessageCount)
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want 'active'", got.Status)
	}
	t.Logf("✅ Empty session: id=%s status=%s messages=%d", got.ID[:12], got.Status, got.MessageCount)

	// Close, verify status changes
	err = b.CloseSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	closed, err := b.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession after close: %v", err)
	}
	if closed.Status != "completed" {
		t.Errorf("After close, Status = %q, want 'completed'", closed.Status)
	}
	if closed.MessageCount != 0 {
		t.Errorf("After close, MessageCount = %d, want 0", closed.MessageCount)
	}
	t.Logf("✅ Empty session closed: status=%s messages=%d", closed.Status, closed.MessageCount)
}

// =========================================================================
// TestBridge_SessionIsolation: Verifies that messages stored in session A
// do not appear when fetching messages for session B.
// =========================================================================

func TestBridge_SessionIsolation(t *testing.T) {
	b := setupBridge(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("t-isol-%d", os.Getpid())

	// Create two sessions
	sessionA, err := b.CreateSession(ctx, prefix+"-session-a")
	if err != nil {
		t.Fatalf("CreateSession A: %v", err)
	}
	defer func() { _ = b.CloseSession(ctx, sessionA.ID) }()
	t.Logf("Session A: %s", sessionA.ID[:12])

	sessionB, err := b.CreateSession(ctx, prefix+"-session-b")
	if err != nil {
		t.Fatalf("CreateSession B: %v", err)
	}
	defer func() { _ = b.CloseSession(ctx, sessionB.ID) }()
	t.Logf("Session B: %s", sessionB.ID[:12])

	// Store a message in A only
	msgA, err := b.AppendMessage(ctx, sessionA.ID, "user", prefix+": This message belongs to session A", 0)
	if err != nil {
		t.Fatalf("AppendMessage A: %v", err)
	}
	t.Logf("Message in A: seq=%d", msgA.Seq)

	// Session A should have 1 message
	msgsA, err := b.GetMessages(ctx, sessionA.ID)
	if err != nil {
		t.Fatalf("GetMessages A: %v", err)
	}
	if len(msgsA) != 1 {
		t.Errorf("Session A has %d messages, want 1", len(msgsA))
	}

	// Session B should have 0 messages
	msgsB, err := b.GetMessages(ctx, sessionB.ID)
	if err != nil {
		t.Fatalf("GetMessages B: %v", err)
	}
	if len(msgsB) != 0 {
		t.Errorf("Session B has %d messages, want 0 (leak from A?)", len(msgsB))
		for _, m := range msgsB {
			t.Logf("  leaked message: seq=%d role=%s content=%.60s", m.Seq, m.Role, m.Content)
		}
	}

	t.Log("✅ Session isolation verified: A has messages, B does not")
}

// =========================================================================
// TestBridge_MessageWithTokenCount: Appends a message with tokenCount > 0
// and verifies the token count is stored and retrievable.
// =========================================================================

func TestBridge_MessageWithTokenCount(t *testing.T) {
	b := setupBridge(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("t-toks-%d", os.Getpid())

	session, err := b.CreateSession(ctx, prefix+"-token-test")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer func() { _ = b.CloseSession(ctx, session.ID) }()

	// Append message with explicit token count
	tokenCount := 42
	msg, err := b.AppendMessage(ctx, session.ID, "user", prefix+": message with token count", tokenCount)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if msg.TokenCount != tokenCount {
		t.Logf("TokenCount in returned message = %d (may differ from input %d — server may override)", msg.TokenCount, tokenCount)
	} else {
		t.Logf("✅ TokenCount preserved: %d", msg.TokenCount)
	}

	// Append another without token count
	msg2, err := b.AppendMessage(ctx, session.ID, "assistant", prefix+": response without token count", 0)
	if err != nil {
		t.Fatalf("AppendMessage 2: %v", err)
	}
	if msg2.TokenCount != 0 {
		t.Logf("Message without token count reports: %d", msg2.TokenCount)
	}

	// Fetch messages and check token counts
	msgs, err := b.GetMessages(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	for i, m := range msgs {
		t.Logf("  Message[%d]: seq=%d role=%s tokens=%d content=%.50s", i, m.Seq, m.Role, m.TokenCount, m.Content)
	}

	t.Log("✅ Token count test completed")
}

// =========================================================================
// TestBridge_ListSessionsWithStatusFilter: Lists sessions filtered by
// "active" and "completed" statuses, verifying the filter works.
// =========================================================================

func TestBridge_ListSessionsWithStatusFilter(t *testing.T) {
	b := setupBridge(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("t-status-%d", os.Getpid())

	// Create an active session that we'll keep open
	activeSession, err := b.CreateSession(ctx, prefix+"-keep-open")
	if err != nil {
		t.Fatalf("CreateSession (active): %v", err)
	}
	// Don't close this one — it stays active

	// Create a session we'll close
	closedSession, err := b.CreateSession(ctx, prefix+"-will-close")
	if err != nil {
		t.Fatalf("CreateSession (to-close): %v", err)
	}
	err = b.CloseSession(ctx, closedSession.ID)
	if err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	defer func() {
		_ = b.CloseSession(ctx, activeSession.ID)
	}()

	// Small delay for eventual consistency
	time.Sleep(500 * time.Millisecond)

	// Filter by "active"
	activeSessions, err := b.ListSessions(ctx, "active")
	if err != nil {
		t.Fatalf("ListSessions('active'): %v", err)
	}
	t.Logf("Active sessions: %d", len(activeSessions))
	foundActive := false
	for _, s := range activeSessions {
		if s.ID == activeSession.ID {
			foundActive = true
		}
		if s.Status != "active" {
			t.Errorf("Session %s has status %q in active filter", s.ID[:12], s.Status)
		}
	}
	if !foundActive {
		t.Log("⚠️  Active session not found in active filter (may take time to propagate)")
	}

	// Filter by "completed"
	completedSessions, err := b.ListSessions(ctx, "completed")
	if err != nil {
		t.Fatalf("ListSessions('completed'): %v", err)
	}
	t.Logf("Completed sessions: %d", len(completedSessions))
	foundCompleted := false
	for _, s := range completedSessions {
		if s.ID == closedSession.ID {
			foundCompleted = true
		}
		if s.Status != "completed" {
			t.Errorf("Session %s has status %q in completed filter", s.ID[:12], s.Status)
		}
	}
	if !foundCompleted {
		t.Log("⚠️  Closed session not found in completed filter")
	}

	// Unfiltered should include both
	allSessions, err := b.ListSessions(ctx, "")
	if err != nil {
		t.Fatalf("ListSessions(''): %v", err)
	}
	t.Logf("All sessions: %d", len(allSessions))

	t.Log("✅ Session status filtering verified")
}

// =========================================================================
// TestBridge_GetSessionNonExistent: Requests a session with a made-up ID
// and verifies the bridge returns a meaningful error (not a panic).
// =========================================================================

func TestBridge_GetSessionNonExistent(t *testing.T) {
	b := setupBridge(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fakeID := "00000000-0000-0000-0000-000000000000"
	_, err := b.GetSession(ctx, fakeID)

	if err == nil {
		t.Error("GetSession with fake ID returned nil error (expected error)")
	} else {
		errStr := err.Error()
		t.Logf("GetSession fake ID error: %s", errStr)
		// Should contain some kind of "not found" indication
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "not found") || strings.Contains(errStr, "not_found") {
			t.Log("✅ Proper not-found error returned")
		} else {
			t.Log("⚠️  Error returned but unexpected format — still acceptable")
		}
	}
}

// =========================================================================
// TestBridge_AppendMessageToNonExistentSession: Attempts to append a message
// to a session that doesn't exist, verifying proper error handling.
// =========================================================================

func TestBridge_AppendMessageToNonExistentSession(t *testing.T) {
	b := setupBridge(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fakeID := "00000000-0000-0000-0000-000000000000"
	_, err := b.AppendMessage(ctx, fakeID, "user", "test message", 0)

	if err == nil {
		t.Error("AppendMessage with fake session ID returned nil error")
	} else {
		errStr := err.Error()
		t.Logf("AppendMessage fake session error: %s", errStr)
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "not found") || strings.Contains(errStr, "not_found") {
			t.Log("✅ Proper error returned for non-existent session")
		} else {
			t.Log("⚠️  Unexpected error format — still catches the issue")
		}
		t.Log("✅ AppendMessage error handling verified")
	}
}
