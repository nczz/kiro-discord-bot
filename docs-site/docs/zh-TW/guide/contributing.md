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

## Static Site

Canonical docs 位於 `docs-site/docs/`。不要讓 README 或 INSTALL files 成為長篇 source of truth；它們應保持精簡並導向靜態站。
