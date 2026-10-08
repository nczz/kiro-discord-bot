package channel

import "testing"

func TestCaptureThreadListenModeIsImmutable(t *testing.T) {
	m := NewManager(ManagerConfig{})

	m.CaptureThreadListenMode("thread-1", true)
	m.CaptureThreadListenMode("thread-1", false)

	mode, ok := m.ThreadListenSnapshot("thread-1")
	if !ok || mode != "mention" {
		t.Fatalf("thread snapshot = (%q, %v), want (mention, true)", mode, ok)
	}
}

func TestCapturedFullThreadIgnoresLaterParentPause(t *testing.T) {
	m := NewManager(ManagerConfig{})
	m.CaptureThreadListenMode("thread-1", false)
	m.Pause("channel-1")

	if m.ThreadMentionOnly("thread-1", "channel-1") {
		t.Fatal("full-listen thread snapshot should survive a later parent /pause")
	}
}

func TestThreadMentionOnlyChecksPausedParentDirectly(t *testing.T) {
	m := NewManager(ManagerConfig{})
	m.Pause("channel-1")
	m.SetThreadMode("channel-1", true)

	if !m.ThreadMentionOnly("thread-1", "channel-1") {
		t.Fatal("paused parent should force mention-only even when thread mode is enabled")
	}
}
