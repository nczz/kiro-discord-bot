package main

import (
	"log"
	"os"
	"strconv"

	"github.com/nczz/kiro-discord-bot/internal/paths"
)

type Config struct {
	DiscordToken         string
	KiroCLIPath          string
	OMPPath              string
	AgentEngine          string
	AgentEnginesEnabled  string
	DefaultCWD           string
	AllowedCwdRoots      string
	AskTimeoutSec        int
	QueueBufferSize      int
	DataDir              string
	StreamUpdateSec      int
	DiscordGuildID       string
	KiroModel            string
	HeartbeatSec         int
	AttRetainDays        int
	AttachmentMaxBytes   int64
	CronTimezone         string
	UsageTimezone        string
	UsageRetentionMonths int
	BotLocale            string
	DownloadTimeoutSec   int
	ThreadAutoArchive    int
	ThreadAgentMax       int
	ThreadAgentIdleSec   int
	ChannelAgentIdleSec  int
	MaxScannerBuffer     int // bytes, scanner buffer upper limit for kiro-cli stdout
	AgentProfile         string
	TrustAllTools        bool
	TrustTools           string
	PreflightMode        string
	BotPeers             string
	AuditEnabled         bool
	AuditDBPath          string
	AuditRetentionDays   int
	AuditQueueSize       int
	AuditRecordContent   bool
	AuditRecordTyping    bool
	STTEnabled           bool
	STTProvider          string
	STTAPIKey            string
	STTModel             string
	STTLanguage          string
	STTMaxDurationSec    int
}

func loadConfig() *Config {
	cfg := &Config{
		DiscordToken:         mustEnv("DISCORD_TOKEN"),
		KiroCLIPath:          envOr("KIRO_CLI_PATH", "kiro-cli"),
		OMPPath:              envOr("OMP_PATH", "omp"),
		AgentEngine:          envOr("AGENT_ENGINE", "kiro"),
		AgentEnginesEnabled:  envOr("AGENT_ENGINES_ENABLED", ""),
		DefaultCWD:           envOr("DEFAULT_CWD", "/projects"),
		AllowedCwdRoots:      envOr("ALLOWED_CWD_ROOTS", ""),
		AskTimeoutSec:        envInt("ASK_TIMEOUT_SEC", 3600),
		QueueBufferSize:      envInt("QUEUE_BUFFER_SIZE", 20),
		DataDir:              envOr("DATA_DIR", "./data"),
		StreamUpdateSec:      envInt("STREAM_UPDATE_SEC", 3),
		DiscordGuildID:       envOr("DISCORD_GUILD_ID", ""),
		KiroModel:            envOr("KIRO_MODEL", ""),
		HeartbeatSec:         envInt("HEARTBEAT_SEC", 60),
		AttRetainDays:        envInt("ATTACHMENT_RETAIN_DAYS", 7),
		AttachmentMaxBytes:   int64(envInt("ATTACHMENT_MAX_MB", 25)) * 1024 * 1024,
		CronTimezone:         envOr("CRON_TIMEZONE", ""),
		UsageTimezone:        envOr("USAGE_TIMEZONE", envOr("CRON_TIMEZONE", "")),
		UsageRetentionMonths: envInt("USAGE_RETENTION_MONTHS", 0),
		BotLocale:            envOr("BOT_LOCALE", "en"),
		DownloadTimeoutSec:   envInt("DOWNLOAD_TIMEOUT_SEC", 120),
		ThreadAutoArchive:    envInt("THREAD_AUTO_ARCHIVE", 1440),
		ThreadAgentMax:       envInt("THREAD_AGENT_MAX", 5),
		ThreadAgentIdleSec:   envInt("THREAD_AGENT_IDLE_SEC", 900),
		ChannelAgentIdleSec:  envInt("CHANNEL_AGENT_IDLE_SEC", 0),
		MaxScannerBuffer:     envInt("MAX_SCANNER_BUFFER_MB", 64) * 1024 * 1024,
		AgentProfile:         envOr("KIRO_AGENT", ""),
		TrustAllTools:        envOr("TRUST_ALL_TOOLS", "true") == "true",
		TrustTools:           envOr("TRUST_TOOLS", ""),
		PreflightMode:        envOr("PREFLIGHT_MODE", "warn"),
		BotPeers:             envOr("BOT_PEERS", ""),
		AuditEnabled:         envBool("AUDIT_LOG_ENABLED", true),
		AuditDBPath:          envOr("AUDIT_LOG_DB", ""),
		AuditRetentionDays:   envInt("AUDIT_LOG_RETENTION_DAYS", 0),
		AuditQueueSize:       envInt("AUDIT_LOG_QUEUE_SIZE", 1000),
		AuditRecordContent:   envBool("AUDIT_LOG_RECORD_CONTENT", true),
		AuditRecordTyping:    envBool("AUDIT_LOG_RECORD_TYPING", false),
		STTEnabled:           envOr("STT_ENABLED", "false") == "true",
		STTProvider:          envOr("STT_PROVIDER", "groq"),
		STTAPIKey:            envOr("STT_API_KEY", ""),
		STTModel:             envOr("STT_MODEL", ""),
		STTLanguage:          envOr("STT_LANGUAGE", ""),
		STTMaxDurationSec:    envInt("STT_MAX_DURATION_SEC", 300),
	}
	if cfg.ThreadAgentMax <= 0 {
		log.Fatalf("THREAD_AGENT_MAX must be greater than 0, got %d", cfg.ThreadAgentMax)
	}
	dataDir, err := paths.DataDir(cfg.DataDir)
	if err != nil {
		log.Fatalf("resolve DATA_DIR: %v", err)
	}
	cfg.DataDir = dataDir
	return cfg
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env: %s", key)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
		return false
	default:
		return def
	}
}
