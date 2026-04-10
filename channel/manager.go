package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	L "github.com/nczz/kiro-discord-bot/locale"
)

// Manager manages per-channel sessions and workers.
type Manager struct {
	mu              sync.Mutex
	workers         map[string]*Worker
	agents          map[string]*acp.Agent
	paused          map[string]bool
	store           *SessionStore
	kiroCLI         string
	defaultCWD      string
	queueBufSize    int
	askTimeoutSec   int
	streamUpdateSec int
	threadArchive   int
	defaultModel    string
	logger          *ChatLogger
	botVersion      string
	guildID         string

	// Thread agent management
	threadAgents       map[string]*threadAgentEntry // threadID → entry
	threadAgentMax     int
	threadAgentIdleSec int
	maxScannerBuffer   int
	agentProfile       string // --agent flag
	trustAllTools      bool   // --trust-all-tools
	trustTools         string // --trust-tools <names>
}

// threadAgentEntry tracks a per-thread agent and its metadata.
type threadAgentEntry struct {
	agent           *acp.Agent
	worker          *Worker
	parentChannelID string
	threadID        string
	lastActivity    time.Time
}

// ManagerConfig holds configuration for creating a Manager.
type ManagerConfig struct {
	Store              *SessionStore
	KiroCLI            string
	DefaultCWD         string
	QueueBufSize       int
	AskTimeoutSec      int
	StreamUpdateSec    int
	ThreadArchive      int
	DefaultModel       string
	DataDir            string
	BotVersion         string
	GuildID            string
	ThreadAgentMax     int
	ThreadAgentIdleSec int
	MaxScannerBuffer   int
	AgentProfile       string
	TrustAllTools      bool
	TrustTools         string
}

func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		workers:            make(map[string]*Worker),
		agents:             make(map[string]*acp.Agent),
		paused:             make(map[string]bool),
		threadAgents:       make(map[string]*threadAgentEntry),
		store:              cfg.Store,
		kiroCLI:            cfg.KiroCLI,
		defaultCWD:         cfg.DefaultCWD,
		queueBufSize:       cfg.QueueBufSize,
		askTimeoutSec:      cfg.AskTimeoutSec,
		streamUpdateSec:    cfg.StreamUpdateSec,
		threadArchive:      cfg.ThreadArchive,
		defaultModel:       cfg.DefaultModel,
		logger:             NewChatLogger(cfg.DataDir),
		botVersion:         cfg.BotVersion,
		guildID:            cfg.GuildID,
		threadAgentMax:     cfg.ThreadAgentMax,
		threadAgentIdleSec: cfg.ThreadAgentIdleSec,
		maxScannerBuffer:   cfg.MaxScannerBuffer,
		agentProfile:       cfg.AgentProfile,
		trustAllTools:      cfg.TrustAllTools,
		trustTools:         cfg.TrustTools,
	}
}

// MaxScannerBuffer returns the configured scanner buffer limit in bytes.
func (m *Manager) MaxScannerBuffer() int { return m.maxScannerBuffer }

func (m *Manager) agentOpts() acp.AgentOptions {
	return acp.AgentOptions{
		MaxBuffer:     m.maxScannerBuffer,
		Agent:         m.agentProfile,
		TrustAllTools: m.trustAllTools,
		TrustTools:    m.trustTools,
		BotName:       "kiro-discord-bot",
		BotVersion:    m.botVersion,
	}
}

// stopChannel stops the worker and agent for a channel. Must be called with m.mu held.
func (m *Manager) stopChannel(channelID string) {
	if w, ok := m.workers[channelID]; ok {
		w.Stop()
		delete(m.workers, channelID)
	}
	if agent, ok := m.agents[channelID]; ok {
		agent.Stop()
		delete(m.agents, channelID)
	}
}

// StopAll stops all active workers and agents. Called during graceful shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for chID := range m.agents {
		m.stopChannel(chID)
	}
	for threadID, entry := range m.threadAgents {
		entry.worker.Stop()
		entry.agent.Stop()
		delete(m.threadAgents, threadID)
	}
	m.logger.Close()
	log.Println("[manager] all agents stopped")
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
		_, _ = ds.ChannelMessageSendReply(job.ChannelID, L.Getf("status.queued", qLen), &discordgo.MessageReference{
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

	sess, _ := m.store.Get(channelID)
	m.stopChannel(channelID)

	if sess != nil {
		if err := m.store.Set(channelID, &Session{CWD: sess.CWD, Model: sess.Model, GuildID: sess.GuildID}); err != nil {
			log.Printf("[manager] save session on reset: %v", err)
		}
	}
	return nil
}

// Restart stops the current agent and immediately starts a new one.
func (m *Manager) Restart(channelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, _ := m.store.Get(channelID)
	m.stopChannel(channelID)

	if sess != nil {
		if err := m.store.Set(channelID, &Session{CWD: sess.CWD, Model: sess.Model, GuildID: sess.GuildID}); err != nil {
			log.Printf("[manager] save session on restart: %v", err)
		}
	}
	_, err := m.startAgentAndWorker(channelID)
	return err
}

// Status returns a human-readable status string for a channel.
func (m *Manager) Status(channelID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.store.Get(channelID)
	if !ok || sess.AgentName == "" {
		return L.Get("status.no_session")
	}

	state := "unknown"
	kiroVersion := ""
	ctxUsage := 0.0
	if agent, ok := m.agents[channelID]; ok {
		state = agent.State()
		kiroVersion = agent.AgentVersion()
		ctxUsage = agent.ContextUsage()
		if !agent.IsAlive() {
			state = "dead"
		}
	}

	qLen := 0
	if w, ok := m.workers[channelID]; ok {
		qLen = w.QueueLen()
	}

	sid := sess.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}

	if kiroVersion == "" {
		kiroVersion = "n/a"
	}

	return L.Getf("status.format", sess.AgentName, state, qLen, sid, modelDisplay(sess.Model), kiroVersion, m.botVersion, ctxUsage)
}

// Cancel cancels the current running job for a channel.
func (m *Manager) Cancel(channelID string) error {
	m.mu.Lock()
	w, ok := m.workers[channelID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no active session")
	}
	w.CancelCurrent()
	return nil
}

// StartAt resets the channel and starts a new agent at the given cwd.
func (m *Manager) StartAt(channelID, cwd string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopChannel(channelID)

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
		return L.Getf("cwd.current", sess.CWD)
	}
	return L.Getf("cwd.default", m.defaultCWD)
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

// SetModel updates the model for a channel.
func (m *Manager) SetModel(channelID, model string) error {
	sess, ok := m.store.Get(channelID)
	if !ok {
		sess = &Session{}
	}
	sess.Model = model
	return m.store.Set(channelID, sess)
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
		return L.Getf("model.current", sess.Model)
	}
	if m.defaultModel != "" {
		return L.Getf("model.current_global", m.defaultModel)
	}
	return L.Get("model.current_default")
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
	sb.WriteString(L.Get("models.header"))
	for _, m := range result.Models {
		marker := " "
		if m.ID == result.Default {
			marker = "▸"
		}
		sb.WriteString(fmt.Sprintf("%s `%s` — %s (%.2f %s)\n", marker, m.ID, m.Description, m.Rate, m.Unit))
	}
	sb.WriteString(L.Get("models.footer"))
	return sb.String(), nil
}

// ensureWorker returns the worker for a channel, creating agent+worker if needed.
func (m *Manager) ensureWorker(channelID string) (*Worker, error) {
	if w, ok := m.workers[channelID]; ok {
		if agent, ok := m.agents[channelID]; ok && agent.IsAlive() {
			return w, nil
		}
		log.Printf("[manager] agent for %s died, restarting", channelID)
		m.stopChannel(channelID)
	}
	return m.startAgentAndWorker(channelID)
}

func (m *Manager) startAgentAndWorker(channelID string) (*Worker, error) {
	cwd := m.defaultCWD
	model := m.defaultModel
	agentName := "ch-" + channelID

	if sess, ok := m.store.Get(channelID); ok {
		if sess.CWD != "" {
			cwd = sess.CWD
		}
		if sess.Model != "" {
			model = sess.Model
		}
	}

	// Stop any existing agent with same name
	if old, ok := m.agents[channelID]; ok {
		old.Stop()
		delete(m.agents, channelID)
	}

	agent, err := acp.StartAgent(agentName, m.kiroCLI, cwd, model, m.agentOpts())
	if err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}
	m.agents[channelID] = agent

	// Watch for unexpected exit — auto-restart on next message
	agent.OnExitFunc(func() {
		m.mu.Lock()
		delete(m.agents, channelID)
		if w, ok := m.workers[channelID]; ok {
			w.Stop()
			delete(m.workers, channelID)
		}
		m.mu.Unlock()
		log.Printf("[manager] agent %s exited, will restart on next message", agentName)
	})

	// Inject previous conversation context into new session
	if ctx := m.logger.BuildContextPrompt(channelID, 10); ctx != "" {
		askCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = agent.Ask(askCtx, ctx+"Please acknowledge you have this context. Reply with: OK", nil)
		cancel()
		log.Printf("[manager] injected %d chars of history into %s", len(ctx), agentName)
	}

	if err := m.store.Set(channelID, &Session{
		AgentName: agentName,
		SessionID: agent.SessionID,
		CWD:       cwd,
		Model:     model,
		GuildID:   m.guildID,
	}); err != nil {
		log.Printf("[manager] save session: %v", err)
	}

	w := NewWorker(channelID, agent, m.queueBufSize, m.askTimeoutSec, m.streamUpdateSec, m.threadArchive, m.logger, model)
	w.Start()
	m.workers[channelID] = w
	return w, nil
}

// GetSession returns the session for a channel.
func (m *Manager) GetSession(channelID string) (*Session, bool) {
	return m.store.Get(channelID)
}

// GetAgent returns the agent for a channel.
func (m *Manager) GetAgent(channelID string) (*acp.Agent, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[channelID]
	return a, ok
}

// ActiveSessions returns all channels with an active agent (for heartbeat).
func (m *Manager) ActiveSessions() []struct{ ChannelID, AgentName string } {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []struct{ ChannelID, AgentName string }
	for chID := range m.agents {
		sess, ok := m.store.Get(chID)
		if ok && sess.AgentName != "" && (m.guildID == "" || sess.GuildID == m.guildID) {
			out = append(out, struct{ ChannelID, AgentName string }{chID, sess.AgentName})
		}
	}
	return out
}

// CheckAgent returns an error if the agent is not alive.
func (m *Manager) CheckAgent(channelID string) error {
	m.mu.Lock()
	agent, ok := m.agents[channelID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no agent")
	}
	if !agent.IsAlive() {
		return fmt.Errorf("agent dead")
	}
	return nil
}

// StartTempAgent starts a temporary agent (for cron jobs).
func (m *Manager) StartTempAgent(name, cwd, model string) (*acp.Agent, error) {
	return acp.StartAgent(name, m.kiroCLI, cwd, model, m.agentOpts())
}

// SendCommand sends a slash command (e.g. /compact, /clear) to the channel's agent.
func (m *Manager) SendCommand(channelID, command string) (string, error) {
	m.mu.Lock()
	agent, ok := m.agents[channelID]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("no agent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return agent.Ask(ctx, command, nil)
}

// StopTempAgent stops a temporary agent.
func (m *Manager) StopTempAgent(agent *acp.Agent) {
	agent.Stop()
}

// AskAgent sends a prompt to a named agent and returns the response.
func (m *Manager) AskAgent(ctx context.Context, agent *acp.Agent, prompt string) (string, error) {
	return agent.Ask(ctx, prompt, nil)
}

// AskAgentStream sends a prompt and collects all streamed chunks.
func (m *Manager) AskAgentStream(ctx context.Context, agent *acp.Agent, prompt string) (string, string, error) {
	var fullLog strings.Builder
	resp, err := agent.Ask(ctx, prompt, func(chunk string) {
		fullLog.WriteString(chunk)
	})
	if err != nil {
		return "", fullLog.String(), err
	}
	if resp == "" {
		resp = fullLog.String()
	}
	return resp, fullLog.String(), nil
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

// --- Thread Agent Management ---

// EnqueueThread routes a job to the thread's dedicated agent, spawning one if needed.
func (m *Manager) EnqueueThread(ds *discordgo.Session, job *Job, parentChannelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.threadAgents[job.ThreadID]
	if ok && !entry.agent.IsAlive() {
		// Dead agent — clean up and re-spawn
		entry.worker.Stop()
		entry.agent.Stop()
		delete(m.threadAgents, job.ThreadID)
		ok = false
	}

	if !ok {
		// Evict oldest if at capacity
		if len(m.threadAgents) >= m.threadAgentMax {
			m.evictOldestThreadAgent()
		}

		var err error
		entry, err = m.spawnThreadAgent(job.ThreadID, parentChannelID)
		if err != nil {
			return fmt.Errorf("spawn thread agent: %w", err)
		}
		m.threadAgents[job.ThreadID] = entry
	}

	entry.lastActivity = time.Now()
	job.Session = ds
	if err := entry.worker.Enqueue(job); err != nil {
		return fmt.Errorf("thread queue full")
	}

	_ = ds.MessageReactionAdd(job.ChannelID, job.MessageID, "⏳")
	return nil
}

// spawnThreadAgent creates a new agent+worker for a thread. Must be called with m.mu held.
func (m *Manager) spawnThreadAgent(threadID, parentChannelID string) (*threadAgentEntry, error) {
	cwd := m.defaultCWD
	model := m.defaultModel
	if sess, ok := m.store.Get(parentChannelID); ok {
		if sess.CWD != "" {
			cwd = sess.CWD
		}
		if sess.Model != "" {
			model = sess.Model
		}
	}

	agentName := "thread-" + threadID
	agent, err := acp.StartAgent(agentName, m.kiroCLI, cwd, model, m.agentOpts())
	if err != nil {
		return nil, err
	}

	entry := &threadAgentEntry{
		agent:           agent,
		parentChannelID: parentChannelID,
		threadID:        threadID,
		lastActivity:    time.Now(),
	}

	// Watch for unexpected exit
	agent.OnExitFunc(func() {
		m.mu.Lock()
		delete(m.threadAgents, threadID)
		m.mu.Unlock()
		log.Printf("[manager] thread agent %s exited unexpectedly", agentName)
	})

	// Inject conversation history
	m.injectThreadHistory(agent, parentChannelID, threadID)

	w := NewWorker("thread-"+threadID, agent, m.queueBufSize, m.askTimeoutSec, m.streamUpdateSec, 0, m.logger, model)
	w.Start()
	entry.worker = w

	log.Printf("[manager] spawned thread agent %s (parent=%s)", agentName, parentChannelID)
	return entry, nil
}

// StopThreadAgent stops a specific thread agent.
func (m *Manager) StopThreadAgent(threadID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.threadAgents[threadID]; ok {
		entry.worker.Stop()
		entry.agent.Stop()
		delete(m.threadAgents, threadID)
		log.Printf("[manager] stopped thread agent thread-%s", threadID)
	}
}

// CancelThreadAgent cancels the current job in a thread agent.
func (m *Manager) CancelThreadAgent(threadID string) error {
	m.mu.Lock()
	entry, ok := m.threadAgents[threadID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no thread agent")
	}
	entry.worker.CancelCurrent()
	return nil
}

// ThreadAgentEntries returns a snapshot of all thread agent entries for heartbeat inspection.
func (m *Manager) ThreadAgentEntries() []struct {
	ThreadID     string
	ParentChID   string
	LastActivity time.Time
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]struct {
		ThreadID     string
		ParentChID   string
		LastActivity time.Time
	}, 0, len(m.threadAgents))
	for _, e := range m.threadAgents {
		out = append(out, struct {
			ThreadID     string
			ParentChID   string
			LastActivity time.Time
		}{e.threadID, e.parentChannelID, e.lastActivity})
	}
	return out
}

// HasThreadAgent returns true if a thread agent exists for the given threadID.
func (m *Manager) HasThreadAgent(threadID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.threadAgents[threadID]
	return ok
}

// evictOldestThreadAgent removes the least recently active thread agent. Must be called with m.mu held.
func (m *Manager) evictOldestThreadAgent() {
	var oldestID string
	var oldestTime time.Time
	for id, e := range m.threadAgents {
		if oldestID == "" || e.lastActivity.Before(oldestTime) {
			oldestID = id
			oldestTime = e.lastActivity
		}
	}
	if oldestID != "" {
		if entry, ok := m.threadAgents[oldestID]; ok {
			entry.worker.Stop()
			entry.agent.Stop()
			delete(m.threadAgents, oldestID)
			log.Printf("[manager] evicted thread agent thread-%s (idle since %s)", oldestID, oldestTime.Format(time.RFC3339))
		}
	}
}

// injectThreadHistory loads conversation history into a thread agent.
// Tries thread-specific JSONL first, falls back to original task context from parent channel.
func (m *Manager) injectThreadHistory(agent *acp.Agent, parentChannelID, threadID string) {
	// Try thread-specific JSONL first
	ctx := m.logger.BuildContextPrompt("thread-"+threadID, 20)
	if ctx == "" {
		// No thread history yet — try to get the original task context from parent channel log
		// The last assistant response before this thread was created is the seed context
		ctx = m.logger.BuildContextPrompt(parentChannelID, 4)
	}
	if ctx == "" {
		return
	}

	// Truncate if too large (rough limit: 80K chars ≈ safe for most models)
	const maxCtxChars = 80000
	if len(ctx) > maxCtxChars {
		ctx = ctx[len(ctx)-maxCtxChars:]
	}

	askCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	_, _ = agent.Ask(askCtx, ctx+"Please acknowledge you have this context. Reply with: OK", nil)
	cancel()
	log.Printf("[manager] injected %d chars of history into thread-%s", len(ctx), threadID)
}
