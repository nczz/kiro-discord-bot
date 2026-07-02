package audit

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	_ "modernc.org/sqlite"
)

func TestStoreRecordsMessageProjection(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Config{
		Enabled:       true,
		DBPath:        filepath.Join(dir, "discord.sqlite"),
		RecordContent: true,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	created := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	msg := &discordgo.Message{
		ID:        "m1",
		GuildID:   "g1",
		ChannelID: "c1",
		Content:   "hello",
		Timestamp: created,
		Author:    &discordgo.User{ID: "u1", Username: "alice"},
		Attachments: []*discordgo.MessageAttachment{{
			ID:          "a1",
			Filename:    "trace.log",
			ContentType: "text/plain",
			Size:        42,
			URL:         "https://cdn.discordapp.com/attachments/a1",
		}},
	}
	payload := &discordgo.MessageCreate{Message: msg}
	evt := EventFromPayload("message_create", payload, func(string) string { return "" })
	if err := store.Record(context.Background(), evt, payload); err != nil {
		t.Fatalf("record: %v", err)
	}

	db := openTestDB(t, filepath.Join(dir, "discord.sqlite"))
	defer db.Close()

	var content, authorID string
	if err := db.QueryRow(`SELECT content, author_id FROM discord_messages WHERE message_id='m1'`).Scan(&content, &authorID); err != nil {
		t.Fatalf("query message: %v", err)
	}
	if content != "hello" || authorID != "u1" {
		t.Fatalf("message projection = content %q author %q", content, authorID)
	}

	var filename string
	if err := db.QueryRow(`SELECT filename FROM discord_attachments WHERE message_id='m1' AND attachment_id='a1'`).Scan(&filename); err != nil {
		t.Fatalf("query attachment: %v", err)
	}
	if filename != "trace.log" {
		t.Fatalf("attachment filename = %q", filename)
	}

	var eventCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM discord_events WHERE event_type='message_create'`).Scan(&eventCount); err != nil {
		t.Fatalf("query events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("event count = %d", eventCount)
	}
}

func TestStoreRedactsProjectedContent(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Config{
		Enabled:       true,
		DBPath:        filepath.Join(dir, "discord.sqlite"),
		RecordContent: false,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	payload := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "m1",
		GuildID:   "g1",
		ChannelID: "c1",
		Content:   "secret",
		Timestamp: time.Now(),
		Author:    &discordgo.User{ID: "u1", Username: "alice"},
	}}
	evt := EventFromPayload("message_create", payload, func(string) string { return "" })
	if err := store.Record(context.Background(), evt, payload); err != nil {
		t.Fatalf("record: %v", err)
	}

	db := openTestDB(t, filepath.Join(dir, "discord.sqlite"))
	defer db.Close()
	var content sql.NullString
	var raw string
	if err := db.QueryRow(`SELECT content, raw_json FROM discord_messages WHERE message_id='m1'`).Scan(&content, &raw); err != nil {
		t.Fatalf("query message: %v", err)
	}
	if content.Valid {
		t.Fatalf("expected redacted projected content, got %q", content.String)
	}
	if raw == "" || strings.Contains(raw, "secret") {
		t.Fatalf("expected redacted raw_json, got %q", raw)
	}
}

func TestStoreUpdatesDeleteAndReactionProjection(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Config{Enabled: true, DBPath: filepath.Join(dir, "discord.sqlite"), RecordContent: true})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	msg := &discordgo.Message{ID: "m1", GuildID: "g1", ChannelID: "c1", Content: "before", Timestamp: time.Now()}
	if err := store.Record(context.Background(), EventFromPayload("message_create", &discordgo.MessageCreate{Message: msg}, func(string) string { return "" }), &discordgo.MessageCreate{Message: msg}); err != nil {
		t.Fatalf("record message: %v", err)
	}
	msg.Content = "after"
	if err := store.Record(context.Background(), EventFromPayload("message_update", &discordgo.MessageUpdate{Message: msg}, func(string) string { return "" }), &discordgo.MessageUpdate{Message: msg}); err != nil {
		t.Fatalf("record update: %v", err)
	}
	reaction := &discordgo.MessageReaction{MessageID: "m1", ChannelID: "c1", GuildID: "g1", UserID: "u2", Emoji: discordgo.Emoji{Name: "✅"}}
	if err := store.Record(context.Background(), EventFromPayload("reaction_add", &discordgo.MessageReactionAdd{MessageReaction: reaction}, func(string) string { return "" }), &discordgo.MessageReactionAdd{MessageReaction: reaction}); err != nil {
		t.Fatalf("record reaction: %v", err)
	}
	if err := store.Record(context.Background(), EventFromPayload("message_delete", &discordgo.MessageDelete{Message: msg}, func(string) string { return "" }), &discordgo.MessageDelete{Message: msg}); err != nil {
		t.Fatalf("record delete: %v", err)
	}

	db := openTestDB(t, filepath.Join(dir, "discord.sqlite"))
	defer db.Close()
	var content string
	var deletedAt sql.NullString
	if err := db.QueryRow(`SELECT content, deleted_at FROM discord_messages WHERE message_id='m1'`).Scan(&content, &deletedAt); err != nil {
		t.Fatalf("query message: %v", err)
	}
	if content != "after" || !deletedAt.Valid {
		t.Fatalf("message projection content=%q deleted_at=%v", content, deletedAt)
	}

	var addedAt sql.NullString
	if err := db.QueryRow(`SELECT added_at FROM discord_reactions WHERE message_id='m1' AND user_id='u2'`).Scan(&addedAt); err != nil {
		t.Fatalf("query reaction: %v", err)
	}
	if !addedAt.Valid {
		t.Fatal("expected reaction added_at")
	}
}

func TestStoreRecordsBotAuditEvent(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Config{Enabled: true, DBPath: filepath.Join(dir, "discord.sqlite"), RecordContent: true})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.RecordBotEvent(context.Background(), BotEvent{
		Type:      "bot_command_invoked",
		GuildID:   "g1",
		ChannelID: "c1",
		TargetID:  "c1",
		UserID:    "u1",
		Username:  "alice",
		Command:   "status",
		Source:    "slash",
		Status:    "started",
		Content:   "secret arg",
		Metadata:  map[string]any{"kind": "test"},
	}); err != nil {
		t.Fatalf("record bot event: %v", err)
	}

	db := openTestDB(t, filepath.Join(dir, "discord.sqlite"))
	defer db.Close()
	var command, content, metadata string
	if err := db.QueryRow(`SELECT command, content, metadata_json FROM bot_audit_events WHERE event_type='bot_command_invoked'`).Scan(&command, &content, &metadata); err != nil {
		t.Fatalf("query bot event: %v", err)
	}
	if command != "status" || content != "secret arg" || !strings.Contains(metadata, "test") {
		t.Fatalf("bot event command=%q content=%q metadata=%q", command, content, metadata)
	}
}

func TestStoreRedactsBotAuditEventContent(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Config{Enabled: true, DBPath: filepath.Join(dir, "discord.sqlite"), RecordContent: false})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.RecordBotEvent(context.Background(), BotEvent{
		Type:    "agent_response_sent",
		Content: "secret output",
	}); err != nil {
		t.Fatalf("record bot event: %v", err)
	}

	db := openTestDB(t, filepath.Join(dir, "discord.sqlite"))
	defer db.Close()
	var content sql.NullString
	var raw string
	if err := db.QueryRow(`SELECT content, raw_json FROM bot_audit_events WHERE event_type='agent_response_sent'`).Scan(&content, &raw); err != nil {
		t.Fatalf("query bot event: %v", err)
	}
	if content.Valid {
		t.Fatalf("expected redacted content, got %q", content.String)
	}
	if strings.Contains(raw, "secret output") {
		t.Fatalf("expected redacted raw json, got %q", raw)
	}
}

func TestStoreRecentTimelineMergesDiscordAndBotEvents(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(Config{Enabled: true, DBPath: filepath.Join(dir, "discord.sqlite"), RecordContent: true})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	msg := &discordgo.Message{
		ID:        "m1",
		GuildID:   "g1",
		ChannelID: "c1",
		Content:   "hello",
		Timestamp: time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC),
		Author:    &discordgo.User{ID: "u1", Username: "alice"},
	}
	payload := &discordgo.MessageCreate{Message: msg}
	if err := store.Record(context.Background(), EventFromPayload("message_create", payload, func(string) string { return "" }), payload); err != nil {
		t.Fatalf("record message: %v", err)
	}
	if err := store.RecordBotEvent(context.Background(), BotEvent{
		Type:      "bot_command_invoked",
		GuildID:   "g1",
		ChannelID: "c1",
		TargetID:  "c1",
		UserID:    "u1",
		Command:   "audit",
		Status:    "started",
		Content:   "20",
	}); err != nil {
		t.Fatalf("record bot event: %v", err)
	}

	events, err := store.RecentTimeline(context.Background(), "c1", 10)
	if err != nil {
		t.Fatalf("recent timeline: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("timeline event count = %d, want 2", len(events))
	}

	var sawDiscord, sawBot bool
	for _, evt := range events {
		switch evt.Kind {
		case "discord":
			sawDiscord = evt.Type == "message_create" && evt.MessageID == "m1" && evt.Content == "hello"
		case "bot":
			sawBot = evt.Type == "bot_command_invoked" && evt.Command == "audit" && evt.Status == "started"
		}
	}
	if !sawDiscord || !sawBot {
		t.Fatalf("timeline did not include expected discord=%v bot=%v events: %+v", sawDiscord, sawBot, events)
	}
}

func TestQueryTimelineReadOnlyScopesTargetAndGuild(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "discord.sqlite")
	store, err := Open(Config{Enabled: true, DBPath: dbPath, RecordContent: true})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	for _, msg := range []*discordgo.Message{
		{ID: "m1", GuildID: "g1", ChannelID: "c1", Content: "visible created_at message_create", Timestamp: time.Now(), Author: &discordgo.User{ID: "u1", Username: "alice"}},
		{ID: "m2", GuildID: "g1", ChannelID: "c2", Content: "other channel", Timestamp: time.Now(), Author: &discordgo.User{ID: "u2", Username: "bob"}},
		{ID: "m3", GuildID: "g2", ChannelID: "c1", Content: "other guild", Timestamp: time.Now(), Author: &discordgo.User{ID: "u3", Username: "carol"}},
	} {
		payload := &discordgo.MessageCreate{Message: msg}
		if err := store.Record(context.Background(), EventFromPayload("message_create", payload, func(string) string { return "" }), payload); err != nil {
			t.Fatalf("record message %s: %v", msg.ID, err)
		}
	}

	events, err := QueryTimelineReadOnly(dbPath, TimelineQueryOptions{
		GuildID:        "g1",
		TargetID:       "c1",
		Contains:       "message_create",
		EventType:      "message_create",
		IncludeContent: true,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("query timeline: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1: %+v", len(events), events)
	}
	if events[0].MessageID != "m1" || events[0].Content != "visible created_at message_create" {
		t.Fatalf("scoped event = %+v", events[0])
	}
}

func TestQueryTimelineReadOnlyOmitsContentByDefault(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "discord.sqlite")
	store, err := Open(Config{Enabled: true, DBPath: dbPath, RecordContent: true})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	msg := &discordgo.Message{ID: "m1", GuildID: "g1", ChannelID: "c1", Content: "secret", Timestamp: time.Now(), Author: &discordgo.User{ID: "u1", Username: "alice"}}
	payload := &discordgo.MessageCreate{Message: msg}
	if err := store.Record(context.Background(), EventFromPayload("message_create", payload, func(string) string { return "" }), payload); err != nil {
		t.Fatalf("record message: %v", err)
	}

	events, err := QueryTimelineReadOnly(dbPath, TimelineQueryOptions{GuildID: "g1", TargetID: "c1", Limit: 10})
	if err != nil {
		t.Fatalf("query timeline: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Content != "" {
		t.Fatalf("content = %q, want omitted", events[0].Content)
	}

	events, err = QueryTimelineReadOnly(dbPath, TimelineQueryOptions{GuildID: "g1", TargetID: "c1", Contains: "secret", Limit: 10})
	if err != nil {
		t.Fatalf("query hidden content: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("hidden content search returned events: %+v", events)
	}
}

func TestQueryTimelineReadOnlyDecoratesMessageDeleteFromProjection(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "discord.sqlite")
	store, err := Open(Config{Enabled: true, DBPath: dbPath, RecordContent: true})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	created := &discordgo.Message{
		ID:        "m-delete",
		GuildID:   "g1",
		ChannelID: "thread-1",
		Content:   "private detail that should be summarized for authorized audit only",
		Timestamp: time.Now(),
		Author:    &discordgo.User{ID: "author-1", Username: "alice"},
	}
	createPayload := &discordgo.MessageCreate{Message: created}
	if err := store.Record(context.Background(), EventFromPayload("message_create", createPayload, func(string) string { return "channel-1" }), createPayload); err != nil {
		t.Fatalf("record message: %v", err)
	}

	deletePayload := &discordgo.MessageDelete{Message: &discordgo.Message{
		ID:        "m-delete",
		GuildID:   "g1",
		ChannelID: "thread-1",
	}}
	if err := store.Record(context.Background(), EventFromPayload("message_delete", deletePayload, func(string) string { return "channel-1" }), deletePayload); err != nil {
		t.Fatalf("record delete: %v", err)
	}

	events, err := QueryTimelineReadOnly(dbPath, TimelineQueryOptions{
		GuildID:   "g1",
		TargetID:  "thread-1",
		EventType: "message_delete",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("query delete timeline: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1: %+v", len(events), events)
	}
	evt := events[0]
	if evt.OriginalAuthorID != "author-1" || evt.OriginalAuthorUsername != "alice" {
		t.Fatalf("original author = %q/%q, want author-1/alice: %+v", evt.OriginalAuthorID, evt.OriginalAuthorUsername, evt)
	}
	if evt.UserID != "" {
		t.Fatalf("message_delete user_id = %q, want empty because gateway event does not expose deleter", evt.UserID)
	}
	if evt.ContentSnippet != "" {
		t.Fatalf("content snippet should be omitted by default, got %q", evt.ContentSnippet)
	}
	if evt.DeletionActorID != "" || !strings.Contains(evt.DeletionNote, "does not identify who deleted") {
		t.Fatalf("deletion attribution fields = actor %q note %q", evt.DeletionActorID, evt.DeletionNote)
	}
	events, err = QueryTimelineReadOnly(dbPath, TimelineQueryOptions{
		GuildID:   "g1",
		TargetID:  "thread-1",
		EventType: "message_delete",
		Contains:  "alice",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("query delete timeline by original author: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("delete search by original author returned %d events: %+v", len(events), events)
	}

	events, err = QueryTimelineReadOnly(dbPath, TimelineQueryOptions{
		GuildID:        "g1",
		TargetID:       "thread-1",
		EventType:      "message_delete",
		IncludeContent: true,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("query delete timeline with content: %v", err)
	}
	if len(events) != 1 || !strings.Contains(events[0].ContentSnippet, "private detail") {
		t.Fatalf("content snippet not returned for authorized content query: %+v", events)
	}
}

func TestQueryTimelineReadOnlyDecoratesMessageDeleteBulk(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "discord.sqlite")
	store, err := Open(Config{Enabled: true, DBPath: dbPath, RecordContent: true})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	payload := &discordgo.MessageDeleteBulk{
		GuildID:   "g1",
		ChannelID: "thread-1",
		Messages:  []string{"m1", "m2"},
	}
	if err := store.Record(context.Background(), EventFromPayload("message_delete_bulk", payload, func(string) string { return "channel-1" }), payload); err != nil {
		t.Fatalf("record bulk delete: %v", err)
	}

	events, err := QueryTimelineReadOnly(dbPath, TimelineQueryOptions{
		GuildID:   "g1",
		TargetID:  "thread-1",
		EventType: "message_delete_bulk",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("query bulk delete timeline: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1: %+v", len(events), events)
	}
	if events[0].DeletedMessageCount != 2 || strings.Join(events[0].DeletedMessageIDs, ",") != "m1,m2" {
		t.Fatalf("bulk deleted messages = count %d ids %+v", events[0].DeletedMessageCount, events[0].DeletedMessageIDs)
	}
	if events[0].DeletionActorID != "" || !strings.Contains(events[0].DeletionNote, "not who deleted") {
		t.Fatalf("bulk deletion attribution = actor %q note %q", events[0].DeletionActorID, events[0].DeletionNote)
	}
}

func openTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}
