package channel

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "moderator"}}); err != nil {
		t.Fatalf("MemberAdd moderator: %v", err)
	}
	if err := ds.State.ChannelAdd(&discordgo.Channel{ID: "channel-1", GuildID: "guild-1", Type: discordgo.ChannelTypeGuildText, PermissionOverwrites: []*discordgo.PermissionOverwrite{{ID: "manager", Type: discordgo.PermissionOverwriteTypeMember, Allow: int64(discordgo.PermissionManageChannels)}, {ID: "moderator", Type: discordgo.PermissionOverwriteTypeMember, Allow: int64(discordgo.PermissionManageMessages | discordgo.PermissionManageThreads)}}}); err != nil {
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
	canManageChannel, _ = botToolsRequesterPermissions(ds, "moderator", "thread-1", "")
	if canManageChannel {
		t.Fatal("message/thread moderator should not receive channel-management bot-tools authority")
	}
}

func TestRemoteA2ATargetStateNeverGrantsDiscordManagePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.json")
	if err := writeBotToolsTargetStateWithRequesterSource(path, "thread-1", false, nil, true, false, "manager", "alice", 1, true, true, "a2a"); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read target state: %v", err)
	}
	var state botToolsTargetState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("parse target state: %v", err)
	}
	if !state.RemoteA2A {
		t.Fatal("remote A2A flag not persisted")
	}
	if state.CanManageChannel || state.CanManageGuild {
		t.Fatalf("remote A2A target state granted Discord manager authority: %+v", state)
	}
}
