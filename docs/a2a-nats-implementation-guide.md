# A2A NATS Implementation Guide Plan

> Status: planning seed, not implementation-ready.  
> Source spec: `docs/a2a-nats-integration-spec.md`.  
> Objective: define the final implementation guide so a coding agent can implement the A2A-like NATS binding by following explicit steps, with validation gates that prove each failure mode is removed before the next phase starts.

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

## 4. Final guide outline

### Phase 0: Guide readiness gate

Purpose: freeze the implementation contract before code changes.

Required guide content:

- File-change matrix with create/modify/delete action per file.
- Type and package ownership table.
- Subject grammar table copied from the spec and converted into parser acceptance/rejection tests.
- Storage schema migrations for TaskStore, A2A events, peer store, policy store, and object refs.
- Tool schema table for every `bot_a2a_*` tool.
- i18n key table with zh-TW/en copy intent.
- Smoke-test matrix and deployment rollback notes.

Validation for the guide itself:

```bash
python - <<'PY'
from pathlib import Path
import re, json
p = Path('docs/a2a-nats-implementation-guide.md')
text = p.read_text()
fence_lines = [line for line in text.splitlines() if line.startswith('```')]
assert len(fence_lines) % 2 == 0
for marker in ('TO' + 'DO', 'TB' + 'D', '待' + '定'):
    assert marker not in text
assert 'Validation' in text and 'Done criteria' in text
print('guide-sanity-ok')
PY
```

### Phase 1: Foundation package and config

Final guide must specify:

- Create `a2a/` package for subject grammar, envelope validation, A2A state mapping, error codes, idempotency keys, and static schemas.
- Add NATS config parsing to `config.go` and `.env.example`.
- Extend `/doctor` runtime overview without leaking raw `NATS_TOKEN`, creds contents, or TLS material.
- Add startup disabled path: `NATS_URL == ""` means A2A is completely off.
- Add production guard: `A2A_PRODUCTION_SECURITY=true` rejects token-only auth.

Validation must include:

- Unit tests for subject parser, slug validation, envelope validation, state mapping, error code parsing, and disabled config behavior.
- Doctor tests proving defaults render and secrets do not leak.
- `go test ./a2a ./channel -run 'Test(A2A|Doctor).*'` or tighter equivalent after actual test names exist.

### Phase 2: NATS connection and JetStream topology

Final guide must specify:

- Add NATS client dependency and connection lifecycle wrapper.
- Create stream setup for `A2A_TASKS`, `A2A_CONTROLS`, `A2A_EVENTS`, and peer/card storage per spec.
- Use only `a2a.v1.task.>`, `a2a.v1.control.>`, and `a2a.v1.event.>` subjects in v1; no `a2a.v1.pool`.
- Implement `Nats-Msg-Id` helpers and duplicate publish behavior.
- Define drain/shutdown hooks in bot startup and Manager shutdown.

Validation must include embedded `nats-server/v2/server` tests for:

- stream creation;
- reconnect/drain;
- duplicate `Nats-Msg-Id` suppression;
- subject permission rejection;
- no pool subject publishing.

### Phase 3: Durable stores

Final guide must specify:

- Create `a2a/task_store.go` with SQLite schema from the spec.
- Create event table and retention cleanup.
- Create peer/card store using KV or SQLite-backed fallback exactly as chosen in the guide.
- Create channel A2A policy store using dedicated SQLite, not `bot/data/channel_metadata.json`.
- Define migration/versioning rules and rollback-safe schema changes.

Validation must include:

- CRUD tests for TaskStore outbound/inbound rows.
- Accepted bootstrap test binding `task_id` and `executor_agent` atomically.
- Rejected-before-accepted correlation by `(direction,message_id)`.
- Terminal immutability tests.
- Policy validation tests for `channel_ref`, `accept_from`, `delegate_to`, `delegate_skills`, `delegate_media`, `result_visibility`, `discord_transcript_mode`, `co_present_from`, and unlimited `0` quota semantics.

### Phase 4: Peer card and discovery

Final guide must specify:

- Canonical public AgentCard vs internal extended card conversion.
- No leaked CWD, Discord channel IDs, internal URLs, tokens, private URLs, or MCP server names in public card.
- Peer trust display fields: name, signature status, credential issuer, and credential/public-key fingerprint.
- Discovery refresh cadence and stale peer behavior.
- Version compatibility handling.

Validation must include:

- AgentCard sanitizer tests.
- Version mismatch tests.
- Peer store stale/refresh tests.
- Admin-visible peer review output tests.

### Phase 5: A2A ingress through channel runtime

Final guide must specify:

- Add a Manager-owned entrypoint such as `ExecuteA2ATask(ctx, req)` with concrete request/result structs.
- Admission validates A2A channel policy before any agent execution.
- Execution reuses channel runtime CWD, MCP policy, agent profile, queue, audit, and timeout controls.
- Remote tasks cannot call bot-tools Discord egress directly unless transparent/co-present policy permits it.
- Result capture returns structured Task state/artifacts/errors to TaskStore and event publisher.

Validation must include:

- Inbound task denied when channel disabled.
- Inbound task accepted once under duplicate delivery.
- Inbound execution goes through Worker/Manager path, not direct ACP bypass.
- Proxy-mode result disables remote Discord final egress.
- Timeout, cancel, input-required, auth-required, failed, and completed transitions persist correct state.

### Phase 6: Transport consumers and event routing

Final guide must specify:

- Task consumer admission algorithm.
- Control consumer for status, cancel, input reply, and auth reply.
- Event consumer for accepted, rejected, status, artifact, and result.
- Ack rules that do not Ack before durable admission or durable rejection/event record.
- Ownership binding for every control/event receive path.

Validation must include:

- Targeted delegation end-to-end with two embedded nodes.
- Duplicate task redelivery does not execute twice.
- Cancel routes to known executor and cannot be forged by another agent.
- Accepted bootstrap requires subject executor/delegator/taskKey consistency.
- Status/result/artifact events replay after delegator reconnect.
- No standalone `error` envelope route exists.

### Phase 7: Bot-tools and natural-language UX

Final guide must specify tool schemas and exact permission checks for:

- `bot_a2a_peers`
- `bot_a2a_policy_get`
- `bot_a2a_task_status`
- `bot_a2a_policy_plan`
- `bot_a2a_policy_apply`
- `bot_a2a_delegate`
- `bot_a2a_cancel`
- `bot_a2a_input_reply`
- `bot_a2a_auth_reply`

It must also specify slash fallback commands and signed button/modal state.

Validation must include:

- Tool annotation tests for read/write/destructive/idempotent hints.
- Bound Discord context rejection tests.
- ManageChannels-only policy apply tests.
- Confirmation token freshness/idempotency tests.
- Input/auth continuation tests.
- User-facing localized response tests.

### Phase 8: Artifacts and object references

Final guide must specify:

- Object reference schema and digest validation.
- Attachment upload/download path.
- Retention cleanup when days > 0 and permanent retention when `0`.
- Media type allowlist policy.
- Safe egress handoff for Discord delivery.

Validation must include:

- Large attachment uses object reference.
- Digest mismatch rejects artifact.
- Unsupported media type rejects before remote execution or delivery.
- Retention cleanup removes expired object refs without touching permanent rows.

### Phase 9: Discord transcript and delivery modes

Final guide must specify:

- `delegator`, `mirror`, and `co_present` transcript behavior.
- Same guild/channel permission checks for `co_present`.
- Proxy default behavior.
- Transparent result delivery conditions.
- Audit metadata required for every visible Discord post.

Validation must include:

- Proxy mode posts final result only through delegator.
- Mirror mode labels executor events but does not grant executor Discord egress.
- Co-present mode works only when both bots can resolve and send in same channel/thread.
- Mismatch falls back to mirror/delegator with localized explanation.

### Phase 10: Production hardening and rollout

Final guide must specify:

- NKey/JWT or mTLS production auth setup.
- NATS subject ACL templates.
- `nats.conf` dev and production examples.
- Startup/shutdown behavior.
- `/doctor` and admin review output.
- Rollout gates: local two-bot smoke, same-channel co-present smoke, cross-server proxy smoke, NATS restart smoke, credential revocation smoke.

Validation must include:

- Token-only production startup fails.
- NATS restart does not lose durable task/result state.
- Credential revocation prevents new work and does not expose secrets.
- A2A disabled leaves existing bot behavior unchanged.

## 5. Work products the final guide must produce before implementation starts

The final implementation guide must include these concrete artifacts:

1. `a2a/` package file list with public types and tests.
2. Config/env table and `.env.example` diff plan.
3. SQLite schemas and migration strategy.
4. NATS stream/consumer setup and ACL templates.
5. Bot-tools schema table with JSON fields, permissions, and examples.
6. Slash command table and button/modal payload format.
7. Locale key table for zh-TW and en.
8. Audit event metadata table.
9. Per-phase targeted test command table.
10. Manual smoke test scripts and expected Discord-visible behavior.
11. Known-deferred list: pool dispatch, official HTTP A2A gateway, SSE streaming, push notification config.
12. Final implementation checklist with no optional correctness gates.

## 6. Guide quality gates

Before implementation begins, run these guide-only checks:

- Markdown fences balanced.
- Every phase has Intent, Touched files/symbols, Change steps, Validation, and Done criteria.
- No placeholder markers, vague fallback wording, or open design choice remains.
- Every schema mentioned in the spec appears in a file-change phase.
- Every user-visible behavior has an i18n key and smoke expectation.
- Every security boundary has at least one negative test.
- Every persistent state transition has an idempotency test.
- Every NATS publish path declares its `Nats-Msg-Id`.
- Every Discord write path declares safe egress/audit/AllowedMentions handling.

## 7. Immediate next planning step

Next, convert this plan into the final implementation guide by filling each phase with exact file-level edit instructions and acceptance commands. The next artifact should replace this planning seed with an implementation-ready document or add a separate final guide only after every phase passes the guide quality gates above.
