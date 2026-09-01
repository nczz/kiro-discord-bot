# Environment Reference

The bot does not load `.env` by itself. Inject these variables through your shell, launchd, systemd, Docker, or another process manager.

Use `/doctor` after startup to inspect effective values. Secrets are redacted in diagnostics.

## How to Use This Page

Environment variables fall into four groups:

- Main bot runtime: Discord connection, ACP agent engines, channel/thread behavior, WebShare, audit, usage, and background maintenance.
- WebShare relay: the separate self-hosted relay process used by browser shares.
- MCP helper servers: standalone processes such as `mcp-discord-server` and `mcp-media-server`.
- Provider credentials: API keys for Kiro, STT, media generation, or other external services.

Required variables must be set before startup. Optional variables can usually stay empty because the bot applies conservative defaults. After changing any process-level environment variable, restart the service and run `/doctor` in Discord to confirm the effective runtime. `/doctor` redacts secrets, so it is the safest way to validate production configuration.

Existing Kiro-only deployments do not need new OMP variables. Add OMP variables only when the host has `omp` installed, authenticated, and intentionally enabled.

`kiro-cli` and `omp` are installed and updated outside this repository. See [Installation](installation.md) for basic CLI setup and update commands, and use the upstream docs for platform-specific details.

## Common Configuration Shapes

### Kiro-Only Default

This is the default upgrade path for existing deployments. OMP is not required.

```env
AGENT_ENGINE=kiro
AGENT_ENGINES_ENABLED=
```

### Dual-Engine Bot

Use this when the same bot should allow channel admins to switch between Kiro and OMP with `/engine`.

```env
AGENT_ENGINE=kiro
AGENT_ENGINES_ENABLED=kiro,omp
OMP_PATH=omp
```

Only enable OMP after `omp` is installed and authenticated for the service user.

### OMP With a Production Profile

Use a named profile when you want bot-managed OMP auth, settings, sessions, and caches to stay isolated from your interactive OMP profile.

```bash
OMP_PROFILE=kiro-discord-bot omp setup
```

```env
OMP_PROFILE=kiro-discord-bot
```

Leave `OMP_PROFILE` empty if you intentionally want the service to use OMP's default profile for backward compatibility.

### Pure OMP Bot

Use this only when Kiro should not be available to the bot.

```env
AGENT_ENGINE=omp
AGENT_ENGINES_ENABLED=omp
OMP_PATH=omp
```

### Multi-Bot Deployment

When running multiple department bots, give each bot its own Discord token and persistent data directory.

```env
DISCORD_TOKEN=...
DATA_DIR=/var/lib/kiro-discord-bot/marketing
BOT_PEERS=...
```

Do not share `DATA_DIR` between bot identities. Audit DBs, the usage SQLite DB and migrated JSONL archives, channel settings, MCP policy, and agent runtime files are bot-owned state.

### WebShare Bot and Relay

Enable WebShare only after a self-host relay is reachable over HTTPS/WSS and the bot can authenticate as the relay host.

```env
WEBSHARE_ENABLED=true
WEBSHARE_RELAY_URL=wss://relay.example
WEBSHARE_PUBLIC_BASE_URL=https://relay.example
WEBSHARE_HOST_TOKEN_FILE=/etc/kdb-webshare/host-token
```

Use the matching relay-side `RELAY_HOST_TOKEN_FILE` or `RELAY_HOST_TOKEN`. The public base URL is what users open in the browser; the relay URL is the relay origin or route prefix the bot uses before appending `/r/<room>`.

## Variable Relationships

- `DATA_DIR` owns persistent bot state: channel metadata, audit DB, usage SQLite DB and migration archives, MCP policy, downloaded attachments, and bot-managed engine runtime directories.
- `DEFAULT_CWD` is the default project root shown during setup. `ALLOWED_CWD_ROOTS` restricts what channel working directories may be selected.
- `AGENT_ENGINE` selects the default engine for new scopes. `AGENT_ENGINES_ENABLED` controls what `/engine` may switch to.
- `OMP_SESSION_DIR` controls where bot-started OMP ACP session files live. `OMP_PROFILE` controls OMP auth/settings/cache identity. They solve different isolation problems.
- `KIRO_MCP_CONFIG` is treated as an MCP catalog source. Runtime agents receive bot-managed, per-policy MCP settings under `DATA_DIR`, rather than inheriting the user's Kiro settings directly.
- `TRUST_ALL_TOOLS` and `TRUST_TOOLS` approve ACP server permission requests. They do not replace Discord command ACLs or MCP channel policy.
- `PREFLIGHT_MODE=skip` is the explicit way to disable ACP preflight. `SKIP_PREFLIGHT` exists for compatibility and skips preflight when non-empty.
- WebShare uses two processes: the bot keeps durable share state under `DATA_DIR/webshare`, while the relay only serves static assets and routes encrypted WebSocket frames.
- `WEBSHARE_PUBLIC_BASE_URL` must match the browser-facing HTTPS origin. `WEBSHARE_RELAY_URL` should point at the relay origin, usually the same origin with `wss://`; the bot appends `/r/<room>` for each host WebSocket.
- Bot `WEBSHARE_HOST_TOKEN_FILE`/`WEBSHARE_HOST_TOKEN` must match relay `RELAY_HOST_TOKEN_FILE`/`RELAY_HOST_TOKEN`; prefer file-based secrets in production.

## Upgrade Notes

- Upgrading a Kiro-only deployment keeps working with no new environment variables.
- WebShare remains disabled on upgrade unless `WEBSHARE_ENABLED=true` and relay URLs/tokens are configured.
- Do not copy `OMP_PROFILE` into production until that profile has been authenticated as the same OS service user that runs the bot.
- After changing engine, MCP, audit, or storage variables, restart the service and run `/doctor`.
- For launchd, systemd, or Docker deployments, put variables in the service definition rather than assuming an interactive shell profile will be inherited.

## Required

| Variable | Default | Purpose |
| --- | --- | --- |
| `DISCORD_TOKEN` | required | Discord bot token. |

## Core Runtime

| Variable | Default | Purpose |
| --- | --- | --- |
| `DISCORD_GUILD_ID` | empty | Guild used for slash command registration. Empty uses Discord's global command scope. |
| `KIRO_CLI_PATH` | `kiro-cli` | Executable path for Kiro CLI. |
| `OMP_PATH` | `omp` | Executable path for the omp engine (only needed when omp is enabled). |
| `OMP_PROFILE` | empty | Optional OMP profile used by bot-managed OMP agents. OMP profiles isolate auth, settings, sessions, and caches. New production deployments should set `kiro-discord-bot` and authenticate that profile before enabling OMP. Empty keeps OMP's default profile for backward compatibility. |
| `OMP_SESSION_DIR` | `DATA_DIR/omp-agent-runtime/sessions` | Bot-managed OMP session directory passed to `omp --session-dir`. Leave empty to use the data-dir default, or set an absolute path when the service needs a shared session directory. |
| `AGENT_ENGINE` | `kiro` | Default agent engine for new channels: `kiro` or `omp`. |
| `AGENT_ENGINES_ENABLED` | (AGENT_ENGINE only) | Comma list of engines `/engine` may switch to (e.g. `kiro,omp`). Empty disables switching. |
| `KIRO_API_KEY` | empty | Headless Kiro authentication key when `kiro-cli login` is not used. |
| `DEFAULT_CWD` | `/projects` | Root shown by `/cwd` setup. |
| `ALLOWED_CWD_ROOTS` | empty | Optional comma-separated root allowlist for channel working directories. |
| `DATA_DIR` | `./data` | Persistent bot data, channel metadata, sessions, audit DB, usage SQLite DB and migration archives, MCP policy, and bot-managed engine runtime directories. |
| `BOT_LOCALE` | `en` | Bot response locale. Supported project locales are English and Traditional Chinese. |

## Agent Execution

| Variable | Default | Purpose |
| --- | --- | --- |
| `ASK_TIMEOUT_SEC` | `3600` | Maximum wait for a single agent request. |
| `QUEUE_BUFFER_SIZE` | `20` | Per-target job queue buffer. |
| `STREAM_UPDATE_SEC` | `3` | Minimum streaming update interval. |
| `MAX_SCANNER_BUFFER_MB` | `64` | Scanner buffer for long Kiro CLI output. |
| `DOWNLOAD_TIMEOUT_SEC` | `120` | Attachment download timeout. |
| `KIRO_MODEL` | empty | Initial model override. |
| `KIRO_AGENT` | empty | Initial Kiro agent profile or mode. |
| `TRUST_ALL_TOOLS` | `true` | If exactly `true`, ACP server permission requests are approved by default. Any other value denies by default unless covered by `TRUST_TOOLS`. |
| `TRUST_TOOLS` | empty | Optional comma-separated allowlist for trusted tool approvals. |
| `KIRO_MCP_CONFIG` | empty | Optional MCP catalog source. Runtime agents receive isolated settings under `DATA_DIR/kiro-agent-runtime/`. |

## Thread and Listen Behavior

| Variable | Default | Purpose |
| --- | --- | --- |
| `THREAD_AUTO_ARCHIVE` | `1440` | Auto-archive duration for task threads, in minutes. |
| `THREAD_AGENT_MAX` | `5` | Maximum active thread agents. Values below `1` are invalid at startup. |
| `THREAD_AGENT_IDLE_SEC` | `900` | Idle timeout for thread agents. |
| `CHANNEL_AGENT_IDLE_SEC` | `0` | Idle timeout for channel agents. `0` disables channel-agent idle shutdown. |
| `BOT_PEERS` | empty | Comma-separated bot peer hints for multi-bot mention and handoff behavior. |

## Time, Usage, and Maintenance

| Variable | Default | Purpose |
| --- | --- | --- |
| `HEARTBEAT_SEC` | `60` | Background maintenance tick. |
| `CRON_TIMEZONE` | empty | Time zone for scheduled jobs. |
| `CRON_TIMEOUT_MIN` | `5` | Cron job agent execution timeout, in minutes. Values below `1` fall back to `5`. |
| `USAGE_TIMEZONE` | `CRON_TIMEZONE`, then local default | Time zone for `/usage` day, week, and month windows. |
| `USAGE_RETENTION_MONTHS` | `0` | Online SQLite usage retention in months. `0` keeps all rows; archived legacy JSONL migration backups are unaffected. |
| `ATTACHMENT_RETAIN_DAYS` | `7` | Retention for downloaded Discord attachments. |
| `ATTACHMENT_MAX_MB` | `25` | Maximum attachment size accepted by the bot. |
| `PREFLIGHT_MODE` | `warn` | ACP compatibility preflight mode. `strict` exits on failure, `skip` disables the check, and unknown values fall back to warn. |
| `SKIP_PREFLIGHT` | empty | Any non-empty value skips ACP preflight. Prefer `PREFLIGHT_MODE=skip` for explicit configuration. |

## WebShare Bot

WebShare is disabled while `WEBSHARE_ENABLED=false` or empty. When enabled, `/webshare start` creates a target-scoped delegated browser link. See [WebShare](webshare.md) for the deployment runbook and security model.

| Variable | Default | Purpose |
| --- | --- | --- |
| `WEBSHARE_ENABLED` | `false` | Enables `/webshare start`, `/webshare stop`, `/webshare status`, and `/webshare revoke`. |
| `WEBSHARE_RELAY_URL` | empty | Relay origin or route prefix the bot connects through as host, for example `wss://relay.example`. Required when WebShare is enabled. |
| `WEBSHARE_PUBLIC_BASE_URL` | empty | Browser-facing HTTPS base URL used to build control and view links, for example `https://relay.example`. Required when WebShare is enabled. |
| `WEBSHARE_HOST_TOKEN_FILE` | empty | Path to the relay host bearer token. Preferred production setting. |
| `WEBSHARE_HOST_TOKEN` | empty | Relay host bearer token value. Use only for local development or secret-manager injection. |
| `WEBSHARE_MAX_FRAME_BYTES` | `4194304` | Maximum encrypted relay frame size accepted by the bot. Keep aligned with `RELAY_MAX_FRAME_BYTES`. |
| `WEBSHARE_RECONNECT_INITIAL_MS` | `1000` | Initial bot-to-relay reconnect backoff in milliseconds. |
| `WEBSHARE_RECONNECT_MAX_MS` | `30000` | Maximum bot-to-relay reconnect backoff in milliseconds. |

## WebShare Relay

These variables configure the standalone `webshare-relay` process, not the main bot process unless both are launched from the same environment.

| Variable | Default | Purpose |
| --- | --- | --- |
| `RELAY_ADDR` | `:8080` | HTTP listen address for static assets, WebSocket rooms, health, and optional local reverse proxying. |
| `RELAY_PUBLIC_BASE_URL` | empty | Browser-facing HTTPS base URL used by relay healthcheck defaults and operator diagnostics. |
| `RELAY_HOST_TOKEN_FILE` | empty | File containing the bearer token required for bot host WebSocket connections. Preferred production setting. |
| `RELAY_HOST_TOKEN` | empty | Bearer token value for host WebSocket authentication. The relay refuses to serve without either token source. |
| `RELAY_TRUST_PROXY` | `false` | Trust `X-Forwarded-*` headers from the reverse proxy. Enable only when requests arrive through a trusted proxy. |
| `RELAY_MAX_ROOMS` | `1000` | Maximum concurrent relay rooms. |
| `RELAY_MAX_PEERS_PER_ROOM` | `32` | Maximum connected guest peers per room. |
| `RELAY_MAX_FRAME_BYTES` | `4194304` | Maximum opaque encrypted frame size the relay routes. |
| `RELAY_HOST_IDLE_TIMEOUT` | `0` | Host idle timeout duration. `0` disables application-level idle expiry. |
| `RELAY_GUEST_IDLE_TIMEOUT` | `0` | Guest idle timeout duration. `0` disables application-level idle expiry. |
| `RELAY_WRITE_TIMEOUT` | `30s` | Per-write timeout for relay WebSocket writes. Must be greater than zero. |
| `RELAY_LOG_LEVEL` | `info` | Relay log level: `debug`, `info`, `warn`, or `error`. |
| `RELAY_METRICS_ADDR` | empty | Optional Prometheus metrics listen address, commonly `127.0.0.1:9090`. |

## Audit

| Variable | Default | Purpose |
| --- | --- | --- |
| `AUDIT_LOG_ENABLED` | `true` | Enable audit recording. |
| `AUDIT_LOG_DB` | `DATA_DIR/audit/discord.sqlite` | SQLite audit database path. |
| `AUDIT_LOG_RETENTION_DAYS` | `0` | Audit retention. `0` keeps all rows. |
| `AUDIT_LOG_QUEUE_SIZE` | `1000` | Async audit queue size. If full, audit-only events may be dropped and logged. |
| `AUDIT_LOG_RECORD_CONTENT` | `true` | Include message content in audit projections and raw event payloads. |
| `AUDIT_LOG_RECORD_TYPING` | `false` | Record Discord typing events. |


## A2A NATS Custom Binding

A2A is disabled while `NATS_URL` is empty. Existing Discord behavior should remain unchanged in that state. If `NATS_URL` is set, startup requires a valid `A2A_AGENT_ID`; clear `NATS_URL` for rollback/no-op disable. For step-by-step setup, see [Enable A2A with NATS](a2a-nats-setup.md). For identity, subject, policy, and task-state terminology, see [A2A Protocol Model](a2a-protocol.md). For rollout gates, ACL templates, and smoke matrix, see [A2A NATS Rollout](a2a-nats-rollout.md).

| Variable | Default | Purpose |
| --- | --- | --- |
| `NATS_URL` | empty | NATS server URL list. Empty disables A2A. |
| `NATS_CREDS_FILE` | empty | NKey/JWT credentials file path. Preferred production credential. |
| `NATS_TOKEN` | empty | Development token. Do not use as the only production credential. |
| `NATS_TLS_CA_FILE` | empty | TLS CA file for server certificate validation. This is not client mTLS authentication by itself. |
| `A2A_AGENT_ID` | empty | Stable bot/process base identity. NATS credentials or ACLs must authorize the runtime IDs derived from this base identity. |
| `A2A_RUNTIME_ID_MODE` | `legacy` | `legacy`, `dual`, or `runtime`. Production target is `runtime`; `dual` is only for a bounded legacy drain. |
| `A2A_CONFIRMATION_SECRET` | empty | Signs A2A policy/delegation confirmation tokens and Discord confirmation buttons. Set a stable secret; otherwise it falls back to `DISCORD_TOKEN` or a process-random value that invalidates pending confirmations on restart. |
| `A2A_AGENT_NAME` | empty | Public peer-card display name. |
| `A2A_AGENT_DESCRIPTION` | empty | Public capability summary. Do not include secrets, private paths, hosts, or user data. |
| `A2A_TASK_TIMEOUT_SEC` | `3600` | Remote task timeout seconds. |
| `A2A_MAX_DELEGATION_DEPTH` | `1` | Maximum nested delegation depth. |
| `A2A_AUTO_DELEGATE_ENABLED` | `false` | Allows automatic outbound delegation when channel policy also permits it. |
| `A2A_REQUIRE_CONFIRMATION_FOR_REMOTE` | `true` | Requires confirmation before remote task execution. |
| `A2A_PRODUCTION_SECURITY` | `false` | When `true`, requires `NATS_CREDS_FILE` and rejects token-only or unauthenticated production startup. |
| `A2A_TASK_RETENTION_DAYS` | `30` | Task/event retention. Set `0` only when permanent retention is intentional. |
| `A2A_OBJECT_RETENTION_DAYS` | `30` | Object/artifact retention. Set `0` only when permanent retention is intentional. |
| `A2A_MAX_PENDING_TASKS` | `100` | Global pending remote task limit. `0` means unlimited. |
| `A2A_MAX_OUTBOUND_TASKS_PER_CHANNEL` | `10` | Outbound remote task limit per channel. `0` means unlimited. |
| `A2A_MAX_INBOUND_TASKS_PER_CHANNEL` | `10` | Inbound remote task limit per channel. `0` means unlimited. |
| `A2A_MAX_EVENT_RATE_PER_MIN` | `120` | A2A event quota per minute. `0` means unlimited. |

After changing any A2A variable, restart the bot and run `/doctor`. `/doctor` reports enabled/disabled state, auth mode, production guard state, retention, quotas, and redacted credential presence without raw tokens or credential paths.

## Speech to Text

| Variable | Default | Purpose |
| --- | --- | --- |
| `STT_ENABLED` | `false` | Enable voice/audio transcription. |
| `STT_PROVIDER` | `groq` | STT provider. |
| `STT_API_KEY` | empty | Provider API key. |
| `STT_MODEL` | empty | Provider model override. |
| `STT_LANGUAGE` | empty | Optional language hint. |
| `STT_MAX_DURATION_SEC` | `300` | Maximum audio duration for transcription. |

## Discord MCP Server

These variables configure `mcp-discord-server`, not the main bot process unless both run in the same environment.

| Variable | Default | Purpose |
| --- | --- | --- |
| `MCP_DISCORD_ALLOWED_GUILDS` | empty | Optional comma-separated guild allowlist. |
| `MCP_DISCORD_ALLOWED_CHANNELS` | empty | Optional comma-separated channel allowlist. |
| `MCP_DISCORD_DOWNLOAD_DIR` | empty | Required root for `discord_download_attachment` save paths when set. |
| `MCP_DISCORD_READ_ONLY` | `false` | Blocks all write tools when `true`. |
| `MCP_DISCORD_ALLOWED_WRITE_TOOLS` | empty | Optional comma-separated write-tool allowlist. |
| `MCP_DISCORD_ALLOW_DESTRUCTIVE` | `true` | Blocks destructive tools, such as delete, when `false`. |

## Media MCP Server

These variables configure `mcp-media-server`.

| Variable | Default | Purpose |
| --- | --- | --- |
| `GEMINI_API_KEY` | empty | Enables Gemini image, video, music, and TTS providers. |
| `OPENAI_API_KEY` | empty | Enables OpenAI image and TTS providers. |
| `MEDIA_DEFAULT_IMAGE_MODEL` | provider default | Default image model override. |
| `MEDIA_DEFAULT_VIDEO_MODEL` | provider default | Default video model override. |
| `MEDIA_DEFAULT_MUSIC_MODEL` | provider default | Default music model override. |
| `MEDIA_DEFAULT_TTS_MODEL` | provider default | Default TTS model override. |
| `MEDIA_SYNC_WAIT_SEC` | `20` | How long a legacy media tool waits for an immediate result before returning a `job_id`. Keep this below the MCP client's request timeout. |
| `MEDIA_SYNC_TIMEOUT_SEC` | `600` | Maximum runtime for a managed job started by a legacy media tool. Explicit async jobs use `MEDIA_JOB_TIMEOUT_SEC` instead. |
| `MEDIA_JOB_TIMEOUT_SEC` | `900` | Maximum runtime for an async media job. |
| `MEDIA_JOB_RETENTION_SEC` | `86400` | How long completed async job metadata remains listable. |
| `MEDIA_JOB_MAX_ACTIVE` | `4` | Maximum queued or running async media jobs in one `mcp-media-server` process. Set `0` to disable the limit. |

If neither `GEMINI_API_KEY` nor `OPENAI_API_KEY` is set, `mcp-media-server` exits at startup.
