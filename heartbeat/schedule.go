package heartbeat

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ParseSchedule converts natural language or raw cron expression to a cron expression.
// Supported patterns:
//   - "每天 09:00"         → "0 9 * * *"
//   - "每天 09:00 18:00"   → "0 9,18 * * *"
//   - "每小時"             → "0 * * * *"
//   - "每 30 分鐘"         → "*/30 * * * *"
//   - "每 N 小時"          → "0 */N * * *"
//   - "每週一 10:00"       → "0 10 * * 1"
//   - "0 9 * * *"          → "0 9 * * *" (passthrough)
func ParseSchedule(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", fmt.Errorf("empty schedule")
	}

	// Raw cron expression (5 fields)
	if matched, _ := regexp.MatchString(`^[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+\s+[0-9*/,-]+$`, s); matched {
		return s, nil
	}

	// 每 N 分鐘 / every N minutes
	if m := regexp.MustCompile(`(?:每|every)\s*(\d+)\s*(?:分鐘|分|min)`).FindStringSubmatch(s); m != nil {
		return fmt.Sprintf("*/%s * * * *", m[1]), nil
	}

	// 每小時 / every hour
	if regexp.MustCompile(`(?:每小時|every\s*hour)`).MatchString(s) {
		return "0 * * * *", nil
	}

	// 每 N 小時 / every N hours
	if m := regexp.MustCompile(`(?:每|every)\s*(\d+)\s*(?:小時|hour)`).FindStringSubmatch(s); m != nil {
		return fmt.Sprintf("0 */%s * * *", m[1]), nil
	}

	// 每週X HH:MM
	weekdays := map[string]string{
		"日": "0", "一": "1", "二": "2", "三": "3", "四": "4", "五": "5", "六": "6",
		"天": "0", "mon": "1", "tue": "2", "wed": "3", "thu": "4", "fri": "5", "sat": "6", "sun": "0",
	}
	weekRe := regexp.MustCompile(`(?:每週|every\s*)([日一二三四五六天]|mon|tue|wed|thu|fri|sat|sun)\s+(\d{1,2}):(\d{2})`)
	if m := weekRe.FindStringSubmatch(strings.ToLower(s)); m != nil {
		dow, ok := weekdays[m[1]]
		if !ok {
			return "", fmt.Errorf("unknown weekday: %s", m[1])
		}
		return fmt.Sprintf("%s %s * * %s", m[3], m[2], dow), nil
	}

	// 每天 HH:MM [HH:MM ...] / every day
	dayRe := regexp.MustCompile(`(?:每天|every\s*day)\s+(.+)`)
	if m := dayRe.FindStringSubmatch(s); m != nil {
		timeRe := regexp.MustCompile(`(\d{1,2}):(\d{2})`)
		times := timeRe.FindAllStringSubmatch(m[1], -1)
		if len(times) == 0 {
			return "", fmt.Errorf("no time found in: %s", s)
		}
		var hours, mins []string
		allSameMin := true
		firstMin := times[0][2]
		for _, t := range times {
			hours = append(hours, t[1])
			mins = append(mins, t[2])
			if t[2] != firstMin {
				allSameMin = false
			}
		}
		if allSameMin {
			return fmt.Sprintf("%s %s * * *", firstMin, strings.Join(hours, ",")), nil
		}
		// Different minutes — use first time only
		return fmt.Sprintf("%s %s * * *", times[0][2], times[0][1]), nil
	}

	return "", fmt.Errorf("無法解析排程: %s", s)
}

// ParseTime converts natural language time to an absolute time.Time.
// Supported: "下午五點", "17:00", "30 分鐘後", "2 小時後", "明天 09:00", "明天下午三點"
func ParseTime(input string, loc *time.Location) (time.Time, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	now := time.Now().In(loc)

	// N 分鐘後 / N min later
	if m := regexp.MustCompile(`(\d+)\s*(?:分鐘|分|min)(?:後|later)?`).FindStringSubmatch(s); m != nil {
		n := 0
		fmt.Sscanf(m[1], "%d", &n)
		return now.Add(time.Duration(n) * time.Minute), nil
	}

	// N 小時後 / N hour later
	if m := regexp.MustCompile(`(\d+)\s*(?:小時|hour)(?:後|later)?`).FindStringSubmatch(s); m != nil {
		n := 0
		fmt.Sscanf(m[1], "%d", &n)
		return now.Add(time.Duration(n) * time.Hour), nil
	}

	// Chinese AM/PM hour: 下午五點, 上午十點, 下午3點
	cnNums := map[string]int{
		"一": 1, "二": 2, "三": 3, "四": 4, "五": 5, "六": 6,
		"七": 7, "八": 8, "九": 9, "十": 10, "十一": 11, "十二": 12,
	}
	tomorrow := false
	work := s
	if strings.HasPrefix(work, "明天") {
		tomorrow = true
		work = strings.TrimPrefix(work, "明天")
	}

	// 下午X點 / 上午X點
	cnTimeRe := regexp.MustCompile(`(上午|下午|早上|晚上)?(\d+|[一二三四五六七八九十]+)(?:點|:)(\d{2})?`)
	if m := cnTimeRe.FindStringSubmatch(work); m != nil {
		period := m[1]
		hourStr := m[2]
		minStr := m[3]

		hour := 0
		if h, ok := cnNums[hourStr]; ok {
			hour = h
		} else {
			fmt.Sscanf(hourStr, "%d", &hour)
		}
		min := 0
		if minStr != "" {
			fmt.Sscanf(minStr, "%d", &min)
		}

		if period == "下午" || period == "晚上" {
			if hour < 12 {
				hour += 12
			}
		}

		target := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, loc)
		if tomorrow {
			target = target.AddDate(0, 0, 1)
		} else if target.Before(now) {
			target = target.AddDate(0, 0, 1)
		}
		return target, nil
	}

	// HH:MM format
	if m := regexp.MustCompile(`^(?:明天\s*)?(\d{1,2}):(\d{2})$`).FindStringSubmatch(s); m != nil {
		hour, min := 0, 0
		fmt.Sscanf(m[1], "%d", &hour)
		fmt.Sscanf(m[2], "%d", &min)
		target := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, loc)
		if strings.HasPrefix(s, "明天") {
			target = target.AddDate(0, 0, 1)
		} else if target.Before(now) {
			target = target.AddDate(0, 0, 1)
		}
		return target, nil
	}

	return time.Time{}, fmt.Errorf("無法解析時間: %s", s)
}
