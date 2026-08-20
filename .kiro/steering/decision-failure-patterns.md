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
- The root `AGENTS.md` entrypoint stops matching the active project architecture, security boundaries, verification expectations, or cross-agent source-of-truth map.

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

### Tagged Discord Webhook Prompts Are Explicit Per-Channel Input, Not Bot Handoff

Decision:

- Discord channel webhook messages stay ignored by default. A parent channel must explicitly enable `/webhook mode:on`, and accepted webhook content must start with the bot's real Discord mention shown by `/webhook mode:status`.

Context:

- Discord webhooks are bot-authored messages with spoofable display names and URLs that can leak. Treating all bot-authored or all webhook messages as agent work would weaken the bot-to-bot loop guard in `bot.handleMessage`.

Rejected alternatives:

- Accepting every webhook message in a listening channel was rejected because CI/Zapier/n8n-style webhooks can be noisy and unauthenticated beyond possession of the URL.
- Reusing bot-to-bot handoff for normal channel webhooks was rejected because handoff is intentionally restricted to controlled thread result flow.
- Letting webhook text execute `!` or slash-style administration commands was rejected because a webhook is not an authenticated Discord actor.

Current scope:

- `/webhook mode:on|off|status` controls a parent-channel allow switch.
- Matching webhook input is prompt text only; it never becomes a privileged lifecycle/admin command.

Future trigger:

- If webhook IDs, external signatures, or per-integration policy become product requirements, design a dedicated policy store and audit model instead of weakening the current leading-mention gate.

Verification:

- Regression tests should cover default ignore, enabled + leading mention acceptance, missing/non-leading mention rejection, persistence, command status hint, and runtime routing past the bot-authored message gate.

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

## Current ACP Protocol Decisions

### Prompt stopReason Is Surfaced, Not Used For Success/Error Reclassification

Decision:

- `acp.Agent` parses the `session/prompt` result `stopReason` and exposes it via `StopReason()`, mirroring the turn-scoped `TurnMetrics()`/`ContextUsage()` accessor pattern. The worker reads it inside `OnComplete` and appends a localized notice only for abnormal reasons (`max_tokens`, `refusal`, `cancelled`). `end_turn`/empty is the silent common case.

Context:

- Verified against kiro-cli 2.10.0: the prompt result is `{"stopReason":"end_turn"}`. Previously `AskAsyncMulti` discarded it with `_ = raw`, so a token-limited or refused turn looked identical to a clean completion.
- This ties to the failure pattern "Agent Produced Output But Worker Did Not Finish".

Rejected alternatives:

- Changing the `OnComplete(response, err)` signature to add stopReason was rejected: it would touch both worker delivery paths plus every test fake, for data that fits the existing accessor pattern.
- Reclassifying `max_tokens`/`refusal` as errors was rejected: it would entangle the delicate `suppressGenericKiroErrorAfterEgress` logic. Current scope only appends a user-visible notice and records `stop_reason` in `agent_job_completed` audit metadata.

Current scope:

- Surface + notice + audit across the three delivery paths (thread async, inline async, sync fallback). No change to success/error classification or suppression logic.

Future trigger:

- If product needs to retry-on-`max_tokens` or block-on-`refusal`, design explicit turn-outcome handling rather than overloading the notice.

Verification:

- `acp` unit tests for reset/parse; `channel` runtime-path tests asserting the inline final reply contains the notice on `max_tokens` and omits it on `end_turn`.

### Subagent List Update Is Rendered Count-First, Best-Effort On Element Fields

Decision:

- `_kiro.dev/subagent/list_update` is parsed into `SubagentState` using only the verified top-level arrays (`subagents`, `pendingStages`). Element fields (`name`/`status`/`description`) are extracted best-effort from common key names and may be empty. The thread path renders a one-line progress message when there is activity; inline/silent skip it.

Context:

- kiro-cli 2.10.0 (default `--agent-engine v2`) emits the notification proactively, but the observed payload was always `{"subagents":[],"pendingStages":[]}`. Multiple probes (including a prompt explicitly asking for parallel subagents) never populated the arrays, so element shapes are unverified.

Rejected alternatives:

- Building a fully-typed parser for element fields was rejected per the project rule against speculative parsers for unobservable structures. Counts come from verified keys; field extraction is guarded and degrades to empty.

Current scope:

- Defensive parse + count-based progress line + best-effort entry labels. No assumption about element field semantics.

Future trigger:

- Once a populated payload is captured at runtime, enrich `SubagentEntry` with the real field names and add a parser test for that shape.

### Agent Engine v3 Is Out Of Scope Until Client Terminal Is Implemented

Decision:

- The bot continues to run the default `--agent-engine v2`. `v3` is not adopted.

Context:

- Probing `kiro-cli acp --agent-engine v3` (2.10.0) showed the agent issues a `_kiro/terminal/shell_type` server-to-client request and then stalls. The bot's generic `OnRequest` handler answers every server request with a permission outcome (`approved`/`denied`), which is the wrong response shape for a terminal-type query, so the v3 session does not progress.

Rejected alternatives:

- Special-casing `_kiro/terminal/shell_type` in the generic permission handler was rejected as a partial patch. v3 requires real client-side terminal capability handling, not a single hard-coded reply.

Current scope:

- Stay on v2. Do not pass `--agent-engine`.

Future trigger:

- If v3 features (e.g. richer subagent staging) become desirable, implement the ACP client `terminal/*` and `fs/*` request handlers properly (the bot already advertises `terminal` and `fs` capabilities it does not currently serve), then re-test the full v3 turn lifecycle.

Verification:

- Runtime probe of v2 vs v3 handshake + first prompt turn against kiro-cli 2.10.0.

### Dual-Engine: omp Integrated As An ACP Dialect, Not A Second Agent Type

Decision:

- omp (Oh My Pi) is integrated via `omp acp` as a SECOND ACP dialect of the existing `acp.Agent`, not
  a separate agent type. A `dialectProfile` (acp/dialect.go) captures the only points that differ from
  kiro-cli: launch args, model setter, cancel framing, session/new result parsing, and metrics source.
  Transport, handshake, streaming, tool_call, `session/request_permission` (+ TRUST policy / OnRequest
  approved/denied shape — verified accepted by omp), MCP injection, stopReason, and session/load are shared.
- The dialect is carried in `AgentOptions.Dialect` (zero value `DialectKiro`), NOT a new positional
  `StartAgent` parameter. This keeps the `StartAgent` signature stable for all ~15 call sites (prod +
  tests + preflight) and is idiomatic (the project already extends spawn behavior via AgentOptions).

Context:

- Verified (kiro 2.10.0 + omp 16.1.23): omp model switch = `session/set_config_option{configId:"model"}`
  (not session/set_model); cancel must be a NOTIFICATION (no id); session/new returns `configOptions[]`
  (not modes/models); usage = cumulative USD `cost` + tokens via `usage_update` + prompt-result `usage`
  (kiro uses credits via `_kiro.dev/metadata`); omp reads `AGENTS.md` (so does kiro) but not `.kiro/steering`;
  MCP per-channel policy is enforced by the bot's mcp-proxy at transport level (engine-agnostic — both
  engines forward injected env to the MCP subprocess). Full plan: docs/dual-engine-integration-plan.md.

Rejected alternatives:

- A positional `StartAgent(dialect)` param (breaks 15 call sites; no benefit over AgentOptions).
- A separate `ompAgent` type / interface explosion across manager/cron (rejected: both engines ARE
  `*acp.Agent`; only the dialect field varies, so `*acp.Agent` stays everywhere).
- Sharing one DATA_DIR / SQLite across two bot runtimes to allow multi-identity (rejected: violates the
  independent-runtime non-goal; the chosen product shape is M2 = single bot, single DATA_DIR, switchable
  engine, so the concern is moot).

Current scope:

- Implemented across all stages: kiroProfile keeps kiro byte-identical; ompProfile drives `omp acp`
  (configOptions→models/modes, set_config_option model switch, session/cancel as notification, USD
  usage via usage_update + prompt usage). `AGENT_ENGINE`/`AGENT_ENGINES_ENABLED`/`OMP_PATH` config with
  engine-aware preflight and KIRO_* gating (pure-omp runs without kiro-cli). `Session.Engine` resolves
  per scope (default→channel→thread→/engine override) and is preserved across all session reconstructions.
  `/engine` (slash+bang) switches engine via stop→fresh-session→restart→history-prefix replay. Usage is
  per-engine: credits (kiro) vs USD cost (omp), never cross-summed; footer + /usage render both. Steering
  is mirrored into a managed block in `AGENTS.md` (cross-engine) while `.kiro/steering` stays kiro-rich.
  Product shape is M2 (single bot identity, switchable engine, single DATA_DIR).

Future trigger:

- If a third ACP engine appears, add another dialect profile; do not fork the agent.

Verification:

- `go build ./...`, `go vet`, and `go test ./acp ./channel ./bot` pass with kiro behavior unchanged at
  each stage commit; omp dialect gets its own parse/cost/cancel tests + a gated ACP smoke.

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

### Engine-Scoped Model Leakage

Symptoms:

- A channel or thread works on one engine, then fails after switching to another engine with an unavailable model error.
- Kiro receives an OMP-only model such as `openai-codex/gpt-5.6-luna` after `/engine kiro` or `/reset`.
- Creating or reopening a thread repeatedly fails until the parent channel runs `/model <kiro-model>` again.

First checks:

- `channel/engine.go`: `/engine` must clear persisted `Session.Model` when changing engine.
- `channel/manager.go`: channel/thread startup must validate or clear persisted startup models before `acp.StartAgent`.
- `channel/manager.go`: thread spawn model precedence is default → parent channel → thread override → explicit command override.
- `acp/dialect.go`: Kiro passes model at launch; OMP selects model via `session/set_config_option`.

Regression expectation:

- Test channel and thread engine switches clear persisted model and session reuse state.
- Test startup clears an unavailable persisted Kiro model and starts without `--model`.
- Test OMP startup retries without a configured model when `session/set_config_option` reports the model unavailable.
- Test thread engine overrides do not inherit or clear the parent channel's model from another engine.
- Test explicit `/model` startup failures remain user-visible instead of falling back silently.

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

### A2A Receiver Consent Versus Sender Delegation

Symptoms:

- `bot_a2a_trust_peer` on the receiver says a peer may delegate into the channel, but `bot_a2a_delegate` on the sender returns `unauthorized_target`.
- `bot_a2a_peers` shows the receiver runtime online and inbound-allowed, but `peerPolicy.outboundDelegateTargets` is empty.
- Direct human requests to delegate feel blocked by an extra sender-side setup step, while cron or remote A2A delegation still needs durable policy.

First checks:

- Receiver channel policy `accept_from_runtimes`: admission consent only.
- Sender channel policy `delegate_targets`: durable outbound authorization for scheduled, cron, remote A2A, bot-handoff, or recurring delegation.
- Bot-tools target state `source` and `remote_a2a`: direct `message`, `thread`, and slash `/a2a ask` may use an ephemeral one-shot runtime target; `cron`, `bot_handoff`, and `remote_a2a` must fail closed without persistent `delegate_targets`.
- Audit metadata `authorization_mode` and `persistent_delegate_target` on `a2a_task_send_requested`.

Regression expectation:

- Cover direct human one-shot delegation without mutating `delegate_targets`, plus cron/remote/bot-handoff rejection without persistent outbound policy.

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

### MCP Server Binary Installed As Main Bot

Symptoms:

- `mcp-discord` or another MCP server process starts a full `kiro-discord-bot` runtime instead of a stdio MCP server.
- Process tree shows recursion such as `kiro-discord-bot -> kiro-cli -> kiro-cli-chat -> mcp-discord-server -> kiro-cli`.
- The same Discord `message_create` raw event appears multiple times with identical payload hashes, and one user message is enqueued/answered multiple times.
- Memory climbs quickly and OOM kills `kiro-cli-chat` or the service after an agent uses the MCP server.

First checks:

- Compare release artifact binary hashes on the host: the main bot, `mcp-discord`, and `mcp-media` must be distinct; compatibility paths such as `mcp-discord-server` and `mcp-media-server` must match their corresponding MCP binary, never the main bot.
- Read the active MCP config (`KIRO_MCP_CONFIG`) and verify each command path points to the matching MCP binary.
- Inspect process ancestry after startup and after a lightweight MCP initialize smoke; MCP servers must not spawn a new full bot or gateway listener.
- Check `discord_events` and `bot_audit_events` for duplicate counts grouped by `message_id` after restart.

Regression expectation:

- Deployment handoff must include per-binary checksums for the main bot and MCP binaries, plus process-tree evidence that MCP server commands do not recurse into another bot runtime.

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
