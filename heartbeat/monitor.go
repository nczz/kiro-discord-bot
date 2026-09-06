package heartbeat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/robfig/cron/v3"

	L "github.com/nczz/kiro-discord-bot/locale"
)

const (
	monitorSummaryLimit = 1200
	monitorReasonLimit  = 500
	monitorMessageLimit = 4000
)

// MonitorResult is the structured result expected from a monitor evaluation.
type MonitorResult struct {
	ConditionMatched bool   `json:"condition_matched"`
	VisibleMessage   string `json:"visible_message"`
	InternalSummary  string `json:"internal_summary"`
	Reason           string `json:"reason"`
}

// MonitorDeps abstracts dependencies for the monitor task.
type MonitorDeps interface {
	StartTempAgent(name, cwd, model, channelID string) (*acp.Agent, error)
	StopTempAgent(agent *acp.Agent)
	ChannelInitialized(channelID string) bool
	ChannelCWD(channelID string) string
	EvaluateMonitor(ctx context.Context, agent *acp.Agent, job *MonitorJob, prompt string) (MonitorResult, error)
	DeliverMonitorNotification(agent *acp.Agent, job *MonitorJob, result MonitorResult, manual bool, startedAt time.Time) (threadID string, responseSent bool, err error)
	RecordMonitorUsage(agent *acp.Agent, job *MonitorJob, threadID, status string)
	RecordMonitorResult(agent *acp.Agent, job *MonitorJob, threadID, status string, result MonitorResult, responseSent bool)
	Notify(channelID, msg string)
}

// MonitorTask checks and executes background monitor jobs.
type MonitorTask struct {
	store    *MonitorStore
	deps     MonitorDeps
	dataDir  string
	location *time.Location
	parser   cron.Parser
	running  sync.Map
	guildID  string
	timeout  time.Duration
}

func NewMonitorTask(store *MonitorStore, deps MonitorDeps, dataDir string, tz string, guildID string, timeoutMin int) *MonitorTask {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.Now().Location()
	}
	if timeoutMin <= 0 {
		timeoutMin = 5
	}
	return &MonitorTask{
		store:    store,
		deps:     deps,
		dataDir:  dataDir,
		location: loc,
		parser:   cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		guildID:  guildID,
		timeout:  time.Duration(timeoutMin) * time.Minute,
	}
}

func (m *MonitorTask) Name() string { return "monitor" }

func (m *MonitorTask) ShouldRun(_ time.Time) bool { return true }

func (m *MonitorTask) Run() error {
	if created := m.store.IngestPending(); len(created) > 0 {
		for _, id := range created {
			if job, ok := m.store.Get(id); ok {
				m.RecalcNextRun(job)
			}
		}
	}
	now := time.Now().In(m.location)
	for _, job := range m.store.All() {
		if !job.Enabled && !job.RunOnce {
			continue
		}
		if m.guildID != "" && job.GuildID != m.guildID {
			continue
		}
		if _, loaded := m.running.LoadOrStore(job.ID, true); loaded {
			continue
		}
		if !m.isDue(job, now) {
			m.running.Delete(job.ID)
			continue
		}
		go m.execute(job, now)
	}
	return nil
}

// RunNow triggers immediate background evaluation by ID. It does not force visible output.
func (m *MonitorTask) RunNow(jobID string) {
	job, ok := m.store.Get(jobID)
	if !ok {
		return
	}
	if _, loaded := m.running.LoadOrStore(job.ID, true); loaded {
		log.Printf("[monitor] run-now ignored job %s (%s): already running", job.ID, job.Name)
		return
	}
	go m.execute(job, time.Now().In(m.location))
}

func (m *MonitorTask) isDue(job *MonitorJob, now time.Time) bool {
	if job.RunOnce {
		return true
	}
	if job.NextRun == "" {
		next, err := m.computeNext(job.Schedule, job.CreatedAt)
		if err != nil {
			return false
		}
		job.NextRun = next.Format(time.RFC3339)
		if err := m.store.Update(job); err != nil {
			log.Printf("[monitor] save next run for %s: %v", job.ID, err)
		}
	}
	if job.NextRun == "" {
		return false
	}
	nextRun, err := time.ParseInLocation(time.RFC3339, job.NextRun, m.location)
	if err != nil {
		return false
	}
	return !now.Before(nextRun)
}

func (m *MonitorTask) computeNext(schedule string, afterStr string) (time.Time, error) {
	sched, err := m.parser.Parse(schedule)
	if err != nil {
		return time.Time{}, err
	}
	after := time.Now().In(m.location)
	if afterStr != "" {
		if t, err := time.ParseInLocation(time.RFC3339, afterStr, m.location); err == nil {
			after = t
		}
	}
	return sched.Next(after), nil
}

func (m *MonitorTask) execute(job *MonitorJob, now time.Time) {
	defer m.running.Delete(job.ID)
	manual := job.RunOnce
	start := time.Now()

	if !m.deps.ChannelInitialized(job.ChannelID) {
		msg := L.Getf("setup.required.command", "monitor")
		log.Printf("[monitor] blocked job %s (%s): channel %s is not initialized", job.ID, job.Name, job.ChannelID)
		job.LastCheckAt = now.Format(time.RFC3339)
		m.saveHistory(job.ID, job.HistoryLimit, MonitorHistory{Timestamp: now.Format(time.RFC3339), CheckPrompt: job.CheckPrompt, NotifyWhen: job.NotifyWhen, Status: MonitorStatusError, Reason: msg, DurationSec: int(time.Since(start).Seconds())})
		m.finishJob(job, now, manual)
		return
	}

	history := m.loadHistory(job.ID, job.HistoryLimit)
	prompt := m.buildPrompt(job, history)
	cwd := m.deps.ChannelCWD(job.ChannelID)
	agentName := "monitor-" + job.ID
	agent, err := m.deps.StartTempAgent(agentName, cwd, job.Model, job.ChannelID)
	if err != nil {
		log.Printf("[monitor] start agent for %s failed: %v", job.ID, err)
		reason := m.userFacingError(err)
		result := MonitorResult{Reason: reason, InternalSummary: reason}
		m.deps.RecordMonitorResult(agent, job, "", MonitorStatusError, result, false)
		m.saveHistory(job.ID, job.HistoryLimit, MonitorHistory{Timestamp: now.Format(time.RFC3339), CheckPrompt: job.CheckPrompt, NotifyWhen: job.NotifyWhen, Status: MonitorStatusError, Reason: reason, InternalSummary: reason, DurationSec: int(time.Since(start).Seconds())})
		job.LastCheckAt = now.Format(time.RFC3339)
		m.finishJob(job, now, manual)
		return
	}
	defer m.deps.StopTempAgent(agent)

	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	result, err := m.deps.EvaluateMonitor(ctx, agent, job, prompt)
	duration := int(time.Since(start).Seconds())
	if err != nil {
		log.Printf("[monitor] evaluate job %s failed: %v", job.ID, err)
		result = MonitorResult{Reason: m.userFacingError(err), InternalSummary: m.userFacingError(err)}
		m.deps.RecordMonitorUsage(agent, job, "", MonitorStatusError)
		m.deps.RecordMonitorResult(agent, job, "", MonitorStatusError, result, false)
		m.saveHistory(job.ID, job.HistoryLimit, MonitorHistory{Timestamp: now.Format(time.RFC3339), CheckPrompt: job.CheckPrompt, NotifyWhen: job.NotifyWhen, Status: MonitorStatusError, Reason: result.Reason, InternalSummary: result.InternalSummary, DurationSec: duration})
		job.LastCheckAt = now.Format(time.RFC3339)
		m.finishJob(job, now, manual)
		return
	}

	status := MonitorStatusSuppressed
	threadID := ""
	responseSent := false
	if result.ConditionMatched {
		current, ok := m.store.Get(job.ID)
		if !ok {
			log.Printf("[monitor] skip notification for deleted job %s (%s)", job.ID, job.Name)
			return
		}
		if !current.Enabled && !manual {
			result.ConditionMatched = false
			result.VisibleMessage = ""
			result.Reason = L.Get("monitor.exec.skipped_disabled")
			result.InternalSummary = result.Reason
		} else {
			status = MonitorStatusMatched
			job = current
			threadID, responseSent, err = m.deps.DeliverMonitorNotification(agent, job, result, manual, start)
			if threadID != "" && threadID != job.ThreadID {
				job.ThreadID = threadID
			}
			if err == nil {
				job.LastNotifyAt = now.Format(time.RFC3339)
			} else {
				status = MonitorStatusError
				result.Reason = m.userFacingError(err)
				result.InternalSummary = result.Reason
			}
		}
	}

	job.LastCheckAt = now.Format(time.RFC3339)
	m.deps.RecordMonitorUsage(agent, job, threadID, status)
	m.deps.RecordMonitorResult(agent, job, threadID, status, result, responseSent)
	m.saveHistory(job.ID, job.HistoryLimit, MonitorHistory{Timestamp: now.Format(time.RFC3339), CheckPrompt: job.CheckPrompt, NotifyWhen: job.NotifyWhen, Status: status, ConditionMatched: result.ConditionMatched, VisibleMessage: result.VisibleMessage, InternalSummary: result.InternalSummary, Reason: result.Reason, DurationSec: duration})
	m.finishJob(job, now, manual)
}

func (m *MonitorTask) userFacingError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return L.Getf("monitor.exec.timeout_reason", m.timeoutMinutes())
	}
	return L.Get("monitor.exec.error_reason")
}

func (m *MonitorTask) timeoutMinutes() int {
	minutes := int(m.timeout / time.Minute)
	if minutes <= 0 {
		return 5
	}
	return minutes
}

func (m *MonitorTask) finishJob(job *MonitorJob, after time.Time, clearRunOnce bool) {
	current, ok := m.store.Get(job.ID)
	if !ok {
		log.Printf("[monitor] skip finish for deleted job %s (%s)", job.ID, job.Name)
		return
	}
	if job.LastCheckAt != "" {
		current.LastCheckAt = job.LastCheckAt
	}
	if job.LastNotifyAt != "" {
		current.LastNotifyAt = job.LastNotifyAt
	}
	if job.ThreadID != "" {
		current.ThreadID = job.ThreadID
	}
	if clearRunOnce && current.RunOnce {
		current.RunOnce = false
	}
	m.advanceNextRun(current, after)
}

func (m *MonitorTask) advanceNextRun(job *MonitorJob, after time.Time) {
	sched, err := m.parser.Parse(job.Schedule)
	if err != nil {
		return
	}
	job.NextRun = sched.Next(after).Format(time.RFC3339)
	if err := m.store.Update(job); err != nil {
		log.Printf("[monitor] advance next run for %s: %v", job.ID, err)
	}
}

// RecalcNextRun recomputes next_run for one monitor based on current time and timezone.
func (m *MonitorTask) RecalcNextRun(job *MonitorJob) {
	m.recalcNextRunAfter(job, time.Now().In(m.location))
}

func (m *MonitorTask) recalcNextRunAfter(job *MonitorJob, after time.Time) {
	sched, err := m.parser.Parse(job.Schedule)
	if err != nil {
		return
	}
	job.NextRun = sched.Next(after.In(m.location)).Format(time.RFC3339)
	if err := m.store.Update(job); err != nil {
		log.Printf("[monitor] recalc next run for %s: %v", job.ID, err)
	}
}

// RecalcAll recomputes future next_run values for enabled monitor jobs.
func (m *MonitorTask) RecalcAll() {
	m.recalcAllAt(time.Now().In(m.location))
}

func (m *MonitorTask) recalcAllAt(now time.Time) {
	for _, job := range m.store.All() {
		if !job.Enabled {
			continue
		}
		if job.NextRun != "" {
			nextRun, err := time.ParseInLocation(time.RFC3339, job.NextRun, m.location)
			if err == nil && !now.Before(nextRun) {
				continue
			}
		}
		m.recalcNextRunAfter(job, now)
	}
	log.Printf("[monitor] recalculated next_run for all enabled jobs")
}

func (m *MonitorTask) buildPrompt(job *MonitorJob, history []MonitorHistory) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Discord context] channel_id=%s guild_id=%s\n\n", job.ChannelID, job.GuildID))
	sb.WriteString("[Background monitor]\n")
	sb.WriteString("You are evaluating a scheduled background monitor. Do not produce ordinary prose. Return ONLY a JSON object matching this schema:\n")
	sb.WriteString(`{"condition_matched":false,"visible_message":"","internal_summary":"short private summary","reason":"why the condition did or did not match"}`)
	sb.WriteString("\nRules:\n")
	sb.WriteString("- condition_matched=true only when the notify condition is satisfied.\n")
	sb.WriteString("- condition_matched=true requires visible_message with the Discord message to publish.\n")
	sb.WriteString("- condition_matched=false must use an empty visible_message.\n")
	sb.WriteString("- internal_summary is for private monitor history; keep it concise and avoid secrets.\n")
	sb.WriteString("- Do not call Discord egress tools; the bot decides whether to publish after parsing this JSON.\n\n")
	sb.WriteString("[Monitor name]\n")
	sb.WriteString(job.Name)
	sb.WriteString("\n\n[Check task]\n")
	sb.WriteString(job.CheckPrompt)
	sb.WriteString("\n\n[Notify only when]\n")
	sb.WriteString(job.NotifyWhen)
	sb.WriteString("\n")
	if len(history) > 0 {
		sb.WriteString("\n[Previous monitor summaries]\n")
		for _, h := range history {
			ts, _ := time.Parse(time.RFC3339, h.Timestamp)
			when := h.Timestamp
			if !ts.IsZero() {
				when = ts.In(m.location).Format("01/02 15:04")
			}
			line := fmt.Sprintf("[%s] %s", when, h.Status)
			if h.Reason != "" {
				line += ": " + h.Reason
			}
			if h.InternalSummary != "" {
				line += " — " + h.InternalSummary
			}
			sb.WriteString(truncateRunes(line, monitorSummaryLimit))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

var monitorCodeBlockRe = regexp.MustCompile("(?s)^```(?:json)?\\s*\n?(.*?)\\s*```$")

// ParseMonitorResult parses and validates the JSON-only monitor output contract.
func ParseMonitorResult(raw string) (MonitorResult, error) {
	s := strings.TrimSpace(raw)
	if m := monitorCodeBlockRe.FindStringSubmatch(s); m != nil {
		s = strings.TrimSpace(m[1])
	}
	if idx := strings.Index(s, "{"); idx >= 0 {
		if end := strings.LastIndex(s, "}"); end > idx {
			s = s[idx : end+1]
		}
	}
	var result MonitorResult
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return MonitorResult{}, fmt.Errorf("invalid monitor JSON: %w", err)
	}
	result.VisibleMessage = strings.TrimSpace(result.VisibleMessage)
	result.InternalSummary = truncateRunes(strings.TrimSpace(result.InternalSummary), monitorSummaryLimit)
	result.Reason = truncateRunes(strings.TrimSpace(result.Reason), monitorReasonLimit)
	if result.ConditionMatched {
		if result.VisibleMessage == "" {
			return MonitorResult{}, fmt.Errorf("visible_message is required when condition_matched is true")
		}
		result.VisibleMessage = truncateRunes(result.VisibleMessage, monitorMessageLimit)
	} else {
		result.VisibleMessage = ""
	}
	if result.InternalSummary == "" {
		result.InternalSummary = result.Reason
	}
	return result, nil
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return string(runes[:1])
	}
	return string(runes[:max-1]) + "…"
}

func (m *MonitorTask) historyPath(jobID string) string {
	dir := filepath.Join(m.dataDir, "monitor", jobID)
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "history.jsonl")
}

func (m *MonitorTask) loadHistory(jobID string, limit int) []MonitorHistory {
	if limit <= 0 {
		limit = 10
	}
	f, err := os.Open(m.historyPath(jobID))
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []MonitorHistory
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var h MonitorHistory
		if json.Unmarshal(scanner.Bytes(), &h) == nil {
			all = append(all, h)
		}
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all
}

// LatestHistory returns the latest stored monitor history row, if present.
func (m *MonitorTask) LatestHistory(jobID string) (MonitorHistory, bool) {
	rows := m.loadHistory(jobID, 1)
	if len(rows) == 0 {
		return MonitorHistory{}, false
	}
	return rows[len(rows)-1], true
}

func (m *MonitorTask) saveHistory(jobID string, limit int, entry MonitorHistory) {
	if limit <= 0 {
		limit = 10
	}
	rows := m.loadHistory(jobID, limit-1)
	rows = append(rows, entry)
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	f, err := os.OpenFile(m.historyPath(jobID), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, row := range rows {
		_ = enc.Encode(row)
	}
}
