#!/bin/bash
# Fast inner test loop — builds once, then re-runs tests without rebuilding.
# Usage: ./test-fast.sh                  # first run (build + test)
#        ./test-fast.sh --quick          # re-run using existing build
#        ./test-fast.sh --live           # include live API tests
#        ./test-fast.sh --coverage       # regenerate coverage report
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
DD="${DERIVED_DATA:-/tmp/DianeDD}"
DEST="${DESTINATION:-platform=macOS,arch=arm64}"

case "${1:-}" in
    --quick)
        echo "=== ⚡ Re-running tests without rebuilding ==="
        time xcodebuild test-without-building \
            -project "$PROJECT_DIR/Diane.xcodeproj" \
            -scheme Diane \
            -destination "$DEST" \
            -derivedDataPath "$DD" \
            -skip-testing:DianeTests/DianeLiveAPITests \
            2>&1 | grep -E "Executed|FAILED|skipped|TEST" | tail -3
        ;;
    --live)
        echo "=== 🌐 Re-running all tests (including live API) ==="
        time xcodebuild test-without-building \
            -project "$PROJECT_DIR/Diane.xcodeproj" \
            -scheme Diane \
            -destination "$DEST" \
            -derivedDataPath "$DD" \
            2>&1 | grep -E "Executed|FAILED|skipped|TEST" | tail -3
        ;;
    --coverage)
        echo "=== 📊 Coverage report ==="
        xcodebuild test-without-building \
            -project "$PROJECT_DIR/Diane.xcodeproj" \
            -scheme Diane \
            -destination "$DEST" \
            -derivedDataPath "$DD" \
            -enableCodeCoverage YES \
            -skip-testing:DianeTests/DianeLiveAPITests \
            2>&1 > /dev/null
        PROFILE=$(ls -t "$DD/Logs/Test"/*.xccovreport 2>/dev/null | head -1)
        if [ -n "$PROFILE" ]; then
            xccov view --report "$PROFILE" 2>/dev/null | head -40
            xccov view --report "$PROFILE" --only-targets 2>/dev/null | grep -i diane
        fi
        ;;
    *)
        echo "=== 🔨 Building for testing ==="
        xcodebuild build-for-testing \
            -project "$PROJECT_DIR/Diane.xcodeproj" \
            -scheme Diane \
            -destination "$DEST" \
            -derivedDataPath "$DD" \
            2>&1 | tail -3 || {
                echo "Build failed — rebuild derived data"
                rm -rf "$DD"
                xcodebuild build-for-testing \
                    -project "$PROJECT_DIR/Diane.xcodeproj" \
                    -scheme Diane \
                    -destination "$DEST" \
                    -derivedDataPath "$DD"
            }

        echo "=== 🧪 Running tests ==="
        time xcodebuild test-without-building \
            -project "$PROJECT_DIR/Diane.xcodeproj" \
            -scheme Diane \
            -destination "$DEST" \
            -derivedDataPath "$DD" \
            -skip-testing:DianeTests/DianeLiveAPITests \
            2>&1 | grep -E "Executed|FAILED|skipped|TEST" | tail -3
        ;;
esac

echo "=== ✅ Done ==="
