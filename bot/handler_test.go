package bot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
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
}
