# Media MCP

`mcp-media-server` is an optional MCP server for media generation. It is not required for normal bot operation.

## Providers

The server registers providers from available API keys:

| API key | Enabled capabilities |
| --- | --- |
| `GEMINI_API_KEY` | Image generation, video generation, music generation, text to speech. |
| `OPENAI_API_KEY` | Image generation and text to speech. |

If no provider API key is set, the server exits at startup.

Optional defaults:

| Variable | Purpose |
| --- | --- |
| `MEDIA_DEFAULT_IMAGE_MODEL` | Override the default image model. |
| `MEDIA_DEFAULT_VIDEO_MODEL` | Override the default video model. |
| `MEDIA_DEFAULT_MUSIC_MODEL` | Override the default music model. |
| `MEDIA_DEFAULT_TTS_MODEL` | Override the default text-to-speech model. |
| `MEDIA_SYNC_TIMEOUT_SEC` | Maximum runtime for one synchronous media tool call. |

## Tools

| Tool | Purpose |
| --- | --- |
| `generate_image` | Generate an image synchronously from `prompt`, with optional `model`, `size`, and `aspect_ratio`. Use only for requests expected to finish inside the MCP client request timeout. |
| `start_image_generation` | Start an asynchronous image generation job and return a `job_id` immediately. |
| `start_image_edit` | Start an asynchronous image edit job using `image_path` and `prompt`. |
| `start_video_generation` | Start an asynchronous video generation job. Use this for long-running video models. |
| `start_music_generation` | Start an asynchronous music generation job. |
| `start_text_to_speech` | Start an asynchronous text-to-speech job. |
| `get_media_job` | Check an asynchronous media job. When `status=succeeded`, the response includes the local file path. |
| `list_media_jobs` | List recent asynchronous media jobs. |
| `cancel_media_job` | Cancel a queued or running asynchronous media job. |
| `edit_image` | Edit an existing local image synchronously using `image_path` and `prompt`. Slow edits should use `start_image_edit`. |
| `generate_video` | Generate a video synchronously from `prompt`, optionally conditioned on `image_path`. Most video requests should use `start_video_generation`. |
| `generate_music` | Generate music synchronously from `prompt`, with optional `duration_sec`. Longer music requests should use `start_music_generation`. |
| `text_to_speech` | Generate speech synchronously from `text`, with optional `model` and `voice`. Long text should use `start_text_to_speech`. |
| `list_models` | List available models, optionally filtered by `image`, `video`, `music`, or `tts`. |

Generated artifacts are returned as local file paths to the agent. Use a channel policy that matches how much media generation cost and egress you want to allow.

## Async Media Jobs

Some media models can take longer than the MCP client's request timeout. For those cases, use the `start_*` async tools instead of synchronous generation.

The flow is:

1. Call the matching async start tool with the same media arguments.
2. Store the returned `job_id`.
3. Poll `get_media_job` until `status` is `succeeded` or `failed`.
4. Use the returned local `Path` when the job succeeds.

Use async tools by default for video generation, high-resolution images, slower image models, long music generations, and long text-to-speech requests.

The server process keeps jobs in memory, so pending job state survives MCP request timeouts but not a restart of `mcp-media-server`. Generated files are still written under `MEDIA_OUTPUT_DIR`.

| Environment Variable | Default | Meaning |
| --- | --- | --- |
| `MEDIA_SYNC_TIMEOUT_SEC` | `600` | Maximum runtime for one synchronous media tool call. Async jobs use `MEDIA_JOB_TIMEOUT_SEC` instead. |
| `MEDIA_JOB_TIMEOUT_SEC` | `900` | Maximum runtime for an async media job. |
| `MEDIA_JOB_RETENTION_SEC` | `86400` | How long completed async job metadata remains listable. |
| `MEDIA_JOB_MAX_ACTIVE` | `4` | Maximum queued or running async media jobs in one server process. Set `0` to disable the limit. |

## Operational Notes

Media tools can spend external provider quota and may produce files that are not suitable for every Discord channel. Keep the media server disabled by default and enable it per channel through `/mcp manage` when the channel has a clear use case.
