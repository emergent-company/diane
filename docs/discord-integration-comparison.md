# Discord Integration: Hermes Agent vs Diane

> **Generated:** 2026-05-06  
> **Scope:** Feature-by-feature comparison of the Discord bot implementation in
> Hermes Agent (`gateway/platforms/discord.py`, ~4200 lines) vs Diane
> (`server/internal/discord/bot.go`, ~2655 lines).

---

## Overview

| | Hermes Agent | Diane |
|---|---|---|
| **Language** | Python (asyncio) | Go (goroutines) |
| **Library** | `discord.py` (`commands.Bot`) | `discordgo` raw API |
| **Architecture** | Gateway adapter via `BasePlatformAdapter` — all platforms share a common interface | Monolithic `Bot` struct with a `DiscordAPI` interface-abstraction for testing |
| **Listening** | `on_message` event on `commands.Bot` | `AddHandler(onMessageCreate)` on `discordgo.Session` |
| **Line count** | ~4200 lines (one file) | ~2655 lines (bot.go + discord_api.go + tests) |
| **LLM integration** | Internal agent loop (via `gateway/session.py`) | Memory Platform runtime agents (create → trigger → poll → cleanup) |

---

## Message Handling

| Feature | Hermes | Diane | Notes |
|---|---|---|---|
| **Text batching** | ✅ Merges rapid successive messages (0.6s delay, 2s split ceiling) | ❌ No batching | Reduces Discord rate-limit hits for bursts |
| **Auto-split >2000 chars** | ✅ Split with reply-reference per chunk | ✅ Split at 1900 chars | Both handle Discord's 2000-char limit |
| **Message editing** | ✅ `edit_message()` for streaming/live updates | ❌ | Hermes can progressively update a single message |
| **Typing indicator** | ✅ Persistent 8s re-trigger loop | ✅ Same pattern | Both keep the "bot is typing..." indicator alive during processing |
| **Dedup on reconnect** | ✅ `MessageDeduplicator` class | ✅ In-memory map + cleanup goroutine | Discord RESUME can replay events |
| **Reaction lifecycle** | ✅ 👀 → ✅/❌, configurable via `DISCORD_REACTIONS` env | ✅ 👀 → ✅/❌ (hardcoded) | Both show eyes-while-processing, checkmark-on-success, X-on-failure |

---

## Indicators & Meta Elements (Tool Usage Feedback)

| Feature | Hermes | Diane | Notes |
|---|---|---|---|
| **Typing indicator** | ✅ Persistent 8s loop via `send_typing()` / `stop_typing()` | ✅ Same pattern: `startTyping()` / `stopTyping()` | Both re-trigger every ~8s |
| **Reaction lifecycle** | ✅ 👀 → ✅/❌, configurable via `DISCORD_REACTIONS=false` | ✅ 👀 → ✅/❌ (hardcoded, no toggle) | Diane lacks the env toggle |
| **Processing lifecycle hooks** | ✅ `on_processing_start()` / `on_processing_complete()` — clean separation from message logic | ❌ Reactions inlined in `handleMessage()` — tightly coupled | Hermes can add/remove indicators without touching core logic |
| **Tool progress streaming** | ✅ **Full system:** single message created at first tool, progressively edited with each tool name, dedup tracking with `(×N)`, `__reset__` markers on content breaks | ❌ No tool progress shown; waits silently then sends response | **Biggest UX gap** — during long runs users see only typing indicator + 👀 reaction, no clue what's happening |
| **Progress edit throttling** | ✅ Rate-limited edits (avoids flood control), fallback to new messages on 429 | ❌ N/A | |
| **Long-tool hint** | ✅ After 30s of a single tool, shows onboarding hint suggesting `/verbose` | ❌ N/A | |
| **Typing pause/resume** | ✅ `pause_typing_for_chat()` / `resume_typing_for_chat()` during approval waits | ❌ No pause mechanism | Critical for platforms where typing blocks user input (Slack) |
| **Typing restore after edits** | ✅ Re-sends typing indicator after each progress edit | ❌ N/A | Without this, typing stops during streaming edits |
| **Streaming edit support** | ✅ Gateway edits a single progress message showing tools as they run | ❌ No `edit_message` on Discord adapter | Diane sends everything as new messages |
| **Reply-reference modes** | ✅ `reply_to_mode` = `off` / `first` / `all` | ❌ Always sends new messages, no reply chaining | |
| **Forum channel handling** | ✅ Detect type 15, create thread post with proper content | ❌ | |

### Why This Matters

Hermes's progress streaming means a Discord user sees **live updates** during a run:

```
🛠️ Searching codebase...
🛠️ Analyzing dependencies...
🛠️ Generating report...
```

These lines appear in a single message that gets **edited progressively**, with the typing indicator restored after each edit. If the same tool runs multiple times, it shows `(×3)` on the line. When the LLM starts writing its response, a `__reset__` marker closes the tool bubble and a fresh content message starts.

Diane currently shows:
1. 👀 reaction (instant)
2. Typing indicator (persistent loop)
3. ✅ reaction + response (after 30s–2min)

**Users have no visibility into what the agent is doing** during long runs. The gap is: typing + 👀 are binary signals (thinking / done), but tool progress gives **structured feedback** about what's happening.

---

## Channel / User / Message Filtering

| Feature | Hermes | Diane | Notes |
|---|---|---|---|
| **User allowlist** | ✅ `DISCORD_ALLOWED_USERS` + `DISCORD_ALLOWED_ROLES` | ❌ | Hermes can restrict which users/roles can interact with the bot |
| **Bot message policy** | ✅ `DISCORD_ALLOW_BOTS` = `none` / `mentions` / `all` | ✅ `TestBotIDs` (only specific bots bypass filter) | Diane's approach is simpler; Hermes's is more flexible |
| **Allowed channels** | ✅ `discord.allowed_channels` | ✅ `AllowedChannels` | Both support channel whitelists |
| **Ignored channels** | ✅ `discord.ignored_channels` | ❌ | Bot never responds in these channels |
| **No-thread channels** | ✅ `discord.no_thread_channels` | ❌ (inverse via `ThreadChannels`) | Diane uses a positive list; Hermes uses both positive and negative |
| **Free-response channels** | ✅ Responds without needing @mention | ❌ | Channels where bot responds to any message |
| **Require @mention** | ✅ `discord.require_mention` | ❌ | Bot only responds when explicitly pinged |
| **Multi-agent awareness** | ✅ If other bots are @mentioned but self isn't → ignores | ❌ | Critical for channels with multiple bots |
| **DM support** | ✅ `dm_messages` intent | ✅ `IntentsDirectMessages` | Both handle DMs |
| **System message filtering** | ✅ Ignores thread renames, pins, member joins, etc. | ❌ Only filters on `Author.Bot` | Diane will attempt to process non-message events |

---

## Thread & Conversation Management

| Feature | Hermes | Diane | Notes |
|---|---|---|---|
| **Auto-thread creation** | ✅ `discord.auto_thread` config | ✅ Thread everywhere or per `ThreadChannels` | Both create threads for conversations |
| **No-thread channels** | ✅ `discord.no_thread_channels` | ❌ | Channels where bot responds inline |
| **Thread naming** | ❌ (auto-named by Discord) | ✅ Smart naming: emoji + category + truncated message | Diane generates meaningful thread names |
| **Thread participation tracking** | ✅ Persisted to disk | ✅ SQLite-backed | Both survive restarts |
| **Forum channel support** | ✅ Creates thread posts in forum channels | ❌ | Forums reject direct messages |
| **Session persistence** | ❌ (ephemeral per-chat) | ✅ Full SQLite persistence: channelID → sessionID → agentType | Diane remembers context across restarts |
| **Auto-continue (todos)** | ❌ | ✅ Checks remaining active todos after response, auto-triggers continuation | Diane can work through a todo list autonomously |
| **Dynamic thread rename** | ❌ | ✅ Renames thread to match session title from MP | Diane keeps threads organized |

---

## Commands

| Feature | Hermes | Diane | Notes |
|---|---|---|---|
| **Native slash commands** | ✅ `/ask`, `/reset`, `/status`, `/stop` etc. | ❌ Text commands only | Slash commands are discoverable in Discord UI |
| **Safe slash sync** | ✅ Diff-based, rate-limit aware, bucket-safe | ❌ N/A | Hermes syncs only changed commands, obeys 5 writes/20s bucket |
| **Slash sync policies** | ✅ `safe` / `bulk` / `off` | ❌ N/A | |
| **`/stop` UI** | ✅ Simple stop | ✅ Rich interactive: buttons per thread + Stop All + Cancel | Diane shows which threads are running and lets you pick |
| **`/btw` (todos)** | ❌ | ✅ `create / list / done / cancel` | Diane has built-in todo management |
| **`/set_ask_channel`** | ❌ | ✅ Persists where agent questions go | |

---

## Agent Questions (Interactive — **Diane-Exclusive**)

| Feature | Hermes | Diane | Notes |
|---|---|---|---|
| **Button responses** | ❌ | ✅ One button per option, color-coded (green=yes, red=no) | |
| **Single-select menus** | ❌ | ✅ Dropdown with option selection | |
| **Multi-select menus** | ❌ | ✅ Select multiple options at once | |
| **Text modals** | ❌ | ✅ "✏️ Respond" button → modal for free-text input | |
| **Overflow handling** | ❌ | ✅ First 5 as buttons, rest in "More..." dropdown | |
| **Rich embed presentation** | ❌ | ✅ Blue embed with title, description, footer, timestamp | |
| **Thread routing** | ❌ | ✅ Creates dedicated thread per question, persists runID→channel mapping | |
| **SSE listener** | ❌ | ✅ Listens to MP Notification Platform for `agent_question` events | Hermione delivers questions triggered by other platforms |
| **Question option parsing** | ❌ | ✅ Flexible: `[]interface{}` or `[]map[string]interface{}` | |

---

## Agent Integration

| Feature | Hermes | Diane | Notes |
|---|---|---|---|
| **LLM integration** | Internal agent loop via `gateway/session.py` | MP runtime agents (create → trigger → poll → cleanup) | |
| **Multiple agent types** | ❌ Single agent | ✅ `default`, `diane-codebase`, `diane-researcher` | Diane routes to different agents by context |
| **Agent routing** | ❌ | ✅ Explicit `!codebase` / `!research` prefixes + keyword heuristics | |
| **Session context** | Chat stored in internal messages array | MP sessions for cross-run context | Diane persists conversation state |
| **Todo injection** | ❌ | ✅ Auto-injects pending todos into trigger prompt | |
| **CancelRun API** | ❌ | ✅ Calls MP `CancelRun` on `/stop` | Diane can cancel a remote agent run |
| **Runtime agent lifecycle** | ❌ Coversation loop in same process | ✅ Create runtime agent → trigger → poll → delete | Diane uses stateless, disposable agents |

---

## Voice & Media

| Feature | Hermes | Diane | Notes |
|---|---|---|---|
| **Voice channel connect** | ✅ Join/leave voice channels | ❌ | |
| **TTS in voice** | ✅ `play_tts` routes to VC if connected | ❌ | |
| **Voice receiver (STT)** | ✅ Full RTP/Opus pipeline: NaCl decrypt, DAVE E2EE, PCM→WAV for Whisper | ❌ | Hermes can listen to voice and transcribe |
| **Image batch send** | ✅ Up to 10 per message, URL→BytesIO auto-download | ❌ | |
| **File attachments** | ✅ `_send_file_attachment`, `send_voice` | ❌ | |
| **Audio playback** | ✅ `FFmpegPCMAudio` with PCMVolumeTransformer | ❌ | |

---

## Infrastructure & Safety

| Feature | Hermes | Diane | Notes |
|---|---|---|---|
| **Proxy support** | ✅ `DISCORD_PROXY` env var (REST + WebSocket) | ❌ | |
| **Platform lock** | ✅ Acquire/release prevents duplicate bot instances sharing a token | ❌ | |
| **Allowed mentions control** | ✅ Granular: everyone, roles, users, replied_user — safe defaults (no @everyone) | ❌ Uses Discord defaults | Hermes prevents LLM output from accidentally pinging @everyone |
| **Error resilience** | ✅ Graceful reply-failure handling, retry without reference | ❌ | |
| **Rate-limit awareness** | ✅ Slash command sync respects Discord's per-app bucket | ❌ | |
| **Testing abstraction** | ✅ `FakeDiscordAPI` with call recording | ❌ Uses `discord.py` directly in tests | Diane has better test infrastructure |

---

## Summary: What Diane Should Consider Adding

**High-value features Diane is missing that Hermes has:**

1. **User/role allowlists** — `AllowedUsers` + `AllowedRoles` to restrict bot access
2. **Bot message policy** — `AllowBots` with `none` / `mentions` / `all` modes
3. **Require @mention** — Most servers want bots to respond only when pinged
4. **Ignored channels** — Channels where bot should never respond
5. **Multi-agent awareness** — Don't respond when another bot is being addressed
6. **🔴 Tool progress streaming** — Show agent activity live via a progressively edited message (single message that accumulates tool names, deduplicated)
7. **🔴 Message editing / streaming** — Edit a single message progressively instead of splitting into N chunks; required for tool progress
8. **Forum channel support** — Create thread posts in forum channels
9. **Native slash commands** — `/ask`, `/reset` etc. for discoverability
10. **Image/file attachment handling** — Receive and process user-uploaded images
11. **Proxy support** — `DISCORD_PROXY` for firewalled environments
12. **Allowed mentions safety** — Prevent @everyone/@here pings in LLM output
13. **Processing lifecycle hooks** — Decouple indicator logic from message processing
14. **Reply-reference modes** — Chain message chunks as replies to original user message

**Where Diane already leads Hermes:**

1. **Agent questions** — Full interactive UI (buttons, select menus, modals) — **unique**
2. **Session persistence** — SQLite-backed with cross-restart continuity
3. **Todo management** — Built-in `/btw` command with MP + SQLite dual-store
4. **Multiple agent routing** — Keyword detection with `!prefix` overrides
5. **SSE notification listener** — Receives cross-platform agent questions
6. **Todo auto-continue** — Autonomous todo list processing
7. **Rich stop UI** — Interactive session selection with per-thread buttons
8. **Dynamic thread renaming** — Renames threads to match session titles
9. **Thread naming** — Meaningful names with emoji and category
10. **CancelRun API** — Can cancel remote agent runs from Discord
