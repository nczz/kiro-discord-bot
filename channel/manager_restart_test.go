package channel

import (
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
)

func TestRestartChannelRuntimesStopsCurrentScopeOnlyAndPreservesSessions(t *testing.T) {
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	m := NewManager(ManagerConfig{Store: store, GuildID: "guild-1", BotID: "bot-1"})

	m.agents["channel-1"] = &acp.Agent{}
	m.workers["channel-1"] = newWorker("channel-1", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	m.channelLastActivity["channel-1"] = time.Now()
	m.agents["other-channel"] = &acp.Agent{}
	m.workers["other-channel"] = newWorker("other-channel", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")

	m.threadAgents["thread-1"] = &threadAgentEntry{threadID: "thread-1", parentChannelID: "channel-1", worker: newWorker("thread-1", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, ""), agent: &acp.Agent{}, lastActivity: time.Now()}
	m.threadAgents["thread-2"] = &threadAgentEntry{threadID: "thread-2", parentChannelID: "channel-1", worker: newWorker("thread-2", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, ""), agent: &acp.Agent{}, lastActivity: time.Now()}
	m.threadAgents["other-thread"] = &threadAgentEntry{threadID: "other-thread", parentChannelID: "other-channel", worker: newWorker("other-thread", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, ""), agent: &acp.Agent{}, lastActivity: time.Now()}

	if err := m.setChannelSession("channel-1", &Session{AgentName: "ch-channel-1", SessionID: "session-1", CWD: "/project", Model: "model-1", Engine: "omp"}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}
	if err := m.setThreadSession("thread-1", "channel-1", &Session{AgentName: "thread-thread-1", SessionID: "thread-session-1", CWD: "/project/thread", Model: "thread-model", Engine: "kiro"}); err != nil {
		t.Fatalf("set thread session: %v", err)
	}

	if stopped := m.RestartChannelRuntimes("channel-1"); stopped != 3 {
		t.Fatalf("stopped = %d, want 3", stopped)
	}
	if _, ok := m.agents["channel-1"]; ok {
		t.Fatal("channel agent was not stopped")
	}
	if _, ok := m.workers["channel-1"]; ok {
		t.Fatal("channel worker was not stopped")
	}
	if _, ok := m.threadAgents["thread-1"]; ok {
		t.Fatal("thread-1 agent was not stopped")
	}
	if _, ok := m.threadAgents["thread-2"]; ok {
		t.Fatal("thread-2 agent was not stopped")
	}
	if _, ok := m.agents["other-channel"]; !ok {
		t.Fatal("unrelated channel agent should remain")
	}
	if _, ok := m.workers["other-channel"]; !ok {
		t.Fatal("unrelated channel worker should remain")
	}
	if _, ok := m.threadAgents["other-thread"]; !ok {
		t.Fatal("unrelated thread agent should remain")
	}

	channelSess, ok := m.getChannelSession("channel-1")
	if !ok {
		t.Fatal("channel session should be preserved")
	}
	if channelSess.SessionID != "session-1" || channelSess.CWD != "/project" || channelSess.Model != "model-1" || channelSess.Engine != "omp" {
		t.Fatalf("channel session changed: %+v", channelSess)
	}
	threadSess, ok := m.getThreadSession("thread-1")
	if !ok {
		t.Fatal("thread session should be preserved")
	}
	if threadSess.SessionID != "thread-session-1" || threadSess.CWD != "/project/thread" || threadSess.Model != "thread-model" || threadSess.Engine != "kiro" || threadSess.ParentChannelID != "channel-1" {
		t.Fatalf("thread session changed: %+v", threadSess)
	}
}
