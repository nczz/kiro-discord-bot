package channel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAgentsManagedBlockCreatesFile(t *testing.T) {
	cwd := t.TempDir()
	if err := writeAgentsManagedBlock(cwd, "Project rules: be concise."); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
	s := string(b)
	if !strings.Contains(s, agentsBlockStart) || !strings.Contains(s, agentsBlockEnd) || !strings.Contains(s, "be concise") {
		t.Fatalf("AGENTS.md missing managed block/content: %q", s)
	}
}

func TestWriteAgentsManagedBlockPreservesUserContentAndReplacesInPlace(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "AGENTS.md")
	// pre-existing user content
	os.WriteFile(path, []byte("# My Project\nUser note.\n"), 0644)

	if err := writeAgentsManagedBlock(cwd, "steering v1"); err != nil {
		t.Fatalf("write1: %v", err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if !strings.Contains(s, "User note.") {
		t.Fatalf("user content lost: %q", s)
	}
	if !strings.Contains(s, "steering v1") {
		t.Fatalf("block content missing: %q", s)
	}

	// second write replaces the block in place (no duplication), keeps user content
	if err := writeAgentsManagedBlock(cwd, "steering v2"); err != nil {
		t.Fatalf("write2: %v", err)
	}
	b, _ = os.ReadFile(path)
	s = string(b)
	if strings.Contains(s, "steering v1") {
		t.Fatalf("old block not replaced: %q", s)
	}
	if !strings.Contains(s, "steering v2") || !strings.Contains(s, "User note.") {
		t.Fatalf("v2 replace wrong: %q", s)
	}
	if strings.Count(s, agentsBlockStart) != 1 {
		t.Fatalf("managed block duplicated: %q", s)
	}
}
