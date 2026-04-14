package main

import "fmt"

// Registry routes model IDs to providers.
type Registry struct {
	imageProviders map[string]ImageProvider
	videoProviders map[string]VideoProvider
	musicProviders map[string]MusicProvider
	ttsProviders   map[string]TTSProvider
	allModels      []ModelInfo
	defaultImage   string
	defaultTTS     string
}

func NewRegistry() *Registry {
	return &Registry{
		imageProviders: make(map[string]ImageProvider),
		videoProviders: make(map[string]VideoProvider),
		musicProviders: make(map[string]MusicProvider),
		ttsProviders:   make(map[string]TTSProvider),
	}
}

func (r *Registry) RegisterImage(p ImageProvider) {
	for _, m := range p.ImageModels() {
		r.imageProviders[m.ID] = p
		r.allModels = append(r.allModels, m)
		if r.defaultImage == "" {
			r.defaultImage = m.ID
		}
	}
}

func (r *Registry) RegisterVideo(p VideoProvider) {
	for _, m := range p.VideoModels() {
		r.videoProviders[m.ID] = p
		r.allModels = append(r.allModels, m)
	}
}

func (r *Registry) RegisterMusic(p MusicProvider) {
	for _, m := range p.MusicModels() {
		r.musicProviders[m.ID] = p
		r.allModels = append(r.allModels, m)
	}
}

func (r *Registry) RegisterTTS(p TTSProvider) {
	for _, m := range p.TTSModels() {
		r.ttsProviders[m.ID] = p
		r.allModels = append(r.allModels, m)
		if r.defaultTTS == "" {
			r.defaultTTS = m.ID
		}
	}
}

func (r *Registry) SetDefaults(image, tts string) {
	if image != "" {
		r.defaultImage = image
	}
	if tts != "" {
		r.defaultTTS = tts
	}
}

func (r *Registry) Models() []ModelInfo { return r.allModels }

func (r *Registry) Image(model string) (ImageProvider, string, error) {
	if model == "" {
		model = r.defaultImage
	}
	p, ok := r.imageProviders[model]
	if !ok {
		return nil, "", fmt.Errorf("unknown image model: %s", model)
	}
	return p, model, nil
}

func (r *Registry) Video(model string) (VideoProvider, string, error) {
	if model == "" {
		for k := range r.videoProviders {
			model = k
			break
		}
	}
	p, ok := r.videoProviders[model]
	if !ok {
		return nil, "", fmt.Errorf("unknown video model: %s", model)
	}
	return p, model, nil
}

func (r *Registry) Music(model string) (MusicProvider, string, error) {
	if model == "" {
		for k := range r.musicProviders {
			model = k
			break
		}
	}
	p, ok := r.musicProviders[model]
	if !ok {
		return nil, "", fmt.Errorf("unknown music model: %s", model)
	}
	return p, model, nil
}

func (r *Registry) TTS(model string) (TTSProvider, string, error) {
	if model == "" {
		model = r.defaultTTS
	}
	p, ok := r.ttsProviders[model]
	if !ok {
		return nil, "", fmt.Errorf("unknown tts model: %s", model)
	}
	return p, model, nil
}
