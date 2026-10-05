package bot

import (
	"encoding/csv"
	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	L "github.com/nczz/kiro-discord-bot/locale"
	"strings"
	"testing"
	"time"
)

func TestUsageHistoryCSVIncludesLegacyUsernameRows(t *testing.T) {
	manager := channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir(), UsageTimezone: "UTC"})
	t.Cleanup(manager.StopAll)
	b := &Bot{manager: manager}
	for _, rec := range []channel.UsageRecord{
		{Timestamp: "2026-09-01T10:00:00Z", GuildID: "g", ChannelID: "c", UserID: "", Username: "alice", Source: "message", Status: "success", Credits: 1},
		{Timestamp: "2026-09-02T10:00:00Z", GuildID: "g", ChannelID: "c", UserID: "u1", Username: "alice", Source: "command:status", Status: "success", Credits: 2},
	} {
		if err := manager.RecordUsage(rec); err != nil {
			t.Fatal(err)
		}
	}
	state := &usageHistoryState{TargetID: "u1", TargetUsername: "alice", GuildID: "g", Period: "last-month", Status: "all", Source: "all", AllowLegacyUsername: true, From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	ds := &discordgo.Session{State: discordgo.NewState()}
	if err := ds.State.GuildAdd(&discordgo.Guild{ID: "g"}); err != nil {
		t.Fatal(err)
	}
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "g", Nick: "Alice Nick", User: &discordgo.User{ID: "u1", Username: "alice"}}); err != nil {
		t.Fatal(err)
	}
	filename, data, truncated, err := b.usageHistoryCSV(ds, state, 100)
	if err != nil {
		t.Fatal(err)
	}
	if filename != "usage-history-last-month.csv" {
		t.Fatalf("filename = %q", filename)
	}
	if truncated {
		t.Fatal("small export should not be truncated")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("csv rows = %d, want header + 2 records: %q", len(rows), string(data))
	}
	if rows[0][8] != "discord_user" {
		t.Fatalf("csv header[8] = %q, want discord_user", rows[0][8])
	}
	if rows[1][8] != "Alice Nick (alice)" {
		t.Fatalf("csv discord_user = %q, want readable Discord display name", rows[1][8])
	}
}

func TestUsageHistoryOptionsRequiresLegacyPermission(t *testing.T) {
	base := usageHistoryState{TargetID: "u1", TargetUsername: "alice", GuildID: "g", Status: "all", Source: "all"}
	if got := base.usageHistoryOptions(0, usageHistoryCursor{}).LegacyUsernames; len(got) != 0 {
		t.Fatalf("legacy usernames without permission = %+v, want none", got)
	}
	base.AllowLegacyUsername = true
	if got := base.usageHistoryOptions(0, usageHistoryCursor{}).LegacyUsernames; len(got) != 1 || got[0] != "alice" {
		t.Fatalf("legacy usernames with permission = %+v, want alice", got)
	}
}

func TestUsageHistoryEffectiveTargetDefaultsByPermission(t *testing.T) {
	tests := []struct {
		name             string
		targetSpecified  bool
		selectedID       string
		selectedUsername string
		canManage        bool
		wantID           string
		wantUsername     string
		wantAllowed      bool
	}{
		{name: "manager omitted user gets all guild", canManage: true, wantAllowed: true},
		{name: "member omitted user gets self", canManage: false, wantID: "requester", wantUsername: "requester-name", wantAllowed: true},
		{name: "member explicit other denied", targetSpecified: true, selectedID: "other", selectedUsername: "other-name", canManage: false, wantID: "other", wantUsername: "other-name", wantAllowed: false},
		{name: "manager explicit other allowed", targetSpecified: true, selectedID: "other", selectedUsername: "other-name", canManage: true, wantID: "other", wantUsername: "other-name", wantAllowed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotUsername, gotAllowed := usageHistoryEffectiveTarget("requester", "requester-name", tt.targetSpecified, tt.selectedID, tt.selectedUsername, tt.canManage)
			if gotID != tt.wantID || gotUsername != tt.wantUsername || gotAllowed != tt.wantAllowed {
				t.Fatalf("target = (%q, %q, %t), want (%q, %q, %t)", gotID, gotUsername, gotAllowed, tt.wantID, tt.wantUsername, tt.wantAllowed)
			}
		})
	}
}

func TestUsageHistoryAuditScope(t *testing.T) {
	if got := usageHistoryAuditScope(""); got != "all_users" {
		t.Fatalf("blank scope = %q", got)
	}
	if got := usageHistoryAuditScope("u1"); got != "user" {
		t.Fatalf("user scope = %q", got)
	}
}

func TestRenderUsageHistoryAllUsersShowsUserContext(t *testing.T) {
	L.Load("en")
	manager := channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir(), UsageTimezone: "UTC"})
	t.Cleanup(manager.StopAll)
	b := &Bot{manager: manager}
	for _, rec := range []channel.UsageRecord{
		{Timestamp: "2026-09-01T10:00:00Z", GuildID: "g", ChannelID: "c", UserID: "u1", Username: "alice", Source: "message", Status: "success", Credits: 1},
		{Timestamp: "2026-09-02T10:00:00Z", GuildID: "g", ChannelID: "c", UserID: "", Username: "legacy", Source: "message", Status: "success", Credits: 2},
	} {
		if err := manager.RecordUsage(rec); err != nil {
			t.Fatal(err)
		}
	}
	state := &usageHistoryState{GuildID: "g", Period: "last-month", Status: "all", Source: "all", From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Cursors: []usageHistoryCursor{{}}}
	content, _, err := b.renderUsageHistory(nil, state, "token")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Usage history for all users", "Summary", "Top users", "alice", "legacy"} {
		if !strings.Contains(content, want) {
			t.Fatalf("all-user content missing %q:\n%s", want, content)
		}
	}
}

func TestWebShareUsageHistoryDefaultTargetFollowsPermission(t *testing.T) {
	L.Load("en")
	manager := channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir(), UsageTimezone: "UTC"})
	t.Cleanup(manager.StopAll)
	now := time.Now().UTC()
	for _, rec := range []channel.UsageRecord{
		{Timestamp: now.Add(-2 * time.Hour).Format(time.RFC3339), GuildID: "g", ChannelID: "c", UserID: "viewer", Username: "viewer", Source: "message", Status: "success", Credits: 1},
		{Timestamp: now.Add(-1 * time.Hour).Format(time.RFC3339), GuildID: "g", ChannelID: "c", UserID: "other", Username: "other", Source: "message", Status: "success", Credits: 2},
	} {
		if err := manager.RecordUsage(rec); err != nil {
			t.Fatal(err)
		}
	}
	b := &Bot{manager: manager, gmUserIDs: parseDiscordUserIDSet("gm")}

	var gmReply string
	b.cmdWebShareUsageHistory(cmdCtx{userID: "gm", username: "gm", guildID: "g", channelID: "c", targetID: "c", reply: func(msg string) { gmReply = msg }})
	for _, want := range []string{"Usage history for all users", "viewer", "other"} {
		if !strings.Contains(gmReply, want) {
			t.Fatalf("GM WebShare usage history missing %q:\n%s", want, gmReply)
		}
	}

	var viewerReply string
	b.cmdWebShareUsageHistory(cmdCtx{userID: "viewer", username: "viewer", guildID: "g", channelID: "c", targetID: "c", reply: func(msg string) { viewerReply = msg }})
	if !strings.Contains(viewerReply, "Usage history for <@viewer>") || !strings.Contains(viewerReply, "viewer") {
		t.Fatalf("member WebShare usage history should default to self:\n%s", viewerReply)
	}
	if strings.Contains(viewerReply, "other") {
		t.Fatalf("member WebShare usage history leaked another user:\n%s", viewerReply)
	}
}

func TestUsageHistoryCSVCellPreventsSpreadsheetFormulaInjection(t *testing.T) {
	if got := usageHistoryCSVCell("=cmd|' /C calc'!A0"); got != "'=cmd|' /C calc'!A0" {
		t.Fatalf("formula cell = %q", got)
	}
	if got := usageHistoryCSVCell("\n=cmd|' /C calc'!A0"); got != "'\n=cmd|' /C calc'!A0" {
		t.Fatalf("newline formula cell = %q", got)
	}
	if got := usageHistoryCSVCell("alice"); got != "alice" {
		t.Fatalf("plain cell = %q", got)
	}
}
