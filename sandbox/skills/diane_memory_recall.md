# diane_memory_recall

Search MemoryFact objects using hybrid semantic+keyword search.

## When to use
- BEFORE answering any question that might depend on prior context
- When the user says "as we discussed" or "remember when"
- When starting a new task that might benefit from past learnings
- To check if a fact already exists before saving a duplicate

## Usage
Call the tool `diane_memory_recall` with these parameters:

| Parameter | Required | Description | Default |
|-----------|----------|-------------|---------|
| `query` | ✅ Yes | Natural language query describing what to find | — |
| `limit` | No | Max results (max 50) | `10` |
| `min_confidence` | No | Minimum confidence filter | `0.0` |
| `category` | No | Filter by specific category | — |
| `tier` | No | Filter by memory tier (1, 2, or 3) | — |

## Example
```
diane_memory_recall(query="user's preferred programming languages and tools", limit=5, min_confidence=0.5)
```

## Guidelines
- Start every new session/run with a recall to re-establish context
- Use specific queries for better results
- Review confidence scores — low-confidence facts might need verification
- Always check for duplicates before saving with diane_memory_save
