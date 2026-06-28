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

func TestPreflightAgentOptionsForOmpDoNotCarryKiroRuntimeEnv(t *testing.T) {
	cfg := &Config{
		DataDir:          t.TempDir(),
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
	for _, env := range omp.Env {
		if strings.HasPrefix(env, "KIRO_") {
			t.Fatalf("omp preflight env contains Kiro runtime variable: %v", omp.Env)
		}
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
