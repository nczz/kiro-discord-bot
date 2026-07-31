# A2A NATS Rollout Runbook

This runbook covers production rollout of the optional A2A NATS custom binding. Keep `NATS_URL` empty until every gate below passes.

## Deployment model

- Development: one local NATS server with JetStream enabled is enough for two-bot smokes.
- Production: run a three-node NATS cluster with JetStream enabled, persistent storage, TLS, and monitored stream/consumer health.
- The A2A binding is disabled by default. `NATS_URL=""` leaves existing Discord behavior unchanged and should be the first rollback step.
- Production security decision: use NKey/JWT credentials (`NATS_CREDS_FILE`) for client authentication; `NATS_TLS_CA_FILE` is server CA validation only. Shared token-only or unauthenticated production A2A is rejected when `A2A_PRODUCTION_SECURITY=true`.

## Local development setup

Start a local server using the repository example:

```bash
nats-server -c dev/nats.conf
```

Use unique stable bot base IDs per bot process. Runtime peers are derived from enabled channel/thread policies:

```bash
NATS_URL=nats://127.0.0.1:4222 \
A2A_AGENT_ID=adam-n200 \
A2A_RUNTIME_ID_MODE=dual \
A2A_AGENT_NAME=Adam \
A2A_PRODUCTION_SECURITY=false \
./kiro-discord-bot
```

A second bot must use a different `A2A_AGENT_ID`, Discord bot token, `DATA_DIR`, and Discord channel/guild binding. In `dual` mode it may still consume legacy bot-level tasks while publishing runtime cards for discoverable policies.

## Production environment block

Required or recommended values:

```bash
NATS_URL=tls://nats-1.example.internal:4222,tls://nats-2.example.internal:4222,tls://nats-3.example.internal:4222
NATS_CREDS_FILE=/etc/kiro-discord-bot/nats/adam-n200.creds
NATS_TOKEN=
NATS_TLS_CA_FILE=/etc/kiro-discord-bot/nats/ca.pem
A2A_AGENT_ID=adam-n200
A2A_RUNTIME_ID_MODE=runtime
A2A_AGENT_NAME=Adam production bot
A2A_AGENT_DESCRIPTION=Public capability summary only; no paths, hosts, tokens, or user data.
A2A_TASK_TIMEOUT_SEC=3600
A2A_MAX_DELEGATION_DEPTH=1
A2A_AUTO_DELEGATE_ENABLED=false
A2A_REQUIRE_CONFIRMATION_FOR_REMOTE=true
A2A_PRODUCTION_SECURITY=true
A2A_TASK_RETENTION_DAYS=30
A2A_OBJECT_RETENTION_DAYS=30
A2A_MAX_PENDING_TASKS=100
A2A_MAX_OUTBOUND_TASKS_PER_CHANNEL=10
A2A_MAX_INBOUND_TASKS_PER_CHANNEL=10
A2A_MAX_EVENT_RATE_PER_MIN=120
```

Use `A2A_RUNTIME_ID_MODE=dual` only during a bounded legacy drain window. Before changing production to `runtime`, verify no nonterminal legacy bot-level tasks remain and run a negative smoke proving new legacy-addressed `send_message` publishes are rejected whenever a matching runtime card exists.

`NATS_TLS_CA_FILE` validates the NATS server certificate. It is not client mTLS authentication by itself; this implementation's production client credential is `NATS_CREDS_FILE`.

Do not put raw credentials, tokens, private paths, or internal topology in `A2A_AGENT_DESCRIPTION`; peer cards are discoverable by other A2A participants.

## Runtime ACL template

Replace `<runtime>` with the exact `runtime_agent_id` bound to this credential or allowed by this bot-process credential, for example `adam-n200-erp-support`. Do not write a single bot-level allowlist that accidentally authorizes all runtimes.
```text
publish allow:
a2a.v1.task.<runtime>.>
a2a.v1.control.<runtime>.>
a2a.v1.event.<runtime>.>
a2a.v1.card.<runtime>
a2a.v1.heartbeat.<runtime>.>
$KV.A2A_PEERS.<runtime>

subscribe allow:
a2a.v1.task.*.<runtime>.>
a2a.v1.control.*.<runtime>.>
a2a.v1.event.*.<runtime>.>
a2a.v1.card.>
a2a.v1.heartbeat.>
$KV.A2A_PEERS.>

JetStream API allow when this credential provisions its own streams/consumers:
$JS.API.INFO
$JS.API.STREAM.*.A2A_TASKS
$JS.API.STREAM.*.A2A_CONTROLS
$JS.API.STREAM.*.A2A_EVENTS
$JS.API.CONSUMER.*.A2A_TASKS.a2a_tasks_<runtime>
$JS.API.CONSUMER.DURABLE.CREATE.A2A_TASKS.a2a_tasks_<runtime>
$JS.API.CONSUMER.*.A2A_CONTROLS.a2a_controls_<runtime>
$JS.API.CONSUMER.DURABLE.CREATE.A2A_CONTROLS.a2a_controls_<runtime>
$JS.API.CONSUMER.*.A2A_EVENTS.a2a_events_<runtime>
$JS.API.CONSUMER.DURABLE.CREATE.A2A_EVENTS.a2a_events_<runtime>
$JS.API.STREAM.INFO.KV_A2A_PEERS
$JS.API.CONSUMER.CREATE.KV_A2A_PEERS.>
$JS.API.CONSUMER.DELETE.KV_A2A_PEERS.>
```

If operators do not want each bot credential to create/update streams and durable consumers, run an explicit provisioning step with a separate admin credential before bot startup, then remove the stream/consumer API permissions from the runtime credential only after confirming startup no longer needs `EnsureStreams`/`EnsureConsumers`.

Response/inbox rule: production must avoid blanket `_INBOX.>` unless account isolation already enforces tenant boundaries. Request/reply discovery fallback and JetStream API calls must use narrow reply inbox permissions, or a separate bucket-admin credential that is not used for task publish.

## Authenticated-principal binding

- Each NATS credential identity maps to one allowed runtime ID or an explicit set of runtime IDs owned by the same bot process.
- Inbound subject `from` and `Envelope.From` must equal an allowed source runtime for that credential.
- Subject `to` must match `Envelope.To` when `Envelope.To` is present.
- Payload `channelRef`, when present, must match the target policy-derived runtime record; mismatches are confused-deputy failures.
- Credential rotation must preserve the same stable runtime IDs; do not mint PID, boot timestamp, or host-ephemeral IDs.

## Negative ACL smokes

Run these before exposing production channels. A credential for runtime `adam-n200-erp-support` must fail to:

1. Publish `a2a.v1.task.eve-local.adam-n200-erp-support.<messageId>`.
2. Publish `a2a.v1.task.adam-n200-main.eve-local.<messageId>` if `adam-n200-main` is not in that credential's allowed runtime set.
3. Write `$KV.A2A_PEERS.eve-local`.
4. Subscribe to another runtime's narrow task inbox, such as `a2a.v1.task.*.eve-local.>`.

Positive smokes for the same credential must prove it can:

1. Publish its own runtime card or heartbeat to `$KV.A2A_PEERS.adam-n200-erp-support` or `a2a.v1.heartbeat.adam-n200-erp-support.>`.
2. Subscribe/watch `$KV.A2A_PEERS.>` for peer discovery.
3. Subscribe to its own runtime task/control/event inboxes.

## Credential lifecycle

- Issue: create one credential per runtime for high isolation, or one bot-process credential whose ACL explicitly enumerates owned runtime IDs; record owner, Discord runtime scope, public fingerprint, and allowed subject template.
- Rotate: issue the replacement credential, deploy it with the same runtime IDs, restart or drain/reconnect one bot at a time, then revoke the old credential.
- Revoke: remove the credential from NATS, remove the runtime peer from channel policy `delegate_targets`, `accept_from_runtimes`, and `co_present_from_runtimes`, then verify new delegated work is rejected.
- Compromised peer removal: immediately set `NATS_URL=""` on the affected host or stop the bot, revoke the credential, remove the affected runtime from every channel policy, keep `DATA_DIR` for audit/postmortem, and only re-enable with a rotated credential.

## Startup and shutdown ordering

Startup:

1. Start NATS/JetStream and verify stream/account health.
2. Start persistent stores under `DATA_DIR`.
3. Start the bot with A2A disabled or with production credential material present.
4. Confirm `/doctor` shows A2A enabled/disabled, auth material presence, retention, quotas, and no raw tokens or credential paths.
5. Enable per-channel policy through `bot_a2a_policy_plan` and `bot_a2a_policy_apply`; require manager confirmation.

Shutdown:

1. Stop accepting new inbound A2A work by disabling channel policy or setting `NATS_URL=""` for rollback.
2. Let in-flight work finish or cancel it explicitly.
3. Drain NATS subscriptions/connections.
4. Close stores and stop the bot.
5. Keep `DATA_DIR` intact unless a separate retention/postmortem procedure approves deletion.

## Rollout gates

Every production rollout must complete these gates in order:

1. local two-bot smoke: two local bots exchange a delegated text task through embedded or local JetStream.
2. same-channel co-present smoke: both Discord bot accounts can post the expected status/result labels in the same channel/thread with `share_discord_context=true` and approved `co_present_from_runtimes`.
3. cross-server proxy smoke: executor works in a different Discord server/channel and the requester bot reports the result through proxy visibility.
4. NATS restart smoke: restart NATS after an accepted task and verify durable task/result state survives reconnect and replay without duplicate Discord delivery.
5. credential revocation smoke: revoke one peer credential and verify new delegated work from that peer is denied while existing audit/task rows remain readable.
6. runtime cutover smoke: with `A2A_RUNTIME_ID_MODE=runtime`, new legacy bot-level `target_agent + target_channel_ref` asks are rejected or require exact runtime migration; no bot-level legacy consumer accepts new work.
7. rollback smoke: set `NATS_URL=""`, restart/drain, verify `/doctor` reports disabled and a normal non-A2A Discord agent reply still works.

## Final validation command matrix

Run before merging or tagging rollout changes:

```bash
go test ./a2a ./channel ./internal/botmcp ./bot ./audit ./locale -run 'Test.*A2A|TestDoctor.*A2A'
python3 - <<'PY'
from pathlib import Path
rollout = Path('docs/a2a-nats-rollout.md').read_text()
for required in ['local two-bot smoke', 'same-channel co-present smoke', 'cross-server proxy smoke', 'NATS restart smoke', 'credential revocation smoke']:
    assert required in rollout
print('a2a-rollout-guide-ok')
PY
```

## Rollback

1. Set `NATS_URL=""` and restart or drain the bot.
2. Verify `/doctor` reports A2A disabled.
3. Send a normal non-A2A Discord agent message and verify the response path still works.
4. Keep A2A SQLite/object rows and audit DB under `DATA_DIR` for postmortem.
5. Revert runbook/config snippets only if the rollout itself is canceled; Phases 1-8 remain inert while A2A is disabled.
