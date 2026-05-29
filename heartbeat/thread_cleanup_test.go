package heartbeat

import (
	"testing"
	"time"
)

type fakeThreadCleanupDeps struct {
	entries []ThreadAgentInfo
	stopped []string
}

func (f *fakeThreadCleanupDeps) ThreadAgentEntries() []ThreadAgentInfo {
	return f.entries
}

func (f *fakeThreadCleanupDeps) StopThreadAgent(threadID string) {
	f.stopped = append(f.stopped, threadID)
}

func (f *fakeThreadCleanupDeps) Notify(string, string) {}

func (f *fakeThreadCleanupDeps) IsSilent(string) bool { return true }

func TestThreadCleanupDoesNotRunWhenDisabled(t *testing.T) {
	task := NewThreadCleanupTask(&fakeThreadCleanupDeps{}, 0, 5)
	if task.ShouldRun(time.Now()) {
		t.Fatal("thread cleanup should be disabled when idleSec <= 0")
	}
}

func TestThreadCleanupSkipsActiveAgents(t *testing.T) {
	deps := &fakeThreadCleanupDeps{
		entries: []ThreadAgentInfo{
			{ThreadID: "active-old", ParentChID: "parent", LastActivity: time.Now().Add(-time.Hour), Active: true},
			{ThreadID: "idle-old", ParentChID: "parent", LastActivity: time.Now().Add(-time.Hour), Active: false},
		},
	}
	task := NewThreadCleanupTask(deps, 60, 5)

	if err := task.Run(); err != nil {
		t.Fatal(err)
	}

	if len(deps.stopped) != 1 || deps.stopped[0] != "idle-old" {
		t.Fatalf("stopped = %v, want [idle-old]", deps.stopped)
	}
}

func TestThreadCleanupSkipsZeroLastActivity(t *testing.T) {
	deps := &fakeThreadCleanupDeps{
		entries: []ThreadAgentInfo{
			{ThreadID: "zero", ParentChID: "parent"},
		},
	}
	task := NewThreadCleanupTask(deps, 60, 5)

	if err := task.Run(); err != nil {
		t.Fatal(err)
	}

	if len(deps.stopped) != 0 {
		t.Fatalf("stopped = %v, want none", deps.stopped)
	}
}
