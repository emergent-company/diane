#!/bin/bash
# sentry-issues.sh — List recent Sentry issues for the iOS app.
#
# The upload-only token in .sentryclirc (org:ci scope) cannot read events.
# Generate a separate read token at:
#   https://sentry.io/orgredirect/organizations/diane-6t/settings/auth-tokens/
# with scope `event:read` (and optionally `project:read`), then either:
#   - export SENTRY_READ_TOKEN=<token>  (recommended), or
#   - put it in ~/.sentry-read-token (chmod 600)
#
# Usage:
#   Scripts/sentry-issues.sh                     # 20 unresolved issues
#   Scripts/sentry-issues.sh "is:unresolved environment:testflight"
#   Scripts/sentry-issues.sh "" 50               # 50 issues, no filter

set -euo pipefail

QUERY="${1:-is:unresolved}"
MAX="${2:-20}"

TOKEN="${SENTRY_READ_TOKEN:-}"
if [ -z "$TOKEN" ] && [ -f "$HOME/.sentry-read-token" ]; then
    TOKEN=$(cat "$HOME/.sentry-read-token")
fi

if [ -z "$TOKEN" ]; then
    echo "Error: no read token found." >&2
    echo "Set SENTRY_READ_TOKEN or create ~/.sentry-read-token (event:read scope)." >&2
    exit 1
fi

SENTRY_AUTH_TOKEN="$TOKEN" sentry-cli issues list \
    --org diane-6t \
    --project apple-ios \
    --query "$QUERY" \
    --max-rows "$MAX"
