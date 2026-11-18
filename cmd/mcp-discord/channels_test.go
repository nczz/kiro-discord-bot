package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestDiscordGuildChannelInventoryIncludesEveryVisibleChannelType(t *testing.T) {
	installChannelInventoryTestSession(t)

	raw, err := discordGuildChannelInventoryJSON("guild-1", false)
	if err != nil {
		t.Fatalf("list guild channels: %v", err)
	}
	var got discordGuildChannelInventory
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode channel inventory: %v", err)
	}
	if got.GuildID != "guild-1" || got.ActiveThreadsIncluded {
		t.Fatalf("inventory metadata = %+v", got)
	}
	if len(got.Channels) != 5 {
		t.Fatalf("channel count = %d, want all 5 visible guild channels: %s", len(got.Channels), raw)
	}

	byID := make(map[string]discordChannelRecord, len(got.Channels))
	for _, channel := range got.Channels {
		byID[channel.ID] = channel
	}
	for id, wantType := range map[string]string{
		"category-1": "category",
		"text-1":     "text",
		"voice-1":    "voice",
		"forum-1":    "forum",
		"media-1":    "media",
	} {
		if gotType := byID[id].Type; gotType != wantType {
			t.Errorf("channel %s type = %q, want %q", id, gotType, wantType)
		}
	}
	if channel := byID["text-1"]; channel.ParentID != "category-1" || channel.Position != 2 {
		t.Errorf("text channel placement = %+v", channel)
	}
}

func TestDiscordGuildChannelInventoryOptionallyIncludesActiveThreads(t *testing.T) {
	installChannelInventoryTestSession(t)

	raw, err := discordGuildChannelInventoryJSON("guild-1", true)
	if err != nil {
		t.Fatalf("list guild channels with active threads: %v", err)
	}
	var got discordGuildChannelInventory
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode channel inventory: %v", err)
	}
	if !got.ActiveThreadsIncluded {
		t.Fatal("active_threads_included = false, want true")
	}
	if len(got.Channels) != 6 {
		t.Fatalf("channel count = %d, want 5 guild channels plus 1 active thread: %s", len(got.Channels), raw)
	}
	for _, channel := range got.Channels {
		if channel.ID == "thread-1" {
			if channel.Type != "public_thread" || channel.ParentID != "text-1" {
				t.Fatalf("active thread = %+v", channel)
			}
			return
		}
	}
	t.Fatal("active thread missing from inventory")
}

func installChannelInventoryTestSession(t *testing.T) {
	t.Helper()
	previous := dg
	t.Cleanup(func() { dg = previous })

	session, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new Discord session: %v", err)
	}
	session.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch {
		case strings.HasSuffix(req.URL.Path, "/guilds/guild-1/channels"):
			body = `[
				{"id":"category-1","guild_id":"guild-1","name":"General","type":4,"position":0},
				{"id":"text-1","guild_id":"guild-1","name":"chat","type":0,"position":2,"parent_id":"category-1"},
				{"id":"voice-1","guild_id":"guild-1","name":"voice","type":2,"position":1,"parent_id":"category-1"},
				{"id":"forum-1","guild_id":"guild-1","name":"forum","type":15,"position":3},
				{"id":"media-1","guild_id":"guild-1","name":"media","type":16,"position":4}
			]`
		case strings.HasSuffix(req.URL.Path, "/guilds/guild-1/threads/active"):
			body = `{"threads":[{"id":"thread-1","guild_id":"guild-1","name":"active","type":11,"position":5,"parent_id":"text-1"}],"members":[],"has_more":false}`
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"message":"not found"}`)),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	dg = session
}
