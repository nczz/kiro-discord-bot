package heartbeat

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	MonitorStatusMatched    = "matched"
	MonitorStatusSuppressed = "suppressed"
	MonitorStatusError      = "error"
)

const (
	MonitorNameMaxRunes        = 100
	MonitorScheduleMaxRunes    = 100
	MonitorCheckPromptMaxRunes = 2000
	MonitorNotifyWhenMaxRunes  = 1000
)

// MonitorJob defines a background monitor that only notifies Discord when its condition matches.
type MonitorJob struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ChannelID     string `json:"channel_id"`
	GuildID       string `json:"guild_id"`
	Schedule      string `json:"schedule"`
	ScheduleHuman string `json:"schedule_human"`
	CheckPrompt   string `json:"check_prompt"`
	NotifyWhen    string `json:"notify_when"`
	Model         string `json:"model,omitempty"`
	Enabled       bool   `json:"enabled"`
	CreatedBy     string `json:"created_by"`
	CreatedByID   string `json:"created_by_id,omitempty"`
	CreatedAt     string `json:"created_at"`
	LastCheckAt   string `json:"last_check_at,omitempty"`
	LastNotifyAt  string `json:"last_notify_at,omitempty"`
	NextRun       string `json:"next_run,omitempty"`
	ThreadID      string `json:"thread_id,omitempty"`
	HistoryLimit  int    `json:"history_limit"`
	RunOnce       bool   `json:"run_once,omitempty"`
}

// MonitorHistory is one background monitor execution record.
type MonitorHistory struct {
	Timestamp        string `json:"ts"`
	CheckPrompt      string `json:"check_prompt"`
	NotifyWhen       string `json:"notify_when"`
	Status           string `json:"status"`
	ConditionMatched bool   `json:"condition_matched"`
	VisibleMessage   string `json:"visible_message,omitempty"`
	InternalSummary  string `json:"internal_summary,omitempty"`
	Reason           string `json:"reason,omitempty"`
	DurationSec      int    `json:"duration_sec"`
}

// MonitorUpdate is a validated partial update for a monitor job.
type MonitorUpdate struct {
	Name        *string `json:"name,omitempty"`
	Schedule    *string `json:"schedule,omitempty"`
	CheckPrompt *string `json:"check_prompt,omitempty"`
	NotifyWhen  *string `json:"notify_when,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

// ValidateMonitorUpdate validates a partial monitor update.
func ValidateMonitorUpdate(update MonitorUpdate) error {
	if update.Name == nil && update.Schedule == nil && update.CheckPrompt == nil && update.NotifyWhen == nil && update.Enabled == nil {
		return fmt.Errorf("at least one of name, schedule, check_prompt, notify_when, or enabled is required")
	}
	if update.Name != nil {
		if err := validateMonitorName(*update.Name); err != nil {
			return err
		}
	}
	if update.Schedule != nil {
		scheduleHuman := strings.TrimSpace(*update.Schedule)
		if scheduleHuman == "" {
			return fmt.Errorf("schedule cannot be empty")
		}
		if len([]rune(scheduleHuman)) > MonitorScheduleMaxRunes {
			return fmt.Errorf("schedule cannot exceed %d characters", MonitorScheduleMaxRunes)
		}
		if _, _, err := ParseMonitorScheduleInput(scheduleHuman); err != nil {
			return fmt.Errorf("invalid schedule: %w", err)
		}
	}
	if update.CheckPrompt != nil {
		if err := validateMonitorTextField("check_prompt", *update.CheckPrompt, MonitorCheckPromptMaxRunes); err != nil {
			return err
		}
	}
	if update.NotifyWhen != nil {
		if err := validateMonitorTextField("notify_when", *update.NotifyWhen, MonitorNotifyWhenMaxRunes); err != nil {
			return err
		}
	}
	return nil
}

// ApplyMonitorUpdate mutates a monitor job and reports whether next_run must be recalculated.
func ApplyMonitorUpdate(job *MonitorJob, update MonitorUpdate) (bool, error) {
	if job == nil {
		return false, fmt.Errorf("monitor job is required")
	}
	if err := ValidateMonitorUpdate(update); err != nil {
		return false, err
	}
	wasEnabled := job.Enabled
	scheduleChanged := false
	if update.Name != nil {
		job.Name = strings.TrimSpace(*update.Name)
	}
	if update.Schedule != nil {
		scheduleHuman := strings.TrimSpace(*update.Schedule)
		cronExpr, normalizedHuman, _ := ParseMonitorScheduleInput(scheduleHuman)
		scheduleChanged = cronExpr != job.Schedule
		job.ScheduleHuman = normalizedHuman
		job.Schedule = cronExpr
	}
	if update.CheckPrompt != nil {
		job.CheckPrompt = strings.TrimSpace(*update.CheckPrompt)
	}
	if update.NotifyWhen != nil {
		job.NotifyWhen = strings.TrimSpace(*update.NotifyWhen)
	}
	if update.Enabled != nil {
		job.Enabled = *update.Enabled
	}
	return scheduleChanged || (!wasEnabled && job.Enabled), nil
}

// MonitorStore persists monitor jobs to a JSON file.
type MonitorStore struct {
	mu   sync.RWMutex
	path string
	jobs map[string]*MonitorJob
}

func NewMonitorStore(dataDir string) (*MonitorStore, error) {
	dir := filepath.Join(dataDir, "monitor")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	s := &MonitorStore{path: filepath.Join(dir, "monitor.json"), jobs: make(map[string]*MonitorJob)}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (s *MonitorStore) Add(job *MonitorJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.normalize()
	if err := validateMonitorJobText(job); err != nil {
		return err
	}
	if job.ID == "" {
		job.ID = randomID()
	}
	if job.HistoryLimit == 0 {
		job.HistoryLimit = 10
	}
	if job.CreatedAt == "" {
		job.CreatedAt = time.Now().Format(time.RFC3339)
	}
	stored := *job
	previous, existed := s.jobs[stored.ID]
	var previousCopy MonitorJob
	if existed && previous != nil {
		previousCopy = *previous
	}
	s.jobs[stored.ID] = &stored
	if err := s.save(); err != nil {
		if existed {
			s.jobs[stored.ID] = &previousCopy
		} else {
			delete(s.jobs, stored.ID)
		}
		return err
	}
	return nil
}

func (s *MonitorStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.jobs[id]
	delete(s.jobs, id)
	if err := s.save(); err != nil {
		if existed {
			s.jobs[id] = previous
		}
		return err
	}
	return nil
}

func (s *MonitorStore) Get(id string) (*MonitorJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	cp := *j
	return &cp, true
}

func (s *MonitorStore) Update(job *MonitorJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.normalize()
	if err := validateMonitorJobText(job); err != nil {
		return err
	}
	stored := *job
	previous, existed := s.jobs[stored.ID]
	var previousCopy MonitorJob
	if existed && previous != nil {
		previousCopy = *previous
	}
	s.jobs[stored.ID] = &stored
	if err := s.save(); err != nil {
		if existed {
			s.jobs[stored.ID] = &previousCopy
		} else {
			delete(s.jobs, stored.ID)
		}
		return err
	}
	return nil
}

func (s *MonitorStore) ListByChannel(channelID string) []*MonitorJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*MonitorJob
	for _, j := range s.jobs {
		if j.ChannelID == channelID {
			cp := *j
			out = append(out, &cp)
		}
	}
	return out
}

func (s *MonitorStore) All() []*MonitorJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*MonitorJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		cp := *j
		out = append(out, &cp)
	}
	return out
}

func (s *MonitorStore) FindByName(channelID, name string) (*MonitorJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, j := range s.jobs {
		if j.ChannelID == channelID && j.Name == name {
			cp := *j
			return &cp, true
		}
	}
	return nil, false
}

func (s *MonitorStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var jobs map[string]*MonitorJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return err
	}
	if jobs == nil {
		return fmt.Errorf("monitor store contains no jobs object")
	}
	dirty := false
	for id, job := range jobs {
		if job == nil {
			return fmt.Errorf("monitor store contains nil job %q", id)
		}
		before := *job
		job.normalize()
		if err := validateMonitorJobText(job); err != nil {
			return fmt.Errorf("monitor job %q is invalid: %w", id, err)
		}
		if before != *job {
			dirty = true
		}
	}
	s.jobs = jobs
	if dirty {
		return s.save()
	}
	return nil
}
func (s *MonitorStore) save() error {
	data, err := json.MarshalIndent(s.jobs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func ValidateMonitorName(name string) error {
	return validateMonitorName(name)
}

func validateMonitorName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if len([]rune(name)) > MonitorNameMaxRunes {
		return fmt.Errorf("name cannot exceed %d characters", MonitorNameMaxRunes)
	}
	return nil
}

func validateMonitorTextField(field, value string, maxRunes int) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s cannot be empty", field)
	}
	if len([]rune(value)) > maxRunes {
		return fmt.Errorf("%s cannot exceed %d characters", field, maxRunes)
	}
	return nil
}

func validateMonitorJobText(job *MonitorJob) error {
	if job == nil {
		return fmt.Errorf("monitor job is required")
	}
	if err := validateMonitorName(job.Name); err != nil {
		return err
	}
	if err := validateMonitorTextField("schedule", job.ScheduleHuman, MonitorScheduleMaxRunes); err != nil {
		return err
	}
	if _, err := ParseSchedule(job.Schedule); err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}
	if err := validateMonitorTextField("check_prompt", job.CheckPrompt, MonitorCheckPromptMaxRunes); err != nil {
		return err
	}
	return validateMonitorTextField("notify_when", job.NotifyWhen, MonitorNotifyWhenMaxRunes)
}

func (j *MonitorJob) normalize() {
	if j == nil {
		return
	}
	j.Name = strings.TrimSpace(j.Name)
	j.ChannelID = strings.TrimSpace(j.ChannelID)
	j.GuildID = strings.TrimSpace(j.GuildID)
	j.Schedule = strings.TrimSpace(j.Schedule)
	j.ScheduleHuman = strings.TrimSpace(j.ScheduleHuman)
	j.CheckPrompt = strings.TrimSpace(j.CheckPrompt)
	j.NotifyWhen = strings.TrimSpace(j.NotifyWhen)
	j.Model = ""
	j.CreatedBy = strings.TrimSpace(j.CreatedBy)
	j.CreatedByID = strings.TrimSpace(j.CreatedByID)
	if j.ScheduleHuman == "" {
		j.ScheduleHuman = j.Schedule
	}
}

// MonitorPendingAction represents a monitor action written by MCP bot-tools.
type MonitorPendingAction struct {
	Action    string         `json:"action"`
	Job       *MonitorJob    `json:"job,omitempty"`
	JobID     string         `json:"job_id,omitempty"`
	ChannelID string         `json:"channel_id,omitempty"`
	Update    *MonitorUpdate `json:"update,omitempty"`
}

// IngestPending scans pending monitor actions and returns IDs whose next_run must be recalculated.
func (s *MonitorStore) IngestPending() []string {
	dir := filepath.Join(filepath.Dir(s.path), "pending")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var recalcIDs []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("[monitor-ingest] read %s: %v", entry.Name(), err)
			continue
		}
		var action MonitorPendingAction
		if err := json.Unmarshal(data, &action); err != nil {
			log.Printf("[monitor-ingest] parse %s: %v", entry.Name(), err)
			os.Remove(path)
			continue
		}
		switch action.Action {
		case "create":
			if action.Job == nil {
				log.Printf("[monitor-ingest] %s: create action missing job", entry.Name())
				os.Remove(path)
				continue
			}
			action.Job.normalize()
			if action.Job.Name == "" || action.Job.Schedule == "" || action.Job.CheckPrompt == "" || action.Job.NotifyWhen == "" || action.Job.ChannelID == "" || action.Job.GuildID == "" {
				log.Printf("[monitor-ingest] %s: create action missing required fields", entry.Name())
				os.Remove(path)
				continue
			}
			cronExpr, scheduleHuman, err := ParseMonitorScheduleInput(action.Job.Schedule)
			if err != nil {
				log.Printf("[monitor-ingest] %s: invalid schedule %q: %v", entry.Name(), action.Job.Schedule, err)
				os.Remove(path)
				continue
			}
			job := *action.Job
			job.ID = ""
			job.Schedule = cronExpr
			if strings.TrimSpace(job.ScheduleHuman) == "" {
				job.ScheduleHuman = scheduleHuman
			}
			job.HistoryLimit = 10
			job.Enabled = true
			job.LastCheckAt = ""
			job.LastNotifyAt = ""
			job.NextRun = ""
			job.ThreadID = ""
			job.RunOnce = false
			if err := s.Add(&job); err != nil {
				log.Printf("[monitor-ingest] add job: %v", err)
				continue
			}
			recalcIDs = append(recalcIDs, job.ID)
			log.Printf("[monitor-ingest] created job %s (%s)", job.ID, job.Name)
		case "delete":
			action.JobID = strings.TrimSpace(action.JobID)
			action.ChannelID = strings.TrimSpace(action.ChannelID)
			if action.JobID == "" || action.ChannelID == "" {
				log.Printf("[monitor-ingest] %s: delete action missing job_id or channel_id", entry.Name())
				os.Remove(path)
				continue
			}
			existing, ok := s.Get(action.JobID)
			if !ok {
				log.Printf("[monitor-ingest] %s: delete skipped — job %s not found", entry.Name(), action.JobID)
				os.Remove(path)
				continue
			}
			if existing.ChannelID != action.ChannelID {
				log.Printf("[monitor-ingest] %s: delete blocked — job %s belongs to channel %s, not %s", entry.Name(), action.JobID, existing.ChannelID, action.ChannelID)
				os.Remove(path)
				continue
			}
			if err := s.Remove(action.JobID); err != nil {
				log.Printf("[monitor-ingest] remove job %s: %v", action.JobID, err)
				continue
			}
		case "update":
			action.JobID = strings.TrimSpace(action.JobID)
			action.ChannelID = strings.TrimSpace(action.ChannelID)
			if action.JobID == "" || action.ChannelID == "" || action.Update == nil {
				log.Printf("[monitor-ingest] %s: update action missing job_id, channel_id, or update", entry.Name())
				os.Remove(path)
				continue
			}
			existing, ok := s.Get(action.JobID)
			if !ok {
				log.Printf("[monitor-ingest] %s: update skipped — job %s not found", entry.Name(), action.JobID)
				os.Remove(path)
				continue
			}
			if existing.ChannelID != action.ChannelID {
				log.Printf("[monitor-ingest] %s: update blocked — job %s belongs to channel %s, not %s", entry.Name(), action.JobID, existing.ChannelID, action.ChannelID)
				os.Remove(path)
				continue
			}
			recalc, err := ApplyMonitorUpdate(existing, *action.Update)
			if err != nil {
				log.Printf("[monitor-ingest] %s: update job %s: %v", entry.Name(), action.JobID, err)
				os.Remove(path)
				continue
			}
			if err := s.Update(existing); err != nil {
				log.Printf("[monitor-ingest] update job %s: %v", action.JobID, err)
				continue
			}
			if recalc {
				recalcIDs = append(recalcIDs, existing.ID)
			}
		default:
			log.Printf("[monitor-ingest] %s: unknown action %q", entry.Name(), action.Action)
		}
		os.Remove(path)
	}
	return recalcIDs
}
