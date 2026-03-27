package channel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// RecentHistory reads the last N conversation turns from the JSONL log.
func (l *ChatLogger) RecentHistory(channelID string, maxTurns int) []ChatEntry {
	path := filepath.Join(l.dataDir, "ch-"+channelID, "chat.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var all []ChatEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		var e ChatEntry
		if json.Unmarshal(scanner.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	if len(all) > maxTurns {
		all = all[len(all)-maxTurns:]
	}
	return all
}

// BuildContextPrompt formats recent history as a context preamble for a new session.
// Returns empty string if no history.
func (l *ChatLogger) BuildContextPrompt(channelID string, maxTurns int) string {
	entries := l.RecentHistory(channelID, maxTurns)
	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[Previous conversation context for session continuity]\n")
	for _, e := range entries {
		role := "User"
		if e.Role == "assistant" {
			role = "Assistant"
		}
		content := e.Content
		if len(content) > 500 {
			content = content[:500] + "...(truncated)"
		}
		sb.WriteString(fmt.Sprintf("[%s] %s\n", role, content))
	}
	sb.WriteString("[End of previous context]\n\n")
	return sb.String()
}
