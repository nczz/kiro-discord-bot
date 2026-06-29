# Core Concepts

## Channel Agents

A parent Discord channel maps to one project working directory and one long-lived ACP agent session. The channel agent owns the main project context, memory block, steering files, MCP policy, and conversation continuity.

## Thread Agents

By default, new parent-channel tasks open Discord threads. A thread gets a dedicated agent with the original task context and bounded thread transcript. Thread agents are independent from the parent channel agent, so long-running work in a thread does not block unrelated channel work.

Thread commands such as `/status`, `/reset`, `/cancel`, `/interrupt`, `/compact`, `/clear`, and `/model` target the current thread agent when run inside a thread.

## Agent Engines

Kiro CLI is the default ACP engine. OMP can be enabled as an alternate engine behind the same Discord command, MCP policy, audit, usage, cron, memory, and thread-agent control plane.

Engine choice is scoped to the current channel or thread. Switching one channel to OMP does not change sibling channels, and switching a thread does not rewrite its parent channel. See [Agent Engines](agent-engines.md) for the full scope and runtime isolation model.

## Memory, Flash Memory, and Steering

Use the three context layers for different jobs:

| Layer | Scope | Best for |
| --- | --- | --- |
| `/memory` | Persistent Discord-native rules | Personal preferences, response language, recurring style rules |
| `/flashmemory` | Current session emphasis | Temporary priorities for the current task or sprint |
| `AGENTS.md` | Versioned project guidance | Architecture, build commands, safety rules, workflow, domain background |

Rules visible in `/memory list` are injected before every agent turn. Removing a memory rule stops future injection, but the current ACP agent session may already contain older turns where the rule appeared. When retiring a stale or conflicting persistent rule, remove it, then run `/clear` and `/reset`.

## MCP Policy

The bot reads MCP server definitions as a catalog from Kiro-format settings sources such as `KIRO_MCP_CONFIG`, `KIRO_HOME/settings/mcp.json`, or `~/.kiro/settings/mcp.json`. It does not expose catalog servers to agents by default.

At runtime, each agent receives only the MCP servers allowed for the current Discord channel through ACP `mcpServers`. The bot launches allowed servers through a policy proxy that filters `tools/list` and blocks unauthorized `tools/call` requests.

## Audit and Private Admin Responses

Operational panels and sensitive responses such as `/mcp manage`, `/cwd`, `/status`, `/usage`, `/doctor`, `/audit`, `/models`, `/memory`, and `/flashmemory` use private interaction responses where Discord supports ephemeral messages. Audit prompt investigations also return the final report privately.
