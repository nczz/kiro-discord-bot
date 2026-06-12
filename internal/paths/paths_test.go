package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirReturnsAbsolutePath(t *testing.T) {
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

	got, err := DataDir("./data")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs("data")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("DataDir = %q, want %q", got, want)
	}
}

func TestDataDirDefault(t *testing.T) {
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

	got, err := DataDir("")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs("data")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("DataDir = %q, want %q", got, want)
	}
}
