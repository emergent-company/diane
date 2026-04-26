package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the SQLite database connection
type DB struct {
	conn *sql.DB
	path string
}

// Job represents a scheduled job in the database
type Job struct {
	ID        int64
	Name      string
	Command   string
	Schedule  string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// JobExecution represents a job execution log entry
type JobExecution struct {
	ID        int64
	JobID     int64
	StartedAt time.Time
	EndedAt   *time.Time
	ExitCode  *int
	Stdout    string
	Stderr    string
	Error     *string
}

// AgentDefinition represents an agent definition stored in SQLite.
// This is the single source of truth for all agents (built-in + user-defined).
type AgentDefinition struct {
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	SystemPrompt   string    `json:"system_prompt"`
	ToolsJSON      string    `json:"tools_json"`
	SkillsJSON     string    `json:"skills_json"`
	ModelConfigJSON string   `json:"model_config_json"`
	FlowType       string    `json:"flow_type"`
	Visibility     string    `json:"visibility"`
	MaxSteps       int       `json:"max_steps"`
	DefaultTimeout int       `json:"default_timeout"`
	TagsJSON       string    `json:"tags_json"`
	RoutingWeight  float64   `json:"routing_weight"`
	IsDefault      bool      `json:"is_default"`
	IsExperimental bool      `json:"is_experimental"`
	Status         string    `json:"status"`
	Source         string    `json:"source"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// AgentRunStat records a single agent run for A/B analytics.
type AgentRunStat struct {
	ID            int64     `json:"id"`
	AgentName     string    `json:"agent_name"`
	RunID         string    `json:"run_id"`
	SessionID     string    `json:"session_id"`
	DurationMs    int       `json:"duration_ms"`
	StepCount     int       `json:"step_count"`
	ToolCallCount int       `json:"tool_call_count"`
	InputTokens   int       `json:"input_tokens"`
	OutputTokens  int       `json:"output_tokens"`
	Status        string    `json:"status"`
	ErrorMessage  string    `json:"error_message"`
	CreatedAt     time.Time `json:"created_at"`
}

// New creates a new database connection
// If path is empty, uses ~/.diane/cron.db
func New(path string) (*DB, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dianeDir := filepath.Join(home, ".diane")
		if err := os.MkdirAll(dianeDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create .diane directory: %w", err)
		}
		path = filepath.Join(dianeDir, "cron.db")
	}

	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	db := &DB{conn: conn, path: path}

	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// migrate creates the database schema
func (db *DB) migrate() error {
	schema := `
	PRAGMA journal_mode=WAL;

	CREATE TABLE IF NOT EXISTS jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		command TEXT NOT NULL,
		schedule TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS job_executions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		job_id INTEGER NOT NULL,
		started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		ended_at DATETIME,
		exit_code INTEGER,
		stdout TEXT,
		stderr TEXT,
		error TEXT,
		FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_job_executions_job_id ON job_executions(job_id);
	CREATE INDEX IF NOT EXISTS idx_job_executions_started_at ON job_executions(started_at);

	CREATE TABLE IF NOT EXISTS agents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		type TEXT NOT NULL DEFAULT 'acp',
		url TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS webhooks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		agent_id INTEGER,
		path TEXT NOT NULL UNIQUE,
		prompt TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS discord_sessions (
		channel_id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL DEFAULT '',
		conversation TEXT NOT NULL DEFAULT '',
		agent_type TEXT NOT NULL DEFAULT 'default',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS agent_definitions (
		name TEXT PRIMARY KEY,
		description TEXT NOT NULL DEFAULT '',
		system_prompt TEXT NOT NULL DEFAULT '',
		tools_json TEXT NOT NULL DEFAULT '[]',
		skills_json TEXT NOT NULL DEFAULT '[]',
		model_config_json TEXT NOT NULL DEFAULT '',
		flow_type TEXT NOT NULL DEFAULT 'standard',
		visibility TEXT NOT NULL DEFAULT 'project',
		max_steps INTEGER NOT NULL DEFAULT 50,
		default_timeout INTEGER NOT NULL DEFAULT 300,
		tags_json TEXT NOT NULL DEFAULT '[]',
		routing_weight REAL NOT NULL DEFAULT 1.0,
		is_default INTEGER NOT NULL DEFAULT 0,
		is_experimental INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'active',
		source TEXT NOT NULL DEFAULT 'user-defined',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS agent_run_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		agent_name TEXT NOT NULL,
		run_id TEXT NOT NULL DEFAULT '',
		session_id TEXT NOT NULL DEFAULT '',
		duration_ms INTEGER NOT NULL DEFAULT 0,
		step_count INTEGER NOT NULL DEFAULT 0,
		tool_call_count INTEGER NOT NULL DEFAULT 0,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT '',
		error_message TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_agent_run_stats_agent ON agent_run_stats(agent_name);
	CREATE INDEX IF NOT EXISTS idx_agent_run_stats_created ON agent_run_stats(created_at);
	`

	_, err := db.conn.Exec(schema)
	return err
}

// ============================================================================
// Agent Definition CRUD
// ============================================================================

// UpsertAgentDefinition inserts or updates an agent definition.
func (db *DB) UpsertAgentDefinition(a *AgentDefinition) error {
	_, err := db.conn.Exec(`
		INSERT INTO agent_definitions (
			name, description, system_prompt, tools_json, skills_json,
			model_config_json, flow_type, visibility, max_steps, default_timeout,
			tags_json, routing_weight, is_default, is_experimental,
			status, source, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(name) DO UPDATE SET
			description = excluded.description,
			system_prompt = excluded.system_prompt,
			tools_json = excluded.tools_json,
			skills_json = excluded.skills_json,
			model_config_json = excluded.model_config_json,
			flow_type = excluded.flow_type,
			visibility = excluded.visibility,
			max_steps = excluded.max_steps,
			default_timeout = excluded.default_timeout,
			tags_json = excluded.tags_json,
			routing_weight = excluded.routing_weight,
			is_default = excluded.is_default,
			is_experimental = excluded.is_experimental,
			status = excluded.status,
			source = excluded.source,
			updated_at = CURRENT_TIMESTAMP
	`,
		a.Name, a.Description, a.SystemPrompt, a.ToolsJSON, a.SkillsJSON,
		a.ModelConfigJSON, a.FlowType, a.Visibility, a.MaxSteps, a.DefaultTimeout,
		a.TagsJSON, a.RoutingWeight, boolToInt(a.IsDefault), boolToInt(a.IsExperimental),
		a.Status, a.Source)
	return err
}

// GetAgentDefinition returns a single agent by name.
func (db *DB) GetAgentDefinition(name string) (*AgentDefinition, error) {
	a := &AgentDefinition{}
	var isDefault, isExp int
	err := db.conn.QueryRow(`
		SELECT name, description, system_prompt, tools_json, skills_json,
			model_config_json, flow_type, visibility, max_steps, default_timeout,
			tags_json, routing_weight, is_default, is_experimental,
			status, source, created_at, updated_at
		FROM agent_definitions WHERE name = ?
	`, name).Scan(
		&a.Name, &a.Description, &a.SystemPrompt, &a.ToolsJSON, &a.SkillsJSON,
		&a.ModelConfigJSON, &a.FlowType, &a.Visibility, &a.MaxSteps, &a.DefaultTimeout,
		&a.TagsJSON, &a.RoutingWeight, &isDefault, &isExp,
		&a.Status, &a.Source, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.IsDefault = isDefault != 0
	a.IsExperimental = isExp != 0
	return a, nil
}

// ListAgentDefinitions returns agents matching optional filters.
// Status "" returns all. Tags filters by tag containment (OR).
func (db *DB) ListAgentDefinitions(status string, tags []string) ([]*AgentDefinition, error) {
	where := []string{}
	args := []any{}

	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	for _, tag := range tags {
		where = append(where, "tags_json LIKE ?")
		args = append(args, `%`+tag+`%`)
	}

	query := "SELECT name, description, system_prompt, tools_json, skills_json, " +
		"model_config_json, flow_type, visibility, max_steps, default_timeout, " +
		"tags_json, routing_weight, is_default, is_experimental, " +
		"status, source, created_at, updated_at FROM agent_definitions"

	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY is_default DESC, routing_weight DESC, name ASC"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*AgentDefinition
	for rows.Next() {
		a := &AgentDefinition{}
		var isDefault, isExp int
		if err := rows.Scan(
			&a.Name, &a.Description, &a.SystemPrompt, &a.ToolsJSON, &a.SkillsJSON,
			&a.ModelConfigJSON, &a.FlowType, &a.Visibility, &a.MaxSteps, &a.DefaultTimeout,
			&a.TagsJSON, &a.RoutingWeight, &isDefault, &isExp,
			&a.Status, &a.Source, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.IsDefault = isDefault != 0
		a.IsExperimental = isExp != 0
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// DeleteAgentDefinition removes an agent definition by name.
// Built-in agents cannot be deleted (only soft-disabled).
func (db *DB) DeleteAgentDefinition(name string, source string) error {
	if source == "built-in" {
		_, err := db.conn.Exec(
			"UPDATE agent_definitions SET status = 'archived', updated_at = CURRENT_TIMESTAMP WHERE name = ?",
			name)
		return err
	}
	_, err := db.conn.Exec("DELETE FROM agent_definitions WHERE name = ?", name)
	return err
}

// GetDefaultAgent returns the first active agent marked as default.
func (db *DB) GetDefaultAgent() (*AgentDefinition, error) {
	agents, err := db.ListAgentDefinitions("active", nil)
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		if a.IsDefault {
			return a, nil
		}
	}
	// Fallback to first active agent
	if len(agents) > 0 {
		return agents[0], nil
	}
	return nil, nil
}

// EnsureDefaultAgent ensures at least one agent is marked as default.
// If none exists, marks the first active agent as default.
func (db *DB) EnsureDefaultAgent() error {
	def, err := db.GetDefaultAgent()
	if err != nil {
		return err
	}
	if def != nil {
		return nil
	}
	// Mark first active agent as default
	_, err = db.conn.Exec(`
		UPDATE agent_definitions SET is_default = 1, updated_at = CURRENT_TIMESTAMP
		WHERE name = (SELECT name FROM agent_definitions WHERE status = 'active' ORDER BY created_at ASC LIMIT 1)
	`)
	return err
}

// ============================================================================
// Agent Run Stats
// ============================================================================

// RecordRunStat records an agent run result for A/B analytics.
func (db *DB) RecordRunStat(s *AgentRunStat) error {
	_, err := db.conn.Exec(`
		INSERT INTO agent_run_stats (agent_name, run_id, session_id, duration_ms,
			step_count, tool_call_count, input_tokens, output_tokens, status, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.AgentName, s.RunID, s.SessionID, s.DurationMs,
		s.StepCount, s.ToolCallCount, s.InputTokens, s.OutputTokens,
		s.Status, s.ErrorMessage)
	return err
}

// GetAgentRunStats returns aggregated stats for an agent.
// If hours > 0, only returns stats from the last N hours.
func (db *DB) GetAgentRunStats(agentName string, hours int) ([]*AgentRunStat, error) {
	query := "SELECT id, agent_name, run_id, session_id, duration_ms, step_count, " +
		"tool_call_count, input_tokens, output_tokens, status, error_message, created_at " +
		"FROM agent_run_stats WHERE agent_name = ?"
	args := []any{agentName}

	if hours > 0 {
		query += " AND created_at >= datetime('now', ?)"
		args = append(args, fmt.Sprintf("-%d hours", hours))
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []*AgentRunStat
	for rows.Next() {
		s := &AgentRunStat{}
		if err := rows.Scan(&s.ID, &s.AgentName, &s.RunID, &s.SessionID,
			&s.DurationMs, &s.StepCount, &s.ToolCallCount,
			&s.InputTokens, &s.OutputTokens, &s.Status, &s.ErrorMessage, &s.CreatedAt); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// GetAgentStatsSummary returns aggregate metrics per agent for a time window.
type AgentStatsSummary struct {
	AgentName         string  `json:"agent_name"`
	TotalRuns         int     `json:"total_runs"`
	SuccessRuns       int     `json:"success_runs"`
	ErrorRuns         int     `json:"error_runs"`
	AvgDurationMs     float64 `json:"avg_duration_ms"`
	AvgStepCount      float64 `json:"avg_step_count"`
	AvgToolCalls      float64 `json:"avg_tool_calls"`
	AvgInputTokens    float64 `json:"avg_input_tokens"`
	AvgOutputTokens   float64 `json:"avg_output_tokens"`
	TotalDurationMs   int     `json:"total_duration_ms"`
	TotalInputTokens  int     `json:"total_input_tokens"`
	TotalOutputTokens int     `json:"total_output_tokens"`
}

// GetAgentStatsSummary returns aggregated stats per agent.
func (db *DB) GetAgentStatsSummary(hours int) ([]*AgentStatsSummary, error) {
	where := ""
	if hours > 0 {
		where = fmt.Sprintf(" WHERE created_at >= datetime('now', '-%d hours')", hours)
	}

	rows, err := db.conn.Query(`
		SELECT
			agent_name,
			COUNT(*) as total_runs,
			SUM(CASE WHEN status IN ('success','completed') THEN 1 ELSE 0 END) as success_runs,
			SUM(CASE WHEN status IN ('error','failed') THEN 1 ELSE 0 END) as error_runs,
			AVG(duration_ms) as avg_duration_ms,
			AVG(step_count) as avg_step_count,
			AVG(tool_call_count) as avg_tool_calls,
			AVG(input_tokens) as avg_input_tokens,
			AVG(output_tokens) as avg_output_tokens,
			SUM(duration_ms) as total_duration_ms,
			SUM(input_tokens) as total_input_tokens,
			SUM(output_tokens) as total_output_tokens
		FROM agent_run_stats` + where + `
		GROUP BY agent_name
		ORDER BY total_runs DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []*AgentStatsSummary
	for rows.Next() {
		s := &AgentStatsSummary{}
		if err := rows.Scan(&s.AgentName, &s.TotalRuns, &s.SuccessRuns, &s.ErrorRuns,
			&s.AvgDurationMs, &s.AvgStepCount, &s.AvgToolCalls,
			&s.AvgInputTokens, &s.AvgOutputTokens,
			&s.TotalDurationMs, &s.TotalInputTokens, &s.TotalOutputTokens); err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// ============================================================================
// Agent Routing (Weighted Selection)
// ============================================================================

// SelectAgentByWeight picks an agent based on routing_weight distribution.
// Higher weight = higher probability of being selected.
func (db *DB) SelectAgentByWeight(tags []string) (*AgentDefinition, error) {
	agents, err := db.ListAgentDefinitions("active", tags)
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		// Fallback to any active agent
		agents, err = db.ListAgentDefinitions("active", nil)
		if err != nil {
			return nil, err
		}
	}
	if len(agents) == 0 {
		return nil, nil
	}
	if len(agents) == 1 {
		return agents[0], nil
	}

	// Collect weights
	var weights []float64
	var totalWeight float64
	for _, a := range agents {
		w := a.RoutingWeight
		if w <= 0 {
			w = 0.01 // minimum chance
		}
		weights = append(weights, w)
		totalWeight += w
	}

	// Random weighted selection
	target := float64(time.Now().UnixNano()%int64(totalWeight*1000)) / 1000.0
	var cumulative float64
	for i, w := range weights {
		cumulative += w
		if target < cumulative {
			return agents[i], nil
		}
	}
	return agents[len(agents)-1], nil
}

// ============================================================================
// Discord Sessions
// ============================================================================

// DiscordSession represents a persisted Discord channel→session mapping.
type DiscordSession struct {
	ChannelID    string    `json:"channel_id"`
	SessionID    string    `json:"session_id"`
	Conversation string    `json:"conversation"`
	AgentType    string    `json:"agent_type"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UpsertDiscordSession inserts or updates a channel→session mapping.
func (db *DB) UpsertDiscordSession(s *DiscordSession) error {
	_, err := db.conn.Exec(`
		INSERT INTO discord_sessions (channel_id, session_id, conversation, agent_type, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(channel_id) DO UPDATE SET
			session_id = excluded.session_id,
			conversation = excluded.conversation,
			agent_type = excluded.agent_type,
			updated_at = CURRENT_TIMESTAMP
	`, s.ChannelID, s.SessionID, s.Conversation, s.AgentType)
	return err
}

// GetDiscordSessionByChannel returns a single session by channel ID.
func (db *DB) GetDiscordSessionByChannel(channelID string) (*DiscordSession, error) {
	s := &DiscordSession{}
	err := db.conn.QueryRow(`
		SELECT channel_id, session_id, conversation, agent_type, created_at, updated_at
		FROM discord_sessions WHERE channel_id = ?
	`, channelID).Scan(&s.ChannelID, &s.SessionID, &s.Conversation, &s.AgentType, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// GetAllDiscordSessions returns all persisted discord sessions.
func (db *DB) GetAllDiscordSessions() ([]*DiscordSession, error) {
	rows, err := db.conn.Query(`
		SELECT channel_id, session_id, conversation, agent_type, created_at, updated_at
		FROM discord_sessions ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*DiscordSession
	for rows.Next() {
		s := &DiscordSession{}
		if err := rows.Scan(&s.ChannelID, &s.SessionID, &s.Conversation, &s.AgentType, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// ============================================================================
// Helpers
// ============================================================================

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ToolsFromJSON parses a JSON string into a string slice.
func ToolsFromJSON(s string) ([]string, error) {
	if s == "" || s == "[]" {
		return []string{}, nil
	}
	var tools []string
	if err := json.Unmarshal([]byte(s), &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

// TagsFromJSON parses a JSON string into a string slice.
func TagsFromJSON(s string) ([]string, error) {
	if s == "" || s == "[]" {
		return []string{}, nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(s), &tags); err != nil {
		return nil, err
	}
	return tags, nil
}
