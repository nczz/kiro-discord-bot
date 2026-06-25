# Discord MCP Server

`mcp-discord` 是 release archive 內附的可選 MCP server。它讓 agent 可以直接使用 Discord REST 能力，例如讀訊息、列頻道、送訊息、建立 thread、下載附件與加 reaction。

它不是一般 bot 回覆的必要路徑。一般 agent final answer 應直接回傳給 bot，由 bot 統一處理 redaction、分段與送出。

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
MCP_DISCORD_ALLOWED_WRITE_TOOLS=discord_send_message,discord_reply_message
MCP_DISCORD_ALLOW_DESTRUCTIVE=false
```

空 allowlist 會保留舊版 unrestricted 行為。正式環境若 bot 有廣泛 Discord 權限，建議明確設定 guild/channel allowlist。

## 依頻道啟用

註冊只會把 server 加進 catalog。在 Discord 中：

1. 執行 `/mcp status` 確認 `mcp-discord` 出現。
2. 執行 `/mcp manage`。
3. 掃描 server。
4. 只啟用該頻道需要的 tools。
5. 讓 bot 重啟 active agents，使新 policy 在下一個 session 注入。

## 常見工具群組

| 群組 | 範例 | 風險 |
| --- | --- | --- |
| Read | `discord_read_messages`, `discord_search_messages`, `discord_channel_info` | 會讓 agent 看見 channel content |
| Write | `discord_send_message`, `discord_reply_message`, `discord_send_embed` | 會送出可見 Discord 訊息 |
| Thread | `discord_create_thread`, `discord_list_threads` | 建立或檢視 conversation surfaces |
| Management | `discord_edit_message`, `discord_pin_message`, `discord_edit_channel_topic` | 維運風險較高 |
| Attachment | `discord_download_attachment` | 需要 download directory 控制 |

建議先開 read-only，再只對工作流程需要的地方加入 non-destructive write tools。
