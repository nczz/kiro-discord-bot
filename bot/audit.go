package bot

import (
	"fmt"
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
	b.recordCommandResponseWithMetadata(ctx, command, source, status, content, nil)
}

func (b *Bot) recordCommandResponseWithMetadata(ctx cmdCtx, command, source, status, content string, metadata map[string]any) {
	b.recordCommandResponseDelivery(ctx, command, source, status, content, metadata, nil, nil)
}

func (b *Bot) recordCommandResponseDelivery(ctx cmdCtx, command, source, status, content string, metadata map[string]any, msg *discordgo.Message, sendErr error) {
	copied := make(map[string]any, len(metadata)+1)
	for k, v := range metadata {
		copied[k] = v
	}
	copied["content_len"] = len(content)
	if msg != nil && msg.ID != "" {
		copied["response_message_id"] = msg.ID
		if msg.ChannelID != "" {
			copied["response_channel_id"] = msg.ChannelID
		}
	}
	errText := ""
	eventType := "bot_command_response_sent"
	if sendErr != nil {
		eventType = "bot_command_response_failed"
		status = "error"
		errText = sendErr.Error()
		copied["send_error"] = errText
	}
	b.recordBotAuditEvent(audit.BotEvent{
		Type:          eventType,
		GuildID:       ctx.guildID,
		ChannelID:     ctx.channelID,
		TargetID:      ctx.targetID,
		ThreadID:      threadIDFromCtx(ctx),
		MessageID:     ctx.messageID,
		InteractionID: ctx.interactionID,
		UserID:        ctx.userID,
		Username:      ctx.username,
		Command:       command,
		Source:        source,
		Status:        status,
		Content:       content,
		Error:         errText,
		Metadata:      copied,
	})
}

func (b *Bot) recordInteractionResponseDelivery(ctx cmdCtx, command, status, content string, responseType discordgo.InteractionResponseType, metadata map[string]any, sendErr error) {
	copied := make(map[string]any, len(metadata)+1)
	for k, v := range metadata {
		copied[k] = v
	}
	copied["interaction_response_type"] = fmt.Sprintf("%d", responseType)
	b.recordCommandResponseDelivery(ctx, command, "slash", status, content, copied, nil, sendErr)
}

func (b *Bot) recordCommandCompleted(ctx cmdCtx, command, source, status, errText string) {
	b.recordBotAuditEvent(audit.BotEvent{
		Type:          "bot_command_completed",
		GuildID:       ctx.guildID,
		ChannelID:     ctx.channelID,
		TargetID:      ctx.targetID,
		ThreadID:      threadIDFromCtx(ctx),
		MessageID:     ctx.messageID,
		InteractionID: ctx.interactionID,
		UserID:        ctx.userID,
		Username:      ctx.username,
		Command:       command,
		Source:        source,
		Status:        status,
		Error:         errText,
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
