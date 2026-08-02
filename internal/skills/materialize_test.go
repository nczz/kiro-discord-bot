package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeWritesProjectLocalSkill(t *testing.T) {
	project := t.TempDir()
	file, err := Materialize(project, "erp-reconcile", "# When to use\nUse it.\n", false)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if file.RelativePath != ".kiro-bot/skills/erp-reconcile/SKILL.md" || file.SHA256 == "" {
		t.Fatalf("materialized file = %+v", file)
	}
	raw, err := os.ReadFile(filepath.Join(project, ".kiro-bot", "skills", "erp-reconcile", "SKILL.md"))
	if err != nil {
		t.Fatalf("read materialized file: %v", err)
	}
	if string(raw) != "# When to use\nUse it.\n" {
		t.Fatalf("content = %q", raw)
	}
}

func TestValidateProjectCWDRejectsSymlinkOutsideAllowedRoot(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(allowed, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if got, err := ValidateProjectCWD(link, []string{allowed}); err == nil {
		t.Fatalf("ValidateProjectCWD accepted symlink outside root: %q", got)
	}
}

func TestMaterializeRejectsSymlinkAncestor(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".kiro-bot"), 0755); err != nil {
		t.Fatalf("mkdir .kiro-bot: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(project, ".kiro-bot", "skills")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Materialize(project, "unsafe", "content", false); err == nil {
		t.Fatal("Materialize accepted symlinked skills directory")
	}
}
