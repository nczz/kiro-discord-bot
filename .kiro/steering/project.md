---
name: project-contract
description: Use for ANY code change, build, debug, or architecture question in this Go project.
---

# kiro-discord-bot — Project Contract

## Build & Run

- Build: `go build -o kiro-discord-bot .`
- Build MCP server: `go build -o mcp-discord-server ./cmd/mcp-discord/`
- Test: `go test ./...`
- Single package test: `go test ./acp/`
- Run: `./start.sh` (reads `.env`, auto-restart watchdog)
- Config: all settings from `.env`, see `config.go` `loadConfig()`

## Architecture Boundaries

```
main.go          → loadConfig + bot.Start + signal wait
config.go        → .env → Config struct (mustEnv / envOr / envInt)
bot/             → Discord gateway, slash commands, message routing
  handler.go     → message + slash command dispatch (不放業務邏輯)
  handler_cron.go→ /cron Modal, /cron-list Button, /remind
  notifier.go    → shared botNotifier (Notify+IsSilent) embedded by all adapters
  health_adapter → heartbeat.HealthDeps bridge
  cron_adapter   → heartbeat.CronDeps bridge
  thread_cleanup_adapter → heartbeat.ThreadCleanupDeps bridge
  channel_cleanup_adapter → heartbeat.ChannelCleanupDeps bridge
channel/         → per-channel lifecycle
  manager.go     → session + worker + agent 生命週期管理中樞
  worker.go      → job queue goroutine, thread-based execution
  session.go     → SessionStore JSON persistence
  logger.go      → JSONL conversation log
acp/             → kiro-cli ACP child process (JSON-RPC over stdio)
  agent.go       → spawn, handshake, ask, cancel, stop
  jsonrpc.go     → ndjson transport
  ringbuf.go     → thread-safe ring buffer for stderr capture
  protocol.go    → ACP constants (protocol version 2025-11-16)
heartbeat/       → background task loop
  health.go      → agent liveness check + auto-restart
  cleanup.go     → expired attachment removal
  cron.go        → cron scheduler + temp agent execution
  cron_store.go  → cron job JSON persistence
  schedule.go    → natural language → cron/time parser
  thread_cleanup.go → idle thread agent eviction
  channel_cleanup.go → idle channel agent eviction
```

- handler 只做路由和轉發，業務邏輯在 channel/manager
- acp/ 以外不直接操作 agent process
- heartbeat/ 透過 interface (HealthDeps, CronDeps, ThreadCleanupDeps, ChannelCleanupDeps) 與 bot 解耦

## NEVER

- 在 `acp/` 以外直接 spawn 或管理 kiro-cli process
- 忽略 Go error return（`err` 必須處理或顯式 `_ =` 標註理由）
- 在 handler 層放業務邏輯（應透過 manager 操作）

## ALWAYS

- 改完 `.go` 檔案後執行 `go build ./... 2>&1 | head -30` 確認編譯
- 改 acp/ 時確認 ACP 協議常數與 handshake 流程一致
- 新增 Discord command 時同步更新 `buildSlashCommands()` 和 handler dispatch
- 修改 struct 欄位時檢查所有 caller 是否同步更新

## Verification（驗證閉環）

每次任務完成前，依變更範圍執行對應驗證：

| 變更範圍 | 必須通過 |
|----------|---------|
| 任何 Go 檔案 | `go build ./... 2>&1 \| head -30` |
| acp/ | `go test ./acp/ 2>&1 \| tail -20` |
| 邏輯變更 | `go vet ./... 2>&1 \| head -20` |
| 新增/修改 struct | 確認所有 caller 欄位同步 |
| handler 新增 command | 確認 slash command 註冊 + dispatch 都有 |
