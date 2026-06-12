package main

import (
	"os"
	"path/filepath"
	"testing"
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
