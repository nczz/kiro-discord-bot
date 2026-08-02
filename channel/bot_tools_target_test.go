package channel

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestBotToolsRequesterPermissionsUsesThreadParentFallback(t *testing.T) {
	ds := &discordgo.Session{State: discordgo.NewState(), Ratelimiter: discordgo.NewRatelimiter()}
	ds.State.User = &discordgo.User{ID: "bot-1", Bot: true}
	if err := ds.State.GuildAdd(&discordgo.Guild{ID: "guild-1", Roles: []*discordgo.Role{{ID: "guild-1", Name: "@everyone", Permissions: int64(discordgo.PermissionViewChannel)}}}); err != nil {
		t.Fatalf("GuildAdd: %v", err)
	}
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "manager"}}); err != nil {
		t.Fatalf("MemberAdd: %v", err)
	}
	if err := ds.State.ChannelAdd(&discordgo.Channel{ID: "channel-1", GuildID: "guild-1", Type: discordgo.ChannelTypeGuildText, PermissionOverwrites: []*discordgo.PermissionOverwrite{{ID: "manager", Type: discordgo.PermissionOverwriteTypeMember, Allow: int64(discordgo.PermissionManageChannels)}}}); err != nil {
		t.Fatalf("ChannelAdd parent: %v", err)
	}
	if err := ds.State.ChannelAdd(&discordgo.Channel{ID: "thread-1", GuildID: "guild-1", ParentID: "channel-1", Type: discordgo.ChannelTypeGuildPublicThread}); err != nil {
		t.Fatalf("ChannelAdd thread: %v", err)
	}
	canManageChannel, _ := botToolsRequesterPermissions(ds, "manager", "thread-1", "")
	if !canManageChannel {
		t.Fatal("thread manager permission did not fall back to parent channel")
	}
	canManageChannel, _ = botToolsRequesterPermissions(ds, "manager", "fresh-thread", "channel-1")
	if !canManageChannel {
		t.Fatal("fresh thread permission did not use explicit parent channel fallback")
	}
}
