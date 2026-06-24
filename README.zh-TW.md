# kiro-discord-bot

[English README](README.md)

**一個住在 Discord 裡的可訓練 AI agent — 綁定你的 codebase、記住你的規矩、越用越強。**

### 這不是聊天機器人

一般 AI bot 每次對話都從零開始。kiro-discord-bot 不同：

🧠 **會記住** — 永久記憶規則讓 agent 記住你的偏好、coding style、專案規範，跨 session 永久生效。

⚡ **能聚焦** — 閃存記憶讓你針對當前任務設定重點強調，用完即丟不污染未來 session。

📂 **懂你的 code** — 每個頻道綁定一個專案目錄，agent 能讀寫程式碼、跑測試、操作基礎設施。

📐 **注入可重複 context** — Steering 文件（`.kiro/steering/*.md`）可把專案背景、協作偏好、重複流程、安全限制、build 指令與架構規則注入 agent。

🔧 **能擴充** — MCP 插件擴展 agent 能力：Discord 操作、圖片/影片生成、任何 API。

⏰ **會自己做事** — Cron 排程讓 agent 定時巡檢伺服器、跑報告、自動化維運。

📈 **越用越強** — Memory + Steering + 對話歷史 + MCP 工具持續累積。第一天它能幫忙，第三十天它是你的隊友。

### 養成你的 Agent

```
Day 1  — 綁定專案，agent 開始認識你的 codebase
         !start /home/user/my-project

Day 3  — 教它你的規矩
         !memory add 永遠用繁體中文回答
         !memory add commit message 一律用英文，遵循 conventional commits

Day 7  — 加入 agent context，處理重複協作資訊
         .kiro/steering/project.md → 工作流程、參考資料、安全注意事項、build 指令

Day 14 — 設定自動化排程
         /cron → 每天 9 點檢查伺服器健康狀態，跟昨天比較

Day 30 — 擴充能力
         Discord MCP → agent 能主動讀訊息、發通知、跨頻道協作
         Media MCP → agent 能生成圖片、影片、音樂、語音
```

### 部署

#### 前置需求

- Go 1.21+
- 已安裝 [kiro-cli](https://cli.kiro.dev/install) 1.29+
- kiro-cli 驗證方式（擇一）：
  - `kiro-cli login`（互動式，開啟瀏覽器）
  - `KIRO_API_KEY` 環境變數（headless / 伺服器部署）
- Discord bot token，需具備：
  - Scopes：`bot`、`applications.commands`
  - 權限：查看頻道、發送訊息、新增反應、讀取訊息歷史
  - Privileged Intents：啟用 **Message Content Intent**

> ⚠️ **重要：** 請確認 Discord Developer Portal → General Information 中的 **Interactions Endpoint URL** 欄位為**空白**。若該欄位有設定 URL，Discord 會將 slash command 的 interaction 送往該 URL 而非 bot 的 gateway 連線，導致所有 `/` 指令出現「該應用程式未及時回應」錯誤。

### 快速開始

```bash
# 1. 安裝 kiro-cli
curl -fsSL https://cli.kiro.dev/install | bash

# 驗證方式擇一：
kiro-cli login                    # 互動式（開瀏覽器）
# 或在 .env 中設定 KIRO_API_KEY   # headless（伺服器推薦）

# 2. 設定環境變數
cp .env.example .env
# 編輯 .env，填入 DISCORD_TOKEN、DISCORD_GUILD_ID、KIRO_CLI_PATH 等

# 3. 編譯
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
go build -ldflags "-X main.Version=$VERSION" -o kiro-discord-bot .

# 4. 啟動（擇一）
# systemd（推薦）：
sudo cp kiro-discord-bot.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now kiro-discord-bot

# 手動：
export $(grep -v '^#' .env | xargs)
./kiro-discord-bot
```

既有服務重啟前，先跑 release preflight：

```bash
scripts/release-preflight.sh
```

若要包含本機已登入的 ACP smoke test：

```bash
RUN_ACP_SMOKE=1 KIRO_CLI=/Users/chun/.local/bin/kiro-cli scripts/release-preflight.sh
```

完整升版與部署檢查表見 `docs/release.md`。

Docker Compose 使用 `network_mode: host`，掛載 `~/.kiro` 供 Kiro 驗證與 MCP catalog discovery，並將 `${PROJECTS_DIR:-/projects}` 掛載為可寫入的專案根目錄。Agent session 仍使用 bot 管理的隔離 runtime（`DATA_DIR/kiro-agent-runtime`），不會直接繼承 Kiro MCP 設定；catalog 內的 MCP server 仍必須由頻道管理員透過 `/mcp` 依頻道啟用。

Compose 會針對容器環境固定部分預設值：`DEFAULT_CWD=/projects`、`DATA_DIR=/data`、`ALLOWED_CWD_ROOTS=/projects`，以及未覆寫時的 `ASK_TIMEOUT_SEC=300`。它有帶入核心 bot、Discord MCP 與 STT 變數，但不是每個選用的 retention、audit、catalog 變數都會自動傳入；若部署需要 `KIRO_MCP_CONFIG`、`USAGE_TIMEZONE`、`USAGE_RETENTION_MONTHS` 或 `AUDIT_LOG_*`，請自行加到 `docker-compose.yml` 的 environment。

### 指令說明

| 指令 | 說明 |
|------|------|
| `/help` | 顯示指令摘要 |
| `/start <目錄>` | 進階：綁定專案目錄並啟動 agent |
| `/reset` | 重置此 channel 目前的 agent session |
| `/status` | 查詢 agent 狀態、queue 長度、context 使用率、session ID、bot/agent uptime |
| `/usage [user]` | 查詢今天、本周、本月至今 credits 用量 |
| `/doctor` | 執行部署診斷與 ACP preflight |
| `/audit [limit]` | 私密查看目前頻道或討論串最近的 raw/semantic 稽核事件 |
| `/mcp manage` | 開啟互動式 MCP 權限面板，包含工具掃描與工具層級允許/移除控制 |
| `/mcp <action> [value]` | 查詢或更新此頻道的 MCP policy。Action：`status`、`enable`、`disable` |
| `/steering <status|create|edit>` | 管理目前頻道專案的 agent context 檔：`.kiro/steering/<project>.md` |
| `/cancel` | 取消目前執行中的任務 |
| `/interrupt` | 中斷卡住的目前任務；先執行取消，仍未結束才嘗試進程層中斷 |
| `/cwd` | 開啟 private 專案/CWD 面板；不用輸入完整路徑即可選擇或建立專案 |
| `/pause` | 切換頻道為 @mention 原頻道回覆模式 |
| `/back` | 恢復完整監聽並啟用新任務討論串 |
| `/thread [on|off]` | 查詢或設定新的頻道任務是否開啟 Discord 討論串 |
| `/silent` | 查詢安靜模式狀態（精簡工具輸出，預設：開啟） |
| `/silent on` | 開啟精簡工具輸出 |
| `/silent off` | 顯示完整工具細節 |
| `/model` | 查詢目前使用的 model |
| `/model <model-id>` | 切換 model 並重啟 agent |
| `/models` | 列出所有可用的 model |
| `/agent` | 列出可用的 agent mode |
| `/agent <mode-id>` | 切換 agent mode，例如 `kiro_default`、`kiro_planner`、`kiro_guide` |
| `/cron` | 新增排程任務（開啟表單） |
| `/cron-list` | 列出排程任務（含操作按鈕） |
| `/cron-run <name>` | 手動執行排程任務 |
| `/cron-prompt <description>` | 用自然語言建立排程任務 |
| `/remind <時間> <內容>` | 預約單次提醒（到期時 tag 你） |
| `/compact` | 壓縮對話歷史以釋放 context |
| `/clear` | 清除對話歷史 |
| `/close` | 僅限討論串：關閉目前討論串 agent |
| `/close-thread <thread_id>` | 關閉目前頻道範圍內的 inactive 討論串 agent |
| `/memory` | 管理永久記憶規則（add/list/remove/clear） |
| `/flashmemory` | 管理 session 閃存記憶（add/list/remove/clear） |

所有指令也支援 `!` 前綴（如 `!status`、`!reset`）。

在 Discord 討論串中使用指令時，會依最符合直覺的作用範圍執行：`/status`、`/reset`、`/cancel`、`/interrupt`、`/compact`、`/clear`、`/model` 會操作目前的討論串 agent。`/pause`、`/back`、`/silent` 會套用在目前目標，因此討論串可以覆蓋建立當下保存的監聽行為。`/thread` 永遠套用在父頻道未來新任務是否開討論串。`/memory` 與 `/flashmemory` 仍套用在父層頻道，因為討論串 agent 會繼承父層記憶。

**Memory、flash memory 與 steering：** `/memory` 是輕量的 Discord 原生規則層。只要規則出現在 `/memory list` 裡，就是目前啟用狀態；bot 會在每次 agent turn 前把它們注入 prompt 的 `[Memory Rules - always follow these]` 區塊。移除規則會停止未來注入，但目前 agent session 可能已經在先前 context 看過舊規則。若要完整排除過時或衝突的永久記憶，請先移除規則，再執行 `/clear` 與 `/reset`，讓 agent 對話、bot 端頻道歷史與已載入的 ACP session 都重新整理。`/flashmemory` 適合不需要永久保存的當前 session 重點。正式的專案背景、架構決策、coding convention 或需要 review/versioning 的規範，建議使用 `/steering` 與 `.kiro/steering/*.md` 管理。

```text
/memory action:list
/memory action:add value:<規則>
/memory action:remove value:<編號>
/memory action:clear

!memory list
!memory add <規則>
!memory remove <編號>
!memory clear

/flashmemory action:list
/flashmemory action:add value:<規則>
/flashmemory action:remove value:<編號>
/flashmemory action:clear

!flashmemory list
!flashmemory add <規則>
!flashmemory remove <編號>
!flashmemory clear
```

頻道設定與排程指令必須在父層頻道使用：`/start`、`/cwd`、`/steering`、`/agent`、`/cron`、`/cron-list`、`/cron-run`、`/cron-prompt`、`/remind`。

新的父層頻道必須先完成初始化才會啟動 agent。未初始化頻道中的第一則一般訊息會被暫停，並提示頻道管理員開啟 private `/cwd` 初始化面板。初次設定只能選擇或建立 `DEFAULT_CWD` 底下的專案；初始化面板會列出 `DEFAULT_CWD` 第一層目錄，並在碰到 Discord select-menu 上限時自動分頁。選擇專案後會先進入確認步驟，按下確認後才會變更頻道 CWD。建立新專案時也會自動建立 `.kiro/steering/`。初始化完成後，頻道會自動以安全預設工具清單啟用內建 `bot-tools` MCP，成功訊息會收斂 CWD 設定流程，只保留 private shortcuts 讓管理員檢視此頻道的 MCP 工具開放設定與建立 agent context 檔。會啟動 agent 或改變 agent 執行上下文的指令會在初始化前被拒絕，例如 `/start`、`/reset`、`/compact`、`/clear`、model/agent 切換、MCP policy 變更、agent context 變更、agent memory 變更、`/cron`、`/cron-run`、`/cron-prompt` 與 agent-backed reminder。完成初始化後，管理員仍可用 `/cwd` 作為進階操作，依一般 cwd allowlist policy 切換到其他允許路徑。

**討論串專用指令**（在 thread 中使用）：

| 指令 | 說明 |
|------|------|
| `!close` | 關閉討論串 agent |
| `!cancel` | 取消討論串 agent 目前的任務 |
| `!interrupt` | 中斷討論串 agent 卡住的目前任務 |
| `!reset` | 重啟討論串 agent |
| `!pause` | 切換討論串為 @mention 模式 |
| `!back` | 恢復討論串完整監聽模式 |
| `!thread [on\|off]` | 查詢或設定父頻道未來新任務是否開討論串 |
| `!silent` | 查詢討論串安靜模式狀態 |
| `!silent on` | 開啟此討論串的精簡工具輸出 |
| `!silent off` | 顯示此討論串的完整工具細節 |
| `!compact` | 壓縮討論串 agent 的對話歷史 |
| `!clear` | 清除討論串 agent 的對話歷史 |
| `!close-thread <thread_id>` | 關閉父頻道範圍內的 inactive 討論串 agent |
| `!model` | 查詢討論串 agent 目前的 model |
| `!model <model-id>` | 切換討論串 agent 的 model 並重啟 |
| `!models` | 列出所有可用的 model |
| `!audit [limit]` | 不支援輸出稽核資料；請改用私密 slash `/audit` |

所有討論串指令也支援 `/` slash command 形式。

### 架構

```
Discord 使用者
    │ 訊息 / slash command
    ▼
Discord Bot (Go)
    ├── 指令與訊息路由          權限、private 管理面板、audit events
    ├── Channel Manager         session、cwd、queue、worker、thread agent
    ├── Channel Setup           DEFAULT_CWD 專案選擇/建立 + steering 初始化
    ├── MCP Policy Catalog      Kiro MCP 設定只作為 catalog + 內建 bot-tools
    ├── Safe Egress             secret redaction + sanitized 訊息/檔案輸出
    ├── Heartbeat               health、cleanup、cron、reminder、閒置 agent 回收
    ├── Agent runtime prep
    │     ├── KIRO_HOME=DATA_DIR/kiro-agent-runtime
    │     ├── KIRO_MCP_CONFIG=runtime 空 settings/mcp.json
    │     ├── 同步 allowlist 內的非 MCP CLI settings
    │     └── agent config 會先做 MCP sanitization
    ├── MCP policy proxy
    │     ├── 過濾 tools/list
    │     └── 阻擋未授權 tools/call
    └── bot-tools MCP
          ├── 唯讀 bot metadata / scoped audit timeline
          ├── redacted Discord 訊息/檔案 egress
          └── pending cron create/delete actions
    ▼
kiro-cli acp                  每個 channel/thread/temp job 一個 process
          │
          ▼
Kiro model provider
```

### 注意事項

- Bot 需要在各 channel 的權限設定中明確授予讀寫權限
- Session ID 會存到 `DATA_DIR/sessions.json`；當 kiro-cli 宣告支援 `loadSession` 時，頻道與討論串 agent 重啟會優先用 `session/load` 接回既有 ACP session。Session key 會依 guild、bot 身分、目標類型與 channel/thread ID 分開；舊版 channel-only key 仍會作為遷移 fallback 讀取
- **頻道初始化**：新的父層頻道必須由具備 Discord 頻道管理權限的人透過 `/cwd` 初始化。初次選擇/建立專案限定在 `DEFAULT_CWD` 底下；建立新專案會自動建立 `.kiro/steering/`。初始化完成後可用 `/steering create` 或成功訊息 shortcut 先填寫可重複注入的背景、工作方式、常用資訊、限制與補充 context，再建立 `.kiro/steering/<project>.md`，預設使用 `inclusion: always`；空白選填欄位不會輸出成區塊。`/steering edit` 會用 private modal 做全文 Markdown 編輯，超過 Discord modal 限制時需直接在專案中修改。
- **MCP servers**：bot 只把 Kiro MCP 設定當作 catalog 讀取，並額外加入 `bot-tools` 這類內建 catalog entry。實際 agent runtime 使用隔離的 `KIRO_HOME=DATA_DIR/kiro-agent-runtime`，並把 `KIRO_MCP_CONFIG` 覆寫到 runtime home 內由 bot 維護的空 MCP config，再依目前頻道 policy 在 ACP session 注入允許的 `mcpServers`。runtime 會同步 allowlist 內的非 MCP CLI 功能設定（`app.*`、`chat.*`、`inline.*`），並把複製進 runtime 的 agent config 做 MCP sanitization：清空 `mcpServers`、關閉 legacy/include MCP、移除 `@mcp` tool selector。新頻道初始化會用安全 allowlist 預設啟用 `bot-tools`，預設開放安全檔案 egress，但不預設開放 Discord message egress 工具；一般 agent 最終回覆應直接回傳文字，由 bot 統一 redaction、分段與送出。其他 catalog server 預設不開放。啟用的 server 一律透過 bot MCP policy proxy 啟動，proxy 會過濾 `tools/list` 並阻擋未授權的 `tools/call`，不把 Kiro `disabledTools` 當作安全邊界。
- **內建 bot-tools MCP**：`bot-tools` 由同一支 bot binary 的 `mcp-bot` 子命令提供。它包含中繼資訊工具、受目前 channel/thread 限制的稽核 timeline 查詢、選用的安全 Discord message egress、預設開放的安全 file egress 與 cron 管理工具；中繼資訊、list 與 audit 工具是唯讀。`bot_list_channel_data` 會回傳 bot 已觀測到的公開頻道/討論串名稱與 metadata，不回傳訊息內容。`bot_query_audit` 只能查目前 bot-tools 綁定 channel/thread 的 timeline row，不接受任意 SQL，也不回傳 raw JSON 或訊息內容；它不在新頻道初始化預設 allowlist 內，管理員 `/audit <prompt>` 會改用私有短生命週期 agent 查詢。`bot_send_message` 不在新頻道初始化預設 allowlist 內，只有管理員明確開放後才會寫入 pending egress action；它適合額外通知、明確交接或特殊 egress，不應用來送一般最終答案。`bot_send_file` 預設開放以保留互動中的檔案交付能力。主 bot 會做 secret redaction 後送到目前 channel 或 thread target，文字訊息會依 Discord 限制自動分段；文字檔會上傳 sanitized copy，可抽取可讀文字的 PDF/DOCX/XLSX 會抽取文字並上傳脫敏後的 `.txt` copy，不支援、無法讀取、超大或解壓膨脹超限的檔案不會原樣傳回 Discord。`bot_create_cron` 會寫入非破壞性 pending create action，`bot_delete_cron` 會寫入破壞性 pending delete action，且不在初始化預設 allowlist 內。
- **結構化 Discord mention**：agent 不會把原始 Discord mention token 當作 API 使用。每次使用者訊息只會在 prompt 中列出已驗證的 mention reference placeholder，來源包含發問者、該訊息中 Discord 結構化提到的 user，以及已設定的 peer handoff 目標。最終送出時只有這些精確 placeholder 會被 bot 轉成 Discord mention，且 `AllowedMentions` 只允許實際渲染的 user/role；agent 自己猜或拼出的 raw mention 字串會被 escape，不會 ping。
- **Secret redaction**：bot 傳到 Discord 的文字 egress 會在送出前替換已知敏感環境變數值，以及常見 API key、token、bearer、password、credential assignment。這是輸出端最後防線；它不代表 agent process 本身不能讀取原本就可存取的檔案。
- **Discord MCP 範圍**：`mcp-discord` 只是 catalog 中的一個 server；`/mcp` 與 `!mcp` 對它的管理方式與其他 MCP server 一致，可開關整個 server，或在 `/mcp manage` 精準管理 tool allowlist。env 層級 guild/channel/read-only/write/destructive guard 只保留作為直接或手動啟動 MCP server 時的底層防護。
- **Agent metrics**：當 ACP 回傳 turn metrics 時，agent 執行完成的可見回覆會帶 `⚡` metrics footer。usage ledger 會分開保存 Discord 訊息 ID 與 slash command interaction ID（`message_id`、`interaction_id`），並額外保存通用的 `invocation_id` 方便和 audit 交叉關聯。
- **Raw Discord 稽核資料庫**：bot 可見的 Discord events 會獨立寫入 SQLite（預設 `DATA_DIR/audit/discord.sqlite`），包含 append-only `discord_events` 與 messages、attachments、reactions、threads 查詢投影；也會在 `bot_audit_events` 紀錄 command 呼叫、command 回覆送出成功/失敗、agent job lifecycle、agent final response 等語意事件。Slash command initial response、deferred followup、cron/reminder command response 都會走同一套 delivery success/failure audit。高頻 typing-start event 預設不紀錄。這不會觸發 agent，也不會自動注入 agent 對話 context；現有 `chat.jsonl` 仍只紀錄實際 user/agent 互動。
- **Audit 關聯 ID**：`bot_audit_events` 的 `message_id` 與 `interaction_id` 代表觸發 bot command 的使用者呼叫來源；Discord 回傳 bot response message object 時，實際 bot 回覆訊息 ID 會存到 metadata 的 `response_message_id`；initial interaction response 與 modal 不會暴露 Discord message ID，所以改存 `interaction_response_type`。Cron agent 的 `response_sent` 代表 final response message 實際送達，不只是 thread 是否存在。
- **Slash command 可視性**：管理型 slash command 會設定 Discord default member permissions，讓一般使用者預設不會在 command picker 看到。`/mcp manage`、`/steering`、`/cron-list`、`/cwd`、`/status`、`/usage`、`/doctor`、`/audit`、`/models`、`/memory`、`/flashmemory` 等操作或查詢回應會優先使用 ephemeral private response，減少設定面板與敏感路徑留在頻道中。Audit prompt 調查的 agent 最終報告也會以私密回覆送出；`!audit` 文字指令因 Discord 無法提供 ephemeral 回覆，不會輸出稽核資料。非稽核的 agent 任務成果與明確的頻道行為變更仍會送到目標 channel/thread。
- **稽核權限**：`/audit` 直接使用 Discord effective channel permissions，不另建 ACL。呼叫者必須能管理目前目標頻道或討論串；討論串會接受父層頻道管理權限。Discord 權限異動會在下一次查詢即時生效。稽核資料只會透過私密 slash command response 回傳；`!audit` 文字指令不會回傳稽核 rows 或調查報告。
- 長回覆會依 Discord 訊息限制自動分段，並在跨段時補齊 code block fence；舊版 `/resume` 與 `!resume` 指令目前保留但不會重送最後回覆。
- **討論串模式**：預設新的父頻道任務會由 bot 主動開 Discord thread，過程與最終回覆都在 thread 中。`/thread off` 或父頻道 `/pause` 會停止新任務開 thread；新任務必須 @mention bot，使用頻道主 agent，在原頻道以 `🔄`、`💭`、`✨`、`🛠️`、`⚙️` 等 reaction heartbeat 顯示仍在運作，最後才送出實際回覆。`/thread on` 或父頻道 `/back` 會恢復新任務開 thread。
- **討論串互動**：在 bot 建立的 thread 中發訊息，會自動啟動獨立的 thread agent 接續討論。thread 會保存建立當下的監聽模式；父頻道後續切換 `/thread off` 不會讓舊 thread 被動改成 mention-only。若父頻道已是 `/thread off`，手動建立或未知來源的 thread 預設 mention-only，直到在該 thread 內 `/back`。非 active agent 閒置超過 `THREAD_AGENT_IDLE_SEC` 或非 active thread 歸檔時自動關閉，再次發訊息可重新啟動。容量上限不會自動關閉任何 thread agent；如果名額已滿，bot 會列出 active/inactive 狀態與 inactive 候選，讓使用者執行 `/close-thread thread_id:<id>` 關閉指定 inactive agent。active work 不會因 idle cleanup、歸檔事件或 thread agent 容量上限被強制終止；active thread 若被歸檔，會在目前 job 回到 idle 後關閉；`THREAD_AGENT_IDLE_SEC=0` 可停用討論串閒置清理。
- **取消與中斷**：`/cancel` 只送出 ACP `session/cancel` 取消目前任務；`/interrupt` 會先做同樣的 soft cancel，短暫等待後若同一任務仍在執行，才嘗試對 agent process group 送 `SIGINT`，用來中斷卡住的工具子程序。若同一任務仍卡住，重複 `/interrupt` 可再嘗試一次 `SIGINT`。它不會清除已保存的 session metadata，也不會關閉 Discord thread；若 agent 因中斷退出，下一則訊息會走既有的重啟與 `session/load` 流程
- **長回覆格式**：bot 會依 Discord 訊息限制自動分段，並先轉成 Discord-safe Markdown；標題會降級為粗體文字，code block 跨段時會自動補上關閉與重新開啟 fence，分段前綴會放在 code block 外。
- **Cron jobs**：排程定義存於 `DATA_DIR/cron/cron.json`，執行歷史存於 `DATA_DIR/cron/<jobID>/history.jsonl`。Cron 設定不再要求輸入工作目錄；排程 agent 一律使用所屬頻道當下的 CWD，因此管理員變更 `/cwd` 後，該頻道後續 cron 執行也會跟著切換。Agent-backed cron 執行一律建立或重用專屬 Discord thread；進度與最終回覆會送到該 cron thread，父頻道只會收到執行連結。若管理員明確開放內建 `bot_send_message` / `bot_send_file` safe egress，這些 pending delivery 也會送到 cron thread。Cron 管理工具一律以所屬父頻道作為 job scope；若 agent 傳入目前 thread target 作為 `channel_id`，bot-tools 會正規化回綁定的父頻道。未初始化頻道不能建立、恢復、手動執行或到點執行 agent-backed cron job。`bot-tools` 會把 cron 建立/刪除請求先寫成 `DATA_DIR/cron/pending/` 內的 JSON action，由 `CronTask` 在 scheduler tick 驗證與 ingest；無效 action 會被移除，刪除 action 只會刪除同一頻道擁有的 job。
- **多 bot 模式**：bot 啟動時會用完整 Discord guild member list 自動偵測同 server 內其他 bot，並盡量補上 bot role。`BOT_PEERS` 只需要用來覆蓋偵測結果、補上偵測不到的 bot、手動加入 role-only peer，或用 `!userID` 排除無關 bot；格式為 `Name:userID`、`Name:userID:roleID`、`Name::roleID` 或 `!userID`。自動 multi-bot mention-only 會在另一個 peer bot 對目前頻道或討論串具有實際可回應權限時啟用，包含直接在頻道發訊息，或建立公開討論串並在討論串內回覆；權限來源可以是明確 channel overwrite、繼承 role 權限或 `@everyone` 權限。自動偵測到的 role-only peer 仍不會單獨觸發 mention-only，除非用 `BOT_PEERS=Name::roleID` 手動指定。請用真正的 Discord mention（例如 `<@111111111111111111>` 或 Discord 介面的提及選單），若偵測或設定了 role ID，role mention（例如 `<@&222222222222222222>`）也會路由到目標 bot；純文字 `@BuildBot` 不一定會觸發。若要讓其中一個 bot 暫時恢復完整監聽，對該 bot 在主頻道執行 `/back` 或 `!back`，該主頻道底下的討論串也會繼承；若只想讓某條討論串回到 mention-only，可在該討論串執行 `/pause` 或 `!pause`
- **Bot 交接限制**：bot 產生的訊息預設不會觸發另一個 bot。只有在討論串內、明確 tag 目標 bot、原始任務訊息已有完成反應（`✅`），且內容不是進度、錯誤、逾時或空輸出時，才會被視為有效交接。一般討論串任務會帶入近期 Discord 討論串訊息作為 bounded context；通過 gate 的跨 bot 交接會帶入較長的 thread transcript 作為 handoff context，讓被交辦 bot 先掌握任務、先前決策、相關檔案、結果與剩餘工作
- **Slash command 範圍**：指令以 guild scope 註冊，但 bot 會拒絕在自己沒有讀寫權限的頻道或討論串中執行。管理型 slash command 會設定 Discord default member permissions，讓一般使用者預設不會在 command picker 看到；若還要做到 channel-specific 的指令選單隱藏，需要在 Discord app command permissions 設定，或用具備 `applications.commands.permissions.update` scope 的 OAuth2 token 同步權限
- **部署診斷**：在目標頻道或討論串執行 `/doctor`，可確認 Discord 權限、`BOT_PEERS` 設定，以及目前是開放模式、`/back` override 開放模式，或自動多 bot mention-only 模式
- **頻道 agent 閒置回收**：設定 `CHANNEL_AGENT_IDLE_SEC`（預設 `0` = 停用）可讓閒置的頻道 agent 自動關閉以釋放資源，下次發訊息時自動重啟

---

### 選配：Discord MCP Server

本專案內建 Discord MCP Server（`cmd/mcp-discord/`），啟用後 kiro agent 可直接操作 Discord——讀訊息、發訊息、列頻道、搜尋、加反應等。

#### MCP 額外權限需求

MCP server 使用的 Discord REST API 超出 bot 本體所需，啟用前請先補上以下權限：

**額外 Bot 權限：**
- Attach Files — `discord_send_file`
- Embed Links — `discord_send_embed`
- Manage Messages — `discord_delete_message`、`discord_pin_message`、`discord_remove_reaction`
- Create Public Threads — `discord_create_thread`
- Manage Channels — `discord_edit_channel_topic`

**額外 Privileged Intent：**
- **Server Members Intent** — `discord_list_members` 需要

> 前往 [Discord Developer Portal](https://discord.com/developers/applications) → 你的應用 → **Bot** 頁籤啟用 intent，並重新產生含額外權限的 OAuth2 邀請連結。

#### 安全範圍

bot 不會預設把 catalog 中的 MCP server 暴露給 agent。它會從 `KIRO_MCP_CONFIG`、`KIRO_HOME/settings/mcp.json` 或 `~/.kiro/settings/mcp.json` 動態讀取 catalog，但這只作為 bot policy catalog。實際 runtime 使用隔離的 `KIRO_HOME=DATA_DIR/kiro-agent-runtime`，並把 `KIRO_MCP_CONFIG` 覆寫到該 runtime home 底下由 bot 維護的空 MCP config；同時只同步 allowlist 內的非 MCP CLI 功能設定（`app.*`、`chat.*`、`inline.*`）到 `settings/cli.json`，讓 todo list、knowledge 等 Kiro 內建功能維持使用者原本偏好，但不複製 `mcp.*` 與未知設定。若同步全域 agent config 到 runtime，會清空 `mcpServers`、關閉 legacy/include MCP、移除 `@mcp` tool selector，避免 agent profile 自行帶入 MCP。agent 只會收到目前 Discord 頻道 policy 允許並透過 ACP `mcpServers` 注入的 server。已啟用的 server 會透過 bot MCP policy proxy 啟動，由 proxy 過濾 `tools/list` 並阻擋未授權 `tools/call`。

bot 也會註冊內建的 `bot-tools` MCP catalog entry，由同一支 bot binary 的 `mcp-bot` 子命令提供。它提供 bot data directory 中繼資訊工具、受目前 channel/thread 限制的稽核 timeline 查詢工具（`bot_query_audit`）、選用的安全 Discord message egress 工具（`bot_send_message`）、預設開放的安全 file egress 工具（`bot_send_file`），以及 cron 管理工具（`bot_create_cron`、`bot_list_cron`、`bot_delete_cron`）。新頻道初始化會自動啟用安全預設 allowlist（`bot_data_summary`、`bot_list_channel_data`、`bot_list_cron`、`bot_send_file`、`bot_create_cron`），但不預設開放 `bot_query_audit`、`bot_send_message` 或 `bot_delete_cron`；管理員授權的 `/audit <prompt>` 調查會使用只注入 `bot_query_audit` 的短生命週期私有 agent，因此即使一般頻道 MCP policy 未開放 audit 查詢，管理員仍可查閱稽核。一般 agent 最終回覆應直接回傳文字給 bot，由 bot 統一 redaction、分段與送出。若要讓一般頻道 agent 使用 audit、egress 或 cron 權限，請用 `/mcp manage` 做工具層級控管。

內建 `bot-tools` 工具：

| Tool | 權限提示 | 說明 |
|------|----------|------|
| `bot_data_summary` | 唯讀 | 摘要 data directory 中繼資訊，不回傳訊息內容 |
| `bot_list_channel_data` | 唯讀 | 列出 channel data directory、中繼檔案是否存在，以及 bot 已觀測到的公開頻道/討論串名稱 |
| `bot_list_cron` | 唯讀 | 列出所屬父頻道的排程任務；傳入 thread ID 時會正規化回父頻道 |
| `bot_query_audit` | 唯讀 | 查詢目前 channel/thread target 範圍內的稽核 timeline。它不是任意 SQL 工具，不能讀 raw table、`raw_json` 或訊息內容 |
| `bot_send_message` | 寫入、非破壞性 | 選用工具，預設不開放。佇列化額外 Discord 訊息，由 bot 端做 secret redaction、分段後送到目前 channel 或 thread target |
| `bot_send_file` | 寫入、非破壞性 | 預設開放的安全檔案 egress。佇列化本機文字檔進行脫敏，或將可抽取可讀文字的 PDF/DOCX/XLSX 抽取為文字後脫敏，最後以 sanitized `.txt` copy 上傳到目前 channel 或 thread target；附加長文字會先分段送出 |
| `bot_create_cron` | 寫入、非破壞性 | 佇列化建立 recurring cron job；傳入 thread ID 時會正規化回所屬父頻道 |
| `bot_delete_cron` | 寫入、破壞性 | 佇列化刪除 cron job；傳入 thread ID 時會正規化回父頻道，且只有 job 所屬頻道相符才會刪除 |

頻道管理員可在 Discord 中管理目前頻道 policy：

```text
/mcp status
/mcp status server:<server>
/mcp enable server:<server>
/mcp disable server:<server>
```

請用 `/mcp status` 查看合併後的 catalog 與目前頻道 policy 檢查清單。`/mcp enable` 會開放整個 server。工具層級控制與 MCP 重新載入請使用 `/mcp manage`：面板會以 private interaction response 顯示，並透過與 agent runtime 相同的 MCP policy proxy 掃描 server 目前的 `tools/list`、把工具快取到 SQLite、用 Discord select menu 執行允許/移除，並停止活躍 agent，讓下一次執行載入目前 MCP policy。原本手打 tool name 的 `allow-tool`、`deny-tool` 指令不再公開，避免使用者需要猜精確工具名稱。若 macOS LaunchAgent 部署中 private LAN URL 在互動 shell 可連，但 `/mcp manage` 掃描出現 `no route to host`，請參考 [macOS MCP Networking](docs/macos-mcp-networking.md)。

升級相容會依 data directory 執行一次。全新安裝時，catalog 內的 MCP server 都維持停用，直到頻道管理員手動啟用。若是從舊版升級，且舊版會全域繼承 Kiro MCP config，bot 只會對 `sessions.json` 中已存在的 channel 保留舊行為：第一次升級啟動當下 catalog 內的 server 會對這些既有 channel 以完整 server access 啟用。這個 migration 可重複執行但只會生效一次，不會覆蓋既有明確 policy，也不會自動啟用 migration 之後才新增的 MCP server。

`mcp-discord` 只是 catalog 中的一個可選 server；管理方式與其他 MCP server 一致：開關整個 server，或在 `/mcp manage` 內精準管理工具 allowlist。

在有較大 Discord 存取權的 workspace 啟用 MCP 前，建議先設定 allowlist：

```env
MCP_DISCORD_ALLOWED_GUILDS=123456789012345678
MCP_DISCORD_ALLOWED_CHANNELS=234567890123456789,345678901234567890
MCP_DISCORD_DOWNLOAD_DIR=/tmp/kiro-discord-mcp
MCP_DISCORD_ALLOW_DESTRUCTIVE=false
```

bot 的頻道 policy 不綁定特定 MCP server：它只負責啟用 catalog server，並透過 MCP policy proxy 過濾 agent 可見與可呼叫的工具，不會注入特定 server 專用的環境變數覆寫。若使用本專案內建 Discord MCP server，請把這些 env guard 設在 catalog command environment 或被載入的 `.env`。設定 guild allowlist 後，所有 channel 類工具都會先解析頻道並拒絕非授權 guild。設定 channel allowlist 後，頻道與 thread 類工具只允許指定 ID。`discord_download_attachment` 只接受 Discord attachment/CDN host；`MCP_DISCORD_DOWNLOAD_DIR` 會限制下載檔案可寫入的目錄。

更嚴格的部署可設定 `MCP_DISCORD_READ_ONLY=true` 封鎖所有寫入工具，或用 `MCP_DISCORD_ALLOWED_WRITE_TOOLS` 指定允許的寫入工具，例如 `discord_send_message,discord_reply_message`。設定 `MCP_DISCORD_ALLOW_DESTRUCTIVE=false` 可阻擋刪除、編輯、釘選、改 topic、移除 reaction 等管理操作，同時保留非破壞性發訊息能力。

#### 手動安裝

```bash
# 1. 編譯 MCP server
go build -o mcp-discord ./cmd/mcp-discord/

# 2. 安裝 steering 文件（全域，讓任何專案目錄都能使用）
mkdir -p ~/.kiro/steering
cp .kiro/steering/discord-mcp.md ~/.kiro/steering/discord-mcp.md

# 3. 註冊到 kiro MCP 設定
# 在 ~/.kiro/settings/mcp.json 的 "mcpServers" 中加入：
```

```json
"mcp-discord": {
  "command": "sh",
  "args": [
    "-c",
    "set -a && . /你的專案絕對路徑/.env && exec /你的專案絕對路徑/mcp-discord"
  ]
}
```

將 `/你的專案絕對路徑` 替換為實際路徑。

```bash
# 4. 依頻道啟用
# 在 Discord 中使用 /mcp status 確認清單中有此 server，再用 /mcp enable server:mcp-discord
```

#### 透過 Agent 自動安裝

也可以直接在 Discord 中對 bot 說：

> 讀取 INSTALL_MCP.md 並照步驟安裝 Discord MCP server。

Agent 會自動讀取說明、編譯、更新 mcp.json，並提示你重啟。

#### 啟用後可用的 Tools

| Tool | 說明 |
|------|------|
| `discord_list_channels` | 列出伺服器的文字頻道 |
| `discord_read_messages` | 讀取頻道最近的訊息 |
| `discord_send_message` | 發送訊息到指定頻道 |
| `discord_reply_message` | 回覆特定訊息 |
| `discord_add_reaction` | 對訊息加 emoji 反應 |
| `discord_list_members` | 列出伺服器成員 |
| `discord_search_messages` | 在頻道中搜尋關鍵字 |
| `discord_channel_info` | 取得頻道詳細資訊 |
| `discord_send_file` | 上傳本地檔案到頻道作為附件 |
| `discord_list_attachments` | 列出頻道近期訊息中的附件 |
| `discord_download_attachment` | 下載 Discord 附件到本地 |
| `discord_edit_message` | 編輯訊息 |
| `discord_delete_message` | 刪除訊息 |
| `discord_get_message` | 以 ID 取得單則訊息 |
| `discord_send_embed` | 發送 embed 富文本訊息 |
| `discord_pin_message` | 釘選或取消釘選訊息 |
| `discord_create_thread` | 從訊息建立 thread |
| `discord_list_threads` | 列出伺服器中的活躍 threads |
| `discord_remove_reaction` | 移除訊息上的 reaction |
| `discord_get_reactions` | 取得對特定 emoji 反應的使用者 |
| `discord_edit_channel_topic` | 編輯頻道主題 |
| `discord_list_roles` | 列出伺服器角色 |
| `discord_get_user` | 查詢特定使用者資訊 |
