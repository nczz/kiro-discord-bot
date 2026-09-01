package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func (b *Bot) cmdWebShareCronList(ctx cmdCtx) {
	if b == nil || b.cronStore == nil {
		ctx.reply(L.Get("cron.list.empty"))
		return
	}
	jobs := b.cronStore.ListByChannel(ctx.channelID)
	if len(jobs) == 0 {
		ctx.reply(L.Get("cron.list.empty"))
		return
	}
	var sb strings.Builder
	sb.WriteString(L.Get("cron.list.header"))
	for i, job := range jobs {
		status := "enabled"
		if !job.Enabled {
			status = "paused"
		}
		if job.OneShot {
			status = "one-shot"
		}
		sb.WriteString(fmt.Sprintf("%d. %s **%s** — %s\n   Prompt: %s\n", i+1, status, job.Name, job.ScheduleHuman, truncate(job.Prompt, 80)))
	}
	ctx.reply(sb.String())
}

func (b *Bot) cmdWebShareCronRun(ctx cmdCtx) {
	if b == nil || b.cronStore == nil || b.cronTask == nil {
		ctx.reply(L.Get("cron.list.empty"))
		return
	}
	name := strings.TrimSpace(ctx.args)
	if name == "" {
		ctx.reply(L.Get("cron.usage"))
		return
	}
	if !b.manager.ChannelInitialized(ctx.channelID) {
		ctx.reply(L.Getf("setup.required.command", "/cron-run"))
		return
	}
	job, ok := b.cronStore.FindByName(ctx.channelID, name)
	if !ok {
		ctx.reply(L.Getf("cron.not_found", name))
		return
	}
	ctx.reply(L.Getf("cron.running", job.Name))
	job.RunOnce = true
	_ = b.cronStore.Update(job)
	b.cronTask.RunNow(job.ID)
}

func (b *Bot) cmdWebShareRemind(ctx cmdCtx) {
	if b == nil || b.cronStore == nil {
		ctx.reply(L.Get("remind.usage"))
		return
	}
	content := strings.TrimSpace(ctx.args)
	useAgent := false
	if strings.HasPrefix(content, "--agent ") {
		useAgent = true
		content = strings.TrimSpace(strings.TrimPrefix(content, "--agent "))
	}
	if useAgent && !b.manager.ChannelInitialized(ctx.channelID) {
		ctx.reply(L.Getf("setup.required.command", "!remind --agent"))
		return
	}
	words := strings.Fields(content)
	var target time.Time
	var timeStr, prompt string
	loc := time.Now().Location()
	if b.cronTimezone != "" {
		if l, err := time.LoadLocation(b.cronTimezone); err == nil {
			loc = l
		}
	}
	for i := 1; i <= len(words) && i <= 3; i++ {
		candidate := strings.Join(words[:i], " ")
		if t, err := heartbeat.ParseTime(candidate, loc); err == nil {
			target = t
			timeStr = candidate
			prompt = strings.TrimSpace(strings.Join(words[i:], " "))
		}
	}
	if prompt == "" {
		ctx.reply(L.Get("error.parse_time_or_empty"))
		return
	}
	job := &heartbeat.CronJob{Name: L.Getf("remind.name_prefix", truncate(prompt, 30)), ChannelID: ctx.channelID, GuildID: ctx.guildID, Prompt: prompt, OneShot: true, UseAgent: useAgent, MentionID: ctx.userID, Enabled: true, CreatedBy: ctx.username, CreatedByID: ctx.userID, NextRun: target.Format(time.RFC3339), ScheduleHuman: timeStr}
	if err := b.cronStore.Add(job); err != nil {
		ctx.reply(L.Getf("error.save_failed", err.Error()))
		return
	}
	ctx.reply(L.Getf("remind.created", target.Format("2006/01/02 15:04"), prompt))
}

func (b *Bot) cmdWebShareUsageHistory(ctx cmdCtx) {
	if b == nil || b.manager == nil {
		ctx.reply(L.Get("usage.history.empty"))
		return
	}
	now := time.Now().In(b.manager.UsageLocation())
	state := &usageHistoryState{RequesterID: ctx.userID, TargetID: ctx.userID, GuildID: ctx.guildID, Period: "30d", Status: "all", Source: "all", From: now.AddDate(0, 0, -30), To: now, Cursors: []usageHistoryCursor{{}}, Page: 0, Expires: time.Now().Add(15 * time.Minute)}
	content, _, err := b.renderUsageHistory(state, uuid.NewString())
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	ctx.reply(content)
}
