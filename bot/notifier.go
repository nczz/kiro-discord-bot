package bot

// botNotifier provides shared Notify and IsSilent implementations for all heartbeat adapters.
type botNotifier struct {
	bot *Bot
}

func (n *botNotifier) Notify(channelID, msg string) {
	_, _ = sendDiscordText(n.bot.discord, channelID, msg, nil)
}

func (n *botNotifier) IsSilent(channelID string) bool {
	return n.bot.manager.IsSilent(channelID)
}
