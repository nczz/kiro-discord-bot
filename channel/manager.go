package channel

import (
	"fmt"
	"log"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/jianghongjun/kiro-discord-bot/acp"
)

// Manager manages per-channel sessions and workers.
type Manager struct {
	mu              sync.Mutex
	workers         map[string]*Worker
	store           *SessionStore
	acpClient       *acp.Client
	kiroCLI         string
	defaultCWD      string
	queueBufSize    int
	askTimeoutSec   int
	streamUpdateSec int
}

func NewManager(store *SessionStore, acpClient *acp.Client, kiroCLI, defaultCWD string, queueBufSize, askTimeoutSec, streamUpdateSec int) *Manager {
	return &Manager{
		workers:         make(map[string]*Worker),
		store:           store,
		acpClient:       acpClient,
		kiroCLI:         kiroCLI,
		defaultCWD:      defaultCWD,
		queueBufSize:    queueBufSize,
		askTimeoutSec:   askTimeoutSec,
		streamUpdateSec: streamUpdateSec,
	}
}

// Enqueue adds a job to the channel's queue, starting the agent/worker if needed.
func (m *Manager) Enqueue(ds *discordgo.Session, job *Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	worker, err := m.ensureWorker(job.ChannelID)
	if err != nil {
		return err
	}

	job.Session = ds
	if err := worker.Enqueue(job); err != nil {
		return fmt.Errorf("queue full (%d jobs pending)", worker.QueueLen())
	}

	// Add ⏳ reaction to user message
	_ = ds.MessageReactionAdd(job.ChannelID, job.MessageID, "⏳")
	return nil
}

// Reset stops the current agent and worker, clears session, starts fresh.
func (m *Manager) Reset(channelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if w, ok := m.workers[channelID]; ok {
		w.Stop()
		delete(m.workers, channelID)
	}

	if sess, ok := m.store.Get(channelID); ok {
		_ = m.acpClient.StopAgent(sess.AgentName)
		_ = m.store.Delete(channelID)
	}
	return nil
}

// Status returns a human-readable status string for a channel.
func (m *Manager) Status(channelID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.store.Get(channelID)
	if !ok {
		return "No active session."
	}

	agentStatus, err := m.acpClient.GetAgent(sess.AgentName)
	state := "unknown"
	if err == nil {
		state = agentStatus.State
	}

	qLen := 0
	if w, ok := m.workers[channelID]; ok {
		qLen = w.QueueLen()
	}

	return fmt.Sprintf("Agent: `%s` | State: `%s` | Queue: %d | Session: `%s`",
		sess.AgentName, state, qLen, sess.SessionID[:8])
}

// Cancel cancels the current running job for a channel.
func (m *Manager) Cancel(channelID string) error {
	sess, ok := m.store.Get(channelID)
	if !ok {
		return fmt.Errorf("no active session")
	}
	return m.acpClient.CancelAgent(sess.AgentName)
}

// SetCWD updates the working directory for a channel (takes effect on next reset).
func (m *Manager) SetCWD(channelID, cwd string) error {
	sess, ok := m.store.Get(channelID)
	if !ok {
		// Store a placeholder session with just the cwd
		return m.store.Set(channelID, &Session{CWD: cwd})
	}
	sess.CWD = cwd
	return m.store.Set(channelID, sess)
}

// ensureWorker returns the worker for a channel, creating agent+worker if needed.
// Must be called with m.mu held.
func (m *Manager) ensureWorker(channelID string) (*Worker, error) {
	if w, ok := m.workers[channelID]; ok {
		// Verify agent is still alive
		sess, _ := m.store.Get(channelID)
		if sess != nil {
			if _, err := m.acpClient.GetAgent(sess.AgentName); err == nil {
				return w, nil
			}
			// Agent died — restart it
			log.Printf("[manager] agent %s died, restarting", sess.AgentName)
			w.Stop()
			delete(m.workers, channelID)
		}
	}

	return m.startAgentAndWorker(channelID)
}

func (m *Manager) startAgentAndWorker(channelID string) (*Worker, error) {
	cwd := m.defaultCWD
	agentName := "ch-" + channelID

	// Use stored CWD if available
	if sess, ok := m.store.Get(channelID); ok && sess.CWD != "" {
		cwd = sess.CWD
	}

	// Stop any existing agent with same name (best effort)
	_ = m.acpClient.StopAgent(agentName)

	status, err := m.acpClient.StartAgent(agentName, m.kiroCLI, cwd)
	if err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}

	if err := m.store.Set(channelID, &Session{
		AgentName: agentName,
		SessionID: status.SessionID,
		CWD:       cwd,
	}); err != nil {
		log.Printf("[manager] save session: %v", err)
	}

	w := NewWorker(channelID, agentName, m.queueBufSize, m.askTimeoutSec, m.streamUpdateSec, m.acpClient)
	w.Start()
	m.workers[channelID] = w
	return w, nil
}
