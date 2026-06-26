# kiro-discord-bot

A trainable AI agent that lives in Discord.

Bind Discord channels to real projects, teach persistent rules, and safely extend the agent with MCP tools.

[Get Started](guide/getting-started.html) · [Installation](guide/installation.html) · [Command Reference](guide/commands.html) · [繁體中文](zh-TW/)

## Highlights

- **Project-bound agents**: each Discord channel can bind to a working directory, keep its own session, and run real development or operations work.
- **Trainable context**: memory, flash memory, steering files, conversation history, and Kiro knowledge work together without treating every chat as a blank slate.
- **Controlled MCP access**: MCP servers are discovered as a catalog, then exposed per channel through policy and a proxy that filters visible tools and calls.
- **Operationally explicit**: admin surfaces are private where Discord supports it, deployment checks are repeatable, and troubleshooting paths call out real failure modes.

## What this site covers

This documentation is the practical operating manual for `kiro-discord-bot`: installation, channel setup, everyday use, steering files, MCP policy, audit and usage, admin safety, deployment, release work, security review, and troubleshooting.

The README remains the compact project entry point. Detailed explanations and operational runbooks live here.

## Role-based Paths

| Role | Recommended pages |
| --- | --- |
| First-time evaluator | [Getting Started](guide/getting-started.html), [Core Concepts](guide/core-concepts.html), [Daily Workflows](guide/daily-workflows.html) |
| Installer | [Installation](guide/installation.html), [Environment Reference](guide/environment.html), [Deployment](guide/deployment.html) |
| Discord user | [Command Reference](guide/commands.html), [Listen Modes](guide/listen-modes.html), [Cron and Reminders](guide/cron-reminders.html) |
| Channel admin | [Admin and Security](guide/admin-security.html), [MCP Policy](guide/mcp.html), [Audit, Usage, and Privacy](guide/audit-usage-privacy.html) |
| MCP admin | [MCP Policy](guide/mcp.html), [Bot Tools MCP](guide/bot-tools.html), [Discord MCP](guide/mcp-discord.html), [Media MCP](guide/media-mcp.html) |
| Operator | [Deployment](guide/deployment.html), [macOS MCP Networking](guide/macos-mcp-networking.html), [Troubleshooting](guide/troubleshooting.html) |
| Maintainer | [Release Runbook](guide/release.html), [Contributor Guide](guide/contributing.html), [Docs Maintenance](guide/docs-maintenance.html) |
| Security reviewer | [Security Model](guide/security-model.html), [Audit, Usage, and Privacy](guide/audit-usage-privacy.html), [Environment Reference](guide/environment.html) |
