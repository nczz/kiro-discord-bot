package heartbeat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupTaskRemovesExpiredProjectCWDAttachments(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := t.TempDir()
	rawSessions, err := json.Marshal(map[string]cleanupSession{"channel-1": {CWD: projectDir}})
	if err != nil {
		t.Fatalf("marshal sessions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "sessions.json"), rawSessions, 0644); err != nil {
		t.Fatalf("write sessions: %v", err)
	}

	oldPath := filepath.Join(projectDir, ".kiro-bot", "attachments", "message-old", "old.txt")
	freshPath := filepath.Join(projectDir, ".kiro-bot", "attachments", "message-fresh", "fresh.txt")
	writeAttachmentFile(t, oldPath, time.Now().AddDate(0, 0, -8))
	writeAttachmentFile(t, freshPath, time.Now())

	task := NewCleanupTask(dataDir, 7)
	if err := task.Run(); err != nil {
		t.Fatalf("cleanup run: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expired project attachment still exists, err=%v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("fresh project attachment missing: %v", err)
	}
}

func TestCleanupTaskStillRemovesExpiredLegacyChannelAttachments(t *testing.T) {
	dataDir := t.TempDir()
	oldPath := filepath.Join(dataDir, "ch-channel-1", "attachments", "old.txt")
	freshPath := filepath.Join(dataDir, "ch-channel-1", "attachments", "fresh.txt")
	writeAttachmentFile(t, oldPath, time.Now().AddDate(0, 0, -8))
	writeAttachmentFile(t, freshPath, time.Now())

	task := NewCleanupTask(dataDir, 7)
	if err := task.Run(); err != nil {
		t.Fatalf("cleanup run: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expired legacy attachment still exists, err=%v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("fresh legacy attachment missing: %v", err)
	}
}

func writeAttachmentFile(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir attachment dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("attachment"), 0644); err != nil {
		t.Fatalf("write attachment: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes attachment: %v", err)
	}
}
