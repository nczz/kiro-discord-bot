package bot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
)

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

	if !b.requiresHumanMention("channel-1", "", "bot-1") {
		t.Fatal("multi-bot channel should require mention by default")
	}

	b.manager.Back("channel-1")
	if b.requiresHumanMention("channel-1", "", "bot-1") {
		t.Fatal("/back should open full-listen mode for the target channel")
	}

	b.manager.Pause("channel-1")
	if !b.requiresHumanMention("channel-1", "", "bot-1") {
		t.Fatal("/pause should restore mention-only mode")
	}
}

func TestThreadMentionModeInheritsParentBack(t *testing.T) {
	b := &Bot{
		peers:   parseBotPeers("M5Bot:bot-1,ChunBot:bot-2"),
		manager: channel.NewManager(channel.ManagerConfig{}),
	}

	if !b.requiresHumanMention("thread-1", "channel-1", "bot-1") {
		t.Fatal("thread should require mention by default in multi-bot mode")
	}

	b.manager.Back("channel-1")
	if b.requiresHumanMention("thread-1", "channel-1", "bot-1") {
		t.Fatal("thread should inherit parent /back full-listen override")
	}

	b.manager.Pause("thread-1")
	if !b.requiresHumanMention("thread-1", "channel-1", "bot-1") {
		t.Fatal("thread /pause should override parent /back")
	}

	b.manager.Back("thread-1")
	if b.requiresHumanMention("thread-1", "channel-1", "bot-1") {
		t.Fatal("thread /back should restore full-listen override")
	}
}

func TestSlashCommandsIncludeAgent(t *testing.T) {
	for _, cmd := range buildSlashCommands() {
		if cmd.Name != "agent" {
			continue
		}
		if len(cmd.Options) != 1 || cmd.Options[0].Name != "mode" {
			t.Fatalf("/agent options = %+v, want optional mode", cmd.Options)
		}
		return
	}
	t.Fatal("expected /agent slash command to be registered")
}
