// Package notifications provides MCP tools for sending notifications (Discord, Home Assistant)
package notifications

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tools "github.com/Emergent-Comapny/diane/mcp/tools"
	_ "github.com/mattn/go-sqlite3"
)

// --- Configuration ---

type discordConfig struct {
	BotToken  string
	ChannelID string
}

type homeAssistantConfig struct {
	ServerURL     string `json:"server_url"`
	AccessToken   string `json:"access_token"`
	NotifyService string `json:"notify_service"`
}

type channelMappings map[string]string

func getDiscordBotToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	dbPath := filepath.Join(home, ".kimaki", "discord-sessions.db")
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return "", fmt.Errorf("failed to open Kimaki database: %w", err)
	}
	defer db.Close()

	var token string
	err = db.QueryRow("SELECT token FROM bot_tokens LIMIT 1").Scan(&token)
	if err != nil {
		return "", fmt.Errorf("no Discord bot token found in Kimaki database: %w", err)
	}

	return token, nil
}

func loadChannelMappings() (channelMappings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// Try DIANE config directories
	paths := []string{
		filepath.Join(home, ".diane", "secrets", "discord-channels.json"),
		filepath.Join(home, ".diane", "secrets", "discord-channels.json"),
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var mappings channelMappings
		if err := json.Unmarshal(data, &mappings); err != nil {
			continue
		}
		return mappings, nil
	}

	return channelMappings{}, nil
}

func getDefaultChannelID() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	dbPath := filepath.Join(home, ".kimaki", "discord-sessions.db")
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return "", fmt.Errorf("failed to open Kimaki database: %w", err)
	}
	defer db.Close()

	// Look for diane channel in Kimaki database
	var channelID string
	err = db.QueryRow(
		"SELECT channel_id FROM channel_directories WHERE directory LIKE '%diane%' ORDER BY id LIMIT 1",
	).Scan(&channelID)
	if err != nil {
		return "", fmt.Errorf("no Discord channel mapping found for diane: %w", err)
	}

	return channelID, nil
}

func getChannelID(channelNameOrID string) (string, error) {
	// If no channel specified, use default
	if channelNameOrID == "" {
		return getDefaultChannelID()
	}

	// If it looks like a channel ID (all digits), use directly
	if matched, _ := regexp.MatchString(`^\d+$`, channelNameOrID); matched {
		return channelNameOrID, nil
	}

	// Otherwise, look up in config
	mappings, err := loadChannelMappings()
	if err != nil {
		return "", err
	}

	channelID, ok := mappings[channelNameOrID]
	if !ok {
		keys := make([]string, 0, len(mappings))
		for k := range mappings {
			keys = append(keys, k)
		}
		return "", fmt.Errorf("channel %q not found in discord-channels.json. Available: %s", channelNameOrID, strings.Join(keys, ", "))
	}

	return channelID, nil
}

func getHomeAssistantConfig() (*homeAssistantConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(home, ".diane", "secrets", "homeassistant-config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("Home Assistant config not found. Create %s with server_url, access_token, notify_service", configPath)
	}

	var config homeAssistantConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse Home Assistant config: %w", err)
	}

	return &config, nil
}

// --- Tool Definition ---

// Tool represents an MCP tool definition
type Tool = tools.Tool

// Provider implements ToolProvider for notification services
type Provider struct {
	discordAvailable       bool
	homeAssistantAvailable bool
}

// NewProvider creates a new notifications tools provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "notifications"
}

// CheckDependencies verifies at least one notification service is available
func (p *Provider) CheckDependencies() error {
	// Check Discord
	_, err := getDiscordBotToken()
	p.discordAvailable = err == nil

	// Check Home Assistant
	_, err = getHomeAssistantConfig()
	p.homeAssistantAvailable = err == nil

	if !p.discordAvailable && !p.homeAssistantAvailable {
		return fmt.Errorf("no notification services available (Discord or Home Assistant)")
	}

	return nil
}

// Tools returns all notification tools
func (p *Provider) Tools() []Tool {
	var result []Tool

	// Discord tools
	if p.discordAvailable {
		result = append(result, []Tool{
			{
				Name: "discord_send_notification",
				Description: `Send a notification message to Discord channel via the Kimaki bot.

Uses the Discord bot credentials stored in Kimaki database.
Perfect for automation notifications, cron job results, and alerts.`,
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"message":      tools.StringProperty("Message content to send", false),
						"title":        tools.StringProperty("Optional title/header (will be bolded)", false),
						"channel_name": tools.StringProperty("Channel name from config, channel ID (digits only), or default #diane", false),
					},
					[]string{"message"},
				),
			},
			{
				Name: "discord_send_embed",
				Description: `Send a rich embed notification to Discord channel.

Rich embeds support:
- Color coding (success=green, error=red, warning=yellow, info=blue)
- Title and description
- Multiple fields (key-value pairs)
- Footer text`,
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"title":        tools.StringProperty("Embed title", false),
						"description":  tools.StringProperty("Embed description text", false),
						"color":        tools.StringProperty("Color: success, error, warning, info (default: info)", false),
						"fields":       tools.StringProperty("JSON array of fields: [{name: string, value: string, inline?: boolean}]", false),
						"footer":       tools.StringProperty("Footer text", false),
						"channel_name": tools.StringProperty("Channel name from config, channel ID, or default", false),
					},
					[]string{"title"},
				),
			},
			{
				Name: "discord_send_reaction",
				Description: `Add a reaction emoji to a Discord message.

Useful for acknowledging task completion, status indicators, and quick feedback.
Common emojis: ✅ ❌ ⚠️ ℹ️ 🔄 ⏳ 💰 📊 🚀`,
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"message_id":   tools.StringProperty("Discord message ID to react to", false),
						"emoji":        tools.StringProperty("Emoji to use (unicode emoji like ✅ or custom :emoji_name:)", false),
						"channel_name": tools.StringProperty("Channel name from config, channel ID, or default", false),
					},
					[]string{"message_id", "emoji"},
				),
			},
			{
				Name: "discord_send_message_with_buttons",
				Description: `Send a message or embed with interactive buttons to Discord.

Buttons allow users to click and respond to questions or options.
Use for yes/no questions, multiple choice, approval workflows.`,
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"message":           tools.StringProperty("Message text (optional if using embed)", false),
						"embed_title":       tools.StringProperty("Embed title (optional)", false),
						"embed_description": tools.StringProperty("Embed description (optional)", false),
						"embed_color":       tools.StringProperty("Embed color: success, error, warning, info", false),
						"buttons":           tools.StringProperty("JSON array of buttons: [{label: string, custom_id: string, style?: number}]. Styles: 1=Primary(blue), 2=Secondary(gray), 3=Success(green), 4=Danger(red)", false),
						"channel_name":      tools.StringProperty("Channel name from config, channel ID, or default", false),
					},
					[]string{"buttons"},
				),
			},
		}...)
	}

	// Home Assistant tools
	if p.homeAssistantAvailable {
		result = append(result, []Tool{
			{
				Name:        "homeassistant_send_notification",
				Description: "Send a notification to Home Assistant companion app. Use for cron job summaries, alerts, and important updates.",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"message": tools.StringProperty("Notification message text", false),
						"title":   tools.StringProperty("Notification title (optional)", false),
						"data":    tools.StringProperty("Additional notification data as JSON string (optional)", false),
					},
					[]string{"message"},
				),
			},
			{
				Name:        "homeassistant_send_actionable_notification",
				Description: "Send an actionable notification with buttons/actions to Home Assistant companion app",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"message": tools.StringProperty("Notification message text", false),
						"title":   tools.StringProperty("Notification title", false),
						"actions": tools.StringProperty("Actions as JSON array: [{action: string, title: string}]", false),
					},
					[]string{"message", "actions"},
				),
			},
			{
				Name:        "homeassistant_send_command",
				Description: "Send a command to Home Assistant companion app (e.g., update_sensors, request_location_update, command_screen_on)",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"command": tools.StringProperty("Command message (e.g., 'command_update_sensors', 'request_location_update')", false),
						"data":    tools.StringProperty("Additional command data as JSON string (optional)", false),
					},
					[]string{"command"},
				),
			},
		}...)
	}

	return result
}

// HasTool checks if a tool name belongs to this provider
func (p *Provider) HasTool(name string) bool {
	for _, tool := range p.Tools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// Call executes a tool by name
func (p *Provider) Call(name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	// Discord tools
	case "discord_send_notification":
		return p.discordSendNotification(args)
	case "discord_send_embed":
		return p.discordSendEmbed(args)
	case "discord_send_reaction":
		return p.discordSendReaction(args)
	case "discord_send_message_with_buttons":
		return p.discordSendMessageWithButtons(args)
	// Home Assistant tools
	case "homeassistant_send_notification":
		return p.haSendNotification(args)
	case "homeassistant_send_actionable_notification":
		return p.haSendActionableNotification(args)
	case "homeassistant_send_command":
		return p.haSendCommand(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// --- Discord Tool Implementations ---

func (p *Provider) discordSendNotification(args map[string]interface{}) (interface{}, error) {
	message, err := tools.GetStringRequired(args, "message")
	if err != nil {
		return nil, err
	}
	title := tools.GetString(args, "title")
	channelName := tools.GetString(args, "channel_name")

	botToken, err := getDiscordBotToken()
	if err != nil {
		return nil, err
	}

	channelID, err := getChannelID(channelName)
	if err != nil {
		return nil, err
	}

	content := message
	if title != "" {
		content = fmt.Sprintf("**%s**\n%s", title, message)
	}

	payload := map[string]interface{}{
		"content": content,
	}

	result, err := discordAPICall(botToken, channelID, "messages", "POST", payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send Discord notification: %w", err)
	}

	response := map[string]interface{}{
		"success":    true,
		"message_id": result["id"],
		"channel_id": channelID,
		"timestamp":  result["timestamp"],
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) discordSendEmbed(args map[string]interface{}) (interface{}, error) {
	title, err := tools.GetStringRequired(args, "title")
	if err != nil {
		return nil, err
	}
	description := tools.GetString(args, "description")
	colorName := tools.GetString(args, "color")
	fieldsJSON := tools.GetString(args, "fields")
	footer := tools.GetString(args, "footer")
	channelName := tools.GetString(args, "channel_name")

	botToken, err := getDiscordBotToken()
	if err != nil {
		return nil, err
	}

	channelID, err := getChannelID(channelName)
	if err != nil {
		return nil, err
	}

	// Map color names to Discord color values
	colorMap := map[string]int{
		"success": 0x00ff00, // Green
		"error":   0xff0000, // Red
		"warning": 0xffff00, // Yellow
		"info":    0x0099ff, // Blue
	}
	color := colorMap["info"]
	if c, ok := colorMap[colorName]; ok {
		color = c
	}

	// Parse fields if provided
	var fields []map[string]interface{}
	if fieldsJSON != "" {
		if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
			return nil, fmt.Errorf("invalid fields JSON: %w", err)
		}
	}

	embed := map[string]interface{}{
		"title":       title,
		"description": description,
		"color":       color,
		"fields":      fields,
	}
	if footer != "" {
		embed["footer"] = map[string]string{"text": footer}
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{embed},
	}

	result, err := discordAPICall(botToken, channelID, "messages", "POST", payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send Discord embed: %w", err)
	}

	response := map[string]interface{}{
		"success":    true,
		"message_id": result["id"],
		"channel_id": channelID,
		"timestamp":  result["timestamp"],
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) discordSendReaction(args map[string]interface{}) (interface{}, error) {
	messageID, err := tools.GetStringRequired(args, "message_id")
	if err != nil {
		return nil, err
	}
	emoji, err := tools.GetStringRequired(args, "emoji")
	if err != nil {
		return nil, err
	}
	channelName := tools.GetString(args, "channel_name")

	botToken, err := getDiscordBotToken()
	if err != nil {
		return nil, err
	}

	channelID, err := getChannelID(channelName)
	if err != nil {
		return nil, err
	}

	// URL-encode the emoji
	encodedEmoji := url.PathEscape(emoji)

	// Add reaction via Discord API
	apiURL := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages/%s/reactions/%s/@me",
		channelID, messageID, encodedEmoji)

	req, err := http.NewRequest("PUT", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Length", "0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Discord API error: %s - %s", resp.Status, string(body))
	}

	response := map[string]interface{}{
		"success":    true,
		"message_id": messageID,
		"emoji":      emoji,
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) discordSendMessageWithButtons(args map[string]interface{}) (interface{}, error) {
	buttonsJSON, err := tools.GetStringRequired(args, "buttons")
	if err != nil {
		return nil, err
	}
	message := tools.GetString(args, "message")
	embedTitle := tools.GetString(args, "embed_title")
	embedDescription := tools.GetString(args, "embed_description")
	embedColor := tools.GetString(args, "embed_color")
	channelName := tools.GetString(args, "channel_name")

	botToken, err := getDiscordBotToken()
	if err != nil {
		return nil, err
	}

	channelID, err := getChannelID(channelName)
	if err != nil {
		return nil, err
	}

	// Parse buttons
	var buttons []map[string]interface{}
	if err := json.Unmarshal([]byte(buttonsJSON), &buttons); err != nil {
		return nil, fmt.Errorf("invalid buttons JSON: %w", err)
	}

	// Build button components
	buttonComponents := make([]map[string]interface{}, len(buttons))
	for i, btn := range buttons {
		style := 1 // Default to Primary (blue)
		if s, ok := btn["style"].(float64); ok {
			style = int(s)
		}
		buttonComponents[i] = map[string]interface{}{
			"type":      2, // Button
			"label":     btn["label"],
			"custom_id": btn["custom_id"],
			"style":     style,
		}
		if url, ok := btn["url"].(string); ok {
			buttonComponents[i]["url"] = url
		}
	}

	components := []map[string]interface{}{
		{
			"type":       1, // Action Row
			"components": buttonComponents,
		},
	}

	payload := map[string]interface{}{
		"components": components,
	}

	// Add embed if provided
	if embedTitle != "" || embedDescription != "" {
		colorMap := map[string]int{
			"success": 0x00ff00,
			"error":   0xff0000,
			"warning": 0xffff00,
			"info":    0x0099ff,
		}
		color := colorMap["info"]
		if c, ok := colorMap[embedColor]; ok {
			color = c
		}

		payload["embeds"] = []map[string]interface{}{
			{
				"title":       embedTitle,
				"description": embedDescription,
				"color":       color,
			},
		}
	}

	// Add plain text message if provided
	if message != "" {
		payload["content"] = message
	}

	result, err := discordAPICall(botToken, channelID, "messages", "POST", payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send message with buttons: %w", err)
	}

	response := map[string]interface{}{
		"success":    true,
		"message_id": result["id"],
		"channel_id": channelID,
		"timestamp":  result["timestamp"],
		"note":       "Button interactions need to be handled by Kimaki bot",
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func discordAPICall(botToken, channelID, endpoint, method string, payload interface{}) (map[string]interface{}, error) {
	apiURL := fmt.Sprintf("https://discord.com/api/v10/channels/%s/%s", channelID, endpoint)

	var reqBody io.Reader
	if payload != nil {
		jsonBody, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, apiURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Discord API error: %s - %s", resp.Status, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

// --- Home Assistant Tool Implementations ---

func (p *Provider) haSendNotification(args map[string]interface{}) (interface{}, error) {
	message, err := tools.GetStringRequired(args, "message")
	if err != nil {
		return nil, err
	}
	title := tools.GetString(args, "title")
	dataJSON := tools.GetString(args, "data")

	config, err := getHomeAssistantConfig()
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"message": message,
	}
	if title != "" {
		payload["title"] = title
	}
	if dataJSON != "" {
		var data interface{}
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			return nil, fmt.Errorf("invalid data JSON: %w", err)
		}
		payload["data"] = data
	}

	if err := haAPICall(config, payload); err != nil {
		return nil, fmt.Errorf("failed to send notification: %w", err)
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Notification sent successfully",
		"title":   title,
		"sent_to": config.NotifyService,
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) haSendActionableNotification(args map[string]interface{}) (interface{}, error) {
	message, err := tools.GetStringRequired(args, "message")
	if err != nil {
		return nil, err
	}
	actionsJSON, err := tools.GetStringRequired(args, "actions")
	if err != nil {
		return nil, err
	}
	title := tools.GetString(args, "title")

	config, err := getHomeAssistantConfig()
	if err != nil {
		return nil, err
	}

	var actions interface{}
	if err := json.Unmarshal([]byte(actionsJSON), &actions); err != nil {
		return nil, fmt.Errorf("invalid actions JSON: %w", err)
	}

	payload := map[string]interface{}{
		"message": message,
		"title":   title,
		"data": map[string]interface{}{
			"actions": actions,
		},
	}

	if err := haAPICall(config, payload); err != nil {
		return nil, fmt.Errorf("failed to send actionable notification: %w", err)
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Actionable notification sent successfully",
		"actions": actions,
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) haSendCommand(args map[string]interface{}) (interface{}, error) {
	command, err := tools.GetStringRequired(args, "command")
	if err != nil {
		return nil, err
	}
	dataJSON := tools.GetString(args, "data")

	config, err := getHomeAssistantConfig()
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"message": command,
	}
	if dataJSON != "" {
		var data interface{}
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			return nil, fmt.Errorf("invalid data JSON: %w", err)
		}
		payload["data"] = data
	}

	if err := haAPICall(config, payload); err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Command sent successfully",
		"command": command,
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func haAPICall(config *homeAssistantConfig, payload interface{}) error {
	// Build the API endpoint
	// notify.mobile_app_xxx -> notify/mobile_app_xxx
	service := strings.Replace(config.NotifyService, "notify.", "", 1)
	service = strings.Replace(service, "mobile_app_", "notify/mobile_app_", 1)
	apiURL := fmt.Sprintf("%s/api/services/%s", config.ServerURL, service)

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+config.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Home Assistant API error (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}
