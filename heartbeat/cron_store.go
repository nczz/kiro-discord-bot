package heartbeat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CronJob defines a scheduled task.
type CronJob struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ChannelID     string `json:"channel_id"`
	GuildID       string `json:"guild_id"`
	Schedule      string `json:"schedule"`       // cron expression (empty for one-shot)
	ScheduleHuman string `json:"schedule_human"` // original user input
	Prompt        string `json:"prompt"`
	CWD           string `json:"cwd,omitempty"`
	Model         string `json:"model,omitempty"`
	HistoryLimit  int    `json:"history_limit"`
	Enabled       bool   `json:"enabled"`
	OneShot       bool   `json:"one_shot,omitempty"`
	MentionID     string `json:"mention_id,omitempty"`
	CreatedBy     string `json:"created_by"`
	CreatedByID   string `json:"created_by_id,omitempty"`
	CreatedAt     string `json:"created_at"`
	LastRun       string `json:"last_run,omitempty"`
	ThreadID      string `json:"thread_id,omitempty"`
	NextRun       string `json:"next_run,omitempty"`
	UseAgent      bool   `json:"use_agent,omitempty"`
	RunOnce       bool   `json:"run_once,omitempty"`
}

// CronUpdate is a validated partial update for a recurring cron job.
// Pointer fields distinguish an omitted field from an explicit value.
type CronUpdate struct {
	Name     *string `json:"name,omitempty"`
	Schedule *string `json:"schedule,omitempty"`
	Prompt   *string `json:"prompt,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
}

// ReminderUpdate is a validated partial update for a one-time reminder.
// ScheduleHuman and NextRun must either both be set or both be omitted.
type ReminderUpdate struct {
	Name          *string `json:"name,omitempty"`
	ScheduleHuman *string `json:"schedule_human,omitempty"`
	NextRun       *string `json:"next_run,omitempty"`
	Prompt        *string `json:"prompt,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
	MentionID     *string `json:"mention_id,omitempty"`
}

// ReminderUpdateRequest carries authenticated ownership and target context.
type ReminderUpdateRequest struct {
	JobID            string         `json:"job_id"`
	ChannelID        string         `json:"channel_id"`
	ActorID          string         `json:"actor_id"`
	CanManageChannel bool           `json:"can_manage_channel,omitempty"`
	Update           ReminderUpdate `json:"update"`
}

var (
	ErrReminderNotFound        = errors.New("reminder not found")
	ErrReminderNotOneShot      = errors.New("job is not a one-time reminder")
	ErrReminderWrongTarget     = errors.New("reminder belongs to a different Discord target")
	ErrReminderUnauthenticated = errors.New("reminder update requires authenticated Discord context")
	ErrReminderForbidden       = errors.New("reminder can only be updated by its creator or a channel manager")
	ErrReminderRunning         = errors.New("reminder is already running")
	ErrReminderTimePast        = errors.New("enabled reminder time must be in the future")
	ErrReminderInvalid         = errors.New("invalid reminder update")
)

// ValidateCronUpdate validates the shape and values of a partial cron update.
func ValidateCronUpdate(update CronUpdate) error {
	if update.Name == nil && update.Schedule == nil && update.Prompt == nil && update.Enabled == nil {
		return fmt.Errorf("at least one of name, schedule, prompt, or enabled is required")
	}
	if update.Name != nil {
		if strings.TrimSpace(*update.Name) == "" {
			return fmt.Errorf("name cannot be empty")
		}
	}
	if update.Schedule != nil {
		scheduleHuman := strings.TrimSpace(*update.Schedule)
		if scheduleHuman == "" {
			return fmt.Errorf("schedule cannot be empty")
		}
		if _, err := ParseSchedule(scheduleHuman); err != nil {
			return fmt.Errorf("invalid schedule: %w", err)
		}
	}
	if update.Prompt != nil {
		if strings.TrimSpace(*update.Prompt) == "" {
			return fmt.Errorf("prompt cannot be empty")
		}
	}
	return nil
}

// ApplyCronUpdate applies a partial update to an existing recurring job.
// It validates every supplied field before mutating job and reports whether
// next_run must be recalculated by CronTask.
func ApplyCronUpdate(job *CronJob, update CronUpdate) (bool, error) {
	if job == nil {
		return false, fmt.Errorf("cron job is required")
	}
	if job.OneShot {
		return false, fmt.Errorf("one-time reminders cannot be updated; delete and recreate the reminder")
	}
	if err := ValidateCronUpdate(update); err != nil {
		return false, err
	}

	wasEnabled := job.Enabled
	scheduleChanged := false
	if update.Name != nil {
		job.Name = strings.TrimSpace(*update.Name)
	}
	if update.Schedule != nil {
		scheduleHuman := strings.TrimSpace(*update.Schedule)
		cronExpr, _ := ParseSchedule(scheduleHuman)
		scheduleChanged = cronExpr != job.Schedule
		job.ScheduleHuman = scheduleHuman
		job.Schedule = cronExpr
	}
	if update.Prompt != nil {
		job.Prompt = strings.TrimSpace(*update.Prompt)
	}
	if update.Enabled != nil {
		job.Enabled = *update.Enabled
	}
	return scheduleChanged || (!wasEnabled && job.Enabled), nil
}

// ValidateReminderUpdate validates the shape and values of a partial reminder update.
func ValidateReminderUpdate(update ReminderUpdate) error {
	if update.Name == nil && update.ScheduleHuman == nil && update.NextRun == nil && update.Prompt == nil && update.Enabled == nil && update.MentionID == nil {
		return fmt.Errorf("at least one of name, time, content, enabled, or mention_user_id is required")
	}
	if update.Name != nil && strings.TrimSpace(*update.Name) == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if (update.ScheduleHuman == nil) != (update.NextRun == nil) {
		return fmt.Errorf("reminder time requires both schedule_human and next_run")
	}
	if update.ScheduleHuman != nil {
		if strings.TrimSpace(*update.ScheduleHuman) == "" {
			return fmt.Errorf("time cannot be empty")
		}
		if _, err := time.Parse(time.RFC3339, strings.TrimSpace(*update.NextRun)); err != nil {
			return fmt.Errorf("invalid reminder next_run: %w", err)
		}
	}
	if update.Prompt != nil && strings.TrimSpace(*update.Prompt) == "" {
		return fmt.Errorf("content cannot be empty")
	}
	return nil
}

// ApplyReminderUpdate applies a partial update to an existing one-time reminder.
func ApplyReminderUpdate(job *CronJob, update ReminderUpdate, now time.Time) error {
	if job == nil {
		return ErrReminderNotFound
	}
	if !job.OneShot {
		return ErrReminderNotOneShot
	}
	if err := ValidateReminderUpdate(update); err != nil {
		return fmt.Errorf("%w: %v", ErrReminderInvalid, err)
	}

	next := *job
	if update.Name != nil {
		next.Name = strings.TrimSpace(*update.Name)
	}
	if update.ScheduleHuman != nil {
		next.ScheduleHuman = strings.TrimSpace(*update.ScheduleHuman)
		next.NextRun = strings.TrimSpace(*update.NextRun)
	}
	if update.Prompt != nil {
		next.Prompt = strings.TrimSpace(*update.Prompt)
	}
	if update.Enabled != nil {
		next.Enabled = *update.Enabled
	}
	if update.MentionID != nil {
		next.MentionID = strings.TrimSpace(*update.MentionID)
	}
	if next.Enabled {
		target, err := time.Parse(time.RFC3339, next.NextRun)
		if err != nil || !target.After(now) {
			return ErrReminderTimePast
		}
	}
	*job = next
	return nil
}

// CronStore persists cron jobs to a JSON file.
type CronStore struct {
	mu   sync.RWMutex
	path string
	jobs map[string]*CronJob // key: job ID
}

func NewCronStore(dataDir string) (*CronStore, error) {
	dir := filepath.Join(dataDir, "cron")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	s := &CronStore{
		path: filepath.Join(dir, "cron.json"),
		jobs: make(map[string]*CronJob),
	}
	_ = s.load()
	return s, nil
}

func (s *CronStore) Add(job *CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job.ID == "" {
		job.ID = randomID()
	}
	if job.HistoryLimit == 0 {
		job.HistoryLimit = 10
	}
	if job.CreatedAt == "" {
		job.CreatedAt = time.Now().Format(time.RFC3339)
	}
	s.jobs[job.ID] = job
	return s.save()
}

func (s *CronStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, id)
	return s.save()
}

func (s *CronStore) Get(id string) (*CronJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	cp := *j
	return &cp, true
}

func (s *CronStore) Update(job *CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	return s.save()
}

func (s *CronStore) updateReminder(req ReminderUpdateRequest, now time.Time) (*CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobID := strings.TrimSpace(req.JobID)
	existing, ok := s.jobs[jobID]
	if !ok {
		return nil, ErrReminderNotFound
	}
	if !existing.OneShot {
		return nil, ErrReminderNotOneShot
	}
	if strings.TrimSpace(existing.ChannelID) != strings.TrimSpace(req.ChannelID) {
		return nil, ErrReminderWrongTarget
	}
	actorID := strings.TrimSpace(req.ActorID)
	if actorID == "" {
		return nil, ErrReminderUnauthenticated
	}
	creatorID := strings.TrimSpace(existing.CreatedByID)
	if !req.CanManageChannel && (creatorID == "" || creatorID != actorID) {
		return nil, ErrReminderForbidden
	}

	updated := *existing
	if err := ApplyReminderUpdate(&updated, req.Update, now); err != nil {
		return nil, err
	}
	s.jobs[jobID] = &updated
	if err := s.save(); err != nil {
		s.jobs[jobID] = existing
		return nil, err
	}
	result := updated
	return &result, nil
}

func (s *CronStore) ListByChannel(channelID string) []*CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*CronJob
	for _, j := range s.jobs {
		if j.ChannelID == channelID {
			cp := *j
			out = append(out, &cp)
		}
	}
	return out
}

func (s *CronStore) All() []*CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*CronJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		cp := *j
		out = append(out, &cp)
	}
	return out
}

// FindByName finds a job by name within a channel.
func (s *CronStore) FindByName(channelID, name string) (*CronJob, bool) {
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

func (s *CronStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.jobs)
}

func (s *CronStore) save() error {
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

func randomID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// PendingAction represents a cron action written by the MCP bot-tools server.
type PendingAction struct {
	Action           string          `json:"action"` // "create", "create_reminder", "update", "update_reminder", or "delete"
	Job              *PendingJob     `json:"job,omitempty"`
	JobID            string          `json:"job_id,omitempty"`
	ChannelID        string          `json:"channel_id,omitempty"`
	Update           *CronUpdate     `json:"update,omitempty"`
	ReminderUpdate   *ReminderUpdate `json:"reminder_update,omitempty"`
	ActorID          string          `json:"actor_id,omitempty"`
	CanManageChannel bool            `json:"can_manage_channel,omitempty"`
}

// PendingJob holds fields for creating a new cron job via pending.
type PendingJob struct {
	Name          string `json:"name"`
	Schedule      string `json:"schedule"`
	ScheduleHuman string `json:"schedule_human,omitempty"`
	Prompt        string `json:"prompt"`
	ChannelID     string `json:"channel_id"`
	GuildID       string `json:"guild_id"`
	CreatedBy     string `json:"created_by,omitempty"`
	CreatedByID   string `json:"created_by_id,omitempty"`
	NextRun       string `json:"next_run,omitempty"`
	MentionID     string `json:"mention_id,omitempty"`
	OneShot       bool   `json:"one_shot,omitempty"`
	UseAgent      bool   `json:"use_agent,omitempty"`
}

// IngestPending scans the pending directory, processes actions, and removes files.
// Returns job IDs whose next_run must be recalculated by CronTask.
func (s *CronStore) IngestPending(updateReminder func(ReminderUpdateRequest) (*CronJob, error)) []string {
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
			log.Printf("[cron-ingest] read %s: %v", entry.Name(), err)
			continue
		}
		var action PendingAction
		if err := json.Unmarshal(data, &action); err != nil {
			log.Printf("[cron-ingest] parse %s: %v", entry.Name(), err)
			os.Remove(path)
			continue
		}
		switch action.Action {
		case "create":
			if action.Job == nil {
				log.Printf("[cron-ingest] %s: create action missing job", entry.Name())
				os.Remove(path)
				continue
			}
			action.Job.Name = strings.TrimSpace(action.Job.Name)
			action.Job.Schedule = strings.TrimSpace(action.Job.Schedule)
			action.Job.Prompt = strings.TrimSpace(action.Job.Prompt)
			action.Job.ChannelID = strings.TrimSpace(action.Job.ChannelID)
			action.Job.GuildID = strings.TrimSpace(action.Job.GuildID)
			action.Job.CreatedBy = strings.TrimSpace(action.Job.CreatedBy)
			action.Job.CreatedByID = strings.TrimSpace(action.Job.CreatedByID)
			if action.Job.Name == "" || action.Job.Schedule == "" || action.Job.Prompt == "" || action.Job.ChannelID == "" || action.Job.GuildID == "" {
				log.Printf("[cron-ingest] %s: create action missing required fields", entry.Name())
				os.Remove(path)
				continue
			}
			cronExpr, err := ParseSchedule(action.Job.Schedule)
			if err != nil {
				log.Printf("[cron-ingest] %s: invalid schedule %q: %v", entry.Name(), action.Job.Schedule, err)
				os.Remove(path)
				continue
			}
			job := &CronJob{
				Name:          action.Job.Name,
				ChannelID:     action.Job.ChannelID,
				GuildID:       action.Job.GuildID,
				Schedule:      cronExpr,
				ScheduleHuman: cronExpr,
				Prompt:        action.Job.Prompt,
				HistoryLimit:  10,
				Enabled:       true,
				CreatedBy:     action.Job.CreatedBy,
				CreatedByID:   action.Job.CreatedByID,
			}
			if err := s.Add(job); err != nil {
				log.Printf("[cron-ingest] add job: %v", err)
				continue
			}
			recalcIDs = append(recalcIDs, job.ID)
			log.Printf("[cron-ingest] created job %s (%s)", job.ID, job.Name)
		case "create_reminder":
			if action.Job == nil {
				log.Printf("[cron-ingest] %s: create_reminder action missing job", entry.Name())
				os.Remove(path)
				continue
			}
			action.Job.Name = strings.TrimSpace(action.Job.Name)
			action.Job.ScheduleHuman = strings.TrimSpace(action.Job.ScheduleHuman)
			action.Job.Prompt = strings.TrimSpace(action.Job.Prompt)
			action.Job.ChannelID = strings.TrimSpace(action.Job.ChannelID)
			action.Job.GuildID = strings.TrimSpace(action.Job.GuildID)
			action.Job.CreatedBy = strings.TrimSpace(action.Job.CreatedBy)
			action.Job.CreatedByID = strings.TrimSpace(action.Job.CreatedByID)
			action.Job.NextRun = strings.TrimSpace(action.Job.NextRun)
			action.Job.MentionID = strings.TrimSpace(action.Job.MentionID)
			if action.Job.Name == "" || action.Job.Prompt == "" || action.Job.ChannelID == "" || action.Job.GuildID == "" || action.Job.NextRun == "" {
				log.Printf("[cron-ingest] %s: create_reminder action missing required fields", entry.Name())
				os.Remove(path)
				continue
			}
			if _, err := time.Parse(time.RFC3339, action.Job.NextRun); err != nil {
				log.Printf("[cron-ingest] %s: invalid reminder next_run %q: %v", entry.Name(), action.Job.NextRun, err)
				os.Remove(path)
				continue
			}
			job := &CronJob{
				Name:          action.Job.Name,
				ChannelID:     action.Job.ChannelID,
				GuildID:       action.Job.GuildID,
				ScheduleHuman: action.Job.ScheduleHuman,
				Prompt:        action.Job.Prompt,
				HistoryLimit:  0,
				Enabled:       true,
				OneShot:       true,
				UseAgent:      action.Job.UseAgent,
				MentionID:     action.Job.MentionID,
				CreatedBy:     action.Job.CreatedBy,
				CreatedByID:   action.Job.CreatedByID,
				NextRun:       action.Job.NextRun,
			}
			if err := s.Add(job); err != nil {
				log.Printf("[cron-ingest] add reminder: %v", err)
				continue
			}
			recalcIDs = append(recalcIDs, job.ID)
			log.Printf("[cron-ingest] created reminder %s (%s)", job.ID, job.Name)
		case "delete":
			action.JobID = strings.TrimSpace(action.JobID)
			action.ChannelID = strings.TrimSpace(action.ChannelID)
			if action.JobID == "" || action.ChannelID == "" {
				log.Printf("[cron-ingest] %s: delete action missing job_id or channel_id", entry.Name())
				os.Remove(path)
				continue
			}
			// Verify channel ownership before deleting.
			existing, ok := s.Get(action.JobID)
			if !ok {
				log.Printf("[cron-ingest] %s: delete skipped — job %s not found", entry.Name(), action.JobID)
				os.Remove(path)
				continue
			}
			if existing.ChannelID != action.ChannelID {
				log.Printf("[cron-ingest] %s: delete blocked — job %s belongs to channel %s, not %s", entry.Name(), action.JobID, existing.ChannelID, action.ChannelID)
				os.Remove(path)
				continue
			}
			if err := s.Remove(action.JobID); err != nil {
				log.Printf("[cron-ingest] remove job %s: %v", action.JobID, err)
			} else {
				log.Printf("[cron-ingest] deleted job %s", action.JobID)
			}
		case "update":
			action.JobID = strings.TrimSpace(action.JobID)
			action.ChannelID = strings.TrimSpace(action.ChannelID)
			if action.JobID == "" || action.ChannelID == "" || action.Update == nil {
				log.Printf("[cron-ingest] %s: update action missing job_id, channel_id, or update", entry.Name())
				os.Remove(path)
				continue
			}
			existing, ok := s.Get(action.JobID)
			if !ok {
				log.Printf("[cron-ingest] %s: update skipped — job %s not found", entry.Name(), action.JobID)
				os.Remove(path)
				continue
			}
			if existing.ChannelID != action.ChannelID {
				log.Printf("[cron-ingest] %s: update blocked — job %s belongs to channel %s, not %s", entry.Name(), action.JobID, existing.ChannelID, action.ChannelID)
				os.Remove(path)
				continue
			}
			recalc, err := ApplyCronUpdate(existing, *action.Update)
			if err != nil {
				log.Printf("[cron-ingest] %s: update job %s: %v", entry.Name(), action.JobID, err)
				os.Remove(path)
				continue
			}
			if err := s.Update(existing); err != nil {
				log.Printf("[cron-ingest] update job %s: %v", action.JobID, err)
				continue
			}
			if recalc {
				recalcIDs = append(recalcIDs, existing.ID)
			}
			log.Printf("[cron-ingest] updated job %s", existing.ID)
		case "update_reminder":
			action.JobID = strings.TrimSpace(action.JobID)
			action.ChannelID = strings.TrimSpace(action.ChannelID)
			action.ActorID = strings.TrimSpace(action.ActorID)
			if action.JobID == "" || action.ChannelID == "" || action.ActorID == "" || action.ReminderUpdate == nil {
				log.Printf("[cron-ingest] %s: update_reminder action missing job_id, channel_id, actor_id, or reminder_update", entry.Name())
				os.Remove(path)
				continue
			}
			if updateReminder == nil {
				log.Printf("[cron-ingest] %s: update_reminder handler is unavailable", entry.Name())
				os.Remove(path)
				continue
			}
			_, err := updateReminder(ReminderUpdateRequest{
				JobID:            action.JobID,
				ChannelID:        action.ChannelID,
				ActorID:          action.ActorID,
				CanManageChannel: action.CanManageChannel,
				Update:           *action.ReminderUpdate,
			})
			if err != nil {
				log.Printf("[cron-ingest] %s: update reminder %s: %v", entry.Name(), action.JobID, err)
				if permanentReminderUpdateError(err) {
					os.Remove(path)
				}
				continue
			}
			log.Printf("[cron-ingest] updated reminder %s", action.JobID)
		default:
			log.Printf("[cron-ingest] %s: unknown action %q", entry.Name(), action.Action)
		}
		os.Remove(path)
	}
	return recalcIDs
}

func permanentReminderUpdateError(err error) bool {
	return errors.Is(err, ErrReminderNotFound) ||
		errors.Is(err, ErrReminderNotOneShot) ||
		errors.Is(err, ErrReminderWrongTarget) ||
		errors.Is(err, ErrReminderUnauthenticated) ||
		errors.Is(err, ErrReminderForbidden) ||
		errors.Is(err, ErrReminderRunning) ||
		errors.Is(err, ErrReminderTimePast) ||
		errors.Is(err, ErrReminderInvalid)
}
