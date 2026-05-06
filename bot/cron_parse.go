package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nczz/kiro-discord-bot/heartbeat"
)

// ParsedCronJob holds the structured result from natural language parsing.
type ParsedCronJob struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Prompt   string `json:"prompt"`
}

var mdCodeBlockRe = regexp.MustCompile("(?s)^```(?:json)?\\s*\n?(.*?)\\s*```$")

// parseCronPrompt uses a temp agent to parse natural language into a structured cron job.
// Retries up to 3 times with specific error feedback on validation failure.
func (b *Bot) parseCronPrompt(ctx context.Context, input string) (*ParsedCronJob, error) {
	agent, err := b.manager.StartTempAgent("cron-parse", b.manager.DefaultCWD(), "")
	if err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}
	defer b.manager.StopTempAgent(agent)

	tz := b.cronTimezone
	if tz == "" {
		tz = time.Now().Location().String()
	}

	systemPrompt := fmt.Sprintf(`You are a cron job configuration parser. Extract structured data from the user's natural language request.

Timezone: %s

Return ONLY a JSON object with these fields:
- "name": short descriptive task name (max 50 chars, in the user's language)
- "schedule": 5-field cron expression (minute hour day-of-month month day-of-week)
- "prompt": the actual task instruction for an AI agent to execute

Example input: "every day at 9am check server health"
Example output: {"name":"Server Health Check","schedule":"0 9 * * *","prompt":"Check server health status and report any issues"}

Example input: "每個月5號早上10點產生月報"
Example output: {"name":"月報產生","schedule":"0 10 5 * *","prompt":"產生本月月報"}

IMPORTANT: Return ONLY the JSON object. No markdown, no explanation, no extra text.`, tz)

	prompt := systemPrompt + "\n\nUser request: " + input

	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := agent.Ask(ctx, prompt, nil)
		if err != nil {
			return nil, fmt.Errorf("agent ask: %w", err)
		}

		result, validationErr := validateCronJSON(resp)
		if validationErr == nil {
			return result, nil
		}

		if attempt == maxAttempts {
			return nil, fmt.Errorf("attempt %d: %w", attempt, validationErr)
		}

		// Build correction prompt for next attempt
		prompt = fmt.Sprintf("Your previous response was invalid: %s\n\nFix it and return ONLY the corrected JSON object. No markdown fencing, no explanation.", validationErr.Error())
	}

	return nil, fmt.Errorf("unreachable")
}

// validateCronJSON parses and validates the agent's response as a CronJob JSON.
func validateCronJSON(raw string) (*ParsedCronJob, error) {
	// Strip markdown code block if present
	s := strings.TrimSpace(raw)
	if m := mdCodeBlockRe.FindStringSubmatch(s); m != nil {
		s = strings.TrimSpace(m[1])
	}

	// Try to extract JSON from surrounding text (agent might add explanation)
	if idx := strings.Index(s, "{"); idx >= 0 {
		if end := strings.LastIndex(s, "}"); end > idx {
			s = s[idx : end+1]
		}
	}

	var result ParsedCronJob
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil, fmt.Errorf("not valid JSON: %s", err.Error())
	}

	if result.Name == "" {
		return nil, fmt.Errorf("missing 'name' field")
	}
	if len(result.Name) > 100 {
		return nil, fmt.Errorf("'name' too long (%d chars, max 100)", len(result.Name))
	}
	if result.Schedule == "" {
		return nil, fmt.Errorf("missing 'schedule' field")
	}
	if result.Prompt == "" {
		return nil, fmt.Errorf("missing 'prompt' field")
	}

	// Validate cron expression using existing parser
	if _, err := heartbeat.ParseSchedule(result.Schedule); err != nil {
		return nil, fmt.Errorf("invalid schedule '%s': %s", result.Schedule, err.Error())
	}

	return &result, nil
}
