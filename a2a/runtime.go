package a2a

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
)

type RuntimeIDMode string

const (
	RuntimeIDModeLegacy  RuntimeIDMode = "legacy"
	RuntimeIDModeDual    RuntimeIDMode = "dual"
	RuntimeIDModeRuntime RuntimeIDMode = "runtime"
)

func NormalizeRuntimeIDMode(raw string) (RuntimeIDMode, error) {
	switch RuntimeIDMode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", RuntimeIDModeLegacy:
		return RuntimeIDModeLegacy, nil
	case RuntimeIDModeDual:
		return RuntimeIDModeDual, nil
	case RuntimeIDModeRuntime:
		return RuntimeIDModeRuntime, nil
	default:
		return "", fmt.Errorf("A2A_RUNTIME_ID_MODE must be legacy, dual, or runtime")
	}
}

func (m RuntimeIDMode) String() string {
	if m == "" {
		return string(RuntimeIDModeLegacy)
	}
	return string(m)
}

func (m RuntimeIDMode) UsesRuntimeIDs() bool {
	return m == RuntimeIDModeDual || m == RuntimeIDModeRuntime
}

type RuntimeRecord struct {
	RuntimeAgentID AgentID   `json:"runtimeAgentId"`
	BotAgentID     AgentID   `json:"botAgentId"`
	GuildID        string    `json:"guildId"`
	ChannelID      string    `json:"channelId"`
	ThreadID       string    `json:"threadId,omitempty"`
	ChannelRef     string    `json:"channelRef"`
	DisplayName    string    `json:"displayName,omitempty"`
	RuntimeKind    string    `json:"runtimeKind"`
	Enabled        bool      `json:"enabled"`
	Discoverable   bool      `json:"discoverable"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type RuntimeStore struct {
	db *sql.DB
}

func GenerateRuntimeAgentID(bot AgentID, channelRef string) (AgentID, error) {
	if err := ValidateAgentID(bot); err != nil {
		return "", err
	}
	base := string(bot)
	slug := runtimeSlug(channelRef)
	if slug != "" && !digitsPattern.MatchString(slug) {
		candidate := base + "-" + slug
		if len(candidate) <= 64 {
			id := AgentID(candidate)
			if err := ValidateAgentID(id); err == nil {
				return id, nil
			}
		}
	}
	sum := sha256.Sum256([]byte(base + "\x00" + strings.TrimSpace(channelRef)))
	hash := hex.EncodeToString(sum[:6])
	prefix := base
	maxPrefix := 64 - len("-rt-") - len(hash)
	if len(prefix) > maxPrefix {
		prefix = strings.TrimRight(prefix[:maxPrefix], "-_")
	}
	if prefix == "" {
		prefix = "rt"
	}
	id := AgentID(prefix + "-rt-" + hash)
	if err := ValidateAgentID(id); err != nil {
		return "", err
	}
	return id, nil
}

func runtimeSlug(raw string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	lastSep := false
	for _, r := range raw {
		valid := r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
		if !valid {
			if b.Len() > 0 && !lastSep {
				b.WriteByte('-')
				lastSep = true
			}
			continue
		}
		if r == '-' || r == '_' {
			if b.Len() == 0 || lastSep {
				continue
			}
			b.WriteByte('-')
			lastSep = true
			continue
		}
		if r > unicode.MaxASCII {
			if b.Len() > 0 && !lastSep {
				b.WriteByte('-')
				lastSep = true
			}
			continue
		}
		b.WriteByte(byte(unicode.ToLower(r)))
		lastSep = false
	}
	return strings.Trim(b.String(), "-")
}

func OpenRuntimeStore(dataDir string) (*RuntimeStore, error) {
	db, err := openA2ASQLite(dataDir, "runtime.sqlite", runtimeStoreMigrations())
	if err != nil {
		return nil, err
	}
	return &RuntimeStore{db: db}, nil
}

func (s *RuntimeStore) Close() error { return closeSQL(s.db) }

func runtimeStoreMigrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS a2a_runtime_registry (
		runtime_agent_id TEXT PRIMARY KEY,
		bot_agent_id TEXT NOT NULL,
		guild_id TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		thread_id TEXT NOT NULL DEFAULT '',
		channel_ref TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		runtime_kind TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 0,
		discoverable INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(bot_agent_id, guild_id, channel_id, thread_id)
	)`,
	}
}

func (s *RuntimeStore) Upsert(ctx context.Context, r RuntimeRecord) (RuntimeRecord, error) {
	if err := validateRuntimeRecord(r); err != nil {
		return RuntimeRecord{}, err
	}
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	r.ThreadID = strings.TrimSpace(r.ThreadID)
	r.DisplayName = strings.TrimSpace(r.DisplayName)
	_, err := s.db.ExecContext(ctx, `INSERT INTO a2a_runtime_registry(runtime_agent_id, bot_agent_id, guild_id, channel_id, thread_id, channel_ref, display_name, runtime_kind, enabled, discoverable, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(runtime_agent_id) DO UPDATE SET channel_ref=excluded.channel_ref, display_name=excluded.display_name, runtime_kind=excluded.runtime_kind, enabled=excluded.enabled, discoverable=excluded.discoverable, updated_at=excluded.updated_at`,
		string(r.RuntimeAgentID), string(r.BotAgentID), strings.TrimSpace(r.GuildID), strings.TrimSpace(r.ChannelID), r.ThreadID, strings.TrimSpace(r.ChannelRef), r.DisplayName, strings.TrimSpace(r.RuntimeKind), boolInt(r.Enabled), boolInt(r.Discoverable), r.CreatedAt.Format(sqliteTimeFormat), r.UpdatedAt.Format(sqliteTimeFormat))
	if err != nil {
		return RuntimeRecord{}, err
	}
	return s.Get(ctx, r.RuntimeAgentID)
}

func (s *RuntimeStore) Get(ctx context.Context, runtime AgentID) (RuntimeRecord, error) {
	if err := ValidateAgentID(runtime); err != nil {
		return RuntimeRecord{}, err
	}
	return s.scan(ctx, `SELECT runtime_agent_id, bot_agent_id, guild_id, channel_id, thread_id, channel_ref, display_name, runtime_kind, enabled, discoverable, created_at, updated_at FROM a2a_runtime_registry WHERE runtime_agent_id=?`, string(runtime))
}

func (s *RuntimeStore) GetByDiscord(ctx context.Context, bot AgentID, guildID, channelID, threadID string) (RuntimeRecord, error) {
	if err := ValidateAgentID(bot); err != nil {
		return RuntimeRecord{}, err
	}
	return s.scan(ctx, `SELECT runtime_agent_id, bot_agent_id, guild_id, channel_id, thread_id, channel_ref, display_name, runtime_kind, enabled, discoverable, created_at, updated_at FROM a2a_runtime_registry WHERE bot_agent_id=? AND guild_id=? AND channel_id=? AND thread_id=?`, string(bot), strings.TrimSpace(guildID), strings.TrimSpace(channelID), strings.TrimSpace(threadID))
}

func (s *RuntimeStore) ListOwned(ctx context.Context, bot AgentID) ([]RuntimeRecord, error) {
	if err := ValidateAgentID(bot); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT runtime_agent_id, bot_agent_id, guild_id, channel_id, thread_id, channel_ref, display_name, runtime_kind, enabled, discoverable, created_at, updated_at FROM a2a_runtime_registry WHERE bot_agent_id=? ORDER BY runtime_agent_id`, string(bot))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RuntimeRecord
	for rows.Next() {
		r, err := scanRuntime(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) scan(ctx context.Context, query string, args ...any) (RuntimeRecord, error) {
	row := s.db.QueryRowContext(ctx, query, args...)
	return scanRuntime(row)
}

type runtimeScanner interface {
	Scan(dest ...any) error
}

func scanRuntime(row runtimeScanner) (RuntimeRecord, error) {
	var r RuntimeRecord
	var enabled, discoverable int
	var created, updated string
	if err := row.Scan(&r.RuntimeAgentID, &r.BotAgentID, &r.GuildID, &r.ChannelID, &r.ThreadID, &r.ChannelRef, &r.DisplayName, &r.RuntimeKind, &enabled, &discoverable, &created, &updated); err != nil {
		return RuntimeRecord{}, err
	}
	r.Enabled = intBool(enabled)
	r.Discoverable = intBool(discoverable)
	r.CreatedAt, _ = time.Parse(sqliteTimeFormat, created)
	r.UpdatedAt, _ = time.Parse(sqliteTimeFormat, updated)
	return r, nil
}

func validateRuntimeRecord(r RuntimeRecord) error {
	if err := ValidateAgentID(r.RuntimeAgentID); err != nil {
		return fmt.Errorf("runtime_agent_id: %w", err)
	}
	if err := ValidateAgentID(r.BotAgentID); err != nil {
		return fmt.Errorf("bot_agent_id: %w", err)
	}
	if strings.TrimSpace(r.GuildID) == "" || strings.TrimSpace(r.ChannelID) == "" {
		return fmt.Errorf("guild_id and channel_id are required")
	}
	if strings.TrimSpace(r.ChannelRef) == "" {
		return fmt.Errorf("channel_ref is required")
	}
	kind := strings.TrimSpace(r.RuntimeKind)
	if kind != "channel" && kind != "thread" {
		return fmt.Errorf("runtime_kind must be channel or thread")
	}
	if r.Discoverable && !r.Enabled {
		return fmt.Errorf("discoverable runtime must be enabled")
	}
	return nil
}
