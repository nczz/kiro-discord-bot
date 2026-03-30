package bot

import "github.com/nczz/kiro-discord-bot/heartbeat"

type threadCleanupAdapter struct {
	bot *Bot
}

func (a *threadCleanupAdapter) ThreadAgentEntries() []heartbeat.ThreadAgentInfo {
	raw := a.bot.manager.ThreadAgentEntries()
	out := make([]heartbeat.ThreadAgentInfo, len(raw))
	for i, e := range raw {
		out[i] = heartbeat.ThreadAgentInfo{ThreadID: e.ThreadID, ParentChID: e.ParentChID, LastActivity: e.LastActivity}
	}
	return out
}

func (a *threadCleanupAdapter) StopThreadAgent(threadID string) {
	a.bot.manager.StopThreadAgent(threadID)
}

func (a *threadCleanupAdapter) Notify(channelID, msg string) {
	_, _ = a.bot.discord.ChannelMessageSend(channelID, msg)
}
