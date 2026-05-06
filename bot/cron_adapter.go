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
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	L "github.com/nczz/kiro-discord-bot/locale"
)

type cronAdapter struct {
	botNotifier
}

var _ heartbeat.CronDeps = (*cronAdapter)(nil)

func (a *cronAdapter) StartTempAgent(name, cwd, model string) (*acp.Agent, error) {
	return a.bot.manager.StartTempAgent(name, cwd, model)
}

func (a *cronAdapter) StopTempAgent(agent *acp.Agent) {
	a.bot.manager.StopTempAgent(agent)
}

func (a *cronAdapter) AskAgentInThread(ctx context.Context, agent *acp.Agent, channelID, threadName, existingThreadID, prompt, mentionID string) (string, string, error) {
	ds := a.bot.discord
	archiveDur := a.bot.manager.ThreadArchive()
	if archiveDur <= 0 {
		archiveDur = 1440
	}

	// Try to reuse existing thread
	threadID := existingThreadID
	if threadID != "" {
		// Test if thread is still accessible by sending the run separator
		sep := fmt.Sprintf("── %s ──", time.Now().Format("01/02 15:04"))
		if _, err := ds.ChannelMessageSend(threadID, sep); err != nil {
			// Thread gone or archived — try to unarchive
			if _, uerr := ds.ChannelEditComplex(threadID, &discordgo.ChannelEdit{
				Archived: boolPtr(false),
				Locked:   boolPtr(false),
			}); uerr != nil {
				threadID = "" // give up, create new
			} else if _, err2 := ds.ChannelMessageSend(threadID, sep); err2 != nil {
				threadID = "" // still can't send, create new
			}
		}
	}

	if threadID == "" {
		// Create new thread
		thread, err := ds.ThreadStart(channelID, threadName, discordgo.ChannelTypeGuildPublicThread, archiveDur)
		if err != nil {
			return "", "", fmt.Errorf("create thread: %w", err)
		}
		threadID = thread.ID
		// Post initial separator for new thread
		ds.ChannelMessageSend(threadID, fmt.Sprintf("── %s ──", time.Now().Format("01/02 15:04")))
	}

	// Post initial status — save message ID for progress updates
	statusMsg, _ := ds.ChannelMessageSend(threadID, "🔄 "+L.Get("worker.processing"))
	var statusMsgID string
	if statusMsg != nil {
		statusMsgID = statusMsg.ID
	}

	startTime := time.Now()
	var toolCount atomic.Int32

	// Use done channel to block until completion
	type result struct {
		response string
		err      error
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
		ds.ChannelMessageEdit(threadID, statusMsgID, msg)
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
				ds.ChannelMessageSend(threadID, icon+" "+title)
			} else {
				msg := icon + " " + title
				if len(evt.Locations) > 0 {
					files := make([]string, 0, len(evt.Locations))
					for _, loc := range evt.Locations {
						files = append(files, "`"+loc.Path+"`")
					}
					msg += "\n📁 " + strings.Join(files, ", ")
				}
				ds.ChannelMessageSend(threadID, msg)
			}
		},
		OnToolResult: func(evt acp.ToolCallEvent) {
			if isSilent() {
				if evt.Status == "failed" {
					ds.ChannelMessageSend(threadID, "❌ "+evt.Title)
				}
				return
			}
			if evt.RawOutput != "" && evt.Status == "completed" {
				out := evt.RawOutput
				if len(out) > 1900 {
					out = out[:1900] + L.Get("tool.output_truncated")
				}
				ds.ChannelMessageSend(threadID, "```\n"+out+"\n```")
			} else if evt.Status == "failed" {
				msg := "❌ " + evt.Title
				if evt.RawOutput != "" {
					o := evt.RawOutput
					if len(o) > 500 {
						o = o[:500] + "..."
					}
					msg += "\n```\n" + o + "\n```"
				}
				ds.ChannelMessageSend(threadID, msg)
			}
		},
		OnThought: func(text string) {
			if isSilent() {
				return
			}
			if len(text) > 1900 {
				text = text[:1900] + "…"
			}
			ds.ChannelMessageSend(threadID, "💭 "+text)
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
					ds.ChannelMessageEdit(threadID, statusMsgID, L.Getf("cron.progress.failed", elapsed, count))
				}
				// Update thread title with last run timestamp
				ts := time.Now().Format("01/02 15:04")
				ds.ChannelEditComplex(threadID, &discordgo.ChannelEdit{
					Name: threadName + " · " + ts,
				})
				ds.ChannelMessageSend(threadID, "❌ "+errMsg)
				done <- result{"", askErr}
				return
			}
			if response == "" {
				response = L.Get("worker.empty_response")
			}
			// Update status to done
			if statusMsgID != "" {
				ds.ChannelMessageEdit(threadID, statusMsgID, L.Getf("cron.progress.done", elapsed, count))
			}
			// Update thread title with last run timestamp
			ts := time.Now().Format("01/02 15:04")
			ds.ChannelEditComplex(threadID, &discordgo.ChannelEdit{
				Name: threadName + " · " + ts,
			})
			// Mention user before response if configured
			if mentionID != "" {
				ds.ChannelMessageSend(threadID, fmt.Sprintf("<@%s>", mentionID))
			}
			channel.SendLongThread(ds, threadID, response)
			done <- result{response, nil}
		},
	}

	// Watch for context cancellation (timeout)
	go func() {
		<-ctx.Done()
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[cron-adapter] timeout, sending cancel")
			agent.CancelPrompt()
		}
	}()

	agent.AskAsync(prompt, callbacks)

	// Block until complete
	r := <-done
	return r.response, threadID, r.err
}

func boolPtr(b bool) *bool { return &b }
