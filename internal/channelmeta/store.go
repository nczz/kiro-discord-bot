package channelmeta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const fileName = "channel_metadata.json"

var mu sync.Mutex

type Entry struct {
	ID              string `json:"id"`
	GuildID         string `json:"guild_id,omitempty"`
	Name            string `json:"name,omitempty"`
	Type            string `json:"type,omitempty"`
	ParentChannelID string `json:"parent_channel_id,omitempty"`
	UpdatedAt       string `json:"updated_at"`
}

func Upsert(dataDir string, entry Entry) error {
	mu.Lock()
	defer mu.Unlock()

	entry.ID = strings.TrimSpace(entry.ID)
	if entry.ID == "" {
		return nil
	}
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "./data"
	}
	path := Path(dataDir)
	entries, err := Read(dataDir)
	if err != nil {
		return err
	}
	if strings.TrimSpace(entry.UpdatedAt) == "" {
		entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	entries[entry.ID] = entry
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return fmt.Errorf("write channel metadata: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace channel metadata: %w", err)
	}
	return nil
}

func Read(dataDir string) (map[string]Entry, error) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "./data"
	}
	raw, err := os.ReadFile(Path(dataDir))
	if os.IsNotExist(err) {
		return map[string]Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries map[string]Entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("read channel metadata: %w", err)
	}
	if entries == nil {
		entries = map[string]Entry{}
	}
	return entries, nil
}

func List(dataDir string) ([]Entry, error) {
	entries, err := Read(dataDir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func Path(dataDir string) string {
	return filepath.Join(dataDir, fileName)
}
