# Diane Session Compaction Design

> **Status:** Deprecated — implementation removed 2026-04-26. Diane agents run on Memory Platform ADK runtime which handles context management server-side (see `emergent.memory` executor.go + session_compressor.go). The local compressor package (`internal/session/`) was ported from Hermes Agent but never wired to any consumer and has been deleted. Any context/pruning improvements should target the MP ADK runtime instead.
> **Date:** 2026-04-25  
> **Research sources:** OpenCode (anomalyco/opencode), Aider (Aider-AI/aider), Anthropic Claude compaction API, MemGPT/Letta, LlamaIndex, LLMLingua, GraphRAG

---

## 1. The Problem

Diane runs conversations (sessions) where the user and AI exchange messages. Over time, each session accumulates:
- User messages + AI responses (the conversation)
- Tool call invocations + results (potentially large outputs)
- Embedded files and code diffs

Even with LLMs supporting 128K-200K context windows, sessions need **compaction** — compressing older conversation history while preserving what matters — so the AI doesn't hit the context limit or lose important context (the "lost in the middle" problem).

## 2. Boundary Decision: Diane Side vs Memory Platform Side

**Recommendation: Compaction lives on Diane's side, enabled by Memory Platform primitives.**

### Why Diane handles it:

| Factor | Argument |
|--------|----------|
| **Context window varies per model** | Diane might use Claude (200K), GPT-4 (128K), or a local model (8K). Compaction thresholds are model-specific. |
| **Compaction strategy is application-specific** | A coding assistant compacts differently than a personal assistant. OpenCode and Aider both do it differently. |
| **LLM calls during compaction** | LLM-based summarization is needed for quality. The LLM caller is Diane's domain. |
| **When to compact** | Trigger conditions depend on Diane's session loop, tool output sizes, and user interaction patterns. |
| **Already has the memory foundation** | Diane already has Sessions, Messages, MemoryFacts in the graph. |

### What Memory Platform should provide:

| Primitive | Why |
|-----------|-----|
| **Token counting on Messages** | `message.token_count` so Diane can estimate context size without re-tokenizing |
| **Session metadata fields** | `session.summary`, `session.compacted_at`, `session.token_usage` |
| **Bulk message queries by session** | Fetch all messages for a session for compaction processing |
| **Relationship traversal** | Navigate from Session → Messages → MemoryFacts |
| **Hybrid search across messages** | Retrieve relevant old messages after compaction |
| **Message update/replacement** | Replace old messages with compacted summaries |

---

## 3. How Others Do It (Research Summary)

### OpenCode — The Most Sophisticated

| Aspect | Implementation |
|--------|---------------|
| **Detection** | 3 paths: (1) Token threshold overflow — `isOverflow()` checks total tokens ≥ `usable = context_limit - 20K buffer`, called after each assistant step. (2) Provider `ContextOverflowError`, returns `"compact"` signal to run loop. (3) Manual via POST `/session/:id/summarize` API. |
| **What's preserved** | Tail of 2 user turns verbatim. Everything older is summarized into a structured compaction block. Tool outputs > 2K chars truncated. Old tool outputs pruned with `[Old tool result content cleared]` placeholder. |
| **Compaction format** | Structured markdown: Goal, Constraints & Preferences, Progress (Done/In Progress/Blocked), Key Decisions, Next Steps, Critical Context, Relevant Files. |
| **How it works** | Inserts a `compaction` type part on a user message. The run loop detects it and delegates to `compaction.process()`. A `select()` function determines the head/tail boundary (walks backwards `tail_turns` default 2). Old messages converted with `stripMedia: true` and `toolOutputMaxChars: 2000`. A separate "compaction" agent (with own system prompt) generates the summary. After compaction, hides the compacted messages via `filterCompacted()`. Async `prune()` marks old tool outputs with `time.compacted` so they show as `[cleared]`. |
| **Auto-continue** | After compaction, inserts a synthetic "Continue if you have next steps..." message with `synthetic: true` and `metadata.compaction_continue: true`. |
| **Config** | `auto: true/false`, `tail_turns` (default 2), `preserve_recent_tokens` (4500-18000), `prune: true/false`, `reserved` buffer. |

### Aider — Dual-List + Background Summarization

| Aspect | Implementation |
|--------|---------------|
| **Detection** | After each commit (edit), `move_back_cur_messages()` appends current turn to `done_messages` and kicks off async `summarize_start()` if `done_messages` exceeds `max_chat_history_tokens` (1/16th of model limit, clamped to 1024-8192 tokens). |
| **What's preserved** | Recent messages within `half_max_tokens` budget + the LLM-generated summary of older messages. |
| **Compaction format** | Conversational first-person summary: "I asked you to help with X. You looked at files A, B and made changes. Then we talked about Z..." |
| **How it works** | Dual message lists: `done_messages` (summarizable) + `cur_messages` (active turn). Background thread runs LLM summarization. Reverse-iterate to find split point preserving a tail. Three-level recursive summarization (depth max 3). Falls back to `weak_model` before `main_model` for cost savings. |
| **Other context items** | Repo map (PageRank-based, binary search for token budget), file contents, system prompt — all assembled into `ChatChunks` with fixed order. |

### Anthropic/Claude — Server-Side Compaction (Beta)

| Aspect | Implementation |
|--------|---------------|
| **Detection** | Configurable `trigger: {type: "input_tokens", value: 150000}` (min 50K). |
| **What's preserved** | Summary of older context. Custom `instructions` parameter controls what to preserve (code, variables, decisions). |
| **Compaction format** | `compaction` content block type returned in API response. Client passes it back in subsequent requests — API drops all prior content. |
| **How it works** | `context_management: { edits: [{ type: "compact_20260112", trigger: {...}, instructions: "focus on code" }] }`. Supports `pause_after_compaction` to inject preserved items before continuing. Accumulates multiple compaction blocks in chain. Streaming support with `content_block_start` events. Returns compaction counter for total budget enforcement. |

### MemGPT/Letta — LLM-Driven Self-Management

| Aspect | Implementation |
|--------|---------------|
| **Detection** | When context window approaches limit, the system sends an interrupt. |
| **What's preserved** | LLM itself decides via function calls. Core memory (always in context) + archival memory (vector-searchable). |
| **How it works** | LLM calls `conversation_search()`, `archival_memory_insert()` etc. to manage its own memory. OS-inspired paging. |

### LlamaIndex — Priority-Based Memory Blocks

| Aspect | Implementation |
|--------|---------------|
| **Trigger** | When short-term chat history exceeds `token_limit * chat_history_token_ratio` (default 30K * 0.7 = 21K), oldest messages flushed to memory blocks. |
| **What's preserved** | StaticMemoryBlock (never truncated, priority 0), FactExtractionMemoryBlock (extracted facts, priority 1+), VectorMemoryBlock (vector-searchable chunks, priority 2+). |
| **Truncation** | Blocks are truncated by priority order (lower priority first). Priority 0 = never removed. |

### LLMLingua Series — Token-Level Compression

Token-level pruning via small language model scoring. Achieves up to 20x compression. LLMLingua-2 uses a BERT-level classifier (GPU-inference). Not directly applicable unless Diane wants token-level compression (expensive, lossy).

### GraphRAG — Context via Graph Modularization

Uses knowledge graphs to modularize context. Entity extraction → relationship graph → community summarization. Diane already has the graph infrastructure — this is complementary.

---

## 4. Proposed Architecture for Diane

### High-Level Design

Diane's session compaction is a **two-tier system**:

```
Session Loop
  │
  ├─ Tier 1: Fast Compaction (sliding window + token budget)
  │   When: Input tokens > 70% of model's context window
  │   What: Prune old tool outputs, truncate to sliding window
  │   Cost: Zero LLM calls (pure arithmetic)
  │
  └─ Tier 2: Deep Compaction (LLM summarization)
      When: After Tier 1, still > 90% of context window
      What: LLM summarizes older conversation turns
      Cost: 1 LLM call per compaction cycle
      Result: Structured summary replaces condensed messages in context
```

### The Data Flow

```
Messages in Graph → Session Runner (Go HTTP server)
                         │
                    Session Loop
                         │
                    ╔═══════════════════════════╗
                    ║  Pending Context Assembly ║
                    ║  ┌──────────────────┐    ║
                    ║  │ Core (never cut) │    ║  ← system prompt, user profile
                    ║  ├──────────────────┤    ║
                    ║  │ Compaction Block │    ║  ← LLM-generated structured summary
                    ║  ├──────────────────┤    ║
                    ║  │ Recent Messages  │    ║  ← last N turns verbatim
                    ║  └──────────────────┘    ║
                    ╚═══════════════════════════╝
                              │
                         LLM Call
```

### Trigger Conditions

| Trigger | Tier | Threshold | Notes |
|---------|------|-----------|-------|
| `input_tokens_pct` | 1 | ≥ 70% of context window | Fast prune/defer |
| `input_tokens_pct` | 2 | ≥ 90% of context window | LLM summarization |
| `message_count` | 2 | Every 50 messages | Periodic quality compaction |
| `explicit` | 2 | User command or API call | Manual trigger |
| `compaction_needed` from LLM | 2 | Provider returns context overflow | Error-driven |
| `tool_output_size` | 1 | Single tool output > 8K tokens | Auto-prune long tool output |

### What Gets Preserved (Priority Order)

| Priority | Content | When Cut |
|----------|---------|----------|
| P0 | System prompt, user profile, active task context | Never |
| P1 | Last 2-3 user turns (tail) | Never during current session |
| P2 | MemoryFacts (extracted from conversation) | Never — always available via hybrid search |
| P3 | Most recent compaction block | Always included |
| P4 | Older compaction blocks | Cut first during Tier 1 overflow |
| P5 | Older tool outputs (large) | Pruned via `[content cleared]` placeholder |
| P6 | Older conversation text | Summarized into compaction block |

### Compaction Block Format

```json
{
  "type": "compaction",
  "version": 1,
  "created_at": "2026-04-25T12:00:00Z",
  "summary": {
    "goal": "What the user was trying to do",
    "state": "Current project state, what's built so far",
    "progress": {
      "completed": ["Feature X implemented", "Bug Y fixed"],
      "in_progress": ["Feature Z being designed"],
      "blocked": ["Waiting on approval"]
    },
    "decisions": [
      {"what": "Use Go over Python", "why": "Performance requirements"},
      {"what": "Postgres for storage", "why": "ACID compliance"}
    ],
    "user_preferences": [
      "Prefers concise error messages",
      "Uses tabs over spaces"
    ],
    "critical_context": [
      "Server runs at memory.emergent-company.ai",
      "Diane is deployed as Docker containers"
    ]
  },
  "statistics": {
    "messages_compacted": 47,
    "tokens_before": 85600,
    "tokens_after": 3200,
    "compression_ratio": "26.75x"
  }
}
```

### How Compaction Markers Work in the Message Stream

```
Messages (in Memory Platform Graph):

  Msg 1  ─── older conversation
  Msg 2      (will be compacted)
  Msg 3
  Msg 4  ─── compaction boundary
  Msg 5  ─── "What did we do so far?"   (compaction marker, type=compaction)
  Msg 6  ─── [Compaction Summary]       (summary, summary=true)
  Msg 7
  Msg 8  ─── recent conversation
  Msg 9      (kept verbatim)
```

During context assembly, messages before the compaction boundary are filtered out, replaced by:
1. The compaction marker ("What did we do so far?")
2. The compaction assistant response (the structured summary)

This is the OpenCode approach — instead of deleting anything, insert markers that act as "continue from here" boundaries.

### Interaction with Diane's Existing Memory Algorithm

| Algorithm Tier | Relation to Compaction |
|----------------|----------------------|
| Tier 1 (Real-time fact extraction) | Runs during conversation, before compaction. Facts extracted independently. |
| Tier 2 (Session-end extraction) | Runs when session ends. Compaction blocks are treated as session artifacts. |
| Tier 3 (Dreaming - nightly consolidation) | Factories compaction blocks for long-lived sessions that didn't hit context limits. Can also detect whether compaction was effective. |

### Tool Output Pruning

At the end of each compaction cycle, prune old tool outputs asynchronously:

- Keep last 2 user turns' tool outputs in full (~40K tokens protection)
- Everything older: keep first 200 chars, replace rest with `[Old tool result content cleared]`
- This recovers ~20K-40K tokens per compaction cycle without losing the conversation structure

---

## 5. Implementation Plan

### Phase 1: Context Budget Tracking (P0)

**Where:** Diane Master (Go server)

```go
type ContextBudget struct {
    ModelLimit        int   // e.g., 200000 for Claude
    ReserveBuffer     int   // e.g., 20000 (space for compaction prompts)
    TailTurns         int   // messages to always preserve (default 2)
    MaxHistoryTokens  int   // max tokens for history (model_limit - system_prompt - tail_budget - reserve)
}
```

- Add token counting to every message stored in the graph
- Track running token totals per session
- Expose `session.usage` as a struct with input/output/cache tokens

### Phase 2: Fast Compaction (P1, zero LLM cost)

**Implemented as an MCP tool or server-side logic:**

1. Check `input_tokens > 0.7 * model_limit` after each message
2. Prune tool outputs > 8K tokens to `[Tool output truncated: N chars]`
3. If still over budget, defer to Deep Compaction

### Phase 3: Deep Compaction (P1, LLM-powered)

**The core algorithm:** (modeled on OpenCode)

1. `select(msgs, tail_turns=2)` → determine head/tail boundary
2. Convert head messages to model messages with `stripMedia: true`, `toolOutputMaxChars: 2000`
3. Build compaction prompt with any previous compaction blocks as context
4. Call LLM (can use a cheaper model if configured) to generate structured summary
5. Insert compaction marker user message + compaction assistant response into session
6. Mark compacted messages as `status: "compacted"` in the graph (don't delete)
7. Run async tool output prune on old messages

### Phase 4: Memory Platform Integration (P1, what to ask for)

**What Diane needs from Memory Platform:**

1. **`Message.token_count` field** — auto-populated on creation
2. **`Session.status` → "compacted"** — tracking field
3. **Ability to update message properties** (to set `status: compacted`) 
4. **Efficient message-by-session query** — `GET /session/:id/messages?limit=100&offset=...`

These are small additions to the Message schema (add a `token_count` numeric field) and ensuring property updates work.

---

## 6. Key Design Decisions

### Decision 1: Insert Markers, Don't Delete

**Chosen approach:** Insert compaction markers as first-class messages in the graph. Never delete conversation history.

**Why:** Preserves the full conversation for future analysis (dreaming, pattern detection). Compaction is a *view* optimization, not a data loss event.

### Decision 2: Structured Summary Format

**Chosen approach:** Structured JSON/Markdown (Goal, Progress, Decisions, Critical Context) rather than free-form prose.

**Why:** Structured format is:
- More reusable across sessions
- Easier to update incrementally
- Machine-parseable for the dreamer pipeline
- Better at preserving specific facts (filenames, decisions, config values)

### Decision 3: Two-Tier with Async Pruning

**Chosen approach:** Fast path (arithmetic pruning) + deep path (LLM summary). Async tool output pruning after compaction completes.

**Why:** Covers both common cases (moderate overflow → fast path) and rare cases (extreme overflow → deep path). Async pruning avoids blocking the main session loop while still freeing significant context budget (20K-40K tokens).

### Decision 4: Separate Compaction Agent

**Chosen approach:** Use a specialized system prompt for compaction (not the main conversation prompt).

**Why:** The compaction LLM call has a different goal: summarize faithfully, not converse. A specialized prompt gives better results. Can also use a cheaper/faster model for compaction.

### Decision 5: Configurable by User

**Why different sessions need different compaction:**

| Parameter | Default | Reasoning |
|-----------|---------|-----------|
| `tail_turns` | 2 | Preserves immediate context for continuity |
| `auto_compact` | true | Most users benefit from auto-compaction |
| `compact_model` | same as session | Quality over cost for critical compaction |
| `preserve_recent_tokens` | 4500-18000 | Adaptive based on model's context window |
| `prune_tool_outputs` | true | Significant savings with minimal loss |

---

## 7. Comparison: Diane vs Other Systems

| Aspect | OpenCode | Aider | Claude API | Diane (proposed) |
|--------|----------|-------|------------|-------------------|
| **Tier 1 (fast path)** | Overflow check, async prune | None (no fast path) | Server-side only | Token % check + tool output prune |
| **Tier 2 (deep path)** | LLM summarization, compaction markers | LLM summarization, dual lists | Server-side compaction block | LLM summarization + compaction markers |
| **Tail preservation** | 2 turns verbatim | Recent within budget | Server configurable | 2-3 turns verbatim |
| **Tool output handling** | Pruned async to `[cleared]` | Inline, user-confirmed | Server-side | Pruned async to `[truncated]` |
| **Persistent storage** | SQLite (embedded) | Filesystem | Server-side | Memory Platform graph |
| **Compaction triggers** | Auto + manual + error | Auto after edit | Auto (threshold) + manual | Auto + manual + error |
| **LLM costs** | 1 call per compaction | 1 call per compaction | Server-side (free) | 1 call per compaction |
| **Data structure** | CompactionPart on Message | done_messages list | compaction block | Compaction marker Message + structured summary |

---

## 8. Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| **Compaction loses important context** | Structured format with explicit sections (decisions, preferences, critical context). User can disable auto-compact. |
| **Compaction LLM call adds latency** | Use async processing. Compaction can happen between user messages. User interaction not blocked. |
| **Multiple compaction blocks accumulate** | Limit to 1 active compaction block. New compaction replaces old one (building on previous summary). |
| **Graph grows with compaction markers** | Markers are tiny (1 message). Worth the trade-off for persistent reusable context. |
| **MemoryFacts out of sync with compaction** | MemoryFacts are independent of compaction. Tier 2 (session-end) and Tier 3 (dreaming) reconcile them. |

---

## 9. What Needs to Be Built

### Diane Master (Go MCP server)
- [ ] Context budget tracking per model/session
- [ ] Token counting on messages when stored
- [ ] Fast compaction (tool output pruning)
- [ ] Deep compaction (LLM summarization)
- [ ] Compaction marker message insertion
- [ ] Context assembly with compacted sections filtered
- [ ] Async tool output pruning after compaction

### Memory Platform (needs from MP)
- [ ] `Message.token_count` as auto-populated field
- [ ] Efficient bulk message query by session
- [ ] Property updates on Message objects (for `status: compacted`)
- [ ] Optional: session-level summary/compaction metadata fields

---

---

## 10. Go Port — Status

The Hermes Agent context compressor has been ported to Go in the `internal/session/` package:

| File | Lines | Purpose |
|------|-------|---------|
| `types.go` | 94 | `CompressorConfig`, `Message`, `ToolCall`, `CompressionResult`, `CompressionStats` |
| `compressor.go` | 764 | Core algorithm: `Compress()`, `pruneOldToolResults()`, `findTailCutByTokens()`, `generateSummary()`, `sanitizeToolPairs()`, anti-thrashing |
| `tool_summary.go` | 79 | `SummarizeToolResult()` — informative 1-liners for 25+ tools, `TruncateToolCallArgsJSON()` — safe JSON truncation |
| `jsonutil.go` | 144 | Argument parsing, exit code extraction, string shrinking helpers |
| `summarizer.go` | 79 | `Summarizer` interface with `MemorySummarizerCaller()` for Memory Platform integration |
| `compressor_test.go` | 477 | 17 tests: unit + integration + boundary cases |

**Status:** ✅ All 17 tests pass. `go build` + `go vet` clean.

**What's done:**
- Tool output pruning with informative 1-liners (replaces `[terminal] ran \`npm test\` -> exit 0, 47 lines`)
- Deduplicaton of identical tool outputs (MD5 hash comparison)
- Safe tool_call argument JSON truncation (valid JSON preserved)
- Token-budget tail protection (adaptive, scales with context window)
- Head protection (system prompt + first exchange)
- Anti-thrashing (skips after 2 ineffective compressions)
- Failure cooldown and fallback
- Tool pair sanitization (removes orphaned results, inserts stubs)
- Structured summary prompt generation (12 sections)
- Iterative summary updates (previous summary preserved across compressions)
- Focus topic support for guided compression

**What's left:**
1. **Wire a Summarizer** — `MemorySummarizerCaller()` wraps the Memory Platform's streaming chat API; once the session runner HTTP server is built, this can be wired from `Bridge.StreamChat`
2. **Integrate into the session loop** — call `Compressor.ShouldCompress()` after each LLM response, then `Compressor.Compress()` when threshold exceeded
3. **Persist compaction state** — store `previousSummary` and compression stats in the Memory Platform graph alongside Sessions/Messages
4. **Extend tool summaries** — as Diane gains new tools, add entries to the switch in `SummarizeToolResult()`

## 11. Recommendation

**Build this on Diane's side, now.** The architecture is well-understood (borrowed from OpenCode's battle-tested approach), the key primitives from Memory Platform are already sufficient (graphs, messages, relationships, hybrid search), and the design fits naturally into Diane's existing three-tier memory algorithm.

The Memory Platform should add two small features to make this smoother:
1. `Message.token_count` (auto-populated)
2. Efficient session-to-messages queries

But neither is blocking — we can implement everything with what exists today.
