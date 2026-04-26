#!/usr/bin/env python3
"""
Agent routing test suite for Diane.
Tests whether diane-default properly routes tasks to sub-agents
or handles them directly based on the task type.

Each scenario has:
- prompt: the user's message
- expect_routing: True if should delegate to sub-agent, False if handle directly
- expected_sub_agent: name of the expected sub-agent (or None if direct)
- expected_tools: tools we expect to see used
"""

import json
import subprocess
import sys
import time
import re
from datetime import datetime

# ── Configuration ──
DIANE_BIN = "~/.diane/bin/diane"
MAX_WAIT = 90  # seconds per scenario
RESULTS_FILE = "/tmp/agent-routing-results.json"

# ── Scenarios ──
SCENARIOS = [
    {
        "id": "S01-general-knowledge",
        "prompt": "What was the GDP of Japan in 2023?",
        "category": "general-knowledge",
        "expect_routing": False,
        "expected_sub_agent": None,
        "description": "Simple factual query - should handle directly"
    },
    {
        "id": "S02-agent-creation",
        "prompt": "Create a new agent called 'diane-researcher' that specializes in web research and can search the web, fetch pages, and summarize findings. It should have a professional tone.",
        "category": "agent-creation",
        "expect_routing": True,
        "expected_sub_agent": "diane-agent-creator",
        "expected_tools": ["spawn_agents", "agent-def-create", "list_available_agents"],
        "description": "Creating a new agent definition - should delegate to agent-creator"
    },
    {
        "id": "S03-web-research",
        "prompt": "Search the web for the latest developments in AI agent routing patterns and summarize what you find.",
        "category": "web-research",
        "expect_routing": False,
        "expected_sub_agent": None,
        "description": "Web research task - default agent has web-search-brave, handle directly"
    },
    {
        "id": "S04-skill-creation",
        "prompt": "Create a new skill called 'diane-database' that documents how to connect to PostgreSQL databases, run queries, and handle migrations in Go.",
        "category": "skill-creation",
        "expect_routing": True,
        "expected_sub_agent": "diane-agent-creator",
        "expected_tools": ["skill-create", "spawn_agents", "list_available_agents"],
        "description": "Creating a new skill doc - should delegate to agent-creator"
    },
    {
        "id": "S05-agent-definition",
        "prompt": "I need an agent that can monitor GitHub repos for new releases and notify me. Define an agent called 'diane-release-monitor' with web-search-brave and skill tools. It should check for releases daily.",
        "category": "agent-definition",
        "expect_routing": True,
        "expected_sub_agent": "diane-agent-creator",
        "expected_tools": ["spawn_agents", "agent-def-create", "list_available_agents"],
        "description": "Defining a new agent spec - should delegate to agent-creator"
    },
    {
        "id": "S06-knowledge-graph",
        "prompt": "Search the knowledge graph for information about our project architecture and tell me what patterns we use.",
        "category": "knowledge-graph",
        "expect_routing": False,
        "expected_sub_agent": None,
        "description": "Graph query - default agent has search-knowledge, handle directly"
    },
    {
        "id": "S07-list-agents",
        "prompt": "What agents are available in this project? List them and describe what each one does.",
        "category": "agent-list",
        "expect_routing": False,
        "expected_sub_agent": None,
        "description": "Listing agents - default agent can call list_available_agents itself"
    },
    {
        "id": "S08-complex-routing",
        "prompt": "We need two new agents: one for monitoring our deployment pipeline called 'diane-deploy-monitor', and another for managing our memory optimization called 'diane-memory-optimizer'. Create both of them.",
        "category": "complex-routing",
        "expect_routing": True,
        "expected_sub_agent": "diane-agent-creator",
        "expected_tools": ["spawn_agents"],
        "description": "Creating multiple agents - should delegate once or twice"
    },
]


def run_diane_trigger(prompt, max_wait=90):
    """Trigger diane-default with a prompt and return the result."""
    # Use go run to get latest code (but that's slow). Use the built binary.
    cmd = f'{DIANE_BIN} agent trigger diane-default "{prompt}"'
    
    try:
        result = subprocess.run(
            ["bash", "-c", cmd],
            capture_output=True,
            text=True,
            timeout=max_wait,
        )
        return {
            "stdout": result.stdout,
            "stderr": result.stderr,
            "exit_code": result.returncode,
            "success": result.returncode == 0,
        }
    except subprocess.TimeoutExpired:
        return {
            "stdout": "",
            "stderr": f"TIMEOUT after {max_wait}s",
            "exit_code": -1,
            "success": False,
        }
    except Exception as e:
        return {
            "stdout": "",
            "stderr": str(e),
            "exit_code": -2,
            "success": False,
        }


def analyze_result(scenario, result):
    """Analyze the agent's tool calls to determine routing behavior.
    
    Returns a dict with:
    - routing_detected: True if agent spawned a sub-agent
    - sub_agent_used: name of sub-agent if detected
    - tools_used: list of tool names called
    - reasoning_visible: any routing reasoning in the response
    - passed: whether the test passed (routing decision matches expected)
    """
    output = result.get("stdout", "")
    stderr = result.get("stderr", "")
    
    analysis = {
        "scenario_id": scenario["id"],
        "prompt": scenario["prompt"],
        "category": scenario["category"],
        "expect_routing": scenario["expect_routing"],
        "expected_sub_agent": scenario.get("expected_sub_agent"),
        "description": scenario["description"],
    }
    
    if not result["success"]:
        analysis["error"] = stderr
        analysis["passed"] = False
        return analysis
    
    # Extract tool calls from output
    tool_calls = []
    in_tool_section = False
    for line in output.split("\n"):
        if "🔧 Tool Calls" in line:
            in_tool_section = True
            continue
        if "💬 Messages" in line:
            in_tool_section = False
            continue
        if in_tool_section and line.strip():
            tool_calls.append(line.strip())
    
    analysis["tool_calls_raw"] = tool_calls
    analysis["tools_used"] = tool_calls
    
    # Extract summary/response
    response_lines = []
    in_response = False
    for line in output.split("\n"):
        if "📋 Response:" in line:
            in_response = True
            continue
        if in_response:
            response_lines.append(line)
    
    analysis["response"] = " ".join(response_lines).strip()[:500]
    
    # Detect routing
    tool_text = " ".join(tool_calls).lower()
    response_text = " ".join(response_lines).lower()
    full_text = output.lower()
    
    # Check for spawn_agents usage
    spawned_agents = []
    if "spawn_agents" in tool_text or "spawn_agents" in response_text:
        # Try to extract agent names
        for match in re.finditer(r'agent[_\s]name[_\s:=]+["\']?(\w[\w-]*)["\']?', full_text):
            spawned_agents.append(match.group(1))
    
    analysis["spawned_agents"] = spawned_agents
    analysis["routing_detected"] = len(spawned_agents) > 0 or "spawn_agents" in tool_text
    
    # Check for direct usage of agent-creator tools
    analysis["agent_creator_tools_used"] = any(
        t in tool_text for t in ["agent-def-create", "skill-create", "update_agent_definition"]
    )
    
    # Check for direct handling tools
    analysis["direct_tools_used"] = any(
        t in tool_text for t in ["web-search-brave", "search-knowledge", "search-hybrid", "entity-query"]
    )
    
    # Determine pass/fail
    if scenario["expect_routing"]:
        # Should have used spawn_agents OR agent-creator tools
        analysis["passed"] = analysis["routing_detected"] or analysis["agent_creator_tools_used"]
        if not analysis["passed"]:
            analysis["fail_reason"] = "Expected routing to sub-agent but agent handled directly"
    else:
        # Should NOT have spawned a sub-agent for simple tasks
        analysis["passed"] = not analysis["routing_detected"]
        if not analysis["passed"]:
            analysis["fail_reason"] = f"Expected direct handling but routed to: {spawned_agents}"
    
    return analysis


def main():
    results = []
    total = len(SCENARIOS)
    passed = 0
    failed = 0
    
    print(f"{'='*60}")
    print(f"  Diane Agent Routing Test Suite")
    print(f"  {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"  {total} scenarios")
    print(f"{'='*60}")
    print()
    
    for i, scenario in enumerate(SCENARIOS, 1):
        print(f"\n{'─'*60}")
        print(f"  [{i}/{total}] {scenario['id']}: {scenario['description']}")
        print(f"  Prompt: {scenario['prompt'][:80]}...")
        print(f"{'─'*60}")
        sys.stdout.flush()
        
        print(f"  ⏳ Running (timeout: {MAX_WAIT}s)...")
        sys.stdout.flush()
        
        result = run_diane_trigger(scenario["prompt"], MAX_WAIT)
        analysis = analyze_result(scenario, result)
        results.append(analysis)
        
        if analysis["passed"]:
            passed += 1
            status = "✅ PASS"
        else:
            failed += 1
            status = "❌ FAIL"
        
        print(f"  {status}")
        print(f"  Tools used: {analysis.get('tools_used', 'N/A')}")
        if analysis.get("spawned_agents"):
            print(f"  Routed to: {analysis['spawned_agents']}")
        if analysis.get("fail_reason"):
            print(f"  Reason: {analysis['fail_reason']}")
        if analysis.get("response"):
            print(f"  Response: {analysis['response'][:200]}...")
        print()
    
    # Summary
    print(f"{'='*60}")
    print(f"  RESULTS: {passed}/{total} passed ({failed} failed)")
    print(f"{'='*60}")
    for r in results:
        icon = "✅" if r["passed"] else "❌"
        reason = ""
        if not r["passed"] and "fail_reason" in r:
            reason = f" — {r['fail_reason']}"
        elif r["passed"] and r.get("spawned_agents"):
            reason = f" → {r['spawned_agents']}"
        print(f"  {icon} {r['scenario_id']}: {r['description']}{reason}")
    
    # Save results
    with open(RESULTS_FILE, "w") as f:
        json.dump({"results": results, "summary": {"total": total, "passed": passed, "failed": failed, "timestamp": datetime.now().isoformat()}}, f, indent=2)
    
    print(f"\n  Results saved to {RESULTS_FILE}")
    
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
