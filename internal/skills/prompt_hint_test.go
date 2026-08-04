package skills

import (
	"strings"
	"testing"
)

func TestExtractWhenToUseReturnsSingleLineSection(t *testing.T) {
	md := `# Skill

## When to use
Use this for ERP exports.
- Match invoices against workbooks.

## Procedure
Do the real work.`

	got := ExtractWhenToUse(md, 200)
	if got != "Use this for ERP exports. - Match invoices against workbooks." {
		t.Fatalf("ExtractWhenToUse = %q", got)
	}
}

func TestExtractWhenToUseSupportsLocalizedHeading(t *testing.T) {
	md := "# 技能\n\n## 使用時機\n處理 Discord 發布前檢查。\n\n## Procedure\nRun checks."
	got := ExtractWhenToUse(md, 200)
	if got != "處理 Discord 發布前檢查。" {
		t.Fatalf("localized ExtractWhenToUse = %q", got)
	}
}

func TestExtractWhenToUseSanitizesAndCaps(t *testing.T) {
	md := "# Skill\n\n## When to use\n" + strings.Repeat("abc\n", 20)
	got := ExtractWhenToUse(md, 16)
	if strings.ContainsAny(got, "\n\r\t") {
		t.Fatalf("hint contains control whitespace: %q", got)
	}
	if len(got) > 16 {
		t.Fatalf("hint len = %d, want <= 16: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("capped hint should end with ellipsis: %q", got)
	}
}

func TestExtractWhenToUseMissingSectionReturnsEmpty(t *testing.T) {
	if got := ExtractWhenToUse("# Skill\n\n## Procedure\nRun it.", 200); got != "" {
		t.Fatalf("missing section = %q, want empty", got)
	}
}

func TestSanitizePromptHintTextNonPositiveLimitReturnsEmpty(t *testing.T) {
	if got := SanitizePromptHintText("do not leak this whole hint", 0); got != "" {
		t.Fatalf("zero-limit sanitized hint = %q, want empty", got)
	}
}
