# Listen Mode Matrix

The canonical listen-mode documentation now lives on the static documentation site: [Listen Modes][listen-modes].

## Short Summary

- Full-listen mode sends normal human channel messages to the agent.
- Mention-only mode requires a real Discord mention.
- `/pause` switches to mention-only and disables new parent-channel task threads.
- `/back` restores full-listen mode and new parent-channel task threads.
- Multi-bot channels can automatically switch to mention-only to avoid bot-to-bot loops.
- Thread agents keep the listen behavior captured when the thread was created.
- Run `/doctor` in the target channel or thread to inspect the effective behavior.

[listen-modes]: https://nczz.github.io/kiro-discord-bot/guide/listen-modes.html
