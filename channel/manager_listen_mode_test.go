package channel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListenModeOverridesPersistAcrossManagerRestart(t *testing.T) {
	dataDir := t.TempDir()

	m := NewManager(ManagerConfig{DataDir: dataDir})
	m.Back("channel-1")
	m.Pause("thread-1")

	restarted := NewManager(ManagerConfig{DataDir: dataDir})
	if !restarted.HasFullListenOverride("channel-1") {
		t.Fatal("expected channel /back override to persist")
	}
	if !restarted.HasMentionOnlyOverride("thread-1") {
		t.Fatal("expected thread /pause override to persist")
	}
}

func TestListenModeOverrideCanBeChangedAfterRestart(t *testing.T) {
	dataDir := t.TempDir()

	m := NewManager(ManagerConfig{DataDir: dataDir})
	m.Back("channel-1")

	restarted := NewManager(ManagerConfig{DataDir: dataDir})
	restarted.Pause("channel-1")

	again := NewManager(ManagerConfig{DataDir: dataDir})
	if !again.HasMentionOnlyOverride("channel-1") {
		t.Fatal("expected later /pause override to replace persisted /back")
	}
	if again.HasFullListenOverride("channel-1") {
		t.Fatal("did not expect stale /back override after /pause")
	}
}

func TestThreadModePersistsAcrossManagerRestart(t *testing.T) {
	dataDir := t.TempDir()

	m := NewManager(ManagerConfig{DataDir: dataDir})
	m.SetThreadMode("channel-1", false)
	m.SetThreadListenMode("thread-full", false)
	m.SetThreadListenMode("thread-mention", true)

	restarted := NewManager(ManagerConfig{DataDir: dataDir})
	if restarted.ThreadModeEnabled("channel-1") {
		t.Fatal("expected channel thread mode off to persist")
	}
	if !restarted.ThreadModeEnabled("channel-default") {
		t.Fatal("expected absent channel thread mode to default on")
	}
	if restarted.ThreadMentionOnly("thread-full", "channel-1") {
		t.Fatal("expected thread full-listen snapshot to persist even when parent thread mode is off")
	}
	if !restarted.ThreadMentionOnly("thread-mention", "channel-1") {
		t.Fatal("expected thread mention-only snapshot to persist")
	}
}

func TestPausedListenModeMigratesThreadModeOff(t *testing.T) {
	dataDir := t.TempDir()

	m := NewManager(ManagerConfig{DataDir: dataDir})
	m.Pause("channel-1")

	restarted := NewManager(ManagerConfig{DataDir: dataDir})
	if restarted.ThreadModeEnabled("channel-1") {
		t.Fatal("expected legacy paused channel to migrate thread mode off")
	}
}

func TestUnknownThreadUsesParentThreadModeFallback(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if m.ThreadMentionOnly("thread-1", "channel-1") {
		t.Fatal("expected unknown thread to default full-listen when parent thread mode is on")
	}
	m.SetThreadMode("channel-1", false)
	if !m.ThreadMentionOnly("thread-1", "channel-1") {
		t.Fatal("expected unknown thread to be mention-only when parent thread mode is off")
	}
}

func TestManagerClearHistoryTruncatesChatLog(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(ManagerConfig{DataDir: dataDir})
	m.logger.Log("channel-1", ChatEntry{Role: "user", Content: "hello"})
	m.logger.Log("channel-1", ChatEntry{Role: "assistant", Content: "response\n\n-# ⚡ 0.22 credit"})

	m.ClearHistory("channel-1")

	path := filepath.Join(dataDir, "ch-channel-1", "chat.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read chat log: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("chat log after clear = %q, want empty", data)
	}
	if history := m.logger.RecentHistory("channel-1", 10); len(history) != 0 {
		t.Fatalf("history len after clear = %d, want 0", len(history))
	}
}

func TestManagerClearThreadHistoryTruncatesThreadLogAndClearsStoredSession(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewSessionStore(dataDir)
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	m := NewManager(ManagerConfig{DataDir: dataDir, Store: store, GuildID: "guild-1"})
	m.logger.Log("thread-thread-1", ChatEntry{Role: "user", Content: "secret thread detail"})
	m.logger.Log("channel-1", ChatEntry{Role: "user", Content: "parent context"})
	if err := m.setThreadSession("thread-1", "channel-1", &Session{SessionID: "session-1", CWD: "/tmp/project"}); err != nil {
		t.Fatalf("set thread session: %v", err)
	}

	if err := m.ClearThreadHistory("thread-1", "channel-1"); err != nil {
		t.Fatalf("clear thread history: %v", err)
	}

	threadPath := filepath.Join(dataDir, "ch-thread-thread-1", "chat.jsonl")
	threadData, err := os.ReadFile(threadPath)
	if err != nil {
		t.Fatalf("read thread chat log: %v", err)
	}
	if len(threadData) != 0 {
		t.Fatalf("thread chat log after clear = %q, want empty", threadData)
	}
	if parentHistory := m.logger.RecentHistory("channel-1", 10); len(parentHistory) != 1 {
		t.Fatalf("parent history should not be cleared, got %d entries", len(parentHistory))
	}
	sess, ok := m.getThreadSession("thread-1")
	if !ok {
		t.Fatal("thread session missing after clear")
	}
	if sess.SessionID != "" || sess.AgentName != "" {
		t.Fatalf("clear should prevent session/load reuse, got agent=%q session=%q", sess.AgentName, sess.SessionID)
	}
	if sess.CWD != "/tmp/project" || sess.ParentChannelID != "channel-1" {
		t.Fatalf("clear should preserve thread binding metadata: %+v", sess)
	}
}
