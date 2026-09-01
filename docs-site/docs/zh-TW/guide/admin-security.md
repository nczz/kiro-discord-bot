# 管理與安全

`kiro-discord-bot` 的能力很強：它可以綁定真實專案目錄、執行 agent tools、呼叫 MCP servers。請把部署與 channel policy 視為正式環境控制面。

## Discord 權限

基礎 bot 需要：

- View Channels
- Send Messages
- Add Reactions
- Read Message History
- 啟用 WebShare 時需要 Manage Webhooks
- Message Content Intent

MCP server 可能需要額外 Discord REST 存取。bot 的 channel policy 不會取代 Discord 權限；兩邊都要允許，操作才會成功。

完整 trust-boundary model 見 [安全模型](security-model.md)。

## 私密回應

管理面板與敏感查詢會在 Discord 支援時使用 ephemeral private response。包含 `/cwd`、`/status`、`/usage`、`/usage-history`、`/doctor`、`/audit`、`/models`、`/memory`、`/flashmemory`、`/mcp manage`、`/steering`、`/cron-list` 與 `/webshare`。

文字指令不一定能提供 Discord private response。Audit 資料請使用 slash `/audit`；usage data 請使用 slash `/usage` 或 `/usage-history`。文字 `!audit` 不會回傳 audit rows 或 prompt 調查報告，文字 `!usage` 只會提示改用 slash。

## CWD 邊界

用 `DEFAULT_CWD` 與 `ALLOWED_CWD_ROOTS` 把頻道 setup 限制在預期專案根目錄內。新頻道必須先初始化才能開始 agent 工作，setup 只會在 `DEFAULT_CWD` 下選擇或建立專案。

## WebShare 控制

WebShare 預設關閉，只有在 self-host relay 與 matching host token 都準備好後才應啟用。Control link 會在停止或撤銷前委派 opener 的 channel/thread authority，因此 managers 應把 active shares 視為短期 privileged sessions。Handoff 期間用 `/webshare status` 檢查，正常完成後用 `/webshare stop`，如果連結可能外洩則用 `/webshare revoke`。

## MCP 安全

採用最小權限：

- Catalog servers 預設保持停用。
- 只啟用該頻道實際需要的工具。
- 大範圍頻道優先使用 read-only 或 non-destructive MCP mode。
- 用 server-level environment guards 作為第二層防護。
- MCP server 升級後重新 scan tools。

## Audit

Audit events 會記錄 command 呼叫、command 回覆、agent lifecycle、final response，以及相關 delivery success/failure metadata。不適合永久保存時，請設定 retention。

Storage paths、content recording、`/audit` 行為與 usage attribution 見 [Audit、用量與隱私](audit-usage-privacy.md)。
