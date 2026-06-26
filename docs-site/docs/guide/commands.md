# Command Reference

Commands are available as slash commands. Most also have `!` text-command equivalents for environments where slash commands are less convenient. Sensitive admin surfaces use private interaction responses where Discord supports ephemeral replies.

## Channel Setup and State

| Command | Purpose |
| --- | --- |
| `/cwd` | Open the private project/CWD setup panel. Use this to initialize or move a channel to an allowed project. |
| `/start <cwd>` | Advanced direct CWD binding. Prefer `/cwd` for normal setup. |
| `/status` | Show agent state, queue length, context use, session ID, and uptime. |
| `/doctor` | Run deployment, permission, and ACP diagnostics for the current target. |
| `/reset` | Restart the current channel or thread agent. |
| `/clear` | Clear conversation history for the current target. |
| `/compact` | Ask Kiro to compact conversation context where supported. |

## Work Control

| Command | Purpose |
| --- | --- |
| `/cancel` | Ask the current ACP session to cancel the active task. |
| `/interrupt` | Soft-cancel first, then interrupt the process group if the same task remains stuck. |
| `/pause` | Switch the current target to mention-only mode. In a parent channel, also disables new task threads. |
| `/back` | Restore full-listen mode and new task threads for the parent channel or current thread. |
| `/thread [on|off]` | Show or set whether future parent-channel tasks create Discord threads. |
| `/silent [on|off]` | Control compact vs detailed tool output visibility. |

## Model and Agent Mode

| Command | Purpose |
| --- | --- |
| `/model` | Show the current model. |
| `/model <model-id>` | Switch the model when Kiro supports dynamic model changes. |
| `/models` | List available models. |
| `/agent` | List available Kiro agent modes. |
| `/agent <mode-id>` | Switch agent mode, such as planner or guide modes advertised by Kiro CLI. |

## Memory and Steering

| Command | Purpose |
| --- | --- |
| `/memory` | Add, list, remove, or clear persistent Discord-native memory rules. |
| `/flashmemory` | Manage session-scoped emphasis rules. |
| `/steering <status|create|edit>` | Manage the current project steering file under `.kiro/steering/`. |

If a memory rule is visible in `/memory list`, it affects future turns. To retire stale persistent guidance completely, remove it, then run `/clear` and `/reset`.

See [Daily Workflows](daily-workflows.md) for the operational difference between memory, flash memory, steering, and session cleanup.

## MCP and Admin

| Command | Purpose |
| --- | --- |
| `/mcp status` | Show catalog and current channel policy status. |
| `/mcp enable` / `/mcp disable` | Enable or disable a server at channel scope. |
| `/mcp manage` | Open the private MCP policy panel, scan tools, and manage tool allowlists. |
| `/audit [limit]` | Privately inspect recent audit events for the current channel or thread. |
| `/usage [user]` | Show Kiro credit usage for today, week, and month-to-date. |

Use slash `/audit` for audit data. Text `!audit` is intentionally not supported for audit rows because Discord cannot make those replies private.

See [Audit, Usage, and Privacy](audit-usage-privacy.md) for how audit rows, audit prompt investigations, and usage attribution work.

## Scheduling

| Command | Purpose |
| --- | --- |
| `/cron` | Create a recurring scheduled task through a form. |
| `/cron-prompt <description>` | Create a scheduled task from natural language. |
| `/cron-list` | List scheduled tasks with management buttons. |
| `/cron-run <name>` | Run a scheduled task manually. |
| `/remind <time> <content>` | Create a one-time reminder that tags the requester when due. |

Scheduling commands must be run in the parent channel. Cron agents use the channel's current CWD at execution time.

See [Cron and Reminders](cron-reminders.md) for scheduling scope, MCP-created jobs, and owner expectations.

## Thread-only Helpers

Inside a Discord thread, the same slash commands usually target the thread agent when that is least surprising: `/status`, `/reset`, `/cancel`, `/interrupt`, `/compact`, `/clear`, and `/model`. `!close` closes the current thread agent, and `!close-thread <thread_id>` can close an inactive thread agent from the parent channel scope.
