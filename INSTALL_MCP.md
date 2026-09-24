# MCP Setup

This file is a short pointer for agents and operators. The canonical MCP documentation is on the static site:

- [MCP Policy][mcp]
- [Bot Tools MCP][bot-tools]
- [Discord MCP Server][mcp-discord]
- [Media MCP][media-mcp]
- [Admin and Security][admin-security]
- [Security Model][security]

## Short Version

MCP setup has two separate layers:

1. **Catalog registration**: add the MCP server to the Kiro-format MCP catalog source: `KIRO_MCP_CONFIG`, `KIRO_HOME/settings/mcp.json`, or `~/.kiro/settings/mcp.json`.
2. **Channel policy**: use `/mcp manage` in Discord to scan the server and allow only the tools that channel should use.

Registering a server in the catalog does not automatically expose it to every Discord channel or every ACP engine. Kiro and OMP both receive tools only through the bot's channel policy injection path.

URL or SSE servers that require authentication can define `headers` in the same catalog entry. The bot uses those headers for `/mcp manage` scans and for injected channel/thread MCP traffic, while redacting header values in stored catalog records:

```json
{
  "mcpServers": {
    "ga4": {
      "type": "sse",
      "url": "http://127.0.0.1:8766/sse",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

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

Configure only the direct Discord MCP guards that still apply locally:

```env
MCP_DISCORD_DOWNLOAD_DIR=/tmp/kiro-discord-mcp
MCP_DISCORD_MEMBER_SCAN_LIMIT=5000
MCP_DISCORD_UPLOAD_DENY_PATHS=/srv/kiro-private/**
MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE=
```

`mcp-discord` is a pure Discord REST MCP server. `/mcp manage` decides which `discord_*` tools are exposed to the agent; Discord guild/channel access and write success are decided by the bot token and Discord API permissions. A Discord `403 Missing Access` means the token cannot access or act on that resource.

Then use `/mcp status` and `/mcp manage` in the target Discord channel.

[mcp]: https://nczz.github.io/kiro-discord-bot/guide/mcp.html
[bot-tools]: https://nczz.github.io/kiro-discord-bot/guide/bot-tools.html
[mcp-discord]: https://nczz.github.io/kiro-discord-bot/guide/mcp-discord.html
[media-mcp]: https://nczz.github.io/kiro-discord-bot/guide/media-mcp.html
[admin-security]: https://nczz.github.io/kiro-discord-bot/guide/admin-security.html
[security]: https://nczz.github.io/kiro-discord-bot/guide/security-model.html
