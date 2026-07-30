package a2a

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type EventRow struct {
	ID          int64
	TaskID      TaskID
	Revision    int64
	EventType   string
	State       TaskState
	PayloadJSON string
	CreatedAt   time.Time
}

type SQLiteEventStore struct{ db *sql.DB }

func OpenEventStore(dataDir string) (*SQLiteEventStore, error) {
	db, err := openA2ASQLite(dataDir, "tasks.sqlite", taskStoreMigrations())
	if err != nil {
		return nil, err
	}
	return &SQLiteEventStore{db: db}, nil
}

func (s *SQLiteEventStore) Close() error { return closeSQL(s.db) }

func (s *SQLiteTaskStore) AppendEvent(ctx context.Context, event EventRow) error {
	return appendEvent(ctx, s.db, event)
}
func (s *SQLiteEventStore) Append(ctx context.Context, event EventRow) error {
	return appendEvent(ctx, s.db, event)
}

func appendEvent(ctx context.Context, db *sql.DB, event EventRow) error {
	if err := ValidateTaskID(event.TaskID); err != nil {
		return err
	}
	if event.Revision <= 0 {
		return fmt.Errorf("revision must be positive")
	}
	event.EventType = strings.TrimSpace(event.EventType)
	if event.EventType == "" {
		return fmt.Errorf("event_type is required")
	}
	if event.State != "" && !IsKnownTaskState(event.State) {
		return fmt.Errorf("unknown event state %s", event.State)
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	_, err := db.ExecContext(ctx, `INSERT INTO a2a_task_events(task_id, revision, event_type, state, payload_json, created_at) VALUES(?,?,?,?,?,?)`, event.TaskID, event.Revision, event.EventType, nullString(string(event.State)), nullString(event.PayloadJSON), event.CreatedAt.UTC().Format(sqliteTimeFormat))
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return err
	}
	var existing EventRow
	var state, payload, created string
	err = db.QueryRowContext(ctx, `SELECT id, task_id, revision, event_type, COALESCE(state,''), COALESCE(payload_json,''), created_at FROM a2a_task_events WHERE task_id=? AND revision=?`, event.TaskID, event.Revision).Scan(&existing.ID, &existing.TaskID, &existing.Revision, &existing.EventType, &state, &payload, &created)
	if err != nil {
		return err
	}
	if existing.EventType == event.EventType && state == string(event.State) && payload == event.PayloadJSON {
		return nil
	}
	return fmt.Errorf("event revision replay differs")
}

func (s *SQLiteEventStore) Replay(ctx context.Context, taskID TaskID, afterRevision int64) ([]EventRow, error) {
	return replayEvents(ctx, s.db, taskID, afterRevision)
}
func (s *SQLiteTaskStore) ReplayEvents(ctx context.Context, taskID TaskID, afterRevision int64) ([]EventRow, error) {
	return replayEvents(ctx, s.db, taskID, afterRevision)
}

func replayEvents(ctx context.Context, db *sql.DB, taskID TaskID, afterRevision int64) ([]EventRow, error) {
	if err := ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, task_id, revision, event_type, COALESCE(state,''), COALESCE(payload_json,''), created_at FROM a2a_task_events WHERE task_id=? AND revision>? ORDER BY revision`, taskID, afterRevision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		var e EventRow
		var state, created string
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Revision, &e.EventType, &state, &e.PayloadJSON, &created); err != nil {
			return nil, err
		}
		e.State = TaskState(state)
		e.CreatedAt, _ = time.Parse(sqliteTimeFormat, created)
		out = append(out, e)
	}
	return out, rows.Err()
}
