package channel

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/audit"
	L "github.com/nczz/kiro-discord-bot/locale"
)

var reMention = regexp.MustCompile(`<@!?\d+>`)

// Job represents a single user message to be processed.
type Job struct {
	ChannelID       string
	ParentChannelID string
	GuildID         string
	MessageID       string
	Prompt          string
	Session         *discordgo.Session
	UserID          string
	Username        string
	Attachments     []string
	ThreadID        string // non-empty = follow-up in existing thread, skip thread creation
	Transcript      string // STT transcription result, shown in thread if non-empty
	Handoff         bool   // true when this job is an accepted cross-bot handoff
	Source          string // message, thread, cron, etc.
}

type AuditSink interface {
	RecordBotEvent(audit.BotEvent)
}

// Worker manages a per-channel job queue and executes jobs sequentially.
type Worker struct {
	channelID       string
	agent           workerAgent
	queue           chan *Job
	askTimeoutSec   int
	streamUpdateSec int
	threadArchive   int // auto-archive duration in minutes
	stopCh          chan struct{}
	idleCh          chan struct{} // signaled when agent finishes a task
	stopped         sync.Once
	started         sync.Once
	logger          *ChatLogger
	usage           *UsageStore
	audit           AuditSink
	model           string

	cancelMu sync.Mutex
	cancelFn context.CancelFunc

	// Current thread info (protected by cancelMu)
	currentThreadID     string
	currentJobID        string
	currentJobSeq       uint64
	currentJobActive    bool
	pendingInterruptSeq uint64

	onActivity func()      // called during work to signal liveness (prevents idle cleanup)
	onIdle     func() bool // called when a job finishes; true means the worker was stopped

	memoryPrefix func() string // returns memory+flash prefix to inject into prompts

	isSilent func() bool // returns true if silent mode is on (compact tool output)

	historyPrefix string // prepended to first job's prompt, then cleared
}

type workerAgent interface {
	Ask(context.Context, string, func(string)) (string, error)
	AskAsync(string, acp.AsyncCallbacks)
	AskAsyncMulti([]acp.PromptContent, acp.AsyncCallbacks)
	CancelPrompt()
	Interrupt() error
	ContextUsage() float64
	TurnMetrics() acp.TurnMetrics
	OnReadErrorFunc(func(error))
	RecentStderr() string
	SupportsImagePrompt() bool
}

func NewWorker(channelID string, agent *acp.Agent, bufSize, askTimeoutSec, streamUpdateSec, threadArchive int, logger *ChatLogger, model string) *Worker {
	return newWorker(channelID, agent, bufSize, askTimeoutSec, streamUpdateSec, threadArchive, logger, model)
}

func newWorker(channelID string, agent workerAgent, bufSize, askTimeoutSec, streamUpdateSec, threadArchive int, logger *ChatLogger, model string) *Worker {
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

// OnIdleFunc sets a callback invoked after a job finishes.
func (w *Worker) OnIdleFunc(fn func() bool) { w.onIdle = fn }

// OnMemoryPrefixFunc sets a callback that returns memory rules to prepend to prompts.
func (w *Worker) OnMemoryPrefixFunc(fn func() string) { w.memoryPrefix = fn }

// SetUsageStore sets the append-only usage ledger used for report commands.
func (w *Worker) SetUsageStore(store *UsageStore) { w.usage = store }

func (w *Worker) SetAuditSink(sink AuditSink) { w.audit = sink }

// OnSilentFunc sets a callback that returns whether silent mode is active.
func (w *Worker) OnSilentFunc(fn func() bool) { w.isSilent = fn }

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

func (w *Worker) isStopped() bool {
	select {
	case <-w.stopCh:
		return true
	default:
		return false
	}
}

// CancelCurrent cancels the currently running job, if any.
func (w *Worker) CancelCurrent() bool {
	w.cancelMu.Lock()
	fn := w.cancelFn
	w.cancelMu.Unlock()
	if fn != nil {
		fn()
		w.agent.CancelPrompt()
		return true
	}
	return false
}

// IsActive reports whether this worker is currently executing a job.
func (w *Worker) IsActive() bool {
	w.cancelMu.Lock()
	defer w.cancelMu.Unlock()
	return w.currentJobActive
}

// InterruptCurrent first requests a normal ACP cancellation, then interrupts
// the agent process group if the same job is still active after grace.
func (w *Worker) InterruptCurrent(grace time.Duration) bool {
	w.cancelMu.Lock()
	fn := w.cancelFn
	jobID := w.currentJobID
	jobSeq := w.currentJobSeq
	alreadyPending := w.pendingInterruptSeq == jobSeq
	if fn != nil && jobSeq != 0 && !alreadyPending {
		w.pendingInterruptSeq = jobSeq
	}
	w.cancelMu.Unlock()
	if fn == nil || jobSeq == 0 {
		return false
	}

	fn()
	w.agent.CancelPrompt()
	if alreadyPending {
		return true
	}

	go func() {
		if grace > 0 {
			timer := time.NewTimer(grace)
			defer timer.Stop()
			<-timer.C
		}

		w.cancelMu.Lock()
		stillActive := w.cancelFn != nil && w.currentJobSeq == jobSeq
		if w.pendingInterruptSeq == jobSeq {
			w.pendingInterruptSeq = 0
		}
		w.cancelMu.Unlock()
		if !stillActive {
			return
		}
		if err := w.agent.Interrupt(); err != nil {
			log.Printf("[worker %s] interrupt failed | job=%s err=%v", w.channelID, jobID, err)
		}
	}()

	return true
}

// CurrentThreadID returns the thread ID of the currently running task.
func (w *Worker) CurrentThreadID() string {
	w.cancelMu.Lock()
	defer w.cancelMu.Unlock()
	return w.currentThreadID
}

func (w *Worker) signalIdle() {
	if w.isStopped() {
		return
	}
	if w.onIdle != nil && w.onIdle() {
		return
	}
	if w.isStopped() {
		return
	}
	select {
	case w.idleCh <- struct{}{}:
	default:
	}
}

func (w *Worker) waitIdle() bool {
	if w.isStopped() {
		return false
	}
	select {
	case <-w.idleCh:
		return !w.isStopped()
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
			if w.isStopped() {
				return
			}
			w.execute(job)
		}
	}
}

func promptSummary(prompt string, maxLen int) string {
	prompt = promptVisibleBody(prompt)
	if len(prompt) > maxLen {
		return truncateUTF8(prompt, maxLen-3) + "..."
	}
	return prompt
}

func promptVisibleBody(prompt string) string {
	prompt = strings.TrimLeft(prompt, "\n")
	for strings.HasPrefix(prompt, "[") {
		idx := strings.Index(prompt, "\n\n")
		if idx < 0 {
			break
		}
		prompt = strings.TrimLeft(prompt[idx+2:], "\n")
	}
	if strings.HasPrefix(prompt, "- /") {
		if idx := strings.Index(prompt, "\n\n"); idx >= 0 {
			prompt = prompt[idx+2:]
		}
	}
	return strings.TrimSpace(prompt)
}

func (w *Worker) execute(job *Job) {
	ds := job.Session
	startTime := time.Now()
	w.auditJobEvent("agent_job_started", job, "", "", nil)

	// Signal activity at task start
	if w.onActivity != nil {
		w.onActivity()
	}

	log.Printf("[worker %s] job start | user=%s(%s) msg=%s prompt=%q",
		w.channelID, job.Username, job.UserID, job.MessageID, promptSummary(job.Prompt, 80))

	w.cancelMu.Lock()
	w.currentJobID = job.MessageID
	w.currentJobSeq++
	w.currentJobActive = true
	w.cancelMu.Unlock()
	var finishOnce sync.Once
	finishJob := func() {
		finishOnce.Do(func() {
			w.cancelMu.Lock()
			w.cancelFn = nil
			w.currentThreadID = ""
			w.currentJobID = ""
			w.currentJobActive = false
			if w.pendingInterruptSeq == w.currentJobSeq {
				w.pendingInterruptSeq = 0
			}
			w.cancelMu.Unlock()
			w.signalIdle()
		})
	}

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
		threadName := promptVisibleBody(job.Prompt)
		threadName = reMention.ReplaceAllString(threadName, "")
		threadName = strings.TrimSpace(threadName)
		if len(threadName) > 95 {
			threadName = truncateUTF8(threadName, 92) + "..."
		}
		if threadName == "" {
			threadName = L.Get("worker.thread_default")
		}

		thread, err := w.threadForMessage(ds, job, threadName)
		if err != nil {
			log.Printf("[worker %s] get/create thread: %v, falling back to sync", w.channelID, err)
			w.auditJobEvent("agent_thread_create_failed", job, "", "error", map[string]any{"error": err.Error()})
			w.executeFallback(job)
			return
		}
		threadID = thread.ID
	}

	w.cancelMu.Lock()
	w.currentThreadID = threadID
	w.cancelMu.Unlock()

	// Post initial status in thread
	if job.Transcript != "" {
		ds.ChannelMessageSend(threadID, L.Get("stt.prefix")+job.Transcript)
	}
	SendProcessMessage(ds, threadID, "🔄 "+L.Get("worker.processing"))

	// Setup timeout context as safety net
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(w.askTimeoutSec)*time.Second)
	w.cancelMu.Lock()
	w.cancelFn = cancel
	w.cancelMu.Unlock()

	// Async callbacks — all post to thread
	callbacks := acp.AsyncCallbacks{
		OnChunk: func(chunk string) {
			if w.onActivity != nil {
				w.onActivity()
			}
		},
		OnToolCall: func(evt acp.ToolCallEvent) {
			if w.onActivity != nil {
				w.onActivity()
			}
			title := evt.Title
			if title == "" {
				title = L.Get("worker.tool_fallback")
			}
			icon := ToolKindIcon(evt.Kind)
			silent := w.isSilent != nil && w.isSilent()
			if silent {
				// Compact: icon + title only
				SendProcessMessage(ds, threadID, CompactToolStartMessage(icon, evt))
			} else {
				// Full: icon + title + affected files
				msg := icon + " " + EscapeDiscordMarkdown(title)
				if len(evt.Locations) > 0 {
					files := make([]string, 0, len(evt.Locations))
					for _, loc := range evt.Locations {
						files = append(files, "`"+loc.Path+"`")
					}
					msg += "\n📁 " + strings.Join(files, ", ")
				}
				SendProcessMessage(ds, threadID, msg)
			}
			swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "⚙️")
		},
		OnToolResult: func(evt acp.ToolCallEvent) {
			if w.onActivity != nil {
				w.onActivity()
			}
			swapReaction(ds, job.ChannelID, job.MessageID, "⚙️", "🔄")
			silent := w.isSilent != nil && w.isSilent()
			if silent {
				// Compact: only show one-line failure
				if evt.Status == "failed" {
					SendProcessMessage(ds, threadID, "❌ "+EscapeDiscordMarkdown(evt.Title))
				}
				return
			}
			// Full: send tool output to thread if meaningful
			if evt.RawOutput != "" && evt.Status == "completed" {
				out := evt.RawOutput
				if len(out) > 1900 {
					out = out[:1900] + L.Get("tool.output_truncated")
				}
				SendProcessMessage(ds, threadID, "```\n"+out+"\n```")
			} else if evt.Status == "failed" {
				msg := "❌ " + EscapeDiscordMarkdown(evt.Title)
				if evt.RawOutput != "" {
					o := evt.RawOutput
					if len(o) > 500 {
						o = o[:500] + "..."
					}
					msg += "\n```\n" + o + "\n```"
				}
				SendProcessMessage(ds, threadID, msg)
			}
		},
		OnThought: func(text string) {
			if w.onActivity != nil {
				w.onActivity()
			}
			if w.isSilent != nil && w.isSilent() {
				return // Compact: skip thoughts
			}
			// Accumulate thought chunks — send as a single collapsed block would be ideal,
			// but Discord doesn't support spoiler streaming. Just prefix with 💭.
			if len(text) > 1900 {
				text = text[:1900] + "…"
			}
			SendProcessMessage(ds, threadID, "💭 "+EscapeDiscordMarkdown(text))
		},
		OnComplete: func(response string, askErr error) {
			if w.onActivity != nil {
				w.onActivity()
			}
			// Capture ctx state BEFORE cancel() — cancel() sets ctx.Err() to Canceled
			ctxErr := ctx.Err()
			cancel() // release timeout context

			if askErr != nil {
				errMsg := askErr.Error()
				emoji := "❌"
				if ctxErr == context.DeadlineExceeded {
					errMsg = L.Getf("worker.timeout", w.askTimeoutSec)
					emoji = "⚠️"
				} else if ctxErr == context.Canceled {
					errMsg = L.Get("cancel.success")
					emoji = "⚠️"
				} else if stderr := w.agent.RecentStderr(); stderr != "" {
					errMsg += "\n```\n" + stderr + "\n```"
				}
				log.Printf("[worker %s] job error | user=%s msg=%s elapsed=%s ctxErr=%v err=%v",
					w.channelID, job.Username, job.MessageID, time.Since(startTime).Round(time.Millisecond), ctxErr, askErr)
				ds.ChannelMessageSend(threadID, "❌ "+errMsg)
				swapReaction(ds, job.ChannelID, job.MessageID, "🔄", emoji)
				swapReaction(ds, job.ChannelID, job.MessageID, "⚙️", emoji)
				if w.logger != nil {
					w.logger.Log(w.channelID, ChatEntry{Role: "assistant", Content: "❌ " + errMsg, Model: w.model})
				}
				w.auditJobEvent("agent_job_failed", job, threadID, "error", map[string]any{
					"error":      errMsg,
					"ctx_error":  fmt.Sprint(ctxErr),
					"elapsed_ms": time.Since(startTime).Milliseconds(),
				})
				w.auditResponseEvent(job, threadID, "error", "❌ "+errMsg)
				w.recordUsage(job, threadID, "error")
				finishJob()
				return
			}

			if response == "" {
				response = L.Get("worker.empty_response")
			}

			swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "✅")
			swapReaction(ds, job.ChannelID, job.MessageID, "⚙️", "✅")
			// Mark the origin done before final text so tagged peer bots see a completed source.
			SendLongThread(ds, threadID, response)
			w.auditResponseEvent(job, threadID, "success", response)

			log.Printf("[worker %s] job done | user=%s msg=%s elapsed=%s len=%d",
				w.channelID, job.Username, job.MessageID, time.Since(startTime).Round(time.Millisecond), len(response))

			if w.logger != nil {
				w.logger.Log(w.channelID, ChatEntry{Role: "assistant", Content: response, Model: w.model})
			}

			// Show turn metrics footer if available
			if footer := formatMetricsFooter(w.agent.TurnMetrics()); footer != "" {
				ds.ChannelMessageSend(threadID, footer)
			}
			w.recordUsage(job, threadID, "success")
			w.auditJobEvent("agent_job_completed", job, threadID, "success", map[string]any{
				"elapsed_ms":   time.Since(startTime).Milliseconds(),
				"response_len": len(response),
			})

			// Warn if context usage is high
			if usage := w.agent.ContextUsage(); usage >= 90 {
				ds.ChannelMessageSend(threadID, "⚠️ "+L.Getf("context.usage_warning", usage))
			}

			finishJob()
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
		if !w.isSilent() {
			msg := L.Getf("error.agent_read", err)
			ds.ChannelMessageSend(threadID, msg)
			swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "⚠️")
			swapReaction(ds, job.ChannelID, job.MessageID, "⚙️", "⚠️")
		}
		cancel()
		finishJob()
	})

	// Inject thread ID into prompt so agent can post directly to thread via MCP
	prompt := strings.Replace(job.Prompt, "channel_id="+job.ChannelID, "channel_id="+job.ChannelID+" thread_id="+threadID, 1)

	// Prepend conversation history to first prompt (avoids wasting a separate Ask round)
	if w.historyPrefix != "" {
		prompt = w.historyPrefix + prompt
		w.historyPrefix = ""
	}

	// Inject memory rules (after thread title extraction, before sending to agent)
	if w.memoryPrefix != nil {
		if mp := w.memoryPrefix(); mp != "" {
			prompt = mp + prompt
		}
	}

	// Build multi-content prompt: split image attachments into image blocks
	promptContent := buildPromptContent(prompt, job.Attachments, w.agent.SupportsImagePrompt())
	w.agent.AskAsyncMulti(promptContent, callbacks)
	// Returns immediately — callbacks handle the rest
}

func (w *Worker) auditJobEvent(eventType string, job *Job, threadID, status string, metadata map[string]any) {
	if w == nil || w.audit == nil || job == nil {
		return
	}
	w.audit.RecordBotEvent(audit.BotEvent{
		Type:            eventType,
		GuildID:         job.GuildID,
		ChannelID:       job.ChannelID,
		TargetID:        job.ChannelID,
		ThreadID:        threadID,
		ParentChannelID: job.ParentChannelID,
		MessageID:       job.MessageID,
		JobID:           job.MessageID,
		UserID:          job.UserID,
		Username:        job.Username,
		Source:          job.Source,
		Status:          status,
		Model:           w.model,
		Metadata:        metadata,
	})
}

func (w *Worker) auditResponseEvent(job *Job, threadID, status, content string) {
	if w == nil || w.audit == nil || job == nil {
		return
	}
	w.audit.RecordBotEvent(audit.BotEvent{
		Type:            "agent_response_sent",
		GuildID:         job.GuildID,
		ChannelID:       job.ChannelID,
		TargetID:        threadID,
		ThreadID:        threadID,
		ParentChannelID: job.ParentChannelID,
		MessageID:       job.MessageID,
		JobID:           job.MessageID,
		UserID:          job.UserID,
		Username:        job.Username,
		Source:          job.Source,
		Status:          status,
		Content:         content,
		Model:           w.model,
		Metadata: map[string]any{
			"content_len": len(content),
		},
	})
}

func (w *Worker) recordUsage(job *Job, threadID, status string) {
	if w.usage == nil || job == nil {
		return
	}
	metrics := w.agent.TurnMetrics()
	channelID := job.ChannelID
	if job.ParentChannelID != "" {
		channelID = job.ParentChannelID
	}
	source := job.Source
	if source == "" {
		if job.ThreadID != "" {
			source = "thread"
		} else {
			source = "message"
		}
	}
	if err := w.usage.Append(UsageRecord{
		GuildID:       job.GuildID,
		ChannelID:     channelID,
		ThreadID:      threadID,
		UserID:        job.UserID,
		Username:      job.Username,
		MessageID:     job.MessageID,
		Model:         w.model,
		Source:        source,
		Status:        status,
		MeteringUsage: metrics.MeteringUsage,
		DurationMs:    metrics.TurnDurationMs,
		ContextUsage:  metrics.ContextUsage,
	}); err != nil {
		log.Printf("[usage] append failed | user=%s msg=%s err=%v", job.UserID, job.MessageID, err)
	}
}

// isImageFile returns true if the path has an image extension supported by ACP.
func isImageFile(path string) bool {
	ext := strings.ToLower(path)
	for _, suffix := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp"} {
		if strings.HasSuffix(ext, suffix) {
			return true
		}
	}
	return false
}

// buildPromptContent constructs []PromptContent from a text prompt and attachments.
// If imageSupport is true, image files are sent as image content blocks.
func buildPromptContent(prompt string, attachments []string, imageSupport bool) []acp.PromptContent {
	var content []acp.PromptContent
	content = append(content, acp.PromptContent{Type: "text", Text: prompt})
	if imageSupport {
		for _, path := range attachments {
			if isImageFile(path) {
				data, err := os.ReadFile(path)
				if err != nil {
					log.Printf("[worker] read image attachment %s: %v", path, err)
					continue
				}
				mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
				if mimeType == "" {
					mimeType = "application/octet-stream"
				}
				content = append(content, acp.PromptContent{
					Type:     "image",
					Data:     base64.StdEncoding.EncodeToString(data),
					MimeType: mimeType,
				})
			}
		}
	}
	return content
}

func (w *Worker) threadForMessage(ds *discordgo.Session, job *Job, threadName string) (*discordgo.Channel, error) {
	if thread, err := ds.Channel(job.MessageID); err == nil && thread != nil && thread.IsThread() {
		log.Printf("[worker %s] reusing existing thread %s for msg=%s", w.channelID, thread.ID, job.MessageID)
		return thread, nil
	}

	archiveDur := w.threadArchive
	if archiveDur <= 0 {
		archiveDur = 1440
	}
	thread, err := ds.MessageThreadStart(job.ChannelID, job.MessageID, threadName, archiveDur)
	if err == nil {
		return thread, nil
	}
	if isThreadAlreadyCreated(err) {
		if thread, fetchErr := ds.Channel(job.MessageID); fetchErr == nil && thread != nil && thread.IsThread() {
			log.Printf("[worker %s] reusing raced thread %s for msg=%s", w.channelID, thread.ID, job.MessageID)
			return thread, nil
		}
	}
	return nil, err
}

func isThreadAlreadyCreated(err error) bool {
	restErr, ok := err.(*discordgo.RESTError)
	return ok && restErr.Message != nil && restErr.Message.Code == discordgo.ErrCodeThreadAlreadyCreatedForThisMessage
}

// executeFallback is the old synchronous path used when thread creation fails.
func (w *Worker) executeFallback(job *Job) {
	ds := job.Session
	defer func() {
		w.cancelMu.Lock()
		w.cancelFn = nil
		w.currentThreadID = ""
		w.currentJobID = ""
		w.currentJobActive = false
		if w.pendingInterruptSeq == w.currentJobSeq {
			w.pendingInterruptSeq = 0
		}
		w.cancelMu.Unlock()
		w.signalIdle()
	}()

	replyMsg, err := ds.ChannelMessageSendReply(job.ChannelID, "🔄 "+L.Get("worker.processing"), &discordgo.MessageReference{
		MessageID: job.MessageID, ChannelID: job.ChannelID,
	})
	if err != nil {
		swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "❌")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(w.askTimeoutSec)*time.Second)
	defer cancel()
	w.cancelMu.Lock()
	w.cancelFn = cancel
	w.cancelMu.Unlock()

	response, askErr := w.agent.Ask(ctx, job.Prompt, func(chunk string) {
		if w.onActivity != nil {
			w.onActivity()
		}
	})

	if askErr != nil {
		errMsg := askErr.Error()
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = L.Getf("worker.timeout", w.askTimeoutSec)
		} else if stderr := w.agent.RecentStderr(); stderr != "" {
			errMsg += "\n```\n" + stderr + "\n```"
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

// SendLongThread sends a long message to a thread, auto-splitting at Discord's limit.
func SendLongThread(ds *discordgo.Session, threadID, content string) {
	const limit = 1990
	parts := splitMessage(content, limit)
	for _, p := range parts {
		if _, err := ds.ChannelMessageSend(threadID, p); err != nil {
			log.Printf("[send] thread %s failed: %v (len=%d)", threadID, err, len(p))
		}
	}
}

func splitMessage(s string, limit int) []string {
	var parts []string
	for len(s) > limit {
		idx := findSplitPoint(s, limit)
		part := s[:idx]

		// If we're splitting inside a code block, close it and reopen in next part
		lang, inBlock := codeBlockState(part)
		if inBlock {
			part += "\n```"
		}
		parts = append(parts, part)

		s = s[idx:]
		if len(s) > 0 && s[0] == '\n' {
			s = s[1:]
		}
		if inBlock {
			s = "```" + lang + "\n" + s
		}
	}
	if s != "" {
		parts = append(parts, s)
	}
	return parts
}

// findSplitPoint finds the best split index within limit, preferring paragraph > newline > utf8 boundary.
func findSplitPoint(s string, limit int) int {
	chunk := s[:limit]

	// Prefer paragraph boundary (double newline)
	if idx := strings.LastIndex(chunk, "\n\n"); idx >= limit/3 {
		return idx
	}
	// Then single newline
	if idx := strings.LastIndex(chunk, "\n"); idx >= limit/3 {
		return idx
	}
	// Fallback: UTF-8 safe boundary
	idx := limit
	for idx > 0 && !utf8.RuneStart(s[idx]) {
		idx--
	}
	return idx
}

// codeBlockState returns whether the text ends inside an unclosed code block,
// and the language tag if any.
func codeBlockState(s string) (lang string, inBlock bool) {
	for {
		idx := strings.Index(s, "```")
		if idx < 0 {
			return lang, inBlock
		}
		if !inBlock {
			inBlock = true
			// Extract language tag (chars after ``` until newline or end)
			rest := s[idx+3:]
			if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
				lang = rest[:nl]
			} else {
				lang = rest
			}
			// Clean: lang should be a simple word, no spaces
			if strings.ContainsAny(lang, " \t`") {
				lang = ""
			}
		} else {
			inBlock = false
			lang = ""
		}
		s = s[idx+3:]
	}
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

// ToolKindIcon returns the emoji icon for a tool kind.
func ToolKindIcon(kind string) string {
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

func CompactToolStartMessage(icon string, evt acp.ToolCallEvent) string {
	title := strings.TrimSpace(evt.Title)
	if title == "" {
		title = L.Get("worker.tool_fallback")
	}
	if evt.Kind == "execute" {
		return icon + " " + compactExecuteTitle(title)
	}
	if len(title) > 80 {
		title = truncateUTF8(title, 77) + "..."
	}
	return icon + " " + EscapeDiscordMarkdown(title)
}

func compactExecuteTitle(title string) string {
	title = strings.TrimSpace(title)
	lower := strings.ToLower(title)
	if strings.HasPrefix(lower, "running:") || strings.HasPrefix(lower, "running ") {
		return compactCommandTitle("Running", title)
	}
	if strings.HasPrefix(lower, "executing:") || strings.HasPrefix(lower, "executing ") {
		return compactCommandTitle("Executing", title)
	}
	if len(title) > 80 {
		return "Running command"
	}
	return EscapeDiscordMarkdown(title)
}

func compactCommandTitle(verb, title string) string {
	cmd := strings.TrimSpace(title)
	if before, after, ok := strings.Cut(cmd, ":"); ok && strings.EqualFold(strings.TrimSpace(before), verb) {
		cmd = strings.TrimSpace(after)
	}
	lower := strings.ToLower(cmd)
	prefix := strings.ToLower(verb)
	if strings.HasPrefix(lower, prefix+" ") {
		cmd = strings.TrimSpace(cmd[len(verb):])
	}
	cmd = strings.Join(strings.Fields(cmd), " ")
	if cmd == "" {
		return verb + " command"
	}
	if len(cmd) > 52 {
		cmd = truncateUTF8(cmd, 49) + "..."
	}
	return verb + ": " + EscapeDiscordMarkdown(cmd)
}

func EscapeDiscordMarkdown(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\', '`', '*', '_', '~', '|', '>', '#':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func SendProcessMessage(ds *discordgo.Session, channelID, content string) (*discordgo.Message, error) {
	if ds == nil || channelID == "" || content == "" {
		return nil, nil
	}
	msg, err := ds.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
		Flags:           discordgo.MessageFlagsSuppressEmbeds,
	})
	if err != nil {
		log.Printf("[send] process message %s failed: %v (len=%d)", channelID, err, len(content))
	}
	return msg, err
}

// formatMetricsFooter builds a one-line metrics summary from turn metrics.
// Returns empty string if no meaningful metrics are available.
func formatMetricsFooter(m acp.TurnMetrics) string {
	if m.TurnDurationMs == 0 && len(m.MeteringUsage) == 0 {
		return ""
	}
	var parts []string
	if len(m.MeteringUsage) > 0 {
		item := m.MeteringUsage[0]
		parts = append(parts, fmt.Sprintf("%.2f %s", item.Value, item.Unit))
	}
	if m.TurnDurationMs > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", float64(m.TurnDurationMs)/1000))
	}
	if m.ContextUsage > 0 {
		parts = append(parts, fmt.Sprintf("ctx %.0f%%", m.ContextUsage))
	}
	if len(parts) == 0 {
		return ""
	}
	return L.Getf("metrics.footer", strings.Join(parts, " · "))
}
