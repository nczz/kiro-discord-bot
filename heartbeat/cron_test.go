package heartbeat

import (
	"context"
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

func (f *fakeCronDeps) StartTempAgent(string, string, string) (*acp.Agent, error) {
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
