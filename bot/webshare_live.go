package bot

import (
	"context"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	"github.com/nczz/kiro-discord-bot/webshare"
)

func (b *Bot) broadcastWebShareDiscordMessage(ctx context.Context, m *discordgo.MessageCreate, parentChannelID string) {
	if b == nil || b.webshareStore == nil || m == nil || m.Message == nil || m.Author == nil {
		return
	}
	shares, err := b.webshareStore.ListActive(ctx)
	if err != nil || len(shares) == 0 {
		return
	}
	for _, share := range shares {
		if share.GuildID != "" && m.GuildID != "" && share.GuildID != m.GuildID {
			continue
		}
		if !webshareShareCoversTarget(b.webshareStore, share, m.ChannelID, parentChannelID) {
			continue
		}
		loop := b.webshareHostLoop(share.ShareID)
		if loop == nil {
			continue
		}
		event := b.webshareDiscordMessageEvent(ctx, share, m, parentChannelID)
		select {
		case loop.send <- event:
		default:
			b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, m.ChannelID, false, "webshare_event_backpressure", map[string]any{"message_id": m.ID})
		}
	}
}

func (b *Bot) webshareHostLoop(shareID string) *webshareHostLoop {
	b.webshareMu.Lock()
	defer b.webshareMu.Unlock()
	return b.webshareHosts[shareID]
}

func (b *Bot) sendWebShareHostEvent(shareID string, event webshare.ServerEvent) bool {
	loop := b.webshareHostLoop(shareID)
	if loop == nil {
		return false
	}
	select {
	case loop.send <- event:
		return true
	default:
		return false
	}
}

func (b *Bot) webshareDiscordMessageEvent(ctx context.Context, share webshare.Share, m *discordgo.MessageCreate, parentChannelID string) webshare.ServerEvent {
	attachments := b.webshareDiscordAttachmentRefs(ctx, share, m)
	author := map[string]any{"id": m.Author.ID, "displayName": displayOrDefault(m.Author.Username), "username": m.Author.Username}
	base := map[string]any{"eventID": webshareEventID("discord-message", m.ID), "timestamp": webshareDiscordTimestamp(m.Message), "messageID": m.ID, "action": "created", "author": author, "content": secrets.RedactEnv(m.Content), "attachments": attachments, "mentionableUsers": mentionableUsersFromMessage(m), "mentionableBot": b.webshareMentionableBot(), "mentions": discordMentionsFromMessage(m)}
	if replyTo := discordReplyReferenceFromMessage(m.Message); replyTo != nil {
		base["replyTo"] = replyTo
	}
	parentID := webshareEventParent(share, m.ChannelID, parentChannelID)
	if parentID == "" {
		return webshare.ServerEvent{Type: "channel_event", Status: "ok", Event: base}
	}
	base["thread"] = map[string]any{"id": m.ChannelID, "name": m.ChannelID, "parentChannelID": parentID}
	base["action"] = "message"
	return webshare.ServerEvent{Type: "thread_event", Status: "ok", Event: base}
}

func (b *Bot) webshareDiscordAttachmentRefs(ctx context.Context, share webshare.Share, m *discordgo.MessageCreate) []map[string]any {
	if len(m.Attachments) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(m.Attachments))
	for _, att := range m.Attachments {
		if att == nil || strings.TrimSpace(att.ID) == "" {
			continue
		}
		ref, err := b.webshareStore.IssueAttachmentRef(ctx, webshare.AttachmentRef{ShareID: share.ShareID, TargetID: m.ChannelID, MessageID: m.ID, AttachmentID: att.ID, Filename: att.Filename, Size: int64(att.Size), ContentType: att.ContentType, Metadata: map[string]any{"source": "discord_attachment"}})
		if err != nil {
			continue
		}
		out = append(out, map[string]any{"attachmentRef": ref.ID, "filename": ref.Filename, "size": ref.Size, "mime": ref.ContentType})
	}
	return out
}

func (b *Bot) broadcastWebShareDiscordMessageUpdate(ctx context.Context, m *discordgo.MessageUpdate, parentChannelID string) {
	if b == nil || b.webshareStore == nil || m == nil || m.Message == nil || strings.TrimSpace(m.ID) == "" || !webshareMessageUpdateHasRenderableChange(m) {
		return
	}
	b.broadcastWebShareDiscordEvent(ctx, m.GuildID, m.ChannelID, parentChannelID, m.ID, func(share webshare.Share) webshare.ServerEvent {
		base := map[string]any{"eventID": webshareEventID("discord-message-updated", m.ID), "timestamp": webshareDiscordTimestamp(m.Message), "messageID": m.ID, "action": "updated", "content": secrets.RedactEnv(m.Content)}
		if m.Author != nil {
			base["author"] = map[string]any{"id": m.Author.ID, "displayName": displayOrDefault(m.Author.Username), "username": m.Author.Username}
		}
		if m.Attachments != nil {
			base["attachments"] = []map[string]any{}
			if attachments := b.webshareDiscordAttachmentRefs(ctx, share, &discordgo.MessageCreate{Message: m.Message}); len(attachments) > 0 {
				base["attachments"] = attachments
			}
		}
		if m.Mentions != nil {
			base["mentions"] = discordMentionsFromUsers(m.Mentions)
		}
		if replyTo := discordReplyReferenceFromMessage(m.Message); replyTo != nil {
			base["replyTo"] = replyTo
		}
		parentID := webshareEventParent(share, m.ChannelID, parentChannelID)
		if parentID == "" {
			return webshare.ServerEvent{Type: "channel_event", Status: "ok", Event: base}
		}
		base["thread"] = map[string]any{"id": m.ChannelID, "name": m.ChannelID, "parentChannelID": parentID}
		base["action"] = "updated"
		return webshare.ServerEvent{Type: "thread_event", Status: "ok", Event: base}
	})
}

func webshareMessageUpdateHasRenderableChange(m *discordgo.MessageUpdate) bool {
	if m == nil || m.Message == nil {
		return false
	}
	return strings.TrimSpace(m.Content) != "" || m.Attachments != nil || m.Mentions != nil
}

func (b *Bot) broadcastWebShareDiscordMessageDelete(ctx context.Context, m *discordgo.MessageDelete, parentChannelID string) {
	if b == nil || b.webshareStore == nil || m == nil || m.Message == nil || strings.TrimSpace(m.ID) == "" {
		return
	}
	b.broadcastWebShareDiscordEvent(ctx, m.GuildID, m.ChannelID, parentChannelID, m.ID, func(share webshare.Share) webshare.ServerEvent {
		base := map[string]any{"eventID": webshareEventID("discord-message-deleted", m.ID), "timestamp": webshareDiscordTimestamp(m.Message), "messageID": m.ID, "action": "deleted"}
		parentID := webshareEventParent(share, m.ChannelID, parentChannelID)
		if parentID == "" {
			return webshare.ServerEvent{Type: "channel_event", Status: "ok", Event: base}
		}
		base["thread"] = map[string]any{"id": m.ChannelID, "name": m.ChannelID, "parentChannelID": parentID}
		base["action"] = "deleted"
		return webshare.ServerEvent{Type: "thread_event", Status: "ok", Event: base}
	})
}

func (b *Bot) broadcastWebShareDiscordEvent(ctx context.Context, guildID, channelID, parentChannelID, messageID string, build func(webshare.Share) webshare.ServerEvent) {
	shares, err := b.webshareStore.ListActive(ctx)
	if err != nil || len(shares) == 0 {
		return
	}
	for _, share := range shares {
		if share.GuildID != "" && guildID != "" && share.GuildID != guildID {
			continue
		}
		if !webshareShareCoversTarget(b.webshareStore, share, channelID, parentChannelID) {
			continue
		}
		loop := b.webshareHostLoop(share.ShareID)
		if loop == nil {
			continue
		}
		event := build(share)
		select {
		case loop.send <- event:
		default:
			b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, channelID, false, "webshare_event_backpressure", map[string]any{"message_id": messageID})
		}
	}
}

func (b *Bot) broadcastWebShareThreadLifecycle(ctx context.Context, ch *discordgo.Channel, action string) {
	if b == nil || b.webshareStore == nil || ch == nil || strings.TrimSpace(ch.ID) == "" || strings.TrimSpace(ch.ParentID) == "" {
		return
	}
	shares, err := b.webshareStore.ListActive(ctx)
	if err != nil || len(shares) == 0 {
		return
	}
	thread := webshareThreadView(ch)
	for _, share := range shares {
		if share.GuildID != "" && ch.GuildID != "" && share.GuildID != ch.GuildID {
			continue
		}
		if !webshareShareCoversThreadLifecycle(share, ch) {
			continue
		}
		if share.TargetType == webshare.TargetChannel {
			if action == "deleted" {
				_ = b.webshareStore.UnregisterManagedChildThread(ctx, share.ShareID, ch.ID)
			} else {
				_ = b.webshareStore.RegisterManagedChildThread(ctx, webshare.ManagedChildThread{ShareID: share.ShareID, ParentChannelID: ch.ParentID, ThreadID: ch.ID, Name: ch.Name, CreatedAt: time.Now().UTC(), Metadata: map[string]any{"source": "discord_thread_event"}})
			}
		}
		loop := b.webshareHostLoop(share.ShareID)
		if loop == nil {
			continue
		}
		event := webshare.ServerEvent{Type: "thread_event", Status: "ok", Event: map[string]any{"eventID": webshareEventID("thread-"+action, ch.ID), "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "thread": thread, "action": action}}
		select {
		case loop.send <- event:
		default:
			b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, ch.ID, false, "webshare_event_backpressure", map[string]any{"thread_id": ch.ID, "action": action})
		}
	}
}

func webshareShareCoversThreadLifecycle(share webshare.Share, ch *discordgo.Channel) bool {
	if ch == nil {
		return false
	}
	if share.TargetType == webshare.TargetThread {
		return share.TargetID == ch.ID
	}
	return share.TargetType == webshare.TargetChannel && share.TargetID == ch.ParentID
}

func webshareEventParent(share webshare.Share, channelID, parentChannelID string) string {
	parentID := strings.TrimSpace(parentChannelID)
	if parentID != "" {
		return parentID
	}
	if share.TargetType == webshare.TargetThread && strings.TrimSpace(channelID) == share.TargetID {
		return share.ParentChannelID
	}
	return ""
}

func webshareDiscordTimestamp(m *discordgo.Message) string {
	if m == nil {
		return ""
	}
	if m.Timestamp.IsZero() {
		return ""
	}
	return m.Timestamp.UTC().Format(time.RFC3339Nano)
}

func webshareThreadView(ch *discordgo.Channel) map[string]any {
	name := strings.TrimSpace(ch.Name)
	if name == "" {
		name = ch.ID
	}
	view := map[string]any{"id": ch.ID, "name": name, "parentChannelID": ch.ParentID}
	if ch.ThreadMetadata != nil {
		view["archived"] = ch.ThreadMetadata.Archived
	}
	return view
}
func mentionableUsersFromMessage(m *discordgo.MessageCreate) []map[string]any {
	seen := make(map[string]bool)
	out := make([]map[string]any, 0, len(m.Mentions)+1)
	add := func(u *discordgo.User) {
		if u == nil || u.Bot || strings.TrimSpace(u.ID) == "" || seen[u.ID] {
			return
		}
		seen[u.ID] = true
		out = append(out, map[string]any{"id": u.ID, "displayName": displayOrDefault(u.Username), "username": u.Username})
	}
	add(m.Author)
	for _, u := range m.Mentions {
		add(u)
	}
	return out
}

func discordMentionsFromMessage(m *discordgo.MessageCreate) []map[string]any {
	if m == nil {
		return nil
	}
	return discordMentionsFromUsers(m.Mentions)
}

func discordMentionsFromUsers(users []*discordgo.User) []map[string]any {
	seen := make(map[string]bool)
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		if u == nil || strings.TrimSpace(u.ID) == "" || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		kind := "user"
		if u.Bot {
			kind = "bot"
		}
		out = append(out, map[string]any{"id": u.ID, "displayName": displayOrDefault(u.Username), "username": u.Username, "bot": u.Bot, "kind": kind})
	}
	return out
}

func discordReplyReferenceFromMessage(m *discordgo.Message) map[string]any {
	if m == nil || (m.MessageReference == nil && m.ReferencedMessage == nil) {
		return nil
	}
	ref := m.MessageReference
	out := map[string]any{}
	if ref != nil {
		if strings.TrimSpace(ref.MessageID) != "" {
			out["messageID"] = ref.MessageID
		}
		if strings.TrimSpace(ref.ChannelID) != "" {
			out["channelID"] = ref.ChannelID
		}
		if strings.TrimSpace(ref.GuildID) != "" {
			out["guildID"] = ref.GuildID
		}
	}
	if referenced := m.ReferencedMessage; referenced != nil {
		if strings.TrimSpace(referenced.ID) != "" {
			out["messageID"] = referenced.ID
		}
		if strings.TrimSpace(referenced.ChannelID) != "" {
			out["channelID"] = referenced.ChannelID
		}
		if strings.TrimSpace(referenced.GuildID) != "" {
			out["guildID"] = referenced.GuildID
		}
		if referenced.Author != nil {
			out["author"] = map[string]any{"id": referenced.Author.ID, "displayName": displayOrDefault(referenced.Author.Username), "username": referenced.Author.Username}
		}
		out["content"] = secrets.RedactEnv(referenced.Content)
	}
	if _, ok := out["messageID"]; !ok {
		return nil
	}
	return out
}

func webshareMentionsFromRefs(refs []discordmention.Ref) []map[string]any {
	out := make([]map[string]any, 0, len(refs))
	seen := make(map[string]bool)
	for _, ref := range refs {
		if strings.TrimSpace(ref.ID) == "" || seen[ref.Kind+":"+ref.ID] {
			continue
		}
		seen[ref.Kind+":"+ref.ID] = true
		name := strings.TrimSpace(ref.DisplayName)
		if name == "" {
			name = ref.ID
		}
		kind := ref.Kind
		item := map[string]any{"id": ref.ID, "displayName": name, "kind": kind}
		if ref.Kind == "user" {
			item["bot"] = false
		}
		out = append(out, item)
	}
	return out
}
