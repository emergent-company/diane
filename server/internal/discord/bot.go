// Package discord provides a Discord bot that bridges messages to Diane sessions
// in the Memory Platform knowledge graph.
//
// The bot connects to the Discord Gateway via WebSocket, listens for messages,
// routes them through the Memory Bridge (session, messages, search, chat),
// and responds in a thread to keep the channel clean.
package discord

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Emergent-Comapny/diane/internal/db"
	"github.com/Emergent-Comapny/diane/internal/events"
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
	api    DiscordAPI // wraps dg; can be swapped for testing

	mu       sync.RWMutex
	sessions map[string]*ChannelSession // channelID → session
	sqliteDB *db.DB                     // SQLite connection for session persistence

	typingMu     sync.RWMutex
	typingCancel map[string]context.CancelFunc // channelID → cancel for typing indicator loop

	dedupMu    sync.RWMutex
	dedupCache map[string]time.Time // messageID → timestamp (for dedup on reconnect)

	// dedupCookie / restartCount — used for detecting bot restarts in logs.
	// Each bot instance generates a random hex cookie at startup. If the
	// cookie changes between log entries, the bot restarted.
	DedupCookie  string // random hex — changes on every restart
	RestartCount int    // incremented at startup

	// msgGuard prevents simultaneous duplicate MessageCreate events from
	// Discord Gateway. Unlike dedupCache (which handles RESUME replay across
	// time), msgGuard catches two events arriving at nearly the same instant.
	// Guard is set BEFORE spawning handleMessage goroutine and cleared via
	// defer inside handleMessage.
	msgGuardMu sync.Mutex
	msgGuard   map[string]struct{} // message IDs currently being processed

	activeMu    sync.Mutex
	activeChans map[string]*ActiveChannel // responseChannel → active processing

	sseClient        *events.Client
	sseNotifications chan map[string]interface{} // buffered channel for all SSE notification events
	sendChannelID    string                      // pre-configured channel ID for notifications

	// runChannels maps agent run IDs to Discord channel/thread IDs for thread-local
	// agent question delivery. Populated when a run starts, cleaned up on release.
	runChannels   map[string]string // runID → channelID
	runChannelsMu sync.RWMutex

	// questionChannelID is the persistent channel for agent questions, set via
	// /set_ask_channel command. Gets priority after thread-local routing.
	questionChannelID string

	// buildResponseFn, if set, overrides buildAndSendResponse for testing.
	// Allows thread routing tests without needing a real memory bridge.
	buildResponseFn func(ctx context.Context, m *discordgo.Message, responseChannel string) string
}

const dedupTTL = 5 * time.Minute // keep dedup entries for 5 minutes

// ActiveChannel tracks an in-progress agent run for a channel.
// Used for concurrency guard, interrupt support, and /stop.
type ActiveChannel struct {
	Cancel   context.CancelFunc   // cancels the poll loop in triggerAgentWithContext
	AgentID  string               // runtime agent ID (for CancelRun API)
	RunID    string               // current run ID (for CancelRun API)
	Pending  []*discordgo.Message // queued messages waiting to be processed
	ParentID string               // parent channel ID (for thread-based sessions, empty for inline)
}

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
	BotToken        string   // Discord bot token (required)
	AllowedChannels []string // Allowed channel IDs (empty = all)
	ThreadChannels  []string // Channel IDs where auto-thread creation happens (empty = thread everywhere)
	SystemPrompt    string   // System prompt for the bot
	ContextMessages int      // Max messages to include as context per turn
	MemoryServerURL string
	MemoryAPIKey    string
	MemoryProjectID string
	MemoryOrgID     string
	// SSEEventStream enables the agent_question notification listener.
	// Requires MemoryServerURL, MemoryAPIKey, and MemoryProjectID.
	SSEEventStream bool
	// TestBotIDs is a list of bot user IDs that bypass the m.Author.Bot filter.
	// This allows test harness bots to send messages that Diane will process.
	// Empty = only human messages are processed (default, production-safe).
	TestBotIDs []string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		ContextMessages: 10,
		SystemPrompt:    "You are Diane, a helpful and natural AI assistant. Be conversational and direct. Answer questions clearly — whether they're about weather, code, or anything else. Do NOT try to use tools, create scenarios, or plan execution unless the user explicitly asks you to. Just talk like a normal person and answer the question.",
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

	// Determine notification channel: use first allowed channel, or a default
	notifChannel := ""
	if len(cfg.AllowedChannels) > 0 {
		notifChannel = cfg.AllowedChannels[0]
	}

	bot := &Bot{
		config:   cfg,
		dg:       dg,
		api:      newDiscordAPI(dg),
		sessions: make(map[string]*ChannelSession),

		typingCancel:     make(map[string]context.CancelFunc),
		dedupCache:       make(map[string]time.Time),
		DedupCookie:      generateDedupCookie(),
		RestartCount:     1,
		msgGuard:         make(map[string]struct{}),
		activeChans:      make(map[string]*ActiveChannel),
		sseNotifications: make(chan map[string]interface{}, 100),
		sendChannelID:    notifChannel,
		runChannels:      make(map[string]string),
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
	dg.AddHandler(bot.onInteractionCreate)

	// Set up SSE listener for Notification Platform events
	if cfg.SSEEventStream && cfg.MemoryServerURL != "" && cfg.MemoryAPIKey != "" && cfg.MemoryProjectID != "" {
		bot.sseClient = events.NewClient(
			cfg.MemoryServerURL,
			cfg.MemoryAPIKey,
			cfg.MemoryProjectID,
			bot.handleNotificationEvent,
		)
		log.Println("[SSE] Notification listener configured")
	} else if cfg.SSEEventStream {
		log.Println("[WARN] SSEEventStream enabled but Memory config incomplete — skipping")
	}

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

	// Load persisted ask_channel config
	if b.sqliteDB != nil {
		if chID, err := b.sqliteDB.GetConfig("ask_channel"); err == nil && chID != "" {
			b.questionChannelID = chID
			log.Printf("[CFG] Loaded ask_channel: %s", chID)
		}
	}

	// Start SSE listener for Notification Platform events
	if b.sseClient != nil {
		b.sseClient.Start()
		go b.dispatchNotifications()
		log.Println("[SSE] Notification listener started")
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	log.Println("Shutting down Discord bot...")

	// Shut down SSE listener
	if b.sseClient != nil {
		b.sseClient.Stop()
		log.Println("[SSE] Notification listener stopped")
	}

	return nil
}

// ============================================================================
// Event Handlers
// ============================================================================

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	log.Printf("Bot connected as %s#%s (ID: %s)", r.User.Username, r.User.Discriminator, r.User.ID)
	log.Printf("Servers: %d — listening on %d channels", len(r.Guilds), len(b.config.AllowedChannels))
}

// onInteractionCreate handles Discord interaction events (button clicks, select menus, modal submits).
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionMessageComponent:
		b.handleComponentInteraction(s, i.Interaction)
	case discordgo.InteractionModalSubmit:
		b.handleModalSubmit(s, i.Interaction)
	default:
		log.Printf("[INT] Unhandled interaction type: %v", i.Type)
	}
}

// handleComponentInteraction processes button clicks, select menu selections, and modal triggers
// on agent question embeds.
func (b *Bot) handleComponentInteraction(s *discordgo.Session, i *discordgo.Interaction) {
	customID := i.MessageComponentData().CustomID
	username := "(no member)"
	if i.Member != nil && i.Member.User != nil {
		username = i.Member.User.Username
	}
	log.Printf("[INT] Component interaction: custom_id=%s user=%s", customID, username)

	// Select menu interactions (aq-sel:<question_id> or aq-msel:<question_id>)
	if strings.HasPrefix(customID, "aq-sel:") || strings.HasPrefix(customID, "aq-msel:") {
		b.handleSelectMenu(s, i, customID)
		return
	}

	// Stop session button interactions
	if strings.HasPrefix(customID, "stop-thread:") || strings.HasPrefix(customID, "stop-all:") || customID == "stop-cancel" {
		b.handleStopSelection(s, i, customID)
		return
	}

	// Button interactions: "aq:<question_id>:<response_value>"
	if !strings.HasPrefix(customID, "aq:") {
		log.Printf("[INT] Unknown custom_id format: %s", customID)
		return
	}

	parts := strings.SplitN(customID, ":", 3)
	if len(parts) < 3 {
		log.Printf("[INT] Invalid custom_id format: %s", customID)
		return
	}

	questionID := parts[1]
	responseValue := parts[2]

	// Special value "__text__" triggers a modal for text input
	if responseValue == "__text__" {
		b.openTextModal(s, i, questionID)
		return
	}

	// Standard button response
	b.respondToQuestion(s, i, questionID, responseValue, responseValue)
}

// handleSelectMenu processes select menu (dropdown) selections on agent questions.
func (b *Bot) handleSelectMenu(s *discordgo.Session, i *discordgo.Interaction, customID string) {
	questionID := strings.TrimPrefix(customID, "aq-sel:")
	questionID = strings.TrimPrefix(questionID, "aq-msel:")

	vals := i.MessageComponentData().Values
	if len(vals) == 0 {
		log.Printf("[INT] Select menu with no values, skipping")
		return
	}

	// Multi-select: join values with comma
	responseValue := vals[0]
	responseLabel := vals[0]
	if len(vals) > 1 {
		responseValue = strings.Join(vals, ",")
		responseLabel = fmt.Sprintf("%s (+%d more)", vals[0], len(vals)-1)
	}

	b.respondToQuestion(s, i, questionID, responseValue, responseLabel)
}

// openTextModal shows a Discord modal for free-text input.
func (b *Bot) openTextModal(s *discordgo.Session, i *discordgo.Interaction, questionID string) {
	err := b.api.InteractionRespond(i, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: fmt.Sprintf("aq-modal:%s", questionID),
			Title:    "Respond to Agent",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "response",
							Label:       "Your response",
							Style:       discordgo.TextInputParagraph,
							Placeholder: "Type your response here...",
							Required:    true,
							MinLength:   1,
							MaxLength:   1000,
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Printf("[INT] Failed to open modal: %v", err)
	}
}

// handleModalSubmit processes modal form submissions for agent questions.
func (b *Bot) handleModalSubmit(s *discordgo.Session, i *discordgo.Interaction) {
	customID := i.ModalSubmitData().CustomID
	username := "(no member)"
	if i.Member != nil && i.Member.User != nil {
		username = i.Member.User.Username
	}
	log.Printf("[INT] Modal submit: custom_id=%s user=%s", customID, username)

	if !strings.HasPrefix(customID, "aq-modal:") {
		log.Printf("[INT] Unknown modal custom_id: %s", customID)
		return
	}

	questionID := strings.TrimPrefix(customID, "aq-modal:")

	// Extract text from the modal response.
	// Components are []MessageComponent where each is an *ActionsRow
	// containing *TextInput components.
	var responseValue string
	for _, row := range i.ModalSubmitData().Components {
		actionRow, ok := row.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, comp := range actionRow.Components {
			if input, ok := comp.(*discordgo.TextInput); ok && input.CustomID == "response" {
				responseValue = input.Value
			}
		}
	}

	if responseValue == "" {
		log.Printf("[INT] Empty modal response, skipping")
		return
	}

	// Use first 50 chars as the display label
	responseLabel := responseValue
	if len(responseLabel) > 50 {
		responseLabel = responseLabel[:47] + "..."
	}

	b.respondToQuestion(s, i, questionID, responseValue, responseLabel)
}

// respondToQuestion sends the user's response to MP and updates the embed.
func (b *Bot) respondToQuestion(s *discordgo.Session, i *discordgo.Interaction, questionID, responseValue, responseLabel string) {
	log.Printf("[INT] Responding to question %s with %q", questionID[:12], responseValue)

	// Acknowledge the interaction immediately (Discord requires this within 3s)
	err := b.api.InteractionRespond(i, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if err != nil {
		log.Printf("[INT] Failed to acknowledge interaction: %v", err)
		return
	}

	// Submit the response to MP's agent-question endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	notifResp, err := globalBridge.RespondToAgentQuestion(ctx, questionID, responseValue)
	if err != nil {
		log.Printf("[INT] Failed to respond to question %s: %v", questionID[:12], err)
		_, editErr := b.api.InteractionResponseEdit(i, &discordgo.WebhookEdit{
			Content: toPtr(fmt.Sprintf("❌ Failed to respond: %v", err)),
		})
		if editErr != nil {
			log.Printf("[INT] Failed to edit response: %v", editErr)
		}
		return
	}

	log.Printf("[INT] Question %s answered successfully (%s → %s)", questionID[:12], responseValue, notifResp.Status)

	// Update the original embed to show the response was submitted
	_, editErr := b.api.InteractionResponseEdit(i, &discordgo.WebhookEdit{
		Content:    toPtr(fmt.Sprintf("🧠 **Agent Question**\nResponded: **%s**\n_Agent resuming..._ ✅", responseLabel)),
		Components: &[]discordgo.MessageComponent{}, // remove buttons
	})
	if editErr != nil {
		log.Printf("[INT] Failed to edit original message: %v", editErr)
	}
}

// handleNotificationEvent is called by the SSE client when any entity.created
// notification arrives. It feeds the raw data into a buffered channel for
// sequential dispatch to Discord. The dispatch function filters by type.
func (b *Bot) handleNotificationEvent(data map[string]interface{}) {
	select {
	case b.sseNotifications <- data:
	default:
		log.Printf("[SSE] Notification channel full, dropping event")
	}
}

// dispatchNotifications runs in a background goroutine and reads from the
// sseNotifications channel, dispatching each notification by its type field.
func (b *Bot) dispatchNotifications() {
	for data := range b.sseNotifications {
		notifType, _ := data["type"].(string)
		switch notifType {
		case "agent_question":
			b.sendAgentQuestionToDiscord(data)
		default:
			log.Printf("[SSE] Unhandled notification type: %s", notifType)
		}
	}
}

// sendAgentQuestionToDiscord sends an agent_question notification to the
// appropriate Discord channel — thread-local if the run is associated with
// a channel, otherwise the configured /set_ask_channel channel, falling back
// to the default notification channel.
// The data map contains: question_id, run_id, question, options,
// interaction_type, placeholder, max_length, status.
func (b *Bot) sendAgentQuestionToDiscord(data map[string]interface{}) {
	runID, _ := data["run_id"].(string)
	channelID := b.resolveQuestionChannel(runID)
	if channelID == "" {
		log.Printf("[SSE] No target channel configured for question (run=%s)", runID[:12])
		return
	}

	questionID, _ := data["question_id"].(string)
	question, _ := data["question"].(string)
	status, _ := data["status"].(string)
	_, _ = data["placeholder"].(string)
	_, _ = data["max_length"].(float64)

	interactionType, _ := data["interaction_type"].(string)
	if interactionType == "" {
		interactionType = "buttons"
	}

	if questionID == "" || question == "" {
		log.Printf("[SSE] Incomplete agent_question event, skipping")
		return
	}

	// Parse options array from data
	options := parseQuestionOptions(data["options"])

	// Truncate question for Discord embed
	displayQuestion := question
	if len(displayQuestion) > 500 {
		displayQuestion = displayQuestion[:497] + "..."
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🧠 Agent Needs Your Input",
		Description: displayQuestion,
		Color:       0x0099ff, // Blue
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Q: %s | Status: %s", questionID[:12], status),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	var components []discordgo.MessageComponent

	switch interactionType {
	case "select":
		if len(options) > 0 {
			selectOpts := make([]discordgo.SelectMenuOption, len(options))
			for i, opt := range options {
				selectOpts[i] = discordgo.SelectMenuOption{
					Label: opt.Label,
					Value: opt.Value,
				}
				if opt.Description != "" {
					selectOpts[i].Description = opt.Description
				}
			}
			components = []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							CustomID:    fmt.Sprintf("aq-sel:%s", questionID),
							Placeholder: "Select an option...",
							Options:     selectOpts,
						},
					},
				},
			}
		}

	case "multi_select":
		if len(options) > 0 {
			selectOpts := make([]discordgo.SelectMenuOption, len(options))
			for i, opt := range options {
				selectOpts[i] = discordgo.SelectMenuOption{
					Label: opt.Label,
					Value: opt.Value,
				}
				if opt.Description != "" {
					selectOpts[i].Description = opt.Description
				}
			}
			components = []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							CustomID:    fmt.Sprintf("aq-msel:%s", questionID),
							Placeholder: "Select options...",
							Options:     selectOpts,
							MaxValues:   len(options),
						},
					},
				},
			}
		}

	case "text":
		// Show a button that opens a modal for text input
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "✏️ Respond",
						Style:    discordgo.PrimaryButton,
						CustomID: fmt.Sprintf("aq:%s:__text__", questionID),
					},
				},
			},
		}

	default: // "buttons" — one button per option, max 5 per row
		components = buildQuestionButtons(questionID, options)
	}

	msgSend := &discordgo.MessageSend{
		Embed:      embed,
		Components: components,
	}

	_, err := b.dg.ChannelMessageSendComplex(channelID, msgSend)
	if err != nil {
		log.Printf("[SSE] Failed to send question to Discord: %v", err)
		return
	}

	log.Printf("[SSE] Question %s sent to Discord (type=%s, %d options)", questionID[:12], interactionType, len(options))
}

// buildQuestionButtons creates button components from a list of options.
// Discord allows max 5 buttons per row. Overflow goes into a select menu.
func buildQuestionButtons(questionID string, options []QuestionOption) []discordgo.MessageComponent {
	if len(options) == 0 {
		// No options: show a "✏️ Respond" button that opens a modal
		return []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "✏️ Respond",
						Style:    discordgo.PrimaryButton,
						CustomID: fmt.Sprintf("aq:%s:__text__", questionID),
					},
				},
			},
		}
	}

	if len(options) <= 5 {
		// Single row of buttons
		buttons := make([]discordgo.MessageComponent, len(options))
		for i, opt := range options {
			buttons[i] = discordgo.Button{
				Label:    opt.Label,
				Style:    optionStyle(opt.Value),
				CustomID: fmt.Sprintf("aq:%s:%s", questionID, opt.Value),
			}
		}
		return []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: buttons},
		}
	}

	// 6+ options: first 5 as buttons, rest in a dropdown "More..."
	var components []discordgo.MessageComponent
	buttons := make([]discordgo.MessageComponent, 5)
	for i := 0; i < 5 && i < len(options); i++ {
		buttons[i] = discordgo.Button{
			Label:    options[i].Label,
			Style:    optionStyle(options[i].Value),
			CustomID: fmt.Sprintf("aq:%s:%s", questionID, options[i].Value),
		}
	}
	components = append(components, discordgo.ActionsRow{Components: buttons})

	// Remaining options in a dropdown
	remaining := options[5:]
	if len(remaining) > 0 {
		selectOpts := make([]discordgo.SelectMenuOption, len(remaining))
		for i, opt := range remaining {
			selectOpts[i] = discordgo.SelectMenuOption{
				Label: opt.Label,
				Value: opt.Value,
			}
			if opt.Description != "" {
				selectOpts[i].Description = opt.Description
			}
		}
		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    fmt.Sprintf("aq-sel:%s", questionID),
					Placeholder: "More options...",
					Options:     selectOpts,
				},
			},
		})
	}

	return components
}

// optionStyle maps common response values to Discord button styles.
func optionStyle(value string) discordgo.ButtonStyle {
	switch value {
	case "yes", "approve", "accept", "confirm", "true":
		return discordgo.SuccessButton
	case "no", "skip", "reject", "decline", "cancel", "false":
		return discordgo.DangerButton
	default:
		return discordgo.PrimaryButton
	}
}

// QuestionOption represents a parsed option from the SSE event data.
type QuestionOption struct {
	Label       string
	Value       string
	Description string
}

// parseQuestionOptions extracts the options array from the SSE data value.
func parseQuestionOptions(raw interface{}) []QuestionOption {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		var opts []QuestionOption
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			label, _ := m["label"].(string)
			val, _ := m["value"].(string)
			if label == "" || val == "" {
				continue
			}
			opt := QuestionOption{Label: label, Value: val}
			if desc, ok := m["description"].(string); ok {
				opt.Description = desc
			}
			opts = append(opts, opt)
		}
		return opts
	case []map[string]interface{}:
		var opts []QuestionOption
		for _, m := range v {
			label, _ := m["label"].(string)
			val, _ := m["value"].(string)
			if label == "" || val == "" {
				continue
			}
			opt := QuestionOption{Label: label, Value: val}
			if desc, ok := m["description"].(string); ok {
				opt.Description = desc
			}
			opts = append(opts, opt)
		}
		return opts
	}
	return nil
}

// toPtr returns a pointer to a string (helper for Discord API calls).
func toPtr(s string) *string {
	return &s
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Deduplicate: Discord Gateway can replay events on reconnect (RESUME)
	if b.isDuplicate(m.ID) {
		log.Printf("[DEDUP] Ignoring duplicate message %s", m.ID)
		return
	}

	// Ignore our own messages
	if m.Author.ID == b.api.BotUserID() {
		return
	}

	// Allow test bots through — bypass the Author.Bot filter for configured IDs
	if m.Author.Bot && !b.isTestBot(m.Author.ID) {
		return
	}

	// Resolve effective channel: for threads, check parent channel
	effectiveChannelID := m.ChannelID
	ch, err := b.api.Channel(m.ChannelID)
	isThread := err == nil && ch.IsThread()
	if isThread {
		effectiveChannelID = ch.ParentID
	}
	if !b.isChannelAllowed(effectiveChannelID) {
		return
	}

	log.Printf("[IN]  channel=%s author=%s#%s msg=%q (thread=%v)", m.ChannelID, m.Author.Username, m.Author.Discriminator, truncateStr(m.Content, 80), isThread)

	log.Printf("[RESTART] dedup_cookie=%s restart_count=%d dedup_count=%d",
		b.DedupCookie, b.RestartCount, len(b.dedupCache))

	// Quick /stop check — respond fast when nothing is running
	if strings.TrimSpace(m.Content) == "/stop" {
		// Check if this is a thread — if so, stop that specific session
		ch, chErr := b.api.Channel(m.ChannelID)
		isThread := chErr == nil && ch.IsThread()

		if isThread {
			b.activeMu.Lock()
			_, hasActive := b.activeChans[m.ChannelID]
			b.activeMu.Unlock()
			if !hasActive {
				log.Printf("[STOP] Nothing running for thread %s — replying idle", m.ChannelID)
				b.api.MessageReactionAdd(m.ChannelID, m.ID, "🛑")
				b.sendMessage(m.ChannelID, "Nothing is currently running.")
				return
			}
			// Active thread — fall through to handleMessage which stops it in the guard
		} else {
			// Parent channel — collect all active threads under this channel
			running := b.listActiveThreads(m.ChannelID)
			if len(running) == 0 {
				log.Printf("[STOP] Nothing running for channel %s — replying idle", m.ChannelID)
				b.api.MessageReactionAdd(m.ChannelID, m.ID, "🛑")
				b.sendMessage(m.ChannelID, "Nothing is currently running.")
				return
			}
			// Show session selection UI
			b.api.MessageReactionAdd(m.ChannelID, m.ID, "🛑")
			b.showStopSelection(m.ChannelID, m.ID, running)
			log.Printf("[STOP] Showing %d active sessions for channel %s", len(running), m.ChannelID)
			return
		}
	}

	// /set_ask_channel — set this channel as the destination for agent questions
	if strings.HasPrefix(strings.TrimSpace(m.Content), "/set_ask_channel") {
		b.questionChannelID = m.ChannelID
		if b.sqliteDB != nil {
			if err := b.sqliteDB.SetConfig("ask_channel", m.ChannelID); err != nil {
				log.Printf("[CFG] Failed to persist ask_channel: %v", err)
			}
		}
		// Fetch channel name for confirmation
		chName := m.ChannelID
		if ch, err := b.api.Channel(m.ChannelID); err == nil {
			chName = "#" + ch.Name
		}
		b.api.MessageReactionAdd(m.ChannelID, m.ID, "✅")
		b.sendMessage(m.ChannelID, fmt.Sprintf("✅ Agent questions will now be sent to %s", chName))
		log.Printf("[CFG] ask_channel set to %s (%s)", m.ChannelID, chName)
		return
	}

	// /btw — todo management
	if strings.HasPrefix(strings.TrimSpace(m.Content), "/btw") {
		b.handleBTW(m.Message)
		return
	}

	// React with 👀 to show we've seen it
	if err := b.api.MessageReactionAdd(m.ChannelID, m.ID, "👀"); err != nil {
		log.Printf("Reaction add error: %v", err)
	}

	// ── Message processing guard ──
	// Prevents simultaneous duplicate events (Discord Gateway can dispatch
	// two MessageCreate events at the same instant). Unlike dedupCache
	// (RESUME replay across time), this catches near-simultaneous events.
	b.msgGuardMu.Lock()
	if _, exists := b.msgGuard[m.ID]; exists {
		b.msgGuardMu.Unlock()
		log.Printf("[GUARD] Already processing message %s — skipping duplicate", m.ID)
		return
	}
	b.msgGuard[m.ID] = struct{}{}
	b.msgGuardMu.Unlock()

	go b.handleMessage(m.Message)
}

// ============================================================================
// Message Handling
// ============================================================================

func (b *Bot) handleMessage(m *discordgo.Message) {
	start := time.Now()
	channelID := m.ChannelID
	botID := b.api.BotUserID()

	// Clear the processing guard when we're done
	defer func() {
		b.msgGuardMu.Lock()
		delete(b.msgGuard, m.ID)
		b.msgGuardMu.Unlock()
	}()

	// ── Determine response channel (thread or inline) ──
	ch, err := b.api.Channel(channelID)
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
			// Strip Discord mentions (<@!123>, <@&123>, <#123>, <:emoji:123>) from thread title
			// IMPORTANT: do this BEFORE stripping unicode symbols (the '<' is \p{Sm} = Math Symbol)
			cleanMsg = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(cleanMsg, "")
			// Strip leading Unicode symbols/emojis (emoji prefix from user, etc.)
			cleanMsg = regexp.MustCompile(`^[\p{So}\p{Sk}\p{Sc}\p{Sm}]\s*`).ReplaceAllString(cleanMsg, "")
			cleanMsg = strings.TrimSpace(cleanMsg)
			threadName := emoji + " " + category + ": " + truncateStr(cleanMsg, 40)
			if len(threadName) > 100 {
				threadName = threadName[:100]
			}
			if threadName == "" || threadName == emoji+" "+category+": " {
				threadName = emoji + " " + category
			}
			thread, err := b.api.MessageThreadStart(channelID, m.ID, threadName, 60*24)
			if err != nil {
				log.Printf("[WARN] Thread creation failed: %v", err)
				b.sendResponse(channelID, m, start)
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
			b.api.MessageReactionAdd(channelID, m.ID, "🛑")
			b.sendMessage(responseChannel, "Nothing is currently running.")
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
			b.api.MessageReactionAdd(channelID, m.ID, "🛑")
			b.stopActiveRun(responseChannel)
			b.sendMessage(responseChannel, "🛑 **Stopped**")
			log.Printf("[STOP] Stopped active run for channel %s", responseChannel)
			return
		}

		// Non-stop message while busy — queue it
		b.api.MessageReactionAdd(channelID, m.ID, "👀")
		b.queueMessage(responseChannel, m)
		log.Printf("[QUEUE] Channel %s busy, queued msg %s", responseChannel, truncateStr(m.ID, 8))
		return
	}

	// Store parent channel ID for thread-based sessions (used by /stop to list active threads)
	if createdNewThread {
		b.activeMu.Lock()
		if ac, ok := b.activeChans[responseChannel]; ok {
			ac.ParentID = channelID
		}
		b.activeMu.Unlock()
	}

	// ── Process loop: drain messages until queue empty or cancelled ──
	processingOK := false
	currentMsg := m
	autoContinueCount := 0
	maxAutoContinues := 5

	for {
		// Log queue state
		queueSize := 0
		b.activeMu.Lock()
		if ac, ok := b.activeChans[responseChannel]; ok {
			queueSize = len(ac.Pending)
		}
		b.activeMu.Unlock()
		dlog("PRC", "channel", responseChannel, "msg", truncateStr(currentMsg.ID, 8), "queue_size", queueSize)

		b.startTyping(responseChannel)
		var response string
		if b.buildResponseFn != nil {
			response = b.buildResponseFn(ctx, currentMsg, responseChannel)
		} else {
			response = b.buildAndSendResponse(ctx, currentMsg, responseChannel)
		}
		b.stopTyping(responseChannel)

		// Check if cancelled by /stop
		if ctx.Err() != nil {
			b.sendMessage(responseChannel, "🛑 **Stopped**")
			processingOK = true
			break
		}

		// Send response
		if response != "" {
			b.sendMessage(responseChannel, response)
			processingOK = true

			// For new threads, check for session title update
			if createdNewThread && !isThread {
				b.mu.RLock()
				cs, exists := b.sessions[responseChannel]
				b.mu.RUnlock()
				if exists && cs.SessionID != "" {
					sd, sdErr := globalBridge.GetSession(context.Background(), cs.SessionID)
					if sdErr == nil && sd.Title != "" && !strings.HasPrefix(sd.Title, "Discord #") {
						if _, editErr := b.api.ChannelEdit(responseChannel, &discordgo.ChannelEdit{
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
			// Queue empty — check for auto-continue (remaining todos)
			if autoContinueCount < maxAutoContinues {
				if contMsg := b.checkAutoContinue(ctx, responseChannel); contMsg != nil {
					nextMsg = contMsg
					autoContinueCount++
					log.Printf("[AUTO] Auto-continue #%d/%d", autoContinueCount, maxAutoContinues)
				}
			}
		}
		if nextMsg == nil {
			break // queue empty, nothing to continue
		}
		currentMsg = nextMsg
		log.Printf("[QUEUE] Processing queued msg %s (channel %s)", truncateStr(currentMsg.ID, 8), responseChannel)
	}

	// ── Cleanup ──
	cancel()
	b.releaseChannel(responseChannel)

	// Swap reactions on ORIGINAL message only
	b.api.MessageReactionRemove(channelID, m.ID, "👀", botID)
	if processingOK {
		b.api.MessageReactionAdd(channelID, m.ID, "✅")
	} else {
		b.api.MessageReactionAdd(channelID, m.ID, "❌")
	}

	log.Printf("[RES] channel=%s duration=%v chars=%d", responseChannel, time.Since(start).Round(time.Millisecond), len(m.Content))
}

// sendResponse handles the full response flow (fallback path, no active guard).
// Uses a plain background context since this path doesn't support /stop.
func (b *Bot) sendResponse(channelID string, m *discordgo.Message, start time.Time) {
	var response string
	if b.buildResponseFn != nil {
		response = b.buildResponseFn(context.Background(), m, channelID)
	} else {
		response = b.buildAndSendResponse(context.Background(), m, channelID)
	}
	b.sendMessage(channelID, response)
	log.Printf("[RES] channel=%s duration=%v chars=%d", channelID, time.Since(start).Round(time.Millisecond), len(response))
}

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
	// Clean up reverse mappings for all runs associated with this channel
	b.runChannelsMu.Lock()
	for runID, ch := range b.runChannels {
		if ch == channelID {
			delete(b.runChannels, runID)
		}
	}
	b.runChannelsMu.Unlock()
	delete(b.activeChans, channelID)
	b.activeMu.Unlock()
}

// setActiveAgentRun stores the runtime agent and run IDs for CancelRun.
// Called from triggerAgentWithContext after the run starts.
func (b *Bot) setActiveAgentRun(channelID, agentID, runID string) {
	b.activeMu.Lock()
	if ac, exists := b.activeChans[channelID]; exists {
		ac.AgentID = agentID
		ac.RunID = runID
	}
	b.activeMu.Unlock()
	// Store reverse mapping: runID → channel for thread-local question delivery
	if runID != "" {
		b.runChannelsMu.Lock()
		b.runChannels[runID] = channelID
		b.runChannelsMu.Unlock()
	}
}

// queueMessage adds a message to the pending queue for a busy channel.
func (b *Bot) queueMessage(channelID string, msg *discordgo.Message) {
	b.activeMu.Lock()
	defer b.activeMu.Unlock()
	if ac, exists := b.activeChans[channelID]; exists {
		ac.Pending = append(ac.Pending, msg)
		log.Printf("[QUEUE] Queued msg %s for channel %s (size=%d)", truncateStr(msg.ID, 8), channelID, len(ac.Pending))
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

// isTestBot checks if a given user ID is in the configured test bot allowlist.
// Test bots bypass the Author.Bot filter so the harness can send messages
// that Diane will actually process.
func (b *Bot) isTestBot(userID string) bool {
	if len(b.config.TestBotIDs) == 0 {
		return false
	}
	for _, id := range b.config.TestBotIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// listActiveThreads returns the channel IDs of all threads under a parent
// channel that currently have active agent runs.
func (b *Bot) listActiveThreads(parentID string) []string {
	b.activeMu.Lock()
	defer b.activeMu.Unlock()
	var out []string
	for chID, ac := range b.activeChans {
		if ac.ParentID == parentID {
			out = append(out, chID)
		}
	}
	return out
}

// showStopSelection sends an embed with buttons to select which active
// session to stop, or "Stop All".
func (b *Bot) showStopSelection(channelID, messageID string, threads []string) {
	if len(threads) == 0 {
		return
	}
	desc := fmt.Sprintf("There are **%d** active session(s). Choose which one to stop:", len(threads))
	embed := &discordgo.MessageEmbed{
		Title:       "🛑 Select Session to Stop",
		Description: desc,
		Color:       0xE74C3C, // Red
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	// Build buttons: one per thread + Stop All
	// Discord allows max 5 buttons per row
	var rows []discordgo.MessageComponent
	var rowButtons []discordgo.MessageComponent
	maxPerRow := 5
	buttonCount := 0

	for i, tid := range threads {
		label := fmt.Sprintf("#%d", i+1)
		if len(threads) <= 5 {
			// Get thread name for labeling
			if ch, err := b.api.Channel(tid); err == nil {
				label = ch.Name
				if len(label) > 80 {
					label = label[:77] + "..."
				}
			}
		}
		rowButtons = append(rowButtons, discordgo.Button{
			Label:    label,
			Style:    discordgo.DangerButton,
			CustomID: fmt.Sprintf("stop-thread:%s", tid),
		})
		buttonCount++
		if buttonCount >= maxPerRow || i == len(threads)-1 {
			rows = append(rows, discordgo.ActionsRow{Components: rowButtons})
			rowButtons = nil
			buttonCount = 0
		}
	}

	// Always add "Stop All" as the last row
	rows = append(rows, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "⏹️ Stop All",
				Style:    discordgo.DangerButton,
				CustomID: fmt.Sprintf("stop-all:%s", channelID),
			},
			discordgo.Button{
				Label:    "Cancel",
				Style:    discordgo.SecondaryButton,
				CustomID: "stop-cancel",
			},
		},
	})

	_, err := b.dg.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Embed:      embed,
		Components: rows,
	})
	if err != nil {
		log.Printf("[STOP] Failed to send stop selection: %v", err)
	}
}

// handleStopSelection processes a stop-thread or stop-all button click.
func (b *Bot) handleStopSelection(s *discordgo.Session, i *discordgo.Interaction, customID string) {
	// Acknowledge immediately
	err := b.api.InteractionRespond(i, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if err != nil {
		log.Printf("[STOP] Failed to acknowledge stop interaction: %v", err)
		return
	}

	// Cancel button — no target needed
	if customID == "stop-cancel" {
		b.editStopResponse(i, "Cancelled. No sessions were stopped.")
		return
	}

	parts := strings.SplitN(customID, ":", 2)
	if len(parts) < 2 {
		log.Printf("[STOP] Invalid customID format: %s", customID)
		return
	}

	action := parts[0]
	target := parts[1]

	switch action {
	case "stop-thread":
		b.stopActiveRun(target)
		b.editStopResponse(i, fmt.Sprintf("🛑 Session `<#%s>` stopped.", target))

	case "stop-all":
		var stopped int
		b.activeMu.Lock()
		for _, ac := range b.activeChans {
			if ac.ParentID == target {
				ac.Cancel()
				stopped++
				go func(agentID, runID string) {
					if agentID != "" && runID != "" {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						globalBridge.Client().Agents.CancelRun(ctx, agentID, runID)
					}
				}(ac.AgentID, ac.RunID)
			}
		}
		b.activeMu.Unlock()
		b.editStopResponse(i, fmt.Sprintf("⏹️ Stopped **%d** session(s).", stopped))

	case "stop-cancel":
		b.editStopResponse(i, "Cancelled. No sessions were stopped.")
	}
}

// editStopResponse updates the original embed to show the result.
func (b *Bot) editStopResponse(i *discordgo.Interaction, content string) {
	_, err := b.api.InteractionResponseEdit(i, &discordgo.WebhookEdit{
		Content:    &content,
		Components: &[]discordgo.MessageComponent{}, // remove buttons
	})
	if err != nil {
		log.Printf("[STOP] Failed to edit response: %v", err)
	}
}

// handleBTW processes /btw commands for todo management.
// Supported subcommands:
//
//	/btw <text>           — create a new todo
//	/btw list             — list pending todos
//	/btw done <id|num>    — mark todo as completed
//	/btw cancel <id|num>  — cancel a todo
func (b *Bot) handleBTW(m *discordgo.Message) {
	if b.sqliteDB == nil {
		b.sendMessage(m.ChannelID, "❌ Todo database is not available.")
		log.Printf("[BTW] SQLite not available, skipping")
		return
	}

	content := strings.TrimSpace(m.Content)
	rest := strings.TrimSpace(strings.TrimPrefix(content, "/btw"))

	// Extract the subcommand (first word)
	parts := strings.Fields(rest)
	subcmd := ""
	if len(parts) > 0 {
		subcmd = parts[0]
	}

	switch subcmd {
	case "list":
		b.listTodos(m)
	case "done":
		if len(parts) >= 2 {
			b.updateTodoStatus(m, parts[1], "completed")
		} else {
			b.sendMessage(m.ChannelID, "❌ Usage: `/btw done <id>`")
		}
	case "cancel":
		if len(parts) >= 2 {
			b.updateTodoStatus(m, parts[1], "cancelled")
		} else {
			b.sendMessage(m.ChannelID, "❌ Usage: `/btw cancel <id>`")
		}
	default:
		if rest != "" {
			b.createTodo(m, rest)
		} else {
			b.sendMessage(m.ChannelID, "📝 **/btw usage:**\n`/btw <text>` — add todo\n`/btw list` — list todos\n`/btw done <id>` — mark done\n`/btw cancel <id>` — cancel")
		}
	}
}

func (b *Bot) createTodo(m *discordgo.Message, text string) {
	author := m.Author.Username
	ch := m.ChannelID

	// Get session ID if we have one
	var sessionID string
	b.mu.RLock()
	if cs, exists := b.sessions[ch]; exists {
		sessionID = cs.SessionID
	}
	b.mu.RUnlock()

	// Try MP API first, fall back to SQLite
	if globalBridge != nil && sessionID != "" {
		todo, err := globalBridge.CreateSessionTodo(context.Background(), sessionID, text, author)
		if err != nil {
			log.Printf("[BTW] MP create error: %v, falling back to SQLite", err)
		} else {
			b.api.MessageReactionAdd(ch, m.ID, "✅")
			shortID := todo.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			b.sendMessage(ch, fmt.Sprintf("✅ Added todo `%s`: **%s**", shortID, text))
			log.Printf("[BTW] Created todo %s: %q (session=%s)", todo.ID, text, sessionID)
			return
		}
	}

	// Fallback to SQLite
	if b.sqliteDB == nil {
		b.sendMessage(ch, "❌ Todo database is not available.")
		return
	}
	todo, err := b.sqliteDB.CreateTodo(ch, sessionID, text, author)
	if err != nil {
		log.Printf("[BTW] Create error: %v", err)
		b.sendMessage(ch, "❌ Failed to create todo: "+err.Error())
		return
	}

	b.api.MessageReactionAdd(ch, m.ID, "✅")
	b.sendMessage(ch, fmt.Sprintf("✅ Added todo #%d: **%s**", todo.ID, text))
	log.Printf("[BTW] Created todo #%d: %q (channel=%s)", todo.ID, text, ch)
}

func (b *Bot) listTodos(m *discordgo.Message) {
	ch := m.ChannelID

	// Try MP API first
	var sessionID string
	b.mu.RLock()
	if cs, exists := b.sessions[ch]; exists {
		sessionID = cs.SessionID
	}
	b.mu.RUnlock()

	if globalBridge != nil && sessionID != "" {
		todos, err := globalBridge.ListSessionTodos(context.Background(), sessionID, "")
		if err != nil {
			log.Printf("[BTW] MP list error: %v, falling back to SQLite", err)
		} else {
			b.renderTodoList(m, ch, todos)
			return
		}
	}

	// Fallback to SQLite
	if b.sqliteDB == nil {
		b.sendMessage(ch, "❌ Todo database is not available.")
		return
	}
	todos, err := b.sqliteDB.ListTodos(ch, "")
	if err != nil {
		log.Printf("[BTW] List error: %v", err)
		b.sendMessage(ch, "❌ Failed to list todos: "+err.Error())
		return
	}

	if len(todos) == 0 {
		b.api.MessageReactionAdd(ch, m.ID, "✅")
		b.sendMessage(ch, "📝 No todos for this channel.")
		return
	}

	// Group by status
	var pending, completed, cancelled []string
	for _, t := range todos {
		line := fmt.Sprintf("`#%d` %s — %s", t.ID, t.Content, t.Author)
		switch t.Status {
		case "draft", "pending":
			pending = append(pending, line)
		case "completed":
			completed = append(completed, line)
		case "cancelled":
			cancelled = append(cancelled, line)
		}
	}

	var msg string
	msg += fmt.Sprintf("📝 **Todos for this channel** (%d total)\n", len(todos))
	if len(pending) > 0 {
		msg += "\n⏳ **Pending:**\n" + strings.Join(pending, "\n")
	}
	if len(completed) > 0 {
		msg += "\n✅ **Completed:**\n" + strings.Join(completed, "\n")
	}
	if len(cancelled) > 0 {
		msg += "\n❌ **Cancelled:**\n" + strings.Join(cancelled, "\n")
	}

	b.api.MessageReactionAdd(ch, m.ID, "✅")
	b.sendMessage(ch, msg)
}

// renderTodoList formats and sends MP API todo list to Discord.
func (b *Bot) renderTodoList(m *discordgo.Message, ch string, todos []memory.SessionTodo) {
	if len(todos) == 0 {
		b.api.MessageReactionAdd(ch, m.ID, "✅")
		b.sendMessage(ch, "📝 No todos for this session.")
		return
	}

	var pending, completed, cancelled []string
	for _, t := range todos {
		shortID := t.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		line := fmt.Sprintf("`%s` %s — %s", shortID, t.Content, t.Author)
		switch t.Status {
		case "draft", "pending":
			pending = append(pending, line)
		case "completed":
			completed = append(completed, line)
		case "cancelled":
			cancelled = append(cancelled, line)
		}
	}

	var msg string
	msg += fmt.Sprintf("📝 **Todos for this session** (%d total)\n", len(todos))
	if len(pending) > 0 {
		msg += "\n⏳ **Pending:**\n" + strings.Join(pending, "\n")
	}
	if len(completed) > 0 {
		msg += "\n✅ **Completed:**\n" + strings.Join(completed, "\n")
	}
	if len(cancelled) > 0 {
		msg += "\n❌ **Cancelled:**\n" + strings.Join(cancelled, "\n")
	}

	b.api.MessageReactionAdd(ch, m.ID, "✅")
	b.sendMessage(ch, msg)
}

func (b *Bot) updateTodoStatus(m *discordgo.Message, idStr, newStatus string) {
	ch := m.ChannelID

	// Try MP API first
	var sessionID string
	b.mu.RLock()
	if cs, exists := b.sessions[ch]; exists {
		sessionID = cs.SessionID
	}
	b.mu.RUnlock()

	if globalBridge != nil && sessionID != "" {
		todo, err := globalBridge.UpdateSessionTodo(context.Background(), sessionID, idStr, newStatus)
		if err != nil {
			log.Printf("[BTW] MP update error: %v, falling back to SQLite", err)
		} else {
			emoji := "✅"
			label := "completed"
			if newStatus == "cancelled" {
				emoji = "❌"
				label = "cancelled"
			}
			b.api.MessageReactionAdd(ch, m.ID, "✅")
			b.sendMessage(ch, fmt.Sprintf("%s Todo `%s` marked as **%s**: %s", emoji, idStr, label, todo.Content))
			return
		}
	}

	// Fallback to SQLite
	if b.sqliteDB == nil {
		b.sendMessage(ch, "❌ Todo database is not available.")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		b.sendMessage(ch, "❌ Invalid todo ID: \""+idStr+"\" — use a number like `/btw done 1`")
		return
	}
	todo, err := b.sqliteDB.GetTodo(id)
	if err != nil {
		log.Printf("[BTW] Get error: %v", err)
		b.sendMessage(ch, "❌ Error looking up todo #%d: "+err.Error())
		return
	}
	if todo == nil {
		b.sendMessage(ch, fmt.Sprintf("❌ Todo #%d not found.", id))
		return
	}

	if err := b.sqliteDB.UpdateTodoStatus(id, newStatus); err != nil {
		log.Printf("[BTW] Update error: %v", err)
		b.sendMessage(ch, "❌ Failed to update todo: "+err.Error())
		return
	}

	emoji := "✅"
	label := "completed"
	if newStatus == "cancelled" {
		emoji = "❌"
		label = "cancelled"
	}

	b.api.MessageReactionAdd(ch, m.ID, "✅")
	b.sendMessage(ch, fmt.Sprintf("%s Todo #%d marked as **%s**: %s", emoji, id, label, todo.Content))
}

// resolveQuestionChannel determines where to send an agent question.
// Priority: 1) thread-local (runID maps to a Discord channel/thread)
//  2. configured question channel (/set_ask_channel)
//  3. default notification channel (first allowed channel)
func (b *Bot) resolveQuestionChannel(runID string) string {
	// 1. Thread-local — if this run is associated with a Discord thread/channel
	if runID != "" {
		b.runChannelsMu.RLock()
		ch, ok := b.runChannels[runID]
		b.runChannelsMu.RUnlock()
		if ok {
			return ch
		}
	}
	// 2. Configured question channel
	if b.questionChannelID != "" {
		return b.questionChannelID
	}
	// 3. Default notification channel
	return b.sendChannelID
}

func (b *Bot) sendMessage(channelID, content string) {
	const maxLen = 1900
	if content == "" {
		return
	}
	if len(content) <= maxLen {
		_, err := b.api.ChannelMessageSend(channelID, content)
		if err != nil {
			log.Printf("[ERR] Send message: %v", err)
		}
		return
	}
	for _, part := range splitMessage(content, maxLen) {
		_, err := b.api.ChannelMessageSend(channelID, part)
		if err != nil {
			log.Printf("[ERR] Send message part: %v", err)
			return
		}
	}
}

// checkAutoContinue checks if the session has remaining active todos and returns
// a synthetic message to trigger auto-continuation. Returns nil if no todos remain.
func (b *Bot) checkAutoContinue(ctx context.Context, responseChannel string) *discordgo.Message {
	b.mu.RLock()
	cs, exists := b.sessions[responseChannel]
	b.mu.RUnlock()
	if !exists || cs.SessionID == "" || globalBridge == nil {
		return nil
	}

	todos, err := globalBridge.ListSessionTodos(ctx, cs.SessionID, "")
	if err != nil {
		log.Printf("[AUTO] List error: %v", err)
		return nil
	}

	// Count active todos
	var active int
	for _, t := range todos {
		if t.Status == "draft" || t.Status == "pending" || t.Status == "in_progress" {
			active++
		}
	}
	if active == 0 {
		return nil
	}

	// Create a synthetic message to continue working
	// Use a unique ID that won't collide with real messages
	return &discordgo.Message{
		ID:        fmt.Sprintf("auto-continue-%d", time.Now().UnixNano()),
		ChannelID: responseChannel,
		Content:   fmt.Sprintf("Continue working. You still have %d pending todos in your todo list.", active),
		Author: &discordgo.User{
			ID:       "auto-continue",
			Username: "System",
		},
	}
}

// startTyping starts a persistent typing indicator loop (Hermes pattern).
// Discord's typing indicator lasts ~10s, so we re-trigger it every 8s
// until stopTyping is called.
func (b *Bot) startTyping(channelID string) {
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
		b.api.ChannelTyping(channelID)

		for {
			select {
			case <-ticker.C:
				b.api.ChannelTyping(channelID)
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

// buildTriggerPromptWithTodos builds the trigger prompt, injecting pending todos
// as instructions for the agent to analyze and work through.
func (b *Bot) buildTriggerPromptWithTodos(ctx context.Context, cs *ChannelSession, userMsg string) string {
	if globalBridge == nil || cs.SessionID == "" {
		return userMsg
	}

	todos, err := globalBridge.ListSessionTodos(ctx, cs.SessionID, "")
	if err != nil {
		log.Printf("[TODO] List error (skipping injection): %v", err)
		return userMsg
	}

	// Filter to active todos (draft + pending + in_progress)
	var active []memory.SessionTodo
	for _, t := range todos {
		if t.Status == "draft" || t.Status == "pending" || t.Status == "in_progress" {
			active = append(active, t)
		}
	}

	if len(active) == 0 {
		return userMsg
	}

	// Build todo block
	var bld strings.Builder
	bld.WriteString("\n\n[📋 TODO DRAFTS — analyze these in the conversation context and work through them:]\n")
	for i, t := range active {
		shortID := t.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		status := t.Status
		if status == "draft" {
			status = "❓ draft (not yet analyzed)"
		}
		bld.WriteString(fmt.Sprintf("%d. [%s] %s (from %s, id=%s)\n", i+1, status, t.Content, t.Author, shortID))
	}
	bld.WriteString("\nAnalyze each todo draft in the context of the conversation.")
	bld.WriteString(" Create a plan and work through items systematically.")
	bld.WriteString(" Mark items complete when done. Continue until all items are resolved.")

	bld.WriteString("\n\n" + userMsg)

	return bld.String()
}

// triggerAgentWithContext triggers a Memory Platform agent with the user's message
// and returns the response text. It creates a runtime agent, triggers it, polls for
// completion, fetches the response + tool calls, cleans up, and returns the text.
// If includeTools is true, appends a short tool usage indicator to the response.
func (b *Bot) triggerAgentWithContext(ctx context.Context, cs *ChannelSession, userMsg string, agentName string) (string, error) {

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

	// 3. Build trigger prompt with todo injection
	triggerPrompt := b.buildTriggerPromptWithTodos(ctx, cs, userMsg)

	dlog("AGT", "action", "triggering", "prompt_chars", len(triggerPrompt))
	triggerResp, err := globalBridge.TriggerAgentWithInput(ctx, agentID, triggerPrompt, cs.SessionID)
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

	// Store agent/run IDs for CancelRun (used by /stop and interrupt)
	b.setActiveAgentRun(cs.ChannelID, agentID, runID)

	// 4. Poll for completion (cancellable via ctx)
	pollStart := time.Now()
	pollInterval := 2 * time.Second
	pollTimeout := 120 * time.Second
	pausedTimeout := 10 * time.Minute // extended timeout when agent asks a question
	var runStatus string
	var wasPaused bool
pollLoop:
	for {
		select {
		case <-ctx.Done():
			// Cancelled by /stop or interrupt
			dlog("POLL", "event", "cancelled", "elapsed", time.Since(pollStart).Round(time.Second).String())
			return "", fmt.Errorf("run %s: cancelled by user", runID[:12])
		case <-time.After(pollInterval):
		}

		// Check timeout — use extended timeout if agent asked a question
		effectiveTimeout := pollTimeout
		if wasPaused {
			effectiveTimeout = pausedTimeout
		}
		if time.Since(pollStart) >= effectiveTimeout {
			dlog("POLL", "event", "timeout", "elapsed", effectiveTimeout.String(), "was_paused", wasPaused)
			return "", fmt.Errorf("run %s: timeout after %v (last status: %s)", runID[:12], effectiveTimeout, runStatus)
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
		case "paused":
			// Agent asked a question — mark and continue polling.
			// The timeout is extended to pausedTimeout automatically.
			if !wasPaused {
				wasPaused = true
				dlog("POLL", "event", "paused", "run", runID[:12], "extending_timeout", pausedTimeout.String())
			}
			continue
		case "error", "failed", "cancelled", "timeout":
			errMsg := ""
			if runResp.Data.ErrorMessage != nil {
				errMsg = *runResp.Data.ErrorMessage
			}
			dlog("AGT", "err", "run_"+runStatus, "run", runID[:12], "error", errMsg)
			return "", fmt.Errorf("run %s: status=%s, error=%s", runID[:12], runStatus, errMsg)
		}
	}
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
				for k := range msg.Content {
					keys = append(keys, k)
				}
				return keys
			}()))
			if msg.Role == "user" || msg.Role == "tool" {
				continue
			}
			var reasoningText string

			// Get reasoning content if present
			if val, ok := msg.Content["reasoning"]; ok {
				if s := extractText(val); len(s) > 20 {
					reasoningText = s
					dlog("EXTR", "found_reasoning", "len", len(reasoningText), "preview", truncateStr(reasoningText, 80))
				}
			}

			// Get the main text content — prefer "text" key, fall back to others
			if val, ok := msg.Content["text"]; ok {
				if s := extractText(val); len(s) > 0 {
					responseText = s
					dlog("EXTR", "found_text", "len", len(responseText), "preview", truncateStr(responseText, 80))
				}
			}
			if responseText == "" {
				// Fallback: scan other keys for content
				for key, val := range msg.Content {
					if key == "reasoning" {
						continue
					}
					s := extractText(val)
					if len(s) > 20 {
						dlog("EXTR", "found_in_key", key, "len", len(s), "preview", truncateStr(s, 80))
						responseText = s
						break
					}
				}
			}

			// Combine reasoning + response — wrap thinking in spoiler tags for foldable display
			if reasoningText != "" && responseText == "" {
				responseText = "||" + reasoningText + "||"
			} else if reasoningText != "" {
				responseText = "🤔 ||*Thinking...*\n" + reasoningText + "||\n\n" + responseText
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

	// Record run stats for A/B analytics (non-blocking)
	if b.sqliteDB != nil {
		go func() {
			recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Fetch run details for token usage and step count
			runDetail, err := globalBridge.GetProjectRun(recordCtx, runID)
			if err != nil {
				return
			}

			// Safely extract values from pointers
			durMs := 0
			if runDetail.Data.DurationMs != nil {
				durMs = *runDetail.Data.DurationMs
			}
			inTokens := 0
			if runDetail.Data.TokenUsage != nil {
				inTokens = int(runDetail.Data.TokenUsage.TotalInputTokens)
			}
			outTokens := 0
			if runDetail.Data.TokenUsage != nil {
				outTokens = int(runDetail.Data.TokenUsage.TotalOutputTokens)
			}

			// Count tool calls from messages
			toolCallCount := 0
			if msgs != nil {
				for _, m := range msgs.Data {
					if _, hasFC := m.Content["function_calls"]; hasFC {
						toolCallCount++
					}
				}
			}

			stat := &db.AgentRunStat{
				AgentName:     agentName,
				RunID:         runID,
				SessionID:     cs.SessionID,
				DurationMs:    durMs,
				StepCount:     runDetail.Data.StepCount,
				ToolCallCount: toolCallCount,
				InputTokens:   inTokens,
				OutputTokens:  outTokens,
				Status:        "success",
			}
			b.sqliteDB.RecordRunStat(stat)
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
		"web-search-brave":      "🌐",
		"web-search-reddit":     "🌐",
		"web-fetch":             "🌐",
		"search-hybrid":         "🔍",
		"search-knowledge":      "🔍",
		"search-semantic":       "🔍",
		"search-similar":        "🔍",
		"entity-query":          "📄",
		"entity-search":         "📄",
		"entity-edges-get":      "📄",
		"entity-type-list":      "📄",
		"entity-create":         "✏️",
		"graph-traverse":        "🗺️",
		"tag-list":              "🏷️",
		"list_available_agents": "💬",
		"spawn_agents":          "🔧",
		"skill":                 "📚",
		"skill-list":            "📚",
		"skill-get":             "📚",
	}
	if e, ok := m[name]; ok {
		return e
	}
	return "⚙️"
}

// shortToolName returns a readable short version of a tool name for display.
func shortToolName(name string) string {
	short := map[string]string{
		"web-search-brave":      "web-search",
		"web-search-reddit":     "reddit",
		"search-hybrid":         "hybrid-search",
		"search-knowledge":      "k-search",
		"search-semantic":       "semantic-search",
		"search-similar":        "similar-search",
		"entity-query":          "entity-query",
		"entity-search":         "entity-search",
		"entity-edges-get":      "entity-edges",
		"entity-type-list":      "entity-types",
		"entity-create":         "entity-create",
		"web-fetch":             "fetch",
		"graph-traverse":        "graph-traverse",
		"tag-list":              "tags",
		"list_available_agents": "agents",
		"spawn_agents":          "spawn",
		"skill":                 "skill",
		"skill-list":            "skills",
		"skill-get":             "skill-get",
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
	for _, key := range []string{"query", "url", "name", "text", "message", "id", "question", "title"} {
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

// generateDedupCookie creates a random 16-char hex string used as a dedup
// instance identifier. Each bot instance generates a unique cookie at startup,
// allowing dedup persistence to be validated across restarts in logs.
func generateDedupCookie() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(buf)
}
