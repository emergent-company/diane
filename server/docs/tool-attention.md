# Tool Attention: Dynamic Tool Gating — Research Notes

**Paper:** [Tool Attention: A Dynamic Gating Approach to MCP Tool Integration](https://arxiv.org/abs/2604.21816)

## Summary

Tool Attention proposes a technique for reducing the "MCP Tools Tax" — the token and latency cost of sending all tool schemas to the LLM on every turn. It uses dynamic per-turn gating to select only relevant tools.

## Key Concepts

- **Phase 1: Query Gating** — lightweight selection of top-N tools based on the user message
- **Phase 2: Lazy Schema Loading** — only load full JSON schemas for gated-in tools
- **Hallucination Rejection Gate** — precondition check before executing tool calls (auth state, validity, safety)

## Relevance to Diane

The Tool Attention paper describes a technique that could theoretically apply to any MCP-based agent system. However, **Diane does not implement a session runner** — the session runner is the Memory Platform executor. Diane provides the MCP server (tools) and CLI frontend. Tool gating, if implemented, would be the responsibility of the agent runtime/executor, not Diane itself.

For reference on how the MP executor currently handles tools: see the agent definition's `Tools` whitelist and `BannedTools` fields, which provide static compile-time gating at the definition level rather than dynamic per-turn gating.
