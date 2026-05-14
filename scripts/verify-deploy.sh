#!/bin/bash
# Verify Diane deploy landed correctly.
# Usage: ./scripts/verify-deploy.sh [expected-version]
# Example: ./scripts/verify-deploy.sh v1.38.69
set -euo pipefail

EXPECTED_VERSION="${1:-}"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

pass() { echo -e "  ${GREEN}✅${NC} $1"; }
warn() { echo -e "  ${YELLOW}⚠️${NC} $1"; }
fail() { echo -e "  ${RED}❌${NC} $1"; exit 1; }

echo "--- Deploy Verification ---"

# 1. Check binary at the real $HOME/.diane/bin/diane (not Hermes sandbox home)
REAL_HOME=$(eval echo "~$(whoami)")
BINARY="$REAL_HOME/.diane/bin/diane"
if [ ! -f "$BINARY" ]; then
    fail "Binary not found at $BINARY"
fi
pass "Binary exists at $BINARY"

# 2. Check it's executable
if [ ! -x "$BINARY" ]; then
    fail "Binary is not executable: $BINARY"
fi
pass "Binary is executable"

# 3. Check it's a real file (not a broken symlink)
if [ -L "$BINARY" ]; then
    TARGET=$(readlink "$BINARY")
    if [ ! -f "$TARGET" ]; then
        fail "Binary symlink points to non-existent file: $TARGET"
    fi
    pass "Symlink resolves to $TARGET"
fi

# 4. Check version
VERSION=$("$BINARY" version 2>/dev/null | head -1 | grep -o 'v[0-9.]*' || echo "")
if [ -z "$VERSION" ]; then
    warn "Could not determine version from '$BINARY version'"
else
    pass "Version: $VERSION"
    if [ -n "$EXPECTED_VERSION" ]; then
        if [ "$VERSION" != "$EXPECTED_VERSION" ]; then
            fail "Version mismatch: expected $EXPECTED_VERSION, got $VERSION"
        fi
        pass "Version matches expected: $EXPECTED_VERSION"
    fi
fi

# 5. Check PATH resolution
PATH_BINARY=$(which diane 2>/dev/null || echo "not_in_path")
if [ "$PATH_BINARY" = "not_in_path" ]; then
    warn "'diane' not in PATH"
elif [ "$PATH_BINARY" = "$BINARY" ] || [ "$(readlink "$PATH_BINARY" 2>/dev/null || echo "$PATH_BINARY")" = "$BINARY" ]; then
    pass "PATH resolves to the correct binary"
else
    warn "'which diane' → $PATH_BINARY ≠ $BINARY"
fi

# 6. Check that diane serve is running (via launchd or process)
SERVE_PID=$(pgrep -f "diane serve" 2>/dev/null | head -1 || echo "")
if [ -n "$SERVE_PID" ]; then
    SERVE_BIN=$(ps -p "$SERVE_PID" -o command= 2>/dev/null | head -1 || echo "")
    pass "diane serve running (PID $SERVE_PID)"
    if echo "$SERVE_BIN" | grep -q "$BINARY"; then
        pass "diane serve uses the correct binary"
    else
        warn "diane serve binary differs from $BINARY"
        warn "  Running: $SERVE_BIN"
    fi
else
    warn "diane serve not running"
fi

# 7. Check local API responds
API_STATUS=$(curl -sf http://127.0.0.1:8890/api/status 2>/dev/null || echo "")
if [ -n "$API_STATUS" ]; then
    API_VER=$(echo "$API_STATUS" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('version','?'))" 2>/dev/null || echo "?")
    pass "Local API responds (version: $API_VER)"
else
    warn "Local API not responding on :8890"
fi

echo ""
echo "--- Result ---"
if [ -n "$EXPECTED_VERSION" ] && [ "$VERSION" = "$EXPECTED_VERSION" ]; then
    echo -e "${GREEN}✅ Deploy verified: $EXPECTED_VERSION${NC}"
else
    echo -e "${YELLOW}⚠️  Deploy check complete — review warnings above${NC}"
fi
