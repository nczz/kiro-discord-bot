package discordfmt

import (
	"strings"
	"testing"
)

func TestNormalizeSafeMarkdownDemotesHeadingsOutsideCodeBlocks(t *testing.T) {
	input := "# Title\n\n```md\n# keep heading in code\n```\n\n## Next"
	got := NormalizeSafeMarkdown(input)
	want := "**Title**\n\n```md\n# keep heading in code\n```\n\n**Next**"
	if got != want {
		t.Fatalf("NormalizeSafeMarkdown() = %q, want %q", got, want)
	}
}

func TestSplitClosesAndReopensCodeBlocks(t *testing.T) {
	input := "```go\n" + strings.Repeat("fmt.Println(\"hello\")\n", 30) + "```"
	parts := Split(input, 120)
	if len(parts) < 2 {
		t.Fatalf("parts = %d, want multiple", len(parts))
	}
	for i, part := range parts[:len(parts)-1] {
		if !strings.HasSuffix(part, "```") {
			t.Fatalf("part %d does not close code block: %q", i, part)
		}
	}
	for i, part := range parts[1:] {
		if !strings.HasPrefix(part, "```go\n") {
			t.Fatalf("part %d does not reopen code block: %q", i+1, part)
		}
	}
}

func TestWithPartPrefixStaysOutsideCodeBlock(t *testing.T) {
	got := WithPartPrefix("```go\nfmt.Println(1)\n```", 1, 3)
	if !strings.HasPrefix(got, "(2/3)\n```go\n") {
		t.Fatalf("prefix inserted incorrectly: %q", got)
	}
}

func TestSplitKeepsUTF8Valid(t *testing.T) {
	input := strings.Repeat("中文內容", 80)
	parts := Split(input, 101)
	for i, part := range parts {
		if strings.ContainsRune(part, '\uFFFD') {
			t.Fatalf("part %d contains replacement rune: %q", i, part)
		}
	}
}

func TestSplitPreservesCodeIndentation(t *testing.T) {
	input := "```go\nfunc main() {\n    fmt.Println(\"hello\")\n}\n" + strings.Repeat("    fmt.Println(\"x\")\n", 20) + "```"
	parts := Split(input, 90)
	joined := strings.Join(parts, "\n")
	if !strings.Contains(joined, "    fmt.Println") {
		t.Fatalf("indented code was not preserved: %q", joined)
	}
}
