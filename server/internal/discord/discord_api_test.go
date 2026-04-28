package discord

import (
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ── Test Helpers ──────────────────────────────────────────────────────────

// FakeChannel describes a channel or thread for the fake Discord API.
type FakeChannel struct {
	ID       string
	ParentID string
	Type     discordgo.ChannelType
	Name     string
}

// TypingCall records a single ChannelTyping invocation.
type TypingCall struct {
	ChannelID string
}

// ReactionCall records a MessageReactionAdd or MessageReactionRemove.
type ReactionCall struct {
	ChannelID string
	MessageID string
	Emoji     string
	Remove    bool
	UserID    string // non-empty for remove
}

// ThreadCreateCall records a MessageThreadStart invocation.
type ThreadCreateCall struct {
	ChannelID       string
	MessageID       string
	Name            string
	ArchiveDuration int
	CreatedID       string
}

// MessageSendCall records a ChannelMessageSend invocation.
type MessageSendCall struct {
	ChannelID string
	Content   string
}

// ChannelEditCall records a ChannelEdit invocation.
type ChannelEditCall struct {
	ChannelID string
	Edit      *discordgo.ChannelEdit
}

// FakeDiscordAPI implements DiscordAPI for testing.
// It stores all calls and returns configurable responses.
type FakeDiscordAPI struct {
	mu sync.Mutex

	// Channels holds the channel directory: id → channel info.
	// Set up test channels here before calling bot methods.
	Channels map[string]*FakeChannel

	// BotID is returned by BotUserID().
	BotID string

	// Tracked calls (populated by each API method)
	TypingCalls       []TypingCall
	ReactionCalls     []ReactionCall
	ThreadCreateCalls []ThreadCreateCall
	MessageSendCalls  []MessageSendCall
	ChannelEditCalls  []ChannelEditCall

	// Interaction responses are just recorded, not acted upon.
	InteractionResponses     []*discordgo.InteractionResponse
	InteractionResponseEdits []*discordgo.WebhookEdit

	// nextThreadID is incremented for each MessageThreadStart call
	nextThreadID int

	// ErrorInjectors let tests force errors on specific methods.
	ErrChannel         error // returned by Channel()
	ErrChannelTyping   error
	ErrMessageSend     error
	ErrReactionAdd     error
	ErrReactionRemove  error
	ErrThreadStart     error
	ErrChannelEdit     error
	ErrInteractionResp error
	ErrInteractionEdit error
}

// NewFakeDiscordAPI creates a fake API with a default bot ID.
func NewFakeDiscordAPI() *FakeDiscordAPI {
	return &FakeDiscordAPI{
		Channels:     make(map[string]*FakeChannel),
		BotID:        "bot-123",
		nextThreadID: 1000,
	}
}

// AddChannel registers a FakeChannel so Channel() can find it.
func (f *FakeDiscordAPI) AddChannel(ch *FakeChannel) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Channels[ch.ID] = ch
}

// AddThread is a convenience to add a thread channel under a parent.
func (f *FakeDiscordAPI) AddThread(threadID, parentID, name string) {
	f.AddChannel(&FakeChannel{
		ID:       threadID,
		ParentID: parentID,
		Type:     discordgo.ChannelTypeGuildPublicThread,
		Name:     name,
	})
}

// AddParentChannel is a convenience to add a regular text channel.
func (f *FakeDiscordAPI) AddParentChannel(channelID, name string) {
	f.AddChannel(&FakeChannel{
		ID:   channelID,
		Type: discordgo.ChannelTypeGuildText,
		Name: name,
	})
}

// ── DiscordAPI implementation ─────────────────────────────────────────────

func (f *FakeDiscordAPI) Channel(channelID string) (*discordgo.Channel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrChannel != nil {
		return nil, f.ErrChannel
	}
	fc, ok := f.Channels[channelID]
	if !ok {
		return nil, fmt.Errorf("FakeDiscordAPI: unknown channel %s", channelID)
	}
	return &discordgo.Channel{
		ID:       fc.ID,
		ParentID: fc.ParentID,
		Type:     fc.Type,
		Name:     fc.Name,
	}, nil
}

func (f *FakeDiscordAPI) ChannelTyping(channelID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrChannelTyping != nil {
		return f.ErrChannelTyping
	}
	f.TypingCalls = append(f.TypingCalls, TypingCall{ChannelID: channelID})
	return nil
}

func (f *FakeDiscordAPI) ChannelMessageSend(channelID, content string) (*discordgo.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrMessageSend != nil {
		return nil, f.ErrMessageSend
	}
	f.MessageSendCalls = append(f.MessageSendCalls, MessageSendCall{
		ChannelID: channelID,
		Content:   content,
	})
	return &discordgo.Message{
		ID:        fmt.Sprintf("msg-%d", len(f.MessageSendCalls)),
		ChannelID: channelID,
		Content:   content,
		Timestamp: time.Now(),
	}, nil
}

func (f *FakeDiscordAPI) MessageReactionAdd(channelID, messageID, emoji string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrReactionAdd != nil {
		return f.ErrReactionAdd
	}
	f.ReactionCalls = append(f.ReactionCalls, ReactionCall{
		ChannelID: channelID,
		MessageID: messageID,
		Emoji:     emoji,
	})
	return nil
}

func (f *FakeDiscordAPI) MessageReactionRemove(channelID, messageID, emoji, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrReactionRemove != nil {
		return f.ErrReactionRemove
	}
	f.ReactionCalls = append(f.ReactionCalls, ReactionCall{
		ChannelID: channelID,
		MessageID: messageID,
		Emoji:     emoji,
		Remove:    true,
		UserID:    userID,
	})
	return nil
}

func (f *FakeDiscordAPI) MessageThreadStart(channelID, messageID, name string, archiveDuration int) (*discordgo.Channel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrThreadStart != nil {
		return nil, f.ErrThreadStart
	}
	f.nextThreadID++
	threadID := fmt.Sprintf("thread-%d", f.nextThreadID)
	f.ThreadCreateCalls = append(f.ThreadCreateCalls, ThreadCreateCall{
		ChannelID:       channelID,
		MessageID:       messageID,
		Name:            name,
		ArchiveDuration: archiveDuration,
		CreatedID:       threadID,
	})
	// Auto-register the new thread channel
	f.Channels[threadID] = &FakeChannel{
		ID:       threadID,
		ParentID: channelID,
		Type:     discordgo.ChannelTypeGuildPublicThread,
		Name:     name,
	}
	return &discordgo.Channel{
		ID:       threadID,
		ParentID: channelID,
		Type:     discordgo.ChannelTypeGuildPublicThread,
		Name:     name,
	}, nil
}

func (f *FakeDiscordAPI) ChannelEdit(channelID string, edit *discordgo.ChannelEdit) (*discordgo.Channel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrChannelEdit != nil {
		return nil, f.ErrChannelEdit
	}
	f.ChannelEditCalls = append(f.ChannelEditCalls, ChannelEditCall{
		ChannelID: channelID,
		Edit:      edit,
	})
	return &discordgo.Channel{ID: channelID, Name: edit.Name}, nil
}

func (f *FakeDiscordAPI) InteractionRespond(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrInteractionResp != nil {
		return f.ErrInteractionResp
	}
	f.InteractionResponses = append(f.InteractionResponses, resp)
	return nil
}

func (f *FakeDiscordAPI) InteractionResponseEdit(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrInteractionEdit != nil {
		return nil, f.ErrInteractionEdit
	}
	f.InteractionResponseEdits = append(f.InteractionResponseEdits, edit)
	return &discordgo.Message{ID: "edit-response"}, nil
}

func (f *FakeDiscordAPI) BotUserID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.BotID
}

// ── Test Assertion Helpers ────────────────────────────────────────────────

// LastTypingChannel returns the channel ID of the last typing call, or "".
func (f *FakeDiscordAPI) LastTypingChannel() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.TypingCalls) == 0 {
		return ""
	}
	return f.TypingCalls[len(f.TypingCalls)-1].ChannelID
}

// TypingChannels returns all channel IDs where typing was triggered, in order.
func (f *FakeDiscordAPI) TypingChannels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	chans := make([]string, len(f.TypingCalls))
	for i, tc := range f.TypingCalls {
		chans[i] = tc.ChannelID
	}
	return chans
}

// ReactionsByEmoji returns all reaction calls matching the emoji.
func (f *FakeDiscordAPI) ReactionsByEmoji(emoji string) []ReactionCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ReactionCall
	for _, r := range f.ReactionCalls {
		if r.Emoji == emoji {
			out = append(out, r)
		}
	}
	return out
}

// LastMessageChannel returns the channel ID of the last message sent, or "".
func (f *FakeDiscordAPI) LastMessageChannel() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.MessageSendCalls) == 0 {
		return ""
	}
	return f.MessageSendCalls[len(f.MessageSendCalls)-1].ChannelID
}

// Reset clears all tracked calls (not channel state).
func (f *FakeDiscordAPI) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.TypingCalls = nil
	f.ReactionCalls = nil
	f.ThreadCreateCalls = nil
	f.MessageSendCalls = nil
	f.ChannelEditCalls = nil
	f.InteractionResponses = nil
	f.InteractionResponseEdits = nil
}

// Compile-time check
var _ DiscordAPI = (*FakeDiscordAPI)(nil)
