package heartbeat

import (
	"log"
	"time"

	L "github.com/nczz/kiro-discord-bot/locale"
)

// ThreadCleanupDeps abstracts the dependencies for thread agent cleanup.
type ThreadCleanupDeps interface {
	// ThreadAgentEntries returns all active thread agents with their last activity time.
	ThreadAgentEntries() []ThreadAgentInfo
	// StopThreadAgent stops the agent for the given threadID.
	StopThreadAgent(threadID string)
	// Notify sends a message to the given channelID (threadID).
	Notify(channelID, msg string)
	// IsSilent returns whether the channel has silent mode enabled.
	IsSilent(channelID string) bool
}

// ThreadAgentInfo holds metadata about a thread agent for cleanup decisions.
type ThreadAgentInfo struct {
	ThreadID     string
	ParentChID   string
	LastActivity time.Time
}

// ThreadCleanupTask periodically kills idle thread agents.
type ThreadCleanupTask struct {
	deps       ThreadCleanupDeps
	idleSec    int
	maxAgents  int
}

func NewThreadCleanupTask(deps ThreadCleanupDeps, idleSec, maxAgents int) *ThreadCleanupTask {
	return &ThreadCleanupTask{deps: deps, idleSec: idleSec, maxAgents: maxAgents}
}

func (t *ThreadCleanupTask) Name() string               { return "thread-cleanup" }
func (t *ThreadCleanupTask) ShouldRun(_ time.Time) bool { return true }

func (t *ThreadCleanupTask) Run() error {
	entries := t.deps.ThreadAgentEntries()
	if len(entries) == 0 {
		return nil
	}

	now := time.Now()
	idleThreshold := time.Duration(t.idleSec) * time.Second

	// Kill idle agents
	for _, e := range entries {
		if now.Sub(e.LastActivity) > idleThreshold {
			log.Printf("[thread-cleanup] killing idle thread agent %s (idle %s)", e.ThreadID, now.Sub(e.LastActivity).Round(time.Second))
			t.deps.StopThreadAgent(e.ThreadID)
			if !t.deps.IsSilent(e.ParentChID) {
				t.deps.Notify(e.ThreadID, L.Get("thread_agent.idle_closed"))
			}
		}
	}

	return nil
}
