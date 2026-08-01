package bot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestChannelMetadataEntryRequiresCurrentBotPermissions(t *testing.T) {
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{botMemberDenyOverwrite("bot-1")})
	b := &Bot{}

	denied, err := ds.State.Channel("channel-1")
	if err != nil {
		t.Fatalf("denied channel: %v", err)
	}
	if _, ok := b.channelMetadataEntryForCurrentBot(ds, "guild-1", denied); ok {
		t.Fatalf("metadata entry created for denied channel")
	}

	allowed, err := ds.State.Channel("channel-2")
	if err != nil {
		t.Fatalf("allowed channel: %v", err)
	}
	entry, ok := b.channelMetadataEntryForCurrentBot(ds, "guild-1", allowed)
	if !ok || entry.ID != "channel-2" || entry.Type != "channel" {
		t.Fatalf("allowed metadata entry = %+v, ok=%v", entry, ok)
	}
}
