# Discord MCP Server

`mcp-discord` is an optional MCP server included with the release archive. It gives an agent direct Discord REST capabilities such as reading messages, listing channels, sending messages, creating threads, downloading attachments, and adding reactions.

It is not required for normal bot replies. Ordinary agent final answers should be returned to the bot, which handles redaction, splitting, and delivery.

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
MCP_DISCORD_ALLOWED_WRITE_TOOLS=discord_send_message,discord_reply_message
MCP_DISCORD_ALLOW_DESTRUCTIVE=false
```

Empty allowlists preserve legacy unrestricted behavior. Production deployments should prefer explicit guild/channel allowlists when the bot has broad Discord access.

## Enable Per Channel

Registration only adds the server to the catalog. In Discord:

1. Run `/mcp status` to confirm `mcp-discord` appears.
2. Run `/mcp manage`.
3. Scan the server.
4. Enable only the tools the channel needs.
5. Let the bot restart active agents so the new policy is injected on the next session.

## Common Tool Groups

| Group | Examples | Risk |
| --- | --- | --- |
| Read | `discord_read_messages`, `discord_search_messages`, `discord_channel_info` | Can expose channel content to the agent |
| Write | `discord_send_message`, `discord_reply_message`, `discord_send_embed` | Sends visible Discord messages |
| Thread | `discord_create_thread`, `discord_list_threads` | Creates or inspects conversation surfaces |
| Management | `discord_edit_message`, `discord_pin_message`, `discord_edit_channel_topic` | Higher operational risk |
| Attachment | `discord_download_attachment` | Needs download directory controls |

Prefer read-only access first, then add non-destructive write tools only where they are part of the workflow.
