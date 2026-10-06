package bot

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/textutil"
	"github.com/nczz/kiro-discord-bot/internal/usagerange"
	L "github.com/nczz/kiro-discord-bot/locale"
)

const usageHistoryCustomPrefix = "usageh"

type usageHistoryCursor struct{ Time, ID string }
type usageHistoryState struct {
	mu                                                                     sync.Mutex
	RequesterID, TargetID, TargetUsername, GuildID, Period, Status, Source string
	AllowLegacyUsername                                                    bool
	From, To                                                               time.Time
	Cursors                                                                []usageHistoryCursor
	Page                                                                   int
	Expires                                                                time.Time
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
	requester, requesterUsername := interactionUser(i)
	target, targetUsername, period, status, source, fromText, toText := requester, requesterUsername, "30d", "all", "all", "", ""
	targetSpecified := false
	exportCSV := false
	for _, opt := range data.Options {
		switch opt.Name {
		case "user":
			targetSpecified = true
			if u := opt.UserValue(ds); u != nil {
				target = u.ID
				targetUsername = u.Username
			}
		case "period":
			period = opt.StringValue()
		case "status":
			status = opt.StringValue()
		case "source":
			source = opt.StringValue()
		case "from":
			fromText = opt.StringValue()
		case "to":
			toText = opt.StringValue()
		case "export":
			exportCSV = opt.BoolValue()
		}
	}
	canManageUsage := b.userCanManageUsageGuild(ds, requester, i.ChannelID)
	target, targetUsername, allowed := usageHistoryEffectiveTarget(requester, requesterUsername, targetSpecified, target, targetUsername, canManageUsage)
	if !allowed {
		msg := L.Get("usage.history.forbidden")
		metadata := map[string]any{"ephemeral": true, "target_user_id": target, "target_scope": usageHistoryAuditScope(target)}
		err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: msg, Flags: discordgo.MessageFlagsEphemeral, AllowedMentions: &discordgo.MessageAllowedMentions{}}})
		b.recordInteractionResponseDelivery(auditCtx, data.Name, "rejected", msg, discordgo.InteractionResponseChannelMessageWithSource, metadata, err)
		return "rejected", "usage_history_forbidden"
	}
	now := time.Now().In(b.manager.UsageLocation())
	resolvedRange, err := usagerange.Resolve(usagerange.Options{Period: period, From: fromText, To: toText, Now: now, Loc: b.manager.UsageLocation()})
	if err != nil {
		msg := L.Getf("usage.history.range.invalid", usageHistoryRangeErrorMessage(err))
		metadata := map[string]any{"ephemeral": true, "target_user_id": target, "target_scope": usageHistoryAuditScope(target), "period": period, "from": fromText != "", "to": toText != ""}
		respondErr := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: msg, Flags: discordgo.MessageFlagsEphemeral, AllowedMentions: &discordgo.MessageAllowedMentions{}}})
		b.recordInteractionResponseDelivery(auditCtx, data.Name, "rejected", msg, discordgo.InteractionResponseChannelMessageWithSource, metadata, respondErr)
		return "rejected", "usage_history_invalid_range"
	}
	period = resolvedRange.Label
	from, now := resolvedRange.From, resolvedRange.To
	deferredMetadata := map[string]any{"ephemeral": true, "target_user_id": target, "target_scope": usageHistoryAuditScope(target), "period": period, "from": from.Format(time.RFC3339), "to": now.Format(time.RFC3339)}
	err = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}})
	b.recordInteractionResponseDelivery(auditCtx, data.Name, "deferred", "", discordgo.InteractionResponseDeferredChannelMessageWithSource, deferredMetadata, err)
	if err != nil {
		return "error", "usage_history_deferred_response_failed"
	}
	token := uuid.NewString()
	state := &usageHistoryState{RequesterID: requester, TargetID: target, TargetUsername: targetUsername, GuildID: i.GuildID, Period: period, Status: status, Source: source, AllowLegacyUsername: canManageUsage, From: from, To: now, Cursors: []usageHistoryCursor{{}}, Expires: time.Now().Add(15 * time.Minute)}
	content, components, err := b.renderUsageHistory(ds, state, token)
	if err != nil {
		content := commandError(err)
		sent, editErr := ds.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content, AllowedMentions: &discordgo.MessageAllowedMentions{}})
		b.recordCommandResponseDelivery(auditCtx, data.Name, "slash", "error", content, map[string]any{"ephemeral": true, "target_user_id": target, "target_scope": usageHistoryAuditScope(target), "period": period, "from": from.Format(time.RFC3339), "to": now.Format(time.RFC3339)}, sent, editErr)
		return "error", "usage_history_query_failed"
	}
	var files []*discordgo.File
	exported := false
	truncated := false
	if exportCSV {
		filename, data, didTruncate, err := b.usageHistoryCSV(ds, state, 10000)
		if err != nil {
			content := commandError(err)
			sent, editErr := ds.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content, AllowedMentions: &discordgo.MessageAllowedMentions{}})
			b.recordCommandResponseDelivery(auditCtx, "usage-history", "slash", "error", content, map[string]any{"ephemeral": true, "target_user_id": target, "target_scope": usageHistoryAuditScope(target), "period": period, "from": from.Format(time.RFC3339), "to": now.Format(time.RFC3339), "export": true}, sent, editErr)
			return "error", "usage_history_export_failed"
		}
		files = []*discordgo.File{{Name: filename, ContentType: "text/csv; charset=utf-8", Reader: bytes.NewReader(data)}}
		exported = true
		truncated = didTruncate
		content += "\n" + L.Get("usage.history.export.attached")
		if truncated {
			content += "\n" + L.Getf("usage.history.export.truncated", 10000)
		}
	}
	usageHistoryStates.Store(token, state)
	sent, err := ds.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content, Components: &components, Files: files, AllowedMentions: &discordgo.MessageAllowedMentions{}})
	b.recordCommandResponseDelivery(auditCtx, data.Name, "slash", "sent", content, map[string]any{"ephemeral": true, "target_user_id": target, "target_scope": usageHistoryAuditScope(target), "period": period, "from": from.Format(time.RFC3339), "to": now.Format(time.RFC3339), "page": 1, "export": exported, "export_truncated": truncated}, sent, err)
	if err != nil {
		return "error", "usage_history_response_failed"
	}
	return "completed", ""
}

func usageHistoryEffectiveTarget(requesterID, requesterUsername string, targetSpecified bool, selectedID, selectedUsername string, canManage bool) (string, string, bool) {
	if !targetSpecified {
		if canManage {
			return "", "", true
		}
		return requesterID, requesterUsername, true
	}
	if selectedID != requesterID && !canManage {
		return selectedID, selectedUsername, false
	}
	return selectedID, selectedUsername, true
}

func usageHistoryAuditScope(targetID string) string {
	if strings.TrimSpace(targetID) == "" {
		return "all_users"
	}
	return "user"
}

func usageHistoryRangeErrorMessage(err error) string {
	rangeErr, ok := err.(*usagerange.RangeError)
	if !ok || rangeErr == nil {
		return L.Get("usage.history.range.error.invalid")
	}
	switch rangeErr.Code {
	case usagerange.ErrInvalidPeriod:
		return L.Get("usage.history.range.error.invalid_period")
	case usagerange.ErrPeriodTooLarge:
		return L.Get("usage.history.range.error.period_too_large")
	case usagerange.ErrFromRequired:
		return L.Get("usage.history.range.error.from_required")
	case usagerange.ErrInvalidFrom:
		return L.Get("usage.history.range.error.invalid_from")
	case usagerange.ErrInvalidTo:
		return L.Get("usage.history.range.error.invalid_to")
	case usagerange.ErrReversedRange:
		return L.Get("usage.history.range.error.reversed_range")
	default:
		return L.Get("usage.history.range.error.invalid")
	}
}

func (b *Bot) renderUsageHistory(ds *discordgo.Session, state *usageHistoryState, token string) (string, []discordgo.MessageComponent, error) {
	summary, err := b.manager.UsageHistorySummary(state.usageHistoryOptions(0, usageHistoryCursor{}), 5)
	if err != nil {
		return "", nil, err
	}
	var sb strings.Builder
	sb.WriteString(usageHistoryTitle(state) + "\n")
	sb.WriteString(L.Getf("usage.history.summary.range", state.From.Format("2006-01-02 15:04"), state.To.Format("2006-01-02 15:04"), state.Status, state.Source) + "\n")
	if summary.Records == 0 {
		sb.WriteString(L.Get("usage.history.empty"))
	} else {
		sb.WriteString(L.Getf("usage.history.summary.totals", summary.Records, summary.Users, summary.Credits, summary.CostUSD, float64(summary.DurationMs)/1000, summary.ContextUsage) + "\n")
		sb.WriteString(L.Getf("usage.history.summary.status", usageHistoryCountList(summary.StatusCounts)) + "\n")
		sb.WriteString(L.Getf("usage.history.summary.sources", usageHistoryCountList(summary.SourceCounts)) + "\n")
		sb.WriteString(L.Getf("usage.history.summary.engines", usageHistoryCountList(summary.EngineCounts)) + "\n")
		if strings.TrimSpace(state.TargetID) == "" && len(summary.TopUsers) > 0 {
			sb.WriteString(L.Getf("usage.history.summary.top_users", usageHistoryTopUsersList(ds, state.GuildID, summary.TopUsers, 3)) + "\n")
		}
		sb.WriteString(L.Get("usage.history.summary.export_hint"))
	}
	buttons := []discordgo.MessageComponent{discordgo.Button{Label: L.Get("usage.history.close"), Style: discordgo.SecondaryButton, CustomID: usageHistoryCustomPrefix + ":" + token + ":close"}}
	return sb.String(), []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}, nil
}

func usageHistoryCountList(counts []channel.UsageHistoryCount) string {
	if len(counts) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(counts))
	for _, item := range counts {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = "unknown"
		}
		parts = append(parts, fmt.Sprintf("%s %d", textutil.TruncateUTF8Bytes(name, 40), item.Records))
	}
	return strings.Join(parts, " · ")
}

func usageHistoryTopUsersList(ds *discordgo.Session, guildID string, rows []channel.UsageHistoryUserSummary, limit int) string {
	if limit <= 0 || limit > len(rows) {
		limit = len(rows)
	}
	parts := make([]string, 0, limit)
	cache := map[string]string{}
	for _, row := range rows[:limit] {
		label := usageHistoryUserSummaryLabel(ds, guildID, row, cache)
		parts = append(parts, fmt.Sprintf("%s %d turns / %.2f credits / $%.4f", label, row.Records, row.Credits, row.CostUSD))
	}
	return strings.Join(parts, " · ")
}

func usageHistoryTitle(state *usageHistoryState) string {
	if strings.TrimSpace(state.TargetID) == "" {
		return L.Getf("usage.history.title_all", state.Period)
	}
	return L.Getf("usage.history.title", state.TargetID, state.Period)
}

func usageHistoryUserSummaryLabel(ds *discordgo.Session, guildID string, row channel.UsageHistoryUserSummary, cache map[string]string) string {
	return usageHistoryReadableUser(ds, guildID, row.UserID, row.Username, cache)
}

func usageHistoryRecordUserLabel(ds *discordgo.Session, guildID string, rec channel.UsageRecord, cache map[string]string) string {
	return usageHistoryReadableUser(ds, guildID, rec.UserID, rec.Username, cache)
}

func usageHistoryReadableUser(ds *discordgo.Session, guildID, userID, username string, cache map[string]string) string {
	userID = strings.TrimSpace(userID)
	username = strings.TrimSpace(username)
	if userID != "" {
		if cached, ok := cache[userID]; ok {
			return cached
		}
		if display := usageHistoryDiscordDisplayName(ds, guildID, userID); display != "" {
			cache[userID] = display
			return display
		}
		if username != "" {
			cache[userID] = username
			return username
		}
		label := "<@" + userID + ">"
		cache[userID] = label
		return label
	}
	if username != "" {
		return textutil.TruncateUTF8Bytes(username, 80)
	}
	return L.Get("usage.history.unknown_user")
}

func usageHistoryDiscordDisplayName(ds *discordgo.Session, guildID, userID string) string {
	if ds == nil || strings.TrimSpace(userID) == "" {
		return ""
	}
	if ds.State != nil && strings.TrimSpace(guildID) != "" {
		if member, err := ds.State.Member(guildID, userID); err == nil {
			return usageHistoryMemberDisplayName(member)
		}
	}
	return ""
}

func usageHistoryMemberDisplayName(member *discordgo.Member) string {
	if member == nil {
		return ""
	}
	userName := usageHistoryUserDisplayName(member.User)
	nick := strings.TrimSpace(member.Nick)
	if nick == "" || nick == userName {
		return userName
	}
	if userName == "" {
		return nick
	}
	return nick + " (" + userName + ")"
}

func usageHistoryUserDisplayName(user *discordgo.User) string {
	if user == nil {
		return ""
	}
	username := strings.TrimSpace(user.Username)
	global := strings.TrimSpace(user.GlobalName)
	if global != "" && username != "" && global != username {
		return global + " (" + username + ")"
	}
	if global != "" {
		return global
	}
	return username
}

func (s *usageHistoryState) usageHistoryOptions(limit int, cursor usageHistoryCursor) channel.UsageHistoryOptions {
	opts := channel.UsageHistoryOptions{
		GuildID:    s.GuildID,
		UserID:     s.TargetID,
		From:       s.From,
		To:         s.To,
		Status:     s.Status,
		Source:     s.Source,
		Limit:      limit,
		BeforeTime: cursor.Time,
		BeforeID:   cursor.ID,
	}
	if s.AllowLegacyUsername {
		opts.LegacyUsernames = usageHistoryLegacyUsernames(s.TargetUsername)
	}
	return opts
}

func usageHistoryLegacyUsernames(username string) []string {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil
	}
	return []string{username}
}

func (b *Bot) usageHistoryCSV(ds *discordgo.Session, state *usageHistoryState, maxRecords int) (string, []byte, bool, error) {
	export, err := b.manager.UsageHistoryExport(state.usageHistoryOptions(0, usageHistoryCursor{}), maxRecords)
	if err != nil {
		return "", nil, false, err
	}
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	header := []string{"occurred_at", "local_time", "usage_id", "guild_id", "channel_id", "thread_id", "user_id", "username", "discord_user", "message_id", "interaction_id", "invocation_id", "engine", "model", "source", "status", "credits", "cost_usd", "metering_supported", "metering_usage_json", "duration_ms", "context_usage"}
	if err := w.Write(header); err != nil {
		return "", nil, false, err
	}
	loc := b.manager.UsageLocation()
	displayNameCache := map[string]string{}
	for _, rec := range export.Records {
		localTime := rec.Timestamp
		if parsed, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil {
			localTime = parsed.In(loc).Format("2006-01-02 15:04:05")
		}
		metering, _ := json.Marshal(rec.MeteringUsage)
		row := []string{
			rec.Timestamp,
			localTime,
			rec.UsageID,
			rec.GuildID,
			rec.ChannelID,
			rec.ThreadID,
			rec.UserID,
			rec.Username,
			usageHistoryRecordUserLabel(ds, state.GuildID, rec, displayNameCache),
			rec.MessageID,
			rec.InteractionID,
			rec.InvocationID,
			rec.Engine,
			rec.Model,
			rec.Source,
			rec.Status,
			fmt.Sprintf("%.6f", rec.Credits),
			fmt.Sprintf("%.6f", rec.CostUSD),
			fmt.Sprintf("%t", rec.MeteringSupported),
			string(metering),
			fmt.Sprintf("%d", rec.DurationMs),
			fmt.Sprintf("%.6f", rec.ContextUsage),
		}
		for i := range row {
			row[i] = usageHistoryCSVCell(row[i])
		}
		if err := w.Write(row); err != nil {
			return "", nil, false, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", nil, false, err
	}
	return usageHistoryCSVFilename(state), buf.Bytes(), export.Truncated, nil
}

func usageHistoryCSVFilename(state *usageHistoryState) string {
	period := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(strings.TrimSpace(state.Period))
	if period == "" {
		period = "history"
	}
	return "usage-history-" + period + ".csv"
}

func usageHistoryCSVCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r', '\n':
		return "'" + value
	default:
		return value
	}
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
	content, components, err := b.renderUsageHistory(ds, state, token)
	if err != nil {
		respondInteractionEphemeral(ds, i, commandError(err))
		return
	}
	state.Expires = time.Now().Add(15 * time.Minute)
	err = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseUpdateMessage, Data: &discordgo.InteractionResponseData{Content: content, Components: components, AllowedMentions: &discordgo.MessageAllowedMentions{}}})
	b.recordInteractionResponseDelivery(auditCtx, "usage-history-page", "sent", content, discordgo.InteractionResponseUpdateMessage, map[string]any{"ephemeral": true, "page": state.Page + 1}, err)
}

func stringPtr(value string) *string { return &value }
