#!/usr/bin/env python3
"""
Diane Dreaming Pipeline — Tier 3 Memory Consolidation

Runs nightly. Performs:
  1. Confidence decay for unaccessed memories
  2. Pattern detection via semantic similarity
  3. Fact merging/consolidation
  4. Synthetic fact generation (hallucination)
  5. Cleanup of ephemeral/duplicate facts

Uses the `memory` CLI for all graph operations.
"""

import json
import subprocess
import sys
import time
from datetime import datetime, timedelta, timezone
from collections import defaultdict

# ── Configuration ──
PROJECT_ID = "b4c8aae0-62a4-43aa-a546-f09042d4a34d"
TOKEN = "emt_78de9e8057d13f5d8cd884c19aa3d3101db1de9b0831370c543840f506bbc5dc"
CONFIDENCE_HALF_LIFE_DAYS = 30
SIMILARITY_THRESHOLD = 0.85
HIGH_CONFIDENCE_THRESHOLD = 0.9
MIN_ACCESS_FOR_HALLUCINATION = 3
DELETE_THRESHOLD = 0.05
DRY_RUN = "--dry-run" in sys.argv


def memory(*args):
    """Run a memory CLI command and return parsed JSON output."""
    cmd = ["memory"] + list(args)
    if "--json" not in args and "--format" not in args:
        cmd.extend(["--json"])
    cmd.extend(["--project", PROJECT_ID])
    
    env = {"MEMORY_API_KEY": TOKEN, "HOME": "/root", "PATH": "/root/.memory/bin:/usr/local/bin:/usr/bin:/bin"}
    
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=30, env=env)
        if result.returncode != 0:
            return {"error": result.stderr.strip() or result.stdout.strip()}
        if result.stdout.strip():
            # Try parsing as JSON
            try:
                return json.loads(result.stdout)
            except json.JSONDecodeError:
                return {"raw": result.stdout.strip()}
        return {"success": True}
    except subprocess.TimeoutExpired:
        return {"error": "timeout"}
    except Exception as e:
        return {"error": str(e)}


def log(msg, data=None):
    """Structured logging."""
    timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    prefix = "[DRY-RUN] " if DRY_RUN else ""
    print(f"{prefix}{timestamp} {msg}")
    if data:
        print(f"  {json.dumps(data, default=str)[:200]}")


def get_all_memory_facts():
    """List all MemoryFact objects in the project."""
    result = memory("graph", "objects", "list", "--type", "MemoryFact", "--limit", "5000", "--output", "json")
    items = []
    
    if isinstance(result, dict):
        if "items" in result:
            items = result["items"]
        elif "error" in result:
            log(f"ERROR listing facts: {result['error']}")
            return []
    
    log(f"Found {len(items)} MemoryFact objects")
    return items


def apply_confidence_decay(facts):
    """Reduce confidence for facts last accessed > half-life days ago."""
    now = datetime.now(timezone.utc)
    decay_count = 0
    archived_count = 0
    
    for fact in facts:
        props = fact.get("properties", {})
        confidence = float(props.get("confidence", 1.0))
        last_accessed_str = props.get("last_accessed", fact.get("created_at", ""))
        access_count = int(props.get("access_count", 0))
        fact_id = fact.get("id", "")
        fact_key = fact.get("key", fact_id[:16])
        
        # Calculate age
        try:
            last_accessed = datetime.fromisoformat(last_accessed_str.replace("Z", "+00:00"))
        except (ValueError, TypeError):
            last_accessed = now
        
        days_since_access = (now - last_accessed).days if last_accessed else 999
        
        # Apply decay: halve confidence every half-life days
        if days_since_access > CONFIDENCE_HALF_LIFE_DAYS and access_count == 0:
            decay_factor = 2 ** (days_since_access / CONFIDENCE_HALF_LIFE_DAYS)
            new_confidence = confidence / decay_factor
            
            if new_confidence < DELETE_THRESHOLD:
                log(f"  ARCHIVE {fact_key}: confidence {confidence:.2f} → {new_confidence:.4f} (below threshold)")
                if not DRY_RUN:
                    memory("graph", "objects", "update", fact_id,
                           "--properties", json.dumps({
                               "confidence": new_confidence,
                               "status": "archived"
                           }))
                archived_count += 1
            else:
                log(f"  DECAY {fact_key}: confidence {confidence:.2f} → {new_confidence:.2f} ({days_since_access}d since access)")
                if not DRY_RUN:
                    memory("graph", "objects", "update", fact_id,
                           "--properties", json.dumps({"confidence": new_confidence}))
                decay_count += 1
    
    log(f"Applied decay: {decay_count} decayed, {archived_count} archived")
    return decay_count, archived_count


def detect_patterns(facts):
    """Use vector search to find similar/overlapping facts and cluster them."""
    clusters = []
    processed = set()
    
    for fact in facts:
        fact_id = fact.get("id", "")
        key = fact.get("key", fact_id[:16])
        
        if key in processed:
            continue
        
        props = fact.get("properties", {})
        content = props.get("content", "")
        if not content or len(content) < 10:
            continue
        
        # Find similar facts via memory query
        result = memory("query", "--mode=search", f"--limit=5", content[:100])
        
        similar = []
        if isinstance(result, dict) and "results" in result:
            for item in result["results"]:
                sim_key = item.get("key", item.get("id", ""))[:16]
                if sim_key != key and sim_key not in processed:
                    score = item.get("score", 0)
                    if score >= SIMILARITY_THRESHOLD:
                        similar.append({
                            "key": sim_key,
                            "id": item.get("id", ""),
                            "content": item.get("content", ""),
                            "score": score
                        })
        
        if len(similar) >= 1:
            cluster = {
                "primary_key": key,
                "primary_content": content,
                "similar": similar,
                "size": 1 + len(similar)
            }
            clusters.append(cluster)
            processed.add(key)
            for s in similar:
                processed.add(s["key"])
        else:
            processed.add(key)
    
    log(f"Found {len(clusters)} pattern clusters")
    for c in clusters:
        log(f"  Cluster: '{c['primary_content'][:60]}...' + {len(c['similar'])} similar facts")
    
    return clusters


def consolidate_clusters(clusters, facts_by_key):
    """Merge weak facts into the strongest fact in each cluster."""
    merge_count = 0
    
    for cluster in clusters:
        primary_key = cluster["primary_key"]
        primary = facts_by_key.get(primary_key, {})
        primary_props = primary.get("properties", {})
        primary_conf = float(primary_props.get("confidence", 0.5))
        
        for similar in cluster["similar"]:
            sim_key = similar["key"]
            sim_fact = facts_by_key.get(sim_key, {})
            sim_props = sim_fact.get("properties", {})
            sim_conf = float(sim_props.get("confidence", 0.5))
            sim_id = sim_fact.get("id", "")
            
            # If similar fact has lower confidence, merge into primary
            if sim_conf <= primary_conf:
                log(f"  MERGE {sim_key} → {primary_key} (confidence {sim_conf} ≤ {primary_conf})")
                if not DRY_RUN:
                    # Archive the weaker fact
                    memory("graph", "objects", "update", sim_id,
                           "--properties", json.dumps({
                               "status": "merged",
                               "merged_into": primary_key
                           }))
                merge_count += 1
            elif sim_conf > primary_conf:
                # The similar fact is stronger — promote it
                log(f"  PROMOTE {sim_key} over {primary_key} (confidence {sim_conf} > {primary_conf})")
                # The cluster could be reorganized, but for simplicity we just note it
                merge_count += 1
    
    log(f"Consolidated {merge_count} facts into clusters")
    return merge_count


def generate_hallucinated_facts(facts):
    """Generate synthetic (hallucinated) derived facts from highly-accessed, high-confidence facts."""
    generated = 0
    
    for fact in facts:
        props = fact.get("properties", {})
        confidence = float(props.get("confidence", 0))
        access_count = int(props.get("access_count", 0))
        content = props.get("content", "")
        memory_tier = int(props.get("memory_tier", 1))
        
        # Only process facts that are accessed often and have high confidence
        if confidence < HIGH_CONFIDENCE_THRESHOLD:
            continue
        if access_count < MIN_ACCESS_FOR_HALLUCINATION:
            continue
        if memory_tier >= 3:
            continue  # Don't hallucinate from already-hallucinated facts
        
        # Generate derived fact by abstraction
        # For now, use simple patterns based on content keywords
        hallu_content = None
        
        # Pattern: "[Subject] prefers/likes/uses [X]" → "[Subject] values [category]"
        if "prefer" in content.lower() or "like" in content.lower() or "use" in content.lower():
            hallu_content = f"[Derived] From repeated pattern: {content[:80]}..."
        # Pattern: "[Subject] always/never/usually [verb]" → generalized behavior
        elif "always" in content.lower() or "never" in content.lower() or "usually" in content.lower():
            hallu_content = f"[Generalized] {content[:100]}"
        
        if hallu_content:
            derived_key = f"dream-fact-{int(time.time())}-{generated}"
            log(f"  HALLUCINATE {derived_key}: from '{content[:50]}...'")
            
            if not DRY_RUN:
                memory("graph", "objects", "create", "--type", "MemoryFact",
                       "--key", derived_key,
                       "--properties", json.dumps({
                           "content": hallu_content,
                           "confidence": 0.5,  # Hallucinated facts start at 0.5
                           "memory_tier": 3,
                           "source_agent": "diane-dreaming",
                           "category": "dreamed",
                           "derived_from": fact.get("key", fact.get("id", "")),
                           "source_session": props.get("source_session", ""),
                       }))
            generated += 1
    
    log(f"Generated {generated} hallucinated derived facts")
    return generated


def cleanup_ephemeral(facts):
    """Delete or archive facts with very low confidence or duplicate content."""
    deleted = 0
    
    for fact in facts:
        props = fact.get("properties", {})
        confidence = float(props.get("confidence", 1.0))
        status = props.get("status", "active")
        fact_id = fact.get("id", "")
        key = fact.get("key", fact_id[:16])
        
        if status == "archive" or status == "archived":
            continue
        
        if confidence < DELETE_THRESHOLD:
            log(f"  DELETE {key}: confidence {confidence:.4f} below threshold")
            if not DRY_RUN:
                memory("graph", "objects", "delete", fact_id)
            deleted += 1
    
    log(f"Cleaned up {deleted} ephemeral facts")
    return deleted


def run_dreaming_pipeline():
    """Execute the full dreaming pipeline."""
    log("═══ Diane Dreaming Pipeline ═══")
    log(f"Date: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    log(f"Mode: {'DRY RUN' if DRY_RUN else 'LIVE'}")
    log(f"Project: {PROJECT_ID}")
    print()
    
    # Step 1: Load all facts
    log("STEP 1: Loading MemoryFact objects...")
    facts = get_all_memory_facts()
    if not facts:
        log("No facts to process.")
        return {"status": "no_facts"}
    
    facts_by_key = {}
    for f in facts:
        k = f.get("key", f.get("id", ""))
        facts_by_key[k] = f
    
    print()
    
    # Step 2: Apply confidence decay
    log("STEP 2: Applying confidence decay...")
    decay_count, archived_count = apply_confidence_decay(facts)
    print()
    
    # Step 3: Detect patterns (semantic similarity clustering)
    log("STEP 3: Detecting patterns...")
    clusters = detect_patterns(facts)
    print()
    
    # Step 4: Consolidate clusters
    log("STEP 4: Consolidating clusters...")
    if clusters:
        merge_count = consolidate_clusters(clusters, facts_by_key)
    else:
        merge_count = 0
    print()
    
    # Step 5: Generate hallucinated facts
    log("STEP 5: Generating derived (hallucinated) facts...")
    hallu_count = generate_hallucinated_facts(facts)
    print()
    
    # Step 6: Cleanup ephemeral facts
    log("STEP 6: Cleaning up ephemeral facts...")
    deleted_count = cleanup_ephemeral(facts)
    print()
    
    # Summary
    log("═══ Dreaming Complete ═══")
    summary = {
        "total_facts": len(facts),
        "decayed": decay_count,
        "archived": archived_count,
        "clusters_found": len(clusters),
        "merged": merge_count,
        "hallucinated": hallu_count,
        "deleted": deleted_count,
    }
    log("Summary:", summary)
    
    return summary


if __name__ == "__main__":
    summary = run_dreaming_pipeline()
    print()
    print(json.dumps(summary, indent=2))
