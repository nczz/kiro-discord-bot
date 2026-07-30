# A2A NATS Implementation Progress

This file is the durable handoff contract for implementing the A2A-inspired NATS custom binding. It exists so any resumed agent can continue from repository state, not chat memory.

## Authoritative inputs

Read these files before editing implementation code:

1. `docs/a2a-nats-integration-spec.md` — source-of-truth behavior and security decisions.
2. `docs/a2a-nats-implementation-guide.md` — exact phase order, touched files, validation commands, rollback boundaries, and done criteria.
3. This file — current execution state, phase evidence, and resume rules.

If these files conflict, stop and report the conflict. Do not invent behavior.

## Non-negotiable execution rules

1. Implement exactly one phase per execution cycle unless the user explicitly approves a broader batch in the same conversation.
2. Do not start phase N+1 until phase N has passing validation evidence and this ledger records the commit.
3. Treat every failing phase validation as a blocker. Fix the issue at the source and rerun that phase validation before continuing.
4. Keep A2A disabled by default until rollout explicitly enables it.
5. `NATS_URL == ""` must remain a no-op path for existing bot behavior.
6. Do not modify runtime `.env` files, deployment hosts, Docker volumes, `DATA_DIR`, or live service state while implementing code phases.
7. Preserve existing Discord safety boundaries: `channel.Manager`, queue/runtime policy, MCP policy proxy, safe egress, audit, AllowedMentions, secret redaction, and channel permissions.
8. Do not implement deferred features early: pool dispatch, official HTTP A2A endpoint, SSE streaming, HTTP push notification config, or public A2A gateway adapter.
9. Every phase must end with: targeted validation output, changed files, commit hash, rollback boundary, and next phase.
10. Do not rely on chat history. Resume from this file, the guide, the spec, git history, and validation results.

## Resume protocol for every agent

At the start of each continuation:

1. Read the three authoritative inputs above.
2. Check the worktree status.
3. Identify the single row marked `next` or `in_progress` in the phase ledger.
4. Verify the previous completed phase has a commit hash and validation evidence.
5. If the previous phase lacks evidence, stop and report the missing item.
6. Execute only the current phase from the implementation guide.
7. Run the exact phase validation commands plus any targeted regression required by touched seams.
8. Update this file before committing:
   - phase status
   - commit hash or `pending commit` before the commit exists
   - validation commands and outputs
   - notes and rollback boundary
9. Commit code and this ledger together.
10. Final response must include the commit hash, validation evidence, and the next phase.

## Current state

- Program state: Phase 0 readiness guard and Phase 1 foundation package/config completed in commit `0d720d2`; Phase 2 NATS node and JetStream topology completed with commit pending.
- Current phase: Phase 2 validation passed; Phase 3 durable stores is next after commit.
- First execution target: completed Phase 0 guide validation fix and Phase 1 foundation only.
- Known pre-implementation issue: resolved by splitting the self-referential forbidden-string checks in `docs/a2a-nats-implementation-guide.md`.

## Phase ledger

| Phase | Name | Status | Commit | Required validation evidence | Notes |
|---:|---|---|---|---|---|
| 0 | Readiness guard | done | `0d720d2` | `python3 - <<'PY' ...` printed `a2a-guide-readiness-ok`; Section 7 guide-only verification printed `a2a-implementation-guide-ready` | Fixed self-referential Phase 0 check without changing A2A behavior. |
| 1 | Foundation package and config | done | `0d720d2` | `go test ./a2a -run 'Test(Subject|Envelope|TaskState|ErrorCode|NatsMsgID)'` passed; `go test ./channel -run 'TestDoctor.*A2A|TestDoctorRuntimeOverviewDoesNotLeakRawEnvironmentValues'` passed; targeted config parse tests passed. | No NATS connection. A2A stays disabled when `NATS_URL` is unset. |
| 2 | NATS node and JetStream topology | done | pending commit | `go test ./a2a -run 'Test(NodeDisabled|ConnectDrain|EnsureStreams|NoPoolSubject|DuplicateNatsMsgID)'` passed; `go test . -run 'TestConfig.*A2A'` passed. | Streams and consumers only; no remote task execution. |
| 3 | Durable stores | next | | `go test ./a2a -run TestTaskStore`; `go test ./a2a -run TestPolicyStore`; `go test ./a2a -run TestPeerStore` | Durable TaskStore, event store, policy store, peer store. |
| 4 | Peer card and discovery | pending | | `go test ./a2a -run TestAgentCard`; `go test ./a2a -run TestPeerStore` | Public card sanitizer and manager-visible trust summary. |
| 5 | Channel ingress and executor | pending | | `go test ./channel -run TestManagerA2A`; `go test ./channel -run TestWorkerA2A`; `go test ./channel -run TestA2A` | Ingress only through `channel.Manager` and worker runtime. |
| 6 | Transport integration | pending | | `go test ./a2a -run TestTransport`; `go test ./a2a -run TestA2AIntegration` | Two-node embedded JetStream closed loop. |
| 7 | Bot-tools and Discord UX | pending | | `go test ./internal/botmcp -run TestA2A`; `go test ./bot -run TestA2A`; `go test ./locale ./bot -run 'Test.*A2A.*Locale'` | Bot-tools, slash fallback, buttons/modals, requester/manager checks. |
| 8 | Artifacts, delivery, audit | pending | | `go test ./a2a -run 'TestObject(Store|Digest|Retention|MediaPolicy)'`; `go test ./bot ./internal/botegress ./channel ./audit -run 'TestA2A(Egress|Artifact|ProxyDelivery|MirrorTranscript|CoPresent|TransparentResult|AuditMetadata)'` | Safe egress, Object Store references, transcript modes, audit metadata. |
| 9 | Production hardening and rollout | pending | | `go test ./a2a ./channel ./internal/botmcp ./bot ./audit ./locale -run 'Test.*A2A|TestDoctor.*A2A'`; rollout guide check prints `a2a-rollout-guide-ok` | Env docs, ACL templates, smokes, rollback guide. |

## Evidence log

Append one subsection per completed phase.

### Contract framework setup

- Status: done.
- Scope: created durable progress ledger and resume protocol only.
- A2A implementation code changed: no.
- Runtime settings touched: no.
- Deployment hosts touched: no.


### Phase 0 — Readiness guard

- Status: done; commit `0d720d2`.
- Changed files: `docs/a2a-nats-implementation-guide.md`.
- Validation:
  - `python3 - <<'PY' ...` printed `a2a-guide-readiness-ok`.
  - `python3 - <<'PY' ...` printed `a2a-implementation-guide-ready`.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Rollback boundary: revert only the Phase 0 guide snippet change if the guide validation contract changes.
- Next phase: Phase 1 foundation package and config.

### Phase 1 — Foundation package and config

- Status: done; commit `0d720d2`.
- Changed files: `.env.example`, `a2a/errors.go`, `a2a/envelope.go`, `a2a/executor.go`, `a2a/idempotency.go`, `a2a/store.go`, `a2a/subject.go`, `a2a/types.go`, `a2a/types_test.go`, `channel/doctor_env.go`, `channel/doctor_env_test.go`, `channel/manager.go`, `config.go`, `config_test.go`, `locale/lang/en.json`, `locale/lang/zh-TW.json`, `main.go`.
- Validation:
  - `go test ./a2a -run 'Test(Subject|Envelope|TaskState|ErrorCode|NatsMsgID)'` passed.
  - `go test ./channel -run 'TestDoctor.*A2A|TestDoctorRuntimeOverviewDoesNotLeakRawEnvironmentValues'` passed.
  - `go test . -run 'TestLoadConfigParsesA2A'` passed.
  - LSP workspace diagnostics: no issues found.
- Done criteria evidence:
  - A2A disabled path is a config-only no-op while `NATS_URL` is unset.
  - A2A envs parse through `loadConfig` into `a2a.Config`.
  - `a2a.Config.ValidateStartup` rejects token-only production mode when `A2A_PRODUCTION_SECURITY=true`.
  - `/doctor` shows A2A env presence and safe effective values without leaking token, credentials file contents, or TLS material.
  - Subject, envelope, task-state, error-code, and idempotency tests include positive and negative cases.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Rollback boundary: revert the `a2a` package plus config/doctor/env documentation wiring added in this phase; no runtime schema or NATS state exists yet.
- Next phase: Phase 2 NATS node and JetStream topology.

### Phase 2 — NATS node and JetStream topology

- Status: done; commit pending.
- Changed files: `a2a/nats_test.go`, `a2a/node.go`, `a2a/node_test.go`, `a2a/streams.go`, `a2a/types.go`, `bot/bot.go`, `channel/manager.go`, `config_test.go`, `docs/a2a-nats-implementation-progress.md`, `go.mod`, `go.sum`, `main.go`.
- Validation:
  - `go test ./a2a -run 'Test(NodeDisabled|ConnectDrain|EnsureStreams|NoPoolSubject|DuplicateNatsMsgID)'` passed.
  - `go test . -run 'TestConfig.*A2A'` passed.
  - LSP workspace diagnostics: no issues found.
  - `grep 'a2a\\.v1\\.pool' a2a;docs` found the literal only in rejection tests and documentation.
- Done criteria evidence:
  - Disabled `a2a.Node` opens no NATS connection or JetStream handle.
  - Embedded NATS tests prove connect/drain, stream creation, durable consumer creation, and duplicate `Nats-Msg-Id` dedupe.
  - Stream subjects are exactly `a2a.v1.task.>`, `a2a.v1.control.>`, and `a2a.v1.event.>`.
  - Local durable consumer filters target only `A2A_AGENT_ID` and do not include pool filters.
  - Startup constructs a disabled/enabled node and skips all NATS work when disabled.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Rollback boundary: revert `a2a/node.go`, `a2a/streams.go`, `a2a/nats_test.go`, `a2a/node_test.go`, `go.mod`, `go.sum`, and startup dependency wiring in `main.go`, `bot/bot.go`, and `channel/manager.go`; Phase 1 validators remain usable.
- Next phase: Phase 3 durable stores.

## Master goal prompt

Use this prompt when starting or resuming the full implementation program:

```text
Resume A2A implementation from repository state, not chat memory.

Authoritative files:
1. docs/a2a-nats-integration-spec.md
2. docs/a2a-nats-implementation-guide.md
3. docs/a2a-nats-implementation-progress.md

Rules:
- Read all three files before editing.
- Check git status before editing.
- Determine the single next phase from docs/a2a-nats-implementation-progress.md.
- Verify the previous completed phase has commit hash and validation evidence.
- Implement exactly one phase.
- Do not start the next phase.
- Preserve source spec decisions exactly.
- Keep A2A disabled by default until rollout.
- Do not touch runtime .env files, DATA_DIR, Docker volumes, deployment hosts, or live services.
- Do not implement pool dispatch, public HTTP A2A endpoint, SSE streaming, HTTP push notification config, or gateway adapter.
- Run the current phase validation commands and targeted regressions required by touched seams.
- Update docs/a2a-nats-implementation-progress.md with status, validation evidence, notes, rollback boundary, and next phase.
- Commit the phase and ledger update together.
- Final response must include changed files, commit hash, validation evidence, rollback boundary, and next phase.
```

## First execution prompt

Use this prompt for the first implementation execution:

```text
Start A2A implementation PR1 only.

Read:
1. docs/a2a-nats-integration-spec.md
2. docs/a2a-nats-implementation-guide.md
3. docs/a2a-nats-implementation-progress.md

Implement exactly:
- Phase 0 readiness guard fix.
- Phase 1 foundation package and config.

Do not implement Phase 2 or any NATS connection code.
Do not touch runtime .env files, DATA_DIR, deployment hosts, or live services.
Do not add MCP tools or Discord visible output.

Required Phase 0 validation:
- Phase 0 guide snippet prints `a2a-guide-readiness-ok`.
- Section 7 guide-only verification prints `a2a-implementation-guide-ready`.

Required Phase 1 validation:
- `go test ./a2a -run 'Test(Subject|Envelope|TaskState|ErrorCode|NatsMsgID)'`
- `go test ./channel -run 'TestDoctor.*A2A|TestDoctorRuntimeOverviewDoesNotLeakRawEnvironmentValues'`

Done criteria:
- A2A disabled path is behaviorally no-op.
- All A2A envs parse deterministically.
- Token-only production mode is rejected when `A2A_PRODUCTION_SECURITY=true`.
- Doctor output does not leak token, creds contents, or TLS material.
- Subject/envelope/idempotency tests include positive and negative cases.
- `docs/a2a-nats-implementation-progress.md` records the completed phases, validation evidence, commit hash, rollback boundary, and next phase.
```
