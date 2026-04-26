# Diane Memory Pipeline Implementation

## Architecture

The Memory Platform executor is the session runner. Memory operations happen at two levels:

1. **Agent-side (Tier 1)**: Agents call MP MCP tools during runs to save/retrieve facts
2. **Post-process (Tier 2-3)**: External scripts process completed runs and consolidate memories

```
┌─────────────────────────────────────────────────────────┐
│                    MEMORY PLATFORM                       │
│                                                         │
│  Agent Run (executor)              Graph Store          │
│  ┌─────────────────┐             ┌──────────────────┐   │
│  │ diane-default    │──Tier 1──►│ Session           │   │
│  │  - entity-create │  save     │   ├─ has_message │   │
│  │  - search-hybrid │  facts    │   │   Messages    │   │
│  │  - search-knowl. │◄──retrieve│   └─ MemoryFacts  │   │
│  │  - search-similar│  memories │   └─ SessionSummary│   │
│  └─────────────────┘             └──────────────────┘   │
│                                          ▲              │
│  ┌─────────────────────────────┐          │              │
│  │ Tier 2: Session-End Proc.   │─────────► create       │
│  │ (post-run webhook/script)   │  summary + facts       │
│  └─────────────────────────────┘          │              │
│                                          │              │
│  ┌─────────────────────────────┐          │              │
│  │ Tier 3: Dreaming (cron)     │─────────► consolidate  │
│  │ nightly: decay, pattern     │  merge, hallucinate    │
│  │ detect, consolidate         │                        │
│  └─────────────────────────────┘                        │
└─────────────────────────────────────────────────────────┘
                               ▲
                               │
                    Diane MCP Server
                    (Apple, Google, Finance, etc.)
```

## Tier 1: Per-Turn Fact Capture (Agent-Side)

Already works with existing MP MCP tools. Agents use:

### Save a fact
```
entity-create(type="MemoryFact", properties={
    "content": "User prefers Go over Python for backend services",
    "confidence": 0.85,
    "memory_tier": 1,
    "source_session": "{current_run_id}",
    "source_agent": "diane-default",
    "category": "user-preference",
    "created_at": "{timestamp}",
    "last_accessed": "{timestamp}",
    "access_count": 1
})
```

### Retrieve relevant memories
```
search-hybrid(query="user preferences language", 
              types=["MemoryFact"],
              limit=5)
```

### Agent system prompt guidance
All agents' system prompts include a MEMORY section telling them to:
- Extract and save important facts during conversations
- Always `search-hybrid` for relevant memories before answering
- Save user preferences, decisions, and learned patterns

### MemoryFact Schema (already exists in graph)
| Field | Type | Description |
|-------|------|-------------|
| `content` | string | The fact text |
| `confidence` | float | 0.0-1.0 confidence score |
| `memory_tier` | int | 1=per-turn, 2=session-end, 3=dreamed |
| `source_session` | string | Session this was extracted from |
| `source_agent` | string | Creating agent name |
| `category` | string | user-preference, decision, pattern, entity, etc. |
| `created_at` | datetime | When created |
| `last_accessed` | datetime | When last retrieved |
| `access_count` | int | Number of times retrieved |
| `ttl_days` | int | Days before confidence decay starts (default 30) |

## Tier 2: Session-End Extraction (Post-Run)

Runs after each agent run completes. Two approaches:

### Option A: Agent Hook (MP-side)
MP supports `agent-hook-create` — create a webhook that fires on run completion.
The hook calls a service that processes the run transcript and extracts memories.

### Option B: Polling Script (Diane-side)
A script that periodically checks for completed runs, extracts summaries.

### Extraction Process
```
For each completed run without a SessionSummary:
  1. GET /agent-runs/{run_id}/messages → full transcript
  2. Send transcript to LLM for structured extraction
     - Key facts discussed
     - Decisions made
     - User preferences revealed
     - Action items
     - Topics/tags
  3. For each extracted fact:
     entity-create(type="MemoryFact", properties={...})
     relationship-create(type="extracted_from", src=fact, dst=session)
  4. Create SessionSummary:
     entity-create(type="SessionSummary", properties={
         "run_id": "{run_id}",
         "topic_clusters": ["Go", "concurrency", "web servers"],
         "key_decisions": ["Use goroutines for concurrent requests"],
         "open_questions": ["Should we migrate to gorilla/mux?"],
         "entities_mentioned": ["Diane", "Memory Platform"],
         "action_items": ["Implement Tier 2 extraction"],
         "mood": "productive",
         "word_count": 1234
     })
```

## Tier 3: Dreaming (Nightly Cron)

A Python script runs nightly (via hermes cron). It processes memories across all sessions.

### Dreaming Pipeline
```
1. QUERY: Get all MemoryFact objects
   - Filter by: confidence < 0.6 OR last_accessed > 30 days ago
   
2. DECAY: Reduce confidence for unaccessed memories
   - If last_accessed > 30 days: confidence *= 0.5
   - If confidence < 0.1: flag for archival (hard_delete or archive)

3. PATTERN DETECTION: Find similar/overlapping facts
   - Use search-similar on MemoryFact objects
   - Cluster facts by semantic similarity (threshold: 0.85)
   - For each cluster:
     a. Identify the strongest/most specific fact
     b. Merge weaker facts into it
     c. If contradictory: keep the most recent, flag for review

4. HALLUCINATION: Generate synthetic memories
   - For facts accessed >3 times with confidence >0.9:
     Create derived facts at higher abstraction level
     Example: User likes Go → User prefers compiled languages over interpreted
   - Mark hallucinated facts with confidence = 0.5 (lower)
   - Set memory_tier = 3

5. CONSOLIDATION: Create cross-session patterns
   - Find MemoryFacts that reference the same entity/category
   - Generate a consolidated "generalized fact"
   - Wire relationships between facts (related_to)

6. CLEANUP: Remove ephemeral or duplicated facts
   - Confidence < 0.05: hard_delete
   - Duplicate content (cosine > 0.95): merge access counts, delete duplicate
```

### Cron Schedule
```bash
# Run nightly at 02:00 UTC (good for a "dreaming" session)
0 2 * * * /usr/bin/python3 /root/diane/scripts/diane-dreaming.py
```

## Implementation Status

| Component | Status | File |
|-----------|--------|------|
| MemoryFact graph schema | ✅ Exists on MP | — |
| Tier 1 tools (entity-create, search-hybrid) | ✅ Available on MP | — |
| Tier 1 agent prompt guidance | 🔲 Update system prompts | `registry.go` |
| Tier 2 session-end extraction script | 🔲 To write | `scripts/diane-session-end.py` |
| Tier 3 dreaming cron script | 🔲 To write | `scripts/diane-dreaming.py` |
| Tier 2+3 cron setup | 🔲 Add to hermes cron | — |
| Memory tools in MP MCP | 🔲 Request: `memory_save`, `memory_recall`, `memory_dream` | Feature request |

## MP Feature Requests

Based on this analysis, Diane needs these additions to the Memory Platform MCP server:

1. **`memory_save(content, category, confidence)`** — creates a MemoryFact with auto-linking
2. **`memory_recall(query, types, min_confidence)`** — wraps search-hybrid for MemoryFacts only
3. **`memory_dream_trigger()`** — triggers the dreaming pipeline server-side
