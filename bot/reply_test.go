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

func TestReplyLongWithMetadataSendsMetadataForEveryPart(t *testing.T) {
	var plain []string
	var withMeta []string
	var metadatas []map[string]any
	ctx := cmdCtx{
		reply: func(msg string) {
			plain = append(plain, msg)
		},
		replyWithMetadata: func(msg string, metadata map[string]any) {
			withMeta = append(withMeta, msg)
			metadatas = append(metadatas, metadata)
		},
	}
	metadata := map[string]any{"credits": 0.22}
	replyLongWithMetadata(ctx, strings.Repeat("x", discordReplyLimit+10), metadata)
	if _, ok := metadata["part_index"]; ok {
		t.Fatal("replyLongWithMetadata mutated caller metadata")
	}

	if len(withMeta) != 2 {
		t.Fatalf("metadata replies = %d, want 2", len(withMeta))
	}
	if len(plain) != 0 {
		t.Fatalf("plain replies = %d, want 0", len(plain))
	}
	for i, metadata := range metadatas {
		if metadata["credits"] != 0.22 {
			t.Fatalf("metadata[%d] = %#v, want credits", i, metadata)
		}
		if metadata["part_index"] != i+1 || metadata["part_total"] != 2 {
			t.Fatalf("metadata[%d] part = %#v/%#v, want %d/2", i, metadata["part_index"], metadata["part_total"], i+1)
		}
	}
	if !strings.HasPrefix(withMeta[0], "(1/2) ") || !strings.HasPrefix(withMeta[1], "(2/2) ") {
		t.Fatalf("reply prefixes = %q / %q, want part prefixes", withMeta[0], withMeta[1])
	}
	if len(withMeta[0]) > discordReplyLimit || len(withMeta[1]) > discordReplyLimit {
		t.Fatalf("reply lengths = %d/%d, want <= %d", len(withMeta[0]), len(withMeta[1]), discordReplyLimit)
	}
}
