// Package discord provides a Discord bot that bridges messages to Diane sessions
// in the Memory Platform knowledge graph.
//
// The bot connects to the Discord Gateway via WebSocket, listens for messages,
// routes them through the Memory Bridge (session, messages, search, chat),
// and responds in a thread to keep the channel clean.
package discord

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Emergent-Comapny/diane/internal/db"
	"github.com/Emergent-Comapny/diane/internal/memory"
	"github.com/bwmarrin/discordgo"
)

// logFile is the path for debug logging. Set via initLogging.
var logFile string

// initLogging sets up file-based logging with line-buffered output.
// Logs go to both file and stderr, but the file is always readable
// regardless of pipe buffering.
func initLogging() {
	home, _ := os.UserHomeDir()
	logFile = filepath.Join(home, ".diane", "debug.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[WARN] Cannot open log file %s: %v", logFile, err)
		return
	}
	// Tee: write to both file and stderr
	multi := io.MultiWriter(f, os.Stderr)
	log.SetOutput(multi)
	log.Printf("═══════════════════════════════════════")
	log.Printf("🐝 Diane bot starting — logging to %s", logFile)
}

// dlog writes a structured debug line to the log.
// Format: [TAG] key=value key=value ...
func dlog(tag string, fields ...interface{}) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%s] ", tag))
	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			b.WriteString(fmt.Sprintf("%v=%v ", fields[i], fields[i+1]))
		} else {
			b.WriteString(fmt.Sprintf("%v", fields[i]))
		}
	}
	log.Println(b.String())
}

// Bot manages the Discord connection and message routing.
type Bot struct {
	config Config
	dg     *discordgo.Session

	mu         sync.RWMutex
	sessions   map[string]*ChannelSession // channelID → session
	sqliteDB   *db.DB                     // SQLite connection for session persistence

	typingMu    sync.RWMutex
	typingCancel map[string]context.CancelFunc // channelID → cancel for typing indicator loop

	dedupMu     sync.RWMutex
	dedupCache  map[string]time.Time // messageID → timestamp (for dedup on reconnect)
}

const dedupTTL = 5 * time.Minute // keep dedup entries for 5 minutes

// ChannelSession tracks a Discord channel's conversation in the graph.
// JSON-serializable for persistence across bot restarts.
// DO NOT add non-serializable fields here.
type ChannelSession struct {
	ChannelID    string    `json:"channel_id"`
	SessionID    string    `json:"session_id,omitempty"`
	Conversation string    `json:"conversation,omitempty"`
	AgentType    string    `json:"agent_type,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Known agent types for session routing.
const (
	AgentTypeDefault    = "default"
	AgentTypeCodebase   = "diane-codebase"
	AgentTypeResearcher = "diane-researcher"
)

// Config holds the bot's configuration.
type Config struct {
	BotToken         string   // Discord bot token (required)
	AllowedChannels  []string // Allowed channel IDs (empty = all)
	SystemPrompt     string   // System prompt for the bot
	ContextMessages  int      // Max messages to include as context per turn
	MemoryServerURL  string
	MemoryAPIKey     string
	MemoryProjectID  string
	MemoryOrgID      string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		ContextMessages:  10,
		SystemPrompt: "You are Diane, a helpful and natural AI assistant. Be conversational and direct. Answer questions clearly — whether they're about weather, code, or anything else. Do NOT try to use tools, create scenarios, or plan execution unless the user explicitly asks you to. Just talk like a normal person and answer the question.",
	}
}

// New creates a new Bot from the given config.
func New(cfg Config) (*Bot, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("discord bot: BotToken is required")
	}

	// Initialize file-based debug logging
	initLogging()

	dg, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("discordgo.New: %w", err)
	}

	dg.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent

	bot := &Bot{
		config:   cfg,
		dg:       dg,
		sessions: make(map[string]*ChannelSession),

		typingCancel: make(map[string]context.CancelFunc),
		dedupCache:   make(map[string]time.Time),
	}

	// Initialize SQLite for session persistence
	sqliteDB, err := db.New("")
	if err != nil {
		log.Printf("[WARN] SQLite unavailable — sessions won't persist across restarts: %v", err)
	} else {
		bot.sqliteDB = sqliteDB
	}

	dg.AddHandler(bot.onMessageCreate)
	dg.AddHandler(bot.onReady)

	return bot, nil
}

// AttachBridge attaches a pre-configured memory bridge to the bot.
func (b *Bot) AttachBridge(bridge *memory.Bridge) {
	globalBridge = bridge
}

var globalBridge *memory.Bridge

// Start connects the bot to Discord and blocks until shutdown.
func (b *Bot) Start() error {
	if globalBridge == nil {
		return fmt.Errorf("discord bot: no memory bridge attached — call AttachBridge first")
	}

	// Start dedup cleanup goroutine (Hermes pattern — Discord RESUME can replay events)
	b.startDedupCleanup()

	if err := b.dg.Open(); err != nil {
		return fmt.Errorf("opening Discord connection: %w", err)
	}
	defer b.dg.Close()

	log.Println("✅ Discord bot connected. Press Ctrl+C to exit.")

	// Load persisted sessions (channel→session mappings survive restarts)
	b.loadSessionsFromDB()
	if len(b.sessions) > 0 {
		log.Printf("[SES] %d session(s) restored from disk", len(b.sessions))
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
	log.Println("Shutting down Discord bot...")
	return nil
}

// ============================================================================
// Event Handlers
// ============================================================================

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	log.Printf("Bot connected as %s#%s (ID: %s)", r.User.Username, r.User.Discriminator, r.User.ID)
	log.Printf("Servers: %d — listening on %d channels", len(r.Guilds), len(b.config.AllowedChannels))
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Deduplicate: Discord Gateway can replay events on reconnect (RESUME)
	if b.isDuplicate(m.ID) {
		log.Printf("[DEDUP] Ignoring duplicate message %s", m.ID)
		return
	}

	// Ignore our own messages
	if m.Author.ID == s.State.User.ID {
		return
	}
	if m.Author.Bot {
		return
	}

	// Resolve effective channel: for threads, check parent channel
	effectiveChannelID := m.ChannelID
	ch, err := s.Channel(m.ChannelID)
	isThread := err == nil && ch.IsThread()
	if isThread {
		effectiveChannelID = ch.ParentID
	}
	if !b.isChannelAllowed(effectiveChannelID) {
		return
	}

	log.Printf("[IN]  channel=%s author=%s#%s msg=%q (thread=%v)", m.ChannelID, m.Author.Username, m.Author.Discriminator, truncateStr(m.Content, 80), isThread)

	// React with 👀 to show we've seen it
	if err := s.MessageReactionAdd(m.ChannelID, m.ID, "👀"); err != nil {
		log.Printf("Reaction add error: %v", err)
	}

	go b.handleMessage(s, m.Message)
}

// ============================================================================
// Message Handling
// ============================================================================

func (b *Bot) handleMessage(s *discordgo.Session, m *discordgo.Message) {
	start := time.Now()
	channelID := m.ChannelID
	botID := s.State.User.ID

	// Check if we're already in a thread
	ch, err := s.Channel(channelID)
	isThread := err == nil && ch.IsThread()

	var responseChannel string

	if isThread {
		// Already in a thread — respond here directly, continue the session
		responseChannel = channelID
		log.Printf("[THR] Continuing in existing thread %s", channelID)
	} else {
		// Create a new thread for this conversation (Hermes-style)
		// Phase 1: Categorize message with emoji prefix based on content heuristics
		emoji, category := categorizeMessage(m.Content)
		cleanMsg := strings.TrimSpace(m.Content)
		// Remove any existing emoji prefix from the message content to avoid double-emoji
		cleanMsg = regexp.MustCompile(`^[\p{So}\p{Sk}\p{Sc}\p{Sm}]\s*`).ReplaceAllString(cleanMsg, "")
		threadName := emoji + " " + category + ": " + truncateStr(cleanMsg, 40)
		if len(threadName) > 100 {
			threadName = threadName[:100]
		}
		if threadName == "" || threadName == emoji+" "+category+": " {
			threadName = emoji + " " + category
		}
		thread, err := s.MessageThreadStart(channelID, m.ID, threadName, 60*24) // auto-archive after 24h
		if err != nil {
			log.Printf("[WARN] Thread creation failed: %v", err)
			// Fall back to responding in the channel
			b.sendResponse(s, channelID, m, start)
			return
		}
		responseChannel = thread.ID
		log.Printf("[THR] Created thread %s (%s) in channel %s", thread.ID, threadName, channelID)
	}

	// Start persistent typing indicator (Hermes pattern — loop every 8s)
	b.startTyping(s, responseChannel)
	processingOK := false

	// Process the message through buildAndSendResponse
	response := b.buildAndSendResponse(m, responseChannel)

	// Stop typing indicator
	b.stopTyping(responseChannel)

	// Send response if we got one
	if response != "" {
		b.sendMessage(s, responseChannel, response)
		processingOK = true
	}

	// Swap reactions: 👀 → ✅ on success, 👀 → ❌ on failure (Hermes pattern)
	s.MessageReactionRemove(channelID, m.ID, "👀", botID)
	if processingOK {
		s.MessageReactionAdd(channelID, m.ID, "✅")
	} else {
		s.MessageReactionAdd(channelID, m.ID, "❌")
	}

	// Phase 2: If this was a new thread and the agent included a [TITLE: ...]
	// tag, extract it, strip it from the visible response, and rename the thread.
	if !isThread && processingOK && strings.Contains(response, "[TITLE:") {
		newTitle := extractTitleFromResponse(&response)
		if newTitle != "" {
			// Re-send with title stripped
			// (message was already sent above — edit it)
			if _, err := s.ChannelEdit(responseChannel, &discordgo.ChannelEdit{
				Name: newTitle,
			}); err != nil {
				log.Printf("[THR] Title update failed: %v", err)
			} else {
				log.Printf("[THR] Renamed thread to %q", newTitle)
				// Re-send the stripped response (remove [TITLE:...] tag)
				// We already sent the full response, so edit the last message
				if msgs, err := s.ChannelMessages(responseChannel, 1, "", "", ""); err == nil && len(msgs) > 0 && msgs[0].Author.ID == botID {
					s.ChannelMessageEdit(responseChannel, msgs[0].ID, response)
				}
			}
		}
	}

	log.Printf("[RES] channel=%s duration=%v chars=%d", responseChannel, time.Since(start).Round(time.Millisecond), len(response))
}

// sendResponse handles the full response flow and sends to a channel.
func (b *Bot) sendResponse(s *discordgo.Session, channelID string, m *discordgo.Message, start time.Time) {
	response := b.buildAndSendResponse(m, channelID)
	b.sendMessage(s, channelID, response)
	log.Printf("[RES] channel=%s duration=%v chars=%d", channelID, time.Since(start).Round(time.Millisecond), len(response))
}

// buildAndSendResponse does the actual work: session management + MP agent call.
// responseChannel is the channel where the response will be sent (used as session key).
// No fallback — if the agent fails, the user sees the error.
func (b *Bot) buildAndSendResponse(m *discordgo.Message, responseChannel string) string {
	ctx := context.Background()

	// Get or create session for this response channel (thread or parent channel)
	cs := b.getOrCreateSession(responseChannel, b.detectAgentType(m.Content))
	log.Printf("[SES] response_channel=%s session=%s agent=%s", responseChannel, cs.SessionID, cs.AgentType)

	// Determine which MP agent to use
	agentName := cs.AgentType
	if agentName == AgentTypeDefault {
		agentName = "diane-default" // Route through MP agent for tool access (web-fetch, etc.)
	}

	log.Printf("[AGT] Routing to agent: %s", agentName)
	response, err := b.triggerAgentWithContext(ctx, cs, m.Content, agentName)
	if err != nil {
		errMsg := fmt.Sprintf("❌ Agent %s failed: %v", agentName, err)
		log.Printf("[AGT] %s", errMsg)
		return errMsg
	}
	return response
}



// ============================================================================
// Session Management
// ============================================================================

func (b *Bot) getOrCreateSession(channelID string, agentType string) *ChannelSession {
	b.mu.RLock()
	cs, exists := b.sessions[channelID]
	b.mu.RUnlock()
	if exists {
		// Once an agent type is set, it sticks — don't override
		if cs.AgentType == "" && agentType != "" {
			cs.AgentType = agentType
			b.saveSession(cs)
		}
		return cs
	}
	cs = &ChannelSession{
		ChannelID: channelID,
		AgentType: agentType,
		CreatedAt: time.Now(),
	}
	b.mu.Lock()
	b.sessions[channelID] = cs
	b.mu.Unlock()
	b.saveSession(cs)
	return cs
}

// loadSessionsFromDB loads persisted channel→session mappings from SQLite.
// Merges into the current sessions map (does not overwrite existing entries).
func (b *Bot) loadSessionsFromDB() {
	if b.sqliteDB == nil {
		return
	}
	all, err := b.sqliteDB.GetAllDiscordSessions()
	if err != nil {
		log.Printf("[SES] Error reading persisted sessions: %v", err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	count := 0
	for _, s := range all {
		if _, exists := b.sessions[s.ChannelID]; !exists {
			b.sessions[s.ChannelID] = &ChannelSession{
				ChannelID:    s.ChannelID,
				SessionID:    s.SessionID,
				Conversation: s.Conversation,
				AgentType:    s.AgentType,
				CreatedAt:    s.CreatedAt,
			}
			count++
		}
	}
	log.Printf("[SES] Restored %d/%d sessions from SQLite", count, len(all))
}

// saveSession writes a single channel→session mapping to SQLite.
// Safe to call from any goroutine.
func (b *Bot) saveSession(cs *ChannelSession) {
	if b.sqliteDB == nil {
		return
	}
	if err := b.sqliteDB.UpsertDiscordSession(&db.DiscordSession{
		ChannelID:    cs.ChannelID,
		SessionID:    cs.SessionID,
		Conversation: cs.Conversation,
		AgentType:    cs.AgentType,
	}); err != nil {
		log.Printf("[SES] Error persisting session: %v", err)
	}
}

// ============================================================================
// Discord Helpers
// ============================================================================

func (b *Bot) isChannelAllowed(channelID string) bool {
	if len(b.config.AllowedChannels) == 0 {
		return true
	}
	for _, id := range b.config.AllowedChannels {
		if id == channelID {
			return true
		}
	}
	return false
}

func (b *Bot) sendMessage(s *discordgo.Session, channelID, content string) {
	const maxLen = 1900
	if content == "" {
		return
	}
	if len(content) <= maxLen {
		_, err := s.ChannelMessageSend(channelID, content)
		if err != nil {
			log.Printf("[ERR] Send message: %v", err)
		}
		return
	}
	for _, part := range splitMessage(content, maxLen) {
		_, err := s.ChannelMessageSend(channelID, part)
		if err != nil {
			log.Printf("[ERR] Send message part: %v", err)
			return
		}
	}
}

// startTyping starts a persistent typing indicator loop (Hermes pattern).
// Discord's typing indicator lasts ~10s, so we re-trigger it every 8s
// until stopTyping is called.
func (b *Bot) startTyping(s *discordgo.Session, channelID string) {
	b.typingMu.Lock()
	defer b.typingMu.Unlock()

	// Don't start a duplicate loop
	if _, exists := b.typingCancel[channelID]; exists {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	b.typingCancel[channelID] = cancel

	go func() {
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()

		// Send the first typing indicator immediately
		s.ChannelTyping(channelID)

		for {
			select {
			case <-ticker.C:
				s.ChannelTyping(channelID)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// stopTyping stops the persistent typing indicator for a channel.
func (b *Bot) stopTyping(channelID string) {
	b.typingMu.Lock()
	defer b.typingMu.Unlock()

	if cancel, exists := b.typingCancel[channelID]; exists {
		cancel()
		delete(b.typingCancel, channelID)
	}
}

// isDuplicate checks if a message ID was already processed (dedup on reconnect).
// Records the message ID if new. Periodically cleaned up by startDedupCleanup.
func (b *Bot) isDuplicate(msgID string) bool {
	b.dedupMu.Lock()
	defer b.dedupMu.Unlock()
	if _, exists := b.dedupCache[msgID]; exists {
		return true
	}
	b.dedupCache[msgID] = time.Now()
	return false
}

// startDedupCleanup runs a background goroutine that clears old dedup entries.
// Call once from New or Start.
func (b *Bot) startDedupCleanup() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			b.dedupMu.Lock()
			now := time.Now()
			for id, ts := range b.dedupCache {
				if now.Sub(ts) > dedupTTL {
					delete(b.dedupCache, id)
				}
			}
			b.dedupMu.Unlock()
		}
	}()
}

// ============================================================================
// Agent Routing
// ============================================================================

// detectAgentType guesses the agent type based on message content.
// Explicit: "!codebase do X" or "!research Y" prefixes take priority.
// Heuristic: codebase/keywords → diane-codebase, research keywords → diane-researcher.
func (b *Bot) detectAgentType(content string) string {
	if content == "" {
		return AgentTypeDefault
	}

	// Check explicit prefixes
	lower := strings.ToLower(strings.TrimSpace(content))
	if strings.HasPrefix(lower, "!codebase") || strings.HasPrefix(lower, "!cb ") {
		return AgentTypeCodebase
	}
	if strings.HasPrefix(lower, "!research") || strings.HasPrefix(lower, "!rs ") {
		return AgentTypeResearcher
	}

	// Heuristic detection for codebase/graph keywords
	codebaseKeywords := []string{
		"codebase", "scenario", "graph", "competitor", "competitive",
		"dependency", "tech stack", "code analysis", "architecture",
	}
	for _, kw := range codebaseKeywords {
		if strings.Contains(lower, kw) {
			return AgentTypeCodebase
		}
	}

	// Heuristic detection for research keywords
	researchKeywords := []string{
		"research", "investigate", "find out about", "look up",
		"deep dive", "analyze", "compare", "survey",
	}
	for _, kw := range researchKeywords {
		if strings.Contains(lower, kw) {
			return AgentTypeResearcher
		}
	}

	return AgentTypeDefault
}

// triggerAgentWithContext triggers a Memory Platform agent with the user's message
// and returns the response text. It creates a runtime agent, triggers it, polls for
// completion, fetches the response + tool calls, cleans up, and returns the text.
// If includeTools is true, appends a short tool usage indicator to the response.
func (b *Bot) triggerAgentWithContext(ctx context.Context, cs *ChannelSession, userMsg string, agentName string) (string, error) {

	isNewSession := cs.SessionID == ""

	// 0. Ensure we have a Memory session for cross-run context
	// The session ID is passed to the agent in the trigger prompt so it can
	// search past messages. Without this, each restarted bot loses all context.
	if cs.SessionID == "" {
		sessionTitle := fmt.Sprintf("Discord #%s", cs.ChannelID)
		if cs.AgentType != AgentTypeDefault && cs.AgentType != "" {
			sessionTitle = fmt.Sprintf("[%s] %s", cs.AgentType, sessionTitle)
		}
		session, err := globalBridge.CreateSession(ctx, sessionTitle)
		if err == nil {
			cs.SessionID = session.ID
			b.saveSession(cs)
			log.Printf("[SES] Created session %s for channel %s", session.ID[:12], cs.ChannelID)
		} else {
			log.Printf("[WARN] Failed to create session: %v", err)
		}
	}

	// 1. Find agent definition by name
	defs, err := globalBridge.ListAgentDefs(ctx)
	if err != nil {
		dlog("AGT", "err", "list_defs", "msg", err.Error())
		return "", fmt.Errorf("list agent defs: %w", err)
	}
	var defID string
	if defs != nil {
		for _, d := range defs.Data {
			if d.Name == agentName {
				defID = d.ID
				break
			}
		}
	}
	if defID == "" {
		dlog("AGT", "err", "def_not_found", "agent", agentName, "available", len(defs.Data))
		return "", fmt.Errorf("agent definition %q not found on Memory Platform", agentName)
	}
	dlog("AGT", "action", "found_def", "agent", agentName, "def_id", defID[:12])

	// 2. Create a runtime agent (named identically for exact-name resolution)
	runtimeName := fmt.Sprintf("discord-%s-%d", agentName, time.Now().UnixMilli())
	agent, err := globalBridge.CreateRuntimeAgent(ctx, runtimeName, defID)
	if err != nil {
		dlog("AGT", "err", "create_runtime", "name", runtimeName, "msg", err.Error())
		return "", fmt.Errorf("create runtime agent: %w", err)
	}
	agentID := agent.Data.ID
	dlog("AGT", "action", "created_runtime", "agent_id", agentID[:12])

	// Ensure cleanup
	defer func() {
		if delErr := globalBridge.Client().Agents.Delete(ctx, agentID); delErr != nil {
			log.Printf("[AGT] Failed to clean up runtime agent %s: %v", agentID, delErr)
		} else {
			log.Printf("[AGT] Cleaned up runtime agent %s", agentID)
		}
	}()

	// 3. Build trigger prompt with session context
	triggerPrompt := userMsg
	if cs.SessionID != "" {
		if isNewSession {
			// For the first message in a new session, ask agent to suggest a thread title
			triggerPrompt = fmt.Sprintf(
				"[Session: %s]\n\n"+
					"After responding to this message, suggest a brief thread title with a relevant emoji prefix. "+
					"Place it at the end of your response as: [TITLE: 🐛 Login Bug]\n"+
					"Choose your prefix from: 🐛 Bug, ✨ Feature, ❓ Question, 💬 Discussion, 💡 Idea, 🔧 Fix, ⚠️ Issue, 📚 Research, 🚀 Release, 🎨 Design\n"+
					"Make it concise (under 50 chars including emoji).\n\n%s",
				cs.SessionID, userMsg)
		} else {
			triggerPrompt = fmt.Sprintf("[Session: %s]\n%s", cs.SessionID, userMsg)
		}
	}

	dlog("AGT", "action", "triggering", "prompt_chars", len(triggerPrompt))
	triggerResp, err := globalBridge.TriggerAgentWithInput(ctx, agentID, triggerPrompt)
	if err != nil {
		dlog("AGT", "err", "trigger", "msg", err.Error())
		return "", fmt.Errorf("trigger agent: %w", err)
	}
	if !triggerResp.Success || triggerResp.RunID == nil {
		errMsg := "unknown error"
		if triggerResp.Error != nil {
			errMsg = *triggerResp.Error
		}
		dlog("AGT", "err", "trigger_failed", "msg", errMsg)
		return "", fmt.Errorf("trigger failed: %s", errMsg)
	}
	runID := *triggerResp.RunID
	dlog("AGT", "action", "run_started", "run_id", runID[:12], "agent", agentName)

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

fetchResponse:
	// 5. Get the final response from messages
	msgs, err := globalBridge.GetRunMessages(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("get run messages: %w", err)
	}

	// DEBUG: dump all messages and their content keys
	dlog("MSG", "run", runID[:12], "count", len(msgs.Data))
	for idx, msg := range msgs.Data {
		var keys []string
		for k := range msg.Content {
			keys = append(keys, k)
		}
		dlog("MSG", "idx", idx, "role", msg.Role, "step", msg.StepNumber, "keys", strings.Join(keys, ","), "raw", fmt.Sprintf("%v", msg.Content))
	}

	// Extract the last assistant message's content
	// Role is the agent NAME (e.g., "diane-default"), not "assistant"
	var responseText string
	if msgs != nil {
		for i := len(msgs.Data) - 1; i >= 0; i-- {
			msg := msgs.Data[i]
			dlog("EXTR", "check_idx", i, "role", msg.Role, "keys", fmt.Sprintf("%v", func() []string {
				var keys []string
				for k := range msg.Content { keys = append(keys, k) }
				return keys
			}()))
			if msg.Role == "user" || msg.Role == "tool" {
				continue
			}
			// Try every key — the text might be nested under a slice
			for key, val := range msg.Content {
				s := extractText(val)
				if len(s) > 20 {
					dlog("EXTR", "found_in_key", key, "len", len(s), "preview", truncateStr(s, 80))
					responseText = s
					break
				}
			}
			if responseText != "" {
				break
			}
			dlog("EXTR", "skip_msg", i, "no_content_found", fmt.Sprintf("%v", msg.Content))
		}
	}
	dlog("EXTR", "responseText_len", len(responseText), "responseText_preview", truncateStr(responseText, 120))
	if responseText == "" {
		responseText = "✅ Agent completed, but produced no visible response."
	}

	duration := time.Since(pollStart).Round(time.Millisecond)
	dlog("AGT", "action", "completed", "agent", agentName, "response_len", len(responseText), "duration", duration.String())

	// 6. Build Hermes-style tool trail (SHOWN BEFORE the response)
	toolTrail := b.buildToolTrail(ctx, runID)
	if toolTrail != "" && !strings.Contains(responseText, "🔧") {
		responseText = toolTrail + "\n\n" + responseText
	}

	// 7. Store messages in session for cross-run context
	if cs.SessionID != "" {
		go func() {
			storeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			globalBridge.AppendMessage(storeCtx, cs.SessionID, "user", userMsg, 0)
			globalBridge.AppendMessage(storeCtx, cs.SessionID, "assistant", responseText, 0)
		}()
	}

	return responseText, nil
}

// buildToolTrail returns a Hermes-style tool call trail like:
//
//	🌐 web-fetch: "example.com"
//	🔍 search-hybrid: "user query"
//
// Returns empty string if no tool calls or on error.
// The trail is shown BEFORE the response (Hermes pattern).
func (b *Bot) buildToolTrail(ctx context.Context, runID string) string {
	tcResp, err := globalBridge.GetRunToolCalls(ctx, runID)
	if err != nil {
		dlog("TOOL", "err", err.Error())
		return ""
	}
	if tcResp == nil || len(tcResp.Data) == 0 {
		return ""
	}

	var lines []string
	for _, tc := range tcResp.Data {
		if tc.ToolName == "" {
			continue
		}
		emoji := toolEmoji(tc.ToolName)
		name := shortToolName(tc.ToolName)
		input := shortToolInput(tc.Input)

		line := fmt.Sprintf("%s %s: `%s`", emoji, name, input)

		// Check for error in output
		if tc.Output != nil {
			if errStr, ok := tc.Output["error"]; ok {
				line += fmt.Sprintf(" %s %v", "⚠️", errStr)
			}
		}

		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// toolEmoji returns an emoji for a tool name (Hermes-style).
func toolEmoji(name string) string {
	m := map[string]string{
		"web-search-brave":    "🌐",
		"web-search-reddit":   "🌐",
		"web-fetch":           "🌐",
		"search-hybrid":       "🔍",
		"search-knowledge":    "🔍",
		"search-semantic":     "🔍",
		"search-similar":      "🔍",
		"entity-query":        "📄",
		"entity-search":       "📄",
		"entity-edges-get":    "📄",
		"entity-type-list":    "📄",
		"entity-create":       "✏️",
		"graph-traverse":      "🗺️",
		"tag-list":            "🏷️",
		"list_available_agents": "💬",
		"spawn_agents":        "🔧",
		"skill":               "📚",
		"skill-list":          "📚",
		"skill-get":           "📚",
	}
	if e, ok := m[name]; ok {
		return e
	}
	return "⚙️"
}

// shortToolName returns a readable short version of a tool name for display.
func shortToolName(name string) string {
	short := map[string]string{
		"web-search-brave":  "web-search",
		"web-search-reddit": "reddit",
		"search-hybrid":     "hybrid-search",
		"search-knowledge":  "k-search",
		"search-semantic":   "semantic-search",
		"search-similar":    "similar-search",
		"entity-query":      "entity-query",
		"entity-search":     "entity-search",
		"entity-edges-get":  "entity-edges",
		"entity-type-list":  "entity-types",
		"entity-create":     "entity-create",
		"web-fetch":         "fetch",
		"graph-traverse":    "graph-traverse",
		"tag-list":          "tags",
		"list_available_agents": "agents",
		"spawn_agents":      "spawn",
		"skill":             "skill",
		"skill-list":        "skills",
		"skill-get":         "skill-get",
	}
	if s, ok := short[name]; ok {
		return s
	}
	return name
}

// shortToolInput returns a compact string representation of tool input.
func shortToolInput(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	// Try common keys
	for _, key := range []string{"query", "url", "name", "text", "message", "id", "question"} {
		if v, ok := input[key]; ok {
			s := fmt.Sprintf("%v", v)
			if len(s) > 60 {
				s = s[:57] + "..."
			}
			return s
		}
	}
	// Fallback: just show keys
	var keys []string
	for k := range input {
		keys = append(keys, k)
	}
	return strings.Join(keys, ",")
}

// extractText tries to extract meaningful text from a content value.
// Values are often []interface{} slices from JSON parsing; this unwraps them.
func extractText(val any) string {
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		var parts []string
		for _, item := range v {
			switch s := item.(type) {
			case string:
				s = strings.TrimSpace(s)
				if s != "" {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]interface{}:
		// Recurse into nested maps
		for _, subval := range v {
			if s := extractText(subval); len(s) > 20 {
				return s
			}
		}
	}
	// Fallback: strip Go-format wrapping brackets
	s := fmt.Sprintf("%v", val)
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	s = strings.TrimSpace(s)
	return s
}

// ============================================================================
// Utility
// ============================================================================

func splitMessage(content string, maxLen int) []string {
	var parts []string
	for len(content) > maxLen {
		splitAt := strings.LastIndex(content[:maxLen], "\n")
		if splitAt < 0 {
			splitAt = maxLen
		}
		parts = append(parts, strings.TrimSpace(content[:splitAt]))
		content = strings.TrimSpace(content[splitAt:])
	}
	if content != "" {
		parts = append(parts, content)
	}
	return parts
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// categorizeMessage scans message content for keywords and returns a relevant
// emoji prefix and category label for thread naming.
func categorizeMessage(content string) (emoji, category string) {
	lower := strings.ToLower(content)

	// Compile-once patterns using sync.Once would be preferred, but these are
	// lightweight enough for per-message calls.
	bugPat := regexp.MustCompile(`\b(bug|broken|error|crash|glitch|fault|defect|regression)\b`)
	featurePat := regexp.MustCompile(`\b(feature|suggest|would be great|would like|add |implement|idea|maybe we could|what if|how about|can you make|request)\b`)
	questionPat := regexp.MustCompile(`\b(how do|what is|where is|why does|can someone|anyone know|question|do you know|is there a way|how to|what does)\b`)
	fixPat := regexp.MustCompile(`\b(fix|issue|problem|wrong|not working|doesn't work|broken|fail|failure|repair)\b`)
	researchPat := regexp.MustCompile(`\b(research|investigate|find out about|look into|deep dive|survey|docs? about|learn about)\b`)

	// Questions (check before fix since "why does something not work" is both)
	if questionPat.MatchString(lower) || strings.HasSuffix(strings.TrimSpace(lower), "?") {
		return "❓", "Question"
	}
	// Bug reports — high precision patterns
	if bugPat.MatchString(lower) {
		return "🐛", "Bug"
	}
	// Feature requests / ideas
	if featurePat.MatchString(lower) {
		return "✨", "Feature"
	}
	// Fix / troubleshooting
	if fixPat.MatchString(lower) {
		return "🔧", "Fix"
	}
	// Research / documentation
	if researchPat.MatchString(lower) {
		return "📚", "Research"
	}
	// Default
	return "💬", "Chat"
}

// extractTitleFromResponse looks for a [TITLE: ...] tag in the response text,
// strips it, and returns the title. If no tag is found, returns "" and the
// response is left unchanged.
func extractTitleFromResponse(response *string) string {
	if response == nil || *response == "" {
		return ""
	}
	re := regexp.MustCompile(`\[TITLE:\s*(.+?)\]`)
	matches := re.FindStringSubmatch(*response)
	if len(matches) < 2 {
		return ""
	}
	title := strings.TrimSpace(matches[1])
	// Strip the tag from the response (only the first occurrence)
	*response = strings.Replace(*response, matches[0], "", 1)
	*response = strings.TrimSpace(*response)
	return title
}
