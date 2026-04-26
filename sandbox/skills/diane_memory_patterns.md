# diane_memory_patterns

Find similar/overlapping MemoryFacts via semantic similarity search.

## When to use
- As part of nightly consolidation/dreaming pipeline
- After running diane_memory_decay
- When you suspect duplicate facts exist

## How it works
- Iterates through all active MemoryFacts
- For each fact, searches for semantically similar content via hybrid search
- Clusters facts with similarity scores above the threshold
- When `merge=true`, marks weaker facts as "merged" with a `merged_into` reference

## Usage
Call the tool `diane_memory_patterns` with these parameters:

| Parameter | Required | Description | Default |
|-----------|----------|-------------|---------|
| `similarity_threshold` | No | Score threshold for clustering (0.0-1.0) | `0.85` |
| `merge` | No | Actually merge weaker facts into strongest | `false` |
| `dry_run` | No | Report only, don't modify | `false` |

## Example
```
diane_memory_patterns(similarity_threshold=0.85, merge=false, dry_run=true)
```

Always run with `dry_run=true` first to review clusters, then re-run with `merge=true` to apply.
