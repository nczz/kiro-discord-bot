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
