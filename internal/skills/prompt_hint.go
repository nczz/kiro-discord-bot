package skills

import (
	"strings"
	"unicode"

	"github.com/nczz/kiro-discord-bot/internal/textutil"
)

var whenToUseHeadings = map[string]struct{}{
	"when to use": {},
	"使用時機":        {},
	"何時使用":        {},
	"適用情境":        {},
}

// ExtractWhenToUse returns the first concise "When to use" section from a
// canonical skill markdown document. The returned text is safe for use as a
// prompt hint: it is single-line, control-character free, and byte capped.
func ExtractWhenToUse(markdown string, maxBytes int) string {
	lines := strings.Split(markdown, "\n")
	inSection := false
	var parts []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if title, ok := markdownHeadingTitle(trimmed); ok {
			if inSection {
				break
			}
			if _, want := whenToUseHeadings[strings.ToLower(title)]; want {
				inSection = true
			}
			continue
		}
		if !inSection || trimmed == "" {
			continue
		}
		parts = append(parts, trimmed)
	}
	return SanitizePromptHintText(strings.Join(parts, " "), maxBytes)
}

// SanitizePromptHintText compresses user-authored skill metadata into a short
// data-only line. It does not make untrusted text authoritative; callers must
// still wrap the result with instructions that summaries are not executable.
func SanitizePromptHintText(s string, maxBytes int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	if maxBytes <= len("...") {
		return textutil.TruncateUTF8Bytes(s, maxBytes)
	}
	return strings.TrimSpace(textutil.TruncateUTF8Bytes(s, maxBytes-len("..."))) + "..."
}

func markdownHeadingTitle(line string) (string, bool) {
	if !strings.HasPrefix(line, "#") {
		return "", false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(line) || line[level] != ' ' {
		return "", false
	}
	title := strings.TrimSpace(line[level+1:])
	title = strings.TrimSuffix(title, ":")
	return strings.TrimSpace(title), title != ""
}
