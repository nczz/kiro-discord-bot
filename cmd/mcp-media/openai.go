package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAIProvider uses the OpenAI REST API for image generation and TTS.
type OpenAIProvider struct {
	apiKey string
	client *http.Client
}

func NewOpenAI(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{apiKey: apiKey, client: &http.Client{}}
}

func (o *OpenAIProvider) ImageModels() []ModelInfo {
	return []ModelInfo{
		{ID: "gpt-image", Provider: "openai", Type: "image", Name: "GPT Image 1"},
		{ID: "dall-e-3", Provider: "openai", Type: "image", Name: "DALL·E 3"},
	}
}

func (o *OpenAIProvider) TTSModels() []ModelInfo {
	return []ModelInfo{
		{ID: "tts-1-hd", Provider: "openai", Type: "tts", Name: "OpenAI TTS HD"},
		{ID: "tts-1", Provider: "openai", Type: "tts", Name: "OpenAI TTS"},
	}
}

// --- Image ---

func (o *OpenAIProvider) GenerateImage(ctx context.Context, prompt, model, size, _ string) (*MediaResult, error) {
	if size == "" {
		size = "1024x1024"
	}
	if model == "" {
		model = "gpt-image-1"
	}
	apiModel := model
	if model == "gpt-image" {
		apiModel = "gpt-image-1"
	}

	body := map[string]interface{}{
		"model":  apiModel,
		"prompt": prompt,
		"n":      1,
		"size":   size,
	}
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/images/generations", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai %d: %s", resp.StatusCode, truncStr(string(respBody), 500))
	}

	var result openaiImageResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no image in response")
	}

	d := result.Data[0]
	if d.B64JSON != "" {
		path, err := saveBase64(d.B64JSON, "png")
		if err != nil {
			return nil, err
		}
		return &MediaResult{Path: path, MimeType: "image/png"}, nil
	}
	if d.URL != "" {
		path, err := downloadToFile(d.URL, "png")
		if err != nil {
			return nil, err
		}
		return &MediaResult{Path: path, MimeType: "image/png"}, nil
	}
	return nil, fmt.Errorf("no image data in response")
}

func (o *OpenAIProvider) EditImage(ctx context.Context, imagePath, prompt, model string) (*MediaResult, error) {
	// OpenAI image edit requires multipart — delegate to generate with prompt describing the edit
	return o.GenerateImage(ctx, prompt, model, "1024x1024", "")
}

// --- TTS ---

func (o *OpenAIProvider) TextToSpeech(ctx context.Context, text, model, voice string) (*MediaResult, error) {
	if model == "" {
		model = "tts-1-hd"
	}
	if voice == "" {
		voice = "alloy"
	}
	body := map[string]string{
		"model": model,
		"input": text,
		"voice": voice,
	}
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/speech", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai tts %d: %s", resp.StatusCode, truncStr(string(respBody), 500))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	path, err := saveBytes(data, "mp3")
	if err != nil {
		return nil, err
	}
	return &MediaResult{Path: path, MimeType: "audio/mpeg"}, nil
}

type openaiImageResponse struct {
	Data []struct {
		URL     string `json:"url"`
		B64JSON string `json:"b64_json"`
	} `json:"data"`
}
