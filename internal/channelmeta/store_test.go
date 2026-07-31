package channelmeta

import "testing"

func TestReadToleratesSingleTrailingBrace(t *testing.T) {
	entries, err := decodeEntries([]byte(`{"channel-1":{"id":"channel-1","guild_id":"guild-1","name":"隨口問","type":"channel","updated_at":"now"}}}`))
	if err != nil {
		t.Fatalf("decodeEntries: %v", err)
	}
	if entries["channel-1"].Name != "隨口問" {
		t.Fatalf("entries = %+v", entries)
	}
}
