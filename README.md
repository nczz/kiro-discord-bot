# kiro-discord-bot

[繁體中文 README](README.zh-TW.md) | [Full documentation](https://nczz.github.io/kiro-discord-bot/)

**A trainable AI agent that lives in Discord.**

`kiro-discord-bot` connects Discord channels to [Kiro CLI](https://kiro.dev) agents through ACP over stdio. Each initialized channel can bind to a real project directory, keep its own agent session, remember project guidance, and expose MCP tools through explicit channel policy.

This repository README is intentionally short. The detailed user guide, admin guide, MCP setup, release runbook, and troubleshooting docs live on the static documentation site:

**https://nczz.github.io/kiro-discord-bot/**

## Why It Is Different

- **Project-bound agents**: each Discord channel maps to a working directory and an agent session.
- **Trainable context**: use memory, flash memory, steering files, conversation history, and Kiro knowledge instead of starting from zero every time.
- **Safe tool expansion**: MCP servers are discovered as a catalog, then exposed per channel through policy and a proxy.
- **Operational controls**: admin panels and sensitive diagnostics use private replies where Discord supports them.
- **Automation**: cron jobs and reminders let an agent run scheduled work under channel ownership.

## Quick Start

1. Install and authenticate `kiro-cli`.
2. Create a Discord bot with `bot` and `applications.commands` scopes, Message Content Intent, and the required channel/message permissions.
3. Download the latest release archive or build from source.
4. Provide environment variables such as `DISCORD_TOKEN`, `DISCORD_GUILD_ID`, `DEFAULT_CWD`, and `DATA_DIR`.
5. Run the bot once in the foreground and confirm it logs in.
6. In Discord, run `/cwd` in a channel to bind it to a project.
7. Run `/doctor` to verify permissions and Kiro readiness.

Full installation guide: https://nczz.github.io/kiro-discord-bot/guide/installation.html

## Common Docs

- Getting started: https://nczz.github.io/kiro-discord-bot/guide/getting-started.html
- Command reference: https://nczz.github.io/kiro-discord-bot/guide/commands.html
- Steering files: https://nczz.github.io/kiro-discord-bot/guide/steering.html
- MCP policy: https://nczz.github.io/kiro-discord-bot/guide/mcp.html
- Discord MCP server: https://nczz.github.io/kiro-discord-bot/guide/mcp-discord.html
- Deployment: https://nczz.github.io/kiro-discord-bot/guide/deployment.html
- Troubleshooting: https://nczz.github.io/kiro-discord-bot/guide/troubleshooting.html

## Build From Source

```bash
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
go build -ldflags "-X main.Version=$VERSION" -o kiro-discord-bot .
go build -o mcp-discord-server ./cmd/mcp-discord
go build -o mcp-media-server ./cmd/mcp-media
```

## Release and Operations

Run the release preflight before tagging or deploying:

```bash
scripts/release-preflight.sh
```

Release runbook: https://nczz.github.io/kiro-discord-bot/guide/release.html

## License

MIT
