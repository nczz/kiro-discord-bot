package a2a

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type TaskRow struct {
	LocalID               string
	TaskID                TaskID
	ClientTaskRef         string
	MessageID             MessageID
	ContextID             string
	Direction             string
	Role                  string
	FromAgent             AgentID
	ToAgent               AgentID
	ExecutorAgent         AgentID
	ChannelID             string
	GuildID               string
	ChannelRef            string
	SkillID               string
	State                 TaskState
	Terminal              bool
	Revision              int64
	ResultVisibility      string
	DiscordTranscriptMode string
	DiscordContextJSON    string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ExpiresAt             time.Time
	Error                 TaskError
}

type SQLiteTaskStore struct{ db *sql.DB }

func OpenTaskStore(dataDir string) (*SQLiteTaskStore, error) {
	db, err := openA2ASQLite(dataDir, "tasks.sqlite", taskStoreMigrations())
	if err != nil {
		return nil, err
	}
	return &SQLiteTaskStore{db: db}, nil
}

func (s *SQLiteTaskStore) Close() error { return closeSQL(s.db) }

func taskStoreMigrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS a2a_tasks (
		local_id TEXT PRIMARY KEY,
		task_id TEXT,
		client_task_ref TEXT,
		message_id TEXT NOT NULL,
		context_id TEXT,
		direction TEXT NOT NULL,
		role TEXT NOT NULL,
		from_agent TEXT NOT NULL,
		to_agent TEXT NOT NULL,
		executor_agent TEXT,
		channel_id TEXT,
		guild_id TEXT,
		channel_ref TEXT,
		skill_id TEXT,
		state TEXT NOT NULL,
		terminal INTEGER NOT NULL DEFAULT 0,
		revision INTEGER NOT NULL DEFAULT 0,
		result_visibility TEXT NOT NULL DEFAULT 'proxy',
		discord_transcript_mode TEXT NOT NULL DEFAULT 'delegator',
		discord_context_json TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		expires_at TEXT,
		error_code TEXT,
		error_message TEXT
	)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_a2a_tasks_message_direction ON a2a_tasks(direction, message_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_a2a_tasks_remote_task ON a2a_tasks(direction, task_id) WHERE task_id IS NOT NULL AND task_id <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_tasks_client_ref ON a2a_tasks(client_task_ref)`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_tasks_context ON a2a_tasks(context_id)`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_tasks_state ON a2a_tasks(state, terminal)`,
		`CREATE TABLE IF NOT EXISTS a2a_task_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL,
		revision INTEGER NOT NULL,
		event_type TEXT NOT NULL,
		state TEXT,
		payload_json TEXT,
		created_at TEXT NOT NULL,
		UNIQUE(task_id, revision, event_type)
	)`,
	}
}

func (s *SQLiteTaskStore) CreateOutbound(ctx context.Context, row TaskRow) (TaskRow, error) {
	row.Direction = "outbound"
	row.Role = "delegator"
	return s.insertTask(ctx, row)
}

func (s *SQLiteTaskStore) AdmitInbound(ctx context.Context, row TaskRow) (TaskRow, error) {
	row.Direction = "inbound"
	row.Role = "executor"
	return s.insertTask(ctx, row)
}

func (s *SQLiteTaskStore) CreateOutboundTask(ctx context.Context, rec TaskRecord) (TaskRecord, error) {
	row, err := s.CreateOutbound(ctx, taskRowFromRecord(rec))
	return taskRecordFromRow(row), err
}

func (s *SQLiteTaskStore) AdmitInboundTask(ctx context.Context, rec TaskRecord) (TaskRecord, error) {
	row, err := s.AdmitInbound(ctx, taskRowFromRecord(rec))
	return taskRecordFromRow(row), err
}

func (s *SQLiteTaskStore) BindAcceptedTask(ctx context.Context, messageID string, taskID TaskID, executor AgentID) (TaskRecord, error) {
	row, err := s.BindAccepted(ctx, MessageID(messageID), taskID, executor)
	return taskRecordFromRow(row), err
}

func (s *SQLiteTaskStore) AppendTaskEvent(ctx context.Context, event TaskEvent) error {
	return s.AppendEvent(ctx, EventRow{TaskID: event.TaskID, Revision: event.Revision, EventType: event.EventType, PayloadJSON: string(event.Payload)})
}

func (s *SQLiteTaskStore) MarkTerminal(ctx context.Context, localID string, state TaskState, taskErr TaskError) (TaskRecord, error) {
	row, err := s.markTerminal(ctx, localID, state, taskErr)
	return taskRecordFromRow(row), err
}

func (s *SQLiteTaskStore) insertTask(ctx context.Context, row TaskRow) (TaskRow, error) {
	if err := normalizeTaskRow(&row); err != nil {
		return TaskRow{}, err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO a2a_tasks(
		local_id, task_id, client_task_ref, message_id, context_id, direction, role, from_agent, to_agent,
		executor_agent, channel_id, guild_id, channel_ref, skill_id, state, terminal, revision,
		result_visibility, discord_transcript_mode, discord_context_json, created_at, updated_at, expires_at,
		error_code, error_message)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.LocalID, nullString(string(row.TaskID)), nullString(row.ClientTaskRef), row.MessageID, nullString(row.ContextID),
		row.Direction, row.Role, row.FromAgent, row.ToAgent, nullString(string(row.ExecutorAgent)), nullString(row.ChannelID), nullString(row.GuildID),
		nullString(row.ChannelRef), nullString(row.SkillID), row.State, boolInt(row.Terminal), row.Revision, row.ResultVisibility,
		row.DiscordTranscriptMode, nullString(row.DiscordContextJSON), row.CreatedAt.UTC().Format(sqliteTimeFormat), row.UpdatedAt.UTC().Format(sqliteTimeFormat),
		nullTime(row.ExpiresAt), nullString(string(row.Error.Code)), nullString(row.Error.Message))
	if err != nil {
		if strings.Contains(err.Error(), "idx_a2a_tasks_message_direction") || strings.Contains(err.Error(), "UNIQUE constraint failed: a2a_tasks.direction, a2a_tasks.message_id") {
			return s.GetByDirectionMessage(ctx, row.Direction, row.MessageID)
		}
		return TaskRow{}, err
	}
	return row, nil
}

func (s *SQLiteTaskStore) BindAccepted(ctx context.Context, messageID MessageID, taskID TaskID, executor AgentID) (TaskRow, error) {
	if err := ValidateMessageID(messageID); err != nil {
		return TaskRow{}, err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return TaskRow{}, err
	}
	if err := ValidateAgentID(executor); err != nil {
		return TaskRow{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskRow{}, err
	}
	defer tx.Rollback()
	row, err := getTaskByDirectionMessageTx(ctx, tx, "outbound", messageID)
	if err != nil {
		return TaskRow{}, err
	}
	if row.TaskID != "" {
		if row.TaskID == taskID && (row.ExecutorAgent == "" || row.ExecutorAgent == executor) {
			return row, tx.Commit()
		}
		return TaskRow{}, fmt.Errorf("accepted bootstrap conflicts with existing task binding")
	}
	if row.ToAgent != executor && row.ExecutorAgent != executor {
		return TaskRow{}, fmt.Errorf("accepted executor %s does not match outbound target", executor)
	}
	if row.Terminal {
		return TaskRow{}, fmt.Errorf("terminal task cannot accept bootstrap")
	}
	now := time.Now().UTC().Format(sqliteTimeFormat)
	if _, err := tx.ExecContext(ctx, `UPDATE a2a_tasks SET task_id=?, executor_agent=?, state=?, revision=?, updated_at=? WHERE local_id=? AND task_id IS NULL AND terminal=0`, taskID, executor, TaskStateWorking, row.Revision+1, now, row.LocalID); err != nil {
		return TaskRow{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO a2a_task_events(task_id, revision, event_type, state, payload_json, created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(task_id, revision, event_type) DO NOTHING`, taskID, row.Revision+1, "accepted", TaskStateWorking, "{}", now); err != nil {
		return TaskRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskRow{}, err
	}
	return s.GetByDirectionMessage(ctx, "outbound", messageID)
}

func (s *SQLiteTaskStore) RejectBeforeAccepted(ctx context.Context, messageID MessageID, clientTaskRef string, executor AgentID, taskErr TaskError) (TaskRow, error) {
	if err := ValidateMessageID(messageID); err != nil {
		return TaskRow{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskRow{}, err
	}
	defer tx.Rollback()
	row, err := getTaskByDirectionMessageTx(ctx, tx, "outbound", messageID)
	if err != nil {
		return TaskRow{}, err
	}
	if strings.TrimSpace(clientTaskRef) != "" && row.ClientTaskRef != clientTaskRef {
		return TaskRow{}, fmt.Errorf("client_task_ref mismatch")
	}
	if row.TaskID != "" && !strings.HasPrefix(string(row.TaskID), "msg_") {
		return TaskRow{}, fmt.Errorf("task already accepted")
	}
	msgTaskID := TaskID("msg_" + string(messageID))
	now := time.Now().UTC().Format(sqliteTimeFormat)
	if _, err := tx.ExecContext(ctx, `UPDATE a2a_tasks SET task_id=?, executor_agent=?, state=?, terminal=1, revision=revision+1, updated_at=?, error_code=?, error_message=? WHERE local_id=? AND terminal=0`, msgTaskID, executor, TaskStateRejected, now, nullString(string(taskErr.Code)), nullString(taskErr.Message), row.LocalID); err != nil {
		return TaskRow{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO a2a_task_events(task_id, revision, event_type, state, payload_json, created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(task_id, revision, event_type) DO NOTHING`, msgTaskID, row.Revision+1, "rejected", TaskStateRejected, "{}", now); err != nil {
		return TaskRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskRow{}, err
	}
	return s.GetByDirectionMessage(ctx, "outbound", messageID)
}

func (s *SQLiteTaskStore) markTerminal(ctx context.Context, localID string, state TaskState, taskErr TaskError) (TaskRow, error) {
	if !IsTerminalState(state) {
		return TaskRow{}, fmt.Errorf("state %s is not terminal", state)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskRow{}, err
	}
	defer tx.Rollback()
	row, err := getTaskByLocalIDTx(ctx, tx, localID)
	if err != nil {
		return TaskRow{}, err
	}
	if row.Terminal {
		if row.State == state && row.Error == taskErr {
			return row, tx.Commit()
		}
		return TaskRow{}, fmt.Errorf("terminal task is immutable")
	}
	now := time.Now().UTC().Format(sqliteTimeFormat)
	if _, err := tx.ExecContext(ctx, `UPDATE a2a_tasks SET state=?, terminal=1, revision=revision+1, updated_at=?, error_code=?, error_message=? WHERE local_id=? AND terminal=0`, state, now, nullString(string(taskErr.Code)), nullString(taskErr.Message), localID); err != nil {
		return TaskRow{}, err
	}
	if row.TaskID != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO a2a_task_events(task_id, revision, event_type, state, payload_json, created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(task_id, revision, event_type) DO NOTHING`, row.TaskID, row.Revision+1, "status", state, "{}", now); err != nil {
			return TaskRow{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return TaskRow{}, err
	}
	return s.GetByLocalID(ctx, localID)
}

func (s *SQLiteTaskStore) GetByLocalID(ctx context.Context, localID string) (TaskRow, error) {
	return getTaskByLocalIDTx(ctx, s.db, localID)
}
func (s *SQLiteTaskStore) GetByDirectionMessage(ctx context.Context, direction string, messageID MessageID) (TaskRow, error) {
	return getTaskByDirectionMessageTx(ctx, s.db, direction, messageID)
}

func (s *SQLiteTaskStore) GetByDirectionTaskID(ctx context.Context, direction string, taskID TaskID) (TaskRow, error) {
	return getTaskByDirectionTaskIDTx(ctx, s.db, direction, taskID)
}

func (s *SQLiteTaskStore) CountOpenOutbound(ctx context.Context, channelID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM a2a_tasks WHERE direction='outbound' AND terminal=0 AND (?='' OR channel_id=?)`, strings.TrimSpace(channelID), strings.TrimSpace(channelID)).Scan(&count)
	return count, err
}

func (s *SQLiteTaskStore) ListByChannel(ctx context.Context, direction string, channelID string, limit int) ([]TaskRow, error) {
	direction = strings.TrimSpace(direction)
	channelID = strings.TrimSpace(channelID)
	if direction != "outbound" && direction != "inbound" {
		return nil, fmt.Errorf("direction must be outbound or inbound")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT local_id, task_id, client_task_ref, message_id, context_id, direction, role, from_agent, to_agent, executor_agent, channel_id, guild_id, channel_ref, skill_id, state, terminal, revision, result_visibility, discord_transcript_mode, discord_context_json, created_at, updated_at, expires_at, error_code, error_message FROM a2a_tasks WHERE direction=? AND (?='' OR channel_id=?) ORDER BY updated_at DESC LIMIT ?`, direction, channelID, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TaskRow, 0, limit)
	for rows.Next() {
		row, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *SQLiteTaskStore) RejectInbound(ctx context.Context, row TaskRow, taskErr TaskError) (TaskRow, error) {
	row.Direction = "inbound"
	row.Role = "executor"
	row.State = TaskStateRejected
	row.Terminal = true
	if row.TaskID == "" {
		row.TaskID = TaskID("msg_" + string(row.MessageID))
	}
	if row.Revision <= 0 {
		row.Revision = 1
	}
	row.Error = taskErr
	stored, err := s.insertTask(ctx, row)
	if err != nil {
		return TaskRow{}, err
	}
	payload := fmt.Sprintf(`{"error_code":%q,"error_message":%q}`, taskErr.Code, taskErr.Message)
	if err := s.AppendEvent(ctx, EventRow{TaskID: stored.TaskID, Revision: stored.Revision, EventType: "rejected", State: TaskStateRejected, PayloadJSON: payload}); err != nil {
		return TaskRow{}, err
	}
	return stored, nil
}

func (s *SQLiteTaskStore) ApplyTaskEvent(ctx context.Context, direction string, taskID TaskID, event EventRow, state TaskState, taskErr TaskError) (TaskRow, error) {
	if err := ValidateTaskID(taskID); err != nil {
		return TaskRow{}, err
	}
	if event.TaskID == "" {
		event.TaskID = taskID
	}
	if event.TaskID != taskID {
		return TaskRow{}, fmt.Errorf("event task_id does not match task")
	}
	if event.Revision <= 0 {
		return TaskRow{}, fmt.Errorf("revision must be positive")
	}
	if state == "" {
		state = event.State
	}
	if state == "" || !IsKnownTaskState(state) {
		return TaskRow{}, fmt.Errorf("unknown task state %s", state)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskRow{}, err
	}
	defer tx.Rollback()
	row, err := getTaskByDirectionTaskIDTx(ctx, tx, direction, taskID)
	if err != nil {
		return TaskRow{}, err
	}
	if event.Revision <= row.Revision {
		if err := appendEventInTx(ctx, tx, event); err != nil {
			return TaskRow{}, err
		}
		return row, tx.Commit()
	}
	if row.Terminal {
		return TaskRow{}, fmt.Errorf("terminal task is immutable")
	}
	now := time.Now().UTC().Format(sqliteTimeFormat)
	terminal := IsTerminalState(state)
	if _, err := tx.ExecContext(ctx, `UPDATE a2a_tasks SET state=?, terminal=?, revision=?, updated_at=?, error_code=?, error_message=? WHERE local_id=? AND terminal=0`, state, boolInt(terminal), event.Revision, now, nullString(string(taskErr.Code)), nullString(taskErr.Message), row.LocalID); err != nil {
		return TaskRow{}, err
	}
	if err := appendEventInTx(ctx, tx, event); err != nil {
		return TaskRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskRow{}, err
	}
	return s.GetByLocalID(ctx, row.LocalID)
}

func appendEventInTx(ctx context.Context, tx *sql.Tx, event EventRow) error {
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
	_, err := tx.ExecContext(ctx, `INSERT INTO a2a_task_events(task_id, revision, event_type, state, payload_json, created_at) VALUES(?,?,?,?,?,?)`, event.TaskID, event.Revision, event.EventType, nullString(string(event.State)), nullString(event.PayloadJSON), event.CreatedAt.UTC().Format(sqliteTimeFormat))
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return err
	}
	var existing EventRow
	var state, payload, created string
	err = tx.QueryRowContext(ctx, `SELECT id, task_id, revision, event_type, COALESCE(state,''), COALESCE(payload_json,''), created_at FROM a2a_task_events WHERE task_id=? AND revision=? AND event_type=?`, event.TaskID, event.Revision, event.EventType).Scan(&existing.ID, &existing.TaskID, &existing.Revision, &existing.EventType, &state, &payload, &created)
	if err != nil {
		return err
	}
	if existing.EventType == event.EventType && state == string(event.State) && payload == event.PayloadJSON {
		return nil
	}
	return fmt.Errorf("event revision replay differs")
}

func normalizeTaskRow(row *TaskRow) error {
	row.LocalID = strings.TrimSpace(row.LocalID)
	if row.LocalID == "" {
		row.LocalID = randomLocalID()
	}
	row.Direction = strings.TrimSpace(row.Direction)
	if row.Direction != "outbound" && row.Direction != "inbound" {
		return fmt.Errorf("direction must be outbound or inbound")
	}
	row.Role = strings.TrimSpace(row.Role)
	if row.Role != "delegator" && row.Role != "executor" {
		return fmt.Errorf("role must be delegator or executor")
	}
	if err := ValidateMessageID(row.MessageID); err != nil {
		return err
	}
	if err := ValidateAgentID(row.FromAgent); err != nil {
		return fmt.Errorf("from_agent: %w", err)
	}
	if err := ValidateAgentID(row.ToAgent); err != nil {
		return fmt.Errorf("to_agent: %w", err)
	}
	if row.ExecutorAgent != "" {
		if err := ValidateAgentID(row.ExecutorAgent); err != nil {
			return fmt.Errorf("executor_agent: %w", err)
		}
	}
	if row.TaskID != "" {
		if err := ValidateTaskID(row.TaskID); err != nil {
			return err
		}
	}
	if row.State == "" {
		row.State = TaskStateSubmitted
	}
	if !IsKnownTaskState(row.State) {
		return fmt.Errorf("unknown task state %s", row.State)
	}
	row.Terminal = IsTerminalState(row.State)
	if row.ResultVisibility == "" {
		row.ResultVisibility = "proxy"
	}
	if row.ResultVisibility != "proxy" && row.ResultVisibility != "transparent" {
		return fmt.Errorf("invalid result_visibility")
	}
	if row.DiscordTranscriptMode == "" {
		row.DiscordTranscriptMode = "delegator"
	}
	if row.DiscordTranscriptMode != "delegator" && row.DiscordTranscriptMode != "mirror" && row.DiscordTranscriptMode != "co_present" {
		return fmt.Errorf("invalid discord_transcript_mode")
	}
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = row.CreatedAt
	}
	return nil
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getTaskByLocalIDTx(ctx context.Context, q rowQuerier, localID string) (TaskRow, error) {
	return scanTask(q.QueryRowContext(ctx, `SELECT local_id, task_id, client_task_ref, message_id, context_id, direction, role, from_agent, to_agent, executor_agent, channel_id, guild_id, channel_ref, skill_id, state, terminal, revision, result_visibility, discord_transcript_mode, discord_context_json, created_at, updated_at, expires_at, error_code, error_message FROM a2a_tasks WHERE local_id=?`, localID))
}

func getTaskByDirectionMessageTx(ctx context.Context, q rowQuerier, direction string, messageID MessageID) (TaskRow, error) {
	return scanTask(q.QueryRowContext(ctx, `SELECT local_id, task_id, client_task_ref, message_id, context_id, direction, role, from_agent, to_agent, executor_agent, channel_id, guild_id, channel_ref, skill_id, state, terminal, revision, result_visibility, discord_transcript_mode, discord_context_json, created_at, updated_at, expires_at, error_code, error_message FROM a2a_tasks WHERE direction=? AND message_id=?`, direction, messageID))
}

func getTaskByDirectionTaskIDTx(ctx context.Context, q rowQuerier, direction string, taskID TaskID) (TaskRow, error) {
	return scanTask(q.QueryRowContext(ctx, `SELECT local_id, task_id, client_task_ref, message_id, context_id, direction, role, from_agent, to_agent, executor_agent, channel_id, guild_id, channel_ref, skill_id, state, terminal, revision, result_visibility, discord_transcript_mode, discord_context_json, created_at, updated_at, expires_at, error_code, error_message FROM a2a_tasks WHERE direction=? AND task_id=?`, direction, taskID))
}

func scanTask(row *sql.Row) (TaskRow, error) {
	var r TaskRow
	var taskID, clientRef, contextID, executor, channelID, guildID, channelRef, skillID, discordCtx, expiresAt, errCode, errMsg sql.NullString
	var created, updated string
	var terminal int
	err := row.Scan(&r.LocalID, &taskID, &clientRef, &r.MessageID, &contextID, &r.Direction, &r.Role, &r.FromAgent, &r.ToAgent, &executor, &channelID, &guildID, &channelRef, &skillID, &r.State, &terminal, &r.Revision, &r.ResultVisibility, &r.DiscordTranscriptMode, &discordCtx, &created, &updated, &expiresAt, &errCode, &errMsg)
	if err != nil {
		return TaskRow{}, err
	}
	r.TaskID = TaskID(taskID.String)
	r.ClientTaskRef = clientRef.String
	r.ContextID = contextID.String
	r.ExecutorAgent = AgentID(executor.String)
	r.ChannelID = channelID.String
	r.GuildID = guildID.String
	r.ChannelRef = channelRef.String
	r.SkillID = skillID.String
	r.Terminal = intBool(terminal)
	r.DiscordContextJSON = discordCtx.String
	r.Error = TaskError{Code: ErrorCode(errCode.String), Message: errMsg.String}
	r.CreatedAt, _ = time.Parse(sqliteTimeFormat, created)
	r.UpdatedAt, _ = time.Parse(sqliteTimeFormat, updated)
	if expiresAt.Valid {
		r.ExpiresAt, _ = time.Parse(sqliteTimeFormat, expiresAt.String)
	}
	return r, nil
}

func scanTaskRows(rows *sql.Rows) (TaskRow, error) {
	var r TaskRow
	var taskID, clientRef, contextID, executor, channelID, guildID, channelRef, skillID, discordCtx, expiresAt, errCode, errMsg sql.NullString
	var created, updated string
	var terminal int
	err := rows.Scan(&r.LocalID, &taskID, &clientRef, &r.MessageID, &contextID, &r.Direction, &r.Role, &r.FromAgent, &r.ToAgent, &executor, &channelID, &guildID, &channelRef, &skillID, &r.State, &terminal, &r.Revision, &r.ResultVisibility, &r.DiscordTranscriptMode, &discordCtx, &created, &updated, &expiresAt, &errCode, &errMsg)
	if err != nil {
		return TaskRow{}, err
	}
	r.TaskID = TaskID(taskID.String)
	r.ClientTaskRef = clientRef.String
	r.ContextID = contextID.String
	r.ExecutorAgent = AgentID(executor.String)
	r.ChannelID = channelID.String
	r.GuildID = guildID.String
	r.ChannelRef = channelRef.String
	r.SkillID = skillID.String
	r.Terminal = intBool(terminal)
	r.DiscordContextJSON = discordCtx.String
	r.Error = TaskError{Code: ErrorCode(errCode.String), Message: errMsg.String}
	r.CreatedAt, _ = time.Parse(sqliteTimeFormat, created)
	r.UpdatedAt, _ = time.Parse(sqliteTimeFormat, updated)
	if expiresAt.Valid {
		r.ExpiresAt, _ = time.Parse(sqliteTimeFormat, expiresAt.String)
	}
	return r, nil
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(sqliteTimeFormat)
}

func randomLocalID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "local_" + hex.EncodeToString(b[:])
}

func taskRecordFromRow(row TaskRow) TaskRecord {
	return TaskRecord{LocalID: row.LocalID, TaskID: row.TaskID, ClientTaskRef: row.ClientTaskRef, MessageID: row.MessageID, ContextID: row.ContextID, Direction: row.Direction, Role: row.Role, FromAgent: row.FromAgent, ToAgent: row.ToAgent, ExecutorAgent: row.ExecutorAgent, ChannelID: row.ChannelID, GuildID: row.GuildID, ChannelRef: row.ChannelRef, SkillID: row.SkillID, State: row.State, Terminal: row.Terminal, Revision: row.Revision, ResultVisibility: row.ResultVisibility, DiscordTranscriptMode: row.DiscordTranscriptMode, DiscordContextJSON: row.DiscordContextJSON, Error: row.Error}
}
func taskRowFromRecord(rec TaskRecord) TaskRow {
	return TaskRow{LocalID: rec.LocalID, TaskID: rec.TaskID, ClientTaskRef: rec.ClientTaskRef, MessageID: rec.MessageID, ContextID: rec.ContextID, Direction: rec.Direction, Role: rec.Role, FromAgent: rec.FromAgent, ToAgent: rec.ToAgent, ExecutorAgent: rec.ExecutorAgent, ChannelID: rec.ChannelID, GuildID: rec.GuildID, ChannelRef: rec.ChannelRef, SkillID: rec.SkillID, State: rec.State, Terminal: rec.Terminal, Revision: rec.Revision, ResultVisibility: rec.ResultVisibility, DiscordTranscriptMode: rec.DiscordTranscriptMode, DiscordContextJSON: rec.DiscordContextJSON, Error: rec.Error}
}

var errNoTask = errors.New("task not found")
