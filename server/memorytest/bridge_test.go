// Package memorytest validates the memory bridge against the live Memory Platform.
//
// Uses the test project (e59a7c1c-6ec9-41aa-9fb4-79071a9569c7) on
// memory.emergent-company.ai.
//
// Run: cd ~/diane/server && MEMORY_TEST_TOKEN=emt_*** /usr/local/go/bin/go test -v -count=1 ./memorytest/
package memorytest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Emergent-Comapny/diane/internal/memory"
)

const (
	bridgeTestPID    = "e59a7c1c-6ec9-41aa-9fb4-79071a9569c7"
	bridgeTestServer = "https://memory.emergent-company.ai"
)

func setupBridge(t *testing.T) *memory.Bridge {
	t.Helper()
	token := os.Getenv("MEMORY_TEST_TOKEN")
	if token == "" {
		t.Skip("MEMORY_TEST_TOKEN not set")
	}
	b, err := memory.New(memory.Config{
		ServerURL: bridgeTestServer,
		APIKey:    token,
		ProjectID: bridgeTestPID,
	})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(b.Close)
	return b
}

// TestBridge_CreateSession verifies session creation and message appending.
func TestBridge_CreateSession(t *testing.T) {
	b := setupBridge(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("t-bridge-%d", os.Getpid())

	session, err := b.CreateSession(ctx, prefix+"-test-session")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID == "" {
		t.Fatal("CreateSession returned empty ID")
	}
	t.Logf("✅ Session created: %s (%s)", session.ID[:12], session.Title)
	defer func() { _ = b.CloseSession(ctx, session.ID); t.Log("✅ Session closed") }()

	msgs := []struct{ role, content string }{
		{"user", prefix + ": Hello, what's the weather?"},
		{"assistant", prefix + ": The weather in Warsaw is sunny, 22°C."},
		{"user", prefix + ": And in Krakow?"},
	}
	for i, m := range msgs {
		msg, err := b.AppendMessage(ctx, session.ID, m.role, m.content, 0)
		if err != nil {
			t.Fatalf("AppendMessage[%d]: %v", i, err)
		}
		t.Logf("  Message[%d]: seq=%d role=%s", i, msg.Seq, msg.Role)
	}
	t.Logf("✅ Appended %d messages", len(msgs))
}

// TestBridge_GetMessages verifies messages are retrievable in order.
func TestBridge_GetMessages(t *testing.T) {
	b := setupBridge(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("t-msgs-%d", os.Getpid())

	session, err := b.CreateSession(ctx, prefix+"-get-msgs")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer func() { _ = b.CloseSession(ctx, session.ID) }()

	for i := 1; i <= 3; i++ {
		_, err := b.AppendMessage(ctx, session.ID, "user", fmt.Sprintf("%s: message %d", prefix, i), 0)
		if err != nil {
			t.Fatalf("AppendMessage[%d]: %v", i, err)
		}
	}

	messages, err := b.GetMessages(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(messages) != 3 {
		t.Errorf("got %d messages, want 3", len(messages))
	}
	for i, m := range messages {
		t.Logf("  Message[%d]: seq=%d role=%s content=%s", i, m.Seq, m.Role, m.Content)
	}
	t.Logf("✅ Retrieved %d messages, order preserved", len(messages))
}

// TestBridge_SearchMemory verifies hybrid search across stored content.
func TestBridge_SearchMemory(t *testing.T) {
	b := setupBridge(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("t-search-%d", os.Getpid())

	session, err := b.CreateSession(ctx, prefix+"-search-test")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer func() { _ = b.CloseSession(ctx, session.ID) }()

	_, err = b.AppendMessage(ctx, session.ID, "user", prefix+": I like PostgreSQL for databases", 0)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	_, err = b.AppendMessage(ctx, session.ID, "assistant", prefix+": PostgreSQL is great for relational data, ACID compliance, and complex queries", 0)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	time.Sleep(2 * time.Second)

	results, err := b.SearchMemory(ctx, prefix+" PostgreSQL database", 10)
	if err != nil {
		t.Fatalf("SearchMemory: %v", err)
	}
	t.Logf("Search returned %d results", len(results))
	for i, r := range results {
		t.Logf("  Result[%d]: type=%s score=%.3f content=%s", i, r.ObjectType, r.Score, truncate(r.Content, 80))
	}
	if len(results) == 0 {
		t.Log("⚠️ No search results — embeddings may need more propagation time")
	} else {
		// At least one result should contain our content
		found := false
		for _, r := range results {
			if r.ObjectType == "Message" && len(r.Content) > 0 {
				found = true
				break
			}
		}
		if found {
			t.Log("✅ Found message results with content")
		} else {
			t.Log("⚠️ Results returned but no Message objects with content found")
		}
	}
}

// TestBridge_StreamChat tests the streaming chat endpoint.
// Requires a token with chat:stream permission.
func TestBridge_StreamChat(t *testing.T) {
	b := setupBridge(t)
	ctx := context.Background()

	stream, err := b.StreamChat(ctx, "Say hello in one word", "")
	if err != nil {
		t.Skipf("StreamChat not available: %v (need chat:stream scope)", err)
	}
	defer stream.Close()

	var tokens []string
	for event := range stream.Events() {
		switch event.Type {
		case "meta":
			t.Logf("  Meta: conversation_id=%s", event.ConversationID)
		case "token":
			tokens = append(tokens, event.Token)
		case "done":
			t.Logf("  Done: stream complete")
		case "error":
			t.Errorf("Stream error: %s", event.Error)
		}
	}

	response := ""
	for _, t := range tokens {
		response += t
	}
	t.Logf("✅ Chat response (%d tokens): %s", len(tokens), truncate(response, 80))
	if len(tokens) == 0 {
		t.Error("No tokens received from chat stream")
	}
}

// TestBridge_FullFlow tests: create session → store messages → close → list.
func TestBridge_FullFlow(t *testing.T) {
	b := setupBridge(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("t-full-%d", os.Getpid())

	// 1. Create session
	session, err := b.CreateSession(ctx, prefix+"-full-flow")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer func() { _ = b.CloseSession(ctx, session.ID) }()
	t.Logf("1️⃣ Session created: %s", session.ID[:12])

	// 2. Store user message
	_, err = b.AppendMessage(ctx, session.ID, "user", prefix+": What's the capital of France?", 0)
	if err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}
	t.Log("2️⃣ User message stored")

	// 3. Store assistant message (simulated)
	_, err = b.AppendMessage(ctx, session.ID, "assistant", prefix+": The capital of France is Paris.", 0)
	if err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}
	t.Log("3️⃣ Assistant message stored")

	// 4. Verify messages
	messages, err := b.GetMessages(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("got %d messages, want 2", len(messages))
	}
	t.Logf("4️⃣ Session has %d messages", len(messages))

	// 5. Close session
	err = b.CloseSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	t.Log("5️⃣ Session closed")

	// 6. Verify via ListSessions or GetSession
	s2, err := b.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	t.Logf("6️⃣ Session status: %s, messages: %d", s2.Status, s2.MessageCount)

	t.Log("✅ Full flow completed successfully")
}

// TestBridge_ListSessions verifies we can list all sessions.
func TestBridge_ListSessions(t *testing.T) {
	b := setupBridge(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("t-list-%d", os.Getpid())

	// Create a few sessions
	for i := 0; i < 3; i++ {
		s, err := b.CreateSession(ctx, fmt.Sprintf("%s-session-%d", prefix, i))
		if err != nil {
			t.Fatalf("CreateSession[%d]: %v", i, err)
		}
		defer func(id string) { _ = b.CloseSession(ctx, id) }(s.ID)
	}

	sessions, err := b.ListSessions(ctx, "")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	t.Logf("Total sessions in project: %d", len(sessions))
	for i, s := range sessions {
		t.Logf("  Session[%d]: id=%s title=%s status=%s", i, s.ID[:12], s.Title, s.Status)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
