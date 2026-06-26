# Cron and Reminders

Scheduling is channel-scoped automation. Use it for recurring work that has a clear owner and a clear channel audience.

## Commands

| Command | Behavior |
| --- | --- |
| `/cron` | Opens a form for recurring scheduled tasks. |
| `/cron-prompt <description>` | Asks the bot to create a recurring task from natural language. |
| `/cron-list` | Lists scheduled jobs and management actions. |
| `/cron-run <name>` | Runs a scheduled job immediately. |
| `/remind <time> <content> [agent]` | Creates a one-time reminder. When `agent` is enabled, the reminder asks the agent to work when due. |

Scheduling commands are channel-only commands. Run them in the parent channel, not inside a task thread.

## Execution Model

The heartbeat loop checks pending schedules. Cron jobs use the channel's current CWD and channel policy at execution time.

Agent-based cron turns are recorded in the usage ledger and audit timeline. Tool audit events are also recorded when cron jobs use MCP tools.

## MCP-created Jobs

The built-in `bot_create_cron`, `bot_list_cron`, and `bot_delete_cron` tools let an agent manage cron jobs when the channel policy allows those tools.

Creates and deletes are written as pending actions and become active through the bot's normal maintenance loop. This keeps MCP tool calls aligned with the same channel-scoped cron store used by slash commands.

## Good Operating Practice

Cron is powerful because it can trigger future agent work without a person present. Keep it tied to a channel owner, review `/cron-list` regularly, and disable write-capable cron MCP tools in channels that should only read data.
