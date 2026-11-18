package channel

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/audit"
	L "github.com/nczz/kiro-discord-bot/locale"
)

const (
	jobRecoveryJournalName = ".active-jobs.json"
	jobRecoveryQueued      = "queued"
	jobRecoveryActive      = "active"
)

type jobRecoveryRecord struct {
	ID                string    `json:"id"`
	State             string    `json:"state"`
	GuildID           string    `json:"guild_id,omitempty"`
	ChannelID         string    `json:"channel_id"`
	ParentChannelID   string    `json:"parent_channel_id,omitempty"`
	ThreadID          string    `json:"thread_id,omitempty"`
	MessageID         string    `json:"message_id,omitempty"`
	ProgressMessageID string    `json:"progress_message_id,omitempty"`
	UserID            string    `json:"user_id,omitempty"`
	Source            string    `json:"source,omitempty"`
	RemoteA2A         bool      `json:"remote_a2a,omitempty"`
	DisableBotEgress  bool      `json:"disable_bot_egress,omitempty"`
	QueuedAt          time.Time `json:"queued_at,omitempty"`
	StartedAt         time.Time `json:"started_at,omitempty"`
}

type jobRecoveryJournal struct {
	Version int                 `json:"version"`
	Jobs    []jobRecoveryRecord `json:"jobs"`
}

type jobRecoveryStore struct {
	mu      sync.Mutex
	path    string
	records map[string]jobRecoveryRecord
	loadErr error
}

func newJobRecoveryStore(dataDir string) *jobRecoveryStore {
	store := &jobRecoveryStore{records: make(map[string]jobRecoveryRecord)}
	if strings.TrimSpace(dataDir) == "" {
		return store
	}
	store.path = filepath.Join(dataDir, jobRecoveryJournalName)
	store.loadErr = store.load()
	return store
}

func (s *jobRecoveryStore) load() error {
	if s == nil || s.path == "" {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read active-job journal: %w", err)
	}
	var journal jobRecoveryJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		return fmt.Errorf("decode active-job journal: %w", err)
	}
	if journal.Version != 1 {
		return fmt.Errorf("unsupported active-job journal version %d", journal.Version)
	}
	for _, record := range journal.Jobs {
		if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.ChannelID) == "" {
			return fmt.Errorf("active-job journal contains an invalid record")
		}
		if record.State != jobRecoveryQueued && record.State != jobRecoveryActive {
			return fmt.Errorf("active-job journal contains invalid state %q", record.State)
		}
		if _, duplicate := s.records[record.ID]; duplicate {
			return fmt.Errorf("active-job journal contains duplicate id %q", record.ID)
		}
		s.records[record.ID] = record
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("secure active-job journal: %w", err)
	}
	return nil
}

func (s *jobRecoveryStore) Put(record jobRecoveryRecord) error {
	if s == nil || s.path == "" || strings.TrimSpace(record.ID) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return s.loadErr
	}
	previous, existed := s.records[record.ID]
	s.records[record.ID] = record
	if committed, err := s.persistLocked(); err != nil {
		if !committed {
			if existed {
				s.records[record.ID] = previous
			} else {
				delete(s.records, record.ID)
			}
		}
		return err
	}
	return nil
}

func (s *jobRecoveryStore) MarkActive(id, threadID, progressMessageID string, startedAt time.Time) error {
	if s == nil || s.path == "" || strings.TrimSpace(id) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return s.loadErr
	}
	previous, ok := s.records[id]
	if !ok {
		return nil
	}
	record := previous
	record.State = jobRecoveryActive
	if strings.TrimSpace(threadID) != "" {
		record.ThreadID = threadID
	}
	if strings.TrimSpace(progressMessageID) != "" {
		record.ProgressMessageID = progressMessageID
	}
	if !startedAt.IsZero() {
		record.StartedAt = startedAt.UTC()
	}
	s.records[id] = record
	if committed, err := s.persistLocked(); err != nil {
		if !committed {
			s.records[id] = previous
		}
		return err
	}
	return nil
}

func (s *jobRecoveryStore) Delete(id string) error {
	if s == nil || s.path == "" || strings.TrimSpace(id) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return s.loadErr
	}
	if _, ok := s.records[id]; !ok {
		return nil
	}
	record := s.records[id]
	delete(s.records, id)
	if committed, err := s.persistLocked(); err != nil {
		if !committed {
			s.records[id] = record
		}
		return err
	}
	return nil
}

func (s *jobRecoveryStore) Records() ([]jobRecoveryRecord, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	records := make([]jobRecoveryRecord, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].QueuedAt.Equal(records[j].QueuedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].QueuedAt.Before(records[j].QueuedAt)
	})
	return records, nil
}

// persistLocked reports committed=true once the final journal path reflects
// the in-memory state. A later directory-sync error is still returned, but
// callers must not roll memory back to a state that no longer matches disk.
func (s *jobRecoveryStore) persistLocked() (committed bool, err error) {
	if len(s.records) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("remove empty active-job journal: %w", err)
		}
		if err := syncDirectory(filepath.Dir(s.path)); err != nil {
			return true, fmt.Errorf("sync active-job journal directory: %w", err)
		}
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return false, fmt.Errorf("create active-job journal directory: %w", err)
	}
	records := make([]jobRecoveryRecord, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	raw, err := json.MarshalIndent(jobRecoveryJournal{Version: 1, Jobs: records}, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode active-job journal: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return false, fmt.Errorf("create active-job journal: %w", err)
	}
	tmp := file.Name()
	writeErr := error(nil)
	if _, err := file.Write(raw); err != nil {
		writeErr = err
	} else if _, err := file.WriteString("\n"); err != nil {
		writeErr = err
	} else if err := file.Sync(); err != nil {
		writeErr = err
	}
	if err := file.Close(); writeErr == nil {
		writeErr = err
	}
	if writeErr != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("write active-job journal: %w", writeErr)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("replace active-job journal: %w", err)
	}
	if err := syncDirectory(filepath.Dir(s.path)); err != nil {
		return true, fmt.Errorf("sync active-job journal directory: %w", err)
	}
	return true, nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

func jobRecoveryRecordFromJob(job *Job) jobRecoveryRecord {
	if job == nil {
		return jobRecoveryRecord{}
	}
	return jobRecoveryRecord{
		ID:               jobRecoveryID(job),
		State:            jobRecoveryQueued,
		GuildID:          job.GuildID,
		ChannelID:        job.ChannelID,
		ParentChannelID:  job.ParentChannelID,
		ThreadID:         job.ThreadID,
		MessageID:        job.MessageID,
		UserID:           job.UserID,
		Source:           job.Source,
		RemoteA2A:        job.RemoteA2A,
		DisableBotEgress: job.DisableBotEgress,
		QueuedAt:         time.Now().UTC(),
	}
}

func jobRecoveryID(job *Job) string {
	if job == nil {
		return ""
	}
	if id := strings.TrimSpace(job.MessageID); id != "" {
		return "discord:" + id
	}
	if id := strings.TrimSpace(string(job.A2ATaskID)); id != "" {
		return "a2a:" + id
	}
	return ""
}

func (m *Manager) persistQueuedJob(job *Job) error {
	if m == nil || m.recovery == nil || job == nil {
		return nil
	}
	if err := m.recovery.Put(jobRecoveryRecordFromJob(job)); err != nil {
		return fmt.Errorf("persist queued job: %w", err)
	}
	return nil
}

func (m *Manager) removeQueuedJobRecovery(job *Job) {
	if m == nil || m.recovery == nil || job == nil {
		return
	}
	if err := m.recovery.Delete(jobRecoveryID(job)); err != nil {
		log.Printf("[recovery] remove unqueued job channel=%s message=%s: %v", job.ChannelID, job.MessageID, err)
	}
}

// ReconcileInterruptedJobs terminalizes jobs left by a previous process. It never
// replays prompts because prior tool calls may already have caused side effects.
func (m *Manager) ReconcileInterruptedJobs(ds *discordgo.Session) (int, error) {
	if m == nil || m.recovery == nil {
		return 0, nil
	}
	records, err := m.recovery.Records()
	if err != nil {
		return 0, err
	}
	reconciled := 0
	var result error
	for _, record := range records {
		if err := m.reconcileInterruptedJob(ds, record); err != nil {
			result = errors.Join(result, fmt.Errorf("job %s: %w", record.ID, err))
			continue
		}
		if err := m.recovery.Delete(record.ID); err != nil {
			result = errors.Join(result, fmt.Errorf("delete job %s: %w", record.ID, err))
			continue
		}
		reconciled++
	}
	return reconciled, result
}

func (m *Manager) reconcileInterruptedJob(ds *discordgo.Session, record jobRecoveryRecord) error {
	if ds == nil {
		return fmt.Errorf("discord session unavailable")
	}
	message := L.Get("worker.restart_interrupted")
	if record.State == jobRecoveryQueued {
		message = L.Get("worker.restart_queued")
	}
	content := "⚠️ " + message
	targetID := record.ThreadID
	if targetID == "" {
		targetID = record.ChannelID
	}
	var deliveryErr error
	if !record.DisableBotEgress {
		switch {
		case record.ProgressMessageID != "":
			deliveryErr = editMessageWithMentions(ds, targetID, record.ProgressMessageID, content, nil)
		case record.ThreadID != "":
			_, deliveryErr = SendProcessMessage(ds, targetID, content)
		default:
			_, deliveryErr = sendDiscordText(ds, record.ChannelID, content, &discordgo.MessageReference{
				MessageID: record.MessageID,
				ChannelID: record.ChannelID,
			})
		}
	}
	if deliveryErr != nil && !discordMessageGone(deliveryErr) {
		return fmt.Errorf("terminalize Discord progress: %w", deliveryErr)
	}

	reactionReconciled := false
	if !record.DisableBotEgress && !record.RemoteA2A && record.ChannelID != "" && record.MessageID != "" {
		reactions, err := recoveredJobReactionState(ds, record.ChannelID, record.MessageID)
		if err != nil {
			if !discordMessageGone(err) {
				return fmt.Errorf("inspect source reaction: %w", err)
			}
		} else if err := reactions.finalError("⚠️"); err != nil {
			return fmt.Errorf("terminalize source reaction: %w", err)
		} else {
			reactionReconciled = true
		}
	}
	if m.audit != nil {
		jobID := record.MessageID
		if jobID == "" {
			jobID = record.ID
		}
		m.audit.RecordBotEvent(audit.BotEvent{
			Type:            "agent_job_failed",
			GuildID:         record.GuildID,
			ChannelID:       record.ChannelID,
			TargetID:        targetID,
			ThreadID:        record.ThreadID,
			ParentChannelID: record.ParentChannelID,
			MessageID:       record.MessageID,
			JobID:           jobID,
			UserID:          record.UserID,
			Source:          record.Source,
			Status:          "error",
			Metadata: map[string]any{
				"reason":              "process_restart",
				"recovery_id":         record.ID,
				"previous_state":      record.State,
				"reaction_reconciled": reactionReconciled,
				"auto_replayed":       false,
			},
		})
	}
	return nil
}

func discordMessageGone(err error) bool {
	var restErr *discordgo.RESTError
	return errors.As(err, &restErr) && restErr.Response != nil && restErr.Response.StatusCode == http.StatusNotFound
}
