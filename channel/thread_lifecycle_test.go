package channel

import (
	"context"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
)

func TestThreadAgentEntriesReportsActiveState(t *testing.T) {
	inactive := newWorker("thread-inactive", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	active := newWorker("thread-active", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	active.cancelMu.Lock()
	active.cancelFn = cancel
	active.currentJobSeq = 1
	active.currentJobActive = true
	active.cancelMu.Unlock()

	m := &Manager{threadAgents: map[string]*threadAgentEntry{
		"inactive": {threadID: "inactive", parentChannelID: "parent", worker: inactive, lastActivity: time.Now()},
		"active":   {threadID: "active", parentChannelID: "parent", worker: active, lastActivity: time.Now()},
	}}

	entries := m.ThreadAgentEntries()
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.ThreadID] = e.Active
	}

	if seen["inactive"] {
		t.Fatal("inactive thread reported active")
	}
	if !seen["active"] {
		t.Fatal("active thread was not reported active")
	}
}

func TestThreadAgentLimitErrorReportsInactiveCandidates(t *testing.T) {
	active := newWorker("thread-active", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	active.cancelMu.Lock()
	active.cancelFn = cancel
	active.currentJobSeq = 1
	active.currentJobActive = true
	active.cancelMu.Unlock()

	idleOld := newWorker("thread-idle-old", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	idleNew := newWorker("thread-idle-new", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")

	m := &Manager{threadAgentMax: 3, threadAgents: map[string]*threadAgentEntry{
		"active": {
			threadID:     "active",
			worker:       active,
			lastActivity: time.Now().Add(-2 * time.Hour),
		},
		"idle-old": {
			threadID:     "idle-old",
			worker:       idleOld,
			lastActivity: time.Now().Add(-time.Hour),
		},
		"idle-new": {
			threadID:     "idle-new",
			worker:       idleNew,
			lastActivity: time.Now(),
		},
	}}

	err, ok := m.threadAgentLimitErrorLocked().(*ThreadAgentLimitError)
	if !ok {
		t.Fatal("expected ThreadAgentLimitError")
	}
	if err.Max != 3 || err.Active != 1 || err.Inactive != 2 {
		t.Fatalf("limit stats = max:%d active:%d inactive:%d, want 3/1/2", err.Max, err.Active, err.Inactive)
	}
	if len(err.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(err.Candidates))
	}
	if err.Candidates[0].ThreadID != "idle-old" || err.Candidates[1].ThreadID != "idle-new" {
		t.Fatalf("candidate order = %v, want idle-old then idle-new", err.Candidates)
	}
}

func TestThreadAgentLimitErrorWhenAllActive(t *testing.T) {
	active := newWorker("thread-active", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	active.cancelMu.Lock()
	active.cancelFn = cancel
	active.currentJobSeq = 1
	active.currentJobActive = true
	active.cancelMu.Unlock()

	m := &Manager{threadAgentMax: 1, threadAgents: map[string]*threadAgentEntry{
		"active": {threadID: "active", worker: active, lastActivity: time.Now()},
	}}

	err, ok := m.threadAgentLimitErrorLocked().(*ThreadAgentLimitError)
	if !ok {
		t.Fatal("expected ThreadAgentLimitError")
	}
	if err.Max != 1 || err.Active != 1 || err.Inactive != 0 {
		t.Fatalf("limit stats = max:%d active:%d inactive:%d, want 1/1/0", err.Max, err.Active, err.Inactive)
	}
	if len(err.Candidates) != 0 {
		t.Fatalf("candidates = %v, want none", err.Candidates)
	}
}

func TestMarkThreadArchivedDefersActiveAgentCloseUntilIdle(t *testing.T) {
	active := newWorker("thread-active", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	active.cancelMu.Lock()
	active.cancelFn = cancel
	active.currentJobSeq = 1
	active.currentJobActive = true
	active.cancelMu.Unlock()

	m := &Manager{threadAgents: map[string]*threadAgentEntry{
		"active": {threadID: "active", worker: active, agent: &acp.Agent{}, lastActivity: time.Now()},
	}}

	stopped, deferred := m.MarkThreadArchived("active")
	if stopped || !deferred {
		t.Fatalf("MarkThreadArchived stopped/deferred = %v/%v, want false/true", stopped, deferred)
	}
	if _, ok := m.threadAgents["active"]; !ok {
		t.Fatal("active archived thread should remain until idle")
	}

	active.cancelMu.Lock()
	active.cancelFn = nil
	active.currentJobActive = false
	active.cancelMu.Unlock()

	if !m.StopThreadAgentIfCloseWhenIdle("active") {
		t.Fatal("expected deferred archived thread to stop after idle")
	}
	if _, ok := m.threadAgents["active"]; ok {
		t.Fatal("deferred archived thread still registered after idle close")
	}
}

func TestMarkThreadArchivedStopsInactiveAgentImmediately(t *testing.T) {
	inactive := newWorker("thread-inactive", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	m := &Manager{threadAgents: map[string]*threadAgentEntry{
		"inactive": {threadID: "inactive", worker: inactive, agent: &acp.Agent{}, lastActivity: time.Now()},
	}}

	stopped, deferred := m.MarkThreadArchived("inactive")
	if !stopped || deferred {
		t.Fatalf("MarkThreadArchived stopped/deferred = %v/%v, want true/false", stopped, deferred)
	}
	if _, ok := m.threadAgents["inactive"]; ok {
		t.Fatal("inactive archived thread should be stopped immediately")
	}
}

func TestThreadAgentDetails(t *testing.T) {
	active := newWorker("thread-active", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	active.cancelMu.Lock()
	active.currentJobActive = true
	active.cancelMu.Unlock()
	m := &Manager{threadAgents: map[string]*threadAgentEntry{
		"active": {threadID: "active", parentChannelID: "parent", worker: active, lastActivity: time.Now()},
	}}

	parent, isActive, ok := m.ThreadAgentDetails("active")
	if !ok || parent != "parent" || !isActive {
		t.Fatalf("details = %q/%v/%v, want parent/true/true", parent, isActive, ok)
	}

	_, _, ok = m.ThreadAgentDetails("missing")
	if ok {
		t.Fatal("missing thread should not report details")
	}
}
