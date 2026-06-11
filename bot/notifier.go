package bot

import "github.com/nczz/kiro-discord-bot/internal/secrets"

// botNotifier provides shared Notify and IsSilent implementations for all heartbeat adapters.
type botNotifier struct {
	bot *Bot
}

func (n *botNotifier) Notify(channelID, msg string) {
	_, _ = n.bot.discord.ChannelMessageSend(channelID, secrets.RedactEnv(msg))
}

func (n *botNotifier) IsSilent(channelID string) bool {
	return n.bot.manager.IsSilent(channelID)
}
