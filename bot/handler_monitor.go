package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func (b *Bot) handleMonitorPrompt(ds *discordgo.Session, i *discordgo.InteractionCreate, auditCtx cmdCtx, input string) {
	const command = "monitor-prompt"
	visibility := commandResponseVisibility(command, "")
	err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: commandInteractionFlags(visibility)},
	})
	visibilityMetadata := commandVisibilityMetadata(visibility)
	b.recordInteractionResponseDelivery(auditCtx, command, "deferred", "", discordgo.InteractionResponseDeferredChannelMessageWithSource, visibilityMetadata, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := b.parseMonitorPrompt(ctx, i.ChannelID, input)
	if err != nil {
		msg := L.Getf("monitor.prompt.failed", 3)
		switch {
		case errors.Is(err, ErrMonitorScheduleUncertain):
			msg = L.Get("monitor.prompt.no_schedule")
		case errors.Is(err, ErrMonitorCheckUncertain):
			msg = L.Get("monitor.prompt.no_check")
		case errors.Is(err, ErrMonitorConditionUncertain):
			msg = L.Get("monitor.prompt.no_condition")
		case errors.Is(err, ErrMonitorFieldTooLong):
			msg = L.Get("monitor.prompt.too_long")
		}
		b.followupInteractionForCommand(ds, i, auditCtx, command, msg, map[string]any{"parse_status": "failed"})
		log.Printf("[monitor-prompt] parse failed: %v", err)
		return
	}

	msg := monitorPromptConfirmMessage(result)
	key := randomA2AConfirmationID()
	b.monitorPromptCache.Store(key, result)
	confirmID := "monp_confirm_" + key
	cancelID := "monp_cancel"
	sent, err := ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content:         msg,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
		Flags:           commandInteractionFlags(visibility),
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: L.Get("monitor.prompt.btn.confirm"), Style: discordgo.SuccessButton, CustomID: confirmID},
			discordgo.Button{Label: L.Get("monitor.prompt.btn.cancel"), Style: discordgo.DangerButton, CustomID: cancelID},
		}}},
	})
	b.recordCommandResponseDelivery(auditCtx, command, "slash", "sent", msg, mergeMetadata(map[string]any{"has_components": true, "parse_status": "ok"}, visibilityMetadata), sent, err)
}

func monitorPromptConfirmMessage(result *ParsedMonitorJob) string {
	if result == nil {
		return ""
	}
	schedule := monitorScheduleDisplay("", result.Schedule)
	return secrets.RedactEnv(L.Getf("monitor.prompt.confirm", result.Name, schedule, truncate(result.CheckPrompt, 700), truncate(result.NotifyWhen, 700)))
}

func (b *Bot) monitorActorCanManage(ds *discordgo.Session, i *discordgo.InteractionCreate, targetID string) bool {
	userID, _ := interactionUser(i)
	return b.userCanManageChannelTarget(ds, userID, targetID)
}

func (b *Bot) requireMonitorManager(ds *discordgo.Session, i *discordgo.InteractionCreate, targetID string) bool {
	if b.monitorActorCanManage(ds, i, targetID) {
		return true
	}
	respondInteractionEphemeral(ds, i, L.Get("monitor.forbidden"))
	return false
}

func (b *Bot) handleMonitorPromptButton(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	if strings.HasPrefix(customID, "monp_") && !b.requireMonitorManager(ds, i, i.ChannelID) {
		return
	}
	if customID == "monp_cancel" {
		if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseUpdateMessage, Data: &discordgo.InteractionResponseData{Content: L.Get("monitor.prompt.cancelled"), Components: []discordgo.MessageComponent{}, AllowedMentions: &discordgo.MessageAllowedMentions{}}}); err != nil {
			log.Printf("[interaction] monp_cancel respond failed: %v", err)
		}
		return
	}
	if !strings.HasPrefix(customID, "monp_confirm_") {
		return
	}

	if !b.manager.ChannelInitialized(i.ChannelID) {
		respondInteraction(ds, i, L.Getf("setup.required.command", "/monitor-prompt"))
		return
	}
	data := strings.TrimPrefix(customID, "monp_confirm_")
	cached, ok := b.monitorPromptCache.LoadAndDelete(data)
	if !ok {
		respondInteraction(ds, i, L.Get("error.expired"))
		return
	}
	result := *cached
	username, userID, guildID := "", "", ""
	if i.Member != nil && i.Member.User != nil {
		username = i.Member.User.Username
		userID = i.Member.User.ID
	}
	if i.GuildID != "" {
		guildID = i.GuildID
	}
	job := &heartbeat.MonitorJob{Name: result.Name, ChannelID: i.ChannelID, GuildID: guildID, Schedule: result.Schedule, ScheduleHuman: result.Schedule, CheckPrompt: result.CheckPrompt, NotifyWhen: result.NotifyWhen, HistoryLimit: 10, Enabled: true, CreatedBy: username, CreatedByID: userID}
	if err := b.monitorStore.Add(job); err != nil {
		log.Printf("[interaction] monitor prompt create save failed: %v", err)
		respondInteraction(ds, i, L.Get("monitor.error.save_failed"))
		return
	}
	b.monitorTask.RecalcNextRun(job)
	content := secrets.RedactEnv(L.Getf("monitor.prompt.created", job.Name, monitorScheduleDisplay(job.ScheduleHuman, job.Schedule), job.NotifyWhen))
	if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseUpdateMessage, Data: &discordgo.InteractionResponseData{Content: content, Components: []discordgo.MessageComponent{}, AllowedMentions: &discordgo.MessageAllowedMentions{}}}); err != nil {
		log.Printf("[interaction] monp_confirm respond failed: %v", err)
	}
}

func (b *Bot) handleMonitorList(ds *discordgo.Session, i *discordgo.InteractionCreate, auditCtx cmdCtx) {
	const command = "monitor-list"
	if !b.monitorActorCanManage(ds, i, i.ChannelID) {
		b.respondInteractionForCommand(ds, i, auditCtx, command, L.Get("monitor.forbidden"), map[string]any{"rejected_reason": "forbidden"})
		return
	}
	visibility := commandResponseVisibility(command, "")
	err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: commandInteractionFlags(visibility)}})
	visibilityMetadata := commandVisibilityMetadata(visibility)
	b.recordInteractionResponseDelivery(auditCtx, command, "deferred", "", discordgo.InteractionResponseDeferredChannelMessageWithSource, visibilityMetadata, err)
	jobs := b.monitorStore.ListByChannel(i.ChannelID)
	if len(jobs) == 0 {
		b.followupInteractionForCommand(ds, i, auditCtx, command, L.Get("monitor.list.empty"), nil)
		return
	}
	for index, job := range jobs {
		content, components := b.buildMonitorCard(job)
		content = secrets.RedactEnv(content)
		sent, err := ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: content, Components: components, AllowedMentions: &discordgo.MessageAllowedMentions{}, Flags: commandInteractionFlags(visibility)})
		b.recordCommandResponseDelivery(auditCtx, command, "slash", "sent", content, mergeMetadata(map[string]any{"part_index": index + 1, "part_total": len(jobs), "has_components": true}, visibilityMetadata), sent, err)
	}
}

func (b *Bot) handleMonitorRun(ds *discordgo.Session, i *discordgo.InteractionCreate, auditCtx cmdCtx, name string) {
	const command = "monitor-run"
	if !b.monitorActorCanManage(ds, i, i.ChannelID) {
		b.respondInteractionForCommand(ds, i, auditCtx, command, L.Get("monitor.forbidden"), map[string]any{"rejected_reason": "forbidden"})
		return
	}
	job, ok := b.monitorStore.FindByName(i.ChannelID, name)
	if !ok {
		b.respondInteractionForCommand(ds, i, auditCtx, command, L.Getf("monitor.not_found", name), nil)
		return
	}
	if job.ChannelID != i.ChannelID && !b.monitorActorCanManage(ds, i, job.ChannelID) {
		b.respondInteractionForCommand(ds, i, auditCtx, command, L.Get("monitor.forbidden"), map[string]any{"rejected_reason": "forbidden"})
		return
	}
	if !b.manager.ChannelInitialized(job.ChannelID) {
		b.respondInteractionForCommand(ds, i, auditCtx, command, L.Getf("setup.required.command", "/monitor-run"), nil)
		return
	}
	job.RunOnce = true
	if err := b.monitorStore.Update(job); err != nil {
		log.Printf("[interaction] monitor run save failed for %s: %v", job.ID, err)
		b.respondInteractionForCommand(ds, i, auditCtx, command, L.Get("monitor.error.save_failed"), nil)
		return
	}
	b.respondInteractionForCommand(ds, i, auditCtx, command, L.Getf("monitor.running", job.Name), nil)
	b.monitorTask.RunNow(job.ID)
}

func (b *Bot) handleMonitorButton(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	var action, jobID string
	for _, prefix := range []string{"monitor_pause_", "monitor_resume_", "monitor_run_", "monitor_edit_", "monitor_delete_"} {
		if strings.HasPrefix(customID, prefix) {
			action = strings.TrimSuffix(strings.TrimPrefix(prefix, "monitor_"), "_")
			jobID = strings.TrimPrefix(customID, prefix)
			break
		}
	}
	if jobID == "" {
		return
	}
	job, ok := b.monitorStore.Get(jobID)
	if !ok {
		respondInteraction(ds, i, L.Getf("monitor.not_found", jobID))
		return
	}
	if !b.requireMonitorManager(ds, i, job.ChannelID) {
		return
	}
	switch action {
	case "pause":
		job.Enabled = false
		if err := b.monitorStore.Update(job); err != nil {
			log.Printf("[interaction] monitor pause save failed for %s: %v", job.ID, err)
			respondInteraction(ds, i, L.Get("monitor.error.save_failed"))
			return
		}
		b.updateMonitorCard(ds, i, job, L.Getf("monitor.paused", job.Name))
	case "resume":
		if !b.manager.ChannelInitialized(job.ChannelID) {
			respondInteraction(ds, i, L.Getf("setup.required.command", "/monitor-list"))
			return
		}
		job.Enabled = true
		if err := b.monitorStore.Update(job); err != nil {
			log.Printf("[interaction] monitor resume save failed for %s: %v", job.ID, err)
			respondInteraction(ds, i, L.Get("monitor.error.save_failed"))
			return
		}
		b.monitorTask.RecalcNextRun(job)
		b.updateMonitorCard(ds, i, job, L.Getf("monitor.resumed", job.Name))
	case "run":
		if !b.manager.ChannelInitialized(job.ChannelID) {
			respondInteraction(ds, i, L.Getf("setup.required.command", "/monitor-run"))
			return
		}
		job.RunOnce = true
		if err := b.monitorStore.Update(job); err != nil {
			log.Printf("[interaction] monitor run button save failed for %s: %v", job.ID, err)
			respondInteraction(ds, i, L.Get("monitor.error.save_failed"))
			return
		}
		b.monitorTask.RunNow(job.ID)
		b.updateMonitorCard(ds, i, job, L.Getf("monitor.running", job.Name))
	case "delete":
		if err := b.monitorStore.Remove(jobID); err != nil {
			log.Printf("[interaction] monitor delete save failed for %s: %v", jobID, err)
			respondInteraction(ds, i, L.Get("monitor.error.save_failed"))
			return
		}
		if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseUpdateMessage, Data: &discordgo.InteractionResponseData{Content: secrets.RedactEnv(L.Getf("monitor.deleted", job.Name)), Components: []discordgo.MessageComponent{}, AllowedMentions: &discordgo.MessageAllowedMentions{}}}); err != nil {
			log.Printf("[interaction] monitor delete respond failed: %v", err)
		}
	case "edit":
		b.showMonitorEditModal(ds, i, job)
	}
}

func (b *Bot) showMonitorEditModal(ds *discordgo.Session, i *discordgo.InteractionCreate, job *heartbeat.MonitorJob) {
	if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseModal, Data: &discordgo.InteractionResponseData{CustomID: "monitor_edit_modal_" + job.ID, Title: L.Get("monitor.modal.title_edit"), Components: monitorModalComponents(job)}}); err != nil {
		log.Printf("[interaction] monitor edit modal respond failed: %v", err)
	}
}

func (b *Bot) handleMonitorEditSubmit(ds *discordgo.Session, i *discordgo.InteractionCreate, jobID string) {
	job, ok := b.monitorStore.Get(jobID)
	if !ok {
		respondInteraction(ds, i, L.Getf("monitor.not_found", jobID))
		return
	}
	if !b.requireMonitorManager(ds, i, job.ChannelID) {
		return
	}
	fields := modalFields(i.ModalSubmitData())
	name, schedule, checkPrompt, notifyWhen := fields["monitor_name"], fields["monitor_schedule"], fields["monitor_check"], fields["monitor_condition"]
	recalc, err := heartbeat.ApplyMonitorUpdate(job, heartbeat.MonitorUpdate{Name: &name, Schedule: &schedule, CheckPrompt: &checkPrompt, NotifyWhen: &notifyWhen})
	if err != nil {
		respondInteraction(ds, i, monitorEditValidationMessage(err))
		return
	}
	if err := b.monitorStore.Update(job); err != nil {
		log.Printf("[interaction] monitor edit save failed for %s: %v", job.ID, err)
		respondInteraction(ds, i, L.Get("monitor.error.save_failed"))
		return
	}
	if recalc {
		b.monitorTask.RecalcNextRun(job)
	}
	respondInteraction(ds, i, L.Getf("monitor.updated", job.Name, monitorScheduleDisplay(job.ScheduleHuman, job.Schedule), job.NotifyWhen))
}

func monitorEditValidationMessage(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	switch {
	case strings.Contains(text, "name cannot be empty"):
		return L.Get("monitor.error.name_required")
	case strings.Contains(text, "name cannot exceed"):
		return L.Get("monitor.error.name_too_long")
	case strings.Contains(text, "schedule cannot be empty"):
		return L.Get("monitor.error.schedule_required")
	case strings.Contains(text, "schedule cannot exceed"):
		return L.Get("monitor.error.schedule_too_long")
	case strings.Contains(text, "check_prompt cannot be empty"):
		return L.Get("monitor.error.check_required")
	case strings.Contains(text, "check_prompt cannot exceed"):
		return L.Get("monitor.error.check_too_long")
	case strings.Contains(text, "notify_when cannot be empty"):
		return L.Get("monitor.error.condition_required")
	case strings.Contains(text, "notify_when cannot exceed"):
		return L.Get("monitor.error.condition_too_long")
	case strings.Contains(text, "invalid schedule"):
		return L.Get("monitor.error.schedule_invalid")
	default:
		return L.Get("monitor.error.invalid")
	}
}

func modalFields(data discordgo.ModalSubmitInteractionData) map[string]string {
	fields := map[string]string{}
	for _, row := range data.Components {
		ar, ok := row.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, comp := range ar.Components {
			ti, ok := comp.(*discordgo.TextInput)
			if ok {
				fields[ti.CustomID] = ti.Value
			}
		}
	}
	return fields
}

func monitorModalComponents(job *heartbeat.MonitorJob) []discordgo.MessageComponent {
	name, schedule, checkPrompt, notifyWhen := "", "", "", ""
	if job != nil {
		name, schedule, checkPrompt, notifyWhen = job.Name, job.ScheduleHuman, job.CheckPrompt, job.NotifyWhen
	}
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: "monitor_name", Label: L.Get("monitor.modal.name"), Style: discordgo.TextInputShort, Value: name, Placeholder: L.Get("monitor.modal.name_ph"), Required: true, MaxLength: 100}}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: "monitor_schedule", Label: L.Get("monitor.modal.schedule"), Style: discordgo.TextInputShort, Value: schedule, Placeholder: L.Get("monitor.modal.schedule_ph"), Required: true, MaxLength: 100}}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: "monitor_check", Label: L.Get("monitor.modal.check"), Style: discordgo.TextInputParagraph, Value: checkPrompt, Placeholder: L.Get("monitor.modal.check_ph"), Required: true, MaxLength: 2000}}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: "monitor_condition", Label: L.Get("monitor.modal.condition"), Style: discordgo.TextInputParagraph, Value: notifyWhen, Placeholder: L.Get("monitor.modal.condition_ph"), Required: true, MaxLength: 1000}}},
	}
}

func monitorStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case heartbeat.MonitorStatusMatched:
		return L.Get("monitor.status.matched")
	case heartbeat.MonitorStatusSuppressed:
		return L.Get("monitor.status.suppressed")
	case heartbeat.MonitorStatusError:
		return L.Get("monitor.status.error")
	default:
		return L.Get("monitor.status.unknown")
	}
}

func monitorScheduleDisplay(human, schedule string) string {
	human = strings.TrimSpace(human)
	schedule = strings.TrimSpace(schedule)
	primary := human
	if primary == "" || primary == schedule {
		primary = monitorCronDescription(schedule)
	}
	if primary == "" {
		if schedule == "" {
			return ""
		}
		return "`" + schedule + "`"
	}
	if schedule != "" && primary != schedule {
		return L.Getf("monitor.schedule.display", primary, schedule)
	}
	return primary
}

func monitorCronDescription(schedule string) string {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return ""
	}
	if strings.HasPrefix(fields[0], "*/") && fields[1] == "*" && fields[2] == "*" && fields[3] == "*" && fields[4] == "*" {
		if minutes, err := strconv.Atoi(strings.TrimPrefix(fields[0], "*/")); err == nil && minutes > 0 {
			return L.Getf("monitor.schedule.every_minutes", minutes)
		}
	}
	if fields[0] == "0" && fields[1] == "*" && fields[2] == "*" && fields[3] == "*" && fields[4] == "*" {
		return L.Get("monitor.schedule.hourly")
	}
	minute, errM := strconv.Atoi(fields[0])
	hour, errH := strconv.Atoi(fields[1])
	if errM != nil || errH != nil {
		return heartbeat.DescribeSchedule(schedule)
	}
	at := fmt.Sprintf("%02d:%02d", hour, minute)
	if fields[2] == "*" && fields[3] == "*" && fields[4] == "*" {
		return L.Getf("monitor.schedule.daily_at", at)
	}
	if fields[2] == "*" && fields[3] == "*" && fields[4] == "1-5" {
		return L.Getf("monitor.schedule.weekdays_at", at)
	}
	if fields[2] == "*" && fields[3] == "*" {
		if day, err := strconv.Atoi(fields[4]); err == nil && day >= 0 && day <= 6 {
			return L.Getf("monitor.schedule.weekday_at", L.Get(fmt.Sprintf("monitor.weekday.%d", day)), at)
		}
	}
	return heartbeat.DescribeSchedule(schedule)
}

func (b *Bot) buildMonitorCard(job *heartbeat.MonitorJob) (string, []discordgo.MessageComponent) {
	status := "✅"
	if !job.Enabled {
		status = "⏸️"
	}
	loc := b.cronLocationOrLocal()
	formatTime := func(raw, empty string) string {
		if raw == "" {
			return empty
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t.In(loc).Format("01/02 15:04")
		}
		return empty
	}
	lastCheck := formatTime(job.LastCheckAt, L.Get("monitor.list.never_checked"))
	lastNotify := formatTime(job.LastNotifyAt, L.Get("monitor.list.never_notified"))
	if job.ThreadID != "" && job.LastNotifyAt != "" {
		lastNotify += " → <#" + job.ThreadID + ">"
	}
	nextRun := formatTime(job.NextRun, L.Get("monitor.list.next_run_pending"))
	lastResult := L.Get("monitor.list.result.none")
	if b.monitorTask != nil {
		if h, ok := b.monitorTask.LatestHistory(job.ID); ok {
			reason := h.Reason
			if h.Status == heartbeat.MonitorStatusError {
				reason = L.Get("monitor.list.result.error_detail")
			} else if h.Status == heartbeat.MonitorStatusSuppressed || reason == "" {
				reason = L.Get("monitor.list.result.no_detail")
			}
			lastResult = L.Getf("monitor.list.result", monitorStatusLabel(h.Status), truncate(secrets.RedactEnv(reason), 120))
		}
	}
	content := L.Getf("monitor.list.item", status, job.Name, monitorScheduleDisplay(job.ScheduleHuman, job.Schedule), lastCheck, lastResult, lastNotify, nextRun, truncate(job.CheckPrompt, 120), truncate(job.NotifyWhen, 120))
	var buttons []discordgo.MessageComponent
	if job.Enabled {
		buttons = append(buttons, discordgo.Button{Label: L.Get("monitor.btn.pause"), Style: discordgo.SecondaryButton, CustomID: "monitor_pause_" + job.ID})
	} else {
		buttons = append(buttons, discordgo.Button{Label: L.Get("monitor.btn.resume"), Style: discordgo.SuccessButton, CustomID: "monitor_resume_" + job.ID})
	}
	buttons = append(buttons, discordgo.Button{Label: L.Get("monitor.btn.run"), Style: discordgo.PrimaryButton, CustomID: "monitor_run_" + job.ID}, discordgo.Button{Label: L.Get("monitor.btn.edit"), Style: discordgo.SecondaryButton, CustomID: "monitor_edit_" + job.ID}, discordgo.Button{Label: L.Get("monitor.btn.delete"), Style: discordgo.DangerButton, CustomID: "monitor_delete_" + job.ID})
	return content, []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
}

func (b *Bot) updateMonitorCard(ds *discordgo.Session, i *discordgo.InteractionCreate, job *heartbeat.MonitorJob, statusMsg string) {
	content, components := b.buildMonitorCard(job)
	content = secrets.RedactEnv(statusMsg + "\n\n" + content)
	if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseUpdateMessage, Data: &discordgo.InteractionResponseData{Content: content, Components: components, AllowedMentions: &discordgo.MessageAllowedMentions{}}}); err != nil {
		log.Printf("[interaction] monitor card update failed: %v", err)
	}
}
