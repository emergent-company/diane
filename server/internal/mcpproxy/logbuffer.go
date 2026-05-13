package mcpproxy

import (
	"sync"
	"time"
)

// DefaultMaxLogLines is the max log entries kept per server.
const DefaultMaxLogLines = 500

// LogEntry is a single log line for an MCP server.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

// ServerLogBuffer holds recent log entries for a single server.
type ServerLogBuffer struct {
	mu    sync.Mutex
	lines []LogEntry
	max   int
}

// newServerLogBuffer creates a buffer that keeps up to max entries.
func newServerLogBuffer(max int) *ServerLogBuffer {
	if max <= 0 {
		max = DefaultMaxLogLines
	}
	return &ServerLogBuffer{max: max}
}

// Append adds a log entry. Thread-safe.
func (b *ServerLogBuffer) Append(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, LogEntry{Time: time.Now(), Message: msg})
	if len(b.lines) > b.max {
		// Drop oldest 25% to avoid O(n) sliding
		drop := b.max / 4
		b.lines = b.lines[drop:]
	}
}

// Lines returns a copy of all stored entries. Thread-safe.
func (b *ServerLogBuffer) Lines() []LogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]LogEntry, len(b.lines))
	copy(out, b.lines)
	return out
}

// LogBuffer is a collection of per-server log buffers.
type LogBuffer struct {
	mu      sync.Mutex
	buffers map[string]*ServerLogBuffer
	max     int
}

// NewLogBuffer creates a new LogBuffer.
func NewLogBuffer(maxLines int) *LogBuffer {
	if maxLines <= 0 {
		maxLines = DefaultMaxLogLines
	}
	return &LogBuffer{
		buffers: make(map[string]*ServerLogBuffer),
		max:     maxLines,
	}
}

// Append adds a log entry for the named server. Creates the server buffer
// on first use. Thread-safe.
func (lb *LogBuffer) Append(serverName, msg string) {
	lb.mu.Lock()
	sb, ok := lb.buffers[serverName]
	if !ok {
		sb = newServerLogBuffer(lb.max)
		lb.buffers[serverName] = sb
	}
	lb.mu.Unlock()
	sb.Append(msg)
}

// Lines returns a copy of all log entries for the given server. Returns
// an empty slice if the server has no entries yet. Thread-safe.
func (lb *LogBuffer) Lines(serverName string) []LogEntry {
	lb.mu.Lock()
	sb, ok := lb.buffers[serverName]
	lb.mu.Unlock()
	if !ok {
		return nil
	}
	return sb.Lines()
}
