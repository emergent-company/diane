#!/bin/bash
# build-dmg.sh — Build, sign, notarize, and package the Diane Companion Mac app as a .dmg
# Usage: ./Scripts/build-dmg.sh [--release] [--notarize]
#
# Environment variables (for CI / notarization):
#   DEVELOPMENT_TEAM     Apple Developer Team ID (e.g. "XXXXXXXXXX")
#   APP_CERT_NAME        Certificate name for app signing (e.g. "Developer ID Application: ...")
#   NOTARIZE_APPLE_ID    Apple ID for notarization
#   NOTARIZE_PASSWORD    App-specific password for notarization
#   NOTARIZE_TEAM_ID     Team ID for notarization

set -euo pipefail

SCHEME="DianeCompanion"
PROJECT="DianeCompanion.xcodeproj"
DERIVED_DATA="build/DerivedData"
ARCHIVE_PATH="build/DianeCompanion.xcarchive"
EXPORT_PATH="build/Export"
DMG_NAME="DianeCompanion"
VERSION=$(defaults read "$(pwd)/DianeCompanion/Info.plist" CFBundleShortVersionString 2>/dev/null || echo "1.0.0")
CONFIGURATION="Release"

echo "==> Building Diane Companion v${VERSION}"

# Step 1: Generate Xcode project (requires xcodegen)
if command -v xcodegen &>/dev/null; then
    echo "==> Generating Xcode project with XcodeGen..."
    xcodegen generate
else
    echo "⚠️  xcodegen not found — using existing .xcodeproj"
fi

# Step 2: Archive
echo "==> Archiving..."
xcodebuild archive \
    -project "${PROJECT}" \
    -scheme "${SCHEME}" \
    -configuration "${CONFIGURATION}" \
    -archivePath "${ARCHIVE_PATH}" \
    -derivedDataPath "${DERIVED_DATA}" \
    DEVELOPMENT_TEAM="${DEVELOPMENT_TEAM:-}" \
    CODE_SIGN_STYLE="${DEVELOPMENT_TEAM:+Manual}" \
    | xcpretty || true

# Step 3: Export .app
echo "==> Exporting .app..."
mkdir -p "${EXPORT_PATH}"
xcodebuild -exportArchive \
    -archivePath "${ARCHIVE_PATH}" \
    -exportPath "${EXPORT_PATH}" \
    -exportOptionsPlist Scripts/ExportOptions.plist

APP_PATH="${EXPORT_PATH}/${SCHEME}.app"

# Step 4: Notarize (optional)
if [[ "${1:-}" == "--notarize" ]]; then
    echo "==> Notarizing..."
    ditto -c -k --keepParent "${APP_PATH}" "${EXPORT_PATH}/DianeCompanion.zip"
    xcrun notarytool submit "${EXPORT_PATH}/DianeCompanion.zip" \
        --apple-id "${NOTARIZE_APPLE_ID}" \
        --password "${NOTARIZE_PASSWORD}" \
        --team-id "${NOTARIZE_TEAM_ID}" \
        --wait
    xcrun stapler staple "${APP_PATH}"
fi

# Step 5: Create .dmg
echo "==> Creating .dmg..."
DMG_PATH="build/${DMG_NAME}-${VERSION}.dmg"
if command -v create-dmg &>/dev/null; then
    create-dmg \
        --volname "Diane Companion" \
        --window-pos 200 120 \
        --window-size 600 400 \
        --icon-size 100 \
        --icon "DianeCompanion.app" 175 190 \
        --hide-extension "DianeCompanion.app" \
        --app-drop-link 425 190 \
        "${DMG_PATH}" \
        "${EXPORT_PATH}/"
else
    # Fallback: plain hdiutil
    hdiutil create -volname "Diane Companion" \
        -srcfolder "${EXPORT_PATH}" \
        -ov -format UDZO \
        "${DMG_PATH}"
fi

echo ""
echo "✓ Built: ${DMG_PATH}"
echo "  Size:  $(du -sh "${DMG_PATH}" | cut -f1)"
