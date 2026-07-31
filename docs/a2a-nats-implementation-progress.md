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

- Program state: Original bot-level A2A NATS implementation completed through Phase 9 (`21e4173`) and deployed for runtime smoke. Current worktree aligns the runtime-scoped A2A path to a policy-derived runtime authority model instead of a separate runtime registry table.
- Current phase: R6 cutover readiness is unit-ready in the worktree; deployment smoke pending operator authorization. R1 policy-derived runtime authority is complete in commit `b0471a4`; `channel_a2a_policy.runtime_agent_id` is the local runtime ownership authority and `RuntimeRecord` remains an in-memory DTO derived from policy rows for cards/status/validation.
- Latest source target: run deployment smoke only if the operator explicitly authorizes touching `.env`/services; otherwise continue code-only hardening.
- Known implementation state: runtime mode parsing, deterministic runtime ID generation, policy runtime fields, runtime peer cards from discoverable policies, runtime transport consumers, runtime source identity, granular `/a2a` slash fallback, requester-scoped status, dual-mode deployment, and unit-tested runtime-mode cutover guards are present. Gaps: there is no standalone `/a2a policy` slash command by design; live production `runtime` cutover remains unproven. Documentation makes normal Discord natural language plus `bot_a2a_*` MCP tools the primary UX contract, with slash commands limited to fallback/bootstrap/admin paths.

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
| 7 | Bot-tools and Discord UX | done | `f0dbe99`, `8a17e88` | `go test ./internal/botmcp -run 'TestA2ATools(Annotations|BoundContext|PolicyPlan|PolicyApply|Delegate|DelegateQuota|Cancel|InputReply|AuthReply)'` passed; `go test ./bot ./locale -run 'TestA2A(Slash|Buttons|Confirmation|Locale|Permission)'` passed; `go test ./a2a -run 'TestSubject|TestEnvelope|TestTaskState|TestTransport'` passed. | Bot-tools A2A service/tool registration, `/a2a` slash fallback, signed confirmation button custom IDs, requester/manager checks, confirmation tokens, and locale-covered confirmation output. |
| 8 | Artifacts, delivery, audit | done | `d73e8b2` | `go test ./a2a -run 'TestObject(Store|Digest|Retention|MediaPolicy)'` passed; `go test ./bot ./internal/botegress ./channel ./audit -run 'TestA2A(Egress|Artifact|ProxyDelivery|MirrorTranscript|CoPresent|TransparentResult|AuditMetadata)|TestStoreRecordsA2AAuditMetadata'` passed; `go test ./...` passed. | JetStream Object Store backend, generated object refs, proxy/mirror/transparent safe-egress delivery, media policy checks, and A2A audit metadata. |
| 9 | Production hardening and rollout | done | `bbdc70d`, `21e4173` | `go test ./a2a ./channel ./internal/botmcp ./bot ./audit ./locale -run 'Test.*A2A|TestDoctor.*A2A'` passed; rollout guide check printed `a2a-rollout-guide-ok`; Workspace LSP diagnostics reported no issues; strict reviewer follow-up reported no remaining prior Critical/High ACL blocker. | Env docs, ACL templates, dev NATS config, smokes, rollback guide, doctor auth-mode summary, credential revocation regression, and review-finding fixes. |
| R0 | Runtime-first planning document correction | done | pending commit | Guide sanity printed `a2a-runtime-guide-ready`; runtime docs sanity printed `a2a-runtime-docs-ok`; strict docs follow-up reported no High/Medium blockers. | Source spec, guide, rollout, and env example aligned around runtime-scoped identity as the target design. |
| R1 | Policy-derived runtime authority and local cutover seams | done | `b0471a4` | `go test ./a2a ./channel -run 'Test(RuntimeID|PolicyStore|Doctor.*A2A|RemoteMemoryWriteDenied|RemoteMemoryWriteAllowed)'` passed; `go test ./...` passed; `git diff --check` passed. | Removed dead `RuntimeStore`/`a2a_runtime_registry` duplicate authority; `channel_a2a_policy.runtime_agent_id` is the local runtime authority and `RuntimeRecord` is a DTO derived from policy rows. |
| R2 | Runtime peer cards and discovery | smoke_ready | `ad06e6a` | `go test ./bot -run 'Test(RuntimePeerCard|BuildBotA2APeerCard)'` passed; included in combined runtime A2A selector. | Runtime cards publish from discoverable policy rows after transport accepts the runtime. |
| R3 | Runtime policy canonicalization | smoke_ready | `ad06e6a` | Included in combined runtime A2A selector; `go test ./...` passed. | Delegation can be scoped by runtime ID when `target_agent` is a runtime ID, and legacy `agent + target_channel_ref` remains migration input. |
| R4 | Runtime NATS routing | smoke_ready | `ad06e6a` | Included in combined runtime A2A selector; `go test ./...` passed. | Transport can consume and publish task/control/event traffic for accepted runtime IDs derived from discoverable policy rows. |
| R5 | Runtime UX, status authorization, and attribution | smoke_ready | `ad06e6a` | Included in combined runtime A2A selector; `go test ./...` passed. | `/doctor`, granular `/a2a` setup/mutation previews, requester status authorization, and usage attribution are implemented; there is no standalone `/a2a policy` slash command. |
| R6 | Cutover readiness and strict review | unit_ready | current commit | `go test ./channel -run 'TestManagerA2A(RuntimeModeUsesRuntimeTarget|RuntimeModeRejectsBotLevelTarget|DualModeAcceptsBotAndRuntimeTargets)'` passed; R6 selector passed; `go test ./...` passed; `git diff --check` passed. | Runtime mode rejects bot-level targets; dual mode accepts bot-level drain and exact runtime targets. Live runtime deployment smoke remains pending operator authorization. |

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

- Status: done; implementation commits `f0dbe99` and `8a17e88`.
- Changed files: `a2a/transport.go`, `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tool_register.go`, `internal/botmcp/a2a_tools_test.go`, `internal/botmcp/server.go`, `bot/a2a_commands.go`, `bot/commands.go`, `bot/handler.go`, `bot/handler_test.go`, `bot/interaction_policy.go`, `locale/lang/en.json`, `locale/lang/zh-TW.json`, `docs/a2a-nats-implementation-progress.md`.
- Validation:
  - `go test ./internal/botmcp -run 'TestA2ATools(Annotations|BoundContext|PolicyPlan|PolicyApply|Delegate|DelegateQuota|Cancel|InputReply|AuthReply)'` passed.
  - `go test ./bot ./locale -run 'TestA2A(Slash|Buttons|Confirmation|Locale|Permission)'` passed.
  - `go test ./a2a -run 'TestSubject|TestEnvelope|TestTaskState|TestTransport'` passed.
- Done criteria evidence:
  - `internal/botmcp.NewServer` now registers `bot_a2a_*` read/write/destructive tools with closed-world annotations; default safe exposure includes read-only A2A peer/policy/status/plan tools only.
  - `internal/botmcp.A2AService` enforces bound guild/channel context, requester identity, manager-only policy mutation, HMAC confirmation tokens, outbound quota checks, policy-allowed peer/skill delegation, durable task/status lookup, input/auth replies, cancel control publication, and audit events for policy/task operations.
  - `/a2a` is guild-scoped and exposes concrete fallback subcommands: `peers`, `setup`, `ask`, `status`, `delegate`, `cancel`, `reply`, `authorize`, plus granular manager policy mutations (`enable`, `disable`, `ref`, `expose`, `unexpose`, `accept-from`, `deny-from`, `delegate-to`, `undelegate-to`, `max-concurrent`, `transcript-mode`, `transcript-from`). There is no standalone `/a2a policy` slash subcommand; structured policy reads are provided by `bot_a2a_policy_get`, and slash policy mutations return localized confirmation before applying.
  - A2A confirmation button custom IDs are HMAC-bound to action/channel/change and stay within Discord's 100-character custom_id limit; tests reject wrong-channel replay.
  - `a2a.Publisher.SendTask` now persists outbound task rows with Discord channel/guild metadata so bot-tools task status can list channel-scoped outbound work.
  - Confirmation output has English and zh-TW locale keys and focused locale tests.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Blockers or risks: Phase 7 intentionally does not deliver artifact bytes, mirror/co-present transcript delivery, or result egress; those remain Phase 8.
- Rollback boundary: revert Phase 7 bot-tools service/registration/tests, `/a2a` command additions, A2A locale keys, and the outbound channel/guild metadata persistence change; Phase 6 transport consumers remain intact.
- Next phase: Phase 8 artifacts, result delivery, and transcript modes.

### Phase 8 — Artifacts, delivery, audit

- Status: done; implementation commit `d73e8b2`.
- Changed files: `a2a/admission.go`, `a2a/audit.go`, `a2a/errors.go`, `a2a/executor.go`, `a2a/object_store.go`, `a2a/store_phase3_test.go`, `a2a/task_store.go`, `a2a/transport.go`, `a2a/types_test.go`, `audit/store_test.go`, `channel/a2a.go`, `channel/a2a_phase8_test.go`, `channel/manager.go`, `internal/botmcp/a2a_tools.go`.
- Validation:
  - `go test ./a2a -run 'TestObject(Store|Digest|Retention|MediaPolicy)'` passed.
  - `go test ./bot ./internal/botegress ./channel ./audit -run 'TestA2A(Egress|Artifact|ProxyDelivery|MirrorTranscript|CoPresent|TransparentResult|AuditMetadata)|TestStoreRecordsA2AAuditMetadata'` passed.
  - `go test ./internal/botmcp -run 'TestA2AITestTool|TestA2ATools' && go test ./bot -run 'TestSlashCommandsIncludeAgentAndUsage|TestA2AConfirmationResponseUsesLocale|TestSlashCommandsApplyVisibilityAndPermissionPolicy|TestChannelOnlySlashCommands' && go test ./a2a -run 'TestSubject|TestEnvelope|TestTaskState|TestTransport'` passed.
  - `go test ./...` passed.
  - Workspace LSP diagnostics reported no issues.
- Done criteria evidence:
  - `a2a.SQLiteObjectStore` now supports generated `a2a-artifacts` object refs backed by JetStream Object Store bytes, fetch-time digest/size verification, generated `tasks/<taskId>/...` keys, and retention pruning that deletes expired backend objects while preserving permanent rows.
  - `a2a.TaskExecutionArtifact` and `TaskEventPayload` carry object-ref fields; `a2a.Publisher.PublishArtifact` emits artifact events using artifact-scoped NATS idempotency, and event storage now allows distinct event types at the same revision.
  - Delegator-side `channel.Manager` delivers proxy final results, mirror/co-present status labels, transparent results, and object artifacts through `internal/botegress.WritePending`, preserving safe egress and `AllowedMentions` behavior.
  - Artifact Discord delivery fetches object bytes into transient safe-egress files, validates digest/size and channel media policy, and rejects disallowed MIME/size/object-ref policy before queuing the file.
  - A2A audit helpers normalize spec metadata keys; bot-tools delegation records publish failures and queued tasks with A2A metadata; visible transcript/result delivery records `a2a_transcript_posted` and `a2a_result_delivered`.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Blockers or risks: co-present executor-direct Discord posting remains intentionally delivery-layer controlled by the delegator safe-egress path in this implementation; Phase 9 rollout smokes must verify multi-bot Discord permission behavior before production enablement.
- Rollback boundary: revert Phase 8 object/artifact/audit/delivery wrappers and tests; Phase 7 bot-tools policy/control UX and Phase 6 durable transport remain intact.
- Next phase: Phase 9 production hardening and rollout.

### Phase 9 — Production hardening and rollout

- Status: done; implementation commits `bbdc70d` and `21e4173`; ledger commits include `0fb8340` plus the final review-fix ledger update.
- Changed files: `.env.example`, `dev/nats.conf`, `a2a/types.go`, `a2a/types_test.go`, `docs/a2a-nats-rollout.md`, `docs/a2a-nats-implementation-guide.md`, `docs/release.md`, `docs-site/docs/guide/a2a-nats-rollout.md`, `docs-site/docs/guide/deployment.md`, `docs-site/docs/guide/environment.md`, `docs-site/docs/guide/release.md`, `channel/doctor_env.go`, `channel/doctor_env_test.go`, `internal/botmcp/a2a_tools_test.go`, `locale/lang/en.json`, `locale/lang/zh-TW.json`.
- Validation:
  - `go test ./a2a ./channel ./internal/botmcp ./bot ./audit ./locale -run 'Test.*A2A|TestDoctor.*A2A'` passed.
  - `python3 - <<'PY' ...` rollout guide check printed `a2a-rollout-guide-ok`.
  - Workspace LSP diagnostics reported no issues.
  - Strict review found three blockers; follow-up fixes landed in `21e4173`. Final review confirmed the remaining `$JS.API.INFO` ACL blocker was resolved.
- Done criteria evidence:
  - A2A remains disabled while `NATS_URL` is unset; `.env.example`, `/doctor`, and rollout rollback instructions describe `NATS_URL=""` as the inert/no-op path for existing Discord behavior, while `NATS_URL` set without `A2A_AGENT_ID` remains a startup validation error.
  - `a2a.Config.ValidateStartup` now requires `NATS_CREDS_FILE` when `A2A_PRODUCTION_SECURITY=true`; `NATS_TOKEN` and `NATS_TLS_CA_FILE` are not treated as production client credentials. `/doctor` reports A2A auth mode, production guard state, startup validation, TLS-CA-only/token-only warnings, and no raw token or credential paths.
  - `docs/a2a-nats-rollout.md` and docs-site copy document development single-node NATS, internal lightweight single-node JetStream production, optional HA three-node JetStream, per-agent ACL template including `$JS.API.INFO`, stream, durable consumer, and KV permissions, response/inbox constraints, authenticated principal binding, negative ACL smokes, credential issue/rotation/revocation, startup/shutdown ordering, rollout gates, rollback, and final validation matrix.
  - `dev/nats.conf` supplies a non-secret local JetStream config for two-bot smoke testing.
  - `TestA2AToolsDelegateRejectsRevokedPeerBeforePublishing` proves an untrusted/revoked peer is denied before any new delegated work can publish.
- Runtime settings touched: no live `.env`, `DATA_DIR`, Docker volume, deployment host, or live service was touched.
- Deployment hosts touched: no.
- Blockers or risks: environment-specific production smokes still require real NATS credentials, Discord bot accounts, and target channels; this phase records the gates but does not execute live rollout.
- Rollback boundary: set `NATS_URL=""`, drain/restart the bot, keep `DATA_DIR` A2A stores and audit rows for postmortem; code/doc rollback is limited to Phase 9 docs/config/doctor/test additions because Phases 1-8 remain inert while A2A is disabled.
- Next phase: none; A2A NATS implementation program is complete, pending operator rollout in target environments.

### Phase R0 — Runtime-first planning document correction

- Status: done; commit pending.
- Changed files: `.env.example`, `docs/a2a-nats-integration-spec.md`, `docs/a2a-nats-implementation-guide.md`, `docs/a2a-nats-implementation-progress.md`, `docs/a2a-nats-rollout.md`.
- Validation:
  - `python3 - <<'PY' ...` guide sanity printed `a2a-runtime-guide-ready`.
  - `python3 - <<'PY' ...` runtime docs sanity printed `a2a-runtime-docs-ok`.
  - Strict docs follow-up review reported: no High/Medium blockers remain.
- Done criteria evidence:
  - Spec now defines bot base identity, `runtime_agent_id`, policy-derived runtime authority, runtime subjects, runtime peer cards, runtime policy fields, runtime-scoped auth, confused-deputy checks, and runtime test cases.
  - Guide now declares R1-R6 runtime migration phases and marks old Phase 0-9 sections as archived bot-level context only.
  - Rollout guide and `.env.example` now document `A2A_RUNTIME_ID_MODE` and runtime ACL/smoke expectations.
- Runtime settings touched: no live `.env`, `DATA_DIR`, Docker volume, deployment host, or live service was touched.
- Deployment hosts touched: no.
- Rollback boundary: revert only the runtime-first planning doc/env-example changes; no implementation behavior changed.
- Next phase: Phase R1 policy-derived runtime authority.


### Runtime-first code-observed audit — dual-mode smoke path

- Status: smoke-ready implementation; implementation commit `ad06e6a`; documentation alignment recorded here.
- Scope: runtime-scoped A2A behavior present in the current worktree and deployed to the local/d80 test bots with `A2A_RUNTIME_ID_MODE=dual`.
- Changed files: `a2a/runtime.go`, `a2a/policy_store.go`, `a2a/card.go`, `a2a/types.go`, `a2a/streams.go`, `a2a/transport.go`, `a2a/task_store.go`, `a2a/admission.go`, `a2a/executor.go`, `a2a/store.go`, `config.go`, `channel/a2a.go`, `channel/doctor_env.go`, `channel/manager.go`, `channel/worker.go`, `channel/bot_tools_target.go`, `bot/a2a_discovery.go`, `bot/a2a_commands.go`, `bot/commands.go`, `bot/interaction_policy.go`, `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tool_register.go`, `internal/botmcp/server.go`, locale files, targeted tests, docs, and `.env.example`.
- Validation:
  - `go test ./a2a ./channel ./internal/botmcp ./bot -run 'Test(RuntimeID|PolicyStore|Doctor.*A2A|RemoteMemoryWriteDenied|RemoteMemoryWriteAllowed|RuntimePeerCard|BuildBotA2APeerCard|A2ATools|ManagerA2A)'` passed.
  - `go test ./...` passed.
  - Local bot deployment: `Bot running as M5Bot#8313`, A2A NATS node enabled for `m5bot-local`, transport consumers started, `/a2a` registered, NATS and Discord TCP sessions established.
  - d80 deployment: systemd active for PID `967010`, `Bot running as ChunBot#4533`, A2A NATS node enabled for `d80-chunbot`, transport consumers started, `/a2a` registered, NATS and Discord TCP sessions established.
- Code-observed done criteria:
  - `A2A_RUNTIME_ID_MODE` parses as `legacy|dual|runtime`; both test bots are deployed in `dual`.
  - Runtime IDs are generated by `GenerateRuntimeAgentID` from bot base ID plus sanitized channel label, with hash fallback for unsafe/too-long labels.
  - `channel_a2a_policy` persists `runtime_agent_id`, `bot_agent_id`, `discoverable`, runtime accept/co-present lists, delegate targets, and fail-closed `remote_tool_policy_json`.
  - Discoverable policies can publish runtime peer cards keyed by `runtime_agent_id`; `runtime` mode suppresses the legacy bot card path.
  - Transport can create per-runtime task/control/event consumers through `EnsureConsumersForAgent`, can add runtime consumers dynamically, and publishes accepted/status/result/artifact events from the runtime source identity.
  - Runtime mode admission rejects bot-level targets and requires the exact channel runtime target; dual mode accepts bot-level and runtime targets for bounded migration.
  - Bot-tools and slash setup/delegation can scope work by exact runtime ID or by legacy `agent + target_channel_ref` migration input.
  - `/doctor` reports `A2A_RUNTIME_ID_MODE` without exposing credentials.
  - `/a2a` exposes granular fallback subcommands only; there is no standalone `/a2a policy` slash command. Structured policy reads remain `bot_a2a_policy_get`.
  - A2A task status is requester-or-manager scoped; origin requester usage attribution is accepted only when the origin guild matches the admitted context.
- Code-observed remaining gaps:
  - Production `A2A_RUNTIME_ID_MODE=runtime` cutover and negative legacy-addressed smokes are not yet proven.
  - `bot_a2a_policy_get` exists, but Discord slash has no `/a2a policy` read command by design.
  - Runtime policy rows remain the only local authority; do not reintroduce a second registry table without a future migration and conflict-resolution contract.
- Runtime settings touched: test deployment `.env` files were updated to `A2A_RUNTIME_ID_MODE=dual`; no `DATA_DIR`, Docker volume, deployment host state beyond bot binary/env/service restart, or production service was changed.
- Deployment hosts touched: local test bot and d80 test bot only.
- Rollback boundary: set `A2A_RUNTIME_ID_MODE=legacy` or `NATS_URL=""`, restart/drain the bots, restore `kiro-discord-bot.pre-runtime-deploy` binaries if needed, then revert this smoke-path implementation.
- Next phase: finish R1 by deleting dead registry-store code, validating the policy-derived authority model, and committing the aligned code/docs.

### Natural-language Discord UX contract alignment

- Status: documentation-only alignment.
- Changed files: `docs/a2a-nats-integration-spec.md`, `docs/a2a-nats-implementation-guide.md`, and this progress ledger.
- Evidence:
  - Source spec now states the Discord happy path as normal channel/thread language handled by the local channel agent through `bot_a2a_*` MCP tools.
  - Source spec and guide both define slash commands as fallback/bootstrap/admin controls that call the same service methods and must not create a slash-only policy path.
  - User-facing copy must prefer localized collaboration terms over raw protocol fields; raw runtime/policy IDs are limited to manager diagnostics/details.
  - Policy setup and delegation flows are documented as natural-language request → bot-tools plan/delegate → human-readable risk/egress preview → signed Discord confirmation when required → audited service apply/publish.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Rollback boundary: revert this UX documentation-only section; no code behavior changes.
- Next phase: apply this contract while finishing policy-row authority for R1.

### Policy-derived runtime authority alignment

- Status: done; commit `b0471a4`.
- Decision: do not wire `a2a_runtime_registry`; use `channel_a2a_policy.runtime_agent_id` as the single local runtime ownership authority for v1.
- Changed files: `a2a/runtime.go`, `docs/a2a-nats-integration-spec.md`, `docs/a2a-nats-implementation-guide.md`, `docs/a2a-nats-implementation-progress.md`, and `docs/a2a-nats-rollout.md`.
- Validation:
  - `go test ./a2a ./channel -run 'Test(RuntimeID|PolicyStore|Doctor.*A2A|RemoteMemoryWriteDenied|RemoteMemoryWriteAllowed)'` passed.
  - `go test ./...` passed.
  - `git diff --check` passed.
- Done criteria evidence:
  - `a2a.RuntimeStore`, `OpenRuntimeStore`, and SQLite `a2a_runtime_registry` code were removed as unused duplicate authority.
  - `a2a.RuntimeRecord` remains as an in-memory DTO for card/status/validation context.
  - Source spec, guide, rollout, and progress ledger now describe policy-derived runtime authority and confused-deputy checks against policy-derived runtime records.
  - The natural-language Discord UX contract remains unchanged: user intent flows through `bot_a2a_*` MCP tools and slash remains fallback/bootstrap/admin.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Rollback boundary: revert this commit to restore the separate registry-store plan/code; no runtime data migration was performed.
- Next phase: R6 cutover readiness and runtime-mode negative smokes.

### R6 cutover readiness unit validation

- Status: deployed smoke passed; current commit.
- Changed files: `a2a/policy_store.go`, `a2a/store_phase3_test.go`, `channel/a2a_phase5_test.go`, and this progress ledger.
- Validation:
  - `go test ./a2a -run TestPolicyStoreValidationAndPersistence` passed.
  - `go test ./channel -run 'TestManagerA2A(RuntimeModeUsesRuntimeTarget|RuntimeModeRejectsBotLevelTarget|DualModeAcceptsBotAndRuntimeTargets)'` passed.
  - `go test ./a2a ./channel ./internal/botmcp ./bot -run 'Test(RuntimeID|PolicyStore|Doctor.*A2A|RemoteMemoryWriteDenied|RemoteMemoryWriteAllowed|RuntimePeerCard|BuildBotA2APeerCard|A2ATools|ManagerA2A)'` passed.
  - `go test ./...` passed.
  - `git diff --check` passed.
- Deployment smoke:
  - Local M5 bot: `A2A_RUNTIME_ID_MODE=runtime`, runtime policy `m5bot-local-m5-main`, fixed darwin binary sha256 `923c1efd99b29e33ea2e2377ecadfe77a7e98a476058744c56135ad02ef8335e`, `Bot running as M5Bot#8313`, and `[a2a] transport consumers started agent=m5bot-local`.
  - Remote d80 bot: `A2A_RUNTIME_ID_MODE=runtime`, runtime policy `d80-chunbot-d80-main`, fixed linux amd64 binary sha256 `0524a838185f931ac22aba5ea4b71f23fa071c41eb5859e65866a14fe899df47`, `Bot running as ChunBot#4533`, and `[a2a] transport consumers started agent=d80-chunbot`.
  - Exact runtime path smoke: `m5bot-local-m5-main -> d80-chunbot-d80-main`, `channel_ref=d80-main`, `skill_id=general/task`, message `smoke_exact_1785481758`, terminal `TASK_STATE_COMPLETED`, executor `d80-chunbot-d80-main`, task `task_a37179372902282d2767af0b`.
  - Legacy negative smoke: `m5bot-local-m5-main -> d80-chunbot`, `channel_ref=d80-main`, message `smoke_legacy_1785481734`, terminal `TASK_STATE_REJECTED`, executor `d80-chunbot`, error `invalid_envelope: request target d80-chunbot does not match runtime d80-chunbot-d80-main`.
- Done criteria evidence:
  - Runtime mode accepts exact runtime-addressed inbound tasks and rejects new legacy bot-level targets in unit tests and in the local-to-d80 live runtime smoke.
  - Dual mode accepts legacy bot-level targets for migration drain and exact runtime targets.
  - Accepted dual-mode legacy tasks still execute under the runtime executor identity when the policy has `runtime_agent_id`.
  - `SQLitePolicyStore.ListDiscoverable` no longer re-enters `Get` while its discovery cursor is open; this fixes the single-connection SQLite startup deadlock observed before deployment smoke.
- Runtime settings touched: local test bot and d80 test bot only.
- Deployment hosts touched: local test bot and d80 test bot only.
- Rollback boundary: set `A2A_RUNTIME_ID_MODE=dual` or `legacy`, restore the `*.pre-runtime-smoke-*` binary/env backups on each test host if needed, and revert this commit for the `ListDiscoverable` cursor fix/test/progress entry.
- Next phase: R7 live cutover decision for production runtime mode, or continue code-only hardening if production rollout is not authorized.

### R7 runtime cutover preflight checker

- Status: code-ready; current commit.
- Changed files: `a2a/runtime_cutover.go`, `a2a/runtime_cutover_test.go`, `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tool_register.go`, `internal/botmcp/server.go`, `internal/botmcp/a2a_tools_test.go`, `internal/botmcp/server_test.go`, and this progress ledger.
- Validation:
  - `go test ./a2a ./internal/botmcp -run 'Test(RuntimeCutover|A2AToolsRuntimePreflight|A2AToolsAnnotations|DefaultSafeToolNames)'` passed.
  - `go test ./...` passed.
  - `git diff --check` passed.
- Done criteria evidence:
  - `SQLitePolicyStore.RuntimeCutoverReadiness` and pure `a2a.RuntimeCutoverReadiness` produce a read-only guild-scoped report with `ready`, blocker/warning counts, runtime IDs, channel refs, and issue codes.
  - The checker blocks production cutover risks: missing/invalid runtime IDs, foreign bot ownership, runtime ID equal to bot ID, missing explicit `accept_from_runtimes`, missing accepted skills, and legacy-only delegation without runtime `delegate_targets`.
  - The checker warns but does not block dual-mode drain, preserved legacy `accept_from`, preserved legacy `delegate_to`/`delegate_skills` when runtime `delegate_targets` exist, preserved legacy `delegate_targets.agent_id`/`channel_ref` migration fields when `runtime_agent_id` exists, disabled discoverable rows, legacy co-present lists without runtime equivalents, and remote memory write being enabled.
  - `bot_a2a_runtime_preflight` exposes the report as a read-only, idempotent, ManageChannels-gated MCP tool; it applies no policy, NATS, service, or filesystem changes.
- Runtime settings touched: no.
- Deployment hosts touched: no.
- Rollback boundary: revert this code/docs commit; no runtime data migration or live service change is involved.
- Next phase: run the preflight against each intended production guild/bot data dir, fix any blockers in policy rows through existing confirmation flow, then decide whether to authorize R7 production runtime cutover.

### R7 runtime cutover preflight on current bot contexts

- Status: preflight passed for current local M5 and d80 bot contexts; current commit.
- Changed files: `a2a/runtime_cutover.go`, `a2a/runtime_cutover_test.go`, and this progress ledger.
- Validation:
  - Read-only SQLite backups were made from `/Users/chun/Projects/kiro-discord-bot-local/data/a2a/policy.sqlite` and remote `d80:/home/chun/kiro-discord-bot/data/a2a/policy.sqlite`; preflight ran against the backups, not live policy DB files.
  - `go run /tmp/a2a_preflight_report.go local-m5 /tmp/a2a-preflight-local m5bot-local runtime d80 /tmp/a2a-preflight-d80 d80-chunbot runtime` passed.
  - `go test ./a2a ./internal/botmcp -run 'Test(RuntimeCutover|A2AToolsRuntimePreflight|A2AToolsAnnotations)'` passed.
- Done criteria evidence:
  - Local M5 bot: guild `1495737767827865620`, runtime policy `m5bot-local-m5-main`, channel ref `m5-main`, `ready=true`, blockers `0`, warnings `3`.
  - Remote d80 bot: guild `1495737767827865620`, runtime policy `d80-chunbot-d80-main`, channel ref `d80-main`, `ready=true`, blockers `0`, warnings `3`.
  - Remaining warnings on both contexts are preserved migration fields only: `legacy_accept_from_present`, `legacy_delegate_policy_present`, and `legacy_delegate_target_fields`; runtime fields take precedence in runtime mode.
- Runtime settings touched: no.
- Live policy DBs touched: no.
- Deployment hosts touched: no service restart; remote d80 policy DB was copied for read-only preflight evidence.
- Rollback boundary: revert this docs/code commit; no runtime data migration or live service change is involved.
- Next phase: operator-authorized production runtime cutover decision for the same current M5/d80 contexts, or continue code-only hardening if production rollout is not authorized.

### R8 internal lightweight production rollout plan

- Status: deployed smoke passed; current commit.
- Decision: user requested internal/lightweight production instead of mandatory three-node NATS. The accepted production profile is one private single-node NATS/JetStream server with persistent storage, TLS server validation, token authentication, localhost-only monitoring, and firewall/VPN host restrictions. This intentionally trades NATS HA for lower operational load.
- Observed current topology:
  - NATS server: `mxp2`, `nats-server.service` active, single node, config path `/etc/nats-server.conf`, listener `tls://89.233.104.250:4222`, HTTP monitoring `127.0.0.1:8222`, JetStream store `/var/lib/nats/jetstream`.
  - Local M5 bot: `A2A_AGENT_ID=m5bot-local`, `A2A_RUNTIME_ID_MODE=runtime`, `A2A_PRODUCTION_SECURITY=false`, `NATS_TOKEN` set, TLS CA file set.
  - Remote d80 bot: `A2A_AGENT_ID=d80-chunbot`, `A2A_RUNTIME_ID_MODE=runtime`, `A2A_PRODUCTION_SECURITY=false`, `NATS_TOKEN` set, TLS CA file set.
- Rollout sequence:
  1. Keep the existing single-node NATS profile; do not introduce a three-node cluster.
  2. Build current binaries from the committed repository state.
  3. Replace the local M5 and remote d80 binaries with current builds, preserving env and DATA_DIR.
  4. Restart/drain each bot one at a time and verify `/doctor`/logs show A2A enabled and runtime transport consumers started.
  5. Run exact runtime delegation smoke from `m5bot-local-m5-main` to `d80-chunbot-d80-main`.
  6. Run legacy bot-level negative smoke and require rejection in `runtime` mode.
  7. Record evidence and leave rollback as binary/env restore or `NATS_URL=""`.
- Required safety constraints:
  - Do not enable `A2A_PRODUCTION_SECURITY=true` while using only `NATS_TOKEN`; that guard intentionally requires `NATS_CREDS_FILE`.
  - Do not expose NATS monitoring outside localhost.
  - Do not delete or reset DATA_DIR, JetStream store, or policy DBs during rollout.
  - If the single NATS node is down, pause new remote work and recover/restart NATS; do not force task DB cleanup.
- Lightweight rollout evidence:
  - Plan/docs commit: `8918fd7`.
  - Built current binaries: darwin sha256 `ea537fbdffee4dd957cd59aca42967422be9427f3eb22311ead3f2f9d886ebca`; linux amd64 sha256 `f947bff251947b2aa571e647004e0a0118cdeb79719e8fa0bc1782f042a6e8f8`.
  - Local M5 binary backup: `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-lightweight-rollout-20260731161704`; current binary sha256 `ea537fbdffee4dd957cd59aca42967422be9427f3eb22311ead3f2f9d886ebca`.
  - Remote d80 binary backup: `/opt/kiro-discord-bot/kiro-discord-bot.pre-lightweight-rollout-20260731162128`; current binary sha256 `f947bff251947b2aa571e647004e0a0118cdeb79719e8fa0bc1782f042a6e8f8`.
  - Local M5 service logs: `[a2a] NATS node enabled agent=m5bot-local`, `[a2a] transport consumers started agent=m5bot-local`, `Bot running as M5Bot#8313`.
  - Remote d80 service logs: `[a2a] NATS node enabled agent=d80-chunbot`, `[a2a] transport consumers started agent=d80-chunbot`, `Bot running as ChunBot#4533`.
  - Single-node NATS health: `mxp2` `nats-server.service` active with `/usr/sbin/nats-server -c /etc/nats-server.conf`.
  - Exact runtime smoke passed: `go run /tmp/a2a_smoke.go exact d80-chunbot-d80-main`; message `smoke_exact_1785486172`; terminal `TASK_STATE_COMPLETED`; task `task_692a120a3954415ea5e8801e`; executor `d80-chunbot-d80-main`.
  - Legacy rejection smoke passed: `go run /tmp/a2a_smoke.go legacy d80-chunbot`; message `smoke_legacy_1785486216`; terminal `TASK_STATE_REJECTED`; executor `d80-chunbot`; error `invalid_envelope: request target d80-chunbot does not match runtime d80-chunbot-d80-main`.
- Runtime settings touched: no env changes; existing runtime/token/TLS config preserved.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only; single-node NATS service was inspected but not restarted or modified.
- Rollback boundary: restore the recorded backup binary on the affected host or set `NATS_URL=""` and restart/drain the bot. DATA_DIR, policy DBs, and JetStream store were not migrated.
- Next phase: monitor internal lightweight A2A usage; defer hardened credential/HA rollout until `A2A_PRODUCTION_SECURITY=true` and NKey/JWT credentials are required.

### R9 MCP A2A peer runtime listing fix

- Status: deployed smoke passed; current commit.
- Decision: `bot_a2a_peers` must advertise callable channel runtimes, not stale bot-level host identities, in `A2A_RUNTIME_ID_MODE=runtime`. A channel runtime may have no live ACP subprocess because channel agents are closed for resource use; it is still callable when the bot process is online and the policy allows the runtime target, because inbound A2A admission calls `ensureWorkerForA2A` and wakes/creates the worker before enqueue.
- Changed files: `a2a/peer_store.go`, `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tool_register.go`, `internal/botmcp/a2a_tools_test.go`, and this progress ledger.
- Validation:
  - `go test ./internal/botmcp -run 'TestA2AToolsPeersRuntimeMode|TestDefaultSafeToolNames|TestA2AToolsAnnotations'` passed.
  - `go test ./...` passed.
  - `git diff --check` passed.
  - Live local peer probe after the fix lists `d80-chunbot-d80-main` with `runtime=channel`, `channelRef=d80-main`, `delegationAllowed=true`, `wakeable=true`, and skill `d80-main/task`; stale bot-level `d80-chunbot` is no longer marked callable.
  - Deployed live local M5 and remote d80 binaries from commit `9fbc228`; local installed binary sha256 after ad-hoc codesign `5153d5f95ce4deb8144c00e8aa730a6fa6cf8079818c51499d739e6d8a258a92`, remote installed linux amd64 sha256 `8d7ac59e04625158ddeff6ad5dba31f151852625c77fb4f27f01877cd0186261`.
  - Post-deploy exact runtime smoke passed: `go run /tmp/a2a_smoke.go exact d80-chunbot-d80-main`; message `smoke_exact_1785494918`; terminal `TASK_STATE_COMPLETED`; task `task_d55714506dceadfa2d80e1cb`; executor `d80-chunbot-d80-main`.
- Done criteria evidence:
  - `A2APeerSummary` now includes `runtime`, `channelRef`, and `wakeable`.
  - Runtime mode peer visibility only marks exact `delegate_targets.runtime_agent_id` matches callable; legacy `delegate_to` migration fields no longer make bot-level host cards appear callable.
  - Peer trust summaries preserve extended card runtime/channel metadata from peer card storage.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-peers-fix-20260731184705` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-peers-fix-20260731184757` on d80; runtime rollout remains usable, but `bot_a2a_peers` would again risk showing legacy bot host cards as callable in runtime mode.
- Next phase: monitor live MCP peer output during normal delegation; no additional code phase is open.

### R10 default-enabled A2A MCP tool surface

- Status: deployed smoke passed; current commit.
- Decision: all A2A MCP tools should be present in the default bot-tools allowlist, with safety enforced by each tool's bound Discord context, requester/manager checks, confirmation tokens, task ownership checks, and policy gates rather than by making lifecycle tools unavailable.
- Changed files: `internal/botmcp/server.go`, `internal/botmcp/server_test.go`, and this progress ledger.
- Validation:
  - `go test ./internal/botmcp -run 'TestDefaultSafeToolNames'` passed.
  - `go test ./...` passed.
  - `git diff --check` passed.
  - Deployed live local M5 and remote d80 binaries from commit `eb4f205`; local installed binary sha256 after ad-hoc codesign `b70f978353b645a0bd51079875df5661d1d0b91e73ca807b408e76fca36cbfe8`, remote installed linux amd64 sha256 `54d9648f0c688b76ababf5831092d0b6ec144b8aeb6c056aa8856d4d338321f5`.
  - Post-deploy exact runtime smoke passed: `go run /tmp/a2a_smoke.go exact d80-chunbot-d80-main`; message `smoke_exact_1785495270`; terminal `TASK_STATE_COMPLETED`; task `task_ffad2ab130db68ce8e5a156c`; executor `d80-chunbot-d80-main`.
- Done criteria evidence:
  - Default A2A MCP allowlist includes `bot_a2a_peers`, `bot_a2a_policy_get`, `bot_a2a_task_status`, `bot_a2a_runtime_preflight`, `bot_a2a_policy_plan`, `bot_a2a_delegate`, `bot_a2a_policy_apply`, `bot_a2a_cancel`, `bot_a2a_input_reply`, and `bot_a2a_auth_reply`.
  - Destructive/open operations still keep their internal gates: `bot_a2a_policy_apply` requires ManageChannels plus fresh confirmation; cancel/input/auth continuation validate requester or manager/task state before publishing.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-a2a-default-20260731185317` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-a2a-default-20260731185403` on d80; A2A remains usable, but default channel setup would no longer expose the full MCP lifecycle surface.
- Next phase: monitor live A2A MCP usage; no additional code phase is open.

### R11 runtime peer identity and exact-policy trust

- Status: deployed smoke passed; current commit.
- Decision: runtime-mode delegation must trust exact `delegate_targets.runtime_agent_id` policy entries even when the peer-store row is not globally trusted. Global `trusted` remains required for legacy `delegate_to` rows so explicit revocation still blocks broad bot-host delegation.
- Changed files: `a2a/card.go`, `a2a/peer_store.go`, `a2a/card_phase4_test.go`, `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tools_test.go`, and this progress ledger.
- Validation:
  - `go test ./a2a ./internal/botmcp -run 'Test(BuildRuntimeAgentCardIncludesDiscordIdentifiers|ExtendedCard|A2AToolsDelegateAllowsUntrustedExactRuntimePolicy|A2AToolsDelegateRejectsRevokedPeerBeforePublishing|A2AToolsPeersRuntimeModeListsWakeableRuntimeAndHidesLegacyBot|DefaultSafeToolNames|A2AToolsAnnotations)'` passed.
  - `go test ./...` passed.
  - `git diff --check` passed.
  - Live `bot_a2a_peers` probe after M5/d80 deploy listed `d80-chunbot-d80-main` as `runtime=channel`, `channelRef=d80-main`, `delegationAllowed=true`, `wakeable=true`, `discordGuildId=1495737767827865620`, and `discordChannelId=1495737768905670719`; stale `d80-chunbot` remained non-callable.
  - Live MCP delegate probe from local M5 to `d80-chunbot-d80-main` completed with executor `d80-chunbot-d80-main` and terminal state `TASK_STATE_COMPLETED`.
- Done criteria evidence:
  - Runtime peer cards now include sanitized Discord guild/channel/thread identifiers in the extended card and expose them through `bot_a2a_peers`.
  - `bot_a2a_peers` preserves runtime/channel metadata, advertises `displayName`, and keeps stale bot-host rows non-callable in runtime mode.
  - `bot_a2a_delegate` allows untrusted runtime peer rows only when the bound policy has an exact runtime target; revoked legacy bot-host delegation still returns `policy_denied: target peer is not trusted`.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-identity-ids-20260731194653` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-identity-ids-20260731194740` on d80; runtime rollout remains usable, but `bot_a2a_peers` would again risk showing stale bot-host cards as callable and exact runtime target policies would still depend on global peer trust.
- Next phase: monitor live MCP peer output during normal delegation; no additional code phase is open.

### R12 exact runtime target channel_ref delivery fix

- Status: deployed smoke passed; current commit.
- Decision: when a delegation policy has an exact `delegate_targets.runtime_agent_id`, the outbound `TaskExecutionRequest.ChannelRef` must use that target row's `channelRef`, not the delegator channel policy's local `channel_ref`. Exact runtime policy authorization is not enough; the executor bot admits inbound work by `req.ChannelRef`.
- Changed files: `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tools_test.go`, and this progress ledger.
- Validation:
  - `go test ./internal/botmcp -run 'TestA2AToolsDelegateAllowsUntrustedExactRuntimePolicy|TestA2AToolsDelegateRejectsRevokedPeerBeforePublishing|TestA2AToolsDelegateConfirmationBindsDeliveryMode'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
  - Live no-`TargetChannelRef` MCP delegate smoke from local M5 to `d80-chunbot-d80-main` produced outbound `channel_ref=d80-main`, reached executor `d80-chunbot-d80-main`, and completed `TASK_STATE_COMPLETED`.
- Done criteria evidence:
  - Existing stuck row `local_c3a48e79afafd4fa375e35887a98d609` was already terminal rejected with `channel_not_enabled: channel_ref is not enabled`; it had `to_agent=d80-chunbot-d80-main`, `skill_id=d80-main/task`, but wrong outbound `channel_ref=m5-main`.
  - Regression coverage now omits `TargetChannelRef` in an exact-runtime policy and verifies the confirmation summary resolves to `peer-n100-support@support/support/task`, proving delivery uses the target policy channel ref.
  - Deployed smoke proves the previously wrong `channel_ref=m5-main` is now `channel_ref=d80-main` for the same M5→d80 exact runtime target path.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-channelref-fix-20260731205757` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-channelref-fix-20260731205840` on d80; exact-runtime delegation without explicit target channel ref would again publish with the local delegator channel ref and be rejected by the executor as `channel_not_enabled`.
- Next phase: monitor normal Discord-issued M5→d80 delegation; no additional code phase is open.

### R13 exact runtime target channel_ref normalization

- Status: deployed smoke passed; current commit.
- Decision: exact runtime target policy rows own the delivery channel ref even when the caller supplies a stale or Discord-derived `TargetChannelRef`. A concrete `runtimeAgentId` identifies one runtime authority; preserving a mismatched explicit channel ref produces executor-side `channel_not_enabled`.
- Changed files: `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tools_test.go`, and this progress ledger.
- Validation:
  - `go test ./internal/botmcp -run 'TestA2AToolsDelegateAllowsUntrustedExactRuntimePolicy|TestA2AToolsDelegateRejectsRevokedPeerBeforePublishing|TestA2AToolsDelegateConfirmationBindsDeliveryMode'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
  - Live explicit-stale-ref MCP delegate smoke with `TargetChannelRef=discord-1495737768905670719` normalized outbound `channel_ref=d80-main`, reached executor `d80-chunbot-d80-main`, and completed `TASK_STATE_COMPLETED`.
- Done criteria evidence:
  - Latest Discord-issued task `local_7a45f3132c345a71727a989509cbcfa8` was terminal rejected with `channel_not_enabled` because the tool call supplied/saved `channel_ref=discord-1495737768905670719` while targeting `d80-chunbot-d80-main`.
  - Regression coverage now supplies `TargetChannelRef=discord-channel-1` for an exact runtime target and verifies the resolved confirmation summary still uses the policy target `support`.
  - Deployed smoke proves the Discord-agent failure mode is fixed: a stale/derived explicit target ref no longer reaches the executor as `discord-1495737768905670719`.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-explicitref-fix-20260731210859` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-explicitref-fix-20260731210943` on d80; Discord-agent tool calls that pass stale/derived target channel refs would again bypass the policy target channel ref and be rejected by the executor.
- Next phase: monitor normal Discord-issued M5→d80 delegation; no additional code phase is open.

### R14 A2A task status source-of-truth guidance

- Status: deployed; current commit.
- Decision: A2A progress/status answers must use `bot_a2a_task_status` / TaskStore as the authoritative source. Audit timeline rows are historical evidence only and can show an earlier `queued` send event after the TaskStore has already reached terminal `rejected` or `completed`.
- Changed files: `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tool_register.go`, `internal/botmcp/a2a_tools_test.go`, `bot/handler.go`, and this progress ledger.
- Validation:
  - `go test ./internal/botmcp -run 'TestA2AToolsTaskStatusRequiresOwnerOrManager|TestA2AToolsDelegateAllowsUntrustedExactRuntimePolicy'` passed.
  - `go test ./bot -run 'TestBuildPrompt|TestSafeToolNamesIncludeA2A'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
  - Source probe against local TaskStore returned `TASK_STATE_REJECTED`, `terminal=true`, `errorCode=channel_not_enabled`, `errorMessage=channel_ref is not enabled`, and `channelRef=discord-1495737768905670719` for `local_7a45f3132c345a71727a989509cbcfa8`.
  - Source probe against local TaskStore returned `TASK_STATE_COMPLETED`, `terminal=true`, and `channelRef=d80-main` for deployed smoke task `local_c509145f0b59061c079ff30c33319e6c`.
  - Local M5 bot restarted and logged `NATS node enabled`, `transport consumers started`, and `Bot running as M5Bot#8313`.
  - Remote d80 bot service is `active` and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-statusfix-20260731211808` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-statusfix-20260731211853` on d80; agent prompt/tool descriptions would again be less explicit that audit rows are not authoritative for A2A task progress, and status summaries would omit `channelRef`.
- Next phase: monitor Discord-issued A2A status/progress questions; no additional code phase is open.

### R15 protocol coverage and status debuggability

- Status: deployed; current worktree.
- Decision: protocol validation now covers outbound ownership checks, forged executor rejection, duplicate delivery, continuation controls, bootstrap admission, replay, rate limiting, missing pool subjects, and NATS `Msg-Id` publication. A2A status output now exposes authoritative TaskStore identifiers plus the recent task event trail so operators can distinguish current state from older audit events.
- Changed files: `a2a/transport.go`, `a2a/integration_test.go`, `channel/a2a.go`, `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tool_register.go`, `internal/botmcp/a2a_tools_test.go`, `bot/a2a_commands.go`, `bot/handler.go`, `bot/handler_test.go`, `locale/lang/en.json`, `locale/lang/zh-TW.json`, and this progress ledger.
- Validation:
  - `gofmt -w a2a/transport.go a2a/integration_test.go channel/a2a.go internal/botmcp/a2a_tools.go internal/botmcp/a2a_tool_register.go internal/botmcp/a2a_tools_test.go bot/a2a_commands.go bot/handler_test.go` passed.
  - `go test ./a2a -run 'TestA2AIntegration(TargetedDelegation|DuplicateDelivery|CancelOwnership|EventOwnershipRejectsForgedExecutor|PreAcceptRejectRequiresOutboundTargetExecutor|ContinuationControlsPublishStatus|AcceptedBootstrap|AdmissionBeforeExecution|AckAfterAdmissionNotCompletion|ReplayAfterReconnect|EventRate|NoErrorEnvelope|NoPoolSubject|TransportPublisherDeclaresNatsMsgID)'` passed.
  - `go test ./internal/botmcp -run 'TestA2ATools(TaskStatusRequiresOwnerOrManager|DelegateAllowsUntrustedExactRuntimePolicy|DelegateRejectsRevokedPeerBeforePublishing|DelegateConfirmationBindsDeliveryMode)'` passed.
  - `go test ./bot -run 'TestA2A(FormatTaskResponseIsHumanReadable|TaskOptionAcceptsDisplayedLocalID|TaskOptionAcceptsDiscordMessageID|SetupSlashDefaultsHumanFlow|SetupSlashAutoCrossRuntimeUsesProxy|TranscriptModeSafeClearsContextSharing)'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - Local M5 bot restarted and logged `NATS node enabled`, `transport consumers started`, and `Bot running as M5Bot#8313`.
  - Remote d80 bot service is `active` and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-protocol-fix-20260731214016` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-protocol-fix-20260731214058` on d80; A2A protocol edge-case tests and richer status summaries would be absent.
- Next phase: monitor Discord-issued A2A delegation/status questions; no additional code phase is open.

### R16 strict A2A/Discord review

- Status: deployed review fixes; continuation flow remains blocked.
- Decision: prior R15 validation was test-heavy but not a full protocol-semantic review. Strict review found one blocking A2A continuation design gap: `input_reply`/`auth_reply` currently advance executor-side state to `TASK_STATE_WORKING` and publish a status, but there is no executor continuation contract that resumes the original job with the supplied payload or handles auth denial. Do not claim continuation support until the executor/manager lifecycle is redesigned and covered end to end.
- Fixed during review: accepted-event bootstrap now validates the outbound row's delegator/target before binding; TaskStatus manager reads are scoped to the task channel and guild; MCP `bot_a2a_task_status` no longer trusts caller-supplied `manage_channels`; task-id/message-id ambiguous lookups return an error instead of silently selecting one row; slash task options preserve unprefixed remote task IDs and rely on service-side message-id fallback; status event detail rendering escapes markdown/mentions and collapses newlines to prevent spoofed Discord rows.
- Validation:
  - `go test ./a2a -run 'TestA2AIntegration(AcceptedEventRequiresOutboundDelegator|EventOwnershipRejectsForgedExecutor|PreAcceptRejectRequiresOutboundTargetExecutor|ContinuationControlsPublishStatus)'` passed.
  - `go test ./internal/botmcp -run 'TestA2ATools(TaskStatusRequiresOwnerOrManager|TaskStatusManagerCannotCrossGuild|TaskStatusManagerCannotCrossChannel|TaskLookupRejectsTaskMessageAmbiguity)'` passed.
  - `go test ./bot -run 'TestA2A(FormatTaskResponseIsHumanReadable|TaskOptionAcceptsDisplayedLocalID|TaskOptionKeepsUnprefixedValueInTaskIDField)'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
  - Local M5 bot restarted and logged `NATS node enabled`, `transport consumers started`, and `Bot running as M5Bot#8313`.
  - Remote d80 bot service is `active` and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-review-fix-20260731215842` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-review-fix-20260731215933` on d80, then revert the R16 code/docs edits; this would re-open the accepted-bootstrap delegator mismatch, status manager overread, caller-forged status manager flag, ambiguous lookup, unprefixed task ID regression, and Discord status spoofing hazards.
- Next phase: design and implement real continuation execution (`input_reply`/`auth_reply`) before advertising or relying on those controls as protocol-complete.

### R17 continuation execution contract

- Status: deployed.
- Decision: `input_reply` and approved `auth_reply` now resume the same durable task by creating an explicit `A2AContinuation` on the resumed admission, enqueueing a same-task manager job with the continuation payload in the prompt, and using a per-control-message started key to avoid duplicate executor resumes on JetStream redelivery. `auth_reply` with `approve=false` is terminal and publishes a failed result with `auth_not_satisfied` without rerunning the executor.
- Changed files: `a2a/executor.go`, `a2a/transport.go`, `a2a/integration_test.go`, `channel/a2a.go`, `channel/a2a_phase5_test.go`, and this progress ledger.
- Validation:
  - `go test ./a2a -run 'TestA2AIntegration(ContinuationControlsPublishStatus|AuthDenyCompletesAsFailed|AcceptedEventRequiresOutboundDelegator)'` passed.
  - `go test ./channel -run 'TestA2A(PromptContainsContinuationPayload|PromptContainsPayloadWithoutDiscordEgress|ManagerA2AInputRequired|ManagerA2AAuthRequired)'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
  - Local M5 bot restarted and logged `NATS node enabled`, `transport consumers started`, and `Bot running as M5Bot#8313`.
  - Remote d80 bot service is `active` and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`.
  - Controlled live NATS continuation smoke passed against the deployed NATS fabric using isolated smoke runtime IDs `smoke-delegator-d2de5526` and `smoke-executor-d2de5526`: `input_reply` resumed the same task to `TASK_STATE_COMPLETED` at revision 4 with executor runs=2; `auth_reply approve=false` completed as `TASK_STATE_FAILED` with `auth_not_satisfied` and executor runs=1.
  - Smoke durable consumer cleanup verified for both isolated smoke runtime IDs; no extra consumers needed deletion.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-continuation-fix-20260731220901` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-continuation-fix-20260731220948` on d80, then revert the R17 files; continuation controls would again not have a real executor resume path.
- Next phase: Discord UI smoke only when a real Discord-issued task naturally reaches input/auth-required; protocol-level live continuation verification is complete.

### R18 executor-owned Discord conversation

- Status: deployed.
- Decision: accepted remote A2A tasks now use the executor bot's normal Discord worker path instead of inline/proxy-only execution. The executor starts an unbound public thread in its configured channel for the initial task, stores that thread in durable `DiscordContextJSON`, reuses it for `input_reply`/`auth_reply` continuations, posts progress/final output with the normal metrics footer, and still records durable A2A status/result back to the delegator. Separate bot-tools egress remains policy-gated.
- Changed files: `a2a/task_store.go`, `bot/bot.go`, `channel/a2a.go`, `channel/manager.go`, `channel/worker.go`, `channel/a2a_phase5_test.go`, `docs/a2a-nats-implementation-guide.md`, `docs/a2a-nats-integration-spec.md`, and this progress ledger.
- Validation:
  - Strict review found two blockers: fresh executor conversations must not use the A2A message ID as a Discord message anchor, and A2A thread-creation failure must release the worker. Both are fixed: A2A jobs now create standalone executor channel threads and `TestManagerA2AThreadCreationFailureReleasesWorker` covers cleanup.
  - `go test ./channel -run 'TestManagerA2A(ThreadCreationFailureReleasesWorker|StartsDiscordConversationWithMetrics|ContinuationReusesExecutorConversationThread)|TestA2APrompt'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
  - Built `/tmp/kiro-discord-bot-darwin-a2a-ux` (`f170dd67e2d0f6037357f60b7e6dd5ef589c7e6adacff6e5dcce19ae762c72c8`) and `/tmp/kiro-discord-bot-linux-a2a-ux` (`dbc3e25571f91ab9cb3d03af7f83fb77900e093a7ea442b22d22bb08d5a49a93`).
  - Local M5 bot restarted and logged `NATS node enabled`, `transport consumers started`, and `Bot running as M5Bot#8313`.
  - Remote d80 bot service is `active` and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`.
  - Live A2A UX smoke `msg_a2a_ux_a6929a1a` from `m5bot-local-m5-main` to `d80-chunbot-d80-main` completed: remote task `task_0ab8fa183d2ac7e73ae36cc5` reached `TASK_STATE_COMPLETED` revision 2, stored executor thread `1532767178611491016`, and audit recorded `agent_response_sent` success in that thread with content `A2A UX smoke OK a6929a1a`.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-a2a-ux-fix-20260731230551` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-a2a-ux-fix-20260731230636` on d80, then revert the R18 files above. Remote tasks would return to durable/proxy-only delivery instead of executor-owned Discord transcript creation.
- Next phase: monitor a natural Discord-issued A2A task that reaches input/auth-required; protocol continuation and executor-owned initial transcript smoke are complete.

### R19 proxy result duplicate suppression

- Status: deployed.
- Decision: executor-owned A2A conversations make the executor thread the only user-facing final transcript for `result_visibility=proxy` and `discord_transcript_mode=delegator`. The delegator still stores durable status/result for queries, but must not enqueue a second full `A2A result from ...` channel message. `transparent`, `mirror`, and `co_present` text delivery semantics remain available for explicit egress/mirroring modes.
- Changed files: `channel/a2a.go`, `channel/a2a_phase8_test.go`, and this progress ledger.
- Validation:
  - `go test ./channel -run 'TestA2A(ProxyDelegatorResultDoesNotDuplicateExecutorTranscript|TransparentResultQueuesSafeEgressAndAudit|MirrorTranscriptQueuesStatusLabel)'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
  - Built `/tmp/kiro-discord-bot-darwin-a2a-dedupe` (`2a45123bb8da4b702a2c46f5695f7aefe8b54426375c9a05ed530f8d5c846c0f`) and `/tmp/kiro-discord-bot-linux-a2a-dedupe` (`c886d5d2a2b29b1f83baba40c37f567ca7132ba259d0226939b03db6b29c5ee5`).
  - Local M5 bot restarted and logged `NATS node enabled`, `transport consumers started`, and `Bot running as M5Bot#8313`.
  - Remote d80 bot service is `active` and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`.
  - Live delegated dedupe smoke `msg_a2a_dedupe_7346d00c` completed from `m5bot-local-m5-main` to `d80-chunbot-d80-main`: local outbound and remote inbound rows reached `TASK_STATE_COMPLETED` revision 2; remote executor thread `1532771856149512292` recorded `agent_response_sent` success with content `A2A dedupe OK 7346d00c`; local M5 audit had zero delivery events for that message, confirming no duplicate delegator channel post.
- Runtime settings touched: no.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-a2a-dedupe-fix-20260731232516` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-a2a-dedupe-fix-20260731232602` on d80, then revert the R19 files above. Delegator proxy result messages would again be emitted for executor-owned tasks.
- Next phase: observe the original joke-review flow once more from Discord; automated and live delegated smoke now cover the duplicate channel-post regression.


### R20 co-present shared-thread delivery

- Status: deployed.
- Decision: when a delegated runtime card resolves to the same Discord guild/channel as the caller, `bot_a2a_delegate` now selects `result_visibility=transparent` + `discord_transcript_mode=co_present`, attaches verified Discord context to the transport envelope, and lets the executor bot post the final answer in the existing shared Discord thread. `bot_a2a_task_status` remains authoritative for state, but omits result text for transparent/co-present tasks so the delegator MCP caller cannot duplicate the executor's user-facing final.
- Changed files: `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tool_register.go`, `internal/botmcp/a2a_tools_test.go`, `channel/a2a.go`, `channel/a2a_phase5_test.go`, and this progress ledger.
- Validation:
  - `go test ./channel ./internal/botmcp -run 'TestManagerA2ACoPresentInitialTaskUsesSharedDiscordThread|TestManagerA2AContinuationReusesExecutorConversationThread|TestManagerA2AThreadCreationFailureReleasesWorker|TestA2AToolsTaskStatusOmitsCoPresentResultText|TestA2AToolsAutoDeliveryUsesCoPresentForSameDiscordRuntime|TestA2AToolsTaskStatusRequiresOwnerOrManager'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./a2a ./channel ./bot ./internal/botmcp -run 'Test.*A2A|TestA2ATools|TestManagerA2A|TestBuildA2APrompt'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
  - Built `/tmp/kiro-discord-bot-darwin-a2a-copresent` (`411bf96d944b6e5a062ec95b339373e33212d8b284d8c02f46d342d2acf6cbd3` before local codesign) and `/tmp/kiro-discord-bot-linux-amd64-a2a-copresent` (`82f9ee12ff90a00cbc884927013b6c1abb635e04e2a6eaa4177d8ca6da9ae204`).
  - Local M5 bot restarted and logged `NATS node enabled`, `transport consumers started`, and `Bot running as M5Bot#8313`.
  - Remote d80 bot service is `active` and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`.
  - Live co-present smoke `msg_a2a_copresent_cc226e3e` from `d80-chunbot-d80-main` to `m5bot-local-m5-main` reached `TASK_STATE_COMPLETED` revision 2; local inbound `discord_context_json` preserved the supplied shared thread `1532771856149512292`; local audit recorded exactly one `agent_response_sent` in that thread with content `A2A co-present OK cc226e3e`.
- Runtime settings touched: local `m5-main` and remote `d80-main` A2A policies were set to `result_visibility=transparent`, `discord_transcript_mode=co_present`, and `share_discord_context=1`.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service. A transient remote deploy used a Linux arm64 binary on x86_64 and failed with systemd `203/EXEC`; it was immediately replaced by the amd64 build above and the service is active.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-a2a-copresent-json-fix-20260731235306` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-a2a-copresent-json-fix-20260731235352` on d80, revert the R20 files above, and set both live policies back to `result_visibility=proxy`, `discord_transcript_mode=delegator`, `share_discord_context=0`. Co-present same-thread final replies and status-result redaction would be disabled.
- Next phase: run one natural Discord-issued ChunBot→M5 delegation in the shared thread to confirm the model-facing MCP path no longer restates the executor result after `bot_a2a_task_status`.

### R21 co-present delegator result post suppression

- Status: deployed.
- Trigger: natural Discord-issued ChunBot→M5 joke review used `result_visibility=transparent` and `discord_transcript_mode=co_present`; M5 posted the review in the thread, but d80 still emitted a channel-level `A2A result from m5bot-local-m5-main ...` because `channel.deliverA2AEvent` treated `co_present` as an explicit delegator text-delivery mode.
- Decision: `co_present` means the executor owns the shared Discord transcript. Delegator transport consumers still persist outbound task status/result, but must not enqueue result text to bot egress. `mirror` remains the explicit delegator transcript mode; transparent non-co-present results still use safe egress.
- Changed files: `channel/a2a.go`, `channel/a2a_phase8_test.go`, and this progress ledger.
- Validation:
  - `go test ./channel -run 'TestA2A(CoPresentTransparentResultDoesNotDuplicateExecutorTranscript|ProxyDelegatorResultDoesNotDuplicateExecutorTranscript|TransparentResultQueuesSafeEgressAndAudit|MirrorTranscriptQueuesStatusLabel)'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
  - Built `/tmp/kiro-discord-bot-darwin-a2a-copresent-dedupe` (`1b645a35f65afe8e6570c185f1118c7c8543c6cef0057c4eb91f34bfe15e14da` before local codesign) and `/tmp/kiro-discord-bot-linux-amd64-a2a-copresent-dedupe` (`629e1aaa29477228aced31a25455b4a7debb7ed70f39d81dee87ba798f9be49f`).
  - Local M5 bot restarted and logged `NATS node enabled`, `transport consumers started`, and `Bot running as M5Bot#8313`.
  - Remote d80 bot service is `active` and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`.
  - Live remote-delegator smoke `msg_a2a_copresent_dedupe_56083689` created a real d80 outbound task row and m5 inbound task row; both reached `TASK_STATE_COMPLETED` revision 2. Local M5 audit recorded one executor `agent_response_sent` in thread `1532779483545862414` with content `A2A co-present dedupe OK 56083689`; remote d80 audit recorded zero `a2a_transcript_posted`/`a2a_result_delivered` rows for that message.
- Runtime settings touched: no additional policy changes beyond R20.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-a2a-copresent-dedupe-fix-20260801000414` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-a2a-copresent-dedupe-fix-20260801000454` on d80, then revert the R21 files above. Co-present executor replies would again risk duplicate delegator channel result posts.
- Next phase: rerun the original Discord joke-review interaction once from the user side; expected observable result is one M5 review in the shared thread and no `A2A result from ...` ChunBot channel post.

### R22 co-present UX guardrails and status source hardening

- Status: deployed.
- Trigger: after R21, Discord UX still exposed confusing seams: policy display did not show whether all co-present gates were aligned, `bot_a2a_delegate`/`bot_a2a_task_status` descriptions did not make the no-follow-up rule explicit enough, and audit metadata did not consistently identify the Discord parent/thread target for A2A delivery diagnostics.
- Decision: make co-present an explicit operator-visible contract. `/a2a policy` now reports same-thread readiness plus legacy/runtime co-present allowlists; MCP tool descriptions and the main bot prompt require `bot_a2a_task_status` as the authoritative progress source and prohibit reposting transparent/co_present executor results; delegate audit metadata now carries `discord_target_id`, `discord_parent_channel_id`, and `discord_thread_id`.
- Changed files: `a2a/audit.go`, `a2a/transport.go`, `bot/a2a_commands.go`, `bot/handler.go`, `bot/handler_test.go`, `channel/a2a.go`, `internal/botmcp/a2a_tool_register.go`, `internal/botmcp/a2a_tools.go`, `internal/botmcp/a2a_tools_test.go`, `locale/lang/en.json`, `locale/lang/zh-TW.json`, and this progress ledger.
- Validation:
  - `go test ./bot -run 'TestA2APolicyFormatterIncludesPolicyMutationFields|TestManagerA2ACoPresentInitialTaskUsesSharedDiscordThread|TestManagerA2AContinuationReusesExecutorConversationThread|TestManagerA2AThreadCreationFailureReleasesWorker'` passed.
  - `go test ./internal/botmcp -run 'TestA2AToolsTaskStatusOmitsCoPresentResultText|TestA2AToolsAutoDeliveryUsesCoPresentForSameDiscordRuntime|TestA2AToolsAuditDiscordFieldsUsesThreadTarget|TestA2AToolsTaskStatusRequiresOwnerOrManager|TestA2AToolsRuntimePreflight'` passed.
  - `go test ./channel -run 'TestA2A\(CoPresentTransparentResultDoesNotDuplicateExecutorTranscript|ProxyDelegatorResultDoesNotDuplicateExecutorTranscript|MirrorTranscriptQueuesSafeEgressAndAudit|TransparentResultQueuesSafeEgressAndAudit|SharedContextAcceptsRuntimeAllowedByPolicy\)'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
  - Built `/tmp/kiro-discord-bot-darwin-a2a-ux-fix` (`9a9e250549268dff301a97d396cb2251c897f7680d43f9df7bfe50c7b0eabdb5` before local codesign), `/tmp/kiro-discord-bot-linux-arm64-a2a-ux-fix` (`f276a82d5c6fb1224d9cab3f769cfffaa210fcd6dd3b8c18cdca735a20f691b5`, wrong arch for d80), and `/tmp/kiro-discord-bot-linux-amd64-a2a-ux-fix` (`6f76c5f5635ccc2a2957c4dacfc275b88b2d3eed113b132c1042cc33fa4c8227`, deployed to d80).
  - Local M5 bot restarted and logged `NATS node enabled`, `transport consumers started`, and `Bot running as M5Bot#8313`.
  - Remote d80 bot service is `active` and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`. A transient redeploy attempted the arm64 binary on x86_64 and failed with systemd `203/EXEC`; it was immediately replaced by the amd64 build and the service is active.
  - Live co-present smoke `msg_a2a_copresent_final_f93c3248` reached `TASK_STATE_COMPLETED` revision 2 with `result_visibility=transparent`, `discord_transcript_mode=co_present`, and `discord_context_json` preserving thread `1532779483545862414`. Local audit recorded one executor `agent_response_sent` in that thread with content `A2A co-present final OK f93c3248`.
- Runtime settings touched: no additional policy changes beyond R20.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-a2a-ux-final-20260801002622` locally or `/opt/kiro-discord-bot/kiro-discord-bot.pre-a2a-ux-final-20260801002715` on d80, then revert the R22 files above. Co-present delivery would continue working from R21, but policy readiness display, stronger MCP/prompt guardrails, and new Discord audit target fields would be removed.
- Next phase: observe one natural Discord-issued delegation that asks for status/progress after completion; expected result is `bot_a2a_task_status` reports terminal state without reposting the executor's transparent/co_present result.

### R23 runtime peer identity cutover

- Status: implemented, not deployed.
- Trigger: `/a2a peers` still exposed bot-host identity alongside runtime identity, and runtime IDs/display names were effectively tied to bot-specific manual refs such as `m5-main`/`d80-main` instead of Discord channel runtime aliases.
- Decision: treat `A2A_AGENT_ID` as a bot prefix/credential owner only. Runtime mode peer UX now hides bot host cards and shows only Discord channel/thread runtime cards for the current guild. Runtime aliases are generated from explicit `channel_ref`, then Discord channel metadata name plus stable disambiguating hash, then a `ch-<hash>` fallback. Snowflake-like raw digits force hash fallback. Pre-release runtime ID rewrites cascade policy references.
- Changed files: `a2a/runtime.go`, `a2a/card.go`, `a2a/peer_store.go`, `a2a/policy_store.go`, `bot/a2a_discovery.go`, `bot/a2a_commands.go`, `internal/botmcp/a2a_tools.go`, tests, locale JSON, `docs/a2a-nats-integration-spec.md`, `docs/a2a-nats-implementation-guide.md`, and this progress ledger.
- Review finding fixed: strict reviewer flagged snowflake leakage in mixed aliases, stale policy references after forced runtime ID rewrites, and duplicate metadata-derived aliases. R23 now hashes mixed-snowflake aliases, rewrites stored policy references when a runtime ID changes, and adds a stable short hash suffix to metadata-derived channel aliases.
- Validation:
  - `go test ./a2a ./internal/botmcp -run 'TestRuntimeAliasUsesChannelNameOrStableHash|TestPolicyStoreValidationAndPersistence|TestA2AToolsDefaultPolicyUsesDiscordChannelNameAlias|TestA2AToolsPeersRuntimeModeListsWakeableRuntimeAndHidesLegacyBot'` passed.
  - `go test ./a2a -run 'TestBuildRuntimeAgentCardIncludesDiscordIdentifiers|TestRuntimeAliasUsesChannelNameOrStableHash|TestRuntimeCutoverReadiness|TestPolicyStore'` passed.
  - `go test ./bot -run 'TestRuntimePeerCardUsesRuntimeIDAndPolicySkills|TestA2APeersFormatterShowsRuntimeContext|TestA2APolicyFormatterIncludesPolicyMutationFields'` passed.
  - `go test ./internal/botmcp -run 'TestA2AToolsPeersRuntimeModeListsWakeableRuntimeAndHidesLegacyBot|TestA2AToolsDefaultPolicyUsesDiscordChannelNameAlias|TestA2AToolsPolicyPlanPolicyApply|TestA2AToolsPolicySetupDefaultsRuntimeTarget'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - `git diff --check` passed.
- Runtime settings touched: no live env/policy DB changes.
- Deployment hosts touched: none.
- Rollback boundary: revert the R23 files above. Runtime mode would again show bot host rows in peers output and default runtime IDs would derive from manual `channel_ref`/`discord-<channel_id>` rather than channel-name aliases/hash fallback.
- Next phase: commit the reviewed pre-release runtime peer identity refactor, then run a local `/a2a peers` UX smoke before any deployment.

### R24 local peers UX smoke

- Status: local smoke passed; not remotely deployed.
- Trigger: follow R23 next phase before deployment.
- Decision: install the R23 commit binary locally only and smoke the live local A2A peer data through `A2AService.Peers`, matching `/a2a peers` runtime-mode filtering rules without mutating live policy DB rows.
- Changed files: this progress ledger only.
- Validation:
  - Built `/tmp/kiro-discord-bot-darwin-r23-peers-smoke` with SHA-256 `ed707ff21287b04ca5b976e3c9a2e03245d1ddea2a1f831eb3d30924c4f58850`.
  - Local M5 bot restarted from `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot` and logged `NATS node enabled`, `transport consumers started`, and `Bot running as M5Bot#8313`.
  - Live peer DB before the smoke contained bot host rows `m5bot-local`/`d80-chunbot` plus runtime rows `m5bot-local-m5-main`/`d80-chunbot-d80-main`.
  - Temporary live smoke test `TestLivePeersUXSmoke` passed. Returned peers were only Discord `channel` runtime rows in guild `1495737767827865620`: `d80-chunbot-d80-main` with `delegationAllowed=true` and skill `d80-main/task`, plus `m5bot-local-m5-main` with one hidden skill and `delegationAllowed=false`; bot host rows did not appear.
- Runtime settings touched: no env/policy DB changes.
- Deployment hosts touched: local M5 bot binary/service only.
- Rollback boundary: restore `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-r23-peers-smoke-20260801011249` and restart `local-kiro-bot`.
- Next phase: if acceptable, build the linux-amd64 R23 binary for d80, backup remote binary, deploy, restart, then repeat the same `/a2a peers` runtime-only smoke against d80.

### R25 d80 peers runtime deployment

- Status: deployed to d80.
- Trigger: R24 local smoke passed and remote d80 needed the same runtime-only peers behavior.
- Decision: deploy the reviewed R23 binary to d80, keep current policy DB unchanged, and validate peers UX with a temporary smoke binary that calls `A2AService.Peers` against d80 data.
- Changed files: this progress ledger only.
- Validation:
  - Built `/tmp/kiro-discord-bot-linux-amd64-r23-peers` with SHA-256 `bfe8f689cd01ca449c69f8f91b23526264a16aac6da61e1c190aa725915dead0`.
  - Installed it to `/opt/kiro-discord-bot/kiro-discord-bot`; remote installed hash matched `bfe8f689cd01ca449c69f8f91b23526264a16aac6da61e1c190aa725915dead0`.
  - d80 `kiro-discord-bot.service` restarted and was `active`.
  - d80 logs after restart showed `NATS node enabled agent=d80-chunbot`, `transport consumers started agent=d80-chunbot`, `Bot running as ChunBot#4533`, peer discovery, and `/a2a` slash registration.
  - Temporary smoke binary `/tmp/a2a-peers-smoke-linux-amd64` SHA-256 `fc1273e1f9c55fb7a9c445e14eba55184afae1ddf30c5bba0af4781299c9a9d2` passed when run with sudo against `/home/chun/kiro-discord-bot/data`; non-sudo failed with readonly SQLite access and did not complete the smoke.
  - Smoke returned only Discord `channel` runtime rows in guild `1495737767827865620`: `d80-chunbot-d80-main` with one hidden local skill and `delegationAllowed=false`, plus `m5bot-local-m5-main` with `delegationAllowed=true` and skill `m5-main/task`; bot host rows did not appear.
- Runtime settings touched: no env/policy DB changes.
- Deployment hosts touched: remote d80 bot binary/service.
- Rollback boundary: restore `/opt/kiro-discord-bot/kiro-discord-bot.pre-r23-peers-20260801011749` on d80 and restart `kiro-discord-bot.service`.
- Next phase: observe one natural Discord `/a2a peers` or delegated peer-selection interaction from the user side and confirm the visible Discord response matches the smoke output: runtime rows only, channel context present, no bot host cards.

### R26 channel metadata runtime registration fix

- Status: deployed locally and to d80.
- Trigger: real `/a2a peers` output still showed `m5bot-local-m5-main` for the same Discord channel where `d80-chunbot` showed `隨口問`, and other Discord channels were absent unless they already had enabled A2A policy rows.
- Decision: make Discord channel metadata authoritative for runtime mode. Existing enabled policies are normalized from channel metadata at startup, metadata-only top-level Discord channels publish runtime cards with no skills, malformed local channel metadata with one trailing brace is tolerated/repaired, and `/a2a peers` hides superseded old channel aliases when a canonical metadata alias exists. Same-channel old delegate targets continue to authorize the new runtime alias during migration.
- Changed files: `bot/a2a_discovery.go`, `channel/manager.go`, `internal/botmcp/a2a_tools.go`, `internal/channelmeta/store.go`, tests, `docs/a2a-nats-integration-spec.md`, `docs/a2a-nats-implementation-guide.md`, and this progress ledger.
- Validation:
  - `go test ./internal/channelmeta ./channel ./bot ./internal/botmcp -run 'TestReadToleratesSingleTrailingBrace|TestA2AKnownRuntimePoliciesNormalizeMetadataAndIncludeInactiveChannels|TestRuntimePeerCardForInactiveMetadataChannelHasNoSkills|TestRuntimePeerCardUsesRuntimeIDAndPolicySkills|TestA2APeersFormatterShowsRuntimeContext|TestA2AToolsPeersRuntimeModeListsWakeableRuntimeAndHidesLegacyBot'` passed.
  - `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...` passed.
  - Local channel metadata file was repaired from backup `/Users/chun/Projects/kiro-discord-bot-local/data/channel_metadata.pre-r26-runtime-metadata-20260801013157.json`.
  - Final local binary `/tmp/kiro-discord-bot-darwin-r26-final` SHA-256 `815d41ef6400ffe0bf7660176c85c9a627a6af4c9710f29ae1dc6efe3610db98` was installed and `local-kiro-bot` restarted.
  - Final d80 binary `/tmp/kiro-discord-bot-linux-amd64-r26-final` SHA-256 `c3a23938803f3ad68ca72c31430ef929b1da1594a956b543592f2493c63a93bb` was installed to `/opt/kiro-discord-bot/kiro-discord-bot`; service restarted active and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`.
  - Final local smoke returned runtime rows for both bots across known channels; old `d80-main`/`m5-main` rows were hidden. Same-channel rows are `d80-chunbot-ch-2cbaf623` and `m5bot-local-ch-2cbaf623`, both display `隨口問`.
  - Final d80 smoke returned the same metadata-normalized runtime rows; `m5bot-local-ch-2cbaf623` is allowed with skill `ch-2cbaf623/task`, and old bot host / old alias rows are absent.
- Runtime settings touched: no env changes. Policy rows for the active `隨口問` channel were normalized to `channel_ref=ch-2cbaf623` and runtime IDs `m5bot-local-ch-2cbaf623` / `d80-chunbot-ch-2cbaf623`; old cross-bot delegate references remain readable and are migration-compatible at runtime.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service.
- Rollback boundary: restore local `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-r26-final-20260801013922` and d80 `/opt/kiro-discord-bot/kiro-discord-bot.pre-r26-final-20260801014013`, then restart both services. If needed, restore local metadata from `/Users/chun/Projects/kiro-discord-bot-local/data/channel_metadata.pre-r26-runtime-metadata-20260801013157.json`.
- Next phase: ask the user to rerun Discord `/a2a peers`; expected visible rows should include both `d80-chunbot-ch-2cbaf623` and `m5bot-local-ch-2cbaf623` under `隨口問`, plus other known channel runtimes such as `大廳`, `測試`, `idempiere-erp`, and `kanboard`, with no `m5-main` or `d80-main` rows.

### R27 delegated-source response label

- Status: deployed locally and to d80.
- Trigger: receiving bot replies in executor-owned A2A Discord threads needed an explicit, server-owned source ref so readers can see which runtime/channel delegated the task.
- Decision: add an immutable `OriginRuntimeRef` snapshot to task send payloads and durable task rows. `bot_a2a_delegate` fills it from the bound server/runtime context, not MCP caller input. The receiver validates `origin_runtime_ref.runtime_agent_id` against the envelope `from` agent, persists the snapshot, includes it in task summaries, and prefixes executor-owned Discord replies with `委託自：<display/channel> · <bot>`. Durable A2A result content remains unprefixed.
- Changed files: `a2a/admission.go`, `a2a/executor.go`, `a2a/store.go`, `a2a/task_store.go`, `a2a/transport.go`, `channel/a2a.go`, `channel/worker.go`, `internal/botmcp/a2a_tools.go`, tests, and this progress ledger.
- Validation:
  - Focused regression passed: `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./a2a ./channel ./internal/botmcp -run 'TestTaskStorePersistsOriginRuntimeRef|TestA2AToolsOriginRuntimeRefUsesServerBoundChannel|TestA2APromptAndReplyPrefixUseDelegatedFromLabel|TestManagerA2ARejectsSpoofedOriginRuntimeRef|TestA2AToolsDelegateAllowsUntrustedExactRuntimePolicy|TestA2AToolsBoundContextDelegateQuotaCancelInputReplyAuthReply|TestManagerA2AResultCapture'`.
  - Full regression passed: `env -u NATS_URL -u NATS_CREDS_FILE -u NATS_TOKEN -u NATS_TLS_CA_FILE -u A2A_AGENT_ID -u A2A_RUNTIME_ID_MODE -u A2A_AGENT_NAME -u A2A_AGENT_DESCRIPTION -u A2A_REQUIRE_CONFIRMATION_FOR_REMOTE go test ./...`.
  - LSP workspace diagnostics reported no issues.
  - Strict reviewer found one blocker: model output beginning with `委託自：` could suppress the server-owned prefix. Fixed by always applying the server prefix; reviewer recheck reported no remaining blockers.
  - Local binary `/tmp/kiro-discord-bot-darwin-r27-origin-ref` SHA-256 `d4a3c753957dc626e6644d97de92c5c3dc52593a7afb5cba1f8e777e0762aebb` was installed and `local-kiro-bot` restarted ready.
  - d80 binary `/tmp/kiro-discord-bot-linux-amd64-r27-origin-ref` SHA-256 `7d525854ae9383f917fae0647584ee8c841c96f4ef116be26238b7ae1f3969ad` was installed to `/opt/kiro-discord-bot/kiro-discord-bot`; service restarted active and logged `NATS node enabled`, `transport consumers started`, and `Bot running as ChunBot#4533`.
- Runtime settings touched: no env or policy changes. `tasks.sqlite` gets additive nullable-compatible column `origin_runtime_ref_json` with default `{}` on next store open.
- Deployment hosts touched: local M5 bot binary/service and remote d80 bot binary/service.
- Rollback boundary: restore local `/Users/chun/Projects/kiro-discord-bot-local/bin/kiro-discord-bot.pre-r27-origin-ref-20260801020923` and d80 `/opt/kiro-discord-bot/kiro-discord-bot.pre-r27-origin-ref-20260801021007`, then restart both services.
- Next phase: issue one real delegated A2A task in Discord and confirm the executor-owned reply starts with `委託自：隨口問 · <delegator bot>` while `/a2a status` keeps durable result content unprefixed.

## Master goal prompt

Use this prompt when starting or resuming the full implementation program:

```text
Resume A2A runtime-first implementation from repository state, not chat memory.

Authoritative files:
1. docs/a2a-nats-integration-spec.md
2. docs/a2a-nats-implementation-guide.md
3. docs/a2a-nats-implementation-progress.md

Rules:
- Read all three files before editing.
- Check git status before editing.
- Determine the single next runtime phase from docs/a2a-nats-implementation-progress.md.
- Verify the previous completed phase has validation evidence.
- Implement exactly one R phase.
- Do not start the next R phase until validation passes and the ledger is updated.
- Preserve source spec decisions exactly.
- Keep A2A disabled by default until rollout.
- Do not touch runtime .env files, DATA_DIR, Docker volumes, deployment hosts, or live services.
- Do not implement pool dispatch, public HTTP A2A endpoint, SSE streaming, HTTP push notification config, or gateway adapter.
- Run the current phase validation commands and targeted regressions required by touched seams.
- Update docs/a2a-nats-implementation-progress.md with status, validation evidence, notes, rollback boundary, and next phase.
- Final response must include changed files, validation evidence, rollback boundary, and next phase.
```

## Next runtime cutover readiness prompt

Use this prompt after committing policy-derived runtime authority:

```text
Start A2A runtime migration R6 cutover readiness validation only.

Read:
1. docs/a2a-nats-integration-spec.md
2. docs/a2a-nats-implementation-guide.md
3. docs/a2a-nats-implementation-progress.md
4. docs/a2a-nats-rollout.md

Validate exactly:
- policy-derived runtime authority remains the single local runtime owner; do not reintroduce `a2a_runtime_registry`.
- natural-language Discord UX remains primary; slash stays fallback/bootstrap/admin.
- `A2A_RUNTIME_ID_MODE=runtime` rejects new legacy bot-level `target_agent + target_channel_ref` asks.
- exact runtime-addressed delegation still works.
- rollback to `dual` or `legacy` is documented and tested.

Do not touch live `.env`, `DATA_DIR`, deployment hosts, Docker volumes, or services unless the operator explicitly requests a deployment smoke.

Required validation:
- `go test ./a2a ./channel ./internal/botmcp ./bot -run 'Test(RuntimeID|PolicyStore|Doctor.*A2A|RemoteMemoryWriteDenied|RemoteMemoryWriteAllowed|RuntimePeerCard|BuildBotA2APeerCard|A2ATools|ManagerA2A)'`
- `go test ./...`
- rollout/runtime-mode smoke evidence if operator authorizes deployment.
```
