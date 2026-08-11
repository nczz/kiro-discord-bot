package channel

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	dataDir := t.TempDir()
	return &Manager{
		store:               store,
		logger:              NewChatLogger(dataDir),
		dataDir:             dataDir,
		workers:             make(map[string]*Worker),
		agents:              make(map[string]*acp.Agent),
		threadAgents:        make(map[string]*threadAgentEntry),
		channelLastActivity: make(map[string]time.Time),
		defaultEngine:       parseDialect(defEngine),
		enabledEngines:      parseEnabledEngines(defEngine, "kiro,omp"),
		kiroCLI:             "kiro-cli",
		ompPath:             "omp",
	}
}

func fakeACPBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "fake-acp.go")
	bin := filepath.Join(dir, "fake-acp")
	code := `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) >= 5 && os.Args[1] == "chat" && os.Args[2] == "--list-models" && os.Args[3] == "-f" && os.Args[4] == "json" {
		fmt.Println(` + "`" + `{"default_model":"model-a","models":[{"model_name":"Model A","model_id":"model-a","description":"Alpha","rate_multiplier":1,"rate_unit":"x"}]}` + "`" + `)
		return
	}
	if path := os.Getenv("FAKE_ACP_ARGS_FILE"); path != "" {
		args := strings.Join(os.Args[1:], "\n")
		_ = os.WriteFile(path, []byte(args), 0644)
	}
	if os.Getenv("FAKE_ACP_FAIL_KIRO_MODEL") != "" && strings.Contains("\n"+strings.Join(os.Args[1:], "\n")+"\n", "\n--model\nmodel-a\n") {
		fmt.Fprintln(os.Stderr, "The model 'model-a' is not available")
		os.Exit(1)
	}
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		if method == "session/set_config_option" && os.Getenv("FAKE_ACP_FAIL_MODEL_CONFIG") != "" {
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id": req["id"],
				"error": map[string]any{"code": -32603, "message": "model unavailable"},
			})
			continue
		}
		result := map[string]any{}
		switch method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": 1,
				"agentInfo": map[string]string{"name": "fake-acp", "version": "test"},
			}
		case "session/new", "session/load":
			result = map[string]any{"sessionId": "sid-test"}
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
	}
}
`
	if err := os.WriteFile(src, []byte(code), 0644); err != nil {
		t.Fatalf("write fake acp: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake acp: %v\n%s", err, out)
	}
	return bin
}

func TestAgentStartModelErrorMatchesOnlyModelSelectionFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "omp config stage", err: errors.New("session/set_config_option model: rpc error -32603"), want: true},
		{name: "model unavailable", err: errors.New("The model 'openai-codex/gpt-5.6-luna' is not available"), want: true},
		{name: "unknown model", err: errors.New("unknown model \"missing\""), want: true},
		{name: "mcp model wording", err: errors.New("mcp model context server failed during initialize"), want: false},
		{name: "nil", err: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentStartModelError(tc.err); got != tc.want {
				t.Fatalf("agentStartModelError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
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

func TestSwitchEngineClearsModelOnSuccessfulEngineChange(t *testing.T) {
	m := newEngineTestManager(t, "omp")
	fake := fakeACPBinary(t)
	m.kiroCLI = fake
	m.ompPath = fake
	cwd := t.TempDir()
	m.defaultCWD = cwd
	if err := m.setChannelSession("ch1", &Session{
		CWD:       cwd,
		Model:     "openai-codex/gpt-5.6-luna",
		Engine:    "omp",
		SessionID: "old-session",
	}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}

	if err := m.SwitchEngine("ch1", "kiro"); err != nil {
		t.Fatalf("SwitchEngine: %v", err)
	}
	t.Cleanup(func() { m.StopAll() })
	got, ok := m.getChannelSession("ch1")
	if !ok {
		t.Fatal("channel session should remain after switch")
	}
	if got.Engine != "kiro" || got.Model != "" {
		t.Fatalf("switched session = %+v, want kiro with cleared model", got)
	}
}

func TestSwitchThreadEngineClearsThreadModelOnSuccessfulEngineChange(t *testing.T) {
	m := newEngineTestManager(t, "omp")
	fake := fakeACPBinary(t)
	m.kiroCLI = fake
	m.ompPath = fake
	cwd := t.TempDir()
	m.defaultCWD = cwd
	if err := m.setChannelSession("parent", &Session{CWD: cwd, Engine: "omp"}); err != nil {
		t.Fatalf("set parent session: %v", err)
	}
	if err := m.setThreadSession("thread", "parent", &Session{
		CWD:       cwd,
		Model:     "openai-codex/gpt-5.6-luna",
		Engine:    "omp",
		SessionID: "old-thread-session",
	}); err != nil {
		t.Fatalf("set thread session: %v", err)
	}

	if err := m.SwitchThreadEngine("thread", "parent", "kiro"); err != nil {
		t.Fatalf("SwitchThreadEngine: %v", err)
	}
	t.Cleanup(func() { m.StopAll() })
	got, ok := m.getThreadSession("thread")
	if !ok {
		t.Fatal("thread session should remain after switch")
	}
	if got.Engine != "kiro" || got.Model != "" {
		t.Fatalf("switched thread session = %+v, want kiro with cleared model", got)
	}
}

func TestRestartClearsUnavailableKiroStartupModel(t *testing.T) {
	m := newEngineTestManager(t, "kiro")
	fake := fakeACPBinary(t)
	m.kiroCLI = fake
	cwd := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("FAKE_ACP_ARGS_FILE", argsFile)
	m.defaultCWD = cwd
	if err := m.setChannelSession("ch1", &Session{
		CWD:       cwd,
		Model:     "missing-model",
		Engine:    "kiro",
		SessionID: "old-session",
	}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}

	if err := m.Restart("ch1"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	t.Cleanup(func() { m.StopAll() })
	got, ok := m.getChannelSession("ch1")
	if !ok {
		t.Fatal("channel session should remain after restart")
	}
	if got.Model != "" {
		t.Fatalf("restart should clear unavailable startup model, got %+v", got)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read fake acp args: %v", err)
	}
	if strings.Contains(string(args), "--model") || strings.Contains(string(args), "missing-model") {
		t.Fatalf("agent should restart without unavailable model, args:\n%s", args)
	}
}

func TestThreadEngineOverrideDoesNotInheritOrClearParentModel(t *testing.T) {
	m := newEngineTestManager(t, "omp")
	fake := fakeACPBinary(t)
	m.kiroCLI = fake
	m.ompPath = fake
	cwd := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("FAKE_ACP_ARGS_FILE", argsFile)
	m.defaultCWD = cwd
	if err := m.setChannelSession("parent", &Session{
		CWD:    cwd,
		Engine: "omp",
		Model:  "openai-codex/gpt-5.6-luna",
	}); err != nil {
		t.Fatalf("set parent session: %v", err)
	}
	if err := m.setThreadSession("thread", "parent", &Session{
		CWD:    cwd,
		Engine: "kiro",
	}); err != nil {
		t.Fatalf("set thread session: %v", err)
	}

	if err := m.ResetThreadAgent("thread"); err != nil {
		t.Fatalf("ResetThreadAgent: %v", err)
	}
	t.Cleanup(func() { m.StopAll() })
	parent, _ := m.getChannelSession("parent")
	if parent.Model != "openai-codex/gpt-5.6-luna" {
		t.Fatalf("thread startup should not clear parent model, got parent %+v", parent)
	}
	thread, _ := m.getThreadSession("thread")
	if thread.Model != "" {
		t.Fatalf("thread should not persist inherited cross-engine model, got %+v", thread)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read fake acp args: %v", err)
	}
	if strings.Contains(string(args), "--model") || strings.Contains(string(args), "openai-codex/gpt-5.6-luna") {
		t.Fatalf("thread override should start without parent OMP model, args:\n%s", args)
	}
}

func TestSwitchModelReportsStartupModelFailure(t *testing.T) {
	m := newEngineTestManager(t, "kiro")
	fake := fakeACPBinary(t)
	m.kiroCLI = fake
	cwd := t.TempDir()
	t.Setenv("FAKE_ACP_FAIL_KIRO_MODEL", "1")
	m.defaultCWD = cwd
	if err := m.setChannelSession("ch1", &Session{
		CWD:    cwd,
		Engine: "kiro",
		Model:  "old-model",
	}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}

	restarted, err := m.SwitchModel("ch1", "model-a")
	if err == nil {
		t.Fatal("SwitchModel should report explicit startup model failure")
	}
	if !restarted {
		t.Fatal("SwitchModel should report that it attempted restart")
	}
	got, _ := m.getChannelSession("ch1")
	if got.Model != "old-model" {
		t.Fatalf("failed explicit model switch should restore old model, got %+v", got)
	}
}

func TestRestartRetriesOmpStartupModelErrorWithoutModel(t *testing.T) {
	m := newEngineTestManager(t, "omp")
	fake := fakeACPBinary(t)
	m.ompPath = fake
	cwd := t.TempDir()
	t.Setenv("FAKE_ACP_FAIL_MODEL_CONFIG", "1")
	m.defaultCWD = cwd
	m.defaultModel = "openai-codex/gpt-5.6-luna"
	if err := m.setChannelSession("ch1", &Session{CWD: cwd, Engine: "omp"}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}

	if err := m.Restart("ch1"); err != nil {
		t.Fatalf("Restart should retry without unavailable OMP model: %v", err)
	}
	t.Cleanup(func() { m.StopAll() })
	got, ok := m.getChannelSession("ch1")
	if !ok {
		t.Fatal("channel session should remain after restart")
	}
	if got.Model != "" {
		t.Fatalf("retry should persist empty model after OMP startup model failure, got %+v", got)
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
