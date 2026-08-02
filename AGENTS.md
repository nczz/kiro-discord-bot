# AGENTS.md — kiro-discord-bot Agent Contract

This file is the cross-agent entrypoint for this repository. Read it before making claims, edits, reviews, or deployment suggestions. It is intentionally short; deeper contracts live in `.kiro/steering/` and `docs/`.

## Source-of-truth map

- Project architecture and recurring rules: `.kiro/steering/project.md`.
- Evidence-first review, debugging, handoff, and no-go triggers: `.kiro/steering/360-review-handoff.md`.
- Architecture decisions, non-goals, accepted limitations, and failure patterns: `.kiro/steering/decision-failure-patterns.md`.
- Discord MCP operating and safety rules: `.kiro/steering/discord-mcp.md`.
- Public contributor contract and GitHub Pages docs: `docs-site/docs/guide/contributing.md` and `docs-site/docs/zh-TW/guide/contributing.md`.
- Scoped skills lifecycle/security contract: `docs-site/docs/guide/scoped-skills.md`, `docs-site/docs/zh-TW/guide/scoped-skills.md`, and `docs/scoped-skills-implementation-plan.md`.
- A2A NATS work: `docs/a2a-nats-integration-spec.md`, `docs/a2a-nats-implementation-guide.md`, and `docs/a2a-nats-implementation-progress.md`.

If these sources conflict, stop and report the conflict. Do not invent behavior from chat memory.

## Non-negotiable principles

- Correctness first. Preserve the existing architecture and fix problems at the owning layer.
- User-facing behavior must stay aligned across code, tests, locale files, and `docs-site/docs/`.
- `docs-site/docs/` is the GitHub Pages source of truth. README files stay compact and link to the site.
- Keep decisions out of chat-only memory. Update steering or design docs when architecture direction, non-goals, known limitations, or regression expectations change.
- Never weaken security, audit, redaction, `AllowedMentions`, MCP policy, CWD validation, or safe egress to improve UX.
- Never expose raw bot runtime state paths such as `DATA_DIR/ch-*`, `sessions.json`, `policy.sqlite`, or audit DB paths in normal user-facing output.
- Default user communication is Traditional Chinese unless the user asks otherwise. Commit messages use English conventional commits.

## Architecture boundaries

- `bot/` routes Discord commands, interactions, replies, audit integration, and UI behavior. Do not put core business state here when a manager/service package owns it.
- `channel/` owns per-channel/session lifecycle, workers, CWD validation, thread behavior, MCP policy, and runtime orchestration.
- `acp/` owns ACP child process transport and protocol handling. Do not spawn or control `kiro-cli`/`omp` directly outside this boundary.
- `heartbeat/` owns cron, reminders, cleanup, and background maintenance through interfaces.
- `internal/botmcp/`, `mcpproxy/`, and `cmd/mcp-discord/` own MCP tools and policy boundaries.
- `internal/discordfmt`, `internal/secrets`, `internal/botegress`, and established bot/channel helpers own Discord output formatting, redaction, and safe delivery.

## Discord output and i18n

- Reuse existing Discord reply/egress helpers. Do not hand-roll 2000-character splitting, code-fence repair, Markdown downgrade, part prefixes, or mention controls.
- Any Discord write path must preserve policy checks, secret redaction, empty `AllowedMentions` where appropriate, delivery error handling, and audit/semantic events.
- User-facing strings must use locale keys in `locale/lang/en.json` and `locale/lang/zh-TW.json`.
- Locale keys must stay in parity. Any `%s` populated by Go errors, technical reasons, or internal state needs a reason map or equivalent localization layer; unknown values must be redacted before fallback display.
- User-visible times must be formatted in the intended timezone, usually `CRON_TIMEZONE`, not implicit local time.

## MCP, permissions, and runtime data

- MCP servers are a catalog until explicitly exposed by channel policy. Do not assume a configured server is available to every agent.
- New MCP tools require read-only/write/destructive classification, policy exposure rules, audit/redaction behavior, and tests.
- Lifecycle/admin actions require authenticated Discord actor context. An agent or MCP client cannot self-assert guild, channel, actor, target, or project authority through normal tool arguments.
- Skill installs and imports never grant missing MCP tools and never mutate MCP policy as a side effect.
- Project/CWD input must flow through `channel.Manager.ValidateCWD` or an established manager/path helper.

## Collaboration workflow

- For parallel agent work, the parent agent defines shared interfaces, permission boundaries, data ownership, and verification commands before assigning slices.
- Each slice must use existing package ownership and helpers instead of creating duplicate local logic.
- Preserve user work. Treat unrelated dirty files as user changes unless explicitly told otherwise.
- For complex reviews or handoffs, follow `.kiro/steering/360-review-handoff.md`.
- For recurring bugs, rejected approaches, non-goals, or future triggers, update `.kiro/steering/decision-failure-patterns.md`.

## Verification expectations

- Run verification that matches the changed behavior. Prefer the smallest command or smoke scenario that exercises the real runtime path.
- Go behavior change: run focused package tests, then broader tests when the contract changed across packages.
- User-facing command or Discord output change: verify slash/bang dispatch, locale parity, helper reuse, redaction/mention behavior, and docs.
- Docs-site change: run `cd docs-site && npm run verify`.
- Release/deployment change: run `scripts/release-preflight.sh`; ACP smoke requires explicit local/runtime capability.
- Do not claim completion from scaffolding, private helper tests, or stale notes when the real path was not exercised.
