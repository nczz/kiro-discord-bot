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
| `MEDIA_DEFAULT_TTS_MODEL` | Override the default text-to-speech model. |

## Tools

| Tool | Purpose |
| --- | --- |
| `generate_image` | Generate an image from `prompt`, with optional `model`, `size`, and `aspect_ratio`. |
| `edit_image` | Edit an existing local image using `image_path` and `prompt`. |
| `generate_video` | Generate a video from `prompt`, optionally conditioned on `image_path`. |
| `generate_music` | Generate music from `prompt`, with optional `duration_sec`. |
| `text_to_speech` | Generate speech from `text`, with optional `model` and `voice`. |
| `list_models` | List available models, optionally filtered by `image`, `video`, `music`, or `tts`. |

Generated artifacts are returned as local file paths to the agent. Use a channel policy that matches how much media generation cost and egress you want to allow.

## Operational Notes

Media tools can spend external provider quota and may produce files that are not suitable for every Discord channel. Keep the media server disabled by default and enable it per channel through `/mcp manage` when the channel has a clear use case.
