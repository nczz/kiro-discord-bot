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
