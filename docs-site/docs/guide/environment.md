# Environment Reference

The bot does not load `.env` by itself. Inject these variables through your shell, launchd, systemd, Docker, or another process manager.

Use `/doctor` after startup to inspect effective values. Secrets are redacted in diagnostics.

## Required

| Variable | Default | Purpose |
| --- | --- | --- |
| `DISCORD_TOKEN` | required | Discord bot token. |

## Core Runtime

| Variable | Default | Purpose |
| --- | --- | --- |
| `DISCORD_GUILD_ID` | empty | Guild used for slash command registration. Empty uses Discord's global command scope. |
| `KIRO_CLI_PATH` | `kiro-cli` | Executable path for Kiro CLI. |
| `KIRO_API_KEY` | empty | Headless Kiro authentication key when `kiro-cli login` is not used. |
| `DEFAULT_CWD` | `/projects` | Root shown by `/cwd` setup. |
| `ALLOWED_CWD_ROOTS` | empty | Optional comma-separated root allowlist for channel working directories. |
| `DATA_DIR` | `./data` | Persistent bot data, channel metadata, sessions, audit DB, usage ledgers, MCP policy, and runtime Kiro settings. |
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
| `USAGE_TIMEZONE` | `CRON_TIMEZONE`, then local default | Time zone for `/usage` day, week, and month windows. |
| `USAGE_RETENTION_MONTHS` | `0` | Usage ledger retention. `0` keeps all monthly files. |
| `ATTACHMENT_RETAIN_DAYS` | `7` | Retention for downloaded Discord attachments. |
| `ATTACHMENT_MAX_MB` | `25` | Maximum attachment size accepted by the bot. |
| `PREFLIGHT_MODE` | `warn` | ACP compatibility preflight mode. `strict` exits on failure, `skip` disables the check, and unknown values fall back to warn. |
| `SKIP_PREFLIGHT` | empty | Any non-empty value skips ACP preflight. Prefer `PREFLIGHT_MODE=skip` for explicit configuration. |

## Audit

| Variable | Default | Purpose |
| --- | --- | --- |
| `AUDIT_LOG_ENABLED` | `true` | Enable audit recording. |
| `AUDIT_LOG_DB` | `DATA_DIR/audit/discord.sqlite` | SQLite audit database path. |
| `AUDIT_LOG_RETENTION_DAYS` | `0` | Audit retention. `0` keeps all rows. |
| `AUDIT_LOG_QUEUE_SIZE` | `1000` | Async audit queue size. If full, audit-only events may be dropped and logged. |
| `AUDIT_LOG_RECORD_CONTENT` | `true` | Include message content in audit projections and raw event payloads. |
| `AUDIT_LOG_RECORD_TYPING` | `false` | Record Discord typing events. |

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
| `MEDIA_DEFAULT_TTS_MODEL` | provider default | Default TTS model override. |

If neither `GEMINI_API_KEY` nor `OPENAI_API_KEY` is set, `mcp-media-server` exits at startup.
