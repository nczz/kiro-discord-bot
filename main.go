package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nczz/kiro-discord-bot/bot"
)

func main() {
	cfg := loadConfig()

	b, err := bot.NewFromConfig(bot.BotConfig{
		DiscordToken:    cfg.DiscordToken,
		AcpBridgeURL:    cfg.AcpBridgeURL,
		KiroCLIPath:     cfg.KiroCLIPath,
		DefaultCWD:      cfg.DefaultCWD,
		DataDir:         cfg.DataDir,
		QueueBufferSize: cfg.QueueBufferSize,
		AskTimeoutSec:   cfg.AskTimeoutSec,
		StreamUpdateSec: cfg.StreamUpdateSec,
		GuildID:         cfg.DiscordGuildID,
		KiroModel:       cfg.KiroModel,
		HeartbeatSec:    cfg.HeartbeatSec,
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
