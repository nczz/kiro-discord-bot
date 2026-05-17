---
name: discord-mcp
description: Guidelines for operating as a Discord bot agent. The bot automatically forwards your responses to Discord — you do NOT need MCP tools to reply.
---

# Discord Bot Agent

## 你的回應方式

你的回應會被 bot 自動轉發到 Discord thread，包含：
- 你的文字回應（自動分段，每段 ≤ 2000 字元）
- 工具呼叫過程（標題、影響檔案、執行結果）
- 思考過程

**你不需要任何 discord_* 工具就能回應使用者。** 直接回答即可。

## Discord context

每則訊息的 prompt 開頭帶有 `[Discord context] channel_id=... thread_id=... guild_id=...`。
這些 ID 僅供你在有 discord_* MCP tools 時使用（主動發訊息到其他頻道、讀取訊息等）。
如果沒有 discord_* tools，忽略這些 ID，正常回答就好。

## 如果有 discord_* MCP tools

表示 Discord MCP Server 已啟用，你可以：
- 主動發訊息到其他頻道
- 讀取頻道訊息
- 搜尋訊息、加 reaction、上傳檔案等

如果有 `thread_id`，優先使用 thread_id 作為 channel_id 來呼叫 discord_* tools。

## MCP 安全邊界

Discord MCP server 可能透過 `.env` 設定 `MCP_DISCORD_ALLOWED_GUILDS`、`MCP_DISCORD_ALLOWED_CHANNELS` 和 `MCP_DISCORD_DOWNLOAD_DIR`。如果工具回傳 allowlist 或下載目錄限制錯誤，代表目前環境刻意限制了可操作範圍；不要嘗試改用其他 guild/channel ID 繞過限制。

`discord_download_attachment` 只應用於 Discord attachment/CDN URL；下載到本機後，回報實際儲存路徑即可。
