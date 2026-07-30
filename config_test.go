package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/acp"
)

func TestLoadConfigNormalizesDataDir(t *testing.T) {
	root := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("DATA_DIR", "./runtime-data")

	cfg := loadConfig()
	want, err := filepath.Abs("runtime-data")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != want {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, want)
	}
}

func TestLoadConfigParsesA2ADisabledByDefault(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("DATA_DIR", t.TempDir())

	cfg := loadConfig()
	if cfg.A2A.Enabled() {
		t.Fatalf("A2A should be disabled when NATS_URL is unset: %+v", cfg.A2A)
	}
	if cfg.A2A.RequireConfirmationForRemote != true || cfg.A2A.AutoDelegateEnabled {
		t.Fatalf("A2A confirmation/delegation defaults = confirmation %v auto %v", cfg.A2A.RequireConfirmationForRemote, cfg.A2A.AutoDelegateEnabled)
	}
	if cfg.A2A.TaskTimeoutSec != 3600 || cfg.A2A.MaxDelegationDepth != 1 {
		t.Fatalf("A2A defaults = timeout %d depth %d", cfg.A2A.TaskTimeoutSec, cfg.A2A.MaxDelegationDepth)
	}
}

func TestLoadConfigParsesA2AEnabledWithProductionMaterial(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("NATS_URL", "nats://127.0.0.1:4222")
	t.Setenv("NATS_TOKEN", "dev-token")
	t.Setenv("NATS_CREDS_FILE", "/tmp/nats.creds")
	t.Setenv("A2A_AGENT_ID", "adam-n200")
	t.Setenv("A2A_TASK_TIMEOUT_SEC", "90")
	t.Setenv("A2A_MAX_DELEGATION_DEPTH", "2")
	t.Setenv("A2A_AUTO_DELEGATE_ENABLED", "true")
	t.Setenv("A2A_PRODUCTION_SECURITY", "true")

	cfg := loadConfig()
	if !cfg.A2A.Enabled() {
		t.Fatal("A2A should be enabled when NATS_URL is set")
	}
	if cfg.A2A.AgentID != "adam-n200" || cfg.A2A.TaskTimeoutSec != 90 || cfg.A2A.MaxDelegationDepth != 2 || !cfg.A2A.AutoDelegateEnabled {
		t.Fatalf("unexpected A2A config: %+v", cfg.A2A)
	}
}

func TestEnabledEngineSpecsKeepsOmpDefaultWithKiroSecondary(t *testing.T) {
	cfg := &Config{
		AgentEngine:         "omp",
		AgentEnginesEnabled: "kiro,omp",
		KiroCLIPath:         "/fake/kiro-cli",
		OMPPath:             "/fake/omp",
	}

	got := enabledEngineSpecs(cfg)
	if len(got) != 2 {
		t.Fatalf("engine specs = %d, want 2: %+v", len(got), got)
	}
	if got[0].name != "kiro" || got[0].dialect != acp.DialectKiro || got[0].binary != "/fake/kiro-cli" {
		t.Fatalf("first spec = %+v, want kiro secondary preflight first", got[0])
	}
	if got[1].name != "omp" || got[1].dialect != acp.DialectOmp || got[1].binary != "/fake/omp" {
		t.Fatalf("second spec = %+v, want omp default engine present", got[1])
	}
}

func TestEnabledEngineSpecsOmpDefaultWithoutEnabledListIsOmpOnly(t *testing.T) {
	cfg := &Config{
		AgentEngine: "omp",
		KiroCLIPath: "/fake/kiro-cli",
		OMPPath:     "/fake/omp",
	}

	got := enabledEngineSpecs(cfg)
	if len(got) != 1 {
		t.Fatalf("engine specs = %d, want omp only: %+v", len(got), got)
	}
	if got[0].name != "omp" || got[0].dialect != acp.DialectOmp {
		t.Fatalf("spec = %+v, want omp only", got[0])
	}
}

func TestPreflightAgentOptionsForOmpUsesIsolatedRuntimeEnv(t *testing.T) {
	cfg := &Config{
		DataDir:          t.TempDir(),
		OMPProfile:       "bot-profile",
		AgentProfile:     "planner",
		MaxScannerBuffer: 12345,
	}

	omp := preflightAgentOptions(cfg, acp.DialectOmp)
	if omp.Dialect != acp.DialectOmp {
		t.Fatalf("omp dialect = %v, want omp", omp.Dialect)
	}
	if omp.Agent != "" {
		t.Fatalf("omp agent profile = %q, want empty", omp.Agent)
	}
	var hasProfile bool
	for _, env := range omp.Env {
		if strings.HasPrefix(env, "KIRO_") {
			t.Fatalf("omp preflight env contains Kiro runtime variable: %v", omp.Env)
		}
		hasProfile = hasProfile || env == "OMP_PROFILE=bot-profile"
	}
	if !hasProfile || omp.SessionDir != filepath.Join(cfg.DataDir, "omp-agent-runtime", "sessions") {
		t.Fatalf("omp preflight env/session = %v/%q, want OMP_PROFILE and OMP session dir", omp.Env, omp.SessionDir)
	}

	kiro := preflightAgentOptions(cfg, acp.DialectKiro)
	if kiro.Agent != "planner" {
		t.Fatalf("kiro agent profile = %q, want planner", kiro.Agent)
	}
	var hasHome, hasMCPConfig bool
	for _, env := range kiro.Env {
		hasHome = hasHome || strings.HasPrefix(env, "KIRO_HOME=")
		hasMCPConfig = hasMCPConfig || strings.HasPrefix(env, "KIRO_MCP_CONFIG=")
	}
	if !hasHome || !hasMCPConfig {
		t.Fatalf("kiro preflight env = %v, want KIRO_HOME and KIRO_MCP_CONFIG", kiro.Env)
	}
}

func TestPreflightAgentOptionsForOmpDoesNotForceProfileWhenUnset(t *testing.T) {
	cfg := &Config{
		DataDir:          t.TempDir(),
		MaxScannerBuffer: 12345,
	}

	omp := preflightAgentOptions(cfg, acp.DialectOmp)
	for _, env := range omp.Env {
		if strings.HasPrefix(env, "OMP_PROFILE=") {
			t.Fatalf("unset OMP_PROFILE should not be forced into omp preflight env: %v", omp.Env)
		}
	}
	if omp.SessionDir != filepath.Join(cfg.DataDir, "omp-agent-runtime", "sessions") {
		t.Fatalf("omp preflight session dir = %q, want default runtime session dir", omp.SessionDir)
	}
}

func TestPreflightAgentOptionsForOmpUsesConfiguredSessionDir(t *testing.T) {
	cfg := &Config{
		DataDir:          t.TempDir(),
		OMPSessionDir:    filepath.Join(t.TempDir(), "omp-sessions"),
		MaxScannerBuffer: 12345,
	}

	omp := preflightAgentOptions(cfg, acp.DialectOmp)
	if omp.SessionDir != cfg.OMPSessionDir {
		t.Fatalf("omp preflight session dir = %q, want %q", omp.SessionDir, cfg.OMPSessionDir)
	}
}
