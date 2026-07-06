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
| `MEDIA_SYNC_WAIT_SEC` | How long legacy `generate_*` tools wait for an immediate result before returning a `job_id`. |
| `MEDIA_SYNC_TIMEOUT_SEC` | Maximum runtime for a managed job started by a legacy `generate_*` tool. |

## Tools

| Tool | Purpose |
| --- | --- |
| `generate_image` | Generate an image through a managed job. Returns a local path when it finishes quickly, or a `job_id` when it is still running. |
| `start_image_generation` | Start an asynchronous image generation job and return a `job_id` immediately. |
| `start_image_edit` | Start an asynchronous image edit job using `image_path` and `prompt`. |
| `start_video_generation` | Start an asynchronous video generation job and return a `job_id` immediately. |
| `start_music_generation` | Start an asynchronous music generation job. |
| `start_text_to_speech` | Start an asynchronous text-to-speech job. |
| `get_media_job` | Check an asynchronous media job. When `status=succeeded`, the response includes the local file path. |
| `list_media_jobs` | List recent asynchronous media jobs. |
| `cancel_media_job` | Cancel a queued or running asynchronous media job. |
| `edit_image` | Edit an existing local image through a managed job. Returns a local path when it finishes quickly, or a `job_id` when it is still running. |
| `generate_video` | Generate a video through a managed job. Returns a local path when it finishes quickly, or a `job_id` when it is still running. |
| `generate_music` | Generate music through a managed job. Returns a local path when it finishes quickly, or a `job_id` when it is still running. |
| `text_to_speech` | Generate speech through a managed job. Returns a local path when it finishes quickly, or a `job_id` when it is still running. |
| `list_models` | List available models, optionally filtered by `image`, `video`, `music`, or `tts`. |

Generated artifacts are returned as local file paths to the agent. Use a channel policy that matches how much media generation cost and egress you want to allow.

## Managed And Async Media Jobs

Media generation can take longer than the MCP client's request timeout. The legacy entrypoints (`generate_image`, `edit_image`, `generate_video`, `generate_music`, and `text_to_speech`) are managed facades: they start a tracked job, wait briefly for a quick result, and return either the saved local path or a `job_id` for polling.

This avoids wasting provider quota on a failed synchronous attempt. If a legacy tool returns a `job_id`, keep polling that job with `get_media_job`; do not start another generation for the same request.

The explicit `start_*` async tools are still available when the agent or operator already wants job-style control from the beginning.

The flow is:

1. Call a legacy `generate_*` tool or the matching `start_*` tool with the same media arguments.
2. Store the returned `job_id`.
3. Poll `get_media_job` until `status` is `succeeded` or `failed`.
4. Use the returned local `Path` when the job succeeds.

The server process keeps jobs in memory, so pending job state survives MCP request timeouts but not a restart of `mcp-media-server`. Generated files are still written under `MEDIA_OUTPUT_DIR`.

| Environment Variable | Default | Meaning |
| --- | --- | --- |
| `MEDIA_SYNC_WAIT_SEC` | `20` | How long a legacy tool waits for an immediate result before returning a `job_id`. Keep this below the MCP client's request timeout. |
| `MEDIA_SYNC_TIMEOUT_SEC` | `600` | Maximum runtime for a managed job started by a legacy `generate_*` tool. Explicit async jobs use `MEDIA_JOB_TIMEOUT_SEC` instead. |
| `MEDIA_JOB_TIMEOUT_SEC` | `900` | Maximum runtime for an async media job. |
| `MEDIA_JOB_RETENTION_SEC` | `86400` | How long completed async job metadata remains listable. |
| `MEDIA_JOB_MAX_ACTIVE` | `4` | Maximum queued or running async media jobs in one server process. Set `0` to disable the limit. |

## Operational Notes

Media tools can spend external provider quota and may produce files that are not suitable for every Discord channel. Keep the media server disabled by default and enable it per channel through `/mcp manage` when the channel has a clear use case.
