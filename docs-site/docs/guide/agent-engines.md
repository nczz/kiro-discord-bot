# Agent Engines

`kiro-discord-bot` is now an ACP agent control plane for Discord. Kiro CLI remains the default and best-tested engine, but engine selection is a first-class runtime concept rather than a hard-coded identity.

An engine is the ACP-speaking command that the bot starts for a channel, thread, cron job, or private audit prompt. Today the supported engines are:

| Engine | Binary | Dialect | Primary use |
| --- | --- | --- | --- |
| `kiro` | `kiro-cli` | Kiro ACP | Default engine, Kiro model fallback, Kiro runtime settings, existing Kiro workflows. |
| `omp` | `omp` | OMP ACP | Alternative ACP engine with model and mode catalog reported by `session/new`, USD cost metadata when available, and OMP profile support. |

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

| Area | Kiro | OMP |
| --- | --- | --- |
| Model listing | Can use Kiro fallback paths. | Requires an active ACP session because models come from `session/new`. |
| Model switching | Uses Kiro ACP model APIs. | Uses `session/set_config_option` with `configId=model`. |
| Usage metadata | Credits when Kiro metering metadata is present. | USD cost when OMP `usage_update` metadata is present. |
| Runtime settings | Isolated `KIRO_HOME` and `KIRO_MCP_CONFIG`. | Isolated `--session-dir`, optional `OMP_PROFILE`. |
| MCP injection | Same bot policy and proxy layer. | Same bot policy and proxy layer. |

Use `/doctor` to inspect enabled engines and effective runtime values. Use `/status`, `/model`, and `/usage` after switching engines to confirm the active engine, actual model, and usage attribution.
