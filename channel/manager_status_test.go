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
	if !strings.Contains(got, "inactive") || !strings.Contains(got, "send a message to restart") {
		t.Fatalf("ThreadStatus() should explain inactive stored thread session, got:\n%s", got)
	}
	if !strings.Contains(got, "Agent uptime: `n/a`") {
		t.Fatalf("ThreadStatus() should include inactive agent uptime, got:\n%s", got)
	}
}

func TestChannelStatusShowsInactiveStoredSession(t *testing.T) {
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

	got := m.Status("channel-1")
	for _, want := range []string{"channel-agent", "channel-", "channel-model", "inactive", "send a message to restart"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Status() missing %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "unknown") {
		t.Fatalf("Status() should not show unknown for stored inactive sessions, got:\n%s", got)
	}
	if !strings.Contains(got, "Agent uptime: `n/a`") {
		t.Fatalf("Status() should include inactive agent uptime, got:\n%s", got)
	}
}

func TestFormatStatusUsesActiveAgentModelWhenProvided(t *testing.T) {
	L.Load("en")
	m := NewManager(ManagerConfig{BotVersion: "test-bot"})
	got := m.formatStatus(&Session{
		AgentName: "channel-agent",
		SessionID: "channel-session",
		Model:     "",
	}, "ready", 0, "omp", "openai-codex/gpt-5.5", "16.2.3", "1m", 12.5)

	if !strings.Contains(got, "openai-codex/gpt-5.5") {
		t.Fatalf("status should show active agent model, got:\n%s", got)
	}
	if strings.Contains(got, "Model: `default`") {
		t.Fatalf("status should not fall back to default when active model is known, got:\n%s", got)
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
