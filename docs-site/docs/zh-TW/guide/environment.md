# 環境變數參考

Bot 不會自行載入 `.env`。請透過 shell、launchd、systemd、Docker 或其他 process manager 注入環境變數。

啟動後可用 `/doctor` 檢查實際值。敏感值會被遮蔽。

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
| `AGENT_ENGINE` | `kiro` | 新頻道的預設 agent 引擎：`kiro` 或 `omp`。 |
| `AGENT_ENGINES_ENABLED` | （僅 AGENT_ENGINE） | `/engine` 可切換的引擎清單（逗號分隔，如 `kiro,omp`）。留空則停用切換。 |
| `KIRO_API_KEY` | 空 | headless 環境的 Kiro 認證金鑰；互動主機也可用 `kiro-cli login`。 |
| `DEFAULT_CWD` | `/projects` | `/cwd` 設定面板顯示的專案根目錄。 |
| `ALLOWED_CWD_ROOTS` | 空 | 可選的逗號分隔工作目錄根目錄 allowlist。 |
| `DATA_DIR` | `./data` | Bot 持久資料、頻道 metadata、sessions、audit DB、usage ledger、MCP policy 與 runtime Kiro settings。 |
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
| `USAGE_TIMEZONE` | `CRON_TIMEZONE`，再退回本機預設 | `/usage` 今日、本週、本月統計時區。 |
| `USAGE_RETENTION_MONTHS` | `0` | Usage ledger 保留月數。`0` 表示全部保留。 |
| `ATTACHMENT_RETAIN_DAYS` | `7` | 已下載 Discord attachment 保留天數。 |
| `ATTACHMENT_MAX_MB` | `25` | Bot 接受的最大 attachment 大小。 |
| `PREFLIGHT_MODE` | `warn` | ACP 相容性 preflight 模式。`strict` 失敗即退出，`skip` 停用檢查，不明值會退回 warn。 |
| `SKIP_PREFLIGHT` | 空 | 任意非空值都會跳過 ACP preflight。建議用 `PREFLIGHT_MODE=skip` 表達明確意圖。 |

## Audit

| 變數 | 預設 | 用途 |
| --- | --- | --- |
| `AUDIT_LOG_ENABLED` | `true` | 啟用 audit 紀錄。 |
| `AUDIT_LOG_DB` | `DATA_DIR/audit/discord.sqlite` | SQLite audit database 路徑。 |
| `AUDIT_LOG_RETENTION_DAYS` | `0` | Audit 保留天數。`0` 表示全部保留。 |
| `AUDIT_LOG_QUEUE_SIZE` | `1000` | Async audit queue 大小。滿載時 audit-only event 可能被丟棄並寫 log。 |
| `AUDIT_LOG_RECORD_CONTENT` | `true` | 在 audit projection 與 raw event payload 中記錄訊息內容。 |
| `AUDIT_LOG_RECORD_TYPING` | `false` | 記錄 Discord typing event。 |

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
| `MEDIA_DEFAULT_TTS_MODEL` | provider default | 預設 TTS model override。 |

如果沒有設定 `GEMINI_API_KEY` 或 `OPENAI_API_KEY`，`mcp-media-server` 會在啟動時退出。
