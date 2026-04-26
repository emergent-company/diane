# diane_memory_decay

Apply confidence decay to unaccessed MemoryFacts using a half-life model.

## When to use
- As a scheduled maintenance task (e.g., daily)
- Before the dreaming/consolidation pipeline
- When you want to clean up stale or outdated memories

## How it works
- Each MemoryFact has a `last_accessed` timestamp
- If it hasn't been accessed within `half_life_days`, its confidence is halved
- Facts whose confidence falls below `delete_threshold` are archived (not deleted)
- Archiving sets status to "archived" — facts are still queryable but excluded from normal recall

## Usage
Call the tool `diane_memory_decay` with these parameters:

| Parameter | Required | Description | Default |
|-----------|----------|-------------|---------|
| `half_life_days` | No | Days after which confidence halves | `30` |
| `delete_threshold` | No | Confidence below which facts are archived | `0.05` |
| `dry_run` | No | Report only, don't modify anything | `false` |

## Example
```
diane_memory_decay(half_life_days=30, delete_threshold=0.05, dry_run=false)
```

Always run with `dry_run=true` first to see what would happen, then run without.
