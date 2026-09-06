package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	L "github.com/nczz/kiro-discord-bot/locale"
)

type monitorAdapter struct {
	botNotifier
}

var _ heartbeat.MonitorDeps = (*monitorAdapter)(nil)

func (a *monitorAdapter) StartTempAgent(name, cwd, model, channelID string) (*acp.Agent, error) {
	resolvedModel, err := a.bot.manager.ResolveTempAgentModel(channelID, model)
	if err != nil {
		return nil, err
	}
	return a.bot.manager.StartTempAgent(name, cwd, resolvedModel, channelID)
}

func (a *monitorAdapter) StopTempAgent(agent *acp.Agent) {
	a.bot.manager.StopTempAgent(agent)
}

func (a *monitorAdapter) ChannelInitialized(channelID string) bool {
	return a.bot.manager.ChannelInitialized(channelID)
}

func (a *monitorAdapter) ChannelCWD(channelID string) string {
	return a.bot.manager.CWDPath(channelID)
}

func (a *monitorAdapter) EvaluateMonitor(ctx context.Context, agent *acp.Agent, job *heartbeat.MonitorJob, prompt string) (heartbeat.MonitorResult, error) {
	if agent == nil {
		return heartbeat.MonitorResult{}, fmt.Errorf("monitor agent is unavailable")
	}
	stateKey := agent.Name
	if strings.TrimSpace(stateKey) == "" {
		stateKey = "monitor-" + job.ID
	}
	if err := a.bot.manager.SetBotToolsTargetStateOptions(stateKey, job.ChannelID, channel.BotToolsTargetOptions{DisableEgress: true, Source: "monitor"}); err != nil {
		return heartbeat.MonitorResult{}, fmt.Errorf("disable monitor egress: %w", err)
	}
	defer a.bot.manager.ClearBotToolsTargetState(stateKey)

	type evalResult struct {
		result heartbeat.MonitorResult
		err    error
	}
	done := make(chan evalResult, 1)
	callbacks := acp.AsyncCallbacks{
		OnToolCall: func(evt acp.ToolCallEvent) {
			a.recordMonitorToolAuditEvent(agent, job, "", "monitor_tool_call", evt)
		},
		OnToolResult: func(evt acp.ToolCallEvent) {
			a.recordMonitorToolAuditEvent(agent, job, "", "monitor_tool_result", evt)
		},
		OnComplete: func(response string, askErr error) {
			if askErr != nil {
				done <- evalResult{err: askErr}
				return
			}
			result, err := heartbeat.ParseMonitorResult(response)
			done <- evalResult{result: result, err: err}
		},
	}
	agent.AskAsync(prompt, callbacks)
	select {
	case r := <-done:
		return r.result, r.err
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			agent.CancelPrompt()
		}
		return heartbeat.MonitorResult{}, ctx.Err()
	}
}

func (a *monitorAdapter) DeliverMonitorNotification(agent *acp.Agent, job *heartbeat.MonitorJob, result heartbeat.MonitorResult, manual bool, startedAt time.Time) (string, bool, error) {
	ds := a.bot.discord
	threadName := scheduledThreadName("🔎 ", job.Name)
	threadID, _, err := a.bot.prepareScheduledThread(ds, scheduledThreadRequest{
		ParentChannelID:  job.ChannelID,
		ExistingThreadID: job.ThreadID,
		CreatedByID:      job.CreatedByID,
		ThreadName:       threadName,
		Location:         a.bot.cronLocationOrLocal(),
	})
	if err != nil {
		return "", false, err
	}
	execTS := time.Now().In(a.bot.cronLocationOrLocal()).Format("01/02 15:04")
	_, _ = ds.ChannelEditComplex(threadID, &discordgo.ChannelEdit{Name: truncateDiscordThreadName(threadName + " · " + execTS)})
	reason := strings.TrimSpace(result.Reason)
	if reason == "" {
		reason = job.NotifyWhen
	}
	body := L.Getf("monitor.exec.condition_matched", reason)
	if result.VisibleMessage != "" {
		body += "\n\n" + result.VisibleMessage
	}
	if agent != nil {
		body = channel.AppendMetricsFooter(body, channel.MetricsWithElapsed(agent.TurnMetrics(), startedAt))
	}
	sentCount, sendErr := channel.SendLongThread(ds, threadID, body)
	if sendErr != nil {
		log.Printf("[monitor] failed to send monitor notification thread=%s: %v", threadID, sendErr)
	}
	if sentCount > 0 && sendErr == nil {
		a.Notify(job.ChannelID, L.Getf("monitor.exec.triggered_link", job.Name, monitorThreadLink(job.GuildID, threadID)))
	}
	return threadID, sentCount > 0 && sendErr == nil, sendErr
}

func monitorThreadLink(guildID, threadID string) string {
	guildID = strings.TrimSpace(guildID)
	threadID = strings.TrimSpace(threadID)
	if guildID != "" && threadID != "" {
		return fmt.Sprintf("https://discord.com/channels/%s/%s", guildID, threadID)
	}
	if threadID != "" {
		return fmt.Sprintf("<#%s>", threadID)
	}
	return ""
}

func (a *monitorAdapter) RecordMonitorUsage(agent *acp.Agent, job *heartbeat.MonitorJob, threadID, status string) {
	if agent == nil || job == nil {
		return
	}
	metrics := agent.TurnMetrics()
	model := job.Model
	if model == "" {
		model = agent.CurrentModelID()
	}
	usageStatus := "success"
	if status == heartbeat.MonitorStatusError {
		usageStatus = "error"
	}
	if err := a.bot.manager.RecordUsage(channel.UsageRecord{
		GuildID:       job.GuildID,
		ChannelID:     job.ChannelID,
		ThreadID:      threadID,
		UserID:        job.CreatedByID,
		Username:      job.CreatedBy,
		MessageID:     job.ID,
		Model:         model,
		Engine:        agent.Dialect().String(),
		Source:        "monitor",
		Status:        usageStatus,
		MeteringUsage: metrics.MeteringUsage,
		DurationMs:    metrics.TurnDurationMs,
		ContextUsage:  metrics.ContextUsage,
	}); err != nil {
		log.Printf("[usage] append monitor failed | job=%s user=%s err=%v", job.ID, job.CreatedByID, err)
	}
}

func (a *monitorAdapter) RecordMonitorResult(agent *acp.Agent, job *heartbeat.MonitorJob, threadID, status string, result heartbeat.MonitorResult, responseSent bool) {
	if a == nil || a.bot == nil || job == nil {
		return
	}
	eventType := "monitor_check_suppressed"
	if status == heartbeat.MonitorStatusMatched {
		eventType = "monitor_notification_sent"
	} else if status == heartbeat.MonitorStatusError {
		eventType = "monitor_check_failed"
	}
	model := job.Model
	if model == "" && agent != nil {
		model = agent.CurrentModelID()
	}
	targetID := threadID
	if targetID == "" {
		targetID = job.ChannelID
	}
	metadata := map[string]any{
		"monitor_job_name":  job.Name,
		"condition_matched": result.ConditionMatched,
		"response_sent":     responseSent,
		"reason":            result.Reason,
	}
	if agent != nil {
		for k, v := range channel.MetricsMetadata(agent.TurnMetrics()) {
			metadata[k] = v
		}
	}
	a.bot.recordBotAuditEvent(audit.BotEvent{
		Type:            eventType,
		GuildID:         job.GuildID,
		ChannelID:       job.ChannelID,
		TargetID:        targetID,
		ThreadID:        threadID,
		ParentChannelID: job.ChannelID,
		JobID:           job.ID,
		UserID:          job.CreatedByID,
		Username:        job.CreatedBy,
		Source:          "monitor",
		Status:          status,
		Content:         result.VisibleMessage,
		Model:           model,
		Metadata:        metadata,
	})
}

func (a *monitorAdapter) recordMonitorToolAuditEvent(agent *acp.Agent, job *heartbeat.MonitorJob, threadID, eventType string, evt acp.ToolCallEvent) {
	if a == nil || a.bot == nil || job == nil {
		return
	}
	model := job.Model
	if model == "" && agent != nil {
		model = agent.CurrentModelID()
	}
	targetID := threadID
	if targetID == "" {
		targetID = job.ChannelID
	}
	a.bot.recordBotAuditEvent(audit.BotEvent{
		Type:            eventType,
		GuildID:         job.GuildID,
		ChannelID:       job.ChannelID,
		TargetID:        targetID,
		ThreadID:        threadID,
		ParentChannelID: job.ChannelID,
		JobID:           job.ID,
		UserID:          job.CreatedByID,
		Username:        job.CreatedBy,
		Source:          "monitor",
		Status:          evt.Status,
		Command:         evt.Title,
		Model:           model,
		Metadata: map[string]any{
			"kind":             evt.Kind,
			"tool_call_id":     evt.ToolCallID,
			"monitor_job_name": job.Name,
		},
	})
}
