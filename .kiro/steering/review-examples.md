---
name: review-examples
description: Concrete examples of low-quality vs high-quality review, reuse, verification, and handoff behavior for this repository.
---

# Review Examples

Use this file with `360-review-handoff.md` and `decision-failure-patterns.md`. The goal is not to memorize fixed answers. The goal is to recognize the difference between a surface pass and a review that can be trusted by the next engineer, operator, or agent.

## 60 vs 90+ Review

| Topic | 60-point answer | 90+ answer |
|-------|-----------------|------------|
| Current state | "Looks fine" after reading the diff | Starts with `git status`, changed files, recent commits, runtime path, and exact untracked files |
| Intent | Repeats what changed | Explains the user problem, why this approach matches the architecture, and what behavior must not regress |
| Reuse | Accepts working code | Checks whether shared helpers already own the concern and flags duplicate security, formatting, path, cron, MCP, or audit logic |
| Risk | Mentions obvious bugs | Names failure modes: policy bypass, wrong Discord target, cwd pollution, secret leak, stuck worker, release artifact mismatch |
| Verification | Says tests pass | Lists exact commands or runtime signals, what each proves, and what remains unverified |
| Docs | Updates one README line | Checks README, `.env.example`, steering, release docs, i18n, command names, and log reason strings when behavior changes |
| Decisions | Leaves tradeoffs in chat | Records rejected shortcuts, non-goals, future triggers, and regression expectations when the direction matters |
| Handoff | Gives a summary | Gives findings first, evidence, go/no-go, residual risk, and next action |

## Example: Discord Safe Egress

Bad implementation:

- Adds a local `splitEvery2000Chars` near `bot_send_message`.
- Sends chunks with raw `ChannelMessageSend` calls.
- Fixes only the reported long message but skips `AllowedMentions`, secret redaction, delivery audit, code fence repair, and MCP policy.
- Tests the helper directly without proving the real tool path uses it.

High-quality implementation:

- Reuses `internal/discordfmt.Split`, `internal/discordfmt.WithPartPrefix`, or an existing helper already built on them.
- Keeps the established safe egress path, redaction, mention suppression, policy checks, and audit events.
- Tests long text, UTF-8, code fences, denied policy, Discord delivery errors, and the runtime tool path.
- Updates `discord-mcp.md` or `project.md` if the rule or supported behavior changed.

Review finding to write:

> High: this new Discord send path bypasses `internal/discordfmt` and safe egress, so long tool output can fail at Discord's 2000-character limit and skip redaction/audit. Route it through the existing helper and add a runtime-path test for a long MCP response.

## Example: Cron Thread Target

Bad implementation:

- Stores a Discord thread ID as the cron owner channel.
- Lets `/cron-list` look only at the parent channel.
- Sends manual-trigger output to the channel because the delivery target was not persisted.

High-quality implementation:

- Treats the parent channel as the ownership and permission scope.
- Stores the thread ID only as the delivery target when the cron was created in a thread.
- Reuses `internal/cronpolicy`, `heartbeat/cron.go`, and `bot/cron_adapter.go` ownership logic.
- Verifies `/cron-list`, manual trigger, scheduled execution, `history.jsonl`, and thread delivery.

Review finding to write:

> High: the cron job mixes owner channel and delivery thread. That makes `/cron-list` and execution target disagree. Normalize ownership through cron policy and persist the thread as delivery metadata only.

## Example: CWD And Kiro Settings

Bad implementation:

- Joins user-provided paths in a handler.
- Writes `.kiro/settings/mcp.json` directly from feature code.
- Claims the path is safe because it matched a previous session.

High-quality implementation:

- Routes cwd acceptance through `channel.Manager.ValidateCWD`.
- Uses `internal/paths` for path containment and `internal/kirosettings` for Kiro CLI settings isolation.
- Confirms current Kiro CLI behavior with the installed version or official docs when behavior may have changed.
- Verifies `/doctor`, startup logs, and that no workspace or global settings were polluted.

Review finding to write:

> Critical: cwd validation is duplicated in the handler and can diverge from Manager policy. Move acceptance back through `ValidateCWD`, then verify `/cwd`, cron temp agents, and `/doctor` all report the same effective project root.

## Example: Multi-Bot And Mention Scope

Bad implementation:

- Assumes every bot in the guild is visible in every channel.
- Changes mention filtering from one log sample.
- Adds peer routing without checking channel/thread permissions or bot roles.

High-quality implementation:

- Reads live startup peer discovery logs and the exact channel/thread member visibility.
- Checks `BOT_PEERS`, `user_peers`, `role_only_peers`, `requiresHumanMention`, and listen-mode docs together.
- Verifies channel mode, thread mode, direct mention, human mention, bot-only mention, and mixed multi-bot messages.
- Documents current limitations instead of implying distributed bot coordination exists.

Review finding to write:

> Medium: this change assumes guild-level peer discovery is enough, but thread-scoped permissions can still hide a peer. Add coverage for channel and thread visibility and update `docs/listen-mode-matrix.md` with the exact supported behavior.

Decision record to update:

> Current architecture non-goal: independent bot deployments do not provide reliable distributed coordination through Discord mentions. If multi-bot orchestration becomes a product goal, it needs an explicit server-side architecture with owned routing, shared state, audit, and identity boundaries.

## Example: Release And Deploy

Bad implementation:

- Builds locally and copies the binary to every host.
- Says deployment is done because the service is active.
- Does not confirm the release artifact, architecture, version banner, or runtime logs.

High-quality implementation:

- Uses the GitHub release artifact requested by the user.
- Confirms artifact version, checksum or release ID, target architecture, service path, restart result, startup banner, and `/doctor` or equivalent runtime signal.
- Reports each host separately: local, remote systemd, remote launchd, Docker, or Incus VM.
- Calls out any target that could not be verified.

Review finding to write:

> High: deployment verification only checked service liveness. Confirm the running binary version from startup logs or `/doctor`; otherwise the host may still be running the old release.

## Example: i18n Format String With Internal Error

Bad implementation:

- Uses `L.Getf("egress.blocked", err.Error())` directly.
- zh-TW users see `⚠️ 安全傳送被阻擋：file type is not safely redactable as text` — Chinese prefix with English error body.
- No attempt to translate known error reasons.
- Tests only prove the format string works, not that the final user-visible output is linguistically consistent.

High-quality implementation:

- Builds a reason map matching known error substrings to locale keys (`egress.reason.*`).
- Known reasons render fully translated: `⚠️ 安全傳送被阻擋：檔案類型無法安全地以文字形式脫敏`.
- Unknown errors fallback to redacted original (acceptable because they are rare runtime failures).
- Tests cover all known reasons, a dynamic-value case (size limit with bytes), and the unknown fallback path.

Review finding to write:

> Medium: the i18n format string embeds a raw English error in a translated template, causing language mixing for zh-TW users. Map known error reasons to locale keys and fallback only for genuinely unknown failures.

## Suspicious Review Phrases

Treat these phrases as a cue to gather more evidence:

- "It should be fine."
- "Looks like it works."
- "Probably the same path."
- "Only docs changed."
- "I tested it" without a command, log, screenshot, or Discord interaction.
- "This helper is simple, so I wrote another one."
- "Direct Discord API is faster."
- "No need to update docs."
- "The old run showed this already."

Replace them with concrete evidence:

- exact file/function names
- exact commands and exit status
- exact log lines or runtime signals
- exact docs updated or intentionally skipped
- exact unverified items

## Commit Readiness Examples

Not ready:

> Go tests pass, but the change adds a new Discord send path that does not reuse `internal/discordfmt`, docs still describe the old MCP behavior, and no test proves the runtime tool path splits long output. Go/no-go: no-go.

Ready:

> The runtime path now uses the shared Discord formatting helper, long MCP output is covered by a package test and a tool-path regression test, docs and steering were updated, and `go test -count=1 ./bot ./cmd/mcp-discord ./internal/...` plus `git diff --check` passed. Go/no-go: ready to commit.

## Final Response Example

For a strict review, keep the final answer evidence-led:

```text
Findings:
- High: [file:line] direct Discord send bypasses safe egress and can skip redaction/audit.
- Medium: [file:line] docs still describe old cron target behavior.

Verification:
- git status --short --branch
- go test -count=1 ./bot ./heartbeat ./internal/...
- git diff --check

Go/no-go:
- No-go until the safe egress path is restored and docs are aligned.
```

For a fix, keep it outcome-led:

```text
Changed:
- Routed cron delivery metadata through the shared cron policy path.
- Reused the existing Discord long-message helper instead of adding local split logic.

Verified:
- go test -count=1 ./bot ./heartbeat ./internal/cronpolicy ./internal/discordfmt
- git diff --check

Ready:
- Ready to commit. Runtime deployment is not verified in this local review.
```
