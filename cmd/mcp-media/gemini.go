package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// GeminiProvider uses the Gemini REST API for image, video, music, and TTS.
type GeminiProvider struct {
	apiKey string
	client *http.Client
}

func NewGemini(apiKey string) *GeminiProvider {
	return &GeminiProvider{apiKey: apiKey, client: &http.Client{}}
}

func (g *GeminiProvider) ImageModels() []ModelInfo {
	return []ModelInfo{
		{ID: "nano-banana-2", Provider: "gemini", Type: "image", Name: "Nano Banana 2 (Gemini 3.1 Flash Image)",
			Description: "High-efficiency image generation and editing, optimized for speed and high-volume use cases. #1 on Image Arena",
			CostTier:    "$$"},
		{ID: "nano-banana-pro", Provider: "gemini", Type: "image", Name: "Nano Banana Pro (Gemini 3 Pro Image)",
			Description: "Studio-quality 4K visuals with precise text rendering and complex layouts. Best quality among Gemini image models",
			CostTier:    "$$$"},
	}
}

func (g *GeminiProvider) VideoModels() []ModelInfo {
	return []ModelInfo{
		{ID: "veo-3.1", Provider: "gemini", Type: "video", Name: "Veo 3.1",
			Description: "State-of-the-art cinematic video with synchronized audio, up to 4K. Best quality",
			CostTier:    "$$$"},
		{ID: "veo-3.1-lite", Provider: "gemini", Type: "video", Name: "Veo 3.1 Lite",
			Description: "High-efficiency video generation at lower cost. Good for prototyping and high-volume use",
			CostTier:    "$$"},
	}
}

func (g *GeminiProvider) MusicModels() []ModelInfo {
	return []ModelInfo{
		{ID: "lyria-3-pro", Provider: "gemini", Type: "music", Name: "Lyria 3 Pro",
			Description: "Full-length songs up to 3 min with verse/chorus/bridge structure, 44.1kHz stereo",
			CostTier:    "$$"},
		{ID: "lyria-3-clip", Provider: "gemini", Type: "music", Name: "Lyria 3 Clip",
			Description: "30-second music clips, loops, and previews. Fast and cost-efficient",
			CostTier:    "$"},
	}
}

func (g *GeminiProvider) TTSModels() []ModelInfo {
	return []ModelInfo{
		{ID: "gemini-tts", Provider: "gemini", Type: "tts", Name: "Gemini 3.1 Flash TTS",
			Description: "Low-latency speech generation with natural output and expressive audio tags",
			CostTier:    "$"},
	}
}

var geminiModelMap = map[string]string{
	"nano-banana-2":   "gemini-3.1-flash-image-preview",
	"nano-banana-pro": "gemini-3-pro-image-preview",
	"veo-3.1":         "veo-3.1-generate-preview",
	"veo-3.1-lite":    "veo-3.1-lite-generate-preview",
	"lyria-3-pro":     "lyria-3-pro-preview",
	"lyria-3-clip":    "lyria-3-clip-preview",
	"gemini-tts":      "gemini-3.1-flash-tts-preview",
}

func (g *GeminiProvider) apiModel(id string) string {
	if m, ok := geminiModelMap[id]; ok {
		return m
	}
	return id
}

// --- Image ---

func (g *GeminiProvider) GenerateImage(ctx context.Context, prompt, model, size, aspectRatio string) (*MediaResult, error) {
	if aspectRatio == "" {
		aspectRatio = "1:1"
	}
	if size == "" {
		size = "1K"
	}
	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"IMAGE"},
			"imageConfig":        map[string]string{"aspectRatio": aspectRatio, "imageSize": size},
		},
	}
	return g.generate(ctx, g.apiModel(model), body)
}

func (g *GeminiProvider) EditImage(ctx context.Context, imagePath, prompt, model string) (*MediaResult, error) {
	imgData, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	mime := "image/png"
	if ext := extFromMime(imagePath); ext == "jpg" {
		mime = "image/jpeg"
	}
	b64 := base64.StdEncoding.EncodeToString(imgData)
	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": []interface{}{
				map[string]interface{}{"inline_data": map[string]string{"mime_type": mime, "data": b64}},
				map[string]string{"text": prompt},
			}},
		},
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"TEXT", "IMAGE"},
		},
	}
	return g.generate(ctx, g.apiModel(model), body)
}

// --- Video ---

func (g *GeminiProvider) GenerateVideo(ctx context.Context, prompt, model, imagePath string) (*MediaResult, error) {
	instance := map[string]interface{}{"prompt": prompt}
	if imagePath != "" {
		imgData, err := os.ReadFile(imagePath)
		if err != nil {
			return nil, fmt.Errorf("read image: %w", err)
		}
		mime := mimeFromPath(imagePath)
		instance["image"] = map[string]string{
			"bytesBase64Encoded": base64.StdEncoding.EncodeToString(imgData),
			"mimeType":           mime,
		}
	}
	body := map[string]interface{}{
		"instances":  []interface{}{instance},
		"parameters": map[string]interface{}{"aspectRatio": "16:9", "resolution": "720p", "durationSeconds": 8, "sampleCount": 1},
	}

	apiModel := g.apiModel(model)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:predictLongRunning?key=%s", apiModel, g.apiKey)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read veo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("veo submit %d: %s", resp.StatusCode, truncStr(string(respBody), 500))
	}

	var op struct{ Name string }
	if err := json.Unmarshal(respBody, &op); err != nil || op.Name == "" {
		return nil, fmt.Errorf("no operation name: %s", truncStr(string(respBody), 300))
	}

	return g.pollVideo(ctx, op.Name)
}

// pollVideo polls a predictLongRunning operation until completion and returns the video.
func (g *GeminiProvider) pollVideo(ctx context.Context, opName string) (*MediaResult, error) {
	pollURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/%s?key=%s", opName, g.apiKey)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for i := 0; i < 120; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}

		pr, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
		if err != nil {
			return nil, err
		}
		pResp, err := g.client.Do(pr)
		if err != nil {
			return nil, err
		}
		pBody, err := io.ReadAll(pResp.Body)
		pResp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read poll response: %w", err)
		}
		if pResp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("veo poll %d: %s", pResp.StatusCode, truncStr(string(pBody), 500))
		}

		var status struct {
			Done  bool                    `json:"done"`
			Error *struct{ Message string } `json:"error"`
		}
		if err := json.Unmarshal(pBody, &status); err != nil {
			continue
		}
		if !status.Done {
			continue
		}
		if status.Error != nil {
			return nil, fmt.Errorf("veo error: %s", status.Error.Message)
		}

		return g.parseVideoResponse(ctx, pBody)
	}
	return nil, fmt.Errorf("veo timeout after 10 minutes")
}

// parseVideoResponse extracts the video URI from a completed operation response.
func (g *GeminiProvider) parseVideoResponse(ctx context.Context, body []byte) (*MediaResult, error) {
	var op struct {
		Response struct {
			GenerateVideoResponse struct {
				Samples []struct {
					Video struct {
						URI string `json:"uri"`
					} `json:"video"`
				} `json:"generatedSamples"`
			} `json:"generateVideoResponse"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &op); err != nil {
		return nil, fmt.Errorf("parse video response: %w", err)
	}
	for _, s := range op.Response.GenerateVideoResponse.Samples {
		if s.Video.URI != "" {
			path, err := g.downloadVideo(ctx, s.Video.URI)
			if err != nil {
				return nil, err
			}
			return &MediaResult{Path: path, MimeType: "video/mp4"}, nil
		}
	}
	return nil, fmt.Errorf("veo done but no video URI in response: %s", truncStr(string(body), 500))
}

func (g *GeminiProvider) downloadVideo(ctx context.Context, uri string) (string, error) {
	dlURL := uri
	if !strings.Contains(dlURL, "key=") {
		sep := "?"
		if strings.Contains(dlURL, "?") {
			sep = "&"
		}
		dlURL += sep + "key=" + g.apiKey
	}
	req, err := http.NewRequestWithContext(ctx, "GET", dlURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) // best-effort for error message
		return "", fmt.Errorf("download video %d: %s", resp.StatusCode, truncStr(string(body), 300))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return saveBytes(data, "mp4")
}

// --- Music ---

func (g *GeminiProvider) GenerateMusic(ctx context.Context, prompt, model string, durationSec int) (*MediaResult, error) {
	if durationSec <= 0 {
		durationSec = 30
	}
	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": []map[string]string{{"text": fmt.Sprintf("Generate a %d second music track: %s", durationSec, prompt)}}},
		},
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"AUDIO"},
		},
	}
	return g.generate(ctx, g.apiModel(model), body)
}

// --- TTS ---

func (g *GeminiProvider) TextToSpeech(ctx context.Context, text, model, voice string) (*MediaResult, error) {
	speechCfg := map[string]interface{}{}
	if voice != "" {
		speechCfg["voiceConfig"] = map[string]interface{}{
			"prebuiltVoiceConfig": map[string]string{"voiceName": voice},
		}
	}
	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": []map[string]string{{"text": text}}},
		},
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"AUDIO"},
			"speechConfig":       speechCfg,
		},
	}
	return g.generate(ctx, g.apiModel(model), body)
}

// --- common ---

func (g *GeminiProvider) generate(ctx context.Context, model string, body map[string]interface{}) (*MediaResult, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, g.apiKey)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini %d: %s", resp.StatusCode, truncStr(string(respBody), 500))
	}

	var result geminiResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	for _, cand := range result.Candidates {
		for _, part := range cand.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				ext := extFromMime(part.InlineData.MimeType)
				path, err := saveBase64(part.InlineData.Data, ext)
				if err != nil {
					return nil, fmt.Errorf("save: %w", err)
				}
				return &MediaResult{Path: path, MimeType: part.InlineData.MimeType}, nil
			}
		}
	}
	for _, cand := range result.Candidates {
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				return nil, fmt.Errorf("no media generated: %s", truncStr(part.Text, 300))
			}
		}
	}
	return nil, fmt.Errorf("no media in response")
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text       string `json:"text,omitempty"`
				InlineData *struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"`
				} `json:"inlineData,omitempty"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func mimeFromPath(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	default:
		return "image/png"
	}
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
