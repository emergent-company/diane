#!/bin/bash
# release.sh — Tag and create a GitHub release for Diane.
# Usage: ./scripts/release.sh [patch|minor|major|<version>]
#
# Ensures the version bumps past the latest GitHub release (not local tags).
# Handles tag creation, push, and GitHub release creation.
# Requires gh CLI with auth token at ~/.config/gh/hosts.yml.
#
# Examples:
#   ./scripts/release.sh           # auto bump minor (default)
#   ./scripts/release.sh patch     # bump patch (1.35.0 → 1.35.1)
#   ./scripts/release.sh minor     # bump minor (1.35.0 → 1.36.0)
#   ./scripts/release.sh 1.40.0    # explicit version

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO="emergent-company/diane"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

info()  { echo -e "${CYAN}ℹ${NC} $1"; }
ok()    { echo -e "${GREEN}✓${NC} $1"; }
warn()  { echo -e "${YELLOW}⚠${NC} $1"; }
err()   { echo -e "${RED}✗${NC} $1"; }

# ─── Pre-flight checks ────────────────────────────────────────

cd "$ROOT"

# Check gh CLI
if ! command -v gh &>/dev/null; then
  err "gh CLI not found. Install: brew install gh"
  exit 1
fi

# Extract GitHub token
GH_TOKEN=$(grep -A3 'oauth_token' /Users/mcj/.config/gh/hosts.yml 2>/dev/null | head -1 | awk '{print $2}')
if [ -z "$GH_TOKEN" ]; then
  err "No GitHub token found in ~/.config/gh/hosts.yml"
  exit 1
fi
export GH_TOKEN

echo ""
echo "═══════════════════════════════════════════════"
echo "  Diane Release Script"
echo "═══════════════════════════════════════════════"
echo ""

# 1. Check working tree
info "Checking working tree..."
if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
  warn "Working tree is dirty. Showing changes:"
  git status --short 2>/dev/null
  echo ""
  read -p "Continue with dirty tree? [y/N] " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    err "Aborted."
    exit 1
  fi
else
  ok "Working tree is clean"
fi

# 2. Fetch latest tags from origin
info "Fetching tags from origin..."
git fetch --tags origin 2>&1 || warn "git fetch failed (network?)"

# 3. Check GitHub releases (authoritative source)
info "Checking latest GitHub release..."
LATEST_GH=$(gh release list --repo "$REPO" --limit 5 --json tagName 2>/dev/null | \
  python3 -c "import sys,json; releases=[r['tagName'] for r in json.load(sys.stdin)]; releases.sort(key=lambda v: [int(x) for x in v.lstrip('v').split('.')]); print(releases[-1] if releases else 'v0.0.0')" 2>/dev/null || echo "v0.0.0")

info "Latest GitHub release: ${GREEN}$LATEST_GH${NC}"

# 4. Get current commit
CURRENT_COMMIT=$(git rev-parse HEAD)
CURRENT_SHORT=$(git rev-parse --short HEAD)
info "Current commit: ${CYAN}${CURRENT_SHORT}${NC}"

# 5. Determine next version
BUMP="${1:-minor}"

next_version() {
  local ver="$1"
  # Strip 'v' prefix
  ver="${ver#v}"
  IFS='.' read -r major minor patch <<< "$ver"
  case "$BUMP" in
    major) echo "v$((major + 1)).0.0" ;;
    minor) echo "v${major}.$((minor + 1)).0" ;;
    patch) echo "v${major}.${minor}.$((patch + 1))" ;;
    v*)    echo "$BUMP" ;;  # explicit version passed
    *)     echo "v${major}.$((minor + 1)).0" ;;  # default minor
  esac
}

if [[ "$1" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  # Explicit version passed (e.g. "v1.40.0" or "1.40.0")
  NEXT_VERSION="$1"
  [[ "$NEXT_VERSION" != v* ]] && NEXT_VERSION="v$NEXT_VERSION"
  BUMP_LABEL="explicit"
else
  NEXT_VERSION=$(next_version "$LATEST_GH")
  BUMP_LABEL="$BUMP"
fi

echo ""
info "Proposed version: ${GREEN}$NEXT_VERSION${NC} (${BUMP_LABEL} bump from $LATEST_GH)"
echo ""

# 6. Check if tag already exists locally or on remote
if git rev-parse "$NEXT_VERSION" &>/dev/null; then
  warn "Tag $NEXT_VERSION already EXISTS locally"
fi
if git ls-remote --tags origin "$NEXT_VERSION" 2>/dev/null | grep -q "$NEXT_VERSION"; then
  warn "Tag $NEXT_VERSION already EXISTS on remote"
fi

echo ""
read -p "Create release ${CYAN}$NEXT_VERSION${NC} at ${CYAN}$CURRENT_SHORT${NC}? [y/N] " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  err "Aborted."
  exit 1
fi

# ─── Create release ───────────────────────────────────────────

echo ""

# Delete existing tag if it exists locally
if git rev-parse "$NEXT_VERSION" &>/dev/null 2>&1; then
  info "Removing existing local tag $NEXT_VERSION..."
  git tag -d "$NEXT_VERSION"
fi

# Create tag
info "Creating tag $NEXT_VERSION..."
git tag "$NEXT_VERSION"
ok "Tag $NEXT_VERSION created"

# Push tag
info "Pushing tag to origin..."
git push origin "$NEXT_VERSION" 2>&1
ok "Tag $NEXT_VERSION pushed"

# Delete existing release if exists
if gh release view "$NEXT_VERSION" --repo "$REPO" &>/dev/null 2>&1; then
  info "Deleting existing GitHub release $NEXT_VERSION..."
  gh release delete "$NEXT_VERSION" --repo "$REPO" --yes 2>&1 || true
fi

# Create GitHub release
info "Creating GitHub release..."
RELEASE_URL=$(gh release create "$NEXT_VERSION" --repo "$REPO" --generate-notes --title "$NEXT_VERSION" 2>&1)
ok "Release created: ${GREEN}$RELEASE_URL${NC}"

echo ""
echo "═══════════════════════════════════════════════"
echo -e "  ${GREEN}Done!${NC} $NEXT_VERSION released"
echo "═══════════════════════════════════════════════"
echo ""
echo "  URL: $RELEASE_URL"
echo "  Commit: $CURRENT_SHORT"
echo ""
info "CI will now build CLI binaries + companion DMG."
info "The companion app auto-update will pick up the new version."
echo ""
