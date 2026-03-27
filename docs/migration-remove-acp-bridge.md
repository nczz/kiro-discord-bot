# Migration Plan: Remove acp-bridge, Direct ACP over stdio

## Background

The bot currently depends on [acp-bridge](https://www.npmjs.com/package/acp-bridge) (Node.js daemon) as a middleman between the Go bot and kiro-cli. This adds:
- An extra HTTP hop (Go → HTTP → acp-bridge → stdio → kiro-cli)
- A Node.js runtime dependency
- Unreliable PID detection via `pgrep` (cross-platform issues)
- No SIGKILL fallback when stopping agents
- A single-maintainer external dependency (v0.3.0, 21 stars)

## PoC Results (2026-03-27)

A standalone PoC at `/tmp/acp-poc/` verified direct ACP communication:

| Test | Result | Notes |
|------|--------|-------|
| Spawn + ACP handshake | ✅ PASS | `initialize` → `session/new`, PID directly available |
| Simple ask | ✅ PASS | `session/prompt`, correct response |
| Streaming | ✅ PASS | `session/update` notifications, chunks received |
| Cancel | ✅ PASS | `session/cancel` + context timeout |
| Ask after cancel | ❌ FAIL | kiro-cli returns Internal error (same behavior via acp-bridge) |
| Stop (SIGTERM + process group) | ✅ PASS | Clean exit, PID confirmed gone |

## kiro-cli ACP Protocol (v1)

```
Client                          kiro-cli
  │                                │
  │─── initialize ────────────────▶│  protocolVersion: "2025-11-16"
  │◀── result ────────────────────│  protocolVersion: 1
  │                                │
  │─── session/new ───────────────▶│  {cwd, mcpServers}
  │◀── result ────────────────────│  {sessionId, modes, models}
  │                                │
  │─── session/prompt ────────────▶│  {sessionId, prompt}
  │◀── notification ──────────────│  session/update {update.content.text}  (×N)
  │◀── result ────────────────────│  {stopReason}
  │                                │
  │─── session/cancel ────────────▶│  {sessionId}
  │◀── result ────────────────────│  (session becomes unusable)
```

Key differences from standard ACP:
- Protocol version is numeric `1`, not string `"2025-11-16"`
- Methods: `session/new` (not `acp/newSession`), `session/prompt`, `session/cancel`
- Notifications: `session/update` with content nested in `update.content`
- Cancel makes session unrecoverable (need to restart agent)

## Migration Phases

### Phase 1: New `acp` package (Issue #1)
Replace HTTP client with direct stdio process management.

Files to create:
- `acp/jsonrpc.go` — JSON-RPC 2.0 ndjson transport (~140 lines)
- `acp/agent.go` — Process spawn + ACP handshake + ask/cancel/stop (~200 lines)
- `acp/protocol.go` — Method name constants + version check (~30 lines)
- `acp/agent_test.go` — Integration tests (~120 lines)

Files to delete:
- `acp/client.go` — HTTP client (replaced)
- `acp/sse.go` — SSE parser (no longer needed)
- `acp/client_test.go` — Old integration tests (replaced)

### Phase 2: Update `channel/manager.go` (Issue #2)
Replace Manager's acp.Client usage with direct Agent management.

Changes:
- Remove `findNewPID`, `currentPIDs`, `killProcessTree` (~50 lines deleted)
- `startAgentAndWorker` → spawn Agent directly, PID from `cmd.Process.Pid`
- Extract `stopChannel()` helper (deduplicate Reset/Restart/StartAt)
- Stop uses process group kill: SIGTERM → wait 2s → SIGKILL
- Remove `AgentPID` from Session struct

### Phase 3: Update `channel/worker.go` (Issue #3)
Replace HTTP-based AskStream with direct Agent.Ask.

Changes:
- Worker holds `*acp.Agent` instead of `*acp.Client` + agentName
- `execute()` calls `agent.Ask(ctx, prompt, onChunk)` directly
- Streaming chunks come from notification handler, not SSE parsing

### Phase 4: Update adapters + heartbeat (Issue #4)
- `bot/health_adapter.go` — CheckAgent via Agent.IsAlive() (process check)
- `bot/cron_adapter.go` — StartTempAgent/StopTempAgent use Agent directly
- `heartbeat/health.go` — Remove acp-bridge reachability check

### Phase 5: Preflight check + version detection (Issue #5)
- Add `PreflightCheck()` in main.go — spawn → handshake → ask → stop
- Log kiro-cli version from initialize response
- Fail fast on protocol mismatch

### Phase 6: Docker + docs + cleanup (Issue #6)
- Remove `acp-bridge` service from `docker-compose.yml`
- Remove Node.js dependency from Dockerfile
- Simplify `start.sh` (no acp-bridge watchdog)
- Remove `ACP_BRIDGE_URL` from `.env.example` and config.go
- Update README.md architecture diagram and prerequisites
- Update INSTALL_MCP.md if affected

## Files Changed Summary

| File | Action | Phase |
|------|--------|-------|
| `acp/jsonrpc.go` | CREATE | 1 |
| `acp/agent.go` | CREATE | 1 |
| `acp/protocol.go` | CREATE | 1 |
| `acp/agent_test.go` | CREATE | 1 |
| `acp/client.go` | DELETE | 1 |
| `acp/sse.go` | DELETE | 1 |
| `acp/client_test.go` | DELETE | 1 |
| `channel/manager.go` | MODIFY | 2 |
| `channel/session.go` | MODIFY | 2 |
| `channel/worker.go` | MODIFY | 3 |
| `bot/bot.go` | MODIFY | 4 |
| `bot/health_adapter.go` | MODIFY | 4 |
| `bot/cron_adapter.go` | MODIFY | 4 |
| `heartbeat/health.go` | MODIFY | 4 |
| `main.go` | MODIFY | 5 |
| `config.go` | MODIFY | 6 |
| `docker-compose.yml` | MODIFY | 6 |
| `Dockerfile` | MODIFY | 6 |
| `start.sh` | MODIFY | 6 |
| `.env.example` | MODIFY | 6 |
| `README.md` | MODIFY | 6 |
| `go.mod` | MODIFY | 6 |

## Test Acceptance Criteria

### Unit / Integration Tests
- [ ] `acp/agent_test.go`: TestStartAndAsk — spawn, handshake, simple ask
- [ ] `acp/agent_test.go`: TestStreaming — verify chunks received
- [ ] `acp/agent_test.go`: TestCancel — cancel mid-response, verify timeout
- [ ] `acp/agent_test.go`: TestStop — SIGTERM, verify process gone
- [ ] `acp/agent_test.go`: TestContextMemory — multi-turn conversation
- [ ] `acp/agent_test.go`: TestPreflightCheck — full lifecycle in <30s

### Manual E2E Tests (Discord)
- [ ] `/start /tmp` — agent starts, bot responds
- [ ] Send message — get streaming response with ⏳→🔄→✅ reactions
- [ ] `/cancel` during long response — stops, shows ⚠️
- [ ] Send message after cancel — agent auto-restarts, responds
- [ ] `/reset` — agent restarts cleanly
- [ ] `/model <id>` — switch model, agent restarts with new model
- [ ] `/status` — shows agent state, queue length
- [ ] `/cron` — scheduled task executes with temp agent
- [ ] `/remind 1min test` — reminder fires
- [ ] `!resume` — re-posts last response
- [ ] `/pause` + `/back` — mention-only mode works
- [ ] Bot restart — sessions recover, agents restart
- [ ] Docker deploy — `docker compose up -d --build` works without Node.js

### Regression Checks
- [ ] No orphan kiro-cli processes after bot stop
- [ ] No orphan kiro-cli processes after `/reset`
- [ ] Conversation context preserved across multiple messages
- [ ] Attachments download and prompt injection still work
- [ ] Chat JSONL logging still works
- [ ] Long responses split correctly with (1/N) labels

## Rollback Plan

Keep the old `acp/client.go` in a `_deprecated` branch. If critical issues found:
1. `git checkout _deprecated -- acp/`
2. Revert manager.go and worker.go changes
3. Restart acp-bridge

## Estimated Effort

| Phase | Effort | Risk |
|-------|--------|------|
| 1. New acp package | 3-4 hours | Low (PoC validated) |
| 2. Manager refactor | 2-3 hours | Medium (state management) |
| 3. Worker refactor | 1-2 hours | Low |
| 4. Adapters + heartbeat | 1-2 hours | Low |
| 5. Preflight check | 1 hour | Low |
| 6. Docker + docs | 1-2 hours | Low |
| Testing | 2-3 hours | — |
| **Total** | **~12-16 hours** | |
