package a2a

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteTimeFormat = time.RFC3339Nano

func openA2ASQLite(dataDir, name string, migrations []string) (*sql.DB, error) {
	if dataDir == "" {
		dataDir = "./data"
	}
	path := filepath.Join(dataDir, "a2a", name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrateA2ASQLite(context.Background(), db, name, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrateA2ASQLite(ctx context.Context, db *sql.DB, component string, migrations []string) error {
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS a2a_schema_versions (
		component TEXT PRIMARY KEY,
		version INTEGER NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	for _, stmt := range migrations {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO a2a_schema_versions(component, version, applied_at)
		VALUES(?, ?, ?)
		ON CONFLICT(component) DO UPDATE SET version=excluded.version, applied_at=excluded.applied_at`, component, len(migrations), time.Now().UTC().Format(sqliteTimeFormat))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func closeSQL(db *sql.DB) error {
	if db == nil {
		return nil
	}
	return db.Close()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func intBool(v int) bool { return v != 0 }

func trimTaskState(state TaskState) TaskState { return TaskState(stringsTrimSpace(string(state))) }

func stringsTrimSpace(s string) string {
	for len(s) > 0 {
		switch s[0] {
		case ' ', '\t', '\n', '\r':
			s = s[1:]
		default:
			goto leftDone
		}
	}
leftDone:
	for len(s) > 0 {
		switch s[len(s)-1] {
		case ' ', '\t', '\n', '\r':
			s = s[:len(s)-1]
		default:
			return s
		}
	}
	return s
}
