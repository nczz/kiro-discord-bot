# Discord MCP Server

`mcp-discord` 是 release archive 內附的可選 MCP server。它讓 agent 可以直接使用 Discord REST 能力，例如讀訊息、列頻道、送訊息、建立 thread、下載附件與加 reaction。

它不是一般 bot 回覆的必要路徑。一般 agent final answer 應直接回傳給 bot，由 bot 統一處理 redaction、分段與送出。

不同於 `bot-tools`，`mcp-discord` direct write tools 不套用 bot safe-egress redaction 與 sanitization。`discord_send_message`、`discord_reply_message`、`discord_edit_message`、`discord_send_embed`、`discord_send_file` 仍會執行 MCP guild/channel/write/destructive policy、Discord 長度處理與 mention 控制，但不會 redaction 疑似 secret 的文字、不會抽取文件內容，也不會把檔案改寫成 sanitized copy。若工作流程需要 bot-owned safe egress，請使用 `bot_send_file` 或 `bot_send_message`。

## 建置或找到 Binary

Release archive 會包含 `mcp-discord`。從原始碼可建置：

```bash
go build -o mcp-discord-server ./cmd/mcp-discord
```

Catalog command 中請一致使用同一個 binary 名稱。部署環境常見做法是把 `mcp-discord-server` 放在 main bot binary 旁邊。

## 安裝 Steering Guidance

repo 內有 `.kiro/steering/discord-mcp.md`。只有當你希望 bot 外的 Kiro sessions 也理解 Discord MCP tools 時，才需要安裝到全域：

```bash
mkdir -p ~/.kiro/steering
cp .kiro/steering/discord-mcp.md ~/.kiro/steering/discord-mcp.md
```

bot-managed runtime session 的 MCP 可見性仍由 channel policy 控制。

## 註冊到 MCP Catalog

在 `~/.kiro/settings/mcp.json` 或 `KIRO_MCP_CONFIG` 指向的檔案加入：

```json
{
  "mcpServers": {
    "mcp-discord": {
      "command": "sh",
      "args": [
        "-c",
        "set -a && . /absolute/path/to/.env && exec /absolute/path/to/mcp-discord-server"
      ],
      "env": {}
    }
  }
}
```

本機多 bot 開發時，請確認 `.env` 是你正在測試的 bot 身分。如果畫面上是 M5Bot，但 catalog command 載入 ChunBot token，替 M5Bot 開 Discord 權限也無法修正 MCP `403 Missing Access`。

## 加上 Defense-in-depth Guards

在載入的 `.env` 或 catalog environment 設定：

```env
MCP_DISCORD_ALLOWED_GUILDS=123456789012345678
MCP_DISCORD_ALLOWED_CHANNELS=234567890123456789,345678901234567890
MCP_DISCORD_DOWNLOAD_DIR=/tmp/kiro-discord-mcp
MCP_DISCORD_READ_ONLY=false
MCP_DISCORD_ALLOWED_WRITE_TOOLS=discord_send_message,discord_reply_message,discord_resolve_mentions
MCP_DISCORD_ALLOW_DESTRUCTIVE=false
MCP_DISCORD_MEMBER_SCAN_LIMIT=5000
MCP_DISCORD_UPLOAD_DENY_PATHS=/srv/kiro-private/**
MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE=
```

空 allowlist 會保留舊版 unrestricted 行為。正式環境若 bot 有廣泛 Discord 權限，建議明確設定 guild/channel allowlist。

Standalone `mcp-discord` process 不會走 bot 的 channel-policy injection path；除非必要，請設定 `MCP_DISCORD_READ_ONLY=true`。若必須開寫入，請維持 `MCP_DISCORD_ALLOW_DESTRUCTIVE=false`，並只在 `MCP_DISCORD_ALLOWED_WRITE_TOOLS` 列出非破壞性 tools。Bot 管理的頻道 session 會從 channel policy 自動注入這些 guards。

## 依頻道啟用

註冊會把 server 加進 catalog。當這個 catalog entry 存在時，bot 的 default bot-tools setup 會自動只替 `mcp-discord` 開啟 `discord_resolve_mentions`，行為對齊預設開啟的 bot-native image URL egress，但其他 Discord REST tools 仍維持關閉。

若要額外啟用其他 tools，請在 Discord 中：

1. 執行 `/mcp status` 確認 `mcp-discord` 出現。
2. 執行 `/mcp manage`。
3. 掃描 server。
4. 只啟用該頻道需要的額外 tools。
5. 讓 bot 重啟 active agents，使新 policy 在下一個 session 注入。

## 常見工具群組

| 群組 | 範例 | 風險 |
| --- | --- | --- |
| Read | `discord_read_messages`, `discord_search_messages`, `discord_channel_info` | 會讓 agent 看見 channel content |
| Mention resolver | `discord_resolve_mentions` | 用 fresh Discord member lookup 解析使用者要求的名字，只把 exact/unique match 授權給目前 bot task，並回傳安全的 `[[discord:user:...]]` placeholder |
| Write | `discord_send_message`, `discord_reply_message`, `discord_send_embed` | 送出可見 Discord 訊息，不套用 bot safe-egress redaction |
| Thread | `discord_create_thread`, `discord_list_threads` | 建立或檢視 conversation surfaces |
| Management | `discord_edit_message`, `discord_pin_message`, `discord_edit_channel_topic` | 維運風險較高 |
| Attachment | `discord_send_file`, `discord_download_attachment` | `discord_send_file` 直接上傳指定本機檔案 bytes；下載附件需要 download directory 控制 |

建議先開 read-only，再只對工作流程需要的地方加入 non-destructive write tools。

只有在目標是原檔傳輸時才使用 `discord_send_file`。它會在 Discord size limit、MCP write policy 與 upload source denylist 下，上傳 `file_path` 指定的檔案；不會把 PDF/DOCX/XLSX 轉文字、不會因有效 binary 無法 redaction 就拒絕，也不會 redaction 文字檔內容。需要 sanitized bot-owned delivery 時，請使用 `bot_send_file`。

`discord_send_file` 會在開檔前執行 source-path denylist。預設會封鎖 bot data directory、Kiro runtime/config roots、OMP session/config roots、mcp-discord executable directory，以及偵測到的 bot deployment directories。`MCP_DISCORD_UPLOAD_DENY_PATHS` 會追加 comma 或 newline 分隔的 wildcard patterns；環境變數與 `~` 展開後支援 `*`、`?`、`**`。檢查會同時比對 requested absolute path 與 symlink-resolved real path；拒絕時不回傳本機路徑。只有在 case-sensitive 部署檔案系統需要精準大小寫比對時，才設定 `MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE=false`。

請讓 `DEFAULT_CWD`、user-content workspaces 與 `MCP_DISCORD_DOWNLOAD_DIR` 位於 bot data directory 之外。把使用者可傳輸檔案混進 bot-owned runtime state 不是最佳實踐：它會提高後續 file-transfer workflow 指到敏感 bot state、或被迫替 upload guard 開例外的風險。預設 denylist 會把 bot data directory 底下的檔案視為最後防線而阻擋，因此後續用 `discord_send_file` direct re-upload 會失敗；需要 sanitized bot-owned delivery 時請使用 `bot_send_file`。

當使用者要求 tag 或通知某個名字，但該人沒有出現在目前 prompt 的 mention references 時，優先使用 `discord_resolve_mentions`，不要用 `discord_list_members` 後自行猜 ID。它會先做 fresh REST member search，再做 bounded scan / cache fallback，並把解析出的 refs 寫入目前 bot target state，回傳 agent 可在 final answer 使用的 placeholders。Ambiguous 或 missing names 必須請使用者確認，不可猜。只有在 guild 很大且 exact name 常被預設 scan limit 漏掉時，才調高 `MCP_DISCORD_MEMBER_SCAN_LIMIT`。
