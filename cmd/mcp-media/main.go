package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	reg := NewRegistry()

	// Register providers based on available API keys
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		g := NewGemini(key)
		reg.RegisterImage(g)
		reg.RegisterVideo(g)
		reg.RegisterMusic(g)
		reg.RegisterTTS(g)
		log.Println("[mcp-media] gemini provider registered")
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		o := NewOpenAI(key)
		reg.RegisterImage(o)
		reg.RegisterTTS(o)
		log.Println("[mcp-media] openai provider registered")
	}

	// Override defaults from env
	reg.SetDefaults(
		os.Getenv("MEDIA_DEFAULT_IMAGE_MODEL"),
		os.Getenv("MEDIA_DEFAULT_VIDEO_MODEL"),
		os.Getenv("MEDIA_DEFAULT_MUSIC_MODEL"),
		os.Getenv("MEDIA_DEFAULT_TTS_MODEL"),
	)

	if len(reg.Models()) == 0 {
		log.Fatal("[mcp-media] no providers configured — set GEMINI_API_KEY or OPENAI_API_KEY")
	}

	maxActiveJobs := envInt("MEDIA_JOB_MAX_ACTIVE", 4)
	jobs := NewMediaJobStore(envDuration("MEDIA_JOB_RETENTION_SEC", 24*time.Hour), maxActiveJobs)
	jobTimeout := envDuration("MEDIA_JOB_TIMEOUT_SEC", 15*time.Minute)
	syncTimeout := envDuration("MEDIA_SYNC_TIMEOUT_SEC", 10*time.Minute)

	s := newMediaServer(reg, jobs, jobTimeout, syncTimeout)

	log.Printf("[mcp-media] starting with %d models", len(reg.Models()))
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func newMediaServer(reg *Registry, jobs *MediaJobStore, jobTimeout, syncTimeout time.Duration) *server.MCPServer {
	s := server.NewMCPServer("mcp-media", "1.0.0", server.WithToolCapabilities(false))

	// --- generate_image ---
	s.AddTool(
		mcp.NewTool("generate_image",
			mcp.WithDescription("Generate an image synchronously from a text prompt. Returns the local file path. For slow models or high-resolution images, prefer start_image_generation and poll with get_media_job to avoid MCP client request timeouts."),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Image description")),
			mcp.WithString("model", mcp.Description("Model ID (use list_models to see options). Default: "+reg.defaultImage)),
			mcp.WithString("size", mcp.Description("Image size. Gemini: 512/1K/2K/4K. OpenAI: 1024x1024/1024x1792/1792x1024")),
			mcp.WithString("aspect_ratio", mcp.Description("Aspect ratio for Gemini models: 1:1, 16:9, 9:16, 3:2, 2:3, 4:3, 3:4, etc.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			prompt, _ := req.RequireString("prompt")
			model := req.GetString("model", "")
			size := req.GetString("size", "")
			ar := req.GetString("aspect_ratio", "")
			p, m, err := reg.Image(model)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			callCtx, cancel := boundedContext(ctx, syncTimeout)
			defer cancel()
			result, err := p.GenerateImage(callCtx, prompt, m, size, ar)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("[%s] %v", m, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Image saved: %s\nType: %s\nModel: %s", result.Path, result.MimeType, m)), nil
		},
	)

	// --- start_image_generation ---
	s.AddTool(
		mcp.NewTool("start_image_generation",
			mcp.WithDescription("Start an asynchronous image generation job and immediately return a job_id. Use this for slow image models, high-resolution outputs, or any request that might exceed the MCP client's request timeout. Poll with get_media_job until status is succeeded or failed."),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Image description")),
			mcp.WithString("model", mcp.Description("Model ID (use list_models to see options). Default: "+reg.defaultImage)),
			mcp.WithString("size", mcp.Description("Image size. Gemini: 512/1K/2K/4K. OpenAI: 1024x1024/1024x1792/1792x1024")),
			mcp.WithString("aspect_ratio", mcp.Description("Aspect ratio for Gemini models: 1:1, 16:9, 9:16, 3:2, 2:3, 4:3, 3:4, etc.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			prompt, _ := req.RequireString("prompt")
			model := req.GetString("model", "")
			size := req.GetString("size", "")
			ar := req.GetString("aspect_ratio", "")
			p, m, err := reg.Image(model)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			job, err := jobs.Start("image", prompt, m, jobTimeout, func(jobCtx context.Context) (*MediaResult, error) {
				return p.GenerateImage(jobCtx, prompt, m, size, ar)
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(formatMediaJobStarted(job)), nil
		},
	)

	// --- start_image_edit ---
	s.AddTool(
		mcp.NewTool("start_image_edit",
			mcp.WithDescription("Start an asynchronous image edit job and immediately return a job_id. Poll with get_media_job until status is succeeded or failed."),
			mcp.WithString("image_path", mcp.Required(), mcp.Description("Absolute path to the source image")),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Edit instructions")),
			mcp.WithString("model", mcp.Description("Model ID. Default: "+reg.defaultImage)),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			imgPath, _ := req.RequireString("image_path")
			prompt, _ := req.RequireString("prompt")
			model := req.GetString("model", "")
			p, m, err := reg.Image(model)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			job, err := jobs.Start("image_edit", prompt, m, jobTimeout, func(jobCtx context.Context) (*MediaResult, error) {
				return p.EditImage(jobCtx, imgPath, prompt, m)
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(formatMediaJobStarted(job)), nil
		},
	)

	// --- start_video_generation ---
	s.AddTool(
		mcp.NewTool("start_video_generation",
			mcp.WithDescription("Start an asynchronous video generation job and immediately return a job_id. Use this instead of synchronous generate_video for long-running video models. Poll with get_media_job until status is succeeded or failed."),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Video description")),
			mcp.WithString("model", mcp.Description("Model ID. Default: "+reg.defaultVideo)),
			mcp.WithString("image_path", mcp.Description("Optional source image for image-to-video")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			prompt, _ := req.RequireString("prompt")
			model := req.GetString("model", "")
			imgPath := req.GetString("image_path", "")
			p, m, err := reg.Video(model)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			job, err := jobs.Start("video", prompt, m, jobTimeout, func(jobCtx context.Context) (*MediaResult, error) {
				return p.GenerateVideo(jobCtx, prompt, m, imgPath)
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(formatMediaJobStarted(job)), nil
		},
	)

	// --- start_music_generation ---
	s.AddTool(
		mcp.NewTool("start_music_generation",
			mcp.WithDescription("Start an asynchronous music generation job and immediately return a job_id. Poll with get_media_job until status is succeeded or failed."),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Music description")),
			mcp.WithString("model", mcp.Description("Model ID. Default: "+reg.defaultMusic)),
			mcp.WithNumber("duration_sec", mcp.Description("Duration in seconds (default: 30)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			prompt, _ := req.RequireString("prompt")
			model := req.GetString("model", "")
			dur := int(req.GetFloat("duration_sec", 30))
			p, m, err := reg.Music(model)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			job, err := jobs.Start("music", prompt, m, jobTimeout, func(jobCtx context.Context) (*MediaResult, error) {
				return p.GenerateMusic(jobCtx, prompt, m, dur)
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(formatMediaJobStarted(job)), nil
		},
	)

	// --- start_text_to_speech ---
	s.AddTool(
		mcp.NewTool("start_text_to_speech",
			mcp.WithDescription("Start an asynchronous text-to-speech job and immediately return a job_id. Poll with get_media_job until status is succeeded or failed."),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to speak")),
			mcp.WithString("model", mcp.Description("Model ID. Default: "+reg.defaultTTS)),
			mcp.WithString("voice", mcp.Description("Voice name. OpenAI: alloy/echo/fable/onyx/nova/shimmer. Gemini: Puck/Charon/Kore/Fenrir/Aoede")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			text, _ := req.RequireString("text")
			model := req.GetString("model", "")
			voice := req.GetString("voice", "")
			p, m, err := reg.TTS(model)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			job, err := jobs.Start("tts", text, m, jobTimeout, func(jobCtx context.Context) (*MediaResult, error) {
				return p.TextToSpeech(jobCtx, text, m, voice)
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(formatMediaJobStarted(job)), nil
		},
	)

	// --- get_media_job ---
	s.AddTool(
		mcp.NewTool("get_media_job",
			mcp.WithDescription("Get the status of an asynchronous media job. When status is succeeded, the result includes the local file path."),
			mcp.WithString("job_id", mcp.Required(), mcp.Description("Job ID returned by a start_* media job tool")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, _ := req.RequireString("job_id")
			job, ok := jobs.Get(id)
			if !ok {
				return mcp.NewToolResultError("media job not found"), nil
			}
			return mcp.NewToolResultText(formatMediaJob(job)), nil
		},
	)

	// --- list_media_jobs ---
	s.AddTool(
		mcp.NewTool("list_media_jobs",
			mcp.WithDescription("List recent asynchronous media jobs."),
			mcp.WithNumber("limit", mcp.Description("Maximum number of jobs to return, default 20, max 50")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			limit := int(req.GetFloat("limit", 20))
			return mcp.NewToolResultText(formatMediaJobList(jobs.List(limit))), nil
		},
	)

	// --- cancel_media_job ---
	s.AddTool(
		mcp.NewTool("cancel_media_job",
			mcp.WithDescription("Cancel an asynchronous media job if it is still queued or running."),
			mcp.WithString("job_id", mcp.Required(), mcp.Description("Job ID returned by a start_* media job tool")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, _ := req.RequireString("job_id")
			job, ok := jobs.Cancel(id)
			if !ok {
				return mcp.NewToolResultError("media job not found"), nil
			}
			return mcp.NewToolResultText(formatMediaJob(job)), nil
		},
	)

	// --- edit_image ---
	s.AddTool(
		mcp.NewTool("edit_image",
			mcp.WithDescription("Edit an existing image synchronously using natural language instructions. Returns the local file path. For slow edits, prefer start_image_edit and poll with get_media_job."),
			mcp.WithString("image_path", mcp.Required(), mcp.Description("Absolute path to the source image")),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Edit instructions (e.g. 'remove the background', 'change the color to blue')")),
			mcp.WithString("model", mcp.Description("Model ID. Default: "+reg.defaultImage)),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			imgPath, _ := req.RequireString("image_path")
			prompt, _ := req.RequireString("prompt")
			model := req.GetString("model", "")
			p, m, err := reg.Image(model)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			callCtx, cancel := boundedContext(ctx, syncTimeout)
			defer cancel()
			result, err := p.EditImage(callCtx, imgPath, prompt, m)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("[%s] %v", m, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Edited image saved: %s\nType: %s\nModel: %s", result.Path, result.MimeType, m)), nil
		},
	)

	// --- generate_video ---
	s.AddTool(
		mcp.NewTool("generate_video",
			mcp.WithDescription("Generate a video synchronously from text and/or an image. Returns the local file path. For most video requests, prefer start_video_generation and poll with get_media_job to avoid MCP client request timeouts."),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Video description")),
			mcp.WithString("model", mcp.Description("Model ID. Default: "+reg.defaultVideo)),
			mcp.WithString("image_path", mcp.Description("Optional source image for image-to-video")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			prompt, _ := req.RequireString("prompt")
			model := req.GetString("model", "")
			imgPath := req.GetString("image_path", "")
			p, m, err := reg.Video(model)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			callCtx, cancel := boundedContext(ctx, syncTimeout)
			defer cancel()
			result, err := p.GenerateVideo(callCtx, prompt, m, imgPath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("[%s] %v", m, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Video saved: %s\nType: %s\nModel: %s", result.Path, result.MimeType, m)), nil
		},
	)

	// --- generate_music ---
	s.AddTool(
		mcp.NewTool("generate_music",
			mcp.WithDescription("Generate a music track synchronously from a text description. Returns the local file path. For longer music jobs, prefer start_music_generation and poll with get_media_job."),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Music description (genre, mood, instruments, etc.)")),
			mcp.WithString("model", mcp.Description("Model ID. Default: "+reg.defaultMusic)),
			mcp.WithNumber("duration_sec", mcp.Description("Duration in seconds (default: 30)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			prompt, _ := req.RequireString("prompt")
			model := req.GetString("model", "")
			dur := int(req.GetFloat("duration_sec", 30))
			p, m, err := reg.Music(model)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			callCtx, cancel := boundedContext(ctx, syncTimeout)
			defer cancel()
			result, err := p.GenerateMusic(callCtx, prompt, m, dur)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("[%s] %v", m, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Music saved: %s\nType: %s\nModel: %s", result.Path, result.MimeType, m)), nil
		},
	)

	// --- text_to_speech ---
	s.AddTool(
		mcp.NewTool("text_to_speech",
			mcp.WithDescription("Convert text to speech audio synchronously. Returns the local file path. For long text, prefer start_text_to_speech and poll with get_media_job."),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to speak")),
			mcp.WithString("model", mcp.Description("Model ID. Default: "+reg.defaultTTS)),
			mcp.WithString("voice", mcp.Description("Voice name. OpenAI: alloy/echo/fable/onyx/nova/shimmer. Gemini: Puck/Charon/Kore/Fenrir/Aoede")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			text, _ := req.RequireString("text")
			model := req.GetString("model", "")
			voice := req.GetString("voice", "")
			p, m, err := reg.TTS(model)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			callCtx, cancel := boundedContext(ctx, syncTimeout)
			defer cancel()
			result, err := p.TextToSpeech(callCtx, text, m, voice)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("[%s] %v", m, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Audio saved: %s\nType: %s\nModel: %s", result.Path, result.MimeType, m)), nil
		},
	)

	// --- list_models ---
	s.AddTool(
		mcp.NewTool("list_models",
			mcp.WithDescription("List all available media generation models"),
			mcp.WithString("type", mcp.Description("Filter by type: image, video, music, tts (empty = all)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			filter := req.GetString("type", "")
			var models []ModelInfo
			for _, m := range reg.Models() {
				if filter == "" || m.Type == filter {
					models = append(models, m)
				}
			}
			if len(models) == 0 {
				return mcp.NewToolResultText("No models available."), nil
			}
			// Group by type
			grouped := map[string][]ModelInfo{}
			for _, m := range models {
				grouped[m.Type] = append(grouped[m.Type], m)
			}
			var lines []string
			for _, t := range []string{"image", "video", "music", "tts"} {
				ms := grouped[t]
				if len(ms) == 0 {
					continue
				}
				lines = append(lines, fmt.Sprintf("## %s", strings.ToUpper(t)))
				for _, m := range ms {
					def := ""
					if (t == "image" && m.ID == reg.defaultImage) || (t == "tts" && m.ID == reg.defaultTTS) {
						def = " ★default"
					}
					cost := ""
					if m.CostTier != "" {
						cost = " " + m.CostTier
					}
					line := fmt.Sprintf("  %s — %s [%s]%s%s", m.ID, m.Name, m.Provider, cost, def)
					if m.Description != "" {
						line += "\n    " + m.Description
					}
					lines = append(lines, line)
				}
			}
			return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
		},
	)

	return s
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	sec, err := strconv.Atoi(raw)
	if err != nil || sec <= 0 {
		return fallback
	}
	return time.Duration(sec) * time.Second
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return parent, func() {}
	}
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= timeout {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}
