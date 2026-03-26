package heartbeat

import (
	"log"
	"net/http"
	"time"
)

// SessionInfo holds the minimal info needed for health checks.
type SessionInfo struct {
	ChannelID string
	AgentName string
}

// HealthDeps abstracts the dependencies for the health check task.
type HealthDeps interface {
	ActiveSessions() []SessionInfo
	CheckAgent(agentName string) error
	RestartAgent(channelID string) error
	Notify(channelID, msg string)
}

// HealthTask checks agent liveness and acp-bridge reachability.
type HealthTask struct {
	deps        HealthDeps
	acpBridgeURL string
	httpClient  *http.Client
}

func NewHealthTask(deps HealthDeps, acpBridgeURL string) *HealthTask {
	return &HealthTask{
		deps:        deps,
		acpBridgeURL: acpBridgeURL,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (h *HealthTask) Name() string { return "health" }

func (h *HealthTask) ShouldRun(_ time.Time) bool { return true }

func (h *HealthTask) Run() error {
	// Check acp-bridge
	resp, err := h.httpClient.Get(h.acpBridgeURL + "/agents")
	if err != nil {
		log.Printf("[health] acp-bridge unreachable: %v", err)
	} else {
		resp.Body.Close()
	}

	// Check each active agent
	for _, s := range h.deps.ActiveSessions() {
		if err := h.deps.CheckAgent(s.AgentName); err != nil {
			log.Printf("[health] agent %s dead, restarting", s.AgentName)
			if restartErr := h.deps.RestartAgent(s.ChannelID); restartErr != nil {
				log.Printf("[health] restart %s failed: %v", s.AgentName, restartErr)
				h.deps.Notify(s.ChannelID, "⚠️ Agent 異常且重啟失敗："+restartErr.Error())
			} else {
				h.deps.Notify(s.ChannelID, "⚠️ Agent 異常，已自動重啟。")
			}
		}
	}
	return nil
}
