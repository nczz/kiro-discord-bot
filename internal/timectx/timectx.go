package timectx

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultWeekStart       = "monday"
	DefaultMonthWeekPolicy = "calendar_row_clipped_to_month"
)

// Context is the agent-visible current datetime in the bot business timezone.
type Context struct {
	Status             string `json:"status"`
	Timezone           string `json:"timezone"`
	TimezoneSource     string `json:"timezone_source"`
	Now                string `json:"now"`
	Unix               int64  `json:"unix"`
	Date               string `json:"date"`
	Time               string `json:"time"`
	Hour24             int    `json:"hour_24"`
	Minute             int    `json:"minute"`
	Weekday            string `json:"weekday"`
	WeekdayZh          string `json:"weekday_zh"`
	DayPeriod          string `json:"day_period"`
	DayPeriodZh        string `json:"day_period_zh"`
	Today              string `json:"today"`
	Yesterday          string `json:"yesterday"`
	Tomorrow           string `json:"tomorrow"`
	WeekStart          string `json:"week_start"`
	WeekStartWeekdayZh string `json:"week_start_weekday_zh"`
	WeekEndInclusive   string `json:"week_end_inclusive"`
	WeekEndWeekdayZh   string `json:"week_end_weekday_zh"`
}

type RangeRequest struct {
	ReferenceTime   string `json:"reference_time,omitempty"`
	RangeType       string `json:"range_type,omitempty"`
	Offset          int    `json:"offset,omitempty"`
	WeekIndex       int    `json:"week_index,omitempty"`
	Weekday         string `json:"weekday,omitempty"`
	WeekStart       string `json:"week_start,omitempty"`
	MonthWeekPolicy string `json:"month_week_policy,omitempty"`
	Date            string `json:"date,omitempty"`
	Days            int    `json:"days,omitempty"`
	Direction       string `json:"direction,omitempty"`
	IncludeToday    *bool  `json:"include_today,omitempty"`
}

type RangeResult struct {
	Status           string              `json:"status"`
	Timezone         string              `json:"timezone"`
	TimezoneSource   string              `json:"timezone_source"`
	ReferenceTime    string              `json:"reference_time"`
	ReferenceDate    string              `json:"reference_date"`
	ResolvedText     string              `json:"resolved_text,omitempty"`
	StartDate        string              `json:"start_date,omitempty"`
	StartDateTime    string              `json:"start_datetime,omitempty"`
	StartWeekday     string              `json:"start_weekday,omitempty"`
	StartWeekdayZh   string              `json:"start_weekday_zh,omitempty"`
	EndDateInclusive string              `json:"end_date_inclusive,omitempty"`
	EndDateTime      string              `json:"end_datetime,omitempty"`
	EndWeekday       string              `json:"end_weekday,omitempty"`
	EndWeekdayZh     string              `json:"end_weekday_zh,omitempty"`
	EndExclusive     string              `json:"end_exclusive,omitempty"`
	Policy           map[string]string   `json:"policy,omitempty"`
	Ambiguity        *Ambiguity          `json:"ambiguity,omitempty"`
	Alternatives     []AlternativeResult `json:"alternatives,omitempty"`
	Error            string              `json:"error,omitempty"`
	Instruction      string              `json:"instruction,omitempty"`
}

type Ambiguity struct {
	Reason      string `json:"reason"`
	Instruction string `json:"instruction"`
}

type AlternativeResult struct {
	Policy           string `json:"policy"`
	StartDate        string `json:"start_date"`
	EndDateInclusive string `json:"end_date_inclusive"`
}

func Current(now time.Time, configuredTZ string) (Context, error) {
	loc, name, source, err := Location(configuredTZ)
	if err != nil {
		return Context{}, err
	}
	local := now.In(loc)
	day := startOfDay(local)
	weekStart := startOfWeek(day, time.Monday)
	weekEnd := weekStart.AddDate(0, 0, 6)
	period, periodZh := dayPeriod(local)
	return Context{
		Status:             "ok",
		Timezone:           name,
		TimezoneSource:     source,
		Now:                local.Format(time.RFC3339),
		Unix:               local.Unix(),
		Date:               day.Format(time.DateOnly),
		Time:               local.Format(time.TimeOnly),
		Hour24:             local.Hour(),
		Minute:             local.Minute(),
		Weekday:            local.Weekday().String(),
		WeekdayZh:          weekdayZh(local.Weekday()),
		DayPeriod:          period,
		DayPeriodZh:        periodZh,
		Today:              day.Format(time.DateOnly),
		Yesterday:          day.AddDate(0, 0, -1).Format(time.DateOnly),
		Tomorrow:           day.AddDate(0, 0, 1).Format(time.DateOnly),
		WeekStart:          weekStart.Format(time.DateOnly),
		WeekStartWeekdayZh: weekdayZh(weekStart.Weekday()),
		WeekEndInclusive:   weekEnd.Format(time.DateOnly),
		WeekEndWeekdayZh:   weekdayZh(weekEnd.Weekday()),
	}, nil
}

func PromptBlock(now time.Time, configuredTZ string) string {
	ctx, err := Current(now, configuredTZ)
	if err != nil {
		return fmt.Sprintf("[Current datetime]\ntimezone=%s\ntimezone_source=CRON_TIMEZONE_INVALID\nerror=%s\n\n", strings.TrimSpace(configuredTZ), err)
	}
	return fmt.Sprintf(`[Current datetime]
timezone=%s
timezone_source=%s
now=%s
unix=%d
date=%s
time=%s
hour_24=%d
minute=%d
weekday=%s
weekday_zh=%s
day_period=%s
day_period_zh=%s
today=%s
yesterday=%s
tomorrow=%s
week_start=%s
week_end_inclusive=%s

`, ctx.Timezone, ctx.TimezoneSource, ctx.Now, ctx.Unix, ctx.Date, ctx.Time, ctx.Hour24, ctx.Minute, ctx.Weekday, ctx.WeekdayZh, ctx.DayPeriod, ctx.DayPeriodZh, ctx.Today, ctx.Yesterday, ctx.Tomorrow, ctx.WeekStart, ctx.WeekEndInclusive)
}

func JSON(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"status":"error","error":%q}`, err.Error())
	}
	return string(raw)
}

func Location(configuredTZ string) (*time.Location, string, string, error) {
	configuredTZ = strings.TrimSpace(configuredTZ)
	if configuredTZ != "" {
		loc, err := time.LoadLocation(configuredTZ)
		if err != nil {
			return nil, configuredTZ, "CRON_TIMEZONE", fmt.Errorf("load CRON_TIMEZONE %q: %w", configuredTZ, err)
		}
		return loc, configuredTZ, "CRON_TIMEZONE", nil
	}
	name := strings.TrimSpace(time.Local.String())
	if name == "" || name == "Local" {
		name = "service process local timezone"
	}
	return time.Local, name, "service_process_local", nil
}

func ResolveDateRange(req RangeRequest, now time.Time, configuredTZ string) (RangeResult, error) {
	loc, tzName, tzSource, err := Location(configuredTZ)
	if err != nil {
		return RangeResult{Status: "error", Timezone: tzName, TimezoneSource: tzSource, Error: err.Error()}, nil
	}
	ref, err := referenceTime(req.ReferenceTime, now, loc)
	if err != nil {
		return RangeResult{Status: "error", Timezone: tzName, TimezoneSource: tzSource, Error: err.Error()}, nil
	}
	res := baseResult("ok", ref, tzName, tzSource, defaultPolicy(req), "")
	if strings.TrimSpace(req.RangeType) == "" {
		res.Status = "error"
		res.Error = "range_type is required"
		res.Instruction = "Translate the user's date phrase into structured range_type fields and retry; do not pass natural language or calculate dates mentally."
		return res, nil
	}
	start, end, label, err := computeRange(req, ref.In(loc), loc)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		return res, nil
	}
	res.ResolvedText = label
	fillRange(&res, start, end)
	if strings.TrimSpace(req.MonthWeekPolicy) == "" && strings.EqualFold(req.RangeType, "month_week") {
		res.Ambiguity = &Ambiguity{Reason: "nth week of a month has multiple common interpretations", Instruction: "Use this default result for ordinary business questions. Ask for clarification before scheduling, billing, finance, legal, or irreversible actions."}
		res.Alternatives = monthWeekAlternatives(req, ref.In(loc), loc)
	}
	return res, nil
}

func baseResult(status string, ref time.Time, tz, source string, policy map[string]string, errText string) RangeResult {
	return RangeResult{Status: status, Timezone: tz, TimezoneSource: source, ReferenceTime: ref.Format(time.RFC3339), ReferenceDate: startOfDay(ref).Format(time.DateOnly), Policy: policy, Error: errText}
}

func fillRange(res *RangeResult, start, end time.Time) {
	res.StartDate = start.Format(time.DateOnly)
	res.StartWeekday = start.Weekday().String()
	res.StartWeekdayZh = weekdayZh(start.Weekday())
	res.EndDateInclusive = end.Format(time.DateOnly)
	res.EndWeekday = end.Weekday().String()
	res.EndWeekdayZh = weekdayZh(end.Weekday())
	res.EndExclusive = end.AddDate(0, 0, 1).Format(time.DateOnly)
}

func computeRange(req RangeRequest, ref time.Time, loc *time.Location) (time.Time, time.Time, string, error) {
	typ := strings.ToLower(strings.TrimSpace(req.RangeType))
	day := startOfDay(ref)
	switch typ {
	case "day":
		if req.Date != "" {
			d, err := parseDate(req.Date, loc)
			if err != nil {
				return time.Time{}, time.Time{}, "", err
			}
			return d, d, d.Format(time.DateOnly), nil
		}
		d := day.AddDate(0, 0, req.Offset)
		return d, d, d.Format(time.DateOnly), nil
	case "week":
		start := startOfWeek(day, parseWeekStart(req.WeekStart)).AddDate(0, 0, req.Offset*7)
		return start, start.AddDate(0, 0, 6), "week", nil
	case "month":
		start := time.Date(day.Year(), day.Month()+time.Month(req.Offset), 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, -1), "month", nil
	case "month_week":
		if req.WeekIndex <= 0 {
			return time.Time{}, time.Time{}, "", fmt.Errorf("week_index must be greater than 0")
		}
		return monthWeek(day, loc, req.Offset, req.WeekIndex, parseWeekStart(req.WeekStart), monthWeekPolicy(req.MonthWeekPolicy))
	case "quarter":
		qStartMonth := time.Month(((int(day.Month())-1)/3)*3 + 1)
		start := time.Date(day.Year(), qStartMonth, 1, 0, 0, 0, 0, loc).AddDate(0, req.Offset*3, 0)
		return start, start.AddDate(0, 3, -1), "quarter", nil
	case "year":
		start := time.Date(day.Year()+req.Offset, 1, 1, 0, 0, 0, 0, loc)
		return start, time.Date(start.Year(), 12, 31, 0, 0, 0, 0, loc), "year", nil
	case "relative_days":
		if req.Days <= 0 {
			return time.Time{}, time.Time{}, "", fmt.Errorf("days must be greater than 0")
		}
		includeToday := true
		if req.IncludeToday != nil {
			includeToday = *req.IncludeToday
		}
		direction := strings.ToLower(strings.TrimSpace(req.Direction))
		switch direction {
		case "", "past":
			end := day
			if !includeToday {
				end = end.AddDate(0, 0, -1)
			}
			return end.AddDate(0, 0, -(req.Days - 1)), end, fmt.Sprintf("past %d days", req.Days), nil
		case "future":
			start := day
			if !includeToday {
				start = start.AddDate(0, 0, 1)
			}
			return start, start.AddDate(0, 0, req.Days-1), fmt.Sprintf("future %d days", req.Days), nil
		default:
			return time.Time{}, time.Time{}, "", fmt.Errorf("unsupported relative_days direction %q", req.Direction)
		}
	case "specific_weekday":
		wd, ok := parseWeekday(req.Weekday)
		if !ok {
			return time.Time{}, time.Time{}, "", fmt.Errorf("unsupported weekday %q", req.Weekday)
		}
		start := startOfWeek(day, parseWeekStart(req.WeekStart)).AddDate(0, 0, req.Offset*7)
		d := start.AddDate(0, 0, weekdayOffset(parseWeekStart(req.WeekStart), wd))
		return d, d, d.Format(time.DateOnly), nil
	default:
		return time.Time{}, time.Time{}, "", fmt.Errorf("unsupported range_type %q", req.RangeType)
	}
}

func defaultPolicy(req RangeRequest) map[string]string {
	return map[string]string{
		"timezone":            "CRON_TIMEZONE",
		"week_start":          strings.ToLower(defaultString(req.WeekStart, DefaultWeekStart)),
		"month_week_policy":   monthWeekPolicy(req.MonthWeekPolicy),
		"structured_args":     "agents must translate user date phrases into range_type fields; this tool does not parse arbitrary natural language",
		"relative_days":       "inclusive date range in the resolved timezone unless include_today=false is explicit",
		"high_risk_ambiguity": "ask before scheduling, billing, finance, legal, or irreversible actions when ambiguity is reported",
	}
}

func monthWeekAlternatives(req RangeRequest, ref time.Time, loc *time.Location) []AlternativeResult {
	policies := []string{"full_weeks_only", "day_blocks_1_7"}
	out := make([]AlternativeResult, 0, len(policies))
	for _, policy := range policies {
		altReq := req
		altReq.MonthWeekPolicy = policy
		start, end, _, err := computeRange(altReq, ref, loc)
		if err != nil {
			continue
		}
		out = append(out, AlternativeResult{Policy: policy, StartDate: start.Format(time.DateOnly), EndDateInclusive: end.Format(time.DateOnly)})
	}
	return out
}

func monthWeek(ref time.Time, loc *time.Location, monthOffset, weekIndex int, weekStart time.Weekday, policy string) (time.Time, time.Time, string, error) {
	monthStart := time.Date(ref.Year(), ref.Month()+time.Month(monthOffset), 1, 0, 0, 0, 0, loc)
	monthEnd := monthStart.AddDate(0, 1, -1)
	switch policy {
	case "calendar_row_clipped_to_month":
		rowStart := startOfWeek(monthStart, weekStart).AddDate(0, 0, (weekIndex-1)*7)
		rowEnd := rowStart.AddDate(0, 0, 6)
		if rowStart.Before(monthStart) {
			rowStart = monthStart
		}
		if rowEnd.After(monthEnd) {
			rowEnd = monthEnd
		}
		if rowStart.After(monthEnd) {
			return time.Time{}, time.Time{}, "", fmt.Errorf("month has no week_index %d under %s", weekIndex, policy)
		}
		return rowStart, rowEnd, fmt.Sprintf("month week %d", weekIndex), nil
	case "full_weeks_only":
		first := startOfWeek(monthStart, weekStart)
		if first.Before(monthStart) {
			first = first.AddDate(0, 0, 7)
		}
		start := first.AddDate(0, 0, (weekIndex-1)*7)
		end := start.AddDate(0, 0, 6)
		if end.After(monthEnd) {
			return time.Time{}, time.Time{}, "", fmt.Errorf("month has no full week_index %d", weekIndex)
		}
		return start, end, fmt.Sprintf("full month week %d", weekIndex), nil
	case "day_blocks_1_7":
		startDay := (weekIndex-1)*7 + 1
		if startDay > monthEnd.Day() {
			return time.Time{}, time.Time{}, "", fmt.Errorf("month has no 7-day block week_index %d", weekIndex)
		}
		start := time.Date(monthStart.Year(), monthStart.Month(), startDay, 0, 0, 0, 0, loc)
		endDay := startDay + 6
		if endDay > monthEnd.Day() {
			endDay = monthEnd.Day()
		}
		end := time.Date(monthStart.Year(), monthStart.Month(), endDay, 0, 0, 0, 0, loc)
		return start, end, fmt.Sprintf("month day block %d", weekIndex), nil
	default:
		return time.Time{}, time.Time{}, "", fmt.Errorf("unsupported month_week_policy %q", policy)
	}
}

func referenceTime(raw string, now time.Time, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return now.In(loc), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.In(loc), nil
	}
	return parseDate(raw, loc)
}

func parseDate(raw string, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.DateOnly, "2006/01/02", "2006/1/2", "2006-1-2"} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return startOfDay(t), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date %q", raw)
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func startOfWeek(t time.Time, start time.Weekday) time.Time {
	t = startOfDay(t)
	delta := (int(t.Weekday()) - int(start) + 7) % 7
	return t.AddDate(0, 0, -delta)
}

func weekdayOffset(start, target time.Weekday) int {
	return (int(target) - int(start) + 7) % 7
}

func weekdayZh(w time.Weekday) string {
	switch w {
	case time.Monday:
		return "星期一"
	case time.Tuesday:
		return "星期二"
	case time.Wednesday:
		return "星期三"
	case time.Thursday:
		return "星期四"
	case time.Friday:
		return "星期五"
	case time.Saturday:
		return "星期六"
	case time.Sunday:
		return "星期日"
	default:
		return ""
	}
}

func dayPeriod(t time.Time) (string, string) {
	switch h := t.Hour(); {
	case h < 6:
		return "early_morning", "凌晨"
	case h < 9:
		return "morning", "早上"
	case h < 12:
		return "forenoon", "上午"
	case h < 13:
		return "noon", "中午"
	case h < 18:
		return "afternoon", "下午"
	default:
		return "evening", "晚上"
	}
}

func parseWeekStart(raw string) time.Weekday {
	wd, ok := parseWeekday(defaultString(raw, DefaultWeekStart))
	if !ok {
		return time.Monday
	}
	return wd
}

func parseWeekday(raw string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "monday", "mon", "一", "週一", "周一", "星期一", "禮拜一":
		return time.Monday, true
	case "tuesday", "tue", "二", "週二", "周二", "星期二", "禮拜二":
		return time.Tuesday, true
	case "wednesday", "wed", "三", "週三", "周三", "星期三", "禮拜三":
		return time.Wednesday, true
	case "thursday", "thu", "四", "週四", "周四", "星期四", "禮拜四":
		return time.Thursday, true
	case "friday", "fri", "五", "週五", "周五", "星期五", "禮拜五":
		return time.Friday, true
	case "saturday", "sat", "六", "週六", "周六", "星期六", "禮拜六":
		return time.Saturday, true
	case "sunday", "sun", "日", "天", "週日", "周日", "星期日", "星期天", "禮拜日", "禮拜天":
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}

func monthWeekPolicy(raw string) string {
	if raw = strings.ToLower(strings.TrimSpace(raw)); raw != "" {
		return raw
	}
	return DefaultMonthWeekPolicy
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
