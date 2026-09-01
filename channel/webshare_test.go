package channel

import (
	"testing"

	"github.com/nczz/kiro-discord-bot/internal/discordmention"
)

func TestWebShareChannelJobUsesSourceAndVisibleMessageID(t *testing.T) {
	job, err := newWebShareChannelJob(WebSharePrompt{GuildID: "guild-1", ChannelID: "channel-1", ThreadID: "thread-1", MessageID: "message-1", Prompt: "hello", UserID: "user-1", Username: "Alice", Attachments: []string{"/tmp/a.txt"}, MentionRefs: []discordmention.Ref{discordmention.UserRef("123456789012345678", "Bob")}, DeliveryMode: DeliveryInline})
	if err != nil {
		t.Fatal(err)
	}
	if job.Source != WebShareSource {
		t.Fatalf("Source = %q, want %q", job.Source, WebShareSource)
	}
	if job.MessageID != "message-1" {
		t.Fatalf("MessageID = %q, want visible prompt record", job.MessageID)
	}
	if job.BotToolsTargetID != "thread-1" || job.DeliveryMode != DeliveryInline {
		t.Fatalf("target/delivery = %q/%q", job.BotToolsTargetID, job.DeliveryMode)
	}
	job.Attachments[0] = "changed"
	if job.Attachments[0] == "/tmp/a.txt" {
		t.Fatal("test mutation did not apply")
	}
}

func TestWebShareThreadJobValidatesTargetAndPreservesOwnership(t *testing.T) {
	if _, _, err := newWebShareThreadJob(WebSharePrompt{ParentChannelID: "parent"}); err == nil {
		t.Fatal("missing thread id should fail")
	}
	job, parentID, err := newWebShareThreadJob(WebSharePrompt{GuildID: "guild-1", ParentChannelID: "parent", ThreadID: "thread", MessageID: "message-1", Prompt: "hello", UserID: "user", Username: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if parentID != "parent" || job.ParentChannelID != "parent" || job.ChannelID != "thread" || job.ThreadID != "thread" {
		t.Fatalf("bad thread ownership: parent=%q job=%+v", parentID, job)
	}
	if job.Source != WebShareSource || job.MessageID != "message-1" {
		t.Fatalf("source/message = %q/%q", job.Source, job.MessageID)
	}
}
