package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	mediaJobQueued    = "queued"
	mediaJobRunning   = "running"
	mediaJobSucceeded = "succeeded"
	mediaJobFailed    = "failed"
	mediaJobCanceled  = "canceled"
)

type MediaJob struct {
	ID          string
	Kind        string
	Status      string
	Model       string
	Prompt      string
	Path        string
	MimeType    string
	Error       string
	CreatedAt   time.Time
	StartedAt   time.Time
	CompletedAt time.Time
	cancel      context.CancelFunc
}

type MediaJobStore struct {
	mu        sync.Mutex
	jobs      map[string]*MediaJob
	retention time.Duration
	maxActive int
}

type MediaJobRunner func(ctx context.Context) (*MediaResult, error)

func NewMediaJobStore(retention time.Duration, maxActive int) *MediaJobStore {
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	return &MediaJobStore{jobs: make(map[string]*MediaJob), retention: retention, maxActive: maxActive}
}

func (s *MediaJobStore) Start(kind, prompt, model string, timeout time.Duration, run MediaJobRunner) (*MediaJob, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, fmt.Errorf("job kind is required")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if run == nil {
		return nil, fmt.Errorf("job runner is required")
	}
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	job := &MediaJob{
		ID:        newMediaJobID(),
		Kind:      kind,
		Status:    mediaJobQueued,
		Model:     strings.TrimSpace(model),
		Prompt:    prompt,
		CreatedAt: time.Now().UTC(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	job.cancel = cancel

	s.mu.Lock()
	s.pruneLocked(time.Now().UTC())
	if s.maxActive > 0 && s.activeLocked() >= s.maxActive {
		s.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("too many active media jobs (max %d)", s.maxActive)
	}
	s.jobs[job.ID] = job
	s.mu.Unlock()

	go s.run(ctx, job.ID, run)
	return s.snapshot(job), nil
}

func (s *MediaJobStore) run(ctx context.Context, jobID string, run MediaJobRunner) {
	s.markRunning(jobID)
	result, err := run(ctx)
	s.complete(jobID, result, err)
}

func (s *MediaJobStore) markRunning(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job := s.jobs[jobID]; job != nil && job.Status == mediaJobQueued {
		job.Status = mediaJobRunning
		job.StartedAt = time.Now().UTC()
	}
}

func (s *MediaJobStore) complete(jobID string, result *MediaResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[jobID]
	if job == nil {
		return
	}
	if job.Status == mediaJobCanceled {
		return
	}
	job.CompletedAt = time.Now().UTC()
	job.cancel = nil
	if err != nil {
		if errors.Is(err, context.Canceled) {
			job.Status = mediaJobCanceled
		} else {
			job.Status = mediaJobFailed
			job.Error = err.Error()
		}
		return
	}
	if result == nil || strings.TrimSpace(result.Path) == "" {
		job.Status = mediaJobFailed
		job.Error = "provider returned no media path"
		return
	}
	job.Status = mediaJobSucceeded
	job.Path = result.Path
	job.MimeType = result.MimeType
}

func (s *MediaJobStore) Get(id string) (*MediaJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[strings.TrimSpace(id)]
	if job == nil {
		return nil, false
	}
	return s.snapshot(job), true
}

func (s *MediaJobStore) List(limit int) []*MediaJob {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now().UTC())
	out := make([]*MediaJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		out = append(out, s.snapshot(job))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *MediaJobStore) Cancel(id string) (*MediaJob, bool) {
	s.mu.Lock()
	job := s.jobs[strings.TrimSpace(id)]
	if job == nil {
		s.mu.Unlock()
		return nil, false
	}
	cancel := job.cancel
	if job.Status == mediaJobQueued || job.Status == mediaJobRunning {
		job.Status = mediaJobCanceled
		job.CompletedAt = time.Now().UTC()
		job.cancel = nil
	}
	snap := s.snapshot(job)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return snap, true
}

func (s *MediaJobStore) pruneLocked(now time.Time) {
	for id, job := range s.jobs {
		if job == nil || job.CompletedAt.IsZero() {
			continue
		}
		if now.Sub(job.CompletedAt) > s.retention {
			delete(s.jobs, id)
		}
	}
}

func (s *MediaJobStore) activeLocked() int {
	active := 0
	for _, job := range s.jobs {
		if job == nil {
			continue
		}
		if job.Status == mediaJobQueued || job.Status == mediaJobRunning {
			active++
		}
	}
	return active
}

func (s *MediaJobStore) snapshot(job *MediaJob) *MediaJob {
	if job == nil {
		return nil
	}
	cp := *job
	cp.cancel = nil
	return &cp
}

func newMediaJobID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "media_" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("media_%d", time.Now().UnixNano())
}

func formatMediaJob(job *MediaJob) string {
	if job == nil {
		return "Job not found."
	}
	var lines []string
	lines = append(lines, "Job ID: "+job.ID)
	lines = append(lines, "Status: "+job.Status)
	lines = append(lines, "Kind: "+job.Kind)
	if job.Model != "" {
		lines = append(lines, "Model: "+job.Model)
	}
	if !job.CreatedAt.IsZero() {
		lines = append(lines, "Created: "+job.CreatedAt.Format(time.RFC3339))
	}
	if !job.CompletedAt.IsZero() {
		lines = append(lines, "Completed: "+job.CompletedAt.Format(time.RFC3339))
	}
	if job.Path != "" {
		lines = append(lines, "Path: "+job.Path)
	}
	if job.MimeType != "" {
		lines = append(lines, "MIME type: "+job.MimeType)
	}
	if job.Error != "" {
		lines = append(lines, "Error: "+job.Error)
	}
	return strings.Join(lines, "\n")
}

func formatMediaJobStarted(job *MediaJob) string {
	if job == nil {
		return "Media job was not started."
	}
	return fmt.Sprintf("%s job started.\nJob ID: %s\nStatus: %s\nModel: %s\nUse get_media_job with job_id=%s to check progress and retrieve the saved path.", job.Kind, job.ID, job.Status, job.Model, job.ID)
}

func formatMediaJobList(jobs []*MediaJob) string {
	if len(jobs) == 0 {
		return "No media jobs."
	}
	lines := make([]string, 0, len(jobs))
	for _, job := range jobs {
		created := ""
		if !job.CreatedAt.IsZero() {
			created = " created=" + job.CreatedAt.Format(time.RFC3339)
		}
		path := ""
		if job.Path != "" {
			path = " path=" + job.Path
		}
		errText := ""
		if job.Error != "" {
			errText = " error=" + job.Error
		}
		lines = append(lines, fmt.Sprintf("%s status=%s type=%s model=%s%s%s%s", job.ID, job.Status, job.Kind, job.Model, created, path, errText))
	}
	return strings.Join(lines, "\n")
}
