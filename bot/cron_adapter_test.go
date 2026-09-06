package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/internal/botegress"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestCronAdapterRecordAgentResponseDistinguishesUnsentFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := audit.Open(audit.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder := audit.NewRecorder(store, 10, nil, false)
	adapter := &cronAdapter{botNotifier{bot: &Bot{auditRecorder: recorder}}}

	adapter.RecordAgentResponse(&acp.Agent{}, &heartbeat.CronJob{
		ID:        "job-1",
		Name:      "Daily",
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		CreatedBy: "alice",
	}, "", "error", "create thread: missing access", false)
	recorder.Close()

	db, err := sql.Open("sqlite", filepath.Join(dir, "audit", "discord.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	var raw, eventType string
	if err := db.QueryRowContext(context.Background(), `SELECT raw_json, event_type FROM bot_audit_events`).Scan(&raw, &eventType); err != nil {
		t.Fatalf("query bot audit event: %v", err)
	}
	if eventType != "agent_response_failed" {
		t.Fatalf("event type = %q, want agent_response_failed", eventType)
	}
	var evt audit.BotEvent
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal raw event: %v", err)
	}
	if evt.Type != "agent_response_failed" || evt.Metadata["response_sent"] != false || evt.Metadata["failure_stage"] != "delivery_setup" {
		t.Fatalf("event = %+v, want unsent failure metadata", evt)
	}
}

func TestCronAdapterRecordAgentResponseStoresSentResponse(t *testing.T) {
	dir := t.TempDir()
	store, err := audit.Open(audit.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder := audit.NewRecorder(store, 10, nil, false)
	adapter := &cronAdapter{botNotifier{bot: &Bot{auditRecorder: recorder}}}

	adapter.RecordAgentResponse(&acp.Agent{}, &heartbeat.CronJob{
		ID:        "job-1",
		Name:      "Daily",
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		CreatedBy: "alice",
	}, "thread-1", "ok", "done", true)
	recorder.Close()

	db, err := sql.Open("sqlite", filepath.Join(dir, "audit", "discord.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	var raw, eventType string
	if err := db.QueryRowContext(context.Background(), `SELECT raw_json, event_type FROM bot_audit_events`).Scan(&raw, &eventType); err != nil {
		t.Fatalf("query bot audit event: %v", err)
	}
	if eventType != "agent_response_sent" {
		t.Fatalf("event type = %q, want agent_response_sent", eventType)
	}
	var evt audit.BotEvent
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal raw event: %v", err)
	}
	if evt.Type != "agent_response_sent" || evt.ThreadID != "thread-1" || evt.Metadata["response_sent"] != true {
		t.Fatalf("event = %+v, want sent response metadata", evt)
	}
	if _, ok := evt.Metadata["failure_stage"]; ok {
		t.Fatalf("event metadata = %+v, failure_stage should be absent for sent responses", evt.Metadata)
	}
}

func TestCronAdapterRecordToolAuditUsesJobContext(t *testing.T) {
	dir := t.TempDir()
	store, err := audit.Open(audit.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder := audit.NewRecorder(store, 10, nil, false)
	adapter := &cronAdapter{botNotifier{bot: &Bot{guildID: "guild-1", auditRecorder: recorder}}}

	adapter.recordCronToolAuditEvent(&acp.Agent{}, &heartbeat.CronJob{
		ID:          "job-1",
		Name:        "Daily report",
		GuildID:     "guild-1",
		ChannelID:   "channel-1",
		CreatedBy:   "alice",
		CreatedByID: "user-1",
		Model:       "model-1",
		OneShot:     true,
	}, "thread-1", "agent_tool_result", acp.ToolCallEvent{
		Kind:       "execute",
		Title:      "Run report",
		ToolCallID: "tool-1",
		Status:     "completed",
	})
	recorder.Close()

	db, err := sql.Open("sqlite", filepath.Join(dir, "audit", "discord.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	var raw string
	if err := db.QueryRowContext(context.Background(), `SELECT raw_json FROM bot_audit_events WHERE event_type='agent_tool_result'`).Scan(&raw); err != nil {
		t.Fatalf("query bot audit event: %v", err)
	}
	var evt audit.BotEvent
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal raw event: %v", err)
	}
	if evt.Source != "reminder" || evt.JobID != "job-1" || evt.TargetID != "thread-1" || evt.ThreadID != "thread-1" {
		t.Fatalf("event routing fields = source:%q job:%q target:%q thread:%q", evt.Source, evt.JobID, evt.TargetID, evt.ThreadID)
	}
	if evt.Model != "model-1" || evt.Command != "Run report" || evt.Status != "completed" {
		t.Fatalf("event model/command/status = %q/%q/%q", evt.Model, evt.Command, evt.Status)
	}
	if evt.Metadata["cron_job_name"] != "Daily report" || evt.Metadata["tool_call_id"] != "tool-1" || evt.Metadata["kind"] != "execute" {
		t.Fatalf("event metadata = %+v", evt.Metadata)
	}
}

func TestCronAdapterDrainSafeEgressFlushesThreadTarget(t *testing.T) {
	dir := t.TempDir()
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	b := &Bot{discord: ds, dataDir: dir}
	b.safeEgress = newSafeEgressTask(b)
	adapter := &cronAdapter{botNotifier{bot: b}}

	if _, err := botegress.WritePending(dir, botegress.Action{
		ID:        "cron-thread",
		Action:    botegress.ActionSendMessage,
		ChannelID: "thread-1",
		Content:   "cron tool payload",
		CreatedAt: "2026-06-12T00:00:00Z",
	}); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	adapter.drainSafeEgress("thread-1")

	paths, bodies := rt.Snapshot()
	if len(paths) != 1 || !strings.Contains(paths[0], "/channels/thread-1/messages") || !strings.Contains(bodies[0], "cron tool payload") {
		t.Fatalf("unexpected drained calls: paths=%v bodies=%v", paths, bodies)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("pending actions = %+v, want empty", actions)
	}
}

func TestCronAdapterPrepareCronThreadAddsCreatorOnlyForNewThread(t *testing.T) {
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	manager := channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir()})
	defer manager.StopAll()
	adapter := &cronAdapter{botNotifier{bot: &Bot{discord: ds, manager: manager}}}

	threadID, created, err := adapter.prepareCronThread(ds, &heartbeat.CronJob{
		ChannelID:   "channel-1",
		CreatedByID: "user-1",
	}, "Daily report")
	if err != nil {
		t.Fatalf("prepareCronThread: %v", err)
	}
	if !created || threadID == "" {
		t.Fatalf("prepareCronThread created=%v threadID=%q, want new thread", created, threadID)
	}
	paths, _ := rt.Snapshot()
	if !containsPath(paths, "/thread-members/user-1") {
		t.Fatalf("new cron thread should add creator once; paths=%v", paths)
	}
}

func TestCronAdapterPrepareCronThreadDoesNotReaddCreatorForExistingThread(t *testing.T) {
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	if err := ds.State.GuildAdd(&discordgo.Guild{ID: "guild-1"}); err != nil {
		t.Fatal(err)
	}
	if err := ds.State.ChannelAdd(&discordgo.Channel{ID: "thread-1", GuildID: "guild-1", ParentID: "channel-1", Type: discordgo.ChannelTypeGuildPublicThread}); err != nil {
		t.Fatal(err)
	}
	manager := channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir()})
	defer manager.StopAll()
	adapter := &cronAdapter{botNotifier{bot: &Bot{discord: ds, manager: manager}}}

	threadID, created, err := adapter.prepareCronThread(ds, &heartbeat.CronJob{
		ChannelID:   "channel-1",
		ThreadID:    "thread-1",
		CreatedByID: "user-1",
	}, "Daily report")
	if err != nil {
		t.Fatalf("prepareCronThread: %v", err)
	}
	if created || threadID != "thread-1" {
		t.Fatalf("prepareCronThread created=%v threadID=%q, want existing thread-1", created, threadID)
	}
	paths, _ := rt.Snapshot()
	if containsPath(paths, "/thread-members/user-1") {
		t.Fatalf("existing cron thread must not re-add creator who may have left; paths=%v", paths)
	}
}

func containsPath(paths []string, fragment string) bool {
	for _, path := range paths {
		if strings.Contains(path, fragment) {
			return true
		}
	}
	return false
}

func TestBuildCronCardDisplaysRunsInCronTimezone(t *testing.T) {
	L.Load("en")
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	job := &heartbeat.CronJob{
		ID:       "job-1",
		Name:     "Daily",
		Enabled:  true,
		Schedule: "0 8 * * *",
		Prompt:   "Run report",
		LastRun:  "2026-06-13T00:30:00Z",
		NextRun:  "2026-06-14T00:30:00Z",
	}

	content, _ := buildCronCard(job, loc)

	if !strings.Contains(content, "06/13 08:30") || !strings.Contains(content, "06/14 08:30") {
		t.Fatalf("cron card did not render RFC3339 timestamps in Asia/Taipei:\n%s", content)
	}
	if strings.Contains(content, "06/13 00:30") || strings.Contains(content, "06/14 00:30") {
		t.Fatalf("cron card leaked UTC timestamps:\n%s", content)
	}
}

func TestBuildCronCardRendersOneShotReminderWithoutCronControls(t *testing.T) {
	L.Load("en")
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	job := &heartbeat.CronJob{
		ID:      "reminder-1",
		Name:    "North drill reminder",
		Enabled: true,
		OneShot: true,
		Prompt:  "Check mobile network drill",
		NextRun: "2026-08-13T06:25:00Z",
	}

	content, components := buildCronCard(job, loc)

	if !strings.Contains(content, "Reminder time: 08/13 14:25") || strings.Contains(content, "Schedule: ``") {
		t.Fatalf("one-shot reminder card should render reminder time without empty cron schedule:\n%s", content)
	}
	if len(components) != 1 {
		t.Fatalf("components = %+v, want one actions row", components)
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("component type = %T, want ActionsRow", components[0])
	}
	if len(row.Components) != 2 {
		t.Fatalf("one-shot reminder controls = %+v, want pause/resume plus delete only", row.Components)
	}
	for _, component := range row.Components {
		button, ok := component.(discordgo.Button)
		if !ok {
			t.Fatalf("component type = %T, want Button", component)
		}
		if strings.Contains(button.CustomID, "cron_run_") || strings.Contains(button.CustomID, "cron_edit_") {
			t.Fatalf("one-shot reminder should not expose recurring cron control %q", button.CustomID)
		}
	}
}

func TestCronComponentUpdatesSuppressMentionsAndRedactNames(t *testing.T) {
	L.Load("en")
	t.Setenv("CRON_CARD_SECRET", "cron-secret-value")
	store, err := heartbeat.NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range []*heartbeat.CronJob{
		{ID: "job-pause", Name: "API_TOKEN=cron-secret-value <@123> @everyone", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "0 8 * * *", Prompt: "API_TOKEN=cron-secret-value prompt", Enabled: true},
		{ID: "job-delete", Name: "API_TOKEN=cron-secret-value <@123> @everyone", ChannelID: "channel-1", GuildID: "guild-1", Schedule: "0 8 * * *", Prompt: "API_TOKEN=cron-secret-value prompt", Enabled: true},
	} {
		if err := store.Add(job); err != nil {
			t.Fatalf("Add cron %s: %v", job.ID, err)
		}
	}
	rt := &recordingDiscordTransport{}
	ds := testPeerPermissionSession(t, nil)
	ds.Client = &http.Client{Transport: rt}
	b := &Bot{cronStore: store}

	b.handleCronButton(ds, cronButtonInteraction("cron_pause_job-pause"))
	b.handleCronButton(ds, cronButtonInteraction("cron_delete_job-delete"))

	_, bodies := waitDiscordRequests(t, rt, 2)
	for _, body := range bodies {
		if !strings.Contains(body, `"allowed_mentions":{`) {
			t.Fatalf("cron component update missing allowed_mentions object: %s", body)
		}
		if strings.Contains(body, "cron-secret-value") {
			t.Fatalf("cron component update leaked env secret: %s", body)
		}
	}
}

func TestCronPromptConfirmCustomIDIsOpaque(t *testing.T) {
	result := &ParsedCronJob{Name: "API_TOKEN=cron-secret-value", Schedule: "0 8 * * *", Prompt: "API_TOKEN=cron-secret-value"}
	b := &Bot{}
	customID := b.cronPromptConfirmCustomID(result)
	if !strings.HasPrefix(customID, "cronp_confirm_") {
		t.Fatalf("customID = %q, want cronp_confirm prefix", customID)
	}
	for _, leaked := range []string{"API_TOKEN", "cron-secret-value", result.Schedule} {
		if strings.Contains(customID, leaked) {
			t.Fatalf("cron prompt confirm custom_id leaked %q: %s", leaked, customID)
		}
	}
	cached, ok := b.cronPromptCache.LoadAndDelete(strings.TrimPrefix(customID, "cronp_confirm_"))
	if !ok || cached != result {
		t.Fatalf("cached result = %+v ok=%v, want original parsed result", cached, ok)
	}
}

func cronButtonInteraction(customID string) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:        "interaction-" + customID,
		Token:     "token-" + customID,
		Type:      discordgo.InteractionMessageComponent,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Member:    &discordgo.Member{User: &discordgo.User{ID: "viewer", Username: "Viewer"}},
		Data:      discordgo.MessageComponentInteractionData{CustomID: customID, ComponentType: discordgo.ButtonComponent},
	}}
}

func TestOneShotCronActionUnsupportedBlocksStaleRunEditButtons(t *testing.T) {
	oneShot := &heartbeat.CronJob{OneShot: true}
	for _, action := range []string{"run", "edit"} {
		if !oneShotCronActionUnsupported(oneShot, action) {
			t.Fatalf("one-shot action %q should be blocked", action)
		}
	}
	for _, action := range []string{"pause", "resume", "delete"} {
		if oneShotCronActionUnsupported(oneShot, action) {
			t.Fatalf("one-shot action %q should remain allowed", action)
		}
	}
	if oneShotCronActionUnsupported(&heartbeat.CronJob{}, "run") {
		t.Fatal("recurring cron run should remain allowed")
	}
}
