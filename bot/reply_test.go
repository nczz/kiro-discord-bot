package bot

import (
	"strings"
	"testing"
)

func TestSplitDiscordMessageShort(t *testing.T) {
	parts := splitDiscordMessage("hello", 10)
	if len(parts) != 1 || parts[0] != "hello" {
		t.Fatalf("parts = %#v", parts)
	}
}

func TestSplitDiscordMessagePrefersNewline(t *testing.T) {
	parts := splitDiscordMessage("alpha\nbeta\ngamma", 12)
	if len(parts) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(parts), parts)
	}
	if parts[0] != "alpha\nbeta" {
		t.Fatalf("first part = %q", parts[0])
	}
}

func TestSplitDiscordMessageKeepsLimit(t *testing.T) {
	msg := strings.Repeat("x", 25)
	parts := splitDiscordMessage(msg, 10)
	if len(parts) != 3 {
		t.Fatalf("len = %d, want 3: %#v", len(parts), parts)
	}
	for _, part := range parts {
		if len(part) > 10 {
			t.Fatalf("part exceeds limit: %q", part)
		}
	}
}

func TestReplyLongSendsAllParts(t *testing.T) {
	var got []string
	replyLong(func(msg string) { got = append(got, msg) }, strings.Repeat("x", discordReplyLimit+10))
	if len(got) != 2 {
		t.Fatalf("got %d replies, want 2", len(got))
	}
}
