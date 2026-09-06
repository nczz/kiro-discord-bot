package heartbeat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	monitorEveryMinutesZHRe = regexp.MustCompile(`^(?:每|每隔)\s*(\d{1,2})\s*(?:分鐘|分钟|分)$`)
	monitorEveryMinutesENRe = regexp.MustCompile(`^every\s+(\d{1,2})\s+minutes?$`)
	monitorTimeColonRe      = regexp.MustCompile(`(\d{1,2})\s*[:：]\s*(\d{2})`)
	monitorTimeZhHourRe     = regexp.MustCompile(`(\d{1,2})\s*(?:點|点)(半)?`)
	monitorTimeAMPMRe       = regexp.MustCompile(`(?i)(\d{1,2})(?:\s*[:：]\s*(\d{2}))?\s*(am|pm)`)
	monitorTimeAtRe         = regexp.MustCompile(`(?i)\bat\s+(\d{1,2})(?:\s*[:：]\s*(\d{2}))?\b`)
	monitorWeekdayRangeENRe = regexp.MustCompile(`(?i)\b(?:mon|monday)\s*(?:-|to|through|until)\s*(?:fri|friday)\b`)
)

// ParseMonitorScheduleInput accepts either a 5-field cron expression or a
// small set of natural schedule phrases used by the monitor UI.
func ParseMonitorScheduleInput(input string) (cronExpr string, human string, err error) {
	human = strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	if human == "" {
		return "", "", fmt.Errorf("schedule cannot be empty")
	}
	if cronExpr, err := ParseSchedule(human); err == nil {
		return cronExpr, human, nil
	}

	lower := strings.ToLower(human)
	if matches := monitorEveryMinutesZHRe.FindStringSubmatch(human); matches != nil {
		return everyMinutesCron(matches[1], human)
	}
	if matches := monitorEveryMinutesENRe.FindStringSubmatch(lower); matches != nil {
		return everyMinutesCron(matches[1], human)
	}
	if lower == "hourly" || lower == "every hour" || human == "每小時" || human == "每小时" || human == "每 1 小時" || human == "每 1 小时" {
		return "0 * * * *", human, nil
	}

	if dayRange, ok := monitorWeekdayRange(lower, human); ok {
		hour, minute, ok := monitorClock(human)
		if !ok {
			return "", human, fmt.Errorf("weekday schedule needs a time, for example 平日 09:00")
		}
		return fmt.Sprintf("%d %d * * %s", minute, hour, dayRange), human, nil
	}
	if day, ok := monitorWeekday(lower, human); ok {
		hour, minute, ok := monitorClock(human)
		if !ok {
			return "", human, fmt.Errorf("weekly schedule needs a time, for example 每週一 09:00")
		}
		return fmt.Sprintf("%d %d * * %d", minute, hour, day), human, nil
	}
	if monitorContainsAny(human, []string{"平日", "工作日"}) || strings.Contains(lower, "weekday") || strings.Contains(lower, "weekdays") {
		hour, minute, ok := monitorClock(human)
		if !ok {
			return "", human, fmt.Errorf("weekday schedule needs a time, for example 平日 09:00")
		}
		return fmt.Sprintf("%d %d * * 1-5", minute, hour), human, nil
	}
	if monitorContainsAny(human, []string{"每天", "每日"}) || strings.Contains(lower, "daily") || strings.Contains(lower, "every day") {
		hour, minute, ok := monitorClock(human)
		if !ok {
			return "", human, fmt.Errorf("daily schedule needs a time, for example 每天 09:00")
		}
		return fmt.Sprintf("%d %d * * *", minute, hour), human, nil
	}
	return "", human, fmt.Errorf("invalid schedule: use cron or examples like 每 10 分鐘, 平日 09:00, or daily at 9am")
}

func everyMinutesCron(raw, human string) (string, string, error) {
	minutes, err := strconv.Atoi(raw)
	if err != nil || minutes < 1 || minutes > 59 {
		return "", human, fmt.Errorf("minute interval must be 1-59")
	}
	return fmt.Sprintf("*/%d * * * *", minutes), human, nil
}

func monitorClock(input string) (hour int, minute int, ok bool) {
	if matches := monitorTimeAMPMRe.FindStringSubmatch(input); matches != nil {
		return parseMonitorClock(matches[1], matches[2], strings.ToLower(matches[3]))
	}
	if matches := monitorTimeColonRe.FindStringSubmatch(input); matches != nil {
		return parseMonitorClock(matches[1], matches[2], "")
	}
	if matches := monitorTimeZhHourRe.FindStringSubmatch(input); matches != nil {
		minuteText := "0"
		if matches[2] != "" {
			minuteText = "30"
		}
		return parseMonitorClock(matches[1], minuteText, zhMonitorMeridiem(input))
	}
	if matches := monitorTimeAtRe.FindStringSubmatch(input); matches != nil {
		return parseMonitorClock(matches[1], matches[2], "")
	}
	return 0, 0, false
}

func parseMonitorClock(hourText, minuteText, meridiem string) (int, int, bool) {
	hour, err := strconv.Atoi(hourText)
	if err != nil {
		return 0, 0, false
	}
	minute := 0
	if strings.TrimSpace(minuteText) != "" {
		minute, err = strconv.Atoi(minuteText)
		if err != nil {
			return 0, 0, false
		}
	}
	if meridiem == "pm" && hour < 12 {
		hour += 12
	}
	if meridiem == "am" && hour == 12 {
		hour = 0
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func zhMonitorMeridiem(input string) string {
	switch {
	case monitorContainsAny(input, []string{"下午", "晚上", "傍晚"}):
		return "pm"
	case monitorContainsAny(input, []string{"上午", "早上", "凌晨", "清晨"}):
		return "am"
	case strings.Contains(input, "中午"):
		return "pm"
	default:
		return ""
	}
}

func monitorWeekdayRange(lower, original string) (string, bool) {
	compact := strings.NewReplacer(" ", "", "\t", "", "\n", "").Replace(original)
	if monitorContainsAny(compact, []string{
		"週一到週五", "周一到周五", "星期一到星期五", "禮拜一到禮拜五", "礼拜一到礼拜五",
		"週一至週五", "周一至周五", "星期一至星期五", "禮拜一至禮拜五", "礼拜一至礼拜五",
		"週一-週五", "周一-周五", "星期一-星期五", "禮拜一-禮拜五", "礼拜一-礼拜五",
	}) {
		return "1-5", true
	}
	if monitorWeekdayRangeENRe.MatchString(lower) || strings.Contains(lower, "monday to friday") || strings.Contains(lower, "monday through friday") {
		return "1-5", true
	}
	return "", false
}

func monitorWeekday(lower, original string) (int, bool) {
	zhDays := []struct {
		needle string
		day    int
	}{
		{"週日", 0}, {"周日", 0}, {"星期日", 0}, {"禮拜日", 0}, {"礼拜日", 0}, {"週天", 0}, {"周天", 0}, {"星期天", 0},
		{"週一", 1}, {"周一", 1}, {"星期一", 1}, {"禮拜一", 1}, {"礼拜一", 1},
		{"週二", 2}, {"周二", 2}, {"星期二", 2}, {"禮拜二", 2}, {"礼拜二", 2},
		{"週三", 3}, {"周三", 3}, {"星期三", 3}, {"禮拜三", 3}, {"礼拜三", 3},
		{"週四", 4}, {"周四", 4}, {"星期四", 4}, {"禮拜四", 4}, {"礼拜四", 4},
		{"週五", 5}, {"周五", 5}, {"星期五", 5}, {"禮拜五", 5}, {"礼拜五", 5},
		{"週六", 6}, {"周六", 6}, {"星期六", 6}, {"禮拜六", 6}, {"礼拜六", 6},
	}
	for _, candidate := range zhDays {
		if strings.Contains(original, candidate.needle) {
			return candidate.day, true
		}
	}
	for day, name := range []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"} {
		if strings.Contains(lower, name) {
			return day, true
		}
	}
	return 0, false
}

func monitorContainsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
