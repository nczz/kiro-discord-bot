package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

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
	reg.SetDefaults(os.Getenv("MEDIA_DEFAULT_IMAGE_MODEL"), os.Getenv("MEDIA_DEFAULT_TTS_MODEL"))

	if len(reg.Models()) == 0 {
		log.Fatal("[mcp-media] no providers configured — set GEMINI_API_KEY or OPENAI_API_KEY")
	}

	s := server.NewMCPServer("mcp-media", "1.0.0", server.WithToolCapabilities(false))

	// --- generate_image ---
	s.AddTool(
		mcp.NewTool("generate_image",
			mcp.WithDescription("Generate an image from a text prompt. Returns the local file path of the generated image."),
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
			result, err := p.GenerateImage(ctx, prompt, m, size, ar)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("[%s] %v", m, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Image saved: %s\nType: %s\nModel: %s", result.Path, result.MimeType, m)), nil
		},
	)

	// --- edit_image ---
	s.AddTool(
		mcp.NewTool("edit_image",
			mcp.WithDescription("Edit an existing image using natural language instructions. Returns the local file path."),
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
			result, err := p.EditImage(ctx, imgPath, prompt, m)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("[%s] %v", m, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Edited image saved: %s\nType: %s\nModel: %s", result.Path, result.MimeType, m)), nil
		},
	)

	// --- generate_video ---
	s.AddTool(
		mcp.NewTool("generate_video",
			mcp.WithDescription("Generate a video from text and/or an image. Returns the local file path."),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Video description")),
			mcp.WithString("model", mcp.Description("Model ID (default: veo-3.1)")),
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
			result, err := p.GenerateVideo(ctx, prompt, m, imgPath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("[%s] %v", m, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Video saved: %s\nType: %s\nModel: %s", result.Path, result.MimeType, m)), nil
		},
	)

	// --- generate_music ---
	s.AddTool(
		mcp.NewTool("generate_music",
			mcp.WithDescription("Generate a music track from a text description. Returns the local file path."),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("Music description (genre, mood, instruments, etc.)")),
			mcp.WithString("model", mcp.Description("Model ID (default: lyria-3-clip)")),
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
			result, err := p.GenerateMusic(ctx, prompt, m, dur)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("[%s] %v", m, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Music saved: %s\nType: %s\nModel: %s", result.Path, result.MimeType, m)), nil
		},
	)

	// --- text_to_speech ---
	s.AddTool(
		mcp.NewTool("text_to_speech",
			mcp.WithDescription("Convert text to speech audio. Returns the local file path."),
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
			result, err := p.TextToSpeech(ctx, text, m, voice)
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

	log.Printf("[mcp-media] starting with %d models", len(reg.Models()))
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
