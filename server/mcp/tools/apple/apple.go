// Package apple provides MCP tools for Apple Reminders and Contacts
package apple

import (
	"fmt"
	"path/filepath"
	"runtime"

	tools "github.com/Emergent-Comapny/diane/mcp/tools"
)

// --- Tool Definition ---

// Tool represents an MCP tool definition
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

	if !tools.CommandExists("remindctl") {
		return fmt.Errorf("remindctl not found. Install with: brew install keith/formulae/remindctl")
	}

	if !tools.CommandExists("swift") {
		return fmt.Errorf("swift not found. Install Xcode Command Line Tools")
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
		{
			Name:        "apple_list_reminders",
			Description: "List Apple Reminders from a specific list or all lists",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"listName": tools.StringProperty("The name of the reminder list (optional, lists all if omitted)", false),
				},
				nil, // no required fields
			),
		},
		{
			Name:        "apple_add_reminder",
			Description: "Add a new Apple Reminder. Dates must be in YYYY-MM-DD HH:MM format.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"title":    tools.StringProperty("The title of the reminder", false),
					"listName": tools.StringProperty("The list to add the reminder to (optional)", false),
					"due":      tools.StringProperty("Due date/time in 'YYYY-MM-DD HH:MM' format (optional)", false),
				},
				[]string{"title"},
			),
		},
		{
			Name:        "apple_search_contacts",
			Description: "Search Apple Contacts by name, email, or phone number. Returns ID, name and email columns in tab-separated format.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"query": tools.StringProperty("Search query (name, email, or phone). Use empty string to list all contacts.", false),
				},
				[]string{"query"},
			),
		},
		{
			Name:        "apple_list_all_contacts",
			Description: "List all contacts in your Apple Contacts. Returns ID, name and email columns.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{},
				nil,
			),
		},
	}
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
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
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

// --- Reminders Tools ---

func (p *Provider) listReminders(args map[string]interface{}) (interface{}, error) {
	listName := tools.GetString(args, "listName")

	var output string
	var err error

	if listName != "" {
		output, err = tools.RunCommand("remindctl", "list", listName, "--json")
	} else {
		output, err = tools.RunCommand("remindctl", "list", "--json")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list reminders: %w", err)
	}

	if output == "" {
		output = "No reminders found."
	}

	return tools.TextContent(output), nil
}

func (p *Provider) addReminder(args map[string]interface{}) (interface{}, error) {
	title, err := tools.GetStringRequired(args, "title")
	if err != nil {
		return nil, err
	}

	listName := tools.GetString(args, "listName")
	due := tools.GetString(args, "due")

	// Build command arguments
	cmdArgs := []string{"add", title}

	if listName != "" {
		cmdArgs = append(cmdArgs, "--list", listName)
	}

	if due != "" {
		cmdArgs = append(cmdArgs, "--due", due)
	}

	output, err := tools.RunCommand("remindctl", cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to add reminder: %w", err)
	}

	return tools.TextContent(output), nil
}

// --- Contacts Tools ---

func (p *Provider) getSwiftScriptPath() string {
	if p.swiftScriptPath != "" {
		return p.swiftScriptPath
	}
	// Default path relative to the diane repo
	return filepath.Join(".diane", "tools", "lib", "search_all_contacts.swift")
}

func (p *Provider) searchContacts(args map[string]interface{}) (interface{}, error) {
	query := tools.GetString(args, "query")

	scriptPath := p.getSwiftScriptPath()
	output, err := tools.RunCommand("swift", scriptPath, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search contacts: %w", err)
	}

	if output == "" {
		output = "No contacts found."
	}

	return tools.TextContent(output), nil
}

func (p *Provider) listAllContacts(args map[string]interface{}) (interface{}, error) {
	scriptPath := p.getSwiftScriptPath()
	output, err := tools.RunCommand("swift", scriptPath, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list contacts: %w", err)
	}

	if output == "" {
		output = "No contacts found."
	}

	return tools.TextContent(output), nil
}
