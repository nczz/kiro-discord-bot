package channel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/acp"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func fakeKiroCLI(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kiro-cli")
	script := `#!/bin/sh
if [ "$1" = "chat" ] && [ "$2" = "--list-models" ] && [ "$3" = "-f" ] && [ "$4" = "json" ]; then
  printf '%s\n' '{"default_model":"model-b","models":[{"model_name":"Model A","model_id":"model-a","description":"Alpha","rate_multiplier":1,"rate_unit":"x"},{"model_name":"Model B","model_id":"model-b","description":"Beta","rate_multiplier":2,"rate_unit":"x"}]}'
  exit 0
fi
echo "unexpected args: $*" >&2
exit 2
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return path
}

func TestValidateModelIDUsesCLIModels(t *testing.T) {
	m := NewManager(ManagerConfig{KiroCLIPath: fakeKiroCLI(t)})

	if err := m.validateModelID("model-a"); err != nil {
		t.Fatalf("validate existing model: %v", err)
	}
	err := m.validateModelID("missing")
	if err == nil {
		t.Fatal("expected missing model to fail")
	}
	if !strings.Contains(err.Error(), "model-a") || !strings.Contains(err.Error(), "model-b") {
		t.Fatalf("error should include available models, got %v", err)
	}
}

func TestListModelsMarksCLIDefault(t *testing.T) {
	m := NewManager(ManagerConfig{KiroCLIPath: fakeKiroCLI(t)})

	got, err := m.ListModels("")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if !strings.Contains(got, "▸ `model-b`") {
		t.Fatalf("expected default model-b to be marked, got:\n%s", got)
	}
	if strings.Contains(got, "▸ `model-a`") {
		t.Fatalf("did not expect model-a to be marked, got:\n%s", got)
	}
}

func TestListModelsOmpStartsOneShotAgent(t *testing.T) {
	L.Load("en")
	m := newEngineTestManager(t, "omp")
	m.ompPath = fakeACPBinary(t)
	m.defaultCWD = t.TempDir()

	got, err := m.ListModels("ch1")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if !strings.Contains(got, "▸ `openai-codex/gpt-5.5`") || !strings.Contains(got, "`openai-codex/gpt-5`") {
		t.Fatalf("model list = %s", got)
	}
}

func TestValidateModelForOmpChannelUsesOneShotAgent(t *testing.T) {
	L.Load("en")
	m := newEngineTestManager(t, "omp")
	m.kiroCLI = filepath.Join(t.TempDir(), "missing-kiro-cli")
	m.ompPath = fakeACPBinary(t)
	m.defaultCWD = t.TempDir()

	if err := m.validateModelForChannel("ch1", "openai-codex/gpt-5"); err != nil {
		t.Fatalf("validate omp model: %v", err)
	}
	err := m.validateModelForChannel("ch1", "missing-model")
	if err == nil {
		t.Fatal("expected missing omp model to fail")
	}
	if strings.Contains(err.Error(), "list models") {
		t.Fatalf("error shows kiro-cli fallback was used: %v", err)
	}
	if !strings.Contains(err.Error(), "openai-codex/gpt-5") {
		t.Fatalf("error = %v, want omp model catalog", err)
	}
}

func TestSwitchModelOmpInactiveUsesOneShotValidation(t *testing.T) {
	L.Load("en")
	m := newEngineTestManager(t, "omp")
	m.ompPath = fakeACPBinary(t)
	m.defaultCWD = t.TempDir()

	restarted, err := m.SwitchModel("ch1", "openai-codex/gpt-5")
	if err != nil {
		t.Fatalf("SwitchModel: %v", err)
	}
	t.Cleanup(func() { m.StopAll() })
	if !restarted {
		t.Fatal("inactive omp switch should start the agent")
	}
	sess, ok := m.getChannelSession("ch1")
	if !ok || sess.Model != "openai-codex/gpt-5" {
		t.Fatalf("session = %+v, ok=%v", sess, ok)
	}
	m.mu.Lock()
	agent := m.agents["ch1"]
	m.mu.Unlock()
	if agent == nil {
		t.Fatal("agent was not started")
	}
	if got := agent.CurrentModelID(); got != "openai-codex/gpt-5" {
		t.Fatalf("agent current model = %q", got)
	}
}

func TestListThreadModelsOmpStartsOneShotAgent(t *testing.T) {
	L.Load("en")
	m := newEngineTestManager(t, "kiro")
	m.ompPath = fakeACPBinary(t)
	cwd := t.TempDir()
	m.defaultCWD = cwd
	if err := m.setChannelSession("parent", &Session{CWD: cwd, Engine: acp.DialectOmp.String()}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}

	got, err := m.ListThreadModels("thread", "parent")
	if err != nil {
		t.Fatalf("ListThreadModels: %v", err)
	}
	if !strings.Contains(got, "▸ `openai-codex/gpt-5.5`") || !strings.Contains(got, "`openai-codex/gpt-5`") {
		t.Fatalf("thread model list = %s", got)
	}
}
