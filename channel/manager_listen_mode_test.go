package channel

import "testing"

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
