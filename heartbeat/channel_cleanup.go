package heartbeat

import (
	"log"
	"time"

	L "github.com/nczz/kiro-discord-bot/locale"
)

// ChannelCleanupDeps abstracts the dependencies for channel agent idle cleanup.
type ChannelCleanupDeps interface {
	ChannelIdleEntries() []ChannelIdleInfo
	StopIdleChannel(channelID string)
	Notify(channelID, msg string)
	IsSilent(channelID string) bool
}

// ChannelIdleInfo holds metadata about a channel agent for idle cleanup.
type ChannelIdleInfo struct {
	ChannelID    string
	LastActivity time.Time
}

// ChannelCleanupTask periodically kills idle channel agents.
type ChannelCleanupTask struct {
	deps    ChannelCleanupDeps
	idleSec int
}

func NewChannelCleanupTask(deps ChannelCleanupDeps, idleSec int) *ChannelCleanupTask {
	return &ChannelCleanupTask{deps: deps, idleSec: idleSec}
}

func (t *ChannelCleanupTask) Name() string { return "channel-cleanup" }

func (t *ChannelCleanupTask) ShouldRun(_ time.Time) bool {
	return t.idleSec > 0
}

func (t *ChannelCleanupTask) Run() error {
	entries := t.deps.ChannelIdleEntries()
	now := time.Now()
	threshold := time.Duration(t.idleSec) * time.Second

	for _, e := range entries {
		if e.LastActivity.IsZero() {
			continue
		}
		if now.Sub(e.LastActivity) > threshold {
			log.Printf("[channel-cleanup] stopping idle channel agent ch-%s (idle %s)", e.ChannelID, now.Sub(e.LastActivity).Round(time.Second))
			t.deps.StopIdleChannel(e.ChannelID)
			if !t.deps.IsSilent(e.ChannelID) {
				t.deps.Notify(e.ChannelID, L.Get("channel_agent.idle_closed"))
			}
		}
	}
	return nil
}
