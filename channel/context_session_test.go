package channel

import (
	"strings"
	"testing"
)

func TestContextSessionKeyChangesWithStoredSession(t *testing.T) {
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	m := NewManager(ManagerConfig{Store: store})
	if err := m.setChannelSession("channel-1", &Session{Engine: "omp", AgentName: "ch-channel-1", SessionID: "session-1"}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}
	first := m.ContextSessionKey("channel-1", "")
	if !strings.Contains(first, "session-1") || !strings.Contains(first, "channel-1") {
		t.Fatalf("first key = %q, want target and session", first)
	}
	if err := m.setChannelSession("channel-1", &Session{Engine: "omp", AgentName: "ch-channel-1", SessionID: "session-2"}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}
	second := m.ContextSessionKey("channel-1", "")
	if second == first || !strings.Contains(second, "session-2") {
		t.Fatalf("second key = %q, first=%q", second, first)
	}
}

func TestContextSessionKeyUsesThreadScope(t *testing.T) {
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	m := NewManager(ManagerConfig{Store: store})
	if err := m.setThreadSession("thread-1", "channel-1", &Session{Engine: "kiro", AgentName: "thread-thread-1", SessionID: "thread-session"}); err != nil {
		t.Fatalf("set thread session: %v", err)
	}
	got := m.ContextSessionKey("thread-1", "channel-1")
	for _, want := range []string{"thread", "thread-1", "thread-session"} {
		if !strings.Contains(got, want) {
			t.Fatalf("thread key = %q, missing %q", got, want)
		}
	}
}
