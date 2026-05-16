#!/bin/bash
# build-ios.sh — Build, sign, and upload the Diane iOS app to App Store Connect / TestFlight
# Usage: ./Scripts/build-ios.sh [--upload]
#
# Environment variables:
#   VERSION            Version string (e.g. "v1.0.0" or "1.0.0-b3")
#   APPLE_ID           Apple ID for altool
#   APPLE_PASSWORD     App-specific password for altool
#   APPLE_TEAM_ID      Team ID (74LC88G9SC)

set -euo pipefail

SCHEME="Diane"
PROJECT="Diane.xcodeproj"
DERIVED_DATA="build/DerivedData"
ARCHIVE_PATH="build/Diane.xcarchive"
EXPORT_PATH="build/Export"
CONFIGURATION="Release"
UPLOAD="${1:-}"

# Step 1: Generate Xcode project
if command -v xcodegen &>/dev/null; then
    echo "==> Generating Xcode project..."
    xcodegen generate
else
    echo "⚠️  xcodegen not found — using existing .xcodeproj"
fi

# Step 2: Set version
if [ -n "${VERSION:-}" ]; then
    echo "==> Setting version: ${VERSION}"
    MARKETING_VERSION="${VERSION#v}"
    CURRENT_PROJECT_VERSION="${VERSION#v}"
else
    MARKETING_VERSION=$(plutil -extract CFBundleShortVersionString raw "$(pwd)/DianeCompanion/Info.plist" 2>/dev/null || echo "1.0.0")
    CURRENT_PROJECT_VERSION="$MARKETING_VERSION"
fi
VERSION="${VERSION:-${MARKETING_VERSION}}"

echo "==> Building Diane iOS v${VERSION}"

# Patch Info.plist version
plutil -replace CFBundleShortVersionString -string "${MARKETING_VERSION}" "$(pwd)/DianeCompanion/Info.plist" 2>/dev/null || true
plutil -replace CFBundleVersion -string "${CURRENT_PROJECT_VERSION}" "$(pwd)/DianeCompanion/Info.plist" 2>/dev/null || true

# Step 3: Archive
echo "==> Archiving..."
XCODEBUILD_ARGS=(
    -project "${PROJECT}"
    -scheme "${SCHEME}"
    -configuration "${CONFIGURATION}"
    -archivePath "${ARCHIVE_PATH}"
    -derivedDataPath "${DERIVED_DATA}"
    -destination 'generic/platform=iOS'
    DEVELOPMENT_TEAM="74LC88G9SC"
    CODE_SIGN_STYLE="Automatic"
    MARKETING_VERSION="${MARKETING_VERSION}"
    CURRENT_PROJECT_VERSION="${CURRENT_PROJECT_VERSION}"
)

if command -v xcpretty &>/dev/null; then
    xcodebuild archive "${XCODEBUILD_ARGS[@]}" -allowProvisioningUpdates 2>&1 | xcpretty
else
    xcodebuild archive "${XCODEBUILD_ARGS[@]}" -allowProvisioningUpdates
fi

# Step 4: Export for App Store
echo "==> Exporting .app..."
mkdir -p "${EXPORT_PATH}"
xcodebuild -exportArchive \
    -archivePath "${ARCHIVE_PATH}" \
    -exportPath "${EXPORT_PATH}" \
    -exportOptionsPlist DianeCompanion/ExportOptions.plist

APP_PATH="${EXPORT_PATH}/${SCHEME}.ipa"

if [ ! -f "$APP_PATH" ]; then
    echo "❌ Export failed — IPA not found at ${APP_PATH}"
    ls "${EXPORT_PATH}/" 2>/dev/null
    exit 1
fi

echo "✓ Exported: ${APP_PATH} ($(du -sh "${APP_PATH}" | cut -f1))"

# Step 5: Upload to App Store Connect (optional)
if [[ "${UPLOAD}" == "--upload" ]]; then
    echo "==> Uploading to App Store Connect..."
    xcrun altool --upload-app \
        -f "${APP_PATH}" \
        -t ios \
        -u "${APPLE_ID}" \
        -p "${APPLE_PASSWORD}" \
        --output-format xml 2>&1

    echo ""
    echo "✓ Uploaded. Check App Store Connect > TestFlight for the build."
else
    echo ""
    echo "ℹ️  Build complete. To upload, re-run with --upload flag."
    echo "   App: ${APP_PATH}"
fi
