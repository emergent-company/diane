#!/bin/bash
# build-dmg.sh — Build, sign, notarize, and package the Diane Mac app as a .dmg
# Usage: ./Scripts/build-dmg.sh [--release] [--notarize] [--no-sign]
#
# Environment variables (for CI / notarization):
#   DEVELOPMENT_TEAM     Apple Developer Team ID (e.g. "XXXXXXXXXX")
#   APP_CERT_NAME        Certificate name for app signing (e.g. "Developer ID Application: ...")
#   NOTARIZE_APPLE_ID    Apple ID for notarization
#   NOTARIZE_PASSWORD    App-specific password for notarization
#   NOTARIZE_TEAM_ID     Team ID for notarization

set -euo pipefail

SCHEME="Diane"
PROJECT="Diane.xcodeproj"
DERIVED_DATA="build/DerivedData"
ARCHIVE_PATH="build/Diane.xcarchive"
EXPORT_PATH="build/Export"
DMG_NAME="Diane"
VERSION=$(defaults read "$(pwd)/DianeCompanion/Info.plist" CFBundleShortVersionString 2>/dev/null || echo "1.0.0")
CONFIGURATION="Release"
NO_SIGN=false

# Parse arguments
for arg in "$@"; do
  case "$arg" in
    --no-sign) NO_SIGN=true ;;
    --notarize) ;;  # handled later
    --release) ;;   # default
  esac
done

echo "==> Building Diane v${VERSION}"

# Step 1: Generate Xcode project (requires xcodegen)
if command -v xcodegen &>/dev/null; then
    echo "==> Generating Xcode project with XcodeGen..."
    xcodegen generate
else
    echo "⚠️  xcodegen not found — using existing .xcodeproj"
fi

if [ "$NO_SIGN" = true ]; then
    # ── Unsigned build ──
    echo "==> Building unsigned (--no-sign)..."
    xcodebuild build \
        -project "${PROJECT}" \
        -scheme "${SCHEME}" \
        -configuration "${CONFIGURATION}" \
        -derivedDataPath "${DERIVED_DATA}" \
        CODE_SIGN_IDENTITY="" \
        CODE_SIGNING_REQUIRED=NO \
        CODE_SIGN_ENTITLEMENTS="" \
        CODE_SIGNING_ALLOWED=NO \
        | xcpretty || true

    APP_PATH=$(find "${DERIVED_DATA}/Build/Products/${CONFIGURATION}" -name "*.app" -type d | head -1)
    if [ -z "$APP_PATH" ]; then
        echo "❌ Could not find built .app in DerivedData"
        exit 1
    fi
    echo "==> Found app at: ${APP_PATH}"
else
    # ── Signed/archived build ──
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

    echo "==> Exporting .app..."
    mkdir -p "${EXPORT_PATH}"
    xcodebuild -exportArchive \
        -archivePath "${ARCHIVE_PATH}" \
        -exportPath "${EXPORT_PATH}" \
        -exportOptionsPlist Scripts/ExportOptions.plist

    APP_PATH="${EXPORT_PATH}/${SCHEME}.app"
fi

# Step 4: Notarize (optional)
if [[ "${1:-}" == "--notarize" ]]; then
    echo "==> Notarizing..."
    ditto -c -k --keepParent "${APP_PATH}" "${EXPORT_PATH}/Diane.zip"
    xcrun notarytool submit "${EXPORT_PATH}/Diane.zip" \
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
        --volname "Diane" \
        --window-pos 200 120 \
        --window-size 600 400 \
        --icon-size 100 \
        --icon "Diane.app" 175 190 \
        --hide-extension "Diane.app" \
        --app-drop-link 425 190 \
        "${DMG_PATH}" \
        "${EXPORT_PATH}/"
else
    # Fallback: plain hdiutil
    hdiutil create -volname "Diane" \
        -srcfolder "${EXPORT_PATH}" \
        -ov -format UDZO \
        "${DMG_PATH}"
fi

echo ""
echo "✓ Built: ${DMG_PATH}"
echo "  Size:  $(du -sh "${DMG_PATH}" | cut -f1)"
