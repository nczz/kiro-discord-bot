package channel

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
)

func TestUsageStoreMonthlyFilesAndReport(t *testing.T) {
	dir := t.TempDir()
	store := NewUsageStore(dir, "Asia/Taipei", 0)

	records := []UsageRecord{
		{
			Timestamp:     "2026-05-28T10:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			UserID:        "u1",
			Username:      "alice",
			MeteringUsage: []acp.MeteringItem{{Value: 1.25, Unit: "credits"}},
		},
		{
			Timestamp:     "2026-05-27T10:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			UserID:        "u1",
			Username:      "alice",
			MeteringUsage: []acp.MeteringItem{{Value: 2.5, Unit: "credits"}},
		},
		{
			Timestamp:     "2026-05-01T10:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			UserID:        "u2",
			Username:      "bob",
			MeteringUsage: []acp.MeteringItem{{Value: 10, Unit: "credits"}},
		},
	}
	for _, rec := range records {
		if err := store.Append(rec); err != nil {
			t.Fatalf("append usage: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "usage", "2026-05.jsonl")); err != nil {
		t.Fatalf("expected monthly usage file: %v", err)
	}

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, store.Location())
	report, err := store.Report("g1", "", "", 10, now)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(report.Rows))
	}
	if report.Rows[0].UserID != "u2" {
		t.Fatalf("first row = %s, want u2 sorted by month credits", report.Rows[0].UserID)
	}
	var alice UsageReportRow
	for _, row := range report.Rows {
		if row.UserID == "u1" {
			alice = row
		}
	}
	if alice.DayCredits != 1.25 || alice.WeekCredits != 3.75 || alice.MonthCredits != 3.75 {
		t.Fatalf("alice credits day/week/month = %.2f/%.2f/%.2f", alice.DayCredits, alice.WeekCredits, alice.MonthCredits)
	}
}

func TestUsageStoreAcceptsSingularCreditUnit(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "Asia/Taipei", 0)
	if err := store.Append(UsageRecord{
		Timestamp:     "2026-05-28T10:00:00+08:00",
		GuildID:       "g1",
		ChannelID:     "c1",
		UserID:        "u1",
		MeteringUsage: []acp.MeteringItem{{Value: 0.5, Unit: "credit"}},
	}); err != nil {
		t.Fatalf("append singular credit usage: %v", err)
	}
	report, err := store.Report("g1", "", "u1", 0, time.Date(2026, 5, 28, 12, 0, 0, 0, store.Location()))
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(report.Rows))
	}
	if report.Rows[0].DayCredits != 0.5 || report.Rows[0].MeteredDayTurns != 1 {
		t.Fatalf("day credits/metered = %.2f/%d, want 0.50/1", report.Rows[0].DayCredits, report.Rows[0].MeteredDayTurns)
	}
}

func TestUsageReportRecomputesCreditsFromRawMetering(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "Asia/Taipei", 0)
	if err := store.Append(UsageRecord{
		Timestamp:         "2026-05-28T10:00:00+08:00",
		GuildID:           "g1",
		ChannelID:         "c1",
		UserID:            "u1",
		Credits:           0,
		MeteringSupported: false,
		MeteringUsage:     []acp.MeteringItem{{Value: 0.75, Unit: "credit"}},
	}); err != nil {
		t.Fatalf("append legacy incorrect usage: %v", err)
	}
	report, err := store.Report("g1", "", "u1", 0, time.Date(2026, 5, 28, 12, 0, 0, 0, store.Location()))
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if got := report.Rows[0].MonthCredits; got != 0.75 {
		t.Fatalf("month credits = %.2f, want 0.75", got)
	}
}

func TestUsageStoreMissingMeteringDoesNotFail(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "Asia/Taipei", 0)
	if err := store.Append(UsageRecord{
		Timestamp: "2026-05-28T10:00:00+08:00",
		GuildID:   "g1",
		ChannelID: "c1",
		UserID:    "u1",
	}); err != nil {
		t.Fatalf("append without metering: %v", err)
	}
	report, err := store.Report("g1", "", "u1", 0, time.Date(2026, 5, 28, 12, 0, 0, 0, store.Location()))
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(report.Rows))
	}
	if report.Rows[0].MonthCredits != 0 || report.Rows[0].MeteredMonthTurns != 0 || report.Rows[0].MonthTurns != 1 {
		t.Fatalf("unexpected unmetered row: %+v", report.Rows[0])
	}
}

func TestUsageReportMergesEmptyUserIDWhenUsernameHasUniqueUserID(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "Asia/Taipei", 0)
	records := []UsageRecord{
		{
			Timestamp:     "2026-05-28T09:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			UserID:        "u1",
			Username:      "mxp.tw",
			Source:        "message",
			MeteringUsage: []acp.MeteringItem{{Value: 1, Unit: "credit"}},
		},
		{
			Timestamp:     "2026-05-28T10:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			Username:      "mxp.tw",
			Source:        "cron",
			MeteringUsage: []acp.MeteringItem{{Value: 0.5, Unit: "credit"}},
		},
	}
	for _, rec := range records {
		if err := store.Append(rec); err != nil {
			t.Fatalf("append usage: %v", err)
		}
	}

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, store.Location())
	report, err := store.Report("g1", "", "", 10, now)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("rows = %d, want 1: %+v", len(report.Rows), report.Rows)
	}
	row := report.Rows[0]
	if row.UserID != "u1" || row.Username != "mxp.tw" {
		t.Fatalf("row identity = %q/%q, want u1/mxp.tw", row.UserID, row.Username)
	}
	if row.MonthTurns != 2 || row.MeteredMonthTurns != 2 || row.MonthCredits != 1.5 {
		t.Fatalf("row totals = %+v, want two metered turns and 1.5 credits", row)
	}

	filtered, err := store.Report("g1", "", "u1", 0, now)
	if err != nil {
		t.Fatalf("filtered report: %v", err)
	}
	if len(filtered.Rows) != 1 || filtered.Rows[0].MonthTurns != 2 {
		t.Fatalf("filtered rows = %+v, want merged two-turn row", filtered.Rows)
	}
}

func TestUsageReportDoesNotMergeEmptyUserIDWhenUsernameIsAmbiguous(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "Asia/Taipei", 0)
	records := []UsageRecord{
		{
			Timestamp:     "2026-05-28T09:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			UserID:        "u1",
			Username:      "shared",
			MeteringUsage: []acp.MeteringItem{{Value: 1, Unit: "credit"}},
		},
		{
			Timestamp:     "2026-05-28T10:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			UserID:        "u2",
			Username:      "shared",
			MeteringUsage: []acp.MeteringItem{{Value: 2, Unit: "credit"}},
		},
		{
			Timestamp:     "2026-05-28T11:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			Username:      "shared",
			Source:        "cron",
			MeteringUsage: []acp.MeteringItem{{Value: 0.5, Unit: "credit"}},
		},
	}
	for _, rec := range records {
		if err := store.Append(rec); err != nil {
			t.Fatalf("append usage: %v", err)
		}
	}

	report, err := store.Report("g1", "", "", 10, time.Date(2026, 5, 28, 12, 0, 0, 0, store.Location()))
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(report.Rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(report.Rows), report.Rows)
	}
	var unresolved *UsageReportRow
	for i := range report.Rows {
		if report.Rows[i].UserID == "" {
			unresolved = &report.Rows[i]
		}
	}
	if unresolved == nil {
		t.Fatalf("missing unresolved ambiguous row: %+v", report.Rows)
	}
	if unresolved.Username != "shared" || unresolved.MonthTurns != 1 || unresolved.MonthCredits != 0.5 {
		t.Fatalf("unresolved row = %+v, want shared one-turn 0.5 credit row", *unresolved)
	}
}

func TestUsageRetentionPrunesOldMonthlyFiles(t *testing.T) {
	dir := t.TempDir()
	usageDir := filepath.Join(dir, "usage")
	if err := os.MkdirAll(usageDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(usageDir, "2026-03.jsonl")
	keepPath := filepath.Join(usageDir, "2026-05.jsonl")
	if err := os.WriteFile(oldPath, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepPath, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewUsageStore(dir, "Asia/Taipei", 0)
	store.retentionMonths = 2
	if err := store.Append(UsageRecord{
		Timestamp:     "2026-05-28T10:00:00+08:00",
		GuildID:       "g1",
		ChannelID:     "c1",
		UserID:        "u1",
		MeteringUsage: []acp.MeteringItem{{Value: 1, Unit: "credits"}},
	}); err != nil {
		t.Fatalf("append to trigger prune: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("keep file missing: %v", err)
	}
}
