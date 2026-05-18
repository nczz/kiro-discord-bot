package bot

import (
	"strings"
	"testing"
)

func TestParseBotPeers(t *testing.T) {
	peers := parseBotPeers("M5Bot:1505737846013558834, bad, ChunBot:1495737209616072815")
	if len(peers) != 2 {
		t.Fatalf("len = %d, want 2", len(peers))
	}
	if peers[0].Name != "M5Bot" || peers[0].Mention() != "<@1505737846013558834>" {
		t.Fatalf("first peer = %#v mention=%q", peers[0], peers[0].Mention())
	}
	if peers[1].Name != "ChunBot" || peers[1].Mention() != "<@1495737209616072815>" {
		t.Fatalf("second peer = %#v mention=%q", peers[1], peers[1].Mention())
	}
}

func TestMultiBotMode(t *testing.T) {
	b := &Bot{peers: parseBotPeers("M5Bot:1505737846013558834,ChunBot:1495737209616072815")}
	if !b.multiBotMode("1505737846013558834") {
		t.Fatal("expected multi-bot mode when another peer exists")
	}
	if b.multiBotMode("unknown") != true {
		t.Fatal("unknown self with configured peers should be treated as multi-bot")
	}
	onlySelf := &Bot{peers: parseBotPeers("M5Bot:1505737846013558834")}
	if onlySelf.multiBotMode("1505737846013558834") {
		t.Fatal("did not expect multi-bot mode with only self configured")
	}
}

func TestPeerPromptContextSeparatesSelf(t *testing.T) {
	b := &Bot{peers: parseBotPeers("M5Bot:1505737846013558834,ChunBot:1495737209616072815")}
	got := b.peerPromptContext("1505737846013558834")
	if !containsAll(got, "self=M5Bot", "handoff_peer=ChunBot", "Never mention yourself") {
		t.Fatalf("peerPromptContext() missing expected content:\n%s", got)
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
