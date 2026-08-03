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
	L "github.com/nczz/kiro-discord-bot/locale"
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
	responseContent      string
	notifyChannel        string
	notifyMsg            string
	notifyMentionChannel string
	notifyMentionMsg     string
	notifyMentionUser    string
	askJobID             string
	askDeadline          time.Time
	askDeadlineSet       bool
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

func (f *fakeCronDeps) AskAgentInThread(ctx context.Context, _ *acp.Agent, job *CronJob, _, _ string) (string, string, bool, error) {
	if deadline, ok := ctx.Deadline(); ok {
		f.askDeadline = deadline
		f.askDeadlineSet = true
	}
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

func (f *fakeCronDeps) RecordAgentResponse(_ *acp.Agent, _ *CronJob, _, _, content string, responseSent bool) {
	f.responseCalls++
	f.responseContent = content
	f.responseSent = responseSent
}
func (f *fakeCronDeps) Notify(channelID, msg string) {
	f.notifyChannel = channelID
	f.notifyMsg = msg
}

func (f *fakeCronDeps) NotifyMention(channelID, msg, userID string) {
	f.notifyMentionChannel = channelID
	f.notifyMentionMsg = msg
	f.notifyMentionUser = userID
}

func TestNewCronTaskAppliesConfiguredTimeoutAndDefault(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	task := NewCronTask(store, &fakeCronDeps{}, t.TempDir(), "Asia/Taipei", "guild-1", 2)
	if task.timeout != 2*time.Minute {
		t.Fatalf("timeout = %s, want 2m", task.timeout)
	}

	task = NewCronTask(store, &fakeCronDeps{}, t.TempDir(), "Asia/Taipei", "guild-1", 0)
	if task.timeout != 5*time.Minute {
		t.Fatalf("zero timeout fallback = %s, want 5m", task.timeout)
	}

	task = NewCronTask(store, &fakeCronDeps{}, t.TempDir(), "Asia/Taipei", "guild-1", -1)
	if task.timeout != 5*time.Minute {
		t.Fatalf("negative timeout fallback = %s, want 5m", task.timeout)
	}
}

func TestCronExecuteUsesConfiguredTimeout(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := &fakeCronDeps{}
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1", 1)
	job := &CronJob{
		ID:        "job-1",
		Name:      "Daily",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Schedule:  "0 0 * * *",
		Prompt:    "Run",
		Enabled:   true,
	}

	before := time.Now().Add(time.Minute)
	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))
	after := time.Now().Add(time.Minute)

	if !deps.askDeadlineSet {
		t.Fatal("AskAgentInThread context had no deadline")
	}
	if deps.askDeadline.Before(before.Add(-5*time.Second)) || deps.askDeadline.After(after.Add(5*time.Second)) {
		t.Fatalf("deadline = %s, want about one minute from execution window [%s, %s]", deps.askDeadline, before, after)
	}
}

func TestCronExecuteLocalizesTimeoutFailure(t *testing.T) {
	L.Load("zh-TW")
	t.Cleanup(func() { L.Load("en") })

	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := &fakeCronDeps{askErr: context.DeadlineExceeded}
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1", 10)
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

	if strings.Contains(deps.notifyMsg, "context deadline exceeded") {
		t.Fatalf("parent notification leaked raw context error: %q", deps.notifyMsg)
	}
	if !strings.Contains(deps.notifyMsg, "執行超過 10 分鐘，已取消") {
		t.Fatalf("parent notification = %q, want localized timeout reason", deps.notifyMsg)
	}
	if deps.responseContent != "執行超過 10 分鐘，已取消" {
		t.Fatalf("recorded response = %q, want localized timeout reason", deps.responseContent)
	}
}

func TestCronExecuteRecordsAgentUsage(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := &fakeCronDeps{}
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1", 5)
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
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1", 5)
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
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1", 5)
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
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1", 5)
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
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1", 5)
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
	task := NewCronTask(store, deps, t.TempDir(), "Asia/Taipei", "guild-1", 5)
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
	task := NewCronTask(store, &fakeCronDeps{}, t.TempDir(), "Asia/Taipei", "guild-1", 5)
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
	task := NewCronTask(store, &fakeCronDeps{}, t.TempDir(), "Asia/Taipei", "guild-1", 5)
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
	task := NewCronTask(store, &fakeCronDeps{}, t.TempDir(), "Asia/Taipei", "guild-1", 5)
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
	task := NewCronTask(store, &fakeCronDeps{}, dir, "Asia/Taipei", "guild-1", 5)

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

func TestCronTaskRunIngestsPendingUpdateAndRecalculatesChangedSchedule(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCronStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	oldNext := "2099-01-01T09:00:00+08:00"
	job := &CronJob{
		ID:            "job-1",
		Name:          "Daily",
		ChannelID:     "channel-1",
		GuildID:       "guild-1",
		Schedule:      "0 9 * * *",
		ScheduleHuman: "0 9 * * *",
		Prompt:        "Run",
		Enabled:       true,
		NextRun:       oldNext,
	}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	pendingDir := filepath.Join(dir, "cron", "pending")
	if err := os.MkdirAll(pendingDir, 0755); err != nil {
		t.Fatal(err)
	}
	newSchedule := "0 10 * * *"
	writePendingAction(t, filepath.Join(pendingDir, "update.json"), PendingAction{
		Action:    "update",
		JobID:     "job-1",
		ChannelID: "channel-1",
		Update:    &CronUpdate{Schedule: &newSchedule},
	})
	task := NewCronTask(store, &fakeCronDeps{}, dir, "Asia/Taipei", "guild-1", 5)

	if err := task.Run(); err != nil {
		t.Fatal(err)
	}

	got, ok := store.Get("job-1")
	if !ok {
		t.Fatal("missing updated job")
	}
	if got.Schedule != newSchedule || got.ScheduleHuman != newSchedule {
		t.Fatalf("schedule was not updated: %+v", got)
	}
	if got.NextRun == "" || got.NextRun == oldNext {
		t.Fatalf("next_run was not recalculated after changed schedule: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(pendingDir, "update.json")); !os.IsNotExist(err) {
		t.Fatalf("pending update should be removed after ingest, stat err=%v", err)
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

func TestApplyCronUpdateValidatesBeforeMutation(t *testing.T) {
	job := &CronJob{Name: "Daily", Schedule: "0 9 * * *", ScheduleHuman: "0 9 * * *", Prompt: "Run", Enabled: true}
	empty, badSchedule := "  ", "not a cron"
	if _, err := ApplyCronUpdate(job, CronUpdate{Name: &empty, Schedule: &badSchedule}); err == nil {
		t.Fatal("expected invalid update")
	}
	if job.Name != "Daily" || job.Schedule != "0 9 * * *" {
		t.Fatalf("invalid update partially mutated job: %+v", job)
	}
	if _, err := ApplyCronUpdate(job, CronUpdate{}); err == nil {
		t.Fatal("expected empty update rejection")
	}
}

func TestApplyCronUpdateDisablesAndResumesWithoutDelete(t *testing.T) {
	job := &CronJob{ID: "job-1", Name: "Daily", Schedule: "0 9 * * *", ScheduleHuman: "0 9 * * *", Prompt: "Run", Enabled: true, NextRun: "2026-07-18T09:00:00+08:00"}
	disabled := false
	recalc, err := ApplyCronUpdate(job, CronUpdate{Enabled: &disabled})
	if err != nil || recalc || job.Enabled {
		t.Fatalf("disable result recalc=%v err=%v job=%+v", recalc, err, job)
	}
	enabled := true
	recalc, err = ApplyCronUpdate(job, CronUpdate{Enabled: &enabled})
	if err != nil || !recalc || !job.Enabled {
		t.Fatalf("resume result recalc=%v err=%v job=%+v", recalc, err, job)
	}
}

func TestApplyCronUpdateSameScheduleDoesNotRecalculate(t *testing.T) {
	job := &CronJob{ID: "job-1", Name: "Daily", Schedule: "0 9 * * *", ScheduleHuman: "0 9 * * *", Prompt: "Run", Enabled: true}
	sameSchedule := " 0 9 * * * "
	recalc, err := ApplyCronUpdate(job, CronUpdate{Schedule: &sameSchedule})
	if err != nil {
		t.Fatal(err)
	}
	if recalc {
		t.Fatalf("same schedule update requested next_run recalculation: %+v", job)
	}
	if job.Schedule != "0 9 * * *" || job.ScheduleHuman != "0 9 * * *" {
		t.Fatalf("same schedule update was not normalized: %+v", job)
	}

	newSchedule := "0 10 * * *"
	recalc, err = ApplyCronUpdate(job, CronUpdate{Schedule: &newSchedule})
	if err != nil {
		t.Fatal(err)
	}
	if !recalc || job.Schedule != newSchedule {
		t.Fatalf("changed schedule did not request recalculation: recalc=%v job=%+v", recalc, job)
	}
}

func TestCronStoreIngestPendingUpdatesOnlyOwningRecurringJob(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCronStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &CronJob{ID: "job-1", Name: "Daily", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "0 9 * * *", ScheduleHuman: "0 9 * * *", Prompt: "Run", Enabled: true}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	pendingDir := filepath.Join(dir, "cron", "pending")
	if err := os.MkdirAll(pendingDir, 0755); err != nil {
		t.Fatal(err)
	}
	disabled, newPrompt := false, "Run safely"
	writePendingAction(t, filepath.Join(pendingDir, "wrong.json"), PendingAction{Action: "update", JobID: "job-1", ChannelID: "channel-2", Update: &CronUpdate{Enabled: &disabled}})
	store.IngestPending()
	if got, _ := store.Get("job-1"); !got.Enabled {
		t.Fatal("cross-channel update disabled job")
	}
	writePendingAction(t, filepath.Join(pendingDir, "right.json"), PendingAction{Action: "update", JobID: "job-1", ChannelID: "channel-1", Update: &CronUpdate{Enabled: &disabled, Prompt: &newPrompt}})
	store.IngestPending()
	got, ok := store.Get("job-1")
	if !ok || got.Enabled || got.Prompt != newPrompt {
		t.Fatalf("unexpected updated job: %+v", got)
	}
}

func TestApplyCronUpdateRejectsOneShotReminder(t *testing.T) {
	job := &CronJob{OneShot: true, Name: "Reminder", Prompt: "Drink", Enabled: true}
	disabled := false
	if _, err := ApplyCronUpdate(job, CronUpdate{Enabled: &disabled}); err == nil {
		t.Fatal("expected one-shot update rejection")
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
