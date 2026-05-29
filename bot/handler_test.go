package bot

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	L "github.com/nczz/kiro-discord-bot/locale"
)

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

func botRoleAllowOverwrite(roleID string) *discordgo.PermissionOverwrite {
	return &discordgo.PermissionOverwrite{
		ID:    roleID,
		Type:  discordgo.PermissionOverwriteTypeRole,
		Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionSendMessagesInThreads,
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

func TestSlashCommandsIncludeAgentAndUsage(t *testing.T) {
	foundAgent := false
	foundUsage := false
	foundInterrupt := false
	for _, cmd := range buildSlashCommands() {
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
	if !foundAgent || !foundUsage || !foundInterrupt {
		t.Fatal("expected /agent, /usage, and /interrupt slash commands to be registered")
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
