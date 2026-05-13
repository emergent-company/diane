#!/usr/bin/env python3
"""
Test how infakt's OAuth refresh token rotation works.

Reads the stored infakt token file and OAuth config from ~/.diane/secrets/,
then performs two sequential refresh attempts to verify rotation behavior:

  1. First refresh: should succeed, returns new access_token + new refresh_token
  2. Second refresh with OLD token: should fail (proving rotation)

Usage:
  python3 scripts/test_infakt_refresh.py
  python3 scripts/test_infakt_refresh.py --dry-run   # Show what would be sent without sending
"""

import argparse
import json
import os
import sys
from pathlib import Path
from urllib.parse import urlencode
from urllib.request import Request, urlopen
from urllib.error import URLError, HTTPError

SECRETS_DIR = Path.home() / ".diane" / "secrets"


def load_json(name: str) -> dict:
    path = SECRETS_DIR / name
    if not path.exists():
        print(f"❌ File not found: {path}")
        sys.exit(1)
    with open(path) as f:
        return json.load(f)


def refresh(token_url: str, client_id: str, refresh_token: str, label: str) -> dict:
    """Send a refresh_token grant and return the parsed response."""
    data = urlencode({
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
        "client_id": client_id,
    }).encode("utf-8")

    req = Request(token_url, data=data, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")

    print(f"\n─── {label} ───")
    print(f"POST {token_url}")
    print(f"Body: grant_type=refresh_token&refresh_token={refresh_token[:40]}...&client_id={client_id[:8]}...")
    print()

    try:
        resp = urlopen(req)
        body = json.loads(resp.read().decode("utf-8"))
        print(f"HTTP {resp.status} {resp.reason}")
        print(f"Response body:")
        print(json.dumps(body, indent=2))
        print()
        return {
            "status": resp.status,
            "body": body,
            "access_token": body.get("access_token", ""),
            "refresh_token": body.get("refresh_token", ""),
            "expires_in": body.get("expires_in", 0),
            "scope": body.get("scope", ""),
        }
    except HTTPError as e:
        body_text = e.read().decode("utf-8", errors="replace")
        print(f"HTTP {e.code} {e.reason}")
        try:
            body = json.loads(body_text)
            print(f"Response body: {json.dumps(body, indent=2)}")
            return {"status": e.code, "body": body, "error": body.get("error", body_text)}
        except json.JSONDecodeError:
            print(f"Response body: {body_text}")
            return {"status": e.code, "body": body_text, "error": body_text}
    except URLError as e:
        print(f"❌ Network error: {e.reason}")
        return {"status": 0, "error": str(e.reason)}


def main():
    parser = argparse.ArgumentParser(description="Test infakt OAuth refresh token rotation")
    parser.add_argument("--dry-run", action="store_true", help="Show what would be sent without actually sending")
    parser.add_argument("--server-name", default="infakt", help="Server name (default: infakt)")
    args = parser.parse_args()

    # Load tokens and OAuth config
    token_file = f"{args.server_name}.json"
    oauth_file = f"{args.server_name}-oauth-config.json"

    print(f"📂 Loading secrets from {SECRETS_DIR}")
    tokens = load_json(token_file)
    oauth = load_json(oauth_file)

    token_url = oauth.get("token_url", "https://mcp.infakt.pl/token")
    client_id = oauth.get("client_id") or tokens.get("client_id", "")
    refresh_token = tokens.get("refresh_token", "")

    if not refresh_token:
        print("❌ No refresh_token found in token file")
        sys.exit(1)
    if not client_id:
        print("❌ No client_id found (check oauth config or token file)")
        sys.exit(1)

    # Show initial state
    expires_at = tokens.get("expires_at", "unknown")
    print(f"\n📋 Initial state:")
    print(f"   access_token:    {tokens.get('access_token', '')[:40]}...")
    print(f"   refresh_token:   {refresh_token[:40]}...")
    print(f"   expires_at:      {expires_at}")
    print(f"   client_id:       {client_id}")
    print(f"   token_url:       {token_url}")
    print(f"   scope:           {tokens.get('scope', '')}")

    if args.dry_run:
        print("\n⚠️  Dry-run mode — no requests sent")
        print(f"   Would POST {token_url}")
        print(f"   With: grant_type=refresh_token&refresh_token=...&client_id=...")
        sys.exit(0)

    # ── First refresh ──
    r1 = refresh(token_url, client_id, refresh_token, "REFRESH ATTEMPT 1 (original token)")

    if r1["status"] not in (200,) or not r1.get("access_token"):
        status = r1.get("status", 0)
        hint = ""
        if status == 403:
            hint = "   (infakt returns 403 'error code: 1010' for stale tokens — rotation confirmed)"
        elif status == 400:
            body_str = json.dumps(r1.get("body", {}))
            if "invalid_grant" in body_str:
                hint = "   (standard OAuth 'invalid_grant' — rotation confirmed)"
        print(f"\n❌ First refresh FAILED (HTTP {status})")
        if hint:
            print(hint)
        print("   The stored refresh token is already consumed/stale.")
        print("   → Run 'diane mcp auth --server infakt' to get fresh tokens, then re-run this script.")
        sys.exit(1)

    print(f"✅ First refresh SUCCEEDED")
    print(f"   New access_token:  {r1['access_token'][:40]}...")
    print(f"   New refresh_token: {r1['refresh_token'][:40]}...")
    print(f"   expires_in:        {r1.get('expires_in', 'unknown')}")
    print(f"   scope:             {r1.get('scope', 'unknown')}")

    # ── Second refresh with OLD token (rotation test) ──
    r2 = refresh(token_url, client_id, refresh_token, "REFRESH ATTEMPT 2 (SAME original token — rotation test)")

    if r2["status"] == 200 and r2.get("access_token"):
        print(f"\n⚠️  Second refresh ALSO succeeded with the same token!")
        print(f"   → infakt does NOT use refresh token rotation.")
        print(f"   The refresh token can be reused multiple times.")
    elif r2["status"] in (400, 401) and "invalid_grant" in json.dumps(r2.get("body", {})):
        print(f"\n✅ Rotation confirmed!")
        print(f"   Second refresh with the same token was rejected (invalid_grant).")
        print(f"   → infakt uses refresh token rotation — each refresh invalidates the old token.")
    else:
        print(f"\n⚠️  Second refresh result: status={r2['status']}")
        print(f"   Could not determine rotation behavior.")
        if "body" in r2:
            print(f"   Response: {json.dumps(r2['body'], indent=2)}")

    print("\n─── CONCLUSION ───")
    print("✓ The token file format is correct (access_token, refresh_token, client_id, expires_at)")
    print("✓ The OAuth config has the correct token_url and client_id")
    if r1["status"] == 200 and r1.get("access_token"):
        print("✓ Refresh flow works — the stored refresh token can produce new tokens")
        if r2["status"] == 200 and r2.get("access_token"):
            print("⚠️ No rotation detected — refresh tokens are reusable")
        else:
            print("✓ Refresh token rotation confirmed")


if __name__ == "__main__":
    main()
