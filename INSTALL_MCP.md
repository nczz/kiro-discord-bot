# Discord MCP Server — Install & Enable Guide

This project includes a Go-based Discord MCP Server (`cmd/mcp-discord/`) that gives the kiro agent direct access to Discord — read messages, send messages, list channels, search, add reactions, etc.

## Install Steps

> **Note:** Replace `<PROJECT_DIR>` with the absolute path of this project directory (run `pwd` to get it).

### Step 1: Build the binary

Run from the project root:

```bash
go build -o mcp-discord-server ./cmd/mcp-discord/
```

### Step 2: Install the steering file

Copy the steering file to the global kiro steering directory so the agent loads it regardless of working directory:

```bash
mkdir -p ~/.kiro/steering
cp .kiro/steering/discord-mcp.md ~/.kiro/steering/discord-mcp.md
```

### Step 3: Register the MCP Server

Edit `~/.kiro/settings/mcp.json` and add the following entry under `"mcpServers"` (keep existing entries intact):

```json
"mcp-discord": {
  "command": "sh",
  "args": [
    "-c",
    "set -a && . <PROJECT_DIR>/.env && exec <PROJECT_DIR>/mcp-discord-server"
  ]
}
```

Replace `<PROJECT_DIR>` with the actual absolute path.

For example, if the project is at `/home/user/kiro-discord-bot`:

```json
"mcp-discord": {
  "command": "sh",
  "args": [
    "-c",
    "set -a && . /home/user/kiro-discord-bot/.env && exec /home/user/kiro-discord-bot/mcp-discord-server"
  ]
}
```

This sources `DISCORD_TOKEN` from the project `.env` file at startup — no token duplication needed.

### Step 3.5: Set a safety scope

Before enabling the server broadly, restrict the Discord surfaces the MCP server can touch in `<PROJECT_DIR>/.env`:

```env
MCP_DISCORD_ALLOWED_GUILDS=123456789012345678
MCP_DISCORD_ALLOWED_CHANNELS=234567890123456789,345678901234567890
MCP_DISCORD_DOWNLOAD_DIR=/tmp/kiro-discord-mcp
```

Empty allowlists preserve unrestricted legacy behavior. When a guild allowlist is set, channel tools reject channels outside allowed guilds. When a channel allowlist is set, channel and thread tools only operate on those IDs. Attachment downloads are limited to Discord attachment/CDN hosts, and `MCP_DISCORD_DOWNLOAD_DIR` restricts where downloaded files can be written.

### Step 4: Restart the agent session

After completing the steps above, tell the user:

> MCP server installed. Please run `/reset` or `!reset` to restart the agent session. After restart, I'll have direct access to Discord.

## Available Tools (after enabled)

| Tool | Description |
|------|-------------|
| `discord_list_channels` | List text channels in a guild |
| `discord_read_messages` | Read recent messages from a channel |
| `discord_send_message` | Send a message to a channel |
| `discord_reply_message` | Reply to a specific message |
| `discord_add_reaction` | Add a reaction emoji to a message |
| `discord_list_members` | List members of a guild |
| `discord_search_messages` | Search recent messages by keyword |
| `discord_channel_info` | Get detailed info about a channel |
| `discord_send_file` | Upload a local file to a channel as an attachment |
| `discord_list_attachments` | List file attachments from recent messages |
| `discord_download_attachment` | Download a Discord attachment to a local file |
| `discord_edit_message` | Edit a message |
| `discord_delete_message` | Delete a message |
| `discord_get_message` | Get a single message by ID |
| `discord_send_embed` | Send a rich embed message |
| `discord_pin_message` | Pin or unpin a message |
| `discord_create_thread` | Create a thread from a message |
| `discord_list_threads` | List active threads in a guild |
| `discord_remove_reaction` | Remove a reaction from a message |
| `discord_get_reactions` | Get users who reacted with a specific emoji |
| `discord_edit_channel_topic` | Edit a channel's topic |
| `discord_list_roles` | List roles in a guild |
| `discord_get_user` | Get info about a specific user |

## Usage Hint

Every user message forwarded to the agent includes a context header:

```
[Discord context] channel_id=<ID> guild_id=<ID>
```

Use these IDs directly with the tools — no need to ask the user for them.
