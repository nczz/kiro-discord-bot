package bot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestDiscordDiscussionContextIncludesReplyFocusAttachment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image-bytes"))
	}))
	defer srv.Close()

	project := t.TempDir()
	b := &Bot{downloadClient: srv.Client(), attachmentMaxBytes: 1024, discussionContext: newDiscordDiscussionContextCache()}
	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "trigger-1",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Content:   "<@bot-1> what is in this attachment?",
		Author:    &discordgo.User{ID: "user-1", Username: "Bob"},
		ReferencedMessage: &discordgo.Message{
			ID:        "source-1",
			ChannelID: "channel-1",
			GuildID:   "guild-1",
			Content:   "Here is the screenshot from production",
			Timestamp: time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC),
			Author:    &discordgo.User{ID: "user-2", Username: "Alice"},
			Attachments: []*discordgo.MessageAttachment{{
				ID:          "att-1",
				Filename:    "screen.png",
				URL:         srv.URL + "/screen.png",
				ContentType: "image/png",
				Size:        11,
			}},
		},
	}}

	got := b.buildDiscordDiscussionContext(nil, msg, discordDiscussionContextOptions{TargetID: "channel-1", ProjectCWD: project, CurrentContent: "what is in this attachment?", SelfID: "bot-1", SessionKey: "session-1"})
	if !strings.Contains(got.Block, "Discord discussion context delta") || !strings.Contains(got.Block, "untrusted context") {
		t.Fatalf("context block missing safety header:\n%s", got.Block)
	}
	for _, want := range []string{"replied_to_message", "Here is the screenshot", "screen.png", "local_path"} {
		if !strings.Contains(got.Block, want) {
			t.Fatalf("context block missing %q:\n%s", want, got.Block)
		}
	}
	if len(got.Attachments) != 1 || !strings.Contains(got.Attachments[0], "screen.png") {
		t.Fatalf("context attachments = %+v, want downloaded source attachment", got.Attachments)
	}
	if len(got.MentionRefs) != 1 || got.MentionRefs[0].ID != "user-2" {
		t.Fatalf("mention refs = %+v, want referenced author", got.MentionRefs)
	}
	b.markDiscordDiscussionContextDelivered(got)
}

func TestDiscordDiscussionContextUsesSessionAwareDelta(t *testing.T) {
	project := t.TempDir()
	b := &Bot{discussionContext: newDiscordDiscussionContextCache()}
	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "trigger-1",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Content:   "<@bot-1> continue",
		Author:    &discordgo.User{ID: "user-1", Username: "Bob"},
		ReferencedMessage: &discordgo.Message{
			ID:        "source-1",
			ChannelID: "channel-1",
			GuildID:   "guild-1",
			Content:   "Original decision: deploy after review",
			Author:    &discordgo.User{ID: "user-2", Username: "Alice"},
		},
	}}

	first := b.buildDiscordDiscussionContext(nil, msg, discordDiscussionContextOptions{TargetID: "channel-1", ProjectCWD: project, CurrentContent: "continue", SelfID: "bot-1", SessionKey: "session-1"})
	if !strings.Contains(first.Block, "Original decision") {
		t.Fatalf("first context should include full focus message:\n%s", first.Block)
	}
	uncommitted := b.buildDiscordDiscussionContext(nil, msg, discordDiscussionContextOptions{TargetID: "channel-1", ProjectCWD: project, CurrentContent: "continue", SelfID: "bot-1", SessionKey: "session-1"})
	if !strings.Contains(uncommitted.Block, "Original decision") {
		t.Fatalf("building context before delivery should not advance watermark:\n%s", uncommitted.Block)
	}
	b.markDiscordDiscussionContextDelivered(first)
	second := b.buildDiscordDiscussionContext(nil, msg, discordDiscussionContextOptions{TargetID: "channel-1", ProjectCWD: project, CurrentContent: "continue", SelfID: "bot-1", SessionKey: "session-1"})
	if strings.Contains(second.Block, "Original decision") || !strings.Contains(second.Block, `"seen_pointer":true`) {
		t.Fatalf("second same-session context should use pointer, got:\n%s", second.Block)
	}
	third := b.buildDiscordDiscussionContext(nil, msg, discordDiscussionContextOptions{TargetID: "channel-1", ProjectCWD: project, CurrentContent: "continue", SelfID: "bot-1", SessionKey: "session-2"})
	if !strings.Contains(third.Block, "Original decision") {
		t.Fatalf("new session key should reset context watermark:\n%s", third.Block)
	}
}

func TestDiscordDiscussionContextRetainsPendingSessionDelta(t *testing.T) {
	project := t.TempDir()
	b := &Bot{discussionContext: newDiscordDiscussionContextCache()}
	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "trigger-1",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Content:   "<@bot-1> continue",
		Author:    &discordgo.User{ID: "user-1", Username: "Bob"},
		ReferencedMessage: &discordgo.Message{
			ID:        "source-1",
			ChannelID: "channel-1",
			GuildID:   "guild-1",
			Content:   "Pending-session context should not replay",
			Author:    &discordgo.User{ID: "user-2", Username: "Alice"},
		},
	}}

	first := b.buildDiscordDiscussionContext(nil, msg, discordDiscussionContextOptions{TargetID: "channel-1", ProjectCWD: project, CurrentContent: "continue", SelfID: "bot-1", SessionKey: "channel|channel-1|||"})
	if !strings.Contains(first.Block, "Pending-session context") {
		t.Fatalf("pending session should include first context:\n%s", first.Block)
	}
	b.markDiscordDiscussionContextDelivered(first)
	second := b.buildDiscordDiscussionContext(nil, msg, discordDiscussionContextOptions{TargetID: "channel-1", ProjectCWD: project, CurrentContent: "continue", SelfID: "bot-1", SessionKey: "channel|channel-1|omp|ch-channel-1|session-1"})
	if strings.Contains(second.Block, "Pending-session context") || !strings.Contains(second.Block, `"seen_pointer":true`) {
		t.Fatalf("newly established session should inherit pending watermark, got:\n%s", second.Block)
	}
}
