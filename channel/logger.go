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
	files   map[string]*os.File // channelID → open file handle
}

func NewChatLogger(dataDir string) *ChatLogger {
	return &ChatLogger{dataDir: dataDir, files: make(map[string]*os.File)}
}

func (l *ChatLogger) getFile(channelID string) (*os.File, error) {
	if f, ok := l.files[channelID]; ok {
		return f, nil
	}
	dir := filepath.Join(l.dataDir, "ch-"+channelID)
	_ = os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(filepath.Join(dir, "chat.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	l.files[channelID] = f
	return f, nil
}

// Close closes all open log file handles.
func (l *ChatLogger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, f := range l.files {
		f.Close()
	}
	l.files = make(map[string]*os.File)
}

// ClearLog truncates the chat log for a channel.
func (l *ChatLogger) ClearLog(channelID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if f, ok := l.files[channelID]; ok {
		f.Close()
		delete(l.files, channelID)
	}
	path := filepath.Join(l.dataDir, "ch-"+channelID, "chat.jsonl")
	_ = os.Truncate(path, 0)
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

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := l.getFile(channelID)
	if err != nil {
		return
	}
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
// Uses budget-based approach: recent turns kept intact, older turns truncated to fit within maxChars.
func (l *ChatLogger) BuildContextPrompt(channelID string, maxTurns int) string {
	return l.BuildContextPromptBudget(channelID, maxTurns, 20000)
}

// BuildContextPromptBudget formats recent history with a total character budget.
// Recent turns are kept intact; older turns are progressively truncated.
func (l *ChatLogger) BuildContextPromptBudget(channelID string, maxTurns, maxChars int) string {
	entries := l.RecentHistory(channelID, maxTurns)
	if len(entries) == 0 {
		return ""
	}

	// Calculate total raw size
	total := 0
	for _, e := range entries {
		total += len(e.Content)
	}

	var sb strings.Builder
	sb.WriteString("[Previous conversation context for session continuity]\n")

	budget := maxChars
	// Process from newest to oldest, reserving budget for recent entries
	reserved := make([]int, len(entries))
	for i := len(entries) - 1; i >= 0 && budget > 0; i-- {
		take := len(entries[i].Content)
		if take > budget {
			take = budget
		}
		reserved[i] = take
		budget -= take
	}

	for i, e := range entries {
		role := "User"
		if e.Role == "assistant" {
			role = "Assistant"
		}
		content := e.Content
		if reserved[i] < len(content) {
			if reserved[i] > 50 {
				content = content[:reserved[i]] + "…(truncated)"
			} else {
				content = "(omitted)"
			}
		}
		sb.WriteString(fmt.Sprintf("[%s] %s\n", role, content))
	}
	sb.WriteString("[End of previous context]\n\n")
	return sb.String()
}
