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

// UpdateToolDescription returns the bot_update_cron tool description.
func UpdateToolDescription(tz string) string {
	return fmt.Sprintf("Update or disable an existing recurring cron job without deleting it. First call bot_list_cron to obtain the exact job_id and current values. Send only fields the user asked to change; omitted fields remain unchanged. Set enabled=false to disable while preserving the job, or enabled=true to resume it. For deletion use bot_delete_cron, never this tool. One-time reminders cannot be updated; delete and recreate them instead. Changing schedule or resuming computes the next future run and does not backfill missed runs. %s", SchedulePolicy(tz))
}

// ScheduleFieldDescription returns the JSON schema description for cron fields.
func ScheduleFieldDescription(tz string) string {
	tz = TimezoneName(tz)
	return fmt.Sprintf("5-field cron expression in the bot cron timezone %s. Do not convert to UTC. Example: '0 9 * * *' means 09:00 in %s.", tz, tz)
}

// ReminderToolDescription returns the bot_create_reminder tool description.
func ReminderToolDescription(tz string) string {
	tz = TimezoneName(tz)
	return fmt.Sprintf("Create a one-time Discord reminder delivered by the bot scheduler. Use for one-time reminders such as 'in 10 minutes', 'two hours later', 'tomorrow 09:00', '2026-08-13 14:25', or '8/13 14:25'. If the user gives a specific date, convert it to an explicit date/time in the bot cron timezone %s before calling this tool; do not degrade a dated request to 'tomorrow'. Do not use this for recurring schedules; use bot_create_cron for daily, weekly, or periodic tasks.", tz)
}

// ReminderTimeFieldDescription returns the JSON schema description for one-time reminder time fields.
func ReminderTimeFieldDescription(tz string) string {
	tz = TimezoneName(tz)
	return fmt.Sprintf("One-time reminder time in the bot cron timezone %s. Prefer canonical RFC3339 for dated requests, e.g. '2026-08-13T14:25:00+08:00'. Supported examples also include '2026-08-13 14:25', '8/13 14:25', '8月13日 14:25', '+30m', '+2h', '10分鐘後', '2 hours later', '18:30', and 'tomorrow 09:00'.", tz)
}
