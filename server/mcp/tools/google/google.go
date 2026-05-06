// Package google provides MCP tools for Google services (Gmail, Drive, Sheets, Calendar)
package google

import (
	"fmt"

	tools "github.com/Emergent-Comapny/diane/mcp/tools"
)

// --- Tool Definition ---

// Tool represents an MCP tool definition
type Tool = tools.Tool

// Provider implements ToolProvider for Google services
type Provider struct{}

// NewProvider creates a new Google tools provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "google"
}

// CheckDependencies verifies required binaries exist
func (p *Provider) CheckDependencies() error {
	if !tools.CommandExists("gog") {
		return fmt.Errorf("gog CLI not found. Install it to use Google tools")
	}
	return nil
}

// Tools returns all Google tools
func (p *Provider) Tools() []Tool {
	return []Tool{
		// Gmail tools
		{
			Name:        "google_search_emails",
			Description: "Search Gmail messages using Gmail search syntax. Returns a list of matched email threads.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"query":   tools.StringProperty("Gmail search query (e.g., 'from:alice', 'is:unread', 'label:inbox', 'subject:meeting')", false),
					"max":     tools.IntProperty("Maximum number of results to return (default: 10)", 10),
					"account": tools.StringProperty("Email account to search (optional, uses default if omitted)", false),
				},
				[]string{"query"},
			),
		},
		{
			Name:        "google_read_email",
			Description: "Get full content of a specific Gmail message by its ID.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"id":      tools.StringProperty("The message or thread ID to retrieve", false),
					"account": tools.StringProperty("Email account to use (optional, uses default if omitted)", false),
				},
				[]string{"id"},
			),
		},
		// Drive tools
		{
			Name:        "google_search_files",
			Description: "Search Google Drive for files and folders using query syntax. Returns file metadata including ID, name, mimeType, and shared status.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"query":   tools.StringProperty("Search query (e.g., 'name contains \"report\"', 'mimeType = \"application/vnd.google-apps.spreadsheet\"')", false),
					"max":     tools.IntProperty("Maximum number of results (default: 10)", 10),
					"account": tools.StringProperty("Google account email to use (optional)", false),
				},
				[]string{"query"},
			),
		},
		{
			Name:        "google_list_files",
			Description: "List recent files from Google Drive with optional filtering. Simpler than search for basic listing.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"max":     tools.IntProperty("Maximum number of results (default: 20)", 20),
					"account": tools.StringProperty("Google account email to use (optional)", false),
				},
				nil,
			),
		},
		// Sheets tools
		{
			Name:        "google_get_sheet",
			Description: "Get data from a Google Sheet range. Returns cell values in JSON format.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"sheetId": tools.StringProperty("The Google Sheets ID (from the URL)", false),
					"range":   tools.StringProperty("Range in A1 notation (e.g., 'Sheet1!A1:D10' or 'Tab!A:C')", false),
					"account": tools.StringProperty("Google account email to use (optional)", false),
				},
				[]string{"sheetId", "range"},
			),
		},
		{
			Name:        "google_update_sheet",
			Description: "Update data in a Google Sheet range. Overwrites existing values.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"sheetId": tools.StringProperty("The Google Sheets ID (from the URL)", false),
					"range":   tools.StringProperty("Range in A1 notation (e.g., 'Sheet1!A1:B2')", false),
					"values":  tools.StringProperty("JSON array of arrays with cell values (e.g., '[[\"A\",\"B\"],[\"1\",\"2\"]]')", false),
					"account": tools.StringProperty("Google account email to use (optional)", false),
				},
				[]string{"sheetId", "range", "values"},
			),
		},
		{
			Name:        "google_append_sheet",
			Description: "Append data to a Google Sheet. Adds new rows at the end of the range.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"sheetId": tools.StringProperty("The Google Sheets ID (from the URL)", false),
					"range":   tools.StringProperty("Range in A1 notation specifying columns (e.g., 'Sheet1!A:C')", false),
					"values":  tools.StringProperty("JSON array of arrays with row values (e.g., '[[\"x\",\"y\",\"z\"]]')", false),
					"account": tools.StringProperty("Google account email to use (optional)", false),
				},
				[]string{"sheetId", "range", "values"},
			),
		},
		{
			Name:        "google_clear_sheet",
			Description: "Clear data from a Google Sheet range without deleting the cells.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"sheetId": tools.StringProperty("The Google Sheets ID (from the URL)", false),
					"range":   tools.StringProperty("Range in A1 notation (e.g., 'Sheet1!A2:Z' or 'Tab!B5:D10')", false),
					"account": tools.StringProperty("Google account email to use (optional)", false),
				},
				[]string{"sheetId", "range"},
			),
		},
		{
			Name:        "google_get_sheet_metadata",
			Description: "Get metadata about a Google Sheet including sheet tabs, properties, and structure.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"sheetId": tools.StringProperty("The Google Sheets ID (from the URL)", false),
					"account": tools.StringProperty("Google account email to use (optional)", false),
				},
				[]string{"sheetId"},
			),
		},
		// Calendar tools
		{
			Name:        "google_list_calendars",
			Description: "List all calendars for a Google account",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"account": tools.StringProperty("Account email (optional - if omitted, uses primary account)", false),
				},
				nil,
			),
		},
		{
			Name:        "google_list_events",
			Description: "List events from a Google Calendar with flexible time filtering",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"calendar_id": tools.StringProperty("Calendar ID (use 'primary' for main calendar, or specific calendar ID)", false),
					"account":     tools.StringProperty("Account email (optional)", false),
					"from":        tools.StringProperty("Start time (RFC3339, date YYYY-MM-DD, or relative: today, tomorrow, monday)", false),
					"to":          tools.StringProperty("End time (RFC3339, date YYYY-MM-DD, or relative)", false),
					"today":       tools.BoolProperty("Show only today's events (timezone-aware)", false),
					"tomorrow":    tools.BoolProperty("Show only tomorrow's events (timezone-aware)", false),
					"week":        tools.BoolProperty("Show this week's events (Monday-Sunday)", false),
					"days":        tools.IntProperty("Show next N days of events (timezone-aware)", 0),
					"max":         tools.IntProperty("Maximum number of events to return (default: 10)", 10),
					"query":       tools.StringProperty("Free text search query to filter events", false),
					"all":         tools.BoolProperty("Fetch events from all calendars (not just one)", false),
				},
				nil,
			),
		},
		{
			Name:        "google_get_event",
			Description: "Get details of a specific calendar event",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"calendar_id": tools.StringProperty("Calendar ID (use 'primary' for main calendar)", false),
					"event_id":    tools.StringProperty("Event ID", false),
					"account":     tools.StringProperty("Account email (optional)", false),
				},
				[]string{"calendar_id", "event_id"},
			),
		},
		{
			Name:        "google_create_event",
			Description: "Create a new event in Google Calendar",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"calendar_id":  tools.StringProperty("Calendar ID (use 'primary' for main calendar)", false),
					"summary":      tools.StringProperty("Event title/summary", false),
					"from":         tools.StringProperty("Start time (RFC3339 format: 2026-02-04T15:30:00+01:00 or date for all-day: 2026-02-04)", false),
					"to":           tools.StringProperty("End time (RFC3339 format or date for all-day)", false),
					"account":      tools.StringProperty("Account email (optional)", false),
					"description":  tools.StringProperty("Event description", false),
					"location":     tools.StringProperty("Event location", false),
					"attendees":    tools.StringProperty("Comma-separated list of attendee emails", false),
					"all_day":      tools.BoolProperty("Is this an all-day event? (use date-only format in from/to)", false),
					"reminder":     tools.StringProperty("Reminder in format 'method:duration' (e.g., 'popup:30m', 'email:1d')", false),
					"with_meet":    tools.BoolProperty("Create a Google Meet video conference link", false),
					"visibility":   tools.StringProperty("Event visibility: default, public, private, confidential", false),
					"transparency": tools.StringProperty("Show as busy (opaque) or free (transparent). Use: busy or free", false),
				},
				[]string{"calendar_id", "summary", "from", "to"},
			),
		},
		{
			Name:        "google_update_event",
			Description: "Update an existing calendar event",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"calendar_id": tools.StringProperty("Calendar ID", false),
					"event_id":    tools.StringProperty("Event ID to update", false),
					"account":     tools.StringProperty("Account email (optional)", false),
					"summary":     tools.StringProperty("New event title/summary", false),
					"from":        tools.StringProperty("New start time (RFC3339 format)", false),
					"to":          tools.StringProperty("New end time (RFC3339 format)", false),
					"description": tools.StringProperty("New event description", false),
					"location":    tools.StringProperty("New event location", false),
				},
				[]string{"calendar_id", "event_id"},
			),
		},
		{
			Name:        "google_delete_event",
			Description: "Delete a calendar event",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"calendar_id": tools.StringProperty("Calendar ID", false),
					"event_id":    tools.StringProperty("Event ID to delete", false),
					"account":     tools.StringProperty("Account email (optional)", false),
				},
				[]string{"calendar_id", "event_id"},
			),
		},
		{
			Name:        "google_check_freebusy",
			Description: "Check free/busy status for one or more calendars",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"calendar_ids": tools.StringProperty("Comma-separated list of calendar IDs to check", false),
					"from":         tools.StringProperty("Start time (RFC3339 format)", false),
					"to":           tools.StringProperty("End time (RFC3339 format)", false),
					"account":      tools.StringProperty("Account email (optional)", false),
				},
				[]string{"calendar_ids", "from", "to"},
			),
		},
	}
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
	// Gmail
	case "google_search_emails":
		return p.searchEmails(args)
	case "google_read_email":
		return p.readEmail(args)
	// Drive
	case "google_search_files":
		return p.searchFiles(args)
	case "google_list_files":
		return p.listFiles(args)
	// Sheets
	case "google_get_sheet":
		return p.getSheet(args)
	case "google_update_sheet":
		return p.updateSheet(args)
	case "google_append_sheet":
		return p.appendSheet(args)
	case "google_clear_sheet":
		return p.clearSheet(args)
	case "google_get_sheet_metadata":
		return p.getSheetMetadata(args)
	// Calendar
	case "google_list_calendars":
		return p.listCalendars(args)
	case "google_list_events":
		return p.listEvents(args)
	case "google_get_event":
		return p.getEvent(args)
	case "google_create_event":
		return p.createEvent(args)
	case "google_update_event":
		return p.updateEvent(args)
	case "google_delete_event":
		return p.deleteEvent(args)
	case "google_check_freebusy":
		return p.checkFreebusy(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// --- Gmail Tools ---

func (p *Provider) searchEmails(args map[string]interface{}) (interface{}, error) {
	query, err := tools.GetStringRequired(args, "query")
	if err != nil {
		return nil, err
	}

	max := tools.GetInt(args, "max", 10)
	account := tools.GetString(args, "account")

	cmdArgs := []string{"gmail", "search", query, fmt.Sprintf("--max=%d", max), "--json"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to search emails: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) readEmail(args map[string]interface{}) (interface{}, error) {
	id, err := tools.GetStringRequired(args, "id")
	if err != nil {
		return nil, err
	}

	account := tools.GetString(args, "account")

	cmdArgs := []string{"gmail", "get", id, "--format=full", "--json"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to read email: %w", err)
	}

	return tools.TextContent(output), nil
}

// --- Drive Tools ---

func (p *Provider) searchFiles(args map[string]interface{}) (interface{}, error) {
	query, err := tools.GetStringRequired(args, "query")
	if err != nil {
		return nil, err
	}

	max := tools.GetInt(args, "max", 10)
	account := tools.GetString(args, "account")

	cmdArgs := []string{"drive", "search", query, fmt.Sprintf("--max=%d", max), "--json"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to search files: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) listFiles(args map[string]interface{}) (interface{}, error) {
	max := tools.GetInt(args, "max", 20)
	account := tools.GetString(args, "account")

	// List all files using a query that matches everything
	cmdArgs := []string{"drive", "search", "name contains ''", fmt.Sprintf("--max=%d", max), "--json"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	return tools.TextContent(output), nil
}

// --- Sheets Tools ---

func (p *Provider) getSheet(args map[string]interface{}) (interface{}, error) {
	sheetId, err := tools.GetStringRequired(args, "sheetId")
	if err != nil {
		return nil, err
	}
	rangeArg, err := tools.GetStringRequired(args, "range")
	if err != nil {
		return nil, err
	}

	account := tools.GetString(args, "account")

	cmdArgs := []string{"sheets", "get", sheetId, rangeArg, "--json"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to get sheet: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) updateSheet(args map[string]interface{}) (interface{}, error) {
	sheetId, err := tools.GetStringRequired(args, "sheetId")
	if err != nil {
		return nil, err
	}
	rangeArg, err := tools.GetStringRequired(args, "range")
	if err != nil {
		return nil, err
	}
	values, err := tools.GetStringRequired(args, "values")
	if err != nil {
		return nil, err
	}

	account := tools.GetString(args, "account")

	cmdArgs := []string{"sheets", "update", sheetId, rangeArg, fmt.Sprintf("--values-json=%s", values), "--input=USER_ENTERED"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to update sheet: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) appendSheet(args map[string]interface{}) (interface{}, error) {
	sheetId, err := tools.GetStringRequired(args, "sheetId")
	if err != nil {
		return nil, err
	}
	rangeArg, err := tools.GetStringRequired(args, "range")
	if err != nil {
		return nil, err
	}
	values, err := tools.GetStringRequired(args, "values")
	if err != nil {
		return nil, err
	}

	account := tools.GetString(args, "account")

	cmdArgs := []string{"sheets", "append", sheetId, rangeArg, fmt.Sprintf("--values-json=%s", values), "--insert=INSERT_ROWS"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to append to sheet: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) clearSheet(args map[string]interface{}) (interface{}, error) {
	sheetId, err := tools.GetStringRequired(args, "sheetId")
	if err != nil {
		return nil, err
	}
	rangeArg, err := tools.GetStringRequired(args, "range")
	if err != nil {
		return nil, err
	}

	account := tools.GetString(args, "account")

	cmdArgs := []string{"sheets", "clear", sheetId, rangeArg}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to clear sheet: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) getSheetMetadata(args map[string]interface{}) (interface{}, error) {
	sheetId, err := tools.GetStringRequired(args, "sheetId")
	if err != nil {
		return nil, err
	}

	account := tools.GetString(args, "account")

	cmdArgs := []string{"sheets", "metadata", sheetId, "--json"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to get sheet metadata: %w", err)
	}

	return tools.TextContent(output), nil
}

// --- Calendar Tools ---

func (p *Provider) listCalendars(args map[string]interface{}) (interface{}, error) {
	account := tools.GetString(args, "account")

	cmdArgs := []string{"calendar", "calendars", "--json"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to list calendars: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) listEvents(args map[string]interface{}) (interface{}, error) {
	cmdArgs := []string{"calendar", "events"}

	// Add calendar ID or --all flag
	if tools.GetBool(args, "all", false) {
		cmdArgs = append(cmdArgs, "--all")
	} else {
		calendarId := tools.GetString(args, "calendar_id")
		if calendarId == "" {
			calendarId = "primary"
		}
		cmdArgs = append(cmdArgs, calendarId)
	}

	// Add optional flags
	if account := tools.GetString(args, "account"); account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}
	if from := tools.GetString(args, "from"); from != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--from=%s", from))
	}
	if to := tools.GetString(args, "to"); to != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--to=%s", to))
	}
	if tools.GetBool(args, "today", false) {
		cmdArgs = append(cmdArgs, "--today")
	}
	if tools.GetBool(args, "tomorrow", false) {
		cmdArgs = append(cmdArgs, "--tomorrow")
	}
	if tools.GetBool(args, "week", false) {
		cmdArgs = append(cmdArgs, "--week")
	}
	if days := tools.GetInt(args, "days", 0); days > 0 {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--days=%d", days))
	}
	if max := tools.GetInt(args, "max", 0); max > 0 {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--max=%d", max))
	}
	if query := tools.GetString(args, "query"); query != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--query=%s", query))
	}

	cmdArgs = append(cmdArgs, "--json")

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) getEvent(args map[string]interface{}) (interface{}, error) {
	calendarId, err := tools.GetStringRequired(args, "calendar_id")
	if err != nil {
		return nil, err
	}
	eventId, err := tools.GetStringRequired(args, "event_id")
	if err != nil {
		return nil, err
	}

	account := tools.GetString(args, "account")

	cmdArgs := []string{"calendar", "event", calendarId, eventId, "--json"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to get event: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) createEvent(args map[string]interface{}) (interface{}, error) {
	calendarId, err := tools.GetStringRequired(args, "calendar_id")
	if err != nil {
		return nil, err
	}
	summary, err := tools.GetStringRequired(args, "summary")
	if err != nil {
		return nil, err
	}
	from, err := tools.GetStringRequired(args, "from")
	if err != nil {
		return nil, err
	}
	to, err := tools.GetStringRequired(args, "to")
	if err != nil {
		return nil, err
	}

	cmdArgs := []string{"calendar", "create", calendarId,
		fmt.Sprintf("--summary=%s", summary),
		fmt.Sprintf("--from=%s", from),
		fmt.Sprintf("--to=%s", to),
	}

	// Optional flags
	if account := tools.GetString(args, "account"); account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}
	if description := tools.GetString(args, "description"); description != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--description=%s", description))
	}
	if location := tools.GetString(args, "location"); location != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--location=%s", location))
	}
	if attendees := tools.GetString(args, "attendees"); attendees != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--attendees=%s", attendees))
	}
	if tools.GetBool(args, "all_day", false) {
		cmdArgs = append(cmdArgs, "--all-day")
	}
	if reminder := tools.GetString(args, "reminder"); reminder != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--reminder=%s", reminder))
	}
	if tools.GetBool(args, "with_meet", false) {
		cmdArgs = append(cmdArgs, "--with-meet")
	}
	if visibility := tools.GetString(args, "visibility"); visibility != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--visibility=%s", visibility))
	}
	if transparency := tools.GetString(args, "transparency"); transparency != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--transparency=%s", transparency))
	}

	cmdArgs = append(cmdArgs, "--json")

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to create event: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) updateEvent(args map[string]interface{}) (interface{}, error) {
	calendarId, err := tools.GetStringRequired(args, "calendar_id")
	if err != nil {
		return nil, err
	}
	eventId, err := tools.GetStringRequired(args, "event_id")
	if err != nil {
		return nil, err
	}

	cmdArgs := []string{"calendar", "update", calendarId, eventId}

	// Optional flags
	if account := tools.GetString(args, "account"); account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}
	if summary := tools.GetString(args, "summary"); summary != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--summary=%s", summary))
	}
	if from := tools.GetString(args, "from"); from != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--from=%s", from))
	}
	if to := tools.GetString(args, "to"); to != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--to=%s", to))
	}
	if description := tools.GetString(args, "description"); description != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--description=%s", description))
	}
	if location := tools.GetString(args, "location"); location != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--location=%s", location))
	}

	cmdArgs = append(cmdArgs, "--json")

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to update event: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) deleteEvent(args map[string]interface{}) (interface{}, error) {
	calendarId, err := tools.GetStringRequired(args, "calendar_id")
	if err != nil {
		return nil, err
	}
	eventId, err := tools.GetStringRequired(args, "event_id")
	if err != nil {
		return nil, err
	}

	account := tools.GetString(args, "account")

	cmdArgs := []string{"calendar", "delete", calendarId, eventId, "--force", "--json"}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to delete event: %w", err)
	}

	return tools.TextContent(output), nil
}

func (p *Provider) checkFreebusy(args map[string]interface{}) (interface{}, error) {
	calendarIds, err := tools.GetStringRequired(args, "calendar_ids")
	if err != nil {
		return nil, err
	}
	from, err := tools.GetStringRequired(args, "from")
	if err != nil {
		return nil, err
	}
	to, err := tools.GetStringRequired(args, "to")
	if err != nil {
		return nil, err
	}

	account := tools.GetString(args, "account")

	cmdArgs := []string{"calendar", "freebusy", calendarIds,
		fmt.Sprintf("--from=%s", from),
		fmt.Sprintf("--to=%s", to),
		"--json",
	}
	if account != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--account=%s", account))
	}

	output, err := tools.RunCommand("gog", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to check free/busy: %w", err)
	}

	return tools.TextContent(output), nil
}
