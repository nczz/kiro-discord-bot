package bot

import (
	"strings"

	"github.com/nczz/kiro-discord-bot/internal/discordfmt"
)

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

func replyLongWithMetadata(ctx cmdCtx, content string, metadata map[string]any) {
	const prefixReserve = 16
	parts := splitDiscordMessage(content, discordReplyLimit-prefixReserve)
	if len(parts) == 0 {
		ctx.sendReplyWithMetadata("", replyPartMetadata(metadata, 1, 1))
		return
	}
	for i, part := range parts {
		if len(parts) > 1 {
			part = formatReplyPart(i, len(parts), part)
		}
		ctx.sendReplyWithMetadata(part, replyPartMetadata(metadata, i+1, len(parts)))
	}
}

func formatReplyPart(idx, total int, content string) string {
	return discordfmt.WithPartPrefix(content, idx, total)
}

func replyPartMetadata(metadata map[string]any, partIndex, partTotal int) map[string]any {
	out := make(map[string]any, len(metadata)+2)
	for k, v := range metadata {
		out[k] = v
	}
	out["part_index"] = partIndex
	out["part_total"] = partTotal
	return out
}

func splitDiscordMessage(content string, limit int) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if limit <= 0 {
		limit = discordReplyLimit
	}
	return discordfmt.Split(content, limit)
}
