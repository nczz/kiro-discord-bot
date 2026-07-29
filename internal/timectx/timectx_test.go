package timectx

import (
	"strings"
	"testing"
	"time"
)

func TestCurrentUsesConfiguredTimezoneAcrossUTCMidnight(t *testing.T) {
	now := time.Date(2026, 7, 27, 16, 30, 0, 0, time.UTC)
	ctx, err := Current(now, "Asia/Taipei")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if ctx.Date != "2026-07-28" || ctx.WeekdayZh != "星期二" {
		t.Fatalf("date/weekday = %s/%s, want 2026-07-28/星期二", ctx.Date, ctx.WeekdayZh)
	}
	if ctx.DayPeriodZh != "凌晨" || ctx.Hour24 != 0 || ctx.Minute != 30 {
		t.Fatalf("period/time = %s %02d:%02d, want 凌晨 00:30", ctx.DayPeriodZh, ctx.Hour24, ctx.Minute)
	}
	if ctx.WeekStart != "2026-07-27" || ctx.WeekEndInclusive != "2026-08-02" {
		t.Fatalf("week range = %s..%s, want 2026-07-27..2026-08-02", ctx.WeekStart, ctx.WeekEndInclusive)
	}
}

func TestPromptBlockPinsCurrentDatetimeRules(t *testing.T) {
	now := time.Date(2026, 7, 27, 16, 30, 0, 0, time.UTC)
	got := PromptBlock(now, "Asia/Taipei")
	for _, want := range []string{
		"[Current datetime]",
		"timezone=Asia/Taipei",
		"timezone_source=CRON_TIMEZONE",
		"date=2026-07-28",
		"time=00:30:00",
		"weekday_zh=星期二",
		"day_period_zh=凌晨",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("PromptBlock missing %q:\n%s", want, got)
		}
	}
}

func TestResolveDateRangeMonthWeekDefaultAndAlternatives(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	res, err := ResolveDateRange(RangeRequest{RangeType: "month_week", Offset: 1, WeekIndex: 2}, now, "Asia/Taipei")
	if err != nil {
		t.Fatalf("ResolveDateRange: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("status = %s error=%s", res.Status, res.Error)
	}
	if res.StartDate != "2026-08-03" || res.EndDateInclusive != "2026-08-09" {
		t.Fatalf("range = %s..%s, want 2026-08-03..2026-08-09", res.StartDate, res.EndDateInclusive)
	}
	if res.StartWeekdayZh != "星期一" || res.EndWeekdayZh != "星期日" {
		t.Fatalf("weekdays = %s..%s", res.StartWeekdayZh, res.EndWeekdayZh)
	}
	if res.Ambiguity == nil || len(res.Alternatives) != 2 {
		t.Fatalf("expected ambiguity and alternatives, got ambiguity=%+v alternatives=%+v", res.Ambiguity, res.Alternatives)
	}
}

func TestResolveDateRangeSpecificWeekday(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	res, err := ResolveDateRange(RangeRequest{RangeType: "specific_weekday", Offset: 1, Weekday: "monday"}, now, "Asia/Taipei")
	if err != nil {
		t.Fatalf("ResolveDateRange: %v", err)
	}
	if res.StartDate != "2026-08-03" || res.StartWeekdayZh != "星期一" {
		t.Fatalf("specific weekday = %s %s, want 2026-08-03 星期一", res.StartDate, res.StartWeekdayZh)
	}
}

func TestResolveDateRangeUsesStructuredDayOffset(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	res, err := ResolveDateRange(RangeRequest{RangeType: "day", Offset: 1}, now, "Asia/Taipei")
	if err != nil {
		t.Fatalf("ResolveDateRange: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("status = %s error=%s", res.Status, res.Error)
	}
	if res.StartDate != "2026-07-29" || res.ResolvedText != "2026-07-29" {
		t.Fatalf("structured day result = date:%s text:%s, want 2026-07-29", res.StartDate, res.ResolvedText)
	}
}

func TestResolveDateRangeRejectsNaturalLanguageWithoutRangeType(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	res, err := ResolveDateRange(RangeRequest{}, now, "Asia/Taipei")
	if err != nil {
		t.Fatalf("ResolveDateRange: %v", err)
	}
	if res.Status != "error" || res.Error != "range_type is required" {
		t.Fatalf("result = %+v, want range_type required error", res)
	}
	if !strings.Contains(res.Instruction, "structured range_type fields") || !strings.Contains(res.Instruction, "do not pass natural language") {
		t.Fatalf("missing structured-only instruction: %+v", res)
	}
}

func TestResolveDateRangeIgnoresRemovedTimezoneOverride(t *testing.T) {
	now := time.Date(2026, 7, 27, 16, 30, 0, 0, time.UTC)
	res, err := ResolveDateRange(RangeRequest{RangeType: "day", Offset: 1}, now, "Asia/Taipei")
	if err != nil {
		t.Fatalf("ResolveDateRange: %v", err)
	}
	if res.Timezone != "Asia/Taipei" || res.TimezoneSource != "CRON_TIMEZONE" {
		t.Fatalf("timezone = %s/%s, want Asia/Taipei/CRON_TIMEZONE", res.Timezone, res.TimezoneSource)
	}
	if res.StartDate != "2026-07-29" {
		t.Fatalf("start date = %s, want 2026-07-29", res.StartDate)
	}
}

func TestResolveDateRangeAgentUtilityScenarios(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	excludeToday := false
	tests := []struct {
		name      string
		userText  string
		req       RangeRequest
		wantStart string
		wantEnd   string
	}{
		{
			name:      "tomorrow",
			userText:  "明天是幾號",
			req:       RangeRequest{RangeType: "day", Offset: 1},
			wantStart: "2026-07-29",
			wantEnd:   "2026-07-29",
		},
		{
			name:      "next month second week",
			userText:  "下個月第二週",
			req:       RangeRequest{RangeType: "month_week", Offset: 1, WeekIndex: 2},
			wantStart: "2026-08-03",
			wantEnd:   "2026-08-09",
		},
		{
			name:      "past seven days excluding today",
			userText:  "過去7天，不含今天",
			req:       RangeRequest{RangeType: "relative_days", Days: 7, Direction: "past", IncludeToday: &excludeToday},
			wantStart: "2026-07-21",
			wantEnd:   "2026-07-27",
		},
		{
			name:      "next monday",
			userText:  "下週一",
			req:       RangeRequest{RangeType: "specific_weekday", Offset: 1, Weekday: "monday"},
			wantStart: "2026-08-03",
			wantEnd:   "2026-08-03",
		},
		{
			name:      "current month",
			userText:  "本月",
			req:       RangeRequest{RangeType: "month"},
			wantStart: "2026-07-01",
			wantEnd:   "2026-07-31",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := ResolveDateRange(tt.req, now, "Asia/Taipei")
			if err != nil {
				t.Fatalf("%s ResolveDateRange: %v", tt.userText, err)
			}
			if res.Status != "ok" {
				t.Fatalf("%s status = %s error=%s", tt.userText, res.Status, res.Error)
			}
			if res.StartDate != tt.wantStart || res.EndDateInclusive != tt.wantEnd {
				t.Fatalf("%s range = %s..%s, want %s..%s", tt.userText, res.StartDate, res.EndDateInclusive, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestResolveDateRangeRejectsInvalidRelativeDaysDirection(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	res, err := ResolveDateRange(RangeRequest{RangeType: "relative_days", Days: 3, Direction: "forward"}, now, "Asia/Taipei")
	if err != nil {
		t.Fatalf("ResolveDateRange: %v", err)
	}
	if res.Status != "error" || !strings.Contains(res.Error, "unsupported relative_days direction") {
		t.Fatalf("result = %+v, want unsupported direction error", res)
	}
}
