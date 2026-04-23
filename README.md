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
CRON_TIMEZONE=Asia/Taipei
BOT_LOCALE=en
DOWNLOAD_TIMEOUT_SEC=120
QUEUE_BUFFER_SIZE=20
MAX_SCANNER_BUFFER_MB=64
KIRO_AGENT=
TRUST_ALL_TOOLS=true
TRUST_TOOLS=
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
| `KIRO_CLI_PATH` | Full path to kiro-cli binary | `kiro-cli` |
| `KIRO_API_KEY` | Kiro API key for headless auth (alternative to `kiro-cli login`) | — |
| `DEFAULT_CWD` | Default working directory for agents | `/projects` |
| `DATA_DIR` | Directory for sessions, logs, and attachments | `./data` |
| `ASK_TIMEOUT_SEC` | Agent response timeout (safety net) in seconds | `3600` |
| `STREAM_UPDATE_SEC` | Discord message update interval during streaming | `3` |
| `THREAD_AUTO_ARCHIVE` | Thread auto-archive duration in minutes (60/1440/4320/10080) | `1440` |
| `THREAD_AGENT_MAX` | Max concurrent thread agents | `5` |
| `THREAD_AGENT_IDLE_SEC` | Thread agent idle timeout in seconds | `900` |
| `CHANNEL_AGENT_IDLE_SEC` | Channel agent idle timeout in seconds (0 = disabled) | `0` |
| `KIRO_MODEL` | Default model ID for kiro-cli (empty = kiro default) | `` |
| `HEARTBEAT_SEC` | Agent health check interval in seconds | `60` |
| `ATTACHMENT_RETAIN_DAYS` | Auto-delete attachments older than N days (0 = keep forever) | `7` |
| `CRON_TIMEZONE` | Timezone for cron schedules (empty = server local) | `` |
| `BOT_LOCALE` | Bot display language (`en`, `zh-TW`) | `en` |
| `DOWNLOAD_TIMEOUT_SEC` | Attachment download timeout in seconds | `120` |
| `QUEUE_BUFFER_SIZE` | Max queued jobs per channel | `20` |
| `MAX_SCANNER_BUFFER_MB` | Max single-line JSON-RPC buffer from kiro-cli (MB). Increase if agents process many large attachments at once | `64` |
| `KIRO_AGENT` | Agent profile name for kiro-cli `--agent` flag (empty = kiro default) | `` |
| `TRUST_ALL_TOOLS` | Auto-approve all tool permission requests (`true`/`false`) | `true` |
| `TRUST_TOOLS` | Trust only specific tools (comma-separated names). Overrides `TRUST_ALL_TOOLS` when set | `` |
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

`docker-compose.yml` uses `network_mode: host` and mounts `~/.kiro` so the bot inherits your kiro login and MCP settings.

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
| `/start <cwd>` | Bind channel to a project directory and start agent |
| `/reset` | Restart the agent for this channel |
| `/status` | Show agent state, queue length, context usage, session ID |
| `/cancel` | Cancel the currently running task |
| `/cwd` | Show current working directory |
| `/pause` | Switch to mention-only mode (bot ignores non-mention messages) |
| `/back` | Resume full-listen mode |
| `/silent` | Toggle silent mode (compact tool output, default: on) |
| `/model` | Show current model |
| `/model <model-id>` | Switch model and restart agent |
| `/models` | List all available models |
| `/cron` | Add a scheduled task (opens form) |
| `/cron-list` | List scheduled tasks with action buttons |
| `/cron-run <name>` | Manually run a scheduled task |
| `/remind <time> <content>` | Set a one-time reminder (tags you when due) |
| `/compact` | Compress conversation history to free context |
| `/clear` | Clear conversation history |
| `/memory` | Manage persistent memory rules (add/list/remove/clear) |
| `/flashmemory` | Manage session-scoped flash memory (add/list/remove/clear) |

All commands also work with `!` prefix (e.g. `!status`, `!reset`).

**Thread-only commands** (inside a thread):

| Command | Description |
|---------|-------------|
| `!close` | Close the thread agent |
| `!cancel` | Cancel the thread agent's current task |
| `!reset` | Restart the thread agent |
| `!pause` | Switch thread to mention-only mode |
| `!back` | Resume thread full-listen mode |
| `!silent` | Toggle thread silent mode |
| `!compact` | Compress thread agent's conversation history |
| `!clear` | Clear thread agent's conversation history |
| `!model` | Show thread agent's current model |
| `!model <model-id>` | Switch thread agent's model and restart |
| `!models` | List all available models |

All thread commands also work as `/` slash commands inside a thread.

### Sending Tasks

**Full-listen mode (default):** Any message in the channel is sent to the agent.

**Mention mode (after `/pause`):** Only `@BotName your message` triggers the agent.

**Thread-based progress:** Each task automatically creates a Discord Thread from your message. Tool execution status and the final response are posted in the thread, keeping the main channel clean.

**Thread discussions:** You can continue chatting with the agent inside any thread. A dedicated agent is spawned per thread with the original task context injected. Thread agents are independent from the main channel agent, so both can work in parallel. Thread agents are automatically closed after idle timeout (`THREAD_AGENT_IDLE_SEC`) or when the thread is archived. Use `!close` in a thread to manually close its agent.

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

**Silent mode** (default: on) shows compact output — tool start shows icon + title only (no file list), tool results and thoughts are hidden, failures show a one-line summary. Use `/silent off` for full detail.

### Recovery

If a response is cut off, use `!resume` to re-post the agent's last output.

---

## Architecture

```
Discord User
    │ message / slash command
    ▼
Discord Bot (Go)
    ├── per-channel SessionStore   { agentName, sessionId, cwd }
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
├── main.go
├── config.go
├── kiro-discord-bot.service  systemd service template
├── bot/
│   ├── bot.go            Discord init, Ready handler, slash command registration
│   ├── handler.go        message routing, slash command handlers
│   ├── handler_cron.go   /cron Modal + /cron-list Button + /remind handlers
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
│   ├── cron_store.go     cron job persistence (JSON)
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
# 3. Restart the agent session
# Use /reset or !reset in Discord
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

### Quick Install

```bash
# 1. Build the MCP server binary
go build -o mcp-discord-server ./cmd/mcp-discord/

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
    "set -a && . /absolute/path/to/kiro-discord-bot/.env && exec /absolute/path/to/kiro-discord-bot/mcp-discord-server"
  ]
}
```

Replace `/absolute/path/to/kiro-discord-bot` with the actual project directory.

```bash
# 4. Restart the agent session
# Use /reset or !reset in Discord
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

- **Session persistence:** Sessions survive as long as the agent process is alive. Bot restart creates a new session with conversation history injected into the first prompt (budget-based: recent turns kept intact, older turns truncated, 20K char default).
- **MCP servers:** Inherited from `~/.kiro/settings/mcp.json` automatically. Note: ACP `session/new` mcpServers field is currently ignored by kiro-cli ([#7349](https://github.com/kirodotdev/Kiro/issues/7349)).
- **Project steering:** Add `.kiro/steering/*.md` in the project directory or `~/.kiro/steering/` globally to guide agent behavior.
- **Long responses:** Automatically split into multiple messages at 2000 char Discord limit.
- **Conversation logs:** All user/agent interactions are recorded in `DATA_DIR/ch-<channelID>/chat.jsonl`.
- **Attachments:** Stored in `DATA_DIR/ch-<channelID>/attachments/` with timestamp prefixes. Auto-cleaned after `ATTACHMENT_RETAIN_DAYS`.
- **Thread agents:** Idle timeout respects active work — `lastActivity` is updated during tool execution, preventing premature cleanup of long-running tasks.
- **Channel agent idle:** Set `CHANNEL_AGENT_IDLE_SEC` (default `0` = disabled) to auto-close idle channel agents and free resources. Agents restart automatically on next message.
- **Cron jobs:** Definitions in `DATA_DIR/cron/cron.json`, execution history in `DATA_DIR/cron/<jobID>/history.jsonl` (includes full agent output).

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

### 指令說明

| 指令 | 說明 |
|------|------|
| `/start <目錄>` | 綁定專案目錄並啟動 agent |
| `/reset` | 重啟此 channel 的 agent |
| `/status` | 查詢 agent 狀態、queue 長度、context 使用率 |
| `/cancel` | 取消目前執行中的任務 |
| `/cwd` | 查詢目前工作目錄 |
| `/pause` | 切換為 @mention 模式 |
| `/back` | 恢復完整監聽模式 |
| `/silent` | 切換安靜模式（精簡工具輸出，預設：開啟） |
| `/model` | 查詢目前使用的 model |
| `/model <model-id>` | 切換 model 並重啟 agent |
| `/models` | 列出所有可用的 model |
| `/cron` | 新增排程任務（開啟表單） |
| `/cron-list` | 列出排程任務（含操作按鈕） |
| `/cron-run <name>` | 手動執行排程任務 |
| `/remind <時間> <內容>` | 預約單次提醒（到期時 tag 你） |
| `/compact` | 壓縮對話歷史以釋放 context |
| `/clear` | 清除對話歷史 |
| `/memory` | 管理永久記憶規則（add/list/remove/clear） |
| `/flashmemory` | 管理 session 閃存記憶（add/list/remove/clear） |

所有指令也支援 `!` 前綴（如 `!status`、`!reset`）。

**討論串專用指令**（在 thread 中使用）：

| 指令 | 說明 |
|------|------|
| `!close` | 關閉討論串 agent |
| `!cancel` | 取消討論串 agent 目前的任務 |
| `!reset` | 重啟討論串 agent |
| `!pause` | 切換討論串為 @mention 模式 |
| `!back` | 恢復討論串完整監聽模式 |
| `!silent` | 切換討論串安靜模式 |
| `!compact` | 壓縮討論串 agent 的對話歷史 |
| `!clear` | 清除討論串 agent 的對話歷史 |
| `!model` | 查詢討論串 agent 目前的 model |
| `!model <model-id>` | 切換討論串 agent 的 model 並重啟 |
| `!models` | 列出所有可用的 model |

所有討論串指令也支援 `/` slash command 形式。

### 注意事項

- Bot 需要在各 channel 的權限設定中明確授予讀寫權限
- Session 在 agent process 存活期間持續，bot 重啟後建立新 session
- MCP 設定自動繼承 `~/.kiro/settings/mcp.json`
- 回應被截斷時可用 `!resume` 補完
- **討論串互動**：在 bot 建立的 thread 中發訊息，會自動啟動獨立的 thread agent 接續討論。閒置超過 `THREAD_AGENT_IDLE_SEC` 或 thread 歸檔時自動關閉，再次發訊息可重新啟動
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

#### 手動安裝

```bash
# 1. 編譯 MCP server
go build -o mcp-discord-server ./cmd/mcp-discord/

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
    "set -a && . /你的專案絕對路徑/.env && exec /你的專案絕對路徑/mcp-discord-server"
  ]
}
```

將 `/你的專案絕對路徑` 替換為實際路徑。

```bash
# 4. 重啟 agent session
# 在 Discord 中使用 /reset 或 !reset
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
