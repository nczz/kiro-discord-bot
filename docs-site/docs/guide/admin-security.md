# Admin and Security

`kiro-discord-bot` is intentionally powerful: it can bind to real project directories, run agent tools, and call MCP servers. Treat deployment and channel policy as production controls.

## Discord Permissions

The base bot needs:

- View Channels
- Send Messages
- Add Reactions
- Read Message History
- Message Content Intent

MCP servers may need additional Discord REST access. The bot's channel policy does not replace Discord permissions; both must allow the operation.

## Private Responses

Admin panels and sensitive command responses use private interaction responses where Discord supports ephemeral messages. This includes `/cwd`, `/status`, `/usage`, `/doctor`, `/audit`, `/models`, `/memory`, `/flashmemory`, `/mcp manage`, `/steering`, and `/cron-list`.

Text commands cannot always provide private Discord responses. For audit data, use slash `/audit`; text `!audit` does not return audit rows or prompt investigation reports.

## CWD Boundaries

Use `DEFAULT_CWD` and `ALLOWED_CWD_ROOTS` to keep channel setup inside expected project roots. New channels must be initialized before agent work starts, and setup only selects or creates projects under `DEFAULT_CWD`.

## MCP Safety

Use least privilege:

- Keep catalog servers disabled by default.
- Enable only the tools a channel actually needs.
- Prefer read-only or non-destructive MCP modes for broad channels.
- Use server-level environment guards as defense in depth.
- Re-scan MCP tools after server upgrades.

## Audit

Audit events record command calls, command replies, agent lifecycle, final responses, and relevant delivery success/failure metadata. Use retention settings when keeping audit data forever is not appropriate.
