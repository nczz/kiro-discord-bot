# Discord MCP Server

`mcp-discord` 是 release archive 內附的可選 MCP server。它讓 agent 可以直接使用 Discord REST 能力，例如讀訊息、列頻道、送訊息、建立 thread、下載附件與加 reaction。

它不是一般 bot 回覆的必要路徑。一般 agent final answer 應直接回傳給 bot，由 bot 統一處理 redaction、分段與送出。

不同於 `bot-tools`，`mcp-discord` direct write tools 不套用 bot safe-egress redaction 與 sanitization。`discord_send_message`、`discord_reply_message`、`discord_edit_message`、`discord_send_embed`、`discord_send_file` 仍會執行 Discord 長度處理與 mention 控制，但不會 redaction 疑似 secret 的文字、不會抽取文件內容，也不會把檔案改寫成 sanitized copy。MCP proxy policy 決定 tool 是否暴露；Discord token 與 API 權限決定可存取的 resources。若工作流程需要 bot-owned safe egress，請使用 `bot_send_file` 或 `bot_send_message`。

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

本機多 bot 開發時，請確認 `.env` 屬於目前受測的 bot 身分。如果 Discord 畫面顯示的是受測 bot，但 catalog command 載入另一個 bot 的 credentials，調整畫面上 bot 的 Discord 權限也無法修正 MCP `403 Missing Access`。

## Direct Discord MCP Guards

只設定仍由 `mcp-discord` 本機執行的 guard：

```env
MCP_DISCORD_DOWNLOAD_DIR=/tmp/kiro-discord-mcp
MCP_DISCORD_UPLOAD_DENY_PATHS=/srv/kiro-private/**
MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE=
```

`mcp-discord` 是純 Discord REST MCP server。它不再套用本地 guild/channel allowlist、read-only cap、write-tool cap、destructive cap 或 bot session target binding。Bot 管理的 session 由 `/mcp manage` 透過 MCP proxy 決定哪些 `discord_*` tools 暴露給 agent。實際 guild/channel 範圍與寫入成敗由 Discord bot token 與 Discord API 權限決定；Discord `403 Missing Access` 代表 token 無法存取或操作該資源。

Direct tools 仍保留本機內容與檔案 guard：Discord-friendly formatting 與 split、讓 raw mentions 失效並使用空 `AllowedMentions`、`discord_send_file` upload source denylist、以及 `discord_download_attachment` path-root/path-traversal 檢查。`mcp-discord` 不會讀取 bot target state，也不會授予 bot-task mention placeholders。

`discord_list_channels` 會回傳 Discord Get Guild Channels API 暴露的全部 guild channels，包括 category、text、voice、announcement、stage、forum 與 media channels。JSON output 會提供每個 channel 的穩定 `type`、數字 `type_id`、`parent_id` 與 `position`。設定 `include_active_threads=true` 可附加 active threads；不包含 archived threads。Discord 會省略 bot token 沒有 `VIEW_CHANNEL` 的 channels，因此此 tool 無法揭露該 credential 看不到的 channels。

`discord_list_members` 與 `discord_get_reactions` 會回傳 JSON pages，包含輸入的 `after` cursor 與 `next_after`；以 `after=next_after` 持續查詢，直到 `next_after` 為空。Limit 會依 Discord endpoint 邊界驗證：members 為 1–1000，reaction users 為 1–100。`discord_get_user` 直接執行 Get User，不再強制 guild；可選的 `guild_id` 或 `channel_id` 會附加 guild-member nickname、roles 與 join metadata。`discord_search_members` 是純 read-only guild member search，不會授予 mention capability。

Discord 的 guild member list 與 search endpoints 需要 privileged **Server Members Intent**。若要使用 `discord_list_members`、`discord_search_members` 或 `bot_resolve_mentions` member lookup，必須啟用此 intent。

## Audit 邊界

透過 bot 變更 MCP policy 時，仍會寫入 bot audit events。從 `discord_resolve_mentions` 升級到 `bot_resolve_mentions` 的 migration，會對每個變更的 policy 寫入 `source=migration` 的 `mcp_policy_updated` event。Normal thread 與 inline worker paths 觀察到的 tool calls 仍會記為通用 `agent_tool_call` 與 `agent_tool_result` events。只有 thread 建立失敗時使用的 synchronous fallback，目前不會寫入逐次 tool-call audit events。

獨立執行的 `mcp-discord` process 不會寫入 bot audit database，也不會收到已驗證的 Discord requester actor。因此，從 bot-managed agent job 以外的 MCP client 發出的呼叫，不會有 bot audit row。Discord 自己的 Guild Audit Log 可能涵蓋部分 moderation 或 configuration mutations，但它是獨立且不完整的資料來源；讀取與一般送訊息也不構成完整的 management audit trail。


## 依頻道啟用

註冊只會把 `mcp-discord` 加進 catalog，不會啟用任何 direct Discord tools。Default bot-tools setup 會在 `bot-tools` 內啟用獨立的 bot-owned `bot_resolve_mentions`，不會啟用或綁定 `mcp-discord`。

升級時，若 stored policy 曾啟用已移除的 `discord_resolve_mentions`，migration 會刪除 stale direct-tool grant；已啟用的 safe-write `bot-tools` policy 會加入 `bot_resolve_mentions`。其他 `mcp-discord` grants 會保留。

若要額外啟用其他 tools，請在 Discord 中：

1. 執行 `/mcp status` 確認 `mcp-discord` 出現。
2. 執行 `/mcp manage`。
3. 掃描 server。
4. 只啟用該頻道需要的額外 tools。
5. 讓 bot 重啟 active agents，使新 policy 在下一個 session 注入。

## 常見工具群組

| 群組 | 範例 | 風險 |
| --- | --- | --- |
| Read | `discord_list_channels`、`discord_list_members`、`discord_search_members`、`discord_get_user`、`discord_read_messages`、`discord_search_messages`、`discord_channel_info`、`discord_get_reactions`、`discord_list_roles` | 會讓 agent 看見 guild structure、members、roles、reaction users 或 channel content |
| Write | `discord_send_message`, `discord_reply_message`, `discord_send_embed` | 送出可見 Discord 訊息，不套用 bot safe-egress redaction |
| Thread | `discord_create_thread`, `discord_list_threads` | 建立或檢視 conversation surfaces |
| Management | `discord_edit_message`, `discord_pin_message`, `discord_edit_channel_topic` | 維運風險較高 |
| Attachment | `discord_send_file`, `discord_download_attachment` | `discord_send_file` 直接上傳指定本機檔案 bytes；下載附件需要 download directory 控制 |

建議先開 read-only，再只對工作流程需要的地方加入 non-destructive write tools。

只有在目標是原檔傳輸時才使用 `discord_send_file`。它會在 Discord size limit、MCP proxy tool exposure、Discord token/API permissions 與 upload source denylist 下，上傳 `file_path` 指定的檔案；不會把 PDF/DOCX/XLSX 轉文字、不會因有效 binary 無法 redaction 就拒絕，也不會 redaction 文字檔內容。需要 sanitized bot-owned delivery 時，請使用 `bot_send_file`。

`discord_send_file` 會在開檔前執行 source-path denylist。預設會封鎖 bot data directory、Kiro runtime/config roots、OMP session/config roots、mcp-discord executable directory，以及偵測到的 bot deployment directories。`MCP_DISCORD_UPLOAD_DENY_PATHS` 會追加 comma 或 newline 分隔的 wildcard patterns；環境變數與 `~` 展開後支援 `*`、`?`、`**`。檢查會同時比對 requested absolute path 與 symlink-resolved real path；拒絕時不回傳本機路徑。只有在 case-sensitive 部署檔案系統需要精準大小寫比對時，才設定 `MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE=false`。

請讓 `DEFAULT_CWD`、user-content workspaces 與 `MCP_DISCORD_DOWNLOAD_DIR` 位於 bot data directory 之外。把使用者可傳輸檔案混進 bot-owned runtime state 不是最佳實踐：它會提高後續 file-transfer workflow 指到敏感 bot state、或被迫替 upload guard 開例外的風險。預設 denylist 會把 bot data directory 底下的檔案視為最後防線而阻擋，因此後續用 `discord_send_file` direct re-upload 會失敗；需要 sanitized bot-owned delivery 時請使用 `bot_send_file`。

一般 bot reply 的具名 mention resolution 屬於 `bot_resolve_mentions`，不屬於 `mcp-discord`。這個 bot-owned tool 會逐一執行 fresh REST search，再於每次呼叫執行一次 bounded guild-member scan，以交叉檢查 exact 或 unique matches；它會把 verified refs 寫入目前 task state，再回傳 final delivery 可用的安全 placeholders。`discord_search_members` 只供管理查詢；不可把它回傳的 user IDs 當作 bot mention authorization。
