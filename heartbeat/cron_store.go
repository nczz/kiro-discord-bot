package heartbeat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CronJob defines a scheduled task.
type CronJob struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ChannelID     string `json:"channel_id"`
	GuildID       string `json:"guild_id"`
	Schedule      string `json:"schedule"`        // cron expression (empty for one-shot)
	ScheduleHuman string `json:"schedule_human"`  // original user input
	Prompt        string `json:"prompt"`
	CWD           string `json:"cwd,omitempty"`
	Model         string `json:"model,omitempty"`
	HistoryLimit  int    `json:"history_limit"`
	Enabled       bool   `json:"enabled"`
	OneShot       bool   `json:"one_shot,omitempty"`
	MentionID     string `json:"mention_id,omitempty"`
	CreatedBy     string `json:"created_by"`
	CreatedAt     string `json:"created_at"`
	LastRun       string `json:"last_run,omitempty"`
	NextRun       string `json:"next_run,omitempty"`
	Running       bool   `json:"-"` // transient, not persisted
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
	return j, ok
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
			out = append(out, j)
		}
	}
	return out
}

func (s *CronStore) All() []*CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*CronJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j)
	}
	return out
}

// FindByName finds a job by name within a channel.
func (s *CronStore) FindByName(channelID, name string) (*CronJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, j := range s.jobs {
		if j.ChannelID == channelID && j.Name == name {
			return j, true
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
	return os.WriteFile(s.path, data, 0644)
}

func randomID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
