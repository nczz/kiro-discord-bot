package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/heartbeat"
)

// handleCronModal responds to /cron by showing a modal form.
func (b *Bot) handleCronModal(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "cron_add_modal",
			Title:    "新增排程任務",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "cron_name",
						Label:       "任務名稱",
						Style:       discordgo.TextInputShort,
						Placeholder: "例：每日伺服器健檢",
						Required:    true,
						MaxLength:   100,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "cron_schedule",
						Label:       "執行頻率",
						Style:       discordgo.TextInputShort,
						Placeholder: "例：每天 09:00、每 30 分鐘、0 9 * * *",
						Required:    true,
						MaxLength:   100,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "cron_prompt",
						Label:       "要 agent 做什麼",
						Style:       discordgo.TextInputParagraph,
						Placeholder: "例：檢查伺服器 CPU、記憶體、磁碟用量，跟上次比較",
						Required:    true,
						MaxLength:   2000,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "cron_cwd",
						Label:       "工作目錄（選填）",
						Style:       discordgo.TextInputShort,
						Placeholder: "例：/home/user/project",
						Required:    false,
						MaxLength:   200,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "cron_model",
						Label:       "Model（選填，留空用預設）",
						Style:       discordgo.TextInputShort,
						Placeholder: "例：claude-sonnet-4-20250514",
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
		respondInteraction(ds, i, "❌ 無法解析排程："+err.Error())
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
		respondInteraction(ds, i, "❌ 儲存失敗："+err.Error())
		return
	}

	respondInteraction(ds, i, fmt.Sprintf("✅ 排程任務已建立\n名稱：**%s**\n頻率：%s (`%s`)\nPrompt：%s",
		name, scheduleInput, cronExpr, prompt))
}

// handleCronList responds to /cron-list with a list of jobs and action buttons.
func (b *Bot) handleCronList(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	jobs := b.cronStore.ListByChannel(i.ChannelID)
	if len(jobs) == 0 {
		followupInteraction(ds, i, "📋 此頻道沒有排程任務。使用 `/cron` 新增。")
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

		lastRun := "無"
		if job.LastRun != "" {
			if t, err := time.Parse(time.RFC3339, job.LastRun); err == nil {
				lastRun = t.Format("01/02 15:04")
			}
		}
		nextRun := "計算中"
		if job.NextRun != "" {
			if t, err := time.Parse(time.RFC3339, job.NextRun); err == nil {
				nextRun = t.Format("01/02 15:04")
			}
		}

		content := fmt.Sprintf("%s **%s**\n頻率：%s | 上次：%s | 下次：%s\nPrompt：%s",
			status, job.Name, job.ScheduleHuman, lastRun, nextRun, truncate(job.Prompt, 100))

		// Build buttons
		var buttons []discordgo.MessageComponent
		if job.Enabled {
			buttons = append(buttons, discordgo.Button{
				Label:    "⏸️ 暫停",
				Style:    discordgo.SecondaryButton,
				CustomID: "cron_pause_" + job.ID,
			})
		} else {
			buttons = append(buttons, discordgo.Button{
				Label:    "▶️ 恢復",
				Style:    discordgo.SuccessButton,
				CustomID: "cron_resume_" + job.ID,
			})
		}
		buttons = append(buttons,
			discordgo.Button{
				Label:    "▶️ 立即執行",
				Style:    discordgo.PrimaryButton,
				CustomID: "cron_run_" + job.ID,
			},
			discordgo.Button{
				Label:    "🗑️ 刪除",
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
	for _, prefix := range []string{"cron_pause_", "cron_resume_", "cron_run_", "cron_delete_"} {
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
		respondInteraction(ds, i, "❌ 找不到此任務")
		return
	}

	switch action {
	case "pause":
		job.Enabled = false
		_ = b.cronStore.Update(job)
		respondInteraction(ds, i, fmt.Sprintf("⏸️ 已暫停：**%s**", job.Name))
	case "resume":
		job.Enabled = true
		_ = b.cronStore.Update(job)
		respondInteraction(ds, i, fmt.Sprintf("▶️ 已恢復：**%s**", job.Name))
	case "run":
		respondInteraction(ds, i, fmt.Sprintf("⏰ 正在手動執行：**%s**", job.Name))
		// Trigger execution in background — set NextRun to now so next heartbeat picks it up
		job.NextRun = time.Now().Add(-time.Minute).Format(time.RFC3339)
		_ = b.cronStore.Update(job)
	case "delete":
		_ = b.cronStore.Remove(jobID)
		respondInteraction(ds, i, fmt.Sprintf("🗑️ 已刪除：**%s**", job.Name))
	}
}

// handleCronRun handles /cron-run <name>
func (b *Bot) handleCronRun(ds *discordgo.Session, i *discordgo.InteractionCreate, name string) {
	job, ok := b.cronStore.FindByName(i.ChannelID, name)
	if !ok {
		respondInteraction(ds, i, "❌ 找不到任務："+name)
		return
	}
	respondInteraction(ds, i, fmt.Sprintf("⏰ 正在手動執行：**%s**", job.Name))
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
			ds.ChannelMessageSend(channelID, "📋 此頻道沒有排程任務。")
			return
		}
		var sb strings.Builder
		sb.WriteString("📋 **排程任務列表**\n\n")
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
			ds.ChannelMessageSend(channelID, "❌ 找不到任務："+name)
			return
		}
		ds.ChannelMessageSend(channelID, fmt.Sprintf("⏰ 正在手動執行：**%s**", job.Name))
		job.NextRun = time.Now().Add(-time.Minute).Format(time.RFC3339)
		_ = b.cronStore.Update(job)

	default:
		ds.ChannelMessageSend(channelID, "Usage: `!cron list` | `!cron run <name>`")
	}
}

// handleRemind handles /remind slash command.
func (b *Bot) handleRemind(ds *discordgo.Session, i *discordgo.InteractionCreate, timeStr, content string) {
	loc := time.Now().Location()
	if b.cronTimezone != "" {
		if l, err := time.LoadLocation(b.cronTimezone); err == nil {
			loc = l
		}
	}

	target, err := heartbeat.ParseTime(timeStr, loc)
	if err != nil {
		respondInteraction(ds, i, "❌ 無法解析時間："+err.Error())
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
		Name:          fmt.Sprintf("提醒：%s", truncate(content, 30)),
		ChannelID:     i.ChannelID,
		GuildID:       guildID,
		Prompt:        content,
		OneShot:       true,
		MentionID:     userID,
		Enabled:       true,
		CreatedBy:     username,
		NextRun:       target.Format(time.RFC3339),
		ScheduleHuman: timeStr,
		HistoryLimit:  0,
	}
	if err := b.cronStore.Add(job); err != nil {
		respondInteraction(ds, i, "❌ 儲存失敗："+err.Error())
		return
	}

	respondInteraction(ds, i, fmt.Sprintf("🔔 已預約提醒\n時間：%s\n內容：%s",
		target.Format("2006/01/02 15:04"), content))
}

// handleRemindText handles !remind text command.
func (b *Bot) handleRemindText(ds *discordgo.Session, channelID, guildID, userID, username, content string) {
	// Parse: !remind <time> <content>
	// Find first space after time portion
	parts := strings.SplitN(content, " ", 2)
	if len(parts) < 2 {
		ds.ChannelMessageSend(channelID, "Usage: `!remind <時間> <內容>`\n例：`!remind 下午五點 提醒我要開會`")
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
		ds.ChannelMessageSend(channelID, "❌ 無法解析時間或內容為空\nUsage: `!remind <時間> <內容>`")
		return
	}

	job := &heartbeat.CronJob{
		Name:          fmt.Sprintf("提醒：%s", truncate(prompt, 30)),
		ChannelID:     channelID,
		GuildID:       guildID,
		Prompt:        prompt,
		OneShot:       true,
		MentionID:     userID,
		Enabled:       true,
		CreatedBy:     username,
		NextRun:       target.Format(time.RFC3339),
		ScheduleHuman: timeStr,
		HistoryLimit:  0,
	}
	if err := b.cronStore.Add(job); err != nil {
		ds.ChannelMessageSend(channelID, "❌ 儲存失敗："+err.Error())
		return
	}

	ds.ChannelMessageSend(channelID, fmt.Sprintf("🔔 已預約提醒\n時間：%s\n內容：%s",
		target.Format("2006/01/02 15:04"), prompt))
}

func init() {
	log.Println("[handler_cron] loaded")
}
