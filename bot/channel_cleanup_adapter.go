package bot

import "github.com/nczz/kiro-discord-bot/heartbeat"

type channelCleanupAdapter struct {
	botNotifier
}

func (a *channelCleanupAdapter) ChannelIdleEntries() []heartbeat.ChannelIdleInfo {
	raw := a.bot.manager.ChannelIdleEntries()
	out := make([]heartbeat.ChannelIdleInfo, len(raw))
	for i, e := range raw {
		out[i] = heartbeat.ChannelIdleInfo{ChannelID: e.ChannelID, LastActivity: e.LastActivity}
	}
	return out
}

func (a *channelCleanupAdapter) StopIdleChannel(channelID string) {
	a.bot.manager.StopIdleChannel(channelID)
}
