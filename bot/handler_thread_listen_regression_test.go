package bot

import (
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
)

func TestHandleThreadCreateCapturesParentListenMode(t *testing.T) {
	dataDir := t.TempDir()
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dataDir})
	b := &Bot{dataDir: dataDir, manager: manager}
	ds := testPeerPermissionSession(t, nil)
	thread := &discordgo.Channel{
		ID:       "manual-thread-listen-capture",
		GuildID:  "guild-1",
		ParentID: "channel-1",
		Type:     discordgo.ChannelTypeGuildPublicThread,
	}
	if err := ds.State.ChannelAdd(thread); err != nil {
		t.Fatalf("ChannelAdd: %v", err)
	}

	manager.Pause("channel-1")
	b.handleThreadCreate(ds, &discordgo.ThreadCreate{Channel: thread})

	mode, ok := manager.ThreadListenSnapshot(thread.ID)
	if !ok || mode != "mention" {
		t.Fatalf("manual thread snapshot = (%q, %v), want (mention, true)", mode, ok)
	}

	manager.Back("channel-1")
	if !manager.ThreadMentionOnly(thread.ID, thread.ParentID) {
		t.Fatal("captured mention-only mode should remain after parent /back")
	}
}

func TestResolveThreadParentUsesDiscordStateBeforeREST(t *testing.T) {
	ds := testPeerPermissionSession(t, nil)
	threadID := "state-thread-parent-resolution"
	if err := ds.State.ChannelAdd(&discordgo.Channel{
		ID:       threadID,
		GuildID:  "guild-1",
		ParentID: "channel-1",
		Type:     discordgo.ChannelTypeGuildPublicThread,
	}); err != nil {
		t.Fatalf("ChannelAdd: %v", err)
	}
	ds.Client = nil

	registerThreadParent(threadID, "")
	parentID := resolveThreadParent(ds, threadID)
	if parentID != "channel-1" {
		t.Fatalf("resolveThreadParent() = %q, want channel-1", parentID)
	}
}

func TestPausedParentIgnoresUnmentionedManualThreadMessage(t *testing.T) {
	dataDir := t.TempDir()
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dataDir})
	rt := &recordingDiscordTransport{}
	ds := testPeerPermissionSession(t, nil)
	ds.Client = &http.Client{Transport: rt}
	b := &Bot{dataDir: dataDir, manager: manager, seen: newSeenMessages()}
	defer b.seen.Stop()
	thread := &discordgo.Channel{
		ID:       "manual-thread-paused-routing",
		GuildID:  "guild-1",
		ParentID: "channel-1",
		Type:     discordgo.ChannelTypeGuildPublicThread,
	}
	if err := ds.State.ChannelAdd(thread); err != nil {
		t.Fatalf("ChannelAdd: %v", err)
	}

	manager.Pause("channel-1")
	b.handleThreadCreate(ds, &discordgo.ThreadCreate{Channel: thread})
	b.handleMessage(ds, &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "manual-thread-paused-message",
		ChannelID: thread.ID,
		GuildID:   "guild-1",
		Content:   "please answer without a mention",
		Author:    &discordgo.User{ID: "viewer", Username: "Viewer"},
	}})

	paths, _ := rt.Snapshot()
	if len(paths) != 0 {
		t.Fatalf("paused manual thread caused Discord requests: %v", paths)
	}
}
