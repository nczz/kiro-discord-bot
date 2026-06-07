package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/heartbeat"
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
