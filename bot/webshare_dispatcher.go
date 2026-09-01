package bot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	L "github.com/nczz/kiro-discord-bot/locale"
	"github.com/nczz/kiro-discord-bot/webshare"
)

type webshareUploadSession struct {
	shareID  string
	targetID string
	parentID string
	uploadID string
	path     string
	filename string
	mime     string
	size     int64
	sha256   string
	received int64
	nextSeq  uint64
	hash     hash.Hash
}

const webshareMaxThreadNameBytes = 90

const webshareWebhookName = "KDB WebShare"

type webshareWebhookCredential struct {
	ID    string
	Token string
}

var errWebShareWebhookPermission = errors.New("webshare webhook permission missing")

// HandleWebShareAction is the bot-side dispatcher for decrypted browser actions.
// It never fabricates Discord events: it reuses the stored share/opener identity,
// rechecks Discord permissions, and enters agent work only through channel.Manager.
func (b *Bot) HandleWebShareAction(ctx context.Context, shareID string, action webshare.ClientAction) webshare.ServerEvent {
	if b == nil || b.webshareStore == nil {
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "webshare_disabled", Content: L.Get("webshare.disabled")}
	}
	share, err := b.webshareStore.GetShare(ctx, shareID)
	if err != nil || share == nil || !share.Status.ActiveLocking() {
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "share_not_active", Content: L.Get("webshare.none")}
	}
	if action.Type == "" || action.Type == "hello" || action.Type == "status" {
		return b.webshareWelcomeEvent(*share)
	}
	if !b.webshareActionCanWrite(*share, action) {
		b.recordWebShareAudit(*share, webshare.EventActionRejected, share.OpenerUserID, action.DisplayName, share.TargetID, false, "write_token_invalid", map[string]any{"action": action.Type})
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "write_token_invalid", Content: L.Get("webshare.rejected")}
	}
	if !b.userCanManageAuditTarget(b.discord, share.OpenerUserID, share.TargetID) {
		_ = b.webshareStore.MarkStatus(ctx, share.ShareID, webshare.StatusDegraded)
		b.recordWebShareAudit(*share, webshare.EventActionRejected, share.OpenerUserID, action.DisplayName, share.TargetID, false, "opener_permission_lost", map[string]any{"action": action.Type})
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "opener_permission_lost", Content: L.Get("webshare.rejected")}
	}

	requestedThreadID := action.TargetThreadID
	if action.Type == "select_thread" {
		requestedThreadID = action.ThreadID
	}
	targetID, parentID, err := b.webshareResolveActionTarget(ctx, *share, requestedThreadID)
	if err != nil {
		b.recordWebShareAudit(*share, webshare.EventActionRejected, share.OpenerUserID, action.DisplayName, share.TargetID, false, "target_scope", map[string]any{"action": action.Type})
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "target_scope", Content: err.Error()}
	}
	if targetID != share.TargetID && !b.userCanManageAuditTarget(b.discord, share.OpenerUserID, targetID) {
		_ = b.webshareStore.MarkStatus(ctx, share.ShareID, webshare.StatusDegraded)
		b.recordWebShareAudit(*share, webshare.EventActionRejected, share.OpenerUserID, action.DisplayName, targetID, false, "opener_permission_lost", map[string]any{"action": action.Type})
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "opener_permission_lost", Content: L.Get("webshare.rejected")}
	}

	b.recordWebShareAudit(*share, webshare.EventActionRequested, share.OpenerUserID, action.DisplayName, targetID, true, "", map[string]any{"action": action.Type})
	switch action.Type {
	case "prompt", "send_agent_prompt":
		return b.websharePrompt(ctx, *share, action, targetID, parentID)
	case "post_channel_message":
		return b.websharePostChannelMessage(ctx, *share, action, targetID, parentID)
	case "run_bot_command":
		return b.webshareRunBotCommand(ctx, *share, action, targetID, parentID)
	case "create_thread":
		return b.webshareCreateThread(ctx, *share, action)
	case "select_thread":
		return webshare.ServerEvent{Type: "thread_event", Status: "ok", Event: map[string]any{"eventID": webshareEventID("thread-selected", targetID), "thread": map[string]any{"id": targetID, "name": targetID, "parentChannelID": parentID, "selected": true}, "action": "selected"}, Metadata: map[string]any{"thread_id": targetID}}
	case "interrupt", "interrupt_agent":
		return b.webshareInterrupt(*share, targetID, parentID)
	case "upload_init", "upload_attachment":
		return b.webshareUploadInit(ctx, *share, action, targetID, parentID)
	case "upload_chunk":
		return b.webshareUploadChunk(*share, action)
	case "upload_finish":
		return b.webshareUploadFinish(ctx, *share, action)
	case "fetch_attachment", "fetch_discord_attachment":
		return b.webshareFetchAttachment(ctx, *share, action)
	case "stop", "revoke":
		if err := b.webshareStore.Revoke(ctx, share.ShareID, share.OpenerUserID, action.Type); err != nil {
			return webshare.ServerEvent{Type: "error", Status: "error", ReasonCode: "revoke_failed", Content: err.Error()}
		}
		b.stopWebShareHost(share.ShareID)
		return webshare.ServerEvent{Type: "share_revoked", Status: "ok"}
	default:
		b.recordWebShareAudit(*share, webshare.EventActionRejected, share.OpenerUserID, action.DisplayName, targetID, false, "unknown_action", map[string]any{"action": action.Type})
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "unknown_action", Content: L.Get("webshare.unknown_action")}
	}
}

func (b *Bot) webshareActionCanWrite(share webshare.Share, action webshare.ClientAction) bool {
	if action.Type == "status" || action.Type == "hello" {
		return true
	}
	token, err := decodeWebShareWriteToken(action.WriteToken)
	if err != nil || len(token) == 0 {
		return false
	}
	return webshare.VerifyTokenHash(token, share.WriteTokenHash)
}

func decodeWebShareWriteToken(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty token")
	}
	if b, err := base64.RawURLEncoding.DecodeString(raw); err == nil {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return b, nil
	}
	return []byte(raw), nil
}

func (b *Bot) webshareResolveActionTarget(ctx context.Context, share webshare.Share, requestedThreadID string) (targetID, parentID string, err error) {
	requestedThreadID = strings.TrimSpace(requestedThreadID)
	if requestedThreadID == "" {
		return share.TargetID, share.ParentChannelID, nil
	}
	if share.TargetType == webshare.TargetThread {
		if requestedThreadID == share.TargetID {
			return share.TargetID, share.ParentChannelID, nil
		}
		return "", "", fmt.Errorf("thread is outside this share")
	}
	child, err := b.webshareStore.ResolveManagedChildThread(ctx, share.ShareID, requestedThreadID)
	if err != nil {
		return "", "", fmt.Errorf("thread is outside this share")
	}
	if child.ParentChannelID != share.TargetID {
		return "", "", fmt.Errorf("thread parent is outside this share")
	}
	return child.ThreadID, child.ParentChannelID, nil
}

func (b *Bot) webshareWelcomeEvent(share webshare.Share) webshare.ServerEvent {
	return webshare.ServerEvent{Type: "welcome", Status: string(share.Status), Share: map[string]any{"id": share.ShareID, "roomID": share.RoomID, "status": string(share.Status), "mode": "write", "createdAt": share.CreatedAt.Format(time.RFC3339)}, Target: b.webshareTargetView(share), Opener: map[string]any{"id": share.OpenerUserID, "displayName": displayOrDefault(share.OpenerUsername), "username": share.OpenerUsername}, Capabilities: share.Capabilities, MentionableUsers: b.webshareMentionableUsers(share.GuildID), MentionableBot: b.webshareMentionableBot(), Threads: b.webshareThreadViews(share), SelectedThreadID: webshareSelectedThread(share)}
}

func (b *Bot) webshareTargetView(share webshare.Share) map[string]any {
	view := map[string]any{"guildID": share.GuildID, "channelID": share.TargetID, "targetType": string(share.TargetType)}
	if share.TargetType == webshare.TargetThread {
		view["channelID"] = share.ParentChannelID
		view["threadID"] = share.TargetID
	}
	if b != nil && b.discord != nil {
		if ch, err := b.discord.Channel(share.TargetID); err == nil && ch != nil {
			if share.TargetType == webshare.TargetThread {
				view["threadName"] = ch.Name
			} else {
				view["channelName"] = ch.Name
			}
		}
		if share.TargetType == webshare.TargetThread && share.ParentChannelID != "" {
			if parent, err := b.discord.Channel(share.ParentChannelID); err == nil && parent != nil {
				view["channelName"] = parent.Name
			}
		}
	}
	return view
}

func (b *Bot) webshareThreadViews(share webshare.Share) []map[string]any {
	if b == nil || b.webshareStore == nil || share.TargetType != webshare.TargetChannel {
		return nil
	}
	children, err := b.webshareStore.ListManagedChildThreads(context.Background(), share.ShareID)
	if err != nil || len(children) == 0 {
		return nil
	}
	views := make([]map[string]any, 0, len(children))
	for _, child := range children {
		if child.ParentChannelID != share.TargetID || strings.TrimSpace(child.ThreadID) == "" {
			continue
		}
		name := strings.TrimSpace(child.Name)
		if name == "" {
			name = child.ThreadID
		}
		views = append(views, map[string]any{"id": child.ThreadID, "name": name, "parentChannelID": child.ParentChannelID})
	}
	return views
}

func (b *Bot) webshareMentionableUsers(guildID string) []map[string]any {
	if b == nil || b.discord == nil || b.discord.State == nil {
		return nil
	}
	g, err := b.discord.State.Guild(guildID)
	if err != nil || g == nil {
		return nil
	}
	users := make([]map[string]any, 0, min(len(g.Members), 100))
	for _, member := range g.Members {
		if member == nil || member.User == nil || member.User.Bot || strings.TrimSpace(member.User.ID) == "" {
			continue
		}
		users = append(users, map[string]any{"id": member.User.ID, "displayName": displayOrDefault(member.User.Username), "username": member.User.Username})
		if len(users) >= 100 {
			break
		}
	}
	return users
}

func (b *Bot) webshareMentionableBot() map[string]any {
	if b == nil || b.discord == nil || b.discord.State == nil || b.discord.State.User == nil {
		return nil
	}
	u := b.discord.State.User
	return map[string]any{"id": u.ID, "displayName": displayOrDefault(u.Username)}
}

func webshareSelectedThread(share webshare.Share) string {
	if share.TargetType == webshare.TargetThread {
		return share.TargetID
	}
	return ""
}

func (b *Bot) websharePrompt(ctx context.Context, share webshare.Share, action webshare.ClientAction, targetID, parentID string) webshare.ServerEvent {
	localPaths, refs, err := b.websharePreparePromptInputs(ctx, share, action, targetID, parentID)
	if err != nil {
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "attachment_scope", Content: err.Error()}
	}
	action.Text = b.stripWebShareBotMention(action.Text)
	if strings.TrimSpace(action.Text) == "" {
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "empty_prompt", Content: L.Get("webshare.empty_prompt")}
	}
	threadTarget := parentID != "" || (share.TargetType == webshare.TargetThread && targetID == share.TargetID)
	sent, attachmentViews, err := b.webshareSendPromptRecord(ctx, share, action, targetID, parentID, refs)
	if err != nil {
		return webshareWebhookFailureEvent("discord_send_failed", err)
	}
	messageID := ""
	if sent != nil {
		messageID = sent.ID
		body, _ := b.websharePromptRecordBody(share, action.Text, refs)
		recordRefs := append([]discordmention.Ref(nil), refs...)
		if ref, ok := b.webshareBotMentionRef(share); ok {
			recordRefs = append(recordRefs, ref)
		}
		record := map[string]any{"eventID": webshareEventID("webshare-prompt", sent.ID), "messageID": sent.ID, "author": map[string]any{"id": share.OpenerUserID, "displayName": webshareDisplayName(share), "username": share.OpenerUsername}, "content": body, "mentions": webshareMentionsFromRefs(recordRefs)}
		if len(attachmentViews) > 0 {
			record["attachments"] = attachmentViews
		}
		b.sendWebShareHostEvent(share.ShareID, webshareMessageEventForTarget(share, targetID, parentID, record))
	}
	selfID := b.webshareSelfID()
	prompt := buildPromptThreadWithMentions(action.Text, localPaths, websharePromptParent(share, targetID, parentID), websharePromptThread(share, targetID, parentID), share.GuildID, share.OpenerUsername, share.OpenerUserID, b.peerPromptContext(selfID), refs)
	final := func(content string) {
		b.recordWebShareAudit(share, "webshare_agent_result", share.OpenerUserID, action.DisplayName, targetID, true, "", map[string]any{"bytes": len(content)})
		if messageID != "" && !threadTarget {
			if err := channel.SendLongReplyWithMentions(b.discord, targetID, messageID, content, refs); err != nil {
				b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, action.DisplayName, targetID, false, "webshare_final_reply_failed", map[string]any{"message_id": messageID, "error": err.Error()})
			}
		}
	}
	if threadTarget {
		if err := b.manager.WebShareEnqueueThread(b.discord, channel.WebSharePrompt{GuildID: share.GuildID, ParentChannelID: websharePromptParent(share, targetID, parentID), ThreadID: targetID, MessageID: messageID, Prompt: prompt, UserID: share.OpenerUserID, Username: share.OpenerUsername, Attachments: localPaths, MentionRefs: refs, FinalReply: final}); err != nil {
			return webshare.ServerEvent{Type: "error", Status: "error", ReasonCode: "enqueue_failed", Content: err.Error()}
		}
	} else {
		if err := b.manager.WebShareEnqueue(b.discord, channel.WebSharePrompt{GuildID: share.GuildID, ChannelID: share.TargetID, MessageID: messageID, Prompt: prompt, UserID: share.OpenerUserID, Username: share.OpenerUsername, Attachments: localPaths, MentionRefs: refs, DeliveryMode: channel.DeliveryInline, FinalReply: final}); err != nil {
			return webshare.ServerEvent{Type: "error", Status: "error", ReasonCode: "enqueue_failed", Content: err.Error()}
		}
	}
	return webshare.ServerEvent{Type: "agent_event", Status: "queued", Event: map[string]any{"eventID": webshareEventID("agent-queued", targetID), "status": "queued"}}
}

func websharePromptParent(share webshare.Share, targetID, parentID string) string {
	if parentID != "" {
		return parentID
	}
	if share.TargetType == webshare.TargetThread {
		return share.ParentChannelID
	}
	return share.TargetID
}

func websharePromptThread(share webshare.Share, targetID, parentID string) string {
	if parentID != "" || share.TargetType == webshare.TargetThread {
		return targetID
	}
	return ""
}

func (b *Bot) websharePreparePromptInputs(ctx context.Context, share webshare.Share, action webshare.ClientAction, targetID, parentID string) ([]string, []discordmention.Ref, error) {
	refs := b.webshareMentionRefs(share, action.AllowedMentions)
	localPaths := make([]string, 0, len(action.Attachments))
	for _, ref := range action.Attachments {
		if strings.TrimSpace(ref.ID) == "" {
			continue
		}
		resolved, err := b.webshareStore.ResolveAttachmentRef(ctx, share.ShareID, ref.ID)
		if err != nil {
			return nil, nil, err
		}
		if resolved.TargetID != targetID && resolved.TargetID != share.TargetID {
			return nil, nil, webshare.ErrScopeMismatch
		}
		if p, _ := resolved.Metadata["local_path"].(string); p != "" {
			cwd, err := b.manager.ValidateCWD(b.manager.TargetCWDPath(targetID, parentID))
			if err != nil {
				return nil, nil, err
			}
			clean, err := filepath.Abs(p)
			if err != nil || !strings.HasPrefix(clean, filepath.Join(cwd, ".kiro-bot", "attachments")+string(os.PathSeparator)) {
				return nil, nil, webshare.ErrScopeMismatch
			}
			localPaths = append(localPaths, clean)
		}
	}
	return localPaths, refs, nil
}

func (b *Bot) webshareMentionRefs(share webshare.Share, sel webshare.AllowedMentionSelection) []discordmention.Ref {
	seen := map[string]bool{}
	var refs []discordmention.Ref
	add := func(id, name string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		refs = append(refs, discordmention.UserRef(id, name))
	}
	if share.Capabilities.MentionUsers {
		for _, id := range sel.Users {
			if u := b.discordUserForMention(share.GuildID, id); u != nil && !u.Bot {
				add(u.ID, u.Username)
			}
		}
	}
	if sel.Here || sel.Everyone || len(sel.Roles) > 0 {
		// v1 intentionally ignores role/everyone/here selections.
	}
	if share.Capabilities.MentionBot && sel.Bot && b != nil && b.discord != nil && b.discord.State != nil && b.discord.State.User != nil {
		add(b.discord.State.User.ID, b.discord.State.User.Username)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return refs
}

func (b *Bot) discordUserForMention(guildID, userID string) *discordgo.User {
	userID = strings.TrimSpace(userID)
	if userID == "" || b == nil || b.discord == nil {
		return nil
	}
	if b.discord.State != nil {
		if m, err := b.discord.State.Member(guildID, userID); err == nil && m != nil && m.User != nil {
			return m.User
		}
	}
	m, err := b.discord.GuildMember(guildID, userID)
	if err == nil && m != nil {
		return m.User
	}
	return nil
}

func (b *Bot) webshareSelfID() string {
	if b != nil && b.discord != nil && b.discord.State != nil && b.discord.State.User != nil {
		return b.discord.State.User.ID
	}
	return ""
}

func (b *Bot) webshareSendPromptRecord(ctx context.Context, share webshare.Share, action webshare.ClientAction, targetID, parentID string, refs []discordmention.Ref) (*discordgo.Message, []map[string]any, error) {
	files, attachmentViews, closeFiles, err := b.webshareDiscordMessageFiles(ctx, share, action, targetID)
	if err != nil {
		return nil, nil, err
	}
	defer closeFiles()
	body, allowed := b.websharePromptRecordBody(share, action.Text, refs)
	sent, err := b.webshareWebhookExecute(share, targetID, parentID, body, allowed, files)
	if err != nil {
		return nil, nil, err
	}
	b.recordWebShareAudit(share, "webshare_agent_prompt", share.OpenerUserID, action.DisplayName, targetID, true, "", map[string]any{"message_id": sent.ID, "mentioned_users": len(allowed.Users), "attachment_count": len(files), "transport": "webhook"})
	return sent, attachmentViews, nil
}

func (b *Bot) websharePromptRecordBody(share webshare.Share, text string, refs []discordmention.Ref) (string, *discordgo.MessageAllowedMentions) {
	recordRefs := append([]discordmention.Ref(nil), refs...)
	target := displayOrDefault("KDB Bot")
	if ref, ok := b.webshareBotMentionRef(share); ok {
		recordRefs = append(recordRefs, ref)
		target = ref.Placeholder
	} else if b != nil && b.discord != nil && b.discord.State != nil && b.discord.State.User != nil {
		target = displayOrDefault(b.discord.State.User.Username)
	}
	body, allowed := discordmention.Render(strings.TrimSpace(target+" "+strings.TrimSpace(text)), recordRefs)
	sanitizeAllowedMentions(allowed)
	return secrets.RedactEnv(body), allowed
}

func (b *Bot) webshareBotMentionRef(share webshare.Share) (discordmention.Ref, bool) {
	if !share.Capabilities.MentionBot || b == nil || b.discord == nil || b.discord.State == nil || b.discord.State.User == nil {
		return discordmention.Ref{}, false
	}
	u := b.discord.State.User
	if strings.TrimSpace(u.ID) == "" {
		return discordmention.Ref{}, false
	}
	return discordmention.UserRef(u.ID, u.Username), true
}

func (b *Bot) stripWebShareBotMention(text string) string {
	text = strings.TrimSpace(text)
	if b == nil || b.discord == nil || b.discord.State == nil || b.discord.State.User == nil {
		return text
	}
	u := b.discord.State.User
	for _, token := range []string{"<@" + u.ID + ">", "<@!" + u.ID + ">", "@" + u.Username, "@" + displayOrDefault(u.Username)} {
		if strings.HasPrefix(strings.ToLower(text), strings.ToLower(strings.TrimSpace(token))) {
			return strings.TrimSpace(text[len(strings.TrimSpace(token)):])
		}
	}
	return text
}

func (b *Bot) websharePostChannelMessage(ctx context.Context, share webshare.Share, action webshare.ClientAction, targetID, parentID string) webshare.ServerEvent {
	refs := b.webshareMentionRefs(share, action.AllowedMentions)
	files, attachmentViews, closeFiles, err := b.webshareDiscordMessageFiles(ctx, share, action, targetID)
	if err != nil {
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "attachment_scope", Content: err.Error()}
	}
	defer closeFiles()
	text := replaceSelectedRawMentions(action.Text, refs)
	body, allowed := discordmention.Render(text, refs)
	sanitizeAllowedMentions(allowed)
	body = secrets.RedactEnv(body)
	sent, err := b.webshareWebhookExecute(share, targetID, parentID, body, allowed, files)
	if err != nil {
		return webshareWebhookFailureEvent("discord_webhook_send_failed", err)
	}
	b.recordWebShareAudit(share, "webshare_channel_message", share.OpenerUserID, action.DisplayName, targetID, true, "", map[string]any{"message_id": sent.ID, "mentioned_users": len(allowed.Users), "attachment_count": len(files), "transport": "webhook"})
	event := map[string]any{"eventID": webshareEventID("channel-message", sent.ID), "messageID": sent.ID, "author": map[string]any{"id": share.OpenerUserID, "displayName": webshareDisplayName(share), "username": share.OpenerUsername}, "content": body, "mentions": webshareMentionsFromRefs(refs)}
	if len(attachmentViews) > 0 {
		event["attachments"] = attachmentViews
	}
	out := webshareMessageEventForTarget(share, targetID, parentID, event)
	out.Metadata = map[string]any{"message_id": sent.ID, "transport": "webhook"}
	b.sendWebShareHostEvent(share.ShareID, out)
	return out
}

func sanitizeAllowedMentions(allowed *discordgo.MessageAllowedMentions) {
	if allowed == nil {
		return
	}
	if len(allowed.Roles) == 0 {
		allowed.Roles = nil
	}
	allowed.Parse = nil
	allowed.RepliedUser = false
}

func webshareMessageEventForTarget(share webshare.Share, targetID, parentID string, event map[string]any) webshare.ServerEvent {
	if strings.TrimSpace(parentID) == "" && !(share.TargetType == webshare.TargetThread && targetID == share.TargetID) {
		return webshare.ServerEvent{Type: "channel_event", Status: "ok", Event: event}
	}
	parent := websharePromptParent(share, targetID, parentID)
	threadEvent := make(map[string]any, len(event)+2)
	for key, value := range event {
		threadEvent[key] = value
	}
	threadEvent["thread"] = map[string]any{"id": targetID, "name": targetID, "parentChannelID": parent}
	threadEvent["action"] = "message"
	return webshare.ServerEvent{Type: "thread_event", Status: "ok", Event: threadEvent}
}

func (b *Bot) webshareWebhookExecute(share webshare.Share, targetID, parentID, content string, allowed *discordgo.MessageAllowedMentions, files []*discordgo.File) (*discordgo.Message, error) {
	if b == nil || b.discord == nil {
		return nil, errors.New("discord session unavailable")
	}
	channelID, threadID := targetID, ""
	if strings.TrimSpace(parentID) != "" {
		channelID = parentID
		threadID = targetID
	} else if share.TargetType == webshare.TargetThread && share.ParentChannelID != "" && targetID == share.TargetID {
		channelID = share.ParentChannelID
		threadID = targetID
	}
	hook, err := b.webshareWebhookForChannel(channelID)
	if err != nil {
		return nil, webshareWebhookError(err)
	}
	params := &discordgo.WebhookParams{Content: content, Username: webshareDisplayName(share), Files: files, AllowedMentions: allowed, Flags: discordgo.MessageFlagsSuppressEmbeds}
	if threadID != "" {
		sent, err := b.discord.WebhookThreadExecute(hook.ID, hook.Token, true, threadID, params)
		return sent, webshareWebhookError(err)
	}
	sent, err := b.discord.WebhookExecute(hook.ID, hook.Token, true, params)
	return sent, webshareWebhookError(err)
}

func (b *Bot) webshareWebhookForChannel(channelID string) (webshareWebhookCredential, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return webshareWebhookCredential{}, errors.New("empty webhook channel")
	}
	b.webshareWebhookMu.Lock()
	defer b.webshareWebhookMu.Unlock()
	if b.webshareWebhookByChannel == nil {
		b.webshareWebhookByChannel = make(map[string]webshareWebhookCredential)
	}
	if b.webshareWebhookIDs == nil {
		b.webshareWebhookIDs = make(map[string]bool)
	}
	if cached := b.webshareWebhookByChannel[channelID]; cached.ID != "" && cached.Token != "" {
		b.webshareWebhookIDs[cached.ID] = true
		return cached, nil
	}
	if err := b.requireWebShareWebhookPermission(channelID); err != nil {
		return webshareWebhookCredential{}, err
	}
	hooks, err := b.discord.ChannelWebhooks(channelID)
	if err != nil {
		return webshareWebhookCredential{}, webshareWebhookError(fmt.Errorf("list channel webhooks: %w", err))
	}
	selfID := b.webshareSelfID()
	for _, hook := range hooks {
		if hook == nil || hook.Name != webshareWebhookName || strings.TrimSpace(hook.ID) == "" || strings.TrimSpace(hook.Token) == "" {
			continue
		}
		if selfID != "" && hook.User != nil && hook.User.ID != "" && hook.User.ID != selfID {
			continue
		}
		cred := webshareWebhookCredential{ID: hook.ID, Token: hook.Token}
		b.webshareWebhookByChannel[channelID] = cred
		b.webshareWebhookIDs[hook.ID] = true
		return cred, nil
	}
	hook, err := b.discord.WebhookCreate(channelID, webshareWebhookName, "")
	if err != nil {
		return webshareWebhookCredential{}, webshareWebhookError(fmt.Errorf("create webshare webhook: %w", err))
	}
	if hook == nil || strings.TrimSpace(hook.ID) == "" || strings.TrimSpace(hook.Token) == "" {
		return webshareWebhookCredential{}, errors.New("created webshare webhook without executable token")
	}
	cred := webshareWebhookCredential{ID: hook.ID, Token: hook.Token}
	b.webshareWebhookByChannel[channelID] = cred
	b.webshareWebhookIDs[hook.ID] = true
	return cred, nil
}

func (b *Bot) requireWebShareWebhookPermission(channelID string) error {
	if b == nil || b.discord == nil {
		return errors.New("discord session unavailable")
	}
	selfID := b.webshareSelfID()
	if strings.TrimSpace(selfID) == "" {
		return errors.New("discord bot identity unavailable")
	}
	perms, err := b.discord.UserChannelPermissions(selfID, channelID)
	if err != nil {
		return webshareWebhookError(fmt.Errorf("check webhook permission: %w", err))
	}
	if perms&discordgo.PermissionManageWebhooks == 0 {
		return errWebShareWebhookPermission
	}
	return nil
}

func webshareWebhookError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errWebShareWebhookPermission) || discordMissingPermission(err) {
		return errWebShareWebhookPermission
	}
	return err
}

func webshareWebhookFailureEvent(fallbackReason string, err error) webshare.ServerEvent {
	if errors.Is(webshareWebhookError(err), errWebShareWebhookPermission) {
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "discord_webhook_permission_missing", Content: L.Get("webshare.webhook_permission_missing")}
	}
	return webshare.ServerEvent{Type: "error", Status: "error", ReasonCode: fallbackReason, Content: commandErrorString(err)}
}

func discordMissingPermission(err error) bool {
	var restErr *discordgo.RESTError
	if !errors.As(err, &restErr) {
		return false
	}
	if restErr.Message != nil && restErr.Message.Code == 50013 {
		return true
	}
	return restErr.Response != nil && restErr.Response.StatusCode == http.StatusForbidden
}

func (b *Bot) isWebShareWebhookMessage(m *discordgo.MessageCreate) bool {
	if b == nil || m == nil || strings.TrimSpace(m.WebhookID) == "" {
		return false
	}
	b.webshareWebhookMu.Lock()
	defer b.webshareWebhookMu.Unlock()
	return b.webshareWebhookIDs[m.WebhookID]
}

func webshareDisplayName(share webshare.Share) string {
	return displayOrDefault(share.OpenerUsername) + " via WebShare"
}

func (b *Bot) webshareDiscordMessageFiles(ctx context.Context, share webshare.Share, action webshare.ClientAction, targetID string) ([]*discordgo.File, []map[string]any, func(), error) {
	var opened []*os.File
	cleanup := func() {
		for _, f := range opened {
			_ = f.Close()
		}
	}
	if len(action.Attachments) == 0 {
		return nil, nil, cleanup, nil
	}
	files := make([]*discordgo.File, 0, len(action.Attachments))
	views := make([]map[string]any, 0, len(action.Attachments))
	for _, actionRef := range action.Attachments {
		refID := strings.TrimSpace(actionRef.ID)
		if refID == "" {
			cleanup()
			return nil, nil, func() {}, webshare.ErrNotFound
		}
		ref, err := b.webshareStore.ResolveAttachmentRef(ctx, share.ShareID, refID)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		if ref.TargetID != targetID && ref.TargetID != share.TargetID {
			cleanup()
			return nil, nil, func() {}, webshare.ErrScopeMismatch
		}
		name := webshare.SafeAttachmentFilename(ref.Filename)
		if name == "" {
			name = "attachment"
		}
		var reader io.Reader
		if p, _ := ref.Metadata["local_path"].(string); p != "" {
			clean, err := b.validatedWebShareLocalAttachmentPath(share, ref, p)
			if err != nil {
				cleanup()
				return nil, nil, func() {}, err
			}
			f, err := os.Open(clean)
			if err != nil {
				cleanup()
				return nil, nil, func() {}, err
			}
			opened = append(opened, f)
			reader = f
		} else {
			data, err := b.fetchDiscordAttachmentBytes(ref)
			if err != nil {
				cleanup()
				return nil, nil, func() {}, err
			}
			reader = bytes.NewReader(data)
		}
		files = append(files, &discordgo.File{Name: name, ContentType: ref.ContentType, Reader: reader})
		view := map[string]any{"attachmentRef": ref.ID, "filename": name, "size": ref.Size}
		if ref.ContentType != "" {
			view["mime"] = ref.ContentType
		}
		views = append(views, view)
	}
	return files, views, cleanup, nil
}

func replaceSelectedRawMentions(text string, refs []discordmention.Ref) string {
	for _, ref := range refs {
		if ref.Kind != "user" || ref.ID == "" {
			continue
		}
		placeholder := fmt.Sprintf("[[discord:user:%s]]", ref.ID)
		text = strings.ReplaceAll(text, "<@"+ref.ID+">", placeholder)
		text = strings.ReplaceAll(text, "<@!"+ref.ID+">", placeholder)
	}
	return text
}

func (b *Bot) webshareRunBotCommand(ctx context.Context, share webshare.Share, action webshare.ClientAction, targetID, parentID string) webshare.ServerEvent {
	cmd := strings.TrimPrefix(strings.TrimSpace(action.Command), "/")
	name, args, _ := strings.Cut(cmd, " ")
	name = strings.ToLower(name)
	if name == "" {
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "command_required", Content: L.Get("webshare.command_required")}
	}
	args = webshareCommandArgs(args, action.Args)
	var replies []string
	ctxCmd := cmdCtx{channelID: websharePromptParent(share, targetID, parentID), targetID: targetID, inThread: parentID != "" || share.TargetType == webshare.TargetThread, guildID: share.GuildID, userID: share.OpenerUserID, username: share.OpenerUsername, reply: func(s string) { replies = append(replies, s) }, replyWithMetadata: func(s string, _ map[string]any) { replies = append(replies, s) }, replyWithComponents: func(s string, _ []discordgo.MessageComponent, _ map[string]any) { replies = append(replies, s) }}
	ctxCmd.args = args
	switch name {
	case "start":
		b.cmdStart(ctxCmd)
	case "cwd":
		b.cmdCwd(ctxCmd)
	case "help":
		b.cmdHelp(ctxCmd)
	case "status":
		b.cmdStatus(ctxCmd)
	case "usage":
		b.cmdUsage(ctxCmd)
	case "doctor":
		b.cmdDoctor(ctxCmd)
	case "audit":
		b.cmdAudit(ctxCmd)
	case "mcp":
		b.cmdMCP(ctxCmd)
	case "skill":
		b.cmdSkill(ctxCmd)
	case "steering":
		b.cmdSteering(ctxCmd)
	case "a2a":
		if !b.a2aConfig.Enabled() {
			replies = append(replies, L.Get("a2a.disabled"))
			break
		}
		b.cmdA2A(ctxCmd)
	case "pause":
		b.cmdPause(ctxCmd)
	case "back":
		b.cmdBack(ctxCmd)
	case "silent":
		b.cmdSilent(ctxCmd)
	case "thread":
		b.cmdThreadMode(ctxCmd)
	case "webhook":
		b.cmdWebhook(ctxCmd)
	case "reset":
		b.cmdReset(ctxCmd)
	case "restart":
		b.cmdRestart(ctxCmd)
	case "cancel":
		b.cmdCancel(ctxCmd)
	case "interrupt":
		b.cmdInterrupt(ctxCmd)
	case "compact":
		b.cmdCompact(ctxCmd)
	case "clear":
		b.cmdClear(ctxCmd)
	case "model":
		b.cmdModel(ctxCmd)
	case "models":
		b.cmdModels(ctxCmd)
	case "agent":
		b.cmdAgent(ctxCmd)
	case "engine":
		b.cmdEngine(ctxCmd)
	case "resume":
		b.cmdResume(ctxCmd)
	case "session":
		b.cmdSession(ctxCmd)
	case "close":
		b.cmdClose(ctxCmd)
	case "close-thread":
		b.cmdCloseThread(ctxCmd)
	case "memory":
		b.cmdMemory(ctxCmd)
	case "flashmemory":
		b.cmdFlashMemory(ctxCmd)
	case "webshare":
		b.cmdWebShare(ctxCmd)
	case "cron-list":
		b.cmdWebShareCronList(ctxCmd)
	case "cron-run":
		b.cmdWebShareCronRun(ctxCmd)
	case "remind":
		b.cmdWebShareRemind(ctxCmd)
	case "usage-history":
		b.cmdWebShareUsageHistory(ctxCmd)
	case "cron", "cron-prompt":
		return webshare.ServerEvent{Type: "command_result", Status: "rejected", ReasonCode: "command_requires_discord_component", Content: L.Get("webshare.command_component_required")}
	default:
		return webshare.ServerEvent{Type: "command_result", Status: "rejected", ReasonCode: "unknown_command", Content: L.Get("webshare.unknown_command")}
	}
	return webshare.ServerEvent{Type: "command_result", Status: "ok", Content: strings.Join(replies, "\n")}
}

func webshareCommandArgs(raw string, values map[string]any) string {
	raw = strings.TrimSpace(raw)
	if raw != "" || len(values) == 0 {
		return raw
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprint(values[key]))
		if value == "" {
			continue
		}
		if key == "args" || key == "value" {
			args = append(args, value)
			continue
		}
		args = append(args, key, value)
	}
	return strings.Join(args, " ")
}

func (b *Bot) webshareCreateThread(ctx context.Context, share webshare.Share, action webshare.ClientAction) webshare.ServerEvent {
	if share.TargetType != webshare.TargetChannel {
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "thread_target_only", Content: L.Get("webshare.thread_create_channel_only")}
	}
	name := strings.TrimSpace(action.Name)
	if name == "" {
		name = L.Get("worker.thread_default")
	}
	if len(name) > webshareMaxThreadNameBytes {
		name = name[:webshareMaxThreadNameBytes]
	}
	archive := action.AutoArchiveDuration
	if archive <= 0 {
		archive = b.manager.ThreadArchive()
	}
	var thread *discordgo.Channel
	var err error
	if sourceMessageID := strings.TrimSpace(action.SourceMessageID); sourceMessageID != "" {
		thread, err = b.discord.MessageThreadStart(share.TargetID, sourceMessageID, name, archive)
	} else {
		thread, err = b.discord.ThreadStart(share.TargetID, name, discordgo.ChannelTypeGuildPublicThread, archive)
	}
	if err != nil {
		return webshare.ServerEvent{Type: "error", Status: "error", ReasonCode: "thread_create_failed", Content: err.Error()}
	}
	registerThreadParent(thread.ID, share.TargetID)
	_ = b.webshareStore.RegisterManagedChildThread(ctx, webshare.ManagedChildThread{ShareID: share.ShareID, ParentChannelID: share.TargetID, ThreadID: thread.ID, Name: thread.Name, CreatedByUserID: share.OpenerUserID, Metadata: map[string]any{"source": "webshare"}})
	return webshare.ServerEvent{Type: "thread_event", Status: "ok", Event: map[string]any{"eventID": webshareEventID("thread-created", thread.ID), "thread": map[string]any{"id": thread.ID, "name": thread.Name, "parentChannelID": share.TargetID, "selected": true}, "action": "created"}, Metadata: map[string]any{"thread_id": thread.ID, "name": thread.Name}}
}

func (b *Bot) webshareInterrupt(share webshare.Share, targetID, parentID string) webshare.ServerEvent {
	var err error
	if parentID != "" || share.TargetType == webshare.TargetThread {
		err = b.manager.InterruptThreadAgent(targetID)
	} else {
		err = b.manager.Interrupt(share.TargetID)
	}
	if err != nil {
		return webshare.ServerEvent{Type: "error", Status: "error", ReasonCode: "interrupt_failed", Content: err.Error()}
	}
	return webshare.ServerEvent{Type: "agent_event", Status: "ok", Event: map[string]any{"eventID": webshareEventID("agent-interrupted", targetID), "status": "interrupted"}}
}

func (b *Bot) webshareUploadInit(ctx context.Context, share webshare.Share, action webshare.ClientAction, targetID, parentID string) webshare.ServerEvent {
	if action.Bytes != "" {
		return b.webshareUploadOneShot(ctx, share, action, targetID, parentID)
	}
	if b.attachmentMaxBytes > 0 && action.Size > b.attachmentMaxBytes {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: action.UploadID, ReasonCode: "attachment_too_large", Reason: L.Get("webshare.attachment_too_large")}
	}
	cwd, err := b.manager.ValidateCWD(b.manager.TargetCWDPath(targetID, parentID))
	if err != nil {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: action.UploadID, ReasonCode: "cwd_invalid", Reason: err.Error()}
	}
	uploadID := strings.TrimSpace(action.UploadID)
	if uploadID == "" {
		uploadID = webshareEventID("up", share.ShareID)
	}
	filename := webshare.SafeAttachmentFilename(action.Name)
	dir := webshare.UploadDir(cwd, share.ShareID, webshare.SafeAttachmentFilename(uploadID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "upload_write_failed", Reason: err.Error()}
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, nil, 0644); err != nil {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "upload_write_failed", Reason: err.Error()}
	}
	b.webshareUploadMu.Lock()
	if b.webshareUploads == nil {
		b.webshareUploads = make(map[string]*webshareUploadSession)
	}
	b.webshareUploads[webshareUploadKey(share.ShareID, uploadID)] = &webshareUploadSession{shareID: share.ShareID, targetID: targetID, parentID: parentID, uploadID: uploadID, path: path, filename: filename, mime: action.MIME, size: action.Size, sha256: action.SHA256, hash: sha256.New()}
	b.webshareUploadMu.Unlock()
	return webshare.ServerEvent{Type: "upload_state", Status: "accepted", UploadID: uploadID}
}

func (b *Bot) webshareUploadChunk(share webshare.Share, action webshare.ClientAction) webshare.ServerEvent {
	uploadID := strings.TrimSpace(action.UploadID)
	b.webshareUploadMu.Lock()
	session := b.webshareUploads[webshareUploadKey(share.ShareID, uploadID)]
	b.webshareUploadMu.Unlock()
	if session == nil {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "upload_not_found", Reason: webshare.ErrNotFound.Error()}
	}
	if action.Seq != session.nextSeq {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "upload_sequence", Reason: "unexpected upload chunk sequence"}
	}
	raw, err := decodeWebShareBytes(action.Bytes)
	if err != nil {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "bad_upload", Reason: err.Error()}
	}
	if b.attachmentMaxBytes > 0 && session.received+int64(len(raw)) > b.attachmentMaxBytes {
		b.clearWebShareUpload(session)
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "attachment_too_large", Reason: L.Get("webshare.attachment_too_large")}
	}
	f, err := os.OpenFile(session.path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "upload_write_failed", Reason: err.Error()}
	}
	_, writeErr := f.Write(raw)
	closeErr := f.Close()
	if writeErr != nil {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "upload_write_failed", Reason: writeErr.Error()}
	}
	if closeErr != nil {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "upload_write_failed", Reason: closeErr.Error()}
	}
	_, _ = session.hash.Write(raw)
	session.received += int64(len(raw))
	session.nextSeq++
	return webshare.ServerEvent{Type: "upload_state", Status: "received", UploadID: uploadID}
}

func (b *Bot) webshareUploadFinish(ctx context.Context, share webshare.Share, action webshare.ClientAction) webshare.ServerEvent {
	uploadID := strings.TrimSpace(action.UploadID)
	b.webshareUploadMu.Lock()
	session := b.webshareUploads[webshareUploadKey(share.ShareID, uploadID)]
	if session != nil {
		delete(b.webshareUploads, webshareUploadKey(share.ShareID, uploadID))
	}
	b.webshareUploadMu.Unlock()
	if session == nil {
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "upload_not_found", Reason: webshare.ErrNotFound.Error()}
	}
	if session.size > 0 && session.received != session.size {
		b.clearWebShareUpload(session)
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "upload_size_mismatch", Reason: "uploaded byte count mismatch"}
	}
	if session.sha256 != "" && !webshareDigestMatches(session.hash.Sum(nil), session.sha256) {
		b.clearWebShareUpload(session)
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "upload_hash_mismatch", Reason: "uploaded file digest mismatch"}
	}
	ref, err := b.webshareStore.IssueAttachmentRef(ctx, webshare.AttachmentRef{ID: uploadID, ShareID: share.ShareID, TargetID: session.targetID, MessageID: "web-upload-" + uploadID, AttachmentID: uploadID, Filename: session.filename, Size: session.received, ContentType: session.mime, Metadata: map[string]any{"local_path": session.path, "source": "webshare_upload"}})
	if err != nil {
		b.clearWebShareUpload(session)
		return webshare.ServerEvent{Type: "upload_state", Status: "rejected", UploadID: uploadID, ReasonCode: "attachment_ref_failed", Reason: err.Error()}
	}
	b.recordWebShareAudit(share, webshare.EventUploadCompleted, share.OpenerUserID, action.DisplayName, session.targetID, true, "", map[string]any{"attachment_ref": ref.ID, "bytes": session.received, "filename": session.filename})
	return webshare.ServerEvent{Type: "upload_state", Status: "complete", UploadID: uploadID, Metadata: map[string]any{"attachmentRef": ref.ID, "filename": ref.Filename, "size": ref.Size, "mime": ref.ContentType}}
}

func (b *Bot) webshareUploadOneShot(ctx context.Context, share webshare.Share, action webshare.ClientAction, targetID, parentID string) webshare.ServerEvent {
	init := b.webshareUploadInit(ctx, share, webshare.ClientAction{Name: action.Name, MIME: action.MIME, Size: action.Size, SHA256: action.SHA256}, targetID, parentID)
	if init.Status != "accepted" {
		return init
	}
	chunk := b.webshareUploadChunk(share, webshare.ClientAction{UploadID: init.UploadID, Bytes: action.Bytes})
	if chunk.Status != "received" {
		return chunk
	}
	return b.webshareUploadFinish(ctx, share, webshare.ClientAction{UploadID: init.UploadID, DisplayName: action.DisplayName})
}

func (b *Bot) webshareFetchAttachment(ctx context.Context, share webshare.Share, action webshare.ClientAction) webshare.ServerEvent {
	ref, err := b.webshareStore.ResolveAttachmentRef(ctx, share.ShareID, action.AttachmentRef)
	if err != nil {
		return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "attachment_ref_invalid", Content: err.Error()}
	}
	var data []byte
	if p, _ := ref.Metadata["local_path"].(string); p != "" {
		clean, err := b.validatedWebShareLocalAttachmentPath(share, ref, p)
		if err != nil {
			return webshare.ServerEvent{Type: "error", Status: "rejected", ReasonCode: "attachment_scope", Content: err.Error()}
		}
		data, err = os.ReadFile(clean)
	} else {
		data, err = b.fetchDiscordAttachmentBytes(ref)
	}
	if err != nil {
		return webshare.ServerEvent{Type: "error", Status: "error", ReasonCode: "attachment_fetch_failed", Content: err.Error()}
	}
	streamID := webshareEventID("att", ref.ID)
	b.recordWebShareAudit(share, webshare.EventAttachmentFetched, share.OpenerUserID, action.DisplayName, ref.TargetID, true, "", map[string]any{"attachment_ref": ref.ID, "bytes": len(data), "filename": ref.Filename})
	return webshare.ServerEvent{Type: "attachment_stream", Status: "ok", StreamID: streamID, Chunk: base64.StdEncoding.EncodeToString(data), Done: true, Metadata: map[string]any{"attachmentRef": ref.ID, "filename": ref.Filename, "size": len(data), "mime": ref.ContentType}}
}

func webshareUploadKey(shareID, uploadID string) string {
	return shareID + ":" + uploadID
}

func (b *Bot) clearWebShareUpload(session *webshareUploadSession) {
	if session == nil {
		return
	}
	b.webshareUploadMu.Lock()
	delete(b.webshareUploads, webshareUploadKey(session.shareID, session.uploadID))
	b.webshareUploadMu.Unlock()
	_ = os.Remove(session.path)
}

func decodeWebShareBytes(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(raw); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(raw)
}

func webshareDigestMatches(sum []byte, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	return expected == base64.RawURLEncoding.EncodeToString(sum) || strings.EqualFold(expected, hex.EncodeToString(sum))
}

func webshareEventID(prefix, seed string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", prefix, seed, time.Now().UnixNano())))
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(digest[:])[:18]
}

func (b *Bot) validatedWebShareLocalAttachmentPath(share webshare.Share, ref webshare.AttachmentRef, p string) (string, error) {
	parentID := share.ParentChannelID
	if share.TargetType == webshare.TargetChannel && ref.TargetID != share.TargetID {
		child, err := b.webshareStore.ResolveManagedChildThread(context.Background(), share.ShareID, ref.TargetID)
		if err != nil {
			return "", err
		}
		parentID = child.ParentChannelID
	}
	cwd, err := b.manager.ValidateCWD(b.manager.TargetCWDPath(ref.TargetID, parentID))
	if err != nil {
		return "", err
	}
	clean, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	root := filepath.Join(cwd, ".kiro-bot", "attachments") + string(os.PathSeparator)
	if !strings.HasPrefix(clean, root) {
		return "", webshare.ErrScopeMismatch
	}
	return clean, nil
}

func (b *Bot) fetchDiscordAttachmentBytes(ref webshare.AttachmentRef) ([]byte, error) {
	msg, err := b.discord.ChannelMessage(ref.TargetID, ref.MessageID)
	if err != nil {
		return nil, err
	}
	var url string
	for _, att := range msg.Attachments {
		if att != nil && att.ID == ref.AttachmentID {
			url = att.URL
			break
		}
	}
	if url == "" {
		return nil, webshare.ErrNotFound
	}
	resp, err := b.downloadClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	reader := io.Reader(resp.Body)
	if b.attachmentMaxBytes > 0 {
		reader = io.LimitReader(resp.Body, b.attachmentMaxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if b.attachmentMaxBytes > 0 && int64(len(data)) > b.attachmentMaxBytes {
		return nil, fmt.Errorf("attachment exceeds max size")
	}
	return data, nil
}
