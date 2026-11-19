package channel

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	L "github.com/nczz/kiro-discord-bot/locale"
)

const (
	foldedProgressEditInterval         = 2 * time.Second
	foldedProgressRefreshInterval      = time.Minute
	foldedNoToolActivityWarningAfter   = 15 * time.Minute
	foldedHighToolCallWarningThreshold = 100
	foldedThoughtBufferMaxBytes        = 4096
	foldedThoughtDisplayMaxBytes       = 320
)

type threadProgressReporter struct {
	mu         sync.Mutex
	editMu     sync.Mutex
	mode       OutputMode
	ds         *discordgo.Session
	threadID   string
	displayCWD string
	messageID  string
	startedAt  time.Time
	lastEdit   time.Time

	toolStarted      int
	toolCompleted    int
	toolFailed       int
	lastToolActivity time.Time
	latest           string
	failedReason     string
	terminal         bool

	thoughtMessageID string
	thoughtBuffer    []byte
}

func newThreadProgressReporter(mode OutputMode, ds *discordgo.Session, threadID, displayCWD string) *threadProgressReporter {
	return &threadProgressReporter{
		mode:       NormalizeOutputMode(mode),
		ds:         ds,
		threadID:   threadID,
		displayCWD: displayCWD,
		startedAt:  time.Now(),
	}
}

func (r *threadProgressReporter) Start() string {
	if r.mode != OutputModeFolded {
		msg, err := SendProcessMessage(r.ds, r.threadID, "🔄 "+L.Get("worker.processing"))
		if err != nil || msg == nil {
			return ""
		}
		r.mu.Lock()
		r.messageID = msg.ID
		r.mu.Unlock()
		return msg.ID
	}
	r.mu.Lock()
	content := r.renderLocked("running")
	r.mu.Unlock()
	msg, err := SendProcessMessage(r.ds, r.threadID, content)
	if err != nil || msg == nil {
		return ""
	}
	r.mu.Lock()
	r.messageID = msg.ID
	r.mu.Unlock()
	return msg.ID
}

func (r *threadProgressReporter) ToolCall(evt acp.ToolCallEvent) {
	if r.mode != OutputModeFolded {
		return
	}
	r.mu.Lock()
	if r.terminal {
		r.mu.Unlock()
		return
	}
	r.toolStarted++
	r.lastToolActivity = time.Now()
	r.resetThoughtLocked()
	r.latest = CompactToolStartMessage(ToolKindIcon(evt.Kind), evt, r.displayCWD)
	r.mu.Unlock()
	r.update(false)
}

func (r *threadProgressReporter) ToolResult(evt acp.ToolCallEvent) {
	if r.mode != OutputModeFolded {
		return
	}
	title := toolDisplayTitle(evt)
	r.mu.Lock()
	if r.terminal {
		r.mu.Unlock()
		return
	}
	r.lastToolActivity = time.Now()
	r.resetThoughtLocked()
	failed := evt.Status == "failed"
	switch evt.Status {
	case "failed":
		r.toolFailed++
		r.latest = "❌ " + EscapeDiscordMarkdown(title)
	case "completed":
		r.toolCompleted++
		r.latest = "✅ " + EscapeDiscordMarkdown(title)
	default:
		r.latest = CompactToolStartMessage(ToolKindIcon(evt.Kind), evt, r.displayCWD)
	}
	r.mu.Unlock()
	if failed {
		if !r.update(true) {
			SendProcessMessage(r.ds, r.threadID, FormatToolFailureMessage(title, evt.RawOutput, 500))
		}
		return
	}
	r.update(false)
}

func (r *threadProgressReporter) Thought(chunk acp.ThoughtChunk) {
	if r.mode != OutputModeFolded || chunk.Text == "" {
		return
	}
	r.mu.Lock()
	if r.terminal {
		r.mu.Unlock()
		return
	}
	r.appendThoughtLocked(chunk)
	r.latest = "💭 " + EscapeDiscordMarkdown(readableThoughtTail(r.thoughtBuffer, foldedThoughtDisplayMaxBytes))
	r.mu.Unlock()
	r.update(false)
}

func (r *threadProgressReporter) Subagent(state acp.SubagentState) {
	if r.mode != OutputModeFolded {
		return
	}
	if msg := FormatSubagentProgress(state); msg != "" {
		r.setLatest(msg)
	}
}

func (r *threadProgressReporter) Plan(state acp.PlanState) {
	if r.mode != OutputModeFolded {
		return
	}
	if msg := FormatPlanUpdate(state); msg != "" {
		r.setLatest(msg)
	}
}

func (r *threadProgressReporter) CompleteSuccess() bool { return r.complete("success", "") }

func (r *threadProgressReporter) CompleteFailure(reason string) bool {
	return r.complete("failed", reason)
}

func (r *threadProgressReporter) CompleteInterrupted(reason string) bool {
	if r.mode == OutputModeFolded {
		return r.complete("failed", reason)
	}
	r.editMu.Lock()
	defer r.editMu.Unlock()
	r.mu.Lock()
	if r.terminal {
		r.mu.Unlock()
		return true
	}
	r.terminal = true
	messageID := r.messageID
	r.mu.Unlock()
	if messageID == "" {
		return false
	}
	if err := editMessageWithMentions(r.ds, r.threadID, messageID, "⚠️ "+reason, nil); err != nil {
		log.Printf("[progress] edit interrupted progress thread=%s message=%s: %v", r.threadID, messageID, err)
		return false
	}
	return true
}

func (r *threadProgressReporter) setLatest(latest string) {
	r.mu.Lock()
	if r.terminal {
		r.mu.Unlock()
		return
	}
	r.resetThoughtLocked()
	r.latest = latest
	r.mu.Unlock()
	r.update(false)
}

func (r *threadProgressReporter) refreshUntil(ctx context.Context, interval time.Duration) {
	if r == nil || r.mode != OutputModeFolded || ctx == nil || interval <= 0 {
		return
	}
	r.mu.Lock()
	ready := r.messageID != "" && !r.terminal
	r.mu.Unlock()
	if !ready {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.update(false)
			}
		}
	}()
}

func (r *threadProgressReporter) appendThoughtLocked(chunk acp.ThoughtChunk) {
	if chunk.MessageID != "" && chunk.MessageID != r.thoughtMessageID {
		r.resetThoughtLocked()
		r.thoughtMessageID = chunk.MessageID
	}
	r.thoughtBuffer = appendBoundedUTF8(r.thoughtBuffer, chunk.Text, foldedThoughtBufferMaxBytes)
}

func (r *threadProgressReporter) resetThoughtLocked() {
	r.thoughtMessageID = ""
	r.thoughtBuffer = r.thoughtBuffer[:0]
}

func appendBoundedUTF8(dst []byte, text string, maxBytes int) []byte {
	if text == "" {
		return dst
	}
	if maxBytes <= 0 {
		return dst[:0]
	}
	if len(text) >= maxBytes {
		start := len(text) - maxBytes
		for start < len(text) && !utf8.RuneStart(text[start]) {
			start++
		}
		return append(dst[:0], text[start:]...)
	}
	if overflow := len(dst) + len(text) - maxBytes; overflow > 0 {
		start := overflow
		for start < len(dst) && !utf8.RuneStart(dst[start]) {
			start++
		}
		copy(dst, dst[start:])
		dst = dst[:len(dst)-start]
	}
	return append(dst, text...)
}

func readableThoughtTail(raw []byte, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) <= maxBytes {
		return string(raw)
	}
	start := len(raw) - maxBytes
	for start < len(raw) && !utf8.RuneStart(raw[start]) {
		start++
	}
	tail := raw[start:]
	if boundary := bytes.IndexAny(tail, " \t\r\n"); boundary >= 0 && boundary < maxBytes/4 {
		tail = bytes.TrimSpace(tail[boundary+1:])
	}
	return "…" + string(tail)
}

func (r *threadProgressReporter) complete(status, reason string) bool {
	if r.mode != OutputModeFolded {
		return true
	}
	r.mu.Lock()
	if r.terminal {
		r.mu.Unlock()
		return true
	}
	r.terminal = true
	r.failedReason = reason
	r.mu.Unlock()
	return r.updateStatus(status, true)
}

func (r *threadProgressReporter) update(force bool) bool { return r.updateStatus("running", force) }

func (r *threadProgressReporter) updateStatus(status string, force bool) bool {
	if r == nil || r.mode != OutputModeFolded {
		return true
	}
	r.editMu.Lock()
	defer r.editMu.Unlock()
	r.mu.Lock()
	if status == "running" && r.terminal {
		r.mu.Unlock()
		return true
	}
	if r.messageID == "" {
		r.mu.Unlock()
		return false
	}
	now := time.Now()
	if !force && !r.lastEdit.IsZero() && now.Sub(r.lastEdit) < foldedProgressEditInterval {
		r.mu.Unlock()
		return true
	}
	content := r.renderLockedAt(status, now)
	r.lastEdit = now
	messageID := r.messageID
	r.mu.Unlock()
	if err := editMessageWithMentions(r.ds, r.threadID, messageID, content, nil); err != nil {
		log.Printf("[progress] edit folded progress thread=%s message=%s: %v", r.threadID, messageID, err)
		return false
	}
	return true
}

func (r *threadProgressReporter) renderLocked(status string) string {
	return r.renderLockedAt(status, time.Now())
}

func (r *threadProgressReporter) renderLockedAt(status string, now time.Time) string {
	elapsed := formatFoldedElapsed(now.Sub(r.startedAt))
	running := r.toolStarted - r.toolCompleted - r.toolFailed
	if running < 0 {
		running = 0
	}
	summary := r.foldedToolSummaryLocked(status, running)
	var msg string
	switch status {
	case "success":
		msg = L.Getf("worker.progress.folded.completed", elapsed, summary)
	case "failed":
		msg = L.Getf("worker.progress.folded.failed", elapsed, summary)
	default:
		msg = L.Getf("worker.progress.folded.running", elapsed, summary)
	}
	if r.latest != "" && status == "running" {
		msg += "\n" + L.Getf("worker.progress.folded.latest", r.latest)
	}
	if status == "running" {
		msg = r.appendActivityDiagnosticsLocked(msg, now)
	}
	if r.failedReason != "" && status == "failed" {
		msg += "\n" + L.Getf("worker.progress.folded.reason", EscapeDiscordMarkdown(r.failedReason))
	}
	return msg
}

func (r *threadProgressReporter) appendActivityDiagnosticsLocked(msg string, now time.Time) string {
	baseline := r.startedAt
	if !r.lastToolActivity.IsZero() {
		baseline = r.lastToolActivity
	}
	inactiveFor := now.Sub(baseline)
	if inactiveFor < 0 {
		inactiveFor = 0
	}
	stale := inactiveFor >= foldedNoToolActivityWarningAfter
	highToolCount := r.toolStarted >= foldedHighToolCallWarningThreshold
	if stale {
		msg += "\n" + L.Getf("worker.progress.folded.no_tool_activity_warning", formatFoldedElapsed(inactiveFor))
	} else if !r.lastToolActivity.IsZero() {
		msg += "\n" + L.Getf("worker.progress.folded.last_tool_activity", formatFoldedElapsed(inactiveFor))
	}
	if highToolCount {
		msg += "\n" + L.Getf("worker.progress.folded.high_tool_activity_warning", r.toolStarted)
	}
	if stale || highToolCount {
		msg += "\n" + L.Get("worker.progress.folded.controls")
	}
	return msg
}

func (r *threadProgressReporter) foldedToolSummaryLocked(status string, running int) string {
	if r.toolStarted == 0 {
		if status == "running" {
			return L.Get("worker.progress.folded.no_tools_yet")
		}
		return L.Get("worker.progress.folded.no_tools")
	}
	if status == "running" && running > 0 && r.toolFailed > 0 {
		return L.Getf("worker.progress.folded.tool_calls_running_failed", r.toolStarted, running, r.toolFailed)
	}
	if status == "running" && running > 0 {
		return L.Getf("worker.progress.folded.tool_calls_running", r.toolStarted, running)
	}
	if r.toolFailed > 0 {
		return L.Getf("worker.progress.folded.tool_calls_failed", r.toolStarted, r.toolFailed)
	}
	return L.Getf("worker.progress.folded.tool_calls", r.toolStarted)
}

func formatFoldedElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
}
