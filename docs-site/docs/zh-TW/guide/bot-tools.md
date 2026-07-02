# Bot Tools MCP

每個已初始化的頻道都會取得內建 `bot-tools` MCP server。它把 bot 原生能力提供給 active ACP agent，同時仍然套用目前 Discord channel、thread、MCP policy 與 safe egress 規則。

這個 server 與 MCP catalog 裡的外部 MCP server 是分開的。

## 預設工具權限

第一次完成頻道設定時，預設會啟用這些安全工具：

| Tool | 類型 | 用途 |
| --- | --- | --- |
| `bot_data_summary` | Read | 摘要 bot data directory，不包含訊息內容。 |
| `bot_list_channel_data` | Read | 列出已知 channel data folders 與 metadata 是否存在，不包含訊息內容。 |
| `bot_list_cron` | Read | 列出目前頻道的排程任務。 |
| `bot_send_file` | Write, non-destructive | 將 sanitized file upload 排入 Discord delivery queue。 |
| `bot_create_cron` | Write, non-destructive | 排入建立 scheduled task 的請求。 |

這些工具存在，但預設不啟用：

| Tool | 類型 | 用途 |
| --- | --- | --- |
| `bot_send_message` | Write, non-destructive | 排入額外 Discord message。 |
| `bot_delete_cron` | Write, destructive | 排入刪除 scheduled task 的請求。 |
| `bot_query_audit` | Read, sensitive | 查詢 scoped audit timeline rows。 |

`/audit <prompt>` 會暫時只授權 `bot_query_audit` 給私密 audit investigation agent。該 agent 不能使用一般 Discord egress tools。

## Scope Enforcement

`bot-tools` session 會綁定目前 channel 或 thread target。工具呼叫若嘗試操作其他 channel，會回傳 channel-scope error。

Cron 管理在 runtime 中是 channel scope；thread ID 會依需要正規化成 parent channel。

## 安全 Discord Egress

`bot_send_message` 與 `bot_send_file` 不會在 MCP call 裡直接寫 Discord。它們會建立 safe egress action，再由 bot 透過正常 Discord path 投遞。

File egress 採保守設計：

- Plain text 會先 redaction 再上傳。
- PDF、DOCX、XLSX 會轉成 sanitized text 再上傳。
- `bot_send_file` 不會把原始 binary 文件傳回 Discord。
- Private audit job 會完全停用 message 與 file egress。

## Audit Query Tool

`bot_query_audit` 是 read-only 且限制在目前 bot-tools context。支援：

- `limit`，範圍 1 到 100。
- `event_type`，exact match。
- `contains`，搜尋 metadata 欄位。
- `target_id`，只能是已綁定 channel 或 thread context。
- `include_content`，針對 manager-authorized audit question 明確要求時，回傳已保留內容與 deleted-message snippets。

刪除事件 row 會在可取得時提供 `original_author_id`、`original_author_username` 與 `content_snippet` 來描述被刪訊息；批量刪除 row 會提供 `deleted_message_count` 與 `deleted_message_ids`。`deletion_note` 會說明歸屬限制。這個工具回傳 timeline rows，不提供任意 SQL access。
