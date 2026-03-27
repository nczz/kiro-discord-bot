package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/bot"
	"github.com/nczz/kiro-discord-bot/locale"
)

func main() {
	cfg := loadConfig()
	locale.Load(cfg.BotLocale)

	// Preflight check
	if os.Getenv("SKIP_PREFLIGHT") == "" {
		if err := acp.PreflightCheck(cfg.KiroCLIPath); err != nil {
			log.Fatalf("[preflight] FATAL: %v — kiro-cli may have updated its ACP protocol", err)
		}
	}

	b, err := bot.NewFromConfig(bot.BotConfig{
		DiscordToken:    cfg.DiscordToken,
		KiroCLIPath:     cfg.KiroCLIPath,
		DefaultCWD:      cfg.DefaultCWD,
		DataDir:         cfg.DataDir,
		QueueBufferSize: cfg.QueueBufferSize,
		AskTimeoutSec:   cfg.AskTimeoutSec,
		StreamUpdateSec: cfg.StreamUpdateSec,
		GuildID:         cfg.DiscordGuildID,
		KiroModel:       cfg.KiroModel,
		HeartbeatSec:    cfg.HeartbeatSec,
		AttRetainDays:   cfg.AttRetainDays,
		CronTimezone:    cfg.CronTimezone,
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
