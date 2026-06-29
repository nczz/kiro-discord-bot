package channel

import (
	"errors"
	"log"
	"path/filepath"
	"strings"

	"github.com/nczz/kiro-discord-bot/acp"
)

// ErrEngineNotEnabled is returned when /engine targets an engine not in AGENT_ENGINES_ENABLED.
var ErrEngineNotEnabled = errors.New("engine not enabled")

// parseDialect maps an engine name to an acp.Dialect. Unknown/empty → DialectKiro.
func parseDialect(name string) acp.Dialect {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "omp":
		return acp.DialectOmp
	default:
		return acp.DialectKiro
	}
}

// parseEnabledEngines builds the set of engines that /engine may select.
// The default engine is always enabled. An empty enabled list means
// "default engine only" (per-engine switching effectively disabled).
func parseEnabledEngines(defaultEngine, enabled string) map[acp.Dialect]bool {
	set := map[acp.Dialect]bool{parseDialect(defaultEngine): true}
	for _, part := range strings.Split(enabled, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !ValidEngineName(part) {
			log.Printf("[engine] ignoring unknown AGENT_ENGINES_ENABLED entry %q", part)
			continue
		}
		set[parseDialect(part)] = true
	}
	return set
}

// binaryFor returns the binary path for a dialect.
func (m *Manager) binaryFor(d acp.Dialect) string {
	if d == acp.DialectOmp {
		return m.ompPath
	}
	return m.kiroCLI
}

// engineEnabled reports whether the dialect may be selected in this runtime.
func (m *Manager) engineEnabled(d acp.Dialect) bool {
	return m.enabledEngines[d]
}

// enabledEngineList returns the enabled engine names (for /doctor and /engine).
func (m *Manager) enabledEngineList() []string {
	var out []string
	if m.enabledEngines[acp.DialectKiro] {
		out = append(out, "kiro")
	}
	if m.enabledEngines[acp.DialectOmp] {
		out = append(out, "omp")
	}
	return out
}

// resolveEngine resolves a Session.Engine string to a dialect, falling back to
// the runtime default when unset. This is the per-scope resolution used at all
// spawn points (default → channel/thread Session.Engine → /engine override,
// the override being persisted into Session.Engine).
func (m *Manager) resolveEngine(sessionEngine string) acp.Dialect {
	s := strings.TrimSpace(sessionEngine)
	if s == "" {
		return m.defaultEngine
	}
	return parseDialect(s)
}

// engineForChannel resolves the dialect + binary for a channel scope.
func (m *Manager) engineForChannel(channelID string) (acp.Dialect, string) {
	engine := ""
	if sess, ok := m.getChannelSession(channelID); ok {
		engine = sess.Engine
	}
	d := m.resolveEngine(engine)
	return d, m.binaryFor(d)
}

// engineForThread resolves the dialect + binary for a thread scope, inheriting
// the parent channel engine unless the thread has its own override.
func (m *Manager) engineForThread(threadID, parentChannelID string) (acp.Dialect, string) {
	engine := ""
	if sess, ok := m.getChannelSession(parentChannelID); ok {
		engine = sess.Engine
	}
	if sess, ok := m.getThreadSession(threadID); ok && strings.TrimSpace(sess.Engine) != "" {
		engine = sess.Engine
	}
	d := m.resolveEngine(engine)
	return d, m.binaryFor(d)
}

// applyEngine stamps the resolved dialect onto spawn options. OMP receives its
// own runtime profile/session directory and never inherits Kiro-specific runtime
// env (KIRO_HOME / KIRO_MCP_CONFIG).
func (m *Manager) applyEngine(opts acp.AgentOptions, d acp.Dialect) acp.AgentOptions {
	opts.Dialect = d
	if d != acp.DialectKiro {
		filtered := make([]string, 0, len(opts.Env))
		for _, e := range opts.Env {
			if strings.HasPrefix(e, "KIRO_HOME=") || strings.HasPrefix(e, "KIRO_MCP_CONFIG=") {
				continue
			}
			filtered = append(filtered, e)
		}
		opts.Env = filtered
	}
	if d == acp.DialectOmp {
		if profile := strings.TrimSpace(m.ompProfile); profile != "" {
			opts.Env = upsertEnv(opts.Env, "OMP_PROFILE", profile)
		}
		if sessionDir := strings.TrimSpace(m.ompSessionDir); sessionDir != "" {
			opts.SessionDir = sessionDir
		}
	}
	return opts
}

func DefaultOMPSessionDir(dataDir, configured string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	if strings.TrimSpace(dataDir) == "" {
		return ""
	}
	return filepath.Join(dataDir, "omp-agent-runtime", "sessions")
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		out = append(out, item)
	}
	return append(out, prefix+value)
}

// ValidEngineName reports whether name is a known engine ("kiro" or "omp").
func ValidEngineName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "kiro", "omp":
		return true
	default:
		return false
	}
}

// EngineEnabled reports whether a (known) engine may be selected in this runtime.
func (m *Manager) EngineEnabled(name string) bool {
	if !ValidEngineName(name) {
		return false
	}
	return m.engineEnabled(parseDialect(name))
}

// EnabledEngines returns the engine names this runtime permits.
func (m *Manager) EnabledEngines() []string { return m.enabledEngineList() }

// ChannelEngine returns the resolved engine name for a channel scope.
func (m *Manager) ChannelEngine(channelID string) string {
	d, _ := m.engineForChannel(channelID)
	return d.String()
}

// ThreadEngine returns the resolved engine name for a thread scope.
func (m *Manager) ThreadEngine(threadID, parentChannelID string) string {
	d, _ := m.engineForThread(threadID, parentChannelID)
	return d.String()
}

// SwitchEngine persists a new engine for a channel and restarts its agent on
// that engine. The ACP session is NOT transferred across engines (a fresh
// session/new is created); recent conversation continuity is preserved by the
// engine-agnostic chat-log history prefix replayed at restart.
func (m *Manager) SwitchEngine(channelID, engineName string) error {
	if !ValidEngineName(engineName) {
		return ErrEngineNotEnabled
	}
	d := parseDialect(engineName)
	if !m.engineEnabled(d) {
		return ErrEngineNotEnabled
	}
	if cur, _ := m.engineForChannel(channelID); cur == d {
		return nil // already on this engine
	}
	oldSess, _ := m.getChannelSession(channelID)
	var oldCopy *Session
	if oldSess != nil {
		cp := *oldSess
		oldCopy = &cp
	}
	newSess := &Session{Engine: d.String()}
	if oldSess != nil {
		newSess.CWD = oldSess.CWD
		newSess.Model = oldSess.Model
	}
	if err := m.setChannelSession(channelID, newSess); err != nil {
		return err
	}
	if err := m.Restart(channelID); err != nil {
		if oldCopy != nil {
			_ = m.setChannelSession(channelID, oldCopy)
		} else {
			_ = m.deleteChannelSession(channelID)
		}
		return err
	}
	return nil
}

// SwitchThreadEngine persists a new engine override for a thread and respawns
// its agent on that engine (parent channel engine is unaffected).
func (m *Manager) SwitchThreadEngine(threadID, parentChannelID, engineName string) error {
	if !ValidEngineName(engineName) {
		return ErrEngineNotEnabled
	}
	d := parseDialect(engineName)
	if !m.engineEnabled(d) {
		return ErrEngineNotEnabled
	}
	if cur, _ := m.engineForThread(threadID, parentChannelID); cur == d {
		return nil
	}
	oldSess, ok := m.getThreadSession(threadID)
	var oldCopy *Session
	if !ok {
		oldSess = &Session{}
	} else {
		cp := *oldSess
		oldCopy = &cp
	}
	sess := *oldSess
	sess.Engine = d.String()
	sess.SessionID = "" // fresh session on the new engine
	sess.AgentName = ""
	if err := m.setThreadSession(threadID, parentChannelID, &sess); err != nil {
		return err
	}
	if err := m.ResetThreadAgent(threadID); err != nil {
		if oldCopy != nil {
			_ = m.setThreadSession(threadID, parentChannelID, oldCopy)
		} else {
			_ = m.deleteThreadSession(threadID)
		}
		return err
	}
	return nil
}
