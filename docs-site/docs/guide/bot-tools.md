# Bot Tools MCP

Every initialized channel gets a built-in `bot-tools` MCP server. It exposes bot-native operations to the active ACP agent while still enforcing the current Discord channel, thread, MCP policy, and safe egress rules.

This server is separate from external MCP servers listed in the MCP catalog.

## Default Tool Policy

On first channel setup, these safe tools are enabled by default:

| Tool | Kind | Purpose |
| --- | --- | --- |
| `bot_data_summary` | Read | Summarize the bot data directory without message content. |
| `bot_list_channel_data` | Read | List known channel data folders and metadata presence without message content. |
| `bot_list_cron` | Read | List scheduled jobs for the current channel. |
| `bot_send_file` | Write, non-destructive | Queue a sanitized file upload for Discord delivery. |
| `bot_create_cron` | Write, non-destructive | Queue creation of a scheduled task. |

These tools are available but not enabled by default:

| Tool | Kind | Purpose |
| --- | --- | --- |
| `bot_send_message` | Write, non-destructive | Queue an additional Discord message. |
| `bot_delete_cron` | Write, destructive | Queue deletion of a scheduled task. |
| `bot_query_audit` | Read, sensitive | Query scoped audit timeline rows. |

`/audit <prompt>` temporarily grants only `bot_query_audit` to the private audit investigation agent. That agent cannot use normal Discord egress tools.

## Scope Enforcement

`bot-tools` sessions are bound to the current channel or thread target. Calls that try to operate on a different channel fail with a channel-scope error.

Thread IDs are normalized to the parent channel for cron management where the runtime stores scheduled work at channel scope.

## Safe Discord Egress

`bot_send_message` and `bot_send_file` do not directly write to Discord from the MCP call. They enqueue safe egress actions and the bot performs delivery through its normal Discord path.

File egress is intentionally conservative:

- Plain text is redacted before upload.
- PDF, DOCX, and XLSX files are converted to sanitized text before upload.
- Original binary documents are not uploaded back to Discord by `bot_send_file`.
- Private audit jobs disable message and file egress completely.

## Audit Query Tool

`bot_query_audit` is read-only and scoped to the current bot-tools context. It supports filters such as:

- `limit`, from 1 to 100.
- `event_type`, exact match.
- `contains`, for metadata-field search.
- `target_id`, only when it matches the bound channel or thread context.
- `include_content`, opt-in stored content and deleted-message snippets for manager-authorized audit questions.

For deletion rows, `original_author_id`, `original_author_username`, and `content_snippet` describe the deleted message when available. Bulk deletion rows include `deleted_message_count` and `deleted_message_ids`. `deletion_note` explains attribution limits. The tool returns timeline rows, not unrestricted SQL access.
