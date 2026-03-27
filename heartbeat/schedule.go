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

var cronFieldRe = regexp.MustCompile(`^[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+$`)

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

// ParseTime converts relative duration or HH:MM to absolute time.
// Supported: "+30m", "+2h", "HH:MM", "明天 HH:MM", "tomorrow HH:MM"
func ParseTime(input string, loc *time.Location) (time.Time, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	now := time.Now().In(loc)

	// +Nm / +Nh duration format
	if m := regexp.MustCompile(`^\+(\d+)\s*([mh])$`).FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		if m[2] == "h" {
			return now.Add(time.Duration(n) * time.Hour), nil
		}
		return now.Add(time.Duration(n) * time.Minute), nil
	}

	// N分鐘後 / N小時後
	if m := regexp.MustCompile(`^(\d+)\s*(?:分鐘|分|min)(?:後|later)?$`).FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return now.Add(time.Duration(n) * time.Minute), nil
	}
	if m := regexp.MustCompile(`^(\d+)\s*(?:小時|hour|hr)(?:後|later)?$`).FindStringSubmatch(s); m != nil {
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

	if m := regexp.MustCompile(`^(\d{1,2}):(\d{2})$`).FindStringSubmatch(work); m != nil {
		hour, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		target := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, loc)
		if tomorrow {
			target = target.AddDate(0, 0, 1)
		} else if target.Before(now) {
			target = target.AddDate(0, 0, 1)
		}
		return target, nil
	}

	return time.Time{}, fmt.Errorf("%s", L.Get("error.parse_time_help"))
}
