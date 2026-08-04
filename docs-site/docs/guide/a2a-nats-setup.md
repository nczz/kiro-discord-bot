# Enable A2A with NATS

A2A lets one `kiro-discord-bot` runtime delegate work to another bot runtime through NATS and JetStream. It is optional. When `NATS_URL` is empty, A2A is disabled and normal Discord bot behavior is unchanged.

Use this guide when you want to turn the feature on. For the implementation model and glossary, see [A2A Protocol Model](a2a-protocol.md). For release gates, ACL hardening, and rollback smoke checks, see [A2A NATS Rollout](a2a-nats-rollout.md). For every environment variable, see [Environment Reference](environment.md).

## What A2A Does

A2A provides durable cross-bot task delegation:

- One channel runtime can ask another runtime to run a task.
- Task, status, result, and artifact metadata move through NATS/JetStream subjects; larger artifact bytes use the A2A object store.
- The bot still enforces Discord permissions, channel policy, MCP policy, safe egress, audit, and confirmation gates.
- Peer discovery and trust are explicit; seeing a peer online is not enough to delegate to it.

A2A does not replace normal Discord replies. It only runs when NATS is configured and a channel policy allows the peer and skill.

## Deployment Shapes

| Shape | Use when | NATS auth | Notes |
| --- | --- | --- | --- |
| Local development | Two-bot smoke tests on one machine | none or dev token | Fastest way to verify the flow. |
| Internal lightweight | Private trusted network, low operational burden | TLS plus `NATS_TOKEN` | Acceptable only with private/firewalled listeners and `A2A_PRODUCTION_SECURITY=false`. |
| Hardened production | Stricter production or multi-host exposure | `NATS_CREDS_FILE` NKey/JWT | Required when `A2A_PRODUCTION_SECURITY=true`. |
| HA production | NATS availability is critical | NKey/JWT plus JetStream cluster | Optional; use only when the operational cost is justified. |

For most private deployments, start with one private JetStream node, persistent storage, TLS, token authentication, localhost-only monitoring, and host firewalling. Move to NKey/JWT or a cluster when the risk profile requires it.

## Prerequisites

Before enabling A2A:

1. Each bot must already work as a normal Discord bot.
2. Each bot must have its own `DISCORD_TOKEN`.
3. Each bot should have its own `DATA_DIR`; do not share state between bot identities.
4. Each bot needs a stable `A2A_AGENT_ID`.
5. NATS must have JetStream enabled.
6. The bot process environment must inject A2A variables. The bot does not load `.env` by itself.
7. The Discord channels that will use A2A must already be initialized with `/cwd` or `/start`.

## Install NATS Server and CLI

Install the upstream NATS server and CLI for your platform:

- NATS Server: https://docs.nats.io/running-a-nats-service/introduction/installation
- NATS CLI: https://docs.nats.io/using-nats/nats-tools/nats_cli

Confirm both commands are available:

```bash
nats-server --version
nats --version
```

## Local Development NATS

Start the repository development server:

```bash
nats-server -c dev/nats.conf
```

Verify JetStream:

```bash
nats --server nats://127.0.0.1:4222 server check jetstream
nats --server nats://127.0.0.1:4222 stream ls
```

The first bot startup creates the task, control, and event streams and the runtime consumers when the NATS credential allows JetStream setup. Peer publishing creates or updates the peer KV bucket. The object store is created lazily when an A2A artifact is written.

## Production NATS Server

### Internal Lightweight Profile

Use this only for private/internal deployments where the single-node and shared-token tradeoff is intentionally accepted.

Minimum requirements:

- JetStream enabled.
- Persistent JetStream storage.
- TLS listener.
- Token authentication.
- Monitoring bound to `127.0.0.1`.
- Firewall, VPN, or private routing restricts client access to trusted bot hosts.

Example NATS config skeleton:

```conf
server_name: a2a-nats
port: 4222
http: 127.0.0.1:8222

jetstream {
  store_dir: "/var/lib/nats/jetstream"
}

authorization {
  token: "<set-a-random-token>"
}

tls {
  cert_file: "/etc/nats/certs/server.crt"
  key_file: "/etc/nats/certs/server.key"
  ca_file: "/etc/nats/certs/ca.pem"
}
```

This profile is lower-security than per-agent credentials. Do not expose the NATS listener broadly.

### Hardened Production Profile

Use NKey/JWT credentials for hardened production. Set:

```env
A2A_PRODUCTION_SECURITY=true
NATS_CREDS_FILE=/etc/kiro-discord-bot/nats/bot.creds
NATS_TOKEN=
NATS_TLS_CA_FILE=/etc/kiro-discord-bot/nats/ca.pem
```

`NATS_TLS_CA_FILE` validates the NATS server certificate. It is not client authentication by itself. `NATS_TOKEN` is not accepted as the only production credential when `A2A_PRODUCTION_SECURITY=true`.

## Configure Bot A

Use one stable base identity for each bot process. Runtime mode publishes one runtime peer per enabled/discoverable Discord channel or thread policy.

Example internal lightweight `.env` block for Bot A:

```env
DISCORD_TOKEN=<bot-a-discord-token>
DISCORD_GUILD_ID=<guild-id>
DEFAULT_CWD=/projects
DATA_DIR=/var/lib/kiro-discord-bot/bot-a

NATS_URL=tls://nats.example.internal:4222
NATS_TOKEN=<internal-token>
NATS_CREDS_FILE=
NATS_TLS_CA_FILE=/etc/kiro-discord-bot/nats/ca.pem

A2A_CONFIRMATION_SECRET=<random-secret>
A2A_AGENT_ID=adam-n200
A2A_RUNTIME_ID_MODE=runtime
A2A_AGENT_NAME=Adam
A2A_AGENT_DESCRIPTION=General project assistant. No secrets, paths, hosts, or user data.
A2A_PRODUCTION_SECURITY=false
A2A_REQUIRE_CONFIRMATION_FOR_REMOTE=true
A2A_AUTO_DELEGATE_ENABLED=false
A2A_MAX_DELEGATION_DEPTH=1
```

## Configure Bot B

Bot B must use a different Discord token, `DATA_DIR`, and `A2A_AGENT_ID`.

```env
DISCORD_TOKEN=<bot-b-discord-token>
DISCORD_GUILD_ID=<guild-id-or-other-guild-id>
DEFAULT_CWD=/projects
DATA_DIR=/var/lib/kiro-discord-bot/bot-b

NATS_URL=tls://nats.example.internal:4222
NATS_TOKEN=<internal-token>
NATS_CREDS_FILE=
NATS_TLS_CA_FILE=/etc/kiro-discord-bot/nats/ca.pem

A2A_CONFIRMATION_SECRET=<random-secret>
A2A_AGENT_ID=eve-local
A2A_RUNTIME_ID_MODE=runtime
A2A_AGENT_NAME=Eve
A2A_AGENT_DESCRIPTION=Backend review assistant. No secrets, paths, hosts, or user data.
A2A_PRODUCTION_SECURITY=false
A2A_REQUIRE_CONFIRMATION_FOR_REMOTE=true
A2A_AUTO_DELEGATE_ENABLED=false
A2A_MAX_DELEGATION_DEPTH=1
```

Never put raw credentials, private paths, hostnames, Discord IDs, or internal topology in `A2A_AGENT_DESCRIPTION`; peer cards are discoverable by other A2A participants.

## Start or Restart the Bots

Foreground smoke:

```bash
set -a
. ./.env
set +a
./kiro-discord-bot
```

Systemd example:

```bash
sudo systemctl restart kiro-discord-bot
sudo systemctl status kiro-discord-bot
```

Expected log markers include NATS enabled, transport consumers started, and bot running. Do not print full environments or secrets while troubleshooting.

## Verify with `/doctor`

In each Discord channel that will use A2A, run:

```text
/doctor
```

Expected:

- A2A reports enabled.
- Runtime mode is `runtime`.
- NATS auth mode is present.
- Transport consumers started.
- No raw tokens, credential file contents, or TLS material are printed.
- Known A2A peers appear after their AgentCards are published; heartbeats only update liveness for known peers.

If `/doctor` reports A2A disabled, inspect the service environment first. `NATS_URL` is the switch.

## Enable Channel Policy in Discord

A2A is process-enabled by environment variables, but each Discord channel still needs explicit policy.

First list visible peers:

```text
/a2a peers
```

Allow one peer runtime to send normal tasks into this channel:

```text
/a2a allow peer_agent:<peer-runtime-agent-id>
```

This receiver-side consent applies immediately to the exact runtime ID. It is not a wildcard bot-prefix grant, not bidirectional trust, and does not configure co-present reply mode.

Co-present requires matching policy, Discord send permissions, and delivery readiness on both sides. `trusted=true` alone is not enough.

Agents can use the built-in high-level `bot-tools` MCP tools:

- `bot_a2a_peers`
- `bot_a2a_trust_peer`
- `bot_a2a_delegate`
- `bot_a2a_task_status`

Do not edit `data/a2a/*.sqlite` directly.

## Send a Test Task

After consent is applied, send a delegated task:

```text
/a2a ask peer_agent:<peer-runtime-agent-id> message:"Please reply with a short A2A smoke-test confirmation."
```


Then inspect status:

```text
/a2a status
```

A successful send means the task was durably queued. It does not mean the remote bot accepted or completed it. Use `/a2a status` or `bot_a2a_task_status` for the authoritative state.

Common TaskStore states:

| State | Meaning |
| --- | --- |
| `TASK_STATE_SUBMITTED` | Local bot queued the task. |
| `TASK_STATE_WORKING` | Remote runtime accepted the task or emitted progress. |
| `TASK_STATE_COMPLETED` | Remote runtime finished successfully. |
| `TASK_STATE_FAILED` | Runtime or execution failed. |
| `TASK_STATE_CANCELED` | Task was canceled. |
| `TASK_STATE_REJECTED` | Policy, skill, quota, auth, or runtime validation rejected the task. |

An `accepted` event is recorded as task progress and moves the durable task state to `TASK_STATE_WORKING`; it is not a separate TaskStore state.

## Delivery Modes

| Mode | Behavior |
| --- | --- |
| `proxy` or safe | The requester bot relays the remote result. This is the safest default across servers. |
| `transparent` | The result is exposed more directly, but policy still controls delivery. |
| `co_present` | Both bots may post in the same Discord channel or thread. Requires matching policy and Discord permissions. |

Use safe/proxy first. Move to co-present only when both operators expect direct same-channel collaboration.

## Troubleshooting

| Symptom | Likely cause | Check |
| --- | --- | --- |
| `/doctor` says A2A disabled | `NATS_URL` is empty or not injected into the service | Check the process manager environment. |
| Startup fails with missing `A2A_AGENT_ID` | `NATS_URL` is set without a stable agent ID | Set `A2A_AGENT_ID`. |
| Startup rejects token-only production | `A2A_PRODUCTION_SECURITY=true` without `NATS_CREDS_FILE` | Use NKey/JWT creds or intentionally choose the internal lightweight profile. |
| Peer is not visible | Peer card not published, ACL issue, stale KV, or wrong runtime mode | Run `/doctor`, `/a2a peers`, and inspect NATS logs. |
| Delegation is rejected | Policy does not allow sender, skill, or target | Run `/a2a peers` and `/a2a allow`, or inspect readiness with bot-tools. |
| Co-present does not work | Missing `co_present_from_runtimes`, target channel allowlist, or Discord send permission | Check delivery readiness in `/doctor` or `/a2a peers`. |
| Events appear delayed | JetStream redelivery or remote runtime delay | Check `/a2a status`; idempotency should prevent duplicate terminal delivery. |

## Rollback

To disable A2A without changing normal Discord behavior:

1. Set `NATS_URL=""`.
2. Restart or drain the bot.
3. Run `/doctor`.
4. Send a normal non-A2A Discord message and verify the bot replies.
5. Keep `DATA_DIR` for audit and postmortem unless a retention procedure says otherwise.

Rollback does not require deleting A2A SQLite rows or JetStream state.
