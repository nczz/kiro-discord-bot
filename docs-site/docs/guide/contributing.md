# Contributor Guide

This project is a Go Discord bot plus optional MCP servers and a zero-dependency static documentation site.

## Repository Layout

| Path | Purpose |
| --- | --- |
| `bot/` | Discord command handling, replies, audit integration, MCP panels, and user-facing command behavior. |
| `channel/` | Channel/session manager, workers, listen modes, memory, usage, and MCP policy. |
| `heartbeat/` | Cron, reminders, cleanup, and background maintenance. |
| `audit/` | SQLite audit recorder and timeline query store. |
| `internal/botmcp/` | Built-in `bot-tools` MCP server. |
| `cmd/mcp-discord/` | Standalone Discord MCP server. |
| `cmd/mcp-media/` | Standalone media-generation MCP server. |
| `docs-site/` | Canonical static documentation site. |
| `docs/` | Historical notes and short compatibility documents that point to the site when applicable. |
| `scripts/` | Release and validation helpers. |

## Local Validation

Run the focused tests for the area you changed, then run the full suite before commit:

```bash
go test -count=1 ./...
```

For documentation:

```bash
cd docs-site
npm run verify
```

For release readiness:

```bash
scripts/release-preflight.sh
```

The release preflight should pass before version bumps and GitHub releases unless a maintainer explicitly accepts a documented exception.

## Development Rules

Keep behavior changes aligned with tests and docs. If you change a command, environment variable, MCP tool, audit event, usage attribution rule, deployment script, or release flow, update the docs-site page that owns that behavior.

Prefer small, code-path-grounded changes over broad refactors. This bot has operational state in Discord, local files, Kiro CLI sessions, and MCP policy, so regressions often appear only when those layers interact.

## Static Site

The canonical docs live under `docs-site/docs/`. Do not make README or INSTALL files the long-form source of truth. They should stay short and point to the site.
