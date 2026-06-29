# Install kiro-discord-bot

This file is a short agent-friendly checklist. The canonical installation guide is [Installation][installation].

## Checklist

1. Detect OS and architecture:

   ```bash
   uname -s
   uname -m
   ```

2. Confirm at least one ACP engine exists and is authenticated. Kiro is the default:

   ```bash
   which kiro-cli
   kiro-cli --version
   ```

   OMP is optional. If you enable it, confirm the binary and authenticate the intended profile:

   ```bash
   omp --version
   # Optional isolated production profile:
   # OMP_PROFILE=kiro-discord-bot omp setup
   ```

3. Create a Discord bot:

   - Scopes: `bot`, `applications.commands`
   - Enable Message Content Intent
   - Permissions: View Channels, Send Messages, Send Messages in Threads, Create Public Threads, Read Message History, Add Reactions, Use Slash Commands
   - Keep Interactions Endpoint URL empty

4. Download the [latest release archive][latest-release].

5. Create `.env` or an equivalent service environment:

   ```env
   DISCORD_TOKEN=<bot token>
   DISCORD_GUILD_ID=<guild id>
   DEFAULT_CWD=/projects
   DATA_DIR=./data
   BOT_LOCALE=en
   # Optional dual-engine setup:
   # AGENT_ENGINES_ENABLED=kiro,omp
   # OMP_PATH=omp
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

Use the static site for full environment, launchd, systemd, Docker, and release deployment guidance:

- [Environment Reference][environment]
- [Agent Engines][agent-engines]
- [Deployment][deployment]
- [Release Runbook][release]
- [macOS MCP Networking][macos-networking]

[installation]: https://nczz.github.io/kiro-discord-bot/guide/installation.html
[latest-release]: https://github.com/nczz/kiro-discord-bot/releases/latest
[environment]: https://nczz.github.io/kiro-discord-bot/guide/environment.html
[agent-engines]: https://nczz.github.io/kiro-discord-bot/guide/agent-engines.html
[deployment]: https://nczz.github.io/kiro-discord-bot/guide/deployment.html
[release]: https://nczz.github.io/kiro-discord-bot/guide/release.html
[macos-networking]: https://nczz.github.io/kiro-discord-bot/guide/macos-mcp-networking.html
