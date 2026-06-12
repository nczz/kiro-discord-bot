# kiro-discord-bot

[繁體中文文件](README.zh-TW.md)

**A trainable AI agent that lives in Discord — binds to your codebase, remembers your rules, and gets smarter the more you use it.**

This bot connects Discord to [kiro-cli](https://kiro.dev) AI agents via the Agent Client Protocol (ACP) over stdio. It's not a chatbot — it's a full development agent workspace that grows with you.

### This is not a chatbot

Most AI bots start from zero every conversation. kiro-discord-bot is different:

🧠 **Remembers** — Persistent memory rules teach the agent your preferences, coding style, and project conventions. They stick across sessions forever.

⚡ **Adapts** — Flash memory lets you set session-scoped emphasis for the current task, then discard it cleanly.

📂 **Knows your code** — Each channel binds to a project directory. The agent reads/writes code, runs tests, manages infrastructure — in your actual repo.

📐 **Carries reusable context** — Steering files (`.kiro/steering/*.md`) can inject project background, collaboration preferences, repeated workflows, safety limits, build commands, and architecture rules into the agent.

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

Day 7  — Add agent context for repeated work
         .kiro/steering/project.md → workflow, references, safety notes, build commands

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
| `CRON_TIMEZONE` | IANA timezone for cron schedules. Set explicitly in production, for example `Asia/Taipei`; if empty, Go uses the service process local timezone, which may be UTC under launchd/systemd even when the host clock displays local time. | `` |
| `USAGE_TIMEZONE` | Timezone for `/usage` day/week/month boundaries (empty = `CRON_TIMEZONE`, then service process local timezone) | `` |
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
| `KIRO_MCP_CONFIG` | Optional MCP catalog source for the bot policy catalog. Runtime agent sessions use bot-managed `DATA_DIR/kiro-agent-runtime/settings/mcp.json`, so only per-channel injected MCP servers are visible to agents | `` |
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

`docker-compose.yml` uses `network_mode: host`, mounts `~/.kiro` for Kiro authentication and MCP catalog discovery, and mounts `${PROJECTS_DIR:-/projects}` as the writable project root. Agent sessions still use the bot-managed isolated runtime (`DATA_DIR/kiro-agent-runtime`) and do not directly inherit Kiro MCP settings; catalog MCP servers must still be enabled per channel through `/mcp`.

Compose intentionally pins some runtime defaults for container use: `DEFAULT_CWD=/projects`, `DATA_DIR=/data`, `ALLOWED_CWD_ROOTS=/projects`, and `ASK_TIMEOUT_SEC=300` unless overridden. It exposes the core bot, Discord MCP, and STT variables, but not every optional retention/audit/catalog variable. Add extra environment entries to `docker-compose.yml` when your deployment needs them, for example `KIRO_MCP_CONFIG`, `USAGE_TIMEZONE`, `USAGE_RETENTION_MONTHS`, or `AUDIT_LOG_*`.

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
| `/help` | Show the command summary |
| `/start <cwd>` | Advanced: bind channel to a project directory and start agent |
| `/reset` | Reset the current agent session for this channel |
| `/status` | Show agent state, queue length, context usage, session ID, and bot/agent uptime |
| `/usage [user]` | Show credit usage for today, this week, and month-to-date |
| `/doctor` | Run deployment diagnostics and ACP preflight |
| `/audit [limit]` | Show recent raw/semantic audit events for the current channel or thread |
| `/mcp manage` | Open the interactive MCP policy panel, including tool scan and tool-level allow/remove controls |
| `/mcp <action> [value]` | Show or update channel MCP policy. Actions: `status`, `enable`, `disable` |
| `/steering <status|create|edit>` | Manage the current channel project's agent context file at `.kiro/steering/<project>.md` |
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
| `/close` | Thread only: close the current thread agent |
| `/close-thread <thread_id>` | Close an inactive thread agent in this channel scope |
| `/memory` | Manage persistent memory rules (add/list/remove/clear) |
| `/flashmemory` | Manage session-scoped flash memory (add/list/remove/clear) |

All commands also work with `!` prefix (e.g. `!status`, `!reset`).

When a command is used inside a Discord thread, it targets the thread agent when that is the least surprising behavior: `/status`, `/reset`, `/cancel`, `/interrupt`, `/compact`, `/clear`, and `/model` operate on the current thread agent. `/pause`, `/back`, and `/silent` apply to the current target, so a thread can override the listen behavior captured when it was created. `/thread` always applies to the parent channel's future new-task behavior. `/memory` and `/flashmemory` remain scoped to the parent channel because thread agents inherit that memory block.

Channel setup and scheduling commands must be run in the parent channel: `/start`, `/cwd`, `/steering`, `/agent`, `/cron`, `/cron-list`, `/cron-run`, `/cron-prompt`, and `/remind`.

New parent channels must be initialized before agent work starts. The first normal message in an uninitialized channel is held back and prompts a channel manager to open the private `/cwd` setup panel. Initial setup can only select or create a project under `DEFAULT_CWD`; the setup panel lists first-level directories under `DEFAULT_CWD` and paginates them when Discord's select-menu limit is reached. Selecting a project opens a confirmation step before the channel CWD is changed. Creating a project also creates `.kiro/steering/`. After setup completes, the channel automatically enables the built-in `bot-tools` MCP with the safe default tool allowlist, keeps the CWD setup closed, and offers private shortcuts to review MCP tool access and create the agent context file. Agent-starting or agent-context-changing commands such as `/start`, `/reset`, `/compact`, `/clear`, model/agent switches, MCP policy changes, agent context changes, agent memory changes, `/cron`, `/cron-run`, `/cron-prompt`, and agent-backed reminders are rejected until initialization is complete. After a channel is initialized, managers can still use `/cwd` as an advanced control to change to another allowed path through the regular cwd allowlist policy.

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

Each task runs in a Discord thread. With `/silent off`, the bot posts the full work process in real-time:

| Event | Display |
|-------|---------|
| Tool start | Kind icon (📖 read, ✏️ edit, ▶️ execute, 🔍 search, 🌐 fetch) + title + affected files |
| Tool result | Full output in code block (up to 1900 chars per message) |
| Tool failure | ❌ title + error output |
| Agent thinking | 💭 thought content |
| Final response | Complete text, auto-split if > 2000 chars |

**Silent mode** (default: on) shows compact output. Tool start messages show only an icon plus a short title, execute commands show a short escaped prefix such as `Running: ssh n200 ...`, tool results and thoughts are hidden, and failures show a one-line summary. Use `/silent off` for full detail. `/silent` without an argument only shows the current status. Silent mode is stored in memory only, so it resets to on after the bot restarts. Threads have their own silent setting and do not inherit a parent channel's `/silent off`.

### Recovery

Long responses are split automatically at Discord message limits with code blocks reopened across message parts. The legacy `/resume` and `!resume` commands are reserved but do not currently replay the last response.

---

## Architecture

```
Discord User
    │ message / slash command
    ▼
Discord Bot (Go)
    ├── command/message router      permissions, ephemeral admin panels, audit events
    ├── Channel Manager             scoped sessions, cwd, queue, worker, thread agents
    ├── Channel Setup               DEFAULT_CWD project selection/creation + steering seed
    ├── MCP Policy Catalog          Kiro MCP config as catalog only + built-in bot-tools
    ├── Safe Egress                 secret redaction + sanitized message/file delivery
    ├── Heartbeat                   health, cleanup, cron, reminders, idle agent eviction
    ├── Agent runtime prep
    │     ├── KIRO_HOME=DATA_DIR/kiro-agent-runtime
    │     ├── KIRO_MCP_CONFIG=runtime empty settings/mcp.json
    │     ├── allowlisted non-MCP CLI settings sync
    │     └── MCP-sanitized agent configs
    ├── MCP policy proxy
    │     ├── filters tools/list
    │     └── blocks unauthorized tools/call
    └── bot-tools MCP
          ├── read-only bot metadata
          ├── redacted Discord message/file egress
          └── pending cron create/delete actions
    ▼
kiro-cli acp                  one process per channel/thread/temp job
          │
          ▼
Kiro model provider
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

The bot does not expose catalog MCP servers to agents by default. It reads MCP server definitions from `KIRO_MCP_CONFIG`, `KIRO_HOME/settings/mcp.json`, or `~/.kiro/settings/mcp.json` as a bot policy catalog only. Runtime agents start with isolated `KIRO_HOME=DATA_DIR/kiro-agent-runtime` plus `KIRO_MCP_CONFIG=DATA_DIR/kiro-agent-runtime/settings/mcp.json`, where the bot writes an empty MCP config without touching user-managed Kiro MCP settings. The runtime syncs allowlisted non-MCP CLI feature settings (`app.*`, `chat.*`, `inline.*`) into `settings/cli.json` so Kiro built-ins such as todo lists and knowledge keep the user's normal behavior; `mcp.*` and unknown settings are not copied. If global Kiro agent configs are copied into the runtime, their MCP entries are sanitized: `mcpServers` is emptied, legacy MCP inclusion is disabled, and `@mcp` tool selectors are removed. Agents then receive only the MCP servers allowed for the current Discord channel through ACP `mcpServers`. Enabled servers are launched through the bot's MCP policy proxy, which filters `tools/list` and blocks unauthorized `tools/call` requests before the agent can see or call those tools.

The bot also registers a built-in `bot-tools` MCP catalog entry backed by the same bot binary (`mcp-bot`). It exposes bot data-directory metadata tools, safe Discord egress tools (`bot_send_message`, `bot_send_file`), and cron management tools (`bot_create_cron`, `bot_list_cron`, `bot_delete_cron`). New channel setup enables `bot-tools` by default with the safe allowlist (`bot_data_summary`, `bot_list_channel_data`, `bot_list_cron`, `bot_send_message`, `bot_send_file`, `bot_create_cron`) and leaves the destructive `bot_delete_cron` tool disabled until a manager explicitly allows it. Existing channels and external catalog servers still require explicit policy changes through `/mcp manage` or `/mcp enable`.

Built-in `bot-tools` tools:

| Tool | Access hint | Description |
|------|-------------|-------------|
| `bot_data_summary` | read-only | Summarize data directory metadata without returning message content |
| `bot_list_channel_data` | read-only | List channel data directories, metadata file presence, and known public channel/thread names when the bot has observed them |
| `bot_list_cron` | read-only | List scheduled cron jobs for a channel |
| `bot_send_message` | write, non-destructive | Queue a Discord message for bot-side secret redaction and delivery to the current channel or thread target |
| `bot_send_file` | write, non-destructive | Queue a local text file for bot-side sanitization and upload as a redacted copy to the current channel or thread target |
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
- **MCP servers:** The bot treats Kiro MCP config as a catalog only, then adds built-in catalog entries such as `bot-tools`. Runtime agents use isolated `KIRO_HOME=DATA_DIR/kiro-agent-runtime`, override `KIRO_MCP_CONFIG` to a bot-managed empty config under that runtime home, and receive per-session `mcpServers` based on the current channel policy. The isolated runtime syncs allowlisted non-MCP CLI feature settings (`app.*`, `chat.*`, `inline.*`) so Kiro built-ins continue to follow the user's CLI preferences without importing MCP catalog settings. Runtime agent configs are sanitized to remove direct MCP sources before Kiro starts. Default policy is deny all MCP servers. Enabled servers are always launched through the bot MCP policy proxy, because Kiro `disabledTools` is not used as a security boundary.
- **Built-in bot-tools MCP:** `bot-tools` is served by the bot binary through the `mcp-bot` subcommand. It exposes metadata tools, safe Discord egress tools, and cron management tools. Metadata/list tools are read-only. New channel setup enables the safe default allowlist automatically; `bot_delete_cron` stays disabled until explicitly allowed. `bot_list_channel_data` returns known public channel/thread names when the bot has observed them, without returning message content. `bot_send_message` and `bot_send_file` queue pending egress actions that the main bot delivers to the current channel or thread target only after secret redaction; file delivery uploads a sanitized text copy and refuses binary or oversized files instead of uploading the original.
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
- **Cron jobs:** Definitions in `DATA_DIR/cron/cron.json`, execution history in `DATA_DIR/cron/<jobID>/history.jsonl` (includes full agent output). Set `CRON_TIMEZONE` explicitly for deployed services so schedules such as `30 12 * * *` run in the intended local timezone. Cron setup no longer asks for a working directory; scheduled agent work always runs with the owning channel's current CWD, so changing `/cwd` changes subsequent cron executions for that channel. Uninitialized channels cannot create, resume, manually run, or execute agent-backed cron jobs. `bot-tools` writes cron create/delete requests as pending JSON actions under `DATA_DIR/cron/pending/`; `CronTask` validates and ingests those actions on scheduler ticks, removes invalid actions, and only deletes jobs when the requested channel owns the job.
