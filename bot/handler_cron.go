package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	L "github.com/nczz/kiro-discord-bot/locale"
)

// handleCronModal responds to /cron by showing a modal form.
func (b *Bot) handleCronModal(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "cron_add_modal",
			Title:    L.Get("cron.modal.title"),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "cron_name",
						Label:       L.Get("cron.modal.name"),
						Style:       discordgo.TextInputShort,
						Placeholder: L.Get("cron.modal.name_ph"),
						Required:    true,
						MaxLength:   100,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "cron_schedule",
						Label:       L.Get("cron.modal.schedule"),
						Style:       discordgo.TextInputShort,
						Placeholder: L.Get("cron.modal.schedule_ph"),
						Required:    true,
						MaxLength:   100,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "cron_prompt",
						Label:       L.Get("cron.modal.prompt"),
						Style:       discordgo.TextInputParagraph,
						Placeholder: L.Get("cron.modal.prompt_ph"),
						Required:    true,
						MaxLength:   2000,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "cron_cwd",
						Label:       L.Get("cron.modal.cwd"),
						Style:       discordgo.TextInputShort,
						Placeholder: L.Get("cron.modal.cwd_ph"),
						Required:    false,
						MaxLength:   200,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "cron_model",
						Label:       L.Get("cron.modal.model"),
						Style:       discordgo.TextInputShort,
						Placeholder: L.Get("cron.modal.model_ph"),
						Required:    false,
						MaxLength:   100,
					},
				}},
			},
		},
	})
}

// handleCronModalSubmit processes the modal form submission.
func (b *Bot) handleCronModalSubmit(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	fields := map[string]string{}
	for _, row := range data.Components {
		ar, ok := row.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, comp := range ar.Components {
			ti, ok := comp.(*discordgo.TextInput)
			if !ok {
				continue
			}
			fields[ti.CustomID] = ti.Value
		}
	}

	name := fields["cron_name"]
	scheduleInput := fields["cron_schedule"]
	prompt := fields["cron_prompt"]
	cwd := fields["cron_cwd"]
	model := fields["cron_model"]

	// Parse schedule
	cronExpr, err := heartbeat.ParseSchedule(scheduleInput)
	if err != nil {
		respondInteraction(ds, i, L.Getf("error.parse_schedule", err.Error()))
		return
	}

	username := ""
	if i.Member != nil && i.Member.User != nil {
		username = i.Member.User.Username
	}
	guildID := ""
	if i.GuildID != "" {
		guildID = i.GuildID
	}

	job := &heartbeat.CronJob{
		Name:          name,
		ChannelID:     i.ChannelID,
		GuildID:       guildID,
		Schedule:      cronExpr,
		ScheduleHuman: scheduleInput,
		Prompt:        prompt,
		CWD:           cwd,
		Model:         model,
		HistoryLimit:  10,
		Enabled:       true,
		CreatedBy:     username,
	}
	if err := b.cronStore.Add(job); err != nil {
		respondInteraction(ds, i, L.Getf("error.save_failed", err.Error()))
		return
	}

	respondInteraction(ds, i, L.Getf("cron.created",
		name, cronExpr, heartbeat.DescribeSchedule(cronExpr), prompt))
}

// handleCronEditSubmit processes the edit modal form submission.
func (b *Bot) handleCronEditSubmit(ds *discordgo.Session, i *discordgo.InteractionCreate, jobID string) {
	job, ok := b.cronStore.Get(jobID)
	if !ok {
		respondInteraction(ds, i, L.Get("cron.not_found"))
		return
	}

	data := i.ModalSubmitData()
	fields := map[string]string{}
	for _, row := range data.Components {
		ar, ok := row.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, comp := range ar.Components {
			ti, ok := comp.(*discordgo.TextInput)
			if !ok {
				continue
			}
			fields[ti.CustomID] = ti.Value
		}
	}

	scheduleInput := fields["cron_schedule"]
	cronExpr, err := heartbeat.ParseSchedule(scheduleInput)
	if err != nil {
		respondInteraction(ds, i, L.Getf("error.parse_schedule", err.Error()))
		return
	}

	job.Name = fields["cron_name"]
	job.Schedule = cronExpr
	job.ScheduleHuman = scheduleInput
	job.Prompt = fields["cron_prompt"]
	job.CWD = fields["cron_cwd"]
	job.Model = fields["cron_model"]

	if err := b.cronStore.Update(job); err != nil {
		respondInteraction(ds, i, L.Getf("error.save_failed", err.Error()))
		return
	}

	respondInteraction(ds, i, L.Getf("cron.updated",
		job.Name, cronExpr, heartbeat.DescribeSchedule(cronExpr), job.Prompt))
}

// handleCronList responds to /cron-list with a list of jobs and action buttons.
func (b *Bot) handleCronList(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	jobs := b.cronStore.ListByChannel(i.ChannelID)
	if len(jobs) == 0 {
		followupInteraction(ds, i, L.Get("cron.list.empty"))
		return
	}

	for _, job := range jobs {
		status := "✅"
		if !job.Enabled {
			status = "⏸️"
		}
		if job.OneShot {
			status = "🔔"
		}

		lastRun := L.Get("cron.list.last_run_none")
		if job.LastRun != "" {
			if t, err := time.Parse(time.RFC3339, job.LastRun); err == nil {
				lastRun = t.Format("01/02 15:04")
			}
		}
		nextRun := L.Get("cron.list.next_run_pending")
		if job.NextRun != "" {
			if t, err := time.Parse(time.RFC3339, job.NextRun); err == nil {
				nextRun = t.Format("01/02 15:04")
			}
		}

		schedDesc := "`" + job.Schedule + "` " + heartbeat.DescribeSchedule(job.Schedule)
		content := L.Getf("cron.list.item",
			status, job.Name, schedDesc, lastRun, nextRun, truncate(job.Prompt, 100))

		// Build buttons
		var buttons []discordgo.MessageComponent
		if job.Enabled {
			buttons = append(buttons, discordgo.Button{
				Label:    L.Get("cron.btn.pause"),
				Style:    discordgo.SecondaryButton,
				CustomID: "cron_pause_" + job.ID,
			})
		} else {
			buttons = append(buttons, discordgo.Button{
				Label:    L.Get("cron.btn.resume"),
				Style:    discordgo.SuccessButton,
				CustomID: "cron_resume_" + job.ID,
			})
		}
		buttons = append(buttons,
			discordgo.Button{
				Label:    L.Get("cron.btn.run"),
				Style:    discordgo.PrimaryButton,
				CustomID: "cron_run_" + job.ID,
			},
			discordgo.Button{
				Label:    L.Get("cron.btn.edit"),
				Style:    discordgo.SecondaryButton,
				CustomID: "cron_edit_" + job.ID,
			},
			discordgo.Button{
				Label:    L.Get("cron.btn.delete"),
				Style:    discordgo.DangerButton,
				CustomID: "cron_delete_" + job.ID,
			},
		)

		_, _ = ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: content,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: buttons},
			},
		})
	}
}

// handleCronButton processes button clicks on cron-list messages.
func (b *Bot) handleCronButton(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID

	// Parse action and job ID
	var action, jobID string
	for _, prefix := range []string{"cron_pause_", "cron_resume_", "cron_run_", "cron_edit_", "cron_delete_"} {
		if strings.HasPrefix(customID, prefix) {
			action = strings.TrimSuffix(strings.TrimPrefix(prefix, "cron_"), "_")
			jobID = strings.TrimPrefix(customID, prefix)
			break
		}
	}
	if jobID == "" {
		return
	}

	job, ok := b.cronStore.Get(jobID)
	if !ok {
		respondInteraction(ds, i, L.Get("cron.not_found"))
		return
	}

	switch action {
	case "pause":
		job.Enabled = false
		_ = b.cronStore.Update(job)
		respondInteraction(ds, i, L.Getf("cron.paused", job.Name))
	case "resume":
		job.Enabled = true
		_ = b.cronStore.Update(job)
		respondInteraction(ds, i, L.Getf("cron.resumed", job.Name))
	case "run":
		respondInteraction(ds, i, L.Getf("cron.running", job.Name))
		// Trigger execution in background — set NextRun to now so next heartbeat picks it up
		job.NextRun = time.Now().Add(-time.Minute).Format(time.RFC3339)
		_ = b.cronStore.Update(job)
	case "delete":
		_ = b.cronStore.Remove(jobID)
		respondInteraction(ds, i, L.Getf("cron.deleted", job.Name))
	case "edit":
		_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: &discordgo.InteractionResponseData{
				CustomID: "cron_edit_modal_" + jobID,
				Title:    L.Get("cron.modal.title_edit"),
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:  "cron_name",
							Label:     L.Get("cron.modal.name"),
							Style:     discordgo.TextInputShort,
							Value:     job.Name,
							Required:  true,
							MaxLength: 100,
						},
					}},
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:  "cron_schedule",
							Label:     L.Get("cron.modal.schedule"),
							Style:     discordgo.TextInputShort,
							Value:     job.ScheduleHuman,
							Required:  true,
							MaxLength: 100,
						},
					}},
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:  "cron_prompt",
							Label:     L.Get("cron.modal.prompt"),
							Style:     discordgo.TextInputParagraph,
							Value:     job.Prompt,
							Required:  true,
							MaxLength: 2000,
						},
					}},
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:  "cron_cwd",
							Label:     L.Get("cron.modal.cwd"),
							Style:     discordgo.TextInputShort,
							Value:     job.CWD,
							Required:  false,
							MaxLength: 200,
						},
					}},
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:  "cron_model",
							Label:     L.Get("cron.modal.model"),
							Style:     discordgo.TextInputShort,
							Value:     job.Model,
							Required:  false,
							MaxLength: 100,
						},
					}},
				},
			},
		})
	}
}

// handleCronRun handles /cron-run <name>
func (b *Bot) handleCronRun(ds *discordgo.Session, i *discordgo.InteractionCreate, name string) {
	job, ok := b.cronStore.FindByName(i.ChannelID, name)
	if !ok {
		respondInteraction(ds, i, L.Getf("cron.not_found", name))
		return
	}
	respondInteraction(ds, i, L.Getf("cron.running", job.Name))
	job.NextRun = time.Now().Add(-time.Minute).Format(time.RFC3339)
	_ = b.cronStore.Update(job)
}

func respondInteraction(ds *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: msg},
	})
}

func followupInteraction(ds *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_, _ = ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: msg})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// handleCronTextCommand handles !cron text commands.
func (b *Bot) handleCronTextCommand(ds *discordgo.Session, channelID, guildID, userID, content string) {
	switch {
	case content == "!cron list":
		jobs := b.cronStore.ListByChannel(channelID)
		if len(jobs) == 0 {
			ds.ChannelMessageSend(channelID, L.Get("cron.list.empty"))
			return
		}
		var sb strings.Builder
		sb.WriteString(L.Get("cron.list.header"))
		for i, job := range jobs {
			status := "✅"
			if !job.Enabled {
				status = "⏸️"
			}
			if job.OneShot {
				status = "🔔"
			}
			sb.WriteString(fmt.Sprintf("%d. %s **%s** — %s\n   Prompt: %s\n",
				i+1, status, job.Name, job.ScheduleHuman, truncate(job.Prompt, 80)))
		}
		ds.ChannelMessageSend(channelID, sb.String())

	case strings.HasPrefix(content, "!cron run "):
		name := strings.TrimSpace(strings.TrimPrefix(content, "!cron run "))
		job, ok := b.cronStore.FindByName(channelID, name)
		if !ok {
			ds.ChannelMessageSend(channelID, L.Getf("cron.not_found", name))
			return
		}
		ds.ChannelMessageSend(channelID, L.Getf("cron.running", job.Name))
		job.NextRun = time.Now().Add(-time.Minute).Format(time.RFC3339)
		_ = b.cronStore.Update(job)

	default:
		ds.ChannelMessageSend(channelID, L.Get("cron.usage"))
	}
}

// handleRemind handles /remind slash command.
func (b *Bot) handleRemind(ds *discordgo.Session, i *discordgo.InteractionCreate, timeStr, content string, useAgent bool) {
	loc := time.Now().Location()
	if b.cronTimezone != "" {
		if l, err := time.LoadLocation(b.cronTimezone); err == nil {
			loc = l
		}
	}

	target, err := heartbeat.ParseTime(timeStr, loc)
	if err != nil {
		respondInteraction(ds, i, L.Getf("error.parse_time", err.Error()))
		return
	}

	userID := ""
	username := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
		username = i.Member.User.Username
	}
	guildID := i.GuildID

	job := &heartbeat.CronJob{
		Name:          L.Getf("remind.name_prefix", truncate(content, 30)),
		ChannelID:     i.ChannelID,
		GuildID:       guildID,
		Prompt:        content,
		OneShot:       true,
		UseAgent:      useAgent,
		MentionID:     userID,
		Enabled:       true,
		CreatedBy:     username,
		NextRun:       target.Format(time.RFC3339),
		ScheduleHuman: timeStr,
		HistoryLimit:  0,
	}
	if err := b.cronStore.Add(job); err != nil {
		respondInteraction(ds, i, L.Getf("error.save_failed", err.Error()))
		return
	}

	respondInteraction(ds, i, L.Getf("remind.created",
		target.Format("2006/01/02 15:04"), content))
}

// handleRemindText handles !remind text command.
func (b *Bot) handleRemindText(ds *discordgo.Session, channelID, guildID, userID, username, content string) {
	// Parse: !remind <time> <content>
	// Check for --agent flag
	useAgent := false
	if strings.HasPrefix(content, "--agent ") {
		useAgent = true
		content = strings.TrimPrefix(content, "--agent ")
	}

	// Find first space after time portion
	parts := strings.SplitN(content, " ", 2)
	if len(parts) < 2 {
		ds.ChannelMessageSend(channelID, L.Get("remind.usage"))
		return
	}

	// Try progressively longer time strings
	loc := time.Now().Location()
	if b.cronTimezone != "" {
		if l, err := time.LoadLocation(b.cronTimezone); err == nil {
			loc = l
		}
	}

	words := strings.Fields(content)
	var target time.Time
	var timeStr, prompt string
	var found bool

	for i := 1; i <= len(words) && i <= 3; i++ {
		candidate := strings.Join(words[:i], " ")
		if t, err := heartbeat.ParseTime(candidate, loc); err == nil {
			target = t
			timeStr = candidate
			prompt = strings.TrimSpace(strings.Join(words[i:], " "))
			found = true
		}
	}
	if !found || prompt == "" {
		ds.ChannelMessageSend(channelID, L.Get("error.parse_time_or_empty"))
		return
	}

	job := &heartbeat.CronJob{
		Name:          L.Getf("remind.name_prefix", truncate(prompt, 30)),
		ChannelID:     channelID,
		GuildID:       guildID,
		Prompt:        prompt,
		OneShot:       true,
		UseAgent:      useAgent,
		MentionID:     userID,
		Enabled:       true,
		CreatedBy:     username,
		NextRun:       target.Format(time.RFC3339),
		ScheduleHuman: timeStr,
		HistoryLimit:  0,
	}
	if err := b.cronStore.Add(job); err != nil {
		ds.ChannelMessageSend(channelID, L.Getf("error.save_failed", err.Error()))
		return
	}

	ds.ChannelMessageSend(channelID, L.Getf("remind.created",
		target.Format("2006/01/02 15:04"), prompt))
}

// handleCronPrompt handles /cron-prompt <description>
func (b *Bot) handleCronPrompt(ds *discordgo.Session, i *discordgo.InteractionCreate, input string) {
	// Deferred response — parsing takes a few seconds
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := b.parseCronPrompt(ctx, input)
	if err != nil {
		followupInteraction(ds, i, L.Getf("cron.prompt.failed", 3))
		log.Printf("[cron-prompt] parse failed: %v", err)
		return
	}

	// Show parsed result with confirm/cancel buttons
	desc := heartbeat.DescribeSchedule(result.Schedule)
	msg := L.Getf("cron.prompt.confirm", result.Name, result.Schedule, desc, result.Prompt)

	// Store parsed data in button custom ID (JSON-encoded, compact)
	payload, _ := json.Marshal(result)
	confirmID := "cronp_confirm_" + string(payload)
	cancelID := "cronp_cancel"

	// Discord custom_id max is 100 chars. If too long, store in memory and use a short key.
	if len(confirmID) > 100 {
		key := fmt.Sprintf("%x", time.Now().UnixNano())
		b.cronPromptCache.Store(key, result)
		confirmID = "cronp_confirm_" + key
	}

	_, _ = ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: msg,
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "✅",
					Style:    discordgo.SuccessButton,
					CustomID: confirmID,
				},
				discordgo.Button{
					Label:    "❌",
					Style:    discordgo.DangerButton,
					CustomID: cancelID,
				},
			}},
		},
	})
}

// handleCronPromptButton handles confirm/cancel buttons from /cron-prompt.
func (b *Bot) handleCronPromptButton(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID

	if customID == "cronp_cancel" {
		_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{Content: "❌ " + L.Get("cancel.success"), Components: []discordgo.MessageComponent{}},
		})
		return
	}

	if !strings.HasPrefix(customID, "cronp_confirm_") {
		return
	}

	data := strings.TrimPrefix(customID, "cronp_confirm_")
	var result ParsedCronJob

	// Try JSON parse first (short payloads fit in custom_id)
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		// Lookup from cache
		if cached, ok := b.cronPromptCache.LoadAndDelete(data); ok {
			result = *cached.(*ParsedCronJob)
		} else {
			respondInteraction(ds, i, "❌ "+L.Get("error.expired"))
			return
		}
	}

	username := ""
	guildID := ""
	if i.Member != nil && i.Member.User != nil {
		username = i.Member.User.Username
	}
	if i.GuildID != "" {
		guildID = i.GuildID
	}

	job := &heartbeat.CronJob{
		Name:          result.Name,
		ChannelID:     i.ChannelID,
		GuildID:       guildID,
		Schedule:      result.Schedule,
		ScheduleHuman: result.Schedule,
		Prompt:        result.Prompt,
		HistoryLimit:  10,
		Enabled:       true,
		CreatedBy:     username,
	}
	if err := b.cronStore.Add(job); err != nil {
		respondInteraction(ds, i, L.Getf("error.save_failed", err.Error()))
		return
	}

	desc := heartbeat.DescribeSchedule(result.Schedule)
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    L.Getf("cron.prompt.created", result.Name, result.Schedule, desc),
			Components: []discordgo.MessageComponent{},
		},
	})
}

func init() {
	log.Println("[handler_cron] loaded")
}
