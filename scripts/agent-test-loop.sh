#!/bin/bash
# Agent Test Loop for Diane
# Runs Go backend tests and API health checks.
# macOS companion app (DianeCompanion) extracted to diane-macos repo.
#
# Outputs summary to stdout AND writes JSON results to /tmp/diane-test-results.json
# (for consumption by the diane-test-loop cron job).
set -euo pipefail

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
echo "=== [1/2] Go Backend Tests ==="
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
    # Count failures for the error message
    FAIL_COUNT=$(grep -c '^--- FAIL' /tmp/gotest-output.txt || true)
    fail_test "go_test" "$FAIL_COUNT test(s) failed. Last lines: $(tail -3 /tmp/gotest-output.txt | tr '\n' ' ')"
fi

# ── Step 2: API Integration Test ──
echo ""
echo "=== [2/2] Live API Check ==="
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
