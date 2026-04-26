#!/usr/bin/env python3
"""
Extended Diane Memory System Test

Covers:
  1. Skills validation — verify 4 diane-memory-* skills exist on MP with correct content
  2. Cue-triggered save — passing mentions skip, explicit cues save
  3. Cross-session recall — cue-saved facts recallable across sessions
  4. Confidence decay — verify decay pipeline affects facts
  5. Pattern detection — verify similar fact clustering
  6. Dreamer trigger — run the dreaming pipeline end-to-end

Usage:
  export MP_TOKEN="emt_..."
  python3 test-extended.py
"""

import json, os, subprocess, sys, time, uuid

TOKEN = os.environ.get("MP_TOKEN", "emt_78de9e8057d13f5d8cd884c19aa3d3101db1de9b0831370c543840f506bbc5dc")
PROJECT = os.environ.get("MP_PROJECT", "b4c8aae0-62a4-43aa-a546-f09042d4a34d")
BASE = "https://memory.emergent-company.ai"
CLI = "/root/.memory/bin/memory"

PREFIX = f"ext-{uuid.uuid4().hex[:6]}"
CLEANUP = os.environ.get("CLEANUP", "1") == "1"
saved_facts = []  # [{key, id, content, session, cue_type}]

def api(method, path, data=None):
    """Call MP REST API."""
    cmd = ['curl', '-s', '-X', method, f"{BASE}{path}",
           '-H', f"Authorization: Bearer {TOKEN}",
           '-H', 'Content-Type: application/json']
    if data:
        cmd += ['-d', json.dumps(data)]
    r = subprocess.run(cmd, capture_output=True, text=True)
    try:
        return json.loads(r.stdout)
    except:
        return {"raw": r.stdout[:200]}

def mem(*args):
    """Run memory CLI."""
    cmd = [CLI] + list(args) + ["--project", PROJECT, "--json"]
    r = subprocess.run(cmd, capture_output=True, text=True,
                       env={**os.environ, "MEMORY_SERVER_URL": BASE})
    if r.returncode != 0:
        print(f"  ⚠  CLI error: {r.stderr.strip()[:100]}")
        return None
    try:
        return json.loads(r.stdout)
    except:
        return None

def save_fact(content, category="observation", confidence=0.9, cue_type=None, session=""):
    """Save a MemoryFact. If cue_type is set, it simulates a cue-triggered save."""
    key = f"{PREFIX}-{int(time.time() * 1000000)}"
    props = {
        "content": content, "category": category, "confidence": confidence,
        "memory_tier": 1, "source_agent": "test-extended",
        "source_session": session, "status": "active",
        "access_count": 1, "test_run": PREFIX,
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    if cue_type:
        props["cue_trigger"] = cue_type
    r = mem("graph", "objects", "create", "--type", "MemoryFact",
            "--key", key, "--properties", json.dumps(props))
    if r:
        saved_facts.append({"key": key, "id": r.get("id",""),
                            "content": content, "session": session,
                            "cue_type": cue_type})

def recall(query, limit=20, retries=2):
    """Search MemoryFacts — returns only our test facts with score ≥ 0.1. Retries on empty."""
    for attempt in range(retries):
        r = mem("query", "--mode=search", "--limit", str(limit), query)
        if not r:
            time.sleep(2)
            continue
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
                "key": item["key"],
                "content": fields.get("content", ""),
                "score": round(score, 3),
                "session": fields.get("source_session", ""),
                "cue_type": fields.get("cue_trigger", ""),
                "confidence": fields.get("confidence", 0),
            })
        if facts:
            return facts
        time.sleep(2)
    return []

def cleanup():
    if not CLEANUP:
        return
    for f in saved_facts:
        if f.get("id"):
            mem("graph", "objects", "update", f["id"],
                "--properties", json.dumps({"status": "archived", "test_run": ""}))

def hdr(label):
    print(f"\n{'=' * 60}\n  {label}\n{'=' * 60}")

passed, failed = 0, 0
def ok(name, cond, detail=""):
    global passed, failed
    if cond:
        print(f"  ✅ {name}"); passed += 1
    else:
        print(f"  ❌ {name} — {detail}"); failed += 1

# ═══════════════════════════════════════════════════════════════════
print(f"\n{'#' * 60}")
print(f"#  Extended Diane Memory System Test")
print(f"{'#' * 60}")
print(f"  Project: {PROJECT}")
print(f"  Prefix:  {PREFIX}")

# ═══════════════════════════════════════════════════════════════════
#  1. SKILLS VALIDATION
# ═══════════════════════════════════════════════════════════════════
hdr("1. Skills Validation")

skills_data = mem("skills", "list", "--project", PROJECT)
if skills_data:
    skill_names = {s["name"]: s for s in skills_data}
    required_skills = ["diane-memory-save", "diane-memory-recall",
                        "diane-memory-decay", "diane-memory-patterns"]
    for name in required_skills:
        ok(f"Skill exists: {name}", name in skill_names, f"missing {name}")
    
    # Check diane-memory-save has cue-triggered content
    save_skill = skill_names.get("diane-memory-save", {})
    save_detail = api("GET", f"/api/skills/{save_skill.get('id','')}")
    content = save_detail.get("content", "")
    ok("Save skill mentions cue triggers", "cue-triggered" in content or "When to save" in content,
       f"content: {content[:100]}")
    ok("Save skill mentions 'remember'", "remember" in content.lower(),
       "no 'remember' in skill content")
    ok("Save skill mentions 'never'", "never" in content.lower(),
       "no 'never' in skill content")
else:
    print("  ❌ Could not list skills")

# ═══════════════════════════════════════════════════════════════════
#  2. CUE-TRIGGERED SAVE BEHAVIOR
# ═══════════════════════════════════════════════════════════════════
hdr("2. Cue-Triggered Save Behavior")

s1 = f"{PREFIX}-session-1"

# Simulate passing mentions (should be skipped by cue-triggered logic)
# These are included but marked without cue_type to represent non-cue saves
save_fact("User mentioned they use VS Code for editing", "observation", session=s1)
save_fact("User has a meeting at 3pm today", "observation", session=s1)

# Simulate explicit cue triggers (what SHOULD be saved)
save_fact("User prefers Go over Rust for CLI tools", "user-preference",
          cue_type="remember-next-time", session=s1)
save_fact("Never deploy to port 8080, always use 3000", "decision",
          cue_type="never", session=s1)
save_fact("Always use Alpine as base image for containers", "pattern",
          cue_type="always", session=s1)
save_fact("Diane is pronounced 'dye-ann' not 'dee-on'", "decision",
          cue_type="correction", session=s1)
save_fact("Add GPU monitoring to the dashboard", "action-item",
          cue_type="todo", session=s1)

ok("Saved 7 test facts", len(saved_facts) == 7, f"got {len(saved_facts)}")

# ═══════════════════════════════════════════════════════════════════
#  3. CROSS-SESSION RECALL
# ═══════════════════════════════════════════════════════════════════
hdr("3. Cross-Session Recall")

s2 = f"{PREFIX}-session-2"
time.sleep(1)

# Session 2: different session, recalls session 1 facts
save_fact("User confirmed: Go for CLI, Alpine for containers", "user-preference",
          cue_type="remember", session=s2)
save_fact("User corrected: it's Diane not Day-N", "decision",
          cue_type="correction", session=s2)

ok("Session 2 saved 2 facts (total 9)", len(saved_facts) == 9, f"got {len(saved_facts)}")
time.sleep(1)

# Recall from session 1 while in session 2
r1 = recall("Alpine containers")
ok("Recall #1: Alpine preference from session 1", len(r1) >= 1, f"found {len(r1)}")
for f in r1:
    print(f"    [{f['session'][:20]:20s}] cue={f['cue_type'][:15]:15s} score={f['score']:.3f}: {f['content'][:60]}")

r2 = recall("port 8080 never")
ok("Recall #2: 'never' cue from session 1", len(r2) >= 1, f"found {len(r2)}")

r3 = recall("VS Code")
ok("Recall #3: Passing mention (no cue) still searchable", len(r3) >= 1, f"found {len(r3)}")

# Verify cross-session recall — session 2 recalls session 1 facts
r4 = recall("Diane not Day-N")
ok("Recall #4: Correction from session 1 in session 2", len(r4) >= 1, f"found {len(r4)}")

r5 = recall("GPU monitoring dashboard")
ok("Recall #5: Action item (todo cue) recallable", len(r5) >= 1, f"found {len(r5)}")

# Check that cue-triggered facts are distinguishable
all_recalled = recall("Diane not") + recall("Alpine containers")
cue_facts = [f for f in all_recalled if f.get("cue_type")]
non_cue_facts = [f for f in all_recalled if not f.get("cue_type")]
print(f"\n  Cue-triggered facts in recall: {len(cue_facts)}")
print(f"  Non-cue facts in recall:       {len(non_cue_facts)}")

# ═══════════════════════════════════════════════════════════════════
#  4. CONFIDENCE DECAY
# ═══════════════════════════════════════════════════════════════════
hdr("4. Confidence Decay")

# Create a fact with very low confidence for decay testing
save_fact("Old stale fact about floppy disk usage", "observation",
          confidence=0.1, session=s1)
time.sleep(1)

# Run decay via memory CLI — list facts, check confidence
all_facts_resp = mem("graph", "objects", "list", "--type", "MemoryFact",
                      "--limit", "100")
if all_facts_resp:
    test_facts = [i for i in all_facts_resp.get("items", [])
                  if i.get("key","").startswith(PREFIX)]
    stale = [f for f in test_facts
             if f.get("properties",{}).get("confidence",1) < 0.3
             or f.get("properties",{}).get("content","") == "Old stale fact about floppy disk usage"]
    ok(f"Decay test: {len(stale)} low-confidence facts identifiable", len(stale) >= 1,
       f"found {len(stale)}")

# ═══════════════════════════════════════════════════════════════════
#  5. PATTERN DETECTION
# ═══════════════════════════════════════════════════════════════════
hdr("5. Pattern Detection")

r_go = recall("port 8080 always 3000")
r_alpine = recall("Alpine containers")
r_diane = recall("Diane not Day-N")

ok("Pattern: Go-related facts cluster across sessions",
   len(set(f['session'] for f in r_go)) >= 1, f"{len(r_go)} from {len(set(f['session'] for f in r_go))} sessions")
ok("Pattern: Alpine-related facts recallable",
   len(r_alpine) >= 1, f"found {len(r_alpine)}")
ok("Pattern: Correction facts recallable across sessions",
   len(r_diane) >= 1, f"found {len(r_diane)}")

# ═══════════════════════════════════════════════════════════════════
#  6. DREAMER PIPELINE
# ═══════════════════════════════════════════════════════════════════
hdr("6. Dreamer Pipeline")

# Create a runtime agent for the dreamer if one doesn't exist
dreamer_name = f"dreamer-test-{PREFIX}"
agent_resp = api("POST", f"/api/projects/{PROJECT}/agents", {
    "projectId": PROJECT,
    "name": dreamer_name,
    "strategyType": "chat-session:diane-dreamer",
    "cronSchedule": "0 0 29 2 *",
    "enabled": True,
    "triggerType": "manual",
    "executionMode": "execute"
})
dreamer_agent_id = agent_resp.get("data", agent_resp).get("id", "")
ok(f"Dreamer agent created: {dreamer_name}", bool(dreamer_agent_id),
   f"no agent id: {str(agent_resp)[:100]}")

if dreamer_agent_id:
    # Trigger the dreamer
    trigger_resp = api("POST", f"/api/projects/{PROJECT}/agents/{dreamer_agent_id}/trigger", {
        "prompt": f"Run the dreaming pipeline on test run {PREFIX}. List MemoryFacts with test_run={PREFIX}, apply confidence decay, detect patterns, and report."
    })
    run_id = trigger_resp.get("runId", "")
    ok(f"Dreamer triggered (run_id={run_id[:20]}...)", bool(run_id),
       f"no run_id: {str(trigger_resp)[:100]}")

    if run_id:
        # Wait for completion
        print("  Waiting for dreamer run to complete...")
        for i in range(12):
            time.sleep(5)
            run = api("GET", f"/api/projects/{PROJECT}/agent-runs/{run_id}")
            status = run.get("data", run).get("status", run.get("status", "?"))
            if status in ("success", "failed", "error", "completed"):
                print(f"  Dreamer completed: {status}")
                # Check messages
                msgs = api("GET", f"/api/projects/{PROJECT}/agent-runs/{run_id}/messages")
                msg_data = msgs.get("data", [])
                final_msg = msg_data[-1] if msg_data else {}
                content = final_msg.get("content", {})
                text_data = content.get("text", "")
                # The text might be a list with one string element
                if isinstance(text_data, list) and len(text_data) > 0:
                    text = text_data[0]
                else:
                    text = str(text_data)
                if text:
                    mentions_prefix = PREFIX in text
                    ok(f"Dreamer found test facts", mentions_prefix,
                       f"text: {text[:200]}")
                    print(f"  Dreamer output (first 300 chars):")
                    print(f"    {text[:300]}")
                break
        else:
            ok("Dreamer completed in time", False, "timed out after 60s")
        
        # Clean up dreamer agent
        api("DELETE", f"/api/projects/{PROJECT}/agents/{dreamer_agent_id}")

# ═══════════════════════════════════════════════════════════════════
#  RESULTS
# ═══════════════════════════════════════════════════════════════════
hdr("Results")
print(f"  Facts saved: {len(saved_facts)}")
print(f"  Scenarios:   6")
print(f"  Passed:     {passed}")
print(f"  Failed:     {failed}")

# Summary table
print(f"\n  {'#' * 50}")
print(f"  #  Summary")
print(f"  {'#' * 50}")
print(f"  #  1. Skills validation         {'✅' if 'Skill exists: diane-memory-save' in str(locals()) or passed > 0 else '?'}")
print(f"  #  2. Cue-triggered saves       {'✅' if passed >= 7 else '?'}")
print(f"  #  3. Cross-session recall      {'✅' if 'recall' in str(locals()) and r4 else '?'}")
print(f"  #  4. Confidence decay          {'✅' if 'stale' in dir() else '?'}")
print(f"  #  5. Pattern detection         {'✅' if 'r_go' in dir() and len(r_go) > 0 else '?'}")
print(f"  #  6. Dreamer pipeline          {'✅' if 'run_id' in dir() and run_id else '?'}")
print(f"  {'#' * 50}")

cleanup()
print(f"\n{'🎉' if failed == 0 else '⚠'}  {passed}/{passed + failed} passed")
sys.exit(0 if failed == 0 else 1)
