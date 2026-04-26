#!/usr/bin/env python3
"""
Multi-Level Agent Spawning Test Suite for Diane.

Tests spawning chains via the Memory Platform executor:
  L1: direct spawning (default → agent-creator)
  L2: nested spawning (default → agent-creator → researcher)
  L3: deep chains with 3+ levels
  ERR: error cases (depth exceeded, nonexistent agent)

Strategy:
  1. Run `diane agent trigger` as the parent driver
  2. Poll the API for parent + child run status
  3. Inspect messages/tool calls across all runs in the chain
"""

import json
import subprocess
import sys
import time
import re
from datetime import datetime

# ── Configuration ──
DIANE_BIN = "~/.diane/bin/diane"
MAX_WAIT = 180  # seconds per scenario
POLL_INTERVAL = 2
MAX_POLLS = MAX_WAIT // POLL_INTERVAL
RESULTS_FILE = "/tmp/spawn-test-results.json"
TOKEN = "emt_78de9e8057d13f5d8cd884c19aa3d3101db1de9b0831370c543840f506bbc5dc"
PID = "b4c8aae0-62a4-43aa-a546-f09042d4a34d"
# Also try the test project
TOKEN2 = "emt_de32b3b3ec43e15a6cc39a5b1e2deee4073d9990230139e66a5ee222f0ec3349"
PID2 = "e59a7c1c-6ec9-41aa-9fb4-79071a9569c7"


def api_get(path, token=TOKEN, pid=PID):
    """GET from MP REST API."""
    actual_pid = pid
    full_path = path.replace("{pid}", actual_pid)
    cmd = ["curl", "-s", f"https://memory.emergent-company.ai{full_path}",
           "-H", f"Authorization: Bearer {token}"]
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
    try:
        return json.loads(r.stdout)
    except json.JSONDecodeError:
        return {"raw": r.stdout}


def find_run_id(output):
    """Extract run ID from diane agent trigger output."""
    m = re.search(r'Run ID:\s*([a-f0-9-]+)', output)
    return m.group(1) if m else None


def find_agent_id(output):
    """Extract agent ID from diane agent trigger output."""
    m = re.search(r'Creating runtime agent\.\.\. ✅ ([a-f0-9-]+)', output)
    return m.group(1) if m else None


def run_scenario(scenario):
    """Run a single spawn test scenario via diane agent trigger + API polling."""
    s_id = scenario["id"]
    prompt = scenario["prompt"]
    parent_agent = scenario.get("parent_agent", "diane-default")

    result = {
        "scenario_id": s_id,
        "description": scenario.get("description", ""),
        "category": scenario.get("category", ""),
        "start_time": datetime.now().isoformat(),
    }

    # Step 1: Trigger the parent agent
    escaped_prompt = prompt.replace('"', '\\"')
    cmd = f'{DIANE_BIN} agent trigger {parent_agent} "{escaped_prompt}"'

    try:
        trigger_result = subprocess.run(
            ["bash", "-c", cmd],
            capture_output=True, text=True,
            timeout=MAX_WAIT,
        )
        output = trigger_result.stdout
        stderr = trigger_result.stderr
        result["trigger_exit_code"] = trigger_result.returncode
        result["trigger_output"] = output
        result["trigger_stderr"] = stderr
    except subprocess.TimeoutExpired:
        result["status"] = "timeout"
        result["passed"] = False
        result["fail_reason"] = f"Trigger timeout after {MAX_WAIT}s"
        return result
    except Exception as e:
        result["status"] = "error"
        result["passed"] = False
        result["fail_reason"] = str(e)
        return result

    # Step 2: Extract run info from output
    run_id = find_run_id(output)
    agent_id = find_agent_id(output)
    result["run_id"] = run_id
    result["agent_id"] = agent_id

    if not run_id:
        # The trigger might have timed out but run is still going
        # Try finding it from the agent_id match
        if "❌" in output and "failed" in output.lower():
            result["status"] = "trigger_failed"
            result["passed"] = scenario.get("expect_error", False)
            result["fail_reason"] = "Trigger failed"
            return result
        result["status"] = "no_run_id"
        result["passed"] = False
        result["fail_reason"] = "Could not extract run ID from output"
        return result

    # Step 3: Poll for parent completion
    parent_status = "running"
    poll_count = 0
    for i in range(MAX_POLLS):
        time.sleep(POLL_INTERVAL)
        poll_count = i + 1
        run_data = api_get(f"/api/projects/{PID}/agent-runs/{run_id}")
        d = run_data.get("data", run_data)
        parent_status = d.get("status", "unknown")
        result["parent_status"] = parent_status
        result["parent_steps"] = d.get("stepCount", 0)
        if parent_status in ("success", "completed", "failed", "error"):
            break
    result["poll_count"] = poll_count

    # Step 4: Collect child/spawned runs
    children_data = api_get(f"/api/projects/{PID}/agent-runs?parent_run_id={run_id}")
    children = []
    items = children_data.get("data", {}).get("items", children_data.get("data", []))

    # Filter: children_data sometimes returns ALL runs; filter by parent_run_id
    for item in items:
        if item.get("parentRunId") == run_id or item.get("id") != run_id:
            if item.get("id") != run_id:  # exclude self
                children.append(item)

    result["child_runs"] = []
    spawn_depth = 0

    for child in children:
        child_id = child.get("id", "")
        child_status = child.get("status", "unknown")
        child_steps = child.get("stepCount", 0)
        child_agent = child.get("agentDefinitionId", "")

        # Get child messages
        child_msgs = api_get(f"/api/projects/{PID}/agent-runs/{child_id}/messages")
        child_tcalls = api_get(f"/api/projects/{PID}/agent-runs/{child_id}/tool-calls")

        cr = {
            "id": child_id,
            "status": child_status,
            "steps": child_steps,
            "agent_def_id": child_agent,
            "message_count": len(child_msgs.get("data", [])),
            "tool_calls": [tc.get("toolName", "") for tc in child_tcalls.get("data", [])],
        }
        result["child_runs"].append(cr)

        # Recursively check grandchildren
        grandchild_data = api_get(f"/api/projects/{PID}/agent-runs?parent_run_id={child_id}")
        grandchild_items = []
        gc_items = grandchild_data.get("data", {}).get("items", grandchild_data.get("data", []))
        for gc in gc_items:
            if gc.get("parentRunId") == child_id:
                grandchild_items.append(gc)

        if grandchild_items:
            cr["grandchildren"] = []
            for gc in grandchild_items:
                gc_id = gc.get("id", "")
                gc_msgs = api_get(f"/api/projects/{PID}/agent-runs/{gc_id}/messages")
                gc_tcalls = api_get(f"/api/projects/{PID}/agent-runs/{gc_id}/tool-calls")
                cr["grandchildren"].append({
                    "id": gc_id,
                    "status": gc.get("status", ""),
                    "steps": gc.get("stepCount", 0),
                    "message_count": len(gc_msgs.get("data", [])),
                    "tool_calls": [tc.get("toolName", "") for tc in gc_tcalls.get("data", [])],
                })

    # Calculate actual spawn depth
    for cr in result["child_runs"]:
        if cr.get("grandchildren"):
            spawn_depth = max(spawn_depth, 2)
            for gc in cr["grandchildren"]:
                if gc.get("grandchildren"):
                    spawn_depth = max(spawn_depth, 3)
        else:
            if not spawn_depth:
                spawn_depth = 1

    result["spawn_depth"] = spawn_depth

    # Step 5: Pass/fail evaluation
    expected_depth = scenario.get("expected_spawn_depth", 1)

    if parent_status in ("success", "completed"):
        if scenario.get("expect_error"):
            result["passed"] = False
            result["fail_reason"] = "Expected error but run succeeded"
        elif spawn_depth >= expected_depth:
            result["passed"] = True
        else:
            result["passed"] = False
            result["fail_reason"] = (
                f"Expected spawn depth ~{expected_depth}, got {spawn_depth}. "
                f"Child runs found: {len(result['child_runs'])}"
            )
    elif parent_status in ("failed", "error"):
        if scenario.get("expect_error"):
            result["passed"] = True
        else:
            err_msg = ""
            d = run_data.get("data", run_data)
            if d.get("errorMessage"):
                err_msg = d["errorMessage"]
            result["passed"] = False
            result["fail_reason"] = f"Run failed: {err_msg}"
    else:
        result["status"] = "running"
        result["passed"] = False
        result["fail_reason"] = f"Run still {parent_status} after {MAX_WAIT}s"

    result["end_time"] = datetime.now().isoformat()
    return result


# ── Scenarios ──
SCENARIOS = [
    # ═══ L1: Direct delegation ═══
    {
        "id": "T1-L1-agent-creation",
        "description": "default → agent-creator: create an agent definition",
        "prompt": "I need an agent called 'diane-test-bot' that specializes in web searches. Create it with web-search-brave and web-fetch tools.",
        "expected_spawn_depth": 1,
        "category": "agent-creation",
    },
    {
        "id": "T2-L1-skill-creation",
        "description": "default → agent-creator: create a skill",
        "prompt": "Create a skill called 'diane-golang-guide' that documents Go error handling best practices for web servers.",
        "expected_spawn_depth": 1,
        "category": "skill-creation",
    },
    {
        "id": "T3-L1-web-research",
        "description": "default → researcher: deep research delegation",
        "prompt": "Research the latest Go concurrency patterns for web servers. Search the web, compile findings, and give me a detailed report with sources.",
        "expected_spawn_depth": 1,
        "category": "research",
    },
    {
        "id": "T4-L1-direct-handling",
        "description": "default handles directly: no spawn needed",
        "prompt": "What's the weather like today? Give a generic forecast.",
        "expected_spawn_depth": 0,
        "category": "direct",
    },

    # ═══ L2: Agent-creator delegates to researcher ═══
    {
        "id": "T5-L2-research-before-create",
        "description": "default → agent-creator → researcher: nested delegation",
        "prompt": "I need an agent called 'diane-infra-monitor'. First research what infrastructure monitoring tools and patterns exist, then create the agent with the appropriate web-search-brave and web-fetch tools for monitoring research.",
        "expected_spawn_depth": 2,
        "category": "nested",
    },

    # ═══ Error cases ═══
    {
        "id": "T6-ERR-nonexistent-spawn",
        "description": "spawn to nonexistent agent: error propagation",
        "prompt": "I need to spawn a task to agent 'diane-nonexistent' to run some diagnostics. Can you do that?",
        "expected_spawn_depth": 0,
        "expect_error": True,
        "category": "error",
    },
]


def main():
    results = []
    total = len(SCENARIOS)
    passed = 0
    failed = 0

    print("=" * 70)
    print(f"  Diane Multi-Level Spawn Test Suite")
    print(f"  {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"  {total} scenarios | Timeout: {MAX_WAIT}s each")
    print("=" * 70)

    for i, scenario in enumerate(SCENARIOS, 1):
        print(f"\n{'─' * 70}")
        print(f"  [{i}/{total}] {scenario['id']}: {scenario['description']}")
        print(f"  Expected spawning: {'none' if scenario.get('expected_spawn_depth', 1) == 0 else f'depth {scenario.get(\"expected_spawn_depth\", 1)}'}")
        print(f"{'─' * 70}")
        sys.stdout.flush()

        print(f"  ⏳ Running... ")
        sys.stdout.flush()

        start = time.time()
        analysis = run_scenario(scenario)
        elapsed = time.time() - start
        results.append(analysis)

        if analysis.get("passed", False):
            passed += 1
            status = "✅ PASS"
        else:
            failed += 1
            status = "❌ FAIL"

        print(f"  {status} ({elapsed:.0f}s)")
        print(f"  Parent: {analysis.get('parent_status', '?')} | Steps: {analysis.get('parent_steps', 0)}")

        depth = analysis.get("spawn_depth", 0)
        if depth > 0:
            print(f"  Spawn depth: {depth}")
        else:
            print(f"  Spawn depth: 0 (no children)")

        child_count = len(analysis.get("child_runs", []))
        if child_count > 0:
            print(f"  Children: {child_count}")
            for c in analysis["child_runs"]:
                gc = c.get("grandchildren", [])
                print(f"    🧒 {c.get('id', '?')[:16]}... status={c.get('status')} steps={c.get('steps')} tools={len(c.get('tool_calls', []))}")
                if gc:
                    print(f"      Grandchildren: {len(gc)}")

        if analysis.get("fail_reason"):
            print(f"  Reason: {analysis['fail_reason']}")

        # Rate limiting
        time.sleep(2)

    # Summary
    print(f"\n{'=' * 70}")
    print(f"  RESULTS: {passed}/{total} passed ({failed} failed)")
    print(f"{'=' * 70}")
    for r in results:
        icon = "✅" if r.get("passed") else "❌"
        reason = ""
        if not r.get("passed") and "fail_reason" in r:
            reason = f" — {r['fail_reason'][:100]}"
        elif r.get("passed") and r.get("spawn_depth", 0) > 0:
            reason = f" → depth {r['spawn_depth']}"
        print(f"  {icon} {r['scenario_id']}{reason}")

    # Save results
    with open(RESULTS_FILE, "w") as f:
        json.dump({"results": results, "summary": {
            "total": total, "passed": passed, "failed": failed,
            "timestamp": datetime.now().isoformat()
        }}, f, indent=2, default=str)

    print(f"\n  Results saved to {RESULTS_FILE}")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
