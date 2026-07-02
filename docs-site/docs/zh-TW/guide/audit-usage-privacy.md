# Audit、用量與隱私

Audit 與 usage 回答的是不同問題：

- Audit 說明 Discord 與 bot 內發生了什麼。
- Usage 說明是哪個 Discord 使用者觸發了可計量的 agent 工作。

## Audit 儲存

Audit 預設啟用，SQLite 預設路徑是 `DATA_DIR/audit/discord.sqlite`，除非用 `AUDIT_LOG_DB` 覆寫。

Recorder 會記錄 Discord gateway 活動與 bot-side events，包含 message create、update、delete、reaction、channel/thread events、interaction、command reply、agent lifecycle、final response，以及 delivery success/failure metadata。

Typing event 只有在 `AUDIT_LOG_RECORD_TYPING=true` 時才會記錄。

## 訊息刪除歸屬

Discord gateway 的 `message_delete` event 能確認某則訊息消失，但不會指出刪除操作者。若 bot 先前已記錄原訊息，audit query 可以顯示原作者；在內容保留已啟用且查詢明確要求時，也能顯示短內容摘要。原作者不等於刪除者，也不能單獨證明是自刪。

管理員或 bot 刪除他人訊息時，可能會出現在 Discord Guild Audit Log 的 `MESSAGE_DELETE` 或 `MESSAGE_BULK_DELETE`，但那是另一個 Discord audit-log 資料來源，欄位有限且有保留期限。使用者自行刪除自己的訊息，通常無法只靠 Discord gateway event 證明。

## 內容紀錄

`AUDIT_LOG_RECORD_CONTENT=true` 會在 audit projection 與 raw payload 中記錄訊息內容。若部署環境不適合保留內容，請設為 `false`。

用 `AUDIT_LOG_RETENTION_DAYS` 控制舊資料清理。預設 `0` 表示全部保留。

## Audit 指令行為

`/audit` 僅支援 slash command：

- `/audit <limit>` 會私密回覆目前 channel 或 thread 的最近 audit rows。
- `/audit <prompt>` 會啟動私密 audit investigation agent，並要求它使用 `bot_query_audit`。
- `!audit` 不會回傳 audit rows，因為 Discord text command 無法保證私密回覆。

Audit 管理需要與敏感 channel controls 相同的 channel/admin 授權。

## Usage 歸屬

Usage record 是 agent work 完成後追加寫入的 ledger。若工作來自使用者 command、prompt、mention、audit prompt、compact、clear 或 scheduled command context，會歸屬到觸發的 Discord 使用者。

Cron job 會使用 job owner 或設定的 user context。Kiro usage 會加總 `credit` 或 `credits` metering metadata；OMP usage 會加總 `usage_update` 回傳的 USD cost metadata。

如果 engine 沒有對某個 turn 回傳 metering metadata，`/usage` 仍會計入該 turn，但會顯示缺少 metadata。這代表 turn 有發生，只是 bot 無法從缺失的 ACP metadata 推算 credits 或 cost。

## 彙總方式

`/usage` 會盡可能依 resolved Discord user ID 分組。若舊紀錄只有 username，只有在能明確對應時才會合併到 user row；有歧義的名稱會保留分開，避免誤歸屬。

報表時間窗：

- 今天：`USAGE_TIMEZONE` 的本地午夜起算。
- 本週：週一本地午夜起算。
- 本月：每月 1 日本地午夜起算。

用 `USAGE_RETENTION_MONTHS` 控制舊 monthly ledger files 清理。
