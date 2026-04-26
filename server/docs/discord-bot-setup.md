# Diane Discord Bot — Setup Guide

This guide walks you through setting up the Diane Discord bot end-to-end.

## Overview

```
User types in Discord channel
         │
         ▼
Diane Bot (Go) ───► Discord Gateway (WebSocket)
         │
         ├── Create Session (Memory graph)
         ├── Store Messages (Memory graph)
         ├── Search Memory (hybrid search)
         └── Stream Chat (Memory Platform LLM)
```

The bot runs via `diane bot` (the main `diane` CLI binary) that:
- Connects to Discord via WebSocket Gateway
- Manages one session per Discord channel
- Stores every message in the Memory Platform graph (auto-embedded)
- Recalls relevant past conversations via hybrid search
- Calls the Memory Platform's LLM via streaming chat API

## Prerequisites

- A Memory Platform project with an API token (test or real)
- A Discord application with a bot token
- Go 1.25+ (for building from source)

## Step 1: Create a Discord Bot

1. Go to https://discord.com/developers/applications
2. Click **New Application** → give it a name (e.g., "Diane")
3. Go to **Bot** → click **Add Bot**
4. Under **Privileged Gateway Intents**, enable:
   - ✅ **MESSAGE CONTENT INTENT** (required to read messages)
   - ✅ **SERVER MEMBERS INTENT** (optional, for member info)
5. Copy the **Token** — this is your `DISCORD_BOT_TOKEN`

## Step 2: Invite the Bot to a Server

1. Go to **OAuth2** → **URL Generator**
2. Scopes: ✅ `bot` ✅ `applications.commands`
3. Bot Permissions:
   - ✅ `Send Messages`
   - ✅ `Read Messages/View Channels`
   - ✅ `Read Message History`
4. Open the generated URL → authorize the bot in your server

## Step 3: Find Your Channel ID

In Discord, enable **Developer Mode** (Settings → Advanced → Developer Mode).
Right-click a channel → **Copy ID** → you get something like `123456789012345678`.

This is your `DISCORD_CHANNEL_IDS` (comma-separated for multiple channels).
Leave empty to allow all channels the bot can see.

## Step 4: Set Up Memory Platform

The bot uses the same Memory Platform credentials as Diane Master.

### Option A: Use the test project (quick start)

```
MEMORY_SERVER_URL=https://memory.emergent-company.ai
MEMORY_API_KEY=emt_de32b3b3ec43e15a6cc39a5b1e2deee4073d9990230139e66a5ee222f0ec3349
MEMORY_PROJECT_ID=e59a7c1c-6ec9-41aa-9fb4-79071a9569c7
```

### Option B: Create a dedicated project (recommended for production)

```bash
# Create a new project
memory projects create --name diane-production --org <your-org-id>

# Create a token with chat + graph permissions
memory tokens create --project <project-uuid> --name diane-bot --scopes chat:stream,graph:read,graph:write

# Copy the token — it starts with emt_
```

**Note:** The chat streaming API (`/api/chat/stream`) requires the `chat:stream` scope. If your token doesn't have it, the bot will fall back to showing the error instead of responding.

## Step 5: Build and Configure

### From source

```bash
cd ~/diane/server
go build -o ~/.diane/bin/diane ./cmd/diane/
```

### Configuration

Create `~/.diane/.env`:

```env
# ── Discord ──
DISCORD_BOT_TOKEN=MTIzNDU2Nzg5MDEyMzQ1Njc4OQ.Gxxxxx.xxxxxxxxxxxxxxxxxxx
DISCORD_CHANNEL_IDS=123456789012345678,987654321098765432
# Leave empty for all channels, or comma-separate specific ones

# ── Memory Platform ──
MEMORY_SERVER_URL=https://memory.emergent-company.ai
MEMORY_API_KEY=emt_de32b3b3ec43e15a6cc39a5b1e2deee4073d9990230139e66a5ee222f0ec3349
MEMORY_PROJECT_ID=e59a7c1c-6ec9-41aa-9fb4-79071a9569c7
# MEMORY_ORG_ID=your-org-uuid   # optional

# ── Bot behavior (optional) ──
# DISCORD_SYSTEM_PROMPT=You are Diane, a coding assistant...
```

### Alternative: use `.env.local` in the working directory

The bot also reads `.env` and `.env.local` from the current working directory.
This is useful if you already have a `.env.local` with Memory credentials.

## Step 6: Run the Bot

```bash
# Load config from .env file
cd ~/.diane
diane bot

# Or export env vars directly
export DISCORD_BOT_TOKEN="..."
export MEMORY_API_KEY="..."
export MEMORY_PROJECT_ID="..."
diane bot

# Or use a custom .env file location
cd /path/to/project && diane bot
```

Expected output:

```
=== Diane Discord Bot ===
  Discord:   bot configured
  Memory:    https://memory.emergent-company.ai (project: e59a7c1c-...)
  Channels:  [123456789012345678]
  ... connecting ...
✅ Discord bot connected. Press Ctrl+C to exit.
```

## Step 7: Test It

Send a message in the configured Discord channel:

```
User:  Hello Diane!
Diane: Hello! How can I help you today?

User:  What's the capital of France?
Diane: The capital of France is Paris.

User:  What did I just ask you about?
Diane: You asked me about the capital of France. Is there anything else?
```

The bot should:
- ✅ Respond within a few seconds
- ✅ Show a typing indicator while processing
- ✅ Remember the conversation within the session
- ✅ Search past conversations for relevant context

## Architecture Notes

### Session Model

```
Discord Channel ──→ Memory Session
  channel_id          title="Discord #<channel>"
                      status=active|completed
                      messages[]
```

Each Discord channel gets its own session. Multiple channels = multiple independent conversations.

### Message Flow

```
1. User sends message → Discord Gateway
2. Bot receives MESSAGE_CREATE event
3. Bot stores message as Message object in Memory graph (auto-embedded)
4. Bot searches Memory for relevant past context (hybrid search + recency boost)
5. Bot calls chat API with: system prompt + recent history + memory context + current message
6. Bot stores assistant response as Message object
7. Bot sends response to Discord channel
```

### Rate Limiting

- Discord rate limits: handled automatically by discordgo
- Memory Platform rate limits: handled by the SDK
- The bot processes one message at a time per channel (goroutine per message)

## Troubleshooting

### "MEMORY_TEST_TOKEN not set" or similar auth errors
The token needs the `data:read` scope at minimum, and `chat:stream` for LLM responses.

### "Failed to create session"
Check that `MEMORY_PROJECT_ID` is a valid UUID. The test project is `e59a7c1c-6ec9-41aa-9fb4-79071a9569c7`.

### Bot doesn't respond in a channel
- Is the bot in the server? Check the member list.
- Does the bot have `Read Messages` + `Send Messages` permissions?
- Is `DISCORD_CHANNEL_IDS` set? If yes, is the channel ID in the list?
- Check the bot's Gateway Intents — `MESSAGE CONTENT INTENT` must be enabled.

### "StreamChat: [403] forbidden"
The API token doesn't have `chat:stream` scope. Create a token with the right scope:
```bash
memory tokens create --project $PROJECT_ID --name diane-bot --scopes chat:stream,graph:read,graph:write
```

### Bot responds with "I'm sorry, I encountered an error"
The Memory Platform's chat API returned an error. Check:
- Is the token valid and not expired?
- Does the token have `chat:stream` scope?
- Is the Memory Platform server reachable?

## Files Reference

| File | Purpose |
|------|---------|
| `server/cmd/diane/main.go` | CLI entry point, dispatches subcommands |
| `server/internal/discord/bot.go` | Core bot logic (317 lines) |
| `server/internal/memory/bridge.go` | Memory Platform bridge (317 lines) |
| `server/memorytest/bridge_test.go` | Integration tests (276 lines) |
| `server/memorytest/diane_memory_requirements_test.go` | Platform capability tests |

## Next Steps After MVP

1. **Session persistence**: close old sessions, create new ones per day/week
2. **Thread support**: Discord threads = child sessions
3. **File uploads**: Discord attachments → sandbox workspace
4. **Slash commands**: `/search`, `/reset`, `/summary`
5. **MCP tunnel**: Register Diane as MCP server on Memory Platform for tool access
6. **Multi-agent**: Route different message types to different agent definitions
