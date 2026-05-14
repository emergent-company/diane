# Companion Version Hardening & Reliability Implementation Plan

> **Goal:** Prevent the "companion lags behind CLI / silent deploy failures / MP errors invisible" class of issues.

**Architecture:** Three independent workstreams: (1) companion version awareness, (2) MP error visibility in companion, (3) deploy verification automation.

**Tech Stack:** Swift (companion), Go (CLI), GitHub Actions (CI)

---

## Task 1: Companion read CLI version at startup

**Objective:** Companion shows a banner when its version lags significantly behind the installed CLI binary.

**Files:**
- Create (later): companion UI banner view
- Modify: `server/swift/DianeCompanion/Sources/CompanionCore/DianeAPIClient.swift` — add `fetchCLIVersion()`
- Modify: `server/swift/DianeCompanion/Sources/Views/MCPServersView.swift` (or likely a more central home view) — add version check on appear

**Step 1: Read existing companion startup code**

Read the companion's main entry point / startup to find where to add a version check.

**Step 2: Add `fetchCLIVersion()` to DianeAPIClient**

```swift
func fetchCLIVersion() async -> String? {
    let data = try? await get("/api/version")
    guard let data else { return nil }
    struct VersionResponse: Decodable { let cli: String? }
    return (try? JSONDecoder().decode(VersionResponse.self, from: data))?.cli
}
```

**Step 3: Ensure serve has a `/api/version` endpoint**

The local API needs a GET /api/version → {"cli": "v1.38.69", "companion": "1.38.50"}.

**Step 4: Add version comparison and banner**

On companion startup / periodic check, compare CLIVersion to app bundle version. If minor version gap > 1, show banner.

---

## Task 2: Surface MP errors in companion UI

**Objective:** When the companion gets 502/500 from Memory Platform, don't silently show "unauthorized"/"no tools" — show the actual error.

**Files:**
- Modify: `server/swift/DianeCompanion/Sources/CompanionCore/DianeAPIClient.swift` — error handling
- Modify: `server/swift/DianeCompanion/Sources/Views/MCPServersView.swift` — error display

**Step 1: Audit current error paths in DianeAPIClient**

Search for all `get()` / `post()` / `fetch*()` calls in the API client and identify which ones route through MP vs. local API.

**Step 2: Add error context to each MP-routed call**

Instead of returning empty data silently, capture the HTTP status and any response body and make them accessible to the UI (e.g. via async throws with a typed error enum, or an error state `@Published var`).

**Step 3: Display errors in the MCP servers list**

Each server card / the "servers" list shows a distinct error state when the backing API call failed.

---

## Task 3: Deploy verification step

**Objective:** After `diane update` or any deploy, verify the binary landed at the real path (not Hermes sandbox) and the version is correct.

**Files:**
- Modify: `server/cmd/diane/serve.go` — add version endpoint if not present
- Create: `scripts/verify-deploy.sh`

**Step 1: Add /api/version endpoint to local API**

```go
// GET /api/version → {"cli": "v1.38.69", "companion": "1.38.50"}
func (h *apiHandlers) handleVersion(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, map[string]any{
        "cli":        Version,
        "companion":  companionVersion,
    })
}
```

**Step 2: Create verify-deploy.sh**

```bash
#!/bin/bash
# Verify diane deploy landed correctly
set -euo pipefail

EXPECTED_VERSION="${1:-}"
REAL_BIN="$HOME/.diane/bin/diane"

# Check binary exists at real path
if [ ! -f "$REAL_BIN" ]; then
    echo "❌ Binary not found at $REAL_BIN"
    exit 1
fi

# Check version
if [ -n "$EXPECTED_VERSION" ]; then
    VERSION=$("$REAL_BIN" version 2>/dev/null | head -1 | grep -o 'v[0-9.]*' || true)
    if [ "$VERSION" != "$EXPECTED_VERSION" ]; then
        echo "❌ Version mismatch: expected $EXPECTED_VERSION, got $VERSION"
        echo "   Binary at: $REAL_BIN"
        echo "   which diane: $(which diane 2>/dev/null || echo 'not in PATH')"
        exit 1
    fi
fi

echo "✅ $REAL_BIN → $VERSION"
```

**Step 3: Integrate into release workflow**

Add to GitHub Actions release step or as a post-deploy checklist item.

---

## Task 4: Auto-build companion on Swift changes (GitHub Actions)

**Objective:** When `server/swift/DianeCompanion/Sources/` changes, auto-build and archive the companion app as a release artifact.

**Files:**
- Create: `.github/workflows/build-companion.yml`

**Step 1: Create workflow**

```yaml
name: Build Companion
on:
  push:
    branches: [main]
    paths:
      - 'server/swift/DianeCompanion/Sources/**'
      - 'server/swift/DianeCompanion/Diane.xcodeproj/**'
  workflow_dispatch:

jobs:
  build:
    runs-on: macos-15
    steps:
      - uses: actions/checkout@v4
      - name: Build Companion
        run: |
          cd server/swift/DianeCompanion
          xcodebuild -project Diane.xcodeproj -scheme Diane \
            -configuration Release \
            -derivedDataPath build/DerivedData \
            -archivePath build/Diane.xcarchive archive
      - name: Upload artifact
        uses: actions/upload-artifact@v4
        with:
          name: Diane-Companion-${{ github.sha }}
          path: server/swift/DianeCompanion/build/Diane.xcarchive
```

---

## Task 5: Companion release pipeline (notarization)

**Objective:** Automate companion export + notarization + DMG creation on tag, so companion ships alongside CLI releases.

**Files:**
- Create: `.github/workflows/release-companion.yml`
- Modify: `server/swift/DianeCompanion/export.plist` (create if missing)

**Step 1: Create exportOptions.plist for the companion**

**Step 2: Create release workflow**

**Step 3: Add notarization step using Apple notarization API**

---

## Execution Order

1. **Task 1** — `/api/version` endpoint + companion banner (fastest, most impactful)
2. **Task 3** — deploy verification script (trivial, prevents sandbox path issues)
3. **Task 2** — MP error visibility (companion-only, no server changes needed)
4. **Task 4** — auto-build companion on pushes (CI change only)
5. **Task 5** — full companion release pipeline
