# 管理與安全

`kiro-discord-bot` 的能力很強：它可以綁定真實專案目錄、執行 agent tools、呼叫 MCP servers。請把部署與 channel policy 視為正式環境控制面。

## Discord 權限

基礎 bot 需要：

- View Channels
- Send Messages
- Add Reactions
- Read Message History
- Message Content Intent

MCP server 可能需要額外 Discord REST 存取。bot 的 channel policy 不會取代 Discord 權限；兩邊都要允許，操作才會成功。

## 私密回應

管理面板與敏感查詢會在 Discord 支援時使用 ephemeral private response。包含 `/cwd`、`/status`、`/usage`、`/doctor`、`/audit`、`/models`、`/memory`、`/flashmemory`、`/mcp manage`、`/steering`、`/cron-list`。

文字指令不一定能提供 Discord private response。Audit 資料請使用 slash `/audit`；文字 `!audit` 不會回傳 audit rows 或 prompt 調查報告。

## CWD 邊界

用 `DEFAULT_CWD` 與 `ALLOWED_CWD_ROOTS` 把頻道 setup 限制在預期專案根目錄內。新頻道必須先初始化才能開始 agent 工作，setup 只會在 `DEFAULT_CWD` 下選擇或建立專案。

## MCP 安全

採用最小權限：

- Catalog servers 預設保持停用。
- 只啟用該頻道實際需要的工具。
- 大範圍頻道優先使用 read-only 或 non-destructive MCP mode。
- 用 server-level environment guards 作為第二層防護。
- MCP server 升級後重新 scan tools。

## Audit

Audit events 會記錄 command 呼叫、command 回覆、agent lifecycle、final response，以及相關 delivery success/failure metadata。不適合永久保存時，請設定 retention。
