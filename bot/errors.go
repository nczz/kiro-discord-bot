package bot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nczz/kiro-discord-bot/channel"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func commandError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	var limitErr *channel.ThreadAgentLimitError
	if errors.As(err, &limitErr) {
		return threadAgentLimitMessage(limitErr)
	}

	switch {
	case strings.Contains(msg, "outside ALLOWED_CWD_ROOTS"):
		return L.Getf("error.cwd_outside_allowlist", msg)
	case strings.Contains(lower, "working directory not found"):
		return L.Getf("error.cwd_not_found", msg)
	case strings.Contains(lower, "working directory is not a directory"):
		return L.Getf("error.cwd_not_directory", msg)
	case strings.Contains(lower, "kiro-cli binary not found"):
		return L.Getf("error.kiro_cli_missing", msg)
	case strings.Contains(lower, "you are not logged in") || strings.Contains(lower, "kiro-cli login"):
		return L.Getf("error.kiro_auth", msg)
	case strings.Contains(lower, "queue full"):
		return L.Getf("error.queue_full_action", msg)
	case strings.Contains(lower, "active job is not cancellable yet"):
		return L.Get("error.active_job_not_cancellable")
	case strings.Contains(lower, "no active job"):
		return L.Get("error.no_active_job")
	default:
		return L.Getf("error.generic", msg)
	}
}

func commandErrorString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimPrefix(commandError(err), "❌ ")
}

func threadAgentLimitMessage(err *channel.ThreadAgentLimitError) string {
	if err.Inactive == 0 {
		return L.Getf("error.thread_agent_limit_all_active", err.Max, err.Active)
	}

	candidates := make([]string, 0, len(err.Candidates))
	for i, c := range err.Candidates {
		if i >= 5 {
			break
		}
		candidates = append(candidates, fmt.Sprintf("<#%s> `%s`", c.ThreadID, c.ThreadID))
	}
	if len(candidates) == 0 {
		return L.Getf("error.thread_agent_limit_all_active", err.Max, err.Active)
	}
	return L.Getf("error.thread_agent_limit_choose", err.Max, err.Active, err.Inactive, strings.Join(candidates, "\n"))
}
