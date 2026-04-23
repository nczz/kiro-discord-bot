package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MediaResult is the unified output from any generation call.
type MediaResult struct {
	Path     string
	MimeType string
}

// ImageProvider generates or edits images.
type ImageProvider interface {
	GenerateImage(ctx context.Context, prompt, model, size, aspectRatio string) (*MediaResult, error)
	EditImage(ctx context.Context, imagePath, prompt, model string) (*MediaResult, error)
	ImageModels() []ModelInfo
}

// VideoProvider generates videos.
type VideoProvider interface {
	GenerateVideo(ctx context.Context, prompt, model, imagePath string) (*MediaResult, error)
	VideoModels() []ModelInfo
}

// MusicProvider generates music.
type MusicProvider interface {
	GenerateMusic(ctx context.Context, prompt, model string, durationSec int) (*MediaResult, error)
	MusicModels() []ModelInfo
}

// TTSProvider converts text to speech.
type TTSProvider interface {
	TextToSpeech(ctx context.Context, text, model, voice string) (*MediaResult, error)
	TTSModels() []ModelInfo
}

// ModelInfo describes an available model.
type ModelInfo struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	Type        string `json:"type"` // image, video, music, tts
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CostTier    string `json:"cost_tier,omitempty"` // "$" cheap, "$$" mid, "$$$" premium
}

// --- helpers ---

var outputDir string

func init() {
	outputDir = os.Getenv("MEDIA_OUTPUT_DIR")
	if outputDir == "" {
		outputDir = filepath.Join(os.TempDir(), "mcp-media")
	}
	os.MkdirAll(outputDir, 0755)
}

func saveBytes(data []byte, ext string) (string, error) {
	name := fmt.Sprintf("%s.%s", time.Now().Format("20060102-150405.000"), ext)
	p := filepath.Join(outputDir, name)
	if err := os.WriteFile(p, data, 0644); err != nil {
		return "", err
	}
	abs, _ := filepath.Abs(p)
	return abs, nil
}

func saveBase64(b64, ext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	return saveBytes(data, ext)
}

func downloadToFile(url, ext string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return saveBytes(data, ext)
}

func extFromMime(mime string) string {
	switch {
	case strings.Contains(mime, "png"):
		return "png"
	case strings.Contains(mime, "jpeg"), strings.Contains(mime, "jpg"):
		return "jpg"
	case strings.Contains(mime, "webp"):
		return "webp"
	case strings.Contains(mime, "mp4"):
		return "mp4"
	case strings.Contains(mime, "mp3"), strings.Contains(mime, "mpeg"):
		return "mp3"
	case strings.Contains(mime, "wav"):
		return "wav"
	case strings.Contains(mime, "ogg"):
		return "ogg"
	default:
		return "bin"
	}
}
