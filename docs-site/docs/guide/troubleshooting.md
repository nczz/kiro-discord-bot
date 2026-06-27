# Troubleshooting

## MCP Discord Returns 403

Separate the two common failure paths:

- `bot-tools` may reject cross-channel sends by design when the current session is not allowed to target that channel.
- `mcp-discord` returning Discord `403 Missing Access` means the Discord token used by the MCP server cannot see or act in the target channel.

For local multi-bot setups, verify the MCP catalog command sources the `.env` for the expected bot identity. If the visible Discord bot is M5Bot but the MCP server loads ChunBot's token, channel permissions for M5Bot will not help.

## MCP Scan Says No Route to Host

If a private LAN MCP URL works from an interactive shell but `/mcp manage` scan fails under macOS launchd, fix the host/service networking environment first. Check proxy settings, `NO_PROXY`, route selection, and macOS Local Network permission. Use a relay only as an explicit deployment fallback.

## Bot Does Not Respond

Check:

- The channel has been initialized with `/cwd`.
- The bot can view and send in the channel.
- The channel is not in mention-only mode unless you used a real Discord mention.
- Multi-bot mode did not switch the channel to automatic mention-only behavior.
- `/doctor` reports healthy Discord permissions and ACP preflight.

## Response Was Cut Off

If the bot says the response reached the model output limit, the ACP turn completed with `stopReason=max_tokens`. Ask the bot to continue in the same channel or thread. The turn is still recorded as completed because ACP returned a final prompt result rather than a transport error.

## Thread Reset Says No Thread Agent

A thread may have conversation history without an active in-memory thread agent. Idle cleanup, archive events, or restart can remove the active agent process. Use a new message in the thread to recreate context when supported, or start from the parent channel if the thread is stale.

## Stale Memory Still Affects Replies

If a removed memory rule still appears to influence the agent, the current Kiro session may already contain previous turns where the rule was injected. Remove the rule, then run `/clear` and `/reset`.
