package channel

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	L "github.com/nczz/kiro-discord-bot/locale"
)

// Job represents a single user message to be processed.
type Job struct {
	ChannelID   string
	MessageID   string
	Prompt      string
	Session     *discordgo.Session
	UserID      string
	Username    string
	Attachments []string
	ThreadID    string // non-empty = follow-up in existing thread, skip thread creation
}

// Worker manages a per-channel job queue and executes jobs sequentially.
type Worker struct {
	channelID       string
	agent           *acp.Agent
	queue           chan *Job
	askTimeoutSec   int
	streamUpdateSec int
	threadArchive   int // auto-archive duration in minutes
	stopCh          chan struct{}
	idleCh          chan struct{} // signaled when agent finishes a task
	stopped         sync.Once
	started         sync.Once
	logger          *ChatLogger
	model           string

	cancelMu sync.Mutex
	cancelFn context.CancelFunc

	// Current thread info (protected by cancelMu)
	currentThreadID string

	onActivity func() // called during work to signal liveness (prevents idle cleanup)

	historyPrefix string // prepended to first job's prompt, then cleared
}

func NewWorker(channelID string, agent *acp.Agent, bufSize, askTimeoutSec, streamUpdateSec, threadArchive int, logger *ChatLogger, model string) *Worker {
	return &Worker{
		channelID:       channelID,
		agent:           agent,
		queue:           make(chan *Job, bufSize),
		askTimeoutSec:   askTimeoutSec,
		streamUpdateSec: streamUpdateSec,
		threadArchive:   threadArchive,
		stopCh:          make(chan struct{}),
		idleCh:          make(chan struct{}, 1),
		logger:          logger,
		model:           model,
	}
}

// OnActivityFunc sets a callback invoked during work to signal liveness.
func (w *Worker) OnActivityFunc(fn func()) { w.onActivity = fn }

// SetHistoryPrefix sets conversation history to prepend to the first job's prompt.
func (w *Worker) SetHistoryPrefix(s string) { w.historyPrefix = s }

func (w *Worker) Enqueue(job *Job) error {
	select {
	case w.queue <- job:
		return nil
	default:
		return fmt.Errorf("queue full")
	}
}

func (w *Worker) QueueLen() int {
	return len(w.queue)
}

func (w *Worker) Start() {
	w.started.Do(func() {
		// Pre-fill idle so first job doesn't wait
		select {
		case w.idleCh <- struct{}{}:
		default:
		}
		go w.run()
	})
}

func (w *Worker) Stop() {
	w.stopped.Do(func() {
		close(w.stopCh)
	})
}

// CancelCurrent cancels the currently running job, if any.
func (w *Worker) CancelCurrent() {
	w.cancelMu.Lock()
	fn := w.cancelFn
	threadID := w.currentThreadID
	w.cancelMu.Unlock()
	if fn != nil {
		fn()
	}
	// Signal idle so next job can proceed
	if threadID != "" {
		w.signalIdle()
	}
}

// CurrentThreadID returns the thread ID of the currently running task.
func (w *Worker) CurrentThreadID() string {
	w.cancelMu.Lock()
	defer w.cancelMu.Unlock()
	return w.currentThreadID
}

func (w *Worker) signalIdle() {
	select {
	case w.idleCh <- struct{}{}:
	default:
	}
}

func (w *Worker) waitIdle() bool {
	select {
	case <-w.idleCh:
		return true
	case <-w.stopCh:
		return false
	}
}

func (w *Worker) run() {
	for {
		select {
		case <-w.stopCh:
			return
		case job := <-w.queue:
			if !w.waitIdle() {
				return
			}
			w.execute(job)
		}
	}
}

func promptSummary(prompt string, maxLen int) string {
	// Strip Discord context prefix
	if idx := strings.Index(prompt, "\n\n"); idx > 0 && idx < 120 {
		prompt = prompt[idx+2:]
	}
	if len(prompt) > maxLen {
		return truncateUTF8(prompt, maxLen-3) + "..."
	}
	return prompt
}

func (w *Worker) execute(job *Job) {
	ds := job.Session
	startTime := time.Now()

	log.Printf("[worker %s] job start | user=%s(%s) msg=%s prompt=%q",
		w.channelID, job.Username, job.UserID, job.MessageID, promptSummary(job.Prompt, 80))

	if w.logger != nil {
		w.logger.Log(w.channelID, ChatEntry{
			Role:        "user",
			UserID:      job.UserID,
			Username:    job.Username,
			MessageID:   job.MessageID,
			Content:     job.Prompt,
			Attachments: job.Attachments,
		})
	}

	swapReaction(ds, job.ChannelID, job.MessageID, "⏳", "🔄")

	// Determine thread ID: reuse existing or create new
	var threadID string
	if job.ThreadID != "" {
		// Thread follow-up: post directly to existing thread
		threadID = job.ThreadID
	} else {
		// Create thread from user's message
		threadName := job.Prompt
		if len(threadName) > 95 {
			threadName = truncateUTF8(threadName, 92) + "..."
		}
		// Strip Discord context prefix for cleaner thread name
		if idx := strings.Index(threadName, "\n\n"); idx > 0 && idx < 80 {
			threadName = threadName[idx+2:]
			if len(threadName) > 95 {
				threadName = truncateUTF8(threadName, 92) + "..."
			}
		}
		if threadName == "" {
			threadName = "Task"
		}

		archiveDur := w.threadArchive
		if archiveDur <= 0 {
			archiveDur = 1440
		}
		thread, err := ds.MessageThreadStart(job.ChannelID, job.MessageID, threadName, archiveDur)
		if err != nil {
			log.Printf("[worker %s] create thread: %v, falling back to sync", w.channelID, err)
			w.executeFallback(job)
			return
		}
		threadID = thread.ID
	}

	w.cancelMu.Lock()
	w.currentThreadID = threadID
	w.cancelMu.Unlock()

	// Post initial status in thread
	ds.ChannelMessageSend(threadID, "🔄 "+L.Get("worker.processing"))

	// Setup timeout context as safety net
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(w.askTimeoutSec)*time.Second)
	w.cancelMu.Lock()
	w.cancelFn = cancel
	w.cancelMu.Unlock()

	// Async callbacks — all post to thread
	callbacks := acp.AsyncCallbacks{
		OnToolCall: func(evt acp.ToolCallEvent) {
			if w.onActivity != nil {
				w.onActivity()
			}
			title := evt.Title
			if title == "" {
				title = "tool"
			}
			// Show kind icon + title + affected files
			icon := toolKindIcon(evt.Kind)
			msg := icon + " " + title
			if len(evt.Locations) > 0 {
				files := make([]string, 0, len(evt.Locations))
				for _, loc := range evt.Locations {
					files = append(files, "`"+loc.Path+"`")
				}
				msg += "\n📁 " + strings.Join(files, ", ")
			}
			ds.ChannelMessageSend(threadID, msg)
			swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "⚙️")
		},
		OnToolResult: func(evt acp.ToolCallEvent) {
			if w.onActivity != nil {
				w.onActivity()
			}
			swapReaction(ds, job.ChannelID, job.MessageID, "⚙️", "🔄")
			// Send tool output to thread if meaningful
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
			// Accumulate thought chunks — send as a single collapsed block would be ideal,
			// but Discord doesn't support spoiler streaming. Just prefix with 💭.
			if len(text) > 1900 {
				text = text[:1900] + "…"
			}
			ds.ChannelMessageSend(threadID, "💭 "+text)
		},
		OnComplete: func(response string, askErr error) {
			// Capture ctx state BEFORE cancel() — cancel() sets ctx.Err() to Canceled
			ctxErr := ctx.Err()
			cancel() // release timeout context

			w.cancelMu.Lock()
			w.cancelFn = nil
			w.currentThreadID = ""
			w.cancelMu.Unlock()

			if askErr != nil {
				errMsg := askErr.Error()
				emoji := "❌"
				if ctxErr == context.DeadlineExceeded {
					errMsg = L.Getf("worker.timeout", w.askTimeoutSec)
					emoji = "⚠️"
				} else if ctxErr == context.Canceled {
					errMsg = L.Get("cancel.success")
					emoji = "⚠️"
				}
				log.Printf("[worker %s] job error | user=%s msg=%s elapsed=%s ctxErr=%v err=%v",
					w.channelID, job.Username, job.MessageID, time.Since(startTime).Round(time.Millisecond), ctxErr, askErr)
				ds.ChannelMessageSend(threadID, "❌ "+errMsg)
				swapReaction(ds, job.ChannelID, job.MessageID, "🔄", emoji)
				swapReaction(ds, job.ChannelID, job.MessageID, "⚙️", emoji)
				if w.logger != nil {
					w.logger.Log(w.channelID, ChatEntry{Role: "assistant", Content: "❌ " + errMsg, Model: w.model})
				}
				w.signalIdle()
				return
			}

			if response == "" {
				response = L.Get("worker.empty_response")
			}

			// Post full response in thread
			sendLongThread(ds, threadID, response)
			swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "✅")
			swapReaction(ds, job.ChannelID, job.MessageID, "⚙️", "✅")

			log.Printf("[worker %s] job done | user=%s msg=%s elapsed=%s len=%d",
				w.channelID, job.Username, job.MessageID, time.Since(startTime).Round(time.Millisecond), len(response))

			if w.logger != nil {
				w.logger.Log(w.channelID, ChatEntry{Role: "assistant", Content: response, Model: w.model})
			}

			// Warn if context usage is high
			if usage := w.agent.ContextUsage(); usage >= 90 {
				ds.ChannelMessageSend(threadID, "⚠️ "+L.Getf("context.usage_warning", usage))
			}

			w.signalIdle()
		},
	}

	// Watch for timeout — send cancel to agent
	go func() {
		<-ctx.Done()
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[worker %s] timeout %ds, sending cancel", w.channelID, w.askTimeoutSec)
			go w.agent.CancelPrompt()
		}
	}()

	// Watch for ReadLoop errors (e.g. buffer overflow)
	w.agent.OnReadErrorFunc(func(err error) {
		log.Printf("[worker %s] agent read error | user=%s msg=%s elapsed=%s err=%v",
			w.channelID, job.Username, job.MessageID, time.Since(startTime).Round(time.Millisecond), err)
		msg := L.Getf("error.agent_read", err)
		ds.ChannelMessageSend(threadID, msg)
		swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "⚠️")
		swapReaction(ds, job.ChannelID, job.MessageID, "⚙️", "⚠️")
		cancel()
		w.cancelMu.Lock()
		w.cancelFn = nil
		w.currentThreadID = ""
		w.cancelMu.Unlock()
		w.signalIdle()
	})

	// Inject thread ID into prompt so agent can post directly to thread via MCP
	prompt := strings.Replace(job.Prompt, "channel_id="+job.ChannelID, "channel_id="+job.ChannelID+" thread_id="+threadID, 1)

	// Prepend conversation history to first prompt (avoids wasting a separate Ask round)
	if w.historyPrefix != "" {
		prompt = w.historyPrefix + prompt
		w.historyPrefix = ""
	}

	w.agent.AskAsync(prompt, callbacks)
	// Returns immediately — callbacks handle the rest
}

// executeFallback is the old synchronous path used when thread creation fails.
func (w *Worker) executeFallback(job *Job) {
	ds := job.Session

	replyMsg, err := ds.ChannelMessageSendReply(job.ChannelID, "🔄 "+L.Get("worker.processing"), &discordgo.MessageReference{
		MessageID: job.MessageID, ChannelID: job.ChannelID,
	})
	if err != nil {
		swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "❌")
		w.signalIdle()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(w.askTimeoutSec)*time.Second)
	defer cancel()

	response, askErr := w.agent.Ask(ctx, job.Prompt, func(chunk string) {})

	if askErr != nil {
		errMsg := askErr.Error()
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = L.Getf("worker.timeout", w.askTimeoutSec)
		}
		editMessage(ds, job.ChannelID, replyMsg.ID, "❌ "+errMsg)
		swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "❌")
	} else {
		swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "✅")
		sendLong(ds, job.ChannelID, replyMsg.ID, response)
	}

	if w.logger != nil {
		content := response
		if askErr != nil {
			content = "❌ " + askErr.Error()
		}
		w.logger.Log(w.channelID, ChatEntry{Role: "assistant", Content: content, Model: w.model})
	}
	w.signalIdle()
}

func editMessage(ds *discordgo.Session, channelID, msgID, content string) {
	if len(content) > 2000 {
		content = truncateUTF8(content, 1997) + "..."
	}
	_, _ = ds.ChannelMessageEdit(channelID, msgID, content)
}

func sendLong(ds *discordgo.Session, channelID, placeholderID, content string) {
	const limit = 1990
	parts := splitMessage(content, limit)
	if len(parts) == 0 {
		editMessage(ds, channelID, placeholderID, L.Get("worker.empty_response"))
		return
	}

	prefix := ""
	if len(parts) > 1 {
		prefix = fmt.Sprintf("(1/%d) ", len(parts))
	}
	editMessage(ds, channelID, placeholderID, prefix+parts[0])

	for i := 1; i < len(parts); i++ {
		label := fmt.Sprintf("(%d/%d) ", i+1, len(parts))
		_, _ = ds.ChannelMessageSend(channelID, label+parts[i])
	}
}

func sendLongThread(ds *discordgo.Session, threadID, content string) {
	const limit = 1990
	parts := splitMessage(content, limit)
	for _, p := range parts {
		ds.ChannelMessageSend(threadID, p)
	}
}

func splitMessage(s string, limit int) []string {
	var parts []string
	for len(s) > limit {
		idx := strings.LastIndex(s[:limit], "\n")
		if idx < limit/2 {
			idx = limit
			for idx > 0 && !utf8.RuneStart(s[idx]) {
				idx--
			}
		}
		parts = append(parts, s[:idx])
		s = s[idx:]
		if len(s) > 0 && s[0] == '\n' {
			s = s[1:]
		}
	}
	if s != "" {
		parts = append(parts, s)
	}
	return parts
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

func toolKindIcon(kind string) string {
	switch kind {
	case "read":
		return "📖"
	case "edit":
		return "✏️"
	case "delete":
		return "🗑️"
	case "execute":
		return "▶️"
	case "search":
		return "🔍"
	case "fetch":
		return "🌐"
	case "think":
		return "🧠"
	default:
		return "⚙️"
	}
}
