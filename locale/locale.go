package locale

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

//go:embed lang/*.json
var langFS embed.FS

var messages map[string]string

// Load initializes the locale with the given language code (e.g. "en", "zh-TW").
// Falls back to "en" if the requested language file is not found.
func Load(lang string) {
	if lang == "" {
		lang = "en"
	}
	messages = make(map[string]string)
	// Load English as base
	loadFile("en")
	// Override with requested language
	if lang != "en" {
		loadFile(lang)
	}
	log.Printf("[locale] loaded: %s (%d keys)", lang, len(messages))
}

func loadFile(lang string) {
	data, err := langFS.ReadFile("lang/" + lang + ".json")
	if err != nil {
		log.Printf("[locale] lang/%s.json not found, skipping", lang)
		return
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("[locale] parse lang/%s.json: %v", lang, err)
		return
	}
	for k, v := range m {
		messages[k] = v
	}
}

// Get returns the localized string for the given key.
// If not found, returns the key itself.
func Get(key string) string {
	if v, ok := messages[key]; ok {
		return v
	}
	return key
}

// Getf returns the localized string formatted with args (like fmt.Sprintf).
func Getf(key string, args ...any) string {
	tmpl := Get(key)
	if len(args) == 0 {
		return tmpl
	}
	// Only format if template contains %
	if strings.Contains(tmpl, "%") {
		return fmt.Sprintf(tmpl, args...)
	}
	return tmpl
}
