package channel

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestFormatDiscordThreadContextForHandoff(t *testing.T) {
	messages := []*discordgo.Message{
		{
			ID:      "current",
			Content: "<@222> please take over",
			Author:  &discordgo.User{Username: "BuildBot", Bot: true},
		},
		{
			ID:      "result",
			Content: "Completed the first pass. Remaining: run deploy verification.",
			Author:  &discordgo.User{Username: "ReviewBot", Bot: true},
		},
		{
			ID:      "file",
			Content: "The relevant file is channel/manager.go",
			Author:  &discordgo.User{Username: "Alice"},
			Attachments: []*discordgo.MessageAttachment{
				{Filename: "trace.log"},
			},
		},
	}

	got := formatDiscordThreadContext("[Cross-bot handoff context]", messages, "current", 100, 3000, 120000)

	if !strings.Contains(got, "[Cross-bot handoff context]") {
		t.Fatalf("missing handoff header:\n%s", got)
	}
	if strings.Contains(got, "please take over") {
		t.Fatalf("current trigger message should not be duplicated:\n%s", got)
	}
	if !strings.Contains(got, "[Alice] The relevant file is channel/manager.go") {
		t.Fatalf("missing human context:\n%s", got)
	}
	if !strings.Contains(got, "[attachments] trace.log") {
		t.Fatalf("missing attachment context:\n%s", got)
	}
	if !strings.Contains(got, "[ReviewBot (bot)] Completed the first pass") {
		t.Fatalf("missing peer bot result context:\n%s", got)
	}
}
