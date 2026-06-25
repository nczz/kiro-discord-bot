# Deployment

## Local Foreground Run

Start with a foreground run before creating a service:

```bash
set -a
. ./.env
set +a
./kiro-discord-bot
```

Confirm the bot logs in, registers slash commands, and responds to `/doctor`.

## macOS launchd

For macOS, run the bot as a LaunchAgent with an explicit shell command that sources `.env` and executes the release binary. If private LAN MCP servers fail from launchd but work from an interactive shell, check proxy variables, Local Network permission, and the service identity. See `docs/macos-mcp-networking.md` in the repository for the full runbook.

## Linux systemd

For Linux hosts, use a service unit with `WorkingDirectory`, `EnvironmentFile`, and an executable path pointing at the installed release binary. Build and test first, then stop the service, replace binaries, start it, and verify with `/doctor`.

## Docker

The Compose setup uses host networking, mounts Kiro authentication and project roots, and keeps runtime MCP config isolated from global Kiro MCP settings. Catalog servers still must be enabled per channel through `/mcp`.

## Release Updates

Before tagging or deploying a release, run:

```bash
scripts/release-preflight.sh
```

When touching ACP, MCP policy, bot tools, cron, or deployment behavior, run the relevant smoke checks described in `docs/release.md`.
