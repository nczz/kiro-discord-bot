package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	_ "modernc.org/sqlite"
)

type Config struct {
	Enabled       bool
	DataDir       string
	DBPath        string
	QueueSize     int
	RetentionDays int
	RecordContent bool
	RecordTyping  bool
}

type Store struct {
	db            *sql.DB
	recordContent bool
}

type Event struct {
	Type            string
	GuildID         string
	ChannelID       string
	ThreadID        string
	ParentChannelID string
	MessageID       string
	UserID          string
	AuthorID        string
	AuthorUsername  string
	AuthorIsBot     *bool
	Content         string
	EventTS         time.Time
	RecordedAt      time.Time
	RawJSON         string
}

type BotEvent struct {
	Type            string         `json:"type"`
	GuildID         string         `json:"guild_id,omitempty"`
	ChannelID       string         `json:"channel_id,omitempty"`
	TargetID        string         `json:"target_id,omitempty"`
	ThreadID        string         `json:"thread_id,omitempty"`
	ParentChannelID string         `json:"parent_channel_id,omitempty"`
	MessageID       string         `json:"message_id,omitempty"`
	InteractionID   string         `json:"interaction_id,omitempty"`
	JobID           string         `json:"job_id,omitempty"`
	UserID          string         `json:"user_id,omitempty"`
	Username        string         `json:"username,omitempty"`
	Command         string         `json:"command,omitempty"`
	Source          string         `json:"source,omitempty"`
	Status          string         `json:"status,omitempty"`
	Content         string         `json:"content,omitempty"`
	Error           string         `json:"error,omitempty"`
	Model           string         `json:"model,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	OccurredAt      time.Time      `json:"occurred_at,omitempty"`
}

type TimelineEvent struct {
	Kind       string
	Type       string
	ChannelID  string
	TargetID   string
	ThreadID   string
	MessageID  string
	UserID     string
	Command    string
	Status     string
	Content    string
	RecordedAt string
}

func Open(cfg Config) (*Store, error) {
	path := cfg.DBPath
	if path == "" {
		path = filepath.Join(cfg.DataDir, "audit", "discord.sqlite")
	}
	if path == "" {
		return nil, errors.New("audit db path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, recordContent: cfg.RecordContent}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if cfg.RetentionDays > 0 {
		if err := s.Prune(context.Background(), cfg.RetentionDays); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) init() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS discord_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			guild_id TEXT,
			channel_id TEXT,
			thread_id TEXT,
			parent_channel_id TEXT,
			message_id TEXT,
			user_id TEXT,
			author_id TEXT,
			author_username TEXT,
			author_is_bot INTEGER,
			content TEXT,
			event_ts TEXT,
			recorded_at TEXT NOT NULL,
			raw_json TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_discord_events_channel_recorded ON discord_events(channel_id, recorded_at)`,
		`CREATE INDEX IF NOT EXISTS idx_discord_events_message ON discord_events(message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_discord_events_user_recorded ON discord_events(user_id, recorded_at)`,
		`CREATE INDEX IF NOT EXISTS idx_discord_events_type_recorded ON discord_events(event_type, recorded_at)`,
		`CREATE TABLE IF NOT EXISTS discord_messages (
			message_id TEXT PRIMARY KEY,
			guild_id TEXT,
			channel_id TEXT,
			thread_id TEXT,
			parent_channel_id TEXT,
			author_id TEXT,
			author_username TEXT,
			author_is_bot INTEGER,
			content TEXT,
			message_type INTEGER,
			flags INTEGER,
			tts INTEGER,
			mention_everyone INTEGER,
			pinned INTEGER,
			webhook_id TEXT,
			edited_at TEXT,
			deleted_at TEXT,
			created_at TEXT,
			first_recorded_at TEXT NOT NULL,
			last_recorded_at TEXT NOT NULL,
			raw_json TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_discord_messages_channel_created ON discord_messages(channel_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_discord_messages_author_created ON discord_messages(author_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS discord_attachments (
			attachment_id TEXT,
			message_id TEXT NOT NULL,
			guild_id TEXT,
			channel_id TEXT,
			thread_id TEXT,
			filename TEXT,
			content_type TEXT,
			size INTEGER,
			width INTEGER,
			height INTEGER,
			url TEXT,
			proxy_url TEXT,
			ephemeral INTEGER,
			duration_secs REAL,
			flags INTEGER,
			recorded_at TEXT NOT NULL,
			raw_json TEXT NOT NULL,
			PRIMARY KEY (message_id, attachment_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_discord_attachments_message ON discord_attachments(message_id)`,
		`CREATE TABLE IF NOT EXISTS discord_reactions (
			message_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			guild_id TEXT,
			user_id TEXT NOT NULL,
			emoji_id TEXT NOT NULL DEFAULT '',
			emoji_name TEXT NOT NULL DEFAULT '',
			emoji_animated INTEGER,
			added_at TEXT,
			removed_at TEXT,
			last_event_at TEXT NOT NULL,
			raw_json TEXT NOT NULL,
			PRIMARY KEY (message_id, user_id, emoji_id, emoji_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_discord_reactions_message ON discord_reactions(message_id)`,
		`CREATE TABLE IF NOT EXISTS discord_threads (
			thread_id TEXT PRIMARY KEY,
			guild_id TEXT,
			parent_channel_id TEXT,
			name TEXT,
			channel_type INTEGER,
			archived INTEGER,
			locked INTEGER,
			deleted_at TEXT,
			created_at TEXT,
			first_recorded_at TEXT NOT NULL,
			last_recorded_at TEXT NOT NULL,
			raw_json TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_discord_threads_parent ON discord_threads(parent_channel_id, last_recorded_at)`,
		`CREATE TABLE IF NOT EXISTS bot_audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			guild_id TEXT,
			channel_id TEXT,
			target_id TEXT,
			thread_id TEXT,
			parent_channel_id TEXT,
			message_id TEXT,
			interaction_id TEXT,
			job_id TEXT,
			user_id TEXT,
			username TEXT,
			command TEXT,
			source TEXT,
			status TEXT,
			content TEXT,
			error TEXT,
			model TEXT,
			metadata_json TEXT,
			occurred_at TEXT NOT NULL,
			recorded_at TEXT NOT NULL,
			raw_json TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_audit_events_target_time ON bot_audit_events(target_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_audit_events_user_time ON bot_audit_events(user_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_audit_events_type_time ON bot_audit_events(event_type, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_audit_events_job ON bot_audit_events(job_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RecordBotEvent(ctx context.Context, evt BotEvent) error {
	if s == nil || s.db == nil {
		return nil
	}
	now := time.Now()
	if evt.OccurredAt.IsZero() {
		evt.OccurredAt = now
	}
	if !s.recordContent {
		evt.Content = ""
	}
	raw, err := marshalPayload(evt, s.recordContent)
	if err != nil {
		return err
	}
	var metadata any
	if len(evt.Metadata) > 0 {
		data, err := marshalPayload(evt.Metadata, s.recordContent)
		if err != nil {
			return err
		}
		metadata = string(data)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO bot_audit_events (
		event_type, guild_id, channel_id, target_id, thread_id, parent_channel_id,
		message_id, interaction_id, job_id, user_id, username, command, source, status,
		content, error, model, metadata_json, occurred_at, recorded_at, raw_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		evt.Type, nullEmpty(evt.GuildID), nullEmpty(evt.ChannelID), nullEmpty(evt.TargetID),
		nullEmpty(evt.ThreadID), nullEmpty(evt.ParentChannelID), nullEmpty(evt.MessageID),
		nullEmpty(evt.InteractionID), nullEmpty(evt.JobID), nullEmpty(evt.UserID),
		nullEmpty(evt.Username), nullEmpty(evt.Command), nullEmpty(evt.Source), nullEmpty(evt.Status),
		nullEmpty(evt.Content), nullEmpty(evt.Error), nullEmpty(evt.Model), metadata,
		formatTime(evt.OccurredAt), formatTime(now), string(raw))
	return err
}

func (s *Store) Record(ctx context.Context, evt Event, projection any) error {
	if s == nil || s.db == nil {
		return nil
	}
	if evt.RecordedAt.IsZero() {
		evt.RecordedAt = time.Now()
	}
	if !s.recordContent {
		evt.Content = ""
	}
	raw := evt.RawJSON
	if raw == "" {
		data, err := marshalPayload(projection, s.recordContent)
		if err != nil {
			return err
		}
		raw = string(data)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := s.insertEvent(ctx, tx, evt, raw); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.project(ctx, tx, evt, projection, raw); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) insertEvent(ctx context.Context, tx *sql.Tx, evt Event, raw string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO discord_events (
		event_type, guild_id, channel_id, thread_id, parent_channel_id, message_id,
		user_id, author_id, author_username, author_is_bot, content, event_ts, recorded_at, raw_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		evt.Type, nullEmpty(evt.GuildID), nullEmpty(evt.ChannelID), nullEmpty(evt.ThreadID),
		nullEmpty(evt.ParentChannelID), nullEmpty(evt.MessageID), nullEmpty(evt.UserID),
		nullEmpty(evt.AuthorID), nullEmpty(evt.AuthorUsername), nullableBool(evt.AuthorIsBot),
		nullEmpty(evt.Content), formatTime(evt.EventTS), formatTime(evt.RecordedAt), raw)
	return err
}

func (s *Store) project(ctx context.Context, tx *sql.Tx, evt Event, payload any, raw string) error {
	switch v := payload.(type) {
	case *discordgo.MessageCreate:
		return s.upsertMessage(ctx, tx, v.Message, evt, raw, false)
	case *discordgo.MessageUpdate:
		return s.upsertMessage(ctx, tx, v.Message, evt, raw, false)
	case *discordgo.MessageDelete:
		return s.markMessageDeleted(ctx, tx, v.Message, evt, raw)
	case *discordgo.MessageDeleteBulk:
		for _, id := range v.Messages {
			if _, err := tx.ExecContext(ctx, `INSERT INTO discord_messages (
				message_id, guild_id, channel_id, deleted_at, first_recorded_at, last_recorded_at, raw_json
			) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(message_id) DO UPDATE SET
				deleted_at=excluded.deleted_at,
				last_recorded_at=excluded.last_recorded_at,
				raw_json=excluded.raw_json`,
				id, nullEmpty(v.GuildID), nullEmpty(v.ChannelID), formatTime(evt.RecordedAt), formatTime(evt.RecordedAt), formatTime(evt.RecordedAt), raw); err != nil {
				return err
			}
		}
	case *discordgo.MessageReactionAdd:
		return s.upsertReaction(ctx, tx, v.MessageReaction, raw, evt.RecordedAt, true)
	case *discordgo.MessageReactionRemove:
		return s.upsertReaction(ctx, tx, v.MessageReaction, raw, evt.RecordedAt, false)
	case *discordgo.MessageReactionRemoveAll:
		if v.MessageReaction == nil {
			return nil
		}
		_, err := tx.ExecContext(ctx, `UPDATE discord_reactions SET removed_at=?, last_event_at=?, raw_json=? WHERE message_id=? AND channel_id=?`,
			formatTime(evt.RecordedAt), formatTime(evt.RecordedAt), raw, v.MessageID, v.ChannelID)
		return err
	case *discordgo.ThreadCreate:
		return s.upsertThread(ctx, tx, v.Channel, raw, evt.RecordedAt, false)
	case *discordgo.ThreadUpdate:
		return s.upsertThread(ctx, tx, v.Channel, raw, evt.RecordedAt, false)
	case *discordgo.ThreadDelete:
		return s.upsertThread(ctx, tx, v.Channel, raw, evt.RecordedAt, true)
	}
	return nil
}

func (s *Store) upsertMessage(ctx context.Context, tx *sql.Tx, msg *discordgo.Message, evt Event, raw string, deleted bool) error {
	if msg == nil || msg.ID == "" {
		return nil
	}
	content := msg.Content
	if !s.recordContent {
		content = ""
	}
	threadID, parentID := messageThreadIDs(msg)
	if threadID == "" {
		threadID = evt.ThreadID
	}
	if parentID == "" {
		parentID = evt.ParentChannelID
	}
	authorID, username, isBot := messageAuthor(msg)
	createdAt := msg.Timestamp
	deletedAt := time.Time{}
	if deleted {
		deletedAt = evt.RecordedAt
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO discord_messages (
		message_id, guild_id, channel_id, thread_id, parent_channel_id, author_id,
		author_username, author_is_bot, content, message_type, flags, tts, mention_everyone,
		pinned, webhook_id, edited_at, deleted_at, created_at, first_recorded_at, last_recorded_at, raw_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(message_id) DO UPDATE SET
		guild_id=COALESCE(excluded.guild_id, discord_messages.guild_id),
		channel_id=COALESCE(excluded.channel_id, discord_messages.channel_id),
		thread_id=COALESCE(excluded.thread_id, discord_messages.thread_id),
		parent_channel_id=COALESCE(excluded.parent_channel_id, discord_messages.parent_channel_id),
		author_id=COALESCE(excluded.author_id, discord_messages.author_id),
		author_username=COALESCE(excluded.author_username, discord_messages.author_username),
		author_is_bot=COALESCE(excluded.author_is_bot, discord_messages.author_is_bot),
		content=excluded.content,
		message_type=excluded.message_type,
		flags=excluded.flags,
		tts=excluded.tts,
		mention_everyone=excluded.mention_everyone,
		pinned=excluded.pinned,
		webhook_id=COALESCE(excluded.webhook_id, discord_messages.webhook_id),
		edited_at=COALESCE(excluded.edited_at, discord_messages.edited_at),
		deleted_at=COALESCE(excluded.deleted_at, discord_messages.deleted_at),
		created_at=COALESCE(excluded.created_at, discord_messages.created_at),
		last_recorded_at=excluded.last_recorded_at,
		raw_json=excluded.raw_json`,
		msg.ID, nullEmpty(msg.GuildID), nullEmpty(msg.ChannelID), nullEmpty(threadID), nullEmpty(parentID),
		nullEmpty(authorID), nullEmpty(username), nullableBool(isBot), nullEmpty(content), int(msg.Type),
		int(msg.Flags), boolInt(msg.TTS), boolInt(msg.MentionEveryone), boolInt(msg.Pinned), nullEmpty(msg.WebhookID),
		formatTimePtr(msg.EditedTimestamp), formatTime(deletedAt), formatTime(createdAt), formatTime(evt.RecordedAt), formatTime(evt.RecordedAt), raw)
	if err != nil {
		return err
	}
	for _, att := range msg.Attachments {
		if att == nil {
			continue
		}
		attRaw, _ := json.Marshal(att)
		if _, err := tx.ExecContext(ctx, `INSERT INTO discord_attachments (
			attachment_id, message_id, guild_id, channel_id, thread_id, filename, content_type,
			size, width, height, url, proxy_url, ephemeral, duration_secs, flags, recorded_at, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id, attachment_id) DO UPDATE SET
			filename=excluded.filename,
			content_type=excluded.content_type,
			size=excluded.size,
			width=excluded.width,
			height=excluded.height,
			url=excluded.url,
			proxy_url=excluded.proxy_url,
			ephemeral=excluded.ephemeral,
			duration_secs=excluded.duration_secs,
			flags=excluded.flags,
			recorded_at=excluded.recorded_at,
			raw_json=excluded.raw_json`,
			att.ID, msg.ID, nullEmpty(msg.GuildID), nullEmpty(msg.ChannelID), nullEmpty(threadID), nullEmpty(att.Filename),
			nullEmpty(att.ContentType), att.Size, att.Width, att.Height, nullEmpty(att.URL), nullEmpty(att.ProxyURL),
			boolInt(att.Ephemeral), att.DurationSecs, int(att.Flags), formatTime(evt.RecordedAt), string(attRaw)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) markMessageDeleted(ctx context.Context, tx *sql.Tx, msg *discordgo.Message, evt Event, raw string) error {
	if msg == nil || msg.ID == "" {
		return nil
	}
	threadID, parentID := messageThreadIDs(msg)
	if threadID == "" {
		threadID = evt.ThreadID
	}
	if parentID == "" {
		parentID = evt.ParentChannelID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO discord_messages (
		message_id, guild_id, channel_id, thread_id, parent_channel_id,
		deleted_at, first_recorded_at, last_recorded_at, raw_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(message_id) DO UPDATE SET
		guild_id=COALESCE(excluded.guild_id, discord_messages.guild_id),
		channel_id=COALESCE(excluded.channel_id, discord_messages.channel_id),
		thread_id=COALESCE(excluded.thread_id, discord_messages.thread_id),
		parent_channel_id=COALESCE(excluded.parent_channel_id, discord_messages.parent_channel_id),
		deleted_at=excluded.deleted_at,
		last_recorded_at=excluded.last_recorded_at,
		raw_json=excluded.raw_json`,
		msg.ID, nullEmpty(msg.GuildID), nullEmpty(msg.ChannelID), nullEmpty(threadID), nullEmpty(parentID),
		formatTime(evt.RecordedAt), formatTime(evt.RecordedAt), formatTime(evt.RecordedAt), raw)
	return err
}

func (s *Store) upsertReaction(ctx context.Context, tx *sql.Tx, reaction *discordgo.MessageReaction, raw string, recordedAt time.Time, added bool) error {
	if reaction == nil {
		return nil
	}
	emojiID := reaction.Emoji.ID
	emojiName := reaction.Emoji.Name
	if added {
		_, err := tx.ExecContext(ctx, `INSERT INTO discord_reactions (
			message_id, channel_id, guild_id, user_id, emoji_id, emoji_name, emoji_animated, added_at, removed_at, last_event_at, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)
		ON CONFLICT(message_id, user_id, emoji_id, emoji_name) DO UPDATE SET
			added_at=excluded.added_at,
			removed_at=NULL,
			last_event_at=excluded.last_event_at,
			raw_json=excluded.raw_json`,
			reaction.MessageID, reaction.ChannelID, nullEmpty(reaction.GuildID), reaction.UserID, emojiID, emojiName,
			boolInt(reaction.Emoji.Animated), formatTime(recordedAt), formatTime(recordedAt), raw)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO discord_reactions (
		message_id, channel_id, guild_id, user_id, emoji_id, emoji_name, emoji_animated, removed_at, last_event_at, raw_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(message_id, user_id, emoji_id, emoji_name) DO UPDATE SET
		removed_at=excluded.removed_at,
		last_event_at=excluded.last_event_at,
		raw_json=excluded.raw_json`,
		reaction.MessageID, reaction.ChannelID, nullEmpty(reaction.GuildID), reaction.UserID, emojiID, emojiName,
		boolInt(reaction.Emoji.Animated), formatTime(recordedAt), formatTime(recordedAt), raw)
	return err
}

func (s *Store) upsertThread(ctx context.Context, tx *sql.Tx, ch *discordgo.Channel, raw string, recordedAt time.Time, deleted bool) error {
	if ch == nil || ch.ID == "" {
		return nil
	}
	archived, locked := 0, 0
	if ch.ThreadMetadata != nil {
		archived = boolInt(ch.ThreadMetadata.Archived)
		locked = boolInt(ch.ThreadMetadata.Locked)
	}
	var deletedAt any
	if deleted {
		deletedAt = formatTime(recordedAt)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO discord_threads (
		thread_id, guild_id, parent_channel_id, name, channel_type, archived, locked,
		deleted_at, created_at, first_recorded_at, last_recorded_at, raw_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(thread_id) DO UPDATE SET
		guild_id=COALESCE(excluded.guild_id, discord_threads.guild_id),
		parent_channel_id=COALESCE(excluded.parent_channel_id, discord_threads.parent_channel_id),
		name=COALESCE(excluded.name, discord_threads.name),
		channel_type=excluded.channel_type,
		archived=excluded.archived,
		locked=excluded.locked,
		deleted_at=COALESCE(excluded.deleted_at, discord_threads.deleted_at),
		last_recorded_at=excluded.last_recorded_at,
		raw_json=excluded.raw_json`,
		ch.ID, nullEmpty(ch.GuildID), nullEmpty(ch.ParentID), nullEmpty(ch.Name), int(ch.Type),
		archived, locked, deletedAt, "", formatTime(recordedAt), formatTime(recordedAt), raw)
	return err
}

func (s *Store) Prune(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Format(time.RFC3339)
	for _, stmt := range []string{
		`DELETE FROM discord_events WHERE recorded_at < ?`,
		`DELETE FROM discord_attachments WHERE recorded_at < ?`,
		`DELETE FROM discord_reactions WHERE last_event_at < ?`,
		`DELETE FROM discord_messages WHERE last_recorded_at < ?`,
		`DELETE FROM discord_threads WHERE last_recorded_at < ?`,
		`DELETE FROM bot_audit_events WHERE recorded_at < ?`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt, cutoff); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RecentTimeline(ctx context.Context, targetID string, limit int) ([]TimelineEvent, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, event_type, channel_id, target_id, thread_id, message_id, user_id, command, status, content, recorded_at
FROM (
	SELECT 'discord' AS kind, event_type, COALESCE(channel_id, '') AS channel_id, COALESCE(channel_id, '') AS target_id, COALESCE(thread_id, '') AS thread_id, COALESCE(message_id, '') AS message_id, COALESCE(user_id, '') AS user_id, '' AS command, '' AS status, COALESCE(content, '') AS content, recorded_at
	FROM discord_events
	WHERE channel_id = ? OR thread_id = ? OR parent_channel_id = ?
	UNION ALL
	SELECT 'bot' AS kind, event_type, COALESCE(channel_id, '') AS channel_id, COALESCE(target_id, '') AS target_id, COALESCE(thread_id, '') AS thread_id, COALESCE(message_id, '') AS message_id, COALESCE(user_id, '') AS user_id, COALESCE(command, '') AS command, COALESCE(status, '') AS status, COALESCE(content, '') AS content, recorded_at
	FROM bot_audit_events
	WHERE target_id = ? OR channel_id = ? OR thread_id = ? OR parent_channel_id = ?
)
ORDER BY recorded_at DESC
LIMIT ?`, targetID, targetID, targetID, targetID, targetID, targetID, targetID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TimelineEvent
	for rows.Next() {
		var e TimelineEvent
		if err := rows.Scan(&e.Kind, &e.Type, &e.ChannelID, &e.TargetID, &e.ThreadID, &e.MessageID, &e.UserID, &e.Command, &e.Status, &e.Content, &e.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func EventFromPayload(eventType string, payload any, parentChannelID func(string) string) Event {
	evt := Event{Type: eventType, RecordedAt: time.Now()}
	switch v := payload.(type) {
	case *discordgo.MessageCreate:
		fillMessageEvent(&evt, v.Message, parentChannelID)
	case *discordgo.MessageUpdate:
		fillMessageEvent(&evt, v.Message, parentChannelID)
	case *discordgo.MessageDelete:
		fillMessageEvent(&evt, v.Message, parentChannelID)
	case *discordgo.MessageDeleteBulk:
		evt.GuildID = v.GuildID
		evt.ChannelID = v.ChannelID
		evt.ParentChannelID = parentChannelID(v.ChannelID)
		if evt.ParentChannelID != "" {
			evt.ThreadID = v.ChannelID
		}
	case *discordgo.MessageReactionAdd:
		fillReactionEvent(&evt, v.MessageReaction, parentChannelID)
		evt.UserID = v.UserID
	case *discordgo.MessageReactionRemove:
		fillReactionEvent(&evt, v.MessageReaction, parentChannelID)
		evt.UserID = v.UserID
	case *discordgo.MessageReactionRemoveAll:
		fillReactionEvent(&evt, v.MessageReaction, parentChannelID)
	case *discordgo.ThreadCreate:
		fillChannelEvent(&evt, v.Channel)
		evt.Type = eventType
	case *discordgo.ThreadUpdate:
		fillChannelEvent(&evt, v.Channel)
		evt.Type = eventType
	case *discordgo.ThreadDelete:
		fillChannelEvent(&evt, v.Channel)
		evt.Type = eventType
	case *discordgo.ChannelCreate:
		fillChannelEvent(&evt, v.Channel)
	case *discordgo.ChannelUpdate:
		fillChannelEvent(&evt, v.Channel)
	case *discordgo.ChannelDelete:
		fillChannelEvent(&evt, v.Channel)
	case *discordgo.ChannelPinsUpdate:
		evt.GuildID = v.GuildID
		evt.ChannelID = v.ChannelID
		evt.ParentChannelID = parentChannelID(v.ChannelID)
		if evt.ParentChannelID != "" {
			evt.ThreadID = v.ChannelID
		}
		evt.EventTS = parseDiscordTime(v.LastPinTimestamp)
	case *discordgo.InteractionCreate:
		if v.Interaction != nil {
			evt.GuildID = v.GuildID
			evt.ChannelID = v.ChannelID
			evt.UserID, evt.AuthorUsername = interactionUser(v.Interaction)
			evt.ParentChannelID = parentChannelID(v.ChannelID)
			if evt.ParentChannelID != "" {
				evt.ThreadID = v.ChannelID
			}
		}
	case *discordgo.TypingStart:
		evt.GuildID = v.GuildID
		evt.ChannelID = v.ChannelID
		evt.UserID = v.UserID
		evt.EventTS = time.Unix(int64(v.Timestamp), 0)
		evt.ParentChannelID = parentChannelID(v.ChannelID)
		if evt.ParentChannelID != "" {
			evt.ThreadID = v.ChannelID
		}
	}
	return evt
}

func fillMessageEvent(evt *Event, msg *discordgo.Message, parentChannelID func(string) string) {
	if msg == nil {
		return
	}
	evt.GuildID = msg.GuildID
	evt.ChannelID = msg.ChannelID
	evt.MessageID = msg.ID
	evt.Content = msg.Content
	evt.EventTS = msg.Timestamp
	evt.ThreadID, evt.ParentChannelID = messageThreadIDs(msg)
	if evt.ParentChannelID == "" && parentChannelID != nil {
		evt.ParentChannelID = parentChannelID(msg.ChannelID)
		if evt.ParentChannelID != "" {
			evt.ThreadID = msg.ChannelID
		}
	}
	evt.AuthorID, evt.AuthorUsername, evt.AuthorIsBot = messageAuthor(msg)
	evt.UserID = evt.AuthorID
}

func fillReactionEvent(evt *Event, reaction *discordgo.MessageReaction, parentChannelID func(string) string) {
	if reaction == nil {
		return
	}
	evt.GuildID = reaction.GuildID
	evt.ChannelID = reaction.ChannelID
	evt.MessageID = reaction.MessageID
	evt.UserID = reaction.UserID
	evt.ParentChannelID = parentChannelID(reaction.ChannelID)
	if evt.ParentChannelID != "" {
		evt.ThreadID = reaction.ChannelID
	}
}

func fillChannelEvent(evt *Event, ch *discordgo.Channel) {
	if ch == nil {
		return
	}
	evt.GuildID = ch.GuildID
	evt.ChannelID = ch.ID
	if ch.IsThread() {
		evt.ThreadID = ch.ID
		evt.ParentChannelID = ch.ParentID
	}
}

func messageThreadIDs(msg *discordgo.Message) (threadID, parentID string) {
	if msg == nil {
		return "", ""
	}
	if msg.Thread != nil {
		return msg.Thread.ID, msg.ChannelID
	}
	return "", ""
}

func messageAuthor(msg *discordgo.Message) (string, string, *bool) {
	if msg == nil || msg.Author == nil {
		return "", "", nil
	}
	isBot := msg.Author.Bot
	name := strings.TrimSpace(msg.Author.Username)
	if msg.Author.Discriminator != "" && msg.Author.Discriminator != "0" {
		name = fmt.Sprintf("%s#%s", name, msg.Author.Discriminator)
	}
	return msg.Author.ID, name, &isBot
}

func interactionUser(i *discordgo.Interaction) (string, string) {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID, i.Member.User.Username
	}
	if i.User != nil {
		return i.User.ID, i.User.Username
	}
	return "", ""
}

func nullEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullableBool(v *bool) any {
	if v == nil {
		return nil
	}
	return boolInt(*v)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func formatTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseDiscordTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, raw)
	return t
}

func marshalPayload(payload any, recordContent bool) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil || recordContent {
		return data, err
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return data, nil
	}
	redactContent(decoded)
	return json.Marshal(decoded)
}

func redactContent(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if k == "content" {
				x[k] = nil
				continue
			}
			redactContent(val)
		}
	case []any:
		for _, item := range x {
			redactContent(item)
		}
	}
}
