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

- Program state: Phase 0 readiness guard and Phase 1 foundation package/config completed in commit `0d720d2`; Phase 2 NATS node and JetStream topology completed in commit `823a998`; Phase 3 durable stores completed in implementation commit `6d3809f`; Phase 4 peer card and discovery completed in implementation commit `ef892e4`; Phase 5 channel ingress and executor completed in implementation commit `9c0683f`; Phase 6 transport integration completed in implementation commit `6e3e31d`; Phase 7 bot-tools and Discord UX completed in implementation commit `f0dbe99`.
- Current phase: Phase 8 artifacts, delivery, audit is next.
- First execution target: completed Phase 0 guide validation fix and Phase 1 foundation only.
- Known pre-implementation issue: resolved by splitting the self-referential forbidden-string checks in `docs/a2a-nats-implementation-guide.md`.

## Phase ledger

| Phase | Name | Status | Commit | Required validation evidence | Notes |
|---:|---|---|---|---|---|
| 0 | Readiness guard | done | `0d720d2` | `python3 - <<'PY' ...` printed `a2a-guide-readiness-ok`; Section 7 guide-only verification printed `a2a-implementation-guide-ready` | Fixed self-referential Phase 0 check without changing A2A behavior. |
| 1 | Foundation package and config | done | `0d720d2` | `go test ./a2a -run 'Test(Subject|Envelope|TaskState|ErrorCode|NatsMsgID)'` passed; `go test ./channel -run 'TestDoctor.*A2A|TestDoctorRuntimeOverviewDoesNotLeakRawEnvironmentValues'` passed; targeted config parse tests passed. | No NATS connection. A2A stays disabled when `NATS_URL` is unset. |
| 2 | NATS node and JetStream topology | done | `823a998` | `go test ./a2a -run 'Test(NodeDisabled|ConnectDrain|EnsureStreams|NoPoolSubject|DuplicateNatsMsgID)'` passed; `go test . -run 'TestConfig.*A2A'` passed. | Streams and consumers only; no remote task execution. |
| 3 | Durable stores | done | `6d3809f` | `go test ./a2a -run 'Test(TaskStore|AcceptedBootstrap|RejectedBeforeAccepted|TerminalImmutable|PolicyStore|PeerStore|ObjectRef)'` passed. | Durable TaskStore, event store, policy store, peer store, and object-ref metadata store. |
| 4 | Peer card and discovery | done | `ef892e4` | `go test ./a2a -run 'Test(AgentCardSanitizer|ExtendedCard|PeerKV|PeerWatch|Heartbeat|PeerRequestReplyFallback|PeerTrustSummary|VersionCompatibility|StalePeer)'` passed; `go test ./bot ./channel -run 'Test.*A2A.*Peer'` passed. | Public card sanitizer, KV/fallback discovery, heartbeat, and manager-visible trust summary. |
| 5 | Channel ingress and executor | done | `9c0683f` | `go test ./channel -run 'TestManagerA2A(IngressDisabled|PolicyDenied|AcceptsOnce|InboundQuota|AdmissionBeforeExecution|AckAfterAdmissionNotCompletion|UsesWorker|ProxyDisablesEgress|RemoteMemoryWriteDenied|RemoteMemoryWriteAllowedByPolicy|Timeout|Cancel|InputRequired|AuthRequired|ResultCapture)'` passed; `go test ./channel -run TestWorkerA2A` passed; `go test ./channel -run TestA2A` passed. | Ingress only through `channel.Manager` and worker runtime; no transport consumers yet. |
| 6 | Transport integration | done | `6e3e31d` | `go test ./a2a -run TestTransport` passed; `go test ./a2a -run 'TestA2AIntegration(TargetedDelegation|DuplicateDelivery|CancelOwnership|AcceptedBootstrap|AdmissionBeforeExecution|AckAfterAdmissionNotCompletion|ReplayAfterReconnect|EventRateQuota|EventRateOverloaded|EventRateZeroUnlimited|NoErrorEnvelope|NoPoolSubject)'` passed. | Two-node embedded JetStream closed loop through durable task/control/event consumers. |
| 7 | Bot-tools and Discord UX | done | `f0dbe99` | `go test ./internal/botmcp -run 'TestA2A|TestTool'` passed; `go test ./bot -run 'TestSlashCommandsIncludeAgentAndUsage|TestA2AConfirmationResponseUsesLocale|TestSlashCommandsApplyVisibilityAndPermissionPolicy|TestChannelOnlySlashCommands'` passed; `go test ./a2a -run 'TestSubject|TestEnvelope|TestTaskState|TestTransport'` passed. | Bot-tools A2A service/tool registration, `/a2a` slash fallback, requester/manager checks, confirmation tokens, and locale-covered confirmation output. |
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

- Status: done; commit `823a998`.
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

### Phase 3 — Durable stores

- Status: done; implementation commit `6d3809f`.
- Changed files: `a2a/event_store.go`, `a2a/object_store.go`, `a2a/peer_store.go`, `a2a/policy_store.go`, `a2a/sqlite_store.go`, `a2a/store_phase3_test.go`, `a2a/task_store.go`, `docs/a2a-nats-implementation-progress.md`.
- Validation:
  - `go test ./a2a -run 'Test(TaskStore|AcceptedBootstrap|RejectedBeforeAccepted|TerminalImmutable|PolicyStore|PeerStore|ObjectRef)'` passed.
  - LSP workspace diagnostics: no issues found.
  - No Discord or ACP imports exist in `a2a/`.
- Done criteria evidence:
  - SQLite migrations create schema version records and deterministic task, event, policy, peer, and object-ref tables under `DATA_DIR/a2a/`.
  - Task tests prove outbound/inbound idempotency, accepted bootstrap executor binding, rejected-before-accepted `msg_<messageId>` correlation, and terminal immutability except idempotent replay.
  - Policy tests prove `0` max concurrency is unlimited, co-present sharing rules are enforced, unstable agent IDs are rejected, and enabled `channel_ref` values are unique.
  - Peer tests prove public card sanitization removes paths/secrets/private URLs and produces a stale/trust display model.
  - Object-ref tests prove size/digest mismatch rejection and expired-row cleanup without object byte upload.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Rollback boundary: revert Phase 3 store files and tests; Phase 2 NATS node/stream code remains inert.
- Next phase: Phase 4 peer card and discovery.

### Phase 4 — Peer card and discovery

- Status: done; implementation commit `ef892e4`.
- Changed files: `a2a/card.go`, `a2a/card_phase4_test.go`, `a2a/discovery.go`, `a2a/heartbeat.go`, `a2a/peer_store.go`, `a2a/sqlite_store.go`, `bot/a2a_peer_test.go`, `bot/commands.go`, `channel/a2a_peer_test.go`, `channel/manager.go`, `docs/a2a-nats-implementation-progress.md`, `locale/lang/en.json`, `locale/lang/zh-TW.json`.
- Validation:
  - `go test ./a2a -run 'Test(AgentCardSanitizer|ExtendedCard|PeerKV|PeerWatch|Heartbeat|PeerRequestReplyFallback|PeerTrustSummary|VersionCompatibility|StalePeer)'` passed.
  - `go test ./bot ./channel -run 'Test.*A2A.*Peer'` passed.
  - LSP workspace diagnostics: no issues found.
  - No Discord or ACP imports exist in `a2a/`.
  - `git diff --check` produced no output.
- Done criteria evidence:
  - Public `AgentCard` builder emits canonical fields only, strips skill examples, removes filesystem paths, Discord IDs, tokens/secrets, and private HTTP/WebSocket URLs, and declares A2A NATS binding v1.0 compatibility without SSE or push notifications.
  - Extended cards are sanitized separately for authenticated peers and store only coarse channel/runtime/trust metadata.
  - JetStream KV bucket `A2A_PEERS` is created with TTL, peer card publishes use key `<agentID>`, KV entries upsert `PeerStore`, and delete/tombstone paths mark peers stale.
  - Fallback request/reply collector gathers multiple peer cards until deadline and reports timeout/no-responder cases explicitly.
  - Heartbeat payloads publish on `a2a.v1.heartbeat.<agent>.<instance>` and update online/stale display state without being used as authorization.
  - Manager and bot doctor surfaces expose deterministic localized peer trust summaries with binding, protocol version, signature status, credential issuer/fingerprint, online/stale state, and skill IDs.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Rollback boundary: revert Phase 4 card/discovery/heartbeat files, peer store display extensions, localized peer summary strings, and the Manager/Bot read-only peer summary wiring; TaskStore and policy store remain intact.
- Next phase: Phase 5 channel ingress through channel runtime.

### Phase 5 — Channel ingress and executor

- Status: done; implementation commit `9c0683f`.
- Changed files: `a2a/errors.go`, `a2a/executor.go`, `a2a/policy_store.go`, `channel/a2a.go`, `channel/a2a_phase5_test.go`, `channel/bot_tools_target.go`, `channel/manager.go`, `channel/worker.go`, `internal/botmcp/server.go`, `internal/botmcp/server_test.go`, `docs/a2a-nats-implementation-progress.md`.
- Validation:
  - `go test ./channel -run 'TestManagerA2A(IngressDisabled|PolicyDenied|AcceptsOnce|InboundQuota|AdmissionBeforeExecution|AckAfterAdmissionNotCompletion|UsesWorker|ProxyDisablesEgress|RemoteMemoryWriteDenied|RemoteMemoryWriteAllowedByPolicy|Timeout|Cancel|InputRequired|AuthRequired|ResultCapture)'` passed.
  - `go test ./channel -run TestWorkerA2A` passed.
  - `go test ./channel -run TestA2A` passed.
  - `go test ./internal/botmcp -run TestA2ARemoteMemoryWrite` passed.
  - `go test ./a2a -run 'Test(Subject|Envelope|TaskState|ErrorCode|NatsMsgID|PolicyStore)'` passed.
  - LSP workspace diagnostics: no issues found.
  - No Discord, ACP, bot, or internal bot packages are imported by `a2a/`.
  - `git diff --check` produced no output.
- Done criteria evidence:
  - `channel.Manager` implements `a2a.Executor` through `AdmitA2ATask` and `RunA2ATask`; `a2a/` owns only DTOs/helpers and does not import `channel`.
  - Admission resolves enabled `channel_ref` through the A2A policy store, validates `accept_from`, `accept_skills`, result visibility, transcript sharing, and inbound quota before execution starts.
  - Duplicate inbound `message_id` admission is idempotent and does not reserve duplicate worker slots; quota/full queue paths reject with `overloaded`.
  - `RunA2ATask` enqueues an inline existing `Worker` job only after admission, captures final worker output as `TaskExecutionResult`, and records terminal results in TaskStore-compatible rows.
  - Proxy mode writes bot-tools target state with Discord egress disabled; remote A2A memory writes remain denied unless `remote_tool_policy_json.allow_memory_write=true`.
  - Timeout, cancel, input-required, auth-required, failed, and completed worker outcomes are represented as A2A task states/results.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Blockers or risks: no Phase 5 blocker. Transport consumers/events are still absent by phase boundary and belong to Phase 6.
- Rollback boundary: revert `channel/a2a.go`, Phase 5 channel worker/manager target-state wiring, A2A executor DTO/policy helper changes, bot-tools remote memory guard, and Phase 5 tests; Phase 4 peer discovery and Phase 3 stores remain intact.
- Next phase: Phase 6 transport consumers and event routing.

### Phase 6 — Transport integration

- Status: done; implementation commit `6e3e31d`.
- Changed files: `a2a/admission.go`, `a2a/integration_test.go`, `a2a/task_store.go`, `a2a/transport.go`, `channel/manager.go`, `docs/a2a-nats-implementation-progress.md`.
- Validation:
  - `go test ./a2a -run TestTransport` passed.
  - `go test ./a2a -run 'TestA2AIntegration(TargetedDelegation|DuplicateDelivery|CancelOwnership|AcceptedBootstrap|AdmissionBeforeExecution|AckAfterAdmissionNotCompletion|ReplayAfterReconnect|EventRateQuota|EventRateOverloaded|EventRateZeroUnlimited|NoErrorEnvelope|NoPoolSubject)'` passed.
  - `go test ./channel -run TestManagerA2A` passed.
  - `go test ./a2a -run 'Test(Subject|Envelope|TaskState|ErrorCode|NatsMsgID|PolicyStore)'` passed.
  - `go test ./bot ./channel -run 'Test.*A2A|TestDoctor.*A2A'` passed.
  - LSP workspace diagnostics: no issues found.
  - `go list -f '{{join .Imports "\n"}}' ./a2a` listed no Discord, ACP, bot, channel, or internal bot packages.
  - `git diff --check` produced no output.
- Done criteria evidence:
  - `a2a.Transport` starts task/control/event durable consumers only when the node is enabled and a concrete `SQLiteTaskStore` plus injected `a2a.Executor` exist.
  - Task receive validates subject and envelope, maps adapter payload into `TaskExecutionRequest`, records durable rejected/accepted outcomes, publishes accepted/rejected events with stable `Nats-Msg-Id`, double-acks only after durable admission/event publication, and starts `RunA2ATask` asynchronously after ack.
  - Duplicate task delivery checks inbound `(direction,message_id)` and the in-memory started set before execution, so a redelivered task does not enqueue a second run.
  - Control receive binds subject sender/executor/task ID to an inbound TaskStore row before applying cancel/status handling; forged cancel is rejected before mutation.
  - Event receive applies accepted bootstrap, pre-accept rejected correlation, status/result/artifact monotonic revisions, terminal immutability, and replay from the durable `A2A_EVENTS` stream after reconnect.
  - Publisher APIs for task/control/accepted/rejected/status/result set the required stable `Nats-Msg-Id`; event-rate quota `0` remains unlimited and nonzero overflow returns canonical `overloaded`.
  - `channel.Manager` starts transport consumers after Manager/stores exist and stops them before closing A2A stores; disabled `NATS_URL` remains a no-op.
  - No pool subject, public HTTP endpoint, SSE stream, push config, gateway adapter, or standalone `error` envelope was added.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Blockers or risks: no Phase 6 blocker. Phase 7 must add bot-tools/slash/button UX on top of publisher APIs; user-facing Discord delivery remains Phase 8.
- Rollback boundary: stop consumers and revert `a2a/transport.go`, `a2a/admission.go`, Phase 6 integration tests, `a2a/task_store.go` event-application helpers, and `channel.Manager` transport lifecycle wiring; Phase 5 executor and Phase 3 stores remain intact.
- Next phase: Phase 7 bot-tools and natural-language Discord UX.

### Phase 7 — Bot-tools and Discord UX

- Status: done; implementation commit `f0dbe99`.
- Changed files: `a2a/transport.go`, `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tool_register.go`, `internal/botmcp/a2a_tools_test.go`, `internal/botmcp/server.go`, `bot/a2a_commands.go`, `bot/commands.go`, `bot/handler.go`, `bot/handler_test.go`, `bot/interaction_policy.go`, `locale/lang/en.json`, `locale/lang/zh-TW.json`, `docs/a2a-nats-implementation-progress.md`.
- Validation:
  - `go test ./internal/botmcp -run 'TestA2A|TestTool'` passed.
  - `go test ./bot -run 'TestSlashCommandsIncludeAgentAndUsage|TestA2AConfirmationResponseUsesLocale|TestSlashCommandsApplyVisibilityAndPermissionPolicy|TestChannelOnlySlashCommands'` passed.
  - `go test ./a2a -run 'TestSubject|TestEnvelope|TestTaskState|TestTransport'` passed.
- Done criteria evidence:
  - `internal/botmcp.NewServer` now registers `bot_a2a_*` read/write/destructive tools with closed-world annotations; default safe exposure includes read-only A2A peer/policy/status/plan tools only.
  - `internal/botmcp.A2AService` enforces bound guild/channel context, requester identity, manager-only policy mutation, HMAC confirmation tokens, outbound quota checks, policy-allowed peer/skill delegation, durable task/status lookup, input/auth replies, cancel control publication, and audit events for policy/task operations.
  - `/a2a` is guild-only, channel-only, ManageChannels-gated at Discord command policy, and exposes peer/status/delegate/cancel/reply/authorize/policy subcommands. Policy mutation without a token returns a localized confirmation message instead of applying state.
  - `a2a.Publisher.SendTask` now persists outbound task rows with Discord channel/guild metadata so bot-tools task status can list channel-scoped outbound work.
  - Confirmation output has English and zh-TW locale keys and a focused zh-TW test.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Blockers or risks: Phase 7 intentionally does not deliver artifact bytes, mirror/co-present transcript delivery, or result egress; those remain Phase 8.
- Rollback boundary: revert Phase 7 bot-tools service/registration/tests, `/a2a` command additions, A2A locale keys, and the outbound channel/guild metadata persistence change; Phase 6 transport consumers remain intact.
- Next phase: Phase 8 artifacts, result delivery, and transcript modes.

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
