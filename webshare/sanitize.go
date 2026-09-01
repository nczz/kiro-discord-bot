package webshare

import (
	"regexp"
	"strings"
	"unicode"
)

const MaxDisplayNameRunes = 40

var discordMentionish = regexp.MustCompile(`(?i)(@everyone|@here|<[@#][!&]?[0-9]{5,}>|<@&[0-9]{5,}>)`)

func SanitizeDisplayName(raw string, redact func(string) string) string {
	name := strings.TrimSpace(raw)
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(b.String())
	name = discordMentionish.ReplaceAllStringFunc(name, neutralizeMention)
	name = strings.ReplaceAll(name, "@", "＠")
	name = strings.ReplaceAll(name, "<#", "<＃")
	if redact != nil {
		name = strings.TrimSpace(redact(name))
	}
	name = capRunes(name, MaxDisplayNameRunes)
	if strings.TrimSpace(name) == "" {
		return "web"
	}
	return name
}

func neutralizeMention(s string) string {
	s = strings.ReplaceAll(s, "@", "＠")
	return strings.ReplaceAll(s, "<#", "<＃")
}

func capRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
