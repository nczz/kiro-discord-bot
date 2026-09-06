package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/internal/cronpolicy"
)

// ParsedMonitorJob holds the structured result from natural language parsing.
type ParsedMonitorJob struct {
	Name        string `json:"name"`
	Schedule    string `json:"schedule"`
	CheckPrompt string `json:"check_prompt"`
	NotifyWhen  string `json:"notify_when"`
}

type monitorPromptStore struct {
	mu      sync.Mutex
	entries map[string]monitorPromptEntry
}

type monitorPromptEntry struct {
	job     *ParsedMonitorJob
	expires time.Time
}

func (s *monitorPromptStore) Store(key string, job *ParsedMonitorJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[string]monitorPromptEntry)
	}
	now := time.Now()
	for k, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, k)
		}
	}
	s.entries[key] = monitorPromptEntry{job: job, expires: now.Add(10 * time.Minute)}
}

func (s *monitorPromptStore) LoadAndDelete(key string) (*ParsedMonitorJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok || time.Now().After(e.expires) {
		delete(s.entries, key)
		return nil, false
	}
	delete(s.entries, key)
	return e.job, true
}

var (
	ErrMonitorScheduleUncertain  = fmt.Errorf("monitor_schedule_uncertain")
	ErrMonitorCheckUncertain     = fmt.Errorf("monitor_check_uncertain")
	ErrMonitorConditionUncertain = fmt.Errorf("monitor_condition_uncertain")
	ErrMonitorFieldTooLong       = fmt.Errorf("monitor_field_too_long")
)

func (b *Bot) parseMonitorPrompt(ctx context.Context, channelID string, input string) (*ParsedMonitorJob, error) {
	cwd := b.manager.CWDPath(channelID)
	model, err := b.manager.ResolveTempAgentModel(channelID, "")
	if err != nil {
		return nil, fmt.Errorf("resolve model: %w", err)
	}
	agent, err := b.manager.StartTempAgent("monitor-parse", cwd, model, channelID)
	if err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}
	defer b.manager.StopTempAgent(agent)

	tzPolicy := cronpolicy.SchedulePolicy(b.cronTimezone)
	systemPrompt := fmt.Sprintf(`You are a background monitor configuration parser. Extract structured data from the user's natural language request.

Timezone policy: %s

Return ONLY a JSON object with these fields:
- "name": short descriptive monitor name (max 50 chars, in the user's language)
- "schedule": 5-field cron expression (minute hour day-of-month month day-of-week)
- "check_prompt": the background check an AI agent must perform
- "notify_when": the condition that must be true before the bot posts a Discord notification

If the request lacks clear schedule/timing information, set schedule to "?".
If it lacks a clear target/action to check, set check_prompt to "?".
If it lacks a clear notification condition, set notify_when to "?".
Do NOT guess missing monitor fields.

Example input: "每天 15:00 背景檢查庫存，庫存低於 10 才通知"
Example output: {"name":"每日庫存監控","schedule":"0 15 * * *","check_prompt":"檢查庫存狀態","notify_when":"庫存低於 10"}

Example input: "every hour check production status and notify only when there is an outage"
Example output: {"name":"Production status monitor","schedule":"0 * * * *","check_prompt":"Check production status","notify_when":"There is an outage"}

IMPORTANT: Return ONLY the JSON object. No markdown, no explanation, no extra text.`, tzPolicy)

	prompt := systemPrompt + "\n\nUser request: " + input
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := agent.Ask(ctx, prompt, nil)
		if err != nil {
			return nil, fmt.Errorf("agent ask: %w", err)
		}
		result, validationErr := validateMonitorJSON(resp)
		if validationErr == nil {
			return result, nil
		}
		if errors.Is(validationErr, ErrMonitorScheduleUncertain) || errors.Is(validationErr, ErrMonitorCheckUncertain) || errors.Is(validationErr, ErrMonitorConditionUncertain) {
			return nil, validationErr
		}
		if attempt == maxAttempts {
			return nil, fmt.Errorf("attempt %d: %w", attempt, validationErr)
		}
		prompt = fmt.Sprintf("Your previous response was invalid: %s\n\nFix it and return ONLY the corrected JSON object. No markdown fencing, no explanation.", validationErr.Error())
	}
	return nil, fmt.Errorf("unreachable")
}

func validateMonitorJSON(raw string) (*ParsedMonitorJob, error) {
	s := strings.TrimSpace(raw)
	if m := mdCodeBlockRe.FindStringSubmatch(s); m != nil {
		s = strings.TrimSpace(m[1])
	}
	if idx := strings.Index(s, "{"); idx >= 0 {
		if end := strings.LastIndex(s, "}"); end > idx {
			s = s[idx : end+1]
		}
	}
	var result ParsedMonitorJob
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil, fmt.Errorf("not valid JSON: %s", err.Error())
	}
	result.Name = strings.TrimSpace(result.Name)
	result.Schedule = strings.TrimSpace(result.Schedule)
	result.CheckPrompt = strings.TrimSpace(result.CheckPrompt)
	result.NotifyWhen = strings.TrimSpace(result.NotifyWhen)
	if result.Name == "" {
		return nil, fmt.Errorf("missing 'name' field")
	}
	if len([]rune(result.Name)) > heartbeat.MonitorNameMaxRunes {
		return nil, fmt.Errorf("'name' too long (%d chars, max %d)", len([]rune(result.Name)), heartbeat.MonitorNameMaxRunes)
	}
	if result.Schedule == "" || result.Schedule == "?" {
		return nil, ErrMonitorScheduleUncertain
	}
	if result.CheckPrompt == "" || result.CheckPrompt == "?" {
		return nil, ErrMonitorCheckUncertain
	}
	if result.NotifyWhen == "" || result.NotifyWhen == "?" {
		return nil, ErrMonitorConditionUncertain
	}
	if len([]rune(result.Schedule)) > heartbeat.MonitorScheduleMaxRunes ||
		len([]rune(result.CheckPrompt)) > heartbeat.MonitorCheckPromptMaxRunes ||
		len([]rune(result.NotifyWhen)) > heartbeat.MonitorNotifyWhenMaxRunes {
		return nil, ErrMonitorFieldTooLong
	}
	if _, err := heartbeat.ParseSchedule(result.Schedule); err != nil {
		return nil, fmt.Errorf("invalid schedule '%s': %s", result.Schedule, err.Error())
	}
	return &result, nil
}
