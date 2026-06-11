# Release and Deployment Preflight

Use this flow before tagging, pushing, or restarting a production service. It is intentionally read-only for runtime data: it builds and tests artifacts, but it does not start the bot, stop systemd, run `docker compose up`, or mutate Discord state.

## Standard Preflight

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

To skip the Docker image build on a machine without Docker or without network/cache access:

```bash
RUN_DOCKER_BUILD=0 scripts/release-preflight.sh
```

## GitHub Actions

Pushes to `main` and pull requests run `.github/workflows/preflight.yml`, which executes the same script with:

```bash
RUN_DOCKER_BUILD=false RUN_ACP_SMOKE=0 scripts/release-preflight.sh
```

Manual workflow runs can enable Docker image build through the `docker_build` input. ACP smoke is intentionally not run in GitHub Actions because it requires an authenticated local `kiro-cli`.

## Local ACP Smoke Test

Run this only on a machine where `kiro-cli` is installed and authenticated. It starts a temporary ACP session under the test process and stops it after the check.

```bash
RUN_ACP_SMOKE=1 KIRO_CLI=/Users/chun/.local/bin/kiro-cli scripts/release-preflight.sh
```

The ACP smoke currently runs `TestPreflightCheck`, which validates spawn, initialize, `session/new`, prompt/response, and stop.

## Built-in MCP Smoke Test

Run this when touching MCP policy, `main.go` subcommands, `internal/botmcp`, or cron pending ingestion. It validates that the built binary can start the built-in `bot-tools` MCP server and answer `tools/list` over stdio.

```bash
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | DATA_DIR="$(mktemp -d)" go run . mcp-bot
```

The output should include `serverInfo.name="bot-tools"` and the tools `bot_data_summary`, `bot_list_channel_data`, `bot_list_cron`, `bot_create_cron`, and `bot_delete_cron` with read/write/destructive annotations matching their behavior.

## Deployment Safety

Before touching an existing service:

1. Confirm the worktree is clean after preflight.
2. Confirm `.env` values for `DEFAULT_CWD`, `ALLOWED_CWD_ROOTS`, `DATA_DIR`, `PREFLIGHT_MODE`, `TRUST_ALL_TOOLS`, and `TRUST_TOOLS`.
3. For Docker, verify `docker compose config --quiet` before `docker compose up -d --build`.
4. For systemd, build the binary first, then restart the service only after preflight passes.
5. Use `/doctor` or `!doctor` after deployment to verify the live bot can resolve `kiro-cli`, write to `DATA_DIR`, validate cwd policy, and complete ACP preflight.

Do not delete `DATA_DIR`, Docker volumes, `.kiro`, or existing `.env` files during a release.
