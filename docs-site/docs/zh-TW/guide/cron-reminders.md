# Cron 與提醒

排程是 channel-scoped automation。適合用在有明確 owner 與明確 channel audience 的週期性工作。

## 指令

| Command | 行為 |
| --- | --- |
| `/cron` | 開啟 recurring scheduled task 表單。 |
| `/cron-prompt <description>` | 用自然語言要求 bot 建立 recurring task。 |
| `/cron-list` | 列出排程與管理動作。 |
| `/cron-run <name>` | 立即執行指定排程。 |
| `/remind <time> <content> [agent]` | 建立一次性提醒。啟用 `agent` 時，到期會請 agent 工作。 |

Scheduling commands 是 channel-only commands。請在 parent channel 執行，不要在任務討論串中執行。

## 執行模型

Heartbeat loop 會檢查 pending schedules。Cron job 執行時使用 channel 當下的 CWD 與 channel policy。

Agent-based cron turns 會寫入 usage ledger 與 audit timeline。Cron job 使用 MCP tools 時，也會記錄 tool audit events。

## MCP 建立的排程

內建 `bot_create_cron`、`bot_list_cron`、`bot_delete_cron` tools 讓 agent 在 channel policy 允許時管理 cron jobs。

Create/delete 會先寫成 pending action，再透過 bot 正常 maintenance loop 生效。這讓 MCP tool call 與 slash command 使用同一份 channel-scoped cron store。

## 維運原則

Cron 很強大，因為它可以在沒有人即時觸發時啟動未來 agent work。請綁定明確 channel owner、定期檢查 `/cron-list`，並在只應讀資料的 channel 停用可寫 cron MCP tools。
