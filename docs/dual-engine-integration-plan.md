# Dual-Engine Integration Plan (kiro-cli + omp)

Status: PLANNING (pre-implementation). This document is the single source of truth
for the dual-agent-engine feature. It must be exhaustive enough that implementation
has no undefined behavior. All protocol facts below were verified by runtime probes
against kiro-cli 2.10.0 and omp 16.1.23.

---

## 0. Goals & Non-Goals

Goals:
- Support two ACP agent engines: `kiro` (kiro-cli) and `omp` (Oh My Pi).
- PRIMARY shape (M2): ONE bot identity (one token, one runtime, one DATA_DIR) that switches the agent
  engine behind it per channel/thread via `/engine`. Engine is an internal detail; users see one bot.
- SECONDARY shape (M1): two bot identities (two runtimes, distinct DATA_DIR), one engine each, for users
  who explicitly want two separate bots. Uses the existing multi-bot model.
- Preserve cross-engine conversation continuity in M2 via the engine-agnostic ChatLogger history prefix.
- Preserve every existing security boundary: per-call tool permission, MCP policy/proxy, safe egress, cwd policy, audit, redaction.

Non-Goals:
- `pi` (flat `--mode rpc` protocol) is NOT integrated (no ACP, no MCP, no per-call permission). Out of scope.
- No single-process orchestration of multiple Discord bot identities (unchanged non-goal; multi-bot stays independent runtimes + BOT_PEERS).
- No cross-engine ACP session migration: switching engine starts a fresh session on the new engine. UX
  continuity is provided by replaying the shared chat-log history window, NOT by transferring the session.
- No shared-DATA_DIR across runtimes; M1's two runtimes use distinct DATA_DIR (SQLite migration is a
  separate, orthogonal initiative, not a dependency of this feature).

---

## 1. Verified Protocol Facts (evidence base)

Both kiro-cli and omp speak ACP over JSON-RPC 2.0 (ndjson). Differences:

| Concern | kiro-cli | omp (`omp acp`) |
|---|---|---|
| Launch | `kiro-cli acp --trust-all-tools [--model M] [--agent A]` | `omp acp` (no flags) |
| initialize | protocolVersion 1, agentCapabilities | protocolVersion 1, richer caps (loadSession, image, embeddedContext, mcp http+sse, sessionCapabilities) |
| session/new result | `modes` + `models` | `configOptions[]` (select entries: `mode`, `model`) |
| streaming | `agent_message_chunk` | `agent_message_chunk` (identical) |
| tool_call | toolCallId/title/kind/status/rawInput/locations | identical shape (+ optional `intent`) |
| tool permission | `session/request_permission` (+ launch `--trust-all-tools`) | `session/request_permission` (always; no trust flag) |
| MCP inject | `session/new` `mcpServers` | `session/new` `mcpServers` (verified: launches + tool callable) |
| session/prompt result | `{stopReason}` | `{stopReason, usage:{inputTokens,outputTokens,cachedReadTokens,totalTokens}}` |
| stopReason values | end_turn/max_tokens/refusal/cancelled | end_turn/cancelled/... |
| model switch | `session/set_model {modelId}` | `session/set_config_option {configId:"model", value}` |
| mode switch | `session/set_mode {modeId}` | `session/set_mode {modeId}` (identical) |
| cancel | `session/cancel` (tolerated as request) | `session/cancel` MUST be a NOTIFICATION (no id) |
| session/load | supported (capability gated) | supported (verified cross-process recall) |
| metrics source | `_kiro.dev/metadata` (contextUsagePercentage, metering) | `usage_update` notif `{size, used, cost:{amount,currency:USD}}` + prompt-result usage |
| usage unit | credits (metering unit="credit"), context % | USD cost (CUMULATIVE; per-turn = delta), tokens, context = used/size |
| auth | AWS (KIRO_API_KEY / IdC) | ChatGPT subscription (~/.omp, openai-codex provider) |

Five dialect adaptation points: launch args, session/new parsing, model setter, cancel framing, metrics source. Plus usage accounting per engine.

---

## 2. Architecture: ACP Dialect Profile

kiro and omp are the SAME transport (ACP/JSON-RPC 2.0). Do NOT write a second agent.
Add a `Dialect` to the existing `acp.Agent`.

```
acp/
  agent.go      → Agent gains `dialect Dialect` field
  dialect.go    → Dialect enum + profile table (NEW)
  jsonrpc.go    → add Transport.SendNotification (no id, no wait)  (NEW method)
  protocol.go   → existing structs; add omp configOptions parsing structs
```

```go
type Dialect int
const ( DialectKiro Dialect = iota; DialectOmp )

// profile drives the 5 differences; everything else is shared.
type dialectProfile struct {
    subcommand   []string                              // kiro: ["acp"] ; omp: ["acp"]
    launchArgs   func(model string, o AgentOptions) []string // trust/model/agent flags (kiro only)
    parseSession func(raw json.RawMessage, into *SessionNewResult) // modes/models vs configOptions
    setModel     func(a *Agent, modelID string) error  // set_model vs set_config_option(configId=model)
    cancel       func(a *Agent)                         // request vs notification
    readMetrics  func(a *Agent) // metadata notif vs usage_update/prompt-usage → fills TurnMetrics
}
```

Shared & reused unchanged: transport read loop, handshake (initialize/session/new/session/prompt),
streaming (agent_message_chunk), tool_call/tool_call_update, `session/request_permission` (OnRequest +
TRUST policy), MCP injection (mcpServers), stopReason (already implemented), session/load,
set_mode, AsyncCallbacks, worker, safe egress, discordfmt, audit.

`StartAgent` signature changes to carry engine:
```go
func StartAgent(name, binary string, dialect Dialect, cwd, model string, opts AgentOptions) (*Agent, error)
```

### Engine abstraction at manager layer

`workerAgent` interface (channel/worker.go) is already the runtime seam. Promote it to the
manager-facing handle and make `acp.Agent` satisfy it (it already implements the 12 worker methods
plus SessionID/AgentVersion/AvailableModels/SetModel/AvailableModes/SetMode/SupportsLoadSession).

Manager changes:
- `agents map[string]*acp.Agent` → keep `*acp.Agent` (single transport type, dialect is a field) — **no interface explosion needed** because both engines ARE `*acp.Agent` with different dialects.
- Add engine resolution to all 4 spawn points (channel, thread, temp/cron, audit) — pick dialect+binary from resolved engine.
- `heartbeat/cron.go` CronDeps and `bot/cron_adapter.go` keep `*acp.Agent` (no change — same type).

> Key simplification discovered: because omp also speaks ACP, both engines are `*acp.Agent`.
> The ONLY thing that varies is the `Dialect` field + binary path. This avoids the heavy
> "abstract to interface across manager/cron" refactor previously feared. `*acp.Agent` stays.

---

## 3. Engine Resolution & Inheritance

Engine follows the EXACT inheritance chain already used for cwd/model
(manager thread spawn, manager.go ~2330):

```
default engine (AGENT_ENGINE)
  → parent channel Session.Engine (if set)
    → thread Session.Engine (if set)        [thread scope only]
      → explicit /engine override (persisted into the scope's Session.Engine)
```

Rules:
- A new channel uses `AGENT_ENGINE` until `/engine` sets `channel Session.Engine`.
- A new thread inherits its parent channel's resolved engine at creation time.
- `/engine` inside a thread overrides only that thread (thread Session.Engine).
- Switching engine for an active scope = stop current agent + start new-engine agent
  (same restart path as model switch). Session is NOT carried across engines
  (different session stores/providers); a fresh session/new is used, history prefix
  applies as in any restart.
- `AGENT_ENGINES_ENABLED` gates which engines `/engine` may pick. If it lists one engine,
  `/engine <other>` is rejected with a localized message.

Persistence: `Session.Engine string \`json:"engine,omitempty"\`` (channel/session.go).
Backward compatible: empty → resolve to `AGENT_ENGINE`.

---

## 4. env Design (full path per project convention)

| env | default | scope | purpose |
|---|---|---|---|
| `AGENT_ENGINE` | `kiro` | runtime | primary/default engine for new scopes |
| `AGENT_ENGINES_ENABLED` | = `AGENT_ENGINE` | runtime | comma list of engines `/engine` may select |
| `OMP_PATH` | `omp` | runtime | omp binary (parallel to KIRO_CLI_PATH) |
| `KIRO_CLI_PATH` | `kiro-cli` | runtime | existing, unchanged |

Full path to touch when adding `AGENT_ENGINE` / `AGENT_ENGINES_ENABLED` / `OMP_PATH`:
config.go (Config + loadConfig) → channel/manager.go (ManagerConfig + Manager fields + NewManager) →
main.go (ManagerConfig assembly + preflight per enabled engine) → channel/doctor_env.go (envSpecs) →
locale/lang/en.json + zh-TW.json (doctor.env.desc.*) → README.md + README.zh-TW.md → .env.example.

Topology via env:
- M1 (multi-bot, one engine each): runtime A `.env` AGENT_ENGINE=kiro; runtime B AGENT_ENGINE=omp. BOT_PEERS for routing. No new commands required.
- M2 (single bot, primary/secondary): AGENT_ENGINE=kiro, AGENT_ENGINES_ENABLED=kiro,omp. `/engine` switches per scope.
- M3 (hybrid): M1 + each runtime may also enable M2.

### 4.0 Bot identity vs engine: two distinct product shapes

`DISCORD_TOKEN` is single-valued and binds one process to one Discord bot identity
(`bot/bot.go`: `discordgo.New("Bot " + cfg.DiscordToken)`; one session per process). This is by
design and matches the documented non-goal (no single-process multi-identity orchestration).
Two product shapes follow:

- **Two bot identities, one engine each (M1)**: TWO Discord applications (two tokens) → TWO runtimes
  (two processes/services). Discord shows two separate bots ("kiro-bot", "omp-bot"); the user @mentions
  the one they want. This is the EXISTING multi-bot model; the only new code is engine selection
  (omp dialect + AGENT_ENGINE), NOT multi-bot machinery.
- **One bot identity, switchable engine (M2)**: ONE token, ONE runtime, ONE bot that runs kiro or omp
  per channel via `/engine`. Discord shows a single bot.

"two bots, one engine each" is multi-runtime BECAUSE a token is per-identity — not a limitation to
overcome.

#### DECISION: M2 (single bot, switchable engine) is the PRIMARY shape

Chosen direction: one bot identity, engine switched behind it. Rationale:
- Single process → single `DATA_DIR` → the whole-file `sessions.json` last-writer-wins concern
  (§ earlier) DISAPPEARS entirely (one writer). No shared-state coordination, no SQLite dependency.
- One Discord identity → engine is an internal implementation detail; users never see "two bots".
- Shared resources are engine-agnostic within the runtime: channel binding, cwd, MCP policy, steering
  (AGENTS.md), memory, chat log, usage store.

Cross-engine conversation continuity (the "no two-systems feel" guarantee), VERIFIED feasible:
- History context comes from `ChatLogger` (`m.logger.BuildContextPrompt(channelID, 10)`, manager.go:1696),
  an engine-agnostic per-channel JSONL of role/content. It is prepended as the worker `historyPrefix`
  on every agent (re)start.
- Therefore switching engine (e.g. kiro→omp) starts a fresh ACP session on the new engine BUT replays
  the recent conversation from the shared chat log → the user perceives continuity.
- Honest boundary: the underlying ACP session itself does NOT transfer (kiro AWS session ≠ omp ChatGPT
  session; a fresh session/new is created). Only the recent history window is replayed, identical to
  any agent restart today. Deep history beyond the window is not carried.

M1 (two bot identities) remains a SUPPORTED but secondary deployment topology (see §4.2) for users who
explicitly want two distinct bots; it requires distinct DATA_DIR per runtime. The default and primary
product is M2.

### 4.1 Pure-engine runtime (e.g. omp-only, kiro NOT installed) — REQUIRED scenario

A runtime must be able to run a SINGLE engine with the other engine's binary/auth ABSENT.
Canonical pure-omp config: `AGENT_ENGINE=omp`, `AGENT_ENGINES_ENABLED=omp` (kiro-cli not installed,
no AWS/KIRO auth). Symmetric pure-kiro is the existing default.

Verified current-state facts (so the change set is precise):
- Startup has NO hard kiro dependency except `PREFLIGHT_MODE=strict|fatal` (main.go) which calls
  `acp.PreflightCheckWithOptions(cfg.KiroCLIPath, ...)` unconditionally.
- `loadConfig()` requires only `DISCORD_TOKEN` (mustEnv). All `KIRO_*` are `envOr` (optional).
- `NewManager` stores `kiroCLI` as a string; it does NOT stat the binary at construction.
- Per-spawn engine resolution uses OMP_PATH+DialectOmp for omp scopes and never touches `m.kiroCLI`.
- So pure-omp ALREADY runs in default `warn` mode, but UNCLEANLY: preflight warns about missing
  kiro-cli; `preflightAgentOptions` still creates `DATA_DIR/kiro-agent-runtime` and injects
  `KIRO_HOME`/`KIRO_MCP_CONFIG`; `/doctor` shows KIRO_* env; steering uses `.kiro/steering`.

Required engine-aware changes for a CLEAN pure-engine runtime:
1. Preflight is engine-aware: preflight each engine in `AGENT_ENGINES_ENABLED` (omp → omp ACP smoke;
   kiro → kiro ACP smoke). Never reference kiro-cli when kiro is not enabled. `strict/fatal` only
   fails on an ENABLED engine's preflight.
2. Gate `KIRO_HOME` / `KIRO_MCP_CONFIG` injection + `kiro-agent-runtime` creation to the kiro dialect
   only (preflightAgentOptions + agentOptsWithRuntime). omp spawns get no KIRO_* env.
3. `/doctor` env display is engine-scoped: show KIRO_* only when kiro enabled; show OMP_PATH + omp
   auth (~/.omp) checks when omp enabled. Per-engine preflight result lines.
4. Steering: pure-omp uses `AGENTS.md` only (no `.kiro/steering`). (See §8.2.)
5. Config defaults stay backward compatible: KIRO_CLI_PATH/KIRO_API_KEY/KIRO_AGENT are simply unused
   when kiro is not in AGENTS_ENABLED; no new mustEnv. OMP auth is omp's own (~/.omp); the bot does
   not manage it, but `/doctor` should surface a clear "omp not authenticated" hint if detectable.

Acceptance for pure-omp: on a host WITHOUT kiro-cli and WITHOUT AWS creds, `AGENT_ENGINE=omp` +
`AGENT_ENGINES_ENABLED=omp` starts with no kiro warnings, no kiro-agent-runtime dir, `/doctor` shows
only omp, and all engine-neutral + engine-branched commands work against omp.

### 4.2 M1 deployment model (two bots, one engine each) — correctness requirements

Each bot identity is a separate runtime. Per runtime, REQUIRED separation:

| Concern | Requirement | Why |
|---|---|---|
| Discord token | distinct `DISCORD_TOKEN` (two Discord apps) | one process binds one identity |
| `DATA_DIR` | **distinct per runtime** | `SessionStore` (`DATA_DIR/sessions.json`), `UsageStore` (`DATA_DIR/usage`), cron (`DATA_DIR/cron`) are DATA_DIR-rooted; sharing → concurrent overwrite/corruption |
| engine | one `AGENT_ENGINE` each (kiro / omp) | identity-to-engine binding |
| `BOT_PEERS` | each runtime lists the other(s) | mention routing + `requiresHumanMention` so both don't answer |
| `DISCORD_GUILD_ID` | same (shared server) | coexist in one guild |
| service unit | separate systemd unit / compose service / launchd plist | independent lifecycle |
| MCP policy store | separate (lives under DATA_DIR) | per-identity channel policy |

Each bot independently binds its own per-channel cwd/session/agent. In a shared channel, a user talks
to a specific bot by @mentioning it; without a distinct mention both could respond — this is exactly
the "Multi-Bot Mention Confusion" failure mode, mitigated by the existing BOT_PEERS + requiresHumanMention
path (must not be weakened for engine coexistence).

Acceptance for M1: two runtimes (token A engine kiro, token B engine omp), distinct DATA_DIR, mutual
BOT_PEERS, same guild → both appear as separate bots, each answers only when addressed, sessions/usage
do not collide, and pure-omp runtime (§4.1) needs no kiro install.

---

## 5. Discord Command × Engine Behavior (all 34 + new /engine)

Legend: ✅ identical behavior | ⚙️ engine-branched internally (UI unchanged) | 🆕 new.

### 5.1 Engine-branched commands (interface unchanged, manager branches by dialect)

| Command | kiro result | omp result |
|---|---|---|
| `/model` (no arg) | shows current model | shows current model (from configOptions currentValue) |
| `/model X` | set_model + restart agent | set_config_option(configId=model) (no restart needed) |
| `/models` | lists kiro models (auto, opus-4.x, sonnet-4.x, ...) | lists omp models (openai-codex/gpt-5.x, codex, ...) |
| `/agent` (no arg) | lists modes: confident/kiro_default/kiro_planner/kiro_guide | lists modes: default/plan |
| `/agent X` | set_mode X | set_mode X (same protocol) |
| `/status` | model/mode/context% | + engine name+version, USD context%, cost |
| `/usage` | credits aggregation | USD cost aggregation (separate column) |
| `/limit` | unchanged (kiro credit-only) | uncapped (no limit enforcement added; out of scope) |
| `/doctor` | preflight kiro | preflight each enabled engine (binary+auth+ACP smoke) + per-channel engine listing |
| `/compact` | kiro compaction | omp session/compact (verify in Stage 2) |
| `/cancel`, `/interrupt` | session/cancel (request) → SIGINT | session/cancel (NOTIFICATION) → SIGINT |

### 5.2 New command

| Command | Behavior |
|---|---|
| 🆕 `/engine` (no arg) | reply current scope engine + version + `AGENT_ENGINES_ENABLED` list |
| 🆕 `/engine <kiro\|omp>` | if enabled: persist scope Session.Engine, stop+restart agent on new engine, confirm; if not enabled: localized rejection; channel-only unless run in thread (then thread-scope override) |

Wiring: buildSlashCommands (with choices kiro/omp) + handler slash dispatch + bang `!engine`/`!e` +
i18n (engine.current/engine.switched/engine.switching/engine.not_enabled/engine.unknown) +
permission (admin-gated like /model) + docs.

### 5.3 Engine-neutral commands (UI + result identical; spawn must carry scope engine)

`/cwd /start /reset /clear /help /pause /back /silent /thread /close /close-thread /resume`
`/steering /memory /flashmemory /mcp /prompt /audit /cron /cron-list /cron-run /cron-prompt /remind /done`

These do not change behavior by engine, BUT every path that spawns an agent must use the
resolved engine for that scope:
- `/start`, channel auto-start → channel engine
- thread first message → inherited thread engine
- `/cron`, `/remind` temp agent → owning channel engine
- `/audit` prompt agent → channel engine
- `/mcp` injection works on both engines (verified)

---

## 6. Discord Scope (channel / thread) × Engine — exhaustive combinations

Scope inheritance recap: thread inherits parent channel engine unless thread has its own override.

| # | Scope | Channel engine | Thread engine | `/engine omp` issued in… | Resulting agent(s) |
|---|---|---|---|---|---|
| C1 | channel | (unset→kiro) | — | — | channel runs kiro |
| C2 | channel | omp (persisted) | — | channel | channel restarts on omp |
| C3 | channel | kiro | — | channel, but omp not in ENABLED | rejected, stays kiro |
| T1 | thread | kiro | (inherit) | — | thread runs kiro (inherited) |
| T2 | thread | omp | (inherit) | — | thread runs omp (inherited) |
| T3 | thread | kiro | omp (override) | thread | thread runs omp; parent channel stays kiro |
| T4 | thread | omp | kiro (override) | thread | thread runs kiro; parent channel stays omp |
| T5 | thread | kiro | — | parent channel | channel switches; existing threads keep their own resolved engine until restarted |
| M1a | channel | bot-A=kiro | — | — | bot A kiro; bot B (separate runtime) omp; BOT_PEERS routes |

State transitions for an active scope when engine changes (C2/T3/T4/T5):
1. mark switching (reply `engine.switching`)
2. stop current `*acp.Agent` (graceful Stop)
3. clear in-flight session continuity for the new engine (new session/new; history prefix applies)
4. start new `*acp.Agent` with new dialect+binary
5. persist Session.Engine
6. reply `engine.switched`
Errors (binary missing / auth fail / preflight fail) → revert to previous engine, reply localized error.

Concurrency: switching while a job is streaming → must abort current job first (reuse existing
cancel/interrupt path), then restart. Define: `/engine` while busy = cancel current turn (warn) then switch.

---

## 7. Usage Accounting (per-engine) — exhaustive

Current model (channel/usage.go): UsageRecord.Credits via creditsFromMetering (unit="credit"),
ContextUsage %, MeteringUsage []MeteringItem. `/usage` report aggregates Credits.

Per-engine rule:
- kiro turn → MeteringItem{Unit:"credit"} (unchanged).
- omp turn → omp dialect tracks cumulative cost; per-turn cost = delta; fills
  TurnMetrics.MeteringUsage = [{Unit:"USD", Value: deltaCost}]; ContextUsage = used/size*100.
- `UsageRecord` gains `Engine string`. `creditsFromMetering` (unit=credit) stays; add
  `costFromMetering` (unit=USD).
- `/usage` report: separate columns Credits (kiro) and Cost USD (omp); turn counts unified;
  NEVER sum credits + USD together.
- Footer (FormatMetricsFooter): kiro `⚡ 0.22 credit · 5.0s · ctx 11%`; omp `⚡ $0.012 · 4.4s · ctx 8%`.
- `/limit`: OUT OF SCOPE for this feature. Usage is CALCULATED and recorded per-engine (credits/USD),
  but no new limit ENFORCEMENT is added. `/limit` keeps its current kiro credit-only behavior unchanged;
  omp turns are simply uncapped. (Revisit only if USD/token enforcement is later requested.)

Exhaustive usage cells:
| Engine | per-turn metric stored | footer | /usage column |
|---|---|---|---|
| kiro | credits (metering) | credit + ctx% | Credits |
| omp | delta USD cost + tokens | $cost + ctx% | Cost(USD) |
| mixed channel history | both, tagged by Engine | per-turn correct | both columns, no cross-sum |

---

## 8. Security alignment (must not regress on either engine)

| Boundary | kiro | omp | Plan |
|---|---|---|---|
| per-call tool permission | request_permission + --trust-all-tools | request_permission (always) | OnRequest + TRUST_ALL_TOOLS/TRUST_TOOLS apply to BOTH; omp even stricter (no trust flag) |
| MCP allowlist/proxy | per-channel policy → session/new mcpServers | same (verified) | unchanged; injection verified on omp |
| safe egress / redaction / AllowedMentions | bot delivery path | same | engine-agnostic (post-agent) |
| cwd policy | ValidateCWD | ValidateCWD | unchanged; omp uses cmd.Dir (also accepts --cwd) |
| audit | agent_job_* events | + engine, + cost/tokens metadata | add engine + usage fields |

### 8.1 MCP per-channel permission is engine-agnostic (VERIFIED)

The bot does NOT rely on the agent's per-call permission for MCP policy. `MCPChannelPolicy.ToACPServer`
(channel/mcp_policy.go:710) injects the **`mcp-proxy` binary** as the MCP server `command`, passing the
real server command + the channel allowlist via **env** (`mcpproxy.ConfigEnv`). The proxy enforces the
allowlist at the MCP transport layer: `filterToolsList` (tools/list) + `toolAllowed`/`blockedToolResponse`
(tools/call), independent of which agent drives it.

Verified at runtime (kiro 2.10.0 + omp 16.1.23): BOTH engines forward the injected `mcpServers` env array
to the launched MCP subprocess (an `env_echo` tool returned the injected env value on both). Therefore the
proxy-based per-channel MCP allowlist applies IDENTICALLY on omp. The earlier observation that "omp does not
emit session/request_permission for MCP tool calls" is irrelevant: enforcement is at the proxy, not the agent
permission layer. **No change needed for omp MCP policy.**

### 8.2 Steering / context files (cross-engine via AGENTS.md)

- kiro reads `.kiro/steering/*.md` AND `AGENTS.md` (AGENTS.md confirmed at runtime: recalled the magic word).
- omp reads `AGENTS.md` / `CLAUDE.md` (confirmed at runtime), NOT `.kiro/steering/`.
- omp's "Steering" internals are the steer/follow-up message queue, unrelated to context files.

Decision: `AGENTS.md` (project root / channel cwd) is the cross-engine common steering file because BOTH
engines read it at runtime. The bot's `/steering` command (currently writes `.kiro/steering/<project>.md`,
kiro-only) becomes engine-aware:
- Maintain `AGENTS.md` at the channel cwd as the cross-engine steering surface (read by kiro + omp).
- Keep `.kiro/steering/<project>.md` for kiro-specific advanced steering (multi-file, front-matter rules).
- All writes still flow through Manager steering-path policy (no handler-side arbitrary path joins; must stay under channel cwd).

Verification expectation: a steering write must be readable by the channel's resolved engine; test both
`.kiro/steering` (kiro) and `AGENTS.md` (kiro + omp) round-trips.

---

## 9. Implementation Stages (task-granular; each stage is one commit; kiro zero-regression first)

Task IDs are stable references; the progress tracker (`docs/dual-engine-progress.md`) mirrors them.

Stage 1 — ACP dialect scaffolding (NO behavior change):
- S1.1 Add `acp/dialect.go`: `Dialect` enum (DialectKiro, DialectOmp) + `dialectProfile` struct + `kiroProfile` = current behavior.
- S1.2 Add `Transport.SendNotification(method, params)` (writes JSON-RPC notification, no id, no wait) in acp/jsonrpc.go.
- S1.3 `acp.Agent` gains `dialect Dialect` field; `StartAgent` signature gains `dialect Dialect` param; route launch args / setModel / cancel / session-parse / metrics through the profile (kiro profile preserves today's exact behavior).
- S1.4 Update all `acp.StartAgent` call sites (manager.go ×4, preflight) to pass `DialectKiro`.
- S1.5 Verify: `go build ./...`, `go vet ./...`, `go test ./acp/ ./channel/ ./bot/ ...` — kiro path behavior-identical. Commit.

Stage 2 — omp dialect + verification:
- S2.1 Implement `ompProfile`: launch `omp acp`; parse `configOptions` → models/modes; `setModel` via `session/set_config_option{configId:"model"}`; `cancel` via `SendNotification("session/cancel")`; metrics from `usage_update` + prompt-result `usage`.
- S2.2 Map omp prompt-result usage → TurnMetrics: per-turn cost = delta of cumulative `cost.amount` (guard negative/reset after compaction → treat as 0); `ContextUsage = used/size*100`; store `MeteringItem{Unit:"USD", Value:deltaCost}`.
- S2.3 omp ACP smoke (PreflightCheck analog, gated like RUN_ACP_SMOKE).
- S2.4 Inline-confirm low-risk items: image prompt e2e, `/clear` semantics, session/load+MCP, omp-unauth error behavior.
- S2.5 Tests: configOptions parsing, omp usage delta cost (incl. compaction reset guard), cancel notification framing, kiro regression. Commit.

Stage 3 — engine config + resolution (enables pure-omp + M1):
- S3.1 env full path: `AGENT_ENGINE`, `AGENT_ENGINES_ENABLED`, `OMP_PATH` through config.go → ManagerConfig + Manager fields + NewManager → main.go → doctor_env.go → locale en/zh-TW → README(+zh) → .env.example.
- S3.2 Engine-aware preflight: preflight each engine in AGENT_ENGINES_ENABLED; never touch kiro-cli when kiro not enabled (§4.1).
- S3.3 Gate `KIRO_HOME`/`KIRO_MCP_CONFIG` injection + `kiro-agent-runtime` creation to kiro dialect only (preflightAgentOptions + agentOptsWithRuntime).
- S3.4 Engine resolution helper (default→channel→thread→override, §3) applied at all 4 spawn points; pick dialect+binary by resolved engine.
- S3.5 `Session.Engine` field (session.go) + read/write at channel/thread spawn (inherit chain).
- S3.6 `/doctor` engine-scoped env display + per-engine preflight result lines.
- S3.7 Verify: pure-omp acceptance (§4.1) on a kiro-absent path (or simulated), full tests, i18n parity. Commit.

Stage 4 — `/engine` command + per-channel/thread switch (M2 primary) + per-engine usage:
- S4.1 `/engine` slash registration (choices kiro/omp) + handler dispatch + bang `!engine`/`!e` + channel/thread scope handling + i18n (engine.current/switched/switching/not_enabled/unknown) + admin permission.
- S4.2 Engine switch state machine (§6): busy→cancel current turn (warn), stop agent, persist Session.Engine, start new dialect agent, history prefix replay, reply; error→revert.
- S4.3 Per-engine usage (§7): `UsageRecord.Engine`; `costFromMetering` (unit=USD) parallel to creditsFromMetering; `/usage` report Credits + Cost(USD) columns, no cross-sum; FormatMetricsFooter branches by unit.
- S4.4 `/steering` engine-aware (§8.2): maintain AGENTS.md (cross-engine) + keep .kiro/steering (kiro), via Manager steering-path policy.
- S4.5 Docs/steering sync: project.md, decision-failure-patterns.md (dual-engine decision + omp dialect facts + M2-primary), README(+zh), .env.example, listen-mode-matrix (if touched), INSTALL/MCP docs if needed.
- S4.6 Verify: full suite, i18n parity, omp smoke, `git diff --check`, manual /engine + /usage + /doctor per engine. Commit.

---

## 10. Execution Protocol (goal-driven, compaction-resistant)

This feature is multi-stage and long. To survive context-window compaction, ALL state lives in files,
not conversation memory. Two files are the single source of truth:
- `docs/dual-engine-integration-plan.md` (this file): WHAT and WHY, exhaustive.
- `docs/dual-engine-progress.md`: WHERE we are — per-task status, current position, next action, verified evidence.

Per-iteration loop (every goal iteration, including the first after any compaction):
1. READ FIRST: re-read `dual-engine-progress.md` (find the single `NEXT:` pointer) and the relevant
   plan section. Never assume position from memory.
2. Re-orient against current code: `git status`, `git log --oneline -5`, and read the actual files the
   next task touches (do not trust remembered file contents).
3. Implement exactly ONE task (smallest committable unit per §9), following project.md + steering rules.
4. VERIFY with the §12 ladder appropriate to the task (build/vet/test/i18n parity). Fix until green.
5. UPDATE `dual-engine-progress.md`: mark the task done with a one-line evidence note (command + result),
   and move the `NEXT:` pointer to the following task.
6. COMMIT at stage boundaries (conventional commit, English) only when the stage's verification passes
   and kiro zero-regression holds. Mid-stage tasks may be left uncommitted but progress file must reflect reality.
7. Repeat until all tasks done and final verification (full suite + omp smoke + i18n + git diff --check) passes.

Compaction-resistance rules:
- The `NEXT:` pointer in the progress file is authoritative. If memory and file disagree, the file wins.
- Never re-do a task already marked done-with-evidence; verify by reading code/tests, not by redoing.
- Record every architecture decision/deviation in decision-failure-patterns.md as it happens, not at the end.
- kiro zero-regression is a hard gate at every commit: if any existing kiro test fails, stop and fix before proceeding.

Completion contract (the goal is satisfied only when ALL hold):
- All S1–S4 tasks marked done-with-evidence in the progress file.
- `go build ./...` + `go vet ./...` + `go test ./...` pass (note any pre-existing env-dependent failures explicitly, e.g. TestDoctorRuntimeOverviewShowsEffectiveDefaultsWhenEnvUnset).
- omp ACP smoke passes (gated run).
- i18n en/zh-TW key parity holds; `git diff --check` clean.
- Docs/steering aligned (§4.5). M2 single-bot switch, pure-omp, and kiro-unchanged paths all verified.

---

## 11. Open items requiring runtime confirmation in Stage 2 (not blockers)

- omp `/compact` (session/compact) exact result shape.
- omp `usage_update` incremental field stability across versions (pin omp version; prompt-result usage is the reliable fallback).

## 12. Verification ladder for this feature

- acp dialect unit tests (parse/setModel/cancel framing).
- channel runtime-path tests (engine resolution, switch restart, per-engine usage).
- omp ACP smoke (gated, like RUN_ACP_SMOKE).
- i18n parity. go build/vet. /doctor manual check per engine.
