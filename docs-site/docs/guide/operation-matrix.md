# Operation Matrix

Use this matrix before releasing changes that affect agent engines, Discord commands, usage accounting, thread agents, or MCP/audit behavior.

## Engine Scopes

| Scope | Kiro | OMP |
| --- | --- | --- |
| New channel | Uses `AGENT_ENGINE` unless `/engine` stores an override. | Same; requires `AGENT_ENGINE=omp` or `/engine omp` with OMP enabled. |
| Existing channel | Uses stored `Session.Engine`; empty means runtime default. | Same. Engine switch starts a fresh ACP session and replays recent chat context. |
| New thread | Inherits parent channel engine unless the thread stores an override. | Same. |
| Existing thread | Uses thread override first, then parent channel engine. | Same. |

## Command Behavior

| Command | Channel | Thread | Inactive Kiro | Inactive OMP |
| --- | --- | --- | --- | --- |
| `/engine` | Shows or switches channel engine. | Shows or switches thread override. | Works if Kiro is enabled. | Works if OMP is enabled. |
| `/models` | Lists channel agent models. | Lists thread agent models. | Falls back to `kiro-cli chat --list-models`. | Requires an active ACP session because models come from `session/new`. |
| `/model <id>` | Switches channel model dynamically when possible, otherwise restarts. | Respawns thread agent with the model. | Validates through Kiro CLI fallback. | Validates through the active ACP session. |
| `/agent` | Lists channel agent modes. | Lists thread agent modes. | Requires active session. | Requires active session. |
| `/agent <id>` | Switches channel agent mode. | Switches thread agent mode. | Uses ACP `session/set_mode`. | Uses ACP `session/set_mode`. |
| `/status` | Shows engine, agent version, model, queue, context usage. | Same for thread agent. | Shows active Kiro version/model. | Shows active OMP version/model. |
| `/usage` | Private guild-wide report: self by default; Manage Guild/Administrator can aggregate all users or select another member. | Same guild-wide scope and permission rules. | Credits come from Kiro metering metadata. | USD cost comes from OMP `usage_update`. |
| `/usage-history` | Private guild-wide detailed usage history with user/period/status/source filters; self-only unless requester has Manage Guild/Administrator. | Same guild-wide scope and permission rules. | Reads SQLite usage history. | Reads SQLite usage history. |
| `/audit prompt` | Uses a short-lived scoped agent and records usage under the Discord caller. | Same, with thread target metadata. | Uses channel engine. | Uses channel engine. |
| `/webshare start` | Opens a delegated browser share for the parent channel when WebShare is enabled and requester can manage the target. | Opens a delegated browser share scoped to the thread target. | Uses the channel/thread engine through normal manager entrypoints. | Uses the channel/thread engine through normal manager entrypoints. |
| `/webshare stop` / `/webshare revoke` | Stops opener share or manager-revokes the active target share; clears opener lockout. | Same for thread target. | Does not bypass Kiro cancellation/session rules. | Does not bypass OMP cancellation/session rules. |

## Release Checklist

- Run normal tests, vet, build, docs verification, and `git diff --check`.
- Run Kiro ACP smoke when Kiro behavior changed.
- Run OMP ACP smoke when OMP behavior changed.
- In Discord, test `/engine`, `/models`, `/model`, `/agent`, `/status`, `/usage`, `/usage-history`, and `/audit prompt` in both a parent channel and a thread.
- For WebShare changes, verify `/webshare start`, browser connect, visible `via WebShare` post, selected-user mention, agent prompt, thread action, upload/fetch attachment, `/webshare stop`, and manager `/webshare revoke`.
- Verify `/doctor` reports every enabled engine, usage SQLite schema/record health, and does not require disabled engines.
- Verify failed `/engine` switches do not leave partial channel or thread sessions.
