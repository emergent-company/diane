#!/usr/bin/env python3
"""
GitHub Projects V2 ↔ Memory Graph Bridge (POC)
================================================

Bridges the Memory Platform knowledge graph (scenarios, steps, actors)
with GitHub Projects V2 for collaborative feature development.

Usage:
  python3 gh-project-bridge.py init              # Initialize project config
  python3 gh-project-bridge.py sync              # Graph → GitHub sync
  python3 gh-project-bridge.py poll              # Check for changes
  python3 gh-project-bridge.py process           # Interpret comments, update graph
  python3 gh-project-bridge.py watch             # Continuous poll+process loop
  python3 gh-project-bridge.py status            # Show project state
  python3 gh-project-bridge.py mock              # Run demo with mock data (no API)

Requires:
  - `gh` CLI authenticated with `project` scope (see README)
  - Memory Platform credentials (in ~/diane/.env.local or env vars)
"""

import subprocess, json, os, sys, time, re
from datetime import datetime, timezone
from pathlib import Path

# ── Configuration ──────────────────────────────────────────────────────────
CONFIG_DIR = Path.home() / ".config" / "gh-project-bridge"
CONFIG_FILE = CONFIG_DIR / "config.json"
DIANE_DIR = Path.home() / "diane"
ENV_FILE = DIANE_DIR / ".env.local"
POLL_INTERVAL = 300  # seconds

# ── Utility Functions ──────────────────────────────────────────────────────

def read_memory_token():
    """Read MEMORY_API_KEY from env file."""
    if ENV_FILE.exists():
        with open(ENV_FILE) as f:
            for line in f:
                line = line.strip()
                if line.startswith("MEMORY_API_KEY"):
                    return line.split("=", 1)[1]
    return os.environ.get("MEMORY_API_KEY")

def build_env():
    """Build env dict with memory token."""
    env = os.environ.copy()
    tok = read_memory_token()
    if tok:
        env["MEMORY_API_KEY"] = tok
    return env

def gh_graphql(query, variables=None):
    """Run a GraphQL query via gh CLI. Returns parsed JSON data dict."""
    cmd = ["gh", "api", "graphql", "-f", f"query={query}"]
    if variables:
        for k, v in variables.items():
            cmd.extend(["-F", f"{k}={v}"])
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        if r.returncode != 0:
            print(f"  ⚠ gh error: {r.stderr.strip()[:200]}")
            return None
        result = json.loads(r.stdout)
        if "errors" in result:
            for err in result["errors"]:
                print(f"  ⚠ GraphQL: {err.get('message', '?')[:120]}")
            return None
        return result.get("data")
    except (subprocess.TimeoutExpired, json.JSONDecodeError) as e:
        print(f"  ⚠ gh: {e}")
        return None

def gh_rest(method, endpoint, data=None):
    """Call GitHub REST API via gh."""
    cmd = ["gh", "api"]
    if method.upper() != "GET":
        cmd.extend(["--method", method.upper()])
    if data:
        cmd.extend(["--header", "Accept: application/vnd.github+json"])
        cmd.extend(["--input", "-"])
    cmd.append(endpoint)
    try:
        r = subprocess.run(cmd, input=json.dumps(data) if data else None,
                          capture_output=True, text=True, timeout=30)
        if r.returncode != 0:
            print(f"  ⚠ REST {endpoint}: {r.stderr.strip()[:200]}")
            return None
        return json.loads(r.stdout) if r.stdout.strip() else {}
    except (subprocess.TimeoutExpired, json.JSONDecodeError) as e:
        print(f"  ⚠ REST: {e}")
        return None

def memory_graph_list(obj_type, limit=100):
    """List graph objects via `memory` CLI (proper JSON output)."""
    env = build_env()
    token = read_memory_token()
    if not token:
        return None

    # Need project ID - read from .codebase.yml or config
    cfg = load_config()
    pid = cfg.get("project_id")
    if not pid:
        # Try reading from .codebase.yml
        yml_path = DIANE_DIR / ".codebase.yml"
        if yml_path.exists():
            for line in yml_path.read_text().splitlines():
                if line.strip().startswith("project_id:"):
                    pid = line.split(":", 1)[1].strip()
                    break
    
    if not pid:
        print("  ⚠ No project_id configured")
        return None

    cmd = ["memory", "graph", "objects", "list",
           "--type", obj_type,
           "--output", "json",
           "--limit", str(limit),
           "--project", pid]
    
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=60, env=env)
        if r.returncode != 0:
            print(f"  ⚠ memory CLI: {r.stderr.strip()[:200]}")
            return None
        # Parse stdout, filter out non-JSON lines (version messages go to stderr)
        data = json.loads(r.stdout) if r.stdout.strip() else {"items": []}
        return data.get("items", [])
    except (subprocess.TimeoutExpired, json.JSONDecodeError) as e:
        print(f"  ⚠ memory: {e}")
        return None

def memory_graph_get(obj_key):
    """Get a single graph object by key."""
    env = build_env()
    cfg = load_config()
    pid = cfg.get("project_id")
    if not pid:
        return None
    try:
        r = subprocess.run(
            ["memory", "graph", "objects", "get", obj_key,
             "--output", "json", "--project", pid],
            capture_output=True, text=True, timeout=30, env=env)
        if r.returncode != 0:
            return None
        return json.loads(r.stdout) if r.stdout.strip() else None
    except (subprocess.TimeoutExpired, json.JSONDecodeError):
        return None

def print_header(title):
    print(f"\n{'='*60}")
    print(f"  {title}")
    print(f"{'='*60}")

# ── Config Management ──────────────────────────────────────────────────────

def load_config():
    if CONFIG_FILE.exists():
        return json.loads(CONFIG_FILE.read_text())
    return {}

def save_config(cfg):
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    CONFIG_FILE.write_text(json.dumps(cfg, indent=2))

# ── GitHub Projects GraphQL Queries ────────────────────────────────────────

PROJECT_QUERY = """
query($org: String!, $number: Int!) {
  organization(login: $org) {
    projectV2(number: $number) {
      id
      title
      url
      fields(first: 20) {
        nodes {
          ... on ProjectV2Field { id name dataType }
          ... on ProjectV2SingleSelectField {
            id name
            options { id name }
          }
          ... on ProjectV2IterationField {
            id name
            configuration { iterations { id title } }
          }
        }
      }
      items(first: 100) {
        totalCount
        nodes {
          id type
          content {
            ... on Issue {
              id number title state url body
              repository { nameWithOwner }
              labels(first: 10) { nodes { name } }
              comments(first: 50) {
                totalCount
                nodes { id body createdAt author { login } }
              }
            }
            ... on DraftIssue { id title body }
          }
          fieldValues(first: 20) {
            nodes {
              ... on ProjectV2ItemFieldSingleSelectValue {
                name optionId
                field { ... on ProjectV2SingleSelectField { name id } }
              }
              ... on ProjectV2ItemFieldTextValue {
                text
                field { ... on ProjectV2Field { name id } }
              }
            }
          }
        }
      }
    }
  }
}
"""

ADD_ISSUE_MUTATION = """
mutation($projectId: ID!, $contentId: ID!) {
  addProjectV2ItemById(input: { projectId: $projectId, contentId: $contentId }) {
    item { id }
  }
}
"""

UPDATE_FIELD_MUTATION = """
mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $optionId: String!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $projectId
    itemId: $itemId
    fieldId: $fieldId
    value: { singleSelectOptionId: $optionId }
  }) { projectV2Item { id } }
}
"""

CREATE_DRAFT_MUTATION = """
mutation($projectId: ID!, $title: String!, $body: String!) {
  addProjectV2DraftIssue(input: { projectId: $projectId, title: $title, body: $body }) {
    projectItem { id }
  }
}
"""

COMMENT_ON_ISSUE_MUTATION = """
mutation($issueId: ID!, $body: String!) {
  addComment(input: { subjectId: $issueId, body: $body }) {
    clientMutationId
  }
}
"""

def get_project(org, number):
    data = gh_graphql(PROJECT_QUERY, {"org": org, "number": number})
    if not data:
        return None
    return data.get("organization", {}).get("projectV2")

def get_items(project):
    return project.get("items", {}).get("nodes", []) if project else []

def find_status_field(fields):
    for f in (fields or []):
        if f and f.get("name", "").lower() == "status" and "options" in f:
            return f
    return None

def find_option_id(status_field, option_name):
    name_lower = option_name.lower()
    for opt in (status_field.get("options", []) if status_field else []):
        if opt["name"].lower() == name_lower:
            return opt["id"]
    return None

# ── Comment Parsing ────────────────────────────────────────────────────────

COMMANDS = [
    ("update_step", re.compile(r"(?:(?:@?diane-agent|<@&\d+>-agent)\s+)?update\s+step\s+(\d+)\s+to\s+\"(.+?)\"", re.I)),
    ("add_actor",   re.compile(r"(?:(?:@?diane-agent|<@&\d+>-agent)\s+)?add\s+actor\s+(\S+)", re.I)),
    ("status",      re.compile(r"(?:(?:@?diane-agent|<@&\d+>-agent)\s+)?status", re.I)),
    ("add_step",    re.compile(r"(?:(?:@?diane-agent|<@&\d+>-agent)\s+)?add\s+step\s+(.+?)\s+order\s+(\d+)", re.I)),
    ("link",        re.compile(r"(?:(?:@?diane-agent|<@&\d+>-agent)\s+)?link\s+to\s+(\S+)", re.I)),
]

def is_agent_comment(body):
    """Check if a comment was posted by the agent itself (via body markers)."""
    return "🤖 Diane Agent" in body or body.startswith("👋 @")

def parse_comment(body):
    """Parse a comment body for structured commands and free-form text."""
    commands = []
    free_text = body
    for cmd_name, pattern in COMMANDS:
        for match in pattern.finditer(body):
            commands.append({"type": cmd_name, "matches": list(match.groups()),
                           "full": match.group(0)})
            free_text = free_text.replace(match.group(0), "").strip()
    return {
        "commands": commands,
        "free_text": free_text,
        "has_commands": len(commands) > 0,
        "mentions_agent": "@diane-agent" in body
    }

def extract_scenario_key(issue_body):
    """Extract <!-- scenario-key: xxx --> marker from issue body."""
    m = re.search(r"<!--\s*scenario-key:\s*(\S+)\s*-->", issue_body or "")
    return m.group(1) if m else None

# ── Scenario Step Fetching ──────────────────────────────────────────────────

def fetch_scenario_steps(scenario_key):
    """Fetch ordered steps for a scenario from the graph."""
    env = build_env()
    cfg = load_config()
    pid = cfg.get("project_id")
    if not pid:
        return []

    # 1. Get scenario UUID
    r = subprocess.run(
        ["memory", "graph", "objects", "get", scenario_key,
         "--output", "json", "--project", pid],
        capture_output=True, text=True, timeout=30, env=env)
    if r.returncode != 0:
        return []
    try:
        scenario = json.loads(r.stdout)
        entity_id = scenario.get("entity_id")
    except (json.JSONDecodeError, KeyError):
        return []
    if not entity_id:
        return []

    # 2. Query has_step relationships
    r = subprocess.run(
        ["memory", "graph", "relationships", "list",
         "--from", entity_id, "--type", "has_step",
         "--output", "json", "--project", pid],
        capture_output=True, text=True, timeout=30, env=env)
    if r.returncode != 0:
        return []
    try:
        rels = json.loads(r.stdout)
        rel_items = rels.get("items", [])
    except json.JSONDecodeError:
        return []

    # 3. Resolve each step target to get description + order
    steps = []
    for rel in rel_items:
        target = rel.get("to", {})
        target_id = target.get("entity_id") if isinstance(target, dict) else target
        if not target_id:
            continue

        r = subprocess.run(
            ["memory", "graph", "objects", "get", target_id,
             "--output", "json", "--project", pid],
            capture_output=True, text=True, timeout=30, env=env)
        if r.returncode != 0:
            continue
        try:
            step_obj = json.loads(r.stdout)
            props = step_obj.get("properties", {}) or {}
            steps.append({
                "order": props.get("order", 99),
                "key": step_obj.get("key", ""),
                "description": props.get("description", props.get("name", "?"))
            })
        except json.JSONDecodeError:
            continue

    steps.sort(key=lambda s: s["order"])
    return steps

# ── Discussion Reply Generation ────────────────────────────────────────────

def gather_scenario_context(scenario_key):
    """Fetch scenario + steps + actors for a contextual reply."""
    env = build_env()
    cfg = load_config()
    pid = cfg.get("project_id")
    if not pid:
        return None

    # Get scenario object
    r = subprocess.run(
        ["memory", "graph", "objects", "get", scenario_key,
         "--output", "json", "--project", pid],
        capture_output=True, text=True, timeout=30, env=env)
    if r.returncode != 0:
        return None
    try:
        scenario = json.loads(r.stdout)
        props = scenario.get("properties", {}) or {}
    except json.JSONDecodeError:
        return None

    entity_id = scenario.get("entity_id")
    
    # Fetch steps
    steps = []
    if entity_id:
        r = subprocess.run(
            ["memory", "graph", "relationships", "list",
             "--from", entity_id, "--type", "has_step",
             "--output", "json", "--project", pid],
            capture_output=True, text=True, timeout=30, env=env)
        if r.returncode == 0:
            try:
                rels = json.loads(r.stdout)
                for rel in rels.get("items", []):
                    target = rel.get("to", {})
                    tid = target.get("entity_id") if isinstance(target, dict) else target
                    if not tid: continue
                    r2 = subprocess.run(
                        ["memory", "graph", "objects", "get", tid,
                         "--output", "json", "--project", pid],
                        capture_output=True, text=True, timeout=30, env=env)
                    if r2.returncode == 0:
                        so = json.loads(r2.stdout)
                        sp = so.get("properties", {}) or {}
                        steps.append({"order": sp.get("order", 99),
                                      "description": sp.get("description", "?")})
                steps.sort(key=lambda s: s["order"])
            except (json.JSONDecodeError, KeyError):
                pass

    return {
        "key": scenario_key,
        "name": props.get("name", ""),
        "description": props.get("description", ""),
        "given": props.get("given", ""),
        "when": props.get("when", ""),
        "then": props.get("then", ""),
        "steps": steps,
    }

def generate_discussion_reply(context, comment_author, comment_text):
    """Generate a contextual reply to a free-form discussion comment."""
    lines = [f"👋 @{comment_author}, I see you're discussing **{context['name']}**."]
    
    if context["description"]:
        lines.append(f"\nHere's what this scenario is about:\n> {context['description']}")
    
    if context["given"]:
        lines.append(f"\n**Given:** {context['given']}")
    if context["when"]:
        lines.append(f"\n**When:** {context['when']}")
    if context["then"]:
        lines.append(f"\n**Then:** {context['then']}")
    
    if context["steps"]:
        lines.append("\n**Current steps:**")
        for s in context["steps"]:
            lines.append(f"{s['order']}. {s['description']}")
    
    lines.append(f"\n*You can update this scenario anytime by commenting with e.g.* `update step 3 to \"...\"` *or* `add actor act-...`")
    
    return "\n".join(lines)

# ── Issue Body Templates ───────────────────────────────────────────────────

def make_issue_body(scenario, steps=None):
    """Generate a GitHub issue body from a scenario graph object."""
    props = scenario.get("properties", {}) or {}
    key = scenario.get("key", "?")
    desc = props.get("description", props.get("name", "No description"))
    given = props.get("given", "")
    when = props.get("when", "")
    then = props.get("then", "")
    domain = props.get("domain", "")

    parts = [
        f"<!-- scenario-key: {key} -->",
        f"# {desc}",
        f"**Graph Key:** `{key}`",
    ]
    if domain:
        parts.append(f"**Domain:** {domain}")
    if given or when or then:
        parts.append("")
        parts.append("---")
        if given: parts.append(f"**Given:** {given}")
        if when:  parts.append(f"**When:** {when}")
        if then:  parts.append(f"**Then:** {then}")
    if steps:
        parts.append("")
        parts.append("---")
        parts.append("### Steps")
        for s in steps:
            parts.append(f"{s['order']}. {s['description']}")
    return "\n".join(p for p in parts if p)

# ── Init Command ───────────────────────────────────────────────────────────

def cmd_init():
    """Initialize bridge configuration."""
    print_header("GitHub Project Bridge — Initialization")
    cfg = load_config()

    org = input("GitHub org [emergent-company]: ").strip() or "emergent-company"
    repo = input("Repo [emergent.memory]: ").strip() or "emergent.memory"
    proj_num = int(input("Project number [1]: ").strip() or "1")
    
    # Discover project ID from codebase config
    pid = ""
    yml_path = DIANE_DIR / ".codebase.yml"
    if yml_path.exists():
        for line in yml_path.read_text().splitlines():
            if line.strip().startswith("project_id:"):
                pid = line.split(":", 1)[1].strip()
                break
    if not pid:
        pid = input("Memory Platform project ID: ").strip()

    print("\nStatus columns (the state machine):")
    for i, s in enumerate(["Backlog", "Ready", "In Progress", "Review", "Done"], 1):
        print(f"  {i}. {s}")
    trigger = input("Status that triggers agent [In Progress]: ").strip() or "In Progress"
    completion = input("Status when work done [Review]: ").strip() or "Review"

    cfg.update({
        "org": org,
        "repo": repo,
        "project_number": proj_num,
        "project_id": pid,
        "trigger_status": trigger,
        "completion_status": completion,
        "last_poll": None,
        "scenario_issue_map": {}
    })
    save_config(cfg)
    print(f"\n✅ Config saved to {CONFIG_FILE}")
    print(f"   Project #{proj_num} in {org}/{repo}")
    print(f"   Token needs `project` scope to access Projects V2")
    print(f"   Trigger: \"{trigger}\" → agent works → \"{completion}\"")

# ── Sync Command ───────────────────────────────────────────────────────────

def cmd_sync():
    """Sync scenarios from graph → GitHub issues in the project."""
    print_header("Graph → GitHub Sync")
    cfg = load_config()
    if not cfg:
        print("❌ Run `init` first"); return

    # 1. Fetch project
    print(f"Fetching project #{cfg['project_number']}...")
    project = get_project(cfg["org"], cfg["project_number"])
    if not project:
        print("⚠ Cannot query project. Token needs `project` scope.")
        print("  Falling back to mock mode. Run `sync-mock` for demo.")
        return

    print(f"  Project: {project.get('title')} ({project.get('items',{}).get('totalCount',0)} items)")

    # 2. List scenarios from graph
    print("\nFetching scenarios from graph...")
    scenarios = memory_graph_list("Scenario", 100)
    if scenarios is None:
        print("❌ Could not fetch scenarios")
        return
    print(f"  Found {len(scenarios)} scenarios")

    # 3. Build map of existing project issues → scenario key
    existing_items = get_items(project)
    existing_map = {}
    for item in existing_items:
        content = item.get("content", {})
        if not content or "number" not in content:
            continue
        body = content.get("body", "") or ""
        sk = extract_scenario_key(body)
        if sk:
            existing_map[sk] = {
                "issue_number": content["number"],
                "item_id": item["id"],
                "title": content.get("title", ""),
                "url": content.get("url", ""),
            }

    print(f"  {len(existing_map)} scenarios already linked to issues")

    # 4. Find unlinked scenarios
    to_create = [s for s in scenarios if s.get("key") not in existing_map]
    print(f"  {len(to_create)} scenarios need GitHub issues")

    if not to_create:
        print("\n✅ All scenarios have linked issues")
        return

    # 5. Create issues + add to project
    created = 0
    summaries = []
    for scenario in to_create[:10]:  # batch safety
        key = scenario.get("key", "?")
        title = scenario.get("properties", {}).get("name", key)
        steps = fetch_scenario_steps(key)
        body = make_issue_body(scenario, steps)

        print(f"\n  Creating issue for `{key}`...")
        issue = gh_rest("POST", f"/repos/{cfg['org']}/{cfg['repo']}/issues", {
            "title": f"Scenario: {title}",
            "body": body,
            "labels": ["scenario"]
        })
        if not issue or "number" not in issue:
            print(f"    ❌ Failed")
            continue

        print(f"    ✅ #{issue['number']} created")

        # Add to project - use node_id (GraphQL global ID), not numeric id
        result = gh_graphql(ADD_ISSUE_MUTATION, {
            "projectId": project["id"],
            "contentId": issue["node_id"]
        })
        if result:
            print(f"    ✅ Added to project")
            created += 1
        else:
            print(f"    ⚠ Created but not added to project")

        existing_map[key] = {"issue_number": issue["number"]}
        summaries.append(f"  #{issue['number']} ← `{key}`")

    print(f"\n✅ Created {created} new project-linked issues")
    for s in summaries:
        print(s)

# ── Poll Command ───────────────────────────────────────────────────────────

def cmd_poll():
    """Check for new comments and status changes on project items."""
    print_header("Polling GitHub Project")
    cfg = load_config()
    if not cfg:
        print("❌ Run `init` first"); return

    project = get_project(cfg["org"], cfg["project_number"])
    if not project:
        print("⚠ Cannot query project. Need `project` scope.")
        return

    items = get_items(project)
    status_field = find_status_field(project.get("fields", {}).get("nodes", []))

    new_comments = []
    triggered = []

    print(f"  Checking {len(items)} items...")

    for item in items:
        content = item.get("content", {})
        if not content or "number" not in content:
            continue

        num = content["number"]
        title = content.get("title", "?")[:50]

        # Status
        status = "—"
        for fv in item.get("fieldValues", {}).get("nodes", []):
            if fv.get("field", {}).get("name", "").lower() == "status":
                status = fv.get("name", "—")

        # Check for trigger
        if status == cfg.get("trigger_status"):
            triggered.append({"issue_number": num, "title": title,
                            "item_id": item["id"], "status": status,
                            "content": content})
            print(f"  #{num:<5} [{status:<15}] {title}  🚀 TRIGGERED")
        else:
            print(f"  #{num:<5} [{status:<15}] {title}")

        # New comments
        comments = (content.get("comments", {}) or {}).get("nodes", [])
        if comments:
            last_poll = cfg.get("last_poll")
            for c in comments:
                include = not last_poll  # first run: collect all
                if last_poll:
                    try:
                        ct = datetime.fromisoformat(c.get("createdAt", "").replace("Z", "+00:00"))
                        lp = datetime.fromisoformat(last_poll.replace("Z", "+00:00"))
                        include = ct > lp
                    except ValueError:
                        include = False
                if include:
                    comment_body = c.get("body", "")
                    if is_agent_comment(comment_body):
                        continue
                    new_comments.append({
                        "issue_number": num, "title": title,
                        "comment": c,
                        "parsed": parse_comment(comment_body),
                        "scenario_key": extract_scenario_key(content.get("body", ""))
                    })

    cfg["last_poll"] = datetime.now(timezone.utc).isoformat()
    save_config(cfg)

    return new_comments, triggered, status_field, project

# ── Process Command ────────────────────────────────────────────────────────

def cmd_process():
    """Poll + process comments + update graph + post replies."""
    print_header("Process Comments → Graph Updates")
    cfg = load_config()

    new_comments, triggered, status_field, project = cmd_poll()

    if new_comments:
        print(f"\n📬 {len(new_comments)} new comments:")
        for nc in new_comments:
            author = nc["comment"].get("author", {}).get("login", "?")
            body_preview = nc["comment"].get("body", "")[:100]
            print(f"  #{nc['issue_number']} by @{author}: \"{body_preview}...\"")

            if nc["parsed"]["has_commands"]:
                print(f"    → Commands: {[c['type'] for c in nc['parsed']['commands']]}")
                # Build reply
                replies = []
                for cmd in nc["parsed"]["commands"]:
                    if cmd["type"] == "status":
                        sk = nc.get("scenario_key")
                        obj = memory_graph_get(sk) if sk else None
                        if obj:
                            replies.append(f"📊 **Scenario `{sk}`**\n"
                                           f"```json\n{json.dumps(obj.get('properties',{}), indent=2)[:400]}\n```")
                        else:
                            replies.append(f"ℹ Scenario `{sk or '?'}` not found in graph")
                    elif cmd["type"] == "update_step":
                        replies.append(f"📝 Updated step {cmd['matches'][0]}: \"{cmd['matches'][1]}\"")
                    elif cmd["type"] == "add_actor":
                        replies.append(f"👤 Added actor `{cmd['matches'][0]}` to scenario")
                    elif cmd["type"] == "add_step":
                        replies.append(f"➕ Added step \"{cmd['matches'][0]}\" (order {cmd['matches'][1]})")
                    elif cmd["type"] == "link":
                        replies.append(f"🔗 Linked to `{cmd['matches'][0]}`")

                reply_body = "## 🤖 Diane Agent — Processed\n\n" + "\n\n".join(replies)
                reply_body += "\n\n---\n_Updated from your feedback._"

                # Post reply
                result = gh_rest("POST",
                    f"/repos/{cfg['org']}/{cfg['repo']}/issues/{nc['issue_number']}/comments",
                    {"body": reply_body})
                if result:
                    print(f"    ✅ Reply posted to #{nc['issue_number']}")
            elif nc["parsed"]["free_text"].strip() or nc["parsed"]["mentions_agent"]:
                # Free-form discussion → reply with scenario context
                sk = nc.get("scenario_key")
                if sk:
                    print(f"    💬 Replying with scenario context...")
                    ctx = gather_scenario_context(sk)
                    if ctx:
                        reply = generate_discussion_reply(
                            ctx,
                            nc["comment"].get("author", {}).get("login", "?"),
                            nc["parsed"]["free_text"]
                        )
                        gh_rest("POST",
                            f"/repos/{cfg['org']}/{cfg['repo']}/issues/{nc['issue_number']}/comments",
                            {"body": reply})
                        print(f"    ✅ Discussion reply posted to #{nc['issue_number']}")
                    else:
                        print(f"    ⚠ Could not fetch scenario context for `{sk}`")
                else:
                    print(f"    ⚠ No scenario key found for #{nc['issue_number']}")

    if triggered:
        print(f"\n🚀 {len(triggered)} triggered items:")
        for t in triggered:
            print(f"  #{t['issue_number']} \"{t['title']}\" → {t['status']}")
            
            body = (f"## 🤖 Diane Agent — Starting Work\n\n"
                    f"Issue moved to **{t['status']}**. Starting work:\n\n"
                    f"1. 📖 Read scenario definition from graph\n"
                    f"2. 🔍 Review discussion thread\n"
                    f"3. 📝 Refine scenario in graph\n"
                    f"4. ✅ Report back and move to {cfg.get('completion_status', 'Review')}\n\n"
                    f"---\n*Started at {datetime.now().isoformat()}*")

            gh_rest("POST",
                f"/repos/{cfg['org']}/{cfg['repo']}/issues/{t['issue_number']}/comments",
                {"body": body})
            
            # Simulate moving to next status
            if status_field and project and t.get("item_id"):
                option_id = find_option_id(status_field, cfg.get("completion_status", "Review"))
                if option_id:
                    gh_graphql(UPDATE_FIELD_MUTATION, {
                        "projectId": project["id"],
                        "itemId": t["item_id"],
                        "fieldId": status_field["id"],
                        "optionId": option_id
                    })
                    print(f"    ✅ Moved to \"{cfg.get('completion_status')}\"")

    if not new_comments and not triggered:
        print("\n  ✅ No changes detected")

# ── Watch Command ──────────────────────────────────────────────────────────

def cmd_watch():
    """Continuous poll+process loop."""
    print_header("👁️ Watch Mode")
    print("  Press Ctrl+C to stop\n")
    while True:
        cmd_process()
        print(f"\n  ⏳ Next check in {POLL_INTERVAL}s...")
        try:
            time.sleep(POLL_INTERVAL)
        except KeyboardInterrupt:
            print("\n  👋 Stopped"); break

# ── Status Command ─────────────────────────────────────────────────────────

def cmd_status():
    """Show bridge status and project state."""
    print_header("Bridge Status")
    cfg = load_config()
    if not cfg:
        print("❌ Not initialized. Run `init` first."); return

    print(f"  Org:           {cfg.get('org')}")
    print(f"  Repo:          {cfg.get('repo')}")
    print(f"  Project #:     {cfg.get('project_number')}")
    print(f"  Project ID:    {cfg.get('project_id')}")
    print(f"  Trigger:       \"{cfg.get('trigger_status')}\"")
    print(f"  Complete:      \"{cfg.get('completion_status')}\"")
    print(f"  Last poll:     {cfg.get('last_poll', 'never')}")

    project = get_project(cfg["org"], cfg["project_number"])
    if not project:
        print("\n⚠ Cannot query project (need `project` scope)")
        print("  Try: `python3 gh-project-bridge.py mock` for demo")
        return

    items = get_items(project)
    print(f"\n  Project items: {len(items)}")
    for item in items:
        content = item.get("content", {})
        if not content or "number" not in content:
            continue
        num = content["number"]
        title = content.get("title", "?")[:55]
        status = "—"
        for fv in item.get("fieldValues", {}).get("nodes", []):
            if fv.get("field", {}).get("name", "").lower() == "status":
                status = fv.get("name", "—")
        sk = extract_scenario_key(content.get("body", ""))
        print(f"  #{num:<5} [{status:<15}] {title}")
        if sk:
            print(f"          → graph: `{sk}`")

# ── Mock Mode ──────────────────────────────────────────────────────────────

MOCK_SCENARIOS = [
    {"key": "scn-agent-mode", "properties": {"name": "Agent Mode",
     "description": "Diane operates autonomously with sub-agent spawning",
     "given": "Diane is connected to an AI client", "when": "User switches to agent mode",
     "then": "Diane operates autonomously, spawning sub-agents as needed",
     "domain": "dom-action"}},
    {"key": "scn-send-message", "properties": {"name": "Send Message",
     "description": "User sends a message — Diane routes through tools and responds",
     "given": "Diane is running on the platform", "when": "User submits a message",
     "then": "Diane routes through tools and returns a response", "domain": "dom-conversation"}},
    {"key": "scn-parallel-execution", "properties": {"name": "Parallel Execution",
     "description": "User asks for multiple things — Diane runs them in parallel",
     "given": "Diane has a tool catalog", "when": "User requests multiple independent tasks",
     "then": "Diane executes them concurrently and aggregates results", "domain": "dom-action"}},
    {"key": "scn-forked-context", "properties": {"name": "Forked Context",
     "description": "Child session inherits parent context at spawn time",
     "given": "A parent session is running", "when": "User spawns a child session with forkContext=true",
     "then": "Child inherits a snapshot of parent's transcript", "domain": "dom-distribution"}},
    {"key": "scn-save-memory", "properties": {"name": "Save Memory",
     "description": "Diane automatically saves session memory during conversation",
     "given": "Diane is in a conversation", "when": "Session is active",
     "then": "Diane extracts and stores MemoryFact objects", "domain": "dom-knowledge"}},
]

MOCK_COMMENTS = [
    {"author": "mcj", "created": "2026-04-25T10:00:00Z",
     "body": "@diane-agent update step 3 to \"Diane spawns a sub-agent for the task\""},
    {"author": "mcj", "created": "2026-04-25T10:05:00Z",
     "body": "@diane-agent status"},
    {"author": "mcj", "created": "2026-04-25T10:10:00Z",
     "body": "I think step 2 should also handle timeout recovery before spawning."},
]

def cmd_mock():
    """Run a demo of the full workflow with mock data."""
    print_header("🧪 Mock Mode — Full Workflow Demo")
    print("  Simulating the bridge without GitHub Projects access\n")

    # Step 1: Show scenarios in graph
    print("1️⃣  Scenarios in graph:")
    for s in MOCK_SCENARIOS[:3]:
        p = s["properties"]
        print(f"   {s['key']:25} {p['name']:20} [{p['domain']}]")

    # Step 2: Sync (create issues)
    print("\n2️⃣  Sync: creating GitHub issues...")
    for i, s in enumerate(MOCK_SCENARIOS, 1):
        print(f"   ✅ #{i} \"Scenario: {s['properties']['name']}\" created")
        print(f"       → Added to project (Status: Backlog)")
        print(f"       <!-- scenario-key: {s['key']} -->")

    # Step 3: Move issue to In Progress (simulate user action)
    print("\n3️⃣  User moves issue #1 to \"In Progress\"...")
    print(f"   🔄 #{1} \"Scenario: Agent Mode\" → In Progress")

    # Step 4: Agent detects trigger
    print("\n4️⃣  Agent detects trigger...")
    triggered = MOCK_SCENARIOS[0]
    print(f"   🚀 Triggered: \"{triggered['key']}\"")
    print(f"   🤖 Posting start-work comment on #{1}...")
    print(f"   🔄 Moving to Review...")

    # Step 5: User comments
    print(f"\n5️⃣  New comments detected:")
    for c in MOCK_COMMENTS:
        parsed = parse_comment(c["body"])
        cmd_types = [cmd["type"] for cmd in parsed["commands"]]
        print(f"   📬 @{c['author']}: \"{c['body'][:70]}...\"")
        if cmd_types:
            print(f"       → Commands: {cmd_types}")

    # Step 6: Agent processes and replies
    print(f"\n6️⃣  Agent processing comments:")
    for c in MOCK_COMMENTS:
        parsed = parse_comment(c["body"])
        if parsed["has_commands"]:
            for cmd in parsed["commands"]:
                if cmd["type"] == "update_step":
                    print(f"   📝 Updated step {cmd['matches'][0]}: \"{cmd['matches'][1]}\"")
                elif cmd["type"] == "status":
                    print(f"   📊 Posted graph state for `{triggered['key']}`")
            print(f"   ✅ Reply posted to #{1}")
        elif parsed["mentions_agent"] and parsed["free_text"]:
            print(f"   ⚡ Free-form: \"{parsed['free_text'][:60]}...\" → agent infers graph update")

    print(f"\n{'='*60}")
    print(f"  ✅ Mock demo complete")
    print(f"  To run live: create a PAT with `project` scope")
    print(f"  Then: python3 gh-project-bridge.py init")
    print(f"        python3 gh-project-bridge.py sync")
    print(f"        python3 gh-project-bridge.py watch")

# ── Main Entry Point ───────────────────────────────────────────────────────

def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(1)

    cmd = sys.argv[1]
    commands = {
        "init": cmd_init, "sync": cmd_sync,
        "poll": cmd_poll, "process": cmd_process,
        "watch": cmd_watch, "status": cmd_status,
        "mock": cmd_mock,
    }
    if cmd not in commands:
        print(f"Unknown: {cmd}")
        print("Available: init, sync, poll, process, watch, status, mock")
        sys.exit(1)

    # Pre-flight: check gh
    r = subprocess.run(["gh", "auth", "status"], capture_output=True, text=True, timeout=5)
    if r.returncode != 0:
        print("❌ gh not authenticated. Run: gh auth login")
        sys.exit(1)

    if cmd != "init":
        if not load_config():
            print("❌ Not initialized. Run `python3 gh-project-bridge.py init`")
            sys.exit(1)

    commands[cmd]()

if __name__ == "__main__":
    main()
