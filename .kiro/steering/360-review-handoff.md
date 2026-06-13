---
name: 360-review-handoff
description: Use when reviewing, debugging, hardening, or handing off work in this repository. This file defines the evidence-first, end-to-end quality loop expected before saying a change is ready.
---

# 360 Review Handoff

This project expects evidence-first engineering. Do not answer from memory when the repo, runtime, logs, or official documentation can be checked. A good handoff explains the current system, the intended behavior, the actual behavior, the fix, the verification, and the remaining risk.

When a rule feels abstract, read `review-examples.md` before acting. It shows concrete bad and good patterns for review quality, helper reuse, runtime verification, and commit readiness.

When a task touches architecture direction, a recurring bug, or an intentionally unsupported behavior, read `decision-failure-patterns.md`. Do not leave major decisions or failure patterns only in chat history.

## Operating Mindset

- At the start of each user request, briefly state what you will inspect or do next. Keep it factual and scoped; do not overpromise the outcome before evidence is gathered.
- Start from the real current state: `git status`, diffs, tests, docs, runtime metadata, and logs.
- Separate facts from inference. If a claim depends on source code, cite the file/function. If it depends on runtime, cite the command/log signal.
- Do not guess from memory. Prior memory can suggest where to look, but current code, current runtime state, current logs, or official docs must be checked before making a current-state claim.
- If evidence is unavailable, say what is unverified and why. Do not turn an assumption into a conclusion just to make the handoff look complete.
- Preserve user work. Never revert unrelated dirty files. If dirty files are in scope, inspect them and work with them.
- Prefer the repo's established paths, helpers, and policy layers over new local shortcuts.
- Treat Discord output, MCP tools, cron, cwd, and agent process control as security-sensitive surfaces.
- Keep docs, tests, and behavior aligned in the same change when the behavior is user-visible.
- While working, provide concise progress updates when moving between investigation, implementation, verification, and deployment. Each update should say what was learned or what is being checked next.
- Before finalizing, give an explicit go/no-go quality judgment and list what was actually verified.

## First Pass: Map The Change

Run a read-only inventory before editing:

```bash
git status --short --branch
git diff --stat
git diff --name-only
git log --oneline -6
```

Then classify the change:

| Area | Primary files | Extra checks |
|------|---------------|--------------|
| Discord routing / commands | `bot/handler.go`, `bot/commands.go`, `bot/peers.go` | Slash registration, bang dispatch, i18n, audit delivery |
| Agent lifecycle / worker | `channel/manager.go`, `channel/worker.go`, `acp/` | Session continuity, timeout/cancel paths, stderr/log evidence |
| Cron / reminders | `heartbeat/cron.go`, `bot/handler_cron.go`, `internal/cronpolicy/` | Timezone, owner channel, thread target, run history |
| MCP policy / tools | `channel/mcp_policy.go`, `mcpproxy/`, `internal/botmcp/`, `cmd/mcp-discord/` | Allowlist, read-only/write/destructive guards, audit, redaction |
| Discord egress / formatting | `bot/safe_egress.go`, `internal/discordfmt/`, `internal/botegress/` | 2000-char split, code fence handling, AllowedMentions, redaction |
| Runtime config / env | `config.go`, `main.go`, `channel/doctor_env.go` | README, `.env.example`, locale descriptions, `/doctor` |
| Documentation-only | `README.md`, `docs/`, `.kiro/steering/` | Must match current code names and command behavior |
| Release/deploy | `docs/release.md`, `.github/workflows/`, scripts | Preflight, tag/release state, host-specific service metadata |

## Review Questions

Ask these before accepting any change:

1. What user problem or operational failure is this change trying to solve?
2. Which runtime path actually exercises the modified code?
3. Does the change preserve existing security boundaries, audit records, redaction, and Discord delivery wrappers?
4. Are all user-facing strings localized in both `locale/lang/en.json` and `locale/lang/zh-TW.json`?
5. Does the documentation use the exact command names and log reasons that code emits today?
6. Are edge cases covered: empty input, long output, Unicode, Discord 2000-char limit, missing config, denied policy, timeout, canceled context, and repeated execution?
7. If the change affects deployed behavior, how would `/doctor`, systemd/launchd logs, or Discord UX prove it works?
8. Did the implementer reuse the established package for this concern, or did they introduce duplicate logic that should be moved into an existing helper?
9. What would make this change unsafe to release, and has that failure mode been tested or explicitly ruled out?
10. Are there any generated files, local runtime binaries, backups, or secrets accidentally included in the worktree?
11. Is this a recurring failure or architecture decision that should update `decision-failure-patterns.md`?

## Severity Standard

Lead reviews with findings. Use severity based on user impact:

| Severity | Meaning |
|----------|---------|
| Critical | Data leak, secret exposure, destructive action, broken release/deploy, or bot loop risk |
| High | User-visible behavior is wrong, docs instruct wrong commands, policy can be bypassed, or cron/thread target is wrong |
| Medium | Missing regression test, confusing diagnostics, fragile ordering, partial i18n/doc drift |
| Low | Style, naming, comments, or small maintainability issue |

If there are no findings, say so plainly and still list residual test or runtime gaps.

## Implementation Rules

- Handler code routes and validates; business state belongs in `channel.Manager` or the relevant service package.
- Agent process management stays inside `acp/` and manager/worker boundaries.
- CWD validation must flow through Manager policy. Do not build ad hoc cwd acceptance in handlers or cron.
- Discord writes must reuse existing helpers and policy layers. Do not call raw Discord APIs to bypass MCP policy, safe egress, redaction, or AllowedMentions suppression.
- Long Discord text must use existing formatting helpers built on `internal/discordfmt.Split` and `internal/discordfmt.WithPartPrefix`.
- New environment variables require the complete path: config load, runtime config struct, `/doctor`, i18n descriptions, README, and `.env.example`.
- New commands require slash registration, interaction dispatch, bang dispatch when applicable, permissions, i18n, docs, and tests.
- New MCP tools require read-only/write/destructive classification, policy exposure, audit/redaction behavior, and tests.

## Reuse Map

Before writing new code, look for the established module that already owns the concern. Prefer extending these modules with tests over adding one-off logic in feature code.

| Concern | Reuse / owner | Do not duplicate |
|---------|---------------|------------------|
| Discord text splitting and Markdown repair | `internal/discordfmt` plus existing `bot` / `channel` send helpers | Manual 2000-char slicing, code fence repair, part prefixes |
| Secret and path redaction | `internal/secrets`, `internal/botegress`, safe egress wrappers | Local regex redactors in handlers or MCP tools |
| Discord write policy and allowlists | `cmd/mcp-discord`, `mcpproxy`, `channel/mcp_policy.go` | Raw REST calls that skip read-only/write/destructive guards |
| Safe Discord egress from agents | `internal/botmcp`, `internal/botegress`, `bot/safe_egress.go` | Direct message/file sending from agent-facing tool handlers |
| CWD and project steering paths | `channel.Manager.ValidateCWD`, manager steering helpers, `internal/paths` | Handler-side path joins or trusting user-provided cwd |
| Kiro CLI settings isolation | `internal/kirosettings`, manager `agentOptsForTarget` | Writing `.kiro/settings` or `mcp.json` ad hoc |
| Cron ownership and permission | `heartbeat/cron.go`, `internal/cronpolicy`, `bot/cron_adapter.go` | Treating Discord thread IDs as owning channel IDs |
| Channel/thread metadata | `internal/channelmeta`, bot metadata recorder | Reading message history just to infer names |
| Text truncation / duration formatting | `internal/textutil` | Local truncation/duration helpers |
| Audit and semantic events | `audit/`, manager/bot record helpers | Silent side effects without audit records |
| Locale strings | `locale` package and `locale/lang/*.json` parity | Hard-coded user-facing English in Go paths |
| ACP capability/session behavior | `acp/agent.go`, manager worker lifecycle | Calling kiro-cli directly outside the agent boundary |

If no helper exists, create or extend the smallest shared package that matches the ownership boundary, then update tests and steering docs. A feature implementation should not carry private copies of reusable formatting, redaction, policy, path, cron, or audit rules.

## Duplication Review

During review, actively search for reinvention:

- New `strings.Split`, rune counting, or prefix logic near Discord sends.
- New regex redaction, path masking, or direct access to secret-bearing env vars.
- Direct `ChannelMessageSend*` calls outside established wrappers without an explicit reason.
- New path normalization, cwd checks, or `.kiro/steering` path construction outside Manager/path helpers.
- New cron channel/thread ownership rules outside cron policy code.
- New JSON persistence formats that overlap existing stores.
- Hard-coded user-facing messages that should be locale keys.
- Tests that only prove the new private helper works instead of proving the shared helper is used on the runtime path.

Treat avoidable duplication in security, formatting, path, cron, MCP, or audit logic as at least a Medium finding; raise it to High when it can cause policy bypass, Discord delivery failure, target confusion, or data leakage.

## No-Go Triggers

Stop and fix before calling work ready when any of these are true:

- A current-state claim is based only on memory or prior rollout notes.
- A Discord write path bypasses policy, redaction, AllowedMentions suppression, safe egress, or audit.
- A cwd, project steering, cron owner, or thread target is accepted without the established manager/policy path.
- A user-facing command, log reason, env var, or MCP tool behavior changed without docs and i18n review.
- Tests pass only because they exercise a private helper while the real runtime path remains untested.
- A release/deploy claim lacks artifact identity, service metadata, startup log, or `/doctor`/runtime evidence.
- Secrets, runtime data, local binaries, backup files, or generated artifacts are staged or left as unreviewed noise.
- A failing validation is waved away without a concrete explanation of why it is irrelevant.

## Documentation Alignment

When code changes behavior, check these documents for drift:

- `.kiro/steering/project.md`: architecture contract, design rules, required verification.
- `.kiro/steering/discord-mcp.md`: Discord reply behavior, MCP tool usage, egress/security rules.
- `.kiro/steering/360-review-handoff.md`: quality loop and review process.
- `.kiro/steering/review-examples.md`: concrete examples for rigorous review, reuse discipline, and handoff quality.
- `.kiro/steering/decision-failure-patterns.md`: decision records, non-goals, recurring failures, and regression expectations.
- `README.md`: install, env vars, commands, runtime behavior, feature notes.
- `.env.example`: every public runtime env var.
- `INSTALL_MCP.md`: MCP install and tool safety behavior.
- `docs/release.md`: release, deployment, rollback, and `/doctor` verification.
- `docs/listen-mode-matrix.md`: pause/back/thread/multi-bot behavior and log reasons.

Documentation quality bar:

- Use exact command names (`/thread`, not invented aliases).
- Use exact log reason strings from code.
- Explain ownership and target IDs for cron/thread/channel behavior.
- Mark current limitations clearly instead of implying unsupported behavior exists.
- Record rejected shortcuts, non-goals, and future trigger conditions for architecture decisions.
- Keep English and Traditional Chinese locale keys aligned.

## Steering Evolution Rule

Treat steering files as living project control documents, not static notes. When the project gains a new package, shared helper, architecture layer, runtime mode, deployment target, MCP tool category, recurring failure pattern, or ownership boundary, review whether the steering set still teaches the next engineer the current truth.

Update the right file:

- `project.md`: architecture boundaries, design principles, build/run contract, completeness checklist.
- `360-review-handoff.md`: review process, reuse map, no-go triggers, verification ladder, handoff evidence.
- `discord-mcp.md`: Discord agent behavior, MCP tool usage, egress/security boundaries.
- `review-examples.md`: concrete bad/good examples and commit-readiness examples.
- `decision-failure-patterns.md`: decisions, non-goals, recurring failures, future triggers, regression expectations.

If two steering files describe the same concern differently, stop and choose the source of truth instead of letting drift remain. Before release, do a quick steering drift scan for any user-visible behavior, new helper, new runtime path, new deployment target, or new known limitation introduced since the last release.

## Verification Ladder

Choose the narrowest sufficient check first, then broaden when risk or shared behavior increases.

| Change type | Minimum verification |
|-------------|----------------------|
| Go compile path | `go build ./...` |
| Package behavior | `go test -count=1 ./<package>` |
| Cross-package behavior | `go test -count=1 ./...` |
| Logic, command routing, MCP, worker, cron | `go test -count=1 ./bot ./channel ./heartbeat ./mcpproxy ./internal/...` as applicable |
| i18n | locale key parity test or `jq` key diff |
| Formatting / whitespace | `git diff --check` |
| Release readiness | `scripts/release-preflight.sh` |
| ACP-sensitive change | `RUN_ACP_SMOKE=1 KIRO_CLI=$(which kiro-cli) scripts/release-preflight.sh` |
| Runtime/deploy | service metadata, restart, startup version log, `/doctor`, relevant journal/launchd logs |

Do not claim runtime verification from tests alone. Say exactly what was and was not verified.

## Evidence Ledger

For any non-trivial review, fix, release, or deploy, keep a short evidence ledger in the final response or handoff:

- **Intent**: the problem being solved.
- **Changed surface**: packages, runtime paths, commands, MCP tools, docs, or env vars touched.
- **Risk areas checked**: security, redaction, audit, cwd policy, cron/thread target, i18n, docs, release/deploy.
- **Verification**: exact commands or runtime signals used.
- **Not verified**: anything intentionally skipped, unavailable, or needing user/runtime confirmation.
- **Decision/failure record**: whether `decision-failure-patterns.md` was updated or intentionally not needed.
- **Commit readiness**: explicit go/no-go.

This is more important than a long narrative. A future reviewer should be able to see why the conclusion is justified without replaying the whole session.

## Runtime Investigation Pattern

For live issues:

1. Identify the runtime owner: local launchd, remote systemd, Docker, or Incus VM.
2. Inspect service metadata before guessing paths.
3. Read the startup banner, bot identity, peer discovery line, preflight line, and relevant error logs.
4. Compare binary/source version to the expected tag or commit.
5. Check runtime data files only when needed, and avoid exposing secrets or raw private message content.
6. Reproduce with the smallest safe command or Discord interaction.
7. Fix code/config/docs, then verify the same signal that demonstrated the bug.

Useful signals:

- Startup: `kiro-discord-bot <version> starting`
- ACP: `[preflight] kiro-cli <version>, protocol=1, check passed`
- Peers: `[peers] discovery complete ... user_peers=<n> role_only_peers=<n>`
- Safe egress: pending action delivery count and sanitized error messages
- Cron: run history under `DATA_DIR/cron/<jobID>/history.jsonl` plus dedicated thread delivery

## External Knowledge

Use official or primary sources when behavior depends on a moving external system:

- Kiro CLI / ACP behavior, MCP protocol details, Discord API limits, GitHub Actions, GoReleaser, and OS service manager behavior can drift.
- Prefer current local binary inspection and official docs over old memory.
- Record the exact version checked, such as `kiro-cli --version`, Go version, release tag, or action run ID.
- If network/docs access is unavailable, mark the claim as unverified instead of guessing.

## Final Response Template

For a review:

1. Findings first, ordered by severity, with file/line references.
2. Open questions or assumptions.
3. Verification performed.
4. Go/no-go judgment.

For a fix:

1. What changed and why.
2. Verification performed.
3. Remaining risk or follow-up, if any.
4. State whether it is ready to commit.

For release/deploy:

1. Version/tag/artifact used.
2. Each target runtime updated.
3. Per-target verification signal.
4. Any skipped or impossible checks.

## Ready-To-Commit Gate

A change is ready only when all are true:

- The implementation solves the stated problem without bypassing established boundaries.
- Tests cover the risky path or there is a clear reason no test is needed.
- User-facing docs and i18n are aligned.
- `git diff --check` passes.
- Required build/tests pass.
- No unrelated generated files, backups, secrets, or runtime artifacts are left in the worktree.
- The final review has no unresolved High or Critical findings.
