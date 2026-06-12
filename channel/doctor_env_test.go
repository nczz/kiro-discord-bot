package channel

import (
	"path/filepath"
	"strings"
	"testing"

	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestDoctorRuntimeOverviewDoesNotLeakRawEnvironmentValues(t *testing.T) {
	L.Load("en")
	t.Setenv("DISCORD_TOKEN", "discord-token-secret-value")
	t.Setenv("KIRO_API_KEY", "kiro-api-key-secret-value")
	t.Setenv("STT_API_KEY", "stt-api-key-secret-value")
	t.Setenv("DEFAULT_CWD", "/raw/env/default-cwd")
	t.Setenv("KIRO_MCP_CONFIG", "/raw/env/mcp.json")

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
	})
	defer m.StopAll()

	got := m.doctorRuntimeOverview()
	for _, notWant := range []string{
		"discord-token-secret-value",
		"kiro-api-key-secret-value",
		"stt-api-key-secret-value",
		"/raw/env/default-cwd",
		"/raw/env/mcp.json",
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
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor runtime overview missing %q:\n%s", want, got)
		}
	}
}

func TestDoctorRuntimeOverviewShowsEffectiveDefaultsWhenEnvUnset(t *testing.T) {
	L.Load("en")
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
