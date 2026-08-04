# Bot Tools MCP

Every initialized channel gets a built-in `bot-tools` MCP server. It exposes bot-native operations to the active ACP agent while still enforcing the current Discord channel, thread, MCP policy, and safe egress rules.

This server is separate from external MCP servers listed in the MCP catalog.

## Default Tool Policy

On first channel setup, these safe tools are enabled by default:

| Tool | Kind | Purpose |
| --- | --- | --- |
| `bot_data_summary` | Read | Summarize the bot data directory without message content. |
| `bot_list_channel_data` | Read | List known channel data folders and metadata presence without message content. |
| `bot_current_time` | Read | Return the bot's exact current date/time, weekday, day period, and current week in `CRON_TIMEZONE`. |
| `bot_resolve_date_range` | Read | Resolve structured calendar ranges without agent-side date math. |
| `bot_list_cron` | Read | List scheduled jobs for the current channel. |
| `bot_send_file` | Write, non-destructive | Queue a sanitized file upload for Discord delivery. |
| `bot_send_image_url` | Write, non-destructive | Queue a JPEG/PNG image fetched from an allowed non-secret URL for Discord delivery. |
| `bot_create_cron` | Write, non-destructive | Queue creation of a scheduled task. |
| `bot_update_cron` | Write, non-destructive, idempotent | Queue changes to an existing recurring cron job: name, schedule, prompt, or enabled state. |
| `bot_create_reminder` | Write, non-destructive | Queue a one-time reminder delivered by the bot scheduler. |
| `bot_query_channel_history` | Read | Search or page through stored history for the current channel or thread context. |
| `bot_memory_list` | Read | List persistent memory rules for the current parent channel. |
| `bot_memory_add` | Write, non-destructive | Queue an explicitly requested, audit-recorded channel memory rule. |
| `bot_skills_search` | Read | Search visible scoped skills without exposing raw bot data paths. |
| `bot_skills_effective_list` | Read | List skills effective for the current channel/project scope. |
| `bot_skill_get` | Read | Read one visible skill after scope and required-tool checks. |
| `bot_skills_server_search` | Read | Search visible server-wide skills. |
| `bot_skills_server_get` | Read | Read one visible server-wide skill. |
| `bot_skills_server_inventory` | Read | Summarize server-wide skill inventory for authorized context. |
| `bot_skills_server_effective_for_channel` | Read | Show server skills that would apply to a channel. |
| `bot_a2a_peers` | Read | List known A2A runtime peers, callable channel refs, visible skills, wakeability, and delivery readiness. |
| `bot_a2a_task_status` | Read | Read durable A2A TaskStore state and event history for a task or recent outbound tasks. |
| `bot_a2a_trust_peer` | Write, non-destructive | Immediately allow one known runtime to send work into this channel when called with only `target_agent`; expert trust changes still require confirmation. |
| `bot_a2a_delegate` | Write, non-destructive | Queue a normal task to another bot/channel with `target_agent` and `message`; policy, quota, and optional remote confirmation checks still apply. |
| `bot_a2a_cancel` | Write, destructive | Cancel a nonterminal A2A task when requested by the requester or a channel manager. |
| `bot_a2a_input_reply` | Write, non-destructive | Send user-provided input for a task in `TASK_STATE_INPUT_REQUIRED`. |
| `bot_a2a_auth_reply` | Write, non-destructive | Approve or deny a task in `TASK_STATE_AUTH_REQUIRED` without carrying raw long-lived credentials. |

These tools are available but not enabled by default:

| Tool | Kind | Purpose |
| --- | --- | --- |
| `bot_send_message` | Write, non-destructive | Queue an additional Discord message. |
| `bot_delete_cron` | Write, destructive | Queue deletion of a scheduled task. |
| `bot_query_audit` | Read, sensitive | Query scoped audit timeline rows. |
| `bot_memory_remove` | Write, destructive | Queue removal of one listed persistent memory rule. |
| `bot_memory_clear` | Write, destructive | Queue removal of all persistent memory rules for the current channel. |

| `bot_skill_usage_record` | Write, audit | Record that an agent used a specific skill ID/version. |
| `bot_skill_create` | Write, non-destructive | Create an installed but disabled skill for any user skill-creation intent. Agents must inspect URLs/Gists/repos/files themselves and submit only clean curated Markdown with source refs; the created skill is not usable until a manager enables it. |
| `bot_skills_channel_inventory` | Read/admin | List installed channel/project skills, including disabled skills, for an authenticated channel manager. |
| `bot_skills_channel_enable` / `bot_skills_channel_disable` / `bot_skills_channel_remove` / `bot_skills_channel_restore` / `bot_skills_channel_rollback` | Write/admin | Manage channel/project skill lifecycle with channel management permission. |
| `bot_skills_server_disable` / `bot_skills_server_remove` / `bot_skills_server_restore` / `bot_skills_server_rollback` | Write/admin | Manage server-wide skills; default-off and server-management scoped. |

`/audit <prompt>` temporarily grants only `bot_query_audit` to the private audit investigation agent. That agent cannot use normal Discord egress tools.

## Scope Enforcement

`bot-tools` sessions are bound to the current channel or thread target. Calls that try to operate on a different channel fail with a channel-scope error.

Thread IDs are normalized to the parent channel for recurring cron management where the runtime stores scheduled work at channel scope. One-time reminders keep the current delivery target, so a reminder created from a task thread is delivered back to that thread.

Persistent memory is parent-channel scoped. `bot_memory_add` is default-enabled only for explicit "remember this" requests, rejects secret-like text, writes through the pending bot-side queue, and records an audit event before the main bot applies it. `bot_memory_remove` and `bot_memory_clear` are not default-enabled.

Use `bot_create_reminder` for one-time delayed reminders such as "in 10 minutes" or "tomorrow at 09:00". Use `bot_create_cron` only for recurring jobs such as daily, weekly, or periodic automation.

## A2A Bot Tools

The default `bot_a2a_*` tools are bound to the current Discord guild/channel context and must pass the current A2A policy, authenticated manager permission where required, quota, and runtime-readiness checks. Expert A2A policy planning/apply tools are retired from bot-tools; normal receiver-side `bot_a2a_trust_peer` consent applies immediately with only `target_agent`.

Use `bot_a2a_peers` before claiming another bot can receive work. `trusted=true` only describes trust display state; same-channel collaboration also requires receiver policy and Discord permission readiness.

For receiver-side consent such as "allow X to delegate", use `bot_a2a_trust_peer` with only `target_agent`. It applies immediate inbound allowlist consent for the exact runtime. Relationship, skill, channel reference, result visibility, and transcript fields are no longer accepted on this high-level tool.

For sender-side delegation, use `bot_a2a_delegate` with `target_agent` and `message` for normal tasks. Same Discord channel/thread means conversation collaboration; a different channel means the receiver works in its own channel/thread and returns the result. A successful call means the task was durably queued, not accepted or completed. Use `bot_a2a_task_status` with `local_id`, `task_id`, or `message_id` for authoritative state before reporting success.

For continuations, use `bot_a2a_input_reply` only when the task is in `TASK_STATE_INPUT_REQUIRED`, and `bot_a2a_auth_reply` only when the task is in `TASK_STATE_AUTH_REQUIRED`. Do not inspect or edit `data/a2a/*.sqlite` directly for normal operation.


## Time Context Tools

The agent prompt includes a `[Current datetime]` block derived from `CRON_TIMEZONE`. Agents should use that block for simple current-time facts such as "today", "weekday", and "morning or afternoon".

For calculated ranges, agents should translate the user's date phrase into structured `bot_resolve_date_range` fields and call that tool instead of calculating weekdays or month boundaries from model memory. The MCP tool does not parse arbitrary natural-language date text; structured range fields are language-neutral and deterministic. For example, "next month, second week" becomes `range_type=month_week`, `offset=1`, and `week_index=2`.

`CRON_TIMEZONE` is therefore the bot business timezone for cron, reminders, injected current datetime, and time helper tools. `USAGE_TIMEZONE` remains only for usage report aggregation.

## Safe Discord Egress

`bot_send_message` and `bot_send_file` do not directly write to Discord from the MCP call. They enqueue safe egress actions and the bot performs delivery through its normal Discord path.

File egress is intentionally conservative:

- Plain text is redacted before upload.
- JPEG and PNG images are validated and uploaded as copied temporary files; image URLs returned by other tools should be passed directly to `bot_send_image_url` instead of copying base64 through the agent.
- `bot_send_image_url` fetches non-secret HTTP(S) image URLs server-side without host or path allowlisting. The URL path does not need to contain an image filename; the `filename` argument controls the Discord display name. The bot still rejects URL credentials and validates the fetched bytes before queueing delivery.
- PDF, DOCX, and XLSX files are converted to sanitized text before upload.
- Original binary documents are not uploaded back to Discord by `bot_send_file`.
- Private audit jobs disable message and file egress completely.

## Incoming Discord Attachments

Incoming attachment paths are always listed in the agent prompt as a JSON-lines manifest with filename, MIME type, size, and image dimensions when available. To avoid context-window failures, the bot only sends ACP image blocks for small visual batches: at most 3 image files and at most 1 MiB total image bytes. Larger image batches stay path-only in the prompt so the agent can inspect or process files on demand.

## Mention Resolution

When the `mcp-discord` catalog entry is present, default bot-tools setup also enables `discord_resolve_mentions` for that server. The resolver can turn names such as "Wendy" or "Cheisy" into verified `[[discord:user:...]]` placeholders for the active bot task. It refreshes from Discord before falling back to cache, updates the task's dynamic mention refs, and keeps final delivery under the bot's `AllowedMentions` guard. Ambiguous or missing names require user clarification.

## Channel History Tool

`bot_query_channel_history` is read-only and scoped to the current bot-tools channel or thread context. Use `target_id` only for the current channel/thread IDs from context: a parent channel ID includes child threads, and a thread ID narrows results to that thread.

`query` is optional. Omit it for broad/exhaustive review of retained history, or provide a keyword/phrase to filter stored message and bot response content plus timeline metadata. Results are paginated JSON pages with `limit`, `offset`, `returned`, `has_more`, `next_offset`, and compact `results`; continue with `offset=next_offset` until `has_more=false` before summarizing full history.

## Audit Query Tool

`bot_query_audit` is read-only and scoped to the current bot-tools context. It supports filters such as:

- `limit`, from 1 to 100.
- `event_type`, exact match.
- `contains`, for metadata-field search.
- `target_id`, only when it matches the bound channel or thread context.
- `include_content`, opt-in stored content and deleted-message snippets for manager-authorized audit questions.

For deletion rows, `original_author_id`, `original_author_username`, and `content_snippet` describe the deleted message when available. Bulk deletion rows include `deleted_message_count` and `deleted_message_ids`. `deletion_note` explains attribution limits. The tool returns timeline rows, not unrestricted SQL access.
