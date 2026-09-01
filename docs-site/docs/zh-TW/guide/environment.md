# 環境變數參考

Bot 不會自行載入 `.env`。請透過 shell、launchd、systemd、Docker 或其他 process manager 注入環境變數。

啟動後可用 `/doctor` 檢查實際值。敏感值會被遮蔽。

## 如何使用本頁

環境變數大致分成四類：

- 主 bot runtime：Discord 連線、ACP agent engines、channel/thread 行為、WebShare、audit、usage 與背景維護。
- WebShare relay：browser shares 使用的獨立 self-host relay process。
- MCP helper servers：獨立執行的 `mcp-discord-server`、`mcp-media-server` 等 process。
- Provider credentials：Kiro、STT、media generation 或其他外部服務 API keys。

必填變數必須在啟動前設定。可選變數通常可以留空，bot 會套用保守預設值。修改任何 process-level 環境變數後，請重啟服務並在 Discord 使用 `/doctor` 確認實際 runtime。`/doctor` 會遮蔽 secrets，是確認 production 設定最安全的方式。

既有 Kiro-only 部署不需要新增 OMP 相關變數。只有在主機已安裝 `omp`、完成認證，並且明確要啟用 OMP 時才加入 OMP 設定。

`kiro-cli` 與 `omp` 都是在本 repository 之外安裝與更新。基本 CLI setup 與更新指令見 [安裝](installation.md)；平台細節請以各自 upstream 文件為準。

## 常見設定型態

### Kiro-Only 預設

這是既有部署最平順的升級路徑。不需要 OMP。

```env
AGENT_ENGINE=kiro
AGENT_ENGINES_ENABLED=
```

### 雙 Engine Bot

當同一個 bot 要允許 channel admins 透過 `/engine` 在 Kiro 與 OMP 間切換時使用。

```env
AGENT_ENGINE=kiro
AGENT_ENGINES_ENABLED=kiro,omp
OMP_PATH=omp
```

只有在服務使用者已安裝並認證 `omp` 後，才啟用 OMP。

### OMP Production Profile

當你希望 bot-managed OMP auth、settings、sessions、caches 與互動式 OMP profile 隔離時，使用 named profile。

```bash
OMP_PROFILE=kiro-discord-bot omp setup
```

```env
OMP_PROFILE=kiro-discord-bot
```

如果你刻意要讓服務沿用 OMP default profile 以維持升級相容性，則讓 `OMP_PROFILE` 留空。

### Pure OMP Bot

只有在 bot 不應使用 Kiro 時才使用。

```env
AGENT_ENGINE=omp
AGENT_ENGINES_ENABLED=omp
OMP_PATH=omp
```

### Multi-Bot 部署

執行多個部門 bot 時，每個 bot 都應該有自己的 Discord token 與持久資料目錄。

```env
DISCORD_TOKEN=...
DATA_DIR=/var/lib/kiro-discord-bot/marketing
BOT_PEERS=...
```

不要在不同 bot identity 之間共用 `DATA_DIR`。Audit DB、usage SQLite DB 與遷移封存 JSONL、channel settings、MCP policy 與 agent runtime files 都是該 bot 擁有的狀態。

### WebShare Bot 與 Relay

只有在 self-host relay 可透過 HTTPS/WSS 連線，且 bot 可作為 relay host 完成驗證後，才啟用 WebShare。

```env
WEBSHARE_ENABLED=true
WEBSHARE_RELAY_URL=wss://relay.example
WEBSHARE_PUBLIC_BASE_URL=https://relay.example
WEBSHARE_HOST_TOKEN_FILE=/etc/kdb-webshare/host-token
```

請使用相同的 relay-side `RELAY_HOST_TOKEN_FILE` 或 `RELAY_HOST_TOKEN`。Public base URL 是使用者在 browser 開啟的網址；relay URL 是 bot 附加 `/r/<room>` 前使用的 relay origin 或 route prefix。

## 變數關係

- `DATA_DIR` 擁有 bot 的持久狀態：channel metadata、audit DB、usage SQLite DB 與遷移封存檔、MCP policy、下載 attachments 與 bot-managed engine runtime directories。
- `DEFAULT_CWD` 是設定時顯示的預設專案根目錄。`ALLOWED_CWD_ROOTS` 會限制可選的 channel working directories。
- `AGENT_ENGINE` 決定新 scope 的預設 engine。`AGENT_ENGINES_ENABLED` 決定 `/engine` 可以切換到哪些 engine。
- `OMP_SESSION_DIR` 決定 bot 啟動的 OMP ACP session files 放在哪裡。`OMP_PROFILE` 決定 OMP auth/settings/cache 身份。兩者處理的是不同層次的隔離。
- `KIRO_MCP_CONFIG` 會被視為 MCP catalog source。Runtime agents 會收到 `DATA_DIR` 內依照 bot policy 產生的 MCP settings，而不是直接繼承使用者自己的 Kiro settings。
- `TRUST_ALL_TOOLS` 與 `TRUST_TOOLS` 是 ACP server permission request 的核准設定，不會取代 Discord command ACL 或 MCP channel policy。
- `PREFLIGHT_MODE=skip` 是停用 ACP preflight 的明確方式。`SKIP_PREFLIGHT` 是相容性設定，只要非空就會跳過 preflight。

- WebShare 使用兩個 processes：bot 會把 durable share state 放在 `DATA_DIR/webshare`，relay 只提供 static assets 並轉送 encrypted WebSocket frames。
- `WEBSHARE_PUBLIC_BASE_URL` 必須符合 browser-facing HTTPS origin。`WEBSHARE_RELAY_URL` 應指向 relay origin，通常是同一 origin 的 `wss://`；bot 會為每個 host WebSocket 附加 `/r/<room>`。
- Bot 的 `WEBSHARE_HOST_TOKEN_FILE`/`WEBSHARE_HOST_TOKEN` 必須符合 relay 的 `RELAY_HOST_TOKEN_FILE`/`RELAY_HOST_TOKEN`；production 優先使用 file-based secrets。

## 升級注意事項

- Kiro-only 部署升級後不需要新增環境變數也能維持既有行為。
- 升級後 WebShare 仍保持停用，除非設定 `WEBSHARE_ENABLED=true` 以及 relay URLs/tokens。
- 不要在 production 設定 `OMP_PROFILE`，除非該 profile 已經用執行 bot 的同一個 OS service user 完成認證。
- 修改 engine、MCP、audit 或儲存相關變數後，請重啟服務並執行 `/doctor`。
- launchd、systemd 或 Docker 部署應該把變數放在 service definition，不要假設互動 shell profile 會被繼承。

## 必填

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `DISCORD_TOKEN` | 必填 | Discord bot token。 |

## 核心執行環境

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `DISCORD_GUILD_ID` | 空 | Slash command 註冊 guild。空值使用 Discord global command scope。 |
| `KIRO_CLI_PATH` | `kiro-cli` | Kiro CLI 執行檔路徑。 |
| `OMP_PATH` | `omp` | omp 引擎執行檔路徑（僅啟用 omp 時需要）。 |
| `OMP_PROFILE` | 空 | bot-managed OMP agents 可選使用的 OMP profile。OMP profile 會隔離 auth、settings、sessions、caches。新的 production 部署建議設定 `kiro-discord-bot`，並在啟用 OMP 前先認證此 profile。留空會沿用 OMP default profile，避免破壞既有安裝。 |
| `OMP_SESSION_DIR` | `DATA_DIR/omp-agent-runtime/sessions` | 透過 `omp --session-dir` 傳入的 bot-managed OMP session 目錄。留空會使用 data-dir 預設；若服務需要共用 session 目錄，可設定絕對路徑。 |
| `AGENT_ENGINE` | `kiro` | 新頻道的預設 agent 引擎：`kiro` 或 `omp`。 |
| `AGENT_ENGINES_ENABLED` | （僅 AGENT_ENGINE） | `/engine` 可切換的引擎清單（逗號分隔，如 `kiro,omp`）。留空則停用切換。 |
| `KIRO_API_KEY` | 空 | headless 環境的 Kiro 認證金鑰；互動主機也可用 `kiro-cli login`。 |
| `DEFAULT_CWD` | `/projects` | `/cwd` 設定面板顯示的專案根目錄。 |
| `ALLOWED_CWD_ROOTS` | 空 | 可選的逗號分隔工作目錄根目錄 allowlist。 |
| `DATA_DIR` | `./data` | Bot 持久資料、頻道 metadata、sessions、audit DB、usage SQLite DB 與遷移封存檔、MCP policy 與 bot-managed engine runtime directories。 |
| `BOT_LOCALE` | `en` | Bot 回應語系。專案文件支援英文與繁體中文。 |

## Agent 執行

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `ASK_TIMEOUT_SEC` | `3600` | 單次 agent 請求最長等待秒數。 |
| `QUEUE_BUFFER_SIZE` | `20` | 每個 target 的 job queue buffer。 |
| `STREAM_UPDATE_SEC` | `3` | 串流更新最小間隔秒數。 |
| `MAX_SCANNER_BUFFER_MB` | `64` | 長輸出 scanner buffer。 |
| `DOWNLOAD_TIMEOUT_SEC` | `120` | Discord attachment 下載 timeout。 |
| `KIRO_MODEL` | 空 | 初始 model override。 |
| `KIRO_AGENT` | 空 | 初始 Kiro agent profile 或 mode。 |
| `TRUST_ALL_TOOLS` | `true` | 完全等於 `true` 時預設允許 ACP server permission request；其他值預設拒絕，除非符合 `TRUST_TOOLS`。 |
| `TRUST_TOOLS` | 空 | 可選的逗號分隔 trusted tool allowlist。 |
| `KIRO_MCP_CONFIG` | 空 | 可選 MCP catalog 來源。實際 agent 使用 `DATA_DIR/kiro-agent-runtime/` 內隔離後的 settings。 |

## Thread 與監聽行為

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `THREAD_AUTO_ARCHIVE` | `1440` | 任務討論串自動封存分鐘數。 |
| `THREAD_AGENT_MAX` | `5` | 最大 active thread agents。小於 `1` 啟動時視為錯誤。 |
| `THREAD_AGENT_IDLE_SEC` | `900` | Thread agent 閒置 timeout 秒數。 |
| `CHANNEL_AGENT_IDLE_SEC` | `0` | Channel agent 閒置 timeout 秒數。`0` 表示停用。 |
| `BOT_PEERS` | 空 | 多 bot mention 與 handoff 的逗號分隔 peer hints。 |

## 時區、用量與維護

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `HEARTBEAT_SEC` | `60` | 背景維護 tick 秒數。 |
| `CRON_TIMEZONE` | 空 | 排程任務時區。 |
| `CRON_TIMEOUT_MIN` | `5` | 排程任務 agent 執行逾時分鐘數。小於 `1` 會退回 `5`。 |
| `USAGE_TIMEZONE` | `CRON_TIMEZONE`，再退回本機預設 | `/usage` 今日、本週、本月統計時區。 |
| `USAGE_RETENTION_MONTHS` | `0` | 線上 SQLite usage 保留月數。`0` 表示全部保留；不影響封存的舊 JSONL 遷移備份。 |
| `ATTACHMENT_RETAIN_DAYS` | `7` | 已下載 Discord attachment 保留天數。 |
| `ATTACHMENT_MAX_MB` | `25` | Bot 接受的最大 attachment 大小。 |
| `PREFLIGHT_MODE` | `warn` | ACP 相容性 preflight 模式。`strict` 失敗即退出，`skip` 停用檢查，不明值會退回 warn。 |
| `SKIP_PREFLIGHT` | 空 | 任意非空值都會跳過 ACP preflight。建議用 `PREFLIGHT_MODE=skip` 表達明確意圖。 |

## WebShare Bot

當 `WEBSHARE_ENABLED=false` 或空值時，WebShare 停用。啟用後，`/webshare start` 會建立 target-scoped delegated browser link。部署 runbook 與安全模型見 [WebShare](webshare.md)。

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `WEBSHARE_ENABLED` | `false` | 啟用 `/webshare start`、`/webshare stop`、`/webshare status` 與 `/webshare revoke`。 |
| `WEBSHARE_RELAY_URL` | 空 | Bot 作為 host 連線使用的 relay origin 或 route prefix，例如 `wss://relay.example`。WebShare 啟用時必填。 |
| `WEBSHARE_PUBLIC_BASE_URL` | 空 | 用於建立 control/view links 的 browser-facing HTTPS base URL，例如 `https://relay.example`。WebShare 啟用時必填。 |
| `WEBSHARE_HOST_TOKEN_FILE` | 空 | Relay host bearer token 檔案路徑。Production 建議使用。 |
| `WEBSHARE_HOST_TOKEN` | 空 | Relay host bearer token 內容。只建議本機開發或 secret manager injection 使用。 |
| `WEBSHARE_MAX_FRAME_BYTES` | `4194304` | Bot 接受的最大 encrypted relay frame size。應與 `RELAY_MAX_FRAME_BYTES` 對齊。 |
| `WEBSHARE_RECONNECT_INITIAL_MS` | `1000` | Bot-to-relay reconnect 初始 backoff milliseconds。 |
| `WEBSHARE_RECONNECT_MAX_MS` | `30000` | Bot-to-relay reconnect 最大 backoff milliseconds。 |

## WebShare Relay

這些變數設定獨立 `webshare-relay` process，不是主 bot process；除非兩者由同一份 environment 啟動。

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `RELAY_ADDR` | `:8080` | Static assets、WebSocket rooms、health 與 optional local reverse proxy 的 HTTP listen address。 |
| `RELAY_PUBLIC_BASE_URL` | 空 | Browser-facing HTTPS base URL，用於 relay healthcheck defaults 與 operator diagnostics。 |
| `RELAY_HOST_TOKEN_FILE` | 空 | Bot host WebSocket connections 需要的 bearer token 檔案。Production 建議使用。 |
| `RELAY_HOST_TOKEN` | 空 | Host WebSocket authentication 的 bearer token 內容。Relay 沒有任何 token source 時會拒絕 serve。 |
| `RELAY_TRUST_PROXY` | `false` | 信任 reverse proxy 傳入的 `X-Forwarded-*` headers。只有在 request 都經 trusted proxy 進入時才啟用。 |
| `RELAY_MAX_ROOMS` | `1000` | Relay rooms 同時存在的最大數量。 |
| `RELAY_MAX_PEERS_PER_ROOM` | `32` | 每個 room 可連線的最大 guest peers。 |
| `RELAY_MAX_FRAME_BYTES` | `4194304` | Relay 轉送的最大 opaque encrypted frame size。 |
| `RELAY_HOST_IDLE_TIMEOUT` | `0` | Host idle timeout duration。`0` 表示停用 application-level idle expiry。 |
| `RELAY_GUEST_IDLE_TIMEOUT` | `0` | Guest idle timeout duration。`0` 表示停用 application-level idle expiry。 |
| `RELAY_WRITE_TIMEOUT` | `30s` | Relay WebSocket write timeout。必須大於 zero。 |
| `RELAY_LOG_LEVEL` | `info` | Relay log level：`debug`、`info`、`warn` 或 `error`。 |
| `RELAY_METRICS_ADDR` | 空 | Optional Prometheus metrics listen address，常見值是 `127.0.0.1:9090`。 |

## Audit

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `AUDIT_LOG_ENABLED` | `true` | 啟用 audit 紀錄。 |
| `AUDIT_LOG_DB` | `DATA_DIR/audit/discord.sqlite` | SQLite audit database 路徑。 |
| `AUDIT_LOG_RETENTION_DAYS` | `0` | Audit 保留天數。`0` 表示全部保留。 |
| `AUDIT_LOG_QUEUE_SIZE` | `1000` | Async audit queue 大小。滿載時 audit-only event 可能被丟棄並寫 log。 |
| `AUDIT_LOG_RECORD_CONTENT` | `true` | 在 audit projection 與 raw event payload 中記錄訊息內容。 |
| `AUDIT_LOG_RECORD_TYPING` | `false` | 記錄 Discord typing event。 |

## A2A NATS Custom Binding

`NATS_URL` 為空時 A2A 停用，既有 Discord 行為應維持不變。如果設定 `NATS_URL`，startup 需要有效的 `A2A_AGENT_ID`；rollback/no-op disable 時清空 `NATS_URL`。Step-by-step setup 見 [使用 NATS 啟用 A2A](a2a-nats-setup.md)。Identity、subject、policy 與 task-state 關鍵字見 [A2A 協議模型](a2a-protocol.md)。Rollout gates、ACL templates 與 smoke matrix 見 [A2A NATS Rollout](../../guide/a2a-nats-rollout.md)。

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `NATS_URL` | 空 | NATS server URL list。空值會完全停用 A2A。 |
| `NATS_CREDS_FILE` | 空 | NKey/JWT credentials file path。建議的 production credential。 |
| `NATS_TOKEN` | 空 | Development token。不要作為唯一 production credential。 |
| `NATS_TLS_CA_FILE` | 空 | NATS server certificate validation 的 TLS CA file。這不是 client mTLS authentication。 |
| `A2A_CONFIRMATION_SECRET` | 空 | A2A policy/delegation confirmation token 的簽章 secret；production 建議由 secret manager 注入。 |
| `A2A_AGENT_ID` | 空 | 穩定 bot/process base identity；runtime mode 中不是單獨的 route。 |
| `A2A_RUNTIME_ID_MODE` | `legacy` | `legacy`、`dual` 或 `runtime`。Production target 是 `runtime`；`dual` 只用於 bounded legacy drain。 |
| `A2A_AGENT_NAME` | 空 | Public peer-card display name。 |
| `A2A_AGENT_DESCRIPTION` | 空 | Public capability summary。不要包含 secrets、private paths、hosts 或 user data。 |
| `A2A_TASK_TIMEOUT_SEC` | `3600` | Remote task timeout 秒數。 |
| `A2A_MAX_DELEGATION_DEPTH` | `1` | 最大 nested delegation depth。 |
| `A2A_AUTO_DELEGATE_ENABLED` | `false` | 當 channel policy 也允許時，允許 automatic outbound delegation。 |
| `A2A_REQUIRE_CONFIRMATION_FOR_REMOTE` | `true` | Remote task execution 前需要 confirmation。 |
| `A2A_PRODUCTION_SECURITY` | `false` | `true` 時需要 `NATS_CREDS_FILE`，並拒絕 token-only 或 unauthenticated production startup。 |
| `A2A_TASK_RETENTION_DAYS` | `30` | Task/event retention。只有明確要永久保留時才設為 `0`。 |
| `A2A_OBJECT_RETENTION_DAYS` | `30` | Object/artifact retention。只有明確要永久保留時才設為 `0`。 |
| `A2A_MAX_PENDING_TASKS` | `100` | Global pending remote task limit。`0` 表示無限制。 |
| `A2A_MAX_OUTBOUND_TASKS_PER_CHANNEL` | `10` | Per-channel outbound remote task limit。`0` 表示無限制。 |
| `A2A_MAX_INBOUND_TASKS_PER_CHANNEL` | `10` | Per-channel inbound remote task limit。`0` 表示無限制。 |
| `A2A_MAX_EVENT_RATE_PER_MIN` | `120` | A2A event quota per minute。`0` 表示無限制。 |

修改任何 A2A 變數後，請重啟 bot 並執行 `/doctor`。`/doctor` 會顯示 enabled/disabled state、auth mode、production guard state、retention、quotas 與已遮蔽的 credential presence，不會印出 raw tokens 或 credential paths。

## 語音轉文字

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `STT_ENABLED` | `false` | 啟用 voice/audio transcription。 |
| `STT_PROVIDER` | `groq` | STT provider。 |
| `STT_API_KEY` | 空 | Provider API key。 |
| `STT_MODEL` | 空 | Provider model override。 |
| `STT_LANGUAGE` | 空 | 可選語言提示。 |
| `STT_MAX_DURATION_SEC` | `300` | 最大轉錄音訊秒數。 |

## Discord MCP Server

這些變數設定 `mcp-discord-server`，不是主 bot process；除非兩者共用同一份 process 環境。

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `MCP_DISCORD_ALLOWED_GUILDS` | 空 | 可選的逗號分隔 guild allowlist。 |
| `MCP_DISCORD_ALLOWED_CHANNELS` | 空 | 可選的逗號分隔 channel allowlist。 |
| `MCP_DISCORD_DOWNLOAD_DIR` | 空 | 設定後，`discord_download_attachment` 的 save path 必須位於此 root 內。 |
| `MCP_DISCORD_READ_ONLY` | `false` | `true` 時阻擋所有 write tools。 |
| `MCP_DISCORD_ALLOWED_WRITE_TOOLS` | 空 | 可選的逗號分隔 write-tool allowlist。 |
| `MCP_DISCORD_ALLOW_DESTRUCTIVE` | `true` | `false` 時阻擋 delete 等 destructive tools。 |

## Media MCP Server

這些變數設定 `mcp-media-server`。

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `GEMINI_API_KEY` | 空 | 啟用 Gemini image、video、music 與 TTS providers。 |
| `OPENAI_API_KEY` | 空 | 啟用 OpenAI image 與 TTS providers。 |
| `MEDIA_DEFAULT_IMAGE_MODEL` | provider default | 預設 image model override。 |
| `MEDIA_DEFAULT_VIDEO_MODEL` | provider default | 預設 video model override。 |
| `MEDIA_DEFAULT_MUSIC_MODEL` | provider default | 預設 music model override。 |
| `MEDIA_DEFAULT_TTS_MODEL` | provider default | 預設 TTS model override。 |
| `MEDIA_SYNC_WAIT_SEC` | `20` | 舊名稱 media tool 在回傳 `job_id` 前，等待即時結果的秒數。這個值應低於 MCP client request timeout。 |
| `MEDIA_SYNC_TIMEOUT_SEC` | `600` | 舊名稱 media tool 啟動的 managed job 最長執行秒數。明確 async jobs 使用 `MEDIA_JOB_TIMEOUT_SEC`。 |
| `MEDIA_JOB_TIMEOUT_SEC` | `900` | 非同步 media job 的最長執行秒數。 |
| `MEDIA_JOB_RETENTION_SEC` | `86400` | completed async job metadata 可被列出的保留秒數。 |
| `MEDIA_JOB_MAX_ACTIVE` | `4` | 單一 `mcp-media-server` process 中 queued 或 running async media jobs 的上限。設為 `0` 可停用限制。 |

如果沒有設定 `GEMINI_API_KEY` 或 `OPENAI_API_KEY`，`mcp-media-server` 會在啟動時退出。
