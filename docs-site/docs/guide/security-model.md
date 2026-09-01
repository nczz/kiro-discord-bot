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
| WebShare relay | Self-hosted relay host token, URL-fragment link secrets, E2E encrypted frames, opener permission rechecks, and explicit `via WebShare` display. |

## Least Privilege Defaults

Initialize each channel separately and enable only the MCP tools that channel needs. The built-in `bot-tools` server starts with a small safe default allowlist; sensitive tools such as `bot_query_audit` and destructive tools such as `bot_delete_cron` are not part of the default set.

External MCP servers should also enforce their own environment-level policy. The Discord MCP server supports guild allowlists, channel allowlists, read-only mode, write-tool allowlists, and destructive-operation blocking.

## WebShare Delegation

WebShare control links are delegated capabilities for one channel or thread target. The relay is content-blind and must be self-hosted; it routes encrypted frames and must not receive Discord user tokens, raw local paths, command secrets, or plaintext prompts. Bot access remains outbound-only through the relay host WebSocket.

Every WebShare write action is authorized as the opener and rechecks Discord access before execution. While a share is active, the opener is locked out of direct Discord prompt and bot-command paths for that target; they must use the browser link or `/webshare stop`. Browser-originated Discord messages must be visibly marked `via WebShare`, and v1 mentions are limited to explicitly selected users plus the bot. Role mentions, parse-all mentions, `@everyone`, and `@here` are disabled.

## Secrets

Keep tokens and provider keys in the service environment, not in repository files. `/doctor` redacts known sensitive values, but logs, shell history, process managers, and crash reports should still be treated as sensitive surfaces.

WebShare relay host tokens and full control/view links are secrets. Keep host tokens in `WEBSHARE_HOST_TOKEN_FILE` and `RELAY_HOST_TOKEN_FILE` where possible. Full WebShare URLs contain fragment secrets; fragments are not sent to the relay, but the URL is still sensitive in browser history, screenshots, chat, and support logs.

## Public vs Private Discord Responses

Admin panels and sensitive slash responses use private interaction responses where Discord supports them. Text commands cannot guarantee privacy, so audit rows, audit prompt reports, and usage reports/history are slash-only.

Agent final answers are normal Discord responses unless the command path explicitly uses a private response. Do not put secrets into prompts or channel messages.

## Audit and Privacy

Audit is enabled by default and can record message content. For deployments with stricter privacy requirements, set `AUDIT_LOG_RECORD_CONTENT=false` and configure `AUDIT_LOG_RETENTION_DAYS`.

The `/audit <prompt>` path uses a private short-lived agent, grants only the audit query tool, disables Discord egress tools, and records usage under the invoking Discord user.

## Network and Host Environment

On macOS launchd, proxy and `NO_PROXY` settings can differ from an interactive terminal. If MCP servers live on private `192.168.0.0/16` networks, configure the host process environment correctly before using relay workarounds. See [macOS MCP Networking](macos-mcp-networking.md).
