# diane_memory_save

Save a fact to the memory graph as a MemoryFact object for future agent runs.

## When NOT to save (most of the time)

Don't save things just because they're interesting or new. The graph shouldn't be a transcript dump. Skip saving for:
- General observations made during conversation
- Things the user mentioned in passing
- Your own reasoning or intermediate findings
- Repeat facts you've already saved

## When to save (cue-triggered only)

Save only when the user gives explicit signals that something should be remembered:

| User says... | Meaning | Category |
|---|---|---|
| "Remember next time, I use Go" | Explicit instruction to persist | `user-preference` |
| "Never do X again" | Corrective instruction | `decision` |
| "Always use this approach" | Repeatable pattern | `pattern` |
| "You know, that kind of stuff" | General preference/approach | `user-preference` |
| "It's X not Y" or similar correction | Direct correction | `decision` |
| "Add this to my TODO" / "Remind me" | Action item | `action-item` |

If the conversation lacks these cues — **don't save**. The session-end extractor (Tier 2) will handle general fact extraction after the run completes.

## Usage

Call `entity-create` with:
```
entity-create(type="MemoryFact", properties={
    "content": "...",
    "confidence": 0.9,
    "memory_tier": 1,
    "category": "user-preference"
})
```

| Parameter | Description |
|-----------|-------------|
| `content` | The fact text (what to remember) |
| `confidence` | 0.9 for stated, 0.7 for inferred |
| `category` | `user-preference`, `decision`, `pattern`, `entity`, `action-item` |
| `source_session` | Current run/session ID |
| `memory_tier` | 1 for during-session, 2 for extracted, 3 for dreamed |

## Example
```
User: "Remember next time, I prefer Alpine over Ubuntu for containers"
→ entity-create(type="MemoryFact", properties={
    "content": "User prefers Alpine over Ubuntu for Docker containers",
    "confidence": 0.9,
    "category": "user-preference",
    "memory_tier": 1
})
```
