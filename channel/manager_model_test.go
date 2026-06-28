package channel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/acp"
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

func TestListModelsOmpRequiresActiveAgent(t *testing.T) {
	m := newEngineTestManager(t, "omp")

	_, err := m.ListModels("ch1")
	if err == nil {
		t.Fatal("expected inactive omp model listing to fail")
	}
	if !strings.Contains(err.Error(), "active omp agent") {
		t.Fatalf("error = %v, want active omp agent guidance", err)
	}
}

func TestValidateModelForOmpChannelDoesNotCallKiroCLI(t *testing.T) {
	m := newEngineTestManager(t, "omp")
	m.kiroCLI = filepath.Join(t.TempDir(), "missing-kiro-cli")

	err := m.validateModelForChannel("ch1", "gpt-5")
	if err == nil {
		t.Fatal("expected inactive omp model validation to fail")
	}
	if !strings.Contains(err.Error(), "active omp agent") {
		t.Fatalf("error = %v, want active omp agent guidance", err)
	}
	if strings.Contains(err.Error(), "list models") {
		t.Fatalf("error shows kiro-cli fallback was used: %v", err)
	}
}

func TestListThreadModelsOmpRequiresActiveThreadAgent(t *testing.T) {
	m := newEngineTestManager(t, "kiro")
	if err := m.setChannelSession("parent", &Session{Engine: acp.DialectOmp.String()}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}

	_, err := m.ListThreadModels("thread", "parent")
	if err == nil {
		t.Fatal("expected inactive omp thread model listing to fail")
	}
	if !strings.Contains(err.Error(), "active omp thread agent") {
		t.Fatalf("error = %v, want active omp thread agent guidance", err)
	}
}
