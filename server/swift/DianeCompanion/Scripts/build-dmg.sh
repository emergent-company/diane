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

# Step 1: Generate Xcode project (requires xcodegen) — this also generates Info.plist
if command -v xcodegen &>/dev/null; then
    echo "==> Generating Xcode project with XcodeGen..."
    xcodegen generate
else
    echo "⚠️  xcodegen not found — using existing .xcodeproj"
fi

# Step 2: Set version from environment (injected via xcodebuild flags)
if [ -n "${VERSION:-}" ]; then
    echo "==> Setting version: ${VERSION}"
    MARKETING_VERSION="${VERSION#v}"
    CURRENT_PROJECT_VERSION="${VERSION#v}"
else
    MARKETING_VERSION=$(plutil -extract CFBundleShortVersionString raw "$(pwd)/DianeCompanion/Info.plist" 2>/dev/null || echo "0.0.0-DEVELOPMENT")
    CURRENT_PROJECT_VERSION="$MARKETING_VERSION"
fi
VERSION="${VERSION:-${MARKETING_VERSION}}"

echo "==> Building Diane v${VERSION}"

# Patch version directly into Info.plist after xcodegen generation.
# xcodegen generates the plist with the default from project.yml (1.0), which
# would override any xcodebuild MARKETING_VERSION flag — the plist uses a
# hardcoded value, not a variable reference. Directly setting it here ensures
# the final binary always has the correct version regardless of xcodebuild flags.
plutil -replace CFBundleShortVersionString -string "${MARKETING_VERSION}" "$(pwd)/DianeCompanion/Info.plist" 2>/dev/null || true
plutil -replace CFBundleVersion -string "${CURRENT_PROJECT_VERSION}" "$(pwd)/DianeCompanion/Info.plist" 2>/dev/null || true
echo "==> Patched Info.plist version to ${MARKETING_VERSION}"

if [ "$NO_SIGN" = true ]; then
    # ── Unsigned build ──
    echo "==> Building unsigned (--no-sign)..."
    if command -v xcpretty &>/dev/null; then
        xcodebuild build \
            -project "${PROJECT}" \
            -scheme "${SCHEME}" \
            -configuration "${CONFIGURATION}" \
            -derivedDataPath "${DERIVED_DATA}" \
            MARKETING_VERSION="${MARKETING_VERSION}" \
            CURRENT_PROJECT_VERSION="${CURRENT_PROJECT_VERSION}" \
            CODE_SIGN_IDENTITY="" \
            CODE_SIGNING_REQUIRED=NO \
            CODE_SIGN_ENTITLEMENTS="" \
            CODE_SIGNING_ALLOWED=NO \
            | xcpretty
    else
        xcodebuild build \
            -project "${PROJECT}" \
            -scheme "${SCHEME}" \
            -configuration "${CONFIGURATION}" \
            -derivedDataPath "${DERIVED_DATA}" \
            MARKETING_VERSION="${MARKETING_VERSION}" \
            CURRENT_PROJECT_VERSION="${CURRENT_PROJECT_VERSION}" \
            CODE_SIGN_IDENTITY="" \
            CODE_SIGNING_REQUIRED=NO \
            CODE_SIGN_ENTITLEMENTS="" \
            CODE_SIGNING_ALLOWED=NO
    fi

    APP_PATH=$(find "${DERIVED_DATA}/Build/Products/${CONFIGURATION}" -name "${SCHEME}.app" -type d | head -1)
    if [ -z "$APP_PATH" ]; then
        echo "❌ Could not find built .app in DerivedData"
        exit 1
    fi
    echo "==> Found app at: ${APP_PATH}"
    # Copy to export path for DMG creation
    mkdir -p "${EXPORT_PATH}"
    cp -R "$APP_PATH" "${EXPORT_PATH}/${SCHEME}.app"
    APP_PATH="${EXPORT_PATH}/${SCHEME}.app"

    # Ad-hoc sign the app bundle so macOS Gatekeeper doesn't flag it as "damaged"
    # This creates a CMS signature without an Apple Developer certificate.
    # Users will still get "unverified developer" on first launch (right-click → Open)
    # but NOT the "app is damaged and can't be opened" fatal error.
    echo "==> Ad-hoc signing bundle (unsigned build)..."
    # Must sign all nested dylibs and frameworks first, then the app
    find "${APP_PATH}" -name "*.dylib" -o -name "*.framework" -type d | while read -r f; do
        codesign --force --deep --sign - "${f}" 2>/dev/null || true
    done
    codesign --force --deep --sign - "${APP_PATH}" 2>/dev/null || true
    echo "==> Ad-hoc signature:"
    codesign -dvv "${APP_PATH}" 2>&1 | head -5 || echo "     (signature not present — non-fatal)"
else
    # ── Signed/archived build ──
    echo "==> Archiving..."
    if command -v xcpretty &>/dev/null; then
        xcodebuild archive \
            -project "${PROJECT}" \
            -scheme "${SCHEME}" \
            -configuration "${CONFIGURATION}" \
            -archivePath "${ARCHIVE_PATH}" \
            -derivedDataPath "${DERIVED_DATA}" \
            DEVELOPMENT_TEAM="${DEVELOPMENT_TEAM:-}" \
            CODE_SIGN_STYLE="${DEVELOPMENT_TEAM:+Manual}" \
            | xcpretty
    else
        xcodebuild archive \
            -project "${PROJECT}" \
            -scheme "${SCHEME}" \
            -configuration "${CONFIGURATION}" \
            -archivePath "${ARCHIVE_PATH}" \
            -derivedDataPath "${DERIVED_DATA}" \
            DEVELOPMENT_TEAM="${DEVELOPMENT_TEAM:-}" \
            CODE_SIGN_STYLE="${DEVELOPMENT_TEAM:+Manual}"
    fi

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
