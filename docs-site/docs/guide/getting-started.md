# Getting Started

`kiro-discord-bot` connects Discord to ACP agents through stdio. Kiro CLI is the default engine, and OMP can be enabled as a replaceable ACP engine behind the same Discord command, MCP policy, audit, usage, cron, memory, and thread-agent control plane. It is designed for teams that want an agent to understand a project, keep useful context, and work from Discord without losing operational control.

## Requirements

- A Discord bot application with `bot` and `applications.commands` scopes.
- Discord permissions: View Channels, Send Messages, Add Reactions, Read Message History.
- Message Content Intent enabled in the Discord Developer Portal.
- At least one ACP engine installed and authenticated: `kiro-cli` or `omp`.
- A project directory the bot is allowed to use as the channel working directory.

## Choose an Agent Engine

New deployments should start with Kiro CLI unless you already have an OMP workflow prepared. Kiro is the default and best-tested path, so it is the fastest way to confirm that Discord permissions, project binding, MCP policy, audit, and usage tracking are working.

Enable OMP after the basic bot workflow is stable, or when a specific channel needs OMP's model/mode catalog and profile isolation. A dual-engine bot can keep Kiro as the default while allowing selected channels or threads to switch with `/engine`.

## Install Flow

1. Download a release archive from GitHub Releases or build from source.
2. Create `.env` with the Discord token, guild ID, default CWD, data directory, and engine settings.
3. Run the bot once in the foreground and confirm it registers slash commands.
4. Add it as a persistent service with launchd, systemd, or Docker.
5. In Discord, use `/cwd` to initialize a channel and bind it to a project.
6. Run `/doctor` in the target channel to verify Discord permissions and ACP readiness.

## First Channel

When a channel is not initialized, the bot holds normal messages back and asks a channel manager to open the private `/cwd` setup panel. Setup chooses or creates a project under `DEFAULT_CWD`, prepares agent context files when needed, and enables the built-in `bot-tools` MCP server with a safe default allowlist.

After setup, the channel can start normal agent work. Use `/pause` for mention-only mode and `/back` to restore full-listen mode with new task threads.

## Next Steps

- Add persistent preferences with `/memory`.
- Add task-specific emphasis with `/flashmemory`.
- Put shared project rules, architecture, and workflow guidance in `AGENTS.md` through `/steering create` or `/steering edit`.
- Enable MCP tools per channel with `/mcp manage`.
- Review [Agent Engines](agent-engines.md) before enabling OMP or changing `AGENT_ENGINES_ENABLED`.
