package bot

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/textutil"
	L "github.com/nczz/kiro-discord-bot/locale"
)

const usageHistoryCustomPrefix = "usageh"

type usageHistoryCursor struct{ Time, ID string }
type usageHistoryState struct {
	mu                                                     sync.Mutex
	RequesterID, TargetID, GuildID, Period, Status, Source string
	From, To                                               time.Time
	Cursors                                                []usageHistoryCursor
	Page                                                   int
	Expires                                                time.Time
}

var usageHistoryStates sync.Map

func pruneUsageHistoryStates(now time.Time) {
	usageHistoryStates.Range(func(key, value any) bool {
		state, ok := value.(*usageHistoryState)
		if !ok {
			usageHistoryStates.Delete(key)
			return true
		}
		state.mu.Lock()
		expired := now.After(state.Expires)
		state.mu.Unlock()
		if expired {
			usageHistoryStates.Delete(key)
		}
		return true
	})
}

func (b *Bot) handleUsageHistory(ds *discordgo.Session, i *discordgo.InteractionCreate, auditCtx cmdCtx) (string, string) {
	pruneUsageHistoryStates(time.Now())
	data := i.ApplicationCommandData()
	requester, _ := interactionUser(i)
	target, period, status, source := requester, "30d", "all", "all"
	for _, opt := range data.Options {
		switch opt.Name {
		case "user":
			if u := opt.UserValue(ds); u != nil {
				target = u.ID
			}
		case "period":
			period = opt.StringValue()
		case "status":
			status = opt.StringValue()
		case "source":
			source = opt.StringValue()
		}
	}
	if target != requester && !b.userCanManageUsageGuild(ds, requester, i.ChannelID) {
		msg := L.Get("usage.history.forbidden")
		err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: msg, Flags: discordgo.MessageFlagsEphemeral, AllowedMentions: &discordgo.MessageAllowedMentions{}}})
		b.recordInteractionResponseDelivery(auditCtx, data.Name, "rejected", msg, discordgo.InteractionResponseChannelMessageWithSource, map[string]any{"ephemeral": true}, err)
		return "rejected", "usage_history_forbidden"
	}
	err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}})
	b.recordInteractionResponseDelivery(auditCtx, data.Name, "deferred", "", discordgo.InteractionResponseDeferredChannelMessageWithSource, map[string]any{"ephemeral": true}, err)
	if err != nil {
		return "error", "usage_history_deferred_response_failed"
	}
	now := time.Now().In(b.manager.UsageLocation())
	from := now.AddDate(0, 0, -30)
	switch period {
	case "7d":
		from = now.AddDate(0, 0, -7)
	case "this-month":
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	case "last-month":
		end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		from = end.AddDate(0, -1, 0)
		now = end.Add(-time.Nanosecond)
	}
	token := uuid.NewString()
	state := &usageHistoryState{RequesterID: requester, TargetID: target, GuildID: i.GuildID, Period: period, Status: status, Source: source, From: from, To: now, Cursors: []usageHistoryCursor{{}}, Expires: time.Now().Add(15 * time.Minute)}
	content, components, err := b.renderUsageHistory(state, token)
	if err != nil {
		content := commandError(err)
		sent, editErr := ds.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content, AllowedMentions: &discordgo.MessageAllowedMentions{}})
		b.recordCommandResponseDelivery(auditCtx, data.Name, "slash", "error", content, map[string]any{"ephemeral": true, "target_user_id": target, "period": period}, sent, editErr)
		return "error", "usage_history_query_failed"
	}
	usageHistoryStates.Store(token, state)
	sent, err := ds.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content, Components: &components, AllowedMentions: &discordgo.MessageAllowedMentions{}})
	b.recordCommandResponseDelivery(auditCtx, data.Name, "slash", "sent", content, map[string]any{"ephemeral": true, "target_user_id": target, "period": period, "page": 1}, sent, err)
	if err != nil {
		return "error", "usage_history_response_failed"
	}
	return "completed", ""
}

func (b *Bot) renderUsageHistory(state *usageHistoryState, token string) (string, []discordgo.MessageComponent, error) {
	cursor := state.Cursors[state.Page]
	page, err := b.manager.UsageHistory(channel.UsageHistoryOptions{GuildID: state.GuildID, UserID: state.TargetID, From: state.From, To: state.To, Status: state.Status, Source: state.Source, Limit: 8, BeforeTime: cursor.Time, BeforeID: cursor.ID})
	if err != nil {
		return "", nil, err
	}
	var sb strings.Builder
	sb.WriteString(L.Getf("usage.history.title", state.TargetID, state.Period) + fmt.Sprintf(" · %d\n", state.Page+1))
	if len(page.Records) == 0 {
		sb.WriteString(L.Get("usage.history.empty"))
	} else {
		for _, rec := range page.Records {
			displayTime := rec.Timestamp
			if parsed, e := time.Parse(time.RFC3339Nano, rec.Timestamp); e == nil {
				displayTime = parsed.In(b.manager.UsageLocation()).Format("2006-01-02 15:04")
			}
			name := strings.TrimSpace(rec.Engine)
			if rec.Model != "" {
				if name != "" {
					name += "/"
				}
				name += rec.Model
			}
			if name == "" {
				name = "agent"
			}
			name = textutil.TruncateUTF8Bytes(name, 100)
			status := textutil.TruncateUTF8Bytes(rec.Status, 30)
			source := textutil.TruncateUTF8Bytes(rec.Source, 50)
			sb.WriteString(fmt.Sprintf("`%s` · **%s** · %s\n%.2f credits / $%.4f · %.1fs · %s\n", displayTime, name, status, rec.Credits, rec.CostUSD, float64(rec.DurationMs)/1000, source))
		}
	}
	if page.NextTime != "" {
		next := usageHistoryCursor{page.NextTime, page.NextID}
		if len(state.Cursors) == state.Page+1 {
			state.Cursors = append(state.Cursors, next)
		} else {
			state.Cursors[state.Page+1] = next
		}
	}
	buttons := []discordgo.MessageComponent{discordgo.Button{Label: L.Get("usage.history.previous"), Style: discordgo.SecondaryButton, CustomID: usageHistoryCustomPrefix + ":" + token + ":prev", Disabled: state.Page == 0}, discordgo.Button{Label: L.Get("usage.history.next"), Style: discordgo.PrimaryButton, CustomID: usageHistoryCustomPrefix + ":" + token + ":next", Disabled: page.NextTime == ""}, discordgo.Button{Label: L.Get("usage.history.close"), Style: discordgo.SecondaryButton, CustomID: usageHistoryCustomPrefix + ":" + token + ":close"}}
	return sb.String(), []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}, nil
}

func (b *Bot) handleUsageHistoryComponent(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	parts := strings.Split(i.MessageComponentData().CustomID, ":")
	if len(parts) != 3 {
		return
	}
	token, action := parts[1], parts[2]
	value, ok := usageHistoryStates.Load(token)
	if !ok {
		respondInteractionEphemeral(ds, i, L.Get("usage.history.expired"))
		return
	}
	state := value.(*usageHistoryState)
	requester, _ := interactionUser(i)
	if requester != state.RequesterID {
		respondInteractionEphemeral(ds, i, L.Get("usage.history.not_owner"))
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if time.Now().After(state.Expires) {
		usageHistoryStates.Delete(token)
		respondInteractionEphemeral(ds, i, L.Get("usage.history.expired"))
		return
	}
	auditCtx := ctxForAudit(i.ChannelID, i.ChannelID, false, i.GuildID, requester, "")
	auditCtx.interactionID = i.ID
	b.recordCommandInvoked(auditCtx, "usage-history-page", "component", "", i.ID)
	defer b.recordCommandCompleted(auditCtx, "usage-history-page", "component", "completed", "")
	if action == "close" {
		usageHistoryStates.Delete(token)
		empty := []discordgo.MessageComponent{}
		content := L.Get("usage.history.closed")
		err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseUpdateMessage, Data: &discordgo.InteractionResponseData{Content: content, Components: empty, AllowedMentions: &discordgo.MessageAllowedMentions{}}})
		b.recordInteractionResponseDelivery(auditCtx, "usage-history-page", "closed", content, discordgo.InteractionResponseUpdateMessage, map[string]any{"ephemeral": true, "page": state.Page + 1}, err)
		return
	}
	if action == "next" && state.Page+1 < len(state.Cursors) {
		state.Page++
	} else if action == "prev" && state.Page > 0 {
		state.Page--
	}
	content, components, err := b.renderUsageHistory(state, token)
	if err != nil {
		respondInteractionEphemeral(ds, i, commandError(err))
		return
	}
	state.Expires = time.Now().Add(15 * time.Minute)
	err = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseUpdateMessage, Data: &discordgo.InteractionResponseData{Content: content, Components: components, AllowedMentions: &discordgo.MessageAllowedMentions{}}})
	b.recordInteractionResponseDelivery(auditCtx, "usage-history-page", "sent", content, discordgo.InteractionResponseUpdateMessage, map[string]any{"ephemeral": true, "page": state.Page + 1}, err)
}

func stringPtr(value string) *string { return &value }
