package channel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
