package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8BytesDoesNotSplitRune(t *testing.T) {
	input := "以加一個中文字"
	got := TruncateUTF8Bytes(input, 8)
	if got != "以加" {
		t.Fatalf("truncated = %q, want %q", got, "以加")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncated string is not valid UTF-8: %q", got)
	}
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("truncated string contains replacement rune: %q", got)
	}
}

func TestTruncateUTF8BytesHandlesSmallBudgets(t *testing.T) {
	if got := TruncateUTF8Bytes("中", 2); got != "" {
		t.Fatalf("truncated = %q, want empty string", got)
	}
	if got := TruncateUTF8Bytes("abc", 0); got != "" {
		t.Fatalf("truncated = %q, want empty string", got)
	}
}
