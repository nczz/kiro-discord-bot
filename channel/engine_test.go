package channel

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/acp"
)

func TestParseDialect(t *testing.T) {
	cases := map[string]acp.Dialect{
		"omp": acp.DialectOmp, "OMP": acp.DialectOmp,
		"kiro": acp.DialectKiro, "": acp.DialectKiro, "unknown": acp.DialectKiro,
	}
	for in, want := range cases {
		if got := parseDialect(in); got != want {
			t.Errorf("parseDialect(%q)=%v want %v", in, got, want)
		}
	}
}

func TestParseEnabledEngines(t *testing.T) {
	// default kiro, no enabled list → kiro only
	s := parseEnabledEngines("kiro", "")
	if !s[acp.DialectKiro] || s[acp.DialectOmp] {
		t.Fatalf("kiro-only set wrong: %v", s)
	}
	// default kiro + omp enabled → both
	s = parseEnabledEngines("kiro", "kiro,omp")
	if !s[acp.DialectKiro] || !s[acp.DialectOmp] {
		t.Fatalf("both set wrong: %v", s)
	}
	// default omp, empty enabled → omp only (default always enabled)
	s = parseEnabledEngines("omp", "")
	if !s[acp.DialectOmp] || s[acp.DialectKiro] {
		t.Fatalf("omp-only set wrong: %v", s)
	}
	// unknown enabled entries are ignored instead of silently widening to kiro.
	s = parseEnabledEngines("omp", "typo")
	if !s[acp.DialectOmp] || s[acp.DialectKiro] {
		t.Fatalf("unknown enabled entry should not enable kiro: %v", s)
	}
}

func TestApplyEngineUsesIsolatedRuntimeEnvForOmp(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{ompProfile: "bot-profile", ompSessionDir: filepath.Join(dir, "omp-agent-runtime", "sessions")}
	base := acp.AgentOptions{Env: []string{
		"KIRO_HOME=/x",
		"FOO=bar",
		"KIRO_MCP_CONFIG=/y",
		"OMP_PROFILE=user-profile",
		"OTHER_ENV=/user/value",
	}, SessionDir: "/user/session-dir"}
	// kiro keeps all env + sets dialect
	k := m.applyEngine(base, acp.DialectKiro)
	if k.Dialect != acp.DialectKiro || len(k.Env) != len(base.Env) {
		t.Fatalf("kiro applyEngine wrong: dialect=%v env=%v", k.Dialect, k.Env)
	}
	// omp strips KIRO_*, keeps unrelated env, and overrides session dir with
	// the bot-managed runtime boundary.
	o := m.applyEngine(base, acp.DialectOmp)
	if o.Dialect != acp.DialectOmp {
		t.Fatalf("omp dialect not set: %v", o.Dialect)
	}
	seen := map[string]bool{}
	for _, e := range o.Env {
		if e == "KIRO_HOME=/x" || e == "KIRO_MCP_CONFIG=/y" {
			t.Fatalf("omp env not stripped: %v", o.Env)
		}
		seen[e] = true
	}
	if !seen["FOO=bar"] || !seen["OMP_PROFILE=bot-profile"] || o.SessionDir != m.ompSessionDir {
		t.Fatalf("omp env wrong: %v", o.Env)
	}
	if seen["OMP_PROFILE=user-profile"] {
		t.Fatalf("omp env should override caller profile: %v", o.Env)
	}
}

func TestApplyEngineDoesNotForceOmpProfileWhenUnset(t *testing.T) {
	m := &Manager{ompSessionDir: filepath.Join(t.TempDir(), "omp-agent-runtime", "sessions")}

	o := m.applyEngine(acp.AgentOptions{}, acp.DialectOmp)
	for _, env := range o.Env {
		if strings.HasPrefix(env, "OMP_PROFILE=") {
			t.Fatalf("unset OMP_PROFILE should not be forced into omp env: %v", o.Env)
		}
	}
	if o.SessionDir != m.ompSessionDir {
		t.Fatalf("omp session dir = %q, want %q", o.SessionDir, m.ompSessionDir)
	}
}

func newEngineTestManager(t *testing.T, defEngine string) *Manager {
	t.Helper()
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return &Manager{
		store:          store,
		defaultEngine:  parseDialect(defEngine),
		enabledEngines: parseEnabledEngines(defEngine, "kiro,omp"),
		kiroCLI:        "kiro-cli",
		ompPath:        "omp",
	}
}

func TestEngineForChannelResolution(t *testing.T) {
	m := newEngineTestManager(t, "kiro")
	// unset → default kiro
	if d, bin := m.engineForChannel("ch1"); d != acp.DialectKiro || bin != "kiro-cli" {
		t.Fatalf("default channel engine wrong: %v %s", d, bin)
	}
	// channel persisted omp → omp + omp binary
	_ = m.store.Set(m.sessionKey(sessionTargetChannel, "ch1"), &Session{Engine: "omp"})
	if d, bin := m.engineForChannel("ch1"); d != acp.DialectOmp || bin != "omp" {
		t.Fatalf("channel omp engine wrong: %v %s", d, bin)
	}
}

func TestOmpDefaultKeepsKiroAsOptionalSecondary(t *testing.T) {
	m := newEngineTestManager(t, "omp")
	m.kiroCLI = "/path/that/does/not/exist/kiro-cli"

	if got := m.ChannelEngine("ch1"); got != acp.DialectOmp.String() {
		t.Fatalf("default channel engine = %q, want omp", got)
	}
	if d, bin := m.engineForChannel("ch1"); d != acp.DialectOmp || bin != "omp" {
		t.Fatalf("engineForChannel = %v %q, want omp/omp", d, bin)
	}
	enabled := m.EnabledEngines()
	if len(enabled) != 2 || enabled[0] != "kiro" || enabled[1] != "omp" {
		t.Fatalf("enabled engines = %v, want [kiro omp]", enabled)
	}
}

func TestEngineForThreadInheritance(t *testing.T) {
	m := newEngineTestManager(t, "kiro")
	// parent channel = omp; thread with no override inherits omp
	_ = m.store.Set(m.sessionKey(sessionTargetChannel, "chP"), &Session{Engine: "omp"})
	if d, _ := m.engineForThread("th1", "chP"); d != acp.DialectOmp {
		t.Fatalf("thread should inherit parent omp, got %v", d)
	}
	// thread override = kiro wins over parent omp
	_ = m.store.Set(m.sessionKey(sessionTargetThread, "th1"), &Session{Engine: "kiro"})
	if d, bin := m.engineForThread("th1", "chP"); d != acp.DialectKiro || bin != "kiro-cli" {
		t.Fatalf("thread override kiro should win, got %v %s", d, bin)
	}
}

func TestSwitchEngineFromOmpToMissingKiroRollsBack(t *testing.T) {
	m := newEngineTestManager(t, "omp")
	cwd := t.TempDir()
	m.defaultCWD = cwd
	m.kiroCLI = filepath.Join(t.TempDir(), "missing-kiro-cli")
	old := &Session{CWD: cwd, Model: "model-a", Engine: "omp"}
	if err := m.setChannelSession("ch1", old); err != nil {
		t.Fatalf("set old session: %v", err)
	}

	err := m.SwitchEngine("ch1", "kiro")
	if err == nil {
		t.Fatal("expected missing kiro binary to fail")
	}
	got, ok := m.getChannelSession("ch1")
	if !ok {
		t.Fatal("old omp session should be restored")
	}
	if got.Engine != "omp" || got.Model != old.Model || got.CWD != old.CWD {
		t.Fatalf("restored session = %+v, want old %+v", got, old)
	}
	if got := m.ChannelEngine("ch1"); got != "omp" {
		t.Fatalf("channel engine after rollback = %q, want omp", got)
	}
}

func TestSwitchEngineRollbackDeletesNewSessionWhenRestartFails(t *testing.T) {
	m := newEngineTestManager(t, "kiro")
	m.defaultCWD = "/path/that/does/not/exist"

	err := m.SwitchEngine("ch1", "omp")
	if err == nil {
		t.Fatal("expected restart failure")
	}
	if _, ok := m.getChannelSession("ch1"); ok {
		t.Fatal("new session should be deleted after failed switch from no prior session")
	}
}

func TestSwitchEngineRollbackRestoresOldSessionWhenRestartFails(t *testing.T) {
	m := newEngineTestManager(t, "kiro")
	m.defaultCWD = "/path/that/does/not/exist"
	old := &Session{CWD: "/path/that/does/not/exist", Model: "model-a", Engine: "kiro"}
	if err := m.setChannelSession("ch1", old); err != nil {
		t.Fatalf("set old session: %v", err)
	}

	err := m.SwitchEngine("ch1", "omp")
	if err == nil {
		t.Fatal("expected restart failure")
	}
	got, ok := m.getChannelSession("ch1")
	if !ok {
		t.Fatal("old session should be restored")
	}
	if got.Engine != "kiro" || got.Model != "model-a" || got.CWD != old.CWD {
		t.Fatalf("restored session = %+v, want old %+v", got, old)
	}
}

func TestSwitchThreadEngineRollbackDeletesNewThreadSessionWhenResetFails(t *testing.T) {
	m := newEngineTestManager(t, "kiro")

	err := m.SwitchThreadEngine("thread", "parent", "omp")
	if err == nil {
		t.Fatal("expected reset failure")
	}
	if _, ok := m.getThreadSession("thread"); ok {
		t.Fatal("new thread session should be deleted after failed switch")
	}
}
