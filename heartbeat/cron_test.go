package heartbeat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
)

type fakeCronDeps struct {
	askErr        error
	recordCalls   int
	recordJobID   string
	recordThread  string
	recordStatus  string
	responseCalls int
	responseSent  bool
	askSent       bool
	askSentSet    bool
	noThread      bool
}

func (f *fakeCronDeps) StartTempAgent(string, string, string, string) (*acp.Agent, error) {
	return &acp.Agent{}, nil
}

func (f *fakeCronDeps) StopTempAgent(*acp.Agent) {}

func (f *fakeCronDeps) AskAgentInThread(context.Context, *acp.Agent, string, string, string, string, string, string) (string, string, bool, error) {
	threadID := "thread-1"
	if f.noThread {
		threadID = ""
	}
	responseSent := true
	if f.askSentSet {
		responseSent = f.askSent
	}
	if f.askErr != nil {
		return "", threadID, responseSent, f.askErr
	}
	return "ok", threadID, responseSent, nil
}

func (f *fakeCronDeps) RecordAgentUsage(_ *acp.Agent, job *CronJob, threadID, status string) {
	f.recordCalls++
	f.recordJobID = job.ID
	f.recordThread = threadID
	f.recordStatus = status
}

func (f *fakeCronDeps) RecordAgentResponse(_ *acp.Agent, _ *CronJob, _, _, _ string, responseSent bool) {
	f.responseCalls++
	f.responseSent = responseSent
}

func (f *fakeCronDeps) Notify(string, string) {}

func TestCronExecuteRecordsAgentUsage(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := &fakeCronDeps{}
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1")
	job := &CronJob{
		ID:          "job-1",
		Name:        "Daily",
		ChannelID:   "channel-1",
		GuildID:     "guild-1",
		Schedule:    "0 0 * * *",
		Prompt:      "Run",
		Enabled:     true,
		CreatedBy:   "alice",
		CreatedByID: "user-1",
	}

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.recordCalls != 1 {
		t.Fatalf("record calls = %d, want 1", deps.recordCalls)
	}
	if deps.recordJobID != "job-1" || deps.recordThread != "thread-1" || deps.recordStatus != "ok" {
		t.Fatalf("recorded job/thread/status = %q/%q/%q", deps.recordJobID, deps.recordThread, deps.recordStatus)
	}
	if deps.responseCalls != 1 {
		t.Fatalf("response calls = %d, want 1", deps.responseCalls)
	}
	if !deps.responseSent {
		t.Fatal("responseSent = false, want true")
	}
}

func TestCronExecuteMarksResponseNotSentWhenSetupFails(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := &fakeCronDeps{askErr: context.Canceled, askSentSet: true, noThread: true}
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1")
	job := &CronJob{
		ID:        "job-1",
		Name:      "Daily",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Schedule:  "0 0 * * *",
		Prompt:    "Run",
		Enabled:   true,
	}
	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.responseCalls != 1 {
		t.Fatalf("response calls = %d, want 1", deps.responseCalls)
	}
	if deps.responseSent {
		t.Fatal("responseSent = true, want false when setup fails before a thread is available")
	}
}

func TestCronExecuteRecordsUnsentAgentResponse(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := &fakeCronDeps{askSentSet: true}
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1")
	job := &CronJob{
		ID:        "job-1",
		Name:      "Daily",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Schedule:  "0 0 * * *",
		Prompt:    "Run",
		Enabled:   true,
	}
	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.responseCalls != 1 {
		t.Fatalf("response calls = %d, want 1", deps.responseCalls)
	}
	if deps.responseSent {
		t.Fatal("responseSent = true, want false when Discord delivery fails")
	}
	if deps.recordStatus != "ok" {
		t.Fatalf("record status = %q, want agent execution status to remain ok", deps.recordStatus)
	}
}

func TestCronRecalcAllPreservesOverdueNextRun(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task := NewCronTask(store, &fakeCronDeps{}, t.TempDir(), "Asia/Taipei", "guild-1")
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, task.location)
	overdue := time.Date(2026, 6, 11, 9, 0, 0, 0, task.location).Format(time.RFC3339)
	job := &CronJob{
		ID:        "job-overdue",
		Name:      "Daily",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Schedule:  "0 9 * * *",
		Prompt:    "Run",
		Enabled:   true,
		NextRun:   overdue,
	}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}

	task.recalcAllAt(now)

	got, ok := store.Get("job-overdue")
	if !ok {
		t.Fatal("missing job")
	}
	if got.NextRun != overdue {
		t.Fatalf("overdue next_run changed to %s, want %s", got.NextRun, overdue)
	}
	if !task.isDue(got, now) {
		t.Fatal("overdue job should remain due after startup recalculation")
	}
}

func TestCronRecalcAllRefreshesFutureNextRun(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task := NewCronTask(store, &fakeCronDeps{}, t.TempDir(), "Asia/Taipei", "guild-1")
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, task.location)
	staleFuture := time.Date(2026, 6, 20, 9, 0, 0, 0, task.location).Format(time.RFC3339)
	want := time.Date(2026, 6, 12, 9, 0, 0, 0, task.location).Format(time.RFC3339)
	job := &CronJob{
		ID:        "job-future",
		Name:      "Daily",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Schedule:  "0 9 * * *",
		Prompt:    "Run",
		Enabled:   true,
		NextRun:   staleFuture,
	}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}

	task.recalcAllAt(now)

	got, ok := store.Get("job-future")
	if !ok {
		t.Fatal("missing job")
	}
	if got.NextRun != want {
		t.Fatalf("future next_run = %s, want %s", got.NextRun, want)
	}
}

func TestCronStoreIngestPendingValidatesAndCreatesJobs(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCronStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	pendingDir := filepath.Join(dir, "cron", "pending")
	if err := os.MkdirAll(pendingDir, 0755); err != nil {
		t.Fatal(err)
	}
	writePendingAction(t, filepath.Join(pendingDir, "bad.json"), PendingAction{
		Action: "create",
		Job: &PendingJob{
			Name:      "bad",
			Schedule:  "not cron",
			Prompt:    "run",
			ChannelID: "channel-1",
			GuildID:   "guild-1",
		},
	})
	writePendingAction(t, filepath.Join(pendingDir, "good.json"), PendingAction{
		Action: "create",
		Job: &PendingJob{
			Name:      " good ",
			Schedule:  "0 9 * * *",
			Prompt:    " run ",
			ChannelID: " channel-1 ",
			GuildID:   " guild-1 ",
			CreatedBy: " alice ",
		},
	})

	created := store.IngestPending()
	if len(created) != 1 {
		t.Fatalf("created = %+v, want one job", created)
	}
	jobs := store.ListByChannel("channel-1")
	if len(jobs) != 1 {
		t.Fatalf("jobs = %+v, want one job", jobs)
	}
	if jobs[0].Name != "good" || jobs[0].Prompt != "run" || jobs[0].GuildID != "guild-1" || jobs[0].CreatedBy != "alice" {
		t.Fatalf("job was not normalized: %+v", jobs[0])
	}
	if _, err := os.Stat(filepath.Join(pendingDir, "bad.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid pending file should be removed, stat err=%v", err)
	}
}

func TestCronStoreIngestPendingDeleteRequiresMatchingChannel(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCronStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &CronJob{
		ID:        "job-1",
		Name:      "Daily",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Schedule:  "0 9 * * *",
		Prompt:    "Run",
		Enabled:   true,
	}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	pendingDir := filepath.Join(dir, "cron", "pending")
	if err := os.MkdirAll(pendingDir, 0755); err != nil {
		t.Fatal(err)
	}
	writePendingAction(t, filepath.Join(pendingDir, "wrong-channel.json"), PendingAction{
		Action:    "delete",
		JobID:     "job-1",
		ChannelID: "channel-2",
	})
	store.IngestPending()
	if _, ok := store.Get("job-1"); !ok {
		t.Fatal("job was deleted from the wrong channel")
	}

	writePendingAction(t, filepath.Join(pendingDir, "right-channel.json"), PendingAction{
		Action:    "delete",
		JobID:     "job-1",
		ChannelID: "channel-1",
	})
	store.IngestPending()
	if _, ok := store.Get("job-1"); ok {
		t.Fatal("job was not deleted from the owning channel")
	}
}

func writePendingAction(t *testing.T, path string, action PendingAction) {
	t.Helper()
	raw, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
}
