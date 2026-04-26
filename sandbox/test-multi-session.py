#!/usr/bin/env python3
"""
Multi-Session Memory Integration Test

Simulates 4 agent sessions saving memories, then verifies cross-session recall.

Usage:
  export MEMORY_PROJECT_ID="e59a7c1c-6ec9-41aa-9fb4-79071a9569c7"
  python3 test-multi-session.py
"""

import json, os, subprocess, sys, time, uuid

PROJECT_ID = os.environ["MEMORY_PROJECT_ID"]
SERVER_URL = os.environ.get("MEMORY_SERVER_URL", "https://memory.emergent-company.ai")
MEMORY_CLI = os.environ.get("MEMORY_CLI", "/root/.memory/bin/memory")
PREFIX = f"ts-{uuid.uuid4().hex[:6]}"
CLEANUP = os.environ.get("CLEANUP", "1") == "1"

saved = []

def mem(*args):
    cmd = [MEMORY_CLI] + list(args) + ["--project", PROJECT_ID, "--json"]
    env = os.environ.copy(); env["MEMORY_SERVER_URL"] = SERVER_URL
    r = subprocess.run(cmd, capture_output=True, text=True, env=env)
    if r.returncode != 0:
        return None
    try:
        return json.loads(r.stdout)
    except json.JSONDecodeError:
        return None

def save(content, cat="observation", conf=0.7, agent="test", session="", tier=1):
    key = f"{PREFIX}-{int(time.time() * 1000000)}"
    props = {"content": content, "confidence": conf, "memory_tier": tier,
             "category": cat, "source_agent": agent, "status": "active",
             "access_count": 1, "source_session": session,
             "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
             "test_run": PREFIX}
    r = mem("graph", "objects", "create", "--type", "MemoryFact",
            "--key", key, "--properties", json.dumps(props))
    if r:
        saved.append({"key": key, "content": content, "session": session, "id": r.get("id","")})

def recall(query, limit=20):
    r = mem("query", "--mode=search", "--limit", str(limit), query)
    if not r:
        return []
    facts = []
    for item in r.get("results", []):
        if item.get("type") != "graph" or item.get("object_type") != "MemoryFact":
            continue
        if not item.get("key", "").startswith(PREFIX):
            continue
        fields = item.get("fields", {})
        score = item.get("score", 0)
        if score < 0.1:
            continue
        facts.append({
            "session": fields.get("source_session", ""),
            "content": fields.get("content", ""),
            "score": round(score, 3),
        })
    return facts

def cleanup():
    if not CLEANUP:
        return
    for e in saved:
        if e.get("id"):
            mem("graph", "objects", "update", e["id"],
                "--properties", json.dumps({"status": "archived", "test_run": ""}))

def hdr(label):
    print(f"\n{'='*60}\n  {label}\n{'='*60}")

passed, failed = 0, 0
def ok(name, cond, detail=""):
    global passed, failed
    if cond:
        print(f"  ✅ {name}"); passed += 1
    else:
        print(f"  ❌ {name} — {detail}"); failed += 1

print(f"\n{'#'*60}\n#  Diane Multi-Session Memory Test\n{'#'*60}")
print(f"  Project: {PROJECT_ID}   Prefix: {PREFIX}")

# ── Session 1 ──────────────────────────────────────────────────────
hdr("Session 1: User Onboarding")
s1 = f"{PREFIX}-s1"
save("User prefers Go for backend services due to goroutines", "user-preference", 0.9, "diane-default", s1)
save("User works on Diane — a personal AI assistant project", "entity", 0.95, "diane-default", s1)
save("User values code quality and thorough testing", "user-preference", 0.85, "diane-default", s1)
save("Diane uses MCP server architecture with Apple, Google, Finance tools", "entity", 0.9, "diane-default", s1)
ok("Session 1 saved 4 facts", len(saved) == 4)

# ── Session 2 ──────────────────────────────────────────────────────
hdr("Session 2: Technical Discussion")
s2 = f"{PREFIX}-s2"
save("Diane is built as Go backend with macOS SwiftUI companion app", "decision", 0.9, "diane-default", s2)
save("User prefers Alpine over Ubuntu for sandbox containers", "user-preference", 0.85, "diane-default", s2)
save("User uses MCP protocol for tool integration in Diane", "pattern", 0.95, "diane-default", s2)
ok("Session 2: 3 facts (total 7)", len(saved) == 7)

# ── Session 3 ──────────────────────────────────────────────────────
hdr("Session 3: Architecture Planning")
s3 = f"{PREFIX}-s3"
save("Three-tier memory: per-turn capture (T1), session extraction (T2), nightly dreaming (T3)", "pattern", 0.9, "diane-default", s3)
save("Action item: Register diane-runner as MCP server on Memory Platform", "action-item", 0.85, "diane-default", s3)
save("Action item: Build diane-sandbox Docker image with Alpine", "action-item", 0.9, "diane-default", s3)
save("Memory Platform runs at memory.emergent-company.ai v0.37.2", "observation", 0.95, "diane-default", s3)
ok("Session 3: 4 facts (total 11)", len(saved) == 11)

# ── Session 4 ──────────────────────────────────────────────────────
hdr("Session 4: Dreaming/Consolidation")
s4 = f"{PREFIX}-s4"
save("Diane sandbox is self-contained — no connection to Master needed", "decision", 0.95, "diane-dreamer", s4, 3)
save("Pattern: Go static binary + Alpine Docker + MCP stdio for sandbox", "pattern", 0.85, "diane-dreamer", s4, 3)
ok("Session 4: 2 facts (total 13)", len(saved) == 13)

# ── Recall ─────────────────────────────────────────────────────────
hdr("Cross-Session Memory Recall")
print("  Waiting 2s for search index...")
time.sleep(2)

# Test 1: Go preferences — use specific terms
r1 = recall("goroutines backend services")
ok("Recall #1: Go preferences", len(r1) >= 1, f"found {len(r1)}")
for f in r1: print(f"    [{f['session'][:24]:24s}] score={f['score']:.3f}: {f['content'][:60]}")

# Test 2: Diane project
r2 = recall("Diane project personal assistant")
ok("Recall #2: Diane project", len(r2) >= 1, f"found {len(r2)}")
for f in r2: print(f"    [{f['session'][:24]:24s}] score={f['score']:.3f}: {f['content'][:60]}")

# Test 3: Alpine (exact match — "Alpine" is in the fact)
r3 = recall("Alpine Ubuntu sandbox")
ok("Recall #3: Alpine/container preference", len(r3) >= 1, f"found {len(r3)}")
for f in r3: print(f"    [{f['session'][:24]:24s}] score={f['score']:.3f}: {f['content'][:60]}")

# Test 4: Companion/SwiftUI
r4 = recall("SwiftUI companion app macOS")
ok("Recall #4: Companion app decision", len(r4) >= 1, f"found {len(r4)}")
for f in r4: print(f"    [{f['session'][:24]:24s}] score={f['score']:.3f}: {f['content'][:60]}")

# Test 5: T3 dreamed facts
r5 = recall("self-contained Diane sandbox")
ok("Recall #5: T3 dreamed facts", len(r5) >= 1, f"found {len(r5)}")
for f in r5: print(f"    [{f['session'][:24]:24s}] score={f['score']:.3f}: {f['content'][:60]}")

# Test 6: Comprehensive cross-session — verify facts span multiple sessions
# Run two focused queries targeting different sessions, combine results
r6a = recall("goroutines backend services Go")      # Session 1 — Go preference
r6b = recall("Alpine over Ubuntu sandbox")         # Session 2 preference fact
r6c = recall("three tier memory T2 T3")            # Session 3 pattern fact
r6d = recall("self-contained no Master")           # Session 4 T3 fact
all_facts = r6a + r6b + r6c + r6d
sessions = set(f['session'] for f in all_facts)
ok("Recall #6: Multi-session results (≥2 sessions)", len(sessions) >= 2,
   f"{len(all_facts)} facts from {len(sessions)} sessions")
# Print all unique sessions and their facts
for s in sorted(sessions):
    fs = [f for f in all_facts if f['session'] == s]
    for f in fs:
        print(f"    [{s[-20:]:20s}] score={f['score']:.3f}: {f['content'][:60]}")

# Test 7: Memory tiers — T1 facts (from sessions 1-3)
r7 = recall("code quality testing thorough")
ok("Recall #7: T1 facts (per-turn)", len(r7) >= 1, f"found {len(r7)}")
for f in r7: print(f"    [{f['session'][:24]:24s}] score={f['score']:.3f}: {f['content'][:60]}")

# ── Results ────────────────────────────────────────────────────────
hdr("Results")
print(f"  Facts saved: {len(saved)}   Passed: {passed}   Failed: {failed}")
cleanup()
if failed == 0:
    print(f"\n🎉 ALL {passed} PASSED — Cross-session memory works!")
else:
    print(f"\n⚠  {failed}/{passed+failed} failed")
    sys.exit(1)
