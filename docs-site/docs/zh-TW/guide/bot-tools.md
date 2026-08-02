# Bot Tools MCP

每個已初始化的頻道都會取得內建 `bot-tools` MCP server。它把 bot 原生能力提供給 active ACP agent，同時仍然套用目前 Discord channel、thread、MCP policy 與 safe egress 規則。

這個 server 與 MCP catalog 裡的外部 MCP server 是分開的。

## 預設工具權限

第一次完成頻道設定時，預設會啟用這些安全工具：

| Tool | 類型 | 用途 |
| --- | --- | --- |
| `bot_data_summary` | Read | 摘要 bot data directory，不包含訊息內容。 |
| `bot_list_channel_data` | Read | 列出已知 channel data folders 與 metadata 是否存在，不包含訊息內容。 |
| `bot_current_time` | Read | 回傳 bot 在 `CRON_TIMEZONE` 下的精確目前日期時間、星期、時段與本週範圍。 |
| `bot_resolve_date_range` | Read | 用結構化欄位解析 calendar range，避免 agent 自己心算日期。 |
| `bot_list_cron` | Read | 列出目前頻道的排程任務。 |
| `bot_send_file` | Write, non-destructive | 將 sanitized file upload 排入 Discord delivery queue。 |
| `bot_send_image_url` | Write, non-destructive | 從允許的 non-secret URL 抓取 JPEG/PNG 圖片並排入 Discord delivery queue。 |
| `bot_create_cron` | Write, non-destructive | 排入建立 scheduled task 的請求。 |
| `bot_update_cron` | Write, non-destructive, idempotent | 排入修改既有 recurring cron job 的 name、schedule、prompt 或 enabled state。 |
| `bot_create_reminder` | Write, non-destructive | 排入一次性提醒，交由 bot scheduler 到期發送。 |
| `bot_query_channel_history` | Read | 搜尋或分頁讀取目前 channel/thread context 的已儲存歷史。 |
| `bot_memory_list` | Read | 列出目前 parent channel 的 persistent memory rules。 |
| `bot_memory_add` | Write, non-destructive | 排入使用者明確要求、且已 audit-recorded 的 channel memory rule。 |
| `bot_skills_search` | Read | 搜尋 visible scoped skills，不暴露 raw bot data paths。 |
| `bot_skills_effective_list` | Read | 列出目前 channel/project scope 的 effective skills。 |
| `bot_skill_get` | Read | 通過 scope 與 required-tool checks 後讀取一個 visible skill。 |
| `bot_skills_server_search` | Read | 搜尋 visible server-wide skills。 |
| `bot_skills_server_get` | Read | 讀取一個 visible server-wide skill。 |
| `bot_skills_server_inventory` | Read | 在 authorized context 下摘要 server-wide skill inventory。 |
| `bot_skills_server_effective_for_channel` | Read | 顯示會套用到 channel 的 server skills。 |
| `bot_a2a_peers` | Read | 列出已知 A2A runtime peers、可呼叫 channel refs、visible skills、wakeability 與 delivery readiness。 |
| `bot_a2a_policy_get` | Read | 顯示目前綁定 channel 的 A2A policy 與 delivery readiness。 |
| `bot_a2a_task_status` | Read | 讀取 durable A2A TaskStore state 與 task 或 recent outbound tasks 的 event history。 |
| `bot_a2a_runtime_preflight` | Read | 檢查 guild-scoped A2A runtime cutover readiness，不套用 policy 或 service changes。 |
| `bot_a2a_policy_plan` | Read | 規劃 A2A policy change 並回傳 confirmation challenge；不套用變更。 |
| `bot_a2a_trust_peer` | Write, non-destructive | 對單一 peer runtime 規劃或套用 high-level trust grant；套用需 confirmation。 |
| `bot_a2a_delegate` | Write, non-destructive | 通過 outbound policy、quota 與 confirmation checks 後，排入 approved remote A2A task。 |
| `bot_a2a_policy_apply` | Write, non-destructive | 在 manager validation 與 fresh token 後套用 confirmed A2A policy diff。 |
| `bot_a2a_cancel` | Write, destructive | requester 或 channel manager 可取消 nonterminal A2A task。 |
| `bot_a2a_input_reply` | Write, non-destructive | 對 `TASK_STATE_INPUT_REQUIRED` 的 task 傳送使用者提供的 input。 |
| `bot_a2a_auth_reply` | Write, non-destructive | 對 `TASK_STATE_AUTH_REQUIRED` 的 task approve 或 deny，不攜帶 raw long-lived credentials。 |

這些工具存在，但預設不啟用：

| Tool | 類型 | 用途 |
| --- | --- | --- |
| `bot_send_message` | Write, non-destructive | 排入額外 Discord message。 |
| `bot_delete_cron` | Write, destructive | 排入刪除 scheduled task 的請求。 |
| `bot_query_audit` | Read, sensitive | 查詢 scoped audit timeline rows。 |
| `bot_memory_remove` | Write, destructive | 排入移除一筆已列出的 persistent memory rule。 |
| `bot_memory_clear` | Write, destructive | 排入清空目前 channel 所有 persistent memory rules。 |

| `bot_skill_usage_record` | Write, audit | 記錄 agent 使用了特定 skill ID/version。 |
| `bot_skill_create_draft` | Write, non-destructive | 對任何「建立技能」意圖建立已安裝但停用的 skill。Agent 必須自行研究 URL/Gist/repo/file，最後只提交整理後的乾淨 Markdown 與 source refs；建立後要等 manager 啟用才可使用。 |
| `bot_skill_preview_draft` / `bot_skill_install_draft` / `bot_skill_discard_draft` | Write/admin | 舊版 review-draft tools。Agent-only install/discard 會被拒絕；建議流程是 create-disabled，再由 manager enable。 |
| `bot_skills_channel_enable` / `bot_skills_channel_disable` / `bot_skills_channel_remove` / `bot_skills_channel_restore` / `bot_skills_channel_rollback` | Write/admin | 使用 channel management permission 管理 channel/project skill lifecycle。 |
| `bot_skills_server_disable` / `bot_skills_server_remove` / `bot_skills_server_restore` / `bot_skills_server_rollback` | Write/admin | 管理 server-wide skills；預設關閉且 server-management scoped。 |

`/audit <prompt>` 會暫時只授權 `bot_query_audit` 給私密 audit investigation agent。該 agent 不能使用一般 Discord egress tools。

## Scope Enforcement

`bot-tools` session 會綁定目前 channel 或 thread target。工具呼叫若嘗試操作其他 channel，會回傳 channel-scope error。

Recurring cron 管理在 runtime 中是 channel scope；thread ID 會依需要正規化成 parent channel。一次性 reminder 會保留目前 delivery target，因此在 task thread 中建立的提醒會回到該 thread。

Persistent memory 是 parent-channel scope。`bot_memory_add` 只在使用者明確說「記住」時預設可用，會拒絕看似 secret 的文字，透過 pending bot-side queue 寫入，並在 main bot 套用前先記錄 audit event。`bot_memory_remove` 與 `bot_memory_clear` 預設不啟用。

「10 分鐘後提醒我」、「明天 09:00 提醒某人」這類一次性提醒應使用 `bot_create_reminder`。每天、每週或週期性自動化才使用 `bot_create_cron`。

## A2A Bot Tools

`bot_a2a_*` tools 預設啟用，但每次呼叫仍綁定目前 Discord guild/channel context，且必須通過目前 A2A policy、manager permission、confirmation-token、quota 與 runtime-readiness checks。

宣稱另一個 bot 可以接工作前，先用 `bot_a2a_peers` 檢查。`trusted=true` 只代表 trust display state；direct same-thread collaboration 還需要相關 runtime policy 的 `deliveryReadiness.coPresentReady`。

使用 `bot_a2a_policy_plan` 或 `bot_a2a_trust_peer` 提出變更。套用 policy 需要 `bot_a2a_policy_apply`，或在 `bot_a2a_trust_peer` 帶入 fresh confirmation token；requester 也必須有 Manage Channels permission。

`bot_a2a_delegate` 只用於已核准的 outbound work。成功呼叫只代表 task 已 durable queued，不代表已 accepted 或 completed。回報成功前，使用 `bot_a2a_task_status` 搭配 `local_id`、`task_id` 或 `message_id` 查權威狀態。

Continuation 場景中，只有 task 在 `TASK_STATE_INPUT_REQUIRED` 時使用 `bot_a2a_input_reply`；只有 task 在 `TASK_STATE_AUTH_REQUIRED` 時使用 `bot_a2a_auth_reply`。一般操作不要直接檢查或編輯 `data/a2a/*.sqlite`。


## 時間脈絡工具

Agent prompt 會包含由 `CRON_TIMEZONE` 產生的 `[Current datetime]` 區塊。簡單的「今天」、「星期幾」、「現在上午還下午」應直接使用這個區塊。

需要計算區間時，agent 應把使用者的日期片語拆成 `bot_resolve_date_range` 的結構化欄位並呼叫工具，不要靠模型記憶自行計算星期或月週邊界。MCP 工具不解析任意自然語言日期文字；結構化 range 欄位不依賴 agent 回答語言，結果可重現。例如「下個月第二週」應拆成 `range_type=month_week`、`offset=1`、`week_index=2`。

因此 `CRON_TIMEZONE` 是 cron、reminder、prompt 注入目前時間與時間輔助工具共用的 bot business timezone。`USAGE_TIMEZONE` 只保留給 usage report aggregation。

## 安全 Discord Egress

`bot_send_message` 與 `bot_send_file` 不會在 MCP call 裡直接寫 Discord。它們會建立 safe egress action，再由 bot 透過正常 Discord path 投遞。

File egress 採保守設計：

- Plain text 會先 redaction 再上傳。
- JPEG 與 PNG 圖片會先驗證，再用 copied temporary file 上傳；其他工具回傳的 image URL 應直接交給 `bot_send_image_url`，不要讓 agent 複製 base64。
- `bot_send_image_url` 會由 bot server-side 抓取 non-secret HTTP(S) 圖片 URL，不做 host 或 path allowlist。URL path 不需要包含圖片檔名；Discord 顯示名稱由 `filename` 參數決定。bot 仍會拒絕 URL credentials，並在 queue delivery 前驗證抓回來的 bytes。
- PDF、DOCX、XLSX 會轉成 sanitized text 再上傳。
- `bot_send_file` 不會把原始 binary 文件傳回 Discord。
- Private audit job 會完全停用 message 與 file egress。

## Incoming Discord Attachments

傳入的 Discord 附件 path 一律會以 JSON-lines manifest 列在 agent prompt，包含 filename、MIME type、size，以及可取得時的圖片尺寸。為避免 context window overflow，bot 只會在小型視覺批次使用 ACP image blocks：最多 3 張圖片，且圖片總大小最多 1 MiB。更大的圖片批次會維持 path-only，讓 agent 依 user prompt 需要再讀取或批次處理檔案。

## Mention Resolution

當 `mcp-discord` catalog entry 存在時，default bot-tools setup 也會替該 server 開啟 `discord_resolve_mentions`。這個 resolver 可以把「Wendy」、「Cheisy」這類名字解析成目前 bot task 可用的 verified `[[discord:user:...]]` placeholders。它會先向 Discord refresh，再 fallback 到 cache，並更新本次任務的 dynamic mention refs；final delivery 仍由 bot 的 `AllowedMentions` guard 控制。Ambiguous 或 missing names 必須請使用者確認。

## Channel History Tool

`bot_query_channel_history` 是 read-only，且限制在目前 bot-tools channel 或 thread context。`target_id` 只能使用 context 中目前的 channel/thread ID：parent channel ID 會包含 child threads，thread ID 則只查該 thread。

`query` 是 optional。若要 broad/exhaustive review 已保留的歷史可省略；若要篩選，則提供 keyword/phrase 搜尋 stored message、bot response content 與 timeline metadata。結果是 paginated JSON page，包含 `limit`、`offset`、`returned`、`has_more`、`next_offset` 與 compact `results`；要整理完整歷史時，必須用 `offset=next_offset` 持續查到 `has_more=false`。

## Audit Query Tool

`bot_query_audit` 是 read-only 且限制在目前 bot-tools context。支援：

- `limit`，範圍 1 到 100。
- `event_type`，exact match。
- `contains`，搜尋 metadata 欄位。
- `target_id`，只能是已綁定 channel 或 thread context。
- `include_content`，針對 manager-authorized audit question 明確要求時，回傳已保留內容與 deleted-message snippets。

刪除事件 row 會在可取得時提供 `original_author_id`、`original_author_username` 與 `content_snippet` 來描述被刪訊息；批量刪除 row 會提供 `deleted_message_count` 與 `deleted_message_ids`。`deletion_note` 會說明歸屬限制。這個工具回傳 timeline rows，不提供任意 SQL access。
