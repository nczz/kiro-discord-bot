# A2A NATS Implementation Guide

> Status: implementation-ready guide.  
> Source spec: `docs/a2a-nats-integration-spec.md`.  
> Objective: provide exact, phase-ordered instructions so a coding agent can implement the A2A-like NATS binding without making architecture decisions, and prove each correctness/security boundary with explicit validation gates.

## 1. Final guide contract

The final implementation guide must be executable, not aspirational. Each section must contain:

1. **Intent**: one behavior or invariant to add.
2. **Touched files and symbols**: exact package, file, exported type/function, and test file.
3. **Preconditions**: existing state that must be true before editing.
4. **Change steps**: ordered edits, no hidden design choices.
5. **Validation**: exact command or smoke scenario, expected pass signal, and the bug it would catch.
6. **Rollback boundary**: what can be reverted independently if this phase fails.
7. **Cross-phase contract**: types, storage schema, subject grammar, or tool schema consumed by later phases.
8. **Done criteria**: observable behavior, not just compilation.

A section is not acceptable if it says only "implement X", "wire Y", "add tests", or leaves a schema/API choice to the implementing agent.

## 2. Source-of-truth decisions from the spec

The guide must preserve these decisions without reinterpretation:

- A2A-like custom NATS binding, not generic A2A HTTP compliance.
- NATS + JetStream internal transport; official A2A v1.0 canonical objects only where applicable.
- v1 supports targeted delegation only. Pool dispatch is explicitly deferred.
- No standalone cross-agent `error` envelope. Failures route through `rejected`, `task_status_update`, or `task_result` with `error_code`.
- Durable TaskStore and `Nats-Msg-Id` idempotency are correctness boundaries.
- All A2A ingress runs through channel runtime and `channel.Manager` boundaries.
- Proxy result delivery is default; remote Discord egress is disabled unless channel policy opts in.
- Natural-language Discord UX drives bot-tools; slash commands are fallback/bootstrap/admin shortcuts.
- `TASK_STATE_INPUT_REQUIRED` and `TASK_STATE_AUTH_REQUIRED` require explicit continuation flows.
- Safe egress, audit, AllowedMentions, secret redaction, channel permissions, and MCP policy proxy remain mandatory.

## 3. Existing project seams to anchor the guide

Grounded code seams identified from the current repository:

| Concern | Existing seam | Guide implication |
|---|---|---|
| Bot startup/config | `main.go`, `config.go`, `channel.ManagerConfig`, `channel.NewManager` | Add A2A config parsing before Manager construction; keep disabled path no-op. |
| Channel runtime | `channel.Manager`, `channel.Worker`, `channel.executeInline` | Add A2A execution entrypoint beside existing job flow, not bypassing worker/runtime policy. |
| MCP policy | `channel.OpenMCPPolicyStore`, `channel.ToACPServer`, `channel/mcp_policy.go` | A2A channel policy should use a dedicated SQLite store modeled after MCP policy durability, not metadata JSON. |
| Bot-tools UX | `internal/botmcp.NewServer` | Add A2A bot-tools here, following existing read/write/destructive annotations and bound context validation. |
| Safe egress | `bot.newSafeEgressTask`, `internal/botegress.WritePending`, `bot/safe_egress.go` | Result delivery and transparent/co-present posting must pass existing safe delivery boundaries. |
| Audit | `audit.EventFromPayload`, `audit.Store`, `bot/interaction_policy.go` | Add A2A audit event types and metadata without raw payload leakage by default. |
| Locale | `locale/lang/*.json`, `locale.Load`, `locale.Getf` | Add user-facing A2A messages as i18n keys, not raw English errors. |
| Health/doctor | `channel.Doctor`, `channel/doctor_env.go`, `/doctor` command | Add A2A runtime visibility without exposing tokens or NATS credentials. |
| Tests | `channel/*_test.go`, `internal/botmcp/*_test.go`, `bot/*_test.go`, `audit/*_test.go` | Place tests next to changed seams; no project-wide suite until final verification gate. |

## 4. Implementation matrices

### 4.1 File-change matrix

| Phase | Action | File | Symbols / contents | Validation anchor |
|---|---|---|---|---|
| 1 | Add | `a2a/types.go` | `AgentID`, `TaskID`, `MessageID`, `TaskState`, `ErrorCode`, `Envelope`, `DeliveryOptions`, `DiscordContext` | `go test ./a2a -run 'Test(A2ATypes|TaskState|ErrorCode)'` |
| 1 | Add | `a2a/subject.go` | `SubjectKind`, `ParseSubject`, `TaskSubject`, `ControlSubject`, `EventSubject`, slug validators | `go test ./a2a -run TestSubject` |
| 1 | Add | `a2a/envelope.go` | envelope validation, canonical type enum, payload size checks | `go test ./a2a -run TestEnvelope` |
| 1 | Add | `a2a/idempotency.go` | `TaskMsgID`, `ControlMsgID`, `EventMsgID`, duplicate key helpers | `go test ./a2a -run TestNatsMsgID` |
| 1 | Modify | `go.mod`, `go.sum` | `github.com/nats-io/nats.go`, `github.com/nats-io/nats-server/v2/server` for tests | `go test ./a2a` |
| 1 | Modify | `config.go`, `.env.example`, `channel/doctor_env.go` | A2A env parsing, production security guard, redacted doctor display | `go test ./channel -run TestDoctor.*A2A` |
| 2 | Add | `a2a/node.go` | connection lifecycle, JetStream handles, startup disabled path | `go test ./a2a -run TestNode` |
| 2 | Add | `a2a/streams.go` | stream/consumer setup for task/control/event only; no pool subjects | `go test ./a2a -run TestStreamSetup` |
| 2 | Add | `a2a/nats_test.go` | embedded nats-server helper | all A2A integration tests |
| 3 | Add | `a2a/task_store.go` | `a2a_tasks`, `a2a_events`, accepted bootstrap, terminal immutability | `go test ./a2a -run TestTaskStore` |
| 3 | Add | `a2a/peer_store.go` | peer card store, stale peer rules, trust display model | `go test ./a2a -run TestPeerStore` |
| 3 | Add | `a2a/policy_store.go` | channel A2A policy SQLite store and validation | `go test ./a2a -run TestPolicyStore` |
| 4 | Add | `a2a/card.go` | canonical/public vs internal/extended AgentCard sanitizer | `go test ./a2a -run TestAgentCard` |
| 5 | Modify | `channel/manager.go` | `AdmitA2ATask`, `RunA2ATask`, admission checks, worker enqueue | `go test ./channel -run TestManagerA2A` |
| 5 | Modify | `channel/worker.go` | A2A result capture path, proxy delivery suppression | `go test ./channel -run TestWorkerA2A` |
| 5 | Add | `a2a/executor.go`, `channel/a2a.go` | `Executor`, `TaskExecutionRequest`, `A2AAdmissionResult`, `TaskExecutionResult`, Manager adapter | `go test ./channel -run TestA2A` |
| 6 | Add | `a2a/transport.go` | task/control/event consumers and publishers | `go test ./a2a -run TestTransport` |
| 6 | Add | `a2a/integration_test.go` | two-node embedded JetStream scenarios | `go test ./a2a -run TestA2AIntegration` |
| 7 | Modify | `internal/botmcp/server.go` | `bot_a2a_*` tools with annotations and bound context validation | `go test ./internal/botmcp -run TestA2A` |
| 7 | Modify | `bot/commands.go`, `bot/interaction_policy.go` | slash fallback, buttons/modals, requester/manager checks | `go test ./bot -run TestA2A` |
| 7 | Modify | `locale/lang/en.json`, `locale/lang/zh-TW.json` | A2A i18n keys | `go test ./locale ./bot -run 'Test.*A2A.*Locale'` |
| 8 | Add | `a2a/object_store.go` | object refs, digests, retention cleanup | `go test ./a2a -run TestObjectStore` |
| 8 | Modify | `internal/botegress`, `bot/safe_egress.go` | safe result/artifact delivery handoff | `go test ./internal/botegress ./bot -run Test.*A2A.*Egress` |
| 8 | Modify | `audit/store.go`, `audit/recorder.go` | A2A event types and metadata redaction | `go test ./audit ./bot -run Test.*A2A.*Audit` |
| 9 | Modify | `docs/`, `scripts/`, `.env.example` | nats.conf examples, smoke runbook, rollout checklist | guide sanity and manual smoke |

### 4.2 Public type contract

The implementing agent MUST define these public contracts before wiring transport:

| Package | Type / function | Required fields or behavior |
|---|---|---|
| `a2a` | `type AgentID string` | validates `[A-Za-z0-9_-]{1,64}` |
| `a2a` | `type TaskID string` | validates `[A-Za-z0-9_-]{1,96}` |
| `a2a` | `type Envelope struct` | `Version`, `Binding`, `MessageID`, `From`, `To`, `Type`, `TaskID`, `Revision`, `Payload` |
| `a2a` | `func ParseSubject(string) (Subject, error)` | accepts task/control/event/card/heartbeat; rejects pool and unversioned subjects |
| `a2a` | `func ValidateEnvelope(Envelope, Subject) error` | enforces authenticated subject/envelope correspondence supplied by caller |
| `a2a` | `type TaskStore interface` | create outbound, admit inbound, bind accepted task, append event, terminal immutability |
| `a2a` | `type Executor interface` | `AdmitA2ATask(context.Context, TaskExecutionRequest) (A2AAdmissionResult, error)` and `RunA2ATask(context.Context, A2AAdmission) (TaskExecutionResult, error)`; transport depends on this interface, not on `channel` |
| `a2a` | `type TaskExecutionRequest` / `A2AAdmissionResult` / `A2AAdmission` / `TaskExecutionResult` | channel-neutral DTOs for remote task identity, durable admission key, channel refs, payload, delivery options, artifacts, state, errors, and metrics |
| `a2a` | `type PolicyStore interface` | get/set channel policy, validate inbound/outbound, audit-friendly diff |
| `channel` | `Manager.AdmitA2ATask` and `Manager.RunA2ATask` | only supported A2A ingress into channel runtime; compile-time implements `a2a.Executor`; execution starts only after accepted event is durably published |

### 4.3 NATS and storage contract

Allowed v1 subjects:

```text
a2a.v1.task.<from>.<to>.<messageId>
a2a.v1.control.<from>.<executor>.<taskId>.<kind>
a2a.v1.event.<executor>.<delegator>.<taskKey>.<kind>
a2a.v1.card.<agent>
a2a.v1.heartbeat.<agent>.<instance>
```

Forbidden v1 subjects:

```text
a2a.v1.pool.>
a2a.task.>
a2a.status.>
a2a.result.>
a2a.cancel.>
```

Every publisher MUST declare a stable `Nats-Msg-Id` before implementation:

| Publish path | `Nats-Msg-Id` |
|---|---|
| task | `task:<from>:<to>:<messageId>` |
| control | `control:<from>:<executor>:<taskId>:<kind>:<revision>` |
| pre-accept rejected | `event:<executor>:<delegator>:msg_<messageId>:rejected` |
| accepted | `event:<executor>:<delegator>:<taskId>:accepted` |
| status | `event:<executor>:<delegator>:<taskId>:status:<revision>` |
| artifact | `event:<executor>:<delegator>:<taskId>:artifact:<artifactId>:<revision>` |
| result | `event:<executor>:<delegator>:<taskId>:result` |

### 4.4 Bot-tools and Discord UX contract

| Tool | Permission | Required validation | Result |
|---|---|---|---|
| `bot_a2a_peers` | normal user | bound guild/channel context | redacted peer/skill list |
| `bot_a2a_policy_get` | normal user | bound channel context | user-friendly policy summary |
| `bot_a2a_task_status` | requester or manager | task belongs to channel/user scope | task state summary |
| `bot_a2a_policy_plan` | ManageChannels | diff is scoped to current channel | confirmation challenge |
| `bot_a2a_policy_apply` | ManageChannels | fresh confirmation token and idempotent `changeId` | applied policy diff |
| `bot_a2a_delegate` | normal user, policy-gated | outbound policy, egress labels, media policy, confirmation when required | task accepted/rejected status |
| `bot_a2a_cancel` | requester or manager | nonterminal task, known executor | cancel control published |
| `bot_a2a_input_reply` | requester or manager | `TASK_STATE_INPUT_REQUIRED`, nonce fresh, safe egress labels | input control published |
| `bot_a2a_auth_reply` | requester or manager | `TASK_STATE_AUTH_REQUIRED`, scoped confirmation, no raw long-lived credential | auth control published |

Slash fallback commands MUST call the same internal service methods; no separate policy path is allowed.


## 5. Executable implementation phases

Every phase below is a coding boundary. Do not start the next phase until its validation command passes and the done criteria are true.

### Phase 0: Readiness guard

**Intent**: prove the repository is ready for A2A work without changing behavior.

**Touched files/symbols**: none.

**Preconditions**:

- `docs/a2a-nats-integration-spec.md` and this guide are present.
- Worktree has no unrelated changes.

**Change steps**:

1. Read the source spec and this guide.
2. Confirm v1 remains targeted-only: `a2a.v1.pool.>` may appear only in forbidden-subject/rejection documentation and tests, never as an allowed stream subject, publisher, consumer, or implementation path.
3. Confirm no code has been edited yet.

**Validation**:

```bash
python - <<'PY'
from pathlib import Path
guide = Path('docs/a2a-nats-implementation-guide.md').read_text()
assert 'Forbidden v1 subjects' in guide and 'a2a.v1.pool.>' in guide
for forbidden in ['subject `a2a.v1.pool` is allowed', 'publish path | `pool', 'EnsureStreams` with subjects exactly: `a2a.v1.pool', 'PoolDispatcher']:
    assert forbidden not in guide, forbidden
assert 'standalone cross-agent `error` envelope' in guide
assert 'channel.Manager' in guide
print('a2a-guide-readiness-ok')
PY
```

**Expected result**: `a2a-guide-readiness-ok`.

**Bugs caught**: accidental reintroduction of deferred pool routing or non-channel ingress.

**Rollback boundary**: none.

**Cross-phase contract**: the source spec remains the authority for behavior; this guide controls implementation order.

**Done criteria**: validation passes and no implementation files changed.

### Phase 1: Foundation package and config

**Intent**: add inert A2A primitives and disabled-by-default configuration.

**Touched files/symbols**:

- Create `a2a/types.go`: `AgentID`, `TaskID`, `MessageID`, `TaskState`, `ErrorCode`, `EnvelopeType`.
- Create `a2a/subject.go`: `Subject`, `SubjectKind`, `ParseSubject`, `TaskSubject`, `ControlSubject`, `EventSubject`, `CardSubject`, `HeartbeatSubject`.
- Create `a2a/envelope.go`: `Envelope`, `ValidateEnvelope`.
- Create `a2a/idempotency.go`: `TaskMsgID`, `ControlMsgID`, `EventMsgID`.
- Create `a2a/errors.go`: canonical error constants including `unsupported_operation`.
- Create `a2a/*_test.go` for each file above.
- Modify `config.go`: add `A2AConfig` and env parsing.
- Modify `.env.example`: add A2A env block.
- Modify `channel/doctor_env.go`: add redacted A2A runtime overview.

**Preconditions**:

- No NATS connection is opened.
- `NATS_URL == ""` means A2A is disabled and existing bot behavior is unchanged.

**Change steps**:

1. Add `a2a` package with pure validation helpers only.
2. Implement subject parser accepting only task/control/event/card/heartbeat v1 subjects.
3. Make parser reject `a2a.v1.pool.>`, unversioned subjects, substring wildcards, empty tokens, PID-like agent IDs, and oversize task IDs.
4. Implement envelope validation for version, binding, known type, subject/envelope from-to correspondence, RFC3339 timestamps, future expiry, payload size, and task ID grammar.
5. Implement idempotency key helpers exactly as section 4.3 declares.
6. Add config envs: `NATS_URL`, `NATS_CREDS_FILE`, `NATS_TOKEN`, `NATS_TLS_CA_FILE`, `A2A_AGENT_ID`, `A2A_AGENT_NAME`, `A2A_AGENT_DESCRIPTION`, `A2A_TASK_TIMEOUT_SEC`, `A2A_MAX_DELEGATION_DEPTH`, `A2A_AUTO_DELEGATE_ENABLED`, `A2A_REQUIRE_CONFIRMATION_FOR_REMOTE`, `A2A_PRODUCTION_SECURITY`, `A2A_TASK_RETENTION_DAYS`, `A2A_OBJECT_RETENTION_DAYS`, `A2A_MAX_PENDING_TASKS`, `A2A_MAX_OUTBOUND_TASKS_PER_CHANNEL`, `A2A_MAX_INBOUND_TASKS_PER_CHANNEL`, `A2A_MAX_EVENT_RATE_PER_MIN`.
7. Add production guard: if `A2A_PRODUCTION_SECURITY=true` and only `NATS_TOKEN` is configured, startup config validation fails before bot start.
8. Add config validation: `NATS_URL != "" && A2A_AGENT_ID == ""` fails startup with an actionable error; all quota/backpressure envs use `0` to mean unlimited.
9. Add `/doctor` A2A rows that show enabled/disabled/defaults without raw tokens, creds contents, or TLS material.

**Validation**:

```bash
go test ./a2a -run 'Test(Subject|Envelope|TaskState|ErrorCode|NatsMsgID)'
go test ./channel -run 'TestDoctor.*A2A|TestDoctorRuntimeOverviewDoesNotLeakRawEnvironmentValues'
```

**Expected result**: both commands pass.

**Bugs caught**:

- accidental pool subject support;
- token-only production mode;
- secret leakage through doctor output;
- invalid task IDs accepted before persistence.

**Rollback boundary**: revert `a2a/`, `config.go`, `.env.example`, and `channel/doctor_env.go`; no storage or network side effects exist in this phase.

**Cross-phase contract**: later phases must import these validators and must not duplicate subject/envelope/idempotency parsing.

**Done criteria**:

- A2A disabled path is behaviorally no-op.
- All A2A envs parse deterministically.
- All subject/envelope tests include positive and negative cases.

### Phase 2: NATS node and JetStream topology

**Intent**: add connection lifecycle and stream setup without executing remote tasks.

**Touched files/symbols**:

- Modify `go.mod`, `go.sum`: add `github.com/nats-io/nats.go`; add embedded `github.com/nats-io/nats-server/v2/server` for tests.
- Create `a2a/node.go`: `Node`, `NodeConfig`, `Connect`, `Close`, `Drain`, `IsEnabled`.
- Create `a2a/streams.go`: `EnsureStreams`, `EnsureConsumers`, stream constants.
- Create `a2a/nats_test.go`: embedded server helper.
- Modify `main.go`: construct disabled/enabled A2A node and pass it to bot/channel only as an inert dependency.

**Preconditions**:

- Phase 1 tests pass.
- `A2AConfig.Enabled` is true only when both `NATS_URL` and `A2A_AGENT_ID` are set; `NATS_URL != "" && A2A_AGENT_ID == ""` is a startup error, not silent disablement.

**Change steps**:

1. Add dependency and embedded test helper.
2. Implement connect options for creds file, token dev mode, TLS CA, logical agent name, reconnect, drain, and closed handlers.
3. Implement `EnsureStreams` with subjects exactly: `a2a.v1.task.>`, `a2a.v1.control.>`, `a2a.v1.event.>`.
4. Implement task/control/event durable consumers for the local `A2A_AGENT_ID`.
5. Do not create pool consumers.
6. Wire startup so disabled A2A skips all NATS work.
7. Wire shutdown so node drains after workers stop accepting new A2A work.

**Validation**:

```bash
go test ./a2a -run 'Test(NodeDisabled|ConnectDrain|EnsureStreams|NoPoolSubject|DuplicateNatsMsgID)'
go test . -run 'TestConfig.*A2A'
```

**Expected result**: stream setup creates no pool subject and disabled startup opens no connection.

**Bugs caught**:

- boot attempts NATS when disabled;
- wrong subject topology;
- duplicate publish not deduped;
- missing drain path.

**Rollback boundary**: revert `a2a/node.go`, `a2a/streams.go`, `go.mod`, `go.sum`, and startup wiring; Phase 1 validators remain usable.

**Cross-phase contract**: all publish paths use `Node.JetStream` and idempotency helpers; no package publishes raw NATS messages directly.

**Done criteria**:

- Embedded NATS tests pass.
- Startup disabled behavior unchanged.
- No `a2a.v1.pool` literal exists outside documentation/spec tests that assert rejection.

### Phase 3: Durable stores

**Intent**: make task, event, peer, object, and policy state durable before transport consumers exist.

**Touched files/symbols**:

- Create `a2a/task_store.go`: `TaskStore`, `TaskRow`, `CreateOutbound`, `AdmitInbound`, `BindAccepted`, `AppendTaskEvent`, `MarkTerminal`.
- Create `a2a/event_store.go`: event append/replay queries.
- Create `a2a/peer_store.go`: peer card storage, stale detection, trust display model.
- Create `a2a/policy_store.go`: channel A2A policy schema and validation.
- Create `a2a/object_store.go`: object ref rows, digest checks, retention cleanup stubs.
- Create matching `*_test.go`.

**Preconditions**:

- Phase 1 validators are used by store constructors.
- Store root is under `DATA_DIR/a2a/`.

**Change steps**:

1. Create SQLite opener with schema version table and rollback-safe migrations.
2. Implement `a2a_tasks` exactly with these columns and indexes:

```sql
CREATE TABLE IF NOT EXISTS a2a_tasks (
  local_id TEXT PRIMARY KEY,
  task_id TEXT,
  client_task_ref TEXT,
  message_id TEXT NOT NULL,
  context_id TEXT,
  direction TEXT NOT NULL,
  role TEXT NOT NULL,
  from_agent TEXT NOT NULL,
  to_agent TEXT NOT NULL,
  executor_agent TEXT,
  channel_id TEXT,
  guild_id TEXT,
  channel_ref TEXT,
  skill_id TEXT,
  state TEXT NOT NULL,
  terminal INTEGER NOT NULL DEFAULT 0,
  revision INTEGER NOT NULL DEFAULT 0,
  result_visibility TEXT NOT NULL DEFAULT 'proxy',
  discord_transcript_mode TEXT NOT NULL DEFAULT 'delegator',
  discord_context_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  expires_at TEXT,
  error_code TEXT,
  error_message TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_a2a_tasks_message_direction ON a2a_tasks(direction, message_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_a2a_tasks_remote_task ON a2a_tasks(direction, task_id) WHERE task_id IS NOT NULL AND task_id <> '';
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_client_ref ON a2a_tasks(client_task_ref);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_context ON a2a_tasks(context_id);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_state ON a2a_tasks(state, terminal);
```

3. Implement `a2a_task_events` exactly with `id`, `task_id`, `revision`, `event_type`, nullable `state`, nullable `payload_json`, `created_at`, and `UNIQUE(task_id, revision)`.
4. Implement accepted bootstrap: bind by outbound `message_id`, require executor/delegator match, atomically set `task_id` and `executor_agent`, then append accepted event.
5. Implement rejected-before-accepted correlation by `(direction,message_id)` and `client_task_ref` using `msg_<messageId>`.
6. Enforce terminal immutability except idempotent replay.
7. Implement `a2a_channel_policy` exactly with these columns and defaults:

```sql
CREATE TABLE IF NOT EXISTS channel_a2a_policy (
  guild_id TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  channel_ref TEXT NOT NULL,
  accept_from_json TEXT NOT NULL DEFAULT '[]',
  accept_skills_json TEXT NOT NULL DEFAULT '[]',
  expose_skills_json TEXT NOT NULL DEFAULT '[]',
  delegate_to_json TEXT NOT NULL DEFAULT '[]',
  delegate_skills_json TEXT NOT NULL DEFAULT '[]',
  delegate_media_json TEXT NOT NULL DEFAULT '{}',
  max_concurrent INTEGER NOT NULL DEFAULT 0,
  result_visibility TEXT NOT NULL DEFAULT 'proxy',
  discord_transcript_mode TEXT NOT NULL DEFAULT 'delegator',
  share_discord_context INTEGER NOT NULL DEFAULT 0,
  co_present_from_json TEXT NOT NULL DEFAULT '[]',
  auto_delegate_enabled INTEGER NOT NULL DEFAULT 0,
  remote_tool_policy_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  PRIMARY KEY (guild_id, channel_id)
);
```

8. Validate policy rules exactly: `channel_ref` required when enabled; `channel_ref` unique per `A2A_AGENT_ID`; `result_visibility` in `proxy|transparent`; `discord_transcript_mode` in `delegator|mirror|co_present`; `share_discord_context` only with `co_present`; agent lists are stable IDs or explicit `*`; skill lists are slugs or explicitly chosen peer skill IDs; media rules define MIME types, max bytes, and object-ref permission; `remote_tool_policy_json.allow_memory_write` defaults false and is the only way channel policy permits remote memory writes; `max_concurrent` is `0..64` with `0` unlimited.
9. Validate `0` means unlimited for concurrency, quota/backpressure envs, and retention.
10. Implement peer card sanitizer fields and object reference digest validation without object bytes upload yet.

**Validation**:

```bash
go test ./a2a -run 'Test(TaskStore|AcceptedBootstrap|RejectedBeforeAccepted|TerminalImmutable|PolicyStore|PeerStore|ObjectRef)'
```

**Expected result**: all store invariants pass without NATS.

**Bugs caught**:

- forged accepted event binding to wrong executor;
- mutable terminal state;
- policy stored in channel metadata JSON;
- quota `0` misread as deny-all;
- object digest mismatch accepted.

**Rollback boundary**: revert store files and tests; network code remains inert.

**Cross-phase contract**: consumers must persist before Ack/AckSync and must use store APIs rather than direct SQL outside the store package.

**Done criteria**:

- Schema migrations are deterministic.
- Store tests prove idempotent replay behavior.
- No Discord or ACP imports exist in `a2a/`.

### Phase 4: Peer card and discovery

**Intent**: publish sanitized capabilities and build manager-visible trust review before any remote work can be accepted.

**Touched files/symbols**:

- Create `a2a/card.go`: `BuildPublicAgentCard`, `BuildExtendedAgentCard`, `SanitizeAgentCard`, `ValidatePeerCard`.
- Create `a2a/discovery.go`: `EnsurePeerKV`, `PublishPeerCard`, `WatchPeerCards`, request-reply fallback collector.
- Create `a2a/heartbeat.go`: `PublishHeartbeat`, `HeartbeatPayload`, stale TTL handling.
- Modify `a2a/peer_store.go`: `UpsertCard`, `ListPeers`, `MarkStale`, `TrustSummary`.
- Modify `channel/manager.go`: expose read-only peer summary through Manager.
- Modify `bot/commands.go` or equivalent slash handler: admin peer review surface.

**Preconditions**:

- Phase 3 peer store exists.
- Public cards cannot include CWD, Discord channel IDs, internal URLs, tokens, private URLs, or MCP server names.

**Change steps**:

1. Implement public card conversion with canonical A2A fields only.
2. Implement internal extended card storage for authenticated peers.
3. Use JetStream KV bucket `A2A_PEERS` as the v1 discovery mechanism: key `<agentID>`, value sanitized public AgentCard plus `instanceId`, `expiresAt`, and version metadata, TTL `heartbeat interval * 3`.
4. Watch KV changes and upsert peer cards into `PeerStore`; mark a peer stale when `expiresAt` passes or KV delete/tombstone arrives.
5. Implement heartbeat payload `{agentId, instanceId, status, activeTasks, startedAt, version}` on `a2a.v1.heartbeat.<agent>.<instance>`; heartbeat is advisory only and must not be the sole routing authority.
6. Implement the request-reply fallback collector only for environments without KV: collect multi-response peer cards until deadline, handle no responders and timeout with localized `unsupported_operation`/offline status, and never hide unsupported bindings.
7. Add peer trust display: name, signature status, credential issuer, credential/public-key fingerprint, supported binding, protocol version, stale/online state.
8. Add version compatibility checks for `binding` and `protocolVersion`.
9. Add stale peer cleanup and redacted admin output.

**Validation**:

```bash
go test ./a2a -run 'Test(AgentCardSanitizer|ExtendedCard|PeerKV|PeerWatch|Heartbeat|PeerRequestReplyFallback|PeerTrustSummary|VersionCompatibility|StalePeer)'
go test ./bot ./channel -run 'Test.*A2A.*Peer'
```

**Expected result**: sanitized public card contains no private fields, KV/watch or fallback discovery populates `PeerStore`, heartbeat updates online/stale state, and admin review displays trust metadata.

**Bugs caught**: public card leaking local runtime details; peers never published into the store; heartbeat treated as authorization; unknown peer versions hidden instead of reported.

**Rollback boundary**: revert card/discovery files; TaskStore/policy store remains intact.

**Cross-phase contract**: delegation tools may target only peers present in peer store and allowed by channel policy.

**Done criteria**: peer review output is localized, redacted, and deterministic.

### Phase 5: A2A ingress through channel runtime

**Intent**: admit remote tasks into the existing channel runtime without bypassing queue, MCP policy, audit, CWD, profile, or safe egress.

**Touched files/symbols**:

- Create `a2a/executor.go`: `Executor` interface, `TaskExecutionRequest`, `A2AAdmissionResult`, `A2AAdmission`, `TaskExecutionResult`, `TaskExecutionArtifact`, `DiscordContext`.
- Create `channel/a2a.go`: `Manager.AdmitA2ATask(ctx context.Context, req a2a.TaskExecutionRequest) (a2a.A2AAdmissionResult, error)` and `Manager.RunA2ATask(ctx context.Context, admitted a2a.A2AAdmission) (a2a.TaskExecutionResult, error)` adapters.
- Modify `channel/manager.go`: channel policy lookup and max-concurrent guard used by the adapter.
- Modify `channel/worker.go`: A2A job construction, result capture, proxy egress disabled mode.
- Modify `channel/worker_test.go`, `channel/manager_test.go`.

**Preconditions**:

- `a2a/` package must not import `channel`; it owns only validation, transport, stores, and the `Executor` interface/DTOs.
- `channel` may import `a2a` validators/types and implements `a2a.Executor`.

**Change steps**:

1. Define `a2a.TaskExecutionRequest` with remote agent identity, local channel ref, user-visible summary, canonical A2A payload, delivery options, task timeout, and audit metadata.
2. Define `a2a.A2AAdmissionResult` with local task ID, delegated/executor IDs, channel/session binding, worker reservation token, initial revision, and rejected/overloaded error mapping; define `a2a.A2AAdmission` as the immutable accepted execution token.
3. Define `a2a.TaskExecutionResult` with state, content, artifacts, error code/message, metrics, and final revision.
4. Implement `Manager.AdmitA2ATask(ctx, req a2a.TaskExecutionRequest) (a2a.A2AAdmissionResult, error)` for channel_ref resolution, policy/quota/media/transcript validation, and worker-slot reservation only; it must not start long-running execution or post Discord output.
5. Implement `Manager.RunA2ATask(ctx, admitted a2a.A2AAdmission) (a2a.TaskExecutionResult, error)` to enqueue the existing Worker job after the transport has durably published `accepted` and AckSynced the task message.
6. Add compile-time assert `var _ a2a.Executor = (*Manager)(nil)`.
7. Validate inbound `accept_from`, `accept_skills`, media policy, result visibility, transcript mode, and concurrency before admission succeeds.
8. Enforce `A2A_MAX_INBOUND_TASKS_PER_CHANNEL` before accepted publication; nonzero cap overflow returns admission result `overloaded` and persists a rejected/failed event.
9. Enqueue through existing Worker path only from `RunA2ATask`; do not call `acp.StartAgent` or Discord API from `a2a/`.
10. For remote A2A jobs, carry a remote-task tool policy flag into Worker/bot-tools context: Discord egress is disabled in proxy mode, and memory write tools such as `bot_memory_add` are disabled by default unless `remote_tool_policy_json.allow_memory_write=true` for this channel.
11. Capture final response as structured result and do not post directly to Discord from executor unless policy later permits transparent/co-present mode.

**Validation**:

```bash
go test ./channel -run 'TestManagerA2A(IngressDisabled|PolicyDenied|AcceptsOnce|InboundQuota|AdmissionBeforeExecution|AckAfterAdmissionNotCompletion|UsesWorker|ProxyDisablesEgress|RemoteMemoryWriteDenied|RemoteMemoryWriteAllowedByPolicy|Timeout|Cancel|InputRequired|AuthRequired|ResultCapture)'
```

**Expected result**: remote execution path uses Manager/Worker and persists/captures state without direct remote Discord egress.

**Bugs caught**: direct ACP bypass, missing channel policy, unsafe bot-tools egress, remote memory write bypass, lost interrupted state.

**Rollback boundary**: revert `channel/a2a.go` and channel wiring; stores and transport remain unused.

**Cross-phase contract**: transport consumers call injected `a2a.Executor.AdmitA2ATask`, publish/record `accepted`, AckSync, then call `RunA2ATask` asynchronously; `a2a` never imports `channel`, and result events are built from `a2a.TaskExecutionResult`.

**Done criteria**: all terminal and interrupted states are represented in TaskStore-compatible result data.

### Phase 6: Transport consumers and event routing

**Intent**: connect NATS transport to durable stores and channel ingress with at-least-once correctness.

**Touched files/symbols**:

- Create `a2a/transport.go`: `Publisher`, `TaskConsumer`, `ControlConsumer`, `EventConsumer`.
- Create `a2a/admission.go`: admission algorithm and Ack/AckSync rules.
- Create `a2a/integration_test.go`: embedded two-node tests.
- Modify startup wiring to inject `channel.Manager` as `a2a.Executor` and start consumers only after Manager and stores exist.

**Preconditions**:

- Phase 5 `channel.Manager` implements `a2a.Executor` admission and execution methods.
- Phase 3 stores enforce idempotency and terminal immutability.

**Change steps**:

1. Implement task consumer for `a2a.v1.task.*.<localAgent>.>`.
2. On receive: parse subject, validate envelope/payload, validate authenticated principal, create or find inbound row, then call the injected `a2a.Executor.AdmitA2ATask`.
3. If admission rejects deterministically, durably record one terminal rejected row/event keyed by `(direction,messageId)` or `client_task_ref`, publish/queue `rejected` with `taskKey=msg_<messageId>`, then AckSync.
4. If admission accepts, persist local task admission and accepted event, publish `accepted` with `taskKey=<taskId>`, then AckSync the original task message before starting long-running execution.
5. Start `RunA2ATask` asynchronously only after AckSync; execution progress/result is represented by durable TaskStore plus `A2A_EVENTS`, never by holding the original task message unacked.
6. Implement control consumer for status/cancel/input_reply/auth_reply and ownership binding.
7. Implement event consumer for accepted/rejected/status/artifact/result; accepted bootstrap binds outbound task ID atomically.
8. Route all failures through `rejected`, `task_status_update`, or `task_result`; do not add an `error` envelope.
9. Enforce `A2A_MAX_EVENT_RATE_PER_MIN` on event publish/consume paths; nonzero cap overflow maps to canonical `overloaded`, while `0` remains unlimited.
10. Replay event stream after reconnect and apply only monotonic revisions or idempotent replays.

**Validation**:

```bash
go test ./a2a -run 'TestA2AIntegration(TargetedDelegation|DuplicateDelivery|CancelOwnership|AcceptedBootstrap|AdmissionBeforeExecution|AckAfterAdmissionNotCompletion|ReplayAfterReconnect|EventRateQuota|EventRateOverloaded|EventRateZeroUnlimited|NoErrorEnvelope|NoPoolSubject)'
```

**Expected result**: two embedded nodes exchange a targeted task once, survive duplicate delivery, AckSync after durable admission rather than completion, enforce event-rate caps with canonical `overloaded`, and reject forged controls.

**Bugs caught**: ack-before-persist data loss, duplicate execution, long-running task holds original message unacked, event-rate quota parsed but unused, forged cancel/result, standalone error route, pool regression.

**Rollback boundary**: stop consumers and revert `a2a/transport.go`, `a2a/admission.go`, integration tests, startup consumer wiring.

**Cross-phase contract**: bot-tools delegate/cancel/input/auth call publisher APIs, not raw NATS.

**Done criteria**: all NATS publish paths declare and test their `Nats-Msg-Id`.

### Phase 7: Bot-tools and natural-language UX

**Intent**: expose A2A operations to the local agent through safe bot-tools and fallback slash/buttons without granting policy bypasses.

**Touched files/symbols**:

- Modify `internal/botmcp/server.go`: register `bot_a2a_peers`, `bot_a2a_policy_get`, `bot_a2a_task_status`, `bot_a2a_policy_plan`, `bot_a2a_policy_apply`, `bot_a2a_delegate`, `bot_a2a_cancel`, `bot_a2a_input_reply`, `bot_a2a_auth_reply`.
- Add `internal/botmcp/a2a_tools.go` if `server.go` would become unmaintainable.
- Modify `bot/commands.go`: slash fallbacks `/a2a peers/status/delegate/cancel/enable/disable/ref/expose/unexpose/accept-from/deny-from/delegate-to/undelegate-to/max-concurrent/transcript-mode/transcript-from/reply/authorize`.
- Modify `bot/interaction_policy.go`: signed confirmation/button/modals for A2A.
- Modify `locale/lang/en.json`, `locale/lang/zh-TW.json`.

**Preconditions**:

- Transport publisher APIs exist.
- Tool calls are bound to current guild/channel/thread context.

**Change steps**:

1. Add tool schemas with read-only/write/destructive/idempotent/open-world annotations matching the tool matrix.
2. Reject user-supplied guild/channel/requester IDs that do not match bound context.
3. Implement policy plan/apply with ManageChannels, fresh confirmation token, idempotent `changeId`, and audit event.
4. Implement delegate with outbound policy, media policy, egress labels, remote confirmation requirement, max-depth check, `A2A_MAX_PENDING_TASKS`, and `A2A_MAX_OUTBOUND_TASKS_PER_CHANNEL`; nonzero cap overflow returns canonical `overloaded`, while `0` remains unlimited.
5. Implement cancel with requester-or-manager validation and known executor requirement.
6. Implement input/auth continuation with expected interrupted state, nonce freshness, no raw long-lived credential, and one idempotent control publish.
7. Add slash fallbacks that call the same service methods as bot-tools.
8. Add localized success/failure/status copy for every user-visible outcome.

**Validation**:

```bash
go test ./internal/botmcp -run 'TestA2ATools(Annotations|BoundContext|PolicyPlan|PolicyApply|Delegate|DelegateQuota|Cancel|InputReply|AuthReply)'
go test ./bot ./locale -run 'TestA2A(Slash|Buttons|Confirmation|Locale|Permission)'
```

**Expected result**: natural-language-capable tools are safe, scoped, audited, localized, and share slash/button internals.

**Bugs caught**: tool-level permission bypass, stale confirmation replay, raw credential leakage, missing i18n, separate slash policy path.

**Rollback boundary**: remove A2A tools/slash/buttons; transport and stores remain.

**Cross-phase contract**: user-facing operations never publish raw NATS directly; they call A2A service/publisher APIs after policy checks.

**Done criteria**: every `bot_a2a_*` tool has tests for allowed and denied paths.

### Phase 8: Artifacts, result delivery, and transcript modes

**Intent**: deliver artifacts/results through safe egress and implement Discord transcript modes without leaking context.

**Touched files/symbols**:

- Finalize `a2a/object_store.go`: object bytes storage/fetch, digest validation, retention cleanup.
- Modify `bot/safe_egress.go`, `internal/botegress/actions.go`: A2A result/artifact delivery wrappers.
- Modify `channel/worker.go`: transparent/co-present delivery gates.
- Modify `audit/store.go`, `audit/recorder.go`: A2A metadata fields from spec section 19.
- Modify locale files for delivery/status messages.

**Preconditions**:

- Bot-tools delegate and result events work in proxy mode.
- Existing safe egress behavior remains unchanged for non-A2A sends.

**Change steps**:

1. Implement artifact references with JetStream Object Store bucket `a2a-artifacts` by default, or an explicitly approved external object reference backend; do not implement local-only `DATA_DIR/a2a/objects` as the cross-agent artifact backend. Each part stores `bucket`, `key`, `digest`, `size`, `mediaType`, and `expiresAt`; keys are generated locally as `tasks/<taskId>/<safeName>`.
2. Enforce media policy before remote execution and before Discord delivery.
3. Implement retention cleanup: `0` keeps permanently; positive days purge expired TaskStore/Object rows only.
4. Add proxy delivery through delegator safe egress by default.
5. Add mirror mode: delegator mirrors executor events with labels, no executor Discord egress.
6. Add co-present mode: require same guild/channel/thread, both bot permissions, `share_discord_context=true`, and executor inbound `co_present_from` allow.
7. Add transparent final result only when result visibility and safe egress checks pass.
8. Audit every visible Discord post with actor agent ID, Discord user ID where known, transcript delivery kind, source event revision, source event ID, error code, payload size, and artifact count.
9. Record every A2A audit event named in spec section 19 when applicable: `a2a_peer_card_updated`, `a2a_policy_change_planned`, `a2a_policy_change_applied`, `a2a_policy_change_denied`, `a2a_task_send_requested`, `a2a_task_publish_failed`, `a2a_task_received`, `a2a_task_rejected`, `a2a_task_admitted`, `a2a_task_started`, `a2a_task_status_published`, `a2a_task_artifact_published`, `a2a_task_completed`, `a2a_task_failed`, `a2a_task_canceled`, `a2a_result_received`, `a2a_result_delivered`, `a2a_control_received`, `a2a_auth_required`, `a2a_input_required`, and `a2a_transcript_posted`.
10. For every A2A Discord write, require existing safe send helpers with `discordgo.MessageAllowedMentions{}` by default; allowlisted user IDs may be passed only through `sendDiscordTextWithAllowedMentions` after requester/manager validation.

**Validation**:

```bash
go test ./a2a -run 'TestObject(Store|Digest|Retention|MediaPolicy)'
go test ./bot ./internal/botegress ./channel ./audit -run 'TestA2A(Egress|Artifact|ProxyDelivery|MirrorTranscript|CoPresent|TransparentResult|AuditMetadata)'
```

**Expected result**: artifacts and visible collaboration records respect media policy, safe egress, audit, and Discord permission checks.

**Bugs caught**: digest mismatch accepted, expired purge deletes permanent rows, executor posts without co-present approval, raw artifact bypasses safe egress.

**Rollback boundary**: disable transcript/artifact feature flags and revert delivery wrappers; core task transport remains.

**Cross-phase contract**: production rollout uses these smoke checks as release gates.

**Done criteria**: all Discord write paths declare safe egress, audit, AllowedMentions, and localization handling.

### Phase 9: Production hardening and rollout

**Intent**: make A2A deployable without weakening production auth, observability, rollback, or disabled-mode safety.

**Touched files/symbols**:

- Modify `.env.example`: complete A2A env block and production notes.
- Add `docs/a2a-nats-rollout.md` or extend this guide with runbook commands.
- Add `dev/nats.conf` or documented config snippet if repository conventions allow.
- Modify `scripts/` only if a docs-only smoke/preflight wrapper is accepted by maintainers.
- Modify `/doctor` and admin review surfaces if not completed earlier.

**Preconditions**:

- Phases 1-8 validation commands pass.
- Production security decision remains NKey/JWT or mTLS, not shared token.

**Change steps**:

1. Document dev single-node NATS setup and production three-node JetStream recommendation.
2. Document one-agent ACL template exactly:

```text
publish allow:
a2a.v1.task.<self>.>
a2a.v1.control.<self>.>
a2a.v1.event.<self>.>
a2a.v1.card.<self>
a2a.v1.heartbeat.<self>.>
$KV.A2A_PEERS.<self>

subscribe allow:
a2a.v1.task.*.<self>.>
a2a.v1.control.*.<self>.>
a2a.v1.event.*.<self>.>
a2a.v1.card.>
a2a.v1.heartbeat.>
$KV.A2A_PEERS.>

JetStream API allow:
$JS.API.STREAM.INFO.KV_A2A_PEERS
$JS.API.CONSUMER.CREATE.KV_A2A_PEERS.>
$JS.API.CONSUMER.DELETE.KV_A2A_PEERS.>
```

3. Document response/inbox rule: production must avoid blanket `_INBOX.>` unless account isolation already enforces tenant boundaries; request/reply discovery fallback and JetStream API calls must use narrow reply inbox permissions or a separate bucket-admin credential that is not used for task publish.
4. Document authenticated-principal binding: NATS credential identity maps to one allowed `AgentID`; inbound subject `from` and `Envelope.From` must equal that identity, and subject `to` must match `Envelope.To` where present.
5. Document negative ACL smokes: a credential for `adam-n200` cannot publish `a2a.v1.task.eve-local.adam-n200.<messageId>`, cannot write `$KV.A2A_PEERS.eve-local`, and cannot subscribe to another agent's narrow task inbox; positive smoke proves it can write `$KV.A2A_PEERS.adam-n200` and watch `$KV.A2A_PEERS.>`.
6. Document credential lifecycle: manual issue, rotation, revocation, and compromised peer removal.
7. Document startup/shutdown ordering and drain behavior.
8. Document rollout gates: local two-bot smoke, same-channel co-present smoke, cross-server proxy smoke, NATS restart smoke, credential revocation smoke.
9. Document rollback: set `NATS_URL=""`, drain, verify existing Discord behavior, keep data for postmortem.
10. Add final validation command matrix.

**Validation**:

```bash
go test ./a2a ./channel ./internal/botmcp ./bot ./audit ./locale -run 'Test.*A2A|TestDoctor.*A2A'
python - <<'PY'
from pathlib import Path
guide = Path('docs/a2a-nats-implementation-guide.md').read_text()
for required in ['local two-bot smoke', 'same-channel co-present smoke', 'cross-server proxy smoke', 'NATS restart smoke', 'credential revocation smoke']:
    assert required in guide
print('a2a-rollout-guide-ok')
PY
```

**Expected result**: targeted tests pass and rollout guide contains every release gate.

**Bugs caught**: missing production auth guard, incomplete smoke coverage, unsafe rollback advice.

**Rollback boundary**: docs/runbook/config snippets only; implementation remains behind A2A disabled flag.

**Cross-phase contract**: implementation is not complete until rollout smokes pass in the target environment.

**Done criteria**:

- A2A disabled leaves existing bot behavior unchanged.
- Token-only production startup fails.
- NATS restart preserves durable task/result state.
- Credential revocation prevents new delegated work.

## 6. Final implementation checklist

Before a coding agent starts implementation, this guide must satisfy every item below:

1. `a2a/` package file list, public types, and tests are declared in section 4.1 and phases 1-3.
2. Config/env table and `.env.example` changes are declared in phase 1 and phase 9.
3. SQLite schemas and migration ownership are declared in phase 3.
4. NATS stream/consumer setup, allowed subjects, forbidden subjects, and ACL expectations are declared in sections 4.3, phase 2, phase 6, and phase 9.
5. Bot-tools schema, slash command fallback, button/modal state, permissions, and confirmation behavior are declared in section 4.4 and phase 7.
6. Locale and audit metadata changes are declared in phases 7-8.
7. Per-phase targeted test commands are present in phases 0-9.
8. Manual rollout smoke gates are declared in phase 9.
9. Deferred features are explicit: pool dispatch, official HTTP A2A gateway, SSE streaming, HTTP push notification config.
10. No implementation phase leaves schema/API/security choices to the coding agent.

## 7. Guide-only verification

Run this before starting implementation and after any edit to this guide:

```bash
python - <<'PY'
from pathlib import Path
import re

guide = Path('docs/a2a-nats-implementation-guide.md').read_text()
spec = Path('docs/a2a-nats-integration-spec.md').read_text()

fence_lines = [line for line in guide.splitlines() if line.startswith('```')]
assert len(fence_lines) % 2 == 0, 'unbalanced markdown fences'

for marker in ('TO' + 'DO', 'TB' + 'D', '待' + '定', 'may' + 'be', 'planning ' + 'seed', 'not implementation' + '-ready'):
    assert marker not in guide, f'open marker remains: {marker}'

assert 'docs/a2a-nats-integration-spec.md' in guide
assert 'Status: implementation-ready guide' in guide
assert 'Forbidden v1 subjects' in guide and 'a2a.v1.pool.>' in guide
assert 'No standalone cross-agent `error` envelope' in guide
assert 'channel.Manager' in guide

required_phase_fields = [
    '**Intent**',
    '**Touched files/symbols**',
    '**Preconditions**',
    '**Change steps**',
    '**Validation**',
    '**Expected result**',
    '**Bugs caught**',
    '**Rollback boundary**',
    '**Cross-phase contract**',
    '**Done criteria**',
]
phase_blocks = re.split(r'\n### Phase \d+: ', guide)[1:]
assert len(phase_blocks) == 10, f'expected 10 phases, got {len(phase_blocks)}'
for idx, block in enumerate(phase_blocks):
    for field in required_phase_fields:
        assert field in block, f'phase {idx} missing {field}'

for required in [
    'TaskMsgID',
    'ControlMsgID',
    'EventMsgID',
    'bot_a2a_delegate',
    'bot_a2a_input_reply',
    'bot_a2a_auth_reply',
    'safe egress',
    'AllowedMentions',
    'local two-bot smoke',
    'same-channel co-present smoke',
    'cross-server proxy smoke',
    'NATS restart smoke',
    'credential revocation smoke',
    'A2A_MAX_PENDING_TASKS',
    'A2A_MAX_EVENT_RATE_PER_MIN',
    'JetStream Object Store bucket `a2a-artifacts`',
    'a2a_task_received',
    'a2a_result_delivered',
    'discordgo.MessageAllowedMentions{}',
    'authenticated-principal binding',
    'type Executor interface',
    'A2A_PEERS',
    'PublishHeartbeat',
    'remote_tool_policy_json.allow_memory_write',
    'A2A_MAX_INBOUND_TASKS_PER_CHANNEL',
    '$KV.A2A_PEERS.<self>',
    'AdmitA2ATask',
    'RunA2ATask',
    'EventRateQuota',
    'RemoteMemoryWriteDenied',
]:
    assert required in guide, f'missing required guide term: {required}'

assert 'A2A-inspired' in spec
print('a2a-implementation-guide-ready')
PY
```

Expected output:

```text
a2a-implementation-guide-ready
```

## 8. Ready-to-implement goal

Before starting or resuming implementation, read `docs/a2a-nats-implementation-progress.md` after this guide and use it as the durable phase ledger. Implementation resumes from repository state, validation evidence, and git history; chat memory is not authoritative.

After the guide-only verification and reviewer pass, the implementation goal is:

```text
Implement docs/a2a-nats-implementation-guide.md exactly, phase by phase, using docs/a2a-nats-implementation-progress.md as the execution ledger, stopping only when each phase validation gate passes and preserving the source spec decisions in docs/a2a-nats-integration-spec.md.
```

The coding agent must treat any failing phase validation as a blocker to the next phase, fix the issue at the source, rerun that phase validation, record evidence in the progress ledger, then continue only when the ledger marks the next phase ready.
