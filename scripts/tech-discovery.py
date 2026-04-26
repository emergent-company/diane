#!/usr/bin/env python3
"""
Technology Discovery Script
Daily run: searches multiple sources for new technologies in:
  - Agentic processing / AI agents
  - Skills / skill systems
  - MCP (Model Context Protocol)
  - Memory / memory management
  - Context management

Outputs JSON to stdout with discovered technologies.
"""

import json
import subprocess
import time
import re
from datetime import datetime
from urllib.request import urlopen, Request
from html.parser import HTMLParser

SEARCH_QUERIES = {
    "agentic-ai": ["AI agent", "autonomous agent", "agent framework", "agentic processing"],
    "skills": ["AI skills", "skill system", "skill library", "agent skills"],
    "mcp": ["MCP server", "Model Context Protocol", "MCP tool", "MCP client"],
    "memory": ["AI memory", "memory management", "vector memory", "knowledge graph memory", "memory layer"],
    "context-management": ["context window", "context management", "context compression", "RAG context"],
}

MONTH_OLD = 30 * 24 * 3600  # 30 days in seconds


def fetch_json(url, headers=None):
    """Fetch JSON from a URL with basic error handling."""
    try:
        req = Request(url, headers=headers or {"User-Agent": "Diane-Tech-Discovery/1.0"})
        with urlopen(req, timeout=15) as resp:
            return json.loads(resp.read().decode())
    except Exception as e:
        return {"error": str(e)}


def fetch_text(url, headers=None):
    """Fetch raw text from a URL."""
    try:
        req = Request(url, headers=headers or {"User-Agent": "Diane-Tech-Discovery/1.0"})
        with urlopen(req, timeout=15) as resp:
            return resp.read().decode()
    except Exception as e:
        return ""


def search_github_trending():
    """Search GitHub for trending repos by topic."""
    results = []
    topics = ["ai-agent", "mcp", "memory", "agent-framework", "llm-agent", "autonomous-agents"]

    for topic in topics:
        url = f"https://api.github.com/search/repositories?q=topic:{topic}+stars:>100+pushed:>2026-01-01&sort=stars&order=desc&per_page=5"
        data = fetch_json(url, {"Accept": "application/vnd.github.v3+json", "User-Agent": "Diane-Tech-Discovery/1.0"})
        if "items" in data:
            for repo in data["items"]:
                results.append({
                    "source": "github",
                    "name": repo.get("full_name", ""),
                    "description": repo.get("description", ""),
                    "url": repo.get("html_url", ""),
                    "stars": repo.get("stargazers_count", 0),
                    "topics": repo.get("topics", []),
                    "language": repo.get("language", ""),
                    "created": repo.get("created_at", ""),
                    "updated": repo.get("updated_at", ""),
                    "categories": [topic],
                })
            print(f"  GitHub {topic}: {len(data.get('items', []))} results", file=__import__('sys').stderr)

    return results


def search_arxiv():
    """Search ArXiv for recent papers on agent memory and context."""
    results = []
    queries = [
        "ti:%22AI+agent%22+AND+%22memory%22",
        "ti:%22context+management%22+AND+%22LLM%22",
        "ti:%22agent+framework%22+AND+%22tool%22",
        "ti:%22memory+consolidation%22+AND+%22agent%22",
    ]

    for query in queries:
        url = f"https://export.arxiv.org/api/query?search_query={query}&sortBy=submittedDate&sortOrder=descending&max_results=5"
        xml_data = fetch_text(url)
        if not xml_data:
            continue

        import xml.etree.ElementTree as ET
        root = ET.fromstring(xml_data)
        ns = {"a": "http://www.w3.org/2005/Atom"}

        for entry in root.findall("a:entry", ns):
            title = entry.find("a:title", ns)
            summary = entry.find("a:summary", ns)
            link = entry.find("a:id", ns)
            published = entry.find("a:published", ns)

            results.append({
                "source": "arxiv",
                "name": (title.text or "").replace("\n", " ").strip() if title is not None else "",
                "description": (summary.text or "").replace("\n", " ").strip()[:500] if summary is not None else "",
                "url": link.text.strip() if link is not None else "",
                "published": published.text.strip() if published is not None else "",
                "categories": [],
            })

        print(f"  ArXiv {query[:40]}: {len(results)} total papers found", file=__import__('sys').stderr)

    return results


def search_hackernews():
    """Search Hacker News for relevant stories from the past week."""
    results = []
    keywords = ["AI agent", "MCP", "memory", "context", "skill system", "agent"]

    # Get top stories from the past week
    data = fetch_json("https://hacker-news.firebaseio.com/v0/topstories.json")
    if isinstance(data, list):
        story_ids = data[:50]
        for sid in story_ids:
            story = fetch_json(f"https://hacker-news.firebaseio.com/v0/item/{sid}.json")
            if story and isinstance(story, dict) and story.get("title"):
                title = story.get("title", "")
                url = story.get("url", f"https://news.ycombinator.com/item?id={sid}")
                score = story.get("score", 0)
                text = story.get("text", "") or ""

                # Check if relevant
                combined = (title + " " + text).lower()
                matched = [kw for kw in keywords if kw.lower() in combined]
                if matched:
                    results.append({
                        "source": "hackernews",
                        "name": title,
                        "description": (text or "")[:300],
                        "url": url,
                        "score": score,
                        "categories": matched,
                    })

    print(f"  HN: {len(results)} relevant stories", file=__import__('sys').stderr)
    return results


def check_blogwatcher():
    """Check blogwatcher-cli for new articles."""
    results = []
    try:
        r = subprocess.run(
            ["blogwatcher-cli", "articles", "--all", "--format", "json"],
            capture_output=True, text=True, timeout=30
        )
        if r.returncode == 0:
            try:
                articles = json.loads(r.stdout)
                for art in articles:
                    results.append({
                        "source": "blogwatcher",
                        "name": art.get("title", ""),
                        "description": art.get("description", "")[:300],
                        "url": art.get("url", ""),
                        "blog": art.get("blog", ""),
                        "published": art.get("published", ""),
                        "read": art.get("read", False),
                        "categories": [],
                    })
            except json.JSONDecodeError:
                pass
    except Exception as e:
        print(f"  blogwatcher error: {e}", file=__import__('sys').stderr)

    print(f"  Blogwatcher: {len(results)} articles", file=__import__('sys').stderr)
    return results


def search_github_trending_repos():
    """Check GitHub trending page for repositories."""
    results = []
    html = fetch_text("https://github.com/trending?since=weekly")
    if not html:
        return results

    # Simple extraction of repo names from trending page
    repo_pattern = re.compile(r'href="/trending[^"]*">([^<]+)</a>', re.IGNORECASE)
    matches = repo_pattern.findall(html)
    for name in matches:
        name = name.strip()
        if name and len(name) > 2:
            results.append({
                "source": "github-trending",
                "name": name,
                "url": f"https://github.com/{name}",
                "categories": [],
            })

    print(f"  GitHub Trending: {len(results)} repos", file=__import__('sys').stderr)
    return results


def deduplicate(results):
    """Simple dedup by name."""
    seen = set()
    deduped = []
    for r in results:
        key = r["name"].lower().strip()
        if key and key not in seen:
            seen.add(key)
            deduped.append(r)
    return deduped


def main():
    print(f"Technology Discovery — {datetime.now().isoformat()}", file=__import__('sys').stderr)
    print(file=__import__('sys').stderr)

    all_results = []

    print("1. GitHub topic search...", file=__import__('sys').stderr)
    all_results.extend(search_github_trending())

    print("\n2. ArXiv papers...", file=__import__('sys').stderr)
    all_results.extend(search_arxiv())

    print("\n3. Hacker News...", file=__import__('sys').stderr)
    all_results.extend(search_hackernews())

    print("\n4. Blogwatcher articles...", file=__import__('sys').stderr)
    all_results.extend(check_blogwatcher())

    print("\n5. GitHub Trending...", file=__import__('sys').stderr)
    all_results.extend(search_github_trending_repos())

    deduped = deduplicate(all_results)
    print(f"\nTotal: {len(all_results)} raw, {len(deduped)} unique", file=__import__('sys').stderr)

    # Output JSON
    output = {
        "timestamp": datetime.now().isoformat(),
        "count": len(deduped),
        "technologies": deduped,
    }
    print(json.dumps(output, indent=2))


if __name__ == "__main__":
    main()
