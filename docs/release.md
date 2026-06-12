# Release and Deployment

This document covers the full flow from local development through tagging, publishing, deploying to a remote host, and verifying production state.

## 1. Standard Preflight

Run before any tag or deploy:

```bash
scripts/release-preflight.sh
```

This runs:

- `go test ./...`
- `go vet ./...`
- `go build ./...`
- `docker compose config --quiet`
- `docker build -t kiro-discord-bot:preflight .`
- `docker run --rm --entrypoint kiro-cli kiro-discord-bot:preflight --version`
- `git diff --check`

Skip Docker on machines without it:

```bash
RUN_DOCKER_BUILD=0 scripts/release-preflight.sh
```

## 2. Local ACP Smoke Test

Run on a machine where `kiro-cli` is installed and authenticated:

```bash
RUN_ACP_SMOKE=1 KIRO_CLI=$(which kiro-cli) scripts/release-preflight.sh
```

Validates spawn, initialize, `session/new`, prompt/response, and stop.

## 3. Built-in MCP Smoke Test

Run when touching MCP policy, `main.go` subcommands, `internal/botmcp`, or cron pending ingestion:

```bash
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | DATA_DIR="$(mktemp -d)" go run . mcp-bot
```

The output should include `serverInfo.name="bot-tools"` and the expected tools:
`bot_data_summary`, `bot_list_channel_data`, `bot_list_cron`, `bot_send_message`,
`bot_send_file`, `bot_create_cron`, and `bot_delete_cron`.

## 4. GitHub Actions

- **Preflight** (`.github/workflows/preflight.yml`): runs on push to `main` and PRs. Same script with `RUN_DOCKER_BUILD=false RUN_ACP_SMOKE=0`.
- **Release** (`.github/workflows/release.yml`): triggered by `v*` tags. Runs tests then GoReleaser to produce cross-platform archives.

## 5. Tagging a Release

```bash
# After preflight passes; replace vX.Y.Z with the intended version:
git tag vX.Y.Z
git push origin vX.Y.Z
```

- Minor bump (`vX.Y.0`): feature additions or behavior changes.
- Patch bump (`vX.Y.Z`): bug fixes, doc changes, no behavior change.

GoReleaser produces archives containing `kiro-discord-bot`, `mcp-discord`, and `mcp-media` binaries for linux/darwin × amd64/arm64.

## 6. Publish Verification

Wait for the release workflow to complete before deploying artifacts:

```bash
RUN_ID=$(gh run list --workflow release --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$RUN_ID" --exit-status
gh run view "$RUN_ID" --log-failed
```

Confirm the tag and GitHub Release are visible:

```bash
TAG=vX.Y.Z
gh release view "$TAG" --json tagName,name,isDraft,isPrerelease,url
git ls-remote origin refs/heads/main "refs/tags/$TAG"
```

Do not deploy from a newly pushed tag until the release exists, is not a draft, and the tag is visible on `origin`.

## 7. Remote Deployment (systemd)

Assumptions: Linux host, systemd service, binaries installed at `<INSTALL_DIR>` (e.g. `/opt/kiro-discord-bot`).

### 7.1 Download Release

```bash
# On the remote host:
VERSION=vX.Y.Z
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
TARBALL="kiro-discord-bot_linux_${ARCH}.tar.gz"

cd /tmp
curl -sLO "https://github.com/<OWNER>/<REPO>/releases/download/${VERSION}/${TARBALL}"
tar xzf "$TARBALL"
```

### 7.2 Stop Service & Backup

```bash
sudo systemctl stop kiro-discord-bot

# Backup current binaries
sudo cp <INSTALL_DIR>/kiro-discord-bot <INSTALL_DIR>/kiro-discord-bot.bak
sudo cp <INSTALL_DIR>/mcp-discord-server <INSTALL_DIR>/mcp-discord-server.bak 2>/dev/null || true
sudo cp <INSTALL_DIR>/mcp-media-server <INSTALL_DIR>/mcp-media-server.bak 2>/dev/null || true
```

### 7.3 Install New Binaries

```bash
sudo cp /tmp/kiro-discord-bot <INSTALL_DIR>/kiro-discord-bot
sudo cp /tmp/mcp-discord <INSTALL_DIR>/mcp-discord-server
sudo cp /tmp/mcp-media <INSTALL_DIR>/mcp-media-server
sudo chmod +x <INSTALL_DIR>/kiro-discord-bot <INSTALL_DIR>/mcp-discord-server <INSTALL_DIR>/mcp-media-server
```

### 7.4 Start Service

```bash
sudo systemctl start kiro-discord-bot
sudo systemctl status kiro-discord-bot
journalctl -u kiro-discord-bot -n 20 --no-pager
```

### 7.5 Post-Deploy Verification

1. Run `/doctor` or `!doctor` in Discord — confirm all checks pass.
2. Verify required secret env vars show `set (redacted)` in the runtime/environment section.
3. Send a test message — confirm agent responds.
4. If STT is enabled, send a voice message — confirm transcription works.
5. If MCP servers are configured, verify they respond to tool calls.

## 8. Rollback

If the new version has issues:

```bash
sudo systemctl stop kiro-discord-bot
sudo cp <INSTALL_DIR>/kiro-discord-bot.bak <INSTALL_DIR>/kiro-discord-bot
sudo cp <INSTALL_DIR>/mcp-discord-server.bak <INSTALL_DIR>/mcp-discord-server 2>/dev/null || true
sudo cp <INSTALL_DIR>/mcp-media-server.bak <INSTALL_DIR>/mcp-media-server 2>/dev/null || true
sudo systemctl start kiro-discord-bot
```

Verify with `/doctor` after rollback.

## 9. kiro-cli Upgrade

`kiro-cli` is the AI backend that the bot spawns via ACP. It is versioned independently.

```bash
# Check current version
kiro-cli version

# Download latest (adjust URL per your distribution method)
curl -sL <KIRO_CLI_DOWNLOAD_URL> -o /tmp/kiro-cli
chmod +x /tmp/kiro-cli
sudo mv /tmp/kiro-cli $(which kiro-cli)

# Verify
kiro-cli version

# Restart bot to pick up new kiro-cli
sudo systemctl restart kiro-discord-bot
```

After upgrade, run `/doctor` to confirm ACP preflight still passes.

## 10. Environment Configuration

- Bot reads `.env` at `<INSTALL_DIR>/.env` (via systemd `EnvironmentFile`).
- Use `/doctor` to see env set/unset state plus selected effective runtime values. Sensitive env vars only show `set (redacted)` and never include raw values or partial secrets.
- When adding a new env var, follow the path in `.kiro/steering/project.md` Completeness Checklist.

## 11. Safety Rules

- Preflight NEVER touches runtime state (no stop/start, no DATA_DIR mutation, no Discord side effects).
- Do NOT delete `DATA_DIR`, Docker volumes, `.kiro/`, or `.env` during a release.
- For Docker deploys: `docker compose config --quiet` before `docker compose up -d --build`.
- For systemd deploys: build/test first, then stop → replace → start.
