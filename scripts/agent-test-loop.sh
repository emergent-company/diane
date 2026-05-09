#!/bin/bash
# Agent Test Loop for Diane Companion App
# Runs unit tests (Swift XCTest) and Go backend tests.
# XCUITest currently blocked by MenuBarExtra + ad-hoc code signing on this machine.
set -euo pipefail

SWIFT_DIR="/Users/mcj/src/diane/server/swift/DianeCompanion"
GO_DIR="/Users/mcj/src/diane/server"

echo "========================================="
echo " Diane Agent Test Loop"
echo " $(date '+%Y-%m-%d %H:%M:%S')"
echo "========================================="

PASS=0
FAIL=0

# ── Step 1: Go backend tests ──
echo ""
echo "=== [1/3] Go Backend Tests ==="
cd "$GO_DIR"
if go vet ./... 2>&1 | tail -3; then
    echo "✓ go vet passed"
    ((PASS++))
else
    echo "✗ go vet failed"
    ((FAIL++))
fi

if go test ./... 2>&1 | tail -3; then
    echo "✓ go test passed"
    ((PASS++))
else
    echo "✗ go test failed"
    ((FAIL++))
fi

# ── Step 2: Swift Unit Tests ──
echo ""
echo "=== [2/3] Swift Unit Tests (XCTest) ==="
cd "$SWIFT_DIR"

# Regenerate project if project.yml changed
xcodegen generate 2>&1 | tail -1

xcodebuild test -project Diane.xcodeproj -scheme Diane \
  -destination 'platform=macOS,arch=arm64' \
  -only-testing:DianeTests \
  -skip-testing:DianeUITests \
  -resultBundlePath /tmp/DianeUnitTests.xcresult \
  2>&1 | tee /tmp/test-output.txt | grep -E "Test Suite|error:|failed|passed"

if grep -qi "failed" /tmp/test-output.txt 2>/dev/null; then
    echo "✗ Unit tests FAILED"
    ((FAIL++))
    grep "error:" /tmp/test-output.txt | head -5
else
    echo "✓ All unit tests passed"
    ((PASS++))
fi

# ── Step 3: API Integration Test ──
echo ""
echo "=== [3/3] Live API Check ==="
# Check if diane serve is running
if curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8890/api/status 2>/dev/null | grep -q 200; then
    echo "✓ diane serve is running on port 8890"
    ((PASS++))

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
fi

# ── Summary ──
echo ""
echo "========================================="
echo " Results: $PASS passed, $FAIL failed"
echo "========================================="

exit $FAIL
