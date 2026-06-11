package botegress

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ActionSendMessage = "send_message"
	ActionSendFile    = "send_file"
)

// Action is a pending Discord egress request written by bot-tools and ingested by the main bot.
type Action struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	ChannelID string `json:"channel_id"`
	Content   string `json:"content,omitempty"`
	FilePath  string `json:"file_path,omitempty"`
	CreatedAt string `json:"created_at"`
}

func PendingDir(dataDir string) string {
	return filepath.Join(dataDir, "egress", "pending")
}

func WritePending(dataDir string, action Action) (string, error) {
	action.Action = strings.TrimSpace(action.Action)
	action.ChannelID = strings.TrimSpace(action.ChannelID)
	action.Content = strings.TrimSpace(action.Content)
	action.FilePath = strings.TrimSpace(action.FilePath)
	if action.ID == "" {
		action.ID = randomID()
	}
	if action.CreatedAt == "" {
		action.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := validateAction(action); err != nil {
		return "", err
	}
	dir := PendingDir(dataDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create egress pending dir: %w", err)
	}
	raw, err := json.Marshal(action)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, action.ID+".json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		return "", fmt.Errorf("write egress action: %w", err)
	}
	return action.ID, nil
}

func ReadPending(dataDir string) ([]Action, error) {
	dir := PendingDir(dataDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read egress pending dir: %w", err)
	}
	actions := make([]Action, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var action Action
		if err := json.Unmarshal(raw, &action); err != nil {
			_ = os.Remove(path)
			continue
		}
		if action.ID == "" {
			action.ID = strings.TrimSuffix(entry.Name(), ".json")
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func RemovePending(dataDir, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return os.Remove(filepath.Join(PendingDir(dataDir), id+".json"))
}

func validateAction(action Action) error {
	if action.ChannelID == "" {
		return fmt.Errorf("channel_id is required")
	}
	switch action.Action {
	case ActionSendMessage:
		if action.Content == "" {
			return fmt.Errorf("content is required")
		}
	case ActionSendFile:
		if action.FilePath == "" {
			return fmt.Errorf("file_path is required")
		}
	default:
		return fmt.Errorf("unknown egress action %q", action.Action)
	}
	return nil
}

func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
