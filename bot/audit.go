package bot

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/audit"
)

func (b *Bot) recordBotAuditEvent(evt audit.BotEvent) {
	if b == nil || b.auditRecorder == nil || evt.Type == "" {
		return
	}
	b.auditRecorder.RecordBotEvent(evt)
}

func (b *Bot) recordCommandInvoked(ctx cmdCtx, command, source, messageID, interactionID string) {
	b.recordBotAuditEvent(audit.BotEvent{
		Type:          "bot_command_invoked",
		GuildID:       ctx.guildID,
		ChannelID:     ctx.channelID,
		TargetID:      ctx.targetID,
		ThreadID:      threadIDFromCtx(ctx),
		MessageID:     messageID,
		InteractionID: interactionID,
		UserID:        ctx.userID,
		Username:      ctx.username,
		Command:       command,
		Source:        source,
		Status:        "started",
		Content:       ctx.args,
	})
}

func (b *Bot) recordCommandResponse(ctx cmdCtx, command, source, status, content string) {
	b.recordBotAuditEvent(audit.BotEvent{
		Type:      "bot_command_response_sent",
		GuildID:   ctx.guildID,
		ChannelID: ctx.channelID,
		TargetID:  ctx.targetID,
		ThreadID:  threadIDFromCtx(ctx),
		UserID:    ctx.userID,
		Username:  ctx.username,
		Command:   command,
		Source:    source,
		Status:    status,
		Content:   content,
		Metadata: map[string]any{
			"content_len": len(content),
		},
	})
}

func (b *Bot) recordCommandCompleted(ctx cmdCtx, command, source, status, errText string) {
	b.recordBotAuditEvent(audit.BotEvent{
		Type:      "bot_command_completed",
		GuildID:   ctx.guildID,
		ChannelID: ctx.channelID,
		TargetID:  ctx.targetID,
		ThreadID:  threadIDFromCtx(ctx),
		UserID:    ctx.userID,
		Username:  ctx.username,
		Command:   command,
		Source:    source,
		Status:    status,
		Error:     errText,
	})
}

func threadIDFromCtx(ctx cmdCtx) string {
	if ctx.inThread {
		return ctx.targetID
	}
	return ""
}

func commandNameFromBang(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "!") {
		return ""
	}
	name := strings.Fields(content)
	if len(name) == 0 {
		return ""
	}
	return strings.TrimPrefix(name[0], "!")
}

func (b *Bot) userCanManageAuditTarget(ds *discordgo.Session, userID, targetID string) bool {
	if ds == nil || userID == "" || targetID == "" {
		return false
	}
	if b.userCanManageTarget(ds, userID, targetID) {
		return true
	}
	if parent := stateThreadParent(ds, targetID); parent != "" {
		return b.userCanManageTarget(ds, userID, parent)
	}
	return false
}

func (b *Bot) userCanManageTarget(ds *discordgo.Session, userID, targetID string) bool {
	perms, err := ds.UserChannelPermissions(userID, targetID)
	if err != nil {
		return false
	}
	allowed := int64(discordgo.PermissionAdministrator |
		discordgo.PermissionManageChannels |
		discordgo.PermissionManageMessages |
		discordgo.PermissionManageThreads)
	return perms&allowed != 0
}

func stateThreadParent(ds *discordgo.Session, targetID string) string {
	if ds == nil || ds.State == nil || targetID == "" {
		return ""
	}
	ch, err := ds.State.Channel(targetID)
	if err != nil || ch == nil || !ch.IsThread() {
		return ""
	}
	return ch.ParentID
}
