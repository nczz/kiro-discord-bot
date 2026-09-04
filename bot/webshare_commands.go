package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/audit"
	L "github.com/nczz/kiro-discord-bot/locale"
	"github.com/nczz/kiro-discord-bot/webshare"
)

func webshareSlashOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "start", Description: L.Get("cmd.webshare.sub.start")},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "stop", Description: L.Get("cmd.webshare.sub.stop")},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "status", Description: L.Get("cmd.webshare.sub.status")},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "revoke", Description: L.Get("cmd.webshare.sub.revoke"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "share_id", Description: L.Get("cmd.webshare.opt.share_id"), Required: false},
			{Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: L.Get("cmd.webshare.opt.reason"), Required: false},
		}},
	}
}

func webshareArgsFromSlashOptions(options []*discordgo.ApplicationCommandInteractionDataOption) string {
	if len(options) == 0 {
		return "status"
	}
	sub := options[0]
	if sub.Name == "revoke" {
		return webshareRevokeArgsFromSlashOptions(sub.Options)
	}
	parts := []string{sub.Name}
	for _, opt := range sub.Options {
		if s := strings.TrimSpace(opt.StringValue()); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

func webshareRevokeArgsFromSlashOptions(options []*discordgo.ApplicationCommandInteractionDataOption) string {
	shareID, reason := "", ""
	for _, opt := range options {
		switch opt.Name {
		case "share_id":
			shareID = strings.TrimSpace(opt.StringValue())
		case "reason":
			reason = strings.TrimSpace(opt.StringValue())
		}
	}
	parts := []string{"revoke"}
	if shareID != "" {
		parts = append(parts, shareID)
	}
	if reason != "" {
		if shareID == "" {
			parts = append(parts, "--reason")
		}
		parts = append(parts, reason)
	}
	return strings.Join(parts, " ")
}

func (b *Bot) cmdWebShare(ctx cmdCtx) {
	fields := strings.Fields(strings.TrimSpace(ctx.args))
	sub := "status"
	if len(fields) > 0 {
		sub = strings.ToLower(fields[0])
	}
	switch sub {
	case "start":
		b.cmdWebShareStart(ctx)
	case "stop":
		b.cmdWebShareStop(ctx)
	case "status":
		b.cmdWebShareStatus(ctx)
	case "revoke":
		shareID, reason := "", ""
		args := fields[1:]
		if len(args) > 0 {
			if args[0] == "--reason" {
				reason = strings.Join(args[1:], " ")
			} else {
				shareID = args[0]
				reason = strings.Join(args[1:], " ")
			}
		}
		b.cmdWebShareRevoke(ctx, shareID, reason)
	default:
		ctx.reply(L.Get("webshare.usage"))
	}
}

func (b *Bot) cmdWebShareStart(ctx cmdCtx) {
	if b == nil || b.webshareStore == nil || !b.webshareConfig.ready() {
		ctx.reply(L.Get("webshare.disabled"))
		return
	}
	if !b.userCanManageAuditTarget(b.discord, ctx.userID, ctx.targetID) {
		ctx.reply(L.Get("webshare.forbidden"))
		return
	}
	if !b.manager.ChannelInitialized(ctx.channelID) {
		ctx.reply(L.Getf("setup.required.command", "/webshare start"))
		return
	}
	webhookChannelID := ctx.targetID
	if ctx.inThread {
		webhookChannelID = ctx.channelID
	}
	if err := b.requireWebShareWebhookPermission(webhookChannelID); err != nil {
		ctx.reply(webshareWebhookFailureEvent("discord_webhook_permission_check_failed", err).Content)
		return
	}
	mat, err := webshare.GenerateSecretMaterial()
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	viewLink, err := webshare.FormatViewLink(b.webshareConfig.PublicBaseURL, mat.RoomID, mat.RoomKey)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	writeLink, err := webshare.FormatWriteLink(b.webshareConfig.PublicBaseURL, mat.RoomID, mat.RoomKey, mat.WriteToken)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	targetType := webshare.TargetChannel
	parentID := ""
	if ctx.inThread {
		targetType = webshare.TargetThread
		parentID = ctx.channelID
	}
	share, err := b.webshareStore.CreateShare(context.Background(), webshare.CreateShareRequest{
		ShareID:         mat.ShareID,
		GuildID:         ctx.guildID,
		TargetType:      targetType,
		TargetID:        ctx.targetID,
		ParentChannelID: parentID,
		OpenerUserID:    ctx.userID,
		OpenerUsername:  ctx.username,
		RelayURL:        b.webshareConfig.RelayURL,
		PublicBaseURL:   b.webshareConfig.PublicBaseURL,
		RoomID:          mat.RoomID,
		RoomKey:         mat.RoomKey,
		WriteToken:      mat.WriteToken,
		Capabilities:    webshare.WriteCapabilities(),
		Status:          webshare.StatusCreated,
	})
	if errors.Is(err, webshare.ErrActiveShare) {
		ctx.reply(L.Get("webshare.active_exists"))
		return
	}
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	b.recordWebShareAudit(*share, webshare.EventShareCreated, ctx.userID, ctx.username, ctx.targetID, true, "", map[string]any{"subcommand": "start"})
	b.startWebShareHost(*share)
	ctx.replyWithMetadata(L.Getf("webshare.started", writeLink, viewLink), map[string]any{"webshare_share_id": share.ShareID, "ephemeral": true, "redact_audit_content": true})
}

func (b *Bot) cmdWebShareStop(ctx cmdCtx) {
	share, ok := b.findActiveWebShare(ctx.userID, ctx.targetID, ctx.channelID, true)
	if !ok {
		ctx.reply(L.Get("webshare.none"))
		return
	}
	if share.OpenerUserID != ctx.userID && !b.userCanManageAuditTarget(b.discord, ctx.userID, share.TargetID) {
		ctx.reply(L.Get("webshare.forbidden"))
		return
	}
	if err := b.webshareStore.Revoke(context.Background(), share.ShareID, ctx.userID, "stopped"); err != nil {
		ctx.reply(commandError(err))
		return
	}
	_ = b.sendWebShareHostEvent(share.ShareID, webshare.ServerEvent{Type: "bye", Status: "ok", ReasonCode: "stopped"})
	b.stopWebShareHostSoon(share.ShareID)
	b.recordWebShareAudit(share, webshare.EventRevoked, ctx.userID, ctx.username, share.TargetID, true, "stopped", map[string]any{"subcommand": "stop"})
	ctx.reply(L.Get("webshare.stopped"))
}

func (b *Bot) cmdWebShareStatus(ctx cmdCtx) {
	share, ok := b.findActiveWebShare(ctx.userID, ctx.targetID, ctx.channelID, true)
	if !ok {
		ctx.reply(L.Get("webshare.none"))
		return
	}
	ctx.reply(L.Getf("webshare.status", share.ShareID, string(share.Status), share.TargetID, share.CreatedAt.Format(time.RFC3339)))
}

func (b *Bot) cmdWebShareRevoke(ctx cmdCtx, shareID, reason string) {
	if b == nil || b.webshareStore == nil {
		ctx.reply(L.Get("webshare.disabled"))
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "revoked"
	}
	var share webshare.Share
	if strings.TrimSpace(shareID) != "" {
		got, err := b.webshareStore.GetShare(context.Background(), shareID)
		if err != nil {
			ctx.reply(L.Get("webshare.none"))
			return
		}
		share = *got
	} else if got, ok := b.findActiveWebShare(ctx.userID, ctx.targetID, ctx.channelID, true); ok {
		share = got
	} else {
		ctx.reply(L.Get("webshare.none"))
		return
	}
	if share.OpenerUserID != ctx.userID && !b.userCanManageAuditTarget(b.discord, ctx.userID, share.TargetID) {
		ctx.reply(L.Get("webshare.forbidden"))
		return
	}
	if err := b.webshareStore.Revoke(context.Background(), share.ShareID, ctx.userID, reason); err != nil {
		ctx.reply(commandError(err))
		return
	}
	_ = b.sendWebShareHostEvent(share.ShareID, webshare.ServerEvent{Type: "bye", Status: "ok", ReasonCode: "revoked"})
	b.stopWebShareHostSoon(share.ShareID)
	b.recordWebShareAudit(share, webshare.EventRevoked, ctx.userID, ctx.username, share.TargetID, true, "revoked", map[string]any{"subcommand": "revoke", "reason_code": reason})
	ctx.reply(L.Get("webshare.revoked"))
}

func (b *Bot) recordWebShareAudit(share webshare.Share, eventType, actorUserID, actorName, targetID string, allowed bool, reason string, metadata map[string]any) {
	if b == nil {
		return
	}
	metadata = sanitizeWebShareMetadata(metadata)
	if b.webshareStore != nil {
		_, _ = b.webshareStore.RecordEvent(context.Background(), webshare.Event{ShareID: share.ShareID, Type: eventType, ActorUserID: actorUserID, RemoteActorName: actorName, TargetID: targetID, Allowed: allowed, ReasonCode: reason, Metadata: metadata})
	}
	threadID, parentID := "", share.ParentChannelID
	if share.TargetType == webshare.TargetThread {
		threadID = share.TargetID
	}
	b.recordBotAuditEvent(audit.BotEvent{Type: eventType, GuildID: share.GuildID, ChannelID: webshareAuditChannel(share), TargetID: targetID, ThreadID: threadID, ParentChannelID: parentID, UserID: actorUserID, Username: actorName, Source: "webshare", Status: webshareAuditStatus(allowed), Metadata: metadata})
}

func webshareAuditChannel(share webshare.Share) string {
	if share.ParentChannelID != "" {
		return share.ParentChannelID
	}
	return share.TargetID
}

func webshareAuditStatus(allowed bool) string {
	if allowed {
		return "allowed"
	}
	return "rejected"
}

func sanitizeWebShareMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "secret") || strings.Contains(lk, "token") || strings.Contains(lk, "link") || strings.Contains(lk, "url") || strings.Contains(lk, "path") || strings.Contains(lk, "cwd") {
			continue
		}
		out[k] = fmt.Sprint(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
