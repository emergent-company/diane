package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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

	CREATE TABLE IF NOT EXISTS discord_sessions (
		channel_id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL DEFAULT '',
		conversation TEXT NOT NULL DEFAULT '',
		agent_type TEXT NOT NULL DEFAULT 'default',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err := db.conn.Exec(schema)
	return err
}

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
