# kiro-discord-bot

住在 Discord 裡、可以被訓練的 AI agent。

把 Discord 頻道綁定到真實專案，持續累積規則與脈絡，並用 MCP 安全擴充能力。

[快速開始](guide/getting-started.html) · [安裝](guide/installation.html) · [指令參考](guide/commands.html) · [English](/)

## 重點

- **綁定專案的 agent**：每個 Discord 頻道可以綁定工作目錄、維持獨立 session，直接處理開發與維運工作。
- **可累積的脈絡**：Memory、flash memory、steering 檔案、對話紀錄與 Kiro knowledge 一起工作，不必每次從零開始。
- **可控的 MCP 權限**：MCP server 先作為 catalog 被發現，再依頻道政策透過 proxy 控制可見工具與可呼叫範圍。
- **清楚的維運邊界**：管理查詢盡量私密回覆，部署檢查可重複執行，疑難排解直接對應真實故障情境。

## 這個網站提供什麼

這裡是 `kiro-discord-bot` 的操作手冊：安裝、頻道初始化、日常使用、steering、MCP 權限、audit 與 usage、管理安全、部署、release、安全審查與疑難排解。

README 保留為精簡入口；完整教學與 runbook 放在這裡。

## 依角色閱讀

| 角色 | 建議頁面 |
| --- | --- |
| 第一次評估專案的人 | [快速開始](guide/getting-started.html)、[核心概念](guide/core-concepts.html)、[日常工作流](guide/daily-workflows.html) |
| 第一次安裝的人 | [安裝](guide/installation.html)、[環境變數參考](guide/environment.html)、[部署](guide/deployment.html) |
| 日常 Discord 使用者 | [指令參考](guide/commands.html)、[監聽模式](guide/listen-modes.html)、[Cron 與提醒](guide/cron-reminders.html) |
| Channel/admin 管理者 | [管理與安全](guide/admin-security.html)、[MCP 權限](guide/mcp.html)、[Audit、用量與隱私](guide/audit-usage-privacy.html) |
| MCP 管理者 | [MCP 權限](guide/mcp.html)、[Bot Tools MCP](guide/bot-tools.html)、[Discord MCP](guide/mcp-discord.html)、[Media MCP](guide/media-mcp.html) |
| 生產環境部署者 | [部署](guide/deployment.html)、[macOS MCP 網路](guide/macos-mcp-networking.html)、[疑難排解](guide/troubleshooting.html) |
| Release 維護者 | [Release Runbook](guide/release.html)、[貢獻者指南](guide/contributing.html)、[文件維護](guide/docs-maintenance.html) |
| Security reviewer | [安全模型](guide/security-model.html)、[Audit、用量與隱私](guide/audit-usage-privacy.html)、[環境變數參考](guide/environment.html) |
