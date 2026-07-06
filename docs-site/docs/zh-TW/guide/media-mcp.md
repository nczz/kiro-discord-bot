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
| `MEDIA_SYNC_TIMEOUT_SEC` | 單次同步 media tool call 的最長執行秒數。 |

## Tools

| Tool | 用途 |
| --- | --- |
| `generate_image` | 同步依 `prompt` 生成圖片，可選 `model`、`size`、`aspect_ratio`。只適合預期會在 MCP client request timeout 內完成的請求。 |
| `start_image_generation` | 啟動非同步 image generation job，立即回傳 `job_id`。 |
| `start_image_edit` | 使用 `image_path` 與 `prompt` 啟動非同步 image edit job。 |
| `start_video_generation` | 啟動非同步 video generation job。長時間 video model 應使用這個工具。 |
| `start_music_generation` | 啟動非同步 music generation job。 |
| `start_text_to_speech` | 啟動非同步 text-to-speech job。 |
| `get_media_job` | 查詢非同步 media job。當 `status=succeeded` 時，回應會包含本機檔案路徑。 |
| `list_media_jobs` | 列出近期非同步 media jobs。 |
| `cancel_media_job` | 取消 queued 或 running 的非同步 media job。 |
| `edit_image` | 同步用 `image_path` 與 `prompt` 編輯既有本機圖片。較慢 edit 應使用 `start_image_edit`。 |
| `generate_video` | 同步依 `prompt` 生成影片，可選 `image_path` 作為條件。大多數 video request 應使用 `start_video_generation`。 |
| `generate_music` | 同步依 `prompt` 生成音樂，可選 `duration_sec`。較長 music request 應使用 `start_music_generation`。 |
| `text_to_speech` | 同步依 `text` 生成語音，可選 `model` 與 `voice`。長文本應使用 `start_text_to_speech`。 |
| `list_models` | 列出可用 models，可用 `image`、`video`、`music`、`tts` 過濾。 |

生成結果會以本機檔案路徑回傳給 agent。請依照成本與 egress 風險設定 channel policy。

## 非同步 Media Jobs

某些 media model 可能超過 MCP client 的 request timeout。這類情境請使用對應的 `start_*` async tool，不要使用同步 generation。

流程如下：

1. 用相同 media 參數呼叫對應的 async start tool。
2. 保留回傳的 `job_id`。
3. 使用 `get_media_job` polling，直到 `status` 是 `succeeded` 或 `failed`。
4. 成功時使用回傳的本機 `Path`。

Video generation、高解析 image、較慢 image model、較長 music generation、長文本 TTS 預設都建議使用 async tools。

Server process 會在記憶體保存 job 狀態，所以 pending job state 可以跨過 MCP request timeout，但不能跨過 `mcp-media-server` 重啟。生成檔案仍會寫入 `MEDIA_OUTPUT_DIR`。

| 環境變數 | 預設 | 意義 |
| --- | --- | --- |
| `MEDIA_SYNC_TIMEOUT_SEC` | `600` | 單次同步 media tool call 的最長執行秒數。非同步 jobs 使用 `MEDIA_JOB_TIMEOUT_SEC`。 |
| `MEDIA_JOB_TIMEOUT_SEC` | `900` | 非同步 media job 的最長執行秒數。 |
| `MEDIA_JOB_RETENTION_SEC` | `86400` | completed async job metadata 可被列出的保留秒數。 |
| `MEDIA_JOB_MAX_ACTIVE` | `4` | 單一 server process 中 queued 或 running async media jobs 的上限。設為 `0` 可停用限制。 |

## 維運注意事項

Media tools 會消耗外部 provider quota，也可能產生不適合所有 Discord channel 的檔案。建議預設停用，只在有明確用途的 channel 透過 `/mcp manage` 啟用。
