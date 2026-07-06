package cronpolicy

import (
	"fmt"
	"strings"
	"time"
)

// TimezoneName returns the configured cron timezone, or the service process
// local timezone name when cron uses its fallback location.
func TimezoneName(configured string) string {
	if tz := strings.TrimSpace(configured); tz != "" {
		return tz
	}
	if name := strings.TrimSpace(time.Now().Location().String()); name != "" {
		return name
	}
	return "the service process local timezone"
}

// SchedulePolicy describes how natural-language times map to cron fields.
func SchedulePolicy(tz string) string {
	tz = TimezoneName(tz)
	return fmt.Sprintf("Interpret all schedule times in the bot cron timezone %s. Do not convert user-local times to UTC. A 5-field cron expression such as '30 12 * * *' means 12:30 in %s.", tz, tz)
}

// CreateToolDescription returns the bot_create_cron tool description.
func CreateToolDescription(tz string) string {
	return fmt.Sprintf("Create a scheduled recurring task in this Discord channel. Use when the user wants something to run periodically (daily, weekly, etc.). The schedule must be a 5-field cron expression. For one-time delayed reminders such as 'in 10 minutes' or 'tomorrow at 09:00', use bot_create_reminder instead. %s", SchedulePolicy(tz))
}

// ScheduleFieldDescription returns the JSON schema description for cron fields.
func ScheduleFieldDescription(tz string) string {
	tz = TimezoneName(tz)
	return fmt.Sprintf("5-field cron expression in the bot cron timezone %s. Do not convert to UTC. Example: '0 9 * * *' means 09:00 in %s.", tz, tz)
}

// ReminderToolDescription returns the bot_create_reminder tool description.
func ReminderToolDescription(tz string) string {
	tz = TimezoneName(tz)
	return fmt.Sprintf("Create a one-time Discord reminder delivered by the bot scheduler. Use for one-time delayed reminders such as 'in 10 minutes', 'two hours later', 'tomorrow 09:00', or 'at 18:30'. Do not use this for recurring schedules; use bot_create_cron for daily, weekly, or periodic tasks. Reminder times are interpreted in the bot cron timezone %s.", tz)
}

// ReminderTimeFieldDescription returns the JSON schema description for one-time reminder time fields.
func ReminderTimeFieldDescription(tz string) string {
	tz = TimezoneName(tz)
	return fmt.Sprintf("One-time reminder time in the bot cron timezone %s. Supported examples include '+30m', '+2h', '10分鐘後', '2 hours later', '18:30', and 'tomorrow 09:00'.", tz)
}
