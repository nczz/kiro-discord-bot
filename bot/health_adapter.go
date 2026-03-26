package bot

import "github.com/nczz/kiro-discord-bot/heartbeat"

// healthAdapter bridges Manager + Discord session to heartbeat.HealthDeps.
type healthAdapter struct {
	bot *Bot
}

func (a *healthAdapter) ActiveSessions() []heartbeat.SessionInfo {
	raw := a.bot.manager.ActiveSessions()
	out := make([]heartbeat.SessionInfo, len(raw))
	for i, s := range raw {
		out[i] = heartbeat.SessionInfo{ChannelID: s.ChannelID, AgentName: s.AgentName}
	}
	return out
}

func (a *healthAdapter) CheckAgent(agentName string) error {
	return a.bot.manager.CheckAgent(agentName)
}

func (a *healthAdapter) RestartAgent(channelID string) error {
	return a.bot.manager.Restart(channelID)
}

func (a *healthAdapter) Notify(channelID, msg string) {
	_, _ = a.bot.discord.ChannelMessageSend(channelID, msg)
}
