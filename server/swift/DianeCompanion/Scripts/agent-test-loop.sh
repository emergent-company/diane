#!/bin/bash
# Diane Agent Test Loop — headless Mac mini
# Tests the companion app build, Go server, API endpoints,
# AND UI navigation via AppleScript + screenshot analysis.
#
# Usage: bash scripts/agent-test-loop.sh
# Output: JSON results at /tmp/diane-test-results.json

set -euo pipefail

SWIFT_DIR="/Users/mcj/src/diane/server/swift/DianeCompanion"
GO_DIR="/Users/mcj/src/diane/server"
RESULTS_FILE="/tmp/diane-test-results.json"
TIMESTAMP=$(date +%s)

log() { echo "[$(date '+%H:%M:%S')] $*"; }

PASSED=0; FAILED=0; TESTS=()
SIDEBAR_ITEMS=("Dashboard" "Sessions" "Documents" "Agents" "Schema" "MCP Servers" "Nodes" "System")
pass() { PASSED=$((PASSED+1)); TESTS+=("{\"name\":\"$1\",\"passed\":true}"); log "  ✅ PASS"; }
fail() { FAILED=$((FAILED+1)); TESTS+=("{\"name\":\"$1\",\"passed\":false,\"error\":\"$2\"}"); log "  ❌ FAIL: $2"; }

# ── 1. Swift companion app build (with uitesting flag) ─────────
log "🏗️ Building Swift companion app (uitesting)..."
cd "$SWIFT_DIR"
BUILD_DIR="/tmp/diane-dd"
XCODE_LOG=$(xcodebuild build -scheme Diane -destination 'platform=macOS,arch=arm64' -configuration Debug -derivedDataPath "$BUILD_DIR" 2>&1)
if echo "$XCODE_LOG" | tail -5 | grep -q "BUILD SUCCEEDED"; then
  pass "swift_build"
else
  fail "swift_build" "$(echo "$XCODE_LOG" | grep error | head -3)"
fi

# ── 2. Go server build ────────────────────────────────────
log "🏗️ Building Go server..."
cd "$GO_DIR"
if go build -o /tmp/diane-test-server ./cmd/diane 2>/dev/null; then
  pass "go_build"
else
  fail "go_build" "go build ./cmd/diane failed"
fi

# ── 3. Start server on UITESTING port 18990 ────────────────
log "🚀 Starting diane serve on port 18990..."
/tmp/diane-test-server serve --api-port 18990 --pidfile "" > /tmp/diane-srv.log 2>&1 &
SERVER_PID=$!

READY=false
for i in $(seq 1 10); do
  if curl -sf --connect-timeout 1 --max-time 2 -o /dev/null http://127.0.0.1:18990/api/status 2>/dev/null; then
    READY=true; log "✅ Server ready (try $i)"; break
  fi
  sleep 1
done

if $READY; then pass "server_start"; else fail "server_start" "not ready after 10s"; fi

# ── 4. API smoke tests ────────────────────────────────────
for endpoint in status sessions mcp-servers nodes; do
  if curl -sf --connect-timeout 2 --max-time 4 -o /dev/null "http://127.0.0.1:18990/api/$endpoint" 2>/dev/null; then
    pass "api_$endpoint"
  else
    fail "api_$endpoint" "GET /api/$endpoint failed"
  fi
done

# ── 5. UI Navigation Test (AppleScript + per-view screenshots) ─
log "🖥️ Running UI navigation test (${SIDEBAR_ITEMS[*]} views)..."
cd "$SWIFT_DIR"
UI_LOG=$(bash Scripts/ui-test-applescript.sh 2>&1) || true
UI_EXIT=$?

if [ $UI_EXIT -eq 0 ]; then
  pass "ui_navigation"
  # Count per-view passes from the detailed results
  UI_PASSES=$(echo "$UI_LOG" | grep -c "view_" || true)
  UI_FAILS=$(echo "$UI_LOG" | grep -c "view_" || true)
  log "  UI details: $UI_PASSES view checks"
elif [ $UI_EXIT -eq 1 ]; then
  # Partial failure — may still have some valid screenshots
  fail "ui_navigation" "UI test had some failures"
  UI_PASSES=$(echo "$UI_LOG" | grep "✅ view_" | wc -l)
  log "  Partial: $UI_PASSES views passed"
else
  fail "ui_navigation" "UI test crashed (exit $UI_EXIT)"
fi
echo "$UI_LOG"

# ── Analyze per-view screenshots ──────────────────────────
log "🔍 Analyzing per-view screenshots..."
VIEW_COUNT=0
VIEW_PASS=0
VIEW_FAIL=0
for shot in /tmp/diane-ui-test/[0-9]_*.png; do
  [ -f "$shot" ] || continue
  VIEW_COUNT=$((VIEW_COUNT + 1))
  NAME=$(basename "$shot" .png)
  VIEW=${NAME#*_}  # strip index
  SIZE=$(stat -f%z "$shot" 2>/dev/null || echo "0")
  if [ "$SIZE" -gt 500000 ]; then
    VIEW_PASS=$((VIEW_PASS + 1))
    log "  ✅ $VIEW"
  else
    VIEW_FAIL=$((VIEW_FAIL + 1))
    log "  ❌ $VIEW — too small"
  fi
done

if [ "$VIEW_COUNT" -gt 0 ]; then
  pass "ui_views_${VIEW_PASS}of${VIEW_COUNT}"
fi

# ── 6. Stop server ────────────────────────────────────────
kill $SERVER_PID 2>/dev/null; wait $SERVER_PID 2>/dev/null; log "🛑 Server stopped"

# ── 7. go vet ─────────────────────────────────────────────
cd "$GO_DIR"
if go vet ./cmd/... ./internal/... 2>/dev/null; then pass "go_vet"; else fail "go_vet" "vet found issues"; fi

# ── 8. go test ────────────────────────────────────────────
cd "$GO_DIR"
GO_TEST_OUT=$(go test ./internal/... 2>&1) && pass "go_test" || fail "go_test" "$(echo "$GO_TEST_OUT" | tail -5)"

# ── 9. Report ─────────────────────────────────────────────
RESULTS_JSON=$(IFS=,; echo "[${TESTS[*]}]")
cat > "$RESULTS_FILE" << EOF
{
  "status":"completed",
  "timestamp":$TIMESTAMP,
  "passed":$PASSED,
  "failed":$FAILED,
  "total":$((PASSED + FAILED)),
  "results":$RESULTS_JSON
}
EOF

log "📊 $PASSED passed, $FAILED failed"
cat "$RESULTS_FILE" | python3 -m json.tool 2>/dev/null || cat "$RESULTS_FILE"
exit $FAILED
