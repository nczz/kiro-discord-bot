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
| `MEDIA_DEFAULT_TTS_MODEL` | 覆寫預設 text-to-speech model。 |

## Tools

| Tool | 用途 |
| --- | --- |
| `generate_image` | 依 `prompt` 生成圖片，可選 `model`、`size`、`aspect_ratio`。 |
| `edit_image` | 用 `image_path` 與 `prompt` 編輯既有本機圖片。 |
| `generate_video` | 依 `prompt` 生成影片，可選 `image_path` 作為條件。 |
| `generate_music` | 依 `prompt` 生成音樂，可選 `duration_sec`。 |
| `text_to_speech` | 依 `text` 生成語音，可選 `model` 與 `voice`。 |
| `list_models` | 列出可用 models，可用 `image`、`video`、`music`、`tts` 過濾。 |

生成結果會以本機檔案路徑回傳給 agent。請依照成本與 egress 風險設定 channel policy。

## 維運注意事項

Media tools 會消耗外部 provider quota，也可能產生不適合所有 Discord channel 的檔案。建議預設停用，只在有明確用途的 channel 透過 `/mcp manage` 啟用。
