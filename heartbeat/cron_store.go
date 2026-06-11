package heartbeat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	Action    string      `json:"action"` // "create" or "delete"
	Job       *PendingJob `json:"job,omitempty"`
	JobID     string      `json:"job_id,omitempty"`
	ChannelID string      `json:"channel_id,omitempty"`
}

// PendingJob holds fields for creating a new cron job via pending.
type PendingJob struct {
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	Prompt    string `json:"prompt"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
	CreatedBy string `json:"created_by,omitempty"`
}

// IngestPending scans the pending directory, processes actions, and removes files.
// Returns the IDs of newly created jobs (for RecalcNextRun).
func (s *CronStore) IngestPending() []string {
	dir := filepath.Join(filepath.Dir(s.path), "pending")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var created []string
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
			}
			if err := s.Add(job); err != nil {
				log.Printf("[cron-ingest] add job: %v", err)
				continue
			}
			created = append(created, job.ID)
			log.Printf("[cron-ingest] created job %s (%s)", job.ID, job.Name)
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
		default:
			log.Printf("[cron-ingest] %s: unknown action %q", entry.Name(), action.Action)
		}
		os.Remove(path)
	}
	return created
}
