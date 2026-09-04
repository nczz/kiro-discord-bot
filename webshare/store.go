package webshare

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

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound      = errors.New("webshare: not found")
	ErrScopeMismatch = errors.New("webshare: scoped reference mismatch")
	ErrExpiredRef    = errors.New("webshare: attachment reference expired")
	ErrActiveShare   = errors.New("webshare: active share already exists for opener target")
)

type Store struct {
	db     *sql.DB
	master []byte
}

type CreateShareRequest struct {
	ShareID         string
	GuildID         string
	TargetType      TargetType
	TargetID        string
	ParentChannelID string
	OpenerUserID    string
	OpenerUsername  string
	RelayURL        string
	PublicBaseURL   string
	RoomID          string
	RoomKey         []byte
	WriteToken      []byte
	Capabilities    Capabilities
	Status          Status
	Now             time.Time
}

func OpenStore(ctx context.Context, dataDir string) (*Store, error) {
	if dataDir == "" {
		dataDir = "./data"
	}
	if err := os.MkdirAll(WebShareDir(dataDir), 0700); err != nil {
		return nil, err
	}
	master, err := LoadOrCreateMasterKey(dataDir)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", SQLitePath(dataDir))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, master: master}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) CreateShare(ctx context.Context, req CreateShareRequest) (*Share, error) {
	if err := validateCreateShare(req); err != nil {
		return nil, err
	}
	now := req.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	status := req.Status
	if status == "" {
		status = StatusCreated
	}
	caps := req.Capabilities
	if !caps.View && !caps.Write {
		caps = CapabilitiesForWrite(len(req.WriteToken) == WriteTokenSize)
	}
	wrapped, err := WrapRoomKeyWithMaster(s.master, req.RoomKey)
	if err != nil {
		return nil, err
	}
	capsJSON, err := json.Marshal(caps)
	if err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO webshare_sessions(
share_id,guild_id,target_type,target_id,parent_channel_id,opener_user_id,opener_username,relay_url,public_base_url,room_id,room_key_ciphertext,write_token_hash,view_secret_fingerprint,capabilities_json,status,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		req.ShareID, req.GuildID, string(req.TargetType), req.TargetID, req.ParentChannelID, req.OpenerUserID, req.OpenerUsername, req.RelayURL, req.PublicBaseURL, req.RoomID, wrapped, TokenHash(req.WriteToken), TokenFingerprint(req.RoomKey), string(capsJSON), string(status), encodeTime(now), encodeTime(now))
	if err != nil {
		if strings.Contains(err.Error(), "webshare_active_opener_target") || strings.Contains(err.Error(), "constraint failed") {
			return nil, fmt.Errorf("%w: %v", ErrActiveShare, err)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetShare(ctx, req.ShareID, withKnownRowID(id))
}

func (s *Store) GetShare(ctx context.Context, shareID string, opts ...getOption) (*Share, error) {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return nil, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `SELECT id,share_id,guild_id,target_type,target_id,parent_channel_id,opener_user_id,opener_username,relay_url,public_base_url,room_id,room_key_ciphertext,write_token_hash,view_secret_fingerprint,capabilities_json,status,created_at,updated_at,last_connected_at,last_peer_seen_at,revoked_at,revoked_by_user_id,revoke_reason FROM webshare_sessions WHERE share_id = ?`, shareID)
	return scanShare(row)
}

type getOption struct{}

func withKnownRowID(_ int64) getOption { return getOption{} }

func (s *Store) ListActive(ctx context.Context) ([]Share, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,share_id,guild_id,target_type,target_id,parent_channel_id,opener_user_id,opener_username,relay_url,public_base_url,room_id,room_key_ciphertext,write_token_hash,view_secret_fingerprint,capabilities_json,status,created_at,updated_at,last_connected_at,last_peer_seen_at,revoked_at,revoked_by_user_id,revoke_reason FROM webshare_sessions WHERE status IN ('created','connecting','active','disconnected') ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Share
	for rows.Next() {
		share, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *share)
	}
	return out, rows.Err()
}

func (s *Store) UnwrapRoomKey(share *Share) ([]byte, error) {
	if share == nil {
		return nil, ErrNotFound
	}
	return UnwrapRoomKeyWithMaster(s.master, share.RoomKeyCiphertext)
}

func (s *Store) MarkStatus(ctx context.Context, shareID string, status Status) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE webshare_sessions SET status=?, updated_at=? WHERE share_id=?`, string(status), encodeTime(now), shareID)
	return oneRow(res, err)
}

func (s *Store) MarkConnected(ctx context.Context, shareID string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `UPDATE webshare_sessions SET status=?, updated_at=?, last_connected_at=? WHERE share_id=? AND status IN ('created','connecting','active','disconnected')`, string(StatusActive), encodeTime(at.UTC()), encodeTime(at.UTC()), shareID)
	return oneRow(res, err)
}

func (s *Store) MarkPeerSeen(ctx context.Context, shareID string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE webshare_sessions SET updated_at=?, last_peer_seen_at=? WHERE share_id=?`, encodeTime(at.UTC()), encodeTime(at.UTC()), shareID)
	return err
}

func (s *Store) AcceptPeerSequence(ctx context.Context, shareID string, peerID uint32, seq uint64, at time.Time) (bool, error) {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return false, fmt.Errorf("share id is required")
	}
	if seq > uint64(1<<63-1) {
		return false, fmt.Errorf("peer sequence exceeds storage range")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO webshare_peer_sequences(share_id,peer_id,highest_seq,updated_at) VALUES(?,?,?,?) ON CONFLICT(share_id,peer_id) DO UPDATE SET highest_seq=excluded.highest_seq, updated_at=excluded.updated_at WHERE excluded.highest_seq > webshare_peer_sequences.highest_seq`, shareID, int64(peerID), int64(seq), encodeTime(at))
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *Store) ClaimAction(ctx context.Context, shareID, actionID, actionType string, at time.Time) (bool, error) {
	shareID = strings.TrimSpace(shareID)
	actionID = strings.TrimSpace(actionID)
	if shareID == "" || actionID == "" {
		return false, fmt.Errorf("share id and action id are required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO webshare_action_receipts(share_id,action_id,action_type,created_at) VALUES(?,?,?,?)`, shareID, actionID, strings.TrimSpace(actionType), encodeTime(at))
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *Store) Revoke(ctx context.Context, shareID, byUserID, reason string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE webshare_sessions SET status=?, updated_at=?, revoked_at=?, revoked_by_user_id=?, revoke_reason=? WHERE share_id=?`, string(StatusRevoked), encodeTime(now), encodeTime(now), byUserID, reason, shareID)
	return oneRow(res, err)
}

type PruneResult struct {
	ExpiredShares         int64
	DeletedShares         int64
	DeletedEvents         int64
	DeletedManagedThreads int64
	DeletedAttachmentRefs int64
	DeletedPeerSequences  int64
	DeletedActionReceipts int64
}

func (r PruneResult) Total() int64 {
	return r.ExpiredShares + r.DeletedShares + r.DeletedEvents + r.DeletedManagedThreads + r.DeletedAttachmentRefs + r.DeletedPeerSequences + r.DeletedActionReceipts
}

func (s *Store) PruneStale(ctx context.Context, now time.Time, activeTTL, historyTTL time.Duration) (PruneResult, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PruneResult{}, err
	}
	var out PruneResult
	exec := func(dest *int64, query string, args ...any) error {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		*dest, err = res.RowsAffected()
		return err
	}
	if activeTTL > 0 {
		cutoff := encodeTime(now.Add(-activeTTL))
		if err := exec(&out.ExpiredShares, `UPDATE webshare_sessions SET status=?, updated_at=? WHERE status IN ('created','connecting','active','disconnected') AND updated_at < ?`, string(StatusExpired), encodeTime(now), cutoff); err != nil {
			_ = tx.Rollback()
			return PruneResult{}, err
		}
	}
	if err := exec(&out.DeletedAttachmentRefs, `DELETE FROM webshare_attachment_refs WHERE expires_at != '' AND expires_at < ?`, encodeTime(now)); err != nil {
		_ = tx.Rollback()
		return PruneResult{}, err
	}
	if historyTTL > 0 {
		cutoff := encodeTime(now.Add(-historyTTL))
		staleShares := `SELECT share_id FROM webshare_sessions WHERE status NOT IN ('created','connecting','active','disconnected') AND updated_at < ?`
		if err := exec(&out.DeletedEvents, `DELETE FROM webshare_events WHERE share_id IN (`+staleShares+`)`, cutoff); err != nil {
			_ = tx.Rollback()
			return PruneResult{}, err
		}
		if err := exec(&out.DeletedManagedThreads, `DELETE FROM webshare_managed_child_threads WHERE share_id IN (`+staleShares+`)`, cutoff); err != nil {
			_ = tx.Rollback()
			return PruneResult{}, err
		}
		if err := exec(&out.DeletedPeerSequences, `DELETE FROM webshare_peer_sequences WHERE share_id IN (`+staleShares+`)`, cutoff); err != nil {
			_ = tx.Rollback()
			return PruneResult{}, err
		}
		if err := exec(&out.DeletedActionReceipts, `DELETE FROM webshare_action_receipts WHERE share_id IN (`+staleShares+`)`, cutoff); err != nil {
			_ = tx.Rollback()
			return PruneResult{}, err
		}
		if err := exec(&out.DeletedShares, `DELETE FROM webshare_sessions WHERE status NOT IN ('created','connecting','active','disconnected') AND updated_at < ?`, cutoff); err != nil {
			_ = tx.Rollback()
			return PruneResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return PruneResult{}, err
	}
	return out, nil
}

func (s *Store) RecordEvent(ctx context.Context, e Event) (int64, error) {
	if strings.TrimSpace(e.ShareID) == "" || strings.TrimSpace(e.Type) == "" {
		return 0, fmt.Errorf("event share id and type are required")
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	} else {
		e.CreatedAt = e.CreatedAt.UTC()
	}
	meta, err := json.Marshal(nonNilMap(e.Metadata))
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO webshare_events(share_id,event_type,actor_user_id,remote_actor_name,target_id,allowed,reason_code,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, e.ShareID, e.Type, e.ActorUserID, e.RemoteActorName, e.TargetID, boolInt(e.Allowed), e.ReasonCode, string(meta), encodeTime(e.CreatedAt))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) RegisterManagedChildThread(ctx context.Context, t ManagedChildThread) error {
	if strings.TrimSpace(t.ShareID) == "" || strings.TrimSpace(t.ParentChannelID) == "" || strings.TrimSpace(t.ThreadID) == "" {
		return fmt.Errorf("share id, parent channel id, and thread id are required")
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	} else {
		t.CreatedAt = t.CreatedAt.UTC()
	}
	meta, err := json.Marshal(nonNilMap(t.Metadata))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO webshare_managed_child_threads(share_id,parent_channel_id,thread_id,name,created_by_user_id,metadata_json,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(share_id,thread_id) DO UPDATE SET name=excluded.name, metadata_json=excluded.metadata_json`, t.ShareID, t.ParentChannelID, t.ThreadID, t.Name, t.CreatedByUserID, string(meta), encodeTime(t.CreatedAt))
	return err
}

func (s *Store) UnregisterManagedChildThread(ctx context.Context, shareID, threadID string) error {
	shareID = strings.TrimSpace(shareID)
	threadID = strings.TrimSpace(threadID)
	if shareID == "" || threadID == "" {
		return fmt.Errorf("share id and thread id are required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM webshare_managed_child_threads WHERE share_id=? AND thread_id=?`, shareID, threadID)
	return err
}

func (s *Store) ResolveManagedChildThread(ctx context.Context, shareID, threadID string) (ManagedChildThread, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,share_id,parent_channel_id,thread_id,name,created_by_user_id,metadata_json,created_at FROM webshare_managed_child_threads WHERE share_id=? AND thread_id=?`, shareID, threadID)
	var t ManagedChildThread
	var meta, created string
	if err := row.Scan(&t.ID, &t.ShareID, &t.ParentChannelID, &t.ThreadID, &t.Name, &t.CreatedByUserID, &meta, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ManagedChildThread{}, ErrNotFound
		}
		return ManagedChildThread{}, err
	}
	_ = json.Unmarshal([]byte(meta), &t.Metadata)
	t.CreatedAt = parseTime(created)
	return t, nil
}

func (s *Store) ListManagedChildThreads(ctx context.Context, shareID string) ([]ManagedChildThread, error) {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,share_id,parent_channel_id,thread_id,name,created_by_user_id,metadata_json,created_at FROM webshare_managed_child_threads WHERE share_id=? ORDER BY created_at DESC`, shareID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ManagedChildThread
	for rows.Next() {
		var t ManagedChildThread
		var meta, created string
		if err := rows.Scan(&t.ID, &t.ShareID, &t.ParentChannelID, &t.ThreadID, &t.Name, &t.CreatedByUserID, &meta, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(meta), &t.Metadata)
		t.CreatedAt = parseTime(created)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) IssueAttachmentRef(ctx context.Context, ref AttachmentRef) (AttachmentRef, error) {
	if strings.TrimSpace(ref.ShareID) == "" || strings.TrimSpace(ref.TargetID) == "" || strings.TrimSpace(ref.MessageID) == "" || strings.TrimSpace(ref.AttachmentID) == "" {
		return AttachmentRef{}, fmt.Errorf("attachment ref scope fields are required")
	}
	var err error
	if strings.TrimSpace(ref.ID) == "" {
		ref.ID, err = randomID("war_", 18)
		if err != nil {
			return AttachmentRef{}, err
		}
	}
	if ref.CreatedAt.IsZero() {
		ref.CreatedAt = time.Now().UTC()
	} else {
		ref.CreatedAt = ref.CreatedAt.UTC()
	}
	if ref.ExpiresAt.IsZero() {
		ref.ExpiresAt = ref.CreatedAt.Add(24 * time.Hour)
	} else {
		ref.ExpiresAt = ref.ExpiresAt.UTC()
	}
	meta, err := json.Marshal(nonNilMap(ref.Metadata))
	if err != nil {
		return AttachmentRef{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO webshare_attachment_refs(ref_id,share_id,target_id,message_id,attachment_id,filename,size,content_type,metadata_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, ref.ID, ref.ShareID, ref.TargetID, ref.MessageID, ref.AttachmentID, ref.Filename, ref.Size, ref.ContentType, string(meta), encodeTime(ref.ExpiresAt), encodeTime(ref.CreatedAt))
	if err != nil {
		return AttachmentRef{}, err
	}
	return ref, nil
}

func (s *Store) ResolveAttachmentRef(ctx context.Context, shareID, refID string) (AttachmentRef, error) {
	row := s.db.QueryRowContext(ctx, `SELECT ref_id,share_id,target_id,message_id,attachment_id,filename,size,content_type,metadata_json,expires_at,created_at FROM webshare_attachment_refs WHERE ref_id=?`, refID)
	var ref AttachmentRef
	var meta, expires, created string
	if err := row.Scan(&ref.ID, &ref.ShareID, &ref.TargetID, &ref.MessageID, &ref.AttachmentID, &ref.Filename, &ref.Size, &ref.ContentType, &meta, &expires, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AttachmentRef{}, ErrNotFound
		}
		return AttachmentRef{}, err
	}
	if ref.ShareID != shareID {
		return AttachmentRef{}, ErrScopeMismatch
	}
	_ = json.Unmarshal([]byte(meta), &ref.Metadata)
	ref.ExpiresAt = parseTime(expires)
	ref.CreatedAt = parseTime(created)
	if !ref.ExpiresAt.IsZero() && time.Now().UTC().After(ref.ExpiresAt) {
		return AttachmentRef{}, ErrExpiredRef
	}
	return ref, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, webshareSchema)
	return err
}

func validateCreateShare(req CreateShareRequest) error {
	if strings.TrimSpace(req.ShareID) == "" || strings.TrimSpace(req.GuildID) == "" || strings.TrimSpace(req.TargetID) == "" || strings.TrimSpace(req.OpenerUserID) == "" || strings.TrimSpace(req.RoomID) == "" {
		return fmt.Errorf("share id, guild id, target id, opener user id, and room id are required")
	}
	if req.TargetType != TargetChannel && req.TargetType != TargetThread {
		return fmt.Errorf("invalid target type %q", req.TargetType)
	}
	if len(req.RoomKey) != RoomKeySize {
		return fmt.Errorf("room key must be %d bytes", RoomKeySize)
	}
	if len(req.WriteToken) != WriteTokenSize {
		return fmt.Errorf("write token must be %d bytes", WriteTokenSize)
	}
	return nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanShare(row rowScanner) (*Share, error) {
	var sh Share
	var targetType, status, capsJSON string
	var created, updated, connected, peerSeen, revoked string
	if err := row.Scan(&sh.ID, &sh.ShareID, &sh.GuildID, &targetType, &sh.TargetID, &sh.ParentChannelID, &sh.OpenerUserID, &sh.OpenerUsername, &sh.RelayURL, &sh.PublicBaseURL, &sh.RoomID, &sh.RoomKeyCiphertext, &sh.WriteTokenHash, &sh.ViewSecretFingerprint, &capsJSON, &status, &created, &updated, &connected, &peerSeen, &revoked, &sh.RevokedByUserID, &sh.RevokeReason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sh.TargetType = TargetType(targetType)
	sh.Status = Status(status)
	_ = json.Unmarshal([]byte(capsJSON), &sh.Capabilities)
	sh.CreatedAt = parseTime(created)
	sh.UpdatedAt = parseTime(updated)
	sh.LastConnectedAt = parseTime(connected)
	sh.LastPeerSeenAt = parseTime(peerSeen)
	sh.RevokedAt = parseTime(revoked)
	return &sh, nil
}

func oneRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func encodeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func EnsureParentMode(path string) (os.FileMode, error) {
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return 0, err
	}
	return info.Mode().Perm(), nil
}
