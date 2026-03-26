package main

import (
	"fmt"
	"os"
)

type Config struct {
	DiscordToken    string
	AcpBridgeURL    string
	KiroCLIPath     string
	DefaultCWD      string
	AskTimeoutSec   int
	QueueBufferSize int
	DataDir         string
	StreamUpdateSec int
	DiscordGuildID  string
	KiroModel       string
	HeartbeatSec    int
	AttRetainDays   int
}

func loadConfig() *Config {
	return &Config{
		DiscordToken:    mustEnv("DISCORD_TOKEN"),
		AcpBridgeURL:    envOr("ACP_BRIDGE_URL", "http://localhost:7800"),
		KiroCLIPath:     envOr("KIRO_CLI_PATH", "kiro-cli"),
		DefaultCWD:      envOr("DEFAULT_CWD", "/projects"),
		AskTimeoutSec:   envInt("ASK_TIMEOUT_SEC", 300),
		QueueBufferSize: envInt("QUEUE_BUFFER_SIZE", 20),
		DataDir:         envOr("DATA_DIR", "./data"),
		StreamUpdateSec: envInt("STREAM_UPDATE_SEC", 3),
		DiscordGuildID:  envOr("DISCORD_GUILD_ID", ""),
		KiroModel:       envOr("KIRO_MODEL", ""),
		HeartbeatSec:    envInt("HEARTBEAT_SEC", 60),
		AttRetainDays:   envInt("ATTACHMENT_RETAIN_DAYS", 7),
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("missing required env: " + key)
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
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}
