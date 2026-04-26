# codebase project — GitHub Projects Integration (Design Sketch)

## Proposed CLI Surface

```
codebase project
├── init                   # Create config, set up project mapping
├── sync                   # Graph → GitHub: scenarios become issues
│   ├── --dry-run          # Preview without creating
│   ├── --direction graph  # Graph → GitHub (default)
│   └── --direction github # GitHub → Graph 
├── poll                   # Check for new comments / status changes
├── process                # Run poll + interpret + update graph + report
├── watch                  # Continuous poll+process loop (cron-friendly)
├── status                 # Show project board state + scenario mappings
└── config                 # View/update bridge configuration
```

## Status-Driven Workflow (State Machine)

```
┌──────────┐    move    ┌─────────┐    auto    ┌─────────────┐
│  Backlog  │ ────────→  │  Ready   │ ────────→  │ In Progress  │
└──────────┘            └─────────┘            └──────┬──────┘
                                                      │ agent works:
                                                      │ 1. reads issue + comments
                                                      │ 2. loads scenario from graph
                                                      │ 3. refines steps/properties
                                                      │ 4. updates graph
                                                      │ 5. posts report comment
                                                      │ 6. moves to Review
                                                      ▼
┌──────────┐    review   ┌─────────┐    auto    ┌─────────────┐
│   Done    │ ←────────  │  Review  │ ←───────  │  In Progress │
└──────────┘            └─────────┘            └─────────────┘
                            │ human reviews,
                            │ requests changes
                            ▼
                      ┌─────────────┐
                      │ In Progress  │  ← if changes requested
                      └─────────────┘
```

## Comment Protocol

The agent listens for these patterns in issue comments:

| Pattern | Action | Example |
|---------|--------|---------|
| `@diane-agent update step N to "..."` | Modifies a step description in graph | `@diane-agent update step 3 to "Diane spawns a sub-agent"` |
| `@diane-agent add actor <key>` | Links actor to scenario via `occurs_in` | `@diane-agent add actor act-diane-master` |
| `@diane-agent add step "..." order N` | Creates a new step | `@diane-agent add step "User confirms action" order 5` |
| `@diane-agent status` | Replies with current graph state | `@diane-agent status` |
| `@diane-agent link to <key>` | Links issue to a different scenario | `@diane-agent link to scn-agent-mode` |
| *Free-form* | Agent infers updates from natural discussion | "I think step 2 should also handle webhooks" |

## Graph ↔ Issue Mapping

```
┌─────────────────┐                    ┌──────────────────────┐
│  Memory Graph    │                    │  GitHub Issue        │
│                 │                    │                      │
│  Scenario       │ ── title ────────→ │  Title               │
│  (scn-xxx)      │ ── description ──→ │  Body (with          │
│  - given        │    given/when/then │  <!-- scenario-key:  │
│  - when         │                    │      scn-xxx -->     │
│  - then         │                    │  marker)             │
│  - domain       │ ── label ────────→ │  Labels (domain:act) │
│                 │                    │                      │
│  Step (ordered) │ ── comment ──────→ │  Issue Comment       │
│                  │                    │                      │
│  Actor          │ ── assignee/label→ │  Assignee / Label     │
│                  │                    │                      │
│  Status field   │ ←── status ──────  │  Project Status      │
│                  │                    │  (SingleSelect)      │
└─────────────────┘                    └──────────────────────┘
```

## Implementation Plan

### Phase 1 — POC (this script)
- Python-based bridge using `gh api graphql` + `codebase` CLI
- Manual trigger (`python3 gh-project-bridge.py process`)
- Status-based trigger detection
- Basic comment parsing and reply

### Phase 2 — Native `codebase project` command
- Go implementation in the codebase CLI itself
- `codebase project init / sync / poll / process / watch`
- Built-in auth (reads same creds as codebase)
- Config stored in `.codebase.yml` or project graph

### Phase 3 — Event-driven (optional)
- GitHub webhook → Memory Platform endpoint
- Real-time processing (no polling delay)
- `project webhook install` / `project webhook status`
