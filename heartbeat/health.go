package heartbeat

import (
	"log"
	"time"

	L "github.com/nczz/kiro-discord-bot/locale"
)

type SessionInfo struct {
	ChannelID string
	AgentName string
}

// HealthDeps abstracts the dependencies for the health check task.
type HealthDeps interface {
	ActiveSessions() []SessionInfo
	CheckAgent(channelID string) error
	RestartAgent(channelID string) error
	Notify(channelID, msg string)
}

type HealthTask struct {
	deps HealthDeps
}

func NewHealthTask(deps HealthDeps) *HealthTask {
	return &HealthTask{deps: deps}
}

func (h *HealthTask) Name() string            { return "health" }
func (h *HealthTask) ShouldRun(_ time.Time) bool { return true }

func (h *HealthTask) Run() error {
	for _, s := range h.deps.ActiveSessions() {
		if err := h.deps.CheckAgent(s.ChannelID); err != nil {
			log.Printf("[health] agent %s (ch=%s) dead, restarting", s.AgentName, s.ChannelID)
			if restartErr := h.deps.RestartAgent(s.ChannelID); restartErr != nil {
				log.Printf("[health] restart %s failed: %v", s.AgentName, restartErr)
				h.deps.Notify(s.ChannelID, L.Getf("health.restart_failed", restartErr.Error()))
			} else {
				h.deps.Notify(s.ChannelID, L.Get("health.restarted"))
			}
		}
	}
	return nil
}
