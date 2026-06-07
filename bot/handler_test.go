package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/channel"
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

func newAuditTestBot(t *testing.T) (*Bot, string, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := audit.Open(audit.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder := audit.NewRecorder(store, 100, nil, false)
	b := &Bot{
		manager:       channel.NewManager(channel.ManagerConfig{DataDir: filepath.Join(dir, "data")}),
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
	ds := &discordgo.Session{State: discordgo.NewState()}
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
		Content:  "@M5Bot review this",
		Mentions: []*discordgo.User{{ID: "self"}},
	}}
	if !messageMentionsUser(msg, msg.Content, "self") {
		t.Fatal("expected structured Discord mention to match even without token text")
	}
}

func TestMentionsOtherPeer(t *testing.T) {
	b := &Bot{peers: parseBotPeers("M5Bot:bot-1:role-1,ChunBot:bot-2:role-2")}

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
		Content:  "@ChunBot handle this",
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
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, nil)

	if !b.requiresHumanMention(ds, "channel-1", "", "bot-1") {
		t.Fatal("effective multi-bot channel should require mention by default")
	}

	b.manager.Back("channel-1")
	if b.requiresHumanMention(ds, "channel-1", "", "bot-1") {
		t.Fatal("/back should open full-listen mode for the target channel")
	}

	b.manager.Pause("channel-1")
	if !b.requiresHumanMention(ds, "channel-1", "", "bot-1") {
		t.Fatal("/pause should restore mention-only mode")
	}
}

func TestThreadMentionModeInheritsParentBack(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, nil)

	if !b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1") {
		t.Fatal("thread should require mention by default when peer bot has effective thread access")
	}

	b.manager.Back("channel-1")
	if b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1") {
		t.Fatal("thread should inherit parent /back full-listen override")
	}

	b.manager.Pause("thread-1")
	if !b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1") {
		t.Fatal("thread /pause should override parent /back")
	}

	b.manager.Back("thread-1")
	if b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1") {
		t.Fatal("thread /back should restore full-listen override")
	}
}

func TestThreadListenSnapshotOutlivesParentThreadModeChange(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, nil)
	b.manager.SetThreadListenMode("thread-1", false)
	b.manager.SetThreadMode("channel-1", false)

	if b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1") {
		t.Fatal("full-listen thread snapshot should not become mention-only when parent thread mode is turned off")
	}
}

func TestUnknownThreadUsesParentThreadModeOffMentionOnly(t *testing.T) {
	b := &Bot{manager: channel.NewManager(channel.ManagerConfig{})}
	ds := testPeerPermissionSession(t, nil)
	b.manager.SetThreadMode("channel-1", false)

	if !b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1") {
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
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberDenyOverwrite("bot-2")})
	ch2, err := ds.State.Channel("channel-2")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}

	if b.requiresHumanMention(ds, "channel-1", "", "bot-1") {
		t.Fatal("peer without effective channel access should not force mention-only")
	}
	if !b.requiresHumanMention(ds, "channel-2", "", "bot-1") {
		t.Fatal("peer with inherited effective channel access should force mention-only")
	}
	ch2.PermissionOverwrites = []*discordgo.PermissionOverwrite{botMemberAllowOverwrite("bot-2")}
	if !b.requiresHumanMention(ds, "channel-2", "", "bot-1") {
		t.Fatal("peer with explicit channel allow should force mention-only")
	}
}

func TestPeerExplicitViewOverwriteForcesMentionOnlyWhenEffectiveSendAllows(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberViewOverwrite("bot-2")})

	if !b.requiresHumanMention(ds, "channel-1", "", "bot-1") {
		t.Fatal("peer with explicit channel view allow and effective send permission should force mention-only")
	}
}

func TestPeerThreadReplyPermissionsForceMentionOnlyWithoutChannelSend(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberThreadReplyOverwrite("bot-2")})

	if !b.requiresHumanMention(ds, "channel-1", "", "bot-1") {
		t.Fatal("peer that can create and reply in threads should force mention-only even without channel SendMessages")
	}
}

func TestPeerThreadPermissionsForceMentionOnlyInThreadWithoutParentChannelSend(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberThreadReplyOverwrite("bot-2")})

	if !b.requiresHumanMention(ds, "thread-1", "channel-1", "bot-1") {
		t.Fatal("peer that can reply in the thread should force mention-only even without parent channel SendMessages")
	}
}

func TestRoleOnlyPeerRequiresExplicitChannelAllow(t *testing.T) {
	b := &Bot{
		peers: []BotPeer{
			{Name: "M5Bot", ID: "bot-1", RoleID: "role-1"},
			{Name: "PeerRole", RoleID: "role-2", Manual: true},
		},
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, nil)

	if b.requiresHumanMention(ds, "channel-1", "", "bot-1") {
		t.Fatal("role-only peer without explicit channel allow should not force mention-only")
	}
	ch, err := ds.State.Channel("channel-1")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	ch.PermissionOverwrites = []*discordgo.PermissionOverwrite{botRoleAllowOverwrite("role-2")}
	if !b.requiresHumanMention(ds, "channel-1", "", "bot-1") {
		t.Fatal("manual role-only peer with explicit channel allow should force mention-only")
	}
}

func TestDiscoveredRoleOnlyPeerDoesNotForceMentionOnly(t *testing.T) {
	b := &Bot{
		peers: []BotPeer{
			{Name: "M5Bot", ID: "bot-1", RoleID: "role-1"},
			{Name: "DiscoveredRole", RoleID: "role-2"},
		},
		manager: channel.NewManager(channel.ManagerConfig{}),
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botRoleAllowOverwrite("role-2")})

	if b.requiresHumanMention(ds, "channel-1", "", "bot-1") {
		t.Fatal("auto-discovered role-only peer should not force mention-only")
	}
}

func TestDoctorBotPeersExplainsChannelTrigger(t *testing.T) {
	L.Load("en")
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberAllowOverwrite("bot-2")})
	b := &Bot{
		discord: ds,
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}

	got := b.doctorBotPeers("channel-1")
	if !strings.Contains(got, "trigger: `ChunBot` (`bot-2`) via member overwrite") {
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
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}

	got := b.doctorBotPeers("channel-1")
	if !strings.Contains(got, "trigger: `ChunBot` (`bot-2`) via effective permissions") {
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
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
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

func TestSlashCommandsIncludeAgentAndUsage(t *testing.T) {
	foundAgent := false
	foundUsage := false
	foundInterrupt := false
	foundThread := false
	for _, cmd := range buildSlashCommands() {
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
		if cmd.Name != "agent" {
			continue
		}
		foundAgent = true
		if len(cmd.Options) != 1 || cmd.Options[0].Name != "mode" {
			t.Fatalf("/agent options = %+v, want optional mode", cmd.Options)
		}
	}
	if !foundAgent || !foundUsage || !foundInterrupt || !foundThread {
		t.Fatal("expected /agent, /usage, /interrupt, and /thread slash commands to be registered")
	}
}

func TestChannelOnlySlashCommands(t *testing.T) {
	for _, name := range []string{"start", "cwd", "agent", "resume", "cron", "cron-list", "cron-run", "cron-prompt", "remind"} {
		if !isChannelOnlySlashCommand(name) {
			t.Fatalf("expected /%s to be channel-only", name)
		}
	}
	for _, name := range []string{"status", "usage", "reset", "cancel", "interrupt", "compact", "clear", "model", "models", "memory", "flashmemory", "close"} {
		if isChannelOnlySlashCommand(name) {
			t.Fatalf("did not expect /%s to be channel-only", name)
		}
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
	files, err := filepath.Glob(filepath.Join(dir, "usage", "*.jsonl"))
	if err != nil {
		t.Fatalf("glob usage files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("usage files = %v, want one file", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read usage file: %v", err)
	}
	if strings.Contains(string(data), `"message_id":"interaction-1"`) {
		t.Fatalf("usage record = %s, interaction id should not be stored as message_id", data)
	}
	if !strings.Contains(string(data), `"interaction_id":"interaction-1"`) || !strings.Contains(string(data), `"invocation_id":"interaction-1"`) {
		t.Fatalf("usage record = %s, want interaction_id and invocation_id", data)
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

func TestHandleSlashCronModalRecordsDeliveryFailure(t *testing.T) {
	L.Load("en")
	b, dbPath, cleanup := newAuditTestBot(t)
	defer cleanup()
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
