package main

import (
	"context"
	"testing"
)

type fakeVideoProvider struct {
	models []ModelInfo
}

func (f fakeVideoProvider) GenerateVideo(ctx context.Context, prompt, model, imagePath string) (*MediaResult, error) {
	return &MediaResult{Path: "/tmp/video.mp4", MimeType: "video/mp4"}, nil
}

func (f fakeVideoProvider) VideoModels() []ModelInfo {
	return f.models
}

type fakeMusicProvider struct {
	models []ModelInfo
}

func (f fakeMusicProvider) GenerateMusic(ctx context.Context, prompt, model string, durationSec int) (*MediaResult, error) {
	return &MediaResult{Path: "/tmp/music.mp3", MimeType: "audio/mpeg"}, nil
}

func (f fakeMusicProvider) MusicModels() []ModelInfo {
	return f.models
}

func TestRegistryDefaultVideoAndMusicModelsAreDeterministic(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterVideo(fakeVideoProvider{models: []ModelInfo{
		{ID: "veo-3.1", Type: "video"},
		{ID: "veo-3.1-lite", Type: "video"},
	}})
	reg.RegisterMusic(fakeMusicProvider{models: []ModelInfo{
		{ID: "lyria-3-pro", Type: "music"},
		{ID: "lyria-3-clip", Type: "music"},
	}})

	_, videoModel, err := reg.Video("")
	if err != nil {
		t.Fatalf("default video model: %v", err)
	}
	if videoModel != "veo-3.1" {
		t.Fatalf("default video model = %q, want first registered model", videoModel)
	}

	_, musicModel, err := reg.Music("")
	if err != nil {
		t.Fatalf("default music model: %v", err)
	}
	if musicModel != "lyria-3-pro" {
		t.Fatalf("default music model = %q, want first registered model", musicModel)
	}
}

func TestGeminiRegistryDefaultsMatchToolContract(t *testing.T) {
	reg := NewRegistry()
	gemini := NewGemini("")
	reg.RegisterVideo(gemini)
	reg.RegisterMusic(gemini)

	_, videoModel, err := reg.Video("")
	if err != nil {
		t.Fatalf("default video model: %v", err)
	}
	if videoModel != "veo-3.1" {
		t.Fatalf("gemini default video model = %q, want veo-3.1", videoModel)
	}

	_, musicModel, err := reg.Music("")
	if err != nil {
		t.Fatalf("default music model: %v", err)
	}
	if musicModel != "lyria-3-clip" {
		t.Fatalf("gemini default music model = %q, want lyria-3-clip", musicModel)
	}
}

func TestRegistryDefaultVideoAndMusicModelsCanBeOverridden(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterVideo(fakeVideoProvider{models: []ModelInfo{
		{ID: "veo-3.1", Type: "video"},
		{ID: "veo-3.1-lite", Type: "video"},
	}})
	reg.RegisterMusic(fakeMusicProvider{models: []ModelInfo{
		{ID: "lyria-3-pro", Type: "music"},
		{ID: "lyria-3-clip", Type: "music"},
	}})
	reg.SetDefaults("", "veo-3.1-lite", "lyria-3-clip", "")

	_, videoModel, err := reg.Video("")
	if err != nil {
		t.Fatalf("default video model: %v", err)
	}
	if videoModel != "veo-3.1-lite" {
		t.Fatalf("default video model = %q, want override", videoModel)
	}

	_, musicModel, err := reg.Music("")
	if err != nil {
		t.Fatalf("default music model: %v", err)
	}
	if musicModel != "lyria-3-clip" {
		t.Fatalf("default music model = %q, want override", musicModel)
	}
}
