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

## Local ACP Smoke Test

Run this only on a machine where `kiro-cli` is installed and authenticated. It starts a temporary ACP session under the test process and stops it after the check.

```bash
RUN_ACP_SMOKE=1 KIRO_CLI=/Users/chun/.local/bin/kiro-cli scripts/release-preflight.sh
```

The ACP smoke currently runs `TestPreflightCheck`, which validates spawn, initialize, `session/new`, prompt/response, and stop.

## Deployment Safety

Before touching an existing service:

1. Confirm the worktree is clean after preflight.
2. Confirm `.env` values for `DEFAULT_CWD`, `ALLOWED_CWD_ROOTS`, `DATA_DIR`, `PREFLIGHT_MODE`, `TRUST_ALL_TOOLS`, and `TRUST_TOOLS`.
3. For Docker, verify `docker compose config --quiet` before `docker compose up -d --build`.
4. For systemd, build the binary first, then restart the service only after preflight passes.
5. Use `/doctor` or `!doctor` after deployment to verify the live bot can resolve `kiro-cli`, write to `DATA_DIR`, validate cwd policy, and complete ACP preflight.

Do not delete `DATA_DIR`, Docker volumes, `.kiro`, or existing `.env` files during a release.
