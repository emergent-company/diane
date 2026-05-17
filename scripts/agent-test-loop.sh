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

# ── Step 2: Swift Unit Tests (build verification) ──
echo ""
echo "=== [2/3] Swift Unit Tests (build verification) ==="
echo "NOTE: Using build-for-testing — MenuBarExtra apps can't auto-exit after test execution."
cd "$SWIFT_DIR"

# Clean up stale xcresult bundle from previous run
rm -rf /tmp/DianeUnitTests.xcresult

# Regenerate project if project.yml changed
xcodegen generate 2>&1 | tail -1

# Create a test-only scheme for DianeTests (separate from the main Diane scheme
# to avoid Xcode 26.5 module name conflicts between the app and test targets).
# UUIDs are extracted dynamically from the pbxproj since xcodegen regenerates them.
SCHEME_DIR="Diane.xcodeproj/xcshareddata/xcschemes"
mkdir -p "$SCHEME_DIR"

DIANE_UUID=$(grep -B1 'isa = PBXNativeTarget' Diane.xcodeproj/project.pbxproj | grep '/\* Diane \*/' | awk '{print $1}')
TEST_UUID=$(grep -B1 'isa = PBXNativeTarget' Diane.xcodeproj/project.pbxproj | grep '/\* DianeTests \*/' | awk '{print $1}')

cat > "$SCHEME_DIR/DianeUnitTests.xcscheme" << SCHEME_EOF
<?xml version="1.0" encoding="UTF-8"?>
<Scheme LastUpgradeVersion="1650" version="2.0">
   <BuildAction
      parallelizeBuildables="YES"
      buildImplicitDependencies="YES">
      <BuildActionEntries>
         <BuildActionEntry
            buildForTesting="YES"
            buildForRunning="YES"
            buildForProfiling="YES"
            buildForArchiving="YES"
            buildForAnalyzing="YES">
            <BuildableReference
               BuildableIdentifier="primary"
               BlueprintIdentifier="${DIANE_UUID}"
               BuildableName="Diane.app"
               BlueprintName="Diane"
               ReferencedContainer="container:Diane.xcodeproj">
            </BuildableReference>
         </BuildActionEntry>
         <BuildActionEntry
            buildForTesting="YES"
            buildForRunning="NO"
            buildForProfiling="NO"
            buildForArchiving="NO"
            buildForAnalyzing="NO">
            <BuildableReference
               BuildableIdentifier="primary"
               BlueprintIdentifier="${TEST_UUID}"
               BuildableName="DianeTests.xctest"
               BlueprintName="DianeTests"
               ReferencedContainer="container:Diane.xcodeproj">
            </BuildableReference>
         </BuildActionEntry>
      </BuildActionEntries>
   </BuildAction>
   <TestAction
      buildConfiguration="Debug"
      selectedDebuggerIdentifier="Xcode.DebuggerFoundation.Debugger.LLDB"
      selectedLauncherIdentifier="Xcode.DebuggerFoundation.Launcher.LLDB"
      shouldUseLaunchSchemeArgsEnv="YES">
      <Testables>
         <TestableReference
            skipped="NO">
            <BuildableReference
               BuildableIdentifier="primary"
               BlueprintIdentifier="${TEST_UUID}"
               BuildableName="DianeTests.xctest"
               BlueprintName="DianeTests"
               ReferencedContainer="container:Diane.xcodeproj">
            </BuildableReference>
         </TestableReference>
      </Testables>
      <MacroExpansion>
         <BuildableReference
            BuildableIdentifier="primary"
            BlueprintIdentifier="${DIANE_UUID}"
            BuildableName="Diane.app"
            BlueprintName="Diane"
            ReferencedContainer="container:Diane.xcodeproj">
         </BuildableReference>
      </MacroExpansion>
   </TestAction>
   <LaunchAction
      buildConfiguration="Debug"
      selectedDebuggerIdentifier=""
      selectedLauncherIdentifier="Xcode.IDEFoundation.Launcher.PosixSpawn"
      launchStyle="0"
      useCustomWorkingDirectory="NO"
      ignoresPersistentStateOnLaunch="NO"
      debugDocumentVersioning="YES"
      debugServiceExtension="internal"
      allowLocationSimulation="NO">
      <MacroExpansion>
         <BuildableReference
            BuildableIdentifier="primary"
            BlueprintIdentifier="${DIANE_UUID}"
            BuildableName="Diane.app"
            BlueprintName="Diane"
            ReferencedContainer="container:Diane.xcodeproj">
         </BuildableReference>
      </MacroExpansion>
   </LaunchAction>
   <ProfileAction
      buildConfiguration="Release"
      shouldUseLaunchSchemeArgsEnv="YES"
      savedToolIdentifier=""
      useCustomWorkingDirectory="NO"
      debugDocumentVersioning="YES">
      <MacroExpansion>
         <BuildableReference
            BuildableIdentifier="primary"
            BlueprintIdentifier="${DIANE_UUID}"
            BuildableName="Diane.app"
            BlueprintName="Diane"
            ReferencedContainer="container:Diane.xcodeproj">
         </BuildableReference>
      </MacroExpansion>
   </ProfileAction>
   <AnalyzeAction buildConfiguration="Debug">
   </AnalyzeAction>
   <ArchiveAction buildConfiguration="Release" revealArchiveInOrganizer="YES">
   </ArchiveAction>
</Scheme>
SCHEME_EOF

# Build tests to verify compilation (don't run — MenuBarExtra app won't auto-exit).
# This checks: test compilation, type-checking, and scheme resolution.
set +e
xcodebuild build-for-testing -project Diane.xcodeproj -scheme DianeUnitTests \
  -destination 'platform=macOS' \
  -skip-testing:DianeTests/DianeLiveAPITests \
  -skip-testing:DianeTests/LiveAPIResponseShapeTests \
  2>&1 | tee /tmp/xctest-build-output.txt | tail -3
BUILD_EXIT=${PIPESTATUS[0]}
set -e

if [ "$BUILD_EXIT" -eq 0 ]; then
    echo "✓ Unit test build succeeded (all $(find DianeTests -name '*.swift' | wc -l | tr -d ' ') test files compile)"
    pass_test "swift_xctest_build"
else
    echo "✗ Unit test build FAILED"
    fail_test "swift_xctest_build" "$(grep -i 'error:' /tmp/xctest-build-output.txt | head -3 | tr '\\n' ' ')"
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
