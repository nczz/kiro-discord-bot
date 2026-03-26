package channel

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
)

// Manager manages per-channel sessions and workers.
type Manager struct {
	mu              sync.Mutex
	workers         map[string]*Worker
	paused          map[string]bool
	store           *SessionStore
	acpClient       *acp.Client
	kiroCLI         string
	defaultCWD      string
	queueBufSize    int
	askTimeoutSec   int
	streamUpdateSec int
	defaultModel    string
}

func NewManager(store *SessionStore, acpClient *acp.Client, kiroCLI, defaultCWD string, queueBufSize, askTimeoutSec, streamUpdateSec int, defaultModel string) *Manager {
	return &Manager{
		workers:         make(map[string]*Worker),
		paused:          make(map[string]bool),
		store:           store,
		acpClient:       acpClient,
		kiroCLI:         kiroCLI,
		defaultCWD:      defaultCWD,
		queueBufSize:    queueBufSize,
		askTimeoutSec:   askTimeoutSec,
		streamUpdateSec: streamUpdateSec,
		defaultModel:    defaultModel,
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

	qLen := worker.QueueLen()
	_ = ds.MessageReactionAdd(job.ChannelID, job.MessageID, "⏳")
	if qLen > 1 {
		_, _ = ds.ChannelMessageSendReply(job.ChannelID, fmt.Sprintf("⏳ 排隊中（第 %d 位）", qLen), &discordgo.MessageReference{
			MessageID: job.MessageID,
			ChannelID: job.ChannelID,
		})
	}
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
		killProcessTree(sess.AgentPID)
		// Preserve CWD and Model across reset
		_ = m.store.Set(channelID, &Session{CWD: sess.CWD, Model: sess.Model})
	}
	return nil
}

// Status returns a human-readable status string for a channel.
func (m *Manager) Status(channelID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.store.Get(channelID)
	if !ok || sess.AgentName == "" {
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

	sid := sess.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}

	return fmt.Sprintf("Agent: `%s` | State: `%s` | Queue: %d | Session: `%s` | Model: `%s`",
		sess.AgentName, state, qLen, sid, modelDisplay(sess.Model))
}

// Cancel cancels the current running job for a channel.
func (m *Manager) Cancel(channelID string) error {
	sess, ok := m.store.Get(channelID)
	if !ok {
		return fmt.Errorf("no active session")
	}
	return m.acpClient.CancelAgent(sess.AgentName)
}

// StartAt resets the channel and starts a new agent at the given cwd.
func (m *Manager) StartAt(channelID, cwd string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing worker/agent
	if w, ok := m.workers[channelID]; ok {
		w.Stop()
		delete(m.workers, channelID)
	}
	if sess, ok := m.store.Get(channelID); ok {
		_ = m.acpClient.StopAgent(sess.AgentName)
		killProcessTree(sess.AgentPID)
	}
	// Store cwd (preserve model) so ensureWorker picks it up
	existing, _ := m.store.Get(channelID)
	newSess := &Session{CWD: cwd}
	if existing != nil {
		newSess.Model = existing.Model
	}
	_ = m.store.Set(channelID, newSess)

	_, err := m.startAgentAndWorker(channelID)
	return err
}

// CWD returns the current working directory for a channel.
func (m *Manager) CWD(channelID string) string {
	if sess, ok := m.store.Get(channelID); ok && sess.CWD != "" {
		return "Current CWD: `" + sess.CWD + "`"
	}
	return "Current CWD: `" + m.defaultCWD + "` (default)"
}

// SetCWD updates the working directory for a channel (takes effect on next reset).
func (m *Manager) SetCWD(channelID, cwd string) error {
	sess, ok := m.store.Get(channelID)
	if !ok {
		return m.store.Set(channelID, &Session{CWD: cwd})
	}
	sess.CWD = cwd
	return m.store.Set(channelID, sess)
}

// SetModel updates the model for a channel, then resets the agent to apply it.
func (m *Manager) SetModel(channelID, model string) error {
	sess, ok := m.store.Get(channelID)
	if !ok {
		sess = &Session{}
	}
	sess.Model = model
	if err := m.store.Set(channelID, sess); err != nil {
		return err
	}
	return nil
}

func modelDisplay(model string) string {
	if model == "" {
		return "default"
	}
	return model
}

// Model returns the current model for a channel.
func (m *Manager) Model(channelID string) string {
	if sess, ok := m.store.Get(channelID); ok && sess.Model != "" {
		return "Current model: `" + sess.Model + "`"
	}
	if m.defaultModel != "" {
		return "Current model: `" + m.defaultModel + "` (global default)"
	}
	return "Current model: `default` (kiro default)"
}

// ListModels calls kiro-cli to get available models.
func (m *Manager) ListModels() (string, error) {
	out, err := exec.Command(m.kiroCLI, "chat", "--list-models", "-f", "json").Output()
	if err != nil {
		return "", fmt.Errorf("list models: %w", err)
	}
	var result struct {
		Models []struct {
			Name        string  `json:"model_name"`
			ID          string  `json:"model_id"`
			Description string  `json:"description"`
			Rate        float64 `json:"rate_multiplier"`
			Unit        string  `json:"rate_unit"`
		} `json:"models"`
		Default string `json:"default_model"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("parse models: %w", err)
	}
	var sb strings.Builder
	sb.WriteString("**Available Models:**\n")
	for _, m := range result.Models {
		marker := " "
		if m.ID == result.Default {
			marker = "▸"
		}
		sb.WriteString(fmt.Sprintf("%s `%s` — %s (%.2f %s)\n", marker, m.ID, m.Description, m.Rate, m.Unit))
	}
	sb.WriteString("\nUsage: `/model <model-id>` or `!model <model-id>`")
	return sb.String(), nil
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
	model := m.defaultModel
	agentName := "ch-" + channelID

	// Use stored CWD/Model if available
	if sess, ok := m.store.Get(channelID); ok {
		if sess.CWD != "" {
			cwd = sess.CWD
		}
		if sess.Model != "" {
			model = sess.Model
		}
	}

	// Cancel + stop any existing agent with same name (best effort)
	_ = m.acpClient.CancelAgent(agentName)
	_ = m.acpClient.StopAgent(agentName)
	if sess, ok := m.store.Get(channelID); ok && sess.AgentPID > 0 {
		killProcessTree(sess.AgentPID)
	}

	beforePIDs := currentPIDs()

	status, err := m.acpClient.StartAgent(agentName, m.kiroCLI, cwd, model)
	if err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}

	agentPID := findNewPID(beforePIDs)
	if agentPID == 0 {
		time.Sleep(2 * time.Second)
		agentPID = findNewPID(beforePIDs)
	}
	if agentPID > 0 {
		log.Printf("[manager] agent %s pid=%d model=%q", agentName, agentPID, model)
	}

	if err := m.store.Set(channelID, &Session{
		AgentName: agentName,
		SessionID: status.SessionID,
		CWD:       cwd,
		AgentPID:  agentPID,
		Model:     model,
	}); err != nil {
		log.Printf("[manager] save session: %v", err)
	}

	w := NewWorker(channelID, agentName, m.queueBufSize, m.askTimeoutSec, m.streamUpdateSec, m.acpClient)
	w.Start()
	m.workers[channelID] = w
	return w, nil
}

// GetSession returns the session for a channel.
func (m *Manager) GetSession(channelID string) (*Session, bool) {
	return m.store.Get(channelID)
}

// GetAgentStatus returns the acp agent status.
func (m *Manager) GetAgentStatus(agentName string) (*acp.AgentStatus, error) {
	return m.acpClient.GetAgent(agentName)
}

// Pause sets the channel to mention-only mode.
func (m *Manager) Pause(channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused[channelID] = true
}

// Back sets the channel back to full-listen mode.
func (m *Manager) Back(channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused[channelID] = false
}

// IsPaused returns true if the channel is in mention-only mode.
func (m *Manager) IsPaused(channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused[channelID]
}

// findNewPID returns the PID of a "kiro-cli-chat" process that wasn't in the before set.
func findNewPID(before map[int]bool) int {
	out, err := exec.Command("pgrep", "-f", "kiro-cli-chat acp").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && !before[pid] {
			return pid
		}
	}
	return 0
}

// currentPIDs returns a set of PIDs matching "kiro-cli-chat acp".
func currentPIDs() map[int]bool {
	pids := make(map[int]bool)
	out, err := exec.Command("pgrep", "-f", "kiro-cli-chat acp").Output()
	if err != nil {
		return pids
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			pids[pid] = true
		}
	}
	return pids
}

// killProcessTree kills a process and all its descendants.
func killProcessTree(pid int) {
	if pid <= 0 {
		return
	}
	// Kill all children first
	if out, err := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if childPID, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
				killProcessTree(childPID)
			}
		}
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
}
