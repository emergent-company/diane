#!/bin/bash
# dev-build.sh — Build Diane CLI + Swift companion app, install, and launch.
# Usage: ./scripts/dev-build.sh [--no-launch] [--version <tag>]
#
# Does the full dev cycle:
#   1. Build the Go CLI (diane)
#   2. Generate Xcode project via XcodeGen
#   3. Build the Swift companion app (Debug, ad-hoc signed)
#   4. Bundle the local diane CLI into the .app
#   5. Replace /Applications/Diane.app
#   6. Register with LaunchServices
#   7. Launch the app (unless --no-launch)

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SERVER="$ROOT/server"
CMD_DIR="$SERVER/cmd/diane"
SWIFT_DIR="$SERVER/swift/DianeCompanion"
DIST_DIR="$ROOT/dist"
APP_NAME="Diane"
INSTALL_PATH="/Applications/$APP_NAME.app"

# Parse args
NO_LAUNCH=false
VERSION=""
for arg in "$@"; do
  case "$arg" in
    --no-launch) NO_LAUNCH=true ;;
    --version=*) VERSION="${arg#*=}" ;;
  esac
done

# If no version specified, detect from git
SHORT_HASH="$(cd "$ROOT" && git rev-parse --short HEAD 2>/dev/null || echo "0000000")"
if [ -z "$VERSION" ]; then
  GIT_TAG="$(cd "$ROOT" && git describe --tags --always 2>/dev/null || echo "")"
  if [[ "$GIT_TAG" == *-g* ]]; then
    # Dirty tag like v1.4.7-66-g3e2c962 — use dev-<hash>
    VERSION="dev-${SHORT_HASH}"
  else
    VERSION="$GIT_TAG"
  fi
fi

# Strip leading "v" if present (will add back in display but not in version strings)
CLEAN_VERSION="${VERSION#v}"
DISPLAY_VERSION="${CLEAN_VERSION}"

echo "==> 🔨 Diane Dev Build v${DISPLAY_VERSION}"
echo ""

# ── Step 1: Build Go CLI ──
echo "==> [1/7] Building diane CLI..."
mkdir -p "$DIST_DIR"
(cd "$CMD_DIR" && go build \
  -ldflags="-s -w -X main.Version=${VERSION} -X 'main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)'" \
  -o "$DIST_DIR/diane" .)
echo "   ✅ Built: $(ls -lh "$DIST_DIR/diane" | awk '{print $5}') arm64 binary"

# ── Step 2: Generate Xcode project ──
echo "==> [2/7] Generating Xcode project..."
(cd "$SWIFT_DIR" && xcodegen generate 2>&1 | tail -1)
echo "   ✅ Project generated"

# ── Step 2.5: Inject version into Info.plist ──
PLIST="$SWIFT_DIR/DianeCompanion/Info.plist"
CLEAN_VERSION="${VERSION#v}"
plutil -replace CFBundleShortVersionString -string "$CLEAN_VERSION" "$PLIST" 2>/dev/null || \
  /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $CLEAN_VERSION" "$PLIST" 2>/dev/null
plutil -replace CFBundleVersion -string "$CLEAN_VERSION" "$PLIST" 2>/dev/null || \
  /usr/libexec/PlistBuddy -c "Set :CFBundleVersion $CLEAN_VERSION" "$PLIST" 2>/dev/null
echo "   ✅ Version injected: ${CLEAN_VERSION}"

# ── Step 3: Build Swift app ──
echo "==> [3/7] Building Swift companion app..."
BUILD_LOG=$(mktemp)
set +e
xcodebuild \
  -project "$SWIFT_DIR/Diane.xcodeproj" \
  -scheme "$APP_NAME" \
  -configuration Debug \
  -derivedDataPath "$SWIFT_DIR/build/DerivedData" \
  CODE_SIGN_IDENTITY="-" \
  CODE_SIGN_STYLE=Manual \
  DEVELOPMENT_TEAM="" \
  build 2>&1 | tee "$BUILD_LOG" | tail -5
BUILD_EXIT=$?
set -e

if [ $BUILD_EXIT -ne 0 ]; then
  echo "❌ Build failed — full log: $BUILD_LOG"
  exit 1
fi
rm "$BUILD_LOG"
echo "   ✅ Build succeeded"

# ── Step 4: Bundle local CLI into app ──
BUILD_APP="$SWIFT_DIR/build/DerivedData/Build/Products/Debug/$APP_NAME.app"
echo "==> [4/7] Bundling diane CLI into app..."
cp "$DIST_DIR/diane" "$BUILD_APP/Contents/Resources/diane"
chmod +x "$BUILD_APP/Contents/Resources/diane"
echo "   ✅ Bundled: $(ls -lh "$BUILD_APP/Contents/Resources/diane" | awk '{print $5}')"

# ── Step 5: Install to /Applications ──
echo "==> [5/7] Installing to /Applications..."
# Kill running instance and any orphaned diane serve processes
pkill -x "$APP_NAME" 2>/dev/null || true
pkill -f "diane serve" 2>/dev/null || true
sleep 1
# Wait for port to be free
for i in 1 2 3; do
  if lsof -ti :8890 >/dev/null 2>&1; then
    echo "   ⏳ Waiting for port 8890... ($i)"
    sleep 1
  else
    break
  fi
done
rm -rf "$INSTALL_PATH"
cp -R "$BUILD_APP" "$INSTALL_PATH"
# Register with LaunchServices
/System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister -f "$INSTALL_PATH" >/dev/null 2>&1

APP_SIZE=$(du -sh "$INSTALL_PATH" | cut -f1)
echo "   ✅ Installed: $INSTALL_PATH ($APP_SIZE)"

# ── Step 6: Install launchd plist ──
echo "==> [6/7] Installing launchd plist..."
PLIST_SRC="$ROOT/server/deploy/com.emergent-company.diane-serve.plist"
# Use the real user home — not $HOME which may be an agent profile in CI/Hermes
REAL_HOME=$(eval echo "~${SUDO_USER:-$USER}")
PLIST_DST="$REAL_HOME/Library/LaunchAgents/com.emergent-company.diane-serve.plist"
BINARY_PATH="/Applications/$APP_NAME.app/Contents/Resources/diane"
mkdir -p "$REAL_HOME/Library/LaunchAgents"
if [ -f "$PLIST_SRC" ]; then
  sed -e "s|__BINARY_PATH__|$BINARY_PATH|g" \
      -e "s|__HOME__|$REAL_HOME|g" \
      "$PLIST_SRC" > "$PLIST_DST"
  echo "   ✅ Plist installed: $PLIST_DST"
  # Bootstrap with launchd (ignore error if already loaded)
  launchctl bootstrap "gui/$(id -u)" "$PLIST_DST" 2>/dev/null || true
  echo "   ✅ launchd service bootstrapped"
else
  echo "   ⚠️  Plist template not found at $PLIST_SRC — skipping launchd setup"
fi

# ── Step 7: Launch ──
if [ "$NO_LAUNCH" = false ]; then
  echo ""
  echo "==> [7/7] 🚀 Launching Diane..."
  open "$INSTALL_PATH"
fi

echo ""
echo "✅ Done — Diane v${VERSION} built and installed."
