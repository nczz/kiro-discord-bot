package bot

import "testing"

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
