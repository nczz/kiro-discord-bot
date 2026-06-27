package channel

import (
	"strings"

	"github.com/nczz/kiro-discord-bot/acp"
)

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

// applyEngine stamps the resolved dialect onto spawn options and, for non-kiro
// engines, strips kiro-specific runtime env (KIRO_HOME / KIRO_MCP_CONFIG) so a
// pure-omp scope never carries kiro settings (plan §4.1 S3.3).
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
	return opts
}
