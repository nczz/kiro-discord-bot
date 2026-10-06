package usagerange

import (
	"testing"
	"time"
)

func TestResolveRelativeAndCalendarPeriods(t *testing.T) {
	loc := time.FixedZone("TST", 8*60*60)
	now := time.Date(2026, 10, 6, 15, 4, 0, 0, loc)
	got, err := Resolve(Options{Period: "90d", Now: now, Loc: loc})
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "90d" || !got.From.Equal(now.AddDate(0, 0, -90)) || !got.To.Equal(now) || got.Custom {
		t.Fatalf("90d range = %+v", got)
	}
	month, err := Resolve(Options{Period: "this-month", Now: now, Loc: loc})
	if err != nil {
		t.Fatal(err)
	}
	if month.From.Format("2006-01-02 15:04") != "2026-10-01 00:00" || !month.To.Equal(now) {
		t.Fatalf("this-month range = %+v", month)
	}
}

func TestResolveCustomRange(t *testing.T) {
	loc := time.FixedZone("TST", 8*60*60)
	now := time.Date(2026, 10, 6, 15, 4, 0, 0, loc)
	got, err := Resolve(Options{From: "2026-08-01", To: "2026-08-31", Now: now, Loc: loc})
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "custom" || !got.Custom {
		t.Fatalf("custom range metadata = %+v", got)
	}
	if got.From.Format(time.RFC3339Nano) != "2026-08-01T00:00:00+08:00" {
		t.Fatalf("from = %s", got.From.Format(time.RFC3339Nano))
	}
	if got.To.Format(time.RFC3339Nano) != "2026-08-31T23:59:59.999999999+08:00" {
		t.Fatalf("to = %s", got.To.Format(time.RFC3339Nano))
	}
	overrodePeriod, err := Resolve(Options{Period: "forever", From: "2026-08-01 09:30", Now: now, Loc: loc})
	if err != nil {
		t.Fatal(err)
	}
	if overrodePeriod.Label != "custom" || overrodePeriod.To != now || overrodePeriod.From.Format("2006-01-02 15:04") != "2026-08-01 09:30" {
		t.Fatalf("custom override range = %+v", overrodePeriod)
	}
}

func TestResolveRejectsInvalidRanges(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 10, 6, 15, 4, 0, 0, loc)
	tests := []struct {
		name string
		opts Options
		code ErrorCode
	}{
		{"invalid period", Options{Period: "forever", Now: now, Loc: loc}, ErrInvalidPeriod},
		{"too many relative days", Options{Period: "3651d", Now: now, Loc: loc}, ErrPeriodTooLarge},
		{"to without from", Options{To: "2026-08-31", Now: now, Loc: loc}, ErrFromRequired},
		{"invalid from", Options{From: "08/01/2026", Now: now, Loc: loc}, ErrInvalidFrom},
		{"invalid to", Options{From: "2026-08-01", To: "08/31/2026", Now: now, Loc: loc}, ErrInvalidTo},
		{"reversed", Options{From: "2026-09-01", To: "2026-08-31", Now: now, Loc: loc}, ErrReversedRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(tt.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			rangeErr, ok := err.(*RangeError)
			if !ok || rangeErr.Code != tt.code {
				t.Fatalf("err = %#v, want code %s", err, tt.code)
			}
		})
	}
}
