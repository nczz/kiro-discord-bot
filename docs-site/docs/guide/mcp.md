# MCP Policy

MCP tools let the agent reach systems outside the core Kiro session: Discord APIs, media generation, internal services, search tools, or project-specific automation.

## Catalog vs Channel Policy

The bot separates discovery from permission:

1. MCP server definitions are loaded into a catalog from configured Kiro MCP settings.
2. A Discord channel manager explicitly enables a server or selected tools with `/mcp manage`.
3. The bot injects only the allowed server/tool set into the current channel or thread agent.
4. A policy proxy filters `tools/list` and blocks unauthorized `tools/call` requests.

This means adding a server to `~/.kiro/settings/mcp.json` does not automatically expose it to every Discord channel.

## Built-in Bot Tools

`bot-tools` is a built-in MCP server backed by the bot binary. It provides safe access to bot-managed data and controlled egress features such as file sending, cron management, and audit timeline queries.

New channel initialization enables a safe default allowlist. Higher-risk tools such as `bot_send_message`, `bot_delete_cron`, and `bot_query_audit` require deliberate authorization.

## Discord MCP

`mcp-discord` is an optional catalog server that can read messages, list channels, send messages, create threads, and perform other Discord REST operations. Before enabling it broadly, restrict its environment:

```bash
MCP_DISCORD_ALLOWED_GUILDS=123456789012345678
MCP_DISCORD_ALLOWED_CHANNELS=234567890123456789,345678901234567890
MCP_DISCORD_READ_ONLY=false
MCP_DISCORD_ALLOWED_WRITE_TOOLS=discord_send_message,discord_reply_message
MCP_DISCORD_ALLOW_DESTRUCTIVE=false
```

For local multi-bot setups, make sure the catalog command loads the `.env` for the same bot identity you are testing. A 403 from Discord may mean the MCP server is using a different bot token than the visible Discord bot.

## Operational Checks

- Use `/mcp status` to see catalog and channel policy status.
- Use `/mcp manage` to scan tools and adjust allowlists.
- Restart or reset active agents after changing policy so the next session receives the current tool set.
- Use `/doctor` when Discord permissions or agent readiness are unclear.
