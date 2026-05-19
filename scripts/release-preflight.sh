#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

RUN_DOCKER_BUILD="${RUN_DOCKER_BUILD:-1}"
RUN_ACP_SMOKE="${RUN_ACP_SMOKE:-0}"
DOCKER_IMAGE="${DOCKER_IMAGE:-kiro-discord-bot:preflight}"
TMP_BASE="${TMPDIR:-/tmp}"
GOCACHE="${GOCACHE:-$TMP_BASE/kiro-discord-bot-gocache}"
GOMODCACHE="${GOMODCACHE:-$TMP_BASE/kiro-discord-bot-gomodcache}"

step() {
  printf '\n==> %s\n' "$*"
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'missing required command: %s\n' "$1" >&2
    exit 1
  fi
}

is_true() {
  value="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  case "$value" in
    1|true|yes|on) return 0 ;;
    *) return 1 ;;
  esac
}

need_cmd go

step "go test ./..."
env -u KIRO_CLI GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" go test ./...

step "go vet ./..."
GOCACHE="$GOCACHE" go vet ./...

step "go build ./..."
GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" go build ./...

if command -v docker >/dev/null 2>&1; then
  step "docker compose config --quiet"
  docker compose config --quiet

  if is_true "$RUN_DOCKER_BUILD"; then
    step "docker build -t $DOCKER_IMAGE ."
    docker build -t "$DOCKER_IMAGE" .

    step "docker image kiro-cli smoke"
    docker run --rm --entrypoint kiro-cli "$DOCKER_IMAGE" --version
  else
    step "docker build skipped (RUN_DOCKER_BUILD=$RUN_DOCKER_BUILD)"
  fi
else
  step "docker unavailable; skipped compose and image checks"
fi

if is_true "$RUN_ACP_SMOKE"; then
  if [[ -z "${KIRO_CLI:-}" ]]; then
    printf 'RUN_ACP_SMOKE=1 requires KIRO_CLI=/path/to/kiro-cli\n' >&2
    exit 1
  fi
  step "ACP smoke with $KIRO_CLI"
  GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" KIRO_CLI="$KIRO_CLI" go test -count=1 -run '^TestPreflightCheck$' -v ./acp
else
  step "ACP smoke skipped (set RUN_ACP_SMOKE=1 KIRO_CLI=/path/to/kiro-cli)"
fi

step "git diff --check"
git diff --check

step "release preflight passed"
