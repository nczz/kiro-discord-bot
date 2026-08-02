# Command Reference

Commands are available as slash commands. Most also have `!` text-command equivalents for environments where slash commands are less convenient. Sensitive admin and account-usage surfaces use private interaction responses where Discord supports ephemeral replies.

## Channel Setup and State

| Command | Purpose |
| --- | --- |
| `/cwd` | Open the private project/CWD setup panel. Use this to initialize or move a channel to an allowed project. |
| `/start <cwd>` | Advanced direct CWD binding. Prefer `/cwd` for normal setup. |
| `/status` | Show agent state, queue length, context use, session ID, and uptime. |
| `/doctor` | Run deployment, permission, and ACP diagnostics for the current target. |
| `/reset` | Restart the current channel or thread agent. |
| `/clear` | Clear conversation history for the current target. |
| `/compact` | Ask the active engine to compact conversation context where supported. |

In a parent channel, `/clear` clears the active agent session and the bot-local channel chat log used for future session continuity. In a Discord thread, `/clear` clears the active thread agent session when one is running, truncates the bot-local `thread-<id>` chat log, and clears saved ACP session metadata so `/reset` cannot reload the previous agent session. The local thread cleanup still runs when no active in-memory thread agent exists. Discord messages that remain visible in the thread may still be used to rebuild context on future turns, so delete or edit details that should not be retained. Memory rules, flash memory, steering, and project files still apply.

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
| `/model <model-id>` | Switch the model for the current channel/thread agent. Kiro can use its fallback model list; OMP validates model IDs from the active ACP session. |
| `/models` | List available models. Kiro can fall back to `kiro-cli`; OMP requires the channel/thread agent to be running because models come from ACP `session/new`. |
| `/agent` | List available modes for the current channel/thread agent. |
| `/agent <mode-id>` | Switch agent mode, such as planner/guide modes or OMP modes advertised by the active ACP session. |
| `/engine` | Show the current agent engine (kiro/omp) and which engines are enabled. |
| `/engine <kiro_or_omp>` | Switch the engine for this channel/thread (only engines listed in `AGENT_ENGINES_ENABLED`). Starts a fresh session on the new engine; recent conversation context is replayed. |

## Memory and Steering

| Command | Purpose |
| --- | --- |
| `/memory` | Add, list, remove, or clear persistent Discord-native memory rules. |
| `/flashmemory` | Manage session-scoped emphasis rules. |
| `/steering <status|create|edit>` | Manage shared `AGENTS.md` guidance for the current project. |

If a memory rule is visible in `/memory list`, it affects future turns. To retire stale persistent guidance completely, remove it, then run `/clear` and `/reset`.

See [Daily Workflows](daily-workflows.md) for the operational difference between memory, flash memory, steering, and session cleanup.

## MCP and Admin

| Command | Purpose |
| --- | --- |
| `/mcp status` | Show catalog and current channel policy status. |
| `/mcp enable` / `/mcp disable` | Enable or disable a server at channel scope. |
| `/mcp manage` | Open the private MCP policy panel, scan tools, and manage tool allowlists. |
| `/audit [limit]` | Privately inspect recent audit events for the current channel or thread. |
| `/usage [user]` | Privately show guild-wide agent usage for today, week, and month-to-date, including credits or USD cost when the engine reports metering metadata. Members see their own usage by default; members with Manage Guild or Administrator permission may omit `user` for all users or choose another member. |
| `/usage-history [user] [period] [status] [source]` | Privately inspect guild-wide detailed usage records. Period choices: `7d`, `30d`, `this-month`, `last-month`; status choices: `all`, `success`, `failed`; source choices: `all`, `message`, `command`, `cron`. Members can inspect their own history; inspecting another member requires Manage Guild or Administrator permission. |

Use slash `/audit` for audit data and slash `/usage` or `/usage-history` for usage data. Text `!audit` does not return audit rows, and text `!usage` only returns a slash-only notice, because Discord cannot make those replies private.

See [Audit, Usage, and Privacy](audit-usage-privacy.md) for how audit rows, audit prompt investigations, and usage attribution work.

## Scoped Skills

Use scoped skills to save reviewed reusable procedures for a server, channel, project, or channel/project pair. Prefer natural-language requests for creating and updating skills; slash commands are fallback and admin shortcuts. See [Scoped Skills](scoped-skills.md) for scope precedence, bot-tools behavior, audit, and recovery.

| Command | Purpose |
| --- | --- |
| `/skill list [query]` | Managers see installed skills, including disabled skills; other users see only effective skills. |
| `/skill get skill_id:<id-or-slug>` | Managers can read one installed skill after scope checks; other users can read only effective skills. |
| `/skill create` | Create a skill from explicit fields; it is installed disabled by default. |
| `/skill disable` / `/skill enable` / `/skill restore` / `/skill rollback` | Manage lifecycle state for an authorized scope. |
| `/skill history skill_id:<id-or-slug> [scope]` | Show recent lifecycle audit history. |

## A2A

Use A2A commands only after NATS is configured and the channel has an A2A policy. For setup from NATS server through Discord policy, see [Enable A2A with NATS](a2a-nats-setup.md). For protocol terms, see [A2A Protocol Model](a2a-protocol.md).

| Command | Purpose |
| --- | --- |
| `/a2a peers` | List visible runtime peers, skills, trust, stale/online status, and delivery readiness. |
| `/a2a trust peer_agent:<runtime>` | Plan or apply high-level peer trust. Defaults to bidirectional general-task trust and requires confirmation before applying. |
| `/a2a ask peer_agent:<runtime> message:<text>` | Queue a general delegated task for a trusted runtime. |
| `/a2a delegate target_agent:<runtime> skill_id:<skill> message:<text> reason:<reason>` | Queue a task for an explicit runtime/skill target after policy and confirmation checks. |
| `/a2a status [task]` | Inspect durable task state and events. A queued task is not complete until status reaches a terminal state. |
| `/a2a cancel task:<task>` | Request cancellation for a remote A2A task. |
| `/a2a reply task:<task> input:<text>` | Provide input when a task is `TASK_STATE_INPUT_REQUIRED`. |
| `/a2a authorize task:<task> approve:true_or_false` | Approve or deny when a task is `TASK_STATE_AUTH_REQUIRED`. |

Policy subcommands such as `/a2a enable`, `/a2a disable`, `/a2a expose`, `/a2a accept-from`, and `/a2a delegate-to` are advanced surfaces. Prefer `/a2a trust` for normal bidirectional peer setup.


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
