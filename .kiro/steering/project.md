---
name: project-contract
description: Use for ANY code change, build, debug, or architecture question in this Go project.
---

# kiro-discord-bot — Project Contract

## Build & Run

- Build: `go build -o kiro-discord-bot .`
- Build MCP server: `go build -o mcp-discord ./cmd/mcp-discord/`
- Test: `go test ./...`
- Single package test: `go test ./acp/`
- Release preflight: `scripts/release-preflight.sh`
- Local ACP smoke: `RUN_ACP_SMOKE=1 KIRO_CLI=$(which kiro-cli) scripts/release-preflight.sh`
- Run: `systemctl start kiro-discord-bot` (systemd, recommended) or `export $(grep -v '^#' .env | xargs) && ./kiro-discord-bot` (manual)
- Config: bot settings from `.env`, see `config.go` `loadConfig()`; Discord MCP-only settings are read in `cmd/mcp-discord/`
- Diagnostics: `/doctor` or `!doctor` checks `kiro-cli`, default cwd, cwd allowlist, data dir writeability, and ACP preflight

## Architecture Boundaries

```
main.go          → loadConfig + bot.Start + signal wait
config.go        → .env → Config struct (mustEnv / envOr / envInt / envBool)
bot/             → Discord gateway, slash commands, message routing
  handler.go     → message + slash command dispatch (不放業務邏輯)
  handler_cron.go→ /cron Modal, /cron-list Button, /remind
  notifier.go    → shared botNotifier (Notify+IsSilent) embedded by all adapters
  health_adapter → heartbeat.HealthDeps bridge
  cron_adapter   → heartbeat.CronDeps bridge
  thread_cleanup_adapter → heartbeat.ThreadCleanupDeps bridge
  channel_cleanup_adapter → heartbeat.ChannelCleanupDeps bridge
channel/         → per-channel lifecycle
  manager.go     → session + worker + agent 生命週期管理中樞、cwd allowlist enforcement
  worker.go      → job queue goroutine, thread-based execution
  session.go     → SessionStore JSON persistence
  logger.go      → JSONL conversation log
acp/             → kiro-cli ACP child process (JSON-RPC over stdio)
  agent.go       → spawn, handshake (session/new or session/load), ask, cancel, stop, set_model, set_mode
  jsonrpc.go     → ndjson transport
  ringbuf.go     → thread-safe ring buffer for stderr capture
  protocol.go    → ACP constants, capability structs, PromptContent (protocol version 1)
heartbeat/       → background task loop
  health.go      → agent liveness check + auto-restart
  cleanup.go     → expired attachment removal
  cron.go        → cron scheduler + temp agent execution
  cron_store.go  → cron job JSON persistence
  schedule.go    → natural language → cron/time parser
  thread_cleanup.go → idle thread agent eviction
  channel_cleanup.go → idle channel agent eviction
cmd/mcp-discord/ → optional Discord MCP server, REST tools + guild/channel allowlists
scripts/         → repeatable local release/preflight checks
docs/release.md  → release and deployment safety checklist
.github/         → CI preflight workflow for push/PR checks
```

- handler 只做路由和轉發，業務邏輯在 channel/manager
- acp/ 以外不直接操作 agent process
- heartbeat/ 透過 interface (HealthDeps, CronDeps, ThreadCleanupDeps, ChannelCleanupDeps) 與 bot 解耦

## Design Principles（設計原則）

- **Silent mode 是全域設計原則**：所有非使用者主動觸發的通知（idle cleanup、agent 斷線、health restart 等）都必須遵守 silent 設定。silent ON = 靜音，silent OFF = 顯示。
- **BotConfig 嵌入 ManagerConfig**：新增 Manager 設定只需改 `ManagerConfig` + `main.go` 兩處，不需逐欄位複製。
- **Adapter 共用 botNotifier**：所有 heartbeat adapter 嵌入 `botNotifier`，Notify / IsSilent 不重複實作。
- **CWD policy 在 Manager 層統一執行**：`/start`、`/cwd`、thread agents、cron temp agents 都必須走 `ValidateCWD`，不得在 handler 或 heartbeat 層自行繞過。
- **Project steering 路徑在 Manager 層控管**：Discord `/steering` 與初始化完成 shortcut 只能操作目前 channel CWD 底下的 `.kiro/steering/<project>.md`，不得在 handler 層自行拼接任意使用者路徑。
- **ACP tool permission 預設由本地策略決定**：只有 `TRUST_ALL_TOOLS=true` 或 `TRUST_TOOLS` 命中才 approve；未授權 tool permission request 要 deny。
- **ACP feature gating**：`session/load`、image prompt 必須先檢查 agent 的 `agentCapabilities`；`session/set_model`、`session/set_mode` 必須先用 `session/new` 回傳的 available models/modes 驗證 ID。不能只信任 RPC success，kiro-cli 可能在下一個 prompt 才暴露 invalid model。
- **Session continuity 優先用 session/load**：agent 重啟時，如果 store 中有 SessionID 且 agent 支援 `loadSession`，優先用 `session/load` 恢復完整對話歷史。失敗時 fallback 到 `session/new` + `historyPrefix`。
- **Discord MCP 安全邊界**：MCP guild/channel allowlist 與 write guard 在 `cmd/mcp-discord` 內執行。新增 Discord REST tool 時必須先判斷它是 guild-scoped、channel-scoped、global、read-only、write 或 destructive，並套用對應 policy。
- **Discord 回覆格式化必須復用既有工具**：任何會送到 Discord 的長文字、MCP tool output、safe egress、thread/final response、reply、embed description，都不得自行實作分段、Markdown 降級、code fence 修補或分段 prefix。必須復用 `internal/discordfmt.Split` 與 `internal/discordfmt.WithPartPrefix`，或復用已建立在它們之上的專案封裝（例如 `bot` / `channel` 既有長訊息送出 helper）。若現有 helper 不足，先擴充共用 helper 並補測試，不要在 feature code 中複製一份 split 邏輯。
- **MCP / egress security 與 audit 不可繞過**：任何 Discord 寫入路徑都必須保留對應的 allowlist、read-only/write/destructive guard、secret redaction、AllowedMentions 防護、delivery error handling 與 audit/語意事件紀錄。新增或修改 MCP tool 時，不得為了修復 UX 而直接改用裸 Discord API 繞過 policy proxy、safe egress pending queue、redactor 或既有 delivery wrapper。若需要分批送出，分批前仍先套用同一套 policy；每批送出也必須沿用同一套 redaction、mention suppression、錯誤處理與測試。
- **Release preflight 不碰 runtime state**：preflight script 只能 build/test/check artifacts，不得停止/啟動 bot、修改 `DATA_DIR`、刪除 Docker volumes、改寫 `.env` 或觸發 Discord side effects。

## Collaboration（協作方式）

- **語言**：繁體中文溝通，commit message 用英文 conventional commits
- **版本慣例**：`vX.Y.Z` — minor 遞增代表功能新增或行為變更；patch 遞增代表 bug fix 或文件修正。用 `git tag vX.Y.Z` + `git push origin vX.Y.Z` 觸發 GoReleaser。
- **功能性變更先討論**：先提方案和 tradeoff，確認方向後再實作。簡單 bug fix 或明確指令可直接動手。
- **完成後主動審視**：功能完成後主動提出重構建議或維護性改善，不需等使用者問。
- **測試要求**：新增功能或 bug fix 應附帶對應測試。至少 exported function + 關鍵邏輯路徑要有覆蓋。純 refactoring 或文件變更免測試。

## NEVER

- 在 `acp/` 以外直接 spawn 或管理 kiro-cli process
- 在 Manager `ValidateCWD` 以外接受使用者提供的 agent cwd
- 新增 Discord MCP channel/guild 操作但未檢查 allowlist
- 自行手寫 Discord 訊息分段、Markdown 包裝或 prefix 格式，而不是復用 `internal/discordfmt` 或既有長訊息 helper
- 新增 Discord/MCP 寫入路徑但繞過 policy guard、secret redaction、AllowedMentions、防嵌入設定、delivery audit 或既有 safe egress pipeline
- 忽略 Go error return（`err` 必須處理或顯式 `_ =` 標註理由）
- 在 handler 層放業務邏輯（應透過 manager 操作）

## ALWAYS

- 改完 `.go` 檔案後執行 `go build ./... 2>&1 | head -30` 確認編譯
- 改 acp/ 時確認 ACP 協議常數與 handshake 流程一致
- 新增 Discord command 時同步更新 `buildSlashCommands()` 和 handler dispatch
- 修改 struct 欄位時檢查所有 caller 是否同步更新
- 修改 Docker runtime 或 deployment env 時同步檢查 README、`.env.example`、`docker-compose.yml`
- 修改 Discord 發訊息、檔案、embed、MCP egress 或 agent final response 時，檢查是否復用 `internal/discordfmt` / 既有 helper，並補長訊息、code block、UTF-8、reply/thread target、redaction / policy guard 測試
- 發布或部署前跑 `scripts/release-preflight.sh`；需要真實 ACP 才加 `RUN_ACP_SMOKE=1 KIRO_CLI=...`
- CI workflow 只跑不需要 secrets 的檢查；ACP smoke 必須留在本機或部署主機執行

## Completeness Checklist（改動完整性）

每次改動完成後，檢查以下項目是否需要同步更新：

- [ ] i18n：`locale/lang/en.json` 和 `zh-TW.json` key 完全對齊
- [ ] README.md：英文段 + 中文段都更新（env 表格、Project Structure、Notes）
- [ ] `.env.example`：新增 env var 時同步
- [ ] `.kiro/steering/project.md`：架構圖或設計原則有變時同步
- [ ] `INSTALL_MCP.md` + `.kiro/steering/discord-mcp.md`：Discord MCP 行為或安全邊界改變時同步
- [ ] `docs/release.md` + `scripts/release-preflight.sh`：發布門檻或部署流程改變時同步
- [ ] `.github/workflows/preflight.yml`：preflight 腳本或 CI 門檻改變時同步
- [ ] 新增 env var 完整路徑：`config.go` → `ManagerConfig` / `BotConfig`（若影響 runtime）→ `main.go` → `channel/doctor_env.go` (envSpecs) → `locale/lang/en.json` + `zh-TW.json` (doctor.env.desc.*) → README.md → `.env.example`
- [ ] 新增 Discord MCP-only env var：`cmd/mcp-discord` → README → `.env.example` → `INSTALL_MCP.md`

## Verification（驗證閉環）

每次任務完成前，依變更範圍執行對應驗證：

| 變更範圍 | 必須通過 |
|----------|---------|
| 任何 Go 檔案 | `go build ./... 2>&1 \| head -30` |
| acp/ | `go test ./acp/ 2>&1 \| tail -20` |
| 邏輯變更 | `go vet ./... 2>&1 \| head -20` |
| 新增/修改 struct | 確認所有 caller 欄位同步 |
| handler 新增 command | 確認 slash command 註冊 + dispatch 都有 |
| i18n 變更 | 確認 en.json 和 zh-TW.json key 數量一致 |
