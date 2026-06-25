# 疑難排解

## MCP Discord 回 403

先分清兩條常見失敗路徑：

- `bot-tools` 可能因為目前 session 不允許目標 channel，而設計性拒絕跨頻道送訊息。
- `mcp-discord` 回 Discord `403 Missing Access`，代表 MCP server 使用的 Discord token 看不到或不能操作目標頻道。

本機多 bot 設定時，請確認 MCP catalog command source 的 `.env` 是預期 bot 身分。如果畫面上是 M5Bot，但 MCP server 載入 ChunBot token，替 M5Bot 開頻道權限也不會解決 403。

## MCP Scan 顯示 No Route to Host

如果 private LAN MCP URL 在互動 shell 可連，但 macOS launchd 下 `/mcp manage` scan 失敗，請先修 host/service networking environment。檢查 proxy settings、`NO_PROXY`、route selection 與 macOS Local Network permission。Relay 只應作為明確部署 fallback。

## Bot 沒有回應

檢查：

- 頻道是否已用 `/cwd` 初始化。
- bot 是否能 view/send 目標 channel。
- channel 是否在 mention-only mode，且你是否使用真正 Discord mention。
- multi-bot mode 是否自動把 channel 切成 mention-only。
- `/doctor` 是否顯示 Discord 權限與 ACP preflight 健康。

## Thread Reset 顯示 No Thread Agent

Thread 可能有對話紀錄，但沒有 active in-memory thread agent。Idle cleanup、archive event 或 bot restart 都可能移除 agent process。必要時在 thread 內用新訊息重新建立脈絡，或回 parent channel 開新任務。

## 移除 Memory 後仍影響回覆

如果已移除的 memory rule 仍像是在影響 agent，代表目前 Kiro session 可能已經包含先前注入該 rule 的對話。請先移除規則，再執行 `/clear` 與 `/reset`。
