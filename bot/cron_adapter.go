package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/internal/textutil"
	L "github.com/nczz/kiro-discord-bot/locale"
)

type cronAdapter struct {
	botNotifier
}

var _ heartbeat.CronDeps = (*cronAdapter)(nil)

func (a *cronAdapter) StartTempAgent(name, cwd, model, channelID string) (*acp.Agent, error) {
	return a.bot.manager.StartTempAgent(name, cwd, model, channelID)
}

func (a *cronAdapter) StopTempAgent(agent *acp.Agent) {
	a.bot.manager.StopTempAgent(agent)
}

func (a *cronAdapter) ChannelInitialized(channelID string) bool {
	return a.bot.manager.ChannelInitialized(channelID)
}

func (a *cronAdapter) ChannelCWD(channelID string) string {
	return a.bot.manager.CWDPath(channelID)
}

func (a *cronAdapter) RecordAgentUsage(agent *acp.Agent, job *heartbeat.CronJob, threadID, status string) {
	if agent == nil || job == nil {
		return
	}
	metrics := agent.TurnMetrics()
	source := "cron"
	if job.OneShot {
		source = "reminder"
	}
	model := job.Model
	if model == "" {
		model = agent.CurrentModelID()
	}
	userID := job.CreatedByID
	if userID == "" {
		userID = job.MentionID
	}
	if err := a.bot.manager.RecordUsage(channel.UsageRecord{
		GuildID:       job.GuildID,
		ChannelID:     job.ChannelID,
		ThreadID:      threadID,
		UserID:        userID,
		Username:      job.CreatedBy,
		MessageID:     job.ID,
		Model:         model,
		Source:        source,
		Status:        status,
		MeteringUsage: metrics.MeteringUsage,
		DurationMs:    metrics.TurnDurationMs,
		ContextUsage:  metrics.ContextUsage,
	}); err != nil {
		log.Printf("[usage] append cron failed | job=%s user=%s err=%v", job.ID, userID, err)
	}
}

func (a *cronAdapter) RecordAgentResponse(agent *acp.Agent, job *heartbeat.CronJob, threadID, status, content string, responseSent bool) {
	if a == nil || a.bot == nil || agent == nil || job == nil {
		return
	}
	metrics := agent.TurnMetrics()
	model := job.Model
	if model == "" {
		model = agent.CurrentModelID()
	}
	userID := job.CreatedByID
	if userID == "" {
		userID = job.MentionID
	}
	source := "cron"
	if job.OneShot {
		source = "reminder"
	}
	targetID := threadID
	if targetID == "" {
		targetID = job.ChannelID
	}
	metadata := channel.MetricsMetadata(metrics)
	metadata["content_len"] = len(content)
	metadata["cron_job_name"] = job.Name
	metadata["response_sent"] = responseSent
	eventType := "agent_response_sent"
	if !responseSent {
		eventType = "agent_response_failed"
		failureStage := "response_delivery"
		if threadID == "" {
			failureStage = "delivery_setup"
		}
		metadata["failure_stage"] = failureStage
	}
	a.bot.recordBotAuditEvent(audit.BotEvent{
		Type:            eventType,
		GuildID:         job.GuildID,
		ChannelID:       job.ChannelID,
		TargetID:        targetID,
		ThreadID:        threadID,
		ParentChannelID: job.ChannelID,
		JobID:           job.ID,
		UserID:          userID,
		Username:        job.CreatedBy,
		Source:          source,
		Status:          status,
		Content:         content,
		Model:           model,
		Metadata:        metadata,
	})
}

func (a *cronAdapter) AskAgentInThread(ctx context.Context, agent *acp.Agent, channelID, threadName, existingThreadID, prompt, mentionID, createdByID string) (string, string, bool, error) {
	ds := a.bot.discord
	loc := a.bot.cronLocationOrLocal()
	archiveDur := a.bot.manager.ThreadArchive()
	if archiveDur <= 0 {
		archiveDur = 1440
	}

	// Try to reuse existing thread
	threadID := existingThreadID
	if threadID != "" {
		// Test if thread is still accessible by sending the run separator
		sep := fmt.Sprintf("── %s ──", time.Now().In(loc).Format("01/02 15:04"))
		if _, err := sendDiscordText(ds, threadID, sep, nil); err != nil {
			// Thread gone or archived — try to unarchive
			if _, uerr := ds.ChannelEditComplex(threadID, &discordgo.ChannelEdit{
				Archived: boolPtr(false),
				Locked:   boolPtr(false),
			}); uerr != nil {
				threadID = "" // give up, create new
			} else if _, err2 := sendDiscordText(ds, threadID, sep, nil); err2 != nil {
				threadID = "" // still can't send, create new
			}
		}
	}

	if threadID == "" {
		// Create new thread
		thread, err := ds.ThreadStart(channelID, threadName, discordgo.ChannelTypeGuildPublicThread, archiveDur)
		if err != nil {
			return "", "", false, fmt.Errorf("create thread: %w", err)
		}
		threadID = thread.ID
		// Post initial separator for new thread
		_, _ = sendDiscordText(ds, threadID, fmt.Sprintf("── %s ──", time.Now().In(loc).Format("01/02 15:04")), nil)
	}

	targetStateKey := channelID
	if agent != nil && strings.TrimSpace(agent.Name) != "" {
		targetStateKey = agent.Name
	}
	if err := a.bot.manager.SetBotToolsTargetState(targetStateKey, threadID); err != nil {
		log.Printf("[cron-adapter] write bot-tools target state key=%s channel=%s target=%s: %v", targetStateKey, channelID, threadID, err)
	}
	defer a.bot.manager.ClearBotToolsTargetState(targetStateKey)

	// Add creator to thread so they get notifications
	if createdByID != "" {
		_ = ds.ThreadMemberAdd(threadID, createdByID)
	}

	// Update thread title with execution start timestamp
	execTS := time.Now().In(loc).Format("01/02 15:04")
	ds.ChannelEditComplex(threadID, &discordgo.ChannelEdit{
		Name: threadName + " · " + execTS,
	})

	// Notify channel with thread link
	a.Notify(channelID, L.Getf("cron.exec.running_link", threadName, threadID))

	// Post initial status — save message ID for progress updates
	statusMsg, _ := channel.SendProcessMessage(ds, threadID, "🔄 "+L.Get("worker.processing"))
	var statusMsgID string
	if statusMsg != nil {
		statusMsgID = statusMsg.ID
	}

	startTime := time.Now()
	var toolCount atomic.Int32

	// Use done channel to block until completion
	type result struct {
		response     string
		responseSent bool
		err          error
	}
	done := make(chan result, 1)

	isSilent := func() bool { return a.bot.manager.IsSilent(channelID) }

	updateProgress := func() {
		if statusMsgID == "" {
			return
		}
		elapsed := int(time.Since(startTime).Seconds())
		count := toolCount.Load()
		msg := L.Getf("cron.progress", L.Get("worker.processing"), elapsed, count)
		_, _ = editDiscordText(ds, threadID, statusMsgID, msg)
	}

	callbacks := acp.AsyncCallbacks{
		OnToolCall: func(evt acp.ToolCallEvent) {
			toolCount.Add(1)
			updateProgress()

			title := evt.Title
			if title == "" {
				title = L.Get("worker.tool_fallback")
			}
			icon := channel.ToolKindIcon(evt.Kind)
			if isSilent() {
				channel.SendProcessMessage(ds, threadID, channel.CompactToolStartMessage(icon, evt))
			} else {
				msg := icon + " " + channel.EscapeDiscordMarkdown(title)
				if len(evt.Locations) > 0 {
					files := make([]string, 0, len(evt.Locations))
					for _, loc := range evt.Locations {
						files = append(files, "`"+loc.Path+"`")
					}
					msg += "\n📁 " + strings.Join(files, ", ")
				}
				channel.SendProcessMessage(ds, threadID, msg)
			}
		},
		OnToolResult: func(evt acp.ToolCallEvent) {
			if isSilent() {
				if evt.Status == "failed" {
					channel.SendProcessMessage(ds, threadID, "❌ "+channel.EscapeDiscordMarkdown(evt.Title))
				}
				return
			}
			if evt.RawOutput != "" && evt.Status == "completed" {
				out := evt.RawOutput
				if len(out) > 1900 {
					out = textutil.TruncateUTF8Bytes(out, 1900) + L.Get("tool.output_truncated")
				}
				channel.SendProcessMessage(ds, threadID, "```\n"+out+"\n```")
			} else if evt.Status == "failed" {
				msg := "❌ " + channel.EscapeDiscordMarkdown(evt.Title)
				if evt.RawOutput != "" {
					o := evt.RawOutput
					if len(o) > 500 {
						o = textutil.TruncateUTF8Bytes(o, 500) + "..."
					}
					msg += "\n```\n" + o + "\n```"
				}
				channel.SendProcessMessage(ds, threadID, msg)
			}
		},
		OnThought: func(text string) {
			if isSilent() {
				return
			}
			if len(text) > 1900 {
				text = textutil.TruncateUTF8Bytes(text, 1900) + "…"
			}
			channel.SendProcessMessage(ds, threadID, "💭 "+channel.EscapeDiscordMarkdown(text))
		},
		OnComplete: func(response string, askErr error) {
			elapsed := int(time.Since(startTime).Seconds())
			count := toolCount.Load()

			if askErr != nil {
				errMsg := askErr.Error()
				if ctx.Err() == context.DeadlineExceeded {
					errMsg = L.Get("worker.timeout.short")
				}
				if statusMsgID != "" {
					_, _ = editDiscordText(ds, threadID, statusMsgID, L.Getf("cron.progress.failed", elapsed, count))
				}
				a.drainSafeEgress(threadID)
				sentCount, sendErr := channel.SendLongThread(ds, threadID, channel.AppendMetricsFooter("❌ "+errMsg, agent.TurnMetrics()))
				if sendErr != nil {
					log.Printf("[cron] failed to send agent error response thread=%s: %v", threadID, sendErr)
				}
				done <- result{responseSent: sentCount > 0 && sendErr == nil, err: askErr}
				return
			}
			if response == "" {
				response = L.Get("worker.empty_response")
			}
			// Update status to done
			if statusMsgID != "" {
				_, _ = editDiscordText(ds, threadID, statusMsgID, L.Getf("cron.progress.done", elapsed, count))
			}
			// Mention user before response if configured
			if mentionID != "" {
				_, _ = ds.ChannelMessageSendComplex(threadID, &discordgo.MessageSend{
					Content:         fmt.Sprintf("<@%s>", mentionID),
					AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{mentionID}},
					Flags:           discordgo.MessageFlagsSuppressEmbeds,
				})
			}
			a.drainSafeEgress(threadID)
			sentCount, sendErr := channel.SendLongThread(ds, threadID, channel.AppendMetricsFooter(response, agent.TurnMetrics()))
			if sendErr != nil {
				log.Printf("[cron] failed to send agent response thread=%s: %v", threadID, sendErr)
			}
			done <- result{response: response, responseSent: sentCount > 0 && sendErr == nil}
		},
	}

	agent.AskAsync(prompt, callbacks)

	// Block until complete
	select {
	case r := <-done:
		return r.response, threadID, r.responseSent, r.err
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[cron-adapter] timeout, sending cancel")
			agent.CancelPrompt()
		}
		errMsg := ctx.Err().Error()
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = L.Get("worker.timeout.short")
		}
		elapsed := int(time.Since(startTime).Seconds())
		count := toolCount.Load()
		if statusMsgID != "" {
			_, _ = editDiscordText(ds, threadID, statusMsgID, L.Getf("cron.progress.failed", elapsed, count))
		}
		a.drainSafeEgress(threadID)
		sentCount, sendErr := channel.SendLongThread(ds, threadID, channel.AppendMetricsFooter("❌ "+errMsg, agent.TurnMetrics()))
		if sendErr != nil {
			log.Printf("[cron] failed to send agent timeout response thread=%s: %v", threadID, sendErr)
		}
		return "", threadID, sentCount > 0 && sendErr == nil, ctx.Err()
	}
}

func (a *cronAdapter) drainSafeEgress(threadID string) {
	if a == nil || a.bot == nil || a.bot.safeEgress == nil {
		return
	}
	a.bot.safeEgress.DrainChannel(threadID)
}

func boolPtr(b bool) *bool { return &b }
