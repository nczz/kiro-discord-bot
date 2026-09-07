# Discord MCP Server

`mcp-discord` is an optional MCP server included with the release archive. It gives an agent direct Discord REST capabilities such as reading messages, listing channels, sending messages, creating threads, downloading attachments, and adding reactions.

It is not required for normal bot replies. Ordinary agent final answers should be returned to the bot, which handles redaction, splitting, and delivery.

Unlike `bot-tools`, `mcp-discord` direct write tools avoid bot safe-egress redaction and sanitization. `discord_send_message`, `discord_reply_message`, `discord_edit_message`, `discord_send_embed`, and `discord_send_file` still enforce MCP guild/channel/write/destructive policy, Discord length handling, and mention controls, but they do not redact secret-looking text, extract documents, or rewrite files to sanitized copies. Use `bot_send_file` or `bot_send_message` when the workflow needs bot-owned safe egress instead.

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

For multi-bot local development, make sure the `.env` is the same bot identity you are testing. If the visible Discord bot is M5Bot but the catalog command loads ChunBot's token, Discord permission changes for M5Bot will not fix MCP `403 Missing Access` errors.

## Add Defense-in-depth Guards

Set env guards in the loaded `.env` or catalog environment:

```env
MCP_DISCORD_ALLOWED_GUILDS=123456789012345678
MCP_DISCORD_ALLOWED_CHANNELS=234567890123456789,345678901234567890
MCP_DISCORD_DOWNLOAD_DIR=/tmp/kiro-discord-mcp
MCP_DISCORD_READ_ONLY=false
MCP_DISCORD_ALLOWED_WRITE_TOOLS=discord_send_message,discord_reply_message,discord_resolve_mentions
MCP_DISCORD_ALLOW_DESTRUCTIVE=false
MCP_DISCORD_MEMBER_SCAN_LIMIT=5000
MCP_DISCORD_UPLOAD_DENY_PATHS=/srv/kiro-private/**
MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE=
```

Empty allowlists preserve legacy unrestricted behavior. Production deployments should prefer explicit guild/channel allowlists when the bot has broad Discord access.

For standalone `mcp-discord` processes, do not rely on the channel-policy injection path. Set `MCP_DISCORD_READ_ONLY=true` unless writes are required; when writes are required, keep `MCP_DISCORD_ALLOW_DESTRUCTIVE=false` and enumerate only non-destructive tools in `MCP_DISCORD_ALLOWED_WRITE_TOOLS`. Bot-managed channel sessions inject these guards from the channel policy automatically.

## Enable Per Channel

Registration adds the server to the catalog. The bot's default bot-tools setup automatically enables only `discord_resolve_mentions` for `mcp-discord` when this catalog entry is present, matching the default-on bot-native image URL egress behavior while keeping broader Discord REST tools closed.

For additional tools in Discord:

1. Run `/mcp status` to confirm `mcp-discord` appears.
2. Run `/mcp manage`.
3. Scan the server.
4. Enable only the extra tools the channel needs.
5. Let the bot restart active agents so the new policy is injected on the next session.

## Common Tool Groups

| Group | Examples | Risk |
| --- | --- | --- |
| Read | `discord_read_messages`, `discord_search_messages`, `discord_channel_info` | Can expose channel content to the agent |
| Mention resolver | `discord_resolve_mentions` | Resolves requested names with fresh Discord member lookup, grants only exact/unique matches for the active bot task, and returns safe `[[discord:user:...]]` placeholders |
| Write | `discord_send_message`, `discord_reply_message`, `discord_send_embed` | Sends visible Discord messages without bot safe-egress redaction |
| Thread | `discord_create_thread`, `discord_list_threads` | Creates or inspects conversation surfaces |
| Management | `discord_edit_message`, `discord_pin_message`, `discord_edit_channel_topic` | Higher operational risk |
| Attachment | `discord_send_file`, `discord_download_attachment` | `discord_send_file` uploads the selected local file bytes directly; downloads need download directory controls |

Prefer read-only access first, then add non-destructive write tools only where they are part of the workflow.

Use `discord_send_file` only when the intended result is original-file transfer. It uploads the file selected by `file_path` subject to Discord size limits, MCP write policy, and the upload source denylist. It does not convert PDF/DOCX/XLSX to text, reject otherwise valid binary files because they are not redactable, or redact text file contents. For sanitized bot-owned delivery, use `bot_send_file`.

`discord_send_file` also enforces a source-path denylist before opening the file. Defaults block direct uploads from the bot data directory, Kiro runtime/config roots, OMP session/config roots, the mcp-discord executable directory, and detected bot deployment directories. `MCP_DISCORD_UPLOAD_DENY_PATHS` appends comma- or newline-separated wildcard patterns; `*`, `?`, and `**` are supported after environment-variable and `~` expansion. The check evaluates both the requested absolute path and the symlink-resolved real path, and denial errors do not echo local paths. Set `MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE=false` only when a case-sensitive deployment filesystem needs exact-case matching.

Keep `DEFAULT_CWD`, user-content workspaces, and `MCP_DISCORD_DOWNLOAD_DIR` outside the bot data directory. Mixing user-transferable files with bot-owned runtime state is not a best practice: it increases the chance that later file-transfer workflows point at sensitive bot state or require exceptions to the upload guard. The default denylist blocks files under the bot data directory as a last-resort guard, so direct re-upload with `discord_send_file` will fail; use `bot_send_file` for sanitized bot-owned delivery.

When a user asks to tag or notify a named person who was not already mentioned in the current prompt, prefer `discord_resolve_mentions` over `discord_list_members`. It performs fresh REST member search before bounded scan/cache fallback, writes resolved refs into the current bot target state, and returns placeholders the agent can use in the final answer. Ambiguous or missing names must be clarified instead of guessed. Increase `MCP_DISCORD_MEMBER_SCAN_LIMIT` only when exact names routinely miss because the guild is larger than the default bounded scan.
