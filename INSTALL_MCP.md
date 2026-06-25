# MCP Setup

This file is a short pointer for agents and operators. The canonical MCP documentation is on the static site:

- MCP policy: https://nczz.github.io/kiro-discord-bot/guide/mcp.html
- Discord MCP server: https://nczz.github.io/kiro-discord-bot/guide/mcp-discord.html
- Admin and security: https://nczz.github.io/kiro-discord-bot/guide/admin-security.html

## Short Version

MCP setup has two separate layers:

1. **Catalog registration**: add the MCP server to `KIRO_MCP_CONFIG`, `KIRO_HOME/settings/mcp.json`, or `~/.kiro/settings/mcp.json`.
2. **Channel policy**: use `/mcp manage` in Discord to scan the server and allow only the tools that channel should use.

Registering a server in Kiro settings does not automatically expose it to every Discord channel.

## Discord MCP Example

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

Add defense-in-depth environment guards where appropriate:

```env
MCP_DISCORD_ALLOWED_GUILDS=123456789012345678
MCP_DISCORD_ALLOWED_CHANNELS=234567890123456789,345678901234567890
MCP_DISCORD_READ_ONLY=false
MCP_DISCORD_ALLOWED_WRITE_TOOLS=discord_send_message,discord_reply_message
MCP_DISCORD_ALLOW_DESTRUCTIVE=false
```

Then use `/mcp status` and `/mcp manage` in the target Discord channel.
