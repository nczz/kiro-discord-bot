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

## Agent Entrypoint

`AGENTS.md` at the repository root is the first file an agent should read. It summarizes the non-negotiable principles, architecture boundaries, source-of-truth map, i18n/Discord output rules, MCP permission rules, and verification expectations that apply across Kiro, OMP, and other coding agents.

Use `.kiro/steering/*.md` for deeper Kiro steering and recurring project knowledge, not as the only cross-agent contract. If `AGENTS.md`, `.kiro/steering/*`, and this docs site disagree, stop and resolve the drift before implementation.

When you change architecture boundaries, security rules, verification expectations, or agent onboarding guidance, update `AGENTS.md` in the same change.

Keep `AGENTS.md` short enough to be a fast entrypoint. Link to deeper docs instead of copying every feature plan into it.

### Minimum agent checklist

- Read `AGENTS.md` first.
- Read the task-specific steering or design doc named by `AGENTS.md`.
- Use existing owner packages and helpers before adding new local logic.
- Keep code, tests, locale files, and docs aligned for user-facing behavior.
- Run the smallest verification that exercises the real changed path before reporting success.

## Agent Collaboration Contract

When an implementation uses subagents, the parent agent owns the shared contract before parallel work starts. The contract must name the stable interfaces, permission boundaries, data ownership, and verification commands that every slice must follow.

- Treat `docs-site/docs/` as the GitHub Pages source of truth for user-facing behavior. Update it in the same change that modifies commands, MCP tools, audit rows, lifecycle state, or permission checks.
- Keep Discord output on shared helpers: redaction first, split oversized replies, suppress mentions with empty `AllowedMentions`, and use locale keys for user-facing text.
- Do not inspect or expose raw bot `DATA_DIR/ch-*` paths for normal feature work. Use scoped MCP tools, audit queries, and documented state APIs instead.
- Use authenticated Discord actor context for lifecycle/admin actions. An agent or MCP client cannot self-assert management permission.
- Record durable audit before reporting mutation success. If install, restore, rollback, or policy changes fail after partial work, fix the transaction instead of papering over the symptom.
- Prefer natural-language UX for channel agents. Slash and text commands are fallback/admin shortcuts and must call the same internal services as bot-tools where applicable.

## Static Site

The canonical docs live under `docs-site/docs/`. Do not make README or INSTALL files the long-form source of truth. They should stay short and point to the site.
