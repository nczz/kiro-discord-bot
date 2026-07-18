package channel

import (
	"fmt"
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
	if _, err := os.Stat(filepath.Join(dir, "usage", "usage.sqlite")); err != nil {
		t.Fatalf("expected usage sqlite database: %v", err)
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

func TestUsageStorePreservesExplicitCreditsWithoutRawMetering(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "UTC", 0)
	if err := store.Append(UsageRecord{
		Timestamp:         "2026-07-17T10:00:00Z",
		GuildID:           "g1",
		ChannelID:         "c1",
		UserID:            "u1",
		Credits:           3.5,
		CostUSD:           0.25,
		MeteringSupported: true,
	}); err != nil {
		t.Fatalf("append explicit usage: %v", err)
	}
	report, err := store.Report("g1", "", "u1", 0, time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(report.Rows))
	}
	row := report.Rows[0]
	if row.MonthCredits != 3.5 || row.MonthCostUSD != 0.25 || row.MeteredMonthTurns != 1 {
		t.Fatalf("row = %+v, want explicit credits, cost, and metered turn", row)
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
			Username:      "alice.dev",
			Source:        "message",
			MeteringUsage: []acp.MeteringItem{{Value: 1, Unit: "credit"}},
		},
		{
			Timestamp:     "2026-05-28T10:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			Username:      "alice.dev",
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
	if row.UserID != "u1" || row.Username != "alice.dev" {
		t.Fatalf("row identity = %q/%q, want u1/alice.dev", row.UserID, row.Username)
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

func TestUsageReportSortsByUSDCostWhenCreditsTie(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "Asia/Taipei", 0)
	records := []UsageRecord{
		{
			Timestamp:     "2026-05-28T09:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			UserID:        "low",
			Username:      "low",
			Engine:        "omp",
			MeteringUsage: []acp.MeteringItem{{Value: 0.01, Unit: "USD"}},
		},
		{
			Timestamp:     "2026-05-28T10:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			UserID:        "high",
			Username:      "high",
			Engine:        "omp",
			MeteringUsage: []acp.MeteringItem{{Value: 0.20, Unit: "USD"}},
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
	if len(report.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(report.Rows))
	}
	if report.Rows[0].UserID != "high" {
		t.Fatalf("first row = %+v, want high USD cost first", report.Rows[0])
	}
}

func TestUsageReportTotalsIncludeCreditsAndUSDCost(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "Asia/Taipei", 0)
	records := []UsageRecord{
		{
			Timestamp:     "2026-05-28T09:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			UserID:        "kiro-user",
			Engine:        "kiro",
			MeteringUsage: []acp.MeteringItem{{Value: 1.25, Unit: "credit"}},
		},
		{
			Timestamp:     "2026-05-28T10:00:00+08:00",
			GuildID:       "g1",
			ChannelID:     "c1",
			UserID:        "omp-user",
			Engine:        "omp",
			MeteringUsage: []acp.MeteringItem{{Value: 0.20, Unit: "USD"}},
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
	if report.Totals.MonthCredits != 1.25 || report.Totals.MonthCostUSD != 0.20 || report.Totals.MonthTurns != 2 {
		t.Fatalf("totals = %+v, want credits+usd across both engines", report.Totals)
	}
}

func TestUsageReportKeepsUnattributedRecords(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "Asia/Taipei", 0)
	if err := store.Append(UsageRecord{
		Timestamp:     "2026-05-28T09:00:00+08:00",
		GuildID:       "g1",
		ChannelID:     "c1",
		Engine:        "omp",
		MeteringUsage: []acp.MeteringItem{{Value: 0.10, Unit: "USD"}},
	}); err != nil {
		t.Fatalf("append usage: %v", err)
	}

	report, err := store.Report("g1", "", "", 10, time.Date(2026, 5, 28, 12, 0, 0, 0, store.Location()))
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(report.Rows) != 1 || report.Rows[0].UserID != "" || report.Rows[0].MonthCostUSD != 0.10 {
		t.Fatalf("rows = %+v, want one unattributed USD row", report.Rows)
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
	if err := os.WriteFile(oldPath, []byte(`{"ts":"2026-03-01T10:00:00+08:00","guild_id":"g1","channel_id":"c1","user_id":"old","source":"message","status":"success","credits":1}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepPath, []byte(`{"ts":"2026-05-01T10:00:00+08:00","guild_id":"g1","channel_id":"c1","user_id":"keep","source":"message","status":"success","credits":1}`+"\n"), 0644); err != nil {
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
	page, err := store.QueryHistory(UsageHistoryOptions{GuildID: "g1", UserID: "old", From: time.Date(2026, 3, 1, 0, 0, 0, 0, store.Location()), To: time.Date(2026, 6, 1, 0, 0, 0, 0, store.Location())})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("expired records = %d, want 0", len(page.Records))
	}
	if _, err := os.Stat(filepath.Join(usageDir, "archive", filepath.Base(oldPath))); err != nil {
		t.Fatalf("old legacy archive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(usageDir, "archive", filepath.Base(keepPath))); err != nil {
		t.Fatalf("kept legacy archive: %v", err)
	}
}

func TestUsageLegacyImportArchivesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	usageDir := filepath.Join(dir, "usage")
	if err := os.MkdirAll(usageDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(usageDir, "2026-06.jsonl")
	line := `{"ts":"2026-06-10T01:02:03+08:00","guild_id":"g","channel_id":"c","user_id":"u","source":"message","status":"success","credits":2}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0644); err != nil {
		t.Fatal(err)
	}
	store := NewUsageStore(dir, "Asia/Taipei", 0)
	if err := store.InitError(); err != nil {
		t.Fatal(err)
	}
	page, err := store.QueryHistory(UsageHistoryOptions{GuildID: "g", UserID: "u", From: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("records=%d", len(page.Records))
	}
	_ = store.Close()
	if _, err := os.Stat(filepath.Join(usageDir, "archive", "2026-06.jsonl")); err != nil {
		t.Fatal(err)
	}
	restarted := NewUsageStore(dir, "Asia/Taipei", 0)
	if err := restarted.InitError(); err != nil {
		t.Fatal(err)
	}
	page, err = restarted.QueryHistory(UsageHistoryOptions{GuildID: "g", UserID: "u", From: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("records after restart=%d", len(page.Records))
	}
}

func TestUsageLegacyImportAcceptsNewSameMonthSegmentAfterRollback(t *testing.T) {
	dir := t.TempDir()
	usageDir := filepath.Join(dir, "usage")
	if err := os.MkdirAll(usageDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(usageDir, "2026-06.jsonl")
	first := `{"ts":"2026-06-10T01:02:03Z","guild_id":"g","channel_id":"c","user_id":"u","source":"message","status":"success","credits":2}` + "\n"
	if err := os.WriteFile(path, []byte(first), 0644); err != nil {
		t.Fatal(err)
	}
	store := NewUsageStore(dir, "UTC", 0)
	if err := store.InitError(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate an older binary writing a new monthly JSONL after rollback.
	second := `{"ts":"2026-06-11T01:02:03Z","guild_id":"g","channel_id":"c","user_id":"u","source":"message","status":"success","credits":3}` + "\n"
	if err := os.WriteFile(path, []byte(second), 0644); err != nil {
		t.Fatal(err)
	}
	restarted := NewUsageStore(dir, "UTC", 0)
	if err := restarted.InitError(); err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	page, err := restarted.QueryHistory(UsageHistoryOptions{GuildID: "g", UserID: "u", From: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 {
		t.Fatalf("records after rollback import = %d, want 2", len(page.Records))
	}
	matches, err := filepath.Glob(filepath.Join(usageDir, "archive", "2026-06.*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("versioned archives = %v, want one checksum-suffixed segment", matches)
	}
}

func TestUsageLegacyImportMalformedFileFailsWithoutPartialRows(t *testing.T) {
	dir := t.TempDir()
	usageDir := filepath.Join(dir, "usage")
	if err := os.MkdirAll(usageDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `{"ts":"2026-06-10T01:02:03Z","guild_id":"g","channel_id":"c","user_id":"u","source":"message","status":"success"}` + "\n{" + "\n"
	if err := os.WriteFile(filepath.Join(usageDir, "2026-06.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	store := NewUsageStore(dir, "UTC", 0)
	if store.InitError() == nil {
		t.Fatal("expected malformed import error")
	}
	if _, err := os.Stat(filepath.Join(usageDir, "2026-06.jsonl")); err != nil {
		t.Fatalf("source should remain: %v", err)
	}
}

func TestUsageReportDoesNotLimitAllUsers(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "UTC", 0)
	for i := 0; i < 15; i++ {
		if err := store.Append(UsageRecord{Timestamp: "2026-07-17T10:00:00Z", GuildID: "g", ChannelID: "c", UserID: fmt.Sprintf("u%02d", i), MeteringUsage: []acp.MeteringItem{{Value: float64(i + 1), Unit: "credits"}}}); err != nil {
			t.Fatal(err)
		}
	}
	report, err := store.Report("g", "", "", 0, time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 15 {
		t.Fatalf("rows=%d, want 15", len(report.Rows))
	}
}

func TestUsageHistoryUsesStableKeysetPaginationAndFilters(t *testing.T) {
	store := NewUsageStore(t.TempDir(), "UTC", 0)
	for i := 0; i < 5; i++ {
		source := "message"
		status := "success"
		if i == 4 {
			source = "command:compact"
			status = "error"
		}
		if err := store.Append(UsageRecord{Timestamp: fmt.Sprintf("2026-07-17T10:00:0%dZ", i), GuildID: "g", ChannelID: "c", UserID: "u", Source: source, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	opts := UsageHistoryOptions{GuildID: "g", UserID: "u", From: time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), Limit: 2}
	first, err := store.QueryHistory(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 2 || first.NextTime == "" {
		t.Fatalf("first=%+v", first)
	}
	opts.BeforeTime, opts.BeforeID = first.NextTime, first.NextID
	second, err := store.QueryHistory(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 2 {
		t.Fatalf("second records=%d", len(second.Records))
	}
	if first.Records[1].UsageID == second.Records[0].UsageID {
		t.Fatal("keyset page repeated boundary record")
	}
	filtered, err := store.QueryHistory(UsageHistoryOptions{GuildID: "g", UserID: "u", From: opts.From, To: opts.To, Status: "error", Source: "command", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Records) != 1 || filtered.Records[0].Source != "command:compact" {
		t.Fatalf("filtered=%+v", filtered.Records)
	}
}
