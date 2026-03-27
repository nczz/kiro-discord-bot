# GitHub Issues for acp-bridge Removal Migration

Below are the 6 issues to create on https://github.com/nczz/kiro-discord-bot/issues.
Use label: `refactor`, milestone: `v2.0 - Remove acp-bridge`

---

## Issue #1: [ACP] Replace HTTP client with direct stdio ACP transport

**Labels:** `refactor`, `acp`

### Description
Replace the current `acp/client.go` (HTTP client talking to acp-bridge daemon) with a direct JSON-RPC over stdio implementation that spawns and manages kiro-cli processes directly.

### Background
PoC validated at `/tmp/acp-poc/` on 2026-03-27. See `docs/migration-remove-acp-bridge.md` for full context.

### Tasks
- [ ] Create `acp/protocol.go` — method name constants, version definitions
- [ ] Create `acp/jsonrpc.go` — ndjson JSON-RPC 2.0 transport (read loop, send with response tracking, notification dispatch)
- [ ] Create `acp/agent.go` — `StartAgent()`, `Ask()`, `Cancel()`, `Stop()`, `Kill()`, `Pid()`, `IsAlive()`
- [ ] Create `acp/agent_test.go` — integration tests (TestStartAndAsk, TestStreaming, TestCancel, TestStop, TestContextMemory)
- [ ] Delete `acp/client.go`, `acp/sse.go`, `acp/client_test.go`

### Key Design Decisions
- Agent spawns with `Setpgid: true` for process group kill
- Stop: SIGTERM → wait 2s → SIGKILL (entire process group)
- Cancel: send `session/cancel`, wait up to 5s for prompt response to drain
- Notification handler for `session/update` extracts text from `update.content.text`
- Protocol v1 method names: `session/new`, `session/prompt`, `session/cancel`

### Acceptance Criteria
- All 5 integration tests pass
- No dependency on acp-bridge or HTTP
- `go build` succeeds with no acp-bridge imports

---

## Issue #2: [Channel] Refactor Manager to use direct Agent

**Labels:** `refactor`, `channel`  
**Depends on:** #1

### Description
Update `channel/manager.go` to spawn `acp.Agent` directly instead of calling acp-bridge HTTP API.

### Tasks
- [ ] Replace `acpClient *acp.Client` with agent management (map of `*acp.Agent` per channel)
- [ ] `startAgentAndWorker()` → call `acp.StartAgent()`, get PID from `agent.Pid()`
- [ ] Extract `stopChannel(channelID)` helper to deduplicate Reset/Restart/StartAt
- [ ] Remove `findNewPID()`, `currentPIDs()`, `killProcessTree()` (~50 lines)
- [ ] Remove `AgentPID` field from `Session` struct in `session.go`
- [ ] `CheckAgent()` → `agent.IsAlive()` (check process state)
- [ ] `StartTempAgent()` / `StopTempAgent()` → use `acp.StartAgent()` / `agent.Stop()`
- [ ] `AskAgent()` / `AskAgentStream()` → use `agent.Ask()`

### Acceptance Criteria
- No `pgrep` or `syscall.Kill` calls outside of `acp/agent.go`
- Reset/Restart/StartAt share common `stopChannel()` logic
- Session JSON no longer contains `agentPid`

---

## Issue #3: [Channel] Update Worker to use direct Agent streaming

**Labels:** `refactor`, `channel`  
**Depends on:** #1, #2

### Description
Update `channel/worker.go` to use `agent.Ask()` with callback-based streaming instead of HTTP SSE.

### Tasks
- [ ] Worker receives `*acp.Agent` from Manager (not client + agentName)
- [ ] `execute()` calls `agent.Ask(ctx, prompt, onChunk)` directly
- [ ] Remove SSE-related code paths
- [ ] Streaming status detection (tool usage keywords) remains unchanged

### Acceptance Criteria
- Discord messages update in real-time during streaming
- ⏳→🔄→✅ reaction flow works
- Timeout shows ⚠️ and cancels agent

---

## Issue #4: [Bot] Update adapters and heartbeat for direct Agent

**Labels:** `refactor`, `bot`, `heartbeat`  
**Depends on:** #2

### Description
Update the adapter layer and heartbeat tasks to work without acp-bridge.

### Tasks
- [ ] `bot/bot.go` — remove `acpBridgeURL` field from Bot struct
- [ ] `bot/health_adapter.go` — `CheckAgent()` uses process liveness check
- [ ] `bot/cron_adapter.go` — `StartTempAgent()` / `StopTempAgent()` use `acp.Agent`
- [ ] `heartbeat/health.go` — remove acp-bridge HTTP health check (`GET /agents`)
- [ ] `heartbeat/health.go` — `HealthDeps` interface: remove or simplify

### Acceptance Criteria
- Health check detects dead agents and auto-restarts
- Cron jobs execute with temp agents
- No HTTP calls to localhost:7800

---

## Issue #5: [Boot] Add preflight check and version detection

**Labels:** `enhancement`, `reliability`  
**Depends on:** #1

### Description
Add a startup preflight check that validates the full ACP lifecycle before accepting Discord messages. Detect kiro-cli version changes.

### Tasks
- [ ] Add `PreflightCheck(kiroCLI string) error` — spawn → handshake → ask "OK" → stop
- [ ] Call in `main.go` before `b.Start()`, `log.Fatal` on failure
- [ ] Log kiro-cli version from `initialize` response (`agentInfo.version`)
- [ ] Log protocol version, warn if unexpected value
- [ ] Add `SKIP_PREFLIGHT` env var to bypass (for development)

### Acceptance Criteria
- Bot refuses to start if kiro-cli ACP handshake fails
- Log shows: `[preflight] kiro-cli v1.28.1, protocol=1, check passed`
- `SKIP_PREFLIGHT=1` skips the check

---

## Issue #6: [Infra] Remove acp-bridge from Docker, docs, and config

**Labels:** `refactor`, `documentation`, `infrastructure`  
**Depends on:** #1-#5

### Description
Remove all acp-bridge references from deployment infrastructure and documentation.

### Tasks
- [ ] `docker-compose.yml` — remove `acp-bridge` service, remove `depends_on`
- [ ] `Dockerfile` — ensure kiro-cli is available in container (document mount or install)
- [ ] `start.sh` — remove acp-bridge startup and watchdog loop
- [ ] `config.go` — remove `AcpBridgeURL` from Config struct
- [ ] `.env.example` — remove `ACP_BRIDGE_URL`
- [ ] `go.mod` — remove unused dependencies if any
- [ ] `README.md` — update:
  - Prerequisites: remove "Node.js 20+" and "acp-bridge"
  - Architecture diagram: remove acp-bridge layer
  - Install steps: remove `npm install -g acp-bridge`
  - Environment variables table: remove `ACP_BRIDGE_URL`
  - Docker section: single-service compose
  - Add "ACP Protocol" section documenting supported version
- [ ] `INSTALL_MCP.md` — verify no acp-bridge references

### Acceptance Criteria
- `docker compose up -d --build` works with single Go service
- `./start.sh` works without Node.js installed
- No mention of "acp-bridge" in README except changelog/history
- `.env.example` has no `ACP_BRIDGE_URL`
