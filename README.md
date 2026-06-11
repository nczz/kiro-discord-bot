# kiro-discord-bot

**A trainable AI agent that lives in Discord — binds to your codebase, remembers your rules, and gets smarter the more you use it.**

This bot connects Discord to [kiro-cli](https://kiro.dev) AI agents via the Agent Client Protocol (ACP) over stdio. It's not a chatbot — it's a full development agent workspace that grows with you.

### This is not a chatbot

Most AI bots start from zero every conversation. kiro-discord-bot is different:

🧠 **Remembers** — Persistent memory rules teach the agent your preferences, coding style, and project conventions. They stick across sessions forever.

⚡ **Adapts** — Flash memory lets you set session-scoped emphasis for the current task, then discard it cleanly.

📂 **Knows your code** — Each channel binds to a project directory. The agent reads/writes code, runs tests, manages infrastructure — in your actual repo.

📐 **Follows your architecture** — Steering files (`.kiro/steering/*.md`) define module boundaries, build commands, and rules the agent must follow.

🔧 **Grows capabilities** — MCP plugins extend what the agent can do: Discord operations, image/video generation, any API you need.

⏰ **Works autonomously** — Cron schedules let the agent check servers, run reports, and automate DevOps on autopilot.

📈 **Gets stronger over time** — Memory + steering + conversation history + MCP tools compound. Day 1 it's helpful. Day 30 it's your team member.

### Train your agent

```
Day 1  — Bind a project, agent starts learning your codebase
         !start /home/user/my-project

Day 3  — Teach it your rules
         !memory add Always respond in Traditional Chinese
         !memory add Commit messages in English, conventional commits format

Day 7  — Add steering files for architecture boundaries
         .kiro/steering/project.md → build commands, module rules, never-do list

Day 14 — Set up autonomous schedules
         /cron → Daily 9am server health check, compare with yesterday

Day 30 — Extend capabilities with MCP plugins
         Discord MCP → agent reads messages, sends notifications across channels
         Media MCP → agent generates images, videos, music, speech
```

### Features

- 💬 Per-channel isolated sessions with project context
- 🔧 Agents read/write code, run commands, manage infrastructure
- 🧠 Persistent memory rules + session-scoped flash memory
- 🔄 Switch models on the fly — per channel, no restart
- ⏰ Cron scheduling + one-time natural language reminders
- 🩺 Auto-healing — dead agents detected and restarted
- 📝 Full JSONL conversation logs for audit and analysis
- 🧵 Thread-based execution with real-time tool visibility
- 🖼️ Image prompt support — uploaded images sent directly to vision model
- 🔄 Session continuity — agent restarts restore full conversation history
- 🎭 Agent modes — switch between modes advertised by kiro-cli

**Created:** 2026-03-21 | **Language:** Go

---

## Deployment Guide

### Prerequisites

- Go 1.21+
- [kiro-cli](https://cli.kiro.dev/install) 1.29+ installed
- kiro-cli authenticated via one of:
  - `kiro-cli login` (interactive, opens browser)
  - `KIRO_API_KEY` environment variable (headless / server deployments)
- A Discord bot token with the following:
  - Scopes: `bot`, `applications.commands`
  - Permissions: View Channels, Send Messages, Add Reactions, Read Message History
  - Privileged Intents: **Message Content Intent** enabled

---

### 1. Create a Discord Bot

1. Go to [Discord Developer Portal](https://discord.com/developers/applications) → New Application
2. **Bot** tab → Reset Token → copy the token
3. **Bot** tab → Privileged Gateway Intents → enable **Message Content Intent**
4. **OAuth2** tab → URL Generator → select scopes: `bot` + `applications.commands`
5. Select permissions: View Channels, Send Messages, Add Reactions, Read Message History
6. Open the generated URL to invite the bot to your server
7. Note your **Guild ID** (right-click server → Copy Server ID, requires Developer Mode)

> ⚠️ **Important:** Make sure the **Interactions Endpoint URL** field (under General Information) is **empty**. If a URL is set there, Discord will send slash command interactions to that URL instead of the bot's gateway connection, and all `/` commands will fail with "application did not respond in time".

---

### 2. Install kiro-cli

```bash
curl -fsSL https://cli.kiro.dev/install | bash
export PATH="$HOME/.local/bin:$PATH"
```

**Authentication** — choose one:

```bash
# Option A: Interactive login (opens browser)
kiro-cli login

# Option B: API key (headless / server — set in .env)
# Get your key from https://kiro.dev/settings → API Keys
# Then add KIRO_API_KEY=your-key to .env
```

---

### 3. Clone and Configure

```bash
git clone https://github.com/nczz/kiro-discord-bot.git
cd kiro-discord-bot

cp .env.example .env
```

Edit `.env`:

```env
DISCORD_TOKEN=your-bot-token
DISCORD_GUILD_ID=your-guild-id
KIRO_CLI_PATH=/home/user/.local/bin/kiro-cli
KIRO_API_KEY=
DEFAULT_CWD=/projects
ALLOWED_CWD_ROOTS=
DATA_DIR=/tmp/kiro-bot-data
ASK_TIMEOUT_SEC=3600
STREAM_UPDATE_SEC=3
THREAD_AUTO_ARCHIVE=1440
THREAD_AGENT_MAX=5
THREAD_AGENT_IDLE_SEC=900
CHANNEL_AGENT_IDLE_SEC=0
KIRO_MODEL=
HEARTBEAT_SEC=60
ATTACHMENT_RETAIN_DAYS=7
ATTACHMENT_MAX_MB=25
CRON_TIMEZONE=Asia/Taipei
USAGE_TIMEZONE=
USAGE_RETENTION_MONTHS=0
BOT_LOCALE=en
DOWNLOAD_TIMEOUT_SEC=120
QUEUE_BUFFER_SIZE=20
MAX_SCANNER_BUFFER_MB=64
KIRO_AGENT=
TRUST_ALL_TOOLS=true
TRUST_TOOLS=
PREFLIGHT_MODE=warn
SKIP_PREFLIGHT=
BOT_PEERS=
KIRO_MCP_CONFIG=

# Optional Discord MCP server env fallback
MCP_DISCORD_ALLOWED_GUILDS=
MCP_DISCORD_ALLOWED_CHANNELS=
MCP_DISCORD_DOWNLOAD_DIR=
MCP_DISCORD_READ_ONLY=false
MCP_DISCORD_ALLOWED_WRITE_TOOLS=
MCP_DISCORD_ALLOW_DESTRUCTIVE=true

STT_ENABLED=false
STT_PROVIDER=groq
STT_API_KEY=
STT_MODEL=
STT_LANGUAGE=
STT_MAX_DURATION_SEC=300
```

| Variable | Description | Default |
|----------|-------------|---------|
| `DISCORD_TOKEN` | Discord bot token | required |
| `DISCORD_GUILD_ID` | Guild ID for instant slash command registration | required |
| `KIRO_CLI_PATH` | kiro-cli binary path or command name resolved from `PATH` | `kiro-cli` |
| `KIRO_API_KEY` | Kiro API key for headless auth (alternative to `kiro-cli login`) | — |
| `DEFAULT_CWD` | Default working directory for agents | `/projects` |
| `ALLOWED_CWD_ROOTS` | Comma-separated cwd allowlist for `/start`, `/cwd`, thread agents, and cron jobs (empty = unrestricted) | `` |
| `DATA_DIR` | Directory for sessions, logs, and attachments | `./data` |
| `ASK_TIMEOUT_SEC` | Agent response timeout (safety net) in seconds | `3600` |
| `STREAM_UPDATE_SEC` | Discord message update interval during streaming | `3` |
| `THREAD_AUTO_ARCHIVE` | Thread auto-archive duration in minutes (60/1440/4320/10080) | `1440` |
| `THREAD_AGENT_MAX` | Max concurrent thread agents (must be > 0) | `5` |
| `THREAD_AGENT_IDLE_SEC` | Thread agent idle timeout in seconds (`0` = disabled) | `900` |
| `CHANNEL_AGENT_IDLE_SEC` | Channel agent idle timeout in seconds (0 = disabled) | `0` |
| `KIRO_MODEL` | Default model ID for kiro-cli (empty = kiro default) | `` |
| `HEARTBEAT_SEC` | Agent health check interval in seconds | `60` |
| `ATTACHMENT_RETAIN_DAYS` | Auto-delete attachments older than N days (0 = keep forever) | `7` |
| `ATTACHMENT_MAX_MB` | Maximum downloaded attachment size per file in MB (0 = no bot-side limit) | `25` |
| `CRON_TIMEZONE` | Timezone for cron schedules (empty = server local) | `` |
| `USAGE_TIMEZONE` | Timezone for `/usage` day/week/month boundaries (empty = `CRON_TIMEZONE`, then server local) | `` |
| `USAGE_RETENTION_MONTHS` | Months of usage ledger files to keep (0 = keep forever) | `0` |
| `BOT_LOCALE` | Bot display language (`en`, `zh-TW`) | `en` |
| `DOWNLOAD_TIMEOUT_SEC` | Attachment download timeout in seconds | `120` |
| `QUEUE_BUFFER_SIZE` | Max queued jobs per channel | `20` |
| `MAX_SCANNER_BUFFER_MB` | Max single-line JSON-RPC buffer from kiro-cli (MB). Increase if agents process many large attachments at once | `64` |
| `KIRO_AGENT` | Agent profile name for kiro-cli `--agent` flag (empty = kiro default) | `` |
| `TRUST_ALL_TOOLS` | Auto-approve all tool permission requests (`true`/`false`) | `true` |
| `TRUST_TOOLS` | Trust only specific tools (comma-separated names). Overrides `TRUST_ALL_TOOLS` when set | `` |
| `PREFLIGHT_MODE` | Startup ACP check behavior: `warn`, `strict`, or `skip` | `warn` |
| `SKIP_PREFLIGHT` | Legacy override; any non-empty value skips startup preflight | `` |
| `BOT_PEERS` | Optional peer bot overrides for multi-bot coordination and handoffs. Peers are auto-discovered from Discord guild bot members first. Format: `Name:userID`, `Name:userID:roleID`, `Name::roleID` for a manual role-only peer, or `!userID` to exclude an auto-discovered bot, e.g. `BuildBot:111111111111111111:222222222222222222,!333333333333333333` | `` |
| `KIRO_MCP_CONFIG` | Optional MCP catalog source. If empty, the bot reads `KIRO_HOME/settings/mcp.json` and then `~/.kiro/settings/mcp.json` as catalog only; runtime agent sessions still use isolated `DATA_DIR/kiro-runtime` and per-channel injected MCP servers | `` |
| `AUDIT_LOG_ENABLED` | Record bot-visible Discord raw events to SQLite without affecting agent routing or context | `true` |
| `AUDIT_LOG_DB` | SQLite audit DB path (empty = `DATA_DIR/audit/discord.sqlite`) | `` |
| `AUDIT_LOG_RETENTION_DAYS` | Days of audit rows to keep (0 = keep forever) | `0` |
| `AUDIT_LOG_QUEUE_SIZE` | Buffered Discord event queue size before dropping audit-only events | `1000` |
| `AUDIT_LOG_RECORD_CONTENT` | Store message content in audit projections and raw JSON (`false` redacts JSON `content` fields) | `true` |
| `AUDIT_LOG_RECORD_TYPING` | Record high-volume Discord typing-start events | `false` |
| `MCP_DISCORD_ALLOWED_GUILDS` | Comma-separated guild IDs the bundled Discord MCP server may access | `` |
| `MCP_DISCORD_ALLOWED_CHANNELS` | Comma-separated channel/thread IDs the bundled Discord MCP server may access | `` |
| `MCP_DISCORD_DOWNLOAD_DIR` | Restrict `discord_download_attachment` writes to this directory (empty = caller-selected directory) | `` |
| `MCP_DISCORD_READ_ONLY` | Block all Discord MCP write tools when `true` | `false` |
| `MCP_DISCORD_ALLOWED_WRITE_TOOLS` | Comma-separated Discord MCP write tool names allowed to run (empty = unrestricted) | `` |
| `MCP_DISCORD_ALLOW_DESTRUCTIVE` | Allow destructive/manage write tools such as delete, pin, edit topic, and remove reaction | `true` |
| `STT_ENABLED` | Enable voice message / audio attachment transcription (`true`/`false`) | `false` |
| `STT_PROVIDER` | STT provider (`groq` or `openai`) | `groq` |
| `STT_API_KEY` | API key for the STT provider (required when `STT_ENABLED=true`) | — |
| `STT_MODEL` | STT model override (empty = provider default: `whisper-large-v3-turbo` for groq, `whisper-1` for openai) | `` |
| `STT_LANGUAGE` | Language hint in ISO-639-1 (e.g. `zh`, `en`). Empty = auto-detect | `` |
| `STT_MAX_DURATION_SEC` | Skip transcription for audio longer than N seconds | `300` |

---

### 4. Build

```bash
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
go build -ldflags "-X main.Version=$VERSION" -o kiro-discord-bot .
```

---

### 5. Start with systemd (recommended)

Install the service (edit paths in the file to match your setup):

```bash
# System-level (root)
sudo cp kiro-discord-bot.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now kiro-discord-bot

# Or user-level (non-root)
mkdir -p ~/.config/systemd/user
cp kiro-discord-bot.service ~/.config/systemd/user/
# Edit WorkingDirectory and EnvironmentFile paths
systemctl --user daemon-reload
systemctl --user enable --now kiro-discord-bot
```

Manage:

```bash
systemctl status kiro-discord-bot     # check status
journalctl -u kiro-discord-bot -f     # follow logs
systemctl restart kiro-discord-bot    # restart
systemctl stop kiro-discord-bot       # stop
```

> For user-level services, add `--user` to all `systemctl` and `journalctl` commands.

---

### 6. Start manually

```bash
# Load .env and run in foreground
export $(grep -v '^#' .env | xargs)
./kiro-discord-bot
```

---

### 7. Deploy with Docker

```bash
docker compose up -d --build
```

Run the release preflight before restarting an existing service:

```bash
scripts/release-preflight.sh
```

For a local authenticated ACP smoke test:

```bash
RUN_ACP_SMOKE=1 KIRO_CLI=/Users/chun/.local/bin/kiro-cli scripts/release-preflight.sh
```

See `docs/release.md` for the full release and deployment checklist.

`docker-compose.yml` uses `network_mode: host` and mounts `~/.kiro` so the bot inherits your kiro login and MCP settings.
The runtime image installs `kiro-cli` during build and defaults `KIRO_CLI_PATH` to `/root/.local/bin/kiro-cli`. For offline or pinned deployments, build your own runtime image with the desired `kiro-cli` version and keep `KIRO_CLI_PATH` aligned.

---

### 8. Grant Channel Permissions

The bot needs explicit permission in each channel it should respond to:

1. Right-click the channel → Edit Channel → Permissions
2. Add the bot role/user
3. Enable: View Channel, Send Messages, Add Reactions, Read Message History

---

## Usage

### Commands

| Command | Description |
|---------|-------------|
| `/start <cwd>` | Advanced: bind channel to a project directory and start agent |
| `/reset` | Reset the current agent session for this channel |
| `/status` | Show agent state, queue length, context usage, session ID |
| `/usage [user]` | Show credit usage for today, this week, and month-to-date |
| `/doctor` | Run deployment diagnostics and ACP preflight |
| `/audit [limit]` | Show recent raw/semantic audit events for the current channel or thread |
| `/mcp manage` | Open the interactive MCP policy panel, including tool scan and tool-level allow/remove controls |
| `/mcp <action> [value]` | Show or update channel MCP policy. Actions: `status`, `enable`, `disable` |
| `/steering <status|create|edit>` | Manage the current channel project's `.kiro/steering/<project>.md` file |
| `/cancel` | Cancel the currently running task |
| `/interrupt` | Interrupt a stuck current task; starts with `/cancel`, then tries a process interrupt if still active |
| `/cwd` | Open the private project/CWD panel; choose or create a project without typing a full path |
| `/pause` | Switch the channel to mention-only inline mode for new tasks |
| `/back` | Resume full-listen mode and enable new task threads |
| `/thread [on|off]` | Show or set whether new channel tasks open Discord threads |
| `/silent` | Show silent mode status (compact tool output, default: on) |
| `/silent on` | Enable compact tool output |
| `/silent off` | Show full tool details |
| `/model` | Show current model |
| `/model <model-id>` | Switch model (dynamic switch, no restart if supported) |
| `/models` | List all available models |
| `/agent` | List available agent modes |
| `/agent <mode-id>` | Switch agent mode (for example `kiro_default`, `kiro_planner`, `kiro_guide`) |
| `/cron` | Add a scheduled task (opens form) |
| `/cron-list` | List scheduled tasks with action buttons |
| `/cron-run <name>` | Manually run a scheduled task |
| `/cron-prompt <description>` | Create a scheduled task using natural language |
| `/remind <time> <content>` | Set a one-time reminder (tags you when due) |
| `/compact` | Compress conversation history to free context |
| `/clear` | Clear conversation history |
| `/close-thread <thread_id>` | Close an inactive thread agent in this channel scope |
| `/memory` | Manage persistent memory rules (add/list/remove/clear) |
| `/flashmemory` | Manage session-scoped flash memory (add/list/remove/clear) |

All commands also work with `!` prefix (e.g. `!status`, `!reset`).

When a command is used inside a Discord thread, it targets the thread agent when that is the least surprising behavior: `/status`, `/reset`, `/cancel`, `/interrupt`, `/compact`, `/clear`, and `/model` operate on the current thread agent. `/pause`, `/back`, and `/silent` apply to the current target, so a thread can override the listen behavior captured when it was created. `/thread` always applies to the parent channel's future new-task behavior. `/memory` and `/flashmemory` remain scoped to the parent channel because thread agents inherit that memory block.

Channel setup and scheduling commands must be run in the parent channel: `/start`, `/cwd`, `/steering`, `/agent`, `/resume`, `/cron`, `/cron-list`, `/cron-run`, `/cron-prompt`, and `/remind`.

New parent channels must be initialized before agent work starts. The first normal message in an uninitialized channel is held back and prompts a channel manager to open the private `/cwd` setup panel. Initial setup can only select or create a project under `DEFAULT_CWD`; the setup panel lists first-level directories under `DEFAULT_CWD` and paginates them when Discord's select-menu limit is reached. Selecting a project opens a confirmation step before the channel CWD is changed. Creating a project also creates `.kiro/steering/`. After setup completes, the success message keeps the channel setup closed and offers private shortcuts to review MCP tool access and create the agent context file. Agent-starting or agent-context-changing commands such as `/start`, `/reset`, `/compact`, `/clear`, model/agent switches, MCP policy changes, agent context changes, agent memory changes, `/cron`, `/cron-run`, `/cron-prompt`, and agent-backed reminders are rejected until initialization is complete. After a channel is initialized, managers can still use `/cwd` as an advanced control to change to another allowed path through the regular cwd allowlist policy.

**Thread-only commands** (inside a thread):

| Command | Description |
|---------|-------------|
| `!close` | Close the thread agent |
| `!cancel` | Cancel the thread agent's current task |
| `!interrupt` | Interrupt the thread agent's stuck current task |
| `!reset` | Restart the thread agent |
| `!pause` | Switch thread to mention-only mode |
| `!back` | Resume thread full-listen mode |
| `!thread [on\|off]` | Show or set whether new parent-channel tasks open Discord threads |
| `!silent` | Show thread silent mode status |
| `!silent on` | Enable compact tool output in this thread |
| `!silent off` | Show full tool details in this thread |
| `!compact` | Compress thread agent's conversation history |
| `!clear` | Clear thread agent's conversation history |
| `!close-thread <thread_id>` | Close an inactive thread agent in the parent channel scope |
| `!model` | Show thread agent's current model |
| `!model <model-id>` | Switch thread agent's model |
| `!models` | List all available models |
| `!audit [limit]` | Show recent audit events for this thread |

All thread commands also work as `/` slash commands inside a thread.

### Sending Tasks

**Full-listen mode (default):** Any message in the channel is sent to the agent. When peer bot discovery finds another bot that can respond in the current channel or thread, including through inherited permissions, that target automatically switches to mention-only behavior to prevent bot-to-bot loops.

**Mention mode (after `/pause` or automatic multi-bot mode):** Only `@BotName your message` triggers the agent. Use a real Discord mention such as `<@111111111111111111>` or pick the bot from Discord's mention UI; plain text like `@BuildBot` may not trigger the target bot. In a parent channel, `/pause` also disables new task threads so mentioned work replies in-channel with emoji progress; `/back` restores full-listen mode and enables new task threads again.

Use `/back` or `!back` on the target bot to open full-listen mode for that channel and enable future task threads, even when multi-bot mode made mention-only the default. A thread can still override its own listen behavior with `/pause` or `!pause`, and can return to full-listen with thread-local `/back` or `!back`.

**Thread mode:** By default, each parent-channel task automatically creates a Discord Thread from your message. Tool execution status and the final response are posted in the thread, keeping the main channel clean. `/thread off` or parent-channel `/pause` stops opening new threads; new tasks must mention the bot, run on the channel's main agent, show progress by rotating reactions such as `🔄`, `💭`, `✨`, `🛠️`, and `⚙️`, then post only the final reply in the channel. `/thread on` or parent-channel `/back` restores new task threads.

**Thread discussions:** You can continue chatting with the agent inside any thread. A dedicated agent is spawned per thread with the original task context injected. Thread agents are independent from the main channel agent, so both can work in parallel. A thread keeps the listen mode captured when it was created; changing the parent channel's thread mode later does not silently change old threads. Manually created or unknown threads under `/thread off` default to mention-only until overridden in that thread. Inactive thread agents are automatically closed after idle timeout (`THREAD_AGENT_IDLE_SEC`) or when an inactive thread is archived. Capacity limits never close thread agents automatically: if all slots are full, the bot reports active/inactive counts and lists inactive candidates so a user can choose which one to close with `/close-thread thread_id:<id>`. Active work is never evicted by idle cleanup, archive events, or capacity limits; archived active threads are closed after the current job returns to idle. Use `!close` in a thread to manually close its agent.

**Cancel vs interrupt:** `/cancel` sends ACP `session/cancel` for the current task. `/interrupt` first does the same soft cancel, waits briefly, and only if the same task is still active tries `SIGINT` on the agent process group so a stuck tool subprocess can be interrupted. A repeated `/interrupt` on the same still-running task can try another `SIGINT`. It does not clear persisted session metadata or close the Discord thread; if the agent exits, the manager's normal restart/load path handles the next message.

**Multi-bot handoff:** Peer bots are auto-discovered from Discord guild bot members at startup using the full guild member list, including their bot role when available. `BOT_PEERS` is only needed to override a discovered name/role, add a bot that discovery cannot see, or exclude an unrelated bot with `!userID`. Automatic multi-bot mention-only mode applies when another peer bot can effectively respond in the current channel or thread, whether that means sending in the channel or creating a public thread and replying there. That access may come from an explicit channel overwrite, inherited role permissions, or `@everyone`. User mentions such as `<@111111111111111111>` and discovered or configured role mentions such as `<@&222222222222222222>` both route to the target bot. Bot-authored messages are ignored by default; a peer bot handoff is only accepted inside a thread when the target bot is explicitly mentioned, the original task message already has the done reaction (`✅`), and the message is not just progress, error, timeout, or empty output. Normal thread tasks include recent Discord thread messages as bounded context. Accepted cross-bot handoffs include a longer thread transcript as handoff context, so the receiving bot can understand the task, prior decisions, files, results, and remaining work before acting. This lets one bot ask another bot to continue work after the first bot has finished, without responding to every intermediate status update.

Run `/doctor` in the target channel or thread to verify Discord permissions, configured peers, and whether the current context is open, open by `/back` override, or automatic multi-bot mention-only mode.

Slash commands are registered at guild scope, but the bot rejects command invocations in channels or threads where its Discord user cannot view and respond. Administrative commands also set default Discord member permissions so they are hidden from most users in the command picker by default. For channel-specific command picker visibility, configure application command permissions for the app in Discord or through an OAuth2 token with `applications.commands.permissions.update`.

Operational panels such as `/mcp manage`, `/steering`, and `/cron-list`, plus sensitive utility replies such as `/cwd`, `/status`, `/usage`, `/doctor`, `/audit`, `/models`, `/memory`, and `/flashmemory`, are sent as private interaction responses where Discord supports ephemeral messages. Agent task results and explicit channel behavior changes remain visible in the target channel or thread.

### Status Indicators

| Reaction | Meaning |
|----------|---------|
| ⏳ | Queued |
| 🔄 | Processing |
| ⚙️ | Running a tool |
| ✅ | Done |
| ❌ | Error |
| ⚠️ | Timed out |

### Thread Visibility

Each task runs in a Discord thread. The bot posts the full work process in real-time:

| Event | Display |
|-------|---------|
| Tool start | Kind icon (📖 read, ✏️ edit, ▶️ execute, 🔍 search, 🌐 fetch) + title + affected files |
| Tool result | Full output in code block (up to 1900 chars per message) |
| Tool failure | ❌ title + error output |
| Agent thinking | 💭 thought content |
| Final response | Complete text, auto-split if > 2000 chars |

**Silent mode** (default: on) shows compact output. Tool start messages show only an icon plus a short title, execute commands show a short escaped prefix such as `Running: ssh n200 ...`, tool results and thoughts are hidden, and failures show a one-line summary. Use `/silent off` for full detail. `/silent` without an argument only shows the current status. Silent mode is stored in memory only, so it resets to on after the bot restarts. Threads have their own silent setting and do not inherit a parent channel's `/silent off`.

### Recovery

If a response is cut off, use `!resume` to re-post the agent's last output.

---

## Architecture

```
Discord User
    │ message / slash command
    ▼
Discord Bot (Go)
    ├── scoped SessionStore        { agentName, sessionId, cwd, botID, channel/thread target }
    ├── per-channel JobQueue       buffered chan, FIFO
    ├── per-channel Worker         goroutine, async thread-based execution
    ├── per-thread Agent (on demand) isolated context, auto-cleanup on idle/archive
    ├── per-channel ChatLogger     JSONL conversation log
    └── Heartbeat                  background maintenance loop
          ├── HealthTask           agent liveness check + auto-restart
          ├── CleanupTask          expired attachment removal
          ├── CronTask             scheduled jobs + one-shot reminders
          └── ThreadCleanupTask    idle thread agent eviction
                │
                ▼
          Temp Agent (per job)     isolated context, auto-cleanup
                │
                ▼
kiro-cli acp --trust-all-tools   (one process per channel, stdio JSON-RPC)
          │
          ▼
    AWS Bedrock / Anthropic
```

---

## Project Structure

```
kiro-discord-bot/
├── main.go              main bot entrypoint plus mcp-proxy / mcp-bot subcommands
├── config.go
├── kiro-discord-bot.service  systemd service template
├── bot/
│   ├── bot.go            Discord init, Ready handler, slash command registration
│   ├── handler.go        message routing, slash command handlers
│   ├── handler_cron.go   /cron Modal + /cron-list Button + /cron-prompt + /remind handlers
│   ├── cron_parse.go     natural language → cron job parser (temp agent + validation loop)
│   ├── notifier.go       shared botNotifier (Notify+IsSilent) for all adapters
│   ├── health_adapter.go heartbeat ↔ manager bridge
│   ├── cron_adapter.go   cron task ↔ manager bridge
│   ├── thread_cleanup_adapter.go  thread cleanup ↔ manager bridge
│   └── channel_cleanup_adapter.go channel idle cleanup ↔ manager bridge
├── channel/
│   ├── manager.go        per-channel session + worker lifecycle
│   ├── session.go        session struct + JSON persistence
│   ├── worker.go         job queue worker goroutine
│   ├── logger.go         JSONL conversation logger
│   └── memory.go         persistent per-channel memory store
├── heartbeat/
│   ├── heartbeat.go      background task loop
│   ├── task.go           Task interface
│   ├── health.go         agent liveness check + auto-restart
│   ├── cleanup.go        expired attachment removal
│   ├── cron.go           cron scheduler + temp agent execution
│   ├── cron_store.go     cron job persistence (JSON) + pending MCP actions
│   ├── schedule.go       natural language → cron/time parser
│   ├── thread_cleanup.go idle thread agent eviction
│   └── channel_cleanup.go idle channel agent eviction
├── acp/
│   ├── agent.go          ACP agent process management (spawn, handshake, ask, stop)
│   ├── jsonrpc.go        JSON-RPC 2.0 ndjson transport
│   ├── ringbuf.go        thread-safe ring buffer for stderr capture
│   └── protocol.go       ACP protocol constants and types
├── stt/
│   └── stt.go            Speech-to-text client (Groq / OpenAI Whisper)
├── internal/
│   ├── botegress/        safe Discord egress queue + file sanitization
│   ├── botmcp/           built-in bot-tools MCP server
│   ├── secrets/          bot-side secret redaction
│   └── textutil/         UTF-8 safe text helpers
├── cmd/
│   ├── mcp-discord/
│   │   └── main.go       Discord MCP server (optional)
│   └── mcp-media/
│       ├── main.go        Media generation MCP server
│       ├── provider.go    Interfaces and types
│       ├── registry.go    Model routing
│       ├── gemini.go      Google Gemini provider
│       └── openai.go      OpenAI provider
├── .kiro/
│   └── steering/
│       └── discord-mcp.md  agent steering (install to ~/.kiro/steering/)
├── INSTALL_MCP.md          MCP server install guide (for agent)
├── Dockerfile
├── docker-compose.yml
├── .env.example
└── README.md
```

---

## Optional: Media Generation MCP Server

This project includes a Media Generation MCP Server (`cmd/mcp-media/`) that gives the kiro agent the ability to generate images, videos, music, and speech using Google Gemini and OpenAI APIs.

### Supported Capabilities

| Tool | Description | Providers |
|------|-------------|-----------|
| `generate_image` | Text-to-image generation | Gemini (Nano Banana 2/Pro), OpenAI (GPT Image 2/1) |
| `edit_image` | Edit images with natural language | Gemini, OpenAI |
| `generate_video` | Text/image-to-video generation | Gemini (Veo 3.1/3.1 Lite) |
| `generate_music` | Text-to-music generation | Gemini (Lyria 3 Pro/Clip) |
| `text_to_speech` | Text-to-speech synthesis | OpenAI (tts-1-hd), Gemini (3.1 Flash TTS) |
| `list_models` | List all available models with descriptions and cost tiers | All |

### Quick Install

```bash
# 1. Build the MCP server binary
go build -o mcp-media-server ./cmd/mcp-media/

# 2. Register in kiro MCP settings
# Add the following to ~/.kiro/settings/mcp.json under "mcpServers":
```

```json
"mcp-media": {
  "command": "/absolute/path/to/mcp-media-server",
  "env": {
    "GEMINI_API_KEY": "your-gemini-api-key",
    "OPENAI_API_KEY": "your-openai-api-key"
  }
}
```

Get your API keys:
- Gemini: [Google AI Studio](https://aistudio.google.com/apikey) (free tier available)
- OpenAI: [OpenAI Platform](https://platform.openai.com/api-keys)

```bash
# 3. Enable it per channel
# Use /mcp status to confirm the server is in the checklist, then /mcp enable server:mcp-media
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `GEMINI_API_KEY` | Google Gemini API key | — |
| `OPENAI_API_KEY` | OpenAI API key | — |
| `MEDIA_DEFAULT_IMAGE_MODEL` | Default image model | `nano-banana-2` |
| `MEDIA_DEFAULT_TTS_MODEL` | Default TTS model | first registered |
| `MEDIA_OUTPUT_DIR` | Directory for generated files | `/tmp/mcp-media` |

### Available Models

**Image:** `nano-banana-2`, `nano-banana-pro`, `gpt-image-2`, `gpt-image-1`
**Video:** `veo-3.1`, `veo-3.1-lite`
**Music:** `lyria-3-pro`, `lyria-3-clip`
**TTS:** `tts-1-hd`, `tts-1`, `gemini-tts`

Only providers with configured API keys are registered. If only `GEMINI_API_KEY` is set, only Gemini models are available.

---

## Optional: Discord MCP Server

This project includes a built-in Discord MCP Server (`cmd/mcp-discord/`) that gives the kiro agent direct access to Discord — read messages, send messages, list channels, search, add reactions, etc.

Once enabled, the agent can proactively interact with Discord instead of only responding to forwarded messages.

### Additional Permissions for MCP

The MCP server uses Discord REST APIs beyond what the base bot needs. Before enabling, add these to your bot:

**Extra Bot Permissions:**
- Attach Files — `discord_send_file`
- Embed Links — `discord_send_embed`
- Manage Messages — `discord_delete_message`, `discord_pin_message`, `discord_remove_reaction`
- Create Public Threads — `discord_create_thread`
- Manage Channels — `discord_edit_channel_topic`

**Extra Privileged Intent:**
- **Server Members Intent** — required by `discord_list_members`

> Go to [Discord Developer Portal](https://discord.com/developers/applications) → your app → **Bot** tab to enable the intent, and regenerate the OAuth2 invite URL with the additional permissions.

### Safety Scope

The bot does not expose catalog MCP servers to agents by default. It reads MCP server definitions from `KIRO_MCP_CONFIG`, `KIRO_HOME/settings/mcp.json`, or `~/.kiro/settings/mcp.json` as a catalog, starts runtime agents with isolated `KIRO_HOME=DATA_DIR/kiro-runtime`, and injects only the MCP servers allowed for the current Discord channel. Enabled servers are launched through the bot's MCP policy proxy, which filters `tools/list` and blocks unauthorized `tools/call` requests before the agent can see or call those tools.

The bot also registers a built-in `bot-tools` MCP catalog entry backed by the same bot binary (`mcp-bot`). It exposes bot data-directory metadata tools, safe Discord egress tools (`bot_send_message`, `bot_send_file`), and cron management tools (`bot_create_cron`, `bot_list_cron`, `bot_delete_cron`). Like external catalog servers, it stays disabled until a channel manager explicitly enables it with `/mcp manage` or `/mcp enable server:bot-tools`; use `/mcp manage` for tool-level access if you want metadata-only, safe egress, or non-destructive cron access.

Built-in `bot-tools` tools:

| Tool | Access hint | Description |
|------|-------------|-------------|
| `bot_data_summary` | read-only | Summarize data directory metadata without returning message content |
| `bot_list_channel_data` | read-only | List channel data directories and metadata file presence |
| `bot_list_cron` | read-only | List scheduled cron jobs for a channel |
| `bot_send_message` | write, non-destructive | Queue a Discord message for bot-side secret redaction and delivery to the bound channel |
| `bot_send_file` | write, non-destructive | Queue a local text file for bot-side sanitization and upload as a redacted copy |
| `bot_create_cron` | write, non-destructive | Queue creation of a recurring cron job for ingestion by the scheduler |
| `bot_delete_cron` | write, destructive | Queue deletion of a cron job; deletion is accepted only for the owning channel |

Channel managers can control the current channel policy from Discord:

```text
/mcp status
/mcp status server:<server>
/mcp enable server:<server>
/mcp disable server:<server>
```

Use `/mcp status` for a combined catalog and policy checklist. `/mcp enable` exposes the entire selected server. Use `/mcp manage` for tool-level control and MCP reload: the panel can scan a server's current `tools/list`, cache those tools in SQLite, expose select menus for allow/remove operations, and stop active agents so the next run loads the current MCP policy. The raw `allow-tool` and `deny-tool` commands are intentionally not exposed because they require users to type exact tool names and cannot provide a good Discord UX.

Upgrade compatibility is handled once per data directory. On fresh installs, catalog MCP servers stay disabled until a channel manager enables them. On upgrades from versions that inherited Kiro MCP config globally, the bot preserves legacy behavior only for channels already known in `sessions.json`: the catalog servers present during the first upgraded startup are enabled with full server access for those existing channels. The migration is idempotent, does not override explicit channel policies, and does not auto-enable MCP servers added after the migration.

Policy changes stop active channel/thread agents in that channel scope so the next message starts with the current MCP policy.

The same commands are available as `!mcp ...`. Policy changes use Discord effective permissions: the caller must be able to manage the current channel, or the parent channel when used inside a thread. `!cwd` and `/cwd` use the same management-permission check because the current working directory can expose project paths. If Discord permissions change later, the next command uses the new effective permissions.

Set environment allowlists as a defense-in-depth fallback before enabling the server in a workspace with broad Discord access:

```env
MCP_DISCORD_ALLOWED_GUILDS=123456789012345678
MCP_DISCORD_ALLOWED_CHANNELS=234567890123456789,345678901234567890
MCP_DISCORD_DOWNLOAD_DIR=/tmp/kiro-discord-mcp
MCP_DISCORD_ALLOW_DESTRUCTIVE=false
```

Bot channel policy is MCP-server agnostic: it enables catalog servers and filters visible/callable tools through the MCP policy proxy, but it does not inject server-specific environment overrides. For the bundled Discord MCP server, configure these env guards in the catalog command environment or the sourced `.env` file. When a guild allowlist is set, channel tools resolve the channel and reject channels outside allowed guilds. When a channel allowlist is set, channel and thread tools only operate on those IDs. `discord_download_attachment` only downloads from Discord attachment/CDN hosts; `MCP_DISCORD_DOWNLOAD_DIR` restricts where files can be written.

For stricter deployments, set `MCP_DISCORD_READ_ONLY=true` to block every write tool, or set `MCP_DISCORD_ALLOWED_WRITE_TOOLS` to a comma-separated list such as `discord_send_message,discord_reply_message`. Set `MCP_DISCORD_ALLOW_DESTRUCTIVE=false` to block delete/edit/pin/topic/reaction-removal operations while still allowing non-destructive sends.

### Quick Install

```bash
# 1. Build the MCP server binary
go build -o mcp-discord ./cmd/mcp-discord/

# 2. Install the steering file (global, so it works in any project directory)
mkdir -p ~/.kiro/steering
cp .kiro/steering/discord-mcp.md ~/.kiro/steering/discord-mcp.md

# 3. Register in kiro MCP settings
# Add the following to ~/.kiro/settings/mcp.json under "mcpServers":
```

```json
"mcp-discord": {
  "command": "sh",
  "args": [
    "-c",
    "set -a && . /absolute/path/to/kiro-discord-bot/.env && exec /absolute/path/to/kiro-discord-bot/mcp-discord"
  ]
}
```

Replace `/absolute/path/to/kiro-discord-bot` with the actual project directory.

```bash
# 4. Enable it per channel
# Use /mcp status to confirm the server is in the checklist, then /mcp enable server:mcp-discord
```

### Auto-Install via Agent

You can also ask the agent to install it by sending this message in Discord:

> Read INSTALL_MCP.md and follow the steps to install the Discord MCP server.

The agent will read the guide, build the binary, update `mcp.json`, and prompt you to restart.

### Available Tools (after enabled)

| Tool | Description |
|------|-------------|
| `discord_list_channels` | List text channels in a guild |
| `discord_read_messages` | Read recent messages from a channel |
| `discord_send_message` | Send a message to a channel |
| `discord_reply_message` | Reply to a specific message |
| `discord_add_reaction` | Add a reaction emoji to a message |
| `discord_list_members` | List members of a guild |
| `discord_search_messages` | Search recent messages by keyword |
| `discord_channel_info` | Get detailed info about a channel |
| `discord_send_file` | Upload a local file to a channel as an attachment |
| `discord_list_attachments` | List file attachments from recent messages |
| `discord_download_attachment` | Download a Discord attachment to a local file |
| `discord_edit_message` | Edit a message |
| `discord_delete_message` | Delete a message |
| `discord_get_message` | Get a single message by ID |
| `discord_send_embed` | Send a rich embed message |
| `discord_pin_message` | Pin or unpin a message |
| `discord_create_thread` | Create a thread from a message |
| `discord_list_threads` | List active threads in a guild |
| `discord_remove_reaction` | Remove a reaction from a message |
| `discord_get_reactions` | Get users who reacted with a specific emoji |
| `discord_edit_channel_topic` | Edit a channel's topic |
| `discord_list_roles` | List roles in a guild |
| `discord_get_user` | Get info about a specific user |

---

## Notes

- **Session persistence:** Channel and thread agents persist ACP session IDs in `DATA_DIR/sessions.json`. When kiro-cli advertises `loadSession`, bot restart first tries `session/load` for the stored ACP session. If loading is unavailable or fails, the bot creates a new session and injects recent JSONL/Discord conversation history into the first prompt. Stored session keys are scoped by guild, bot identity, target type, and target ID; legacy channel-only keys are still read as a migration fallback.
- **MCP servers:** The bot treats Kiro MCP config as a catalog only, then adds built-in catalog entries such as `bot-tools`. Runtime agents use isolated `KIRO_HOME=DATA_DIR/kiro-runtime` and receive per-session `mcpServers` based on the current channel policy. Default policy is deny all MCP servers. Enabled servers are always launched through the bot MCP policy proxy, because Kiro `disabledTools` is not used as a security boundary.
- **Built-in bot-tools MCP:** `bot-tools` is served by the bot binary through the `mcp-bot` subcommand. It exposes metadata tools, safe Discord egress tools, and cron management tools. Metadata/list tools are read-only. `bot_send_message` and `bot_send_file` queue pending egress actions that the main bot delivers only after secret redaction; file delivery uploads a sanitized text copy and refuses binary or oversized files instead of uploading the original. `bot_create_cron` queues a non-destructive pending create action; `bot_delete_cron` queues a destructive pending delete action. Use `/mcp manage` for tool-level access instead of enabling the whole server when only metadata or safe egress access is needed.
- **Secret redaction:** Bot-side Discord text egress redacts known sensitive environment values and common secret assignments such as API keys, tokens, bearer credentials, passwords, and credentials. This is a final output guard; it does not prevent the agent process from reading files it can otherwise access.
- **Discord MCP scope:** `mcp-discord` is one catalog server with extra defense-in-depth guards. `/mcp` and `!mcp` manage it like any other server: enable/disable the server or manage exact tools from `/mcp manage`. Its env-level guild/channel/read-only/write/destructive restrictions remain a lower-level fallback for direct/manual MCP launches.
- **Agent context:** Add `.kiro/steering/*.md` in the project directory or `~/.kiro/steering/` globally to inject reusable context into the agent. New projects created from the Discord `/cwd` setup panel automatically get `.kiro/steering/`. Use `/steering create` or the setup completion shortcut to fill in reusable background, workflow, references, limits, and other context first, then create `.kiro/steering/<project>.md` with `inclusion: always`; empty optional fields are omitted from the generated file. Use `/steering edit` for full Markdown editing when the file fits Discord's modal size limit.
- **CWD allowlist:** Set `ALLOWED_CWD_ROOTS` to restrict all agent working directories to approved roots. Docker defaults this to `/projects`. Initial channel setup is stricter: project selection and creation are limited to `DEFAULT_CWD`; later manager-initiated `/cwd` changes use the regular allowlist policy.
- **Long responses:** Automatically split into multiple messages at the Discord limit. Bot output is normalized as Discord-safe Markdown: headings are downgraded to bold text, code blocks are closed and reopened across message parts, and part prefixes stay outside code blocks.
- **Conversation logs:** All user/agent interactions are recorded in `DATA_DIR/ch-<channelID>/chat.jsonl`.
- **Agent metrics:** Completed agent executions display a `⚡` metrics footer when ACP turn metrics are available. The usage ledger stores Discord message IDs and slash-command interaction IDs separately (`message_id`, `interaction_id`) plus a generic `invocation_id` for cross-audit correlation.
- **Raw Discord audit DB:** Bot-visible Discord events are recorded independently in SQLite at `DATA_DIR/audit/discord.sqlite` by default. The audit recorder stores append-only `discord_events` rows plus query projections for messages, attachments, reactions, and threads. It also records semantic bot events such as command invocations, command response delivery success/failure, agent job lifecycle, and agent final responses in `bot_audit_events`. Slash-command initial responses, deferred followups, and cron/reminder command responses all use the same delivery success/failure audit path. High-volume typing-start events are disabled by default. Audit data does not trigger the agent and is never injected into conversation context unless an explicit command or future tool reads it.
- **Audit correlation IDs:** In `bot_audit_events`, `message_id` and `interaction_id` identify the user invocation that triggered a bot command. When Discord returns a bot response message object, the actual bot response message ID is stored in metadata as `response_message_id`; initial interaction responses and modals do not expose a Discord message ID, so they store `interaction_response_type` instead. Cron agent `response_sent` reflects final response message delivery, not just whether a thread exists.
- **Audit permissions:** `/audit` and `!audit` use Discord effective channel permissions instead of a separate ACL. The caller must be able to manage the current target channel/thread, either directly or through the parent channel for threads. Discord permission changes take effect on the next audit query.
- **Attachments:** Stored in `DATA_DIR/ch-<channelID>/attachments/` with timestamp prefixes. Filenames are sanitized, downloads must return HTTP 200, and each file is capped by `ATTACHMENT_MAX_MB`. Auto-cleaned after `ATTACHMENT_RETAIN_DAYS`.
- **Tool permissions:** Server-initiated ACP permission requests are approved only when `TRUST_ALL_TOOLS=true` or `TRUST_TOOLS` is set; otherwise they are denied by local policy.
- **Preflight:** `PREFLIGHT_MODE=warn` keeps the bot online when `kiro-cli` is temporarily unavailable. Use `strict` for fail-fast production startup or `skip` for development.
- **Thread agents:** Idle timeout respects active work — cleanup skips workers with an active job, and `lastActivity` is updated during tool execution. Set `THREAD_AGENT_IDLE_SEC=0` to disable thread idle cleanup. `THREAD_AGENT_MAX` must be greater than zero and is a hard safety limit; capacity overflow asks the user to close an inactive thread with `/close-thread thread_id:<id>` instead of auto-evicting one.
- **Channel agent idle:** Set `CHANNEL_AGENT_IDLE_SEC` (default `0` = disabled) to auto-close idle channel agents and free resources. Agents restart automatically on next message.
- **Cron jobs:** Definitions in `DATA_DIR/cron/cron.json`, execution history in `DATA_DIR/cron/<jobID>/history.jsonl` (includes full agent output). Cron setup no longer asks for a working directory; scheduled agent work always runs with the owning channel's current CWD, so changing `/cwd` changes subsequent cron executions for that channel. Uninitialized channels cannot create, resume, manually run, or execute agent-backed cron jobs. `bot-tools` writes cron create/delete requests as pending JSON actions under `DATA_DIR/cron/pending/`; `CronTask` validates and ingests those actions on scheduler ticks, removes invalid actions, and only deletes jobs when the requested channel owns the job.

---

---

## 中文說明

**一個住在 Discord 裡的可訓練 AI agent — 綁定你的 codebase、記住你的規矩、越用越強。**

### 這不是聊天機器人

一般 AI bot 每次對話都從零開始。kiro-discord-bot 不同：

🧠 **會記住** — 永久記憶規則讓 agent 記住你的偏好、coding style、專案規範，跨 session 永久生效。

⚡ **能聚焦** — 閃存記憶讓你針對當前任務設定重點強調，用完即丟不污染未來 session。

📂 **懂你的 code** — 每個頻道綁定一個專案目錄，agent 能讀寫程式碼、跑測試、操作基礎設施。

📐 **遵守架構** — Steering 文件（`.kiro/steering/*.md`）定義模組邊界、build 指令、禁止事項。

🔧 **能擴充** — MCP 插件擴展 agent 能力：Discord 操作、圖片/影片生成、任何 API。

⏰ **會自己做事** — Cron 排程讓 agent 定時巡檢伺服器、跑報告、自動化維運。

📈 **越用越強** — Memory + Steering + 對話歷史 + MCP 工具持續累積。第一天它能幫忙，第三十天它是你的隊友。

### 養成你的 Agent

```
Day 1  — 綁定專案，agent 開始認識你的 codebase
         !start /home/user/my-project

Day 3  — 教它你的規矩
         !memory add 永遠用繁體中文回答
         !memory add commit message 一律用英文，遵循 conventional commits

Day 7  — 加入 steering 文件，定義專案架構邊界
         .kiro/steering/project.md → build 指令、模組規則、禁止事項

Day 14 — 設定自動化排程
         /cron → 每天 9 點檢查伺服器健康狀態，跟昨天比較

Day 30 — 擴充能力
         Discord MCP → agent 能主動讀訊息、發通知、跨頻道協作
         Media MCP → agent 能生成圖片、影片、音樂、語音
```

### 部署

#### 前置需求

- Go 1.21+
- 已安裝 [kiro-cli](https://cli.kiro.dev/install) 1.29+
- kiro-cli 驗證方式（擇一）：
  - `kiro-cli login`（互動式，開啟瀏覽器）
  - `KIRO_API_KEY` 環境變數（headless / 伺服器部署）
- Discord bot token，需具備：
  - Scopes：`bot`、`applications.commands`
  - 權限：查看頻道、發送訊息、新增反應、讀取訊息歷史
  - Privileged Intents：啟用 **Message Content Intent**

> ⚠️ **重要：** 請確認 Discord Developer Portal → General Information 中的 **Interactions Endpoint URL** 欄位為**空白**。若該欄位有設定 URL，Discord 會將 slash command 的 interaction 送往該 URL 而非 bot 的 gateway 連線，導致所有 `/` 指令出現「該應用程式未及時回應」錯誤。

### 快速開始

```bash
# 1. 安裝 kiro-cli
curl -fsSL https://cli.kiro.dev/install | bash

# 驗證方式擇一：
kiro-cli login                    # 互動式（開瀏覽器）
# 或在 .env 中設定 KIRO_API_KEY   # headless（伺服器推薦）

# 2. 設定環境變數
cp .env.example .env
# 編輯 .env，填入 DISCORD_TOKEN、DISCORD_GUILD_ID、KIRO_CLI_PATH 等

# 3. 編譯
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
go build -ldflags "-X main.Version=$VERSION" -o kiro-discord-bot .

# 4. 啟動（擇一）
# systemd（推薦）：
sudo cp kiro-discord-bot.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now kiro-discord-bot

# 手動：
export $(grep -v '^#' .env | xargs)
./kiro-discord-bot
```

既有服務重啟前，先跑 release preflight：

```bash
scripts/release-preflight.sh
```

若要包含本機已登入的 ACP smoke test：

```bash
RUN_ACP_SMOKE=1 KIRO_CLI=/Users/chun/.local/bin/kiro-cli scripts/release-preflight.sh
```

完整升版與部署檢查表見 `docs/release.md`。

### 指令說明

| 指令 | 說明 |
|------|------|
| `/start <目錄>` | 進階：綁定專案目錄並啟動 agent |
| `/reset` | 重置此 channel 目前的 agent session |
| `/status` | 查詢 agent 狀態、queue 長度、context 使用率 |
| `/usage [user]` | 查詢今天、本周、本月至今 credits 用量 |
| `/doctor` | 執行部署診斷與 ACP preflight |
| `/audit [limit]` | 查看目前頻道或討論串最近的 raw/semantic 稽核事件 |
| `/mcp manage` | 開啟互動式 MCP 權限面板，包含工具掃描與工具層級允許/移除控制 |
| `/mcp <action> [value]` | 查詢或更新此頻道的 MCP policy。Action：`status`、`enable`、`disable` |
| `/steering <status|create|edit>` | 管理目前頻道專案的 `.kiro/steering/<project>.md` 規範檔 |
| `/cancel` | 取消目前執行中的任務 |
| `/interrupt` | 中斷卡住的目前任務；先執行取消，仍未結束才嘗試進程層中斷 |
| `/cwd` | 開啟 private 專案/CWD 面板；不用輸入完整路徑即可選擇或建立專案 |
| `/pause` | 切換頻道為 @mention 原頻道回覆模式 |
| `/back` | 恢復完整監聽並啟用新任務討論串 |
| `/thread [on|off]` | 查詢或設定新的頻道任務是否開啟 Discord 討論串 |
| `/silent` | 查詢安靜模式狀態（精簡工具輸出，預設：開啟） |
| `/silent on` | 開啟精簡工具輸出 |
| `/silent off` | 顯示完整工具細節 |
| `/model` | 查詢目前使用的 model |
| `/model <model-id>` | 切換 model 並重啟 agent |
| `/models` | 列出所有可用的 model |
| `/cron` | 新增排程任務（開啟表單） |
| `/cron-list` | 列出排程任務（含操作按鈕） |
| `/cron-run <name>` | 手動執行排程任務 |
| `/cron-prompt <description>` | 用自然語言建立排程任務 |
| `/remind <時間> <內容>` | 預約單次提醒（到期時 tag 你） |
| `/compact` | 壓縮對話歷史以釋放 context |
| `/clear` | 清除對話歷史 |
| `/close-thread <thread_id>` | 關閉目前頻道範圍內的 inactive 討論串 agent |
| `/memory` | 管理永久記憶規則（add/list/remove/clear） |
| `/flashmemory` | 管理 session 閃存記憶（add/list/remove/clear） |

所有指令也支援 `!` 前綴（如 `!status`、`!reset`）。

在 Discord 討論串中使用指令時，會依最符合直覺的作用範圍執行：`/status`、`/reset`、`/cancel`、`/interrupt`、`/compact`、`/clear`、`/model` 會操作目前的討論串 agent。`/pause`、`/back`、`/silent` 會套用在目前目標，因此討論串可以覆蓋建立當下保存的監聽行為。`/thread` 永遠套用在父頻道未來新任務是否開討論串。`/memory` 與 `/flashmemory` 仍套用在父層頻道，因為討論串 agent 會繼承父層記憶。

頻道設定與排程指令必須在父層頻道使用：`/start`、`/cwd`、`/steering`、`/agent`、`/resume`、`/cron`、`/cron-list`、`/cron-run`、`/cron-prompt`、`/remind`。

新的父層頻道必須先完成初始化才會啟動 agent。未初始化頻道中的第一則一般訊息會被暫停，並提示頻道管理員開啟 private `/cwd` 初始化面板。初次設定只能選擇或建立 `DEFAULT_CWD` 底下的專案；初始化面板會列出 `DEFAULT_CWD` 第一層目錄，並在碰到 Discord select-menu 上限時自動分頁。選擇專案後會先進入確認步驟，按下確認後才會變更頻道 CWD。建立新專案時也會自動建立 `.kiro/steering/`。初始化完成後，成功訊息會收斂 CWD 設定流程，只保留 private shortcuts 讓管理員檢視此頻道的 MCP 工具開放設定與建立 agent context 檔。會啟動 agent 或改變 agent 執行上下文的指令會在初始化前被拒絕，例如 `/start`、`/reset`、`/compact`、`/clear`、model/agent 切換、MCP policy 變更、專案規範變更、agent memory 變更、`/cron`、`/cron-run`、`/cron-prompt` 與 agent-backed reminder。完成初始化後，管理員仍可用 `/cwd` 作為進階操作，依一般 cwd allowlist policy 切換到其他允許路徑。

**討論串專用指令**（在 thread 中使用）：

| 指令 | 說明 |
|------|------|
| `!close` | 關閉討論串 agent |
| `!cancel` | 取消討論串 agent 目前的任務 |
| `!interrupt` | 中斷討論串 agent 卡住的目前任務 |
| `!reset` | 重啟討論串 agent |
| `!pause` | 切換討論串為 @mention 模式 |
| `!back` | 恢復討論串完整監聽模式 |
| `!thread [on\|off]` | 查詢或設定父頻道未來新任務是否開討論串 |
| `!silent` | 查詢討論串安靜模式狀態 |
| `!silent on` | 開啟此討論串的精簡工具輸出 |
| `!silent off` | 顯示此討論串的完整工具細節 |
| `!compact` | 壓縮討論串 agent 的對話歷史 |
| `!clear` | 清除討論串 agent 的對話歷史 |
| `!close-thread <thread_id>` | 關閉父頻道範圍內的 inactive 討論串 agent |
| `!model` | 查詢討論串 agent 目前的 model |
| `!model <model-id>` | 切換討論串 agent 的 model 並重啟 |
| `!models` | 列出所有可用的 model |
| `!audit [limit]` | 查看此討論串最近的稽核事件 |

所有討論串指令也支援 `/` slash command 形式。

### 注意事項

- Bot 需要在各 channel 的權限設定中明確授予讀寫權限
- Session ID 會存到 `DATA_DIR/sessions.json`；當 kiro-cli 宣告支援 `loadSession` 時，頻道與討論串 agent 重啟會優先用 `session/load` 接回既有 ACP session。Session key 會依 guild、bot 身分、目標類型與 channel/thread ID 分開；舊版 channel-only key 仍會作為遷移 fallback 讀取
- **頻道初始化**：新的父層頻道必須由具備 Discord 頻道管理權限的人透過 `/cwd` 初始化。初次選擇/建立專案限定在 `DEFAULT_CWD` 底下；建立新專案會自動建立 `.kiro/steering/`。初始化完成後可用 `/steering create` 或成功訊息 shortcut 先填寫可重複注入的背景、工作方式、常用資訊、限制與補充 context，再建立 `.kiro/steering/<project>.md`，預設使用 `inclusion: always`；空白選填欄位不會輸出成區塊。`/steering edit` 會用 private modal 做全文 Markdown 編輯，超過 Discord modal 限制時需直接在專案中修改。
- **MCP servers**：bot 只把 Kiro MCP 設定當作 catalog 讀取，並額外加入 `bot-tools` 這類內建 catalog entry。實際 agent runtime 使用隔離的 `KIRO_HOME=DATA_DIR/kiro-runtime`，並依目前頻道 policy 在 ACP session 注入允許的 `mcpServers`。預設不開放任何 MCP server。啟用的 server 一律透過 bot MCP policy proxy 啟動，proxy 會過濾 `tools/list` 並阻擋未授權的 `tools/call`，不把 Kiro `disabledTools` 當作安全邊界。
- **內建 bot-tools MCP**：`bot-tools` 由同一支 bot binary 的 `mcp-bot` 子命令提供。它包含中繼資訊工具、安全 Discord egress 工具與 cron 管理工具；中繼資訊與 list 工具是唯讀。`bot_send_message` 與 `bot_send_file` 會寫入 pending egress action，由主 bot 做 secret redaction 後送出；檔案只會上傳 sanitized text copy，二進位或超大檔不會原樣傳回 Discord。`bot_create_cron` 會寫入非破壞性 pending create action，`bot_delete_cron` 會寫入破壞性 pending delete action。若只需要中繼資訊或 safe egress，請用 `/mcp manage` 做工具層級授權，不要直接開放整個 server。
- **Secret redaction**：bot 傳到 Discord 的文字 egress 會在送出前替換已知敏感環境變數值，以及常見 API key、token、bearer、password、credential assignment。這是輸出端最後防線；它不代表 agent process 本身不能讀取原本就可存取的檔案。
- **Discord MCP 範圍**：`mcp-discord` 只是 catalog 中的一個 server；`/mcp` 與 `!mcp` 對它的管理方式與其他 MCP server 一致，可開關整個 server，或在 `/mcp manage` 精準管理 tool allowlist。env 層級 guild/channel/read-only/write/destructive guard 只保留作為直接或手動啟動 MCP server 時的底層防護。
- **Agent metrics**：當 ACP 回傳 turn metrics 時，agent 執行完成的可見回覆會帶 `⚡` metrics footer。usage ledger 會分開保存 Discord 訊息 ID 與 slash command interaction ID（`message_id`、`interaction_id`），並額外保存通用的 `invocation_id` 方便和 audit 交叉關聯。
- **Raw Discord 稽核資料庫**：bot 可見的 Discord events 會獨立寫入 SQLite（預設 `DATA_DIR/audit/discord.sqlite`），包含 append-only `discord_events` 與 messages、attachments、reactions、threads 查詢投影；也會在 `bot_audit_events` 紀錄 command 呼叫、command 回覆送出成功/失敗、agent job lifecycle、agent final response 等語意事件。Slash command initial response、deferred followup、cron/reminder command response 都會走同一套 delivery success/failure audit。高頻 typing-start event 預設不紀錄。這不會觸發 agent，也不會自動注入 agent 對話 context；現有 `chat.jsonl` 仍只紀錄實際 user/agent 互動。
- **Audit 關聯 ID**：`bot_audit_events` 的 `message_id` 與 `interaction_id` 代表觸發 bot command 的使用者呼叫來源；Discord 回傳 bot response message object 時，實際 bot 回覆訊息 ID 會存到 metadata 的 `response_message_id`；initial interaction response 與 modal 不會暴露 Discord message ID，所以改存 `interaction_response_type`。Cron agent 的 `response_sent` 代表 final response message 實際送達，不只是 thread 是否存在。
- **Slash command 可視性**：管理型 slash command 會設定 Discord default member permissions，讓一般使用者預設不會在 command picker 看到。`/mcp manage`、`/steering`、`/cron-list`、`/cwd`、`/status`、`/usage`、`/doctor`、`/audit`、`/models`、`/memory`、`/flashmemory` 等操作或查詢回應會優先使用 ephemeral private response，減少設定面板與敏感路徑留在頻道中。Agent 任務成果與明確的頻道行為變更仍會送到目標 channel/thread。
- **稽核權限**：`/audit` 與 `!audit` 直接使用 Discord effective channel permissions，不另建 ACL。呼叫者必須能管理目前目標頻道或討論串；討論串會接受父層頻道管理權限。Discord 權限異動會在下一次查詢即時生效。
- 回應被截斷時可用 `!resume` 補完
- **討論串模式**：預設新的父頻道任務會由 bot 主動開 Discord thread，過程與最終回覆都在 thread 中。`/thread off` 或父頻道 `/pause` 會停止新任務開 thread；新任務必須 @mention bot，使用頻道主 agent，在原頻道以 `🔄`、`💭`、`✨`、`🛠️`、`⚙️` 等 reaction heartbeat 顯示仍在運作，最後才送出實際回覆。`/thread on` 或父頻道 `/back` 會恢復新任務開 thread。
- **討論串互動**：在 bot 建立的 thread 中發訊息，會自動啟動獨立的 thread agent 接續討論。thread 會保存建立當下的監聽模式；父頻道後續切換 `/thread off` 不會讓舊 thread 被動改成 mention-only。若父頻道已是 `/thread off`，手動建立或未知來源的 thread 預設 mention-only，直到在該 thread 內 `/back`。非 active agent 閒置超過 `THREAD_AGENT_IDLE_SEC` 或非 active thread 歸檔時自動關閉，再次發訊息可重新啟動。容量上限不會自動關閉任何 thread agent；如果名額已滿，bot 會列出 active/inactive 狀態與 inactive 候選，讓使用者執行 `/close-thread thread_id:<id>` 關閉指定 inactive agent。active work 不會因 idle cleanup、歸檔事件或 thread agent 容量上限被強制終止；active thread 若被歸檔，會在目前 job 回到 idle 後關閉；`THREAD_AGENT_IDLE_SEC=0` 可停用討論串閒置清理。
- **取消與中斷**：`/cancel` 只送出 ACP `session/cancel` 取消目前任務；`/interrupt` 會先做同樣的 soft cancel，短暫等待後若同一任務仍在執行，才嘗試對 agent process group 送 `SIGINT`，用來中斷卡住的工具子程序。若同一任務仍卡住，重複 `/interrupt` 可再嘗試一次 `SIGINT`。它不會清除已保存的 session metadata，也不會關閉 Discord thread；若 agent 因中斷退出，下一則訊息會走既有的重啟與 `session/load` 流程
- **長回覆格式**：bot 會依 Discord 訊息限制自動分段，並先轉成 Discord-safe Markdown；標題會降級為粗體文字，code block 跨段時會自動補上關閉與重新開啟 fence，分段前綴會放在 code block 外。
- **Cron jobs**：排程定義存於 `DATA_DIR/cron/cron.json`，執行歷史存於 `DATA_DIR/cron/<jobID>/history.jsonl`。Cron 設定不再要求輸入工作目錄；排程 agent 一律使用所屬頻道當下的 CWD，因此管理員變更 `/cwd` 後，該頻道後續 cron 執行也會跟著切換。未初始化頻道不能建立、恢復、手動執行或到點執行 agent-backed cron job。`bot-tools` 會把 cron 建立/刪除請求先寫成 `DATA_DIR/cron/pending/` 內的 JSON action，由 `CronTask` 在 scheduler tick 驗證與 ingest；無效 action 會被移除，刪除 action 只會刪除同一頻道擁有的 job。
- **多 bot 模式**：bot 啟動時會用完整 Discord guild member list 自動偵測同 server 內其他 bot，並盡量補上 bot role。`BOT_PEERS` 只需要用來覆蓋偵測結果、補上偵測不到的 bot、手動加入 role-only peer，或用 `!userID` 排除無關 bot；格式為 `Name:userID`、`Name:userID:roleID`、`Name::roleID` 或 `!userID`。自動 multi-bot mention-only 會在另一個 peer bot 對目前頻道或討論串具有實際可回應權限時啟用，包含直接在頻道發訊息，或建立公開討論串並在討論串內回覆；權限來源可以是明確 channel overwrite、繼承 role 權限或 `@everyone` 權限。自動偵測到的 role-only peer 仍不會單獨觸發 mention-only，除非用 `BOT_PEERS=Name::roleID` 手動指定。請用真正的 Discord mention（例如 `<@111111111111111111>` 或 Discord 介面的提及選單），若偵測或設定了 role ID，role mention（例如 `<@&222222222222222222>`）也會路由到目標 bot；純文字 `@BuildBot` 不一定會觸發。若要讓其中一個 bot 暫時恢復完整監聽，對該 bot 在主頻道執行 `/back` 或 `!back`，該主頻道底下的討論串也會繼承；若只想讓某條討論串回到 mention-only，可在該討論串執行 `/pause` 或 `!pause`
- **Bot 交接限制**：bot 產生的訊息預設不會觸發另一個 bot。只有在討論串內、明確 tag 目標 bot、原始任務訊息已有完成反應（`✅`），且內容不是進度、錯誤、逾時或空輸出時，才會被視為有效交接。一般討論串任務會帶入近期 Discord 討論串訊息作為 bounded context；通過 gate 的跨 bot 交接會帶入較長的 thread transcript 作為 handoff context，讓被交辦 bot 先掌握任務、先前決策、相關檔案、結果與剩餘工作
- **Slash command 範圍**：指令以 guild scope 註冊，但 bot 會拒絕在自己沒有讀寫權限的頻道或討論串中執行。管理型 slash command 會設定 Discord default member permissions，讓一般使用者預設不會在 command picker 看到；若還要做到 channel-specific 的指令選單隱藏，需要在 Discord app command permissions 設定，或用具備 `applications.commands.permissions.update` scope 的 OAuth2 token 同步權限
- **部署診斷**：在目標頻道或討論串執行 `/doctor`，可確認 Discord 權限、`BOT_PEERS` 設定，以及目前是開放模式、`/back` override 開放模式，或自動多 bot mention-only 模式
- **頻道 agent 閒置回收**：設定 `CHANNEL_AGENT_IDLE_SEC`（預設 `0` = 停用）可讓閒置的頻道 agent 自動關閉以釋放資源，下次發訊息時自動重啟

---

### 選配：Discord MCP Server

本專案內建 Discord MCP Server（`cmd/mcp-discord/`），啟用後 kiro agent 可直接操作 Discord——讀訊息、發訊息、列頻道、搜尋、加反應等。

#### MCP 額外權限需求

MCP server 使用的 Discord REST API 超出 bot 本體所需，啟用前請先補上以下權限：

**額外 Bot 權限：**
- Attach Files — `discord_send_file`
- Embed Links — `discord_send_embed`
- Manage Messages — `discord_delete_message`、`discord_pin_message`、`discord_remove_reaction`
- Create Public Threads — `discord_create_thread`
- Manage Channels — `discord_edit_channel_topic`

**額外 Privileged Intent：**
- **Server Members Intent** — `discord_list_members` 需要

> 前往 [Discord Developer Portal](https://discord.com/developers/applications) → 你的應用 → **Bot** 頁籤啟用 intent，並重新產生含額外權限的 OAuth2 邀請連結。

#### 安全範圍

bot 不會預設把 catalog 中的 MCP server 暴露給 agent。它會從 `KIRO_MCP_CONFIG`、`KIRO_HOME/settings/mcp.json` 或 `~/.kiro/settings/mcp.json` 動態讀取 catalog，實際 runtime 使用隔離的 `KIRO_HOME=DATA_DIR/kiro-runtime`，並只在目前 Discord 頻道 policy 允許時，才把對應 MCP server 注入 ACP session。已啟用的 server 會透過 bot MCP policy proxy 啟動，由 proxy 過濾 `tools/list` 並阻擋未授權 `tools/call`。

bot 也會註冊內建的 `bot-tools` MCP catalog entry，由同一支 bot binary 的 `mcp-bot` 子命令提供。它提供 bot data directory 中繼資訊工具、安全 Discord egress 工具（`bot_send_message`、`bot_send_file`），以及 cron 管理工具（`bot_create_cron`、`bot_list_cron`、`bot_delete_cron`）。和外部 catalog server 一樣，必須由頻道管理員透過 `/mcp manage` 或 `/mcp enable server:bot-tools` 明確啟用後才會注入 agent；若只想開放中繼資訊、safe egress 或非破壞性 cron 權限，請用 `/mcp manage` 做工具層級控管。

內建 `bot-tools` 工具：

| Tool | 權限提示 | 說明 |
|------|----------|------|
| `bot_data_summary` | 唯讀 | 摘要 data directory 中繼資訊，不回傳訊息內容 |
| `bot_list_channel_data` | 唯讀 | 列出 channel data directory 與中繼檔案是否存在 |
| `bot_list_cron` | 唯讀 | 列出指定頻道的排程任務 |
| `bot_send_message` | 寫入、非破壞性 | 佇列化 Discord 訊息，由 bot 端做 secret redaction 後送到綁定頻道 |
| `bot_send_file` | 寫入、非破壞性 | 佇列化本機文字檔，由 bot 端產生 sanitized copy 後上傳 |
| `bot_create_cron` | 寫入、非破壞性 | 佇列化建立 recurring cron job，等待 scheduler ingest |
| `bot_delete_cron` | 寫入、破壞性 | 佇列化刪除 cron job；只有 job 所屬頻道相符才會刪除 |

頻道管理員可在 Discord 中管理目前頻道 policy：

```text
/mcp status
/mcp status server:<server>
/mcp enable server:<server>
/mcp disable server:<server>
```

請用 `/mcp status` 查看合併後的 catalog 與目前頻道 policy 檢查清單。`/mcp enable` 會開放整個 server。工具層級控制與 MCP 重新載入請使用 `/mcp manage`：面板會以 private interaction response 顯示，可以掃描 server 目前的 `tools/list`、把工具快取到 SQLite、用 Discord select menu 執行允許/移除，並停止活躍 agent，讓下一次執行載入目前 MCP policy。原本手打 tool name 的 `allow-tool`、`deny-tool` 指令不再公開，避免使用者需要猜精確工具名稱。

升級相容會依 data directory 執行一次。全新安裝時，catalog 內的 MCP server 都維持停用，直到頻道管理員手動啟用。若是從舊版升級，且舊版會全域繼承 Kiro MCP config，bot 只會對 `sessions.json` 中已存在的 channel 保留舊行為：第一次升級啟動當下 catalog 內的 server 會對這些既有 channel 以完整 server access 啟用。這個 migration 可重複執行但只會生效一次，不會覆蓋既有明確 policy，也不會自動啟用 migration 之後才新增的 MCP server。

`mcp-discord` 只是 catalog 中的一個可選 server；管理方式與其他 MCP server 一致：開關整個 server，或在 `/mcp manage` 內精準管理工具 allowlist。

在有較大 Discord 存取權的 workspace 啟用 MCP 前，建議先設定 allowlist：

```env
MCP_DISCORD_ALLOWED_GUILDS=123456789012345678
MCP_DISCORD_ALLOWED_CHANNELS=234567890123456789,345678901234567890
MCP_DISCORD_DOWNLOAD_DIR=/tmp/kiro-discord-mcp
MCP_DISCORD_ALLOW_DESTRUCTIVE=false
```

bot 的頻道 policy 不綁定特定 MCP server：它只負責啟用 catalog server，並透過 MCP policy proxy 過濾 agent 可見與可呼叫的工具，不會注入特定 server 專用的環境變數覆寫。若使用本專案內建 Discord MCP server，請把這些 env guard 設在 catalog command environment 或被載入的 `.env`。設定 guild allowlist 後，所有 channel 類工具都會先解析頻道並拒絕非授權 guild。設定 channel allowlist 後，頻道與 thread 類工具只允許指定 ID。`discord_download_attachment` 只接受 Discord attachment/CDN host；`MCP_DISCORD_DOWNLOAD_DIR` 會限制下載檔案可寫入的目錄。

更嚴格的部署可設定 `MCP_DISCORD_READ_ONLY=true` 封鎖所有寫入工具，或用 `MCP_DISCORD_ALLOWED_WRITE_TOOLS` 指定允許的寫入工具，例如 `discord_send_message,discord_reply_message`。設定 `MCP_DISCORD_ALLOW_DESTRUCTIVE=false` 可阻擋刪除、編輯、釘選、改 topic、移除 reaction 等管理操作，同時保留非破壞性發訊息能力。

#### 手動安裝

```bash
# 1. 編譯 MCP server
go build -o mcp-discord ./cmd/mcp-discord/

# 2. 安裝 steering 文件（全域，讓任何專案目錄都能使用）
mkdir -p ~/.kiro/steering
cp .kiro/steering/discord-mcp.md ~/.kiro/steering/discord-mcp.md

# 3. 註冊到 kiro MCP 設定
# 在 ~/.kiro/settings/mcp.json 的 "mcpServers" 中加入：
```

```json
"mcp-discord": {
  "command": "sh",
  "args": [
    "-c",
    "set -a && . /你的專案絕對路徑/.env && exec /你的專案絕對路徑/mcp-discord"
  ]
}
```

將 `/你的專案絕對路徑` 替換為實際路徑。

```bash
# 4. 依頻道啟用
# 在 Discord 中使用 /mcp status 確認清單中有此 server，再用 /mcp enable server:mcp-discord
```

#### 透過 Agent 自動安裝

也可以直接在 Discord 中對 bot 說：

> 讀取 INSTALL_MCP.md 並照步驟安裝 Discord MCP server。

Agent 會自動讀取說明、編譯、更新 mcp.json，並提示你重啟。

#### 啟用後可用的 Tools

| Tool | 說明 |
|------|------|
| `discord_list_channels` | 列出伺服器的文字頻道 |
| `discord_read_messages` | 讀取頻道最近的訊息 |
| `discord_send_message` | 發送訊息到指定頻道 |
| `discord_reply_message` | 回覆特定訊息 |
| `discord_add_reaction` | 對訊息加 emoji 反應 |
| `discord_list_members` | 列出伺服器成員 |
| `discord_search_messages` | 在頻道中搜尋關鍵字 |
| `discord_channel_info` | 取得頻道詳細資訊 |
| `discord_send_file` | 上傳本地檔案到頻道作為附件 |
| `discord_list_attachments` | 列出頻道近期訊息中的附件 |
| `discord_download_attachment` | 下載 Discord 附件到本地 |
| `discord_edit_message` | 編輯訊息 |
| `discord_delete_message` | 刪除訊息 |
| `discord_get_message` | 以 ID 取得單則訊息 |
| `discord_send_embed` | 發送 embed 富文本訊息 |
| `discord_pin_message` | 釘選或取消釘選訊息 |
| `discord_create_thread` | 從訊息建立 thread |
| `discord_list_threads` | 列出伺服器中的活躍 threads |
| `discord_remove_reaction` | 移除訊息上的 reaction |
| `discord_get_reactions` | 取得對特定 emoji 反應的使用者 |
| `discord_edit_channel_topic` | 編輯頻道主題 |
| `discord_list_roles` | 列出伺服器角色 |
| `discord_get_user` | 查詢特定使用者資訊 |
