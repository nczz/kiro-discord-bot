package channel

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	L "github.com/nczz/kiro-discord-bot/locale"
)

type envEntry struct {
	Name      string
	Sensitive bool
	Group     string // i18n key suffix for group header
	Effective func(*Manager) string
}

var envSpecs = []envEntry{
	// Core
	{Name: "DISCORD_TOKEN", Sensitive: true, Group: "core"},
	{Name: "DISCORD_GUILD_ID", Group: "core", Effective: func(m *Manager) string { return configuredOrNone(m.guildID) }},
	{Name: "KIRO_CLI_PATH", Group: "core", Effective: func(m *Manager) string { return defaultIfEmpty(m.kiroCLI, "kiro-cli") }},
	{Name: "KIRO_API_KEY", Sensitive: true, Group: "core"},

	// Agent
	{Name: "DEFAULT_CWD", Group: "agent", Effective: func(m *Manager) string { return m.defaultCWD }},
	{Name: "ALLOWED_CWD_ROOTS", Group: "agent", Effective: func(m *Manager) string {
		if len(m.allowedCwdRoots) == 0 {
			return L.Get("doctor.value.not_restricted")
		}
		return strings.Join(m.allowedCwdRoots, ", ")
	}},
	{Name: "KIRO_MODEL", Group: "agent", Effective: func(m *Manager) string { return configuredOrAuto(m.defaultModel) }},
	{Name: "KIRO_AGENT", Group: "agent", Effective: func(m *Manager) string { return configuredOrDefault(m.agentProfile) }},
	{Name: "TRUST_ALL_TOOLS", Group: "agent", Effective: func(m *Manager) string { return strconv.FormatBool(m.trustAllTools) }},
	{Name: "TRUST_TOOLS", Group: "agent", Effective: func(m *Manager) string { return configuredOrNone(m.trustTools) }},
	{Name: "KIRO_MCP_CONFIG", Group: "agent", Effective: func(m *Manager) string {
		if strings.TrimSpace(m.agentRuntimeHome) == "" {
			return L.Get("doctor.value.not_configured")
		}
		return filepath.Join(m.agentRuntimeHome, "settings", "mcp.json")
	}},

	// Execution
	{Name: "ASK_TIMEOUT_SEC", Group: "execution", Effective: func(m *Manager) string { return strconv.Itoa(m.askTimeoutSec) }},
	{Name: "STREAM_UPDATE_SEC", Group: "execution", Effective: func(m *Manager) string { return strconv.Itoa(m.streamUpdateSec) }},
	{Name: "QUEUE_BUFFER_SIZE", Group: "execution", Effective: func(m *Manager) string { return strconv.Itoa(m.queueBufSize) }},
	{Name: "MAX_SCANNER_BUFFER_MB", Group: "execution", Effective: func(m *Manager) string { return strconv.Itoa(m.maxScannerBuffer / 1024 / 1024) }},
	{Name: "DOWNLOAD_TIMEOUT_SEC", Group: "execution"},

	// Thread & Channel
	{Name: "THREAD_AUTO_ARCHIVE", Group: "thread", Effective: func(m *Manager) string { return strconv.Itoa(m.threadArchive) }},
	{Name: "THREAD_AGENT_MAX", Group: "thread", Effective: func(m *Manager) string { return strconv.Itoa(m.threadAgentMax) }},
	{Name: "THREAD_AGENT_IDLE_SEC", Group: "thread", Effective: func(m *Manager) string { return strconv.Itoa(m.threadAgentIdleSec) }},
	{Name: "CHANNEL_AGENT_IDLE_SEC", Group: "thread", Effective: func(m *Manager) string { return strconv.Itoa(m.channelAgentIdleSec) }},

	// Maintenance
	{Name: "DATA_DIR", Group: "maintenance", Effective: func(m *Manager) string { return m.dataDir }},
	{Name: "HEARTBEAT_SEC", Group: "maintenance"},
	{Name: "ATTACHMENT_RETAIN_DAYS", Group: "maintenance"},
	{Name: "ATTACHMENT_MAX_MB", Group: "maintenance"},
	{Name: "PREFLIGHT_MODE", Group: "maintenance"},
	{Name: "SKIP_PREFLIGHT", Group: "maintenance"},

	// Locale & Time
	{Name: "BOT_LOCALE", Group: "locale"},
	{Name: "CRON_TIMEZONE", Group: "locale"},
	{Name: "USAGE_TIMEZONE", Group: "locale"},
	{Name: "USAGE_RETENTION_MONTHS", Group: "locale"},

	// Multi-bot
	{Name: "BOT_PEERS", Group: "multibot"},

	// Audit
	{Name: "AUDIT_LOG_ENABLED", Group: "audit"},
	{Name: "AUDIT_LOG_DB", Group: "audit"},
	{Name: "AUDIT_LOG_RETENTION_DAYS", Group: "audit"},
	{Name: "AUDIT_LOG_QUEUE_SIZE", Group: "audit"},
	{Name: "AUDIT_LOG_RECORD_CONTENT", Group: "audit"},
	{Name: "AUDIT_LOG_RECORD_TYPING", Group: "audit"},

	// STT
	{Name: "STT_ENABLED", Group: "stt"},
	{Name: "STT_PROVIDER", Group: "stt"},
	{Name: "STT_API_KEY", Sensitive: true, Group: "stt"},
	{Name: "STT_MODEL", Group: "stt"},
	{Name: "STT_LANGUAGE", Group: "stt"},
	{Name: "STT_MAX_DURATION_SEC", Group: "stt"},

	// Discord MCP
	{Name: "MCP_DISCORD_ALLOWED_GUILDS", Group: "mcp_discord"},
	{Name: "MCP_DISCORD_ALLOWED_CHANNELS", Group: "mcp_discord"},
	{Name: "MCP_DISCORD_DOWNLOAD_DIR", Group: "mcp_discord"},
	{Name: "MCP_DISCORD_READ_ONLY", Group: "mcp_discord"},
	{Name: "MCP_DISCORD_ALLOWED_WRITE_TOOLS", Group: "mcp_discord"},
	{Name: "MCP_DISCORD_ALLOW_DESTRUCTIVE", Group: "mcp_discord"},
}

// doctorRuntimeOverview returns a safe runtime configuration overview for /doctor.
// It reports environment presence and selected effective values, but never raw
// environment values. /doctor is also available through bang commands, so this
// output must be safe for a normal channel.
func (m *Manager) doctorRuntimeOverview() string {
	var sb strings.Builder
	sb.WriteString("\n" + L.Get("doctor.env.header"))

	lastGroup := ""
	for _, e := range envSpecs {
		if e.Group != lastGroup {
			sb.WriteString("\n" + L.Get("doctor.env.group."+e.Group) + "\n")
			lastGroup = e.Group
		}
		state := L.Get("doctor.env.state.unset")
		mark := "⬚"
		if os.Getenv(e.Name) != "" {
			mark = "✅"
			state = L.Get("doctor.env.state.set")
			if e.Sensitive {
				state = L.Get("doctor.env.state.set_redacted")
			}
		}
		desc := L.Get("doctor.env.desc." + e.Name)
		line := fmt.Sprintf("  %s `%s`: %s — %s", mark, e.Name, state, desc)
		if e.Effective != nil {
			line += " " + L.Getf("doctor.env.effective", e.Effective(m))
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

func configuredOrNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return L.Get("doctor.value.not_configured")
	}
	return v
}

func configuredOrAuto(v string) string {
	if strings.TrimSpace(v) == "" {
		return "auto"
	}
	return v
}

func configuredOrDefault(v string) string {
	if strings.TrimSpace(v) == "" {
		return "default"
	}
	return v
}

func defaultIfEmpty(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
