package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/bot"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/locale"
)

func main() {
	cfg := loadConfig()
	locale.Load(cfg.BotLocale)

	// Preflight mode preserves the historical default of warning-only startup
	// while allowing production deployments to fail fast when desired.
	preflightMode := strings.ToLower(strings.TrimSpace(cfg.PreflightMode))
	if os.Getenv("SKIP_PREFLIGHT") != "" {
		preflightMode = "skip"
	}
	switch preflightMode {
	case "", "warn":
		if err := acp.PreflightCheck(cfg.KiroCLIPath); err != nil {
			log.Printf("[preflight] WARNING: %v — agent may be unavailable, errors will surface per-request", err)
		}
	case "strict", "fatal":
		if err := acp.PreflightCheck(cfg.KiroCLIPath); err != nil {
			log.Fatalf("[preflight] FATAL: %v — set PREFLIGHT_MODE=warn or skip only if you accept delayed per-request failures", err)
		}
	case "skip", "off", "false":
		log.Printf("[preflight] skipped")
	default:
		log.Printf("[preflight] unknown PREFLIGHT_MODE=%q, using warn", cfg.PreflightMode)
		if err := acp.PreflightCheck(cfg.KiroCLIPath); err != nil {
			log.Printf("[preflight] WARNING: %v — agent may be unavailable, errors will surface per-request", err)
		}
	}

	log.Printf("kiro-discord-bot %s starting", Version)

	b, err := bot.NewFromConfig(bot.BotConfig{
		ManagerConfig: channel.ManagerConfig{
			KiroCLIPath:         cfg.KiroCLIPath,
			DefaultCWD:          cfg.DefaultCWD,
			DataDir:             cfg.DataDir,
			QueueBufferSize:     cfg.QueueBufferSize,
			AskTimeoutSec:       cfg.AskTimeoutSec,
			StreamUpdateSec:     cfg.StreamUpdateSec,
			ThreadAutoArchive:   cfg.ThreadAutoArchive,
			GuildID:             cfg.DiscordGuildID,
			KiroModel:           cfg.KiroModel,
			BotVersion:          Version,
			ThreadAgentMax:      cfg.ThreadAgentMax,
			ThreadAgentIdleSec:  cfg.ThreadAgentIdleSec,
			ChannelAgentIdleSec: cfg.ChannelAgentIdleSec,
			MaxScannerBuffer:    cfg.MaxScannerBuffer,
			AgentProfile:        cfg.AgentProfile,
			TrustAllTools:       cfg.TrustAllTools,
			TrustTools:          cfg.TrustTools,
		},
		DiscordToken:       cfg.DiscordToken,
		HeartbeatSec:       cfg.HeartbeatSec,
		AttRetainDays:      cfg.AttRetainDays,
		AttachmentMaxBytes: cfg.AttachmentMaxBytes,
		CronTimezone:       cfg.CronTimezone,
		DownloadTimeoutSec: cfg.DownloadTimeoutSec,
		STTEnabled:         cfg.STTEnabled,
		STTProvider:        cfg.STTProvider,
		STTAPIKey:          cfg.STTAPIKey,
		STTModel:           cfg.STTModel,
		STTLanguage:        cfg.STTLanguage,
		STTMaxDurationSec:  cfg.STTMaxDurationSec,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := b.Start(); err != nil {
		log.Fatal(err)
	}
	log.Println("Bot running. Ctrl+C to stop.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	<-sc

	b.Stop()
}
