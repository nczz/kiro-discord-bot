# Daily Workflows

This page describes common operating patterns for users and channel owners.

## Ask in a Working Channel

Use a department or project channel as the primary working space. The bot records context there, uses the channel CWD, and applies that channel's MCP policy.

Mention the bot or use the configured listen mode. When thread mode is on, substantial tasks move into task threads so the parent channel stays readable.

## Capture Durable Guidance

Use `/memory` for lightweight Discord-native rules that should affect future turns. If a rule appears in `/memory list`, it is active.

Use `/flashmemory` for temporary emphasis that should not become long-term project behavior.

Use `/steering create` and `/steering edit` for project guidance that should live with the repository under `.kiro/steering/`.

## Manage Stale Context

If a persistent memory rule was wrong or has expired, remove it. If the current Kiro session has already seen that rule, also run `/clear` and `/reset` so future turns start without the old injected context.

## Coordinate Multiple Bots

For department-assistant patterns, keep each bot's durable memory and channel policy close to its department channel. In an executive channel, invite multiple bots and ask each bot for summaries, risks, or follow-up messages that are grounded in the channels it can access and the MCP tools it is allowed to use.

Cross-channel communication should happen through explicit Discord MCP access, summaries, shared project files, or steering files. Do not assume one bot can read another bot's private channel state unless the channel, Discord permissions, and MCP policy all allow it.

## Review Operations

Use `/usage` to understand metered work, `/audit` to inspect recent bot and Discord activity, and `/doctor` when behavior differs between local shells, launchd, systemd, or a remote host.
