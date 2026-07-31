# Deployment

## Local Foreground Run

Start with a foreground run before creating a service:

```bash
set -a
. ./.env
set +a
./kiro-discord-bot
```

Confirm the bot logs in, registers slash commands, and responds to `/doctor`. Review [Environment Reference](environment.md) before turning the foreground command into a service.

## macOS launchd

For macOS, run the bot as a LaunchAgent with an explicit shell command that sources `.env` and executes the release binary. If private LAN MCP servers fail from launchd but work from an interactive shell, check proxy variables, Local Network permission, and the service identity. See [macOS MCP Networking](macos-mcp-networking.html) for the full runbook.

## Linux systemd

For Linux hosts, use a service unit with `WorkingDirectory`, `EnvironmentFile`, and an executable path pointing at the installed release binary. Build and test first, then stop the service, replace binaries, start it, and verify with `/doctor`.

## Docker

The Compose setup uses host networking, mounts the selected engine authentication state and project roots, and keeps runtime MCP config isolated from global catalog sources. Catalog servers still must be enabled per channel through `/mcp`.

## A2A NATS Deployment

Deploy NATS/JetStream before enabling bot A2A variables. The internal lightweight deployment uses one private JetStream node with TLS, token authentication, persistent storage, localhost-only monitoring, and host/network firewalling; hardened deployments may instead use NKey/JWT credentials through `NATS_CREDS_FILE`, optional TLS CA validation, one credential per stable `A2A_AGENT_ID`, and `A2A_PRODUCTION_SECURITY=true`. Inject A2A variables through the service manager or container environment, restart or drain the bot, then verify `/doctor` plus the [A2A rollout smokes](a2a-nats-rollout.md). Keep `NATS_URL` empty until the rollout gates are ready.

## Release Updates

Before tagging or deploying a release, run:

```bash
scripts/release-preflight.sh
```

When touching ACP, MCP policy, bot tools, cron, or deployment behavior, run the relevant smoke checks described in the [Release Runbook](release.html).
