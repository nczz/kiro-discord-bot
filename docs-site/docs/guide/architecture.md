# Architecture

`kiro-discord-bot` is a Discord gateway bot that manages ACP agent sessions, channel state, thread state, MCP policy, cron jobs, audit events, and delivery behavior. Kiro CLI and OMP are implemented as agent engines behind the same manager, command, usage, and audit layers.

## Runtime Components

```text
Discord Gateway
  -> command/message router
  -> Channel Manager
       -> channel agent
       -> thread agents
       -> temp agents for private audit/cron flows
  -> MCP Policy Store
       -> catalog discovery
       -> channel policy
       -> policy proxy
  -> bot-tools MCP
  -> audit recorder
  -> cron/reminder scheduler
```

## Agent Runtime Isolation

The bot treats user Kiro MCP settings as a catalog, not as direct runtime inheritance. Kiro agent sessions use an isolated runtime home under `DATA_DIR/kiro-agent-runtime`, and the runtime MCP config is kept empty unless the bot injects channel-approved servers through ACP.

This prevents a user's global Kiro MCP configuration from silently becoming available in every Discord channel.

OMP sessions are launched through the same ACP transport and MCP injection path, but they do not receive `KIRO_HOME` or `KIRO_MCP_CONFIG`. OMP model and mode catalogs come from ACP `session/new`, so model listing for OMP requires an active agent session.

## Channel and Thread State

Parent channels own:

- CWD binding.
- Persistent session metadata.
- Memory and flash memory blocks.
- MCP policy.
- Cron jobs.
- Default thread/listen settings.

Threads can spawn independent agents with the parent channel's context and a bounded thread transcript. Idle cleanup can stop inactive thread agents, but active work is not evicted by capacity cleanup.

## MCP Policy Proxy

Enabled MCP servers are launched through the bot's policy proxy. The proxy:

- Filters `tools/list` so the agent only sees allowed tools.
- Rejects unauthorized `tools/call`.
- Applies the channel's allowlist.
- Keeps policy enforcement outside agent prompt behavior.

Engine-specific disabled-tool settings are not treated as the security boundary.

## Delivery and Redaction

Normal agent final answers are delivered by the bot. The bot handles secret redaction, message splitting, file egress policy, and Discord delivery errors. `bot_send_message` is intentionally not the default path for final answers; it is a controlled extra egress tool for explicit notifications or handoffs.

ACP prompt results may include a `stopReason`. Normal `end_turn` completion is silent; abnormal reasons such as `max_tokens`, `refusal`, or `cancelled` are surfaced as localized notices appended to the final answer and recorded in job audit metadata. The bot does not reclassify those turns as delivery failures unless ACP itself returns an error.

Kiro subagent progress notifications are rendered conservatively. The bot trusts the verified top-level subagent and pending-stage counts, and displays best-effort labels only when the notification includes recognizable names or statuses.

## Audit

Audit storage records semantic bot events such as command calls, command responses, agent job lifecycle, and final response delivery. Audit prompt investigations use short-lived private agents with only the audit query tool injected.

See [Bot Tools MCP](bot-tools.md), [Audit, Usage, and Privacy](audit-usage-privacy.md), and [Security Model](security-model.md) for the detailed behavior and trust boundaries that sit on top of this architecture.
