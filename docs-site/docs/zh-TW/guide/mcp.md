# MCP 權限

MCP 工具讓 agent 能觸及核心 ACP session 以外的系統：Discord API、媒體生成、內部服務、搜尋工具或專案自訂自動化。

## Catalog 與 Channel Policy

bot 會把 discovery 與 permission 分開：

1. MCP server 定義先從 Kiro-format MCP settings source 載入 catalog。
2. Discord 頻道管理員用 `/mcp manage` 明確啟用 server 或指定工具。
3. bot 只把目前 channel/thread 允許的 server/tool set 注入 agent。
4. policy proxy 會過濾 `tools/list` 並阻擋未授權的 `tools/call`。

因此，把 server 加到 `~/.kiro/settings/mcp.json` 或其他 catalog source，不代表它會自動暴露給所有 Discord 頻道或所有 ACP engine。Kiro 與 OMP 都只會透過 bot 的 channel policy injection path 收到 MCP servers。

## 內建 Bot Tools

`bot-tools` 是由 bot binary 提供的內建 MCP server。它提供 bot-managed data 的安全查詢，以及受控的 egress 功能，例如送檔、cron 管理與 audit timeline 查詢。

新頻道初始化會啟用安全預設 allowlist。風險較高的 `bot_send_message`、`bot_delete_cron`、`bot_query_audit` 需要明確授權。

完整 tool list、預設值、scope rules 與 audit prompt 行為見 [Bot Tools MCP](bot-tools.md)。

## Discord MCP

`mcp-discord` 是可選 catalog server，可以讀訊息、列頻道、送訊息、開 thread 與執行其他 Discord REST 操作。廣泛啟用前，請先限制它的環境：

```bash
MCP_DISCORD_ALLOWED_GUILDS=123456789012345678
MCP_DISCORD_ALLOWED_CHANNELS=234567890123456789,345678901234567890
MCP_DISCORD_READ_ONLY=false
MCP_DISCORD_ALLOWED_WRITE_TOOLS=discord_send_message,discord_reply_message
MCP_DISCORD_ALLOW_DESTRUCTIVE=false
```

本機多 bot 測試時，請確認 catalog command 載入的是你預期 bot 身分的 `.env`。Discord 回 403 可能代表 MCP server 使用了和畫面上 bot 不同的 token。

完整 Discord MCP tools 與 policy guards 見 [Discord MCP](mcp-discord.md)。可選媒體生成工具見 [Media MCP](media-mcp.md)。

## 帶 Headers 的 URL/SSE Servers

URL-based MCP servers 同樣從 Kiro-format catalog 載入。如果 server 需要 HTTP headers，請在 catalog entry 裡定義：

```json
{
  "mcpServers": {
    "ga4": {
      "type": "sse",
      "url": "http://127.0.0.1:8766/sse",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

bot 只會在 runtime MCP proxy traffic 使用這些 headers。`/mcp manage` scan、channel/thread agent injection、streamable HTTP POST、SSE endpoint GET 與 SSE message POST 都會使用同一份 headers。bot 儲存 catalog record 時會遮蔽 header values，因此 authorization secrets 不會明文寫入 policy database。

如果 scan 只顯示 generic transport failure，請看 bot logs 的 upstream status。`401 Unauthorized` 或 `403 Forbidden` 通常代表 URL server 沒收到預期 header，或 token 沒有足夠權限。

## 操作檢查

- 用 `/mcp status` 查看 catalog 與目前 channel policy。
- 用 `/mcp manage` 掃描工具並調整 allowlist。
- 變更 policy 後，重啟或 reset active agents，讓下一次 session 收到最新工具集合。
- Discord 權限或 agent 狀態不明時，使用 `/doctor`。
