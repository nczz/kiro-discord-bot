package a2a

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type ObjectRef struct {
	ArtifactID string
	TaskID     TaskID
	Bucket     string
	Key        string
	Digest     string
	Size       int64
	MediaType  string
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

type SQLiteObjectStore struct{ db *sql.DB }

func OpenObjectStore(dataDir string) (*SQLiteObjectStore, error) {
	db, err := openA2ASQLite(dataDir, "objects.sqlite", objectStoreMigrations())
	if err != nil {
		return nil, err
	}
	return &SQLiteObjectStore{db: db}, nil
}
func (s *SQLiteObjectStore) Close() error { return closeSQL(s.db) }

func objectStoreMigrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS a2a_object_refs (
		artifact_id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		bucket TEXT NOT NULL,
		key TEXT NOT NULL,
		digest TEXT NOT NULL,
		size INTEGER NOT NULL,
		media_type TEXT NOT NULL,
		expires_at TEXT,
		created_at TEXT NOT NULL
	)`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_object_refs_task ON a2a_object_refs(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_object_refs_expires ON a2a_object_refs(expires_at)`,
	}
}

func (s *SQLiteObjectStore) PutRef(ctx context.Context, ref ObjectRef, content []byte) (ObjectRef, error) {
	if err := validateObjectRef(ref, content); err != nil {
		return ObjectRef{}, err
	}
	if ref.CreatedAt.IsZero() {
		ref.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO a2a_object_refs(artifact_id, task_id, bucket, key, digest, size, media_type, expires_at, created_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(artifact_id) DO UPDATE SET task_id=excluded.task_id, bucket=excluded.bucket, key=excluded.key, digest=excluded.digest, size=excluded.size, media_type=excluded.media_type, expires_at=excluded.expires_at`, ref.ArtifactID, ref.TaskID, ref.Bucket, ref.Key, ref.Digest, ref.Size, ref.MediaType, nullTime(ref.ExpiresAt), ref.CreatedAt.UTC().Format(sqliteTimeFormat))
	if err != nil {
		return ObjectRef{}, err
	}
	return ref, nil
}

func (s *SQLiteObjectStore) GetRef(ctx context.Context, artifactID string) (ObjectRef, error) {
	var r ObjectRef
	var created string
	var expires sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT artifact_id, task_id, bucket, key, digest, size, media_type, expires_at, created_at FROM a2a_object_refs WHERE artifact_id=?`, artifactID).Scan(&r.ArtifactID, &r.TaskID, &r.Bucket, &r.Key, &r.Digest, &r.Size, &r.MediaType, &expires, &created)
	if err != nil {
		return ObjectRef{}, err
	}
	if expires.Valid {
		r.ExpiresAt, _ = time.Parse(sqliteTimeFormat, expires.String)
	}
	r.CreatedAt, _ = time.Parse(sqliteTimeFormat, created)
	return r, nil
}

func (s *SQLiteObjectStore) PruneExpired(ctx context.Context, now time.Time) (int64, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM a2a_object_refs WHERE expires_at IS NOT NULL AND expires_at <> '' AND expires_at < ?`, now.UTC().Format(sqliteTimeFormat))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func validateObjectRef(ref ObjectRef, content []byte) error {
	if strings.TrimSpace(ref.ArtifactID) == "" {
		return fmt.Errorf("artifact_id is required")
	}
	if err := ValidateTaskID(ref.TaskID); err != nil {
		return err
	}
	if strings.TrimSpace(ref.Bucket) == "" || strings.TrimSpace(ref.Key) == "" {
		return fmt.Errorf("bucket and key are required")
	}
	if strings.Contains(ref.Key, "..") || strings.HasPrefix(ref.Key, "/") {
		return fmt.Errorf("object key must be generated relative path")
	}
	if ref.Size < 0 {
		return fmt.Errorf("object size cannot be negative")
	}
	if strings.TrimSpace(ref.MediaType) == "" {
		return fmt.Errorf("media_type is required")
	}
	if len(content) > 0 {
		if int64(len(content)) != ref.Size {
			return fmt.Errorf("object size mismatch")
		}
		digest := "sha256:" + sha256Hex(content)
		if digest != ref.Digest {
			return fmt.Errorf("object digest mismatch")
		}
	}
	if !strings.HasPrefix(ref.Digest, "sha256:") || len(ref.Digest) != len("sha256:")+64 {
		return fmt.Errorf("object digest must be sha256 hex")
	}
	return nil
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
