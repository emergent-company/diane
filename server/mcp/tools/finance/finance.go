// Package finance provides MCP tools for banking and budgeting (Enable Banking, Actual Budget)
package finance

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tools "github.com/Emergent-Comapny/diane/mcp/tools"
)

// --- Configuration ---

type enableBankingConfig struct {
	AppID       string `json:"app_id"`
	APIBaseURL  string `json:"api_base_url"`
	RedirectURL string `json:"redirect_url"`
}

type actualBudgetConfig struct {
	ServerURL string `json:"serverURL"`
	Password  string `json:"password"`
	DataDir   string `json:"dataDir"`
}

type accountMapping struct {
	Name              string `json:"name"`
	Bank              string `json:"bank"`
	BankAccountID     string `json:"bank_account_id"`
	BankAccountName   string `json:"bank_account_name"`
	BankIBAN          string `json:"bank_iban"`
	ActualBudgetID    string `json:"actual_budget_id"`
	ActualAccountID   string `json:"actual_account_id"`
	ActualAccountName string `json:"actual_account_name"`
	Enabled           bool   `json:"enabled"`
}

type syncSettings struct {
	DefaultDaysBack int  `json:"default_days_back"`
	AutoSyncEnabled bool `json:"auto_sync_enabled"`
	AutoCategorize  bool `json:"auto_categorize"`
	MarkAsCleared   bool `json:"mark_as_cleared"`
}

type bankMappingConfig struct {
	Mappings     []accountMapping `json:"mappings"`
	SyncSettings syncSettings     `json:"sync_settings"`
}

var (
	ebConfig      *enableBankingConfig
	ebPrivateKey  *rsa.PrivateKey
	actualCLIPath string
	secretsDir    string
)

// --- Enable Banking JWT Generation ---

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func generateJWT() (string, error) {
	if ebConfig == nil || ebPrivateKey == nil {
		return "", fmt.Errorf("Enable Banking not configured")
	}

	now := time.Now().Unix()
	exp := now + 3600 // 1 hour

	header := map[string]interface{}{
		"alg": "RS256",
		"typ": "JWT",
		"kid": ebConfig.AppID,
	}

	payload := map[string]interface{}{
		"iss": "enablebanking.com",
		"aud": "api.enablebanking.com",
		"iat": now,
		"exp": exp,
	}

	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(payload)

	signingInput := base64URLEncode(headerJSON) + "." + base64URLEncode(payloadJSON)

	// Sign with RS256
	hash := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, ebPrivateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return signingInput + "." + base64URLEncode(signature), nil
}

func makeEnableBankingRequest(endpoint, method string, body interface{}) (map[string]interface{}, error) {
	token, err := generateJWT()
	if err != nil {
		return nil, err
	}

	url := ebConfig.APIBaseURL + endpoint

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Some endpoints return arrays
		var arrResult []interface{}
		if err := json.Unmarshal(respBody, &arrResult); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		return map[string]interface{}{"data": arrResult}, nil
	}

	return result, nil
}

// --- Actual Budget CLI Wrapper ---

func runActualCLI(command string, args ...string) (interface{}, error) {
	if actualCLIPath == "" {
		return nil, fmt.Errorf("Actual Budget CLI not configured")
	}

	cmdArgs := append([]string{actualCLIPath, command}, args...)
	cmd := exec.Command("node", cmdArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("CLI failed: %v, stderr: %s", err, stderr.String())
	}

	output := stdout.String()
	if output == "" {
		return nil, fmt.Errorf("CLI produced no output, stderr: %s", stderr.String())
	}

	var result interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("failed to parse CLI output: %w", err)
	}

	// Check for error in result
	if m, ok := result.(map[string]interface{}); ok {
		if errMsg, hasErr := m["error"].(string); hasErr {
			return nil, fmt.Errorf("%s", errMsg)
		}
	}

	return result, nil
}

// --- Tool Definition ---

// Tool represents an MCP tool definition
type Tool = tools.Tool

// Provider implements ToolProvider for finance tools
type Provider struct {
	enableBankingAvailable bool
	actualBudgetAvailable  bool
	bankSyncAvailable      bool
}

// NewProvider creates a new finance tools provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "finance"
}

// CheckDependencies verifies required configurations exist
func (p *Provider) CheckDependencies() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	secretsDir = filepath.Join(home, ".diane", "secrets")

	// Check Enable Banking
	ebConfigPath := filepath.Join(secretsDir, "enablebanking-config.json")
	ebKeyPath := filepath.Join(secretsDir, "enablebanking-private.pem")

	if _, err := os.Stat(ebConfigPath); err == nil {
		if _, err := os.Stat(ebKeyPath); err == nil {
			// Load config
			data, err := os.ReadFile(ebConfigPath)
			if err == nil {
				var cfg enableBankingConfig
				if err := json.Unmarshal(data, &cfg); err == nil {
					ebConfig = &cfg

					// Load private key
					keyData, err := os.ReadFile(ebKeyPath)
					if err == nil {
						block, _ := pem.Decode(keyData)
						if block != nil {
							// Try PKCS8 first (BEGIN PRIVATE KEY)
							if privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
								if rsaKey, ok := privKey.(*rsa.PrivateKey); ok {
									ebPrivateKey = rsaKey
									p.enableBankingAvailable = true
								}
							} else {
								// Fall back to PKCS1 (BEGIN RSA PRIVATE KEY)
								if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
									ebPrivateKey = key
									p.enableBankingAvailable = true
								}
							}
						}
					}
				}
			}
		}
	}

	// Check Actual Budget CLI
	actualCLIPath = filepath.Join(home, ".diane", "tools", "actualbudget-cli.mjs")
	if _, err := os.Stat(actualCLIPath); err == nil {
		// Check if Node.js is available
		if _, err := exec.LookPath("node"); err == nil {
			p.actualBudgetAvailable = true
		}
	}

	// Bank sync requires both
	p.bankSyncAvailable = p.enableBankingAvailable && p.actualBudgetAvailable

	if !p.enableBankingAvailable && !p.actualBudgetAvailable {
		return fmt.Errorf("no finance services available (Enable Banking or Actual Budget)")
	}

	return nil
}

// Tools returns all finance tools
func (p *Provider) Tools() []Tool {
	var toolList []Tool

	// Enable Banking tools
	if p.enableBankingAvailable {
		toolList = append(toolList, []Tool{
			{
				Name:        "enablebanking_list_banks",
				Description: "List available banks (ASPSPs) in Enable Banking for a specific country. Returns bank names, countries, and supported services (AIS/PIS).",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"country": tools.StringProperty("Two-letter ISO 3166 country code (e.g., 'PL' for Poland, 'GB' for UK)", false),
					},
					[]string{"country"},
				),
			},
			{
				Name:        "enablebanking_start_authorization",
				Description: "Start the authorization flow to connect a bank account. Returns an authorization URL that the user must open in their browser to log in to their bank and grant access. After completing authentication, the user will be redirected with a 'code' parameter.",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"bank_name":    tools.StringProperty("Name of the bank as listed in list_banks (e.g., 'Revolut', 'mBank')", false),
						"bank_country": tools.StringProperty("Two-letter ISO 3166 country code for the bank", false),
						"psu_type":     tools.StringProperty("Type of account: 'personal' or 'business'. Default: 'personal'", false),
					},
					[]string{"bank_name", "bank_country"},
				),
			},
			{
				Name:        "enablebanking_create_session",
				Description: "Create a session using the authorization code received after the user completed bank login. Returns session_id and list of authorized accounts. The session remains valid for 180 days.",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"code": tools.StringProperty("Authorization code from the redirect URL (the 'code' parameter after user completes bank login)", false),
					},
					[]string{"code"},
				),
			},
			{
				Name:        "enablebanking_get_transactions",
				Description: "Fetch transaction history for a bank account. Returns transactions with dates, amounts, descriptions, and counterparty information. Supports date filtering and pagination.",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"account_id": tools.StringProperty("Account UUID from the session (obtained from create_session)", false),
						"date_from":  tools.StringProperty("Start date in YYYY-MM-DD format (default: 90 days ago)", false),
						"date_to":    tools.StringProperty("End date in YYYY-MM-DD format (default: today)", false),
					},
					[]string{"account_id"},
				),
			},
			{
				Name:        "enablebanking_get_balances",
				Description: "Fetch current balance information for a bank account. Returns available balance, booked balance, and other balance types depending on the bank.",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"account_id": tools.StringProperty("Account UUID from the session (obtained from create_session)", false),
					},
					[]string{"account_id"},
				),
			},
		}...)
	}

	// Actual Budget tools
	if p.actualBudgetAvailable {
		toolList = append(toolList, []Tool{
			{
				Name:        "actualbudget_list_budgets",
				Description: "List all budget files available on the Actual Budget server",
				InputSchema: tools.ObjectSchema(map[string]interface{}{}, nil),
			},
			{
				Name:        "actualbudget_get_accounts",
				Description: "Get all accounts from an Actual Budget file",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
					},
					[]string{"budget_id"},
				),
			},
			{
				Name:        "actualbudget_get_transactions",
				Description: "Get all transactions from an account within a date range",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id":  tools.StringProperty("The sync ID (groupId) of the budget file", false),
						"account_id": tools.StringProperty("The account ID to fetch transactions from", false),
						"start_date": tools.StringProperty("Start date in YYYY-MM-DD format", false),
						"end_date":   tools.StringProperty("End date in YYYY-MM-DD format", false),
					},
					[]string{"budget_id", "account_id", "start_date", "end_date"},
				),
			},
			{
				Name:        "actualbudget_import_transactions",
				Description: "Import bank transactions into Actual Budget with automatic reconciliation and rule processing",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id":    tools.StringProperty("The sync ID (groupId) of the budget file", false),
						"account_id":   tools.StringProperty("The account ID to import transactions into", false),
						"transactions": tools.StringProperty(`JSON array of transactions: [{"date": "YYYY-MM-DD", "amount": 123.45, "payee_name": "Store", "notes": "...", "imported_id": "unique-id", "cleared": true}]`, false),
					},
					[]string{"budget_id", "account_id", "transactions"},
				),
			},
			{
				Name:        "actualbudget_get_categories",
				Description: "Get all categories from an Actual Budget file",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
					},
					[]string{"budget_id"},
				),
			},
			{
				Name:        "actualbudget_get_category_groups",
				Description: "Get all category groups from an Actual Budget file",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
					},
					[]string{"budget_id"},
				),
			},
			{
				Name:        "actualbudget_create_category_group",
				Description: "Create a new category group in Actual Budget",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
						"group":     tools.StringProperty(`JSON group object: {"name": "Group Name", "is_income": false, "hidden": false}`, false),
					},
					[]string{"budget_id", "group"},
				),
			},
			{
				Name:        "actualbudget_create_category",
				Description: "Create a new category in Actual Budget within a specific group",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
						"category":  tools.StringProperty(`JSON category object: {"name": "Category Name", "group_id": "group-id"}`, false),
					},
					[]string{"budget_id", "category"},
				),
			},
			{
				Name:        "actualbudget_update_category",
				Description: "Update an existing category in Actual Budget",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id":   tools.StringProperty("The sync ID (groupId) of the budget file", false),
						"category_id": tools.StringProperty("The ID of the category to update", false),
						"fields":      tools.StringProperty(`JSON object with fields to update: {"name": "New Name", "hidden": true}`, false),
					},
					[]string{"budget_id", "category_id", "fields"},
				),
			},
			{
				Name:        "actualbudget_delete_category",
				Description: "Delete a category from Actual Budget (optionally transfer transactions to another category)",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id":            tools.StringProperty("The sync ID (groupId) of the budget file", false),
						"category_id":          tools.StringProperty("The ID of the category to delete", false),
						"transfer_category_id": tools.StringProperty("Optional ID of category to transfer existing transactions to", false),
					},
					[]string{"budget_id", "category_id"},
				),
			},
			{
				Name:        "actualbudget_get_payees",
				Description: "Get all payees from an Actual Budget file",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
					},
					[]string{"budget_id"},
				),
			},
			{
				Name:        "actualbudget_get_account_balance",
				Description: "Get the current balance of an account in Actual Budget",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id":   tools.StringProperty("The sync ID (groupId) of the budget file", false),
						"account_id":  tools.StringProperty("The account ID to check balance for", false),
						"cutoff_date": tools.StringProperty("Optional date (YYYY-MM-DD) to get balance as of that date", false),
					},
					[]string{"budget_id", "account_id"},
				),
			},
			{
				Name:        "actualbudget_sync_budget",
				Description: "Synchronize local budget changes with the Actual Budget server",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
					},
					[]string{"budget_id"},
				),
			},
			{
				Name:        "actualbudget_get_rules",
				Description: "Get all rules from an Actual Budget file",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
					},
					[]string{"budget_id"},
				),
			},
			{
				Name:        "actualbudget_create_rule",
				Description: "Create a new rule in Actual Budget for automatic transaction categorization",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
						"rule":      tools.StringProperty(`JSON rule object with conditions and actions`, false),
					},
					[]string{"budget_id", "rule"},
				),
			},
			{
				Name:        "actualbudget_update_rule",
				Description: "Update an existing rule in Actual Budget",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
						"rule":      tools.StringProperty(`JSON rule object with id field included`, false),
					},
					[]string{"budget_id", "rule"},
				),
			},
			{
				Name:        "actualbudget_delete_rule",
				Description: "Delete a rule from Actual Budget",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
						"rule_id":   tools.StringProperty("The ID of the rule to delete", false),
					},
					[]string{"budget_id", "rule_id"},
				),
			},
			{
				Name:        "actualbudget_run_rules",
				Description: "Run all rules on existing transactions in the budget to re-categorize them",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("The sync ID (groupId) of the budget file", false),
					},
					[]string{"budget_id"},
				),
			},
		}...)
	}

	// Bank Sync tools (requires both Enable Banking and Actual Budget)
	if p.bankSyncAvailable {
		toolList = append(toolList, []Tool{
			{
				Name:        "banksync_list_mappings",
				Description: "List all bank account to Actual Budget account mappings",
				InputSchema: tools.ObjectSchema(map[string]interface{}{}, nil),
			},
			{
				Name:        "banksync_update_mapping",
				Description: "Update a specific bank account mapping with Actual Budget account details",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"bank_account_id":     tools.StringProperty("The Enable Banking account ID to update mapping for", false),
						"actual_account_id":   tools.StringProperty("The Actual Budget account ID to map to", false),
						"actual_account_name": tools.StringProperty("The Actual Budget account name for reference", false),
						"enabled":             tools.BoolProperty("Enable this mapping for automatic sync", false),
					},
					[]string{"bank_account_id", "actual_account_id", "actual_account_name"},
				),
			},
			{
				Name:        "banksync_sync_bank_to_actual",
				Description: "Sync transactions from Enable Banking to Actual Budget for a specific account mapping",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"bank_account_id": tools.StringProperty("The Enable Banking account ID to sync (must have a configured mapping)", false),
						"days_back":       tools.IntProperty("Number of days back to fetch transactions (default: 30)", 0),
					},
					[]string{"bank_account_id"},
				),
			},
			{
				Name:        "banksync_sync_all_accounts",
				Description: "Sync transactions from all enabled bank accounts to Actual Budget",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"days_back": tools.IntProperty("Number of days back to fetch transactions (default: 30)", 0),
					},
					nil,
				),
			},
			{
				Name:        "banksync_setup_list_actual_accounts",
				Description: "Helper tool to list all Actual Budget accounts for easy mapping setup",
				InputSchema: tools.ObjectSchema(
					map[string]interface{}{
						"budget_id": tools.StringProperty("Budget ID (optional, uses default if omitted)", false),
					},
					nil,
				),
			},
		}...)
	}

	return toolList
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
	// Enable Banking tools
	case "enablebanking_list_banks":
		return p.ebListBanks(args)
	case "enablebanking_start_authorization":
		return p.ebStartAuth(args)
	case "enablebanking_create_session":
		return p.ebCreateSession(args)
	case "enablebanking_get_transactions":
		return p.ebGetTransactions(args)
	case "enablebanking_get_balances":
		return p.ebGetBalances(args)

	// Actual Budget tools
	case "actualbudget_list_budgets":
		return p.abListBudgets(args)
	case "actualbudget_get_accounts":
		return p.abGetAccounts(args)
	case "actualbudget_get_transactions":
		return p.abGetTransactions(args)
	case "actualbudget_import_transactions":
		return p.abImportTransactions(args)
	case "actualbudget_get_categories":
		return p.abGetCategories(args)
	case "actualbudget_get_category_groups":
		return p.abGetCategoryGroups(args)
	case "actualbudget_create_category_group":
		return p.abCreateCategoryGroup(args)
	case "actualbudget_create_category":
		return p.abCreateCategory(args)
	case "actualbudget_update_category":
		return p.abUpdateCategory(args)
	case "actualbudget_delete_category":
		return p.abDeleteCategory(args)
	case "actualbudget_get_payees":
		return p.abGetPayees(args)
	case "actualbudget_get_account_balance":
		return p.abGetAccountBalance(args)
	case "actualbudget_sync_budget":
		return p.abSyncBudget(args)
	case "actualbudget_get_rules":
		return p.abGetRules(args)
	case "actualbudget_create_rule":
		return p.abCreateRule(args)
	case "actualbudget_update_rule":
		return p.abUpdateRule(args)
	case "actualbudget_delete_rule":
		return p.abDeleteRule(args)
	case "actualbudget_run_rules":
		return p.abRunRules(args)

	// Bank Sync tools
	case "banksync_list_mappings":
		return p.bsListMappings(args)
	case "banksync_update_mapping":
		return p.bsUpdateMapping(args)
	case "banksync_sync_bank_to_actual":
		return p.bsSyncBankToActual(args)
	case "banksync_sync_all_accounts":
		return p.bsSyncAllAccounts(args)
	case "banksync_setup_list_actual_accounts":
		return p.bsSetupListActualAccounts(args)

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// --- Enable Banking Tool Implementations ---

func (p *Provider) ebListBanks(args map[string]interface{}) (interface{}, error) {
	country, err := tools.GetStringRequired(args, "country")
	if err != nil {
		return nil, err
	}

	result, err := makeEnableBankingRequest(fmt.Sprintf("/aspsps?country=%s", strings.ToUpper(country)), "GET", nil)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) ebStartAuth(args map[string]interface{}) (interface{}, error) {
	bankName, err := tools.GetStringRequired(args, "bank_name")
	if err != nil {
		return nil, err
	}
	bankCountry, err := tools.GetStringRequired(args, "bank_country")
	if err != nil {
		return nil, err
	}
	psuType := tools.GetString(args, "psu_type")
	if psuType == "" {
		psuType = "personal"
	}

	validUntil := time.Now().Add(180 * 24 * time.Hour).Format(time.RFC3339)

	body := map[string]interface{}{
		"access": map[string]interface{}{
			"valid_until": validUntil,
		},
		"aspsp": map[string]interface{}{
			"name":    bankName,
			"country": strings.ToUpper(bankCountry),
		},
		"state":        fmt.Sprintf("%d", time.Now().UnixNano()),
		"redirect_url": ebConfig.RedirectURL,
		"psu_type":     psuType,
	}

	result, err := makeEnableBankingRequest("/auth", "POST", body)
	if err != nil {
		return nil, err
	}

	response := map[string]interface{}{
		"message": "Authorization started successfully",
		"next_steps": []string{
			"1. Open the authorization URL in your browser",
			"2. Log in to your bank and grant access",
			fmt.Sprintf("3. After redirect to %s, copy the 'code' parameter from the URL", ebConfig.RedirectURL),
			"4. Use create_session with the code to complete authorization",
		},
		"authorization_url": result["url"],
		"authorization_id":  result["authorization_id"],
	}

	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) ebCreateSession(args map[string]interface{}) (interface{}, error) {
	code, err := tools.GetStringRequired(args, "code")
	if err != nil {
		return nil, err
	}

	result, err := makeEnableBankingRequest("/sessions", "POST", map[string]interface{}{
		"code": code,
	})
	if err != nil {
		return nil, err
	}

	// Save session to file
	sessionsPath := filepath.Join(secretsDir, "sessions.json")
	sessionsData, _ := os.ReadFile(sessionsPath)
	var sessions map[string]interface{}
	json.Unmarshal(sessionsData, &sessions)
	if sessions == nil {
		sessions = map[string]interface{}{"sessions": map[string]interface{}{}}
	}
	sessMap := sessions["sessions"].(map[string]interface{})
	aspsp := result["aspsp"].(map[string]interface{})
	key := fmt.Sprintf("%s_%s", aspsp["name"], aspsp["country"])
	sessMap[key] = result
	newData, _ := json.MarshalIndent(sessions, "", "  ")
	os.WriteFile(sessionsPath, newData, 0644)

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) ebGetTransactions(args map[string]interface{}) (interface{}, error) {
	accountID, err := tools.GetStringRequired(args, "account_id")
	if err != nil {
		return nil, err
	}

	dateFrom := tools.GetString(args, "date_from")
	dateTo := tools.GetString(args, "date_to")

	if dateFrom == "" {
		dateFrom = time.Now().AddDate(0, 0, -90).Format("2006-01-02")
	}
	if dateTo == "" {
		dateTo = time.Now().Format("2006-01-02")
	}

	endpoint := fmt.Sprintf("/accounts/%s/transactions?date_from=%s&date_to=%s", accountID, dateFrom, dateTo)
	result, err := makeEnableBankingRequest(endpoint, "GET", nil)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) ebGetBalances(args map[string]interface{}) (interface{}, error) {
	accountID, err := tools.GetStringRequired(args, "account_id")
	if err != nil {
		return nil, err
	}

	result, err := makeEnableBankingRequest(fmt.Sprintf("/accounts/%s/balances", accountID), "GET", nil)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

// --- Actual Budget Tool Implementations ---

func (p *Provider) abListBudgets(args map[string]interface{}) (interface{}, error) {
	result, err := runActualCLI("list-budgets")
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abGetAccounts(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("get-accounts", budgetID)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abGetTransactions(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}
	accountID, err := tools.GetStringRequired(args, "account_id")
	if err != nil {
		return nil, err
	}
	startDate, err := tools.GetStringRequired(args, "start_date")
	if err != nil {
		return nil, err
	}
	endDate, err := tools.GetStringRequired(args, "end_date")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("get-transactions", budgetID, accountID, startDate, endDate)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abImportTransactions(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}
	accountID, err := tools.GetStringRequired(args, "account_id")
	if err != nil {
		return nil, err
	}
	transactions, err := tools.GetStringRequired(args, "transactions")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("import-transactions", budgetID, accountID, transactions)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abGetCategories(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("get-categories", budgetID)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abGetCategoryGroups(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("get-category-groups", budgetID)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abCreateCategoryGroup(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}
	group, err := tools.GetStringRequired(args, "group")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("create-category-group", budgetID, group)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abCreateCategory(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}
	category, err := tools.GetStringRequired(args, "category")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("create-category", budgetID, category)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abUpdateCategory(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}
	categoryID, err := tools.GetStringRequired(args, "category_id")
	if err != nil {
		return nil, err
	}
	fields, err := tools.GetStringRequired(args, "fields")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("update-category", budgetID, categoryID, fields)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abDeleteCategory(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}
	categoryID, err := tools.GetStringRequired(args, "category_id")
	if err != nil {
		return nil, err
	}
	transferID := tools.GetString(args, "transfer_category_id")

	var result interface{}
	var err2 error
	if transferID != "" {
		result, err2 = runActualCLI("delete-category", budgetID, categoryID, transferID)
	} else {
		result, err2 = runActualCLI("delete-category", budgetID, categoryID)
	}
	if err2 != nil {
		return nil, err2
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abGetPayees(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("get-payees", budgetID)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abGetAccountBalance(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}
	accountID, err := tools.GetStringRequired(args, "account_id")
	if err != nil {
		return nil, err
	}
	cutoffDate := tools.GetString(args, "cutoff_date")

	var result interface{}
	var err2 error
	if cutoffDate != "" {
		result, err2 = runActualCLI("get-account-balance", budgetID, accountID, cutoffDate)
	} else {
		result, err2 = runActualCLI("get-account-balance", budgetID, accountID)
	}
	if err2 != nil {
		return nil, err2
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abSyncBudget(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("sync-budget", budgetID)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abGetRules(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("get-rules", budgetID)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abCreateRule(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}
	rule, err := tools.GetStringRequired(args, "rule")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("create-rule", budgetID, rule)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abUpdateRule(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}
	rule, err := tools.GetStringRequired(args, "rule")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("update-rule", budgetID, rule)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abDeleteRule(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}
	ruleID, err := tools.GetStringRequired(args, "rule_id")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("delete-rule", budgetID, ruleID)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) abRunRules(args map[string]interface{}) (interface{}, error) {
	budgetID, err := tools.GetStringRequired(args, "budget_id")
	if err != nil {
		return nil, err
	}

	result, err := runActualCLI("run-rules", budgetID)
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.TextContent(string(output)), nil
}

// --- Bank Sync Tool Implementations ---

func loadBankMappingConfig() (*bankMappingConfig, error) {
	configPath := filepath.Join(secretsDir, "bank-account-mapping.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read bank mapping config: %w", err)
	}

	var config bankMappingConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse bank mapping config: %w", err)
	}

	return &config, nil
}

func saveBankMappingConfig(config *bankMappingConfig) error {
	configPath := filepath.Join(secretsDir, "bank-account-mapping.json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return os.WriteFile(configPath, data, 0644)
}

func (p *Provider) bsListMappings(args map[string]interface{}) (interface{}, error) {
	config, err := loadBankMappingConfig()
	if err != nil {
		return nil, err
	}

	output, _ := json.MarshalIndent(config, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) bsUpdateMapping(args map[string]interface{}) (interface{}, error) {
	bankAccountID, err := tools.GetStringRequired(args, "bank_account_id")
	if err != nil {
		return nil, err
	}
	actualAccountID, err := tools.GetStringRequired(args, "actual_account_id")
	if err != nil {
		return nil, err
	}
	actualAccountName, err := tools.GetStringRequired(args, "actual_account_name")
	if err != nil {
		return nil, err
	}

	config, err := loadBankMappingConfig()
	if err != nil {
		return nil, err
	}

	var found *accountMapping
	for i := range config.Mappings {
		if config.Mappings[i].BankAccountID == bankAccountID {
			found = &config.Mappings[i]
			break
		}
	}

	if found == nil {
		return nil, fmt.Errorf("no mapping found for bank account ID: %s", bankAccountID)
	}

	found.ActualAccountID = actualAccountID
	found.ActualAccountName = actualAccountName
	found.Enabled = tools.GetBool(args, "enabled", found.Enabled)

	if err := saveBankMappingConfig(config); err != nil {
		return nil, err
	}

	response := map[string]interface{}{
		"message": "Mapping updated successfully",
		"mapping": found,
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) bsSyncBankToActual(args map[string]interface{}) (interface{}, error) {
	bankAccountID, err := tools.GetStringRequired(args, "bank_account_id")
	if err != nil {
		return nil, err
	}
	daysBack := int(tools.GetInt(args, "days_back", 30))

	config, err := loadBankMappingConfig()
	if err != nil {
		return nil, err
	}

	var mapping *accountMapping
	for i := range config.Mappings {
		if config.Mappings[i].BankAccountID == bankAccountID {
			mapping = &config.Mappings[i]
			break
		}
	}

	if mapping == nil {
		return nil, fmt.Errorf("no mapping found for bank account ID: %s", bankAccountID)
	}
	if !mapping.Enabled {
		return nil, fmt.Errorf("mapping is disabled for %s. Enable it first using update_mapping", mapping.Name)
	}
	if mapping.ActualAccountID == "TO_BE_FILLED" {
		return nil, fmt.Errorf("mapping not configured. Please set actual_account_id using update_mapping")
	}

	// Calculate date range
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -daysBack)
	dateFrom := startDate.Format("2006-01-02")
	dateTo := endDate.Format("2006-01-02")

	// Fetch transactions from Enable Banking
	endpoint := fmt.Sprintf("/accounts/%s/transactions?date_from=%s&date_to=%s", bankAccountID, dateFrom, dateTo)
	result, err := makeEnableBankingRequest(endpoint, "GET", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch bank transactions: %w", err)
	}

	transactions, ok := result["transactions"].([]interface{})
	if !ok || len(transactions) == 0 {
		response := map[string]interface{}{
			"message":        "No transactions to sync",
			"bank_account":   mapping.BankAccountName,
			"actual_account": mapping.ActualAccountName,
			"date_range":     map[string]string{"from": dateFrom, "to": dateTo},
		}
		output, _ := json.MarshalIndent(response, "", "  ")
		return tools.TextContent(string(output)), nil
	}

	// Transform to Actual Budget format
	var actualTxns []map[string]interface{}
	for _, t := range transactions {
		tx := t.(map[string]interface{})

		// Parse amount
		var amount float64
		if txAmount, ok := tx["transaction_amount"].(map[string]interface{}); ok {
			if amtStr, ok := txAmount["amount"].(string); ok {
				fmt.Sscanf(amtStr, "%f", &amount)
			} else if amtNum, ok := txAmount["amount"].(float64); ok {
				amount = amtNum
			}
		}

		// Handle credit/debit indicator
		indicator, _ := tx["credit_debit_indicator"].(string)
		if indicator == "CRDT" {
			amount = -amount // Income is negative in Actual
		}

		// Get payee name
		var payeeName string
		if creditor, ok := tx["creditor"].(map[string]interface{}); ok {
			payeeName, _ = creditor["name"].(string)
		} else if debtor, ok := tx["debtor"].(map[string]interface{}); ok {
			payeeName, _ = debtor["name"].(string)
		}
		if payeeName == "" {
			if remInfo, ok := tx["remittance_information"].([]interface{}); ok && len(remInfo) > 0 {
				payeeName, _ = remInfo[0].(string)
			}
		}
		if payeeName == "" {
			payeeName = "Unknown"
		}

		// Get date
		date, _ := tx["booking_date"].(string)
		if date == "" {
			date, _ = tx["value_date"].(string)
		}

		// Get transaction ID
		txID, _ := tx["transaction_id"].(string)
		if txID == "" {
			txID, _ = tx["entry_reference"].(string)
		}
		if txID == "" {
			txID = fmt.Sprintf("%s-%f-%s", date, amount, payeeName)
		}

		// Get status
		status, _ := tx["status"].(string)
		cleared := config.SyncSettings.MarkAsCleared && (status == "BOOK" || status == "BOOKED")

		actualTxns = append(actualTxns, map[string]interface{}{
			"date":        date,
			"amount":      amount,
			"payee_name":  payeeName,
			"notes":       tx["remittance_information_unstructured"],
			"imported_id": txID,
			"cleared":     cleared,
		})
	}

	// Import to Actual Budget
	txnsJSON, _ := json.Marshal(actualTxns)
	_, err = runActualCLI("import-transactions", mapping.ActualBudgetID, mapping.ActualAccountID, string(txnsJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to import transactions: %w", err)
	}

	response := map[string]interface{}{
		"message":        "Transactions synced successfully",
		"bank_account":   mapping.BankAccountName,
		"actual_account": mapping.ActualAccountName,
		"date_range":     map[string]string{"from": dateFrom, "to": dateTo},
		"fetched":        len(transactions),
		"imported":       len(actualTxns),
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) bsSyncAllAccounts(args map[string]interface{}) (interface{}, error) {
	daysBack := int(tools.GetInt(args, "days_back", 30))

	config, err := loadBankMappingConfig()
	if err != nil {
		return nil, err
	}

	var enabledMappings []accountMapping
	for _, m := range config.Mappings {
		if m.Enabled && m.ActualAccountID != "TO_BE_FILLED" {
			enabledMappings = append(enabledMappings, m)
		}
	}

	if len(enabledMappings) == 0 {
		response := map[string]interface{}{
			"message": "No enabled mappings found. Enable at least one mapping using update_mapping.",
		}
		output, _ := json.MarshalIndent(response, "", "  ")
		return tools.TextContent(string(output)), nil
	}

	var results []map[string]interface{}
	for _, m := range enabledMappings {
		result, err := p.bsSyncBankToActual(map[string]interface{}{
			"bank_account_id": m.BankAccountID,
			"days_back":       float64(daysBack),
		})
		if err != nil {
			results = append(results, map[string]interface{}{
				"mapping": m.Name,
				"success": false,
				"error":   err.Error(),
			})
		} else {
			results = append(results, map[string]interface{}{
				"mapping": m.Name,
				"success": true,
				"result":  result,
			})
		}
	}

	successCount := 0
	for _, r := range results {
		if r["success"].(bool) {
			successCount++
		}
	}

	response := map[string]interface{}{
		"message":        fmt.Sprintf("Sync completed: %d successful, %d failed", successCount, len(results)-successCount),
		"total_mappings": len(enabledMappings),
		"results":        results,
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}

func (p *Provider) bsSetupListActualAccounts(args map[string]interface{}) (interface{}, error) {
	budgetID := tools.GetString(args, "budget_id")
	if budgetID == "" {
		budgetID = "814f8b26-a186-4962-b2d8-7acab5b25c5b" // Default
	}

	result, err := runActualCLI("get-accounts", budgetID)
	if err != nil {
		return nil, err
	}

	response := map[string]interface{}{
		"message":   "Actual Budget accounts retrieved",
		"budget_id": budgetID,
		"accounts":  result,
	}
	output, _ := json.MarshalIndent(response, "", "  ")
	return tools.TextContent(string(output)), nil
}
