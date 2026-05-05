// Package apple provides MCP tools for Apple Reminders, Contacts, Calendar,
// Notes, Mail, Messages, and local Notifications via AppleScript.
package apple

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Emergent-Comapny/diane/mcp/tools"
)

// --- Helper Functions (embedded from SDK) ---

// RunCommand executes a command and returns stdout
func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return "", fmt.Errorf("%s: %s", err, stderrStr)
		}
		return "", err
	}

	return strings.TrimSpace(stdout.String()), nil
}

// runOSAScript runs an AppleScript via osascript and returns the result.
func runOSAScript(script string) (string, error) {
	return runCommand("osascript", "-e", script)
}

// runAppleScriptJSON runs an AppleScript and parses its JSON output.
func runAppleScriptJSON(result interface{}, script string) error {
	output, err := runOSAScript(script)
	if err != nil {
		return err
	}
	if output == "" {
		return nil // empty is valid
	}
	return nil
}

// escapeAppleScript escapes a string for safe embedding in AppleScript.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// CommandExists checks if a command is available in PATH
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// GetString extracts a string argument, returns empty string if not found
func getString(args map[string]interface{}, key string) string {
	if val, ok := args[key].(string); ok {
		return val
	}
	return ""
}

// GetStringRequired extracts a required string argument
func getStringRequired(args map[string]interface{}, key string) (string, error) {
	if val, ok := args[key].(string); ok && val != "" {
		return val, nil
	}
	return "", fmt.Errorf("missing required argument: %s", key)
}

// TextContent creates an MCP text content response
func textContent(text string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
	}
}

// ObjectSchema creates a standard object inputSchema
func objectSchema(properties map[string]interface{}, required []string) map[string]interface{} {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// StringProperty creates a string property for inputSchema
func stringProperty(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": description,
	}
}

// --- Tool Definition ---

// Tool is a type alias for the shared tools.Tool type
type Tool = tools.Tool

// Provider implements ToolProvider for Apple services
type Provider struct {
	swiftScriptPath string
}

// NewProvider creates a new Apple tools provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "apple"
}

// CheckDependencies verifies required binaries exist
func (p *Provider) CheckDependencies() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("Apple tools are only available on macOS")
	}

	if !commandExists("remindctl") {
		return fmt.Errorf("remindctl not found. Install with: brew install keith/formulae/remindctl")
	}

	if !commandExists("swift") {
		return fmt.Errorf("swift not found. Install Xcode Command Line Tools")
	}

	if !commandExists("osascript") {
		return fmt.Errorf("osascript not found")
	}

	return nil
}

// SetSwiftScriptPath sets the path to the contacts Swift script
func (p *Provider) SetSwiftScriptPath(path string) {
	p.swiftScriptPath = path
}

// Tools returns all Apple tools
func (p *Provider) Tools() []Tool {
	return []Tool{
		// --- Reminders ---
		{
			Name:        "apple_list_reminders",
			Description: "List Apple Reminders from a specific list or all lists",
			InputSchema: objectSchema(
				map[string]interface{}{
					"listName": stringProperty("The name of the reminder list (optional, lists all if omitted)"),
				},
				nil,
			),
		},
		{
			Name:        "apple_add_reminder",
			Description: "Add a new Apple Reminder. Dates must be in YYYY-MM-DD HH:MM format.",
			InputSchema: objectSchema(
				map[string]interface{}{
					"title":    stringProperty("The title of the reminder"),
					"listName": stringProperty("The list to add the reminder to (optional)"),
					"due":      stringProperty("Due date/time in 'YYYY-MM-DD HH:MM' format (optional)"),
				},
				[]string{"title"},
			),
		},
		// --- Contacts ---
		{
			Name:        "apple_search_contacts",
			Description: "Search Apple Contacts by name, email, or phone number. Returns ID, name and email columns in tab-separated format.",
			InputSchema: objectSchema(
				map[string]interface{}{
					"query": stringProperty("Search query (name, email, or phone). Use empty string to list all contacts."),
				},
				[]string{"query"},
			),
		},
		{
			Name:        "apple_list_all_contacts",
			Description: "List all contacts in your Apple Contacts. Returns ID, name and email columns.",
			InputSchema: objectSchema(
				map[string]interface{}{},
				nil,
			),
		},
		// --- Calendar ---
		{
			Name:        "apple_list_calendars",
			Description: "List all calendar names available in Apple Calendar.",
			InputSchema: objectSchema(
				map[string]interface{}{},
				nil,
			),
		},
		{
			Name:        "apple_list_events",
			Description: "List calendar events for today, or for a specific date. Dates in YYYY-MM-DD format.",
			InputSchema: objectSchema(
				map[string]interface{}{
					"date":       stringProperty("Date in YYYY-MM-DD format (optional, defaults to today)"),
					"calendar":   stringProperty("Calendar name to filter by (optional)"),
					"maxResults": stringProperty("Maximum number of events to return (optional, default: 20)"),
				},
				nil,
			),
		},
		{
			Name:        "apple_create_event",
			Description: "Create a new calendar event. Dates in 'YYYY-MM-DD HH:MM' format.",
			InputSchema: objectSchema(
				map[string]interface{}{
					"title":       stringProperty("Event title/summary"),
					"startDate":   stringProperty("Start date/time in 'YYYY-MM-DD HH:MM' format"),
					"endDate":     stringProperty("End date/time in 'YYYY-MM-DD HH:MM' format"),
					"calendar":    stringProperty("Calendar name to add to (optional, uses default)"),
					"location":    stringProperty("Event location (optional)"),
					"notes":       stringProperty("Event notes/description (optional)"),
					"isAllDay":    stringProperty("Set to 'true' for all-day event (optional)"),
				},
				[]string{"title", "startDate", "endDate"},
			),
		},
		// --- Notes ---
		{
			Name:        "apple_list_notes",
			Description: "List notes from your Apple Notes. Shows note title, folder, and modification date.",
			InputSchema: objectSchema(
				map[string]interface{}{
					"folder": stringProperty("Folder/account name to filter by (optional, e.g., 'iCloud', 'On My Mac')"),
					"maxResults": stringProperty("Maximum number of notes to return (optional, default: 30)"),
				},
				nil,
			),
		},
		{
			Name:        "apple_create_note",
			Description: "Create a new note in Apple Notes.",
			InputSchema: objectSchema(
				map[string]interface{}{
					"title":  stringProperty("The title of the note"),
					"body":   stringProperty("The body text of the note"),
					"folder": stringProperty("Folder/account to add to (optional, e.g., 'iCloud', 'Notes')"),
				},
				[]string{"title", "body"},
			),
		},
		// --- Mail ---
		{
			Name:        "apple_list_inbox",
			Description: "List recent email messages from the Mail.app inbox.",
			InputSchema: objectSchema(
				map[string]interface{}{
					"maxResults": stringProperty("Maximum number of messages to return (optional, default: 10)"),
				},
				nil,
			),
		},
		{
			Name:        "apple_send_email",
			Description: "Send an email via the Mail.app. Requires Mail.app to be configured.",
			InputSchema: objectSchema(
				map[string]interface{}{
					"to":      stringProperty("Recipient email address"),
					"subject": stringProperty("Email subject"),
					"body":    stringProperty("Email body text"),
				},
				[]string{"to", "subject", "body"},
			),
		},
		// --- Messages ---
		{
			Name:        "apple_send_imessage",
			Description: "Send an iMessage to a contact via the Messages app. Recipient can be an email, phone number, or contact name.",
			InputSchema: objectSchema(
				map[string]interface{}{
					"text":      stringProperty("The message text to send"),
					"recipient": stringProperty("Recipient: phone number, email, or contact name"),
				},
				[]string{"text", "recipient"},
			),
		},
		{
			Name:        "apple_list_conversations",
			Description: "List recent iMessage conversations with participant names.",
			InputSchema: objectSchema(
				map[string]interface{}{},
				nil,
			),
		},
		// --- Notifications ---
		{
			Name:        "apple_show_notification",
			Description: "Show a local macOS notification via Notification Center. Useful for alerts and reminders.",
			InputSchema: objectSchema(
				map[string]interface{}{
					"title":   stringProperty("Notification title"),
					"message": stringProperty("Notification body text"),
				},
				[]string{"title", "message"},
			),
		},
	}
}

// HasTool checks if a tool name belongs to this provider
func (p *Provider) HasTool(name string) bool {
	for _, t := range p.Tools() {
		if t.Name == name {
			return true
		}
	}
	return false
}

// Call executes a tool by name
func (p *Provider) Call(name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "apple_list_reminders":
		return p.listReminders(args)
	case "apple_add_reminder":
		return p.addReminder(args)
	case "apple_search_contacts":
		return p.searchContacts(args)
	case "apple_list_all_contacts":
		return p.listAllContacts(args)
	case "apple_list_calendars":
		return p.listCalendars(args)
	case "apple_list_events":
		return p.listEvents(args)
	case "apple_create_event":
		return p.createEvent(args)
	case "apple_list_notes":
		return p.listNotes(args)
	case "apple_create_note":
		return p.createNote(args)
	case "apple_list_inbox":
		return p.listInbox(args)
	case "apple_send_email":
		return p.sendEmail(args)
	case "apple_send_imessage":
		return p.sendMessage(args)
	case "apple_list_conversations":
		return p.listConversations(args)
	case "apple_show_notification":
		return p.showNotification(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// ─── Reminders Tools ──────────────────────────────────────────

func (p *Provider) listReminders(args map[string]interface{}) (interface{}, error) {
	listName := getString(args, "listName")

	var output string
	var err error

	if listName != "" {
		output, err = runCommand("remindctl", "list", listName, "--json")
	} else {
		output, err = runCommand("remindctl", "list", "--json")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list reminders: %w", err)
	}

	if output == "" {
		output = "No reminders found."
	}

	return textContent(output), nil
}

func (p *Provider) addReminder(args map[string]interface{}) (interface{}, error) {
	title, err := getStringRequired(args, "title")
	if err != nil {
		return nil, err
	}

	listName := getString(args, "listName")
	due := getString(args, "due")

	cmdArgs := []string{"add", title}

	if listName != "" {
		cmdArgs = append(cmdArgs, "--list", listName)
	}

	if due != "" {
		cmdArgs = append(cmdArgs, "--due", due)
	}

	output, err := runCommand("remindctl", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to add reminder: %w", err)
	}

	return textContent(output), nil
}

// ─── Contacts Tools ───────────────────────────────────────────

func (p *Provider) getSwiftScriptPath() string {
	if p.swiftScriptPath != "" {
		return p.swiftScriptPath
	}
	// Default path relative to the diane repo
	return filepath.Join(".diane", "tools", "lib", "search_all_contacts.swift")
}

func (p *Provider) searchContacts(args map[string]interface{}) (interface{}, error) {
	query := getString(args, "query")

	scriptPath := p.getSwiftScriptPath()
	output, err := runCommand("swift", scriptPath, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search contacts: %w", err)
	}

	if output == "" {
		output = "No contacts found."
	}

	return textContent(output), nil
}

func (p *Provider) listAllContacts(args map[string]interface{}) (interface{}, error) {
	scriptPath := p.getSwiftScriptPath()
	output, err := runCommand("swift", scriptPath, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list contacts: %w", err)
	}

	if output == "" {
		output = "No contacts found."
	}

	return textContent(output), nil
}

// ─── Calendar Tools ───────────────────────────────────────────

func (p *Provider) listCalendars(args map[string]interface{}) (interface{}, error) {
	script := `tell application "Calendar"
		set calNames to {}
		repeat with c in calendars
			set end of calNames to name of c
		end repeat
		return calNames
	end tell`
	output, err := runOSAScript(script)
	if err != nil {
		return nil, fmt.Errorf("failed to list calendars: %w", err)
	}
	if output == "" {
		output = "No calendars found."
	}
	return textContent("Calendars:\n" + output), nil
}

func (p *Provider) listEvents(args map[string]interface{}) (interface{}, error) {
	dateStr := getString(args, "date")
	calendarName := getString(args, "calendar")

	// Default to today
	if dateStr == "" {
		dateStr = "today"
	}

	// Build AppleScript to list events for the given date
	script := fmt.Sprintf(`set todayDate to current date
	set startDate to date "%s 00:00:00"
	set endDate to date "%s 23:59:59"
	
	tell application "Calendar"
		set output to ""
		repeat with c in calendars
			set calName to name of c
			%s
			try
				set todaysEvents to (every event of c whose start date ≥ startDate and start date ≤ endDate)
				repeat with e in todaysEvents
					set eventStart to start date of e
					set startStr to (time string of eventStart)
					set eventSummary to summary of e
					set eventLocation to location of e
					if eventLocation is missing value then set eventLocation to ""
					if eventLocation is not "" then
						set output to output & "• " & startStr & " - " & eventSummary & " (" & eventLocation & ")" & return
					else
						set output to output & "• " & startStr & " - " & eventSummary & return
					end if
				end repeat
			end try
		end repeat
		if output is "" then set output to "No events found."
		return output
	end tell`, dateStr, dateStr, calendarFilter(calendarName))

	output, err := runOSAScript(script)
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}
	return textContent(output), nil
}

// calendarFilter generates a filter for a specific calendar name, or empty string for all.
func calendarFilter(name string) string {
	if name == "" {
		return ""
	}
	escaped := escapeAppleScript(name)
	return fmt.Sprintf(`if calName is not "%s" then set c's events to {}`, escaped)
}

func (p *Provider) createEvent(args map[string]interface{}) (interface{}, error) {
	title, err := getStringRequired(args, "title")
	if err != nil {
		return nil, err
	}
	startDate, err := getStringRequired(args, "startDate")
	if err != nil {
		return nil, err
	}
	endDate, err := getStringRequired(args, "endDate")
	if err != nil {
		return nil, err
	}

	calendarName := getString(args, "calendar")
	location := getString(args, "location")
	notes := getString(args, "notes")

	escapedTitle := escapeAppleScript(title)
	escapedNotes := escapeAppleScript(notes)
	escapedLocation := escapeAppleScript(location)

	var calendarBlock string
	if calendarName != "" {
		escapedCal := escapeAppleScript(calendarName)
		calendarBlock = fmt.Sprintf(`set targetCal to calendar "%s"`, escapedCal)
	} else {
		calendarBlock = `set targetCal to first calendar`
	}

	script := fmt.Sprintf(`%s
	
	set startDate to date "%s"
	set endDate to date "%s"
	
	tell application "Calendar"
		tell targetCal
			set newEvent to make new event at end of events with properties {summary:"%s", start date:startDate, end date:endDate, description:"%s", location:"%s"}
		end tell
		return "Created event: " & "%s"
	end tell`, calendarBlock, startDate, endDate, escapedTitle, escapedNotes, escapedLocation, escapedTitle)

	_, err = runOSAScript(script)
	if err != nil {
		return nil, fmt.Errorf("failed to create event: %w", err)
	}

	return textContent(fmt.Sprintf("Created event: %s", title)), nil
}

// ─── Notes Tools ──────────────────────────────────────────────

func (p *Provider) listNotes(args map[string]interface{}) (interface{}, error) {
	folder := getString(args, "folder")

	var script string
	if folder != "" {
		escapedFolder := escapeAppleScript(folder)
		script = fmt.Sprintf(`tell application "Notes"
			set output to ""
			repeat with a in every account
				set accName to name of a
				repeat with f in folders of a
					if name of f is "%s" or accName is "%s" then
						repeat with n in notes of f
							set noteTitle to name of n
							set modDate to modification date of n
							set output to output & "• " & noteTitle & " (" & accName & "/" & (name of f) & ")" & return
						end repeat
					end if
				end repeat
			end repeat
			if output is "" then set output to "No notes found in folder: %s"
			return output
		end tell`, escapedFolder, escapedFolder, escapedFolder)
	} else {
		script = `tell application "Notes"
			set output to ""
			repeat with a in every account
				set accName to name of a
				repeat with f in folders of a
					repeat with n in notes of f
						set noteTitle to name of n
						set output to output & "• " & noteTitle & " (" & accName & "/" & (name of f) & ")" & return
					end repeat
				end repeat
			end repeat
			if output is "" then set output to "No notes found."
			return output
		end tell`
	}

	output, err := runOSAScript(script)
	if err != nil {
		return nil, fmt.Errorf("failed to list notes: %w", err)
	}

	return textContent(output), nil
}

func (p *Provider) createNote(args map[string]interface{}) (interface{}, error) {
	title, err := getStringRequired(args, "title")
	if err != nil {
		return nil, err
	}
	body, err := getStringRequired(args, "body")
	if err != nil {
		return nil, err
	}

	escapedTitle := escapeAppleScript(title)
	escapedBody := escapeAppleScript(body)

	folder := getString(args, "folder")

	var script string
	if folder != "" {
		escapedFolder := escapeAppleScript(folder)
		script = fmt.Sprintf(`tell application "Notes"
			set targetFolder to missing value
			repeat with a in every account
				repeat with f in folders of a
					if name of f is "%s" then
						set targetFolder to f
						exit repeat
					end if
				end repeat
				if targetFolder is not missing value then exit repeat
			end repeat
			if targetFolder is missing value then
				set targetFolder to default folder of first account
			end if
			make new note at targetFolder with properties {name:"%s", body:"%s"}
			return "Created note: %s"
		end tell`, escapedFolder, escapedTitle, escapedBody, escapedTitle)
	} else {
		script = fmt.Sprintf(`tell application "Notes"
			set targetFolder to default folder of first account
			make new note at targetFolder with properties {name:"%s", body:"%s"}
			return "Created note: %s"
		end tell`, escapedTitle, escapedBody, escapedTitle)
	}

	_, err = runOSAScript(script)
	if err != nil {
		return nil, fmt.Errorf("failed to create note: %w", err)
	}

	return textContent(fmt.Sprintf("Created note: %s", title)), nil
}

// ─── Mail Tools ───────────────────────────────────────────────

func (p *Provider) listInbox(args map[string]interface{}) (interface{}, error) {
	script := `tell application "Mail"
		set msgs to messages of inbox
		set output to ""
		repeat with i from 1 to count of msgs
			set msg to item i of msgs
			set msgSubject to subject of msg
			if msgSubject is missing value then set msgSubject to "(no subject)"
			set msgSender to sender of msg
			if msgSender is missing value then set msgSender to "(unknown)"
			set msgDate to date received of msg
			if msgDate is missing value then
				set dateStr to ""
			else
				set dateStr to (msgDate as string)
			end if
			set output to output & "• " & msgSubject & " — " & msgSender & " [" & dateStr & "]" & return
		end repeat
		if output is "" then set output to "Inbox is empty."
		return output
	end tell`

	output, err := runOSAScript(script)
	if err != nil {
		return nil, fmt.Errorf("failed to list inbox: %w", err)
	}
	return textContent(output), nil
}

func (p *Provider) sendEmail(args map[string]interface{}) (interface{}, error) {
	to, err := getStringRequired(args, "to")
	if err != nil {
		return nil, err
	}
	subject, err := getStringRequired(args, "subject")
	if err != nil {
		return nil, err
	}
	body, err := getStringRequired(args, "body")
	if err != nil {
		return nil, err
	}

	escapedTo := escapeAppleScript(to)
	escapedSubject := escapeAppleScript(subject)
	escapedBody := escapeAppleScript(body)

	script := fmt.Sprintf(`tell application "Mail"
		set newMessage to make new outgoing message with properties {subject:"%s", content:"%s", visible:true}
		tell newMessage
			make new to recipient at end of recipients with properties {address:"%s"}
			send
		end tell
		return "Sent email to: %s"
	end tell`, escapedSubject, escapedBody, escapedTo, escapedTo)

	output, err := runOSAScript(script)
	if err != nil {
		return nil, fmt.Errorf("failed to send email: %w", err)
	}
	return textContent(output), nil
}

// ─── Messages Tools ───────────────────────────────────────────

func (p *Provider) sendMessage(args map[string]interface{}) (interface{}, error) {
	text, err := getStringRequired(args, "text")
	if err != nil {
		return nil, err
	}
	recipient, err := getStringRequired(args, "recipient")
	if err != nil {
		return nil, err
	}

	escapedText := escapeAppleScript(text)
	escapedRecipient := escapeAppleScript(recipient)

	script := fmt.Sprintf(`tell application "Messages"
		set targetService to 1st service whose service type = iMessage
		set targetBuddy to buddy "%s" of targetService
		send "%s" to targetBuddy
		return "Sent iMessage to: %s"
	end tell`, escapedRecipient, escapedText, escapedRecipient)

	output, err := runOSAScript(script)
	if err != nil {
		return nil, fmt.Errorf("failed to send iMessage: %w", err)
	}
	return textContent(output), nil
}

func (p *Provider) listConversations(args map[string]interface{}) (interface{}, error) {
	script := `tell application "Messages"
		set chatNames to {}
		repeat with c in text chats
			set chatName to name of c
			if chatName is not missing value and chatName is not "" then
				set end of chatNames to chatName
			end if
		end repeat
		if (count of chatNames) is 0 then
			return "No conversations found."
		end if
		set AppleScript's text item delimiters to return
		set output to chatNames as string
		set AppleScript's text item delimiters to ""
		set output to "Recent conversations:" & return & output
		return output
	end tell`

	output, err := runOSAScript(script)
	if err != nil {
		// Messages automation may not be granted
		return nil, fmt.Errorf("failed to list conversations (ensure Messages automation is enabled in System Settings → Privacy → Automation): %w", err)
	}
	return textContent(output), nil
}

// ─── Notification Tools ───────────────────────────────────────

func (p *Provider) showNotification(args map[string]interface{}) (interface{}, error) {
	title, err := getStringRequired(args, "title")
	if err != nil {
		return nil, err
	}
	message, err := getStringRequired(args, "message")
	if err != nil {
		return nil, err
	}

	escapedTitle := escapeAppleScript(title)
	escapedMessage := escapeAppleScript(message)

	script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "default"`, escapedMessage, escapedTitle)

	_, err = runOSAScript(script)
	if err != nil {
		return nil, fmt.Errorf("failed to show notification: %w", err)
	}

	return textContent(fmt.Sprintf("Notification shown: %s — %s", title, message)), nil
}
