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
	{Name: "OMP_PATH", Group: "core", Effective: func(m *Manager) string { return defaultIfEmpty(m.ompPath, "omp") }},
	{Name: "OMP_PROFILE", Group: "core", Effective: func(m *Manager) string { return configuredOrDefault(m.ompProfile) }},
	{Name: "OMP_SESSION_DIR", Group: "core", Effective: func(m *Manager) string { return configuredOrNone(m.ompSessionDir) }},
	{Name: "AGENT_ENGINE", Group: "core", Effective: func(m *Manager) string { return m.defaultEngine.String() }},
	{Name: "AGENT_ENGINES_ENABLED", Group: "core", Effective: func(m *Manager) string {
		if list := m.enabledEngineList(); len(list) > 0 {
			return strings.Join(list, ", ")
		}
		return m.defaultEngine.String()
	}},
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

	// A2A
	{Name: "NATS_URL", Group: "a2a", Effective: func(m *Manager) string {
		if m.a2aConfig.Enabled() {
			return "enabled"
		}
		return "disabled"
	}},
	{Name: "NATS_CREDS_FILE", Sensitive: true, Group: "a2a", Effective: func(m *Manager) string { return configuredPresence(m.a2aConfig.NATSCredsFile) }},
	{Name: "NATS_TOKEN", Sensitive: true, Group: "a2a", Effective: func(m *Manager) string { return configuredPresence(m.a2aConfig.NATSToken) }},
	{Name: "NATS_TLS_CA_FILE", Sensitive: true, Group: "a2a", Effective: func(m *Manager) string { return configuredPresence(m.a2aConfig.NATSTLSCAFile) }},
	{Name: "A2A_AGENT_ID", Group: "a2a", Effective: func(m *Manager) string { return configuredOrNone(string(m.a2aConfig.AgentID)) }},
	{Name: "A2A_AGENT_NAME", Group: "a2a", Effective: func(m *Manager) string { return configuredOrNone(m.a2aConfig.AgentName) }},
	{Name: "A2A_AGENT_DESCRIPTION", Group: "a2a", Effective: func(m *Manager) string { return configuredPresence(m.a2aConfig.AgentDescription) }},
	{Name: "A2A_TASK_TIMEOUT_SEC", Group: "a2a", Effective: func(m *Manager) string { return strconv.Itoa(m.a2aConfig.TaskTimeoutSec) }},
	{Name: "A2A_MAX_DELEGATION_DEPTH", Group: "a2a", Effective: func(m *Manager) string { return strconv.Itoa(m.a2aConfig.MaxDelegationDepth) }},
	{Name: "A2A_AUTO_DELEGATE_ENABLED", Group: "a2a", Effective: func(m *Manager) string { return strconv.FormatBool(m.a2aConfig.AutoDelegateEnabled) }},
	{Name: "A2A_REQUIRE_CONFIRMATION_FOR_REMOTE", Group: "a2a", Effective: func(m *Manager) string { return strconv.FormatBool(m.a2aConfig.RequireConfirmationForRemote) }},
	{Name: "A2A_PRODUCTION_SECURITY", Group: "a2a", Effective: func(m *Manager) string { return strconv.FormatBool(m.a2aConfig.ProductionSecurity) }},
	{Name: "A2A_TASK_RETENTION_DAYS", Group: "a2a", Effective: func(m *Manager) string { return strconv.Itoa(m.a2aConfig.TaskRetentionDays) }},
	{Name: "A2A_OBJECT_RETENTION_DAYS", Group: "a2a", Effective: func(m *Manager) string { return strconv.Itoa(m.a2aConfig.ObjectRetentionDays) }},
	{Name: "A2A_MAX_PENDING_TASKS", Group: "a2a", Effective: func(m *Manager) string { return strconv.Itoa(m.a2aConfig.MaxPendingTasks) }},
	{Name: "A2A_MAX_OUTBOUND_TASKS_PER_CHANNEL", Group: "a2a", Effective: func(m *Manager) string { return strconv.Itoa(m.a2aConfig.MaxOutboundTasksPerChannel) }},
	{Name: "A2A_MAX_INBOUND_TASKS_PER_CHANNEL", Group: "a2a", Effective: func(m *Manager) string { return strconv.Itoa(m.a2aConfig.MaxInboundTasksPerChannel) }},
	{Name: "A2A_MAX_EVENT_RATE_PER_MIN", Group: "a2a", Effective: func(m *Manager) string { return strconv.Itoa(m.a2aConfig.MaxEventRatePerMin) }},

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
	sb.WriteString(m.doctorA2AReadiness())
	return sb.String()
}

func (m *Manager) doctorA2AReadiness() string {
	cfg := m.a2aConfig
	mode := L.Get("doctor.a2a.auth.none")
	hasToken := strings.TrimSpace(cfg.NATSToken) != ""
	hasCreds := strings.TrimSpace(cfg.NATSCredsFile) != ""
	hasTLS := strings.TrimSpace(cfg.NATSTLSCAFile) != ""
	switch {
	case hasCreds && hasTLS:
		mode = L.Get("doctor.a2a.auth.creds_mtls")
	case hasCreds:
		mode = L.Get("doctor.a2a.auth.creds")
	case hasTLS:
		mode = L.Get("doctor.a2a.auth.mtls")
	case hasToken:
		mode = L.Get("doctor.a2a.auth.token_dev")
	}
	status := L.Get("doctor.a2a.status.disabled")
	if cfg.Enabled() {
		status = L.Get("doctor.a2a.status.enabled")
	}
	guard := L.Get("doctor.a2a.guard.off")
	if cfg.ProductionSecurity {
		guard = L.Get("doctor.a2a.guard.on")
	}
	validation := L.Get("doctor.a2a.validation.ok")
	if err := cfg.ValidateStartup(); err != nil {
		validation = L.Getf("doctor.a2a.validation.invalid", err.Error())
	}
	warning := ""
	if cfg.Enabled() && hasToken && !hasCreds && !hasTLS && !cfg.ProductionSecurity {
		warning = "\n" + L.Get("doctor.a2a.warning.token_only")
	}
	return fmt.Sprintf("\n%s\n- %s\n- %s\n- %s\n- %s%s\n", L.Get("doctor.a2a.header"), status, L.Getf("doctor.a2a.auth_mode", mode), guard, validation, warning)
}

func configuredOrNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return L.Get("doctor.value.not_configured")
	}
	return v
}

func configuredPresence(v string) string {
	if strings.TrimSpace(v) == "" {
		return L.Get("doctor.value.not_configured")
	}
	return L.Get("doctor.value.configured")
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

func (m *Manager) doctorListenModeConsistency() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var issues []string
	for ch, paused := range m.paused {
		if !paused {
			continue
		}
		if enabled, ok := m.threadMode[ch]; ok && enabled {
			issues = append(issues, L.Getf("doctor.listen_mode.inconsistent", ch))
		}
	}
	if len(issues) == 0 {
		return "\n" + L.Get("doctor.listen_mode.ok")
	}
	var sb strings.Builder
	sb.WriteString("\n" + L.Get("doctor.listen_mode.header"))
	for _, issue := range issues {
		sb.WriteString(issue)
	}
	return sb.String()
}
