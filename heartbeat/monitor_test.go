package heartbeat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"

	L "github.com/nczz/kiro-discord-bot/locale"
)

type fakeMonitorDeps struct {
	result      MonitorResult
	err         error
	startErr    error
	initialized bool
	channelCWD  string

	startCalls    int
	deliverCalls  int
	recordCalls   int
	recordedState string
	notifyCalls   int
	notifyMsg     string
	deliverErr    error
}

func (f *fakeMonitorDeps) StartTempAgent(_, _, _, _ string) (*acp.Agent, error) {
	f.startCalls++
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &acp.Agent{}, nil
}

func (f *fakeMonitorDeps) StopTempAgent(*acp.Agent) {}

func (f *fakeMonitorDeps) ChannelInitialized(string) bool { return f.initialized }

func (f *fakeMonitorDeps) ChannelCWD(string) string { return f.channelCWD }

func (f *fakeMonitorDeps) EvaluateMonitor(context.Context, *acp.Agent, *MonitorJob, string) (MonitorResult, error) {
	return f.result, f.err
}

func (f *fakeMonitorDeps) DeliverMonitorNotification(*acp.Agent, *MonitorJob, MonitorResult, bool, time.Time) (string, bool, error) {
	f.deliverCalls++
	return "thread-1", f.deliverErr == nil, f.deliverErr
}

func (f *fakeMonitorDeps) RecordMonitorUsage(_ *acp.Agent, _ *MonitorJob, _, status string) {
	f.recordedState = status
}

func (f *fakeMonitorDeps) RecordMonitorResult(_ *acp.Agent, _ *MonitorJob, _ string, status string, _ MonitorResult, _ bool) {
	f.recordCalls++
	f.recordedState = status
}

func (f *fakeMonitorDeps) Notify(_ string, msg string) {
	f.notifyCalls++
	f.notifyMsg = msg
}

func TestParseMonitorResultSuppressesFalseVisibleMessage(t *testing.T) {
	got, err := ParseMonitorResult(`{"condition_matched":false,"visible_message":"do not post","internal_summary":"checked","reason":"ok"}`)
	if err != nil {
		t.Fatalf("ParseMonitorResult: %v", err)
	}
	if got.ConditionMatched || got.VisibleMessage != "" || got.InternalSummary != "checked" || got.Reason != "ok" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestParseMonitorResultRequiresVisibleMessageWhenMatched(t *testing.T) {
	if _, err := ParseMonitorResult(`{"condition_matched":true,"visible_message":"","internal_summary":"matched","reason":"threshold"}`); err == nil {
		t.Fatal("expected true monitor result without visible_message to fail")
	}
}

func TestMonitorExecuteFalseConditionCreatesNoNotification(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	deps := &fakeMonitorDeps{initialized: true, result: MonitorResult{ConditionMatched: false, InternalSummary: "green", Reason: "no failures"}}
	task := NewMonitorTask(store, deps, dir, "Asia/Taipei", "guild-1", 1)

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.deliverCalls != 0 || deps.notifyCalls != 0 {
		t.Fatalf("false condition delivered public artifact: deliver=%d notify=%d", deps.deliverCalls, deps.notifyCalls)
	}
	if deps.recordedState != MonitorStatusSuppressed {
		t.Fatalf("status = %q, want %q", deps.recordedState, MonitorStatusSuppressed)
	}
	updated, ok := store.Get("job-1")
	if !ok || updated.NextRun == "" || updated.LastCheckAt == "" {
		t.Fatalf("job was not advanced after silent check: %+v ok=%v", updated, ok)
	}
	if _, ok := task.LatestHistory("job-1"); !ok {
		t.Fatal("silent check did not write private monitor history")
	}
}

func TestMonitorHistoryRetentionLimitsRows(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", HistoryLimit: 2, Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	deps := &fakeMonitorDeps{initialized: true}
	task := NewMonitorTask(store, deps, dir, "Asia/Taipei", "guild-1", 1)

	for _, reason := range []string{"first", "second", "third"} {
		deps.result = MonitorResult{ConditionMatched: false, InternalSummary: reason, Reason: reason}
		task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))
	}

	rows := task.loadHistory("job-1", 10)
	if len(rows) != 2 {
		t.Fatalf("history rows = %d, want 2: %+v", len(rows), rows)
	}
	if rows[0].Reason != "second" || rows[1].Reason != "third" {
		t.Fatalf("history retention kept wrong rows: %+v", rows)
	}
}

func TestMonitorExecuteErrorCreatesNoNotification(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	deps := &fakeMonitorDeps{initialized: true, err: errors.New("boom")}
	task := NewMonitorTask(store, deps, dir, "Asia/Taipei", "guild-1", 1)

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.deliverCalls != 0 || deps.notifyCalls != 0 {
		t.Fatalf("error check delivered public artifact: deliver=%d notify=%d", deps.deliverCalls, deps.notifyCalls)
	}
	if deps.recordedState != MonitorStatusError {
		t.Fatalf("status = %q, want %q", deps.recordedState, MonitorStatusError)
	}
}

func TestMonitorExecuteStartFailureCreatesNoNotification(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	deps := &fakeMonitorDeps{initialized: true, startErr: errors.New("start failed")}
	task := NewMonitorTask(store, deps, dir, "Asia/Taipei", "guild-1", 1)

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.deliverCalls != 0 || deps.notifyCalls != 0 {
		t.Fatalf("start failure delivered public artifact: deliver=%d notify=%d", deps.deliverCalls, deps.notifyCalls)
	}
	if deps.recordedState != MonitorStatusError {
		t.Fatalf("status = %q, want %q", deps.recordedState, MonitorStatusError)
	}
}

func TestMonitorManualRunReportsStartFailure(t *testing.T) {
	L.Load("en")
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, RunOnce: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	deps := &fakeMonitorDeps{initialized: true, startErr: errors.New("working directory /tmp/secret is outside ALLOWED_CWD_ROOTS: /safe")}
	task := NewMonitorTask(store, deps, dir, "Asia/Taipei", "guild-1", 1)

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.notifyCalls != 0 {
		t.Fatalf("manual start failure delivered public notification: %d/%q", deps.notifyCalls, deps.notifyMsg)
	}
	history := task.loadHistory(job.ID, 10)
	if len(history) != 1 || strings.Contains(history[0].Reason, "/tmp/secret") || strings.Contains(history[0].InternalSummary, "/tmp/secret") {
		t.Fatalf("manual start failure history = %+v, want sanitized failure", history)
	}
}

func TestMonitorManualRunReportsEvaluationFailure(t *testing.T) {
	L.Load("en")
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, RunOnce: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	deps := &fakeMonitorDeps{initialized: true, err: errors.New("boom")}
	task := NewMonitorTask(store, deps, dir, "Asia/Taipei", "guild-1", 1)

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.notifyCalls != 0 {
		t.Fatalf("manual evaluation failure delivered public notification: %d/%q", deps.notifyCalls, deps.notifyMsg)
	}
	history := task.loadHistory(job.ID, 10)
	if len(history) != 1 || !strings.Contains(history[0].Reason, "check bot logs") || strings.Contains(history[0].Reason, "boom") {
		t.Fatalf("manual evaluation failure history = %+v, want sanitized private history", history)
	}
}

func TestMonitorManualRunReportsDeliveryFailure(t *testing.T) {
	L.Load("en")
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, RunOnce: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	deps := &fakeMonitorDeps{initialized: true, result: MonitorResult{ConditionMatched: true, VisibleMessage: "CI failed", InternalSummary: "red", Reason: "failure"}, deliverErr: errors.New("send failed")}
	task := NewMonitorTask(store, deps, dir, "Asia/Taipei", "guild-1", 1)

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.notifyCalls != 0 {
		t.Fatalf("manual delivery failure delivered public notification: %d/%q", deps.notifyCalls, deps.notifyMsg)
	}
	history := task.loadHistory(job.ID, 10)
	if len(history) != 1 || !strings.Contains(history[0].Reason, "check bot logs") || strings.Contains(history[0].Reason, "send failed") {
		t.Fatalf("manual delivery failure history = %+v, want sanitized private history", history)
	}
}

func TestMonitorRunOnceIsImmediatelyDue(t *testing.T) {
	task := NewMonitorTask(nil, nil, t.TempDir(), "Asia/Taipei", "guild-1", 1)
	job := &MonitorJob{
		ID:        "job-1",
		Schedule:  "*/5 * * * *",
		NextRun:   time.Now().Add(time.Hour).Format(time.RFC3339),
		RunOnce:   true,
		Enabled:   false,
		CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
	}
	if !task.isDue(job, time.Now()) {
		t.Fatal("manual run-once monitor was not immediately due")
	}
}

func TestMonitorScheduledFinishPreservesQueuedRunOnce(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	task := NewMonitorTask(store, nil, dir, "Asia/Taipei", "guild-1", 1)
	queued := *job
	queued.RunOnce = true
	if err := store.Update(&queued); err != nil {
		t.Fatal(err)
	}
	scheduledCopy := *job
	scheduledCopy.LastCheckAt = time.Now().Format(time.RFC3339)
	task.finishJob(&scheduledCopy, time.Now(), false)
	got, ok := store.Get(job.ID)
	if !ok || !got.RunOnce {
		t.Fatalf("scheduled finish cleared queued manual run: %+v ok=%v", got, ok)
	}
	task.finishJob(got, time.Now(), true)
	got, ok = store.Get(job.ID)
	if !ok || got.RunOnce {
		t.Fatalf("manual finish did not clear consumed run once: %+v ok=%v", got, ok)
	}
}

func TestMonitorStoreAddRollsBackOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "monitor", "monitor.json.tmp"), 0755); err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err == nil {
		t.Fatal("Add succeeded despite save failure")
	}
	if _, ok := store.Get("job-1"); ok {
		t.Fatal("failed Add left job in memory")
	}
}

func TestMonitorStoreUpdateRollsBackOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "monitor", "monitor.json.tmp"), 0755); err != nil {
		t.Fatal(err)
	}
	updated := *job
	updated.Name = "Updated CI"
	if err := store.Update(&updated); err == nil {
		t.Fatal("Update succeeded despite save failure")
	}
	got, ok := store.Get("job-1")
	if !ok || got.Name != "CI" {
		t.Fatalf("failed Update did not restore previous job: %+v ok=%v", got, ok)
	}
}

func TestMonitorStoreStoresDetachedCopies(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	job.Name = "mutated after add"
	added, ok := store.Get("job-1")
	if !ok || added.Name != "CI" {
		t.Fatalf("Add stored caller-owned pointer: %+v ok=%v", added, ok)
	}

	updated := *added
	updated.Name = "Updated CI"
	if err := store.Update(&updated); err != nil {
		t.Fatal(err)
	}
	updated.Name = "mutated after update"
	got, ok := store.Get("job-1")
	if !ok || got.Name != "Updated CI" {
		t.Fatalf("Update stored caller-owned pointer: %+v ok=%v", got, ok)
	}
}

func TestMonitorExecuteUninitializedChannelCreatesNoNotification(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	deps := &fakeMonitorDeps{initialized: false}
	task := NewMonitorTask(store, deps, dir, "Asia/Taipei", "guild-1", 1)

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.startCalls != 0 || deps.notifyCalls != 0 {
		t.Fatalf("uninitialized monitor produced public artifact: start=%d notify=%d", deps.startCalls, deps.notifyCalls)
	}
	updated, ok := store.Get(job.ID)
	if !ok || updated.LastCheckAt == "" {
		t.Fatalf("uninitialized monitor did not record check attempt: %+v ok=%v", updated, ok)
	}
}

func TestMonitorStoreIngestPendingCreateNormalizesSchedule(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	pendingDir := filepath.Join(dir, "monitor", "pending")
	if err := os.MkdirAll(pendingDir, 0755); err != nil {
		t.Fatal(err)
	}
	action := MonitorPendingAction{Action: "create", Job: &MonitorJob{Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "  */5 * * * *  ", CheckPrompt: "check CI", NotifyWhen: "CI fails", CreatedBy: "alice", ThreadID: "thread-untrusted", LastCheckAt: "2026-05-28T12:00:00Z", LastNotifyAt: "2026-05-28T12:00:00Z", RunOnce: true}}
	raw, _ := json.Marshal(action)
	if err := os.WriteFile(filepath.Join(pendingDir, "one.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}

	ids := store.IngestPending()
	if len(ids) != 1 {
		t.Fatalf("recalc ids = %+v, want one", ids)
	}
	job, ok := store.Get(ids[0])
	if !ok {
		t.Fatalf("created monitor not found: %s", ids[0])
	}
	if job.Schedule != "*/5 * * * *" || job.ScheduleHuman != "*/5 * * * *" || !job.Enabled || job.ThreadID != "" || job.LastCheckAt != "" || job.LastNotifyAt != "" || job.RunOnce {
		t.Fatalf("unexpected ingested monitor: %+v", job)
	}
}

func TestMonitorStoreRejectsOverlongNames(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	tooLong := strings.Repeat("a", MonitorNameMaxRunes+1)
	job := &MonitorJob{ID: "job-1", Name: tooLong, ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err == nil {
		t.Fatal("overlong monitor name accepted on create")
	}
	valid := &MonitorJob{ID: "job-2", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(valid); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyMonitorUpdate(valid, MonitorUpdate{Name: &tooLong}); err == nil {
		t.Fatal("overlong monitor name accepted on update")
	}
	longSchedule := strings.Repeat("a", MonitorScheduleMaxRunes+1)
	if _, err := ApplyMonitorUpdate(valid, MonitorUpdate{Schedule: &longSchedule}); err == nil {
		t.Fatal("overlong monitor schedule accepted on update")
	}
	longCheck := strings.Repeat("a", MonitorCheckPromptMaxRunes+1)
	if _, err := ApplyMonitorUpdate(valid, MonitorUpdate{CheckPrompt: &longCheck}); err == nil {
		t.Fatal("overlong monitor check prompt accepted on update")
	}
	longCondition := strings.Repeat("a", MonitorNotifyWhenMaxRunes+1)
	if _, err := ApplyMonitorUpdate(valid, MonitorUpdate{NotifyWhen: &longCondition}); err == nil {
		t.Fatal("overlong monitor notification condition accepted on update")
	}
}

func TestMonitorStoreLoadClearsStoredModelOverride(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "monitor")
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatal(err)
	}
	raw := `{"job-1":{"id":"job-1","name":"CI","channel_id":"channel-1","guild_id":"guild-1","schedule":"*/5 * * * *","schedule_human":"*/5 * * * *","check_prompt":"check CI","notify_when":"CI fails","model":"unvalidated-model","enabled":true,"created_by":"alice","created_at":"2026-05-28T12:00:00Z","history_limit":10}}`
	if err := os.WriteFile(filepath.Join(storeDir, "monitor.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job, ok := store.Get("job-1")
	if !ok {
		t.Fatal("loaded monitor not found")
	}
	if job.Model != "" {
		t.Fatalf("stored monitor model override = %q, want cleared", job.Model)
	}
	persisted, err := os.ReadFile(filepath.Join(storeDir, "monitor.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persisted), `"model"`) || strings.Contains(string(persisted), "unvalidated-model") {
		t.Fatalf("persisted monitor still contains stale model override: %s", persisted)
	}
}

func TestMonitorStoreLoadRejectsMalformedStore(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "invalid json", raw: `{`},
		{name: "null map", raw: `null`},
		{name: "nil entry", raw: `{"job-1":null}`},
		{name: "invalid schedule", raw: `{"job-1":{"id":"job-1","name":"CI","channel_id":"channel-1","guild_id":"guild-1","schedule":"99 99 * * *","schedule_human":"99 99 * * *","check_prompt":"check CI","notify_when":"CI fails","enabled":true,"created_by":"alice","created_at":"2026-05-28T12:00:00Z","history_limit":10}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			storeDir := filepath.Join(dir, "monitor")
			if err := os.MkdirAll(storeDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(storeDir, "monitor.json"), []byte(tc.raw), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := NewMonitorStore(dir); err == nil {
				t.Fatalf("NewMonitorStore accepted malformed store %q", tc.raw)
			}
		})
	}
}

func TestMonitorExecuteDoesNotResurrectDeletedInFlightJob(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	deps := &fakeMonitorDeps{initialized: true, result: MonitorResult{ConditionMatched: true, VisibleMessage: "CI failed", InternalSummary: "red", Reason: "failure"}}
	task := NewMonitorTask(store, deps, dir, "Asia/Taipei", "guild-1", 1)
	if err := store.Remove(job.ID); err != nil {
		t.Fatal(err)
	}

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if _, ok := store.Get(job.ID); ok {
		t.Fatal("deleted in-flight monitor was resurrected by completion")
	}
	if deps.deliverCalls != 0 {
		t.Fatalf("deleted in-flight monitor delivered notification %d time(s), want zero", deps.deliverCalls)
	}
}

func TestMonitorExecuteSkipsNotificationWhenPausedInFlight(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMonitorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := &MonitorJob{ID: "job-1", Name: "CI", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "*/5 * * * *", CheckPrompt: "check CI", NotifyWhen: "CI fails", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}
	paused := *job
	paused.Enabled = false
	if err := store.Update(&paused); err != nil {
		t.Fatal(err)
	}
	deps := &fakeMonitorDeps{initialized: true, result: MonitorResult{ConditionMatched: true, VisibleMessage: "CI failed", InternalSummary: "red", Reason: "failure"}}
	task := NewMonitorTask(store, deps, dir, "Asia/Taipei", "guild-1", 1)

	task.execute(job, time.Date(2026, 5, 28, 12, 0, 0, 0, task.location))

	if deps.deliverCalls != 0 {
		t.Fatalf("paused in-flight monitor delivered notification %d time(s), want zero", deps.deliverCalls)
	}
	got, ok := store.Get(job.ID)
	if !ok {
		t.Fatal("monitor missing after paused execution")
	}
	if got.Enabled {
		t.Fatal("paused monitor was re-enabled")
	}
}

func TestParseMonitorScheduleInputAcceptsNaturalPhrases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"週一到週五 09:00", "0 9 * * 1-5"},
		{"Monday-Friday 09:00", "0 9 * * 1-5"},
		{"每 10 分鐘", "*/10 * * * *"},
		{"every 15 minutes", "*/15 * * * *"},
		{"平日 09:00", "0 9 * * 1-5"},
		{"daily at 9am", "0 9 * * *"},
		{"daily at 9:30pm", "30 21 * * *"},
		{"every monday at 18:30", "30 18 * * 1"},
		{"每週一下午3點", "0 15 * * 1"},
		{"每天下午3點半", "30 15 * * *"},
		{"0 12 * * *", "0 12 * * *"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, human, err := ParseMonitorScheduleInput(tt.input)
			if err != nil {
				t.Fatalf("ParseMonitorScheduleInput(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseMonitorScheduleInput(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if human == "" {
				t.Fatalf("human schedule was not preserved for %q", tt.input)
			}
		})
	}
}
