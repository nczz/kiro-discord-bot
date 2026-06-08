# Install kiro-discord-bot

This document is written for AI agents. When a user asks you to help install kiro-discord-bot, follow the steps below. Use environment detection to minimize questions to the user.

## Agent: Environment Detection

Before starting, run these commands to auto-determine the user's setup:

```bash
uname -s          # OS: Darwin=macOS, Linux=linux, MINGW/MSYS=windows
uname -m          # Arch: arm64/aarch64=arm64, x86_64=amd64
echo $LANG        # Locale hint: zh_TW → BOT_LOCALE=zh-TW, else en
which kiro-cli    # Verify kiro-cli exists and note its path
kiro-cli --version  # Verify authentication works
echo $SHELL       # Shell type for .env sourcing method
pwd               # Candidate for DEFAULT_CWD
```

Use these results to:
- Select the correct download archive (Step 1)
- Pre-fill `BOT_LOCALE` based on system locale
- Skip `KIRO_CLI_PATH` if `which kiro-cli` succeeds
- Suggest `DEFAULT_CWD` based on current working directory or home

## Prerequisites

- `kiro-cli` installed and authenticated (`kiro-cli --version` succeeds)
  - Install: `curl -fsSL https://cli.kiro.dev/install | bash`
  - Auth: `kiro-cli login` (interactive) or set `KIRO_API_KEY` env (headless)
- A Discord account with permission to create bots

## Step 1: Download the binary

Release page: <https://github.com/nczz/kiro-discord-bot/releases/latest>

Select based on detected OS/Arch:

| uname -s | uname -m | File |
|----------|----------|------|
| Darwin | arm64 | `kiro-discord-bot_darwin_arm64.tar.gz` |
| Darwin | x86_64 | `kiro-discord-bot_darwin_amd64.tar.gz` |
| Linux | x86_64 | `kiro-discord-bot_linux_amd64.tar.gz` |
| Linux | aarch64 | `kiro-discord-bot_linux_arm64.tar.gz` |

Each archive contains:
- `kiro-discord-bot` — the bot
- `mcp-discord` — Discord MCP server (optional)
- `mcp-media` — Media generation MCP server (optional)

Download and extract example (adapt to detected values):

```bash
# macOS arm64 example:
curl -fsSL https://github.com/nczz/kiro-discord-bot/releases/latest/download/kiro-discord-bot_darwin_arm64.tar.gz | tar xz
```

## Step 2: Create a Discord Bot

Guide the user through these steps (they require browser interaction):

1. Open <https://discord.com/developers/applications> → **New Application**
2. **Bot** tab → **Reset Token** → copy token → this is `DISCORD_TOKEN`
3. **Bot** tab → **Privileged Gateway Intents** → enable **Message Content Intent**
4. **OAuth2** → **URL Generator**:
   - Scopes: `bot`, `applications.commands`
   - Permissions: View Channels, Send Messages, Send Messages in Threads, Create Public Threads, Manage Threads, Embed Links, Attach Files, Read Message History, Add Reactions, Use Slash Commands
5. Open generated URL → invite bot to server
6. Copy **Guild ID** (right-click server → Copy Server ID; needs Developer Mode in Discord settings)

⚠️ **Interactions Endpoint URL** (General Information tab) must be **empty**, otherwise slash commands will timeout.

Ask the user to provide: `DISCORD_TOKEN` and `DISCORD_GUILD_ID`.

## Step 3: Create .env

Create `.env` in the bot directory. Generate it based on detected environment + user-provided values:

```env
# === Required (user provides) ===
DISCORD_TOKEN=<from user>
DISCORD_GUILD_ID=<from user>

# === Auto-detected ===
# Set only if kiro-cli is NOT in PATH:
# KIRO_CLI_PATH=<result of which kiro-cli, only if needed>

# Derived from $LANG — use zh-TW if locale contains zh_TW, else en:
BOT_LOCALE=<detected>

# Suggest current project directory or ~/projects:
DEFAULT_CWD=<detected>
```

All other settings use sane defaults. Do NOT add settings the user didn't ask for.

## Step 4: Run

```bash
# Load .env and run in foreground
export $(grep -v '^#' .env | xargs)
./kiro-discord-bot
```

Note: The bot does NOT auto-load `.env`. Environment variables must be injected externally. The command above is the simplest method for manual runs. For persistent services, use systemd `EnvironmentFile` or Docker Compose `environment` instead.

Expected output:
1. `[preflight] ...` — kiro-cli connectivity check
2. `kiro-discord-bot <version> starting`
3. `Bot running as <name>#<discriminator>`
4. `Bot running. Ctrl+C to stop.`

If preflight shows WARNING, verify `kiro-cli --version` works.

## Step 5 (Optional): Persistent service

### macOS — run in background via tmux or launchd

### Linux — systemd unit

```ini
[Unit]
Description=kiro-discord-bot
After=network.target

[Service]
Type=simple
WorkingDirectory=/path/to/bot-dir
ExecStart=/path/to/bot-dir/kiro-discord-bot
EnvironmentFile=/path/to/bot-dir/.env
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Step 6 (Optional): Enable MCP Tools

The archive includes two MCP servers that extend the bot's agent capabilities:

- **mcp-discord** — lets the agent read/send Discord messages, manage channels, etc.
- **mcp-media** — lets the agent generate images, video, music, and TTS

These are NOT required for basic operation. Only set them up if the user wants the agent to have these extra abilities.

For setup instructions, guide the user through `INSTALL_MCP.md` in this project (or in the extracted archive directory). That file covers:
1. Registering the MCP server in kiro's catalog (`~/.kiro/settings/mcp.json`)
2. Configuring safety scopes (allowed guilds/channels)
3. Enabling per-channel via `/mcp enable`

## Verification

Tell the user to:
1. Go to their Discord server
2. Run `/doctor` — bot responds with health status
3. Mention the bot — it should reply via agent

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| "missing required env: DISCORD_TOKEN" | .env not in working dir | Run from the dir containing .env |
| Preflight WARNING | kiro-cli not found/authed | `kiro-cli login` or set KIRO_CLI_PATH |
| Slash commands missing | Sync delay | Wait 1 min or restart bot |
| "application did not respond" | Interactions Endpoint URL set | Clear it in Developer Portal |
| Bot ignores messages | Message Content Intent off | Enable in Developer Portal → Bot tab |
| Permission errors in channel | Bot role lacks perms | Check channel permission overrides for the bot role |
