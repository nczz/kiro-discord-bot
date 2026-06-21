---
name: decision-failure-patterns
description: Use when a change involves architecture direction, known limitations, recurring production failures, or regression prevention.
---

# Decision And Failure Patterns

This file records how to avoid repeating known wrong turns. Use it when a task touches architecture direction, multi-bot behavior, cron/thread delivery, MCP tools, cwd/Kiro settings, stuck agents, release/deploy, or any recurring runtime failure.

## When To Update This File

Update this file when any of these happen:

- A proposed fix is rejected because it solves the symptom in the wrong layer.
- A behavior is intentionally left unsupported or out of scope.
- A production/runtime issue reveals a reusable debugging pattern.
- A bug fix adds a regression test for a failure that could return.
- A release/deploy incident exposes a missing verification signal.
- A future issue should be opened, but the current change should not start that architecture work.
- A new architecture layer, shared helper, runtime mode, deployment target, or MCP tool category changes the source of truth documented in steering.

Do not hide these decisions only in chat history. Future maintainers need the decision, tradeoff, and trigger conditions in the repository.

## Decision Record Template

Use this shape for significant direction changes:

```text
Decision:
- What we decided.

Context:
- Current behavior and evidence.
- User goal or operational problem.

Rejected alternatives:
- Option and why it was rejected.

Current scope:
- What this change will do now.
- What this change explicitly will not do.

Future trigger:
- What evidence or requirement should reopen the decision.

Verification:
- Tests, logs, docs, or runtime checks that prove the current scope.
```

## Current Architecture Non-Goals

These are deliberate boundaries unless a new architecture task changes them:

- This project is not currently a single-process orchestrator for multiple Discord bot identities.
- Multiple deployed bots in one Discord server are independent runtimes. They may share code and configuration patterns, but they do not have a reliable distributed coordination layer through Discord mentions.
- Do not rely on bot-to-bot tag conversations as a durable task handoff mechanism. Discord delivery order, network delay, thread membership, permissions, and bot filtering can make that unreliable.
- Do not teach agents to coordinate by recursively asking each other questions in Discord. That creates latency, loop, mention-target, and accountability problems.
- Do not patch multi-bot coordination by weakening `requiresHumanMention`, peer filtering, MCP policy, or safe egress.
- If reliable multi-bot orchestration becomes a product goal, design it as an explicit server-side architecture with owned routing, state, audit, and bot identity boundaries.

## Current Safe Egress Decisions

### Document Files Are Extracted To Text, Not Rewritten In Original Format

Decision:

- `bot_send_file` may accept document formats with extractable readable text such as PDF, DOCX, and XLSX, but the bot-side safe egress output is an extracted, redacted `.txt` copy. The original binary document is not uploaded back to Discord.

Context:

- Safe egress is a security boundary for agent-accessible local files. Text files can be redacted directly, but binary document containers may hide content in headers, footers, comments, metadata, formulas, hidden sheets, embedded objects, or compressed streams.
- Rebuilding a PDF/DOCX/XLSX that preserves original format while proving every secret-bearing location was removed requires format-specific sanitization guarantees this project does not currently own.

Rejected alternatives:

- Rewriting PDF/DOCX/XLSX in place or generating a same-extension sanitized copy was rejected because it can leave unredacted content in unsupported document parts, corrupt the file, or imply a stronger guarantee than tests can prove.
- Uploading unsupported binary files after filename/content redaction was rejected because the original bytes may contain secrets that the bot cannot inspect safely.

Current scope:

- Text files are redacted and uploaded as sanitized text copies.
- Supported extractable document formats are converted to readable text, redacted, and uploaded as `.redacted.txt` copies.
- Unsupported binary files, unreadable files, oversized files, and files that expand beyond the safe extraction limit are refused instead of uploaded.

Future trigger:

- Reopen original-format output only if the project adds a format-specific sanitizer with tests for hidden document parts, comments, metadata, formulas, embedded objects, malformed files, and output-openability for each supported format.

Verification:

- Regression tests should cover extracted document redaction, compressed PDF text extraction, safe display names, temp directory creation, unsupported binary refusal, extraction/output size limits, locale reasons, MCP tool wording, and README behavior alignment.

## Known Failure Patterns

### CWD Or Kiro Settings Pollution

Symptoms:

- Agent appears to understand the wrong project.
- `.kiro/settings`, `mcp.json`, `cli.json`, or steering files are read or written under an unexpected root.
- `/doctor` and runtime behavior disagree about the effective cwd.

First checks:

- `channel.Manager.ValidateCWD`
- `internal/paths`
- `internal/kirosettings`
- manager `agentOptsForTarget`
- `/doctor` output
- startup cwd/default-cwd logs

Regression expectation:

- Cover `/cwd`, thread agents, cron temp agents, and project steering paths through the Manager path, not handler-local path joins.

### Cron Owner And Delivery Target Drift

Symptoms:

- A cron job is created successfully but does not appear in `/cron-list`.
- Manual trigger sends to the parent channel instead of the thread.
- Scheduled execution and manual execution disagree about the target.

First checks:

- `heartbeat/cron.go`
- `internal/cronpolicy`
- `bot/cron_adapter.go`
- stored job owner channel vs delivery thread metadata
- `DATA_DIR/cron/<jobID>/history.jsonl`

Regression expectation:

- Test creation from channel and thread, `/cron-list`, manual trigger, scheduled execution, timezone, and history persistence.

### Agent Produced Output But Worker Did Not Finish

Symptoms:

- Discord shows tool output or safe egress output, then the job remains processing.
- The agent reports a generic Kiro internal error after a useful tool result was already delivered.
- Worker completion, final response, and pending safe egress state disagree.

First checks:

- `channel/worker.go`
- `acp/agent.go`
- `bot/safe_egress.go`
- `internal/botegress`
- ACP stderr ring buffer
- timeout/cancel paths

Regression expectation:

- Test delivered safe egress followed by agent error separately from true undelivered failure. Do not hide real failures just because any tool ran.

### MCP Tool Used For Normal Replies

Symptoms:

- Agent calls `bot_send_message`, `discord_send_message`, or `discord_reply_message` for a normal final answer.
- Message lands in the wrong channel/thread.
- Long content fails with Discord 400 because a tool path skipped shared splitting.

First checks:

- `.kiro/steering/discord-mcp.md`
- `cmd/mcp-discord`
- `mcpproxy`
- `internal/botmcp`
- `internal/botegress`
- `internal/discordfmt`

Regression expectation:

- Normal replies should flow through the bot delivery path. Tool write paths remain available when the task needs Discord side effects, but they must reuse shared formatting, policy, redaction, mention suppression, error handling, and audit.

### Multi-Bot Mention Confusion

Symptoms:

- A bot answers a prompt intended for another bot.
- Bots ask each other follow-up questions instead of completing the user's task.
- Thread mode changes who responds.
- Peer discovery logs look correct at startup but behavior differs in a channel or thread.

First checks:

- `bot/peers.go`
- mention parsing in `bot/handler.go`
- `BOT_PEERS`
- startup `user_peers` / `role_only_peers` logs
- channel and thread member visibility
- `docs/listen-mode-matrix.md`

Regression expectation:

- Cover direct human mention, bot-only mention, mixed mentions, channel mode, thread mode, and role-only peers. Do not make bot-to-bot coordination a hidden side effect.

### Release Artifact Or Runtime Version Mismatch

Symptoms:

- Host service is active but still running an old binary.
- Local build was deployed when the user asked for the GitHub-built artifact.
- Different hosts run different versions after a release.

First checks:

- GitHub release/tag identity
- downloaded artifact name, architecture, and checksum when available
- service unit or launchd plist executable path
- startup version banner
- `/doctor`
- journal/launchd logs

Regression expectation:

- Deployment handoff must report each target separately and include artifact identity plus runtime version evidence.

### User-Visible Timestamp Shows Wrong Timezone

Symptoms:

- Cron thread title shows UTC or server-local time instead of the configured `CRON_TIMEZONE`.
- `/cron-list` last_run/next_run times do not match the user's expected timezone.
- Cron execution separator lines in threads use a different timezone than the schedule.

First checks:

- `bot/cron_adapter.go`: all `time.Now().Format(...)` must use `.In(loc)`.
- `bot/handler_cron.go`: `buildCronCard` must receive and apply a `*time.Location`.
- `heartbeat/cron.go`: history display must use `.In(c.location)`.
- `CRON_TIMEZONE` env var value and whether it was loaded correctly at startup.
- `/doctor` runtime overview.

Root cause pattern:

- `time.Now().Format("01/02 15:04")` uses the process-local timezone (often UTC on servers), not the user-configured `CRON_TIMEZONE`.
- `time.Parse(time.RFC3339, ...)` preserves the stored offset but does not convert to the display timezone — correct only by accident when the writer and reader use the same location.

Fix pattern:

- Always call `.In(loc)` before `.Format(...)` for any user-visible timestamp.
- Obtain location from `CRON_TIMEZONE` via the shared Bot helper (`cronLocationOrLocal`) or `c.location` inside CronTask.
- Do not rely on RFC3339 offset preservation as a substitute for explicit timezone conversion.

Regression expectation:

- All user-visible time outputs in cron/thread/slash responses must show in `CRON_TIMEZONE`.
- Test with a non-UTC `CRON_TIMEZONE` to catch implicit local-time bugs.

## Architecture Decision Checklist

Before implementing a structural change, answer:

1. Is the change solving the root problem or only suppressing a symptom?
2. Which layer owns the concern today?
3. Which existing helper or policy would be bypassed by the easy fix?
4. What current limitation should be documented instead of patched around?
5. What future issue should own larger architecture work?
6. What regression test proves the chosen boundary?
7. What evidence would make this decision wrong later?

If these answers are unclear, stop at a design proposal instead of editing code.

## Regression Test Standard

Every recurring failure should leave a test or an explicit reason why a runtime-only verification is the best available guard.

A good regression test:

- Exercises the runtime path, not just a new private helper.
- Covers the original failure symptom and at least one nearby edge case.
- Proves the shared helper or policy layer is used.
- Fails clearly when ownership, target, or policy is wrong.
- Avoids real Discord, secrets, or local machine paths unless explicitly marked as integration-only.

## Handoff Standard For Known Failures

When closing a fix for a known failure pattern, include:

- the failure pattern name
- the root cause layer
- the rejected shortcut
- the shared helper or policy used
- the regression test or runtime signal
- whether docs, steering, and i18n changed
- whether a larger architecture issue remains out of scope
