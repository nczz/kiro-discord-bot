# Cron 與提醒

排程是 channel-scoped automation。適合有明確 owner、也有明確 Discord audience 的工作。

## 我該用哪個指令？

| 需求 | 指令 |
| --- | --- |
| 讓 agent 重複執行，且每次都公開結果 | `/cron-prompt <description>` 或 `/cron` |
| 只在未來某個時間提醒一次 | `/remind <time> <content> [agent]` |
| 重複檢查，但只有條件成立才通知 | `/monitor-prompt <description>` |
| 查看或管理排程 | `/cron-list` 或 `/monitor-list` |
| 立刻跑一次 | `/cron-run <name>` 或 `/monitor-run <name>` |

Scheduling commands 請在 parent channel 執行，不要在任務 thread 裡執行。

## 背景監控

需要安靜的週期性檢查時，用 `/monitor-prompt`：

```text
/monitor-prompt 每 10 分鐘檢查 CI，main 失敗才通知
/monitor-prompt 平日 09:00 檢查庫存，低於 5 才通知
```

一個 monitor 有兩段提示：

- **檢查**：背景 agent 要檢查什麼。
- **通知條件**：什麼情況才允許公開通知 Discord。

條件不成立時，bot 只記錄 private history 並安排下次檢查。不會建立 thread、不會貼進度、不會送出 tool output，也不會顯示 agent final response。

條件成立時，bot 會建立或重用 monitor thread、在 parent channel 貼 thread link，並把可見的監控訊息送到該 thread。

使用 `/monitor-list` 可以暫停、恢復、立即執行、編輯或刪除監控。編輯表單接受常見時間說法，例如 `每 10 分鐘`、`平日 09:00`、`每天 09:00`，也接受 5-field cron expression。

## 週期性 Cron job

每次執行都應該公開輸出時，使用 cron job。一般情況優先用 `/cron-prompt`：

```text
/cron-prompt 平日 09:00 檢查伺服器健康狀態並貼一段摘要
```

已經知道精確排程欄位時，再用 `/cron`。

## 一次性提醒

只需要未來送一次時，使用 `/remind`：

```text
/remind 明天 09:00 交 release checklist
/remind 30 分鐘後 檢查部署狀態
```

啟用 `agent` 時，到期會請 agent 工作；否則 bot 只送出提醒文字。

## 進階：MCP 建立的排程

Agent 只有在 channel policy 允許對應 bot-tools 時才能管理排程。Monitor bot-tools 另外要求已驗證 requester 對目標頻道具備 Discord Manage Channels 權限：

- `bot_create_reminder`：一次性提醒，例如「10 分鐘後」或「明天 09:00」。
- `bot_create_cron`：每次都應公開貼結果的 recurring job。
- `bot_create_monitor`：只有 `notify_when` 命中才公開發訊的 recurring background check。

Create、update、delete 都會先寫成 pending action，再由 bot maintenance loop 套用到 slash commands 共用的 channel-scoped store。

靜默 monitor check 期間，bot-tools write actions 會被停用。Agent 可以讀取已授權內容，但不能排入 Discord egress、cron/reminder 變更、monitor 變更或 persistent memory write；bot 只會在解析 monitor JSON result 後自行決定是否通知。

## 維運原則

Cron job 與 monitor 都能在沒有人即時觸發時啟動未來 agent work。請綁定明確 channel owner、定期檢查 `/cron-list` 與 `/monitor-list`，並在只應讀資料的 channel 停用可寫 scheduler MCP tools。
