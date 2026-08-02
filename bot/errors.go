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
	var mcpDiscoveryErr *channel.MCPDiscoveryError
	if errors.As(err, &mcpDiscoveryErr) {
		return mcpDiscoveryErrorMessage(mcpDiscoveryErr)
	}
	if errors.Is(err, channel.ErrNoThreadAgent) {
		return L.Get("error.no_thread_agent")
	}

	switch {
	case strings.Contains(msg, "outside ALLOWED_CWD_ROOTS"):
		return L.Getf("error.cwd_outside_allowlist", msg)
	case strings.Contains(lower, "working directory not found"):
		return L.Getf("error.cwd_not_found", msg)
	case strings.Contains(lower, "working directory is not a directory"):
		return L.Getf("error.cwd_not_directory", msg)
	case strings.Contains(lower, "agent binary not found"):
		return L.Getf("error.agent_binary_missing", msg)
	case strings.Contains(lower, "kiro-cli binary not found"):
		return L.Getf("error.kiro_cli_missing", msg)
	case strings.Contains(lower, "you are not logged in") || strings.Contains(lower, "kiro-cli login"):
		return L.Getf("error.kiro_auth", msg)
	case strings.Contains(lower, "skills store is unavailable"):
		return L.Get("skill.error.store_unavailable")
	case strings.Contains(lower, "skill content is required"):
		return L.Get("skill.error.content_required")
	case strings.Contains(lower, "skill content must be curated markdown"):
		return L.Get("skill.error.raw_html_content")
	case strings.Contains(lower, "required tools must be json"):
		return L.Get("skill.error.required_tools_json")
	case strings.Contains(lower, "risk report must be json"):
		return L.Get("skill.error.risk_report_json")
	case strings.Contains(lower, "source message refs must be json"):
		return L.Get("skill.error.source_message_refs_json")
	case strings.Contains(lower, "guild scope requires guild_id"):
		return L.Get("skill.error.guild_scope_requires")
	case strings.Contains(lower, "channel scope requires guild_id and channel_id"):
		return L.Get("skill.error.channel_scope_requires")
	case strings.Contains(lower, "project scope requires project_cwd"):
		return L.Get("skill.error.project_scope_requires")
	case strings.Contains(lower, "channel_project scope requires"):
		return L.Get("skill.error.channel_project_scope_requires")
	case strings.Contains(lower, "unsupported skill scope"):
		return L.Get("skill.error.unsupported_scope")
	case strings.Contains(lower, "is not an active draft"):
		return L.Get("skill.error.creation_not_active")
	case strings.Contains(lower, "draft ") && strings.Contains(lower, " expired"):
		return L.Get("skill.error.creation_expired")
	case strings.Contains(lower, "materialized skill file drifted"):
		return L.Get("skill.error.materialized_drift")
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

func mcpDiscoveryErrorMessage(err *channel.MCPDiscoveryError) string {
	if err == nil {
		return ""
	}
	reason := mcpDiscoveryUserReason(err)
	if reason != "" {
		return L.Getf("error.mcp_scan_failed_reason", err.ServerName, err.Stage, reason)
	}
	detail := ""
	if err.Err != nil {
		detail = err.Err.Error()
	}
	if detail == "" {
		detail = err.Error()
	}
	return L.Getf("error.mcp_scan_failed", err.ServerName, err.Stage, detail)
}

func mcpDiscoveryUserReason(err *channel.MCPDiscoveryError) string {
	combined := strings.ToLower(strings.Join([]string{err.Stderr, err.Error()}, "\n"))
	switch {
	case strings.Contains(combined, "no providers configured") &&
		(strings.Contains(combined, "gemini_api_key") || strings.Contains(combined, "openai_api_key")):
		return L.Get("error.mcp_scan_reason_media_provider_missing")
	case strings.Contains(combined, "transport closed"):
		return L.Get("error.mcp_scan_reason_transport_closed")
	default:
		return ""
	}
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
