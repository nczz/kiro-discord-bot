package heartbeat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jsuar/go-cron-descriptor/pkg/crondescriptor"
	"github.com/robfig/cron/v3"

	L "github.com/nczz/kiro-discord-bot/locale"
)

var (
	cronFieldRe               = regexp.MustCompile(`^[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+$`)
	relativeDurationRe        = regexp.MustCompile(`^\+(\d+)\s*([mh])$`)
	relativeMinutesRe         = regexp.MustCompile(`^(\d+)\s*(?:分鐘|分|min)(?:後|later)?$`)
	relativeHoursRe           = regexp.MustCompile(`^(\d+)\s*(?:小時|hour|hr)(?:後|later)?$`)
	fullDateTimeRe            = regexp.MustCompile(`^(\d{4})[-/](\d{1,2})[-/](\d{1,2})[ T]+(\d{1,2}):(\d{2})$`)
	fullChineseDateTimeRe     = regexp.MustCompile(`^(\d{4})年(\d{1,2})月(\d{1,2})日?\s*(\d{1,2}):(\d{2})$`)
	monthDayDateTimeRe        = regexp.MustCompile(`^(\d{1,2})[-/](\d{1,2})[日號]?\s+(\d{1,2}):(\d{2})$`)
	monthDayChineseDateTimeRe = regexp.MustCompile(`^(\d{1,2})月(\d{1,2})日?\s*(\d{1,2}):(\d{2})$`)
	timeOfDayRe               = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
)

// ParseSchedule validates a cron expression (5 fields). Returns the expression or error.
func ParseSchedule(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", fmt.Errorf("empty schedule")
	}
	if !cronFieldRe.MatchString(s) {
		return "", fmt.Errorf("%s", L.Get("error.not_cron_format"))
	}
	// Validate with cron parser
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(s); err != nil {
		return "", fmt.Errorf("%s", L.Getf("error.invalid_cron", err.Error()))
	}
	return s, nil
}

// DescribeSchedule returns a human-readable description of a cron expression.
func DescribeSchedule(cronExpr string) string {
	cd, err := crondescriptor.NewCronDescriptor(cronExpr)
	if err != nil {
		return cronExpr
	}
	desc, err := cd.GetDescription(crondescriptor.Full)
	if err != nil || desc == nil {
		return cronExpr
	}
	return *desc
}

// ParseTime converts canonical absolute dates, relative durations, or HH:MM to absolute time.
// Supported: RFC3339, "YYYY-MM-DD HH:MM", "M/D HH:MM", "M月D日 HH:MM", "+30m", "+2h", "HH:MM", "明天 HH:MM", "tomorrow HH:MM".
func ParseTime(input string, loc *time.Location) (time.Time, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	now := time.Now().In(loc)

	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return futureAbsoluteTime(t.In(loc), now)
	}

	if m := fullDateTimeRe.FindStringSubmatch(s); m != nil {
		return parseAbsoluteDateTime(m[1], m[2], m[3], m[4], m[5], loc, now, false)
	}
	if m := fullChineseDateTimeRe.FindStringSubmatch(s); m != nil {
		return parseAbsoluteDateTime(m[1], m[2], m[3], m[4], m[5], loc, now, false)
	}
	if m := monthDayDateTimeRe.FindStringSubmatch(s); m != nil {
		return parseAbsoluteDateTime("", m[1], m[2], m[3], m[4], loc, now, true)
	}
	if m := monthDayChineseDateTimeRe.FindStringSubmatch(s); m != nil {
		return parseAbsoluteDateTime("", m[1], m[2], m[3], m[4], loc, now, true)
	}

	// +Nm / +Nh duration format
	if m := relativeDurationRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		if m[2] == "h" {
			return now.Add(time.Duration(n) * time.Hour), nil
		}
		return now.Add(time.Duration(n) * time.Minute), nil
	}

	// N分鐘後 / N小時後
	if m := relativeMinutesRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return now.Add(time.Duration(n) * time.Minute), nil
	}
	if m := relativeHoursRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return now.Add(time.Duration(n) * time.Hour), nil
	}

	// [明天|tomorrow] HH:MM
	tomorrow := false
	work := s
	if strings.HasPrefix(work, "明天") {
		tomorrow = true
		work = strings.TrimSpace(strings.TrimPrefix(work, "明天"))
	} else if strings.HasPrefix(strings.ToLower(work), "tomorrow") {
		tomorrow = true
		work = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(work), "tomorrow"))
	}

	if m := timeOfDayRe.FindStringSubmatch(work); m != nil {
		hour, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		target, ok := buildTime(now.Year(), int(now.Month()), now.Day(), hour, min, loc)
		if !ok {
			return time.Time{}, fmt.Errorf("%s", L.Get("error.parse_time_help"))
		}
		if tomorrow {
			target = target.AddDate(0, 0, 1)
		} else if target.Before(now) {
			target = target.AddDate(0, 0, 1)
		}
		return target, nil
	}

	return time.Time{}, fmt.Errorf("%s", L.Get("error.parse_time_help"))
}

func parseAbsoluteDateTime(yearStr, monthStr, dayStr, hourStr, minStr string, loc *time.Location, now time.Time, rollYear bool) (time.Time, error) {
	year := now.Year()
	if yearStr != "" {
		var err error
		year, err = strconv.Atoi(yearStr)
		if err != nil {
			return time.Time{}, fmt.Errorf("%s", L.Get("error.parse_time_help"))
		}
	}
	month, _ := strconv.Atoi(monthStr)
	day, _ := strconv.Atoi(dayStr)
	hour, _ := strconv.Atoi(hourStr)
	min, _ := strconv.Atoi(minStr)
	target, ok := buildTime(year, month, day, hour, min, loc)
	if !ok {
		return time.Time{}, fmt.Errorf("%s", L.Get("error.parse_time_help"))
	}
	if rollYear && target.Before(now) {
		target = target.AddDate(1, 0, 0)
	}
	return futureAbsoluteTime(target, now)
}

func futureAbsoluteTime(target, now time.Time) (time.Time, error) {
	if target.Before(now) {
		return time.Time{}, fmt.Errorf("%s", L.Get("error.time_in_past"))
	}
	return target, nil
}

func buildTime(year, month, day, hour, min int, loc *time.Location) (time.Time, bool) {
	if month < 1 || month > 12 || day < 1 || hour < 0 || hour > 23 || min < 0 || min > 59 {
		return time.Time{}, false
	}
	target := time.Date(year, time.Month(month), day, hour, min, 0, 0, loc)
	if target.Year() != year || int(target.Month()) != month || target.Day() != day || target.Hour() != hour || target.Minute() != min {
		return time.Time{}, false
	}
	return target, true
}
