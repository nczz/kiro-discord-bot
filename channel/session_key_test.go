package channel

import "testing"

func newSessionKeyTestManager(t *testing.T) *Manager {
	t.Helper()
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	return NewManager(ManagerConfig{
		Store:   store,
		GuildID: "guild-1",
	})
}

func TestChannelSessionKeyIsScopedByGuildAndBot(t *testing.T) {
	m := newSessionKeyTestManager(t)

	m.SetBotID("bot-a")
	if err := m.setChannelSession("channel-1", &Session{SessionID: "session-a"}); err != nil {
		t.Fatalf("set bot-a session: %v", err)
	}

	m.SetBotID("bot-b")
	if sess, ok := m.getChannelSession("channel-1"); ok {
		t.Fatalf("expected bot-b not to see bot-a session, got %#v", sess)
	}
	if err := m.setChannelSession("channel-1", &Session{SessionID: "session-b"}); err != nil {
		t.Fatalf("set bot-b session: %v", err)
	}

	m.SetBotID("bot-a")
	sess, ok := m.getChannelSession("channel-1")
	if !ok {
		t.Fatal("expected bot-a session")
	}
	if sess.SessionID != "session-a" {
		t.Fatalf("expected bot-a session-a, got %q", sess.SessionID)
	}

	m.SetBotID("bot-b")
	sess, ok = m.getChannelSession("channel-1")
	if !ok {
		t.Fatal("expected bot-b session")
	}
	if sess.SessionID != "session-b" {
		t.Fatalf("expected bot-b session-b, got %q", sess.SessionID)
	}
}

func TestChannelSessionKeyFallsBackToLegacyChannelID(t *testing.T) {
	m := newSessionKeyTestManager(t)
	if err := m.store.Set("channel-1", &Session{SessionID: "legacy"}); err != nil {
		t.Fatalf("set legacy session: %v", err)
	}

	m.SetBotID("bot-a")
	sess, ok := m.getChannelSession("channel-1")
	if !ok {
		t.Fatal("expected legacy channel session fallback")
	}
	if sess.SessionID != "legacy" {
		t.Fatalf("expected legacy session, got %q", sess.SessionID)
	}

	if err := m.setChannelSession("channel-1", &Session{SessionID: "scoped"}); err != nil {
		t.Fatalf("set scoped session: %v", err)
	}
	sess, ok = m.getChannelSession("channel-1")
	if !ok {
		t.Fatal("expected scoped channel session")
	}
	if sess.SessionID != "scoped" {
		t.Fatalf("expected scoped session to win over legacy, got %q", sess.SessionID)
	}
}

func TestThreadSessionKeyIsScopedAndStoresParentChannel(t *testing.T) {
	m := newSessionKeyTestManager(t)

	m.SetBotID("bot-a")
	if err := m.setThreadSession("thread-1", "channel-1", &Session{SessionID: "thread-a"}); err != nil {
		t.Fatalf("set bot-a thread session: %v", err)
	}

	m.SetBotID("bot-b")
	if sess, ok := m.getThreadSession("thread-1"); ok {
		t.Fatalf("expected bot-b not to see bot-a thread session, got %#v", sess)
	}

	m.SetBotID("bot-a")
	sess, ok := m.getThreadSession("thread-1")
	if !ok {
		t.Fatal("expected bot-a thread session")
	}
	if sess.SessionID != "thread-a" {
		t.Fatalf("expected bot-a thread session, got %q", sess.SessionID)
	}
	if sess.ParentChannelID != "channel-1" {
		t.Fatalf("expected parent channel to persist, got %q", sess.ParentChannelID)
	}
	if sess.TargetType != sessionTargetThread || sess.TargetID != "thread-1" {
		t.Fatalf("expected thread target metadata, got type=%q id=%q", sess.TargetType, sess.TargetID)
	}
}
