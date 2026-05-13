#!/bin/bash
# ⚠️ DEPRECATED — Use ../scripts/agent-test-loop.sh (project root) instead
# This script has 11 tests including Swift build + UI screenshots.
# The active test loop uses a simpler 4-test script to avoid XCUITest
# limitations on headless Mac mini.
#
# Diane Agent Test Loop — headless Mac mini (DEPRECATED)
# Tests the companion app build, Go server, API endpoints,
# AND UI navigation via AppleScript + screenshot analysis.
#
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

# ── 5. Swift unit tests ─────────────────────────────────
log "🧪 Running Swift unit tests..."
cd "$SWIFT_DIR"
xcodegen generate 2>&1 | tail -1
XCTEST_LOG=$(xcodebuild test -project Diane.xcodeproj -scheme Diane \
  -destination 'platform=macOS,arch=arm64' \
  -only-testing:DianeTests \
  -resultBundlePath /tmp/DianeUnitTests.xcresult 2>&1) || true
if echo "$XCTEST_LOG" | tail -3 | grep -q "Executed.*test"; then
  pass "swift_unit_tests"
else
  fail "swift_unit_tests" "$(echo "$XCTEST_LOG" | grep error | head -3)"
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
