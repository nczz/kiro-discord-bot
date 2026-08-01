package channelmeta

import (
	"testing"
)

func TestReadToleratesSingleTrailingBrace(t *testing.T) {
	entries, err := decodeEntries([]byte(`{"channel-1":{"id":"channel-1","guild_id":"guild-1","name":"隨口問","type":"channel","updated_at":"now"}}}`))
	if err != nil {
		t.Fatalf("decodeEntries: %v", err)
	}
	if entries["channel-1"].Name != "隨口問" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestReplaceGuildChannelsPrunesStaleChannelsOnly(t *testing.T) {
	dir := t.TempDir()
	if err := Upsert(dir, Entry{ID: "channel-old", GuildID: "guild-1", Name: "old", Type: "channel"}); err != nil {
		t.Fatalf("upsert old channel: %v", err)
	}
	if err := Upsert(dir, Entry{ID: "thread-1", GuildID: "guild-1", Name: "thread", Type: "thread", ParentChannelID: "channel-old"}); err != nil {
		t.Fatalf("upsert thread: %v", err)
	}
	if err := Upsert(dir, Entry{ID: "channel-other", GuildID: "guild-2", Name: "other", Type: "channel"}); err != nil {
		t.Fatalf("upsert other guild: %v", err)
	}
	if err := ReplaceGuildChannels(dir, "guild-1", []Entry{{ID: "channel-new", GuildID: "guild-1", Name: "new", Type: "channel"}}); err != nil {
		t.Fatalf("ReplaceGuildChannels: %v", err)
	}
	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := entries["channel-old"]; ok {
		t.Fatalf("stale guild channel was not pruned: %+v", entries)
	}
	if _, ok := entries["channel-new"]; !ok {
		t.Fatalf("new guild channel missing: %+v", entries)
	}
	if _, ok := entries["thread-1"]; !ok {
		t.Fatalf("thread metadata should be preserved: %+v", entries)
	}
	if _, ok := entries["channel-other"]; !ok {
		t.Fatalf("other guild channel should be preserved: %+v", entries)
	}
}
