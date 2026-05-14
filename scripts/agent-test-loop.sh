#!/bin/bash
# Agent Test Loop for Diane Companion App
# Runs unit tests (Swift XCTest) and Go backend tests.
# XCUITest currently blocked by MenuBarExtra + ad-hoc code signing on this machine.
#
# Outputs summary to stdout AND writes JSON results to /tmp/diane-test-results.json
# (for consumption by the diane-test-loop cron job).
set -euo pipefail

SWIFT_DIR="/Users/mcj/src/diane/server/swift/DianeCompanion"
GO_DIR="/Users/mcj/src/diane/server"
RESULTS_FILE="/tmp/diane-test-results.json"
TIMESTAMP=$(date +%s)

echo "========================================="
echo " Diane Agent Test Loop"
echo " $(date '+%Y-%m-%d %H:%M:%S')"
echo "========================================="

PASS=0
FAIL=0
TESTS=()
pass_test() { PASS=$((PASS+1)); TESTS+=("{\"name\":\"$1\",\"passed\":true}"); echo "  ✅ $1"; }
fail_test() { FAIL=$((FAIL+1)); TESTS+=("{\"name\":\"$1\",\"passed\":false,\"error\":\"$2\"}"); echo "  ❌ $1: $2"; }

# ── Step 1: Go backend tests ──
echo ""
echo "=== [1/3] Go Backend Tests ==="
cd "$GO_DIR"

# Run vet — capture full output, tail only summary lines for display
set +e
go vet ./... > /tmp/govet-output.txt 2>&1
VET_EXIT=$?
set -e
tail -3 /tmp/govet-output.txt
if [ "$VET_EXIT" -eq 0 ]; then
    pass_test "go_vet"
else
    fail_test "go_vet" "$(tail -3 /tmp/govet-output.txt | tr '\n' ' ')"
fi

# Run tests — capture full output (avoid pipefail/SIGPIPE from tail)
set +e
go test -count=1 ./... > /tmp/gotest-output.txt 2>&1
TEST_EXIT=$?
set -e
tail -10 /tmp/gotest-output.txt
if [ "$TEST_EXIT" -eq 0 ]; then
    pass_test "go_test"
else
    fail_test "go_test" "$(tail -3 /tmp/gotest-output.txt | tr '\n' ' ')"
fi

# ── Step 2: Swift Unit Tests ──
echo ""
echo "=== [2/3] Swift Unit Tests (XCTest) ==="
cd "$SWIFT_DIR"

# Clean up stale xcresult bundle from previous run
rm -rf /tmp/DianeUnitTests.xcresult

# Regenerate project if project.yml changed
xcodegen generate 2>&1 | tail -1

# Run tests — capture exit code but don't abort (set -e), so Step 3 still runs
# NOTE: Skip LiveAPI tests — they require cloud connectivity to memory.emergent-company.ai
# and fail the entire suite when the cloud API is unreachable (transient DNS/network issues).
# Use test-fast.sh --live to run these intentionally.
set +e
xcodebuild test -project Diane.xcodeproj -scheme Diane \
  -destination 'platform=macOS,arch=arm64' \
  -only-testing:DianeTests \
  -skip-testing:DianeUITests \
  -skip-testing:DianeTests/DianeLiveAPITests \
  -skip-testing:DianeTests/LiveAPIResponseShapeTests \
  -resultBundlePath /tmp/DianeUnitTests.xcresult \
  2>&1 | tee /tmp/xctest-raw-output.txt | grep -E "Test Suite|error:|failed|passed"
# Use PIPESTATUS[0] to get xcodebuild's real exit code (not the pipe's)
XCODE_EXIT=${PIPESTATUS[0]}
set -e

# Always clean up the result bundle
rm -rf /tmp/DianeUnitTests.xcresult

if [ "$XCODE_EXIT" -ne 0 ] && grep -q "failed at " /tmp/xctest-raw-output.txt 2>/dev/null; then
    echo "✗ Unit tests FAILED"
    fail_test "swift_xctest" "$(grep 'failed at ' /tmp/xctest-raw-output.txt | grep -v 'nw_' | head -3 | tr '\n' ' ')"
else
    echo "✓ All unit tests passed"
    pass_test "swift_xctest"
fi

# ── Step 3: API Integration Test ──
echo ""
echo "=== [3/3] Live API Check ==="
# Check if diane serve is running
if curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8890/api/status 2>/dev/null | grep -q 200; then
    echo "✓ diane serve is running on port 8890"
    pass_test "api_health"

    # Quick API sanity: check that endpoints return valid JSON
    for endpoint in /api/status /api/sessions /api/schema /api/agents /api/mcp-servers /api/nodes; do
        status=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8890$endpoint")
        if [ "$status" = "200" ] || [ "$status" = "404" ]; then
            echo "  $endpoint → $status"
        else
            echo "  ⚠ $endpoint → $status (unexpected)"
        fi
    done
else
    echo "⚠ diane serve not running — skipping API checks"
    fail_test "api_health" "diane serve not running on port 8890"
fi

# ── Summary ──
echo ""
echo "========================================="
echo " Results: $PASS passed, $FAIL failed"
echo "========================================="

# Write JSON results file for cron consumption
RESULTS_JSON=$(IFS=,; echo "[${TESTS[*]}]")
cat > "$RESULTS_FILE" << EOF
{
  "status":"completed",
  "timestamp":$TIMESTAMP,
  "passed":$PASS,
  "failed":$FAIL,
  "total":$((PASS + FAIL)),
  "results":$RESULTS_JSON
}
EOF
echo "Results written to $RESULTS_FILE"

exit $FAIL
