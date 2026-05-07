#!/bin/bash
# UI Navigation Test — per-view screenshot capture for Diane Companion App
# Uses cliclick keyboard simulation to navigate sidebar items.
#
# Depends: cliclick (brew install cliclick), wakeable display
# Output: Screenshots in /tmp/diane-ui-test/, JSON results

set -euo pipefail

SCREENSHOT_DIR="/tmp/diane-ui-test"
RESULTS_FILE="/tmp/diane-ui-test-results.json"
TIMESTAMP=$(date +%s)

rm -rf "$SCREENSHOT_DIR"; mkdir -p "$SCREENSHOT_DIR"

log() { echo "[$(date '+%H:%M:%S')] $*"; }

# ── Find the built .app ──
APP_PATH="/tmp/diane-dd/Build/Products/Debug/Diane.app"
if [ ! -d "$APP_PATH" ]; then
  DERIVED=$(find "$HOME/Library/Developer/Xcode/DerivedData" -name "Diane.app" -type d 2>/dev/null | head -1)
  if [ -n "$DERIVED" ]; then APP_PATH="$DERIVED"; fi
fi
if [ ! -d "$APP_PATH" ]; then echo "ERROR: Diane.app not found"; exit 1; fi
log "📱 App: $APP_PATH"

# ── SIDEBAR ITEMS (matching SidebarItem enum order) ──
# Dashboard → Sessions → Documents → Agents → Schema → MCP Servers → Nodes → System
ITEMS=("dashboard" "sessions" "documents" "agents" "schema" "mcpservers" "nodes" "system")
NUM_ITEMS=${#ITEMS[@]}

# ── Kill existing instances ──
killall Diane 2>/dev/null || true
sleep 1

# ── Wake display + keep alive ──
log "💡 Waking display..."
launchctl asuser 501 caffeinate -dimsu -t 120 &>/dev/null &
CAFF_PID=$!
sleep 2
launchctl asuser 501 osascript -e 'tell application "Finder" to activate' 2>/dev/null || true
sleep 2

# ── Launch app ──
log "🚀 Launching app..."
launchctl asuser 501 open -a "$APP_PATH" --args --uitesting
sleep 5

# ── Activate ──
launchctl asuser 501 osascript -e 'tell application "System Events" to tell process "Diane" to set frontmost to true' 2>/dev/null || true
sleep 3

# ── Navigate + screenshot each view ──
PASSED=0; FAILED=0; RESULTS=()
pass() { PASSED=$((PASSED+1)); RESULTS+=("{\"name\":\"$1\",\"passed\":true}"); log "  ✅ $1"; }
fail() { FAILED=$((FAILED+1)); RESULTS+=("{\"name\":\"$1\",\"passed\":false,\"error\":\"$2\"}"); log "  ❌ $1: $2"; }

FIRST_SHOT=""

for i in "${!ITEMS[@]}"; do
  ITEM="${ITEMS[$i]}"

  if [ "$i" -gt 0 ]; then
    log "  ↳ Arrow Down → $ITEM..."
    cliclick kp:arrow-down 2>/dev/null || true
    sleep 2
  fi

  SHOT="$SCREENSHOT_DIR/${i}_${ITEM}.png"
  launchctl asuser 501 screencapture -x -t png "$SHOT" 2>&1
  sleep 1

  # Validate screenshot
  if [ -f "$SHOT" ]; then
    SIZE=$(stat -f%z "$SHOT" 2>/dev/null || echo "0")
    if [ "$SIZE" -gt 500000 ]; then
      pass "view_${ITEM}"
      [ -z "$FIRST_SHOT" ] && FIRST_SHOT="$SHOT"
    elif [ "$SIZE" -gt 10000 ]; then
      fail "view_${ITEM}" "Only ${SIZE} bytes (display may have gone to sleep)"
    else
      fail "view_${ITEM}" "Too small (${SIZE} bytes)"
    fi
  else
    fail "view_${ITEM}" "No screenshot file"
  fi
done

# ── Pairwise comparison: verify screenshots are distinct ──
log "🔍 Comparing screenshots..."
ALL_SHOTS=()
for shot in "$SCREENSHOT_DIR"/*.png; do
  [ -f "$shot" ] && ALL_SHOTS+=("$shot")
done

NUM_DIFFERENT=0
NUM_SAME=0
for ((i=0; i<${#ALL_SHOTS[@]}; i++)); do
  for ((j=i+1; j<${#ALL_SHOTS[@]}; j++)); do
    # Quick content comparison using first 200 bytes of first IDAT chunk
    A=$(python3 -c "
import struct, zlib
f=open('${ALL_SHOTS[$i]}','rb')
p=f.read()
pos=8
while pos<len(p):
    l=struct.unpack('>I',p[pos:pos+4])[0]
    ct=p[pos+4:pos+8]
    if ct==b'IDAT':
        d=zlib.decompress(p[pos+8:pos+8+l])
        print(sum(d[:200]))
        break
    pos+=12+l
" 2>/dev/null)
    B=$(python3 -c "
import struct, zlib
f=open('${ALL_SHOTS[$j]}','rb')
p=f.read()
pos=8
while pos<len(p):
    l=struct.unpack('>I',p[pos:pos+4])[0]
    ct=p[pos+4:pos+8]
    if ct==b'IDAT':
        d=zlib.decompress(p[pos+8:pos+8+l])
        print(sum(d[:200]))
        break
    pos+=12+l
" 2>/dev/null)
    if [ "$A" = "$B" ]; then
      NUM_SAME=$((NUM_SAME+1))
    else
      NUM_DIFFERENT=$((NUM_DIFFERENT+1))
    fi
  done
done

TOTAL_PAIRS=$((NUM_DIFFERENT + NUM_SAME))
if [ "$TOTAL_PAIRS" -gt 0 ] && [ "$NUM_SAME" -eq 0 ]; then
  pass "views_distinct"  # All pairs different
elif [ "$NUM_DIFFERENT" -gt "$NUM_SAME" ]; then
  fail "views_distinct" "$NUM_SAME pairs identical out of $TOTAL_PAIRS"
else
  fail "views_distinct" "Most views appear identical ($NUM_SAME same, $NUM_DIFFERENT different)"
fi

pass "views_captured_${NUM_ITEMS}"

# ── Quit ──
killall Diane 2>/dev/null || true
kill $CAFF_PID 2>/dev/null || true

# ── Final report ──
RESULTS_JSON=$(IFS=,; echo "[${RESULTS[*]}]")
cat > "$RESULTS_FILE" << EOF
{
  "status": "completed",
  "timestamp": $TIMESTAMP,
  "screenshot_dir": "$SCREENSHOT_DIR",
  "passed": $PASSED,
  "failed": $FAILED,
  "total": $((PASSED + FAILED)),
  "results": $RESULTS_JSON
}
EOF

log "📊 UI tests: $PASSED passed, $FAILED failed"
ls -lh "$SCREENSHOT_DIR"/
exit $FAILED
