# Listen Modes

Listen mode determines when a Discord message becomes agent work. It is the main safety layer that prevents accidental bot-to-bot loops while keeping single-bot channels ergonomic.

## Parent Channel Modes

| Mode | Trigger | Thread behavior | Typical use |
| --- | --- | --- | --- |
| Full-listen | Any normal human message | New tasks open threads by default | A channel with one assistant bot |
| Mention-only | Real Discord mention of the bot | Parent-channel replies without new threads unless re-enabled | Shared channels or quiet periods |
| Automatic multi-bot mention-only | Real Discord mention of a specific bot | Depends on the target's current thread setting | Channels where multiple peer bots can respond |

`/pause` switches a parent channel to mention-only and disables new task threads. `/back` restores full-listen behavior and turns new task threads back on.

## Thread Behavior

Each task thread can continue as its own agent conversation. A thread keeps the listen mode captured when it was created. Later parent-channel changes do not silently rewrite old thread behavior.

Thread agents are independent from the parent channel agent. Work in one thread does not block the parent channel or another thread.

## Mentions

Use real Discord mentions from the Discord UI. Plain text such as `@BuildBot` may not trigger the bot. When multiple peer bot mentions appear at the start of a human message, they are treated as routing metadata and removed from the task text delivered to each mentioned bot.

The bot also provides structured mention references to the agent so final answers can mention known users or peer bots without guessing raw Discord IDs.

## Multi-bot Handoff

Peer bots are auto-discovered from guild bot members when the bot starts. `BOT_PEERS` is only needed to override names/roles, add a bot discovery cannot see, or exclude a bot.

Bot-authored messages are ignored by default. A peer handoff is accepted only in controlled situations, such as a thread where the target bot is explicitly mentioned and the original task has already completed. This prevents agents from responding to progress updates or partial tool output.

## Debugging Listen Mode

Run `/doctor` in the target channel or thread. It reports whether the context is open, mention-only, opened by `/back`, or in automatic multi-bot mention-only mode. It also checks whether the bot can view and respond in the target.
