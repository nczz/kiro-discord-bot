package channel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ChatEntry is a single line in the chat JSONL log.
type ChatEntry struct {
	Timestamp   string   `json:"ts"`
	Role        string   `json:"role"` // "user" or "assistant"
	UserID      string   `json:"user_id,omitempty"`
	Username    string   `json:"username,omitempty"`
	MessageID   string   `json:"message_id,omitempty"`
	Content     string   `json:"content"`
	Attachments []string `json:"attachments,omitempty"`
	Model       string   `json:"model,omitempty"`
}

// ChatLogger appends chat entries to per-channel JSONL files.
type ChatLogger struct {
	mu      sync.Mutex
	dataDir string
}

func NewChatLogger(dataDir string) *ChatLogger {
	return &ChatLogger{dataDir: dataDir}
}

func (l *ChatLogger) Log(channelID string, entry ChatEntry) {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().Format(time.RFC3339)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')

	dir := filepath.Join(l.dataDir, "ch-"+channelID)
	_ = os.MkdirAll(dir, 0755)

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(filepath.Join(dir, "chat.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(data)
}
