package channel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateCWDAllowsConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0755); err != nil {
		t.Fatal(err)
	}

	m := NewManager(ManagerConfig{
		DefaultCWD:      project,
		AllowedCwdRoots: root,
	})

	got, err := m.ValidateCWD(project)
	if err != nil {
		t.Fatalf("ValidateCWD: %v", err)
	}
	want, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}
}

func TestValidateCWDRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	m := NewManager(ManagerConfig{
		DefaultCWD:      root,
		AllowedCwdRoots: root,
	})

	if _, err := m.ValidateCWD(outside); err == nil {
		t.Fatal("expected outside root to be rejected")
	}
}

func TestValidateCWDRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	m := NewManager(ManagerConfig{
		DefaultCWD:      root,
		AllowedCwdRoots: root,
	})

	if _, err := m.ValidateCWD(link); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}
