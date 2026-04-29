# Diane Discord Bot — Production Playbook

## Project

- **Memory Platform project:** `a4c877b6-c562-4d6a-a865-2368e49e84f1` (mcj-diane)
- **Memory Platform server:** `https://memory.emergent-company.ai`
- **Discord server:** b4nglab, channel `1468226969010438257`

## Configuration

Config file: `~/.config/diane.yml`

```yaml
default: default
projects:
  default:
    project_id: a4c877b6-c562-4d6a-a865-2368e49e84f1
    server_url: https://memory.emergent-company.ai
    token: REDACTED_PROJECT_TOKEN
    discord_bot_token: "REDACTED_DISCORD_BOT_TOKEN"
    discord_channel_ids:
      - '1497517847377743973'
    brave_api_key: REDACTED_BRAVE_API_KEY
    generative_provider:
      provider: openai-compatible
      api_key: "none"
      base_url: http://10.10.10.61:8001/v1
      model: Qwen3.5-35B-A3B-UD-Q4_K_XL.gguf
    embedding_provider:
      provider: google
      api_key: AIzaSy...4EAw
```

Both test and production now use **Kvasir** (on-prem Qwen 35B via openai-compatible). See [`/root/infrastructure/docs/diane-provider-setup.md`](../../infrastructure/docs/diane-provider-setup.md) for details.

## Build

```bash
cd ~/diane/server && /usr/local/go/bin/go build -o ~/.diane/bin/diane ./cmd/diane/
sudo cp ~/.diane/bin/diane /usr/local/bin/diane
```

## Run

```bash
# First kill any existing bot:
for pid in $(ps aux | grep "diane bot" | grep -v grep | awk '{print $2}'); do
  kill -9 $pid 2>/dev/null
done
rm -f /root/.diane/bot.pid

# Start:
/usr/local/bin/diane bot --pidfile /root/.diane/bot.pid --restart --restart-delay 5s

# Verify:
sleep 3 && tail -5 /root/.diane/debug.log
```

Expected output: `✅ Discord bot connected.`

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `Authentication failed (close 4004)` | Discord token expired/rotated | Generate new token in Discord Developer Portal → Bot → Reset Token, update `~/.config/diane.yml` |
| `text file busy` on `cp` | Old process still holds binary | `kill -9` all diane processes, wait, then `cp` |
| Bot doesn't respond to messages | Token scopes missing Message Content Intent | Enable in Discord Developer Portal → Bot → Privileged Gateway Intents → Message Content Intent |

## Built-in Tool: set_session_title

The Memory Platform has a hidden built-in tool `set_session_title` that agents can call to update the session title:

```
set_session_title title="🐛 Bug: Login Timeout" session_id="..."
```

The title is stored in the graph session's `Properties.title`. The bot reads it after each agent run via `bridge.GetSession()` and renames the Discord thread if the title changed.

## Thread Naming

**Phase 1 (instant):** Keyword-based emoji prefix on thread creation:
- `❓ Question`, `🐛 Bug`, `✨ Feature`, `🔧 Fix`, `📚 Research`, `💬 Chat`

**Phase 2 (post-response):** Agent calls `set_session_title` → bot reads session title → renames thread.

Discord mention syntax (`<@...>`, `<@&...>`, `<#...>`) is stripped from the thread name.
