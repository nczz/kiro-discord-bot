# A2A NATS Rollout Runbook

This runbook covers production rollout of the optional A2A NATS custom binding. Keep `NATS_URL` empty until every gate below passes.

For first-time enablement from NATS server setup through bot `.env` and Discord policy, start with [Enable A2A with NATS](a2a-nats-setup.md). For the implementation glossary and protocol model, see [A2A Protocol Model](a2a-protocol.md).

## Deployment model

- Development: one local NATS server with JetStream enabled is enough for two-bot smokes.
- Internal lightweight production: one private single-node NATS server with JetStream enabled, persistent storage, TLS, token authentication, localhost-only monitoring, and host/network firewalling is the default deployment model for this project. This trades HA for simple operations; bot-side durable stores and rollback gates remain mandatory.
- HA production remains optional: use a three-node JetStream cluster only when service availability requirements justify the operational cost.
- The A2A binding is disabled by default. `NATS_URL=""` leaves existing Discord behavior unchanged and should be the first rollback step.
- Production security decision: the lightweight internal profile may use `NATS_TOKEN` with TLS only when `A2A_PRODUCTION_SECURITY=false` and the NATS listener is restricted to trusted hosts. If `A2A_PRODUCTION_SECURITY=true`, use NKey/JWT credentials (`NATS_CREDS_FILE`) for client authentication; `NATS_TLS_CA_FILE` is server CA validation only, and shared token-only or unauthenticated A2A is rejected.

## Local development setup

Start a local server using the repository example:

```bash
nats-server -c dev/nats.conf
```

Use unique stable logical IDs per bot:

```bash
NATS_URL=nats://127.0.0.1:4222 \
A2A_AGENT_ID=adam-n200 \
A2A_AGENT_NAME=Adam \
A2A_PRODUCTION_SECURITY=false \
./kiro-discord-bot
```

A second bot must use a different `A2A_AGENT_ID`, Discord bot token, `DATA_DIR`, and Discord channel/guild binding.

## Production environment block

Required or recommended values:

```bash
NATS_URL=tls://nats.example.internal:4222
NATS_CREDS_FILE=
NATS_TOKEN=<internal-shared-token-or-empty-when-using-creds>
NATS_TLS_CA_FILE=/etc/kiro-discord-bot/nats/ca.pem
A2A_AGENT_ID=adam-n200
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

`NATS_TLS_CA_FILE` validates the NATS server certificate. It is not client mTLS authentication by itself; this implementation's production client credential is `NATS_CREDS_FILE`.

Do not put raw credentials, tokens, private paths, or internal topology in `A2A_AGENT_DESCRIPTION`; peer cards are discoverable by other A2A participants.


For the internal lightweight profile, document the single-node tradeoff before rollout: NATS outage pauses new remote work, but JetStream/persistent bot stores must preserve accepted task state across restart. Do not expose the NATS listener broadly; prefer private IP/VPN/firewall allowlists plus TLS server validation.
## One-agent ACL template

Replace `<self>` with the exact `A2A_AGENT_ID` bound to this credential. Do not reuse the credential for another agent.

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

JetStream API allow when this credential provisions its own streams/consumers:
$JS.API.INFO
$JS.API.STREAM.*.A2A_TASKS
$JS.API.STREAM.*.A2A_CONTROLS
$JS.API.STREAM.*.A2A_EVENTS
$JS.API.CONSUMER.*.A2A_TASKS.a2a_tasks_<self>
$JS.API.CONSUMER.DURABLE.CREATE.A2A_TASKS.a2a_tasks_<self>
$JS.API.CONSUMER.*.A2A_CONTROLS.a2a_controls_<self>
$JS.API.CONSUMER.DURABLE.CREATE.A2A_CONTROLS.a2a_controls_<self>
$JS.API.CONSUMER.*.A2A_EVENTS.a2a_events_<self>
$JS.API.CONSUMER.DURABLE.CREATE.A2A_EVENTS.a2a_events_<self>
$JS.API.STREAM.INFO.KV_A2A_PEERS
$JS.API.CONSUMER.CREATE.KV_A2A_PEERS.>
$JS.API.CONSUMER.DELETE.KV_A2A_PEERS.>
```

If operators do not want each bot credential to create/update streams and durable consumers, run an explicit provisioning step with a separate admin credential before bot startup, then remove the stream/consumer API permissions from the runtime credential only after confirming startup no longer needs `EnsureStreams`/`EnsureConsumers`.

Response/inbox rule: production must avoid blanket `_INBOX.>` unless account isolation already enforces tenant boundaries. Request/reply discovery fallback and JetStream API calls must use narrow reply inbox permissions, or a separate bucket-admin credential that is not used for task publish.

## Authenticated-principal binding

- Each NATS credential identity maps to exactly one allowed `AgentID`.
- Inbound subject `from` and `Envelope.From` must equal the authenticated identity.
- Subject `to` must match `Envelope.To` when `Envelope.To` is present.
- Credential rotation must preserve the same stable `A2A_AGENT_ID`; do not mint PID, boot timestamp, or host-ephemeral IDs.

## Negative ACL smokes

Run these before exposing production channels. A credential for `adam-n200` must fail to:

1. Publish `a2a.v1.task.eve-local.adam-n200.<messageId>`.
2. Write `$KV.A2A_PEERS.eve-local`.
3. Subscribe to another agent's narrow task inbox, such as `a2a.v1.task.*.eve-local.>`.

Positive smokes for the same credential must prove it can:

1. Publish its own card or heartbeat to `$KV.A2A_PEERS.adam-n200` or `a2a.v1.heartbeat.adam-n200.>`.
2. Subscribe/watch `$KV.A2A_PEERS.>` for peer discovery.
3. Subscribe to its own task/control/event inboxes.

## Credential lifecycle

- Issue: create one credential per bot logical `A2A_AGENT_ID`; record owner, Discord guild/channel scope, public fingerprint, and allowed subject template.
- Rotate: issue the replacement credential, deploy it with the same `A2A_AGENT_ID`, restart or drain/reconnect one bot at a time, then revoke the old credential.
- Revoke: remove the credential from NATS, remove the peer from channel policy `delegate_to`, `accept_from`, and `co_present_from`, then verify new delegated work is rejected.
- Compromised peer removal: immediately set `NATS_URL=""` on the affected host or stop the bot, revoke the credential, remove the peer from every channel policy, keep `DATA_DIR` for audit/postmortem, and only re-enable with a rotated credential.

## Startup and shutdown ordering

Startup:

1. Start NATS/JetStream and verify stream/account health.
2. Start persistent stores under `DATA_DIR`.
3. Start the bot with A2A disabled or with production credential material present.
4. Confirm `/doctor` shows A2A enabled/disabled, auth material presence, retention, quotas, and no raw tokens or credential paths.
5. Enable per-channel receiver consent with `/a2a allow peer_agent:<runtime>` or `bot_a2a_trust_peer` using only `target_agent`; retired expert bot-tools policy apply surfaces must not be used for rollout.

Shutdown:

1. Stop accepting new inbound A2A work by disabling channel policy or setting `NATS_URL=""` for rollback.
2. Let in-flight work finish or cancel it explicitly.
3. Drain NATS subscriptions/connections.
4. Close stores and stop the bot.
5. Keep `DATA_DIR` intact unless a separate retention/postmortem procedure approves deletion.

## Rollout gates

Every production rollout must complete the gate profile that matches its security/topology mode.

Internal lightweight profile (`A2A_PRODUCTION_SECURITY=false`, private single-node NATS, TLS + token):

1. current-binary deploy: both bot processes run the committed binary with `A2A_RUNTIME_ID_MODE=runtime`, `NATS_TOKEN` set, and TLS CA validation configured.
2. exact runtime smoke: a runtime-addressed delegated text task completes through NATS/JetStream.
3. legacy rejection smoke: a new bot-level `target_agent + target_channel_ref` ask is rejected in `runtime` mode.
4. service health smoke: both bots report A2A NATS enabled and runtime transport consumers started; the single NATS service remains active.
5. rollback readiness: binary/env backups exist and rollback is `NATS_URL=""` plus bot restart/drain, or restoring the previous binary/env backup.

Hardened/HA profile (`A2A_PRODUCTION_SECURITY=true` or multi-tenant exposure):

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
