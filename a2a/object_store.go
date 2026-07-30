package a2a

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const DefaultObjectBucket = "a2a-artifacts"

type ObjectRef struct {
	ArtifactID string    `json:"artifactId"`
	TaskID     TaskID    `json:"taskId"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"key"`
	Digest     string    `json:"digest"`
	Size       int64     `json:"size"`
	MediaType  string    `json:"mediaType"`
	ExpiresAt  time.Time `json:"expiresAt,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
}

type ObjectBackend interface {
	PutObject(ctx context.Context, bucket string, key string, content []byte, mediaType string) error
	GetObject(ctx context.Context, bucket string, key string) ([]byte, error)
	DeleteObject(ctx context.Context, bucket string, key string) error
}

type ObjectStoreOption func(*SQLiteObjectStore)

func WithObjectBackend(backend ObjectBackend) ObjectStoreOption {
	return func(s *SQLiteObjectStore) { s.backend = backend }
}

type SQLiteObjectStore struct {
	db      *sql.DB
	backend ObjectBackend
}

func OpenObjectStore(dataDir string, opts ...ObjectStoreOption) (*SQLiteObjectStore, error) {
	db, err := openA2ASQLite(dataDir, "objects.sqlite", objectStoreMigrations())
	if err != nil {
		return nil, err
	}
	store := &SQLiteObjectStore{db: db}
	for _, opt := range opts {
		opt(store)
	}
	return store, nil
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

func (s *SQLiteObjectStore) PutObject(ctx context.Context, taskID TaskID, artifactID, name, mediaType string, content []byte, retentionDays int) (ObjectRef, error) {
	if s == nil {
		return ObjectRef{}, fmt.Errorf("object store is nil")
	}
	if s.backend == nil {
		return ObjectRef{}, fmt.Errorf("object byte backend is unavailable")
	}
	ref := NewObjectRef(taskID, artifactID, name, mediaType, content, retentionDays)
	if err := s.backend.PutObject(ctx, ref.Bucket, ref.Key, content, ref.MediaType); err != nil {
		return ObjectRef{}, err
	}
	return s.PutRef(ctx, ref, content)
}

func (s *SQLiteObjectStore) FetchObject(ctx context.Context, artifactID string) ([]byte, ObjectRef, error) {
	if s == nil {
		return nil, ObjectRef{}, fmt.Errorf("object store is nil")
	}
	if s.backend == nil {
		return nil, ObjectRef{}, fmt.Errorf("object byte backend is unavailable")
	}
	ref, err := s.GetRef(ctx, artifactID)
	if err != nil {
		return nil, ObjectRef{}, err
	}
	content, err := s.backend.GetObject(ctx, ref.Bucket, ref.Key)
	if err != nil {
		return nil, ObjectRef{}, err
	}
	if err := validateObjectRef(ref, content); err != nil {
		return nil, ObjectRef{}, err
	}
	return content, ref, nil
}

func (s *SQLiteObjectStore) PutRef(ctx context.Context, ref ObjectRef, content []byte) (ObjectRef, error) {
	if s == nil || s.db == nil {
		return ObjectRef{}, fmt.Errorf("object store is unavailable")
	}
	ref.normalize()
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
	err := s.db.QueryRowContext(ctx, `SELECT artifact_id, task_id, bucket, key, digest, size, media_type, expires_at, created_at FROM a2a_object_refs WHERE artifact_id=?`, strings.TrimSpace(artifactID)).Scan(&r.ArtifactID, &r.TaskID, &r.Bucket, &r.Key, &r.Digest, &r.Size, &r.MediaType, &expires, &created)
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
	if s == nil || s.db == nil {
		return 0, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := s.db.QueryContext(ctx, `SELECT artifact_id, bucket, key FROM a2a_object_refs WHERE expires_at IS NOT NULL AND expires_at <> '' AND expires_at < ?`, now.UTC().Format(sqliteTimeFormat))
	if err != nil {
		return 0, err
	}
	type expiredRef struct {
		artifactID string
		bucket     string
		key        string
	}
	var expired []expiredRef
	for rows.Next() {
		var ref expiredRef
		if err := rows.Scan(&ref.artifactID, &ref.bucket, &ref.key); err != nil {
			rows.Close()
			return 0, err
		}
		expired = append(expired, ref)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, ref := range expired {
		if s.backend != nil {
			_ = s.backend.DeleteObject(ctx, ref.bucket, ref.key)
		}
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM a2a_object_refs WHERE expires_at IS NOT NULL AND expires_at <> '' AND expires_at < ?`, now.UTC().Format(sqliteTimeFormat))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func NewObjectRef(taskID TaskID, artifactID, name, mediaType string, content []byte, retentionDays int) ObjectRef {
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		artifactID = "artifact-" + sha256Hex([]byte(string(taskID) + ":" + name + ":" + sha256Hex(content)))[:16]
	}
	now := time.Now().UTC()
	ref := ObjectRef{
		ArtifactID: artifactID,
		TaskID:     taskID,
		Bucket:     DefaultObjectBucket,
		Key:        objectKey(taskID, artifactID, name),
		Digest:     "sha256:" + sha256Hex(content),
		Size:       int64(len(content)),
		MediaType:  canonicalMediaType(mediaType),
		CreatedAt:  now,
	}
	if retentionDays > 0 {
		ref.ExpiresAt = now.Add(time.Duration(retentionDays) * 24 * time.Hour)
	}
	return ref
}

func (r *ObjectRef) normalize() {
	r.ArtifactID = strings.TrimSpace(r.ArtifactID)
	r.Bucket = strings.TrimSpace(r.Bucket)
	r.Key = strings.TrimSpace(r.Key)
	r.Digest = strings.ToLower(strings.TrimSpace(r.Digest))
	r.MediaType = canonicalMediaType(r.MediaType)
}

func validateObjectRef(ref ObjectRef, content []byte) error {
	ref.normalize()
	if strings.TrimSpace(ref.ArtifactID) == "" {
		return fmt.Errorf("artifact_id is required")
	}
	if err := ValidateTaskID(ref.TaskID); err != nil {
		return err
	}
	if ref.Bucket != DefaultObjectBucket {
		return fmt.Errorf("bucket %q is not allowed", ref.Bucket)
	}
	if strings.TrimSpace(ref.Key) == "" {
		return fmt.Errorf("object key is required")
	}
	if strings.Contains(ref.Key, "..") || strings.HasPrefix(ref.Key, "/") || path.Clean(ref.Key) != ref.Key || !strings.HasPrefix(ref.Key, "tasks/"+string(ref.TaskID)+"/") {
		return fmt.Errorf("object key must be generated relative path")
	}
	if ref.Size < 0 {
		return fmt.Errorf("object size cannot be negative")
	}
	if strings.TrimSpace(ref.MediaType) == "" {
		return fmt.Errorf("media_type is required")
	}
	if _, _, err := mime.ParseMediaType(ref.MediaType); err != nil {
		return fmt.Errorf("invalid media_type %q", ref.MediaType)
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

var objectNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func objectKey(taskID TaskID, artifactID, name string) string {
	base := path.Base(strings.TrimSpace(name))
	if base == "." || base == "/" || base == "" {
		base = artifactID
	}
	base = objectNameUnsafe.ReplaceAllString(base, "-")
	base = strings.Trim(base, ".-")
	if base == "" {
		base = "artifact"
	}
	return "tasks/" + string(taskID) + "/" + objectNameUnsafe.ReplaceAllString(artifactID, "-") + "/" + base
}

func canonicalMediaType(mediaType string) string {
	mediaType = strings.TrimSpace(mediaType)
	if mediaType == "" {
		return "application/octet-stream"
	}
	parsed, params, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return mediaType
	}
	if len(params) == 0 {
		return strings.ToLower(parsed)
	}
	return mime.FormatMediaType(strings.ToLower(parsed), params)
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

type JetStreamObjectBackend struct {
	js     jetstream.JetStream
	mu     sync.Mutex
	stores map[string]jetstream.ObjectStore
}

func NewJetStreamObjectBackend(js jetstream.JetStream) *JetStreamObjectBackend {
	return &JetStreamObjectBackend{js: js, stores: make(map[string]jetstream.ObjectStore)}
}

func (b *JetStreamObjectBackend) PutObject(ctx context.Context, bucket string, key string, content []byte, mediaType string) error {
	store, err := b.store(ctx, bucket)
	if err != nil {
		return err
	}
	_, err = store.Put(ctx, jetstream.ObjectMeta{Name: key, Metadata: map[string]string{"media_type": canonicalMediaType(mediaType)}}, bytes.NewReader(content))
	return err
}

func (b *JetStreamObjectBackend) GetObject(ctx context.Context, bucket string, key string) ([]byte, error) {
	store, err := b.store(ctx, bucket)
	if err != nil {
		return nil, err
	}
	result, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	content, err := io.ReadAll(result)
	if err != nil {
		return nil, err
	}
	if err := result.Error(); err != nil {
		return nil, err
	}
	return content, nil
}

func (b *JetStreamObjectBackend) DeleteObject(ctx context.Context, bucket string, key string) error {
	store, err := b.store(ctx, bucket)
	if err != nil {
		return err
	}
	return store.Delete(ctx, key)
}

func (b *JetStreamObjectBackend) store(ctx context.Context, bucket string) (jetstream.ObjectStore, error) {
	if b == nil || b.js == nil {
		return nil, fmt.Errorf("JetStream object backend is unavailable")
	}
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		bucket = DefaultObjectBucket
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if store := b.stores[bucket]; store != nil {
		return store, nil
	}
	store, err := b.js.CreateOrUpdateObjectStore(ctx, jetstream.ObjectStoreConfig{Bucket: bucket})
	if err != nil {
		return nil, err
	}
	b.stores[bucket] = store
	return store, nil
}
