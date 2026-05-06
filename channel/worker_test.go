package channel

import (
	"strings"
	"testing"
)

func TestCodeBlockState(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantIn  bool
		wantLng string
	}{
		{"no blocks", "hello world", false, ""},
		{"open block", "text\n```go\nfunc main() {", true, "go"},
		{"closed block", "```go\ncode\n```", false, ""},
		{"open no lang", "text\n```\ncode", true, ""},
		{"two blocks closed", "```\na\n```\n```python\nb\n```", false, ""},
		{"two blocks second open", "```\na\n```\n```js\nb", true, "js"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lang, inBlock := codeBlockState(tt.input)
			if inBlock != tt.wantIn {
				t.Errorf("inBlock = %v, want %v", inBlock, tt.wantIn)
			}
			if lang != tt.wantLng {
				t.Errorf("lang = %q, want %q", lang, tt.wantLng)
			}
		})
	}
}

func TestSplitMessage_Short(t *testing.T) {
	msg := "hello world"
	parts := splitMessage(msg, 100)
	if len(parts) != 1 || parts[0] != msg {
		t.Fatalf("expected 1 part %q, got %v", msg, parts)
	}
}

func TestSplitMessage_ParagraphBoundary(t *testing.T) {
	// Build message with two paragraphs, total > limit
	p1 := strings.Repeat("a", 50)
	p2 := strings.Repeat("b", 50)
	msg := p1 + "\n\n" + p2
	parts := splitMessage(msg, 80)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %v", len(parts), parts)
	}
	if parts[0] != p1 {
		t.Errorf("part[0] = %q, want %q", parts[0], p1)
	}
}

func TestSplitMessage_CodeBlockClosure(t *testing.T) {
	// Code block that spans the split point
	code := "```go\n" + strings.Repeat("x\n", 100) + "```"
	parts := splitMessage(code, 200)
	if len(parts) < 2 {
		t.Fatalf("expected multiple parts, got %d", len(parts))
	}
	// First part should end with ```
	if !strings.HasSuffix(parts[0], "```") {
		t.Errorf("first part should end with ```, got: ...%q", parts[0][len(parts[0])-20:])
	}
	// Second part should start with ```go
	if !strings.HasPrefix(parts[1], "```go\n") {
		t.Errorf("second part should start with ```go, got: %q...", parts[1][:20])
	}
}

func TestSplitMessage_NoCodeBlock(t *testing.T) {
	// Plain text split should not add ```
	msg := strings.Repeat("hello world\n", 200)
	parts := splitMessage(msg, 200)
	for i, p := range parts {
		if strings.Contains(p, "```") {
			t.Errorf("part[%d] should not contain ```: %q", i, p[:50])
		}
	}
}
