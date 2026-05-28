package channel

import (
	"strings"
	"testing"

	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestThreadStatusUsesThreadSession(t *testing.T) {
	L.Load("en")
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	m := NewManager(ManagerConfig{Store: store, BotVersion: "test-bot"})

	if err := m.setChannelSession("channel-1", &Session{
		AgentName: "channel-agent",
		SessionID: "channel-session",
		Model:     "channel-model",
	}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}
	if err := m.setThreadSession("thread-1", "channel-1", &Session{
		AgentName: "thread-agent",
		SessionID: "thread-session",
		Model:     "thread-model",
	}); err != nil {
		t.Fatalf("set thread session: %v", err)
	}

	got := m.ThreadStatus("thread-1")
	for _, want := range []string{"thread-agent", "thread-s", "thread-model"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ThreadStatus() missing %q, got:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"channel-agent", "channel-model"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("ThreadStatus() should not include %q, got:\n%s", notWant, got)
		}
	}
}

func TestThreadStatusWithoutThreadSession(t *testing.T) {
	L.Load("en")
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	m := NewManager(ManagerConfig{Store: store})

	got := m.ThreadStatus("thread-1")
	if !strings.Contains(got, "No active session") && !strings.Contains(got, "沒有活躍的 session") {
		t.Fatalf("expected no-session status, got:\n%s", got)
	}
}
