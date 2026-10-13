# Cron and Reminders

Scheduling is channel-scoped automation. Use it for work with a clear owner and a clear Discord audience.

## Which command should I use?

| Need | Command |
| --- | --- |
| Run the agent on a repeating schedule and post every result | `/cron-prompt <description>` or `/cron` |
| Send one future reminder once | `/remind <time> <content> [agent]` |
| Check something repeatedly but stay quiet unless a condition is true | `/monitor-prompt <description>` |
| Review or manage scheduled work | `/cron-list` or `/monitor-list` |
| Run a job immediately | `/cron-run <name>` or `/monitor-run <name>` |

Create and manage recurring cron jobs and monitors in the parent channel. `/remind` may create a thread-targeted one-time reminder, and `/cron-list` must be used in that same thread to manage it.

## Background monitors

Use `/monitor-prompt` when you want quiet recurring checks:

```text
/monitor-prompt every 10 minutes check CI and notify when main fails
/monitor-prompt weekdays 09:00 check inventory and notify when stock drops below 5
```

A monitor has two prompts:

- **Check**: what the background agent inspects.
- **Notify when**: the condition that allows a visible Discord notification.

When the condition is false, the bot records private history and schedules the next check. It does not create a thread, post a progress message, send tool output, or show the agent's final response.

When the condition is true, the bot creates or reuses the monitor thread, posts a parent-channel link, and sends the visible monitor message in that thread.

Use `/monitor-list` to pause, resume, run now, edit, or delete monitors. The edit form accepts common schedule phrases such as `every 10 minutes`, `weekdays 09:00`, `daily at 9am`, and 5-field cron expressions.

## Recurring cron jobs

Use cron jobs when every run should produce visible output. Prefer `/cron-prompt` for normal use:

```text
/cron-prompt every weekday at 09:00 check server health and post a short summary
```

Use `/cron` when you already know the exact schedule fields.

## One-time reminders

Use `/remind` for one future delivery:

```text
/remind tomorrow 09:00 Submit the release checklist
/remind +30m Check the deployment
```

If `agent` is enabled, the reminder asks the agent to work when due. Otherwise the bot sends the reminder text. After either execution path finishes, the one-time reminder is removed from storage and no longer appears in `/cron-list`.

`/cron-list` shows pending one-time reminders with pause/resume, edit, and delete controls. Pause/resume and edit use the scheduler's guarded update path; editing can change the name, future delivery time, and content. Only the reminder creator or a channel manager can update it, and a thread reminder must be managed from that exact thread.

## Advanced: MCP-created jobs

Agents can manage schedules through bot-tools only when the channel policy allows those tools. Monitor bot-tools also require an authenticated requester with Discord Manage Channels permission for the target channel:

- `bot_create_reminder`: create a one-time reminder such as "in 10 minutes" or "tomorrow 09:00".
- `bot_update_reminder`: update a still-pending one-time reminder's name, future delivery time, content, enabled state, or mentioned user. The call queues the update; the scheduler normally applies it within 60 seconds.
- `bot_create_cron`: create a recurring job that should visibly post every run.
- `bot_create_monitor`: create a recurring background check that must stay silent unless `notify_when` matches.

Create, update, and delete operations are written as pending actions. The bot's maintenance loop applies them to the same channel-scoped stores used by slash commands. A reminder update is rejected if the authenticated requester is neither its creator nor a channel manager, if the delivery target differs, or if execution has already started.

During a silent monitor check, bot-tools write actions are disabled. The agent can read allowed context, but it cannot queue Discord egress, cron/reminder changes, monitor changes, or persistent memory writes; the bot alone decides whether to notify after parsing the monitor JSON result.

## Operating practice

Cron jobs and monitors can trigger future agent work without a person present. Keep them tied to a channel owner, review `/cron-list` and `/monitor-list` regularly, and disable write-capable scheduler MCP tools in channels that should only read data.
