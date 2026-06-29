# Agent Engines

`kiro-discord-bot` is a Discord control plane for project-bound ACP agents. The bot owns the Discord experience, channel policy, project binding, audit, usage, cron, memory, and thread lifecycle. The agent engine owns the actual coding-agent session that reasons over the project.

Kiro CLI remains the default and best-tested engine. OMP can be enabled when a team wants an alternate ACP engine without changing the Discord workflow.

Both engines are external projects. This repository does not maintain `kiro-cli` or `omp`; it provides the Discord control plane around them. Install, authenticate, and update each CLI through its own upstream tooling. See [Installation](installation.md) for the basic commands and links.

## Why Engines Matter

Most teams should not have to think about protocol details. In practice, an engine choice answers a simpler product question:

- Which coding agent should this Discord channel talk to?
- Which authentication, model catalog, and metering system should be used?
- Can a team try another agent without rebuilding its Discord operations?

The bot keeps those decisions scoped and reversible. A channel can stay on Kiro, another channel can use OMP, and a thread can temporarily switch engines for one investigation. The surrounding controls remain the same: `/cwd`, `/mcp`, `/status`, `/usage`, `/audit`, cron, memory, steering, and thread agents.

## Kiro CLI

Kiro CLI is the default engine and the recommended starting point for new deployments. It is the path with the broadest project history in this repository and the most complete fallback behavior.

Use Kiro when you want:

- The default, best-tested production path.
- Kiro model fallback behavior for `/models` and `/model`.
- Kiro runtime settings isolated under `DATA_DIR/kiro-agent-runtime`.
- Kiro metering metadata, reported as credits when the engine provides it.
- Existing Kiro workflows and MCP catalog sources to continue working.

For a Kiro-only deployment, leave OMP disabled. Existing installations upgrade cleanly without new environment variables.

## OMP

OMP is an optional replaceable ACP engine. It is useful when your team already uses OMP, wants to evaluate another ACP-compatible agent, or wants a separate engine profile for selected channels.

Start from the OMP project site: [omp.sh](https://omp.sh/).

Use OMP when you want:

- A second engine behind the same Discord commands and MCP policy.
- Model and mode catalogs reported by the active ACP session.
- USD cost metadata when OMP emits `usage_update` events.
- Profile-scoped OMP auth/settings/cache through `OMP_PROFILE`.
- Bot-managed OMP session files through `OMP_SESSION_DIR`.

OMP is opt-in. Install and authenticate `omp` before enabling it, and use `/doctor` after restart to confirm readiness.

## Choosing a Deployment Shape

| Shape | Best for | Configuration |
| --- | --- | --- |
| Kiro-only | First install, conservative production rollout, existing Kiro teams. | `AGENT_ENGINE=kiro`, empty or `kiro`-only `AGENT_ENGINES_ENABLED`. |
| Dual-engine bot | Teams that want selected channels or threads to try OMP without adding another Discord bot. | `AGENT_ENGINE=kiro`, `AGENT_ENGINES_ENABLED=kiro,omp`. |
| OMP-only | Teams that intentionally do not want Kiro available in this bot process. | `AGENT_ENGINE=omp`, `AGENT_ENGINES_ENABLED=omp`. |
| Multiple bot identities | Departments that want separate Discord bot personas, data directories, and ownership. | Run one process per bot token and use distinct `DATA_DIR` values. |

Start with Kiro-only unless you already know why OMP should be available. Add OMP after the basic Discord, project, MCP, and audit workflow is stable.

## What the Bot Owns

An engine is the ACP-speaking command that the bot starts for a channel, thread, cron job, or private audit prompt. Today the supported engines are:

| Engine | Binary | Dialect | Primary use |
| --- | --- | --- | --- |
| `kiro` | `kiro-cli` | Kiro ACP | Default engine, Kiro model fallback, Kiro runtime settings, existing Kiro workflows. |
| `omp` | `omp` | OMP ACP | Alternative ACP engine with model and mode catalog reported by `session/new`, USD cost metadata when available, and OMP profile support. |

The engine does not bypass the bot's policy model. Kiro and OMP both receive tools through the same MCP policy injection path, both write usage through the bot ledger, and both are represented in audit events as the engine that handled the work.

## Scope Rules

Engine state is scoped to the Discord target, not global process state.

| Scope | Behavior |
| --- | --- |
| New channel | Uses `AGENT_ENGINE` unless a stored channel override exists. |
| Existing channel | Uses the channel's stored `Session.Engine`; empty values fall back to the runtime default. |
| New thread | Inherits the parent channel engine. |
| Existing thread | Uses the thread override first, then parent channel engine. |
| `/engine <engine>` in a channel | Switches only that channel and starts a fresh ACP session with replayed recent context. |
| `/engine <engine>` in a thread | Switches only that thread; it does not rewrite the parent channel or sibling threads. |

Switching one channel from Kiro to OMP does not affect any other channel. The bot resolves engine, binary, dialect, runtime env, MCP policy, audit attribution, and usage attribution at each spawn point.

## Configuration

| Variable | Purpose |
| --- | --- |
| `AGENT_ENGINE` | Default engine for new scopes. Default: `kiro`. |
| `AGENT_ENGINES_ENABLED` | Comma-separated engines that `/engine` may switch to. Empty means only `AGENT_ENGINE`. |
| `KIRO_CLI_PATH` | Kiro binary path. |
| `OMP_PATH` | OMP binary path. |
| `OMP_SESSION_DIR` | Optional OMP session directory. Empty means `DATA_DIR/omp-agent-runtime/sessions`. |
| `OMP_PROFILE` | Optional OMP profile for isolated auth/settings/cache. Empty keeps OMP's default profile for backward compatibility. |

For a smooth upgrade from older Kiro-only deployments, no new environment variable is required. Enable OMP only after installing and authenticating `omp`.

See [Environment Reference](environment.md) for every supported variable, common configuration shapes, and upgrade notes.

## Runtime Isolation

Kiro sessions receive bot-managed Kiro runtime settings:

- `KIRO_HOME=DATA_DIR/kiro-agent-runtime`
- `KIRO_MCP_CONFIG=DATA_DIR/kiro-agent-runtime/settings/mcp.json`

The bot treats user Kiro MCP settings as a catalog source, not direct runtime inheritance.

OMP sessions do not receive Kiro runtime env. The bot passes `--session-dir DATA_DIR/omp-agent-runtime/sessions` so ACP session files are bot-managed without moving the existing OMP auth/model database.

`OMP_PROFILE` is optional. If configured, OMP uses that named profile for auth, settings, sessions, and caches. Authenticate it before enabling:

```bash
OMP_PROFILE=kiro-discord-bot omp setup
```

If `OMP_PROFILE` is not configured, OMP uses the default profile. This is intentional so existing OMP installations keep working after upgrade.

## Operational Differences

| Area | Kiro CLI | OMP |
| --- | --- | --- |
| Recommended default | Yes. | No, opt-in after setup. |
| Model listing | Can use Kiro fallback paths. | Requires an active ACP session because models come from `session/new`. |
| Model switching | Uses Kiro ACP model APIs. | Uses `session/set_config_option` with `configId=model`. |
| Usage metadata | Credits when Kiro metering metadata is present. | USD cost when OMP `usage_update` metadata is present. |
| Runtime settings | Isolated `KIRO_HOME` and `KIRO_MCP_CONFIG`. | Isolated `--session-dir`, optional `OMP_PROFILE`. |
| MCP injection | Same bot policy and proxy layer. | Same bot policy and proxy layer. |
| Best rollout path | Start here. | Enable after Kiro-only operation is stable or when OMP is already part of the team's workflow. |

Use `/doctor` to inspect enabled engines and effective runtime values. Use `/status`, `/model`, and `/usage` after switching engines to confirm the active engine, actual model, and usage attribution.
