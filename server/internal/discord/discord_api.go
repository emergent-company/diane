// Package discord provides a Discord bot that bridges messages to Diane sessions.
package discord

import "github.com/bwmarrin/discordgo"

// DiscordAPI defines the subset of discordgo.Session methods used by the bot.
// This interface exists so the bot logic can be tested with a fake implementation
// instead of requiring a real Discord Gateway connection.
type DiscordAPI interface {
	// Channel returns a channel/thread by ID.
	Channel(channelID string) (*discordgo.Channel, error)

	// ChannelTyping triggers the typing indicator in a channel or thread.
	ChannelTyping(channelID string) error

	// ChannelMessageSend sends a message to a channel or thread.
	ChannelMessageSend(channelID, content string) (*discordgo.Message, error)

	// MessageReactionAdd adds a reaction emoji to a message.
	MessageReactionAdd(channelID, messageID, emoji string) error

	// MessageReactionRemove removes a reaction emoji from a message (by user).
	MessageReactionRemove(channelID, messageID, emoji, userID string) error

	// MessageThreadStart creates a new thread from a message.
	MessageThreadStart(channelID, messageID, name string, archiveDuration int) (*discordgo.Channel, error)

	// ChannelEdit updates a channel's properties (e.g., thread name).
	ChannelEdit(channelID string, edit *discordgo.ChannelEdit) (*discordgo.Channel, error)

	// InteractionRespond responds to a Discord interaction (button click, select menu, modal).
	InteractionRespond(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse) error

	// InteractionResponseEdit edits the original interaction response message.
	InteractionResponseEdit(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error)

	// BotUserID returns the bot's own Discord user ID.
	BotUserID() string
}

// compile-time check: discorderAPI implements DiscordAPI
var _ DiscordAPI = (*discordAPI)(nil)

// discordAPI adapts a *discordgo.Session to the DiscordAPI interface.
type discordAPI struct {
	session *discordgo.Session
}

// newDiscordAPI wraps a discordgo session.
func newDiscordAPI(s *discordgo.Session) DiscordAPI {
	return &discordAPI{session: s}
}

func (a *discordAPI) Channel(channelID string) (*discordgo.Channel, error) {
	return a.session.Channel(channelID)
}

func (a *discordAPI) ChannelTyping(channelID string) error {
	return a.session.ChannelTyping(channelID)
}

func (a *discordAPI) ChannelMessageSend(channelID, content string) (*discordgo.Message, error) {
	return a.session.ChannelMessageSend(channelID, content)
}

func (a *discordAPI) MessageReactionAdd(channelID, messageID, emoji string) error {
	return a.session.MessageReactionAdd(channelID, messageID, emoji)
}

func (a *discordAPI) MessageReactionRemove(channelID, messageID, emoji, userID string) error {
	return a.session.MessageReactionRemove(channelID, messageID, emoji, userID)
}

func (a *discordAPI) MessageThreadStart(channelID, messageID, name string, archiveDuration int) (*discordgo.Channel, error) {
	return a.session.MessageThreadStart(channelID, messageID, name, archiveDuration)
}

func (a *discordAPI) ChannelEdit(channelID string, edit *discordgo.ChannelEdit) (*discordgo.Channel, error) {
	return a.session.ChannelEdit(channelID, edit)
}

func (a *discordAPI) InteractionRespond(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse) error {
	return a.session.InteractionRespond(interaction, resp)
}

func (a *discordAPI) InteractionResponseEdit(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
	return a.session.InteractionResponseEdit(interaction, edit)
}

func (a *discordAPI) BotUserID() string {
	if a.session.State != nil && a.session.State.User != nil {
		return a.session.State.User.ID
	}
	return ""
}
