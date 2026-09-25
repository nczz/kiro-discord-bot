package channel

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	L "github.com/nczz/kiro-discord-bot/locale"
)

const foldedProgressEditInterval = 2 * time.Second

type threadProgressReporter struct {
	mu         sync.Mutex
	mode       OutputMode
	ds         *discordgo.Session
	threadID   string
	displayCWD string
	messageID  string
	startedAt  time.Time
	lastEdit   time.Time

	toolStarted   int
	toolCompleted int
	toolFailed    int
	latest        string
	failedReason  string
}

func newThreadProgressReporter(mode OutputMode, ds *discordgo.Session, threadID, displayCWD string) *threadProgressReporter {
	return &threadProgressReporter{mode: NormalizeOutputMode(mode), ds: ds, threadID: threadID, displayCWD: displayCWD, startedAt: time.Now()}
}

func (r *threadProgressReporter) Start() {
	if r.mode != OutputModeFolded {
		SendProcessMessage(r.ds, r.threadID, "🔄 "+L.Get("worker.processing"))
		return
	}
	r.mu.Lock()
	content := r.renderLocked("running")
	r.mu.Unlock()
	msg, err := SendProcessMessage(r.ds, r.threadID, content)
	if err != nil {
		return
	}
	if msg != nil {
		r.messageID = msg.ID
	}
}

func (r *threadProgressReporter) ToolCall(evt acp.ToolCallEvent) {
	if r.mode != OutputModeFolded {
		return
	}
	r.mu.Lock()
	r.toolStarted++
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
	failed := evt.Status == "failed"
	if failed {
		r.toolFailed++
		r.latest = "❌ " + EscapeDiscordMarkdown(title)
	} else if evt.Status == "completed" {
		r.toolCompleted++
		r.latest = "✅ " + EscapeDiscordMarkdown(title)
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

func (r *threadProgressReporter) Thought(text string) {
	if r.mode != OutputModeFolded || text == "" {
		return
	}
	if len(text) > 160 {
		text = truncateUTF8(text, 157) + "…"
	}
	r.setLatest("💭 " + EscapeDiscordMarkdown(text))
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

func (r *threadProgressReporter) setLatest(latest string) {
	r.mu.Lock()
	r.latest = latest
	r.mu.Unlock()
	r.update(false)
}

func (r *threadProgressReporter) complete(status, reason string) bool {
	if r.mode != OutputModeFolded {
		return true
	}
	r.mu.Lock()
	r.failedReason = reason
	r.mu.Unlock()
	return r.updateStatus(status, true)
}

func (r *threadProgressReporter) update(force bool) bool { return r.updateStatus("running", force) }

func (r *threadProgressReporter) updateStatus(status string, force bool) bool {
	if r == nil || r.mode != OutputModeFolded {
		return true
	}
	r.mu.Lock()
	if r.messageID == "" {
		r.mu.Unlock()
		return false
	}
	now := time.Now()
	if !force && !r.lastEdit.IsZero() && now.Sub(r.lastEdit) < foldedProgressEditInterval {
		r.mu.Unlock()
		return true
	}
	content := r.renderLocked(status)
	r.lastEdit = now
	r.mu.Unlock()
	if err := editMessageWithMentions(r.ds, r.threadID, r.messageID, content, nil); err != nil {
		log.Printf("[progress] edit folded progress thread=%s message=%s: %v", r.threadID, r.messageID, err)
		return false
	}
	return true
}

func (r *threadProgressReporter) renderLocked(status string) string {
	elapsed := formatFoldedElapsed(time.Since(r.startedAt))
	running := r.toolStarted - r.toolCompleted - r.toolFailed
	if running < 0 {
		running = 0
	}
	var msg string
	switch status {
	case "success":
		msg = L.Getf("worker.progress.folded.completed", elapsed, r.toolCompleted, r.toolFailed)
	case "failed":
		msg = L.Getf("worker.progress.folded.failed", elapsed, r.toolCompleted, r.toolFailed)
	default:
		msg = L.Getf("worker.progress.folded.running", elapsed, r.toolCompleted, running, r.toolFailed)
	}
	if r.latest != "" && status == "running" {
		msg += "\n" + L.Getf("worker.progress.folded.latest", r.latest)
	}
	if r.failedReason != "" && status == "failed" {
		msg += "\n" + L.Getf("worker.progress.folded.reason", EscapeDiscordMarkdown(r.failedReason))
	}
	return msg
}

func formatFoldedElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
}
