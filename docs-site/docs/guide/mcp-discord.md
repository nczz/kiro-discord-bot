# Discord MCP Server

`mcp-discord` is an optional MCP server included with the release archive. It gives an agent direct Discord REST capabilities such as reading messages, listing channels, sending messages, creating threads, downloading attachments, and adding reactions.

It is not required for normal bot replies. Ordinary agent final answers should be returned to the bot, which handles redaction, splitting, and delivery.

Unlike `bot-tools`, `mcp-discord` direct write tools avoid bot safe-egress redaction and sanitization. `discord_send_message`, `discord_reply_message`, `discord_edit_message`, `discord_send_embed`, and `discord_send_file` still enforce Discord length handling and mention controls, but they do not redact secret-looking text, extract documents, or rewrite files to sanitized copies. MCP proxy policy decides whether a tool is exposed; the Discord token and API permissions decide which resources it can access. Use `bot_send_file` or `bot_send_message` when the workflow needs bot-owned safe egress instead.

## Build or Locate the Binary

Release archives include `mcp-discord`. Source builds can create it with:

```bash
go build -o mcp-discord-server ./cmd/mcp-discord
```

Use one binary name consistently in the catalog command. Deployed environments often use `mcp-discord-server` next to the main bot binary.

## Install Steering Guidance

The repository includes `.kiro/steering/discord-mcp.md`. Install it globally only when you want Kiro sessions outside this bot to understand the Discord MCP tools too:

```bash
mkdir -p ~/.kiro/steering
cp .kiro/steering/discord-mcp.md ~/.kiro/steering/discord-mcp.md
```

For bot-managed runtime sessions, MCP visibility is still controlled by channel policy.

## Register in the MCP Catalog

Add an entry to `~/.kiro/settings/mcp.json` or the file pointed to by `KIRO_MCP_CONFIG`:

```json
{
  "mcpServers": {
    "mcp-discord": {
      "command": "sh",
      "args": [
        "-c",
        "set -a && . /absolute/path/to/.env && exec /absolute/path/to/mcp-discord-server"
      ],
      "env": {}
    }
  }
}
```

For multi-bot local development, make sure the `.env` belongs to the bot identity under test. If Discord shows the bot under test but the catalog command loads credentials for a different bot, changing permissions for the visible bot will not fix MCP `403 Missing Access` errors.

## Direct Discord MCP Guards

Set only the guards that still apply inside `mcp-discord`:

```env
MCP_DISCORD_DOWNLOAD_DIR=/tmp/kiro-discord-mcp
MCP_DISCORD_UPLOAD_DENY_PATHS=/srv/kiro-private/**
MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE=
```

`mcp-discord` is a pure Discord REST MCP server. It does not apply local guild/channel allowlists, read-only caps, write-tool caps, destructive caps, or bot session target binding. For bot-managed sessions, `/mcp manage` decides which `discord_*` tools are exposed to the agent through the MCP proxy. Actual guild/channel scope and write success are decided by the Discord bot token and Discord API permissions; a Discord `403 Missing Access` means the token cannot access or act on that resource.

The direct tools still keep local content/file guards: Discord-friendly formatting and split behavior, inert raw mentions with empty `AllowedMentions`, the `discord_send_file` upload source denylist, and `discord_download_attachment` path-root/path-traversal checks. `mcp-discord` never reads bot target state or grants bot-task mention placeholders.

`discord_list_channels` returns every guild channel exposed by Discord's Get Guild Channels API, including categories, text, voice, announcement, stage, forum, and media channels. Its JSON output includes each channel's stable `type`, numeric `type_id`, `parent_id`, and `position`. Set `include_active_threads=true` to append active threads; archived threads are not included. Discord omits channels the bot token cannot `VIEW_CHANNEL`, so this tool cannot reveal channels hidden from that credential.

`discord_list_members` and `discord_get_reactions` return JSON pages with the input `after` cursor and `next_after`; continue with `after=next_after` until `next_after` is empty. Limits are validated against Discord's endpoint bounds: 1–1000 members and 1–100 reaction users. `discord_get_user` performs direct Get User lookup without requiring a guild; optional `guild_id` or `channel_id` adds guild-member nickname, role, and join metadata. `discord_search_members` is a pure read-only guild member search and does not grant mention capability.

Discord's guild member list and search endpoints require the privileged **Server Members Intent**. Enable it when using `discord_list_members`, `discord_search_members`, or `bot_resolve_mentions` member lookup.

## Audit Boundary

MCP policy changes made through the bot remain bot audit events. The upgrade migration from `discord_resolve_mentions` to `bot_resolve_mentions` records `mcp_policy_updated` events with `source=migration` for each changed policy. Tool calls observed through the normal thread and inline worker paths remain visible as generic `agent_tool_call` and `agent_tool_result` events. The synchronous fallback used only when thread creation fails does not currently emit per-call tool audit events.

The standalone `mcp-discord` process does not write to the bot audit database and does not receive an authenticated requesting Discord actor. Calls made by an MCP client outside a bot-managed agent job therefore have no bot audit row. Discord's own Guild Audit Log may cover some moderation or configuration mutations, but it is a separate, partial data source; reads and ordinary message sends are not a complete management audit trail.


## Enable Per Channel

Registration only adds `mcp-discord` to the catalog; it does not enable any direct Discord tools. Default bot-tools setup enables the separate bot-owned `bot_resolve_mentions` tool inside `bot-tools`, without enabling or binding `mcp-discord`.

On upgrade, stored policies that enabled the retired `discord_resolve_mentions` tool are migrated: the stale direct-tool grant is removed, and an already-enabled safe-write `bot-tools` policy receives `bot_resolve_mentions`. Other `mcp-discord` grants are preserved.

For additional tools in Discord:

1. Run `/mcp status` to confirm `mcp-discord` appears.
2. Run `/mcp manage`.
3. Scan the server.
4. Enable only the extra tools the channel needs.
5. Let the bot restart active agents so the new policy is injected on the next session.

## Common Tool Groups

| Group | Examples | Risk |
| --- | --- | --- |
| Read | `discord_list_channels`, `discord_list_members`, `discord_search_members`, `discord_get_user`, `discord_read_messages`, `discord_search_messages`, `discord_channel_info`, `discord_get_reactions`, `discord_list_roles` | Can expose guild structure, membership, roles, reaction users, or channel content to the agent |
| Write | `discord_send_message`, `discord_reply_message`, `discord_send_embed` | Sends visible Discord messages without bot safe-egress redaction |
| Thread | `discord_create_thread`, `discord_list_threads` | Creates or inspects conversation surfaces |
| Management | `discord_edit_message`, `discord_pin_message`, `discord_edit_channel_topic` | Higher operational risk |
| Attachment | `discord_send_file`, `discord_download_attachment` | `discord_send_file` uploads the selected local file bytes directly; downloads need download directory controls |

Prefer read-only access first, then add non-destructive write tools only where they are part of the workflow.

Use `discord_send_file` only when the intended result is original-file transfer. It uploads the file selected by `file_path` subject to Discord size limits, MCP proxy tool exposure, Discord token/API permissions, and the upload source denylist. It does not convert PDF/DOCX/XLSX to text, reject otherwise valid binary files because they are not redactable, or redact text file contents. For sanitized bot-owned delivery, use `bot_send_file`.

`discord_send_file` also enforces a source-path denylist before opening the file. Defaults block direct uploads from the bot data directory, Kiro runtime/config roots, OMP session/config roots, the mcp-discord executable directory, and detected bot deployment directories. `MCP_DISCORD_UPLOAD_DENY_PATHS` appends comma- or newline-separated wildcard patterns; `*`, `?`, and `**` are supported after environment-variable and `~` expansion. The check evaluates both the requested absolute path and the symlink-resolved real path, and denial errors do not echo local paths. Set `MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE=false` only when a case-sensitive deployment filesystem needs exact-case matching.

Keep `DEFAULT_CWD`, user-content workspaces, and `MCP_DISCORD_DOWNLOAD_DIR` outside the bot data directory. Mixing user-transferable files with bot-owned runtime state is not a best practice: it increases the chance that later file-transfer workflows point at sensitive bot state or require exceptions to the upload guard. The default denylist blocks files under the bot data directory as a last-resort guard, so direct re-upload with `discord_send_file` will fail; use `bot_send_file` for sanitized bot-owned delivery.

For ordinary bot replies, named mention resolution belongs to `bot_resolve_mentions`, not `mcp-discord`. The bot-owned tool performs fresh per-name REST searches plus one bounded guild-member scan per call to cross-check exact or unique matches, writes verified refs into the current task state, and returns safe placeholders for final delivery. `discord_search_members` is an administrative lookup only; do not treat its user IDs as bot mention authorization.
