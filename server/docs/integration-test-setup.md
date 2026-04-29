# Diane Integration Test Harness — Setup Guide

## Environment Variables (copy to ~/diane/.env.test)

```bash
# Discord test bot token (FakeHuman)
TEST_BOT_TOKEN=<discord-bot-token>

# Discord channel where tests run
TEST_CHANNEL_ID=<discord-channel-id>

# Diane's Discord user ID — harness watches for her responses
DIANE_BOT_ID=<diane-discord-user-id>

# Memory Platform API credentials (for /btw session_todos)
MEMORY_API_KEY=<memory-platform-api-key>
MEMORY_SERVER_URL=<memory-platform-server-url>
MEMORY_PROJECT=<memory-platform-project-name>
```

## Code Setup

```bash
# Checkout the test harness branch
cd /path/to/diane/server
git checkout feat/diane-test-harness

# Build the test binary
go build -o diane-test ./cmd/diane-test/

# (Optional) Build Diane binary too
go build -o diane ./cmd/diane/
```

## Diane's Server-Side Config

Add to Diane's `config.yml` on the machine running Diane:

```yaml
discord_test_bot_ids:
  - "<test-bot-user-id>"
```

Otherwise Diane ignores messages from the test bot (blocked by `m.Author.Bot`).

## Running Tests

```bash
# Load env vars
export $(grep -v '^#' ~/diane/.env.test | xargs)

# Run all 7 tests
./diane-test

# Run specific tests
./diane-test -test basic-ping,btw-todo

# Verbose mode
./diane-test -v
```

## Test Suite (7 tests)

| Test | What it checks |
|---|---|
| `basic-ping` | 👀 → thread created → ✅ → response |
| `thread-continuation` | Follow-up in thread gets its own 👀 + ✅ |
| `stop-when-idle` | `/stop` when idle → 🛑 → "Nothing running" |
| `thread-stop-active` | `/stop` in active thread → 🛑 **Stopped** |
| `picker-display` | 2 active sessions → `/stop` in parent → selection embed |
| `btw-todo` | Create → list → done → list (verifies completed state) |
| `unconfigured-channel-silent` | Info-only — needs channel filter setup |

## Gotchas

- **The test bot must have Administrator role** in the Discord server (or at minimum: Send Messages, Create Threads, Read Message History, Add Reactions)
- **The test bot's Message Content Intent** must be enabled in Discord Dev Portal
- **Each test cleans up** by archiving leftover threads before running
- **Tests run sequentially** with a 2s gap between them
- **Diane must be running** on the same Discord server and configured with `discord_test_bot_ids`
