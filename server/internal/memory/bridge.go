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
	"strconv"
	"time"

	sdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/chat"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	sdkprovider "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/provider"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkagentrun "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

// NodeConfig represents a Diane node's configuration stored in the MP graph.
// Each node creates/updates its config on startup for cross-node discovery.
type NodeConfig struct {
	InstanceID string `json:"instance_id"`
	Hostname   string `json:"hostname,omitempty"`
	Mode       string `json:"mode"` // "master" or "slave"
	Version    string `json:"version,omitempty"`
	LastSeen   string `json:"last_seen,omitempty"` // ISO 8601
	EntityID   string `json:"entity_id,omitempty"` // graph object EntityID, populated on read
}

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
// Runs a live credential test and syncs model catalog on success.
func (b *Bridge) UpsertProjectProvider(ctx context.Context, projectID, providerType string, apiKey, model, baseURL string) (*sdkprovider.ProviderConfig, error) {
	req := &sdkprovider.UpsertProviderConfigRequest{
		APIKey:          apiKey,
		GenerativeModel: model,
		BaseURL:         baseURL,
	}
	return b.client.Provider.UpsertProjectConfig(ctx, projectID, providerType, req)
}

// These provider names are tried when listing a project's configured providers.
var knownProviderNames = []string{
	"google", "openai", "anthropic", "deepseek",
	"openai-compatible", "meta", "mistral", "xai",
}

// ProjectProviderInfo holds a project-level provider config (no secrets).
type ProjectProviderInfo struct {
	Provider        string `json:"provider"`
	BaseURL         string `json:"base_url,omitempty"`
	GenerativeModel string `json:"generative_model,omitempty"`
	EmbeddingModel  string `json:"embedding_model,omitempty"`
}

// ListProjectProviders queries known provider names to discover which ones
// are configured at the project level. Returns only configured providers.
func (b *Bridge) ListProjectProviders(ctx context.Context) ([]ProjectProviderInfo, error) {
	var result []ProjectProviderInfo
	for _, name := range knownProviderNames {
		cfg, err := b.client.Provider.GetProjectConfig(ctx, b.projectID, name)
		if err != nil {
			// not_found is expected — skip silently
			continue
		}
		if cfg != nil {
			result = append(result, ProjectProviderInfo{
				Provider:        cfg.Provider,
				BaseURL:         cfg.BaseURL,
				GenerativeModel: cfg.GenerativeModel,
				EmbeddingModel:  cfg.EmbeddingModel,
			})
		}
	}
	return result, nil
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
// Falls back to local aggregation from ListProjectRuns when the MP stats
// endpoint returns 0 runs but runs actually exist.
func (b *Bridge) GetProjectRunStats(ctx context.Context, opts *sdkagentrun.RunStatsOptions) (*sdkagentrun.APIResponse[sdkagentrun.RunStats], error) {
	resp, err := b.client.Agents.GetProjectRunStats(ctx, b.projectID, opts)
	if err != nil {
		return nil, err
	}

	// If the MP stats endpoint returns data, use it directly
	if resp != nil && resp.Data.Overview.TotalRuns > 0 {
		return resp, nil
	}

	// Fallback: aggregate from ListProjectRuns when stats endpoint returns 0
	// but runs actually exist (known MP API issue).
	runs, listErr := b.client.Agents.ListProjectRuns(ctx, b.projectID, &sdkagentrun.ListRunsOptions{
		Limit: 500,
	})
	if listErr != nil || runs == nil || len(runs.Data.Items) == 0 {
		// No runs at all — return the original (empty) response
		return resp, nil
	}

	return b.aggregateRunStats(runs.Data.Items, opts), nil
}

// aggregateRunStats converts a slice of AgentRun records into a RunStats
// response, grouped by agent. Used as fallback when the MP stats API
// returns empty results.
func (b *Bridge) aggregateRunStats(runs []sdkagentrun.AgentRun, opts *sdkagentrun.RunStatsOptions) *sdkagentrun.APIResponse[sdkagentrun.RunStats] {
	// Filter by time window if specified
	var since time.Time
	if opts != nil && opts.Since != nil {
		since = *opts.Since
	}

	type agentAccum struct {
		total       int64
		success     int64
		failed      int64
		errored     int64
		totalDurMs  float64
		totalCost   float64
		totalInput  float64
		totalOutput float64
	}

	byAgent := make(map[string]*agentAccum)
	var totalRuns, successCount, failedCount, errorCount int64
	var totalDurationMs, totalCostUsd float64

	for _, r := range runs {
		if !since.IsZero() && r.StartedAt.Before(since) {
			continue
		}

		// Determine agent key — use AgentName if available, fall back to AgentID
		agentKey := r.AgentID
		if r.AgentName != "" {
			agentKey = r.AgentName
		}

		a, ok := byAgent[agentKey]
		if !ok {
			a = &agentAccum{}
			byAgent[agentKey] = a
		}

		a.total++
		totalRuns++

		var durMs float64
		if r.DurationMs != nil {
			durMs = float64(*r.DurationMs)
		}

		switch r.Status {
		case "success":
			a.success++
			successCount++
		case "failed":
			a.failed++
			failedCount++
		case "error", "errored":
			a.errored++
			errorCount++
		default:
			// Treat unknown as errored for stats purposes
			a.errored++
			errorCount++
		}

		a.totalDurMs += durMs
		totalDurationMs += durMs

		if r.TokenUsage != nil {
			a.totalInput += float64(r.TokenUsage.TotalInputTokens)
			a.totalOutput += float64(r.TokenUsage.TotalOutputTokens)
			a.totalCost += r.TokenUsage.EstimatedCostUSD
			totalCostUsd += r.TokenUsage.EstimatedCostUSD
		}
	}

	// Build per-agent stats
	byAgentStats := make(map[string]sdkagentrun.RunStatsAgent, len(byAgent))
	for key, a := range byAgent {
		var avgDur, avgCost, avgInput, avgOutput float64
		if a.total > 0 {
			avgDur = a.totalDurMs / float64(a.total)
			avgCost = a.totalCost / float64(a.total)
			avgInput = a.totalInput / float64(a.total)
			avgOutput = a.totalOutput / float64(a.total)
		}
		byAgentStats[key] = sdkagentrun.RunStatsAgent{
			Total:           a.total,
			Success:         a.success,
			Failed:          a.failed,
			Errored:         a.errored,
			AvgDurationMs:   avgDur,
			AvgCostUSD:      avgCost,
			TotalCostUSD:    a.totalCost,
			AvgInputTokens:  avgInput,
			AvgOutputTokens: avgOutput,
		}
	}

	// Compute overall averages
	var overallAvgDur, successRate float64
	if totalRuns > 0 {
		overallAvgDur = totalDurationMs / float64(totalRuns)
		successRate = float64(successCount) / float64(totalRuns) * 100
	}

	return &sdkagentrun.APIResponse[sdkagentrun.RunStats]{
		Success: true,
		Data: sdkagentrun.RunStats{
			Overview: sdkagentrun.RunStatsOverview{
				TotalRuns:     totalRuns,
				SuccessCount:  successCount,
				FailedCount:   failedCount,
				ErrorCount:    errorCount,
				SuccessRate:   successRate,
				AvgDurationMs: overallAvgDur,
				TotalCostUSD:  totalCostUsd,
			},
			ByAgent: byAgentStats,
			Period: sdkagentrun.RunStatsPeriod{
				Since: time.Now().Add(-24 * time.Hour),
				Until: time.Now(),
			},
		},
	}
}

// GetProjectRunSessionStats returns session-level analytics — runs grouped by
// (platform, channelId, threadId) from triggerMetadata.
func (b *Bridge) GetProjectRunSessionStats(ctx context.Context, opts *sdkagentrun.RunStatsOptions) (*sdkagentrun.APIResponse[sdkagentrun.RunSessionStats], error) {
	return b.client.Agents.GetProjectRunSessionStats(ctx, b.projectID, opts)
}

// SessionRunAggregates holds aggregated cost/token/run data for a single session.
type SessionRunAggregates struct {
	TotalRuns         int     `json:"total_runs"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	EstimatedCostUSD  float64 `json:"estimated_cost_usd"`
}

// GetSessionRunAggregates returns aggregated run stats for a session by
// listing recent project runs and filtering by trigger metadata session ID.
func (b *Bridge) GetSessionRunAggregates(ctx context.Context, sessionID string) (*SessionRunAggregates, error) {
	runs, err := b.client.Agents.ListProjectRuns(ctx, b.projectID, &sdkagentrun.ListRunsOptions{
		Limit: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("list project runs: %w", err)
	}

	var agg SessionRunAggregates
	for _, r := range runs.Data.Items {
		// Match by session ID in trigger metadata
		if !hasSessionID(r.TriggerMetadata, sessionID) {
			continue
		}
		agg.TotalRuns++
		if r.TokenUsage != nil {
			agg.TotalInputTokens += r.TokenUsage.TotalInputTokens
			agg.TotalOutputTokens += r.TokenUsage.TotalOutputTokens
			agg.EstimatedCostUSD += r.TokenUsage.EstimatedCostUSD
		}
	}
	return &agg, nil
}

// hasSessionID checks if a trigger metadata map contains the given session ID
// under any of the common key variations (sessionId, session_id, sessionID).
func hasSessionID(meta map[string]any, sessionID string) bool {
	if meta == nil {
		return false
	}
	for _, key := range []string{"sessionId", "session_id", "sessionID"} {
		if v, ok := meta[key]; ok {
			if s, ok := v.(string); ok && s == sessionID {
				return true
			}
		}
	}
	return false
}

// ============================================================================
// Node Config (DianeNodeConfig graph objects)
// ============================================================================

// NodeConfigType is the graph object type name for node configuration objects.
const NodeConfigType = "DianeNodeConfig"

// UpsertNodeConfig creates or updates this node's config in the graph.
// Nodes call this on startup so other nodes can discover them via the MP.
func (b *Bridge) UpsertNodeConfig(ctx context.Context, cfg *NodeConfig) (*NodeConfig, error) {
	props := map[string]any{
		"instance_id": cfg.InstanceID,
		"hostname":    cfg.Hostname,
		"mode":        cfg.Mode,
		"version":     cfg.Version,
		"last_seen":   cfg.LastSeen,
	}

	// Try to find existing object by key (instance_id)
	existing, err := b.client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: NodeConfigType,
		Key:  cfg.InstanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list node configs: %w", err)
	}

	if len(existing.Items) > 0 {
		// Update existing
		entityID := existing.Items[0].EntityID
		_, err = b.client.Graph.UpdateObject(ctx, entityID, &graph.UpdateObjectRequest{
			Properties: props,
		})
		if err != nil {
			return nil, fmt.Errorf("update node config %s: %w", cfg.InstanceID, err)
		}
		cfg.EntityID = entityID
		return cfg, nil
	}

	// Create new
	key := cfg.InstanceID
	obj, err := b.client.Graph.CreateObject(ctx, &graph.CreateObjectRequest{
		Type:       NodeConfigType,
		Key:        &key,
		Properties: props,
	})
	if err != nil {
		return nil, fmt.Errorf("create node config %s: %w", cfg.InstanceID, err)
	}
	cfg.EntityID = obj.EntityID
	return cfg, nil
}

// ListNodeConfigs returns all node configs registered in the project's graph.
func (b *Bridge) ListNodeConfigs(ctx context.Context) ([]NodeConfig, error) {
	resp, err := b.client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: NodeConfigType,
	})
	if err != nil {
		return nil, fmt.Errorf("list node configs: %w", err)
	}

	nodes := make([]NodeConfig, 0, len(resp.Items))
	for _, obj := range resp.Items {
		props := obj.Properties
		if props == nil {
			continue
		}
		nc := NodeConfig{
			InstanceID: safePropStr(props, "instance_id"),
			Hostname:   safePropStr(props, "hostname"),
			Mode:       safePropStr(props, "mode"),
			Version:    safePropStr(props, "version"),
			LastSeen:   safePropStr(props, "last_seen"),
			EntityID:   obj.EntityID,
		}
		if nc.InstanceID == "" {
			continue
		}
		nodes = append(nodes, nc)
	}
	return nodes, nil
}

// MCPProxyConfigType is the graph object type name for MCP proxy config objects.
const MCPProxyConfigType = "MCPProxyConfig"

// MCPProxyConfigRequest holds the config for an MCP proxy config in the graph.
type MCPProxyConfigRequest struct {
	Scope   string `json:"scope"`   // e.g. "all", "instance:tool", "slave:*"
	Config  string `json:"config"`  // JSON string matching mcpproxy.ServerConfig
	Version int    `json:"version"` // bump on each update
}

// UpsertMCPProxyConfig creates or updates an MCP proxy config in the graph.
// Uses Key = scope for dedup so multiple versions of the same scope overwrite.
func (b *Bridge) UpsertMCPProxyConfig(ctx context.Context, req *MCPProxyConfigRequest) error {
	if req.Scope == "" {
		return fmt.Errorf("scope is required")
	}
	if req.Config == "" {
		return fmt.Errorf("config is required")
	}

	props := map[string]any{
		"scope":   req.Scope,
		"config":  req.Config,
		"version": req.Version,
	}

	// Try to find existing by key (scope)
	existing, err := b.client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: MCPProxyConfigType,
		Key:  req.Scope,
	})
	if err != nil {
		return fmt.Errorf("list mcp proxy configs: %w", err)
	}

	if len(existing.Items) > 0 {
		_, err = b.client.Graph.UpdateObject(ctx, existing.Items[0].EntityID, &graph.UpdateObjectRequest{
			Properties: props,
		})
		if err != nil {
			return fmt.Errorf("update mcp proxy config %s: %w", req.Scope, err)
		}
		return nil
	}

	key := req.Scope
	_, err = b.client.Graph.CreateObject(ctx, &graph.CreateObjectRequest{
		Type:       MCPProxyConfigType,
		Key:        &key,
		Properties: props,
	})
	if err != nil {
		return fmt.Errorf("create mcp proxy config %s: %w", req.Scope, err)
	}
	return nil
}

// DeleteMCPProxyConfig removes an MCP proxy config from the graph by scope.
func (b *Bridge) DeleteMCPProxyConfig(ctx context.Context, scope string) error {
	existing, err := b.client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: MCPProxyConfigType,
		Key:  scope,
	})
	if err != nil {
		return fmt.Errorf("list mcp proxy configs: %w", err)
	}
	if len(existing.Items) == 0 {
		return fmt.Errorf("no MCP proxy config found for scope %q", scope)
	}
	return b.client.Graph.DeleteObject(ctx, existing.Items[0].EntityID, nil)
}

// MCPSecretType is the graph object type name for MCP secret objects.
const MCPSecretType = "MCPSecret"

// MCPSecretRequest holds a secret for MCP server authentication stored in the graph.
type MCPSecretRequest struct {
	Name  string `json:"name"`  // server name this secret belongs to
	Scope string `json:"scope"` // scope targeting (same as MCPProxyConfig)
	Value string `json:"value"` // the secret value (OAuth token, API key, etc.)
}

// UpsertMCPSecret creates or updates an MCP secret in the graph.
// Uses a composite key: {scope}/{name} for dedup.
func (b *Bridge) UpsertMCPSecret(ctx context.Context, req *MCPSecretRequest) error {
	if req.Name == "" || req.Scope == "" {
		return fmt.Errorf("name and scope are required")
	}
	key := req.Scope + "/" + req.Name

	props := map[string]any{
		"name":  req.Name,
		"scope": req.Scope,
		"value": req.Value,
	}

	existing, err := b.client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: MCPSecretType,
		Key:  key,
	})
	if err != nil {
		return fmt.Errorf("list mcp secrets: %w", err)
	}

	if len(existing.Items) > 0 {
		_, err = b.client.Graph.UpdateObject(ctx, existing.Items[0].EntityID, &graph.UpdateObjectRequest{
			Properties: props,
		})
		if err != nil {
			return fmt.Errorf("update mcp secret %s: %w", key, err)
		}
		return nil
	}

	_, err = b.client.Graph.CreateObject(ctx, &graph.CreateObjectRequest{
		Type:       MCPSecretType,
		Key:        &key,
		Properties: props,
	})
	if err != nil {
		return fmt.Errorf("create mcp secret %s: %w", key, err)
	}
	return nil
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
	Total int          `json:"total"`
	ByType []TypeCount `json:"by_type"`
}

// GetGraphObjectStats queries the Memory Platform for graph object counts.
// It returns total objects across all types plus a per-type breakdown.
func (b *Bridge) GetGraphObjectStats(ctx context.Context) (*GraphObjectStats, error) {
	// Key types we track individually
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

	// Get total count of all objects
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
			// Skip types that don't exist in the project schema yet
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

	// Sort by count descending
	sort.Slice(byType, func(i, j int) bool {
		return byType[i].Count > byType[j].Count
	})

	return &GraphObjectStats{
		Total:   total,
		ByType:  byType,
	}, nil
}

// GraphObjectSummary is a lightweight summary of a graph object for display.
type GraphObjectSummary struct {
	EntityID          string `json:"entity_id"`
	Key               string `json:"key,omitempty"`
	Type              string `json:"type"`
	CreatedAt         string `json:"created_at"`
	RelationshipCount int    `json:"relationship_count"`
	Title             string `json:"title,omitempty"`
	Status            string `json:"status,omitempty"`
}

// GetObjectCountsForSchema queries object counts for each schema type name.
// Returns a map of type_name → count.
func (b *Bridge) GetObjectCountsForSchema(ctx context.Context, typeNames []string) (map[string]int, error) {
	counts := make(map[string]int, len(typeNames))
	for _, tn := range typeNames {
		count, err := b.client.Graph.CountObjects(ctx, &graph.CountObjectsOptions{
			Type: tn,
		})
		if err != nil {
			// Types not yet in the project schema — skip
			continue
		}
		counts[tn] = count
	}
	return counts, nil
}

// ListRecentObjectsByType returns the most recently created objects of a given type.
func (b *Bridge) ListRecentObjectsByType(ctx context.Context, typeName string, limit int) ([]GraphObjectSummary, error) {
	resp, err := b.client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type:  typeName,
		Limit: limit,
		Order: "desc",
	})
	if err != nil {
		return nil, fmt.Errorf("list objects by type %s: %w", typeName, err)
	}

	results := make([]GraphObjectSummary, 0, len(resp.Items))
	for _, obj := range resp.Items {
		s := GraphObjectSummary{
			EntityID:          obj.EntityID,
			Type:              obj.Type,
			RelationshipCount: safeInt(obj.RelationshipCount),
			Title:             safePropStr(obj.Properties, "title"),
			Status:            safePropStr(obj.Properties, "status"),
		}
		s.CreatedAt = obj.CreatedAt.Format(time.RFC3339)
		if obj.Key != nil {
			s.Key = *obj.Key
		}
		results = append(results, s)
	}
	return results, nil
}

// safeInt dereferences an *int, returning 0 if nil.
func safeInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// CountObjectsByType returns the total count of objects for a given type.
func (b *Bridge) CountObjectsByType(ctx context.Context, typeName string) (int, error) {
	return b.client.Graph.CountObjects(ctx, &graph.CountObjectsOptions{
		Type: typeName,
	})
}

// ProviderStats holds aggregated metrics grouped by (provider, model).
type ProviderStats struct {
	ProviderName      string  `json:"provider_name"`
	ModelName         string  `json:"model_name"`
	TotalRuns         int     `json:"total_runs"`
	SuccessRuns       int     `json:"success_runs"`
	ErrorRuns         int     `json:"error_runs"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
}

// GetProviderStats aggregates recent project runs by (provider, model).
func (b *Bridge) GetProviderStats(ctx context.Context, hours int) ([]ProviderStats, error) {
	if hours <= 0 || hours > 720 {
		hours = 24
	}
	runs, err := b.client.Agents.ListProjectRuns(ctx, b.projectID, &sdkagentrun.ListRunsOptions{
		Limit: 500,
	})
	if err != nil {
		return nil, fmt.Errorf("list project runs: %w", err)
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	type key struct {
		provider string
		model    string
	}
	type agg struct {
		total, success, errorRuns int
		inTokens, outTokens       int64
		cost                      float64
	}
	byKey := make(map[key]*agg)

	for _, r := range runs.Data.Items {
		if r.StartedAt.Before(since) {
			continue
		}
		prov := ""
		if r.Provider != nil {
			prov = *r.Provider
		}
		mod := ""
		if r.Model != nil {
			mod = *r.Model
		}
		// Normalize empty to "unknown"
		if prov == "" {
			prov = "unknown"
		}
		if mod == "" {
			mod = "unknown"
		}
		k := key{prov, mod}
		a, ok := byKey[k]
		if !ok {
			a = &agg{}
			byKey[k] = a
		}
		a.total++
		switch r.Status {
		case "completed", "success":
			a.success++
		case "failed", "errored":
			a.errorRuns++
		}
		if r.TokenUsage != nil {
			a.inTokens += r.TokenUsage.TotalInputTokens
			a.outTokens += r.TokenUsage.TotalOutputTokens
			a.cost += r.TokenUsage.EstimatedCostUSD
		}
	}

	result := make([]ProviderStats, 0, len(byKey))
	for k, a := range byKey {
		result = append(result, ProviderStats{
			ProviderName:      k.provider,
			ModelName:         k.model,
			TotalRuns:         a.total,
			SuccessRuns:       a.success,
			ErrorRuns:         a.errorRuns,
			TotalInputTokens:  a.inTokens,
			TotalOutputTokens: a.outTokens,
			TotalCostUSD:      a.cost,
		})
	}
	// Sort by total runs descending
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].TotalRuns > result[i].TotalRuns {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result, nil
}

// ============================================================================
// Auth Info — retrieve project metadata from the auth/me endpoint.
// This works for project-scoped API tokens (emt_*) that lack projects:read scope.
// ============================================================================

// AuthInfo represents the response from GET /api/auth/me for API tokens.
type AuthInfo struct {
	UserID      string   `json:"user_id"`
	Email       string   `json:"email"`
	Scopes      []string `json:"scopes"`
	Type        string   `json:"type"`
	ProjectID   string   `json:"project_id"`
	ProjectName string   `json:"project_name"`
	OrgID       string   `json:"org_id"`
	TokenID     string   `json:"token_id"`
	TokenName   string   `json:"token_name"`
}

// GetProjectInfo calls GET /api/auth/me and returns project metadata.
// This is preferred over sdkClient.Projects.Get() for project-scoped tokens
// that may not have projects:read scope (e.g. slave mode tokens).
func (b *Bridge) GetProjectInfo(ctx context.Context) (*AuthInfo, error) {
	url := fmt.Sprintf("%s/api/auth/me", b.serverURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create auth/me request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)

	resp, err := bridgeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth/me http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("auth/me: %d %s", resp.StatusCode, resp.Status)
	}

	var info AuthInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode auth/me response: %w", err)
	}
	return &info, nil
}

// ============================================================================
// Session Todo API — /api/v1/agent/sessions/:id/todos
// ============================================================================

// SessionTodo represents a todo item associated with an agent session.
type SessionTodo struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	Content   string    `json:"content"`
	Status    string    `json:"status"`
	Author    string    `json:"author"`
	Order     int       `json:"order"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CreateSessionTodo creates a new todo draft for a session.
func (b *Bridge) CreateSessionTodo(ctx context.Context, sessionID, content, author string) (*SessionTodo, error) {
	body := map[string]any{
		"content": content,
		"author":  author,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal create todo: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/agent/sessions/%s/todos", b.serverURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create todo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := bridgeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create todo http: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		var errResp map[string]any
		json.NewDecoder(httpResp.Body).Decode(&errResp)
		return nil, fmt.Errorf("create todo: %d %v", httpResp.StatusCode, errResp)
	}

	var todo SessionTodo
	if err := json.NewDecoder(httpResp.Body).Decode(&todo); err != nil {
		return nil, fmt.Errorf("decode create todo: %w", err)
	}
	return &todo, nil
}

// ListSessionTodos lists todos for a session, optionally filtered by status.
func (b *Bridge) ListSessionTodos(ctx context.Context, sessionID, status string) ([]SessionTodo, error) {
	url := fmt.Sprintf("%s/api/v1/agent/sessions/%s/todos", b.serverURL, sessionID)
	if status != "" {
		url += "?status=" + status
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("list todos request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)

	httpResp, err := bridgeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list todos http: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		var errResp map[string]any
		json.NewDecoder(httpResp.Body).Decode(&errResp)
		return nil, fmt.Errorf("list todos: %d %v", httpResp.StatusCode, errResp)
	}

	var todos []SessionTodo
	if err := json.NewDecoder(httpResp.Body).Decode(&todos); err != nil {
		return nil, fmt.Errorf("decode list todos: %w", err)
	}
	return todos, nil
}

// UpdateSessionTodo updates the status of a session todo.
func (b *Bridge) UpdateSessionTodo(ctx context.Context, sessionID, todoID, status string) (*SessionTodo, error) {
	body := map[string]any{
		"status": status,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal update todo: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/agent/sessions/%s/todos/%s", b.serverURL, sessionID, todoID)
	req, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("update todo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := bridgeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update todo http: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		var errResp map[string]any
		json.NewDecoder(httpResp.Body).Decode(&errResp)
		return nil, fmt.Errorf("update todo: %d %v", httpResp.StatusCode, errResp)
	}

	var todo SessionTodo
	if err := json.NewDecoder(httpResp.Body).Decode(&todo); err != nil {
		return nil, fmt.Errorf("decode update todo: %w", err)
	}
	return &todo, nil
}

// DeleteSessionTodo deletes a todo from a session.
func (b *Bridge) DeleteSessionTodo(ctx context.Context, sessionID, todoID string) error {
	url := fmt.Sprintf("%s/api/v1/agent/sessions/%s/todos/%s", b.serverURL, sessionID, todoID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("delete todo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)

	httpResp, err := bridgeHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete todo http: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		var errResp map[string]any
		json.NewDecoder(httpResp.Body).Decode(&errResp)
		return fmt.Errorf("delete todo: %d %v", httpResp.StatusCode, errResp)
	}
	return nil
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
