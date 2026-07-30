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

- Program state: collaboration contract established; A2A implementation code has not started in this ledger.
- Current phase: Phase 0 readiness guard, followed by Phase 1 foundation package and config.
- First execution target: complete Phase 0 guide validation fix and Phase 1 foundation only.
- Known pre-implementation issue: the Phase 0 readiness snippet in `docs/a2a-nats-implementation-guide.md` currently contains a self-referential forbidden-string check. The first implementation execution must fix that snippet, then run both Phase 0 and Section 7 guide-only verification.

## Phase ledger

| Phase | Name | Status | Commit | Required validation evidence | Notes |
|---:|---|---|---|---|---|
| 0 | Readiness guard | next | | `a2a-guide-readiness-ok`; `a2a-implementation-guide-ready` | Fix self-referential Phase 0 check before implementation edits. |
| 1 | Foundation package and config | pending | | `go test ./a2a -run 'Test(Subject|Envelope|TaskState|ErrorCode|NatsMsgID)'`; `go test ./channel -run 'TestDoctor.*A2A|TestDoctorRuntimeOverviewDoesNotLeakRawEnvironmentValues'` | No NATS connection. Disabled path must be no-op. |
| 2 | NATS node and JetStream topology | pending | | `go test ./a2a -run TestNode`; `go test ./a2a -run TestStreamSetup` | Streams and consumers only; no remote task execution. |
| 3 | Durable stores | pending | | `go test ./a2a -run TestTaskStore`; `go test ./a2a -run TestPolicyStore`; `go test ./a2a -run TestPeerStore` | Durable TaskStore, event store, policy store, peer store. |
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
