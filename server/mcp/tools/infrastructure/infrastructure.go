// Package infrastructure provides MCP tools for infrastructure management (Cloudflare DNS, etc.)
package infrastructure

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	tools "github.com/Emergent-Comapny/diane/mcp/tools"
)

// --- Configuration ---

type cloudflareConfig struct {
	APIToken string `json:"api_token"`
}

func getCloudflareConfig() (*cloudflareConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(home, ".diane", "secrets", "cloudflare-config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("Cloudflare config not found at %s. Please create it with your API token", configPath)
	}

	var config cloudflareConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse Cloudflare config: %w", err)
	}

	if config.APIToken == "" {
		return nil, fmt.Errorf("api_token not found in Cloudflare config")
	}

	return &config, nil
}

// --- Cloudflare API Client ---

type cloudflareResponse struct {
	Success bool            `json:"success"`
	Errors  []cfError       `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func cloudflareAPI(method, endpoint string, body interface{}) (json.RawMessage, error) {
	config, err := getCloudflareConfig()
	if err != nil {
		return nil, err
	}

	url := "https://api.cloudflare.com/client/v4" + endpoint

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var cfResp cloudflareResponse
	if err := json.Unmarshal(respBody, &cfResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !cfResp.Success {
		var errMsgs string
		for _, e := range cfResp.Errors {
			errMsgs += e.Message + "; "
		}
		return nil, fmt.Errorf("Cloudflare API error: %s", errMsgs)
	}

	return cfResp.Result, nil
}

// --- Zone helpers ---

type zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func getZoneID(zoneNameOrID string) (string, error) {
	result, err := cloudflareAPI("GET", "/zones", nil)
	if err != nil {
		return "", err
	}

	var zones []zone
	if err := json.Unmarshal(result, &zones); err != nil {
		return "", fmt.Errorf("failed to parse zones: %w", err)
	}

	for _, z := range zones {
		if z.Name == zoneNameOrID || z.ID == zoneNameOrID {
			return z.ID, nil
		}
	}

	return "", fmt.Errorf("zone not found: %s", zoneNameOrID)
}

// --- Tool Definition ---

// Tool represents an MCP tool definition
type Tool = tools.Tool

// Provider implements ToolProvider for infrastructure services
type Provider struct{}

// NewProvider creates a new infrastructure tools provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "infrastructure"
}

// CheckDependencies verifies Cloudflare config exists
func (p *Provider) CheckDependencies() error {
	_, err := getCloudflareConfig()
	return err
}

// Tools returns all infrastructure tools
func (p *Provider) Tools() []Tool {
	return []Tool{
		{
			Name:        "cloudflare_list_zones",
			Description: "List all domains (zones) in your Cloudflare account",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"status":   tools.StringProperty("Filter by status: active, pending, initializing, moved, deleted, deactivated", false),
					"page":     tools.IntProperty("Page number for pagination (default: 1)", 0),
					"per_page": tools.IntProperty("Results per page (default: 20, max: 50)", 0),
				},
				nil,
			),
		},
		{
			Name:        "cloudflare_get_zone",
			Description: "Get details for a specific domain (zone) by name or ID",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"identifier": tools.StringProperty("Zone name (e.g., 'example.com') or zone ID", false),
				},
				[]string{"identifier"},
			),
		},
		{
			Name:        "cloudflare_list_dns_records",
			Description: "List DNS records for a specific domain",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"zone":     tools.StringProperty("Zone name (e.g., 'example.com') or zone ID", false),
					"type":     tools.StringProperty("Filter by record type: A, AAAA, CNAME, MX, TXT, NS, SRV, CAA, etc.", false),
					"name":     tools.StringProperty("Filter by record name", false),
					"page":     tools.IntProperty("Page number for pagination", 0),
					"per_page": tools.IntProperty("Results per page (max: 100)", 0),
				},
				[]string{"zone"},
			),
		},
		{
			Name:        "cloudflare_create_dns_record",
			Description: "Create a new DNS record for a domain. Supports A, AAAA, CNAME, MX, TXT, NS, SRV, CAA, and more.",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"zone":     tools.StringProperty("Zone name (e.g., 'example.com') or zone ID", false),
					"type":     tools.StringProperty("Record type: A, AAAA, CNAME, MX, TXT, NS, SRV, CAA, etc.", false),
					"name":     tools.StringProperty("DNS record name (e.g., 'www', 'mail', '@' for root)", false),
					"content":  tools.StringProperty("Record content/value (IP for A/AAAA, hostname for CNAME/MX, text for TXT)", false),
					"ttl":      tools.IntProperty("Time to live in seconds (1 = automatic, 120-86400, default: 1)", 0),
					"priority": tools.IntProperty("Priority for MX/SRV records (required for MX, default: 10)", 0),
					"proxied":  tools.BoolProperty("Enable Cloudflare proxy (orange cloud) for A/AAAA/CNAME records", false),
				},
				[]string{"zone", "type", "name", "content"},
			),
		},
		{
			Name:        "cloudflare_update_dns_record",
			Description: "Update an existing DNS record",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"zone":      tools.StringProperty("Zone name (e.g., 'example.com') or zone ID", false),
					"record_id": tools.StringProperty("DNS record ID to update", false),
					"type":      tools.StringProperty("Record type: A, AAAA, CNAME, MX, TXT, etc.", false),
					"name":      tools.StringProperty("DNS record name", false),
					"content":   tools.StringProperty("Record content/value", false),
					"ttl":       tools.IntProperty("Time to live in seconds (1-86400)", 0),
					"priority":  tools.IntProperty("Priority for MX/SRV records", 0),
					"proxied":   tools.BoolProperty("Enable/disable Cloudflare proxy (orange cloud)", false),
				},
				[]string{"zone", "record_id"},
			),
		},
		{
			Name:        "cloudflare_delete_dns_record",
			Description: "Delete a DNS record. Use with caution!",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"zone":      tools.StringProperty("Zone name (e.g., 'example.com') or zone ID", false),
					"record_id": tools.StringProperty("DNS record ID to delete", false),
				},
				[]string{"zone", "record_id"},
			),
		},
		{
			Name:        "cloudflare_get_dns_record",
			Description: "Get details of a specific DNS record by ID",
			InputSchema: tools.ObjectSchema(
				map[string]interface{}{
					"zone":      tools.StringProperty("Zone name (e.g., 'example.com') or zone ID", false),
					"record_id": tools.StringProperty("DNS record ID", false),
				},
				[]string{"zone", "record_id"},
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
	case "cloudflare_list_zones":
		return p.listZones(args)
	case "cloudflare_get_zone":
		return p.getZone(args)
	case "cloudflare_list_dns_records":
		return p.listDNSRecords(args)
	case "cloudflare_create_dns_record":
		return p.createDNSRecord(args)
	case "cloudflare_update_dns_record":
		return p.updateDNSRecord(args)
	case "cloudflare_delete_dns_record":
		return p.deleteDNSRecord(args)
	case "cloudflare_get_dns_record":
		return p.getDNSRecord(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// --- Cloudflare Tool Implementations ---

func (p *Provider) listZones(args map[string]interface{}) (interface{}, error) {
	endpoint := "/zones"
	params := ""

	if status := tools.GetString(args, "status"); status != "" {
		params += fmt.Sprintf("&status=%s", status)
	}
	if page := tools.GetInt(args, "page", 0); page > 0 {
		params += fmt.Sprintf("&page=%d", page)
	}
	if perPage := tools.GetInt(args, "per_page", 0); perPage > 0 {
		params += fmt.Sprintf("&per_page=%d", perPage)
	}

	if params != "" {
		endpoint += "?" + params[1:] // Remove leading &
	}

	result, err := cloudflareAPI("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list zones: %w", err)
	}

	// Pretty print the result
	var zones interface{}
	json.Unmarshal(result, &zones)
	output, _ := json.MarshalIndent(zones, "", "  ")

	return tools.TextContent(string(output)), nil
}

func (p *Provider) getZone(args map[string]interface{}) (interface{}, error) {
	identifier, err := tools.GetStringRequired(args, "identifier")
	if err != nil {
		return nil, err
	}

	// Get all zones and find the matching one
	result, err := cloudflareAPI("GET", "/zones", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get zones: %w", err)
	}

	var zones []map[string]interface{}
	if err := json.Unmarshal(result, &zones); err != nil {
		return nil, fmt.Errorf("failed to parse zones: %w", err)
	}

	for _, z := range zones {
		if z["name"] == identifier || z["id"] == identifier {
			output, _ := json.MarshalIndent(z, "", "  ")
			return tools.TextContent(string(output)), nil
		}
	}

	return nil, fmt.Errorf("zone not found: %s", identifier)
}

func (p *Provider) listDNSRecords(args map[string]interface{}) (interface{}, error) {
	zoneName, err := tools.GetStringRequired(args, "zone")
	if err != nil {
		return nil, err
	}

	zoneID, err := getZoneID(zoneName)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("/zones/%s/dns_records", zoneID)
	params := ""

	if recordType := tools.GetString(args, "type"); recordType != "" {
		params += fmt.Sprintf("&type=%s", recordType)
	}
	if name := tools.GetString(args, "name"); name != "" {
		params += fmt.Sprintf("&name=%s", name)
	}
	if page := tools.GetInt(args, "page", 0); page > 0 {
		params += fmt.Sprintf("&page=%d", page)
	}
	if perPage := tools.GetInt(args, "per_page", 0); perPage > 0 {
		params += fmt.Sprintf("&per_page=%d", perPage)
	}

	if params != "" {
		endpoint += "?" + params[1:]
	}

	result, err := cloudflareAPI("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list DNS records: %w", err)
	}

	var records interface{}
	json.Unmarshal(result, &records)
	output, _ := json.MarshalIndent(records, "", "  ")

	return tools.TextContent(string(output)), nil
}

func (p *Provider) createDNSRecord(args map[string]interface{}) (interface{}, error) {
	zoneName, err := tools.GetStringRequired(args, "zone")
	if err != nil {
		return nil, err
	}
	recordType, err := tools.GetStringRequired(args, "type")
	if err != nil {
		return nil, err
	}
	name, err := tools.GetStringRequired(args, "name")
	if err != nil {
		return nil, err
	}
	content, err := tools.GetStringRequired(args, "content")
	if err != nil {
		return nil, err
	}

	zoneID, err := getZoneID(zoneName)
	if err != nil {
		return nil, err
	}

	recordData := map[string]interface{}{
		"type":    recordType,
		"name":    name,
		"content": content,
		"ttl":     tools.GetInt(args, "ttl", 1),
	}

	if priority := tools.GetInt(args, "priority", -1); priority >= 0 {
		recordData["priority"] = priority
	}

	if _, ok := args["proxied"]; ok {
		recordData["proxied"] = tools.GetBool(args, "proxied", false)
	}

	result, err := cloudflareAPI("POST", fmt.Sprintf("/zones/%s/dns_records", zoneID), recordData)
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS record: %w", err)
	}

	var record interface{}
	json.Unmarshal(result, &record)
	output, _ := json.MarshalIndent(record, "", "  ")

	return tools.TextContent(string(output)), nil
}

func (p *Provider) updateDNSRecord(args map[string]interface{}) (interface{}, error) {
	zoneName, err := tools.GetStringRequired(args, "zone")
	if err != nil {
		return nil, err
	}
	recordID, err := tools.GetStringRequired(args, "record_id")
	if err != nil {
		return nil, err
	}

	zoneID, err := getZoneID(zoneName)
	if err != nil {
		return nil, err
	}

	// Get current record to merge with updates
	currentResult, err := cloudflareAPI("GET", fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get current record: %w", err)
	}

	var currentRecord map[string]interface{}
	if err := json.Unmarshal(currentResult, &currentRecord); err != nil {
		return nil, fmt.Errorf("failed to parse current record: %w", err)
	}

	// Build update data with current values as defaults
	recordData := map[string]interface{}{
		"type":    currentRecord["type"],
		"name":    currentRecord["name"],
		"content": currentRecord["content"],
		"ttl":     currentRecord["ttl"],
	}

	// Override with provided values
	if recordType := tools.GetString(args, "type"); recordType != "" {
		recordData["type"] = recordType
	}
	if name := tools.GetString(args, "name"); name != "" {
		recordData["name"] = name
	}
	if content := tools.GetString(args, "content"); content != "" {
		recordData["content"] = content
	}
	if ttl := tools.GetInt(args, "ttl", -1); ttl >= 0 {
		recordData["ttl"] = ttl
	}
	if priority := tools.GetInt(args, "priority", -1); priority >= 0 {
		recordData["priority"] = priority
	} else if p, ok := currentRecord["priority"]; ok {
		recordData["priority"] = p
	}
	if _, ok := args["proxied"]; ok {
		recordData["proxied"] = tools.GetBool(args, "proxied", false)
	} else if p, ok := currentRecord["proxied"]; ok {
		recordData["proxied"] = p
	}

	result, err := cloudflareAPI("PUT", fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), recordData)
	if err != nil {
		return nil, fmt.Errorf("failed to update DNS record: %w", err)
	}

	var record interface{}
	json.Unmarshal(result, &record)
	output, _ := json.MarshalIndent(record, "", "  ")

	return tools.TextContent(string(output)), nil
}

func (p *Provider) deleteDNSRecord(args map[string]interface{}) (interface{}, error) {
	zoneName, err := tools.GetStringRequired(args, "zone")
	if err != nil {
		return nil, err
	}
	recordID, err := tools.GetStringRequired(args, "record_id")
	if err != nil {
		return nil, err
	}

	zoneID, err := getZoneID(zoneName)
	if err != nil {
		return nil, err
	}

	result, err := cloudflareAPI("DELETE", fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to delete DNS record: %w", err)
	}

	response := map[string]interface{}{
		"success": true,
		"message": "DNS record deleted successfully",
		"result":  string(result),
	}
	output, _ := json.MarshalIndent(response, "", "  ")

	return tools.TextContent(string(output)), nil
}

func (p *Provider) getDNSRecord(args map[string]interface{}) (interface{}, error) {
	zoneName, err := tools.GetStringRequired(args, "zone")
	if err != nil {
		return nil, err
	}
	recordID, err := tools.GetStringRequired(args, "record_id")
	if err != nil {
		return nil, err
	}

	zoneID, err := getZoneID(zoneName)
	if err != nil {
		return nil, err
	}

	result, err := cloudflareAPI("GET", fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get DNS record: %w", err)
	}

	var record interface{}
	json.Unmarshal(result, &record)
	output, _ := json.MarshalIndent(record, "", "  ")

	return tools.TextContent(string(output)), nil
}
