package bot

import (
	"strings"

	L "github.com/nczz/kiro-discord-bot/locale"
)

func commandError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)

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
