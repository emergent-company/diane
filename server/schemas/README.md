# Embedded Schema Definitions

Schema definitions are stored one file per schema group (e.g. `diane-personal-schema.json`)
and are embedded into the Diane binary at build time via `//go:embed` in
`internal/schema/apply.go`.

## Location

Due to Go's embed restrictions (patterns must not use `..`), the canonical
location is:

```
internal/schema/schemas/*.json
```

## Format

Each file is a JSON array of schema definitions:

```json
[
  {
    "type_name": "MemoryFact",
    "description": "Personal memory facts — preferences, decisions, patterns...",
    "json_schema": {
      "label": "Memory Fact",
      "properties": { ... }
    },
    "enabled": true
  }
]
```

## How to add or modify

1. Edit or create a file in `internal/schema/schemas/`
2. Rebuild Diane: `go build ./cmd/diane/`
3. Apply: `diane schema apply`

## How it works

- At build time, `//go:embed schemas/*.json` embeds all `.json` files
- At runtime, `schema.Apply()` reads them and calls the MP Schema Registry API
- Create or Update (PUT) for each type — idempotent
- Detects changes by comparing JSON schemas, descriptions, and enabled state
