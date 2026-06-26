# kiro-discord-bot

[繁體中文 README](README.zh-TW.md) | [Full documentation][docs]

**A trainable AI agent that lives in Discord.**

`kiro-discord-bot` connects Discord channels to [Kiro CLI](https://kiro.dev) agents through ACP over stdio. Each initialized channel can bind to a real project directory, keep its own agent session, remember project guidance, and expose MCP tools through explicit channel policy.

This repository README is intentionally short. The detailed user guide, admin guide, MCP setup, release runbook, and troubleshooting docs live on the [documentation site][docs].

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

Continue with the [full installation guide][install-doc].

## Common Docs

| Topic | Start here |
| --- | --- |
| First-time setup | [Getting Started][getting-started] · [Installation][install-doc] |
| Daily use | [Command Reference][commands] · [Listen Modes][listen-modes] |
| Agent context | [Steering Files][steering] · [Core Concepts][core-concepts] |
| Tool access | [MCP Policy][mcp] · [Bot Tools][bot-tools] · [Discord MCP Server][mcp-discord] |
| Operations | [Environment][environment] · [Deployment][deployment] · [Release Runbook][release] |
| Security and review | [Security Model][security] · [Audit, Usage, and Privacy][audit-usage] |
| Support | [Troubleshooting][troubleshooting] · [macOS MCP Networking][macos-networking] |

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

Follow the [release runbook][release] for tagging, publishing, deployment, and rollback.

## License

MIT

[docs]: https://nczz.github.io/kiro-discord-bot/
[getting-started]: https://nczz.github.io/kiro-discord-bot/guide/getting-started.html
[install-doc]: https://nczz.github.io/kiro-discord-bot/guide/installation.html
[commands]: https://nczz.github.io/kiro-discord-bot/guide/commands.html
[listen-modes]: https://nczz.github.io/kiro-discord-bot/guide/listen-modes.html
[core-concepts]: https://nczz.github.io/kiro-discord-bot/guide/core-concepts.html
[steering]: https://nczz.github.io/kiro-discord-bot/guide/steering.html
[mcp]: https://nczz.github.io/kiro-discord-bot/guide/mcp.html
[bot-tools]: https://nczz.github.io/kiro-discord-bot/guide/bot-tools.html
[mcp-discord]: https://nczz.github.io/kiro-discord-bot/guide/mcp-discord.html
[environment]: https://nczz.github.io/kiro-discord-bot/guide/environment.html
[deployment]: https://nczz.github.io/kiro-discord-bot/guide/deployment.html
[release]: https://nczz.github.io/kiro-discord-bot/guide/release.html
[security]: https://nczz.github.io/kiro-discord-bot/guide/security-model.html
[audit-usage]: https://nczz.github.io/kiro-discord-bot/guide/audit-usage-privacy.html
[troubleshooting]: https://nczz.github.io/kiro-discord-bot/guide/troubleshooting.html
[macos-networking]: https://nczz.github.io/kiro-discord-bot/guide/macos-mcp-networking.html
