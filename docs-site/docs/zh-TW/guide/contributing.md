# 貢獻者指南

這個專案是 Go Discord bot，加上可選 MCP servers 與零依賴 static documentation site。

## Repository Layout

| Path | 用途 |
| --- | --- |
| `bot/` | Discord command handling、replies、audit integration、MCP panels、user-facing command behavior。 |
| `channel/` | Channel/session manager、workers、listen modes、memory、usage、MCP policy。 |
| `heartbeat/` | Cron、reminders、cleanup、background maintenance。 |
| `audit/` | SQLite audit recorder 與 timeline query store。 |
| `internal/botmcp/` | 內建 `bot-tools` MCP server。 |
| `cmd/mcp-discord/` | Standalone Discord MCP server。 |
| `cmd/mcp-media/` | Standalone media-generation MCP server。 |
| `docs-site/` | Canonical static documentation site。 |
| `docs/` | 歷史 notes 與短 compatibility documents；適合時導向靜態站。 |
| `scripts/` | Release 與 validation helpers。 |

## 本機驗證

先跑你修改範圍的 focused tests，commit 前再跑完整測試：

```bash
go test -count=1 ./...
```

文件驗證：

```bash
cd docs-site
npm run verify
```

Release readiness：

```bash
scripts/release-preflight.sh
```

除非 maintainer 明確接受並記錄例外，升版與 GitHub release 前 release preflight 應通過。

## 開發規則

行為改動要同步測試與文件。只要改到 command、environment variable、MCP tool、audit event、usage attribution rule、deployment script 或 release flow，就要更新 docs-site 中負責該行為的頁面。

偏好小而有 code-path 依據的修改，避免無關的大型 refactor。這個 bot 的 operational state 橫跨 Discord、本機檔案、Kiro CLI sessions 與 MCP policy，回歸常常只會在多層互動時出現。

## Agent 入口文件

Repo root 的 `AGENTS.md` 是 agent 第一個應讀的文件。它摘要跨 Kiro、OMP 與其他 coding agents 都適用的不可違背原則、架構邊界、source-of-truth map、i18n/Discord output 規則、MCP permission 規則與 verification expectations。

`.kiro/steering/*.md` 用於更深入的 Kiro steering 與 recurring project knowledge，不作為唯一的跨 agent 契約。若 `AGENTS.md`、`.kiro/steering/*` 與本 docs site 互相衝突，先停止並修正 drift，再實作。

只要改 architecture boundaries、security rules、verification expectations 或 agent onboarding guidance，就要在同一個 change 更新 `AGENTS.md`。

`AGENTS.md` 應保持短小，能快速帶 agent 進入狀況；細節請連到 deeper docs，不要把每個 feature plan 全部複製進去。

### Agent 最小檢查清單

- 先讀 `AGENTS.md`。
- 依 `AGENTS.md` 指向，讀取 task-specific steering 或 design doc。
- 優先使用既有 owner packages 與 helpers，不新增重複 local logic。
- User-facing behavior 要保持 code、tests、locale files 與 docs 對齊。
- 回報成功前，執行能覆蓋真實 changed path 的最小 verification。

## Agent 協作契約

實作需要 subagents 時，parent agent 必須先定義共享契約，再開始平行工作。契約要明確列出所有 slice 必須遵守的穩定 interfaces、permission boundaries、data ownership 與 verification commands。

- `docs-site/docs/` 是 GitHub Pages 與 user-facing behavior 的 source of truth。只要改 command、MCP tool、audit row、lifecycle state 或 permission check，就要在同一個 change 更新它。
- Discord output 必須走共用 helpers：先 redaction、oversized replies 要 split、用 empty `AllowedMentions` suppress mentions，且 user-facing text 要用 locale keys。
- 一般 feature work 不檢查或暴露 raw bot `DATA_DIR/ch-*` paths。請改用 scoped MCP tools、audit queries 與已文件化的 state APIs。
- Lifecycle/admin actions 必須使用 authenticated Discord actor context。Agent 或 MCP client 不能自行宣稱有 management permission。
- 回報 mutation success 前必須先記錄 durable audit。Install、restore、rollback 或 policy changes 若有 partial failure，要修 transaction，不要只遮住症狀。
- Channel agent UX 優先自然語言。Slash 與 text commands 是 fallback/admin shortcuts，適用時必須呼叫與 bot-tools 相同的 internal services。

## Static Site

Canonical docs 位於 `docs-site/docs/`。不要讓 README 或 INSTALL files 成為長篇 source of truth；它們應保持精簡並導向靜態站。
