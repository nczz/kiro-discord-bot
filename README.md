# kiro-discord-bot

Turn any Discord channel into an AI-powered workspace. This bot connects Discord to [kiro-cli](https://kiro.dev) AI agents directly via the Agent Client Protocol (ACP) over stdio, giving your team on-demand access to coding assistants, DevOps automation, scheduled tasks, and more — all from the chat interface you already use.

**What you can do:**
- 💬 Chat with AI agents per channel — each channel gets its own isolated session and project context
- 🔧 Let agents read/write code, run commands, and manage infrastructure in your project directories
- 🔄 Switch between models on the fly — per channel, no restart needed
- ⏰ Schedule recurring tasks with cron — agents check servers, run tests, generate reports on autopilot
- 🔔 Set one-time reminders — natural language like "in 30 minutes" or "tomorrow 9am" just works
- 🩺 Auto-healing — dead agents are detected and restarted automatically
- 📝 Full conversation logs — every interaction is recorded in JSONL for audit and analysis

**Created:** 2026-03-21 | **Language:** Go

---

## Deployment Guide

### Prerequisites

- Go 1.21+
- [kiro-cli](https://cli.kiro.dev/install) installed and logged in
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

### 2. Install kiro-cli and Log In

```bash
curl -fsSL https://cli.kiro.dev/install | bash
export PATH="$HOME/.local/bin:$PATH"
kiro-cli login
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
DEFAULT_CWD=/projects
DATA_DIR=/tmp/kiro-bot-data
ASK_TIMEOUT_SEC=300
STREAM_UPDATE_SEC=3
KIRO_MODEL=
HEARTBEAT_SEC=60
ATTACHMENT_RETAIN_DAYS=7
CRON_TIMEZONE=Asia/Taipei
```

| Variable | Description | Default |
|----------|-------------|---------|
| `DISCORD_TOKEN` | Discord bot token | required |
| `DISCORD_GUILD_ID` | Guild ID for instant slash command registration | required |
| `KIRO_CLI_PATH` | Full path to kiro-cli binary | `kiro-cli` |
| `DEFAULT_CWD` | Default working directory for agents | `/projects` |
| `DATA_DIR` | Directory for sessions, logs, and attachments | `./data` |
| `ASK_TIMEOUT_SEC` | Agent response timeout in seconds | `300` |
| `STREAM_UPDATE_SEC` | Discord message update interval during streaming | `3` |
| `KIRO_MODEL` | Default model ID for kiro-cli (empty = kiro default) | `` |
| `HEARTBEAT_SEC` | Agent health check interval in seconds | `60` |
| `ATTACHMENT_RETAIN_DAYS` | Auto-delete attachments older than N days (0 = keep forever) | `7` |
| `CRON_TIMEZONE` | Timezone for cron schedules (empty = server local) | `` |
| `BOT_LOCALE` | Bot display language (`en`, `zh-TW`) | `en` |

---

### 5. Start (Local)

```bash
chmod +x start.sh
./start.sh
```

The script:
- Skips restart if bot is already running
- Builds and starts the bot with auto-restart watchdog
- Reads all config from `.env`

To force restart:
```bash
pkill -f kiro-discord-bot
./start.sh
```

---

### 6. Deploy with Docker

```bash
docker compose up -d --build
```

`docker-compose.yml` uses `network_mode: host` and mounts `~/.kiro` so the bot inherits your kiro login and MCP settings.

---

### 7. Grant Channel Permissions

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
| `/status` | Show agent state, queue length, session ID |
| `/cancel` | Cancel the currently running task |
| `/cwd` | Show current working directory |
| `/pause` | Switch to mention-only mode (bot ignores non-mention messages) |
| `/back` | Resume full-listen mode |
| `/model` | Show current model |
| `/model <model-id>` | Switch model and restart agent |
| `/models` | List all available models |
| `/cron` | Add a scheduled task (opens form) |
| `/cron-list` | List scheduled tasks with action buttons |
| `/cron-run <name>` | Manually run a scheduled task |
| `/remind <time> <content>` | Set a one-time reminder (tags you when due) |

All commands also work with `!` prefix (e.g. `!status`, `!reset`).

### Sending Tasks

**Full-listen mode (default):** Any message in the channel is sent to the agent.

**Mention mode (after `/pause`):** Only `@BotName your message` triggers the agent.

### Status Indicators

| Reaction | Meaning |
|----------|---------|
| ⏳ | Queued |
| 🔄 | Processing |
| ⚙️ | Running a tool |
| ✅ | Done |
| ❌ | Error |
| ⚠️ | Timed out |

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
    ├── per-channel Worker         goroutine, sequential execution
    ├── per-channel ChatLogger     JSONL conversation log
    └── Heartbeat                  background maintenance loop
          ├── HealthTask           agent liveness check + auto-restart
          ├── CleanupTask          expired attachment removal
          └── CronTask             scheduled jobs + one-shot reminders
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
├── start.sh              local start + watchdog script
├── bot/
│   ├── bot.go            Discord init, Ready handler, slash command registration
│   ├── handler.go        message routing, slash command handlers
│   ├── handler_cron.go   /cron Modal + /cron-list Button + /remind handlers
│   ├── health_adapter.go heartbeat ↔ manager bridge
│   └── cron_adapter.go   cron task ↔ manager bridge
├── channel/
│   ├── manager.go        per-channel session + worker lifecycle
│   ├── session.go        session struct + JSON persistence
│   ├── worker.go         job queue worker goroutine
│   └── logger.go         JSONL conversation logger
├── heartbeat/
│   ├── heartbeat.go      background task loop
│   ├── task.go           Task interface
│   ├── health.go         agent liveness check + auto-restart
│   ├── cleanup.go        expired attachment removal
│   ├── cron.go           cron scheduler + temp agent execution
│   ├── cron_store.go     cron job persistence (JSON)
│   └── schedule.go       natural language → cron/time parser
├── acp/
│   ├── agent.go          ACP agent process management (spawn, handshake, ask, stop)
│   ├── jsonrpc.go        JSON-RPC 2.0 ndjson transport
│   └── protocol.go       ACP protocol constants and types
├── cmd/
│   └── mcp-discord/
│       └── main.go       Discord MCP server (optional)
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

## Optional: Discord MCP Server

This project includes a built-in Discord MCP Server (`cmd/mcp-discord/`) that gives the kiro agent direct access to Discord — read messages, send messages, list channels, search, add reactions, etc.

Once enabled, the agent can proactively interact with Discord instead of only responding to forwarded messages.

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

- **Session persistence:** Sessions survive as long as the agent process is alive. Bot restart creates a new session (kiro-cli 1.28.1 does not support `session/load`).
- **MCP servers:** Inherited from `~/.kiro/settings/mcp.json` automatically.
- **Project steering:** Add `.kiro/steering/*.md` in the project directory to guide agent behavior.
- **Long responses:** Automatically split into multiple messages with `(1/N)` labels.
- **Conversation logs:** All user/agent interactions are recorded in `DATA_DIR/ch-<channelID>/chat.jsonl`.
- **Attachments:** Stored in `DATA_DIR/ch-<channelID>/attachments/` with timestamp prefixes. Auto-cleaned after `ATTACHMENT_RETAIN_DAYS`.
- **Cron jobs:** Definitions in `DATA_DIR/cron/cron.json`, execution history in `DATA_DIR/cron/<jobID>/history.jsonl` (includes full agent output).

---

---

## 部署說明（中文）

### 前置需求

- Go 1.21+
- Go 1.21+
- 已安裝並登入 [kiro-cli](https://cli.kiro.dev/install)
- Discord bot token，需具備：
  - Scopes：`bot`、`applications.commands`
  - 權限：查看頻道、發送訊息、新增反應、讀取訊息歷史
  - Privileged Intents：啟用 **Message Content Intent**

> ⚠️ **重要：** 請確認 Discord Developer Portal → General Information 中的 **Interactions Endpoint URL** 欄位為**空白**。若該欄位有設定 URL，Discord 會將 slash command 的 interaction 送往該 URL 而非 bot 的 gateway 連線，導致所有 `/` 指令出現「該應用程式未及時回應」錯誤。

### 快速開始

```bash
# 1. 安裝並登入 kiro-cli
curl -fsSL https://cli.kiro.dev/install | bash
kiro-cli login

# 2. 設定環境變數
cp .env.example .env
# 編輯 .env，填入 DISCORD_TOKEN、DISCORD_GUILD_ID、KIRO_CLI_PATH 等

# 3. 啟動
chmod +x start.sh && ./start.sh
```

### 指令說明

| 指令 | 說明 |
|------|------|
| `/start <目錄>` | 綁定專案目錄並啟動 agent |
| `/reset` | 重啟此 channel 的 agent |
| `/status` | 查詢 agent 狀態、queue 長度 |
| `/cancel` | 取消目前執行中的任務 |
| `/cwd` | 查詢目前工作目錄 |
| `/pause` | 切換為 @mention 模式 |
| `/back` | 恢復完整監聽模式 |
| `/model` | 查詢目前使用的 model |
| `/model <model-id>` | 切換 model 並重啟 agent |
| `/models` | 列出所有可用的 model |
| `/cron` | 新增排程任務（開啟表單） |
| `/cron-list` | 列出排程任務（含操作按鈕） |
| `/cron-run <name>` | 手動執行排程任務 |
| `/remind <時間> <內容>` | 預約單次提醒（到期時 tag 你） |

所有指令也支援 `!` 前綴（如 `!status`、`!reset`）。

### 注意事項

- Bot 需要在各 channel 的權限設定中明確授予讀寫權限
- Session 在 agent process 存活期間持續，bot 重啟後建立新 session
- MCP 設定自動繼承 `~/.kiro/settings/mcp.json`
- 回應被截斷時可用 `!resume` 補完

---

### 選配：Discord MCP Server

本專案內建 Discord MCP Server（`cmd/mcp-discord/`），啟用後 kiro agent 可直接操作 Discord——讀訊息、發訊息、列頻道、搜尋、加反應等。

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
