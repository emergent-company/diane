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
	"sort"
	"time"

	sdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/acp"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/chat"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	sdkprovider "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/provider"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
	sdkskills "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills"
)

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
	ID         string
	Role       string
	Content    string
	Seq        int
	TokenCount int // 0 if unknown; populated when stored with token counting
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

// ACP returns an ACP v1 client configured with this bridge's credentials.
// Uses a separate HTTP client with no timeout for SSE streaming.
func (b *Bridge) ACP() *acp.Client {
	return acp.NewClientWithHTTP(b.serverURL, b.apiKey, &http.Client{
		Timeout: 0, // no timeout for SSE streaming
	})
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
	resp, err := b.client.Graph.ListSessions(ctx, 100, "")
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	sessions := make([]Session, 0, len(resp.Items))
	for _, obj := range resp.Items {
		s := graphObjectToSession(obj)
		if status != "" {
			if s.Status != status {
				continue
			}
		}
		sessions = append(sessions, *s)
	}
	return sessions, nil
}

// ============================================================================
// Messages
// ============================================================================

// AppendMessage appends a message to a session and returns the created message.
// If tokenCount > 0, it's included in the request so the server can auto-maintain
// the session's total_tokens counter. Pass 0 to skip token counting.
func (b *Bridge) AppendMessage(ctx context.Context, sessionID, role, content string, tokenCount int, toolCallsJSON string) (*Message, error) {
	req := &graph.AppendMessageRequest{
		Role:    role,
		Content: content,
	}
	if tokenCount > 0 {
		req.TokenCount = &tokenCount
	}
	if toolCallsJSON != "" {
		// Store tool calls as a JSON string in ExtraProps so they're
		// accessible as a regular graph property on the message object.
		req.ExtraProps = map[string]any{
			"tool_calls_json": toolCallsJSON,
		}
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

// ListProjectProviderConfigs returns project-level provider configs for the given org.
func (b *Bridge) ListProjectProviderConfigs(ctx context.Context, orgID string) ([]sdkprovider.ProjectProviderConfig, error) {
	return b.client.Provider.ListProjectConfigsByOrg(ctx, orgID)
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
// Runs a live credential test and syncs model catalog on success.
func (b *Bridge) UpsertProjectProvider(ctx context.Context, projectID, providerType string, apiKey, model, baseURL string) (*sdkprovider.ProviderConfig, error) {
	req := &sdkprovider.UpsertProviderConfigRequest{
		APIKey:          apiKey,
		GenerativeModel: model,
		BaseURL:         baseURL,
	}
	return b.client.Provider.UpsertProjectConfig(ctx, projectID, providerType, req)
}

// DeleteOrgProvider removes an org-level provider config.
func (b *Bridge) DeleteOrgProvider(ctx context.Context, orgID, provider string) error {
	return b.client.Provider.DeleteOrgConfig(ctx, orgID, provider)
}

// DeleteProjectProvider removes a project-level provider config.
func (b *Bridge) DeleteProjectProvider(ctx context.Context, projectID, provider string) error {
	return b.client.Provider.DeleteProjectConfig(ctx, projectID, provider)
}

// ListProviderModels returns the model catalog for a given provider and type.
func (b *Bridge) ListProviderModels(ctx context.Context, provider, modelType string) ([]sdkprovider.SupportedModel, error) {
	return b.client.Provider.ListModels(ctx, provider, modelType)
}

// TestProvider sends a live generation call to verify provider credentials work.
func (b *Bridge) TestProvider(ctx context.Context, orgID, providerType string) (*sdkprovider.TestProviderResponse, error) {
	return b.client.Provider.TestProvider(ctx, providerType, b.projectID, orgID)
}

// TestProviderByProject sends a live generation call scoped to a specific project.
func (b *Bridge) TestProviderByProject(ctx context.Context, projectID, providerType string) (*sdkprovider.TestProviderResponse, error) {
	orgID := b.projectID // fallback: pass projectID as orgID hint
	return b.client.Provider.TestProvider(ctx, providerType, projectID, orgID)
}

// ============================================================================
// Skills API — delegates to Memory Platform's Skills API
// ============================================================================

// SkillsClient returns the raw skills client for advanced operations.
func (b *Bridge) SkillsClient() *sdkskills.Client {
	return b.client.Skills
}

// ListSkills returns all skills for the current project (merged with global).
func (b *Bridge) ListSkills(ctx context.Context) ([]*sdkskills.Skill, error) {
	return b.client.Skills.List(ctx, b.projectID)
}

// GetSkill returns a skill by ID.
func (b *Bridge) GetSkill(ctx context.Context, id string) (*sdkskills.Skill, error) {
	return b.client.Skills.Get(ctx, id)
}

// CreateSkill creates a new skill scoped to the current project.
func (b *Bridge) CreateSkill(ctx context.Context, req *sdkskills.CreateSkillRequest) (*sdkskills.Skill, error) {
	return b.client.Skills.Create(ctx, b.projectID, req)
}

// UpdateSkill updates an existing skill.
func (b *Bridge) UpdateSkill(ctx context.Context, id string, req *sdkskills.UpdateSkillRequest) (*sdkskills.Skill, error) {
	return b.client.Skills.Update(ctx, id, req)
}

// DeleteSkill deletes a skill by ID.
func (b *Bridge) DeleteSkill(ctx context.Context, id string) error {
	return b.client.Skills.Delete(ctx, id)
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

	httpResp, err := http.DefaultClient.Do(req)
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

// ListSessionRuns returns agent runs associated with a given session ID.
// It fetches recent project runs and filters by triggerMetadata.sessionId.
func (b *Bridge) ListSessionRuns(ctx context.Context, sessionID string, limit int) ([]sdkagentrun.AgentRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	opts := &sdkagentrun.ListRunsOptions{
		Limit: limit,
	}
	resp, err := b.client.Agents.ListProjectRuns(ctx, b.projectID, opts)
	if err != nil {
		return nil, fmt.Errorf("list project runs: %w", err)
	}
	if resp == nil {
		return nil, nil
	}
	var out []sdkagentrun.AgentRun
	for _, run := range resp.Data.Items {
		if run.TriggerMetadata == nil {
			continue
		}
		sid, ok := run.TriggerMetadata["sessionId"]
		if !ok {
			continue
		}
		sidStr, ok := sid.(string)
		if !ok || sidStr != sessionID {
			continue
		}
		out = append(out, run)
	}
	return out, nil
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
	if ua, ok := obj.Properties["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ua); err == nil {
			s.UpdatedAt = t
		}
	} else if ea, ok := obj.Properties["ended_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ea); err == nil {
			s.UpdatedAt = t
		}
	}
	return s
}

func graphObjectToMessage(obj *graph.GraphObject) *Message {
	m := &Message{
		ID:      obj.EntityID,
		Role:    safePropStr(obj.Properties, "role"),
		Content: safePropStr(obj.Properties, "content"),
	}
	if seq, ok := obj.Properties["sequence_number"].(float64); ok {
		m.Seq = int(seq)
	}
	if tc, ok := obj.Properties["token_count"].(float64); ok {
		m.TokenCount = int(tc)
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
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// safePropBool extracts a bool from a map by key, defaulting to false.
func safePropBool(props map[string]any, key string) bool {
	if props == nil {
		return false
	}
	v, ok := props[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

// safePropVersion extracts an int from a map by key, handling JSON float64 encoding.
func safePropVersion(props map[string]any, key string) int {
	if props == nil {
		return 0
	}
	v, ok := props[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func boolPtr(v bool) *bool {
	return &v
}

// ============================================================================
// Discord Channel Map
// ============================================================================

// DiscordChannelMapType is the graph object type name for Discord channel↔session mappings.
const DiscordChannelMapType = "DiscordChannelMap"

// DiscordChannelMap maps a Discord channel/thread to a Memory Platform session.
type DiscordChannelMap struct {
	ChannelID string
	SessionID string
	AgentType string
	EntityID  string
}

// UpsertDiscordChannelMap creates or updates a Discord channel→session mapping.
// Keyed by channel_id for dedup.
func (b *Bridge) UpsertDiscordChannelMap(ctx context.Context, channelID, sessionID, agentType string) (*DiscordChannelMap, error) {
	props := map[string]any{
		"channel_id": channelID,
		"session_id": sessionID,
		"agent_type": agentType,
	}

	existing, err := b.client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: DiscordChannelMapType,
		Key:  channelID,
	})
	if err != nil {
		return nil, fmt.Errorf("list discord channel maps: %w", err)
	}

	if len(existing.Items) > 0 {
		entityID := existing.Items[0].EntityID
		_, err = b.client.Graph.UpdateObject(ctx, entityID, &graph.UpdateObjectRequest{
			Properties: props,
		})
		if err != nil {
			return nil, fmt.Errorf("update discord channel map %s: %w", channelID, err)
		}
		return &DiscordChannelMap{
			ChannelID: channelID,
			SessionID: sessionID,
			AgentType: agentType,
			EntityID:  entityID,
		}, nil
	}

	key := channelID
	obj, err := b.client.Graph.CreateObject(ctx, &graph.CreateObjectRequest{
		Type:       DiscordChannelMapType,
		Key:        &key,
		Properties: props,
	})
	if err != nil {
		return nil, fmt.Errorf("create discord channel map %s: %w", channelID, err)
	}
	return &DiscordChannelMap{
		ChannelID: channelID,
		SessionID: sessionID,
		AgentType: agentType,
		EntityID:  obj.EntityID,
	}, nil
}

// ListDiscordChannelMaps returns all Discord channel→session mappings.
func (b *Bridge) ListDiscordChannelMaps(ctx context.Context) ([]DiscordChannelMap, error) {
	resp, err := b.client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: DiscordChannelMapType,
	})
	if err != nil {
		return nil, fmt.Errorf("list discord channel maps: %w", err)
	}

	maps := make([]DiscordChannelMap, 0, len(resp.Items))
	for _, obj := range resp.Items {
		props := obj.Properties
		if props == nil {
			continue
		}
		maps = append(maps, DiscordChannelMap{
			ChannelID: safePropStr(props, "channel_id"),
			SessionID: safePropStr(props, "session_id"),
			AgentType: safePropStr(props, "agent_type"),
			EntityID:  obj.EntityID,
		})
	}
	return maps, nil
}

// ============================================================================
// Session Todo — lightweight task items attached to a session
// ============================================================================

const sessionTodoType = "SessionTodo"

// SessionTodo is a task item attached to a conversation session.
type SessionTodo struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Status    string `json:"status"` // pending, completed, cancelled
	Order     int    `json:"order"`
	CreatedAt string `json:"created_at,omitempty"`
}

// CreateSessionTodo creates a new todo item for a session.
// Key format: session:<sessionID>:order:<order>
func (b *Bridge) CreateSessionTodo(ctx context.Context, sessionID, content string, order int) (*SessionTodo, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	key := fmt.Sprintf("session:%s:order:%d", sessionID, order)

	obj, err := b.client.Graph.CreateObject(ctx, &graph.CreateObjectRequest{
		Type: sessionTodoType,
		Key:  &key,
		Properties: map[string]any{
			"session_id": sessionID,
			"content":    content,
			"status":     "pending",
			"order":      order,
			"created_at": now,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create session todo: %w", err)
	}
	return graphObjectToSessionTodo(obj), nil
}

// ListSessionTodos returns all todos for a session, ordered by order ascending.
func (b *Bridge) ListSessionTodos(ctx context.Context, sessionID string) ([]SessionTodo, error) {
	resp, err := b.client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: sessionTodoType,
	})
	if err != nil {
		return nil, fmt.Errorf("list session todos: %w", err)
	}

	todos := make([]SessionTodo, 0, len(resp.Items))
	for _, obj := range resp.Items {
		t := graphObjectToSessionTodo(obj)
		if t.SessionID == sessionID {
			todos = append(todos, *t)
		}
	}

	sort.Slice(todos, func(i, j int) bool {
		return todos[i].Order < todos[j].Order
	})
	return todos, nil
}

// UpdateSessionTodo updates the content, status, and/or order of a todo item.
func (b *Bridge) UpdateSessionTodo(ctx context.Context, todoID string, content, status *string, order *int) (*SessionTodo, error) {
	props := make(map[string]any)
	if content != nil {
		props["content"] = *content
	}
	if status != nil {
		props["status"] = *status
	}
	if order != nil {
		props["order"] = *order
	}
	if len(props) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	obj, err := b.client.Graph.UpdateObject(ctx, todoID, &graph.UpdateObjectRequest{
		Properties: props,
	})
	if err != nil {
		return nil, fmt.Errorf("update session todo %s: %w", todoID, err)
	}
	return graphObjectToSessionTodo(obj), nil
}

// DeleteSessionTodo removes a todo item.
func (b *Bridge) DeleteSessionTodo(ctx context.Context, todoID string) error {
	return b.client.Graph.DeleteObject(ctx, todoID, nil)
}

func graphObjectToSessionTodo(obj *graph.GraphObject) *SessionTodo {
	props := obj.Properties
	order := 0
	if o, ok := props["order"].(float64); ok {
		order = int(o)
	}
	return &SessionTodo{
		ID:        obj.EntityID,
		SessionID: safePropStr(props, "session_id"),
		Content:   safePropStr(props, "content"),
		Status:    safePropStr(props, "status"),
		Order:     order,
		CreatedAt: safePropStr(props, "created_at"),
	}
}

// ============================================================================
// Graph Object Stats
// ============================================================================

// TypeCount holds the count for a single graph object type.
type TypeCount struct {
	TypeName string `json:"type_name"`
	Count    int    `json:"count"`
}

// GraphObjectStats holds aggregate stats about graph objects in the project.
type GraphObjectStats struct {
	Total  int         `json:"total"`
	ByType []TypeCount `json:"by_type"`
}

// GetGraphObjectStats queries the Memory Platform for graph object counts.
func (b *Bridge) GetGraphObjectStats(ctx context.Context) (*GraphObjectStats, error) {
	keyTypes := []struct {
		name string
		nice string
	}{
		{"Session", "Session"},
		{"MemoryFact", "Memory Fact"},
		{"DianeNodeConfig", "Node Config"},
		{"MCPProxyConfig", "MCP Proxy Config"},
		{"MCPSecret", "MCP Secret"},
	}

	total, err := b.client.Graph.CountObjects(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("count objects: %w", err)
	}

	byType := make([]TypeCount, 0, len(keyTypes))
	keyTotal := 0

	for _, kt := range keyTypes {
		count, err := b.client.Graph.CountObjects(ctx, &graph.CountObjectsOptions{
			Type: kt.name,
		})
		if err != nil {
			continue
		}
		if count > 0 {
			byType = append(byType, TypeCount{TypeName: kt.nice, Count: count})
			keyTotal += count
		}
	}

	other := total - keyTotal
	if other > 0 {
		byType = append(byType, TypeCount{TypeName: "Other", Count: other})
	}

	sort.Slice(byType, func(i, j int) bool {
		return byType[i].Count > byType[j].Count
	})

	return &GraphObjectStats{
		Total:   total,
		ByType:  byType,
	}, nil
}

// GetObjectCountsForSchema queries object counts for each schema type name.
func (b *Bridge) GetObjectCountsForSchema(ctx context.Context, typeNames []string) (map[string]int, error) {
	counts := make(map[string]int, len(typeNames))
	for _, tn := range typeNames {
		count, err := b.client.Graph.CountObjects(ctx, &graph.CountObjectsOptions{
			Type: tn,
		})
		if err != nil {
			continue
		}
		counts[tn] = count
	}
	return counts, nil
}

// MCPProxyConfigType is the graph object type name for MCP proxy config objects.
const MCPProxyConfigType = "MCPProxyConfig"

// MCPProxyConfigEntry holds a parsed MCP proxy config for the local API.
type MCPProxyConfigEntry struct {
	Scope    string `json:"scope"`
	Config   string `json:"config"`
	Version  int    `json:"version"`
	EntityID string `json:"entity_id"`
}

// ListMCPProxyConfigs returns all MCP proxy configs registered in the project's graph.
func (b *Bridge) ListMCPProxyConfigs(ctx context.Context) ([]MCPProxyConfigEntry, error) {
	resp, err := b.client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: MCPProxyConfigType,
	})
	if err != nil {
		return nil, fmt.Errorf("list mcp proxy configs: %w", err)
	}

	entries := make([]MCPProxyConfigEntry, 0, len(resp.Items))
	for _, obj := range resp.Items {
		props := obj.Properties
		if props == nil {
			continue
		}
		scope := safePropStr(props, "scope")
		if scope == "" {
			continue
		}
		entries = append(entries, MCPProxyConfigEntry{
			Scope:    scope,
			Config:   safePropStr(props, "config"),
			Version:  safePropVersion(props, "version"),
			EntityID: obj.EntityID,
		})
	}
	return entries, nil
}

// UpdateMCPProxyConfigScope updates the scope of an MCP proxy config by entity ID.
// Returns the updated entity ID on success.
func (b *Bridge) UpdateMCPProxyConfigScope(ctx context.Context, entityID, newScope string) error {
	_, err := b.client.Graph.UpdateObject(ctx, entityID, &graph.UpdateObjectRequest{
		Properties: map[string]any{
			"scope": newScope,
		},
	})
	if err != nil {
		return fmt.Errorf("update mcp proxy config scope: %w", err)
	}
	return nil
}

// FindMCPProxyConfigEntityID finds the entity ID for an MCP proxy config by server name.
func (b *Bridge) FindMCPProxyConfigEntityID(ctx context.Context, serverName string) (string, error) {
	entries, err := b.ListMCPProxyConfigs(ctx)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		var nameCheck struct {
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(e.Config), &nameCheck) == nil && nameCheck.Name == serverName {
			return e.EntityID, nil
		}
	}
	return "", fmt.Errorf("mcp proxy config not found: %s", serverName)
}

// CreateMCPProxyConfig creates a new MCP proxy config in the graph.
func (b *Bridge) CreateMCPProxyConfig(ctx context.Context, configJSON, scope string) error {
	_, err := b.client.Graph.CreateObject(ctx, &graph.CreateObjectRequest{
		Type: MCPProxyConfigType,
		Properties: map[string]any{
			"scope":  scope,
			"config": configJSON,
		},
	})
	if err != nil {
		return fmt.Errorf("create mcp proxy config: %w", err)
	}
	return nil
}

// DeleteMCPProxyConfig deletes an MCP proxy config by entity ID.
func (b *Bridge) DeleteMCPProxyConfig(ctx context.Context, entityID string) error {
	err := b.client.Graph.DeleteObject(ctx, entityID, nil)
	if err != nil {
		return fmt.Errorf("delete mcp proxy config: %w", err)
	}
	return nil
}

// UpdateMCPProxyConfig replaces the config JSON for an MCP proxy config by entity ID.
func (b *Bridge) UpdateMCPProxyConfig(ctx context.Context, entityID, configJSON string) error {
	_, err := b.client.Graph.UpdateObject(ctx, entityID, &graph.UpdateObjectRequest{
		Properties: map[string]any{
			"config": configJSON,
		},
	})
	if err != nil {
		return fmt.Errorf("update mcp proxy config: %w", err)
	}
	return nil
}
