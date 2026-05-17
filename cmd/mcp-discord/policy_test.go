package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseIDSet(t *testing.T) {
	got := parseIDSet(" 123,456,,123 ")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if _, ok := got["123"]; !ok {
		t.Fatal("missing 123")
	}
	if _, ok := got["456"]; !ok {
		t.Fatal("missing 456")
	}
}

func TestDiscordPolicyGuildAllowed(t *testing.T) {
	p := discordPolicy{allowedGuilds: parseIDSet("g1,g2")}
	if !p.guildAllowed("g1") {
		t.Fatal("g1 should be allowed")
	}
	if p.guildAllowed("g3") {
		t.Fatal("g3 should be denied")
	}

	p = discordPolicy{}
	if !p.guildAllowed("anything") {
		t.Fatal("empty guild allowlist should allow")
	}
}

func TestDiscordPolicyChannelAllowed(t *testing.T) {
	p := discordPolicy{allowedChannels: parseIDSet("c1,c2")}
	if !p.channelIDAllowed("c2") {
		t.Fatal("c2 should be allowed")
	}
	if p.channelIDAllowed("c3") {
		t.Fatal("c3 should be denied")
	}

	p = discordPolicy{}
	if !p.channelIDAllowed("anything") {
		t.Fatal("empty channel allowlist should allow")
	}
}

func TestValidateDiscordAttachmentURL(t *testing.T) {
	if _, err := validateDiscordAttachmentURL("https://cdn.discordapp.com/attachments/1/file.txt"); err != nil {
		t.Fatalf("discord attachment url denied: %v", err)
	}
	if _, err := validateDiscordAttachmentURL("http://cdn.discordapp.com/attachments/1/file.txt"); err == nil {
		t.Fatal("http url should be denied")
	}
	if _, err := validateDiscordAttachmentURL("https://example.com/file.txt"); err == nil {
		t.Fatal("non-discord host should be denied")
	}
}

func TestResolveDownloadDirWithinRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MCP_DISCORD_DOWNLOAD_DIR", root)

	got, err := resolveDownloadDir(filepath.Join(root, "nested"))
	if err != nil {
		t.Fatalf("resolve inside root: %v", err)
	}
	if got != filepath.Join(root, "nested") {
		t.Fatalf("got %q, want nested dir", got)
	}

	if _, err := resolveDownloadDir(filepath.Dir(root)); err == nil {
		t.Fatal("outside root should be denied")
	}
}

func TestResolveDownloadDirRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("MCP_DISCORD_DOWNLOAD_DIR", root)

	if _, err := resolveDownloadDir(link); err == nil {
		t.Fatal("symlink escape should be denied")
	}
}

func TestResolveDownloadDirDefaultsToPolicyRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MCP_DISCORD_DOWNLOAD_DIR", root)

	got, err := resolveDownloadDir(os.TempDir())
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	if got != root {
		t.Fatalf("got %q, want policy root %q", got, root)
	}
}

func TestSafeAttachmentFilename(t *testing.T) {
	got := safeAttachmentFilename("/attachments/1/../../a b?.txt")
	if got != "a-b" {
		t.Fatalf("got %q, want sanitized base name", got)
	}
	if got := safeAttachmentFilename("/"); got != "attachment" {
		t.Fatalf("empty name got %q", got)
	}
}
