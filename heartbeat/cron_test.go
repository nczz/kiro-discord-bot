package heartbeat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
)

type fakeCronDeps struct {
	askErr               error
	channelCWD           string
	uninitialized        bool
	startCalls           int
	startCWD             string
	recordCalls          int
	recordJobID          string
	recordThread         string
	recordStatus         string
	responseCalls        int
	responseSent         bool
	notifyMentionChannel string
	notifyMentionMsg     string
	notifyMentionUser    string
	askJobID             string
	askSent              bool
	askSentSet           bool
	noThread             bool
}

func (f *fakeCronDeps) StartTempAgent(_, cwd, _, _ string) (*acp.Agent, error) {
	f.startCalls++
	f.startCWD = cwd
	return &acp.Agent{}, nil
}

func (f *fakeCronDeps) StopTempAgent(*acp.Agent) {}

func (f *fakeCronDeps) ChannelInitialized(string) bool {
	return !f.uninitialized
}

func (f *fakeCronDeps) ChannelCWD(string) string {
	if f.channelCWD == "" {
		return "/channel/default"
	}
	return f.channelCWD
}

func (f *fakeCronDeps) AskAgentInThread(_ context.Context, _ *acp.Agent, job *CronJob, _, _ string) (string, string, bool, error) {
	f.askJobID = job.ID
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
func (f *fakeCronDeps) NotifyMention(channelID, msg, userID string) {
	f.notifyMentionChannel = channelID
	f.notifyMentionMsg = msg
	f.notifyMentionUser = userID
}

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
	if deps.askJobID != "job-1" {
		t.Fatalf("ask job ID = %q, want job-1", deps.askJobID)
	}
	if deps.responseCalls != 1 {
		t.Fatalf("response calls = %d, want 1", deps.responseCalls)
	}
	if !deps.responseSent {
		t.Fatal("responseSent = false, want true")
	}
}

func TestCronExecuteUsesCurrentChannelCWD(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := &fakeCronDeps{channelCWD: "/projects/current"}
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1")
	job := &CronJob{
		ID:        "job-1",
		Name:      "Daily",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Schedule:  "0 0 * * *",
		Prompt:    "Run",
		CWD:       "/legacy/job/cwd",
		Enabled:   true,
	}

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.startCWD != "/projects/current" {
		t.Fatalf("StartTempAgent cwd = %q, want current channel cwd", deps.startCWD)
	}
}

func TestCronExecuteBlocksUninitializedChannel(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := &fakeCronDeps{uninitialized: true}
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

	if deps.startCalls != 0 {
		t.Fatalf("StartTempAgent calls = %d, want no agent start", deps.startCalls)
	}
	if deps.recordCalls != 0 || deps.responseCalls != 0 {
		t.Fatalf("record calls = %d/%d, want no agent records", deps.recordCalls, deps.responseCalls)
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

func TestCronExecuteOneShotReminderUsesMentionNotification(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := &fakeCronDeps{}
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1")
	now := time.Date(2026, 7, 6, 14, 30, 0, 0, task.location)
	job := &CronJob{
		ID:        "reminder-1",
		Name:      "Drink water",
		ChannelID: "thread-1",
		GuildID:   "guild-1",
		Prompt:    "drink water",
		Enabled:   true,
		OneShot:   true,
		NextRun:   now.Format(time.RFC3339),
		MentionID: "user-2",
	}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}

	task.execute(job, now)

	if deps.notifyMentionChannel != "thread-1" {
		t.Fatalf("NotifyMention channel = %q, want thread-1", deps.notifyMentionChannel)
	}
	if deps.notifyMentionUser != "user-2" {
		t.Fatalf("NotifyMention user = %q, want user-2", deps.notifyMentionUser)
	}
	if deps.notifyMentionMsg != "🔔 <@user-2> drink water" {
		t.Fatalf("NotifyMention msg = %q", deps.notifyMentionMsg)
	}
	if _, ok := store.Get("reminder-1"); ok {
		t.Fatal("one-shot reminder should be removed after firing")
	}
}

func TestCronBuildPromptDisplaysHistoryInCronTimezone(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task := NewCronTask(store, &fakeCronDeps{}, t.TempDir(), "Asia/Taipei", "guild-1")
	job := &CronJob{
		ID:        "job-1",
		Name:      "Daily",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Schedule:  "0 8 * * *",
		Prompt:    "Run report",
		Enabled:   true,
	}

	prompt := task.buildPrompt(job, []CronHistory{{
		Timestamp: "2026-06-13T00:30:00Z",
		Status:    "ok",
		Response:  "done",
	}})

	if !strings.Contains(prompt, "[06/13 08:30]") {
		t.Fatalf("history timestamp was not rendered in Asia/Taipei:\n%s", prompt)
	}
	if strings.Contains(prompt, "[06/13 00:30]") {
		t.Fatalf("history timestamp leaked UTC time:\n%s", prompt)
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

func TestCronTaskRunIngestsPendingWhenNoJobsExist(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCronStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	pendingDir := filepath.Join(dir, "cron", "pending")
	if err := os.MkdirAll(pendingDir, 0755); err != nil {
		t.Fatal(err)
	}
	writePendingAction(t, filepath.Join(pendingDir, "create.json"), PendingAction{
		Action: "create",
		Job: &PendingJob{
			Name:      "Morning check",
			Schedule:  "0 9 * * *",
			Prompt:    "run health check",
			ChannelID: "channel-1",
			GuildID:   "guild-1",
			CreatedBy: "agent",
		},
	})
	task := NewCronTask(store, &fakeCronDeps{}, dir, "Asia/Taipei", "guild-1")

	if !task.ShouldRun(time.Now()) {
		t.Fatal("cron task should run even with no stored jobs so pending actions can be ingested")
	}
	if err := task.Run(); err != nil {
		t.Fatal(err)
	}

	jobs := store.ListByChannel("channel-1")
	if len(jobs) != 1 {
		t.Fatalf("jobs = %+v, want one pending-created job", jobs)
	}
	if jobs[0].NextRun == "" {
		t.Fatalf("next_run was not calculated for pending-created job: %+v", jobs[0])
	}
	if _, err := os.Stat(filepath.Join(pendingDir, "create.json")); !os.IsNotExist(err) {
		t.Fatalf("pending action should be removed after ingest, stat err=%v", err)
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
			Name:        " good ",
			Schedule:    "0 9 * * *",
			Prompt:      " run ",
			ChannelID:   " channel-1 ",
			GuildID:     " guild-1 ",
			CreatedBy:   " alice ",
			CreatedByID: " user-1 ",
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
	if jobs[0].Name != "good" || jobs[0].Prompt != "run" || jobs[0].GuildID != "guild-1" || jobs[0].CreatedBy != "alice" || jobs[0].CreatedByID != "user-1" {
		t.Fatalf("job was not normalized: %+v", jobs[0])
	}
	if _, err := os.Stat(filepath.Join(pendingDir, "bad.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid pending file should be removed, stat err=%v", err)
	}
}

func TestCronStoreIngestPendingCreatesOneShotReminder(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCronStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	pendingDir := filepath.Join(dir, "cron", "pending")
	if err := os.MkdirAll(pendingDir, 0755); err != nil {
		t.Fatal(err)
	}
	nextRun := time.Date(2026, 7, 6, 14, 30, 0, 0, time.FixedZone("TST", 8*3600)).Format(time.RFC3339)
	writePendingAction(t, filepath.Join(pendingDir, "reminder.json"), PendingAction{
		Action: "create_reminder",
		Job: &PendingJob{
			Name:          " drink ",
			ScheduleHuman: " +2m ",
			Prompt:        " drink water ",
			ChannelID:     " thread-1 ",
			GuildID:       " guild-1 ",
			CreatedBy:     " alice ",
			CreatedByID:   " user-1 ",
			NextRun:       nextRun,
			MentionID:     " user-2 ",
			OneShot:       true,
		},
	})

	created := store.IngestPending()
	if len(created) != 1 {
		t.Fatalf("created = %+v, want one reminder", created)
	}
	jobs := store.ListByChannel("thread-1")
	if len(jobs) != 1 {
		t.Fatalf("jobs = %+v, want one thread reminder", jobs)
	}
	got := jobs[0]
	if !got.OneShot || got.UseAgent || got.Schedule != "" || got.Name != "drink" || got.Prompt != "drink water" || got.GuildID != "guild-1" || got.CreatedBy != "alice" || got.CreatedByID != "user-1" || got.MentionID != "user-2" || got.NextRun != nextRun {
		t.Fatalf("reminder was not normalized into a one-shot job: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(pendingDir, "reminder.json")); !os.IsNotExist(err) {
		t.Fatalf("pending reminder should be removed after ingest, stat err=%v", err)
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
