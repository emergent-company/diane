# MCP Relay Config — Schema Pack

## Schema: `mcp-relay-config` v1.0.0

Defines two object types for storing MCP relay configuration in the MP graph.

### Object Types

#### MCPProxyConfig
Stores `mcp-servers.json` contents. Master writes, slaves auto-pull on relay startup.

| Property | Type | Description |
|----------|------|-------------|
| `config` | string | Full JSON of `mcp-servers.json` — list of proxied MCP servers |
| `scope` | string | `"all"`, `"master"`, `"slave"`, or specific instance name |
| `version` | integer | Bump to signal instances to re-pull |
| `instance_id` | string | Redundant with scope, kept for query convenience |

#### MCPSecret
Stores a single secret file value.

| Property | Type | Description |
|----------|------|-------------|
| `name` | string | Secret filename (e.g. `github-bot-token.json`) |
| `value` | string | Secret content |
| `scope` | string | Same scope semantics |
| `description` | string | Human-readable |

### Relationship Types

| Name | Source | Target | Description |
|------|--------|--------|-------------|
| `uses_config` | MCPProxyConfig | MCPProxyConfig | Config inheritance (layering) |
| `requires_secret` | MCPProxyConfig | MCPSecret | This config needs this secret |

### Installation

```bash
# Already installed on project b4c8aae0
memory schemas install 57295b25-cf1a-4102-b0a4-412f44bfa98c --project <project-id>

# Seed a default config
memory graph objects create \
  --type MCPProxyConfig \
  --key mcp-proxy-config-all \
  --name "Default Config" \
  --properties '{"scope":"all","version":1,"config":"{\"servers\":[]}"}'
```
