// Package memory provides a bridge between Diane and the Memory Platform.
//
// It wraps the emergent.memory SDK to handle:
//   - Session lifecycle (create, retrieve, close)
//   - Message persistence (append, list)
//   - Semantic memory search across sessions and facts
//   - Streaming chat via the Memory Platform's LLM
//
// Architecture: Diane calls Memory Platform over outbound HTTP.
// No inbound connectivity is required for these operations.
package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	sdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/chat"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	sdkprovider "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/provider"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

// bridgeHTTPClient is a shared HTTP client with a 15-second timeout.
var bridgeHTTPClient = &http.Client{Timeout: 15 * time.Second}

// Bridge is the main interface to the Memory Platform.
// Each Bridge is scoped to a single Memory project.
type Bridge struct {
	client    *sdk.Client
	serverURL string
	apiKey    string
	projectID string
}

// Session represents a conversation session stored in the graph.
type Session struct {
	ID           string
	Key          string
	Title        string
	MessageCount int
	TotalTokens  int // auto-maintained by server when messages have token_count
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Message represents a single turn in a session.
type Message struct {
	ID               string
	Role             string
	Content          string
	Seq              int
	TokenCount       int        // 0 if unknown; populated when stored with token counting
	ToolCalls        []ToolCall // tool calls made by the assistant (if any)
	ReasoningContent string     // thinking/reasoning content (e.g. from DeepSeek)
	CreatedAt        time.Time  // when this message was created
}

// ToolCall represents a single tool invocation embedded in an assistant message.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON string of arguments
}

// SearchResult is a single match from memory recall.
type SearchResult struct {
	ObjectType string
	Content    string
	Score      float64
	ObjectID   string
}

// Config holds configuration for creating a Bridge.
type Config struct {
	ServerURL string
	APIKey    string
	ProjectID string
	OrgID     string
	// HTTPClientTimeout overrides the default 30s HTTP client timeout.
	// Use a longer timeout (e.g., 120s) when making streaming chat calls.
	HTTPClientTimeout time.Duration
}

// New creates a Bridge with explicit config.
func New(cfg Config) (*Bridge, error) {
	httpTimeout := cfg.HTTPClientTimeout
	if httpTimeout <= 0 {
		httpTimeout = 30 * time.Second
	}
	client, err := sdk.New(sdk.Config{
		ServerURL: cfg.ServerURL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: cfg.APIKey},
		HTTPClient: &http.Client{
			Timeout: httpTimeout,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("memory bridge: sdk.New: %w", err)
	}
	client.SetContext(cfg.OrgID, cfg.ProjectID)
	return &Bridge{client: client, serverURL: cfg.ServerURL, apiKey: cfg.APIKey, projectID: cfg.ProjectID}, nil
}

// Client returns the raw SDK client for advanced operations.
func (b *Bridge) Client() *sdk.Client {
	return b.client
}

// RespondToAgentQuestion submits a response to a pending agent question
// and triggers the agent resume. Returns the updated question object.
func (b *Bridge) RespondToAgentQuestion(ctx context.Context, questionID, response string) (*sdkagentrun.AgentQuestion, error) {
	req := &sdkagentrun.RespondToQuestionRequest{
		Response: response,
	}
	resp, err := b.client.Agents.RespondToQuestion(ctx, b.projectID, questionID, req)
	if err != nil {
		return nil, fmt.Errorf("respond to question: %w", err)
	}
	return &resp.Data, nil
}

// Close releases idle connections.
func (b *Bridge) Close() {
	if b.client != nil {
		b.client.Close()
	}
}

// ============================================================================
// Session Lifecycle
// ============================================================================

// CreateSession creates a new conversation session in the graph.
func (b *Bridge) CreateSession(ctx context.Context, title string) (*Session, error) {
	obj, err := b.client.Graph.CreateSession(ctx, &graph.CreateSessionRequest{
		Title: title,
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return graphObjectToSession(obj), nil
}

// GetSession retrieves a session by its graph object ID.
func (b *Bridge) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	obj, err := b.client.Graph.GetObject(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session %s: %w", sessionID, err)
	}
	return graphObjectToSession(obj), nil
}

// CloseSession marks a session as completed.
func (b *Bridge) CloseSession(ctx context.Context, sessionID string) error {
	_, err := b.client.Graph.UpdateObject(ctx, sessionID, &graph.UpdateObjectRequest{
		Properties: map[string]any{
			"status":   "completed",
			"ended_at": time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return fmt.Errorf("close session %s: %w", sessionID, err)
	}
	return nil
}

// ListSessions lists all sessions, optionally filtered by status.
func (b *Bridge) ListSessions(ctx context.Context, status string) ([]Session, error) {
	opts := &graph.ListObjectsOptions{
		Type: "Session",
	}
	if status != "" {
		opts.PropertyFilters = []graph.PropertyFilter{
			{Path: "status", Op: "eq", Value: status},
		}
	}
	resp, err := b.client.Graph.ListObjects(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	sessions := make([]Session, 0, len(resp.Items))
	for _, obj := range resp.Items {
		sessions = append(sessions, *graphObjectToSession(obj))
	}
	return sessions, nil
}

// ============================================================================
// Messages
// ============================================================================

// AppendMessage appends a message to a session and returns the created message.
// If tokenCount > 0, it's included in the request so the server can auto-maintain
// the session's total_tokens counter. Pass 0 to skip token counting.
func (b *Bridge) AppendMessage(ctx context.Context, sessionID, role, content string, tokenCount int) (*Message, error) {
	req := &graph.AppendMessageRequest{
		Role:    role,
		Content: content,
	}
	if tokenCount > 0 {
		req.TokenCount = &tokenCount
	}
	obj, err := b.client.Graph.AppendMessage(ctx, sessionID, req)
	if err != nil {
		return nil, fmt.Errorf("append message: %w", err)
	}
	return graphObjectToMessage(obj), nil
}

// GetMessages retrieves all messages for a session, ordered by sequence number.
func (b *Bridge) GetMessages(ctx context.Context, sessionID string) ([]Message, error) {
	var all []Message
	cursor := ""
	for {
		resp, err := b.client.Graph.ListMessages(ctx, sessionID, 100, cursor)
		if err != nil {
			return nil, fmt.Errorf("list messages: %w", err)
		}
		for _, obj := range resp.Items {
			all = append(all, *graphObjectToMessage(obj))
		}
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}

// ============================================================================
// Memory Recall — Hybrid Search across stored content
// ============================================================================

// SearchMemory performs hybrid (semantic + keyword) search across graph objects.
// Returns matched objects ranked by relevance.
func (b *Bridge) SearchMemory(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	resp, err := b.client.Graph.HybridSearch(ctx, &graph.HybridSearchRequest{
		Query: query,
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search memory: %w", err)
	}
	out := make([]SearchResult, 0, len(resp.Data))
	for _, r := range resp.Data {
		content := extractContent(r.Object)
		out = append(out, SearchResult{
			ObjectType: r.Object.Type,
			Content:    content,
			Score:      float64(r.Score),
			ObjectID:   r.Object.EntityID,
		})
	}
	return out, nil
}

// extractContent pulls the best "content" field from a graph object's properties.
func extractContent(obj *graph.GraphObject) string {
	if obj == nil || obj.Properties == nil {
		return ""
	}
	// Try content, then description, then title
	for _, key := range []string{"content", "description", "title", "summary", "name"} {
		if v, ok := obj.Properties[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// ============================================================================
// Streaming Chat (via Memory Platform's LLM)
// ============================================================================

// StreamChat starts a streaming chat session with the Memory Platform's LLM.
// If conversationID is empty, a new conversation is created.
// Caller must call Close() on the returned stream.
func (b *Bridge) StreamChat(ctx context.Context, message string, conversationID string) (*ChatStream, error) {
	req := &chat.StreamRequest{
		Message: message,
	}
	if conversationID != "" {
		req.ConversationID = &conversationID
	}
	stream, err := b.client.Chat.StreamChat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("stream chat: %w", err)
	}
	return &ChatStream{stream: stream}, nil
}

// ChatStream is an active SSE stream from the Memory Platform's chat endpoint.
type ChatStream struct {
	stream *chat.Stream
}

// Events returns a channel of stream events. Read from it until it closes.
func (cs *ChatStream) Events() <-chan *chat.StreamEvent {
	return cs.stream.Events()
}

// Close terminates the stream.
func (cs *ChatStream) Close() error {
	return cs.stream.Close()
}

// ============================================================================
// LLM Provider Proxy — delegates to Memory Platform's provider API
// ============================================================================

// ListOrgProviders returns all provider configs configured at the org level.
func (b *Bridge) ListOrgProviders(ctx context.Context, orgID string) ([]sdkprovider.ProviderConfig, error) {
	return b.client.Provider.ListOrgConfigs(ctx, orgID)
}

// UpsertOrgProvider creates or updates an org-level provider config with credentials.
// Runs a live credential test and syncs model catalog on success.
func (b *Bridge) UpsertOrgProvider(ctx context.Context, orgID, providerType string, apiKey, model, baseURL string) (*sdkprovider.ProviderConfig, error) {
	req := &sdkprovider.UpsertProviderConfigRequest{
		APIKey:          apiKey,
		GenerativeModel: model,
		BaseURL:         baseURL,
	}
	return b.client.Provider.UpsertOrgConfig(ctx, orgID, providerType, req)
}

// UpsertProjectProvider creates or updates a project-level provider config with credentials.
func (b *Bridge) UpsertProjectProvider(ctx context.Context, projectID, providerType string, apiKey, model, baseURL string) (*sdkprovider.ProviderConfig, error) {
	req := &sdkprovider.UpsertProviderConfigRequest{
		APIKey:          apiKey,
		GenerativeModel: model,
		BaseURL:         baseURL,
	}
	return b.client.Provider.UpsertProjectConfig(ctx, projectID, providerType, req)
}

// TestProvider sends a live generation call to verify provider credentials work.
// Uses the bridge's configured project ID. orgID is optional (pass "" for project-level test).
func (b *Bridge) TestProvider(ctx context.Context, orgID, providerType string) (*sdkprovider.TestProviderResponse, error) {
	return b.client.Provider.TestProvider(ctx, providerType, b.projectID, orgID)
}

// ============================================================================
// Agent Definition Proxy — delegates to Memory Platform's AgentDefinitions API
// ============================================================================

// ListAgentDefs returns all agent definitions for the current project.
func (b *Bridge) ListAgentDefs(ctx context.Context) (*sdkagents.APIResponse[[]sdkagents.AgentDefinitionSummary], error) {
	return b.client.AgentDefinitions.List(ctx)
}

// GetAgentDef returns a single agent definition by ID.
func (b *Bridge) GetAgentDef(ctx context.Context, id string) (*sdkagents.APIResponse[sdkagents.AgentDefinition], error) {
	return b.client.AgentDefinitions.Get(ctx, id)
}

// CreateAgentDef creates a new agent definition.
func (b *Bridge) CreateAgentDef(ctx context.Context, req *sdkagents.CreateAgentDefinitionRequest) (*sdkagents.APIResponse[sdkagents.AgentDefinition], error) {
	return b.client.AgentDefinitions.Create(ctx, req)
}

// UpdateAgentDef updates an existing agent definition.
func (b *Bridge) UpdateAgentDef(ctx context.Context, id string, req *sdkagents.UpdateAgentDefinitionRequest) (*sdkagents.APIResponse[sdkagents.AgentDefinition], error) {
	return b.client.AgentDefinitions.Update(ctx, id, req)
}

// DeleteAgentDef deletes an agent definition.
func (b *Bridge) DeleteAgentDef(ctx context.Context, id string) error {
	return b.client.AgentDefinitions.Delete(ctx, id)
}

// SetAgentWorkspaceConfig configures sandbox settings for an agent definition.
func (b *Bridge) SetAgentWorkspaceConfig(ctx context.Context, defID string, config map[string]any) (*sdkagents.APIResponse[map[string]any], error) {
	return b.client.AgentDefinitions.SetWorkspaceConfig(ctx, defID, config)
}

// ============================================================================
// Agent Runtime — delegates to Memory Platform's Agents API
// ============================================================================

// CreateRuntimeAgent creates a runtime agent linked to an agent definition.
// The agent is named identically to the definition for exact-name resolution.
func (b *Bridge) CreateRuntimeAgent(ctx context.Context, name, defID string) (*sdkagentrun.APIResponse[sdkagentrun.Agent], error) {
	return b.client.Agents.Create(ctx, &sdkagentrun.CreateAgentRequest{
		Name:          name,
		StrategyType:  "chat-session:" + defID,
		CronSchedule:  "0 0 29 2 *", // Feb 29 — never fires except leap years at 00:00
		TriggerType:   "manual",
		ExecutionMode: "execute",
		Enabled:       boolPtr(true),
	})
}

// CreateScheduledRuntimeAgent creates a runtime agent with a cron schedule.
// The agent will auto-trigger on the cron schedule without manual intervention.
// Use "" for triggerPrompt to use the agent's default startup prompt.
func (b *Bridge) CreateScheduledRuntimeAgent(ctx context.Context, name, defID, cronSchedule, triggerPrompt string) (*sdkagentrun.APIResponse[sdkagentrun.Agent], error) {
	req := &sdkagentrun.CreateAgentRequest{
		Name:          name,
		StrategyType:  "chat-session:" + defID,
		CronSchedule:  cronSchedule,
		TriggerType:   "schedule",
		ExecutionMode: "execute",
		Enabled:       boolPtr(true),
	}
	return b.client.Agents.Create(ctx, req)
}

// TriggerAgentWithInput triggers a runtime agent with a prompt.
// sessionID, if non-empty, ties this trigger to a persistent ADK conversation session
// so successive triggers share conversation history (requires MP server >= v0.40.15).
// Uses raw HTTP because the SDK's TriggerRequest struct may not have the SessionID field.
func (b *Bridge) TriggerAgentWithInput(ctx context.Context, agentID, prompt, sessionID string) (*sdkagentrun.TriggerResponse, error) {
	// Build request body with optional sessionId
	body := map[string]any{
		"prompt": prompt,
	}
	if sessionID != "" {
		body["sessionId"] = sessionID
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal trigger body: %w", err)
	}

	url := fmt.Sprintf("%s/api/projects/%s/agents/%s/trigger", b.serverURL, b.projectID, agentID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create trigger request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := bridgeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trigger http: %w", err)
	}
	defer httpResp.Body.Close()

	var triggerResp sdkagentrun.TriggerResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&triggerResp); err != nil {
		return nil, fmt.Errorf("decode trigger response: %w", err)
	}
	return &triggerResp, nil
}

// GetAgentRuns returns recent runs for a runtime agent.
func (b *Bridge) GetAgentRuns(ctx context.Context, agentID string, limit int) (*sdkagentrun.APIResponse[[]sdkagentrun.AgentRun], error) {
	return b.client.Agents.GetRuns(ctx, agentID, limit)
}

// GetProjectRun returns details for a specific run.
func (b *Bridge) GetProjectRun(ctx context.Context, runID string) (*sdkagentrun.APIResponse[sdkagentrun.AgentRun], error) {
	return b.client.Agents.GetProjectRun(ctx, b.projectID, runID)
}

// GetRunMessages returns the conversation transcript for a run.
func (b *Bridge) GetRunMessages(ctx context.Context, runID string) (*sdkagentrun.APIResponse[[]sdkagentrun.AgentRunMessage], error) {
	return b.client.Agents.GetRunMessages(ctx, b.projectID, runID)
}

// GetRunToolCalls returns the tool calls made during a run.
func (b *Bridge) GetRunToolCalls(ctx context.Context, runID string) (*sdkagentrun.APIResponse[[]sdkagentrun.AgentRunToolCall], error) {
	return b.client.Agents.GetRunToolCalls(ctx, b.projectID, runID)
}

// GetProjectRunFull returns the full trace for a single run — run metadata,
// messages, tool calls, and optional parent run — in one request.
func (b *Bridge) GetProjectRunFull(ctx context.Context, runID string) (*sdkagentrun.APIResponse[sdkagentrun.AgentRunFull], error) {
	return b.client.Agents.GetProjectRunFull(ctx, b.projectID, runID)
}

// GetProjectRunStats returns aggregate analytics for agent runs over a period
// (overview, per-agent, top errors, tool stats, time series).
func (b *Bridge) GetProjectRunStats(ctx context.Context, opts *sdkagentrun.RunStatsOptions) (*sdkagentrun.APIResponse[sdkagentrun.RunStats], error) {
	return b.client.Agents.GetProjectRunStats(ctx, b.projectID, opts)
}

// GetProjectRunSessionStats returns session-level analytics — runs grouped by
// (platform, channelId, threadId) from triggerMetadata.
func (b *Bridge) GetProjectRunSessionStats(ctx context.Context, opts *sdkagentrun.RunStatsOptions) (*sdkagentrun.APIResponse[sdkagentrun.RunSessionStats], error) {
	return b.client.Agents.GetProjectRunSessionStats(ctx, b.projectID, opts)
}

// ============================================================================
// Internal helpers
// ============================================================================

func graphObjectToSession(obj *graph.GraphObject) *Session {
	s := &Session{
		ID:        obj.EntityID,
		Key:       safeStr(obj.Key),
		Title:     safePropStr(obj.Properties, "title"),
		Status:    safePropStr(obj.Properties, "status"),
		CreatedAt: obj.CreatedAt,
	}
	if mc, ok := obj.Properties["message_count"].(float64); ok {
		s.MessageCount = int(mc)
	}
	if tt, ok := obj.Properties["total_tokens"].(float64); ok {
		s.TotalTokens = int(tt)
	}
	return s
}

func graphObjectToMessage(obj *graph.GraphObject) *Message {
	m := &Message{
		ID:               obj.EntityID,
		Role:             safePropStr(obj.Properties, "role"),
		Content:          safePropStr(obj.Properties, "content"),
		ReasoningContent: safePropStr(obj.Properties, "reasoning_content"),
		CreatedAt:        obj.CreatedAt,
	}
	if m.ReasoningContent == "" {
		m.ReasoningContent = safePropStr(obj.Properties, "reasoningContent")
	}
	if seq, ok := obj.Properties["sequence_number"].(float64); ok {
		m.Seq = int(seq)
	}
	if tc, ok := obj.Properties["token_count"].(float64); ok {
		m.TokenCount = int(tc)
	}
	// Extract tool calls from the graph object properties.
	// The server stores toolCalls as a JSON array in the properties.
	if raw, ok := obj.Properties["toolCalls"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, item := range arr {
				if tc, ok := item.(map[string]any); ok {
					toolCall := ToolCall{
						ID:   safeAnyStr(tc, "id"),
						Name: safeAnyStr(tc, "name"),
					}
					if args, ok := tc["arguments"]; ok {
						switch v := args.(type) {
						case string:
							toolCall.Arguments = v
						case map[string]any, []any:
							if b, err := json.Marshal(v); err == nil {
								toolCall.Arguments = string(b)
							}
						}
					}
					m.ToolCalls = append(m.ToolCalls, toolCall)
				}
			}
		}
	}
	return m
}

func safeStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func safePropStr(props map[string]any, key string) string {
	if props == nil {
		return ""
	}
	v, ok := props[key]
	if !ok {
		return ""
	}
	return safeAnyToStr(v)
}

// safeAnyToStr converts an any value to a string, handling all JSON-compatible types.
func safeAnyToStr(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(s)
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return ""
	}
}

// safeAnyStr extracts a string from a map by key, handling type flexibility.
func safeAnyStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	return safeAnyToStr(v)
}

func boolPtr(v bool) *bool {
	return &v
}
