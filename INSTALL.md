# Install kiro-discord-bot

This file is a short agent-friendly checklist. The canonical installation guide is:

https://nczz.github.io/kiro-discord-bot/guide/installation.html

## Checklist

1. Detect OS and architecture:

   ```bash
   uname -s
   uname -m
   ```

2. Confirm `kiro-cli` exists and is authenticated:

   ```bash
   which kiro-cli
   kiro-cli --version
   ```

3. Create a Discord bot:

   - Scopes: `bot`, `applications.commands`
   - Enable Message Content Intent
   - Permissions: View Channels, Send Messages, Send Messages in Threads, Create Public Threads, Read Message History, Add Reactions, Use Slash Commands
   - Keep Interactions Endpoint URL empty

4. Download the latest release archive:

   https://github.com/nczz/kiro-discord-bot/releases/latest

5. Create `.env` or an equivalent service environment:

   ```env
   DISCORD_TOKEN=<bot token>
   DISCORD_GUILD_ID=<guild id>
   DEFAULT_CWD=/projects
   DATA_DIR=./data
   BOT_LOCALE=en
   ```

6. Run once in foreground:

   ```bash
   set -a
   . ./.env
   set +a
   ./kiro-discord-bot
   ```

7. In Discord, run `/cwd` to initialize a channel and `/doctor` to verify readiness.

## Persistent Services

Use the static site for full launchd, systemd, Docker, and release deployment guidance:

- Deployment: https://nczz.github.io/kiro-discord-bot/guide/deployment.html
- Release runbook: https://nczz.github.io/kiro-discord-bot/guide/release.html
- macOS MCP networking: https://nczz.github.io/kiro-discord-bot/guide/macos-mcp-networking.html
