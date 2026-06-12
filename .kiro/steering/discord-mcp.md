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

## Discord 輸出與分段規則

一般最終答案請直接回覆文字，讓 bot 的既有 delivery path 統一做 secret redaction、Discord Markdown 正規化、長訊息分段、code block fence 修補與分段 prefix。不要為了回覆使用者而主動呼叫 `bot_send_message` 或 `discord_send_message`。

當功能本身確實需要呼叫 Discord MCP / bot-tools 寫入工具時，實作層必須復用專案既有的 Discord formatter 與長訊息 helper：

- 分段與 Discord-friendly Markdown 包裝只能使用 `internal/discordfmt.Split`。
- 分段標示只能使用 `internal/discordfmt.WithPartPrefix`。
- 若在 `bot` 或 `channel` package 內已有長訊息送出 helper，優先復用既有 helper。
- 不得在 tool handler、adapter、worker 或 feature code 中自行手寫 split point、UTF-8 boundary、code fence reopen/close、heading 降級或分段 prefix。
- 若既有 helper 不足，先擴充共用 helper 並補測試，再接到功能路徑。

這條規則同時適用於 `bot_send_message`、`bot_send_file` 的附加文字、`discord_send_message`、`discord_reply_message`、`discord_send_file` 的附加文字、`discord_send_embed` description、cron/thread/final response，以及任何未來新增的 Discord 文字輸出。

## MCP 安全邊界

Discord MCP server 可能透過 `.env` 設定 `MCP_DISCORD_ALLOWED_GUILDS`、`MCP_DISCORD_ALLOWED_CHANNELS` 和 `MCP_DISCORD_DOWNLOAD_DIR`。如果工具回傳 allowlist 或下載目錄限制錯誤，代表目前環境刻意限制了可操作範圍；不要嘗試改用其他 guild/channel ID 繞過限制。

環境也可能設定 `MCP_DISCORD_READ_ONLY`、`MCP_DISCORD_ALLOWED_WRITE_TOOLS` 或 `MCP_DISCORD_ALLOW_DESTRUCTIVE=false` 來限制寫入工具。若寫入工具被拒絕，直接向使用者說明目前 MCP policy 不允許該操作，不要改用其他工具繞過。

`discord_download_attachment` 只應用於 Discord attachment/CDN URL；下載到本機後，回報實際儲存路徑即可。

## 資安稽核與不可繞過規則

MCP / safe egress 的 UX 修正不得降低安全邊界。任何 Discord 寫入或檔案輸出都必須維持以下控制：

- 先套用 guild/channel allowlist 與 read-only/write/destructive guard，再執行送出。
- 經過 secret redaction 後才送到 Discord。
- 禁止任意 mention 擴散：沿用既有 `AllowedMentions` suppression 或既有送出 helper。
- 保留 delivery success/failure handling 與 audit / semantic event 記錄；不能因為分批送出而吞掉錯誤。
- 檔案輸出只能走既有 sanitized copy / size / binary guard；不得直接上傳敏感原始檔。
- 不能為了繞過工具限制而改用裸 Discord REST call、直接修改 policy DB、改寫 `.env`、臨時開 allow-all，或把 MCP proxy / safe egress pending queue 拿掉。

若需要新增工具或送出模式，先明確判定它是 read-only、write 或 destructive，補上 policy/audit/redaction 測試，再交付。
