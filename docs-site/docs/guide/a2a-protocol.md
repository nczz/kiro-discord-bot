# A2A Protocol Model

This page explains how `kiro-discord-bot` implements its A2A-like integration. It is the conceptual companion to [Enable A2A with NATS](a2a-nats-setup.md). Operators can use this page to understand the keywords shown by `/doctor`, `/a2a peers`, `/a2a status`, and the built-in `bot-tools` MCP tools.

This project does not expose a public A2A HTTP server. It implements a custom internal NATS binding that preserves A2A task concepts where they matter: agent cards, skills, tasks, status, artifacts, cancellation, input/auth replies, durable state, and explicit authorization.

## Design Summary

| Area | Decision |
| --- | --- |
| Transport | NATS plus JetStream. |
| Public HTTP A2A | Not implemented. |
| Routing identity | Runtime agent ID, not Discord bot account. |
| Discovery | Runtime AgentCards through JetStream KV; heartbeats through NATS liveness subjects. |
| Correctness boundary | JetStream plus SQLite task/policy stores. |
| Delivery default | Executor-owned Discord transcript with safe proxy result visibility. |
| Security default | A2A disabled while `NATS_URL` is empty. Remote work requires policy and usually confirmation. |

## Mental Model

```text
Discord bot process/account = transport host and runtime container
Discord bot + guild + channel/thread + A2A policy = runtime agent
NATS-visible AgentID = runtime_agent_id
```

One bot process can publish multiple runtime peers, one per enabled/discoverable channel or thread policy. A remote peer should delegate to the runtime peer, not to the bot process as a whole.

## Keyword Glossary

| Keyword | Meaning |
| --- | --- |
| A2A | Optional cross-bot delegation layer implemented by this project. |
| NATS | External message system used as the transport between bot processes. |
| JetStream | NATS persistence layer used for tasks, controls, events, peer KV, and object artifacts. |
| `A2A_AGENT_ID` | Stable bot/process base identity. It namespaces runtime IDs and credential ownership; it is not the main routing identity in runtime mode. |
| Runtime agent | A Discord channel or thread runtime that can expose skills and receive delegated work. |
| `runtime_agent_id` | Stable NATS-visible identity for one runtime agent. Used in task/control/event/card/heartbeat subjects. |
| `channel_ref` | Subject-safe public alias for the channel runtime. Used for display, migration metadata, and skill context. |
| AgentCard | Public sanitized description of a runtime peer, its supported binding, skills, and display metadata. |
| AgentSkill | A capability exposed by a runtime, such as `task` or a more specific skill ID. |
| Peer | A discovered runtime AgentCard plus local trust/status metadata. |
| Trust | Local policy granting inbound, outbound, or bidirectional task permissions for a peer runtime. |
| Policy | Per-channel A2A rules: enabled, discoverable, accepted senders, exposed skills, delegate targets, visibility, transcript mode, quotas, and tool policy. |
| `delegate_targets` | Outbound allowlist of `{runtime_agent_id, skill_id}` pairs. |
| `accept_from_runtimes` | Inbound allowlist of remote runtime IDs. |
| `co_present_from_runtimes` | Runtimes allowed to share Discord context and co-present replies when other delivery gates pass. |
| Confirmation token | Signed token returned by a plan step and required before applying policy or sensitive remote delegation. |
| TaskStore | Local SQLite durable task and event state. `/a2a status` and `bot_a2a_task_status` read this state. |
| PeerStore | Local SQLite view of known peers, trust display data, staleness, and skills. |
| Object store | JetStream Object Store bucket for larger A2A artifacts. |
| `Nats-Msg-Id` | Stable idempotency header on JetStream publishes. Prevents duplicate effects under redelivery. |
| `proxy` | Safe result mode where the requester bot relays the remote result. |
| `transparent` | Result mode that exposes the remote result more directly while still enforcing policy. |
| `co_present` | Transcript mode where both bots may post in the same Discord channel or thread after policy and Discord permission checks. |
| `legacy` | Migration mode that uses bot-level routing. Not the production target. |
| `dual` | Bounded drain mode that can consume legacy and runtime subjects. Use only during migration. |
| `runtime` | Production target mode. New routing uses exact runtime IDs. |

## Identity Model

### Bot Base Identity

`A2A_AGENT_ID` identifies the bot process and credential owner:

```env
A2A_AGENT_ID=adam-n200
```

It must be stable and subject-safe. It is used to derive runtime IDs and to display bot host identity in audit/doctor output. In runtime mode it is not the user-facing peer route by itself.

### Runtime Agent ID

A runtime ID identifies one channel or thread runtime:

```text
runtime_agent_id = <bot-prefix>-<public-channel-alias-slug>
```

If the alias is unsafe, too long, private, colliding, or contains raw Discord snowflake-like digits, the implementation falls back to a short hash:

```text
runtime_agent_id = <bot-prefix>-rt-<short-hash>
```

Examples:

```text
remote-bot-erp-support
remote-bot-backend
m5bot-main
m5bot-rt-4f8a9c01
```

Runtime IDs must remain stable across restarts. They must not include PID, boot timestamp, random suffix, raw Discord snowflake, private host path, or secret material.

### Channel Reference

`channel_ref` is an operator-readable, subject-safe alias for the runtime channel. It is not the primary durable route in runtime mode, but it appears in peer cards, policy displays, and skill context.

Allowed shape:

```text
[A-Za-z0-9_-]{1,64}
```

Avoid dots, spaces, slashes, wildcard characters, and private channel names unless a manager intentionally makes the alias public.

## Runtime ID Modes

| Mode | Behavior | Use |
| --- | --- | --- |
| `legacy` | Bot-level identity and legacy fields remain active. | Migration compatibility only. |
| `dual` | Runtime cards are published while legacy consumers may still drain old tasks. | Short migration window. |
| `runtime` | New routing uses exact runtime IDs. Legacy bot-level target asks are rejected or require migration. | Production target. |

New deployments should use `A2A_RUNTIME_ID_MODE=runtime`.

## NATS Subject Schema

All production task/control/event traffic uses JetStream. Subjects use the `a2a.v1` prefix.

| Subject | Stream | Purpose |
| --- | --- | --- |
| `a2a.v1.task.<from_runtime>.<to_runtime>.<messageId>` | `A2A_TASKS` | Delegator sends a task to a specific runtime. |
| `a2a.v1.control.<from_runtime>.<executor_runtime>.<taskId>.<kind>` | `A2A_CONTROLS` | Cancel, input reply, auth reply, or other post-accept controls. |
| `a2a.v1.event.<executor_runtime>.<delegator_runtime>.<taskKey>.<kind>` | `A2A_EVENTS` | Accepted, rejected, status, result, and artifact events. |
| `a2a.v1.card.<runtime_agent_id>` | KV or stream | Runtime AgentCard update. |
| `a2a.v1.heartbeat.<runtime_agent_id>.<instance>` | Core or KV | Ephemeral liveness signal. |

Common event/control kinds:

```text
accepted
rejected
status
artifact
result
cancel
input_reply
auth_reply
```

Old unversioned subjects such as `a2a.task.{agent-id}`, `a2a.status.{task-id}`, and `a2a.announce` are not used.

## JetStream Topology

The implementation uses these streams:

| Stream | Subjects | Purpose |
| --- | --- | --- |
| `A2A_TASKS` | `a2a.v1.task.>` | Durable task submissions. |
| `A2A_CONTROLS` | `a2a.v1.control.>` | Durable control messages after acceptance. |
| `A2A_EVENTS` | `a2a.v1.event.>` | Durable accepted/rejected/status/result/artifact events. |
| `A2A_PEERS` KV | runtime peer keys | Peer cards and discovery metadata. |
| `a2a-artifacts` object store | generated object keys | Larger task artifacts. |

Consumers are runtime-targeted. A runtime consumes only task/control/event subjects addressed to its exact runtime ID.

## Task Lifecycle

1. The delegator validates local outbound policy.
2. The delegator publishes a task message with a stable `Nats-Msg-Id`.
3. The executor validates inbound subject, envelope, sender, policy, skill, quota, and runtime context.
4. The executor stores the admitted task durably before acknowledging the JetStream message.
5. The executor runs the task in its own Discord channel or thread runtime.
6. The executor publishes accepted, status, artifact, and result events.
7. The delegator stores received events and reports status through `/a2a status` or `bot_a2a_task_status`.

The system is at-least-once, not exactly-once. Durable idempotency prevents duplicate terminal effects when NATS redelivers messages.

## Task States

| State | Terminal | Meaning |
| --- | --- | --- |
| `TASK_STATE_SUBMITTED` | no | Delegator queued the task. |
| `TASK_STATE_WORKING` | no | Executor accepted the task or emitted progress. |
| `TASK_STATE_INPUT_REQUIRED` | no | Executor needs requester input. |
| `TASK_STATE_AUTH_REQUIRED` | no | Executor needs an authorization decision. |
| `TASK_STATE_COMPLETED` | yes | Task finished successfully. |
| `TASK_STATE_FAILED` | yes | Runtime or execution failure. |
| `TASK_STATE_CANCELED` | yes | Task was canceled. |
| `TASK_STATE_REJECTED` | yes | Policy, auth, skill, quota, or validation denied the task. |

An `accepted` event is an event and state transition into `TASK_STATE_WORKING`; it is not a distinct TaskStore state. A queued tool call is not a completed remote task. Always check task state before reporting completion.

## Policy Model

A2A policy is owned by the channel runtime. Important fields:

| Field | Meaning |
| --- | --- |
| `enabled` | Allows the runtime to participate in A2A. |
| `discoverable` | Publishes a runtime card for discovery. |
| `runtime_agent_id` | Stable runtime route. Required before enabled/discoverable runtime policy can be saved. |
| `accept_from_runtimes` | Runtime IDs allowed to send inbound tasks. |
| `accept_skills` | Skills accepted inbound. |
| `expose_skills` | Local skills shown in the runtime card. |
| `delegate_targets` | Runtime and skill pairs this channel may delegate to. |
| `result_visibility` | Proxy or transparent result behavior. |
| `discord_transcript_mode` | Delegator, mirror, or co-present transcript behavior. |
| `share_discord_context` | Allows co-present context sharing only when transcript mode allows it. |
| `co_present_from_runtimes` | Runtimes allowed for co-present transcript. |
| `co_present_target_channels` | Same-guild target channels/threads allowed for co-present replies. |
| `remote_tool_policy_json.allow_memory_write` | Defaults false; only this can allow remote jobs to use memory-write bot tools. |

Legacy fields such as `accept_from`, `delegate_to`, and `delegate_skills` are compatibility inputs only. New setup writes canonical runtime fields.

## Delivery and Transcript Modes

| Mode | Responsibility |
| --- | --- |
| Delegator/proxy | Requester bot reports remote status/result. Safe cross-server default. |
| Executor-owned | Executor bot runs the real worker transcript in its own channel/thread. |
| Transparent | Result visibility is less mediated, but policy still controls delivery. |
| Co-present | Executor and delegator can share a Discord channel/thread transcript when both policy and permissions allow it. |

`trusted=true` does not imply transparent or co-present readiness. For direct same-thread replies, the opposite bot's inbound policy, co-present allowlist, target channel policy, and Discord send permissions must all pass.

## Security Boundaries

A2A does not bypass existing bot boundaries:

- Remote tasks do not gain extra MCP tools unless the executor channel policy exposes them.
- Remote tasks do not get memory-write permission unless `allow_memory_write=true` is explicitly set.
- Discord delivery still uses safe egress and AllowedMentions guards.
- Confirmation tokens protect policy changes and sensitive remote delegation.
- `/doctor` redacts tokens, credentials, TLS material, private paths, and sensitive environment values.
- Heartbeat is liveness only; it is not authorization.

## Operator Surfaces

| Surface | Use |
| --- | --- |
| `/doctor` | Check A2A enabled state, auth mode, runtime mode, peer status, and readiness without exposing secrets. |
| `/a2a peers` | List visible peer runtimes, skills, trust, staleness, and delivery readiness. |
| `/a2a trust` | High-level trust setup with confirmation. |
| `/a2a ask` | Send a general task to a trusted peer. |
| `/a2a delegate` | Send a task to an explicit target runtime and skill. |
| `/a2a status` | Inspect durable local task state and events. |
| `bot_a2a_*` tools | Agent-facing MCP surface for policy, delegation, status, and peer inspection. |

Do not inspect or edit raw `data/a2a/*.sqlite` files for normal operation.

## Related Pages

- [Enable A2A with NATS](a2a-nats-setup.md)
- [A2A NATS Rollout](a2a-nats-rollout.md)
- [Environment Reference](environment.md)
- [Bot Tools MCP](bot-tools.md)
- [Security Model](security-model.md)
