package locale

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestLocaleKeyParity(t *testing.T) {
	en := readLocaleKeys(t, "en")
	zh := readLocaleKeys(t, "zh-TW")

	for key := range en {
		if _, ok := zh[key]; !ok {
			t.Errorf("zh-TW missing key %q", key)
		}
	}
	for key := range zh {
		if _, ok := en[key]; !ok {
			t.Errorf("zh-TW has extra key %q", key)
		}
	}
}

func readLocaleKeys(t *testing.T, lang string) map[string]struct{} {
	t.Helper()
	data, err := langFS.ReadFile("lang/" + lang + ".json")
	if err != nil {
		t.Fatalf("read %s locale: %v", lang, err)
	}
	var messages map[string]string
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatalf("parse %s locale: %v", lang, err)
	}
	keys := make([]string, 0, len(messages))
	for key := range messages {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		out[key] = struct{}{}
	}
	return out
}
