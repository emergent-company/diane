**Title:** Support ACP (Agent Communication Protocol) for agent run streaming

**Labels:** enhancement, agent-runtime

---

## Summary

Add support for the [Agent Communication Protocol (ACP)](https://agentcommunicationprotocol.dev) to the Memory Platform agent runtime. This would enable real-time streaming of agent run events — tool calls, reasoning, response tokens — to clients like Diane, instead of the current polling-based approach.

## Motivation

Currently, Diane's Discord bot uses a poll loop to check agent run status:

1. `CreateRuntimeAgent` → `TriggerAgentWithInput`
2. Poll `GetProjectRun()` every 2s looking for `completed` / `paused` / `error`
3. Fetch final messages via `GetRunMessages()`

This gives zero visibility into what the agent is doing during execution. Users see only a typing indicator and a 👀 reaction — no tool names, no reasoning, no progress. For runs that take 30–120s, this is a poor UX.

ACP provides the missing layer: a WebSocket-based session protocol that pushes real-time events as the agent executes.

## ACP Event Types Needed

| ACP Event | Purpose | Example |
|---|---|---|
| `tool_call` (ToolCallStart) | Emitted when a tool starts | `search_web`, `read_file` |
| `tool_call_update` (ToolCallProgress) | Progress/completion with result | `search_web → 3 results found` |
| `agent_thought_chunk` | Reasoning/thinking tokens | `"Looking up the codebase..."` |
| `agent_message_chunk` | Response token stream | Streaming the final answer |
| `plan` | Multi-step plan | `Step 1: search, Step 2: analyze` |

## Proposed Implementation

### Option A: ACP-over-WebSocket (recommended)

Add a WebSocket endpoint to the MP agent runtime:

```
GET /api/agents/runs/{runId}/events (WebSocket upgrade)
```

The server pushes ACP `session_update` messages as the run progresses. Clients connect, receive events in real-time, and disconnect when the run completes.

**Integration path in Diane:** Replace the poll loop in `triggerAgentWithContext()` with a WebSocket connection + event channel. Already have the ACP SDK types mapped via `github.com/i-am-bee/acp` (or equivalent Go package).

### Option B: Extended SSE notification stream (minimal)

Add tool lifecycle event types to the existing SSE notification stream at `/api/events/stream`:

- `tool.started` (tool name, args, timestamp)
- `tool.completed` (tool name, result summary, duration)
- `agent.thought` (reasoning text chunk)
- `agent.message_chunk` (response token)

The existing SSE client in `server/internal/events/client.go` already handles reconnection, backoff, and dispatching — it just needs new event types.

Option A is preferred because ACP is becoming an open standard under the Linux Foundation (via BeeAI/A2A), and implementing it positions MP as ACP-compatible for future interop with other agent frameworks.

## Existing Infrastructure

- **SSE client** exists at `server/internal/events/client.go` for Option B
- **ACP Go SDK** available at `github.com/i-am-bee/acp` (Go client)
- **Hermes Agent reference** at `hermes-agent/acp_adapter/events.py` — shows the exact callbacks and event shapes
- **ACP spec:** https://agentcommunicationprotocol.dev/spec/openapi.yaml

## Acceptance Criteria

- Client can subscribe to a run and receive tool lifecycle events in real-time
- Events include: tool name, arguments, result summary, duration
- No polling required — events push as they happen
- Backward compatible: existing `GetRun` / `GetRunMessages` endpoints continue to work
- Graceful disconnection: client disconnect does not cancel the run
