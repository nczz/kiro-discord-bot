package channel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/acp"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestDoctorRuntimeOverviewDoesNotLeakRawEnvironmentValues(t *testing.T) {
	L.Load("en")
	t.Setenv("DISCORD_TOKEN", "discord-token-secret-value")
	t.Setenv("KIRO_API_KEY", "kiro-api-key-secret-value")
	t.Setenv("STT_API_KEY", "stt-api-key-secret-value")
	t.Setenv("DEFAULT_CWD", "/raw/env/default-cwd")
	t.Setenv("KIRO_MCP_CONFIG", "/raw/env/mcp.json")
	t.Setenv("OMP_PROFILE", "raw-user-profile")
	t.Setenv("OMP_SESSION_DIR", "/raw/env/omp-session")

	t.Setenv("NATS_TOKEN", "nats-token-secret-value")
	t.Setenv("NATS_CREDS_FILE", "/raw/env/nats.creds")
	t.Setenv("NATS_TLS_CA_FILE", "/raw/env/ca.pem")
	dir := t.TempDir()
	m := NewManager(ManagerConfig{
		KiroCLIPath:         "kiro-cli",
		DefaultCWD:          "/projects/default",
		AllowedCwdRoots:     "/projects,/work",
		AskTimeoutSec:       3600,
		StreamUpdateSec:     3,
		QueueBufferSize:     20,
		ThreadAutoArchive:   1440,
		ThreadAgentMax:      5,
		ThreadAgentIdleSec:  900,
		ChannelAgentIdleSec: 0,
		MaxScannerBuffer:    64 * 1024 * 1024,
		DataDir:             dir,
		A2A: a2a.Config{
			NATSToken:     "nats-token-secret-value",
			NATSCredsFile: "/raw/env/nats.creds",
			NATSTLSCAFile: "/raw/env/ca.pem",
		},
	})
	defer m.StopAll()

	got := m.doctorRuntimeOverview()
	for _, notWant := range []string{
		"discord-token-secret-value",
		"kiro-api-key-secret-value",
		"stt-api-key-secret-value",
		"nats-token-secret-value",
		"/raw/env/default-cwd",
		"/raw/env/mcp.json",
		"/raw/env/omp-session",
		"/raw/env/nats.creds",
		"/raw/env/ca.pem",
	} {
		if strings.Contains(got, notWant) {
			t.Fatalf("doctor runtime overview leaked %q:\n%s", notWant, got)
		}
	}
	for _, want := range []string{
		"`DISCORD_TOKEN`: set (redacted)",
		"`DEFAULT_CWD`: set",
		"(effective: `/projects/default`)",
		"(effective: `/projects, /work`)",
		filepath.Join(dir, "kiro-agent-runtime", "settings", "mcp.json"),
		"(effective: `default`)",
		filepath.Join(dir, "omp-agent-runtime", "sessions"),
		"`NATS_TOKEN`: set (redacted)",
		"`NATS_CREDS_FILE`: set (redacted)",
		"(effective: `configured`)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor runtime overview missing %q:\n%s", want, got)
		}
	}
}

func TestDoctorRuntimeOverviewShowsEffectiveDefaultsWhenEnvUnset(t *testing.T) {
	L.Load("en")
	for _, name := range []string{"KIRO_CLI_PATH", "DEFAULT_CWD", "ALLOWED_CWD_ROOTS", "KIRO_MODEL"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	m := NewManager(ManagerConfig{
		KiroCLIPath:      "",
		DefaultCWD:       "/projects",
		AskTimeoutSec:    3600,
		StreamUpdateSec:  3,
		QueueBufferSize:  20,
		ThreadAgentMax:   5,
		MaxScannerBuffer: 64 * 1024 * 1024,
		DataDir:          dir,
	})
	defer m.StopAll()

	got := m.doctorRuntimeOverview()
	for _, want := range []string{
		"`KIRO_CLI_PATH`: unset",
		"(effective: `kiro-cli`)",
		"`ALLOWED_CWD_ROOTS`: unset",
		"(effective: `not restricted`)",
		"`KIRO_MODEL`: unset",
		"(effective: `auto`)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor runtime overview missing %q:\n%s", want, got)
		}
	}
}

func TestDoctorRuntimeOverviewShowsA2ADisabledAndSafeEffectiveValues(t *testing.T) {
	L.Load("en")
	t.Setenv("NATS_URL", "")
	t.Setenv("A2A_AGENT_ID", "adam-n200")
	m := NewManager(ManagerConfig{
		DataDir: t.TempDir(),
		A2A: a2a.Config{
			AgentID:                      "adam-n200",
			TaskTimeoutSec:               3600,
			MaxDelegationDepth:           1,
			RequireConfirmationForRemote: true,
			TaskRetentionDays:            30,
			ObjectRetentionDays:          30,
			MaxPendingTasks:              100,
			MaxOutboundTasksPerChannel:   10,
			MaxInboundTasksPerChannel:    10,
			MaxEventRatePerMin:           120,
		},
	})
	defer m.StopAll()

	got := m.doctorRuntimeOverview()
	for _, want := range []string{
		"**A2A NATS**",
		"`NATS_URL`: unset",
		"(effective: `disabled`)",
		"`A2A_AGENT_ID`: set",
		"(effective: `adam-n200`)",
		"`A2A_REQUIRE_CONFIRMATION_FOR_REMOTE`: unset",
		"(effective: `true`)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor runtime overview missing %q:\n%s", want, got)
		}
	}
}

func TestDoctorListenModeConsistencyReportsOk(t *testing.T) {
	L.Load("en")
	m := NewManager(ManagerConfig{})
	m.Pause("channel-1")
	m.SetThreadMode("channel-1", false)
	m.Back("channel-2")
	m.SetThreadMode("channel-2", true)

	got := m.doctorListenModeConsistency()
	if !strings.Contains(got, "listen mode: consistent") {
		t.Fatalf("doctor listen mode consistency = %q, want ok message", got)
	}
	if strings.Contains(got, "inconsistency") {
		t.Fatalf("doctor listen mode consistency reported unexpected issue:\n%s", got)
	}
}

func TestDoctorListenModeConsistencyReportsPausedThreadModeOn(t *testing.T) {
	L.Load("en")
	m := NewManager(ManagerConfig{})
	m.Pause("channel-1")
	m.SetThreadMode("channel-1", true)
	m.Back("channel-2")
	m.SetThreadMode("channel-2", true)

	got := m.doctorListenModeConsistency()
	for _, want := range []string{
		"listen mode inconsistency",
		"channel channel-1: paused=true but threadMode=on",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor listen mode consistency missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "channel channel-2") {
		t.Fatalf("doctor listen mode consistency reported full-listen channel as inconsistent:\n%s", got)
	}
}

func TestResolveEngineBinaryUsesPathForOmp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake omp: %v", err)
	}

	got, err := resolveEngineBinary(acp.DialectOmp, path)
	if err != nil {
		t.Fatalf("resolve omp path: %v", err)
	}
	if got != path {
		t.Fatalf("resolved omp = %q, want %q", got, path)
	}
}

func TestDoctorEngineDiagnosticsPureOmpDoesNotRequireKiro(t *testing.T) {
	L.Load("en")
	m := NewManager(ManagerConfig{
		KiroCLIPath:         filepath.Join(t.TempDir(), "missing-kiro-cli"),
		OMPPath:             filepath.Join(t.TempDir(), "missing-omp"),
		AgentEngine:         "omp",
		AgentEnginesEnabled: "omp",
		DataDir:             t.TempDir(),
	})
	defer m.StopAll()

	got := m.doctorEngineDiagnostics(context.Background())
	if strings.Contains(got, "kiro") {
		t.Fatalf("pure-omp diagnostics should not reference kiro:\n%s", got)
	}
	if !strings.Contains(got, "omp binary") {
		t.Fatalf("pure-omp diagnostics should report omp binary:\n%s", got)
	}
}
