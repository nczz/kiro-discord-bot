# Audit, Usage, and Privacy

The audit and usage systems answer different questions:

- Audit explains what happened in Discord and through the bot.
- Usage explains which Discord user caused metered agent work.

## Audit Storage

Audit is enabled by default and stores SQLite data at `DATA_DIR/audit/discord.sqlite` unless `AUDIT_LOG_DB` overrides it.

The recorder stores Discord gateway activity and bot-side events, including message creates, updates, deletes, reactions, channel and thread events, interactions, command replies, agent lifecycle events, final responses, and delivery success or failure metadata.

Typing events are recorded only when `AUDIT_LOG_RECORD_TYPING=true`.

## Message Deletion Attribution

Discord gateway `message_delete` events confirm that a message disappeared, but they do not identify the deletion actor. When the bot previously recorded the original message, audit queries can show the original author and, when content retention is enabled and explicitly requested, a short content snippet. That original author is not proof of who deleted the message.

Moderator or bot deletions may appear in Discord Guild Audit Log as `MESSAGE_DELETE` or `MESSAGE_BULK_DELETE`, but that is a separate Discord audit-log data source with limited fields and a finite retention window. User self-deletes generally cannot be proven from Discord gateway events alone.

## Content Recording

`AUDIT_LOG_RECORD_CONTENT=true` records message content in audit projections and raw payloads. Set it to `false` when content retention is not acceptable for the deployment.

Use `AUDIT_LOG_RETENTION_DAYS` to prune old rows. The default `0` keeps all audit data.

## Audit Command Behavior

`/audit` is slash-only:

- `/audit <limit>` privately returns recent audit rows for the current channel or thread.
- `/audit <prompt>` starts a private audit investigation agent and asks it to use `bot_query_audit`.
- Text `!audit` intentionally does not return audit rows because Discord text commands cannot guarantee private responses.

Audit management requires the same channel/admin authorization used by sensitive channel controls.

## Usage Attribution

Usage records are append-only ledgers written after completed agent work. They are attributed to the invoking Discord user when the job came from a user command, prompt, mention, audit prompt, compact, clear, or scheduled command context.

Runtime usage data is stored in the dedicated SQLite database `DATA_DIR/usage/usage.sqlite`. On the first startup after upgrading, legacy monthly `DATA_DIR/usage/*.jsonl` ledgers are imported transactionally and moved to `DATA_DIR/usage/archive/`. The archived files are retained as migration backups and are no longer queried or written at runtime. A malformed legacy file aborts startup without partially importing that file.

For cron jobs, the record uses the job owner or configured user context. Kiro usage sums `credit` or `credits` metering metadata when present. OMP usage sums USD cost metadata from `usage_update`.

If an engine does not return metering metadata for a turn, `/usage` still counts the turn but reports the missing metadata. This means the turn happened, but the bot cannot infer credits or cost from absent ACP metadata.

## Aggregation

`/usage` privately lists the requester's guild-wide current-month usage by default. Members with Manage Guild or Administrator permission may list every user with a current-month record or select another member; long reports automatically send additional ephemeral response parts when they exceed Discord's message limit. The report groups records by resolved Discord user ID when possible. If older records only have a username, the report merges that username into a user row only when it can do so unambiguously. Ambiguous names remain separate so the bot does not misattribute usage.

`/usage-history` privately returns guild-wide detailed records for the selected period and supports user, status, and source filters. Members may inspect their own history. Inspecting another member's detailed history requires Manage Guild or Administrator permission.

The report windows are:

- Today from local midnight in `USAGE_TIMEZONE`.
- This week from Monday local midnight.
- This month from the first day local midnight.

Use `USAGE_RETENTION_MONTHS` to prune old rows from the online SQLite database. The default `0` retains all online usage rows. This setting does not delete archived legacy JSONL migration backups.
