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
		{ID: "nano-banana-2", Provider: "gemini", Type: "image", Name: "Nano Banana 2 (Gemini 3.1 Flash Image)"},
		{ID: "nano-banana-pro", Provider: "gemini", Type: "image", Name: "Nano Banana Pro (Gemini 3 Pro Image)"},
		{ID: "nano-banana", Provider: "gemini", Type: "image", Name: "Nano Banana (Gemini 2.5 Flash Image)"},
	}
}

func (g *GeminiProvider) VideoModels() []ModelInfo {
	return []ModelInfo{
		{ID: "veo-3.1", Provider: "gemini", Type: "video", Name: "Veo 3.1"},
	}
}

func (g *GeminiProvider) MusicModels() []ModelInfo {
	return []ModelInfo{
		{ID: "lyria", Provider: "gemini", Type: "music", Name: "Lyria (Gemini Music)"},
	}
}

func (g *GeminiProvider) TTSModels() []ModelInfo {
	return []ModelInfo{
		{ID: "gemini-tts", Provider: "gemini", Type: "tts", Name: "Gemini TTS"},
	}
}

var geminiModelMap = map[string]string{
	"nano-banana-2":   "gemini-3.1-flash-image-preview",
	"nano-banana-pro": "gemini-3-pro-image-preview",
	"nano-banana":     "gemini-2.5-flash-image",
	"veo-3.1":         "veo-3.1-generate-preview",
	"lyria":           "lyria-realtime-exp",
	"gemini-tts":      "gemini-2.5-flash",
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
	parts := []interface{}{map[string]string{"text": prompt}}
	if imagePath != "" {
		imgData, err := os.ReadFile(imagePath)
		if err != nil {
			return nil, fmt.Errorf("read image: %w", err)
		}
		b64 := base64.StdEncoding.EncodeToString(imgData)
		parts = append([]interface{}{
			map[string]interface{}{"inline_data": map[string]string{"mime_type": "image/png", "data": b64}},
		}, parts...)
	}
	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": parts},
		},
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"VIDEO"},
		},
	}
	return g.generate(ctx, g.apiModel(model), body)
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
	jsonBody, _ := json.Marshal(body)
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
	respBody, _ := io.ReadAll(resp.Body)
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

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
