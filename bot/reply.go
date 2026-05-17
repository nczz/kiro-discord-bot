package bot

import "strings"

const discordReplyLimit = 1900

func replyLong(reply func(string), content string) {
	parts := splitDiscordMessage(content, discordReplyLimit)
	if len(parts) == 0 {
		reply("")
		return
	}
	for _, part := range parts {
		reply(part)
	}
}

func splitDiscordMessage(content string, limit int) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if limit <= 0 {
		limit = discordReplyLimit
	}
	var parts []string
	for len(content) > limit {
		idx := bestDiscordSplit(content, limit)
		part := strings.TrimSpace(content[:idx])
		if part != "" {
			parts = append(parts, part)
		}
		content = strings.TrimSpace(content[idx:])
	}
	if strings.TrimSpace(content) != "" {
		parts = append(parts, strings.TrimSpace(content))
	}
	return parts
}

func bestDiscordSplit(content string, limit int) int {
	window := content[:limit]
	for _, sep := range []string{"\n\n", "\n", " "} {
		if idx := strings.LastIndex(window, sep); idx >= limit/3 {
			return idx + len(sep)
		}
	}
	idx := limit
	for idx > 0 && !isUTF8Start(content[idx]) {
		idx--
	}
	if idx == 0 {
		return limit
	}
	return idx
}

func isUTF8Start(b byte) bool {
	return b < 0x80 || b >= 0xC0
}
