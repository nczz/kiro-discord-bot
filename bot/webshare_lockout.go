package bot

import (
	"context"
	"strings"

	L "github.com/nczz/kiro-discord-bot/locale"
	"github.com/nczz/kiro-discord-bot/webshare"
)

func webshareCommandAllowedDuringLockout(command string) bool {
	command = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(command, "!")))
	return command == "webshare"
}

func (b *Bot) rejectWebShareLockedDiscordUse(userID, targetID, parentID, command string) bool {
	if b == nil || b.webshareStore == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	if webshareCommandAllowedDuringLockout(command) {
		return false
	}
	share, ok := b.findActiveWebShare(userID, targetID, parentID, false)
	if !ok {
		return false
	}
	b.recordWebShareAudit(share, webshare.EventActionRejected, userID, share.OpenerUsername, targetID, false, "opener_discord_lockout", map[string]any{"surface": "discord"})
	return true
}

func (b *Bot) findActiveWebShare(userID, targetID, parentID string, allowAnyManager bool) (webshare.Share, bool) {
	if b == nil || b.webshareStore == nil {
		return webshare.Share{}, false
	}
	shares, err := b.webshareStore.ListActive(context.Background())
	if err != nil {
		return webshare.Share{}, false
	}
	for _, share := range shares {
		if share.GuildID != "" && b.guildID != "" && share.GuildID != b.guildID {
			continue
		}
		if !allowAnyManager && share.OpenerUserID != userID {
			continue
		}
		if allowAnyManager && share.OpenerUserID != userID && !b.userCanManageAuditTarget(b.discord, userID, share.TargetID) {
			continue
		}
		if webshareShareCoversTarget(b.webshareStore, share, targetID, parentID) {
			return share, true
		}
	}
	return webshare.Share{}, false
}

func webshareShareCoversTarget(store *webshare.Store, share webshare.Share, targetID, parentID string) bool {
	targetID = strings.TrimSpace(targetID)
	parentID = strings.TrimSpace(parentID)
	if targetID == "" {
		return false
	}
	if share.TargetID == targetID {
		return true
	}
	if share.TargetType == webshare.TargetChannel && parentID == share.TargetID && store != nil {
		if _, err := store.ResolveManagedChildThread(context.Background(), share.ShareID, targetID); err == nil {
			return true
		}
	}
	return false
}

func (b *Bot) webshareLockoutMessage() string { return L.Get("webshare.locked") }
