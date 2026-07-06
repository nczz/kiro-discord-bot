# Media MCP

`mcp-media-server` 是可選的媒體生成 MCP server。一般 bot 操作不需要它。

## Providers

Server 會依照可用 API key 註冊 providers：

| API key | 啟用能力 |
| --- | --- |
| `GEMINI_API_KEY` | Image generation、video generation、music generation、text to speech。 |
| `OPENAI_API_KEY` | Image generation 與 text to speech。 |

如果沒有任何 provider API key，server 會在啟動時退出。

可選預設：

| 變數 | 用途 |
| --- | --- |
| `MEDIA_DEFAULT_IMAGE_MODEL` | 覆寫預設 image model。 |
| `MEDIA_DEFAULT_VIDEO_MODEL` | 覆寫預設 video model。 |
| `MEDIA_DEFAULT_MUSIC_MODEL` | 覆寫預設 music model。 |
| `MEDIA_DEFAULT_TTS_MODEL` | 覆寫預設 text-to-speech model。 |
| `MEDIA_SYNC_WAIT_SEC` | 舊名稱 `generate_*` tools 在回傳 `job_id` 前，等待即時結果的秒數。 |
| `MEDIA_SYNC_TIMEOUT_SEC` | 舊名稱 `generate_*` tool 啟動的 managed job 最長執行秒數。 |

## Tools

| Tool | 用途 |
| --- | --- |
| `generate_image` | 透過 managed job 生成圖片。若很快完成會回傳本機路徑；若仍在執行會回傳 `job_id`。 |
| `start_image_generation` | 啟動非同步 image generation job，立即回傳 `job_id`。 |
| `start_image_edit` | 使用 `image_path` 與 `prompt` 啟動非同步 image edit job。 |
| `start_video_generation` | 啟動非同步 video generation job，立即回傳 `job_id`。 |
| `start_music_generation` | 啟動非同步 music generation job。 |
| `start_text_to_speech` | 啟動非同步 text-to-speech job。 |
| `get_media_job` | 查詢非同步 media job。當 `status=succeeded` 時，回應會包含本機檔案路徑。 |
| `list_media_jobs` | 列出近期非同步 media jobs。 |
| `cancel_media_job` | 取消 queued 或 running 的非同步 media job。 |
| `edit_image` | 透過 managed job 編輯既有本機圖片。若很快完成會回傳本機路徑；若仍在執行會回傳 `job_id`。 |
| `generate_video` | 透過 managed job 生成影片。若很快完成會回傳本機路徑；若仍在執行會回傳 `job_id`。 |
| `generate_music` | 透過 managed job 生成音樂。若很快完成會回傳本機路徑；若仍在執行會回傳 `job_id`。 |
| `text_to_speech` | 透過 managed job 生成語音。若很快完成會回傳本機路徑；若仍在執行會回傳 `job_id`。 |
| `list_models` | 列出可用 models，可用 `image`、`video`、`music`、`tts` 過濾。 |

生成結果會以本機檔案路徑回傳給 agent。請依照成本與 egress 風險設定 channel policy。

## Managed 與非同步 Media Jobs

Media generation 可能超過 MCP client 的 request timeout。舊名稱入口（`generate_image`、`edit_image`、`generate_video`、`generate_music`、`text_to_speech`）現在是 managed facade：會先啟動可追蹤 job，短暫等待快速結果，然後回傳已保存的本機路徑，或在仍執行時回傳 `job_id` 讓 agent polling。

這可以避免先浪費一次 provider quota 在同步逾時上。如果舊名稱 tool 回傳 `job_id`，請繼續用 `get_media_job` 查同一個 job；不要為同一個請求重新開始另一個 generation。

明確的 `start_*` async tools 仍然保留，適合 agent 或操作人一開始就想使用 job 控制流程時使用。

流程如下：

1. 用相同 media 參數呼叫舊名稱 `generate_*` tool，或對應的 `start_*` tool。
2. 保留回傳的 `job_id`。
3. 使用 `get_media_job` polling，直到 `status` 是 `succeeded` 或 `failed`。
4. 成功時使用回傳的本機 `Path`。

Server process 會在記憶體保存 job 狀態，所以 pending job state 可以跨過 MCP request timeout，但不能跨過 `mcp-media-server` 重啟。生成檔案仍會寫入 `MEDIA_OUTPUT_DIR`。

| 環境變數 | 預設 | 意義 |
| --- | --- | --- |
| `MEDIA_SYNC_WAIT_SEC` | `20` | 舊名稱 tool 在回傳 `job_id` 前，等待即時結果的秒數。這個值應低於 MCP client request timeout。 |
| `MEDIA_SYNC_TIMEOUT_SEC` | `600` | 舊名稱 `generate_*` tool 啟動的 managed job 最長執行秒數。明確 async jobs 使用 `MEDIA_JOB_TIMEOUT_SEC`。 |
| `MEDIA_JOB_TIMEOUT_SEC` | `900` | 非同步 media job 的最長執行秒數。 |
| `MEDIA_JOB_RETENTION_SEC` | `86400` | completed async job metadata 可被列出的保留秒數。 |
| `MEDIA_JOB_MAX_ACTIVE` | `4` | 單一 server process 中 queued 或 running async media jobs 的上限。設為 `0` 可停用限制。 |

## 維運注意事項

Media tools 會消耗外部 provider quota，也可能產生不適合所有 Discord channel 的檔案。建議預設停用，只在有明確用途的 channel 透過 `/mcp manage` 啟用。
