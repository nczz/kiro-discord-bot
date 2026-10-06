package usagerange

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxRelativeDays = 3650

var relativeDaysPattern = regexp.MustCompile(`^([1-9][0-9]{0,3})d$`)

type ErrorCode string

const (
	ErrInvalidPeriod  ErrorCode = "invalid_period"
	ErrPeriodTooLarge ErrorCode = "period_too_large"
	ErrFromRequired   ErrorCode = "from_required"
	ErrInvalidFrom    ErrorCode = "invalid_from"
	ErrInvalidTo      ErrorCode = "invalid_to"
	ErrReversedRange  ErrorCode = "reversed_range"
)

type RangeError struct {
	Code ErrorCode
}

func (e *RangeError) Error() string {
	if e == nil || e.Code == "" {
		return "invalid_usage_range"
	}
	return string(e.Code)
}

type Options struct {
	Period string
	From   string
	To     string
	Now    time.Time
	Loc    *time.Location
}

type Range struct {
	Label  string
	From   time.Time
	To     time.Time
	Custom bool
}

func Resolve(opts Options) (Range, error) {
	loc := opts.Loc
	if loc == nil {
		loc = time.Local
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(loc)
	fromText := strings.TrimSpace(opts.From)
	toText := strings.TrimSpace(opts.To)
	if fromText != "" || toText != "" {
		if fromText == "" {
			return Range{}, &RangeError{Code: ErrFromRequired}
		}
		from, err := parseBoundary(fromText, loc, false)
		if err != nil {
			return Range{}, &RangeError{Code: ErrInvalidFrom}
		}
		to := now
		if toText != "" {
			to, err = parseBoundary(toText, loc, true)
			if err != nil {
				return Range{}, &RangeError{Code: ErrInvalidTo}
			}
		}
		if from.After(to) {
			return Range{}, &RangeError{Code: ErrReversedRange}
		}
		return Range{Label: "custom", From: from, To: to, Custom: true}, nil
	}
	period, from, to, err := resolvePeriod(opts.Period, now)
	if err != nil {
		return Range{}, err
	}
	return Range{Label: period, From: from, To: to}, nil
}

func resolvePeriod(period string, now time.Time) (string, time.Time, time.Time, error) {
	period = strings.TrimSpace(period)
	if period == "" {
		period = "30d"
	}
	switch period {
	case "this-month":
		return period, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), now, nil
	case "last-month":
		end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return period, end.AddDate(0, -1, 0), end.Add(-time.Nanosecond), nil
	}
	match := relativeDaysPattern.FindStringSubmatch(period)
	if len(match) != 2 {
		return "", time.Time{}, time.Time{}, &RangeError{Code: ErrInvalidPeriod}
	}
	days, err := strconv.Atoi(match[1])
	if err != nil || days <= 0 {
		return "", time.Time{}, time.Time{}, &RangeError{Code: ErrInvalidPeriod}
	}
	if days > MaxRelativeDays {
		return "", time.Time{}, time.Time{}, &RangeError{Code: ErrPeriodTooLarge}
	}
	return fmt.Sprintf("%dd", days), now.AddDate(0, 0, -days), now, nil
}

func parseBoundary(value string, loc *time.Location, endOfDay bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.In(loc), nil
	}
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02T15:04"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", value, loc); err == nil {
		if endOfDay {
			return t.AddDate(0, 0, 1).Add(-time.Nanosecond), nil
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid time")
}

func Message(err error) string {
	rangeErr, ok := err.(*RangeError)
	if !ok || rangeErr == nil {
		return "invalid usage time range"
	}
	switch rangeErr.Code {
	case ErrInvalidPeriod:
		return "period must be a day count such as 7d, 30d, or 90d, or one of this-month and last-month"
	case ErrPeriodTooLarge:
		return "relative day periods are limited to 3650d; use from/to dates for older history"
	case ErrFromRequired:
		return "from is required when to is set"
	case ErrInvalidFrom:
		return "from must be YYYY-MM-DD, YYYY-MM-DD HH:MM, or RFC3339"
	case ErrInvalidTo:
		return "to must be YYYY-MM-DD, YYYY-MM-DD HH:MM, or RFC3339"
	case ErrReversedRange:
		return "from must be before to"
	default:
		return "invalid usage time range"
	}
}
