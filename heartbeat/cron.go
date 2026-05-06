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
	StartTempAgent(name, cwd, model string) (*acp.Agent, error)
	StopTempAgent(agent *acp.Agent)
	AskAgentInThread(ctx context.Context, agent *acp.Agent, channelID, threadName, threadID, prompt, mentionID string) (response string, usedThreadID string, err error)
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
	return len(c.store.All()) > 0
}

func (c *CronTask) Run() error {
	now := time.Now().In(c.location)
	for _, job := range c.store.All() {
		if !job.Enabled {
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
	c.deps.Notify(job.ChannelID, L.Getf("cron.exec.running", label, job.Name))

	// Load history (skip for one-shot)
	var history []CronHistory
	if !job.OneShot {
		history = c.loadHistory(job.ID, job.HistoryLimit)
	}
	prompt := c.buildPrompt(job, history)

	// Start temp agent with model
	cwd := job.CWD
	if cwd == "" {
		cwd = "/tmp"
	}
	agent, err := c.deps.StartTempAgent(agentName, cwd, job.Model)
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
	response, usedThreadID, err := c.deps.AskAgentInThread(ctx, agent, job.ChannelID, threadName, job.ThreadID, prompt, job.MentionID)
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

	c.saveHistory(job.ID, CronHistory{
		Timestamp: now.Format(time.RFC3339), Prompt: job.Prompt, Response: response, Status: status, DurationSec: duration,
	})
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
			sb.WriteString(fmt.Sprintf("[%s] (%s) %s\n", ts.Format("01/02 15:04"), h.Status, h.Response))
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
