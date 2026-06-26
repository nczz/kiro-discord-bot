# Security Model

`kiro-discord-bot` is a bridge between Discord, Kiro CLI, local project files, and MCP servers. Security depends on each layer being configured deliberately.

## Trust Boundaries

| Boundary | Control |
| --- | --- |
| Discord access | Bot token, guild/channel permissions, privileged intents, and slash command permissions. |
| Channel workspace | `/cwd`, `DEFAULT_CWD`, `ALLOWED_CWD_ROOTS`, and channel metadata. |
| Agent tools | Kiro CLI authentication, ACP tool permission decisions, `TRUST_ALL_TOOLS`, and `TRUST_TOOLS`. |
| MCP tools | `/mcp manage`, channel policy DB, external MCP server environment guards, and per-tool allowlists. |
| Audit data | `AUDIT_LOG_*` settings, SQLite file permissions, and retention policy. |
| Generated egress | Discord send permissions, `bot-tools` safe egress queues, and MCP server write restrictions. |

## Least Privilege Defaults

Initialize each channel separately and enable only the MCP tools that channel needs. The built-in `bot-tools` server starts with a small safe default allowlist; sensitive tools such as `bot_query_audit` and destructive tools such as `bot_delete_cron` are not part of the default set.

External MCP servers should also enforce their own environment-level policy. The Discord MCP server supports guild allowlists, channel allowlists, read-only mode, write-tool allowlists, and destructive-operation blocking.

## Secrets

Keep tokens and provider keys in the service environment, not in repository files. `/doctor` redacts known sensitive values, but logs, shell history, process managers, and crash reports should still be treated as sensitive surfaces.

## Public vs Private Discord Responses

Admin panels and sensitive slash responses use private interaction responses where Discord supports them. Text commands cannot guarantee privacy, so audit rows and audit prompt reports are slash-only.

Agent final answers are normal Discord responses unless the command path explicitly uses a private response. Do not put secrets into prompts or channel messages.

## Audit and Privacy

Audit is enabled by default and can record message content. For deployments with stricter privacy requirements, set `AUDIT_LOG_RECORD_CONTENT=false` and configure `AUDIT_LOG_RETENTION_DAYS`.

The `/audit <prompt>` path uses a private short-lived agent, grants only the audit query tool, disables Discord egress tools, and records usage under the invoking Discord user.

## Network and Host Environment

On macOS launchd, proxy and `NO_PROXY` settings can differ from an interactive terminal. If MCP servers live on private `192.168.0.0/16` networks, configure the host process environment correctly before using relay workarounds. See [macOS MCP Networking](macos-mcp-networking.md).
