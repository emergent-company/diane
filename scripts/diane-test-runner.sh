#!/bin/bash
# diane-test-runner — Runs the Diane integration test suite
# Reads config from ~/diane/.env.test
# Usage: diane-test-runner [-v] [-test name1,name2]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$HOME/diane/.env.test"

if [ ! -f "$ENV_FILE" ]; then
    echo "❌ $ENV_FILE not found — run 'diane init' or create it manually"
    echo "   Required vars: TEST_BOT_TOKEN, TEST_CHANNEL_ID, DIANE_BOT_ID"
    exit 1
fi

# Source env vars (export them so diane-test can read them)
set -a
source "$ENV_FILE"
set +a

# Validate required vars
missing=""
[ -z "$TEST_BOT_TOKEN" ]   && missing="$missing TEST_BOT_TOKEN"
[ -z "$TEST_CHANNEL_ID" ]  && missing="$missing TEST_CHANNEL_ID"
[ -z "$DIANE_BOT_ID" ]     && missing="$missing DIANE_BOT_ID"

if [ -n "$missing" ]; then
    echo "❌ Missing required vars in $ENV_FILE:$missing"
    exit 1
fi

exec diane-test "$@"
