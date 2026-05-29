package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	silent          map[string]bool // channelID → silent mode (default true when absent)
	store           *SessionStore
	kiroCLI         string
	defaultCWD      string
	allowedCwdRoots []string
	queueBufSize    int
	askTimeoutSec   int
	streamUpdateSec int
	threadArchive   int
	defaultModel    string
	logger          *ChatLogger
	usage           *UsageStore
	botVersion      string
	guildID         string
	botID           string
	dataDir         string

	// Memory
	memory      *MemoryStore
	flashMemory map[string][]string // channelID → session-scoped entries

	// Thread agent management
	threadAgents       map[string]*threadAgentEntry // threadID → entry
	threadAgentMax     int
	threadAgentIdleSec int
	maxScannerBuffer   int
	agentProfile       string // --agent flag
	trustAllTools      bool   // --trust-all-tools
	trustTools         string // --trust-tools <names>

	// Channel agent idle tracking
	channelLastActivity map[string]time.Time
	channelAgentIdleSec int
}

// threadAgentEntry tracks a per-thread agent and its metadata.
type threadAgentEntry struct {
	agent           *acp.Agent
	worker          *Worker
	parentChannelID string
	threadID        string
	lastActivity    time.Time
	closeWhenIdle   bool
}

// ThreadAgentLimitCandidate is an inactive thread agent the user may choose to close.
type ThreadAgentLimitCandidate struct {
	ThreadID     string
	ParentChID   string
	LastActivity time.Time
}

// ThreadAgentLimitError reports that no new thread agent can be started
// without exceeding the configured capacity.
type ThreadAgentLimitError struct {
	Max        int
	Active     int
	Inactive   int
	Candidates []ThreadAgentLimitCandidate
}

func (e *ThreadAgentLimitError) Error() string {
	return fmt.Sprintf("thread agent limit reached: max=%d active=%d inactive=%d", e.Max, e.Active, e.Inactive)
}

// ManagerConfig holds configuration for creating a Manager.
type ManagerConfig struct {
	Store                *SessionStore // set by bot.go after creation
	KiroCLIPath          string
	DefaultCWD           string
	AllowedCwdRoots      string
	QueueBufferSize      int
	AskTimeoutSec        int
	StreamUpdateSec      int
	ThreadAutoArchive    int
	KiroModel            string
	DataDir              string
	BotVersion           string
	GuildID              string
	ThreadAgentMax       int
	ThreadAgentIdleSec   int
	ChannelAgentIdleSec  int
	MaxScannerBuffer     int
	AgentProfile         string
	TrustAllTools        bool
	TrustTools           string
	BotID                string
	UsageTimezone        string
	UsageRetentionMonths int
}

func NewManager(cfg ManagerConfig) *Manager {
	m := &Manager{
		workers:             make(map[string]*Worker),
		agents:              make(map[string]*acp.Agent),
		paused:              make(map[string]bool),
		silent:              make(map[string]bool),
		threadAgents:        make(map[string]*threadAgentEntry),
		store:               cfg.Store,
		kiroCLI:             cfg.KiroCLIPath,
		defaultCWD:          cfg.DefaultCWD,
		allowedCwdRoots:     parseCwdRoots(cfg.AllowedCwdRoots),
		queueBufSize:        cfg.QueueBufferSize,
		askTimeoutSec:       cfg.AskTimeoutSec,
		streamUpdateSec:     cfg.StreamUpdateSec,
		threadArchive:       cfg.ThreadAutoArchive,
		defaultModel:        cfg.KiroModel,
		logger:              NewChatLogger(cfg.DataDir),
		usage:               NewUsageStore(cfg.DataDir, cfg.UsageTimezone, cfg.UsageRetentionMonths),
		botVersion:          cfg.BotVersion,
		guildID:             cfg.GuildID,
		botID:               cfg.BotID,
		dataDir:             cfg.DataDir,
		memory:              NewMemoryStore(cfg.DataDir),
		flashMemory:         make(map[string][]string),
		threadAgentMax:      cfg.ThreadAgentMax,
		threadAgentIdleSec:  cfg.ThreadAgentIdleSec,
		channelAgentIdleSec: cfg.ChannelAgentIdleSec,
		channelLastActivity: make(map[string]time.Time),
		maxScannerBuffer:    cfg.MaxScannerBuffer,
		agentProfile:        cfg.AgentProfile,
		trustAllTools:       cfg.TrustAllTools,
		trustTools:          cfg.TrustTools,
	}
	if err := m.loadListenModes(); err != nil {
		log.Printf("[manager] load listen modes: %v", err)
	}
	return m
}

// MaxScannerBuffer returns the configured scanner buffer limit in bytes.
func (m *Manager) MaxScannerBuffer() int { return m.maxScannerBuffer }

// ThreadArchive returns the configured thread auto-archive duration in minutes.
func (m *Manager) ThreadArchive() int { return m.threadArchive }

// DefaultCWD returns the configured default working directory.
func (m *Manager) DefaultCWD() string { return m.defaultCWD }

// SetBotID scopes persisted ACP sessions to this Discord bot identity.
func (m *Manager) SetBotID(botID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.botID = strings.TrimSpace(botID)
}

const (
	sessionTargetChannel = "channel"
	sessionTargetThread  = "thread"
	interruptGrace       = 5 * time.Second
)

func (m *Manager) sessionKey(targetType, targetID string) string {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return ""
	}
	if m.guildID == "" || m.botID == "" {
		if targetType == sessionTargetChannel {
			return targetID
		}
		return targetType + ":" + targetID
	}
	return strings.Join([]string{"g", m.guildID, "b", m.botID, targetType, targetID}, ":")
}

func (m *Manager) getChannelSession(channelID string) (*Session, bool) {
	key := m.sessionKey(sessionTargetChannel, channelID)
	if key != "" {
		if sess, ok := m.store.Get(key); ok {
			return sess, true
		}
	}
	if key != channelID {
		return m.store.Get(channelID)
	}
	return nil, false
}

func (m *Manager) setChannelSession(channelID string, sess *Session) error {
	key := m.sessionKey(sessionTargetChannel, channelID)
	if key == "" {
		return fmt.Errorf("empty channel session key")
	}
	if sess == nil {
		sess = &Session{}
	}
	sess.GuildID = m.guildID
	sess.BotID = m.botID
	sess.TargetType = sessionTargetChannel
	sess.TargetID = channelID
	return m.store.Set(key, sess)
}

func (m *Manager) getThreadSession(threadID string) (*Session, bool) {
	return m.store.Get(m.sessionKey(sessionTargetThread, threadID))
}

func (m *Manager) setThreadSession(threadID, parentChannelID string, sess *Session) error {
	key := m.sessionKey(sessionTargetThread, threadID)
	if key == "" {
		return fmt.Errorf("empty thread session key")
	}
	if sess == nil {
		sess = &Session{}
	}
	sess.GuildID = m.guildID
	sess.BotID = m.botID
	sess.TargetType = sessionTargetThread
	sess.TargetID = threadID
	sess.ParentChannelID = parentChannelID
	return m.store.Set(key, sess)
}

// AllowedCwdRoots returns a copy of configured cwd allowlist roots.
func (m *Manager) AllowedCwdRoots() []string {
	out := make([]string, len(m.allowedCwdRoots))
	copy(out, m.allowedCwdRoots)
	return out
}

func parseCwdRoots(raw string) []string {
	if raw == "" {
		return nil
	}
	var roots []string
	for _, part := range strings.Split(raw, ",") {
		root := strings.TrimSpace(part)
		if root == "" {
			continue
		}
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
		if real, err := filepath.EvalSymlinks(root); err == nil {
			root = real
		}
		roots = append(roots, filepath.Clean(root))
	}
	return roots
}

// ValidateCWD checks whether cwd exists and is inside ALLOWED_CWD_ROOTS when configured.
func (m *Manager) ValidateCWD(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", fmt.Errorf("working directory is empty")
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("working directory not found: %s", cwd)
	}
	if fi, err := os.Stat(real); err != nil {
		return "", fmt.Errorf("working directory not found: %s", cwd)
	} else if !fi.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", cwd)
	}
	if len(m.allowedCwdRoots) == 0 {
		return real, nil
	}
	for _, root := range m.allowedCwdRoots {
		if pathWithinRoot(real, root) {
			return real, nil
		}
	}
	return "", fmt.Errorf("working directory %s is outside ALLOWED_CWD_ROOTS: %s", real, strings.Join(m.allowedCwdRoots, ", "))
}

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

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
	delete(m.channelLastActivity, channelID)
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
	m.channelLastActivity[job.ChannelID] = time.Now()

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

	sess, _ := m.getChannelSession(channelID)
	m.stopChannel(channelID)
	delete(m.flashMemory, channelID)

	if sess != nil {
		if err := m.setChannelSession(channelID, &Session{CWD: sess.CWD, Model: sess.Model}); err != nil {
			log.Printf("[manager] save session on reset: %v", err)
		}
	}
	return nil
}

// Restart stops the current agent and immediately starts a new one.
func (m *Manager) Restart(channelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, _ := m.getChannelSession(channelID)
	m.stopChannel(channelID)

	if sess != nil {
		if err := m.setChannelSession(channelID, &Session{CWD: sess.CWD, Model: sess.Model}); err != nil {
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

	sess, ok := m.getChannelSession(channelID)
	if !ok || sess.AgentName == "" {
		return L.Get("status.no_session")
	}

	state := "unknown"
	kiroVersion := ""
	ctxUsage := 0.0
	qLen := 0
	if agent, ok := m.agents[channelID]; ok {
		state = agent.State()
		kiroVersion = agent.AgentVersion()
		ctxUsage = agent.ContextUsage()
		if !agent.IsAlive() {
			state = "dead"
		}
	}

	if w, ok := m.workers[channelID]; ok {
		qLen = w.QueueLen()
	}

	return m.formatStatus(sess, state, qLen, kiroVersion, ctxUsage)
}

// ThreadStatus returns the status string for a thread agent.
func (m *Manager) ThreadStatus(threadID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.getThreadSession(threadID)
	if !ok || sess.AgentName == "" {
		return L.Get("status.no_session")
	}

	state := "unknown"
	kiroVersion := ""
	ctxUsage := 0.0
	qLen := 0
	if entry, ok := m.threadAgents[threadID]; ok {
		state = entry.agent.State()
		kiroVersion = entry.agent.AgentVersion()
		ctxUsage = entry.agent.ContextUsage()
		if !entry.agent.IsAlive() {
			state = "dead"
		}
		qLen = entry.worker.QueueLen()
	}

	return m.formatStatus(sess, state, qLen, kiroVersion, ctxUsage)
}

func (m *Manager) formatStatus(sess *Session, state string, qLen int, kiroVersion string, ctxUsage float64) string {
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
	if !w.CancelCurrent() {
		if w.IsActive() {
			return fmt.Errorf("active job is not cancellable yet")
		}
		return fmt.Errorf("no active job")
	}
	return nil
}

// Interrupt cancels the current channel job and escalates to a process-level
// interrupt if the same job does not finish within a short grace period.
func (m *Manager) Interrupt(channelID string) error {
	m.mu.Lock()
	w, ok := m.workers[channelID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no active session")
	}
	if !w.InterruptCurrent(interruptGrace) {
		return fmt.Errorf("no active job")
	}
	return nil
}

// StartAt resets the channel and starts a new agent at the given cwd.
func (m *Manager) StartAt(channelID, cwd string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cwd, err := m.ValidateCWD(cwd)
	if err != nil {
		return err
	}
	m.stopChannel(channelID)

	existing, _ := m.getChannelSession(channelID)
	newSess := &Session{CWD: cwd}
	if existing != nil {
		newSess.Model = existing.Model
	}
	_ = m.setChannelSession(channelID, newSess)

	_, err = m.startAgentAndWorker(channelID)
	return err
}

// CWD returns the current working directory for a channel.
func (m *Manager) CWD(channelID string) string {
	if sess, ok := m.getChannelSession(channelID); ok && sess.CWD != "" {
		return L.Getf("cwd.current", sess.CWD)
	}
	return L.Getf("cwd.default", m.defaultCWD)
}

// SetCWD updates the working directory for a channel (takes effect on next reset).
func (m *Manager) SetCWD(channelID, cwd string) error {
	cwd, err := m.ValidateCWD(cwd)
	if err != nil {
		return err
	}
	sess, ok := m.getChannelSession(channelID)
	if !ok {
		return m.setChannelSession(channelID, &Session{CWD: cwd})
	}
	sess.CWD = cwd
	return m.setChannelSession(channelID, sess)
}

// SetModel updates the model for a channel.
func (m *Manager) SetModel(channelID, model string) error {
	sess, ok := m.getChannelSession(channelID)
	if !ok {
		sess = &Session{}
	}
	sess.Model = model
	return m.setChannelSession(channelID, sess)
}

// SwitchModel attempts a dynamic model switch via session/set_model.
// Falls back to Restart if the agent doesn't support it or isn't running.
// Returns (restarted bool, err).
func (m *Manager) SwitchModel(channelID, model string) (bool, error) {
	m.mu.Lock()
	agent, ok := m.agents[channelID]
	worker := m.workers[channelID]
	m.mu.Unlock()
	if ok && agent.IsAlive() {
		if err := validateAgentModel(agent, model); err != nil {
			return false, err
		}
		if err := agent.SetModel(model); err == nil {
			if err := m.SetModel(channelID, model); err != nil {
				return false, err
			}
			if worker != nil {
				worker.model = model
			}
			log.Printf("[manager] dynamic model switch to %s for %s", model, channelID)
			return false, nil
		}
		log.Printf("[manager] dynamic model switch failed, restarting: %v", model)
	}

	if err := m.validateModelID(model); err != nil {
		return false, err
	}
	oldSess, _ := m.getChannelSession(channelID)
	var oldCopy *Session
	if oldSess != nil {
		cp := *oldSess
		oldCopy = &cp
	}
	if err := m.SetModel(channelID, model); err != nil {
		return false, err
	}
	if err := m.Restart(channelID); err != nil {
		if oldCopy != nil {
			_ = m.setChannelSession(channelID, oldCopy)
		}
		return true, err
	}
	return true, nil
}

func validateAgentModel(agent *acp.Agent, model string) error {
	if models := agent.AvailableModels(); len(models) > 0 && !agent.HasModel(model) {
		return fmt.Errorf("unknown model %q; available models: %s", model, modelIDs(models))
	}
	return nil
}

// SwitchMode attempts a dynamic mode switch via session/set_mode.
// Returns error if agent is not running or mode switch fails.
func (m *Manager) SwitchMode(channelID, modeID string) error {
	m.mu.Lock()
	agent, ok := m.agents[channelID]
	m.mu.Unlock()
	if !ok || !agent.IsAlive() {
		return fmt.Errorf("no active agent")
	}
	if modes := agent.AvailableModes(); len(modes) > 0 && !agent.HasMode(modeID) {
		return fmt.Errorf("unknown mode %q; available modes: %s", modeID, modeIDs(modes))
	}
	return agent.SetMode(modeID)
}

func modelIDs(models []acp.ModelEntry) string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ModelID)
	}
	return strings.Join(ids, ", ")
}

func (m *Manager) validateModelID(model string) error {
	if model == "" {
		return nil
	}
	models, _, err := m.cliModels()
	if err != nil {
		return err
	}
	for _, entry := range models {
		if entry.ModelID == model {
			return nil
		}
	}
	return fmt.Errorf("unknown model %q; available models: %s", model, modelIDs(models))
}

func (m *Manager) cliModels() ([]acp.ModelEntry, string, error) {
	out, err := exec.Command(m.kiroCLI, "chat", "--list-models", "-f", "json").Output()
	if err != nil {
		return nil, "", fmt.Errorf("list models: %w", err)
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
		return nil, "", fmt.Errorf("parse models: %w", err)
	}
	models := make([]acp.ModelEntry, 0, len(result.Models))
	for _, model := range result.Models {
		models = append(models, acp.ModelEntry{
			ModelID:     model.ID,
			Name:        model.Name,
			Description: model.Description,
		})
	}
	return models, result.Default, nil
}

func modeIDs(modes []acp.ModeEntry) string {
	ids := make([]string, 0, len(modes))
	for _, mode := range modes {
		ids = append(ids, mode.ID)
	}
	return strings.Join(ids, ", ")
}

// AgentModes returns available modes for a channel's agent.
func (m *Manager) AgentModes(channelID string) (current string, modes []acp.ModeEntry) {
	m.mu.Lock()
	agent, ok := m.agents[channelID]
	m.mu.Unlock()
	if !ok || !agent.IsAlive() {
		return "", nil
	}
	return agent.CurrentModeID(), agent.AvailableModes()
}

func modelDisplay(model string) string {
	if model == "" {
		return "default"
	}
	return model
}

// Model returns the current model for a channel.
func (m *Manager) Model(channelID string) string {
	if sess, ok := m.getChannelSession(channelID); ok && sess.Model != "" {
		return L.Getf("model.current", sess.Model)
	}
	if m.defaultModel != "" {
		return L.Getf("model.current_global", m.defaultModel)
	}
	return L.Get("model.current_default")
}

// ListModels calls kiro-cli to get available models.
// If channelID is provided, it marks the channel's current model instead of global default.
func (m *Manager) ListModels(channelID string) (string, error) {
	// Try to get models from active agent's session response (no subprocess needed)
	m.mu.Lock()
	agent, agentOk := m.agents[channelID]
	m.mu.Unlock()
	if agentOk && agent.IsAlive() {
		if models := agent.AvailableModels(); len(models) > 0 {
			return m.formatAgentModels(channelID, agent.CurrentModelID(), models), nil
		}
	}

	models, defaultModel, err := m.cliModels()
	if err != nil {
		return "", err
	}

	currentModel := defaultModel
	if channelID != "" {
		if sess, ok := m.getChannelSession(channelID); ok && sess.Model != "" {
			currentModel = sess.Model
		} else if m.defaultModel != "" {
			currentModel = m.defaultModel
		}
	}

	var sb strings.Builder
	sb.WriteString(L.Get("models.header"))
	for _, m := range models {
		marker := " "
		if m.ModelID == currentModel {
			marker = "▸"
		}
		sb.WriteString(fmt.Sprintf("%s `%s` — %s\n", marker, m.ModelID, m.Description))
	}
	sb.WriteString(L.Get("models.footer"))
	return sb.String(), nil
}

// formatAgentModels formats the model list from agent's session response.
func (m *Manager) formatAgentModels(channelID, agentCurrentModel string, models []acp.ModelEntry) string {
	// Use channel's configured model as current if available
	currentModel := agentCurrentModel
	if sess, ok := m.getChannelSession(channelID); ok && sess.Model != "" {
		currentModel = sess.Model
	}

	var sb strings.Builder
	sb.WriteString(L.Get("models.header"))
	for _, model := range models {
		marker := " "
		if model.ModelID == currentModel {
			marker = "▸"
		}
		sb.WriteString(fmt.Sprintf("%s `%s` — %s\n", marker, model.ModelID, model.Description))
	}
	sb.WriteString(L.Get("models.footer"))
	return sb.String()
}

// --- Memory (persistent) ---

func (m *Manager) MemoryAdd(channelID, entry string) error { return m.memory.Add(channelID, entry) }

// ClearHistory truncates the JSONL conversation log for a channel.
func (m *Manager) ClearHistory(channelID string)        { m.logger.ClearLog(channelID) }
func (m *Manager) MemoryList(channelID string) []string { return m.memory.List(channelID) }
func (m *Manager) MemoryRemove(channelID string, idx int) error {
	return m.memory.Remove(channelID, idx)
}
func (m *Manager) MemoryClear(channelID string) error { return m.memory.Clear(channelID) }

// --- Flash Memory (session-scoped, in-memory only) ---

func (m *Manager) FlashMemoryAdd(channelID, entry string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flashMemory[channelID] = append(m.flashMemory[channelID], entry)
}

func (m *Manager) FlashMemoryList(channelID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.flashMemory[channelID]))
	copy(out, m.flashMemory[channelID])
	return out
}

func (m *Manager) FlashMemoryRemove(channelID string, idx int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.flashMemory[channelID]
	if idx < 0 || idx >= len(entries) {
		return fmt.Errorf("index out of range: %d", idx)
	}
	m.flashMemory[channelID] = append(entries[:idx], entries[idx+1:]...)
	return nil
}

func (m *Manager) FlashMemoryClear(channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.flashMemory, channelID)
}

// BuildMemoryPrefix returns the memory + flash memory block to prepend to prompts.
// Must be called with m.mu held.
func (m *Manager) BuildMemoryPrefix(channelID string) string {
	mem := m.memory.List(channelID)
	flash := m.flashMemory[channelID]
	if len(mem) == 0 && len(flash) == 0 {
		return ""
	}
	var sb strings.Builder
	if len(mem) > 0 {
		sb.WriteString("[Memory Rules — always follow these]\n")
		for i, e := range mem {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, e))
		}
		sb.WriteString("\n")
	}
	if len(flash) > 0 {
		sb.WriteString("[Flash Memory — current session emphasis]\n")
		for i, e := range flash {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, e))
		}
		sb.WriteString("\n")
	}
	return sb.String()
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

	if sess, ok := m.getChannelSession(channelID); ok {
		if sess.CWD != "" {
			cwd = sess.CWD
		}
		if sess.Model != "" {
			model = sess.Model
		}
	}
	var err error
	cwd, err = m.ValidateCWD(cwd)
	if err != nil {
		return nil, err
	}

	// Stop any existing agent with same name
	if old, ok := m.agents[channelID]; ok {
		old.Stop()
		delete(m.agents, channelID)
	}

	// Try to load previous session if available
	opts := m.agentOpts()
	if sess, ok := m.getChannelSession(channelID); ok && sess.SessionID != "" {
		opts.LoadSessionID = sess.SessionID
	}

	agent, err := acp.StartAgent(agentName, m.kiroCLI, cwd, model, opts)
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

	// Prepare conversation history only if session/load was not used
	var historyCtx string
	if !agent.LoadedSession() {
		historyCtx = m.logger.BuildContextPrompt(channelID, 10)
		if historyCtx != "" {
			log.Printf("[manager] prepared %d chars of history for %s", len(historyCtx), agentName)
		}
	}

	if err := m.setChannelSession(channelID, &Session{
		AgentName: agentName,
		SessionID: agent.SessionID,
		CWD:       cwd,
		Model:     model,
	}); err != nil {
		log.Printf("[manager] save session: %v", err)
	}

	w := NewWorker(channelID, agent, m.queueBufSize, m.askTimeoutSec, m.streamUpdateSec, m.threadArchive, m.logger, model)
	w.SetUsageStore(m.usage)
	w.SetHistoryPrefix(historyCtx)
	w.OnMemoryPrefixFunc(func() string {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.BuildMemoryPrefix(channelID)
	})
	w.OnSilentFunc(func() bool { return m.IsSilent(channelID) })
	w.OnActivityFunc(func() {
		m.mu.Lock()
		m.channelLastActivity[channelID] = time.Now()
		m.mu.Unlock()
	})
	w.Start()
	m.workers[channelID] = w
	return w, nil
}

// GetSession returns the session for a channel.
func (m *Manager) GetSession(channelID string) (*Session, bool) {
	return m.getChannelSession(channelID)
}

// UsageReport returns credit usage aggregated for the current day, week, and month.
func (m *Manager) UsageReport(guildID, channelID, userID string, limit int) (UsageReport, error) {
	if m.usage == nil {
		return UsageReport{}, fmt.Errorf("usage store not configured")
	}
	return m.usage.Report(guildID, channelID, userID, limit, time.Now())
}

// RecordUsage appends one usage record to the usage ledger.
func (m *Manager) RecordUsage(record UsageRecord) error {
	if m.usage == nil {
		return fmt.Errorf("usage store not configured")
	}
	return m.usage.Append(record)
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
		sess, ok := m.getChannelSession(chID)
		if ok && sess.AgentName != "" {
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

// Doctor runs deployment diagnostics that are useful before accepting live work.
func (m *Manager) Doctor(ctx context.Context) string {
	var sb strings.Builder
	sb.WriteString("**Doctor**\n")

	resolvedCLI, err := acp.ResolveKiroCLI(m.kiroCLI)
	if err != nil {
		sb.WriteString("❌ kiro-cli: " + err.Error() + "\n")
	} else {
		sb.WriteString("✅ kiro-cli: `" + resolvedCLI + "`")
		if out, err := exec.CommandContext(ctx, resolvedCLI, "--version").Output(); err == nil {
			sb.WriteString(" — `" + strings.TrimSpace(string(out)) + "`")
		} else {
			sb.WriteString(" — version check failed: `" + err.Error() + "`")
		}
		sb.WriteString("\n")
	}

	if cwd, err := m.ValidateCWD(m.defaultCWD); err != nil {
		sb.WriteString("❌ default cwd: " + err.Error() + "\n")
	} else {
		sb.WriteString("✅ default cwd: `" + cwd + "`\n")
	}

	if len(m.allowedCwdRoots) == 0 {
		sb.WriteString("⚠️ cwd allowlist: not configured\n")
	} else {
		sb.WriteString("✅ cwd allowlist: `" + strings.Join(m.allowedCwdRoots, "`, `") + "`\n")
	}

	if m.trustTools != "" {
		sb.WriteString("✅ ACP tool policy: allowlisted tools `" + m.trustTools + "`\n")
	} else if m.trustAllTools {
		sb.WriteString("⚠️ ACP tool policy: trust all tools\n")
	} else {
		sb.WriteString("✅ ACP tool policy: deny by default\n")
	}

	m.mu.Lock()
	activeAgents := len(m.agents)
	activeThreadAgents := len(m.threadAgents)
	m.mu.Unlock()
	sb.WriteString(fmt.Sprintf("✅ agents: channels=%d threads=%d thread_limit=%d\n", activeAgents, activeThreadAgents, m.threadAgentMax))

	if err := os.MkdirAll(m.dataDir, 0755); err != nil {
		sb.WriteString("❌ data dir: " + err.Error() + "\n")
	} else {
		probe := filepath.Join(m.dataDir, ".doctor-write-test")
		if err := os.WriteFile(probe, []byte("ok\n"), 0644); err != nil {
			sb.WriteString("❌ data dir writable: " + err.Error() + "\n")
		} else {
			_ = os.Remove(probe)
			sb.WriteString("✅ data dir writable: `" + m.dataDir + "`\n")
		}
	}

	if err := acp.PreflightCheck(m.kiroCLI); err != nil {
		sb.WriteString("❌ ACP preflight: " + err.Error() + "\n")
	} else {
		sb.WriteString("✅ ACP preflight: passed\n")
	}

	return sb.String()
}

// StartTempAgent starts a temporary agent (for cron jobs).
func (m *Manager) StartTempAgent(name, cwd, model string) (*acp.Agent, error) {
	if strings.TrimSpace(cwd) == "" {
		cwd = m.defaultCWD
	}
	var err error
	cwd, err = m.ValidateCWD(cwd)
	if err != nil {
		return nil, err
	}
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
	if err := m.saveListenModesLocked(); err != nil {
		log.Printf("[manager] save listen mode for %s: %v", channelID, err)
	}
}

// Back sets the channel back to full-listen mode.
func (m *Manager) Back(channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused[channelID] = false
	if err := m.saveListenModesLocked(); err != nil {
		log.Printf("[manager] save listen mode for %s: %v", channelID, err)
	}
}

func (m *Manager) listenModesPath() string {
	if m.dataDir == "" {
		return ""
	}
	return filepath.Join(m.dataDir, "listen_modes.json")
}

func (m *Manager) loadListenModes() error {
	path := m.listenModesPath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var modes map[string]string
	if err := json.Unmarshal(data, &modes); err != nil {
		return err
	}
	for channelID, mode := range modes {
		switch mode {
		case "mention":
			m.paused[channelID] = true
		case "full":
			m.paused[channelID] = false
		}
	}
	return nil
}

func (m *Manager) saveListenModesLocked() error {
	path := m.listenModesPath()
	if path == "" {
		return nil
	}
	modes := make(map[string]string, len(m.paused))
	for channelID, paused := range m.paused {
		if paused {
			modes[channelID] = "mention"
		} else {
			modes[channelID] = "full"
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(modes, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// HasFullListenOverride returns true when the channel/thread was explicitly
// resumed with /back. An absent entry means "use the default mode", which may
// still be mention-only in multi-bot channels.
func (m *Manager) HasFullListenOverride(channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	paused, ok := m.paused[channelID]
	return ok && !paused
}

// HasMentionOnlyOverride returns true when the channel/thread was explicitly
// paused with /pause. This lets a thread override a parent channel's /back.
func (m *Manager) HasMentionOnlyOverride(channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused[channelID]
}

// IsPaused returns true if the channel is in mention-only mode.
func (m *Manager) IsPaused(channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused[channelID]
}

// SetSilent sets the silent (compact output) mode for a channel.
func (m *Manager) SetSilent(channelID string, on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.silent[channelID] = on
}

// IsSilent returns true if the channel is in silent mode (compact tool output).
// Default is true (silent) when not explicitly set.
func (m *Manager) IsSilent(channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.silent[channelID]
	if !ok {
		return true
	}
	return v
}

// --- Thread Agent Management ---

// EnqueueThread routes a job to the thread's dedicated agent, spawning one if needed.
func (m *Manager) EnqueueThread(ds *discordgo.Session, job *Job, parentChannelID string) error {
	var discordCtx string
	if job.Handoff {
		discordCtx = m.buildDiscordThreadHandoffContext(ds, job.ThreadID, job.MessageID)
	} else {
		discordCtx = m.buildDiscordThreadContext(ds, job.ThreadID, job.MessageID)
	}
	if discordCtx != "" {
		job.Prompt = discordCtx + job.Prompt
	}

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
		// Capacity decisions are user-facing. Never kill an existing thread
		// agent automatically; report inactive candidates so the user can choose
		// what to close.
		if len(m.threadAgents) >= m.threadAgentMax {
			return m.threadAgentLimitErrorLocked()
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

func (m *Manager) threadAgentLimitErrorLocked() error {
	err := &ThreadAgentLimitError{Max: m.threadAgentMax}
	for _, e := range m.threadAgents {
		active := e.worker != nil && e.worker.IsActive()
		if active {
			err.Active++
			continue
		}
		err.Inactive++
		err.Candidates = append(err.Candidates, ThreadAgentLimitCandidate{
			ThreadID:     e.threadID,
			ParentChID:   e.parentChannelID,
			LastActivity: e.lastActivity,
		})
	}
	sort.Slice(err.Candidates, func(i, j int) bool {
		return err.Candidates[i].LastActivity.Before(err.Candidates[j].LastActivity)
	})
	return err
}

// spawnThreadAgent creates a new agent+worker for a thread. Must be called with m.mu held.
func (m *Manager) spawnThreadAgent(threadID, parentChannelID string, modelOverride ...string) (*threadAgentEntry, error) {
	cwd := m.defaultCWD
	model := m.defaultModel
	if sess, ok := m.getChannelSession(parentChannelID); ok {
		if sess.CWD != "" {
			cwd = sess.CWD
		}
		if sess.Model != "" {
			model = sess.Model
		}
	}
	threadSess, hasThreadSession := m.getThreadSession(threadID)
	if hasThreadSession {
		if threadSess.CWD != "" {
			cwd = threadSess.CWD
		}
		if threadSess.Model != "" {
			model = threadSess.Model
		}
	}
	if len(modelOverride) > 0 && modelOverride[0] != "" {
		model = modelOverride[0]
	}
	var err error
	cwd, err = m.ValidateCWD(cwd)
	if err != nil {
		return nil, err
	}

	agentName := "thread-" + threadID
	opts := m.agentOpts()
	if hasThreadSession && threadSess.SessionID != "" {
		opts.LoadSessionID = threadSess.SessionID
	}

	agent, err := acp.StartAgent(agentName, m.kiroCLI, cwd, model, opts)
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

	// Prepare history only when ACP did not restore an existing session.
	var historyCtx string
	if !agent.LoadedSession() {
		historyCtx = m.buildThreadHistory(parentChannelID, threadID)
		if historyCtx != "" {
			log.Printf("[manager] prepared %d chars of history for thread-%s", len(historyCtx), threadID)
		}
	}

	if err := m.setThreadSession(threadID, parentChannelID, &Session{
		AgentName: agentName,
		SessionID: agent.SessionID,
		CWD:       cwd,
		Model:     model,
	}); err != nil {
		log.Printf("[manager] save thread session: %v", err)
	}

	w := NewWorker("thread-"+threadID, agent, m.queueBufSize, m.askTimeoutSec, m.streamUpdateSec, 0, m.logger, model)
	w.SetUsageStore(m.usage)
	w.SetHistoryPrefix(historyCtx)
	w.OnActivityFunc(func() { m.TouchThreadAgent(threadID) })
	w.OnIdleFunc(func() bool { return m.StopThreadAgentIfCloseWhenIdle(threadID) })
	w.OnMemoryPrefixFunc(func() string {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.BuildMemoryPrefix(parentChannelID)
	})
	w.OnSilentFunc(func() bool { return m.IsSilent(threadID) })
	w.Start()
	entry.worker = w

	log.Printf("[manager] spawned thread agent %s (parent=%s)", agentName, parentChannelID)
	return entry, nil
}

// TouchThreadAgent updates lastActivity to prevent idle cleanup while agent is working.
func (m *Manager) TouchThreadAgent(threadID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.threadAgents[threadID]; ok {
		entry.lastActivity = time.Now()
	}
}

// StopThreadAgent stops a specific thread agent.
func (m *Manager) StopThreadAgent(threadID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopThreadAgentLocked(threadID)
}

func (m *Manager) stopThreadAgentLocked(threadID string) bool {
	if entry, ok := m.threadAgents[threadID]; ok {
		entry.worker.Stop()
		entry.agent.Stop()
		delete(m.threadAgents, threadID)
		log.Printf("[manager] stopped thread agent thread-%s", threadID)
		return true
	}
	return false
}

// MarkThreadArchived stops an inactive archived thread agent immediately, or
// defers cleanup until the active job finishes.
func (m *Manager) MarkThreadArchived(threadID string) (stopped bool, deferred bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.threadAgents[threadID]
	if !ok {
		return false, false
	}
	if entry.worker != nil && entry.worker.IsActive() {
		entry.closeWhenIdle = true
		return false, true
	}
	return m.stopThreadAgentLocked(threadID), false
}

// StopThreadAgentIfCloseWhenIdle closes a thread agent after a deferred archive.
func (m *Manager) StopThreadAgentIfCloseWhenIdle(threadID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.threadAgents[threadID]
	if !ok || !entry.closeWhenIdle {
		return false
	}
	if entry.worker != nil && entry.worker.IsActive() {
		return false
	}
	return m.stopThreadAgentLocked(threadID)
}

// CancelThreadAgent cancels the current job in a thread agent.
func (m *Manager) CancelThreadAgent(threadID string) error {
	m.mu.Lock()
	entry, ok := m.threadAgents[threadID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no thread agent")
	}
	if !entry.worker.CancelCurrent() {
		if entry.worker.IsActive() {
			return fmt.Errorf("active job is not cancellable yet")
		}
		return fmt.Errorf("no active job")
	}
	return nil
}

// InterruptThreadAgent interrupts the current job in a thread agent.
func (m *Manager) InterruptThreadAgent(threadID string) error {
	m.mu.Lock()
	entry, ok := m.threadAgents[threadID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no thread agent")
	}
	if !entry.worker.InterruptCurrent(interruptGrace) {
		return fmt.Errorf("no active job")
	}
	return nil
}

// ThreadAgentEntries returns a snapshot of all thread agent entries for heartbeat inspection.
// ChannelIdleEntries returns channel agents with their last activity time.
func (m *Manager) ChannelIdleEntries() []struct {
	ChannelID    string
	LastActivity time.Time
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []struct {
		ChannelID    string
		LastActivity time.Time
	}
	for chID := range m.agents {
		la := m.channelLastActivity[chID]
		out = append(out, struct {
			ChannelID    string
			LastActivity time.Time
		}{chID, la})
	}
	return out
}

// StopIdleChannel stops a channel agent and its worker (called by idle cleanup).
func (m *Manager) StopIdleChannel(channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopChannel(channelID)
	log.Printf("[manager] stopped idle channel agent ch-%s", channelID)
}

func (m *Manager) ThreadAgentEntries() []struct {
	ThreadID     string
	ParentChID   string
	LastActivity time.Time
	Active       bool
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]struct {
		ThreadID     string
		ParentChID   string
		LastActivity time.Time
		Active       bool
	}, 0, len(m.threadAgents))
	for _, e := range m.threadAgents {
		out = append(out, struct {
			ThreadID     string
			ParentChID   string
			LastActivity time.Time
			Active       bool
		}{e.threadID, e.parentChannelID, e.lastActivity, e.worker != nil && e.worker.IsActive()})
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

// ThreadAgentDetails returns the parent channel and active state for a thread agent.
func (m *Manager) ThreadAgentDetails(threadID string) (parentChannelID string, active bool, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.threadAgents[threadID]
	if !ok {
		return "", false, false
	}
	return entry.parentChannelID, entry.worker != nil && entry.worker.IsActive(), true
}

// IsThreadAgentActive reports whether a thread agent is currently executing a job.
func (m *Manager) IsThreadAgentActive(threadID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.threadAgents[threadID]
	return ok && entry.worker != nil && entry.worker.IsActive()
}

// SendCommandThread sends a slash command (e.g. "/compact", "/clear") to a thread agent.
func (m *Manager) SendCommandThread(threadID, command string) (string, error) {
	m.mu.Lock()
	entry, ok := m.threadAgents[threadID]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("no thread agent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return entry.agent.Ask(ctx, command, nil)
}

// ResetThreadAgent stops and respawns a thread agent, preserving parent binding.
func (m *Manager) ResetThreadAgent(threadID string) error {
	return m.resetThreadAgentWithModel(threadID, "")
}

// ResetThreadAgentWithModel stops and respawns a thread agent with a new model.
func (m *Manager) ResetThreadAgentWithModel(threadID, model string) error {
	return m.resetThreadAgentWithModel(threadID, model)
}

func (m *Manager) resetThreadAgentWithModel(threadID, model string) error {
	if err := m.validateModelID(model); err != nil {
		return err
	}
	m.mu.Lock()
	entry, ok := m.threadAgents[threadID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("no thread agent")
	}
	parentChannelID := entry.parentChannelID
	entry.worker.Stop()
	entry.agent.Stop()
	delete(m.threadAgents, threadID)

	if sess, ok := m.getThreadSession(threadID); ok {
		cp := *sess
		cp.AgentName = ""
		cp.SessionID = ""
		if model != "" {
			cp.Model = model
		}
		if err := m.setThreadSession(threadID, parentChannelID, &cp); err != nil {
			log.Printf("[manager] clear thread session on reset: %v", err)
		}
	}

	newEntry, err := m.spawnThreadAgent(threadID, parentChannelID, model)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("respawn thread agent: %w", err)
	}
	m.threadAgents[threadID] = newEntry
	m.mu.Unlock()
	return nil
}

// ThreadModel returns the current model display string for a thread agent.
func (m *Manager) ThreadModel(threadID string) string {
	m.mu.Lock()
	entry, ok := m.threadAgents[threadID]
	m.mu.Unlock()
	if !ok {
		return L.Get("model.current_default")
	}
	if entry.worker.model != "" {
		return L.Getf("model.current", entry.worker.model)
	}
	if m.defaultModel != "" {
		return L.Getf("model.current_global", m.defaultModel)
	}
	return L.Get("model.current_default")
}

// buildThreadHistory builds conversation history string for a thread agent.
func (m *Manager) buildThreadHistory(parentChannelID, threadID string) string {
	ctx := m.logger.BuildContextPrompt("thread-"+threadID, 20)
	if ctx == "" {
		ctx = m.logger.BuildContextPrompt(parentChannelID, 4)
	}
	if ctx == "" {
		return ""
	}
	const maxCtxChars = 80000
	if len(ctx) > maxCtxChars {
		ctx = ctx[len(ctx)-maxCtxChars:]
	}
	return ctx
}

func (m *Manager) buildDiscordThreadContext(ds *discordgo.Session, threadID, currentMessageID string) string {
	if ds == nil || threadID == "" {
		return ""
	}
	const messageLimit = 30
	messages, err := ds.ChannelMessages(threadID, messageLimit, "", "", "")
	if err != nil {
		log.Printf("[manager] fetch discord thread history thread=%s: %v", threadID, err)
		return ""
	}
	return formatDiscordThreadContext("[Recent Discord thread context]", messages, currentMessageID, messageLimit, 1500, 80000)
}

func (m *Manager) buildDiscordThreadHandoffContext(ds *discordgo.Session, threadID, currentMessageID string) string {
	if ds == nil || threadID == "" {
		return ""
	}
	const messageLimit = 100
	messages, err := ds.ChannelMessages(threadID, messageLimit, "", "", "")
	if err != nil {
		log.Printf("[manager] fetch handoff thread history thread=%s: %v", threadID, err)
		return ""
	}
	return formatDiscordThreadContext("[Cross-bot handoff context]\nA peer bot explicitly handed this thread to this bot. Use this transcript to understand the task, prior decisions, files, results, and remaining work before acting.", messages, currentMessageID, messageLimit, 3000, 120000)
}

func formatDiscordThreadContext(header string, messages []*discordgo.Message, currentMessageID string, maxMessages, maxMessageChars, maxTotalChars int) string {
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n")
	written := 0
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg == nil || msg.ID == currentMessageID {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" && len(msg.Attachments) == 0 {
			continue
		}
		if maxMessages > 0 && written >= maxMessages {
			break
		}
		if maxMessageChars > 0 && len(content) > maxMessageChars {
			content = content[:maxMessageChars] + "...(truncated)"
		}
		author := "unknown"
		if msg.Author != nil {
			author = msg.Author.Username
			if msg.Author.Bot {
				author += " (bot)"
			}
		}
		sb.WriteString(fmt.Sprintf("[%s] %s\n", author, content))
		if len(msg.Attachments) > 0 {
			var names []string
			for _, attachment := range msg.Attachments {
				if attachment != nil {
					names = append(names, attachment.Filename)
				}
			}
			if len(names) > 0 {
				sb.WriteString(fmt.Sprintf("[attachments] %s\n", strings.Join(names, ", ")))
			}
		}
		written++
		if maxTotalChars > 0 && sb.Len() >= maxTotalChars {
			break
		}
	}
	out := sb.String()
	if written == 0 {
		return ""
	}
	return out + "[End of Discord thread context]\n\n"
}
