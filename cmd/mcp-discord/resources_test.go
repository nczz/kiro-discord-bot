package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestDiscordPageLimitValidation(t *testing.T) {
	for _, tc := range []struct {
		raw     float64
		max     int
		want    int
		wantErr bool
	}{
		{raw: 1, max: 100, want: 1},
		{raw: 100, max: 100, want: 100},
		{raw: 0, max: 100, wantErr: true},
		{raw: 101, max: 100, wantErr: true},
		{raw: 1.5, max: 100, wantErr: true},
	} {
		got, err := discordPageLimit(tc.raw, tc.max)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("discordPageLimit(%v, %d) = %d, %v; want %d, error=%v", tc.raw, tc.max, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestDiscordListMembersJSONUsesCursorAndReturnsNextCursor(t *testing.T) {
	installDiscordResourceTestSession(t, func(req *http.Request) string {
		if !strings.HasSuffix(req.URL.Path, "/guilds/guild-1/members") {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		if req.URL.Query().Get("after") != "user-0" || req.URL.Query().Get("limit") != "2" {
			t.Fatalf("unexpected query: %s", req.URL.RawQuery)
		}
		return `[
			{"nick":"Alice","user":{"id":"user-1","username":"alice","global_name":"Alice"}},
			{"nick":"Bob","user":{"id":"user-2","username":"bob","global_name":"Bob","bot":true}}
		]`
	})

	raw, err := discordListMembersJSON("guild-1", "user-0", 2)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	var got discordMemberPage
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if got.After != "user-0" || got.NextAfter != "user-2" || len(got.Members) != 2 || !got.Members[1].Bot {
		t.Fatalf("member page = %+v", got)
	}
}

func TestDiscordGetUserDirectLookupDoesNotRequireGuild(t *testing.T) {
	requests := 0
	installDiscordResourceTestSession(t, func(req *http.Request) string {
		requests++
		if !strings.HasSuffix(req.URL.Path, "/users/user-1") {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		return `{"id":"user-1","username":"alice","global_name":"Alice","bot":false}`
	})

	raw, err := discordUserDetailsJSON("user-1", "", "")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	var got discordUserDetails
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	if got.User.ID != "user-1" || got.Member != nil || requests != 1 {
		t.Fatalf("user details = %+v, requests=%d", got, requests)
	}
}

func TestDiscordGetUserOptionallyEnrichesFromChannelGuild(t *testing.T) {
	requests := make(map[string]int)
	installDiscordResourceTestSession(t, func(req *http.Request) string {
		switch {
		case strings.HasSuffix(req.URL.Path, "/users/user-1"):
			requests["user"]++
			return `{"id":"user-1","username":"alice","global_name":"Alice"}`
		case strings.HasSuffix(req.URL.Path, "/channels/channel-1"):
			requests["channel"]++
			return `{"id":"channel-1","guild_id":"guild-1","name":"chat","type":0}`
		case strings.HasSuffix(req.URL.Path, "/guilds/guild-1/members/user-1"):
			requests["member"]++
			return `{"nick":"Ops Alice","roles":["role-1"],"joined_at":"2025-01-02T03:04:05Z","pending":true,"user":{"id":"user-1","username":"alice","global_name":"Alice"}}`
		default:
			t.Fatalf("unexpected path: %s", req.URL.Path)
			return ""
		}
	})

	raw, err := discordUserDetailsJSON("user-1", "", "channel-1")
	if err != nil {
		t.Fatalf("get enriched user: %v", err)
	}
	var got discordUserDetails
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode enriched user: %v", err)
	}
	if got.Member == nil ||
		got.Member.GuildID != "guild-1" ||
		got.Member.Nickname != "Ops Alice" ||
		len(got.Member.Roles) != 1 ||
		got.Member.Roles[0] != "role-1" ||
		got.Member.JoinedAt != "2025-01-02T03:04:05Z" ||
		!got.Member.Pending {
		t.Fatalf("enriched member = %+v", got.Member)
	}
	for _, endpoint := range []string{"user", "channel", "member"} {
		if requests[endpoint] != 1 {
			t.Errorf("request count for %s endpoint = %d, want 1", endpoint, requests[endpoint])
		}
	}
}

func TestDiscordReactionPageJSONUsesAfterCursor(t *testing.T) {
	installDiscordResourceTestSession(t, func(req *http.Request) string {
		if !strings.Contains(req.URL.Path, "/channels/channel-1/messages/message-1/reactions/") {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		if req.URL.Query().Get("after") != "user-0" || req.URL.Query().Get("limit") != "2" {
			t.Fatalf("unexpected query: %s", req.URL.RawQuery)
		}
		return `[
			{"id":"user-1","username":"alice"},
			{"id":"user-2","username":"bob","global_name":"Bob"}
		]`
	})

	raw, err := discordReactionPageJSON("channel-1", "message-1", "👍", "user-0", 2)
	if err != nil {
		t.Fatalf("list reactions: %v", err)
	}
	var got discordReactionPage
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode reactions: %v", err)
	}
	if got.After != "user-0" || got.NextAfter != "user-2" || len(got.Users) != 2 {
		t.Fatalf("reaction page = %+v", got)
	}
}

func installDiscordResourceTestSession(t *testing.T, response func(*http.Request) string) {
	t.Helper()
	previous := dg
	t.Cleanup(func() { dg = previous })
	session, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new Discord session: %v", err)
	}
	session.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := response(req)
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
