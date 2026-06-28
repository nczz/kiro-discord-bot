# Installation

This page is the canonical installation guide. The short `INSTALL.md` in the repository is kept as an agent-friendly checklist and points back here for details.

## 1. Prepare an ACP Engine

Install and authenticate at least one supported ACP engine before starting the bot.

For Kiro CLI:

```bash
curl -fsSL https://cli.kiro.dev/install | bash
export PATH="$HOME/.local/bin:$PATH"
kiro-cli --version
```

For interactive hosts, use:

```bash
kiro-cli login
```

For headless hosts, set `KIRO_API_KEY` in the bot service environment.

For OMP, install and authenticate `omp`, then expose it with `OMP_PATH` when it is not already on `PATH`:

```bash
omp --version
```

## 2. Create a Discord Bot

Create an application in the Discord Developer Portal, then configure:

| Area | Required setting |
| --- | --- |
| OAuth2 scopes | `bot`, `applications.commands` |
| Base permissions | View Channels, Send Messages, Send Messages in Threads, Create Public Threads, Read Message History, Add Reactions, Use Slash Commands |
| Optional permissions | Manage Threads, Embed Links, Attach Files, depending on enabled features |
| Privileged intents | Message Content Intent |

The Interactions Endpoint URL in General Information must be empty. If it is set, Discord sends slash commands to that endpoint instead of the gateway connection, and commands time out.

## 3. Download or Build

Use the latest release archive for your OS and architecture:

| OS | Arch | Archive |
| --- | --- | --- |
| macOS | arm64 | `kiro-discord-bot_darwin_arm64.tar.gz` |
| macOS | amd64 | `kiro-discord-bot_darwin_amd64.tar.gz` |
| Linux | amd64 | `kiro-discord-bot_linux_amd64.tar.gz` |
| Linux | arm64 | `kiro-discord-bot_linux_arm64.tar.gz` |

Example:

```bash
curl -fsSL https://github.com/nczz/kiro-discord-bot/releases/latest/download/kiro-discord-bot_darwin_arm64.tar.gz | tar xz
```

To build from source:

```bash
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
go build -ldflags "-X main.Version=$VERSION" -o kiro-discord-bot .
go build -o mcp-discord-server ./cmd/mcp-discord
go build -o mcp-media-server ./cmd/mcp-media
```

## 4. Configure Environment

The bot does not automatically load `.env`. A foreground shell, launchd, systemd, or Docker must inject environment variables.

Minimum configuration:

```env
DISCORD_TOKEN=your-bot-token
DISCORD_GUILD_ID=your-guild-id
DEFAULT_CWD=/projects
DATA_DIR=./data
BOT_LOCALE=en
```

Recommended production additions:

```env
KIRO_API_KEY=your-headless-key
AGENT_ENGINE=kiro
AGENT_ENGINES_ENABLED=kiro
ALLOWED_CWD_ROOTS=/projects
CRON_TIMEZONE=Asia/Taipei
USAGE_TIMEZONE=Asia/Taipei
PREFLIGHT_MODE=warn
THREAD_AGENT_MAX=5
THREAD_AGENT_IDLE_SEC=900
```

Use `/doctor` after startup to inspect effective runtime values. Sensitive values are redacted. See [Environment Reference](environment.md) for every supported variable and default.

## 5. Run Once in Foreground

```bash
set -a
. ./.env
set +a
./kiro-discord-bot
```

Expected log sequence:

1. ACP preflight starts and reports each enabled engine.
2. `kiro-discord-bot <version> starting`.
3. Slash commands register.
4. `Bot running as <name>#<discriminator>`.

## 6. Initialize a Discord Channel

In a Discord channel, run `/cwd`. The setup panel lets a channel manager select or create a project under `DEFAULT_CWD`. Once setup completes, the channel can start agent work and the built-in `bot-tools` MCP server is enabled with a safe default allowlist.

Run `/doctor` in the initialized channel to verify the channel can view, send, create threads, read history, and talk to the enabled ACP engine.

## 7. Decide What to Enable Next

Basic operation does not require external MCP servers. Add capabilities deliberately:

- Use `/memory` and `/flashmemory` for lightweight prompt rules.
- Use `/steering create` for versioned project guidance.
- Use `/mcp manage` to enable MCP tools per channel.
- Use `/cron` only when the channel has a clear owner for scheduled work.
