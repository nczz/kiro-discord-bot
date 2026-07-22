package bot

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/channel"
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

func TestSplitDiscordMessageDemotesHeadings(t *testing.T) {
	parts := splitDiscordMessage("# Title\n\nbody", 50)
	if len(parts) != 1 || parts[0] != "**Title**\n\nbody" {
		t.Fatalf("parts = %#v, want heading demoted", parts)
	}
}

func TestReplyLongWithMetadataPrefixesOutsideCodeBlock(t *testing.T) {
	var withMeta []string
	ctx := cmdCtx{
		replyWithMetadata: func(msg string, metadata map[string]any) {
			withMeta = append(withMeta, msg)
		},
	}
	content := "```go\n" + strings.Repeat("fmt.Println(1)\n", 180) + "```"
	replyLongWithMetadata(ctx, content, nil)
	if len(withMeta) < 2 {
		t.Fatalf("replies = %d, want multiple", len(withMeta))
	}
	if !strings.HasPrefix(withMeta[1], "(2/") || !strings.Contains(withMeta[1], "\n```go\n") {
		t.Fatalf("prefix/code block placement = %q", withMeta[1])
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

func TestSplitOversizedReplyPreservesShortPayload(t *testing.T) {
	metadata := map[string]any{"ephemeral": true}
	payloads := splitOversizedReply("short", metadata)
	if len(payloads) != 1 {
		t.Fatalf("payloads = %d, want 1", len(payloads))
	}
	if payloads[0].content != "short" {
		t.Fatalf("content = %q, want short", payloads[0].content)
	}
	if payloads[0].metadata["ephemeral"] != true {
		t.Fatalf("metadata = %#v, want original metadata", payloads[0].metadata)
	}
	if _, ok := payloads[0].metadata["part_index"]; ok {
		t.Fatal("short payload should not add part metadata")
	}
}

func TestSplitOversizedReplyAddsPartMetadata(t *testing.T) {
	metadata := map[string]any{"ephemeral": true}
	payloads := splitOversizedReply(strings.Repeat("x", discordReplyLimit+10), metadata)
	if len(payloads) != 2 {
		t.Fatalf("payloads = %d, want 2", len(payloads))
	}
	if _, ok := metadata["part_index"]; ok {
		t.Fatal("splitOversizedReply mutated caller metadata")
	}
	for i, payload := range payloads {
		if len(payload.content) > discordReplyLimit {
			t.Fatalf("payload %d length = %d, want <= %d", i, len(payload.content), discordReplyLimit)
		}
		if payload.metadata["ephemeral"] != true {
			t.Fatalf("payload %d metadata = %#v, want original metadata", i, payload.metadata)
		}
		if payload.metadata["part_index"] != i+1 || payload.metadata["part_total"] != len(payloads) {
			t.Fatalf("payload %d part = %#v/%#v, want %d/%d", i, payload.metadata["part_index"], payload.metadata["part_total"], i+1, len(payloads))
		}
	}
}

func TestCmdModelsSplitsLongModelList(t *testing.T) {
	cliPath := filepath.Join(t.TempDir(), "kiro-cli")
	var payload strings.Builder
	payload.WriteString(`{"default_model":"model-000","models":[`)
	for i := range 80 {
		if i > 0 {
			payload.WriteByte(',')
		}
		id := "model-" + strconv.Itoa(i)
		payload.WriteString(`{"model_name":"`)
		payload.WriteString(id)
		payload.WriteString(`","model_id":"`)
		payload.WriteString(id)
		payload.WriteString(`","description":"`)
		payload.WriteString(strings.Repeat("long description ", 8))
		payload.WriteString(`","rate_multiplier":1,"rate_unit":"Credit"}`)
	}
	payload.WriteString(`]}`)

	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"chat\" ] && [ \"$2\" = \"--list-models\" ] && [ \"$3\" = \"-f\" ] && [ \"$4\" = \"json\" ]; then\n" +
		"cat <<'JSON'\n" + payload.String() + "\nJSON\n" +
		"exit 0\nfi\n" +
		"echo unexpected args >&2\nexit 2\n"
	if err := os.WriteFile(cliPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	b := &Bot{manager: channel.NewManager(channel.ManagerConfig{KiroCLIPath: cliPath})}
	var replies []string
	var metadatas []map[string]any
	b.cmdModels(cmdCtx{
		replyWithMetadata: func(msg string, metadata map[string]any) {
			replies = append(replies, msg)
			metadatas = append(metadatas, metadata)
		},
	})

	if len(replies) < 2 {
		t.Fatalf("replies = %d, want split model list", len(replies))
	}
	for i, reply := range replies {
		if len(reply) > discordReplyLimit {
			t.Fatalf("reply %d length = %d, want <= %d", i, len(reply), discordReplyLimit)
		}
		if metadatas[i]["part_index"] != i+1 || metadatas[i]["part_total"] != len(replies) {
			t.Fatalf("metadata[%d] part = %#v/%#v, want %d/%d", i, metadatas[i]["part_index"], metadatas[i]["part_total"], i+1, len(replies))
		}
	}
}
