package discordfmt

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var headingPattern = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)\s*$`)

// NormalizeSafeMarkdown keeps useful Discord markdown while lowering headings to bold text.
func NormalizeSafeMarkdown(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	inBlock := false
	for i, line := range lines {
		if codeFenceLine(line) {
			inBlock = !inBlock
			continue
		}
		if inBlock {
			continue
		}
		if m := headingPattern.FindStringSubmatch(line); m != nil {
			text := strings.TrimSpace(m[2])
			if text != "" {
				lines[i] = "**" + strings.Trim(text, "*") + "**"
			}
		}
	}
	return strings.Join(lines, "\n")
}

// Split formats and splits text into Discord-sized message parts.
func Split(s string, limit int) []string {
	s = NormalizeSafeMarkdown(s)
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if limit <= 0 {
		limit = 1900
	}
	var parts []string
	for len(s) > limit {
		idx := findSplitPoint(s, limit)
		if idx <= 0 {
			idx = safeBoundary(s, limit)
		}
		part := strings.Trim(s[:idx], "\n")
		if part != "" {
			lang, inBlock := CodeBlockState(part)
			if inBlock {
				part += "\n```"
			}
			parts = append(parts, part)
			s = strings.TrimLeft(s[idx:], "\n")
			if inBlock {
				s = "```" + lang + "\n" + s
			}
		} else {
			s = strings.TrimLeft(s[idx:], "\n")
		}
	}
	if tail := strings.Trim(s, "\n"); strings.TrimSpace(tail) != "" {
		parts = append(parts, tail)
	}
	return parts
}

// WithPartPrefix prefixes a split part without putting the prefix inside a code block.
func WithPartPrefix(part string, index, total int) string {
	prefix := fmt.Sprintf("(%d/%d)", index+1, total)
	part = strings.TrimSpace(part)
	if part == "" {
		return prefix
	}
	if strings.HasPrefix(part, "```") {
		return prefix + "\n" + part
	}
	return prefix + " " + part
}

// CodeBlockState reports whether text ends inside a fenced code block.
func CodeBlockState(s string) (lang string, inBlock bool) {
	for {
		idx := strings.Index(s, "```")
		if idx < 0 {
			return lang, inBlock
		}
		if !inBlock {
			inBlock = true
			rest := s[idx+3:]
			if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
				lang = rest[:nl]
			} else {
				lang = rest
			}
			if strings.ContainsAny(lang, " \t`") {
				lang = ""
			}
		} else {
			inBlock = false
			lang = ""
		}
		s = s[idx+3:]
	}
}

func findSplitPoint(s string, limit int) int {
	window := s[:limit]
	for _, sep := range []string{"\n\n", "\n- ", "\n* ", "\n1. ", "\n", ". ", " "} {
		if idx := strings.LastIndex(window, sep); idx >= limit/3 {
			if strings.HasPrefix(sep, "\n") && sep != "\n\n" && sep != "\n" {
				return idx + 1
			}
			return idx + len(sep)
		}
	}
	return safeBoundary(s, limit)
}

func safeBoundary(s string, limit int) int {
	if limit >= len(s) {
		return len(s)
	}
	idx := limit
	for idx > 0 && !utf8.RuneStart(s[idx]) {
		idx--
	}
	if idx == 0 {
		return limit
	}
	return idx
}

func codeFenceLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}
