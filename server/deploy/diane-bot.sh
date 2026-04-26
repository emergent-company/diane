#!/bin/bash
# diane-bot — cross-platform process wrapper for the Diane Discord bot
#
# Works on: Linux, macOS, WSL (any Unix with bash)
# Zero external dependencies. Uses PID file from diane bot --pidfile.
#
# Usage:
#   ./diane-bot.sh start             Start the bot (background)
#   ./diane-bot.sh stop              Stop the bot (SIGTERM, graceful)
#   ./diane-bot.sh restart           Restart the bot
#   ./diane-bot.sh status            Check if running
#   ./diane-bot.sh logs              Tail debug log
#   ./diane-bot.sh run               Run in foreground (Ctrl+C to stop)
#
# Config: override via env vars
#   DIANE_BOT_PATH    Path to diane binary (default: /usr/local/bin/diane)
#   DIANE_PIDFILE     Path to PID file   (default: ~/.diane/bot.pid)
#   DIANE_LOGFILE     Path to debug log  (default: ~/.diane/debug.log)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
HOME="${HOME:-/root}"
DIANE_BOT_PATH="${DIANE_BOT_PATH:-/usr/local/bin/diane}"
DIANE_PIDFILE="${DIANE_PIDFILE:-$HOME/.diane/bot.pid}"
DIANE_LOGFILE="${DIANE_LOGFILE:-$HOME/.diane/debug.log}"

pid() {
    if [ -f "$DIANE_PIDFILE" ]; then
        cat "$DIANE_PIDFILE"
    fi
}

is_running() {
    local p
    p="$(pid)"
    [ -n "$p" ] && kill -0 "$p" 2>/dev/null
}

cmd_start() {
    if is_running; then
        echo "✅ Bot already running (PID $(pid))"
        return 0
    fi
    echo "🚀 Starting Diane bot..."
    mkdir -p "$(dirname "$DIANE_PIDFILE")" "$(dirname "$DIANE_LOGFILE")"
    nohup "$DIANE_BOT_PATH" bot \
        --pidfile "$DIANE_PIDFILE" \
        >> "$DIANE_LOGFILE" 2>&1 &
    # Wait for PID file to appear (up to 5s)
    for i in $(seq 1 10); do
        if [ -f "$DIANE_PIDFILE" ]; then
            local p
            p="$(pid)"
            if kill -0 "$p" 2>/dev/null; then
                echo "✅ Bot started (PID $p)"
                return 0
            fi
        fi
        sleep 0.5
    done
    echo "❌ Bot failed to start — check logs:"
    tail -5 "$DIANE_LOGFILE"
    return 1
}

cmd_stop() {
    local p
    p="$(pid)"
    if [ -z "$p" ]; then
        echo "ℹ️  Bot not running (no PID file)"
        rm -f "$DIANE_PIDFILE"
        return 0
    fi
    if ! kill -0 "$p" 2>/dev/null; then
        echo "ℹ️  Bot not running (stale PID $p)"
        rm -f "$DIANE_PIDFILE"
        return 0
    fi
    echo "🛑 Stopping bot (PID $p)..."
    kill -TERM "$p" 2>/dev/null || true
    # Wait up to 15s for graceful shutdown
    for i in $(seq 1 30); do
        if ! kill -0 "$p" 2>/dev/null; then
            echo "✅ Bot stopped"
            rm -f "$DIANE_PIDFILE"
            return 0
        fi
        sleep 0.5
    done
    echo "⚠️  Force killing bot (PID $p)..."
    kill -KILL "$p" 2>/dev/null || true
    rm -f "$DIANE_PIDFILE"
    echo "✅ Bot killed"
}

cmd_restart() {
    cmd_stop
    sleep 1
    cmd_start
}

cmd_status() {
    if is_running; then
        local p
        p="$(pid)"
        echo "✅ Bot running (PID $p)"
        ps -p "$p" -o pid,etime,args --no-headers 2>/dev/null || \
            ps -p "$p" -o pid,etime,command 2>/dev/null || \
            echo "  (process info unavailable)"
    else
        echo "❌ Bot not running"
        if [ -f "$DIANE_PIDFILE" ]; then
            echo "  (stale PID file: $(cat "$DIANE_PIDFILE"))"
            rm -f "$DIANE_PIDFILE"
        fi
        return 1
    fi
}

cmd_logs() {
    if [ -f "$DIANE_LOGFILE" ]; then
        tail -f "$DIANE_LOGFILE"
    else
        echo "No log file at $DIANE_LOGFILE"
    fi
}

cmd_run() {
    echo "🚀 Starting bot in foreground (Ctrl+C to stop)..."
    exec "$DIANE_BOT_PATH" bot --pidfile "$DIANE_PIDFILE"
}

# ── Main ──
case "${1:-run}" in
    start)   cmd_start ;;
    stop)    cmd_stop ;;
    restart) cmd_restart ;;
    status)  cmd_status ;;
    logs)    cmd_logs ;;
    run)     cmd_run ;;
    *)
        echo "Usage: $(basename "$0") <command>"
        echo ""
        echo "Commands:"
        echo "  start      Start the bot (background daemon)"
        echo "  stop       Stop the bot gracefully"
        echo "  restart    Restart the bot"
        echo "  status     Check if running"
        echo "  logs       Tail debug log"
        echo "  run        Run in foreground (default)"
        exit 1
        ;;
esac
