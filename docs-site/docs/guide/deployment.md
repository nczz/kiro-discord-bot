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

Use [Enable A2A with NATS](a2a-nats-setup.md) for first-time setup from NATS server through `.env` and Discord policy. Deploy NATS/JetStream before enabling bot A2A variables. The internal lightweight deployment uses one private JetStream node with TLS, token authentication, persistent storage, localhost-only monitoring, and host/network firewalling; hardened deployments may instead use NKey/JWT credentials through `NATS_CREDS_FILE`, optional TLS CA validation, one credential per stable bot/base identity with ACLs that authorize the derived runtime ID subjects, and `A2A_PRODUCTION_SECURITY=true`. Inject A2A variables through the service manager or container environment, restart or drain the bot, then verify `/doctor` plus the [A2A rollout smokes](a2a-nats-rollout.md). Keep `NATS_URL` empty until the setup and rollout gates are ready. For the identity, subject, task, policy, and delivery model, see [A2A Protocol Model](a2a-protocol.md).

## WebShare Relay Deployment

WebShare adds a second process when enabled: the pure Go `webshare-relay` binary. The relay may sit behind nginx, Caddy, or Traefik and serves both the TypeScript web UI and WebSocket room endpoint. The bot connects outbound as the authenticated host, so do not open an inbound bot port for WebShare.

Production deployments should:

- terminate TLS at the reverse proxy or relay;
- keep `RELAY_HOST_TOKEN_FILE` and matching `WEBSHARE_HOST_TOKEN_FILE` outside the repository;
- preserve WebSocket `Upgrade`, `Connection`, `Authorization`, `Host`, and `X-Forwarded-Proto` headers through the proxy;
- use long WebSocket read/send timeouts;
- keep one relay process per public hostname unless sticky room routing is implemented; and
- run `/webshare status` and `/doctor` after restart.

For nginx, systemd, Docker Compose, bot env, and relay env examples, see [WebShare](webshare.md).

## Release Updates

Before tagging or deploying a release, run:

```bash
scripts/release-preflight.sh
```

When touching ACP, MCP policy, bot tools, cron, or deployment behavior, run the relevant smoke checks described in the [Release Runbook](release.html).
