package bot

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestParseBotPeers(t *testing.T) {
	peers := parseBotPeers("BuildBot:111111111111111111:222222222222222222, bad, ReviewBot:333333333333333333, RoleOnly::444444444444444444")
	if len(peers) != 3 {
		t.Fatalf("len = %d, want 3", len(peers))
	}
	if peers[0].Name != "BuildBot" || peers[0].Mention() != "<@111111111111111111>" || peers[0].RoleMention() != "<@&222222222222222222>" {
		t.Fatalf("first peer = %#v mention=%q role=%q", peers[0], peers[0].Mention(), peers[0].RoleMention())
	}
	if peers[1].Name != "ReviewBot" || peers[1].Mention() != "<@333333333333333333>" {
		t.Fatalf("second peer = %#v mention=%q", peers[1], peers[1].Mention())
	}
	if peers[2].Name != "RoleOnly" || peers[2].ID != "" || peers[2].RoleID != "444444444444444444" || !peers[2].Manual {
		t.Fatalf("manual role-only peer = %#v", peers[2])
	}
}

func TestMultiBotMode(t *testing.T) {
	b := &Bot{peers: parseBotPeers("BuildBot:111111111111111111,ReviewBot:333333333333333333")}
	if !b.multiBotMode("111111111111111111") {
		t.Fatal("expected multi-bot mode when another peer exists")
	}
	if b.multiBotMode("unknown") != true {
		t.Fatal("unknown self with configured peers should be treated as multi-bot")
	}
	onlySelf := &Bot{peers: parseBotPeers("BuildBot:111111111111111111")}
	if onlySelf.multiBotMode("111111111111111111") {
		t.Fatal("did not expect multi-bot mode with only self configured")
	}
}

func TestPeerPromptContextSeparatesSelf(t *testing.T) {
	b := &Bot{peers: parseBotPeers("BuildBot:111111111111111111,ReviewBot:333333333333333333")}
	got := b.peerPromptContext("111111111111111111")
	if !containsAll(got, "self=BuildBot", "handoff_peer=ReviewBot", "Never mention yourself") {
		t.Fatalf("peerPromptContext() missing expected content:\n%s", got)
	}
}

func TestMergeBotPeersManualOverridesDiscovery(t *testing.T) {
	discovered := []BotPeer{
		{Name: "AutoSelf", ID: "bot-1", RoleID: "auto-role-1"},
		{Name: "AutoPeer", ID: "bot-2", RoleID: "auto-role-2"},
		{Name: "UnrelatedBot", ID: "bot-3", RoleID: "auto-role-3"},
		{Name: "RoleOnly", RoleID: "role-only"},
	}
	manual := parseBotPeers("ManualPeer:bot-2:manual-role-2,!bot-3,ExtraBot:bot-4:role-4,ManualRole:bot-5:role-only")

	got := mergeBotPeers(discovered, manual)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4: %#v", len(got), got)
	}
	if got[1].Name != "ManualPeer" || got[1].RoleID != "manual-role-2" {
		t.Fatalf("manual override not applied: %#v", got[1])
	}
	for _, p := range got {
		if p.ID == "bot-3" {
			t.Fatalf("excluded peer remained: %#v", got)
		}
	}
	if got[2].ID != "bot-5" || got[2].RoleID != "role-only" {
		t.Fatalf("manual role override missing: %#v", got)
	}
	if got[3].ID != "bot-4" {
		t.Fatalf("manual extra peer missing: %#v", got)
	}
}

func TestRoleOnlyPeerTriggersMultiBotAndMentions(t *testing.T) {
	b := &Bot{peers: []BotPeer{
		{Name: "SelfBot", ID: "bot-1", RoleID: "role-1"},
		{Name: "PeerRoleOnly", RoleID: "role-2"},
	}}

	if !b.multiBotMode("bot-1") {
		t.Fatal("role-only peer should enable multi-bot mode")
	}
	if !b.messageMentionsOtherPeer(nil, "<@&role-2> handle this", "bot-1") {
		t.Fatal("role-only peer mention should count as another peer")
	}
	if b.messageMentionsOtherPeer(nil, "<@&role-1> handle this", "bot-1") {
		t.Fatal("self role mention should not count as another peer")
	}
}

func TestBotPeerCountsSeparatesUserAndRoleOnlyPeers(t *testing.T) {
	userPeers, roleOnlyPeers := botPeerCounts([]BotPeer{
		{Name: "SelfBot", ID: "bot-1", RoleID: "role-1"},
		{Name: "PeerBot", ID: "bot-2", RoleID: "role-2"},
		{Name: "RoleOnly", RoleID: "role-3"},
	})

	if userPeers != 2 || roleOnlyPeers != 1 {
		t.Fatalf("botPeerCounts() = (%d, %d), want (2, 1)", userPeers, roleOnlyPeers)
	}
}

func TestBotRoleIDPrefersManagedNameMatch(t *testing.T) {
	member := &discordgo.Member{
		Nick:  "M5Bot",
		User:  &discordgo.User{ID: "bot-1", Username: "m5-app", Bot: true},
		Roles: []string{"generic", "managed-match"},
	}
	roles := map[string]*discordgo.Role{
		"generic":       {ID: "generic", Name: "Bot", Managed: true},
		"managed-match": {ID: "managed-match", Name: "M5Bot", Managed: true},
	}

	if got := botRoleID(member, roles); got != "managed-match" {
		t.Fatalf("botRoleID() = %q, want managed-match", got)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
