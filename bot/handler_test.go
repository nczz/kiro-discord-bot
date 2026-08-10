package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/botegress"
	"github.com/nczz/kiro-discord-bot/internal/botmcp"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
	L "github.com/nczz/kiro-discord-bot/locale"
)

type failingDiscordTransport struct{}

func (f failingDiscordTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"message":"forced discord failure","code":0}`)),
		Request:    req,
	}, nil
}

type countingDiscordTransport struct {
	mu    sync.Mutex
	count int
}

func (c *countingDiscordTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	if req.Method == http.MethodPost && strings.Contains(req.URL.Path, "/channels/channel-1/messages") {
		c.count++
	}
	c.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{
			"id":"response-1",
			"channel_id":"channel-1",
			"content":"ok",
			"author":{"id":"bot-1","username":"bot","discriminator":"0000","bot":true}
		}`)),
		Request: req,
	}, nil
}

func (c *countingDiscordTransport) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

type recordingDiscordTransport struct {
	mu      sync.Mutex
	paths   []string
	bodies  []string
	counter int
}

func (r *recordingDiscordTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
	}
	r.mu.Lock()
	r.counter++
	id := fmt.Sprintf("response-%d", r.counter)
	r.paths = append(r.paths, req.Method+" "+req.URL.Path)
	r.bodies = append(r.bodies, body)
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
			"id":%q,
			"channel_id":"channel-1",
			"content":"ok",
			"author":{"id":"bot-1","username":"bot","discriminator":"0000","bot":true}
		}`, id))),
		Request: req,
	}, nil
}

func (r *recordingDiscordTransport) Snapshot() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...), append([]string(nil), r.bodies...)
}

func newFailingDiscordSession(t *testing.T) *discordgo.Session {
	t.Helper()
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: failingDiscordTransport{}}
	ds.State = testPeerPermissionSession(t, nil).State
	return ds
}

func TestSendDiscordTextWithAllowedMentionsAllowsExplicitUserPing(t *testing.T) {
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}

	if _, err := sendDiscordTextWithAllowedMentions(ds, "channel-1", "<@user-1> drink water", nil, []string{" user-1 "}); err != nil {
		t.Fatalf("send discord text: %v", err)
	}

	paths, bodies := rt.Snapshot()
	if len(paths) != 1 || !strings.Contains(paths[0], "/channels/channel-1/messages") {
		t.Fatalf("unexpected discord calls: paths=%v bodies=%v", paths, bodies)
	}
	var payload struct {
		Content         string `json:"content"`
		AllowedMentions struct {
			Users []string `json:"users"`
		} `json:"allowed_mentions"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &payload); err != nil {
		t.Fatalf("discord payload json: %v\n%s", err, bodies[0])
	}
	if payload.Content != "<@user-1> drink water" {
		t.Fatalf("content = %q", payload.Content)
	}
	if len(payload.AllowedMentions.Users) != 1 || payload.AllowedMentions.Users[0] != "user-1" {
		t.Fatalf("allowed mention users = %+v, want [user-1]", payload.AllowedMentions.Users)
	}
}

func TestSafeEgressAppliesMemoryActionsWithoutDiscordFailureMessage(t *testing.T) {
	dir := t.TempDir()
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dir})
	b := &Bot{dataDir: dir, manager: manager}
	task := newSafeEgressTask(b)

	if err := task.process(botegress.Action{
		Action:      botegress.ActionMemoryAdd,
		ChannelID:   "channel-1",
		MemoryEntry: "Always reply in Traditional Chinese.",
	}); err != nil {
		t.Fatalf("process memory add: %v", err)
	}
	entries := manager.MemoryList("channel-1")
	if len(entries) != 1 || entries[0] != "Always reply in Traditional Chinese." {
		t.Fatalf("memory after add = %+v", entries)
	}
	if err := task.process(botegress.Action{
		Action:      botegress.ActionMemoryRemove,
		ChannelID:   "channel-1",
		MemoryIndex: 1,
	}); err != nil {
		t.Fatalf("process memory remove: %v", err)
	}
	if entries := manager.MemoryList("channel-1"); len(entries) != 0 {
		t.Fatalf("memory after remove = %+v", entries)
	}
	if err := task.process(botegress.Action{
		Action:      botegress.ActionMemoryAdd,
		ChannelID:   "channel-1",
		MemoryEntry: "Use concise answers.",
	}); err != nil {
		t.Fatalf("process second memory add: %v", err)
	}
	if err := task.process(botegress.Action{
		Action:    botegress.ActionMemoryClear,
		ChannelID: "channel-1",
	}); err != nil {
		t.Fatalf("process memory clear: %v", err)
	}
	if entries := manager.MemoryList("channel-1"); len(entries) != 0 {
		t.Fatalf("memory after clear = %+v", entries)
	}
}

func TestSafeEgressDrainChannelDoesNotCountMemoryAsDiscordDelivery(t *testing.T) {
	dir := t.TempDir()
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dir})
	b := &Bot{dataDir: dir, manager: manager}
	task := newSafeEgressTask(b)

	if _, err := botegress.WritePending(dir, botegress.Action{
		Action:      botegress.ActionMemoryAdd,
		ChannelID:   "channel-1",
		MemoryEntry: "Always reply in Traditional Chinese.",
		RequestedBy: "alice user_id=user-1",
		Reason:      "user explicitly asked for a memory",
	}); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if delivered := task.DrainChannel("channel-1"); delivered != 0 {
		t.Fatalf("DrainChannel delivered = %d, want 0 for memory-only action", delivered)
	}
	if entries := manager.MemoryList("channel-1"); len(entries) != 1 || entries[0] != "Always reply in Traditional Chinese." {
		t.Fatalf("memory after drain = %+v", entries)
	}
}

func TestSafeEgressMemoryApplicationAuditIncludesGuild(t *testing.T) {
	dir := t.TempDir()
	store, err := audit.Open(audit.Config{DataDir: dir, RecordContent: true})
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder := audit.NewRecorder(store, 10, nil, false)
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dir})
	b := &Bot{
		dataDir:       dir,
		guildID:       "guild-1",
		manager:       manager,
		auditRecorder: recorder,
	}
	task := newSafeEgressTask(b)

	if err := task.process(botegress.Action{
		Action:      botegress.ActionMemoryAdd,
		ChannelID:   "channel-1",
		MemoryEntry: "Always reply in Traditional Chinese.",
		RequestedBy: "alice user_id=user-1",
		Reason:      "user explicitly asked for a memory",
	}); err != nil {
		t.Fatalf("process memory add: %v", err)
	}
	recorder.Close()

	events, err := audit.QueryTimelineReadOnly(filepath.Join(dir, "audit", "discord.sqlite"), audit.TimelineQueryOptions{
		GuildID:   "guild-1",
		TargetID:  "channel-1",
		EventType: "bot_memory_updated",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("query memory audit: %v", err)
	}
	if len(events) != 1 || events[0].Status != "applied" {
		t.Fatalf("memory audit events = %+v, want one applied event visible under guild filter", events)
	}
}

func TestSafeEgressDrainChannelFlushesOnlyMatchingPendingActions(t *testing.T) {
	dir := t.TempDir()
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}

	ds.Client = &http.Client{Transport: rt}
	b := &Bot{discord: ds, dataDir: dir}
	task := newSafeEgressTask(b)

	if _, err := botegress.WritePending(dir, botegress.Action{
		ID:        "a-thread",
		Action:    botegress.ActionSendMessage,
		ChannelID: "thread-1",
		Content:   "thread payload",
		CreatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("write thread pending: %v", err)
	}
	if _, err := botegress.WritePending(dir, botegress.Action{
		ID:        "b-channel",
		Action:    botegress.ActionSendMessage,
		ChannelID: "channel-1",
		Content:   "channel payload",
		CreatedAt: "2026-01-01T00:00:01Z",
	}); err != nil {
		t.Fatalf("write channel pending: %v", err)
	}

	if delivered := task.DrainChannel("thread-1"); delivered != 1 {
		t.Fatalf("DrainChannel delivered = %d, want 1", delivered)
	}

	paths, bodies := rt.Snapshot()
	if len(paths) != 1 || !strings.Contains(paths[0], "/channels/thread-1/messages") || !strings.Contains(bodies[0], "thread payload") {
		t.Fatalf("unexpected drained Discord calls: paths=%v bodies=%v", paths, bodies)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if len(actions) != 1 || actions[0].ID != "b-channel" || actions[0].ChannelID != "channel-1" {
		t.Fatalf("unexpected remaining pending actions: %+v", actions)
	}
}

func TestSafeEgressSendMessageSplitsLongContent(t *testing.T) {
	dir := t.TempDir()
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	b := &Bot{discord: ds, dataDir: dir}
	task := newSafeEgressTask(b)

	longContent := strings.Repeat("alpha beta gamma\n", 180)
	if _, err := botegress.WritePending(dir, botegress.Action{
		ID:        "long-message",
		Action:    botegress.ActionSendMessage,
		ChannelID: "channel-1",
		Content:   longContent,
		CreatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	if delivered := task.DrainChannel("channel-1"); delivered != 1 {
		t.Fatalf("DrainChannel delivered = %d, want 1", delivered)
	}

	paths, bodies := rt.Snapshot()
	if len(paths) < 2 {
		t.Fatalf("safe egress should split long Discord messages, got paths=%v bodies=%v", paths, bodies)
	}
	for i, body := range bodies {
		var payload struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("discord payload %d json: %v\n%s", i, err, body)
		}
		if utf8.RuneCountInString(payload.Content) > 2000 {
			t.Fatalf("discord payload %d content len = %d, want <= 2000", i, utf8.RuneCountInString(payload.Content))
		}
		if strings.Contains(payload.Content, "Safe egress blocked") {
			t.Fatalf("safe egress reported failure instead of splitting: %q", payload.Content)
		}
	}
}

func TestSafeEgressSendFileSplitsLongContentBeforeUpload(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(filePath, []byte("report body"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	b := &Bot{discord: ds, dataDir: dir}
	task := newSafeEgressTask(b)

	if _, err := botegress.WritePending(dir, botegress.Action{
		ID:        "long-file-message",
		Action:    botegress.ActionSendFile,
		ChannelID: "channel-1",
		FilePath:  filePath,
		Content:   strings.Repeat("file intro\n", 260),
		CreatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	if delivered := task.DrainChannel("channel-1"); delivered != 1 {
		t.Fatalf("DrainChannel delivered = %d, want 1", delivered)
	}

	_, bodies := rt.Snapshot()
	if len(bodies) < 3 {
		t.Fatalf("safe file egress should send split text plus file upload, got %d bodies: %v", len(bodies), bodies)
	}
	for i, body := range bodies[:len(bodies)-1] {
		var payload struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("text payload %d json: %v\n%s", i, err, body)
		}
		if utf8.RuneCountInString(payload.Content) > 2000 {
			t.Fatalf("text payload %d content len = %d, want <= 2000", i, utf8.RuneCountInString(payload.Content))
		}
	}
	if !strings.Contains(bodies[len(bodies)-1], `name="files[0]"`) {
		t.Fatalf("last safe egress call should upload the file, got: %s", bodies[len(bodies)-1])
	}
}

func TestSafeEgressRemovesTransientSourceAfterSend(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "egress", "incoming", "source")
	if err := os.MkdirAll(sourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(sourceDir, "report.txt")
	if err := os.WriteFile(filePath, []byte("report body"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	b := &Bot{discord: ds, dataDir: dir}
	task := newSafeEgressTask(b)

	if _, err := botegress.WritePending(dir, botegress.Action{
		ID:                  "transient-file",
		Action:              botegress.ActionSendFile,
		ChannelID:           "channel-1",
		FilePath:            filePath,
		RemoveFileAfterSend: true,
		CreatedAt:           "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	if delivered := task.DrainChannel("channel-1"); delivered != 1 {
		t.Fatalf("DrainChannel delivered = %d, want 1", delivered)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("transient source should be removed after send, err=%v", err)
	}
	if _, err := os.Stat(sourceDir); !os.IsNotExist(err) {
		t.Fatalf("empty transient source dir should be removed after send, err=%v", err)
	}
}

func TestSafeEgressDrainChannelCountsOnlySuccessfulDeliveries(t *testing.T) {
	dir := t.TempDir()
	ds := newFailingDiscordSession(t)
	b := &Bot{discord: ds, dataDir: dir}
	task := newSafeEgressTask(b)

	if _, err := botegress.WritePending(dir, botegress.Action{
		ID:        "failing-message",
		Action:    botegress.ActionSendMessage,
		ChannelID: "thread-1",
		Content:   "thread payload",
		CreatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	if delivered := task.DrainChannel("thread-1"); delivered != 0 {
		t.Fatalf("DrainChannel delivered = %d, want 0 for failed Discord delivery", delivered)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("failed pending action should still be removed after safe failure handling: %+v", actions)
	}
}

func TestSafeEgressFailureRedactsSensitiveFilePath(t *testing.T) {
	L.Load("en")
	dir := t.TempDir()
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	b := &Bot{discord: ds, dataDir: dir}
	task := newSafeEgressTask(b)
	sensitivePath := filepath.Join(dir, ".kiro", "settings", "mcp.json")

	if _, err := botegress.WritePending(dir, botegress.Action{
		ID:        "sensitive-file",
		Action:    botegress.ActionSendFile,
		ChannelID: "thread-1",
		FilePath:  sensitivePath,
		Content:   "please send",
		CreatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("write sensitive file pending: %v", err)
	}

	task.DrainChannel("thread-1")

	_, bodies := rt.Snapshot()
	if len(bodies) != 1 {
		t.Fatalf("discord calls = %d, want 1; bodies=%v", len(bodies), bodies)
	}
	if strings.Contains(bodies[0], ".kiro") || strings.Contains(bodies[0], "mcp.json") || strings.Contains(bodies[0], sensitivePath) {
		t.Fatalf("safe failure leaked sensitive path: %q", bodies[0])
	}
	if !strings.Contains(bodies[0], "[REDACTED:PATH]") {
		t.Fatalf("safe failure missing redacted path marker: %q", bodies[0])
	}
}

func TestEgressReasonMessageLocalizesKnownSafeFailures(t *testing.T) {
	L.Load("en")
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "not text",
			raw:  "file type is not safely redactable as text",
			want: "file type is not safely redactable as text",
		},
		{
			name: "too large",
			raw:  "file exceeds sanitizable size limit (5242880 bytes)",
			want: "file exceeds sanitizable size limit",
		},
		{
			name: "directory",
			raw:  "directories cannot be sent as files",
			want: "directories cannot be sent as files",
		},
		{
			name: "path required",
			raw:  "file_path is required",
			want: "file_path is required",
		},
		{
			name: "extract failed",
			raw:  "extract readable text (.docx): extract readable text: no readable text found",
			want: "failed to extract readable text from this file",
		},
		{
			name: "unsupported extractable format",
			raw:  "unsupported extractable format",
			want: "file format is not supported for safe text extraction",
		},
		{
			name: "image too large",
			raw:  "image exceeds upload size limit (26214400 bytes)",
			want: "image exceeds upload size or dimension limits",
		},
		{
			name: "invalid image",
			raw:  "invalid image file: invalid JPEG format",
			want: "image format validation failed",
		},
		{
			name: "unknown fallback",
			raw:  "open sanitized file: permission denied",
			want: "open sanitized file: permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := egressReasonMessage(tt.raw); got != tt.want {
				t.Fatalf("egressReasonMessage(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestStatusWithRuntimeIncludesBotUptime(t *testing.T) {
	L.Load("en")
	b := &Bot{startedAt: time.Now().Add(-90 * time.Second)}

	got := b.statusWithRuntime("Agent: `test`")
	if !strings.Contains(got, "Agent: `test`") {
		t.Fatalf("statusWithRuntime should preserve base status, got:\n%s", got)
	}
	if !strings.Contains(got, "Bot uptime: `1m") {
		t.Fatalf("statusWithRuntime should include bot uptime, got:\n%s", got)
	}
}

func newAuditTestBot(t *testing.T) (*Bot, string, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := audit.Open(audit.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder := audit.NewRecorder(store, 100, nil, false)
	sessionStore, err := channel.NewSessionStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("open session store: %v", err)
	}
	b := &Bot{
		manager:       channel.NewManager(channel.ManagerConfig{DataDir: filepath.Join(dir, "data"), Store: sessionStore}),
		auditRecorder: recorder,
		seen:          newSeenMessages(),
	}
	cleanup := func() {
		b.seen.Stop()
		recorder.Close()
	}
	return b, filepath.Join(dir, "audit", "discord.sqlite"), cleanup
}

func waitBotAuditEvent(t *testing.T, dbPath, eventType string) audit.BotEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		var raw string
		err = db.QueryRowContext(context.Background(), `SELECT raw_json FROM bot_audit_events WHERE event_type=? ORDER BY id DESC LIMIT 1`, eventType).Scan(&raw)
		_ = db.Close()
		if err == nil {
			var evt audit.BotEvent
			if err := json.Unmarshal([]byte(raw), &evt); err != nil {
				t.Fatalf("unmarshal raw event: %v", err)
			}
			return evt
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: %v", eventType, lastErr)
	return audit.BotEvent{}
}

func waitBotAuditEvents(t *testing.T, dbPath, eventType string, minCount int) []audit.BotEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		rows, err := db.QueryContext(context.Background(), `SELECT raw_json FROM bot_audit_events WHERE event_type=? ORDER BY id`, eventType)
		if err != nil {
			_ = db.Close()
			t.Fatalf("query audit events: %v", err)
		}
		var events []audit.BotEvent
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				_ = rows.Close()
				_ = db.Close()
				t.Fatalf("scan audit event: %v", err)
			}
			var evt audit.BotEvent
			if err := json.Unmarshal([]byte(raw), &evt); err != nil {
				_ = rows.Close()
				_ = db.Close()
				t.Fatalf("unmarshal raw event: %v", err)
			}
			events = append(events, evt)
		}
		lastErr = rows.Err()
		_ = rows.Close()
		_ = db.Close()
		if lastErr != nil {
			t.Fatalf("rows: %v", lastErr)
		}
		if len(events) >= minCount {
			return events
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d %s events: %v", minCount, eventType, lastErr)
	return nil
}

func testPeerPermissionSession(t *testing.T, channelOneOverwrites []*discordgo.PermissionOverwrite) *discordgo.Session {
	t.Helper()
	ds := &discordgo.Session{State: discordgo.NewState(), Ratelimiter: discordgo.NewRatelimiter()}
	ds.State.User = &discordgo.User{ID: "bot-1", Bot: true}
	basePerms := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionSendMessagesInThreads)
	guild := &discordgo.Guild{
		ID: "guild-1",
		Roles: []*discordgo.Role{
			{ID: "guild-1", Name: "@everyone", Permissions: basePerms},
		},
	}
	if err := ds.State.GuildAdd(guild); err != nil {
		t.Fatalf("GuildAdd: %v", err)
	}
	for _, member := range []*discordgo.Member{
		{GuildID: "guild-1", User: &discordgo.User{ID: "bot-1", Bot: true}},
		{GuildID: "guild-1", User: &discordgo.User{ID: "bot-2", Bot: true}},
		{GuildID: "guild-1", User: &discordgo.User{ID: "viewer"}},
	} {
		if err := ds.State.MemberAdd(member); err != nil {
			t.Fatalf("MemberAdd: %v", err)
		}
	}
	for _, ch := range []*discordgo.Channel{
		{ID: "channel-1", GuildID: "guild-1", Type: discordgo.ChannelTypeGuildText, PermissionOverwrites: channelOneOverwrites},
		{ID: "channel-2", GuildID: "guild-1", Type: discordgo.ChannelTypeGuildText},
		{ID: "thread-1", GuildID: "guild-1", ParentID: "channel-1", Type: discordgo.ChannelTypeGuildPublicThread},
	} {
		if err := ds.State.ChannelAdd(ch); err != nil {
			t.Fatalf("ChannelAdd: %v", err)
		}
	}
	return ds
}

func botMemberAllowOverwrite(botID string) *discordgo.PermissionOverwrite {
	return &discordgo.PermissionOverwrite{
		ID:    botID,
		Type:  discordgo.PermissionOverwriteTypeMember,
		Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionSendMessagesInThreads,
	}
}

func botMemberViewOverwrite(botID string) *discordgo.PermissionOverwrite {
	return &discordgo.PermissionOverwrite{
		ID:    botID,
		Type:  discordgo.PermissionOverwriteTypeMember,
		Allow: discordgo.PermissionViewChannel,
	}
}

func botMemberDenyOverwrite(botID string) *discordgo.PermissionOverwrite {
	return &discordgo.PermissionOverwrite{
		ID:   botID,
		Type: discordgo.PermissionOverwriteTypeMember,
		Deny: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionSendMessagesInThreads,
	}
}

func botMemberThreadReplyOverwrite(botID string) *discordgo.PermissionOverwrite {
	return &discordgo.PermissionOverwrite{
		ID:    botID,
		Type:  discordgo.PermissionOverwriteTypeMember,
		Allow: discordgo.PermissionViewChannel | discordgo.PermissionCreatePublicThreads | discordgo.PermissionSendMessagesInThreads,
		Deny:  discordgo.PermissionSendMessages,
	}
}

func botRoleAllowOverwrite(roleID string) *discordgo.PermissionOverwrite {
	return &discordgo.PermissionOverwrite{
		ID:    roleID,
		Type:  discordgo.PermissionOverwriteTypeRole,
		Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionSendMessagesInThreads,
	}
}

func userMemberManageOverwrite(userID string, perms int64) *discordgo.PermissionOverwrite {
	return &discordgo.PermissionOverwrite{
		ID:    userID,
		Type:  discordgo.PermissionOverwriteTypeMember,
		Allow: discordgo.PermissionViewChannel | perms,
	}
}

func TestShouldIgnoreMessage(t *testing.T) {
	tests := []struct {
		name   string
		msg    *discordgo.MessageCreate
		selfID string
		want   bool
	}{
		{name: "nil message", msg: nil, selfID: "self", want: true},
		{name: "nil author", msg: &discordgo.MessageCreate{}, selfID: "self", want: true},
		{
			name:   "self",
			msg:    &discordgo.MessageCreate{Message: &discordgo.Message{Author: &discordgo.User{ID: "self"}}},
			selfID: "self",
			want:   true,
		},
		{
			name:   "other bot can be considered by bot-result gate",
			msg:    &discordgo.MessageCreate{Message: &discordgo.Message{Author: &discordgo.User{ID: "bot-2", Bot: true}}},
			selfID: "self",
			want:   false,
		},
		{
			name:   "human",
			msg:    &discordgo.MessageCreate{Message: &discordgo.Message{Author: &discordgo.User{ID: "human"}}},
			selfID: "self",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldIgnoreMessage(tt.msg, tt.selfID); got != tt.want {
				t.Fatalf("shouldIgnoreMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelfMentionHelpers(t *testing.T) {
	if !isSelfMentioned("<@self> review this", "self") {
		t.Fatal("expected standard mention to match")
	}
	if !isSelfMentioned("<@!self> review this", "self") {
		t.Fatal("expected nickname mention to match")
	}
	if got := stripSelfMentions("<@self> <@!self> review this", "self"); got != "review this" {
		t.Fatalf("stripSelfMentions() = %q, want %q", got, "review this")
	}

	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		Content:  "@AlphaBot review this",
		Mentions: []*discordgo.User{{ID: "self"}},
	}}
	if !messageMentionsUser(msg, msg.Content, "self") {
		t.Fatal("expected structured Discord mention to match even without token text")
	}
}

func TestMentionsOtherPeer(t *testing.T) {
	b := &Bot{peers: parseBotPeers("AlphaBot:bot-1:role-1,BetaBot:bot-2:role-2")}

	if !b.mentionsOtherPeer("<@bot-2> review this", "bot-1") {
		t.Fatal("expected mention of another configured peer to match")
	}
	if !b.mentionsOtherPeer("<@!bot-2> review this", "bot-1") {
		t.Fatal("expected nickname mention of another configured peer to match")
	}
	if b.mentionsOtherPeer("<@bot-1> handle this", "bot-1") {
		t.Fatal("did not expect self mention to count as other peer")
	}
	if b.mentionsOtherPeer("<@unknown> handle this", "bot-1") {
		t.Fatal("did not expect unknown mention to count as other peer")
	}

	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		Content:  "@BetaBot handle this",
		Mentions: []*discordgo.User{{ID: "bot-2"}},
	}}
	if !b.messageMentionsOtherPeer(msg, msg.Content, "bot-1") {
		t.Fatal("expected structured peer mention to match")
	}
	if b.messageMentionsOtherPeer(msg, msg.Content, "bot-2") {
		t.Fatal("did not expect self structured mention to count as other peer")
	}
	if !b.messageMentionsOtherPeer(nil, "<@&role-2> handle this", "bot-1") {
		t.Fatal("expected peer role mention to match")
	}
	if !b.messageMentionsSelf(nil, "<@&role-1> handle this", "bot-1") {
		t.Fatal("expected self role mention to match")
	}
	if got := b.stripOwnMentions("<@&role-1> handle this", "bot-1"); got != "handle this" {
		t.Fatalf("stripOwnMentions() = %q, want handle this", got)
	}
}

func TestStripLeadingPeerMentions(t *testing.T) {
	b := &Bot{peers: parseBotPeers("AlphaBot:bot-1:role-1,BetaBot:bot-2:role-2,ReviewBot:bot-3")}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "multiple leading user mentions",
			content: "<@bot-2> <@!bot-3> hi",
			want:    "hi",
		},
		{
			name:    "leading role mention",
			content: "<@&role-2> hi",
			want:    "hi",
		},
		{
			name:    "preserves non-leading handoff mention",
			content: "please ask <@bot-2> to review",
			want:    "please ask <@bot-2> to review",
		},
		{
			name:    "unknown leading mention remains",
			content: "<@unknown> hi",
			want:    "<@unknown> hi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := b.stripLeadingPeerMentions(tt.content); got != tt.want {
				t.Fatalf("stripLeadingPeerMentions() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHumanMultiBotLeadingMentionsBecomeTaskText(t *testing.T) {
	b := &Bot{peers: parseBotPeers("AlphaBot:bot-1:role-1,BetaBot:bot-2:role-2")}
	content := b.stripOwnMentions("<@bot-1> <@bot-2> hi", "bot-1")
	content = b.stripLeadingPeerMentions(content)
	if content != "hi" {
		t.Fatalf("normalized content = %q, want hi", content)
	}
}

func TestHumanMessageAddressesSelfOnlyWhenSelfIsAddressed(t *testing.T) {
	b := &Bot{peers: parseBotPeers("AlphaBot:bot-1:role-1,BetaBot:bot-2:role-2,KiroAgent:bot-3:role-3")}

	tests := []struct {
		name    string
		content string
		selfID  string
		want    bool
	}{
		{
			name:    "self in leading mention block",
			content: "<@bot-1> <@bot-2> hi",
			selfID:  "bot-1",
			want:    true,
		},
		{
			name:    "second leading mention is also addressed",
			content: "<@bot-1> <@bot-2> hi",
			selfID:  "bot-2",
			want:    true,
		},
		{
			name:    "self mention in task body is target not addressee",
			content: "<@bot-2> 幫我出一個數學題給 <@bot-1> 解",
			selfID:  "bot-1",
			want:    false,
		},
		{
			name:    "addressed peer should process target mention",
			content: "<@bot-2> 幫我出一個數學題給 <@bot-1> 解",
			selfID:  "bot-2",
			want:    true,
		},
		{
			name:    "role target in task body is not addressee",
			content: "<@&role-2> 幫我出一個數學題給 <@&role-1> 解",
			selfID:  "bot-1",
			want:    false,
		},
		{
			name:    "no leading peer remains compatible",
			content: "請問 <@bot-1> 這題怎麼解",
			selfID:  "bot-1",
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := b.humanMessageAddressesSelf(nil, tt.content, tt.selfID); got != tt.want {
				t.Fatalf("humanMessageAddressesSelf() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHumanMessageAddressesSelfUsesStructuredMentions(t *testing.T) {
	b := &Bot{peers: parseBotPeers("AlphaBot:bot-1:role-1,BetaBot:bot-2:role-2")}
	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		Content:  "@AlphaBot handle this",
		Mentions: []*discordgo.User{{ID: "bot-1"}},
	}}
	if !b.humanMessageAddressesSelf(msg, msg.Content, "bot-1") {
		t.Fatal("expected structured Discord mention to address self")
	}
}

func TestIsBotGeneratedNonResult(t *testing.T) {
	tests := []struct {
		content string
		want    bool
	}{
		{content: "🔄 處理中...", want: true},
		{content: "\u200b", want: true},
		{content: "thread queue full", want: true},
		{content: "transport closed", want: true},
		{content: "這是完成後的分析結果，請 review", want: false},
	}
	for _, tt := range tests {
		if got := isBotGeneratedNonResult(tt.content); got != tt.want {
			t.Fatalf("isBotGeneratedNonResult(%q) = %v, want %v", tt.content, got, tt.want)
		}
	}
}

func TestMessageHasReaction(t *testing.T) {
	msg := &discordgo.Message{Reactions: []*discordgo.MessageReactions{
		{Count: 1, Emoji: &discordgo.Emoji{Name: "✅"}},
	}}
	if !messageHasReaction(msg, "✅") {
		t.Fatal("expected done reaction to match")
	}
	if messageHasReaction(msg, "🔄") {
		t.Fatal("did not expect processing reaction to match")
	}
	if got := messageReactionState(msg); got != "done" {
		t.Fatalf("messageReactionState() = %q, want done", got)
	}
}

func TestMultiBotMentionOnlyCanBeOpenedByBack(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("AlphaBot:bot-1,BetaBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, nil)

	if needed, _ := b.requiresHumanMention(ds, "channel-1", "", "bot-1"); !needed {
		t.Fatal("effective multi-bot channel should require mention by default")
	}

	b.manager.Back("channel-1")
	if needed, _ := b.requiresHumanMention(ds, "channel-1", "", "bot-1"); needed {
		t.Fatal("/back should open full-listen mode for the target channel")
	}

	b.manager.Pause("channel-1")
	if needed, _ := b.requiresHumanMention(ds, "channel-1", "", "bot-1"); !needed {
		t.Fatal("/pause should restore mention-only mode")
	}
}

func TestRequiresHumanMentionReasons(t *testing.T) {
	ds := testPeerPermissionSession(t, nil)

	tests := []struct {
		name       string
		setup      func(*channel.Manager)
		targetID   string
		parentID   string
		peers      string
		wantNeeded bool
		wantReason string
	}{
		{
			name:       "paused override",
			setup:      func(m *channel.Manager) { m.Pause("channel-1") },
			targetID:   "channel-1",
			peers:      "AlphaBot:bot-1,BetaBot:bot-2",
			wantNeeded: true,
			wantReason: "paused",
		},
		{
			name:       "thread snapshot mention",
			setup:      func(m *channel.Manager) { m.SetThreadListenMode("thread-1", true) },
			targetID:   "thread-1",
			parentID:   "channel-1",
			peers:      "AlphaBot:bot-1,BetaBot:bot-2",
			wantNeeded: true,
			wantReason: "thread_snapshot_mention",
		},
		{
			name:       "unknown thread inherits parent thread mode off",
			setup:      func(m *channel.Manager) { m.SetThreadMode("channel-1", false) },
			targetID:   "thread-1",
			parentID:   "channel-1",
			peers:      "AlphaBot:bot-1,BetaBot:bot-2",
			wantNeeded: true,
			wantReason: "thread_inherit",
		},
		{
			name:       "channel thread mode off",
			setup:      func(m *channel.Manager) { m.SetThreadMode("channel-1", false) },
			targetID:   "channel-1",
			peers:      "AlphaBot:bot-1,BetaBot:bot-2",
			wantNeeded: true,
			wantReason: "thread_mode_off",
		},
		{
			name:       "multi bot parent paused",
			setup:      func(m *channel.Manager) { m.Pause("channel-1") },
			targetID:   "thread-1",
			parentID:   "channel-1",
			peers:      "AlphaBot:bot-1,BetaBot:bot-2",
			wantNeeded: true,
			wantReason: "multi_bot_parent_paused",
		},
		{
			name:       "multi bot",
			targetID:   "channel-1",
			peers:      "AlphaBot:bot-1,BetaBot:bot-2",
			wantNeeded: true,
			wantReason: "multi_bot",
		},
		{
			name:       "no responding peer",
			targetID:   "channel-1",
			peers:      "AlphaBot:bot-1",
			wantNeeded: false,
			wantReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := channel.NewManager(channel.ManagerConfig{})
			if tt.setup != nil {
				tt.setup(manager)
			}
			b := &Bot{peers: parseBotPeers(tt.peers), manager: manager}
			gotNeeded, gotReason := b.requiresHumanMention(ds, tt.targetID, tt.parentID, "bot-1")
			if gotNeeded != tt.wantNeeded || gotReason != tt.wantReason {
				t.Fatalf("requiresHumanMention() = (%v, %q), want (%v, %q)", gotNeeded, gotReason, tt.wantNeeded, tt.wantReason)
			}
		})
	}
}

func TestThreadMentionModeInheritsParentBack(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("AlphaBot:bot-1,BetaBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, nil)

	if needed, _ := b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1"); !needed {
		t.Fatal("thread should require mention by default when peer bot has effective thread access")
	}

	b.manager.Back("channel-1")
	if needed, _ := b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1"); needed {
		t.Fatal("thread should inherit parent /back full-listen override")
	}

	b.manager.Pause("thread-1")
	if needed, _ := b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1"); !needed {
		t.Fatal("thread /pause should override parent /back")
	}

	b.manager.Back("thread-1")
	if needed, _ := b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1"); needed {
		t.Fatal("thread /back should restore full-listen override")
	}
}

func TestThreadListenSnapshotOutlivesParentThreadModeChange(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("AlphaBot:bot-1,BetaBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, nil)
	b.manager.SetThreadListenMode("thread-1", false)
	b.manager.SetThreadMode("channel-1", false)

	if needed, _ := b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1"); needed {
		t.Fatal("full-listen thread snapshot should not become mention-only when parent thread mode is turned off")
	}
}

func TestUnknownThreadUsesParentThreadModeOffMentionOnly(t *testing.T) {
	b := &Bot{manager: channel.NewManager(channel.ManagerConfig{})}
	ds := testPeerPermissionSession(t, nil)
	b.manager.SetThreadMode("channel-1", false)

	if needed, _ := b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1"); !needed {
		t.Fatal("unknown thread under thread-mode-off parent should require mention")
	}
}

func TestChannelPauseBackToggleThreadMode(t *testing.T) {
	L.Load("en")
	b := &Bot{manager: channel.NewManager(channel.ManagerConfig{})}
	ctx := cmdCtx{channelID: "channel-1", targetID: "channel-1", reply: func(string) {}}

	b.cmdPause(ctx)
	if b.manager.ThreadModeEnabled("channel-1") {
		t.Fatal("channel /pause should disable new thread creation")
	}
	b.cmdBack(ctx)
	if !b.manager.ThreadModeEnabled("channel-1") {
		t.Fatal("channel /back should re-enable new thread creation")
	}

	threadCtx := cmdCtx{channelID: "channel-1", targetID: "thread-1", inThread: true, reply: func(string) {}}
	b.cmdPause(threadCtx)
	if !b.manager.ThreadModeEnabled("channel-1") {
		t.Fatal("thread /pause should not change parent channel thread mode")
	}
}

func TestMultiBotMentionOnlyIsChannelScoped(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("AlphaBot:bot-1,BetaBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberDenyOverwrite("bot-2")})
	ch2, err := ds.State.Channel("channel-2")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}

	if needed, _ := b.requiresHumanMention(ds, "channel-1", "", "bot-1"); needed {
		t.Fatal("peer without effective channel access should not force mention-only")
	}
	if needed, _ := b.requiresHumanMention(ds, "channel-2", "", "bot-1"); !needed {
		t.Fatal("peer with inherited effective channel access should force mention-only")
	}
	ch2.PermissionOverwrites = []*discordgo.PermissionOverwrite{botMemberAllowOverwrite("bot-2")}
	if needed, _ := b.requiresHumanMention(ds, "channel-2", "", "bot-1"); !needed {
		t.Fatal("peer with explicit channel allow should force mention-only")
	}
}

func TestPeerExplicitViewOverwriteForcesMentionOnlyWhenEffectiveSendAllows(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("AlphaBot:bot-1,BetaBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberViewOverwrite("bot-2")})

	if needed, _ := b.requiresHumanMention(ds, "channel-1", "", "bot-1"); !needed {
		t.Fatal("peer with explicit channel view allow and effective send permission should force mention-only")
	}
}

func TestPeerThreadReplyPermissionsForceMentionOnlyWithoutChannelSend(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("AlphaBot:bot-1,BetaBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberThreadReplyOverwrite("bot-2")})

	if needed, _ := b.requiresHumanMention(ds, "channel-1", "", "bot-1"); !needed {
		t.Fatal("peer that can create and reply in threads should force mention-only even without channel SendMessages")
	}
}

func TestPeerThreadPermissionsForceMentionOnlyInThreadWithoutParentChannelSend(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("AlphaBot:bot-1,BetaBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberThreadReplyOverwrite("bot-2")})

	if needed, _ := b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1"); !needed {
		t.Fatal("peer that can reply in the thread should force mention-only even without parent channel SendMessages")
	}
}

func TestRoleOnlyPeerRequiresExplicitChannelAllow(t *testing.T) {
	b := &Bot{
		peers: []BotPeer{
			{Name: "AlphaBot", ID: "bot-1", RoleID: "role-1"},
			{Name: "PeerRole", RoleID: "role-2", Manual: true},
		},
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, nil)

	if needed, _ := b.requiresHumanMention(ds, "channel-1", "", "bot-1"); needed {
		t.Fatal("role-only peer without explicit channel allow should not force mention-only")
	}
	ch, err := ds.State.Channel("channel-1")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	ch.PermissionOverwrites = []*discordgo.PermissionOverwrite{botRoleAllowOverwrite("role-2")}
	if needed, _ := b.requiresHumanMention(ds, "channel-1", "", "bot-1"); !needed {
		t.Fatal("manual role-only peer with explicit channel allow should force mention-only")
	}
}

func TestDiscoveredRoleOnlyPeerDoesNotForceMentionOnly(t *testing.T) {
	b := &Bot{
		peers: []BotPeer{
			{Name: "AlphaBot", ID: "bot-1", RoleID: "role-1"},
			{Name: "DiscoveredRole", RoleID: "role-2"},
		},
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botRoleAllowOverwrite("role-2")})

	if needed, _ := b.requiresHumanMention(ds, "channel-1", "", "bot-1"); needed {
		t.Fatal("auto-discovered role-only peer should not force mention-only")
	}
}

func TestDoctorBotPeersExplainsChannelTrigger(t *testing.T) {
	L.Load("en")
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberAllowOverwrite("bot-2")})
	b := &Bot{
		discord: ds,
		peers:   parseBotPeers("AlphaBot:bot-1,BetaBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}

	got := b.doctorBotPeers("channel-1")
	if !strings.Contains(got, "trigger: `BetaBot` (`bot-2`) via member overwrite") {
		t.Fatalf("doctor output missing trigger explanation:\n%s", got)
	}
	if !strings.Contains(got, "mention-only") {
		t.Fatalf("doctor output missing mention-only mode:\n%s", got)
	}
}

func TestDoctorBotPeersExplainsEffectivePermissionTrigger(t *testing.T) {
	L.Load("en")
	ds := testPeerPermissionSession(t, nil)
	b := &Bot{
		discord: ds,
		peers:   parseBotPeers("AlphaBot:bot-1,BetaBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}

	got := b.doctorBotPeers("channel-1")
	if !strings.Contains(got, "trigger: `BetaBot` (`bot-2`) via effective permissions") {
		t.Fatalf("doctor output missing effective permission trigger explanation:\n%s", got)
	}
	if !strings.Contains(got, "mention-only") {
		t.Fatalf("doctor output missing mention-only mode:\n%s", got)
	}
}

func TestDoctorBotPeersExplainsNoRespondingPeer(t *testing.T) {
	L.Load("en")
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberDenyOverwrite("bot-2")})
	b := &Bot{
		discord: ds,
		peers:   parseBotPeers("AlphaBot:bot-1,BetaBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}

	got := b.doctorBotPeers("channel-1")
	if !strings.Contains(got, "discovered peers, but none can respond in this channel/thread") {
		t.Fatalf("doctor output missing no-responding-peer explanation:\n%s", got)
	}
	if !strings.Contains(got, "channel/thread mode: open") {
		t.Fatalf("doctor output missing open mode:\n%s", got)
	}
}

func TestSlashCommandAllowedInTargetRequiresBotChannelAccess(t *testing.T) {
	b := &Bot{}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{
		{
			ID:   "bot-1",
			Type: discordgo.PermissionOverwriteTypeMember,
			Deny: discordgo.PermissionSendMessages,
		},
	})

	if b.slashCommandAllowedInTarget(ds, "channel-1") {
		t.Fatal("bot without channel send permission should not be allowed to run slash commands")
	}
	if !b.slashCommandAllowedInTarget(ds, "channel-2") {
		t.Fatal("bot with channel send permission should be allowed to run slash commands")
	}
}

func TestPeerPermissionCacheCachesChannelChecks(t *testing.T) {
	b := &Bot{peerPermCache: make(map[string]peerPermissionCacheEntry)}
	ds := testPeerPermissionSession(t, nil)

	if !b.peerCanRespondInTarget(ds, "bot-2", "channel-1") {
		t.Fatal("expected peer to be allowed initially")
	}
	// Deny after the first check; the second read should still use the TTL cache.
	ch, err := ds.State.Channel("channel-1")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	ch.PermissionOverwrites = []*discordgo.PermissionOverwrite{{
		ID:   "bot-2",
		Type: discordgo.PermissionOverwriteTypeMember,
		Deny: discordgo.PermissionSendMessages,
	}}
	if !b.peerCanRespondInTarget(ds, "bot-2", "channel-1") {
		t.Fatal("expected cached peer permission result")
	}
}

func TestUserCanManageAuditTargetUsesDiscordChannelPermissions(t *testing.T) {
	b := &Bot{}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{
		userMemberManageOverwrite("manager", discordgo.PermissionManageChannels),
	})
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "manager"}}); err != nil {
		t.Fatalf("MemberAdd manager: %v", err)
	}
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "viewer"}}); err != nil {
		t.Fatalf("MemberAdd viewer: %v", err)
	}

	if !b.userCanManageAuditTarget(ds, "manager", "channel-1") {
		t.Fatal("manager with channel manage permission should be allowed")
	}
	if b.userCanManageAuditTarget(ds, "viewer", "channel-1") {
		t.Fatal("viewer without manage permission should be denied")
	}
}

func TestUsageReportArgsForRequesterScopesNonManagersToSelf(t *testing.T) {
	b := &Bot{}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{
		userMemberManageOverwrite("channel-manager", discordgo.PermissionManageChannels),
	})
	guild, err := ds.State.Guild("guild-1")
	if err != nil {
		t.Fatalf("Guild: %v", err)
	}
	guild.Roles = append(guild.Roles,
		&discordgo.Role{ID: "guild-manager-role", Permissions: discordgo.PermissionManageGuild},
		&discordgo.Role{ID: "administrator-role", Permissions: discordgo.PermissionAdministrator},
	)
	for _, member := range []*discordgo.Member{
		{GuildID: "guild-1", User: &discordgo.User{ID: "channel-manager"}},
		{GuildID: "guild-1", User: &discordgo.User{ID: "guild-manager"}, Roles: []string{"guild-manager-role"}},
		{GuildID: "guild-1", User: &discordgo.User{ID: "administrator"}, Roles: []string{"administrator-role"}},
	} {
		if err := ds.State.MemberAdd(member); err != nil {
			t.Fatalf("MemberAdd %s: %v", member.User.ID, err)
		}
	}

	if got, ok := b.usageReportArgsForRequester(ds, "viewer", "channel-1", ""); !ok || got != "viewer" {
		t.Fatalf("viewer default args = %q/%v, want self/true", got, ok)
	}
	if _, ok := b.usageReportArgsForRequester(ds, "viewer", "channel-1", "guild-manager"); ok {
		t.Fatal("viewer should not inspect another user's usage")
	}
	if got, ok := b.usageReportArgsForRequester(ds, "channel-manager", "channel-1", ""); !ok || got != "channel-manager" {
		t.Fatalf("channel manager default args = %q/%v, want self/true", got, ok)
	}
	if _, ok := b.usageReportArgsForRequester(ds, "channel-manager", "channel-1", "viewer"); ok {
		t.Fatal("channel manager should not inspect guild-wide usage")
	}
	if got, ok := b.usageReportArgsForRequester(ds, "guild-manager", "channel-1", ""); !ok || got != "" {
		t.Fatalf("guild manager default args = %q/%v, want all/true", got, ok)
	}
	if got, ok := b.usageReportArgsForRequester(ds, "administrator", "thread-1", "viewer"); !ok || got != "viewer" {
		t.Fatalf("administrator target args = %q/%v, want viewer/true", got, ok)
	}
}

func TestUserCanManageAuditTargetFallsBackToThreadParent(t *testing.T) {
	b := &Bot{}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{
		userMemberManageOverwrite("manager", discordgo.PermissionManageChannels),
	})
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "manager"}}); err != nil {
		t.Fatalf("MemberAdd manager: %v", err)
	}
	if !b.userCanManageAuditTarget(ds, "manager", "thread-1") {
		t.Fatal("manager of parent channel should be allowed to audit thread")
	}
}

func TestCwdCommandRequiresChannelManager(t *testing.T) {
	L.Load("en")
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{
		userMemberManageOverwrite("manager", discordgo.PermissionManageChannels),
	})
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "manager"}}); err != nil {
		t.Fatalf("MemberAdd manager: %v", err)
	}
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "viewer"}}); err != nil {
		t.Fatalf("MemberAdd viewer: %v", err)
	}
	store, err := channel.NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	b := &Bot{
		discord: ds,
		manager: channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir(), Store: store}),
	}

	var viewerReplies []string
	b.cmdCwd(cmdCtx{
		channelID: "channel-1",
		targetID:  "channel-1",
		userID:    "viewer",
		reply:     func(msg string) { viewerReplies = append(viewerReplies, msg) },
	})
	if len(viewerReplies) != 1 || viewerReplies[0] != L.Get("cwd.forbidden") {
		t.Fatalf("viewer replies = %#v, want cwd forbidden", viewerReplies)
	}

	var managerReplies []string
	b.cmdCwd(cmdCtx{
		channelID: "channel-1",
		targetID:  "channel-1",
		userID:    "manager",
		reply:     func(msg string) { managerReplies = append(managerReplies, msg) },
	})
	if len(managerReplies) != 1 || !strings.Contains(managerReplies[0], "Current CWD") {
		t.Fatalf("manager replies = %#v, want current cwd", managerReplies)
	}
}

func TestSlashCommandsIncludeAgentAndUsage(t *testing.T) {
	foundAgent := false
	foundUsage := false
	foundInterrupt := false
	foundThread := false
	foundMCP := false
	foundSteering := false
	foundA2A := false
	for _, cmd := range buildSlashCommands() {
		if cmd.Name == "mcp" {
			foundMCP = true
			if len(cmd.Options) != 4 {
				t.Fatalf("/mcp should expose 4 subcommands, got %+v", cmd.Options)
			}
			if cmd.Options[0].Type != discordgo.ApplicationCommandOptionSubCommand {
				t.Fatalf("/mcp option should be subcommand, got %+v", cmd.Options[0])
			}
			if cmd.Options[0].Name != "manage" {
				t.Fatalf("/mcp first subcommand = %q, want manage", cmd.Options[0].Name)
			}
			for _, opt := range cmd.Options {
				switch opt.Name {
				case "catalog", "preset", "readonly", "destructive", "restart-agent":
					t.Fatalf("/mcp should not expose %q as a separate subcommand: %+v", opt.Name, cmd.Options)
				}
				if opt.Name == "status" {
					if len(opt.Options) != 1 || opt.Options[0].Name != "server" || opt.Options[0].Required {
						t.Fatalf("/mcp status server option should be optional, got %+v", opt.Options)
					}
				}
				if opt.Name == "enable" {
					if len(opt.Options) != 1 || opt.Options[0].Name != "server" {
						t.Fatalf("/mcp enable should only require server, got %+v", opt.Options)
					}
				}
			}
			continue
		}
		if cmd.Name == "steering" {
			foundSteering = true
			if len(cmd.Options) != 3 {
				t.Fatalf("/steering should expose 3 subcommands, got %+v", cmd.Options)
			}
			if cmd.Options[0].Name != "status" || cmd.Options[1].Name != "create" || cmd.Options[2].Name != "edit" {
				t.Fatalf("/steering subcommands = %+v", cmd.Options)
			}
			continue
		}
		if cmd.Name == "thread" {
			foundThread = true
			if len(cmd.Options) != 1 || cmd.Options[0].Name != "mode" {
				t.Fatalf("/thread options = %+v, want optional mode", cmd.Options)
			}
			continue
		}
		if cmd.Name == "interrupt" {
			foundInterrupt = true
			continue
		}
		if cmd.Name == "usage" {
			foundUsage = true
			if len(cmd.Options) != 1 || cmd.Options[0].Name != "user" {
				t.Fatalf("/usage options = %+v, want optional user", cmd.Options)
			}
			continue
		}
		if cmd.Name == "a2a" {
			foundA2A = true
			if len(cmd.Options) != 8 {
				t.Fatalf("/a2a should expose 8 subcommands, got %+v", cmd.Options)
			}
			if cmd.Options[0].Name != "peers" || cmd.Options[1].Name != "allow" || cmd.Options[2].Name != "ask" || cmd.Options[7].Name != "revoke" {
				t.Fatalf("/a2a subcommands = %+v", cmd.Options)
			}
			continue
		}
		if cmd.Name != "agent" {
			continue
		}
		foundAgent = true
		if len(cmd.Options) != 1 || cmd.Options[0].Name != "mode" {
			t.Fatalf("/agent options = %+v, want optional mode", cmd.Options)
		}
	}
	if !foundAgent || !foundUsage || !foundInterrupt || !foundThread || !foundMCP || !foundSteering || !foundA2A {
		t.Fatal("expected /agent, /usage, /interrupt, /thread, /mcp, /steering, and /a2a slash commands to be registered")
	}
}

func TestSlashCommandsOmitA2AWhenDisabled(t *testing.T) {
	for _, cmd := range buildSlashCommandsWithA2A(false) {
		if cmd.Name == "a2a" {
			t.Fatalf("A2A command registered when disabled: %+v", cmd)
		}
	}
	found := false
	for _, cmd := range buildSlashCommandsWithA2A(true) {
		if cmd.Name == "a2a" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("A2A command missing when enabled")
	}
}

func TestA2AConfirmationResponseUsesLocale(t *testing.T) {
	L.Load("zh-TW")
	got := formatA2AResponse(botmcp.A2AToolResponse{OK: true, RequiresConfirmation: true, ConfirmationSummary: "啟用 A2A", ChangeID: "change-1", ConfirmationToken: "token-1"})
	for _, want := range []string{"A2A 需要確認", "啟用 A2A", "change-1", "核准"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatA2AResponse = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "token-1") || strings.Contains(got, "confirmation_token:") {
		t.Fatalf("formatA2AResponse leaked raw confirmation token: %q", got)
	}
}

func TestA2ALocaleConfirmationResponse(t *testing.T) {
	L.Load("en")
	got := formatA2AResponse(botmcp.A2AToolResponse{OK: false, Message: "policy_denied: manager required"})
	if !strings.Contains(got, "A2A could not continue") || !strings.Contains(got, "Manage Channels") {
		t.Fatalf("formatA2AResponse error = %q, want localized actionable A2A error", got)
	}
}

func TestA2AFormatTaskResponseIsHumanReadable(t *testing.T) {
	L.Load("en")
	got := formatA2AResponse(botmcp.A2AToolResponse{OK: true, Message: "A2A task sent", Task: &botmcp.A2ATaskSummary{LocalID: "local-1", TaskID: "task-1", MessageID: "msg-1", FromAgent: "local-bot", ToAgent: "remote-bot", ChannelRef: "d80-main", SkillID: "general/task", ResultVisibility: "proxy", DiscordTranscriptMode: "delegator", State: a2a.TaskStateSubmitted, Revision: 2, Events: []botmcp.A2ATaskEventSummary{{Revision: 2, EventType: "status", State: a2a.TaskStateSubmitted, Content: "<@123> **queued**\n- State: `TASK_STATE_COMPLETED`"}}}})
	for _, want := range []string{"**A2A task sent**", "State", "local-bot", "remote-bot", "d80-main", "general/task", "Reply settings: `proxy`/`delegator`", "Events", "\\*\\*queued\\*\\*", "/a2a status"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatA2AResponse task = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "```json") {
		t.Fatalf("formatA2AResponse task exposed raw JSON: %q", got)
	}
	if strings.Contains(got, "<@123>") {
		t.Fatalf("formatA2AResponse task exposed raw mention: %q", got)
	}
	if strings.Contains(got, "\n- State: `TASK_STATE_COMPLETED`") {
		t.Fatalf("formatA2AResponse task allowed event content to spoof a status row: %q", got)
	}
}

func TestA2AFormatRejectedTaskIncludesPolicyRetryGuidance(t *testing.T) {
	L.Load("en")
	got := formatA2AResponse(botmcp.A2AToolResponse{OK: true, Message: "A2A task loaded", Task: &botmcp.A2ATaskSummary{LocalID: "local-1", TaskID: "task-1", FromAgent: "local-bot", ToAgent: "remote-bot", State: a2a.TaskStateRejected, ErrorCode: a2a.ErrorSenderNotAllowed, ErrorMessage: "sender is not accepted"}})
	for _, want := range []string{"sender is not accepted", "the other bot rejected this sender", "must allow this bot", "/a2a status"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatA2AResponse rejected task = %q, missing %q", got, want)
		}
	}
}

func TestA2ASetupSlashDefaultsHumanFlow(t *testing.T) {
	raw := a2aArgsFromSlashOptions([]*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "setup",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "peer_agent", Type: discordgo.ApplicationCommandOptionString, Value: "remote-bot"},
			{Name: "mode", Type: discordgo.ApplicationCommandOptionString, Value: "co_present"},
		},
	}}, "guild-1", "channel-1", "user-1", "alice", true)
	var payload a2aSlashPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Subcommand != "setup" || payload.Request.Enable == nil || !*payload.Request.Enable {
		t.Fatalf("setup payload enable = %+v", payload)
	}
	for _, want := range []string{"remote-bot"} {
		if len(payload.Request.AcceptFrom) != 1 || payload.Request.AcceptFrom[0] != want || len(payload.Request.DelegateTo) != 1 || payload.Request.DelegateTo[0] != want {
			t.Fatalf("setup peer defaults = %+v, want %s", payload.Request, want)
		}
	}
	if payload.Request.SkillID != "task" || len(payload.Request.AcceptSkills) != 1 || payload.Request.AcceptSkills[0] != "task" || len(payload.Request.ExposeSkills) != 1 || payload.Request.ExposeSkills[0] != "task" || len(payload.Request.DelegateSkills) != 1 || payload.Request.DelegateSkills[0] != "task" {
		t.Fatalf("setup skill defaults = %+v", payload.Request)
	}
	if payload.Request.TranscriptMode != "co_present" || payload.Request.ShareDiscordContext == nil || !*payload.Request.ShareDiscordContext {
		t.Fatalf("setup UX defaults = %+v", payload.Request)
	}
	if payload.Request.TargetChannelRef != "" {
		t.Fatalf("setup should not default target runtime to current channel = %+v", payload.Request)
	}
}

func TestA2ATrustSlashDefaultsInboundAutoWithoutSkill(t *testing.T) {
	raw := a2aArgsFromSlashOptions([]*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "trust",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "peer_agent", Type: discordgo.ApplicationCommandOptionString, Value: "remote-bot-ch-2cbaf623"},
		},
	}}, "guild-1", "channel-1", "user-1", "alice", true)
	var payload a2aSlashPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Subcommand != "trust" || payload.Request.TargetAgent != "remote-bot-ch-2cbaf623" {
		t.Fatalf("trust payload target = %+v", payload)
	}
	if payload.Request.SkillID != "" || payload.Request.TrustRelationship != "inbound" || payload.Request.SetupMode != "auto" {
		t.Fatalf("trust defaults = %+v", payload.Request)
	}
}

func TestA2ADelegateSlashKeepsSkillAndReasonOptional(t *testing.T) {
	raw := a2aArgsFromSlashOptions([]*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "delegate",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "target_agent", Type: discordgo.ApplicationCommandOptionString, Value: "remote-bot-ch-2cbaf623"},
			{Name: "message", Type: discordgo.ApplicationCommandOptionString, Value: "ping"},
		},
	}}, "guild-1", "channel-1", "user-1", "alice", false)
	var payload a2aSlashPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Request.SkillID != "" || payload.Request.Reason != "" || payload.Request.TargetChannelRef != "" {
		t.Fatalf("delegate should preserve optional resolver fields = %+v", payload.Request)
	}
}

func TestA2ASetupSlashAutoCrossRuntimeDefersDeliveryDefaults(t *testing.T) {
	raw := a2aArgsFromSlashOptions([]*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "setup",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "peer_agent", Type: discordgo.ApplicationCommandOptionString, Value: "remote-bot"},
			{Name: "target_channel_ref", Type: discordgo.ApplicationCommandOptionString, Value: "erp-support"},
		},
	}}, "guild-1", "channel-1", "user-1", "alice", true)
	var payload a2aSlashPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Request.SetupMode != "auto" || payload.Request.TargetChannelRef != "erp-support" {
		t.Fatalf("setup target runtime = %+v", payload.Request)
	}
	if payload.Request.ResultVisibility != "" || payload.Request.TranscriptMode != "" || payload.Request.ShareDiscordContext != nil {
		t.Fatalf("cross-runtime auto should defer delivery defaults to bot-tools = %+v", payload.Request)
	}
}

func TestA2ATaskOptionAcceptsDisplayedLocalID(t *testing.T) {
	raw := a2aArgsFromSlashOptions([]*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "status",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "task", Type: discordgo.ApplicationCommandOptionString, Value: "local_1234"},
		},
	}}, "guild-1", "channel-1", "user-1", "alice", false)
	var payload a2aSlashPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Request.LocalID != "local_1234" || payload.Request.TaskID != "" {
		t.Fatalf("task option = %+v, want local id lookup", payload.Request)
	}
}

func TestA2ATaskOptionKeepsUnprefixedValueInTaskIDField(t *testing.T) {
	raw := a2aArgsFromSlashOptions([]*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "status",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "task", Type: discordgo.ApplicationCommandOptionString, Value: "444444444444444444"},
		},
	}}, "guild-1", "channel-1", "user-1", "alice", false)
	var payload a2aSlashPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Request.TaskID != "444444444444444444" || payload.Request.MessageID != "" || payload.Request.LocalID != "" {
		t.Fatalf("task option = %+v, want task id lookup with service-side message id fallback", payload.Request)
	}
}

func TestA2ATranscriptModeSafeClearsContextSharing(t *testing.T) {
	raw := a2aArgsFromSlashOptions([]*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "transcript-mode",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "mode", Type: discordgo.ApplicationCommandOptionString, Value: "safe"},
		},
	}}, "guild-1", "channel-1", "user-1", "alice", true)
	var payload a2aSlashPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Request.TranscriptMode != "delegator" || payload.Request.ShareDiscordContext == nil || *payload.Request.ShareDiscordContext {
		t.Fatalf("transcript-mode safe defaults = %+v", payload.Request)
	}
}

func TestA2APolicyFormatterIncludesPolicyMutationFields(t *testing.T) {
	L.Load("en")
	got := formatA2APolicy("Preview", a2a.ChannelA2APolicy{Enabled: true, ChannelRef: "d80-test", ResultVisibility: "transparent", DiscordTranscriptMode: "co_present", ShareDiscordContext: true, AcceptFrom: []string{"remote-bot"}, AcceptFromRuntimes: []string{"remote-bot-main"}, AcceptSkills: []string{"task"}, ExposeSkills: []a2a.SkillPolicy{{ID: "task"}}, DelegateTo: []string{"remote-bot"}, DelegateSkills: []string{"task"}, DelegateTargets: []a2a.DelegateTargetPolicy{{RuntimeAgentID: "remote-bot-main", SkillID: "task"}}, CoPresentFromRuntimes: []string{"remote-bot-main"}, CoPresentTargetChannels: []string{"222222222222222222"}, MaxConcurrent: 3})
	for _, want := range []string{"Result handling", "Bot channels allowed to send work", "`remote-bot-main inbound default_task`", "`remote-bot-main outbound default_task`", "Local capabilities shown to other bots", "`task`", "Max simultaneous received tasks: 3", "Shared-reply readiness: ready locally", "Shared-reply allowed bot channels: `remote-bot-main`", "Extra channels for shared replies: `222222222222222222`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatA2APolicy = %q, missing %q", got, want)
		}
	}
}

func TestA2APeersFormatterShowsRuntimeContext(t *testing.T) {
	L.Load("en")
	got := formatA2APeers([]botmcp.A2APeerSummary{{
		AgentID:           "m5bot-backend-support",
		BotAgentID:        "m5bot",
		Name:              "m5bot-backend-support",
		DisplayName:       "Backend Support",
		ChannelRef:        "backend-support",
		Runtime:           "channel",
		Online:            true,
		Trusted:           true,
		DelegationAllowed: true,
		DelegationReason:  "allowed",
		Skills:            []string{"backend-support/task"},
	}}, nil)
	for _, want := range []string{"Bots/channels this Discord channel can work with", "Backend Support", "m5bot-backend-support", "bot `m5bot`", "label `backend-support`", "allowed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatA2APeers = %q, missing %q", got, want)
		}
	}
}

func TestA2AButtonsConfirmationCustomIDIsSigned(t *testing.T) {
	t.Setenv("A2A_CONFIRMATION_SECRET", "component-secret")
	customID := a2aConfirmationButtonCustomID("policy_apply", "channel-1", "change-1")
	if len(customID) > 100 {
		t.Fatalf("custom id length = %d, want Discord-safe <= 100", len(customID))
	}
	if !validateA2AConfirmationButtonCustomID(customID, "policy_apply", "channel-1", "change-1") {
		t.Fatal("signed A2A confirmation button custom_id did not validate")
	}
	if validateA2AConfirmationButtonCustomID(customID, "policy_apply", "channel-2", "change-1") {
		t.Fatal("signed A2A confirmation button custom_id accepted wrong channel")
	}

	buttonID := a2aPolicyConfirmationButtonCustomID("apply", "state-1", "channel-1", "change-1")
	if len(buttonID) > 100 {
		t.Fatalf("policy button custom id length = %d, want Discord-safe <= 100", len(buttonID))
	}
	if !validateA2APolicyConfirmationButtonCustomID(buttonID, "apply", "state-1", "channel-1", "change-1") {
		t.Fatal("signed A2A policy button custom_id did not validate")
	}
	if validateA2APolicyConfirmationButtonCustomID(buttonID, "apply", "state-1", "channel-2", "change-1") {
		t.Fatal("signed A2A policy button custom_id accepted wrong channel")
	}
}

func TestA2AComponentSecretFallbackIsProcessRandom(t *testing.T) {
	t.Setenv("A2A_CONFIRMATION_SECRET", "")
	t.Setenv("DISCORD_TOKEN", "")
	first := a2aComponentSecret()
	second := a2aComponentSecret()
	if first == "" || first != second {
		t.Fatalf("fallback component secret unstable: first=%q second=%q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("fallback component secret length = %d, want 64 hex chars", len(first))
	}
}

func TestA2AConfirmationComponentsStoreTokenForButtonApply(t *testing.T) {
	L.Load("en")
	b := &Bot{a2aConfirmations: newA2APolicyConfirmationStore(func() time.Time { return time.Unix(100, 0).UTC() })}
	payload := a2aSlashPayload{Request: botmcp.A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedByID: "user-1", RequestedBy: "alice"}}
	resp := botmcp.A2AToolResponse{OK: true, RequiresConfirmation: true, ChangeID: "change-1", ConfirmationToken: "token-1", ExpiresAt: time.Unix(700, 0).UTC().Format(time.RFC3339)}
	components := b.a2aPolicyConfirmationComponents(payload, resp)
	if len(components) != 1 {
		t.Fatalf("components = %+v, want one action row", components)
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok || len(row.Components) != 2 {
		t.Fatalf("components = %+v, want apply/cancel buttons", components)
	}
	button, ok := row.Components[0].(discordgo.Button)
	if !ok || button.Label != "Approve" || button.Style != discordgo.SuccessButton {
		t.Fatalf("apply button = %+v", row.Components[0])
	}
	parts := strings.Split(button.CustomID, ":")
	if len(parts) != 5 {
		t.Fatalf("button custom id = %q", button.CustomID)
	}
	entry, ok := b.a2aConfirmations.Get(parts[3])
	if !ok {
		t.Fatal("confirmation entry not stored")
	}
	if entry.Payload.Request.ConfirmationToken != "token-1" || entry.Payload.Request.ChangeID != "change-1" {
		t.Fatalf("stored payload = %+v", entry.Payload.Request)
	}
	if strings.Contains(formatA2AResponse(resp), "token-1") {
		t.Fatalf("button confirmation response leaked token")
	}
}

func TestA2AConfirmationRoutingClassifiesDelegateAsRequesterApproval(t *testing.T) {
	for _, subcommand := range []string{"ask", "delegate"} {
		if !isA2ADelegateConfirmation(subcommand) {
			t.Fatalf("%s confirmation should replay Delegate without manager-only policy apply", subcommand)
		}
		if isA2ATrustConfirmation(subcommand) {
			t.Fatalf("%s confirmation must not route to TrustPeer", subcommand)
		}
	}
	for _, subcommand := range []string{"allow", "trust"} {
		if !isA2ATrustConfirmation(subcommand) {
			t.Fatalf("%s confirmation should route to TrustPeer", subcommand)
		}
		if isA2ADelegateConfirmation(subcommand) {
			t.Fatalf("%s confirmation must not route to Delegate", subcommand)
		}
	}
}

func TestA2ASlashPermissionPolicy(t *testing.T) {
	for _, cmd := range buildSlashCommands() {
		if cmd.Name != "a2a" {
			continue
		}
		if cmd.DefaultMemberPermissions != nil {
			t.Fatalf("/a2a permissions = %v, want unrestricted default", cmd.DefaultMemberPermissions)
		}
		if isChannelOnlySlashCommand("a2a") {
			t.Fatal("/a2a should allow thread-aware low-friction subcommands")
		}
		return
	}
	t.Fatal("/a2a command not registered")
}

func TestSlashCommandOptionsKeepRequiredBeforeOptional(t *testing.T) {
	var walk func(prefix string, opts []*discordgo.ApplicationCommandOption)
	walk = func(prefix string, opts []*discordgo.ApplicationCommandOption) {
		seenOptional := false
		for _, opt := range opts {
			if opt.Type == discordgo.ApplicationCommandOptionSubCommand || opt.Type == discordgo.ApplicationCommandOptionSubCommandGroup {
				walk(prefix+"/"+opt.Name, opt.Options)
				continue
			}
			if !opt.Required {
				seenOptional = true
				continue
			}
			if seenOptional {
				t.Fatalf("%s option %q is required after an optional option", prefix, opt.Name)
			}
		}
	}
	for _, cmd := range buildSlashCommands() {
		walk("/"+cmd.Name, cmd.Options)
	}
}
func TestSlashCommandsApplyVisibilityAndPermissionPolicy(t *testing.T) {
	managed := map[string]bool{
		"audit": true, "mcp": true, "cwd": true, "start": true, "agent": true,
		"steering": true,
		"cron":     true, "cron-list": true, "cron-run": true, "cron-prompt": true,
		"memory": true, "flashmemory": true, "clear": true,
	}
	for _, cmd := range buildSlashCommands() {
		if cmd.Contexts == nil || len(*cmd.Contexts) != 1 || (*cmd.Contexts)[0] != discordgo.InteractionContextGuild {
			t.Fatalf("/%s contexts = %+v, want guild-only", cmd.Name, cmd.Contexts)
		}
		if managed[cmd.Name] {
			if cmd.DefaultMemberPermissions == nil || *cmd.DefaultMemberPermissions != int64(discordgo.PermissionManageChannels) {
				t.Fatalf("/%s default member permissions = %v, want ManageChannels", cmd.Name, cmd.DefaultMemberPermissions)
			}
			continue
		}
		if cmd.DefaultMemberPermissions != nil {
			t.Fatalf("/%s should not have default member permissions, got %d", cmd.Name, *cmd.DefaultMemberPermissions)
		}
	}
}

func TestCommandRequiresInitializedChannelPolicy(t *testing.T) {
	tests := []struct {
		name string
		args string
		want bool
	}{
		{name: "start", want: true},
		{name: "reset", want: true},
		{name: "compact", want: true},
		{name: "clear", want: true},
		{name: "cron", want: true},
		{name: "cron-run", want: true},
		{name: "cron-prompt", want: true},
		{name: "model", want: false},
		{name: "model", args: "claude-sonnet", want: true},
		{name: "agent", want: false},
		{name: "agent", args: "autopilot", want: true},
		{name: "memory", args: "list", want: false},
		{name: "memory", args: "add project rules", want: true},
		{name: "flashmemory", args: "clear", want: true},
		{name: "mcp", args: "status", want: false},
		{name: "mcp", args: "manage", want: true},
		{name: "mcp", args: "enable server:bot-tools", want: true},
		{name: "steering", args: "status", want: true},
		{name: "status", want: false},
		{name: "doctor", want: false},
	}
	for _, tt := range tests {
		if got := commandRequiresInitializedChannel(tt.name, tt.args); got != tt.want {
			t.Fatalf("commandRequiresInitializedChannel(%q, %q) = %v, want %v", tt.name, tt.args, got, tt.want)
		}
	}
}

func TestMCPArgsFromSlashOptions(t *testing.T) {
	got := mcpArgsFromSlashOptions([]*discordgo.ApplicationCommandInteractionDataOption{{
		Type: discordgo.ApplicationCommandOptionSubCommand,
		Name: "manage",
	}})
	if got != "manage" {
		t.Fatalf("mcp manage args = %q", got)
	}

}

func TestCmdClearThreadClearsLocalHistoryWithoutActiveAgent(t *testing.T) {
	L.Load("en")
	dataDir := t.TempDir()
	store, err := channel.NewSessionStore(dataDir)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	m := channel.NewManager(channel.ManagerConfig{DataDir: dataDir, Store: store, GuildID: "guild-1"})
	b := &Bot{manager: m}

	var reply string
	b.cmdClear(cmdCtx{
		channelID: "channel-1",
		targetID:  "thread-1",
		inThread:  true,
		reply: func(s string) {
			reply = s
		},
		replyWithMetadata: func(s string, _ map[string]any) {
			reply = s
		},
	})

	if strings.Contains(reply, "no thread agent") {
		t.Fatalf("clear should not fail when only local thread history exists: %q", reply)
	}
	if !strings.Contains(reply, "No active thread agent was running") {
		t.Fatalf("clear response should describe local-only clear, got: %q", reply)
	}
	var sessions map[string]channel.Session
	data, err := os.ReadFile(filepath.Join(dataDir, "sessions.json"))
	if err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	if err := json.Unmarshal(data, &sessions); err != nil {
		t.Fatalf("unmarshal sessions: %v", err)
	}
	var found bool
	for _, sess := range sessions {
		if sess.TargetType == "thread" && sess.TargetID == "thread-1" {
			found = true
			if sess.ParentChannelID != "channel-1" {
				t.Fatalf("parent channel = %q, want channel-1", sess.ParentChannelID)
			}
			if sess.SessionID != "" || sess.AgentName != "" {
				t.Fatalf("local clear should not persist reusable agent session: %+v", sess)
			}
		}
	}
	if !found {
		t.Fatalf("thread session with cleared agent metadata not found in %#v", sessions)
	}
}

func TestBuildMCPManagePanel(t *testing.T) {
	L.Load("en")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"context7":{"command":"npx","args":["-y","@upstash/context7-mcp"]},"generic-tools":{"command":"/tmp/generic-tools"}}}`), 0644); err != nil {
		t.Fatalf("write mcp config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	m := channel.NewManager(channel.ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()
	b := &Bot{manager: m}

	content, components := b.buildMCPManagePanel("channel-1", "context7")
	if !strings.Contains(content, "MCP policy panel") || !strings.Contains(content, "context7") {
		t.Fatalf("unexpected panel content:\n%s", content)
	}
	if len(components) != 4 {
		t.Fatalf("components len = %d, want select + actions + tools + restart", len(components))
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok || len(row.Components) != 1 {
		t.Fatalf("first row should contain select menu: %+v", components[0])
	}
	menu, ok := row.Components[0].(discordgo.SelectMenu)
	if !ok {
		t.Fatalf("first component should be select menu: %+v", row.Components[0])
	}
	if len(menu.Options) < 2 {
		t.Fatalf("select options = %+v", menu.Options)
	}
	labels := make([]string, 0, len(menu.Options))
	for _, opt := range menu.Options {
		labels = append(labels, opt.Label)
	}
	if !containsAll(strings.Join(labels, ","), "context7", "generic-tools") {
		t.Fatalf("select options missing configured servers: %+v", menu.Options)
	}
	actionRow, ok := components[1].(discordgo.ActionsRow)
	if !ok || len(actionRow.Components) != 2 {
		t.Fatalf("action row should contain full and disable buttons: %+v", components[1])
	}
}

func TestBuildCWDPanelListsDefaultProjects(t *testing.T) {
	L.Load("en")
	root := filepath.Join(t.TempDir(), "projects")
	project := filepath.Join(root, "kiro-discord-bot")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0755); err != nil {
		t.Fatalf("mkdir project marker: %v", err)
	}
	store, err := channel.NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	m := channel.NewManager(channel.ManagerConfig{DefaultCWD: root, Store: store})
	b := &Bot{manager: m}

	content, components := b.buildCWDPanel("channel-1", "")
	if !strings.Contains(content, "Channel project setup") || !strings.Contains(content, "Select one of 1 discovered project") {
		t.Fatalf("unexpected cwd panel content:\n%s", content)
	}
	if len(components) != 2 {
		t.Fatalf("components len = %d, want select + actions", len(components))
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok || len(row.Components) != 1 {
		t.Fatalf("first row should contain select menu: %+v", components[0])
	}
	menu, ok := row.Components[0].(discordgo.SelectMenu)
	if !ok {
		t.Fatalf("first component should be select menu: %+v", row.Components[0])
	}
	if len(menu.Options) != 1 || menu.Options[0].Value != cwdProjectToken("kiro-discord-bot") {
		t.Fatalf("menu options = %+v, want project token value", menu.Options)
	}
	if menu.CustomID != "cwdui:select:channel-1:0" {
		t.Fatalf("select custom id = %q, want page-scoped select id", menu.CustomID)
	}
	if got := b.resolveCWDProjectToken(menu.Options[0].Value); got != "kiro-discord-bot" {
		t.Fatalf("resolved project token = %q, want relative project path", got)
	}
}

func TestBuildCWDPanelPaginatesDefaultProjects(t *testing.T) {
	L.Load("en")
	root := filepath.Join(t.TempDir(), "projects")
	for i := 1; i <= 30; i++ {
		name := fmt.Sprintf("project-%02d", i)
		if i == 30 {
			name = "unicode-project-測試-30"
		}
		if err := os.MkdirAll(filepath.Join(root, name), 0755); err != nil {
			t.Fatalf("mkdir project %s: %v", name, err)
		}
	}
	store, err := channel.NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	b := &Bot{manager: channel.NewManager(channel.ManagerConfig{DefaultCWD: root, Store: store})}

	content, components := b.buildCWDPanelPage("channel-1", "", 0)
	if !strings.Contains(content, "Select one of 30 discovered project") || !strings.Contains(content, "Showing projects 1-25 of 30") {
		t.Fatalf("unexpected first page content:\n%s", content)
	}
	firstMenu := cwdPanelSelectMenu(t, components)
	if len(firstMenu.Options) != cwdPageSize {
		t.Fatalf("first page option count = %d, want %d", len(firstMenu.Options), cwdPageSize)
	}
	firstButtons := cwdPanelButtons(t, components)
	if len(firstButtons) != 4 {
		t.Fatalf("first page buttons = %+v, want new/refresh/prev/next", firstButtons)
	}
	if !firstButtons[2].Disabled || firstButtons[3].Disabled {
		t.Fatalf("first page prev/next disabled state = prev:%v next:%v, want prev disabled and next enabled", firstButtons[2].Disabled, firstButtons[3].Disabled)
	}

	content, components = b.buildCWDPanelPage("channel-1", "", 1)
	if !strings.Contains(content, "Showing projects 26-30 of 30") {
		t.Fatalf("unexpected second page content:\n%s", content)
	}
	secondMenu := cwdPanelSelectMenu(t, components)
	if len(secondMenu.Options) != 5 {
		t.Fatalf("second page option count = %d, want 5", len(secondMenu.Options))
	}
	foundUnicode := false
	for _, option := range secondMenu.Options {
		if option.Label == "unicode-project-測試-30" {
			foundUnicode = true
			if got := b.resolveCWDProjectToken(option.Value); got != "unicode-project-測試-30" {
				t.Fatalf("resolved unicode token = %q, want unicode project", got)
			}
		}
	}
	if !foundUnicode {
		t.Fatalf("second page options = %+v, want unicode project", secondMenu.Options)
	}
	secondButtons := cwdPanelButtons(t, components)
	if secondButtons[2].Disabled || !secondButtons[3].Disabled {
		t.Fatalf("second page prev/next disabled state = prev:%v next:%v, want prev enabled and next disabled", secondButtons[2].Disabled, secondButtons[3].Disabled)
	}
}

func cwdPanelSelectMenu(t *testing.T, components []discordgo.MessageComponent) discordgo.SelectMenu {
	t.Helper()
	if len(components) < 1 {
		t.Fatalf("components len = %d, want select row", len(components))
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok || len(row.Components) != 1 {
		t.Fatalf("first row should contain select menu: %+v", components[0])
	}
	menu, ok := row.Components[0].(discordgo.SelectMenu)
	if !ok {
		t.Fatalf("first component should be select menu: %+v", row.Components[0])
	}
	return menu
}

func cwdPanelButtons(t *testing.T, components []discordgo.MessageComponent) []discordgo.Button {
	t.Helper()
	if len(components) < 2 {
		t.Fatalf("components len = %d, want action row", len(components))
	}
	row, ok := components[1].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("second row should contain action buttons: %+v", components[1])
	}
	buttons := make([]discordgo.Button, 0, len(row.Components))
	for _, component := range row.Components {
		button, ok := component.(discordgo.Button)
		if !ok {
			t.Fatalf("action component should be a button: %+v", component)
		}
		buttons = append(buttons, button)
	}
	return buttons
}

func TestCWDConfirmButtonsPreserveProjectTokenAndPage(t *testing.T) {
	L.Load("en")
	token := cwdProjectToken("unicode-project-測試")
	row, ok := cwdConfirmButtons("channel-1", token, 1).(discordgo.ActionsRow)
	if !ok || len(row.Components) != 2 {
		t.Fatalf("confirm row = %+v, want confirm and back buttons", row)
	}
	confirmButton, ok := row.Components[0].(discordgo.Button)
	if !ok {
		t.Fatalf("confirm component should be button: %+v", row.Components[0])
	}
	if confirmButton.CustomID != "cwdui:confirm:channel-1:"+token+":1" {
		t.Fatalf("confirm custom id = %q, want token and page", confirmButton.CustomID)
	}
	backButton, ok := row.Components[1].(discordgo.Button)
	if !ok {
		t.Fatalf("back component should be button: %+v", row.Components[1])
	}
	if backButton.CustomID != "cwdui:back:channel-1:1" {
		t.Fatalf("back custom id = %q, want original page", backButton.CustomID)
	}
}

func TestCWDProjectOptionsUseTokensForUnicodeProjectNames(t *testing.T) {
	L.Load("en")
	name := strings.Repeat("測試專案", 12)
	projects := []channel.ProjectOption{{
		Name:        name,
		Relative:    name,
		Description: name + " | .git",
	}}

	options := cwdProjectOptions(projects)
	if len(options) != 1 {
		t.Fatalf("options = %+v, want unicode project to remain selectable", options)
	}
	if options[0].Label == "" || !strings.Contains(options[0].Label, "測試專案") {
		t.Fatalf("label = %q, want visible unicode project name", options[0].Label)
	}
	if options[0].Value == name || !strings.HasPrefix(options[0].Value, cwdProjectTokenID) || len(options[0].Value) > 100 {
		t.Fatalf("value = %q, want short project token", options[0].Value)
	}
}

func TestCWDSetupCompleteOffersMCPAndSteeringNextSteps(t *testing.T) {
	L.Load("en")
	msg := cwdSetupCompleteMessage("Channel initialized.")
	if !strings.Contains(msg, "Channel initialized.") || !strings.Contains(msg, "review MCP tool access") || !strings.Contains(msg, "agent context") {
		t.Fatalf("unexpected complete message: %q", msg)
	}

	components := cwdSetupCompleteComponents("channel-1")
	if len(components) != 1 {
		t.Fatalf("components len = %d, want one action row", len(components))
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok || len(row.Components) != 2 {
		t.Fatalf("completion row should contain exactly two buttons: %+v", components[0])
	}
	mcpButton, ok := row.Components[0].(discordgo.Button)
	if !ok {
		t.Fatalf("completion component should be a button: %+v", row.Components[0])
	}
	if mcpButton.CustomID != "cwdui:mcp:channel-1" || mcpButton.Label != "Review MCP settings" {
		t.Fatalf("unexpected mcp button: %+v", mcpButton)
	}
	steeringButton, ok := row.Components[1].(discordgo.Button)
	if !ok {
		t.Fatalf("completion component should be a button: %+v", row.Components[1])
	}
	if steeringButton.CustomID != "steerui:create:channel-1" || steeringButton.Label != "Create agent context" {
		t.Fatalf("unexpected steering button: %+v", steeringButton)
	}
}

func TestSteeringCreateModalRowsCollectReusableContext(t *testing.T) {
	L.Load("en")
	rows := []discordgo.MessageComponent{
		steeringTextInputRow("background", "Background and goals", "Project or recurring task", true),
		steeringTextInputRow("working_style", "How the agent should work", "Reply style", false),
		steeringTextInputRow("references", "Common info or checks", "Commands and docs", false),
		steeringTextInputRow("constraints", "Limits and safety notes", "Do-not-touch files", false),
		steeringTextInputRow("extra", "Other context", "Anything reusable", false),
	}
	wantIDs := []string{"background", "working_style", "references", "constraints", "extra"}
	for idx, rowComponent := range rows {
		row, ok := rowComponent.(discordgo.ActionsRow)
		if !ok || len(row.Components) != 1 {
			t.Fatalf("row %d = %+v, want one text input", idx, rowComponent)
		}
		input, ok := row.Components[0].(discordgo.TextInput)
		if !ok {
			t.Fatalf("row %d component = %+v, want text input", idx, row.Components[0])
		}
		if input.CustomID != wantIDs[idx] {
			t.Fatalf("row %d custom id = %q, want %q", idx, input.CustomID, wantIDs[idx])
		}
		if input.Style != discordgo.TextInputParagraph || input.MaxLength != steeringDraftLimit {
			t.Fatalf("row %d input config = %+v, want paragraph with draft limit", idx, input)
		}
	}
	backgroundRow := rows[0].(discordgo.ActionsRow)
	backgroundInput := backgroundRow.Components[0].(discordgo.TextInput)
	if !backgroundInput.Required {
		t.Fatal("background input should be required")
	}
	workingStyleRow := rows[1].(discordgo.ActionsRow)
	workingStyleInput := workingStyleRow.Components[0].(discordgo.TextInput)
	if workingStyleInput.Required {
		t.Fatal("optional steering inputs should not be required")
	}
}

func TestChannelSetupPromptCooldownSuppressesRepeatedUserPrompts(t *testing.T) {
	L.Load("en")
	ds := testPeerPermissionSession(t, nil)
	transport := &countingDiscordTransport{}
	ds.Client = &http.Client{Transport: transport}

	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	b := &Bot{
		setupPromptCooldown: newSetupPromptCooldown(func() time.Time { return now }),
	}
	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "message-1",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Author:    &discordgo.User{ID: "viewer", Username: "viewer"},
	}}

	b.sendChannelSetupPrompt(ds, msg)
	b.sendChannelSetupPrompt(ds, msg)
	if got := transport.Count(); got != 1 {
		t.Fatalf("sent setup prompts = %d, want 1 within cooldown", got)
	}

	now = now.Add(setupPromptCooldownDuration + time.Second)
	b.sendChannelSetupPrompt(ds, msg)
	if got := transport.Count(); got != 2 {
		t.Fatalf("sent setup prompts = %d, want 2 after cooldown expires", got)
	}
}

func TestMCPManagePanelUsesShortComponentPayloads(t *testing.T) {
	L.Load("en")
	dir := t.TempDir()
	longServer := "vendor:" + strings.Repeat("very-long-context-server/", 8)
	cfg := fmt.Sprintf(`{"mcpServers":{%q:{"command":"/tmp/long-mcp"}}}`, longServer)
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write mcp config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	m := channel.NewManager(channel.ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()
	b := &Bot{manager: m}

	_, components := b.buildMCPManagePanel("123456789012345678", longServer)
	for _, customID := range collectMCPComponentCustomIDs(components) {
		if len(customID) > 100 {
			t.Fatalf("custom_id length = %d, want <= 100: %q", len(customID), customID)
		}
		if strings.Contains(customID, longServer) {
			t.Fatalf("custom_id leaked raw server name: %q", customID)
		}
	}
	row := components[0].(discordgo.ActionsRow)
	menu := row.Components[0].(discordgo.SelectMenu)
	var selected discordgo.SelectMenuOption
	for _, opt := range menu.Options {
		if opt.Default {
			selected = opt
			break
		}
	}
	if selected.Value == "" {
		t.Fatalf("selected long-server option not found: %+v", menu.Options)
	}
	if len(selected.Value) > 100 || strings.Contains(selected.Value, longServer) {
		t.Fatalf("select value should be short token, got %q", selected.Value)
	}
	if got := b.resolveMCPServerToken("123456789012345678", selected.Value); got != longServer {
		t.Fatalf("resolved server = %q, want %q", got, longServer)
	}
}

func TestMCPToolSelectOptionsUseShortValues(t *testing.T) {
	longTool := "tool:" + strings.Repeat("read-large-dataset/", 8)
	allow, _ := mcpToolSelectOptions([]channel.MCPToolView{{
		MCPToolInfo: channel.MCPToolInfo{Name: longTool, Description: "long tool"},
	}})
	if len(allow) != 1 {
		t.Fatalf("allow options = %+v", allow)
	}
	if len(allow[0].Value) > 100 || strings.Contains(allow[0].Value, longTool) {
		t.Fatalf("tool select value should be short token, got %q", allow[0].Value)
	}
	if allow[0].Emoji == nil || allow[0].Emoji.Name != "⚪" {
		t.Fatalf("blocked tool option emoji = %+v, want white circle", allow[0].Emoji)
	}
	_, remove := mcpToolSelectOptions([]channel.MCPToolView{{
		MCPToolInfo: channel.MCPToolInfo{Name: "allowed-tool"},
		Allowed:     true,
	}})
	if len(remove) != 1 || remove[0].Emoji == nil || remove[0].Emoji.Name != "🟢" {
		t.Fatalf("allowed tool option = %+v, want green emoji", remove)
	}
}

func TestMCPToolPaginationBounds(t *testing.T) {
	if got := mcpToolPageCount(0); got != 1 {
		t.Fatalf("empty page count = %d, want 1", got)
	}
	if got := mcpToolPageCount(26); got != 2 {
		t.Fatalf("page count = %d, want 2", got)
	}
	start, end := mcpToolPageBounds(53, 2)
	if start != 50 || end != 53 {
		t.Fatalf("page 3 bounds = %d/%d, want 50/53", start, end)
	}
	start, end = mcpToolPageBounds(53, 99)
	if start != 50 || end != 53 {
		t.Fatalf("overflow page bounds = %d/%d, want 50/53", start, end)
	}
	allowed, blocked := mcpToolCounts([]channel.MCPToolView{{Allowed: true}, {}, {Allowed: true}})
	if allowed != 2 || blocked != 1 {
		t.Fatalf("tool counts = %d/%d, want 2/1", allowed, blocked)
	}
	if got := parseMCPPage("-1"); got != 0 {
		t.Fatalf("negative page = %d, want 0", got)
	}
	if got := parseMCPPage("bad"); got != 0 {
		t.Fatalf("invalid page = %d, want 0", got)
	}
}

func TestTruncateDiscordMessageContent(t *testing.T) {
	got := truncateDiscordMessageContent(strings.Repeat("x", 20), 10)
	if got != "xxxxxxx..." {
		t.Fatalf("truncated content = %q", got)
	}
}

func TestCronTruncateDoesNotSplitUTF8(t *testing.T) {
	got := truncate("以加一個中文字", 8)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated content is not valid UTF-8: %q", got)
	}
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("truncated content contains replacement rune: %q", got)
	}
	if got != "以加..." {
		t.Fatalf("truncated content = %q, want %q", got, "以加...")
	}
}

func TestBuildPromptDocumentsCronOwnerChannelScope(t *testing.T) {
	got := buildPromptThread("create cron", nil, "channel-1", "thread-1", "guild-1", "alice", "")
	if !strings.Contains(got, "For cron management tools, use channel_id as the owning parent channel ID") {
		t.Fatalf("prompt missing cron owner scope guidance:\n%s", got)
	}
	if !strings.Contains(got, "For one-time reminders, use bot_create_reminder; for recurring schedules, use bot_create_cron.") || !strings.Contains(got, "do not degrade it to tomorrow") {
		t.Fatalf("prompt missing reminder tool guidance:\n%s", got)
	}
	if !strings.Contains(got, "first use bot_list_cron, then bot_update_cron") || !strings.Contains(got, "enabled=false") || !strings.Contains(got, "deletion requires bot_delete_cron") {
		t.Fatalf("prompt missing safe cron update guidance:\n%s", got)
	}
	if !strings.Contains(got, "channel_id=channel-1 thread_id=thread-1") {
		t.Fatalf("prompt missing channel/thread context:\n%s", got)
	}
}

func TestBuildPromptSeparatesMemoryFromKnowledgeBase(t *testing.T) {
	got := buildPromptThread("更新查詢方法到知識庫", nil, "channel-1", "thread-1", "guild-1", "alice", "")
	for _, want := range []string{
		"bot_memory_add is not a knowledge base",
		"do not use it for requests phrased as 知識庫",
		"cannot write that knowledge base from Discord",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q guidance:\n%s", want, got)
		}
	}
}

func TestBuildPromptInjectsCurrentDatetimeGuidance(t *testing.T) {
	t.Setenv("CRON_TIMEZONE", "Asia/Taipei")
	got := buildPromptThread("今天星期幾？", nil, "channel-1", "thread-1", "guild-1", "alice", "")
	for _, want := range []string{
		"[Current datetime]",
		"timezone=Asia/Taipei",
		"timezone_source=CRON_TIMEZONE",
		"weekday_zh=",
		"day_period_zh=",
		"translate the user's date phrase into structured bot_resolve_date_range fields",
		"The structured fields are language-neutral",
		"do not pass natural-language date text to the MCP tool",
		"明天 => range_type=day offset=1",
		"下個月第二週 => range_type=month_week offset=1 week_index=2",
		"過去7天 => range_type=relative_days days=7 direction=past",
		"Do not calculate weekdays, month boundaries, or relative ranges from model memory",
		"Do not inspect or mutate raw bot state files or databases",
		"Use available exposed bot MCP tools",
		"bot_a2a_task_status",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q:\n%s", want, got)
		}
	}
}

func TestBuildPromptMapsCurrentChannelLanguageToDiscordContext(t *testing.T) {
	got := buildPromptThread("search 本頻道 history", nil, "channel-1", "thread-1", "guild-1", "alice", "")
	if !strings.Contains(got, "When users say 本頻道") || !strings.Contains(got, "bot_query_channel_history") {
		t.Fatalf("prompt missing current channel history guidance:\n%s", got)
	}
	if !strings.Contains(got, "channel_id and includes child threads") || !strings.Contains(got, "本討論串/this thread means thread_id") || !strings.Contains(got, "offset=next_offset until has_more=false") {
		t.Fatalf("prompt missing channel/thread history scope guidance:\n%s", got)
	}
}

func TestBuildPromptIncludesRequesterDiscordIDWhenAvailable(t *testing.T) {
	got := buildPromptThreadWithMentions("create reminder", nil, "channel-1", "thread-1", "guild-1", "alice", "user-1", "", nil)
	if !strings.Contains(got, "user=alice user_id=user-1") {
		t.Fatalf("prompt missing requester Discord user ID:\n%s", got)
	}
}

func TestBuildPromptIncludesAttachmentManifestMetadata(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "sample.png")
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.White)
	f, err := os.Create(imagePath)
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("encode image: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close image: %v", err)
	}

	got := buildPromptThread("process attachments", []string{imagePath}, "channel-1", "thread-1", "guild-1", "alice", "")
	for _, want := range []string{
		"[Attached files manifest]",
		`"id":"att-1"`,
		`"path":"` + imagePath + `"`,
		`"filename":"sample.png"`,
		`"mime":"image/png"`,
		`"width":2`,
		`"height":1`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %s:\n%s", want, got)
		}
	}
	lines := strings.Split(got, "\n")
	var manifestLine string
	for _, line := range lines {
		if strings.Contains(line, `"id":"att-1"`) {
			manifestLine = line
			break
		}
	}
	if manifestLine == "" {
		t.Fatalf("manifest JSON line not found:\n%s", got)
	}
	if strings.HasPrefix(manifestLine, "- ") {
		t.Fatalf("manifest line should be raw JSONL, got %q", manifestLine)
	}
	var entry attachmentManifestEntry
	if err := json.Unmarshal([]byte(manifestLine), &entry); err != nil {
		t.Fatalf("manifest line is not JSON: %v\n%s", err, manifestLine)
	}
	if entry.Path != imagePath || entry.Width != 2 || entry.Height != 1 {
		t.Fatalf("manifest entry = %+v", entry)
	}
	if !strings.Contains(got, "Do not infer image contents from filenames or metadata alone") {
		t.Fatalf("prompt missing attachment manifest handling guidance:\n%s", got)
	}
}

func TestBuildPromptDoesNotNameRawBotStateFiles(t *testing.T) {
	got := buildPromptThread("hello", nil, "channel-1", "thread-1", "guild-1", "alice", "")
	for _, forbidden := range []string{
		"data/a2a/policy.sqlite",
		"data/mcp/policy.sqlite",
		"data/audit/discord.sqlite",
		"sessions.json",
		"channel_metadata.json",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("prompt exposed raw bot state path %q:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(got, "Do not inspect or mutate raw bot state files or databases") {
		t.Fatalf("prompt missing generic raw bot state guidance:\n%s", got)
	}
}

func TestBuildPromptDocumentsStructuredMentionReferences(t *testing.T) {
	got := buildPromptThreadWithMentions("please notify Chun", nil, "channel-1", "thread-1", "guild-1", "alice", "", "", []discordmention.Ref{
		discordmention.UserRef("123", "Chun"),
	})
	if !strings.Contains(got, "[[discord:user:123]]") {
		t.Fatalf("prompt missing structured mention placeholder:\n%s", got)
	}
	if strings.Contains(got, "<@123>") {
		t.Fatalf("prompt should not expose raw mention token:\n%s", got)
	}
	if !strings.Contains(got, "Do not write raw Discord angle-bracket mention strings") {
		t.Fatalf("prompt missing raw mention guidance:\n%s", got)
	}
	if !strings.Contains(got, "discord_resolve_mentions") {
		t.Fatalf("prompt missing dynamic mention resolver guidance:\n%s", got)
	}
	if !strings.Contains(got, "Do not use discord_list_members") {
		t.Fatalf("prompt missing mention lookup bypass guidance:\n%s", got)
	}
}

func TestMentionRefsForMessageUsesStructuredDiscordMentions(t *testing.T) {
	refs := mentionRefsForMessage(&discordgo.MessageCreate{Message: &discordgo.Message{
		Author:   &discordgo.User{ID: "111", Username: "alice"},
		Mentions: []*discordgo.User{{ID: "222", Username: "bob"}, {ID: "999", Username: "bot"}},
	}}, "999")
	if len(refs) != 2 {
		t.Fatalf("refs = %+v, want author plus mentioned non-bot user", refs)
	}
	placeholders := []string{refs[0].Placeholder, refs[1].Placeholder}
	hasAuthor := false
	hasMentioned := false
	for _, placeholder := range placeholders {
		if placeholder == "[[discord:user:111]]" {
			hasAuthor = true
		}
		if placeholder == "[[discord:user:222]]" {
			hasMentioned = true
		}
	}
	if !hasAuthor || !hasMentioned {
		t.Fatalf("refs = %+v", refs)
	}
}

func TestMCPComponentIDParsesLegacyEscapedServerNames(t *testing.T) {
	id := "mcpui:apply:channel-1:vendor%3Acontext%2Fserver:full"
	parts := parseMCPComponentID(id)
	if len(parts) != 5 {
		t.Fatalf("parts = %+v", parts)
	}
	if parts[3] != "vendor:context/server" {
		t.Fatalf("server part = %q", parts[3])
	}
}

func collectMCPComponentCustomIDs(components []discordgo.MessageComponent) []string {
	var out []string
	for _, component := range components {
		row, ok := component.(discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, child := range row.Components {
			switch c := child.(type) {
			case discordgo.Button:
				out = append(out, c.CustomID)
			case discordgo.SelectMenu:
				out = append(out, c.CustomID)
			}
		}
	}
	return out
}

func TestChannelOnlySlashCommands(t *testing.T) {
	for _, name := range []string{"start", "cwd", "steering", "agent", "cron", "cron-list", "cron-run", "cron-prompt", "remind"} {
		if !isChannelOnlySlashCommand(name) {
			t.Fatalf("expected /%s to be channel-only", name)
		}
	}
	for _, name := range []string{"status", "usage", "reset", "cancel", "interrupt", "compact", "clear", "model", "models", "memory", "flashmemory", "close", "resume", "session", "a2a"} {
		if isChannelOnlySlashCommand(name) {
			t.Fatalf("did not expect /%s to be channel-only", name)
		}
	}
}

func TestSessionSlashResponseIsPrivate(t *testing.T) {
	if got := commandResponseVisibility("session", "list"); got != commandVisibilityPrivate {
		t.Fatalf("/session visibility = %v, want private", got)
	}
	if got := commandInteractionFlags(commandResponseVisibility("session", "list")); got != discordgo.MessageFlagsEphemeral {
		t.Fatalf("/session flags = %v, want ephemeral", got)
	}
}

func TestFormatSessionListUsesDiscordMentions(t *testing.T) {
	L.Load("en")
	got := formatSessionList([]channel.SessionView{{
		TargetType:      "thread",
		TargetID:        "150000000000000001",
		ParentChannelID: "150000000000000002",
		SessionID:       "019f8779-a3a8-7000-920c-425bf0ddbf91",
		Engine:          "omp",
		Model:           "gpt-5",
		CWD:             "/project",
	}})
	if !strings.Contains(got, "<#150000000000000001>") || !strings.Contains(got, "<#150000000000000002>") {
		t.Fatalf("session list should include Discord mentions, got:\n%s", got)
	}
	if strings.Contains(got, "`150000000000000001`") {
		t.Fatalf("session list should not hide target mention inside code formatting, got:\n%s", got)
	}
}

func TestChannelOnlyCommandRejectsThreadContext(t *testing.T) {
	L.Load("en")
	var replies []string
	ctx := cmdCtx{
		channelID: "channel-1",
		targetID:  "thread-1",
		inThread:  true,
		reply:     func(msg string) { replies = append(replies, msg) },
	}

	(&Bot{}).cmdCwd(ctx)

	if len(replies) != 1 || replies[0] != L.Get("error.channel_only") {
		t.Fatalf("replies = %#v, want channel-only error", replies)
	}
}

func TestRecordAgentCommandUsageWritesLedger(t *testing.T) {
	dir := t.TempDir()
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dir, UsageTimezone: "UTC"})
	b := &Bot{manager: manager}

	b.recordAgentCommandUsage(cmdCtx{
		channelID:     "channel-1",
		guildID:       "guild-1",
		userID:        "user-1",
		username:      "mxp",
		interactionID: "interaction-1",
	}, "/compact", channel.AgentCommandResult{
		Model:    "model-1",
		Executed: true,
		Metrics: acp.TurnMetrics{
			MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
			TurnDurationMs: 5000,
			ContextUsage:   11,
		},
	}, "error")

	report, err := manager.UsageReport("guild-1", "channel-1", "", 10)
	if err != nil {
		t.Fatalf("usage report: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(report.Rows))
	}
	if report.Rows[0].DayTurns != 1 || math.Abs(report.Rows[0].DayCredits-0.22) > 0.000001 {
		t.Fatalf("usage row = %+v, want one 0.22 credit turn", report.Rows[0])
	}
	records, err := manager.UsageReport("guild-1", "channel-1", "user-1", 10)
	if err != nil {
		t.Fatalf("usage report by user: %v", err)
	}
	if len(records.Rows) != 1 {
		t.Fatalf("filtered usage rows = %d, want 1", len(records.Rows))
	}
	history, err := manager.UsageHistory(channel.UsageHistoryOptions{GuildID: "guild-1", UserID: "user-1", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("usage history: %v", err)
	}
	if len(history.Records) != 1 {
		t.Fatalf("history records = %d, want 1", len(history.Records))
	}
	got := history.Records[0]
	if got.MessageID != "" {
		t.Fatalf("message id = %q, want empty", got.MessageID)
	}
	if got.InteractionID != "interaction-1" || got.InvocationID != "interaction-1" {
		t.Fatalf("interaction/invocation = %q/%q", got.InteractionID, got.InvocationID)
	}
}

func TestCmdUsageReturnsEveryUserAcrossDiscordSizedParts(t *testing.T) {
	manager := channel.NewManager(channel.ManagerConfig{UsageTimezone: "UTC"})
	defer manager.StopAll()
	for n := 0; n < 80; n++ {
		if err := manager.RecordUsage(channel.UsageRecord{Timestamp: time.Now().UTC().Format(time.RFC3339), GuildID: "guild", ChannelID: "channel", UserID: fmt.Sprintf("%018d", n+1), Username: fmt.Sprintf("person-with-a-long-name-%03d", n), MeteringUsage: []acp.MeteringItem{{Value: 1, Unit: "credits"}}}); err != nil {
			t.Fatal(err)
		}
	}
	var replies []string
	b := &Bot{manager: manager}
	b.cmdUsage(cmdCtx{guildID: "guild", channelID: "channel", userID: "1", reply: func(msg string) { replies = append(replies, msg) }, replyWithMetadata: func(msg string, _ map[string]any) { replies = append(replies, msg) }})
	if len(replies) < 2 {
		t.Fatalf("reply parts=%d, want multiple", len(replies))
	}
	joined := strings.Join(replies, "\n")
	for n := 0; n < 80; n++ {
		id := fmt.Sprintf("%018d", n+1)
		if !strings.Contains(joined, id) {
			t.Fatalf("missing user %s", id)
		}
	}
	for i, part := range replies {
		if len(part) > discordReplyLimit {
			t.Fatalf("part %d bytes=%d", i, len(part))
		}
	}
}

func TestRecordCommandResponseWithMetadataStoresMetrics(t *testing.T) {
	dir := t.TempDir()
	store, err := audit.Open(audit.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder := audit.NewRecorder(store, 10, nil, false)
	b := &Bot{auditRecorder: recorder}

	metadata := map[string]any{
		"credits":       0.22,
		"duration_ms":   int64(5000),
		"context_usage": 11.0,
	}
	b.recordCommandResponseWithMetadata(cmdCtx{
		channelID:     "channel-1",
		targetID:      "channel-1",
		guildID:       "guild-1",
		userID:        "user-1",
		username:      "mxp",
		messageID:     "message-1",
		interactionID: "interaction-1",
	}, "compact", "slash", "sent", "✅ compacted", metadata)
	if _, ok := metadata["content_len"]; ok {
		t.Fatal("recordCommandResponseWithMetadata mutated caller metadata")
	}
	recorder.Close()

	db, err := sql.Open("sqlite", filepath.Join(dir, "audit", "discord.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	var raw, messageID, interactionID string
	if err := db.QueryRowContext(context.Background(), `SELECT raw_json, message_id, interaction_id FROM bot_audit_events WHERE event_type='bot_command_response_sent'`).Scan(&raw, &messageID, &interactionID); err != nil {
		t.Fatalf("query bot audit event: %v", err)
	}
	if messageID != "message-1" || interactionID != "interaction-1" {
		t.Fatalf("stored message/interaction id = %q/%q, want message-1/interaction-1", messageID, interactionID)
	}
	var evt audit.BotEvent
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal raw event: %v", err)
	}
	if evt.MessageID != "message-1" || evt.InteractionID != "interaction-1" {
		t.Fatalf("raw event message/interaction id = %q/%q, want message-1/interaction-1", evt.MessageID, evt.InteractionID)
	}
	if evt.Metadata["content_len"].(float64) != float64(len("✅ compacted")) {
		t.Fatalf("metadata content_len = %#v", evt.Metadata["content_len"])
	}
	if math.Abs(evt.Metadata["credits"].(float64)-0.22) > 0.000001 {
		t.Fatalf("metadata credits = %#v, want 0.22", evt.Metadata["credits"])
	}
	if evt.Metadata["duration_ms"].(float64) != 5000 {
		t.Fatalf("metadata duration_ms = %#v, want 5000", evt.Metadata["duration_ms"])
	}
}

func TestRecordCommandCompletedStoresInvocationIDs(t *testing.T) {
	dir := t.TempDir()
	store, err := audit.Open(audit.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder := audit.NewRecorder(store, 10, nil, false)
	b := &Bot{auditRecorder: recorder}

	b.recordCommandCompleted(cmdCtx{
		channelID:     "channel-1",
		targetID:      "channel-1",
		guildID:       "guild-1",
		userID:        "user-1",
		username:      "mxp",
		messageID:     "message-1",
		interactionID: "interaction-1",
	}, "compact", "slash", "completed", "")
	recorder.Close()

	db, err := sql.Open("sqlite", filepath.Join(dir, "audit", "discord.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	var raw, messageID, interactionID string
	if err := db.QueryRowContext(context.Background(), `SELECT raw_json, message_id, interaction_id FROM bot_audit_events WHERE event_type='bot_command_completed'`).Scan(&raw, &messageID, &interactionID); err != nil {
		t.Fatalf("query bot audit event: %v", err)
	}
	if messageID != "message-1" || interactionID != "interaction-1" {
		t.Fatalf("stored message/interaction id = %q/%q, want message-1/interaction-1", messageID, interactionID)
	}
	var evt audit.BotEvent
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal raw event: %v", err)
	}
	if evt.MessageID != "message-1" || evt.InteractionID != "interaction-1" {
		t.Fatalf("raw event message/interaction id = %q/%q, want message-1/interaction-1", evt.MessageID, evt.InteractionID)
	}
}

func TestRecordCommandResponseDeliveryStoresDiscordResult(t *testing.T) {
	dir := t.TempDir()
	store, err := audit.Open(audit.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder := audit.NewRecorder(store, 10, nil, false)
	b := &Bot{auditRecorder: recorder}

	ctx := cmdCtx{
		channelID: "channel-1",
		targetID:  "channel-1",
		guildID:   "guild-1",
		userID:    "user-1",
		messageID: "invoke-message-1",
	}
	b.recordCommandResponseDelivery(ctx, "compact", "message", "sent", "ok", map[string]any{"credits": 0.22}, &discordgo.Message{
		ID:        "response-message-1",
		ChannelID: "channel-1",
	}, nil)
	b.recordCommandResponseDelivery(ctx, "compact", "message", "sent", "failed", nil, nil, fmt.Errorf("discord send failed"))
	recorder.Close()

	db, err := sql.Open("sqlite", filepath.Join(dir, "audit", "discord.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(), `SELECT raw_json FROM bot_audit_events WHERE event_type IN ('bot_command_response_sent', 'bot_command_response_failed') ORDER BY id`)
	if err != nil {
		t.Fatalf("query bot audit events: %v", err)
	}
	defer rows.Close()
	var events []audit.BotEvent
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		var evt audit.BotEvent
		if err := json.Unmarshal([]byte(raw), &evt); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		events = append(events, evt)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].Type != "bot_command_response_sent" || events[0].Status != "sent" || events[0].Metadata["response_message_id"] != "response-message-1" {
		t.Fatalf("success event = %+v, want sent with response_message_id", events[0])
	}
	if events[1].Type != "bot_command_response_failed" || events[1].Status != "error" || events[1].Error != "discord send failed" || events[1].Metadata["send_error"] != "discord send failed" {
		t.Fatalf("error event = %+v, want send error metadata", events[1])
	}
}

func TestHandleBangCommandRecordsDeliveryFailure(t *testing.T) {
	L.Load("en")
	b, dbPath, cleanup := newAuditTestBot(t)
	defer cleanup()
	if err := b.manager.SetCWD("channel-1", t.TempDir()); err != nil {
		t.Fatalf("initialize channel cwd: %v", err)
	}
	ds := newFailingDiscordSession(t)

	b.handleMessage(ds, &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "invoke-message-1",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Content:   "!thread",
		Author:    &discordgo.User{ID: "user-1", Username: "mxp"},
	}})

	evt := waitBotAuditEvent(t, dbPath, "bot_command_response_failed")
	if evt.Command != "thread" || evt.Source != "message" || evt.MessageID != "invoke-message-1" {
		t.Fatalf("event command/source/message = %q/%q/%q, want thread/message/invoke-message-1", evt.Command, evt.Source, evt.MessageID)
	}
	if evt.Error == "" || evt.Metadata["send_error"] == "" {
		t.Fatalf("event = %+v, want send error recorded", evt)
	}
}

func TestHandleSlashCommandRecordsFollowupDeliveryFailure(t *testing.T) {
	L.Load("en")
	b, dbPath, cleanup := newAuditTestBot(t)
	defer cleanup()
	ds := newFailingDiscordSession(t)

	b.handleSlashCommand(ds, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:        "interaction-1",
		Type:      discordgo.InteractionApplicationCommand,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Token:     "token-1",
		Member:    &discordgo.Member{User: &discordgo.User{ID: "user-1", Username: "mxp"}},
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "thread",
		},
	}})

	events := waitBotAuditEvents(t, dbPath, "bot_command_response_failed", 2)
	var foundFollowup bool
	for _, evt := range events {
		if evt.Command == "thread" && evt.Source == "slash" && evt.InteractionID == "interaction-1" && evt.Metadata["interaction_response_type"] == nil && evt.Metadata["content_len"].(float64) > 0 {
			foundFollowup = true
			if evt.Error == "" || evt.Metadata["send_error"] == "" {
				t.Fatalf("event = %+v, want send error recorded", evt)
			}
		}
	}
	if !foundFollowup {
		t.Fatalf("events = %+v, want failed followup command response", events)
	}
}

func TestPrivateSlashCommandRecordsEphemeralDeferredResponse(t *testing.T) {
	L.Load("en")
	b, dbPath, cleanup := newAuditTestBot(t)
	defer cleanup()
	ds := newFailingDiscordSession(t)

	b.handleSlashCommand(ds, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:        "interaction-private",
		Type:      discordgo.InteractionApplicationCommand,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Token:     "token-1",
		Member:    &discordgo.Member{User: &discordgo.User{ID: "user-1", Username: "mxp"}},
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "status",
		},
	}})

	events := waitBotAuditEvents(t, dbPath, "bot_command_response_failed", 2)
	var foundDeferred bool
	for _, evt := range events {
		if evt.Command == "status" && evt.Status == "error" && evt.Metadata["interaction_response_type"] == fmt.Sprintf("%d", discordgo.InteractionResponseDeferredChannelMessageWithSource) {
			foundDeferred = true
			if evt.Metadata["ephemeral"] != true {
				t.Fatalf("event = %+v, want ephemeral deferred response metadata", evt)
			}
		}
	}
	if !foundDeferred {
		t.Fatalf("events = %+v, want failed deferred status response", events)
	}
}

func TestDeferredSlashResponderEditsOriginalThenEphemeralFollowup(t *testing.T) {
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	interaction := &discordgo.Interaction{
		AppID: "app-1",
		Token: "token-1",
	}
	responder := newDeferredSlashResponder(ds, interaction, discordgo.MessageFlagsEphemeral)

	if _, err := responder.Send("first private part"); err != nil {
		t.Fatalf("send first part: %v", err)
	}
	if _, err := responder.Send("second private part"); err != nil {
		t.Fatalf("send second part: %v", err)
	}

	paths, bodies := rt.Snapshot()
	if len(paths) != 2 {
		t.Fatalf("requests = %v, want 2", paths)
	}
	if !strings.Contains(paths[0], "/webhooks/app-1/token-1/messages/@original") || !strings.HasPrefix(paths[0], "PATCH ") {
		t.Fatalf("first request = %q, want edit original response", paths[0])
	}
	if strings.Contains(bodies[0], `"flags"`) {
		t.Fatalf("original response edit body should not set flags: %s", bodies[0])
	}
	if !strings.Contains(bodies[0], "first private part") {
		t.Fatalf("first body = %s", bodies[0])
	}
	if !strings.Contains(paths[1], "/webhooks/app-1/token-1") || !strings.HasPrefix(paths[1], "POST ") {
		t.Fatalf("second request = %q, want followup create", paths[1])
	}
	if !strings.Contains(bodies[1], `"flags":64`) {
		t.Fatalf("second followup body should be ephemeral: %s", bodies[1])
	}
	if !strings.Contains(bodies[1], "second private part") {
		t.Fatalf("second body = %s", bodies[1])
	}
}

func TestHandleSlashCommandRecordsInitialRejectionDeliveryFailure(t *testing.T) {
	L.Load("en")
	b, dbPath, cleanup := newAuditTestBot(t)
	defer cleanup()
	ds := newFailingDiscordSession(t)
	ds.State = testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{{
		ID:   "bot-1",
		Type: discordgo.PermissionOverwriteTypeMember,
		Deny: int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages),
	}}).State

	b.handleSlashCommand(ds, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:        "interaction-early-reject",
		Type:      discordgo.InteractionApplicationCommand,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Token:     "token-1",
		Member:    &discordgo.Member{User: &discordgo.User{ID: "user-1", Username: "mxp"}},
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "thread",
		},
	}})

	evt := waitBotAuditEvent(t, dbPath, "bot_command_response_failed")
	if evt.Command != "thread" || evt.Source != "slash" || evt.InteractionID != "interaction-early-reject" {
		t.Fatalf("event command/source/interaction = %q/%q/%q, want thread/slash/interaction-early-reject", evt.Command, evt.Source, evt.InteractionID)
	}
	if evt.Status != "error" || evt.Metadata["ephemeral"] != true || evt.Metadata["interaction_response_type"] == "" {
		t.Fatalf("event = %+v, want failed ephemeral initial interaction response metadata", evt)
	}
}

func TestUsagePermissionDenialRecordsRejectedCompletion(t *testing.T) {
	L.Load("en")
	b, dbPath, cleanup := newAuditTestBot(t)
	defer cleanup()
	ds := newFailingDiscordSession(t)
	ds.State = testPeerPermissionSession(t, nil).State

	b.handleSlashCommand(ds, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:        "interaction-usage-denied",
		Type:      discordgo.InteractionApplicationCommand,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Token:     "token-1",
		Member:    &discordgo.Member{User: &discordgo.User{ID: "viewer", Username: "viewer"}},
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "usage",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{{
				Name: "user", Type: discordgo.ApplicationCommandOptionUser, Value: "bot-2",
			}},
			Resolved: &discordgo.ApplicationCommandInteractionDataResolved{
				Users: map[string]*discordgo.User{"bot-2": {ID: "bot-2"}},
			},
		},
	}})

	evt := waitBotAuditEvent(t, dbPath, "bot_command_completed")
	if evt.Command != "usage" || evt.InteractionID != "interaction-usage-denied" || evt.Status != "rejected" || evt.Error != "usage_report_forbidden" {
		t.Fatalf("event = %+v, want rejected usage completion", evt)
	}
}

func TestUsageHistoryClassifiesDenialAndDeliveryFailure(t *testing.T) {
	L.Load("en")
	b, _, cleanup := newAuditTestBot(t)
	defer cleanup()
	ds := newFailingDiscordSession(t)
	ds.State = testPeerPermissionSession(t, nil).State
	interaction := func(id string, target string) *discordgo.InteractionCreate {
		data := discordgo.ApplicationCommandInteractionData{Name: "usage-history"}
		if target != "" {
			data.Options = []*discordgo.ApplicationCommandInteractionDataOption{{Name: "user", Type: discordgo.ApplicationCommandOptionUser, Value: target}}
			data.Resolved = &discordgo.ApplicationCommandInteractionDataResolved{Users: map[string]*discordgo.User{target: {ID: target}}}
		}
		return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
			ID: id, Type: discordgo.InteractionApplicationCommand, GuildID: "guild-1", ChannelID: "channel-1", Token: "token-1",
			Member: &discordgo.Member{User: &discordgo.User{ID: "viewer", Username: "viewer"}}, Data: data,
		}}
	}
	auditCtx := cmdCtx{guildID: "guild-1", channelID: "channel-1", targetID: "channel-1", userID: "viewer"}

	if status, reason := b.handleUsageHistory(ds, interaction("history-denied", "bot-2"), auditCtx); status != "rejected" || reason != "usage_history_forbidden" {
		t.Fatalf("denied status/reason = %q/%q", status, reason)
	}
	if status, reason := b.handleUsageHistory(ds, interaction("history-delivery-failed", ""), auditCtx); status != "error" || reason != "usage_history_deferred_response_failed" {
		t.Fatalf("delivery status/reason = %q/%q", status, reason)
	}
}

func TestHandleSlashCronModalRecordsDeliveryFailure(t *testing.T) {
	L.Load("en")
	b, dbPath, cleanup := newAuditTestBot(t)
	defer cleanup()
	if err := b.manager.SetCWD("channel-1", t.TempDir()); err != nil {
		t.Fatalf("initialize channel cwd: %v", err)
	}
	ds := newFailingDiscordSession(t)

	b.handleSlashCommand(ds, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:        "interaction-cron",
		Type:      discordgo.InteractionApplicationCommand,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Token:     "token-1",
		Member:    &discordgo.Member{User: &discordgo.User{ID: "user-1", Username: "mxp"}},
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "cron",
		},
	}})

	evt := waitBotAuditEvent(t, dbPath, "bot_command_response_failed")
	if evt.Command != "cron" || evt.Source != "slash" || evt.InteractionID != "interaction-cron" {
		t.Fatalf("event command/source/interaction = %q/%q/%q, want cron/slash/interaction-cron", evt.Command, evt.Source, evt.InteractionID)
	}
	if evt.Metadata["modal_custom_id"] != "cron_add_modal" || evt.Metadata["interaction_response_type"] == "" {
		t.Fatalf("event = %+v, want modal delivery metadata", evt)
	}
}

func TestHandleSlashCronRequiresInitializedChannel(t *testing.T) {
	L.Load("en")
	b, dbPath, cleanup := newAuditTestBot(t)
	defer cleanup()
	ds := newFailingDiscordSession(t)

	b.handleSlashCommand(ds, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:        "interaction-cron-uninitialized",
		Type:      discordgo.InteractionApplicationCommand,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Token:     "token-1",
		Member:    &discordgo.Member{User: &discordgo.User{ID: "user-1", Username: "mxp"}},
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "cron",
		},
	}})

	evt := waitBotAuditEvent(t, dbPath, "bot_command_response_failed")
	if evt.Command != "cron" || evt.Metadata["rejected_reason"] != "channel_uninitialized" {
		t.Fatalf("event = %+v, want uninitialized cron rejection", evt)
	}
}

func TestAgentCommandUsageIDPrefersInteractionThenMessage(t *testing.T) {
	if got := agentCommandUsageID(cmdCtx{messageID: "msg-1", interactionID: "interaction-1"}); got != "interaction-1" {
		t.Fatalf("usage id = %q, want interaction id", got)
	}
	if got := agentCommandUsageID(cmdCtx{messageID: "msg-1"}); got != "msg-1" {
		t.Fatalf("usage id = %q, want message id", got)
	}
}

func TestAgentCommandErrorAppendsMetricsFooter(t *testing.T) {
	L.Load("en")
	msg := agentCommandError(fmt.Errorf("agent failed"), channel.AgentCommandResult{
		Executed: true,
		Metrics: acp.TurnMetrics{
			MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
			TurnDurationMs: 5000,
			ContextUsage:   11,
		},
	})
	if !strings.Contains(msg, "agent failed") || !strings.Contains(msg, "⚡ 0.22 credit · 5.0s · ctx 11%") {
		t.Fatalf("agent command error = %q, want error with metrics footer", msg)
	}
}

func TestAgentCommandMetadataIncludesStatus(t *testing.T) {
	metadata := agentCommandMetadata(channel.AgentCommandResult{Executed: true}, "error")
	if metadata["agent_status"] != "error" {
		t.Fatalf("agent_status = %#v, want error", metadata["agent_status"])
	}
	if metadata["agent_executed"] != true {
		t.Fatalf("agent_executed = %#v, want true", metadata["agent_executed"])
	}
}
