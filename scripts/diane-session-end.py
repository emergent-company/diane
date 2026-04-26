#!/usr/bin/env python3
"""
Diane Session-End Extraction — Tier 2 Memory Consolidation

Runs after each agent run completes. For each completed run:
  1. Fetches the full transcript
  2. Extracts structured facts
  3. Creates MemoryFact objects
  4. Creates a SessionSummary

Can be run as a cron or triggered via webhook.
"""

import json
import subprocess
import sys
import time
from datetime import datetime

# ── Configuration ──
PROJECT_ID = "b4c8aae0-62a4-43aa-a546-f09042d4a34d"
TOKEN = "emt_78de9e8057d13f5d8cd884c19aa3d3101db1de9b0831370c543840f506bbc5dc"
DRY_RUN = "--dry-run" in sys.argv
RUN_ID = None

# Extract run_id from args
for arg in sys.argv[1:]:
    if arg.startswith("--run-id="):
        RUN_ID = arg.split("=", 1)[1]
    elif arg.startswith("--run-id"):
        # Next arg
        pass


def memory(*args):
    """Run a memory CLI command and return parsed JSON."""
    cmd = ["memory"] + list(args)
    if "--json" not in args:
        cmd.append("--json")
    cmd.extend(["--project", PROJECT_ID])
    
    env = {"MEMORY_API_KEY": TOKEN, "PATH": "/root/.memory/bin:/usr/local/bin:/usr/bin:/bin"}
    
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=30, env=env)
        if result.returncode != 0:
            return {"error": result.stderr.strip()}
        if result.stdout.strip():
            try:
                return json.loads(result.stdout)
            except json.JSONDecodeError:
                return {"raw": result.stdout.strip()}
        return {"success": True}
    except Exception as e:
        return {"error": str(e)}


def curl(*args):
    """Direct API call for agent-run endpoints (not available via memory CLI)."""
    cmd = ["curl", "-s"]
    cmd.extend(args)
    # Extract the URL from args and add Auth header
    for i, a in enumerate(args):
        if a.startswith("https://memory.emergent-company.ai"):
            break
    cmd.extend(["-H", f"Authorization: Bearer {TOKEN}"])
    
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError:
        return {"raw": result.stdout}


def log(msg, data=None):
    """Structured logging."""
    timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    prefix = "[DRY-RUN] " if DRY_RUN else ""
    print(f"{prefix}{timestamp} {msg}")
    if data:
        print(f"  {json.dumps(data, default=str)[:300]}")


def find_completed_runs():
    """Find recently completed agent runs that don't have SessionSummary yet."""
    # Get runs that completed in the last hour
    result = curl("-s", 
        f"https://memory.emergent-company.ai/api/projects/{PROJECT_ID}/agent-runs?limit=50")
    
    runs = []
    data = result.get("data", {})
    items = data.get("items", data if isinstance(data, list) else [])
    
    for item in items:
        status = item.get("status", "")
        completed = item.get("completedAt") or item.get("completed_at")
        if status in ("success", "completed") and completed:
            runs.append({
                "id": item.get("id", ""),
                "agent_name": item.get("agentName", item.get("agent_name", "unknown")),
                "completed_at": completed,
                "step_count": item.get("stepCount", item.get("step_count", 0)),
            })
    
    return runs


def fetch_run_messages(run_id):
    """Fetch all messages for a run."""
    result = curl("-s",
        f"https://memory.emergent-company.ai/api/projects/{PROJECT_ID}/agent-runs/{run_id}/messages")
    
    messages = result.get("data", [])
    
    # Format messages
    formatted = []
    for m in messages:
        role = m.get("role", "unknown")
        content = m.get("content", {})
        
        # Extract text from various formats
        text = ""
        if isinstance(content, dict):
            text = content.get("text", content.get("content", str(content)))
        elif isinstance(content, str):
            text = content
        
        formatted.append({
            "role": role,
            "content": str(text)[:2000],  # Truncate for extraction
        })
    
    return formatted


def extract_memories(messages):
    """
    Extract structured memories from a run transcript.
    For now, does simple pattern-based extraction.
    Could be upgraded to LLM-based extraction.
    """
    facts = []
    categories = []
    conversations = []
    
    for msg in messages:
        content = msg.get("content", "")
        role = msg.get("role", "")
        
        conversations.append(f"[{role}]: {content}")
        
        if role in ("assistant", "user", "diane-default", "diane-agent-creator", "diane-researcher"):
            conversations.append(f"{role}: {content}")
        
        # Pattern: user states a preference
        if role == "user" or "user" in role.lower():
            for pattern, category in [
                (r"I (?:prefer|like|use|want) (\w+)", "user-preference"),
                (r"(?:my|our) (\w+) (?:is|should be|needs)", "entity"),
                (r"(?:create|make|build) (\w+)", "action-item"),
                (r"(?:decided|decision|choose|going with) (\w+)", "decision"),
            ]:
                import re
                for match in re.finditer(pattern, content, re.IGNORECASE):
                    obj = match.group(1)
                    fact_text = f"User: {content[:100]}..."
                    facts.append({
                        "content": fact_text,
                        "confidence": 0.7,
                        "memory_tier": 2,
                        "category": category,
                    })
        
        # Pattern: explicit statements (assistant confirms something)
        if role == "assistant" or "diane" in role.lower():
            if "created" in content.lower():
                facts.append({
                    "content": f"Agent created/modified something: {content[:120]}...",
                    "confidence": 0.8,
                    "memory_tier": 2,
                    "category": "action-completed",
                })
    
    # Extract topics (simple keyword frequency)
    word_freq = {}
    stop_words = {"the", "a", "an", "is", "are", "was", "were", "be", "been",
                  "have", "has", "had", "do", "does", "did", "will", "would",
                  "can", "could", "should", "may", "might", "i", "you", "he",
                  "she", "it", "we", "they", "this", "that", "these", "those",
                  "and", "or", "but", "in", "on", "at", "to", "for", "of",
                  "with", "by", "from", "as", "into", "through", "during"}
    
    for msg in messages:
        for word in msg.get("content", "").split():
            word = word.strip(".,!?;:'\"()[]{}").lower()
            if len(word) > 4 and word not in stop_words and word.isalpha():
                word_freq[word] = word_freq.get(word, 0) + 1
    
    sorted_words = sorted(word_freq.items(), key=lambda x: -x[1])
    topics = [w for w, c in sorted_words[:10] if c >= 2]
    
    return facts, topics


def save_session_summary(run_id, agent_name, messages, facts, topics):
    """Save SessionSummary and MemoryFacts to the graph."""
    
    # Create SessionSummary
    session_key = f"session-summary-{run_id[:12]}"
    summary_props = {
        "run_id": run_id,
        "source_agent": agent_name,
        "topic_clusters": topics,
        "fact_count": len(facts),
        "message_count": len(messages),
        "extracted_at": datetime.utcnow().isoformat() + "Z",
        "status": "complete",
    }
    
    log(f"  Creating SessionSummary: {session_key}")
    log(f"    Topics: {', '.join(topics[:5])}")
    
    if not DRY_RUN:
        result = memory("graph", "objects", "create", "--type", "SessionSummary",
                        "--key", session_key,
                        "--properties", json.dumps(summary_props))
        if "error" in result:
            log(f"  ERROR creating summary: {result['error']}")
    
    # Create MemoryFacts
    for i, fact in enumerate(facts):
        fact_key = f"{run_id[:12]}-fact-{i}"
        log(f"  Creating MemoryFact: {fact_key} [{fact['category']}]")
        
        if not DRY_RUN:
            result = memory("graph", "objects", "create", "--type", "MemoryFact",
                            "--key", fact_key,
                            "--properties", json.dumps({
                                "content": fact["content"],
                                "confidence": fact["confidence"],
                                "memory_tier": 2,
                                "category": fact["category"],
                                "source_session": run_id,
                                "source_agent": agent_name,
                                "status": "active",
                                "access_count": 0,
                            }))
            if "error" in result:
                log(f"    ERROR: {result['error']}")
    
    return len(facts), session_key


def process_run(run_id, agent_name):
    """Process a single completed run."""
    log(f"Processing run: {run_id} ({agent_name})")
    
    # Fetch messages
    log("  Fetching messages...")
    messages = fetch_run_messages(run_id)
    log(f"  Got {len(messages)} messages")
    
    if len(messages) < 2:
        log("  Too few messages, skipping")
        return
    
    # Extract memories
    log("  Extracting memories...")
    facts, topics = extract_memories(messages)
    log(f"  Extracted {len(facts)} facts, {len(topics)} topics")
    
    # Save to graph
    log("  Saving to graph...")
    fact_count, summary_key = save_session_summary(run_id, agent_name, messages, facts, topics)
    
    log(f"  ✅ Saved {fact_count} facts + SessionSummary ({summary_key})")
    
    return {
        "run_id": run_id,
        "agent": agent_name,
        "messages": len(messages),
        "facts": len(facts),
        "topics": topics,
        "summary_key": summary_key,
    }


def main():
    """Main entry point."""
    log("═══ Diane Session-End Extraction ═══")
    log(f"Date: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    log(f"Mode: {'DRY RUN' if DRY_RUN else 'LIVE'}")
    log(f"Project: {PROJECT_ID}")
    print()
    
    if RUN_ID:
        # Process a specific run
        log(f"Processing specific run: {RUN_ID}")
        result = process_run(RUN_ID, "unknown")
        if result:
            print(json.dumps(result, indent=2))
        return
    
    # Find recently completed runs
    log("STEP 1: Finding completed runs...")
    runs = find_completed_runs()
    log(f"Found {len(runs)} completed runs")
    
    if not runs:
        log("No completed runs to process.")
        return
    
    results = []
    for run in runs[:5]:  # Max 5 per run to avoid overload
        print()
        result = process_run(run["id"], run.get("agent_name", "unknown"))
        if result:
            results.append(result)
        time.sleep(1)  # Rate limiting
    
    # Summary
    print()
    log("═══ Extraction Complete ═══")
    log(f"Processed {len(results)} runs")
    
    print(json.dumps(results, indent=2, default=str))


if __name__ == "__main__":
    main()
