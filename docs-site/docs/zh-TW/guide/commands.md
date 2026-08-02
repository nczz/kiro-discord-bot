# 指令參考

指令主要以 slash command 使用，大多也支援 `!` 文字指令。敏感管理與帳務用量介面會在 Discord 支援時使用 ephemeral private response。

## 頻道設定與狀態

| Command | 用途 |
| --- | --- |
| `/cwd` | 開啟私密 project/CWD setup panel。用於初始化或切換頻道專案。 |
| `/start <cwd>` | 進階直接綁定 CWD；一般建議用 `/cwd`。 |
| `/status` | 顯示 agent 狀態、queue、context、session ID 與 uptime。 |
| `/doctor` | 執行部署、權限與 ACP 診斷。 |
| `/reset` | 重啟目前 channel 或 thread agent。 |
| `/clear` | 清除目前目標的對話歷史。 |
| `/compact` | 在支援時要求 active engine 壓縮對話 context。 |

在 parent channel 使用 `/clear` 時，會清 active agent session，並清除 bot-local channel chat log，避免未來 session continuity 再使用。於 Discord thread 內使用 `/clear` 時，若 active thread agent 正在執行會同步清除該 agent session，並且一定會截斷 bot-local `thread-<id>` chat log、清除已保存的 ACP session metadata，避免 `/reset` 載回先前 agent session；即使目前沒有 active in-memory thread agent，本地 thread 清除仍會執行。Discord thread 中仍可見的訊息，後續仍可能透過 Discord API 被用來重建 context，因此不想保留的細節請直接刪除或編輯。Memory rules、flash memory、steering 與專案檔案仍會生效。

## 工作控制

| Command | 用途 |
| --- | --- |
| `/cancel` | 要求目前 ACP session 取消 active task。 |
| `/interrupt` | 先 soft cancel；若同一任務仍卡住，再中斷 process group。 |
| `/pause` | 將目前目標切成 mention-only。Parent channel 也會停用新任務 thread。 |
| `/back` | 恢復 full-listen 與 parent channel 新任務開 thread。 |
| `/thread [on|off]` | 查詢或設定未來 parent-channel task 是否建立 Discord thread。 |
| `/silent [on|off]` | 控制 compact 或詳細 tool output。 |

## Model 與 Agent Mode

| Command | 用途 |
| --- | --- |
| `/model` | 顯示目前 model。 |
| `/model <model-id>` | 切換目前頻道/討論串 agent 的 model。Kiro 可使用 fallback model list；OMP 會從 active ACP session 驗證 model ID。 |
| `/models` | 列出可用 models。Kiro 可 fallback 到 `kiro-cli`；OMP 的 model catalog 來自 ACP `session/new`，因此需要頻道/討論串 agent 已啟動。 |
| `/agent` | 列出目前頻道/討論串 agent 的可用 modes。 |
| `/agent <mode-id>` | 切換 agent mode，例如 planner/guide mode 或 active ACP session 回報的 OMP modes。 |
| `/engine` | 顯示目前的 agent 引擎（kiro/omp）與已啟用的引擎清單。 |
| `/engine <kiro_or_omp>` | 切換此頻道/討論串的引擎（僅限 `AGENT_ENGINES_ENABLED` 列出的引擎）。會在新引擎開全新 session，並重放最近的對話內容。 |

## Memory 與 Steering

| Command | 用途 |
| --- | --- |
| `/memory` | 新增、列出、移除或清除持久 memory rules。 |
| `/flashmemory` | 管理 session-scoped emphasis rules。 |
| `/steering <status|create|edit>` | 管理目前專案共用的 `AGENTS.md` guidance。 |

只要 memory rule 還在 `/memory list` 看得到，就會影響未來 turns。若要完全淘汰過期指引，移除後再執行 `/clear` 與 `/reset`。

Memory、flash memory、steering 與 session cleanup 的操作差異見 [日常工作流](daily-workflows.md)。

## MCP 與 Admin

| Command | 用途 |
| --- | --- |
| `/mcp status` | 顯示 catalog 與目前 channel policy。 |
| `/mcp enable` / `/mcp disable` | 在 channel scope 啟用或停用 server。 |
| `/mcp manage` | 開啟私密 MCP policy panel，掃描 tools 並管理 allowlist。 |
| `/audit [limit]` | 私密檢視目前 channel/thread 的 audit events。 |
| `/usage [user]` | 私密顯示全伺服器今日、本周、本月至今的 agent 用量；engine 有回傳 metering metadata 時會包含 credits 或 USD cost。一般成員預設只看自己；具備管理伺服器或系統管理員權限者可留空查看所有使用者或指定其他成員。 |
| `/usage-history [user] [period] [status] [source]` | 私密查詢全伺服器詳細用量紀錄。`period` 可選 `7d`、`30d`、`this-month`、`last-month`；`status` 可選 `all`、`success`、`failed`；`source` 可選 `all`、`message`、`command`、`cron`。一般成員可查詢自己；查詢其他成員需要管理伺服器或系統管理員權限。 |

Audit data 請使用 slash `/audit`；usage data 請使用 slash `/usage` 或 `/usage-history`。文字 `!audit` 不回傳 audit rows，文字 `!usage` 只會提示改用 slash，因為 Discord 無法讓這類文字回覆變成 private。

Audit rows、audit prompt investigations 與 usage attribution 的行為見 [Audit、用量與隱私](audit-usage-privacy.md)。

## Scoped Skills

使用 scoped skills 可把經 review 的 reusable procedures 存到 server、channel、project，或 channel/project scope。建議以自然語言請 agent 草擬與更新 skill；slash commands 是 fallback 與 admin shortcuts。Scope precedence、bot-tools 行為、audit 與 recovery 見 [Scoped Skills](scoped-skills.md)。

| Command | 用途 |
| --- | --- |
| `/skill list [query]` | Manager 會看到已安裝 skills，包含已停用項目；其他使用者只看到 effective skills。 |
| `/skill get skill_id:<id-or-slug>` | Manager 可在 scope 檢查後讀取已安裝 skill；其他使用者只能讀取 effective skills。 |
| `/skill create` | 用明確欄位建立 skill；預設已安裝但停用。 |
| `/skill disable` / `/skill enable` / `/skill restore` / `/skill rollback` | 管理 authorized scope 的 lifecycle state。 |
| `/skill history skill_id:<id-or-slug> [scope]` | 顯示近期 lifecycle audit history。 |

## A2A

只有在 NATS 已設定且 channel 有 A2A policy 後才使用 A2A commands。從 NATS server、`.env` 到 Discord policy 的完整 setup 見 [使用 NATS 啟用 A2A](a2a-nats-setup.md)。協議關鍵字見 [A2A 協議模型](a2a-protocol.md)。

| Command | 用途 |
| --- | --- |
| `/a2a peers` | 列出可見 runtime peers、skills、trust、stale/online status 與 delivery readiness。 |
| `/a2a trust peer_agent:<runtime>` | Plan 或 apply 高階 peer trust。預設為 bidirectional general-task trust，套用前需要 confirmation。 |
| `/a2a ask peer_agent:<runtime> message:<text>` | 對 trusted runtime 排入 general delegated task。 |
| `/a2a delegate target_agent:<runtime> skill_id:<skill> message:<text> reason:<reason>` | Policy 與 confirmation checks 通過後，對明確 runtime/skill target 排入 task。 |
| `/a2a status [task]` | 查看 durable task state 與 events。Queued task 不代表完成，必須到 terminal state。 |
| `/a2a cancel task:<task>` | 要求取消 remote A2A task。 |
| `/a2a reply task:<task> input:<text>` | 當 task 是 `TASK_STATE_INPUT_REQUIRED` 時提供 input。 |
| `/a2a authorize task:<task> approve:true_or_false` | 當 task 是 `TASK_STATE_AUTH_REQUIRED` 時 approve 或 deny。 |

`/a2a enable`、`/a2a disable`、`/a2a expose`、`/a2a accept-from`、`/a2a delegate-to` 等 policy subcommands 是進階介面。一般 bidirectional peer setup 優先使用 `/a2a trust`。


## 排程

| Command | 用途 |
| --- | --- |
| `/cron` | 透過表單建立 recurring scheduled task。 |
| `/cron-prompt <description>` | 用自然語言建立 scheduled task。 |
| `/cron-list` | 列出 scheduled tasks 與管理按鈕。 |
| `/cron-run <name>` | 手動執行 scheduled task。 |
| `/remind <time> <content>` | 建立一次性 reminder，到期時 tag requester。 |

排程指令必須在 parent channel 使用。Cron agent 執行時使用該頻道當下 CWD。

Scheduling scope、MCP-created jobs 與 owner expectations 見 [Cron 與提醒](cron-reminders.md)。

## Thread Helpers

在 Discord thread 內，`/status`、`/reset`、`/cancel`、`/interrupt`、`/compact`、`/clear`、`/model` 通常會作用在目前 thread agent。`!close` 可關閉目前 thread agent，`!close-thread <thread_id>` 可從 parent channel scope 關閉 inactive thread agent。
