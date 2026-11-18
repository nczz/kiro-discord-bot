package channel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/audit"
	L "github.com/nczz/kiro-discord-bot/locale"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type recoveryRoundTripper struct {
	recordingRoundTripper
	mu        sync.Mutex
	reactions map[string]bool
}

func (rt *recoveryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/messages/") {
		rt.mu.Lock()
		reactions := make([]map[string]any, 0, len(rt.reactions))
		for emoji, present := range rt.reactions {
			if present {
				reactions = append(reactions, map[string]any{"count": 1, "me": true, "emoji": map[string]any{"name": emoji}})
			}
		}
		rt.mu.Unlock()
		body, _ := json.Marshal(map[string]any{"id": "message-1", "channel_id": "channel-1", "reactions": reactions})
		return discordTestResponse(req, http.StatusOK, string(body)), nil
	}
	if emoji := reactionEmojiFromRequest(req); emoji != "" {
		rt.mu.Lock()
		if req.Method == http.MethodPut {
			rt.reactions[emoji] = true
		} else if req.Method == http.MethodDelete {
			delete(rt.reactions, emoji)
		}
		rt.mu.Unlock()
	}
	return rt.recordingRoundTripper.RoundTrip(req)
}

func (rt *recoveryRoundTripper) reactionSnapshot() map[string]bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make(map[string]bool, len(rt.reactions))
	for emoji, present := range rt.reactions {
		out[emoji] = present
	}
	return out
}

func TestJobRecoveryStorePersistsMinimalStateAndRemovesEmptyJournal(t *testing.T) {
	dir := t.TempDir()
	store := newJobRecoveryStore(dir)
	record := jobRecoveryRecord{
		ID:        "job-1",
		State:     jobRecoveryQueued,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		MessageID: "message-1",
	}
	if err := store.Put(record); err != nil {
		t.Fatalf("put recovery record: %v", err)
	}

	reloaded := newJobRecoveryStore(dir)
	records, err := reloaded.Records()
	if err != nil {
		t.Fatalf("reload recovery records: %v", err)
	}
	if len(records) != 1 || records[0] != record {
		t.Fatalf("reloaded records = %#v, want %#v", records, record)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatalf("stat recovery journal: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("recovery journal mode = %o, want 600", info.Mode().Perm())
	}

	if err := reloaded.Delete(record.ID); err != nil {
		t.Fatalf("delete recovery record: %v", err)
	}
	if _, err := os.Stat(store.path); !os.IsNotExist(err) {
		t.Fatalf("empty recovery journal still exists: %v", err)
	}
}

func TestJobRecoveryJournalExcludesPromptAndLocalPayloads(t *testing.T) {
	store := newJobRecoveryStore(t.TempDir())
	job := &Job{
		GuildID:     "guild-1",
		ChannelID:   "channel-1",
		MessageID:   "message-1",
		Username:    "private-operator-name",
		Prompt:      "private prompt payload",
		Attachments: []string{"/private/runtime/attachment.txt"},
	}
	if err := store.Put(jobRecoveryRecordFromJob(job)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{job.Username, job.Prompt, job.Attachments[0]} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("recovery journal persisted private payload %q: %s", forbidden, raw)
		}
	}
}

func TestWorkerShutdownFinalizesActiveJobBeforeAgentStop(t *testing.T) {
	L.Load("en")
	dir := t.TempDir()
	store := newJobRecoveryStore(dir)
	rt := &recoveryRoundTripper{reactions: map[string]bool{"⏳": true}}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{}
	auditSink := &recordingAuditSink{}
	w := newWorker("channel-1", agent, 2, 30, 1, 1440, nil, "")
	w.SetAuditSink(auditSink)
	w.setRecoveryStore(store)
	w.OnOutputModeFunc(func() OutputMode { return OutputModeFolded })
	job := &Job{GuildID: "guild-1", ChannelID: "channel-1", ThreadID: "thread-1", MessageID: "message-1", Prompt: "work", Session: ds, QueuedReactionAdded: true}
	if err := store.Put(jobRecoveryRecordFromJob(job)); err != nil {
		t.Fatal(err)
	}

	w.execute(job)
	w.Shutdown(context.Background())

	if agent.CancelCalls() != 1 {
		t.Fatalf("agent cancel calls = %d, want 1", agent.CancelCalls())
	}
	if records, err := store.Records(); err != nil || len(records) != 0 {
		t.Fatalf("recovery records after shutdown = %#v, err=%v", records, err)
	}
	var failed *audit.BotEvent
	for _, event := range auditSink.Snapshot() {
		if event.Type == "agent_job_failed" {
			e := event
			failed = &e
		}
	}
	if failed == nil || failed.Metadata["reason"] != "service_shutdown" {
		t.Fatalf("shutdown audit event = %#v, want service_shutdown failure", failed)
	}
	reqs, bodies := rt.Snapshot()
	var patched, notified bool
	for i, req := range reqs {
		if strings.HasPrefix(req, "PATCH ") && strings.Contains(req, "/messages/") {
			patched = true
		}
		if strings.Contains(bodies[i], "Bot is restarting") {
			notified = true
		}
	}
	if !patched || !notified {
		t.Fatalf("shutdown Discord requests patched=%t notified=%t requests=%v bodies=%v", patched, notified, reqs, bodies)
	}
	if got := rt.reactionSnapshot(); len(got) != 1 || !got["⚠️"] {
		t.Fatalf("shutdown reactions = %#v, want warning only", got)
	}
}

func TestWorkerShutdownRejectsQueuedJobWithTerminalAudit(t *testing.T) {
	L.Load("en")
	store := newJobRecoveryStore(t.TempDir())
	rt := &recoveryRoundTripper{reactions: map[string]bool{"⏳": true}}
	ds := testDiscordSession(rt)
	auditSink := &recordingAuditSink{}
	w := newWorker("channel-1", &fakeWorkerAgent{}, 2, 30, 1, 1440, nil, "")
	w.SetAuditSink(auditSink)
	w.setRecoveryStore(store)
	job := &Job{GuildID: "guild-1", ChannelID: "channel-1", MessageID: "message-1", Prompt: "queued", Session: ds, QueuedReactionAdded: true}
	if err := store.Put(jobRecoveryRecordFromJob(job)); err != nil {
		t.Fatal(err)
	}
	if err := w.Enqueue(job); err != nil {
		t.Fatal(err)
	}

	w.Shutdown(context.Background())

	if w.HasWork() {
		t.Fatal("worker retained queued work after shutdown")
	}
	var rejected bool
	for _, event := range auditSink.Snapshot() {
		if event.Type == "agent_job_rejected" && event.Metadata["reason"] == "service_shutdown" {
			rejected = true
		}
	}
	if !rejected {
		t.Fatalf("audit events = %#v, want service_shutdown rejection", auditSink.Snapshot())
	}
	_, bodies := rt.Snapshot()
	joined := strings.Join(bodies, "\n")
	if !strings.Contains(joined, "did not start this queued task") {
		t.Fatalf("queued shutdown reply missing: %s", joined)
	}
}

func TestManagerShutdownDeadlineDoesNotWaitForBlockedDiscordFinalization(t *testing.T) {
	L.Load("en")
	release := make(chan struct{})
	started := make(chan struct{})
	var startedOnce sync.Once
	rt := &recoveryRoundTripper{reactions: map[string]bool{"⏳": true}}
	blocking := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPatch {
			startedOnce.Do(func() { close(started) })
			<-release
		}
		return rt.RoundTrip(req)
	})
	w := newWorker("channel-1", &fakeWorkerAgent{}, 1, 30, 1, 1440, nil, "")
	w.OnOutputModeFunc(func() OutputMode { return OutputModeFolded })
	w.execute(&Job{ChannelID: "channel-1", ThreadID: "thread-1", MessageID: "message-1", Prompt: "work", Session: testDiscordSession(blocking), QueuedReactionAdded: true})
	w.cancelMu.Lock()
	done := w.currentDone
	w.cancelMu.Unlock()
	m := &Manager{
		workers:       map[string]*Worker{"channel-1": w},
		shutdownGrace: 20 * time.Millisecond,
	}

	returned := make(chan struct{})
	go func() {
		m.StopAll()
		close(returned)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not attempt to terminalize progress")
	}
	select {
	case <-returned:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("manager shutdown exceeded its configured deadline")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not finish after Discord finalization resumed")
	}
}

func TestManagerReconcilesInterruptedJobWithoutRerun(t *testing.T) {
	L.Load("en")
	store := newJobRecoveryStore(t.TempDir())
	if err := store.Put(jobRecoveryRecord{
		ID:                "job-1",
		State:             jobRecoveryActive,
		GuildID:           "guild-1",
		ChannelID:         "channel-1",
		ThreadID:          "thread-1",
		MessageID:         "message-1",
		ProgressMessageID: "progress-1",
	}); err != nil {
		t.Fatal(err)
	}
	rt := &recoveryRoundTripper{reactions: map[string]bool{"⚙️": true}}
	auditSink := &recordingAuditSink{}
	m := &Manager{recovery: store, audit: auditSink}

	count, err := m.ReconcileInterruptedJobs(testDiscordSession(rt))
	if err != nil {
		t.Fatalf("reconcile interrupted jobs: %v", err)
	}
	if count != 1 {
		t.Fatalf("reconciled count = %d, want 1", count)
	}
	if records, err := store.Records(); err != nil || len(records) != 0 {
		t.Fatalf("recovery records after reconciliation = %#v, err=%v", records, err)
	}
	if got := rt.reactionSnapshot(); len(got) != 1 || !got["⚠️"] {
		t.Fatalf("reconciled reactions = %#v, want warning only", got)
	}
	var recovered bool
	for _, event := range auditSink.Snapshot() {
		if event.Type == "agent_job_failed" && event.Metadata["reason"] == "process_restart" {
			recovered = true
		}
	}
	if !recovered {
		t.Fatalf("audit events = %#v, want process_restart failure", auditSink.Snapshot())
	}
	reqs, bodies := rt.Snapshot()
	var patched bool
	for i, req := range reqs {
		if strings.HasPrefix(req, "PATCH ") && strings.Contains(bodies[i], "previous bot stop or restart") {
			patched = true
		}
	}
	if !patched {
		t.Fatalf("recovery did not terminalize progress card: requests=%v bodies=%v", reqs, bodies)
	}
}

func TestManagerReconciliationPreservesDisabledBotEgress(t *testing.T) {
	L.Load("en")
	store := newJobRecoveryStore(t.TempDir())
	if err := store.Put(jobRecoveryRecord{
		ID:               "a2a:task-1",
		State:            jobRecoveryActive,
		GuildID:          "guild-1",
		ChannelID:        "channel-1",
		UserID:           "user-1",
		RemoteA2A:        true,
		DisableBotEgress: true,
	}); err != nil {
		t.Fatal(err)
	}
	rt := &recoveryRoundTripper{reactions: make(map[string]bool)}
	auditSink := &recordingAuditSink{}
	m := &Manager{recovery: store, audit: auditSink}

	count, err := m.ReconcileInterruptedJobs(testDiscordSession(rt))
	if err != nil || count != 1 {
		t.Fatalf("reconcile disabled-egress job count=%d err=%v", count, err)
	}
	if reqs, _ := rt.Snapshot(); len(reqs) != 0 {
		t.Fatalf("disabled-egress recovery made Discord requests: %v", reqs)
	}
	if events := auditSink.Snapshot(); len(events) != 1 || events[0].Metadata["reason"] != "process_restart" {
		t.Fatalf("disabled-egress recovery audit = %#v", events)
	}
}

func TestManagerRejectsEnqueueWhenRecoveryJournalIsUnavailable(t *testing.T) {
	L.Load("en")
	for _, test := range []struct {
		name    string
		enqueue func(*Manager, *Job) error
	}{
		{
			name: "Discord",
			enqueue: func(m *Manager, job *Job) error {
				return m.Enqueue(nil, job)
			},
		},
		{
			name: "A2A",
			enqueue: func(m *Manager, job *Job) error {
				return m.enqueueA2AJob(job)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &jobRecoveryStore{
				path:    "unavailable-journal",
				records: make(map[string]jobRecoveryRecord),
				loadErr: errors.New("disk unavailable"),
			}
			manager := &Manager{recovery: store}
			err := test.enqueue(manager, &Job{ChannelID: "channel-1", MessageID: "message-1"})
			if err == nil {
				t.Fatal("enqueue succeeded without a writable recovery journal")
			}
			if got, want := err.Error(), L.Get("worker.queue_unavailable"); got != want {
				t.Fatalf("enqueue error = %q, want sanitized %q", got, want)
			}
			if strings.Contains(err.Error(), "disk unavailable") {
				t.Fatalf("enqueue error exposed storage detail: %v", err)
			}
		})
	}
}

func TestWorkerFallbackShutdownTerminalizesOrPreservesRecovery(t *testing.T) {
	L.Load("en")
	for _, test := range []struct {
		name                string
		failProgressUpdate  bool
		wantRecoveryRecords int
	}{
		{name: "terminal delivery succeeds", wantRecoveryRecords: 0},
		{name: "terminal delivery fails", failProgressUpdate: true, wantRecoveryRecords: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newJobRecoveryStore(t.TempDir())
			base := &recoveryRoundTripper{reactions: map[string]bool{"⏳": true}}
			var transport http.RoundTripper = base
			if test.failProgressUpdate {
				transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.Method == http.MethodPatch {
						return nil, errors.New("Discord update unavailable")
					}
					return base.RoundTrip(req)
				})
			}
			ds := testDiscordSession(transport)
			agent := &fakeWorkerAgent{askWaitForContext: true}
			auditSink := &recordingAuditSink{}
			worker := newWorker("channel-1", agent, 1, 30, 1, 1440, nil, "")
			worker.SetAuditSink(auditSink)
			worker.setRecoveryStore(store)
			job := &Job{
				GuildID:             "guild-1",
				ChannelID:           "channel-1",
				MessageID:           "message-1",
				Prompt:              "work",
				Session:             ds,
				QueuedReactionAdded: true,
			}
			if err := store.Put(jobRecoveryRecordFromJob(job)); err != nil {
				t.Fatal(err)
			}
			worker.startCurrentJob(job)
			executeDone := make(chan struct{})
			go func() {
				worker.executeFallback(job, nil)
				close(executeDone)
			}()

			deadline := time.After(time.Second)
			for {
				worker.cancelMu.Lock()
				terminalReady := worker.currentTerminal != nil
				worker.cancelMu.Unlock()
				if terminalReady {
					break
				}
				select {
				case <-deadline:
					t.Fatal("fallback shutdown terminal was not installed")
				case <-time.After(time.Millisecond):
				}
			}

			worker.Shutdown(context.Background())
			select {
			case <-executeDone:
			case <-time.After(time.Second):
				t.Fatal("fallback execution did not return after shutdown")
			}
			if agent.CancelCalls() != 1 {
				t.Fatalf("agent cancel calls = %d, want 1", agent.CancelCalls())
			}
			records, err := store.Records()
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != test.wantRecoveryRecords {
				t.Fatalf("recovery records = %#v, want %d", records, test.wantRecoveryRecords)
			}
			var shutdownAudit bool
			for _, event := range auditSink.Snapshot() {
				if event.Type == "agent_job_failed" && event.Metadata["reason"] == "service_shutdown" {
					shutdownAudit = true
				}
			}
			if !shutdownAudit {
				t.Fatalf("audit events = %#v, want service_shutdown failure", auditSink.Snapshot())
			}
			if !test.failProgressUpdate {
				reqs, bodies := base.Snapshot()
				var terminalized bool
				for i, req := range reqs {
					if strings.HasPrefix(req, "PATCH ") && strings.Contains(bodies[i], "Bot is restarting") {
						terminalized = true
					}
				}
				if !terminalized {
					t.Fatalf("fallback progress was not terminalized: requests=%v bodies=%v", reqs, bodies)
				}
			}
		})
	}
}
