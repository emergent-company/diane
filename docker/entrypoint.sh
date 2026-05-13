#!/bin/sh
# Diane Test Node entrypoint
#
# Renders diane.yml config from environment variables, then starts
# `diane serve` as a slave MCP relay node.
#
# Required env vars:
#   MEMORY_SERVER_URL   — Memory Platform URL (e.g. https://memory.emergent-company.ai)
#   MEMORY_PROJECT_ID   — Project UUID
#   MEMORY_API_KEY      — Project token (emt_...)
#
# Optional env vars:
#   DIANE_INSTANCE_ID   — Stable relay instance ID (default: test-node-<hostname>)
#   DIANE_API_PORT      — Companion API port (default: 8890)
#   DIANE_LOG_LEVEL     — Log level (default: info)

set -e

# ── Required checks ──────────────────────────────────────────────────────────
if [ -z "$MEMORY_SERVER_URL" ] || [ -z "$MEMORY_PROJECT_ID" ] || [ -z "$MEMORY_API_KEY" ]; then
    echo "ERROR: MEMORY_SERVER_URL, MEMORY_PROJECT_ID, and MEMORY_API_KEY are required"
    exit 1
fi

# ── Defaults ─────────────────────────────────────────────────────────────────
DIANE_INSTANCE_ID="${DIANE_INSTANCE_ID:-test-node-$(hostname)}"
DIANE_API_PORT="${DIANE_API_PORT:-8890}"
DIANE_MODE="${DIANE_MODE:-slave}"

# ── Render config ────────────────────────────────────────────────────────────
export MEMORY_SERVER_URL MEMORY_PROJECT_ID MEMORY_API_KEY DIANE_INSTANCE_ID DIANE_API_PORT DIANE_MODE
mkdir -p /home/diane/.config
envsubst < /etc/diane/config.tmpl.yml > /home/diane/.config/diane.yml

# ── Start serve ──────────────────────────────────────────────────────────────
echo "=== Diane Test Node ==="
echo "  Instance:  ${DIANE_INSTANCE_ID}"
echo "  Server:    ${MEMORY_SERVER_URL}"
echo "  Project:   ${MEMORY_PROJECT_ID}"
echo "  API port:  ${DIANE_API_PORT}"
echo ""

exec diane serve --api-port "${DIANE_API_PORT}"
