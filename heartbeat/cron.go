package heartbeat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/robfig/cron/v3"

	L "github.com/nczz/kiro-discord-bot/locale"
)

// CronHistory is a single execution record.
type CronHistory struct {
	Timestamp   string `json:"ts"`
	Prompt      string `json:"prompt"`
	Response    string `json:"response"`
	FullLog     string `json:"full_log,omitempty"`
	Status      string `json:"status"` // "ok" or "error"
	DurationSec int    `json:"duration_sec"`
}

// CronDeps abstracts dependencies for the cron task.
type CronDeps interface {
	StartTempAgent(name, cwd, model, channelID string) (*acp.Agent, error)
	StopTempAgent(agent *acp.Agent)
	ChannelInitialized(channelID string) bool
	ChannelCWD(channelID string) string
	AskAgentInThread(ctx context.Context, agent *acp.Agent, job *CronJob, threadName, prompt string) (response string, usedThreadID string, responseSent bool, err error)
	RecordAgentUsage(agent *acp.Agent, job *CronJob, threadID, status string)
	RecordAgentResponse(agent *acp.Agent, job *CronJob, threadID, status, content string, responseSent bool)
	Notify(channelID, msg string)
}

// CronTask checks and executes due cron jobs.
type CronTask struct {
	store    *CronStore
	deps     CronDeps
	dataDir  string
	location *time.Location
	parser   cron.Parser
	running  sync.Map // job ID → bool
	guildID  string
}

func NewCronTask(store *CronStore, deps CronDeps, dataDir string, tz string, guildID string) *CronTask {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.Now().Location()
	}
	return &CronTask{
		store:    store,
		deps:     deps,
		dataDir:  dataDir,
		location: loc,
		parser:   cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		guildID:  guildID,
	}
}

func (c *CronTask) Name() string { return "cron" }

func (c *CronTask) ShouldRun(_ time.Time) bool {
	// Pending MCP cron actions must be ingested even before any jobs exist.
	return true
}

func (c *CronTask) Run() error {
	// Ingest pending actions from MCP bot-tools.
	if created := c.store.IngestPending(); len(created) > 0 {
		for _, id := range created {
			if job, ok := c.store.Get(id); ok {
				c.RecalcNextRun(job)
			}
		}
	}

	now := time.Now().In(c.location)
	for _, job := range c.store.All() {
		if !job.Enabled && !job.RunOnce {
			continue
		}
		if c.guildID != "" && job.GuildID != c.guildID {
			continue
		}
		if _, loaded := c.running.LoadOrStore(job.ID, true); loaded {
			continue
		}
		if !c.isDue(job, now) {
			c.running.Delete(job.ID)
			continue
		}
		go c.execute(job, now)
	}
	return nil
}

// RunNow triggers immediate execution of a job by ID. Safe to call concurrently.
func (c *CronTask) RunNow(jobID string) {
	job, ok := c.store.Get(jobID)
	if !ok {
		return
	}
	if _, loaded := c.running.LoadOrStore(job.ID, true); loaded {
		log.Printf("[cron] run-now ignored job %s (%s): already running", job.ID, job.Name)
		return // already running
	}
	go c.execute(job, time.Now().In(c.location))
}

func (c *CronTask) isDue(job *CronJob, now time.Time) bool {
	if job.NextRun == "" && !job.OneShot {
		// First run — compute next from schedule
		next, err := c.computeNext(job.Schedule, job.CreatedAt)
		if err != nil {
			return false
		}
		job.NextRun = next.Format(time.RFC3339)
		if err := c.store.Update(job); err != nil {
			log.Printf("[cron] save next run for %s: %v", job.ID, err)
		}
	}
	if job.NextRun == "" {
		return false
	}
	nextRun, err := time.ParseInLocation(time.RFC3339, job.NextRun, c.location)
	if err != nil {
		return false
	}
	return !now.Before(nextRun)
}

func (c *CronTask) computeNext(schedule string, afterStr string) (time.Time, error) {
	sched, err := c.parser.Parse(schedule)
	if err != nil {
		return time.Time{}, err
	}
	after := time.Now().In(c.location)
	if afterStr != "" {
		if t, err := time.ParseInLocation(time.RFC3339, afterStr, c.location); err == nil {
			after = t
		}
	}
	return sched.Next(after), nil
}

func (c *CronTask) execute(job *CronJob, now time.Time) {
	defer c.running.Delete(job.ID)

	// Simple notify mode (no agent)
	if job.OneShot && !job.UseAgent {
		mention := ""
		if job.MentionID != "" {
			mention = fmt.Sprintf("<@%s> ", job.MentionID)
		}
		c.deps.Notify(job.ChannelID, fmt.Sprintf("🔔 %s%s", mention, job.Prompt))
		c.finishJob(job, now)
		return
	}

	agentName := "cron-" + job.ID
	start := time.Now()

	label := L.Get("cron.label.cron")
	if job.OneShot {
		label = L.Get("cron.label.reminder")
	}
	log.Printf("[cron] executing job %s (%s)", job.ID, job.Name)

	if !c.deps.ChannelInitialized(job.ChannelID) {
		msg := L.Getf("setup.required.command", "cron")
		log.Printf("[cron] blocked job %s (%s): channel %s is not initialized", job.ID, job.Name, job.ChannelID)
		c.deps.Notify(job.ChannelID, msg)
		if !job.OneShot {
			c.saveHistory(job.ID, CronHistory{
				Timestamp: now.Format(time.RFC3339), Prompt: job.Prompt, Response: msg, Status: "error",
				DurationSec: int(time.Since(start).Seconds()),
			})
		}
		if job.RunOnce {
			job.RunOnce = false
		}
		c.finishJob(job, now)
		return
	}

	// Load history (skip for one-shot)
	var history []CronHistory
	if !job.OneShot {
		history = c.loadHistory(job.ID, job.HistoryLimit)
	}
	prompt := c.buildPrompt(job, history)

	// Start temp agent with the current channel CWD. CronJob.CWD is kept only for
	// backward-compatible JSON loading and is intentionally ignored.
	cwd := c.deps.ChannelCWD(job.ChannelID)
	agent, err := c.deps.StartTempAgent(agentName, cwd, job.Model, job.ChannelID)
	if err != nil {
		log.Printf("[cron] start agent for %s failed: %v", job.ID, err)
		c.deps.Notify(job.ChannelID, L.Getf("cron.exec.start_failed", label, job.Name, err.Error()))
		c.saveHistory(job.ID, CronHistory{
			Timestamp: now.Format(time.RFC3339), Prompt: job.Prompt, Response: err.Error(), Status: "error",
			DurationSec: int(time.Since(start).Seconds()),
		})
		c.finishJob(job, now)
		return
	}
	defer c.deps.StopTempAgent(agent)

	// Ask
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	threadName := "⏰ " + job.Name
	response, usedThreadID, responseSent, err := c.deps.AskAgentInThread(ctx, agent, job, threadName, prompt)
	duration := int(time.Since(start).Seconds())
	status := "ok"

	// Persist thread ID for reuse on next run
	if usedThreadID != "" && usedThreadID != job.ThreadID {
		job.ThreadID = usedThreadID
		_ = c.store.Update(job)
	}

	if err != nil {
		response = err.Error()
		status = "error"
		c.deps.Notify(job.ChannelID, L.Getf("cron.exec.failed", label, job.Name, err.Error()))
	}
	c.deps.RecordAgentUsage(agent, job, usedThreadID, status)
	c.deps.RecordAgentResponse(agent, job, usedThreadID, status, response, responseSent)

	c.saveHistory(job.ID, CronHistory{
		Timestamp: now.Format(time.RFC3339), Prompt: job.Prompt, Response: response, Status: status, DurationSec: duration,
	})
	if job.RunOnce {
		job.RunOnce = false
		_ = c.store.Update(job)
	}
	c.finishJob(job, now)
}

func (c *CronTask) buildPrompt(job *CronJob, history []CronHistory) string {
	var sb strings.Builder
	// Inject Discord context for MCP tools
	sb.WriteString(fmt.Sprintf("[Discord context] channel_id=%s guild_id=%s\n\n", job.ChannelID, job.GuildID))

	if job.OneShot {
		sb.WriteString(L.Get("cron.prompt.reminder_header"))
		sb.WriteString(job.Prompt)
		return sb.String()
	}

	sb.WriteString(L.Getf("cron.prompt.task_header", job.Name))
	if len(history) > 0 {
		sb.WriteString(L.Get("cron.prompt.history_header"))
		for _, h := range history {
			ts, _ := time.Parse(time.RFC3339, h.Timestamp)
			sb.WriteString(fmt.Sprintf("[%s] (%s) %s\n", ts.In(c.location).Format("01/02 15:04"), h.Status, h.Response))
		}
		sb.WriteString("---\n\n")
	}
	sb.WriteString(L.Get("cron.prompt.execute"))
	sb.WriteString(job.Prompt)
	return sb.String()
}

func (c *CronTask) finishJob(job *CronJob, after time.Time) {
	if job.OneShot {
		if err := c.store.Remove(job.ID); err != nil {
			log.Printf("[cron] remove one-shot %s: %v", job.ID, err)
		}
		log.Printf("[cron] one-shot job %s (%s) completed and removed", job.ID, job.Name)
		return
	}
	c.advanceNextRun(job, after)
}

func (c *CronTask) advanceNextRun(job *CronJob, after time.Time) {
	sched, err := c.parser.Parse(job.Schedule)
	if err != nil {
		return
	}
	job.LastRun = after.Format(time.RFC3339)
	job.NextRun = sched.Next(after).Format(time.RFC3339)
	if err := c.store.Update(job); err != nil {
		log.Printf("[cron] advance next run for %s: %v", job.ID, err)
	}
}

// RecalcNextRun recomputes next_run for a single job based on current time and timezone.
func (c *CronTask) RecalcNextRun(job *CronJob) {
	c.recalcNextRunAfter(job, time.Now().In(c.location))
}

func (c *CronTask) recalcNextRunAfter(job *CronJob, after time.Time) {
	sched, err := c.parser.Parse(job.Schedule)
	if err != nil {
		return
	}
	job.NextRun = sched.Next(after.In(c.location)).Format(time.RFC3339)
	if err := c.store.Update(job); err != nil {
		log.Printf("[cron] recalc next run for %s: %v", job.ID, err)
	}
}

// RecalcAll recomputes future next_run values for all enabled jobs. Overdue
// jobs keep their persisted next_run so the next scheduler tick can run them.
func (c *CronTask) RecalcAll() {
	c.recalcAllAt(time.Now().In(c.location))
}

func (c *CronTask) recalcAllAt(now time.Time) {
	for _, job := range c.store.All() {
		if !job.Enabled || job.OneShot {
			continue
		}
		if job.NextRun != "" {
			nextRun, err := time.ParseInLocation(time.RFC3339, job.NextRun, c.location)
			if err == nil && !now.Before(nextRun) {
				continue
			}
		}
		c.recalcNextRunAfter(job, now)
	}
	log.Printf("[cron] recalculated next_run for all enabled jobs")
}

func (c *CronTask) historyPath(jobID string) string {
	dir := filepath.Join(c.dataDir, "cron", jobID)
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "history.jsonl")
}

func (c *CronTask) loadHistory(jobID string, limit int) []CronHistory {
	f, err := os.Open(c.historyPath(jobID))
	if err != nil {
		return nil
	}
	defer f.Close()

	var all []CronHistory
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var h CronHistory
		if json.Unmarshal(scanner.Bytes(), &h) == nil {
			all = append(all, h)
		}
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all
}

func (c *CronTask) saveHistory(jobID string, entry CronHistory) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')
	f, err := os.OpenFile(c.historyPath(jobID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(data)
}
