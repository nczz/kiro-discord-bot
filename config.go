package main

import (
	"log"
	"os"
	"strconv"
)

type Config struct {
	DiscordToken       string
	KiroCLIPath        string
	DefaultCWD         string
	AskTimeoutSec      int
	QueueBufferSize    int
	DataDir            string
	StreamUpdateSec    int
	DiscordGuildID     string
	KiroModel          string
	HeartbeatSec       int
	AttRetainDays      int
	CronTimezone       string
	BotLocale          string
	DownloadTimeoutSec int
	ThreadAutoArchive  int
	ThreadAgentMax     int
	ThreadAgentIdleSec  int
	ChannelAgentIdleSec int
	MaxScannerBuffer    int // bytes, scanner buffer upper limit for kiro-cli stdout
	AgentProfile       string
	TrustAllTools      bool
	TrustTools         string
	STTEnabled         bool
	STTProvider        string
	STTAPIKey          string
	STTModel           string
	STTLanguage        string
	STTMaxDurationSec  int
}

func loadConfig() *Config {
	return &Config{
		DiscordToken:    mustEnv("DISCORD_TOKEN"),
		KiroCLIPath:     envOr("KIRO_CLI_PATH", "kiro-cli"),
		DefaultCWD:      envOr("DEFAULT_CWD", "/projects"),
		AskTimeoutSec:   envInt("ASK_TIMEOUT_SEC", 3600),
		QueueBufferSize: envInt("QUEUE_BUFFER_SIZE", 20),
		DataDir:         envOr("DATA_DIR", "./data"),
		StreamUpdateSec: envInt("STREAM_UPDATE_SEC", 3),
		DiscordGuildID:  envOr("DISCORD_GUILD_ID", ""),
		KiroModel:       envOr("KIRO_MODEL", ""),
		HeartbeatSec:    envInt("HEARTBEAT_SEC", 60),
		AttRetainDays:   envInt("ATTACHMENT_RETAIN_DAYS", 7),
		CronTimezone:       envOr("CRON_TIMEZONE", ""),
		BotLocale:          envOr("BOT_LOCALE", "en"),
		DownloadTimeoutSec: envInt("DOWNLOAD_TIMEOUT_SEC", 120),
		ThreadAutoArchive:  envInt("THREAD_AUTO_ARCHIVE", 1440),
		ThreadAgentMax:     envInt("THREAD_AGENT_MAX", 5),
		ThreadAgentIdleSec:  envInt("THREAD_AGENT_IDLE_SEC", 900),
		ChannelAgentIdleSec: envInt("CHANNEL_AGENT_IDLE_SEC", 0),
		MaxScannerBuffer:   envInt("MAX_SCANNER_BUFFER_MB", 64) * 1024 * 1024,
		AgentProfile:       envOr("KIRO_AGENT", ""),
		TrustAllTools:      envOr("TRUST_ALL_TOOLS", "true") == "true",
		TrustTools:         envOr("TRUST_TOOLS", ""),
		STTEnabled:         envOr("STT_ENABLED", "false") == "true",
		STTProvider:        envOr("STT_PROVIDER", "groq"),
		STTAPIKey:          envOr("STT_API_KEY", ""),
		STTModel:           envOr("STT_MODEL", ""),
		STTLanguage:        envOr("STT_LANGUAGE", ""),
		STTMaxDurationSec:  envInt("STT_MAX_DURATION_SEC", 300),
	}
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
