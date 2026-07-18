package channel

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nczz/kiro-discord-bot/acp"
	_ "modernc.org/sqlite"
)

const usageTimeFormat = time.RFC3339
const usageDBTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

func formatUsageDBTime(t time.Time) string { return t.UTC().Format(usageDBTimeFormat) }

// UsageRecord is an append-only audit entry for one completed agent turn.
type UsageRecord struct {
	UsageID           string             `json:"usage_id,omitempty"`
	Timestamp         string             `json:"ts"`
	GuildID           string             `json:"guild_id,omitempty"`
	ChannelID         string             `json:"channel_id"`
	ThreadID          string             `json:"thread_id,omitempty"`
	UserID            string             `json:"user_id"`
	Username          string             `json:"username,omitempty"`
	MessageID         string             `json:"message_id,omitempty"`
	InteractionID     string             `json:"interaction_id,omitempty"`
	InvocationID      string             `json:"invocation_id,omitempty"`
	Model             string             `json:"model,omitempty"`
	Engine            string             `json:"engine,omitempty"`
	Source            string             `json:"source"`
	Status            string             `json:"status"`
	Credits           float64            `json:"credits"`
	CostUSD           float64            `json:"cost_usd,omitempty"`
	MeteringSupported bool               `json:"metering_supported"`
	MeteringUsage     []acp.MeteringItem `json:"metering_usage,omitempty"`
	DurationMs        int64              `json:"duration_ms,omitempty"`
	ContextUsage      float64            `json:"context_usage,omitempty"`
}

type UsageStore struct {
	mu              sync.Mutex
	dir             string
	db              *sql.DB
	initErr         error
	location        *time.Location
	retentionMonths int
	lastPruneMonth  string
}

type UsageReport struct {
	GeneratedAt time.Time
	Location    *time.Location
	DayStart    time.Time
	WeekStart   time.Time
	MonthStart  time.Time
	Rows        []UsageReportRow
	Totals      UsageReportTotals
}

type UsageReportRow struct {
	UserID            string
	Username          string
	DayCredits        float64
	WeekCredits       float64
	MonthCredits      float64
	DayCostUSD        float64
	WeekCostUSD       float64
	MonthCostUSD      float64
	DayTurns          int
	WeekTurns         int
	MonthTurns        int
	MeteredDayTurns   int
	MeteredWeekTurns  int
	MeteredMonthTurns int
}

type UsageReportTotals struct {
	DayCredits   float64
	WeekCredits  float64
	MonthCredits float64
	DayCostUSD   float64
	WeekCostUSD  float64
	MonthCostUSD float64
	DayTurns     int
	WeekTurns    int
	MonthTurns   int
}

type UsageHistoryOptions struct {
	GuildID, UserID, Status, Source string
	From, To                        time.Time
	Limit                           int
	BeforeTime, BeforeID            string
}

type UsageHistoryPage struct {
	Records          []UsageRecord
	NextTime, NextID string
}

type UsageHealth struct {
	Healthy                               bool
	SchemaVersion, Records, FailedImports int
	Error                                 string
}

func NewUsageStore(dataDir, timezone string, retentionMonths int) *UsageStore {
	loc := resolveUsageLocation(timezone)
	usageDir := ""
	if strings.TrimSpace(dataDir) != "" {
		usageDir = filepath.Join(dataDir, "usage")
	}
	s := &UsageStore{
		dir:             usageDir,
		location:        loc,
		retentionMonths: retentionMonths,
	}
	if err := s.open(dataDir); err != nil {
		s.initErr = err
		return s
	}
	if err := s.importLegacyFiles(); err != nil {
		s.initErr = err
		_ = s.db.Close()
		s.db = nil
		return s
	}
	if retentionMonths > 0 {
		if err := s.PruneExpired(time.Now().In(loc)); err != nil {
			s.initErr = err
		}
	}
	return s
}

func (s *UsageStore) open(dataDir string) error {
	dsn := "file:usage-" + uuid.NewString() + "?mode=memory&cache=shared"
	if strings.TrimSpace(dataDir) != "" {
		if err := os.MkdirAll(s.dir, 0755); err != nil {
			return err
		}
		dsn = filepath.Join(s.dir, "usage.sqlite")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s.db = db
	var schemaVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&schemaVersion); err != nil {
		_ = db.Close()
		s.db = nil
		return err
	}
	if schemaVersion > 1 {
		_ = db.Close()
		s.db = nil
		return fmt.Errorf("usage database schema %d is newer than supported version 1", schemaVersion)
	}
	stmts := []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS usage_records (
			usage_id TEXT PRIMARY KEY, occurred_at TEXT NOT NULL, guild_id TEXT NOT NULL DEFAULT '',
			channel_id TEXT NOT NULL DEFAULT '', thread_id TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '', message_id TEXT NOT NULL DEFAULT '', interaction_id TEXT NOT NULL DEFAULT '',
			invocation_id TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', engine TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL, status TEXT NOT NULL, credits REAL NOT NULL DEFAULT 0, cost_usd REAL NOT NULL DEFAULT 0,
			metering_supported INTEGER NOT NULL DEFAULT 0, metering_usage_json TEXT NOT NULL DEFAULT '[]',
			duration_ms INTEGER NOT NULL DEFAULT 0, context_usage REAL NOT NULL DEFAULT 0, recorded_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_guild_time ON usage_records(guild_id, occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_guild_user_time ON usage_records(guild_id, user_id, occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_guild_status_time ON usage_records(guild_id, status, occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_guild_source_time ON usage_records(guild_id, source, occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_invocation ON usage_records(invocation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_message ON usage_records(message_id)`,
		`CREATE TABLE IF NOT EXISTS usage_imports (source_file TEXT PRIMARY KEY, file_size INTEGER NOT NULL,
			file_mtime_ns INTEGER NOT NULL, checksum TEXT NOT NULL, rows_seen INTEGER NOT NULL,
			rows_imported INTEGER NOT NULL, status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL, completed_at TEXT)`}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			s.db = nil
			return err
		}
	}
	if _, err := db.Exec(`PRAGMA user_version=1`); err != nil {
		_ = db.Close()
		s.db = nil
		return err
	}
	return nil
}

func (s *UsageStore) InitError() error {
	if s == nil {
		return errors.New("usage store not configured")
	}
	return s.initErr
}
func (s *UsageStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *UsageStore) Health() UsageHealth {
	if s == nil || s.db == nil {
		return UsageHealth{Error: "not configured"}
	}
	var h UsageHealth
	var check string
	if err := s.db.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil {
		h.Error = err.Error()
		return h
	}
	if check != "ok" {
		h.Error = check
		return h
	}
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&h.SchemaVersion); err != nil {
		h.Error = err.Error()
		return h
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_records`).Scan(&h.Records); err != nil {
		h.Error = err.Error()
		return h
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_imports WHERE status='failed'`).Scan(&h.FailedImports); err != nil {
		h.Error = err.Error()
		return h
	}
	h.Healthy = h.FailedImports == 0
	if !h.Healthy {
		h.Error = "legacy import failed"
	}
	return h
}

func (s *UsageStore) importLegacyFiles() error {
	if s == nil || s.db == nil || strings.TrimSpace(s.dir) == "" {
		return nil
	}
	paths, err := filepath.Glob(filepath.Join(s.dir, "????-??.jsonl"))
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := s.importLegacyFile(path); err != nil {
			return err
		}
	}
	return nil
}

func (s *UsageStore) importLegacyFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	checksum, err := fileSHA256(path)
	if err != nil {
		return err
	}
	var existingChecksum, existingStatus string
	err = s.db.QueryRow(`SELECT checksum,status FROM usage_imports WHERE source_file=?`, filepath.Base(path)).Scan(&existingChecksum, &existingStatus)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existingStatus == "completed" && existingChecksum == checksum {
		return s.archiveLegacyFile(path)
	}
	now := time.Now().In(s.Location()).Format(usageTimeFormat)
	if _, err = s.db.Exec(`INSERT INTO usage_imports(source_file,file_size,file_mtime_ns,checksum,rows_seen,rows_imported,status,error,started_at) VALUES(?,?,?,?,0,0,'running','',?) ON CONFLICT(source_file) DO UPDATE SET file_size=excluded.file_size,file_mtime_ns=excluded.file_mtime_ns,checksum=excluded.checksum,rows_seen=0,rows_imported=0,status='running',error='',started_at=excluded.started_at,completed_at=NULL`, filepath.Base(path), info.Size(), info.ModTime().UnixNano(), checksum, now); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	line, imported := 0, 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var rec UsageRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			_ = tx.Rollback()
			s.markImportFailed(path, line, err)
			return fmt.Errorf("import usage %s line %d: %w", filepath.Base(path), line, err)
		}
		rec.UsageID = fmt.Sprintf("legacy:%s:%d", checksum, line)
		if err := normalizeUsageRecord(&rec, s.Location()); err != nil {
			_ = tx.Rollback()
			s.markImportFailed(path, line, err)
			return fmt.Errorf("import usage %s line %d: %w", filepath.Base(path), line, err)
		}
		metering, err := json.Marshal(rec.MeteringUsage)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		res, err := tx.Exec(`INSERT INTO usage_records (usage_id,occurred_at,guild_id,channel_id,thread_id,user_id,username,message_id,interaction_id,invocation_id,model,engine,source,status,credits,cost_usd,metering_supported,metering_usage_json,duration_ms,context_usage,recorded_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(usage_id) DO NOTHING`, rec.UsageID, rec.Timestamp, rec.GuildID, rec.ChannelID, rec.ThreadID, rec.UserID, rec.Username, rec.MessageID, rec.InteractionID, rec.InvocationID, rec.Model, rec.Engine, rec.Source, rec.Status, rec.Credits, rec.CostUSD, rec.MeteringSupported, string(metering), rec.DurationMs, rec.ContextUsage, now)
		if err != nil {
			_ = tx.Rollback()
			s.markImportFailed(path, line, err)
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			imported++
		}
	}
	if err := scanner.Err(); err != nil {
		_ = tx.Rollback()
		s.markImportFailed(path, line, err)
		return err
	}
	if err := tx.Commit(); err != nil {
		s.markImportFailed(path, line, err)
		return err
	}
	_, err = s.db.Exec(`UPDATE usage_imports SET rows_seen=?,rows_imported=?,status='completed',error='',completed_at=? WHERE source_file=?`, line, imported, time.Now().In(s.Location()).Format(usageTimeFormat), filepath.Base(path))
	if err != nil {
		return err
	}
	return s.archiveLegacyFile(path)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (s *UsageStore) markImportFailed(path string, line int, cause error) {
	_, _ = s.db.Exec(`UPDATE usage_imports SET rows_seen=?,status='failed',error=? WHERE source_file=?`, line, cause.Error(), filepath.Base(path))
}
func (s *UsageStore) archiveLegacyFile(path string) error {
	archive := filepath.Join(s.dir, "archive")
	if err := os.MkdirAll(archive, 0755); err != nil {
		return err
	}
	dst := filepath.Join(archive, filepath.Base(path))
	if _, err := os.Stat(dst); err == nil {
		srcSum, readErr := fileSHA256(path)
		if readErr != nil {
			return readErr
		}
		dstSum, readErr := fileSHA256(dst)
		if readErr != nil {
			return readErr
		}
		if srcSum != dstSum {
			ext := filepath.Ext(path)
			base := strings.TrimSuffix(filepath.Base(path), ext)
			dst = filepath.Join(archive, fmt.Sprintf("%s.%s%s", base, srcSum[:12], ext))
			if _, err := os.Stat(dst); err == nil {
				existingSum, readErr := fileSHA256(dst)
				if readErr != nil {
					return readErr
				}
				if existingSum != srcSum {
					return fmt.Errorf("usage archive checksum collision for %s", filepath.Base(path))
				}
				return os.Remove(path)
			} else if !os.IsNotExist(err) {
				return err
			}
			return os.Rename(path, dst)
		}
		return os.Remove(path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(path, dst)
}

func normalizeUsageRecord(record *UsageRecord, loc *time.Location) error {
	if record.Timestamp == "" {
		return errors.New("missing usage timestamp")
	}
	t, err := parseUsageTime(record.Timestamp, loc)
	if err != nil {
		return err
	}
	record.Timestamp = formatUsageDBTime(t)
	if len(record.MeteringUsage) > 0 {
		record.Credits, record.MeteringSupported = creditsFromMetering(record.MeteringUsage)
		if cost, supported := costFromMetering(record.MeteringUsage); supported {
			record.CostUSD = cost
			record.MeteringSupported = true
		}
	} else if record.Credits != 0 || record.CostUSD != 0 {
		record.MeteringSupported = true
	}
	if record.Source == "" {
		record.Source = "message"
	}
	if record.Status == "" {
		record.Status = "success"
	}
	return nil
}

func resolveUsageLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
}

func (s *UsageStore) Location() *time.Location {
	if s == nil || s.location == nil {
		return time.Local
	}
	return s.location
}

func (s *UsageStore) Append(record UsageRecord) error {
	if s == nil || s.db == nil {
		return errors.New("usage store not configured")
	}
	if s.initErr != nil {
		return s.initErr
	}
	now := time.Now().In(s.Location())
	if record.Timestamp == "" {
		record.Timestamp = now.Format(usageTimeFormat)
	}
	if len(record.MeteringUsage) > 0 {
		record.Credits, record.MeteringSupported = creditsFromMetering(record.MeteringUsage)
		record.CostUSD = 0
		if cost, supported := costFromMetering(record.MeteringUsage); supported {
			record.CostUSD = cost
			record.MeteringSupported = true
		}
	} else if record.Credits != 0 || record.CostUSD != 0 {
		record.MeteringSupported = true
	}
	if record.Source == "" {
		record.Source = "message"
	}
	if record.Status == "" {
		record.Status = "success"
	}

	t, err := parseUsageTime(record.Timestamp, s.Location())
	if err != nil {
		t = now
		record.Timestamp = t.Format(usageTimeFormat)
	}
	record.Timestamp = formatUsageDBTime(t)
	if record.UsageID == "" {
		record.UsageID = uuid.NewString()
	}
	meteringJSON, err := json.Marshal(record.MeteringUsage)
	if err != nil {
		return err
	}
	s.mu.Lock()
	_, err = s.db.Exec(`INSERT INTO usage_records (usage_id,occurred_at,guild_id,channel_id,thread_id,user_id,username,message_id,interaction_id,invocation_id,model,engine,source,status,credits,cost_usd,metering_supported,metering_usage_json,duration_ms,context_usage,recorded_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(usage_id) DO NOTHING`, record.UsageID, record.Timestamp, record.GuildID, record.ChannelID, record.ThreadID, record.UserID, record.Username, record.MessageID, record.InteractionID, record.InvocationID, record.Model, record.Engine, record.Source, record.Status, record.Credits, record.CostUSD, record.MeteringSupported, string(meteringJSON), record.DurationMs, record.ContextUsage, now.Format(usageTimeFormat))
	prune := false
	if err == nil && s.retentionMonths > 0 {
		month := now.Format("2006-01")
		if s.lastPruneMonth != month {
			s.lastPruneMonth = month
			prune = true
		}
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if prune {
		return s.PruneExpired(now)
	}
	return nil
}

func creditsFromMetering(items []acp.MeteringItem) (float64, bool) {
	var credits float64
	supported := false
	for _, item := range items {
		unit := strings.ToLower(strings.TrimSpace(item.Unit))
		if unit == "credit" || unit == "credits" {
			credits += item.Value
			supported = true
		}
	}
	return credits, supported
}

// costFromMetering sums USD-denominated metering entries (omp engine).
func costFromMetering(items []acp.MeteringItem) (float64, bool) {
	var cost float64
	supported := false
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Unit), "USD") {
			cost += item.Value
			supported = true
		}
	}
	return cost, supported
}

func (s *UsageStore) Report(guildID, channelID, userID string, limit int, now time.Time) (UsageReport, error) {
	if s == nil {
		return UsageReport{}, errors.New("usage store not configured")
	}
	loc := s.Location()
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(loc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	weekStart := dayStart.AddDate(0, 0, -int((int(dayStart.Weekday())+6)%7))
	readStart := earliestTime(dayStart, weekStart, monthStart)

	rows := map[string]*UsageReportRow{}
	records, err := s.readRange(readStart, now)
	if err != nil {
		return UsageReport{}, err
	}
	scoped := make([]usageRecordWithTime, 0, len(records))
	for _, rec := range records {
		if guildID != "" {
			if rec.GuildID != guildID {
				continue
			}
		} else if channelID != "" && rec.ChannelID != channelID {
			continue
		}
		t, err := parseUsageTime(rec.Timestamp, loc)
		if err != nil || t.After(now) {
			continue
		}
		scoped = append(scoped, usageRecordWithTime{record: rec, timestamp: t})
	}
	usernameAliases := uniqueUsernameAliases(scoped)
	var total UsageReportTotals
	for _, item := range scoped {
		rec := item.record
		resolvedUserID := resolveUsageUserID(rec, usernameAliases)
		if userID != "" && resolvedUserID != userID {
			continue
		}
		credits := rec.Credits
		meteringSupported := rec.MeteringSupported
		if len(rec.MeteringUsage) > 0 {
			credits, meteringSupported = creditsFromMetering(rec.MeteringUsage)
		}
		cost := rec.CostUSD
		costSupported := false
		if len(rec.MeteringUsage) > 0 {
			cost, costSupported = costFromMetering(rec.MeteringUsage)
		}
		if costSupported {
			meteringSupported = true
		}
		row := rows[resolvedUserID]
		if row == nil {
			row = &UsageReportRow{UserID: resolvedUserID}
			rows[resolvedUserID] = row
		}
		if rec.Username != "" {
			row.Username = rec.Username
		}
		if !item.timestamp.Before(monthStart) {
			row.MonthCredits += credits
			row.MonthCostUSD += cost
			row.MonthTurns++
			reportAddMonth(&total, credits, cost)
			if meteringSupported {
				row.MeteredMonthTurns++
			}
		}
		if !item.timestamp.Before(weekStart) {
			row.WeekCredits += credits
			row.WeekCostUSD += cost
			row.WeekTurns++
			reportAddWeek(&total, credits, cost)
			if meteringSupported {
				row.MeteredWeekTurns++
			}
		}
		if !item.timestamp.Before(dayStart) {
			row.DayCredits += credits
			row.DayCostUSD += cost
			row.DayTurns++
			reportAddDay(&total, credits, cost)
			if meteringSupported {
				row.MeteredDayTurns++
			}
		}
	}

	out := make([]UsageReportRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MonthCredits != out[j].MonthCredits {
			return out[i].MonthCredits > out[j].MonthCredits
		}
		if out[i].MonthCostUSD != out[j].MonthCostUSD {
			return out[i].MonthCostUSD > out[j].MonthCostUSD
		}
		if out[i].WeekCredits != out[j].WeekCredits {
			return out[i].WeekCredits > out[j].WeekCredits
		}
		if out[i].WeekCostUSD != out[j].WeekCostUSD {
			return out[i].WeekCostUSD > out[j].WeekCostUSD
		}
		if out[i].DayCredits != out[j].DayCredits {
			return out[i].DayCredits > out[j].DayCredits
		}
		if out[i].DayCostUSD != out[j].DayCostUSD {
			return out[i].DayCostUSD > out[j].DayCostUSD
		}
		return out[i].UserID < out[j].UserID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return UsageReport{
		GeneratedAt: now,
		Location:    loc,
		DayStart:    dayStart,
		WeekStart:   weekStart,
		MonthStart:  monthStart,
		Rows:        out,
		Totals:      total,
	}, nil
}

func (s *UsageStore) QueryHistory(opts UsageHistoryOptions) (UsageHistoryPage, error) {
	if s == nil || s.db == nil {
		return UsageHistoryPage{}, errors.New("usage store not configured")
	}
	if opts.Limit <= 0 || opts.Limit > 50 {
		opts.Limit = 12
	}
	where := []string{"guild_id = ?", "user_id = ?", "occurred_at >= ?", "occurred_at <= ?"}
	args := []any{opts.GuildID, opts.UserID, formatUsageDBTime(opts.From), formatUsageDBTime(opts.To)}
	if opts.Status != "" && opts.Status != "all" {
		where = append(where, "status = ?")
		args = append(args, opts.Status)
	}
	if opts.Source != "" && opts.Source != "all" {
		if opts.Source == "command" {
			where = append(where, "source LIKE 'command:%'")
		} else {
			where = append(where, "source = ?")
			args = append(args, opts.Source)
		}
	}
	if opts.BeforeTime != "" {
		where = append(where, "(occurred_at < ? OR (occurred_at = ? AND usage_id < ?))")
		args = append(args, opts.BeforeTime, opts.BeforeTime, opts.BeforeID)
	}
	args = append(args, opts.Limit+1)
	rows, err := s.db.Query(`SELECT usage_id,occurred_at,guild_id,channel_id,thread_id,user_id,username,message_id,interaction_id,invocation_id,model,engine,source,status,credits,cost_usd,metering_supported,metering_usage_json,duration_ms,context_usage FROM usage_records WHERE `+strings.Join(where, " AND ")+` ORDER BY occurred_at DESC, usage_id DESC LIMIT ?`, args...)
	if err != nil {
		return UsageHistoryPage{}, err
	}
	defer rows.Close()
	var out []UsageRecord
	for rows.Next() {
		var rec UsageRecord
		var metering string
		if err := rows.Scan(&rec.UsageID, &rec.Timestamp, &rec.GuildID, &rec.ChannelID, &rec.ThreadID, &rec.UserID, &rec.Username, &rec.MessageID, &rec.InteractionID, &rec.InvocationID, &rec.Model, &rec.Engine, &rec.Source, &rec.Status, &rec.Credits, &rec.CostUSD, &rec.MeteringSupported, &metering, &rec.DurationMs, &rec.ContextUsage); err != nil {
			return UsageHistoryPage{}, err
		}
		_ = json.Unmarshal([]byte(metering), &rec.MeteringUsage)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return UsageHistoryPage{}, err
	}
	page := UsageHistoryPage{}
	if len(out) > opts.Limit {
		last := out[opts.Limit-1]
		page.NextTime = last.Timestamp
		page.NextID = last.UsageID
		out = out[:opts.Limit]
	}
	page.Records = out
	return page, nil
}

func reportAddDay(total *UsageReportTotals, credits, cost float64) {
	total.DayCredits += credits
	total.DayCostUSD += cost
	total.DayTurns++
}

func reportAddWeek(total *UsageReportTotals, credits, cost float64) {
	total.WeekCredits += credits
	total.WeekCostUSD += cost
	total.WeekTurns++
}

func reportAddMonth(total *UsageReportTotals, credits, cost float64) {
	total.MonthCredits += credits
	total.MonthCostUSD += cost
	total.MonthTurns++
}

type usageRecordWithTime struct {
	record    UsageRecord
	timestamp time.Time
}

func uniqueUsernameAliases(records []usageRecordWithTime) map[string]string {
	aliases := make(map[string]string)
	ambiguous := make(map[string]bool)
	for _, item := range records {
		rec := item.record
		username := strings.TrimSpace(rec.Username)
		userID := strings.TrimSpace(rec.UserID)
		if username == "" || userID == "" {
			continue
		}
		if existing, ok := aliases[username]; ok && existing != userID {
			ambiguous[username] = true
			continue
		}
		aliases[username] = userID
	}
	for username := range ambiguous {
		delete(aliases, username)
	}
	return aliases
}

func resolveUsageUserID(rec UsageRecord, usernameAliases map[string]string) string {
	userID := strings.TrimSpace(rec.UserID)
	if userID != "" {
		return userID
	}
	username := strings.TrimSpace(rec.Username)
	if username == "" {
		return ""
	}
	return usernameAliases[username]
}

func (s *UsageStore) readRange(start, end time.Time) ([]UsageRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("usage store not configured")
	}
	var records []UsageRecord
	rows, err := s.db.Query(`SELECT usage_id,occurred_at,guild_id,channel_id,thread_id,user_id,username,message_id,interaction_id,invocation_id,model,engine,source,status,credits,cost_usd,metering_supported,metering_usage_json,duration_ms,context_usage FROM usage_records WHERE occurred_at >= ? AND occurred_at <= ? ORDER BY occurred_at`, formatUsageDBTime(start), formatUsageDBTime(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rec UsageRecord
		var metering string
		if err := rows.Scan(&rec.UsageID, &rec.Timestamp, &rec.GuildID, &rec.ChannelID, &rec.ThreadID, &rec.UserID, &rec.Username, &rec.MessageID, &rec.InteractionID, &rec.InvocationID, &rec.Model, &rec.Engine, &rec.Source, &rec.Status, &rec.Credits, &rec.CostUSD, &rec.MeteringSupported, &metering, &rec.DurationMs, &rec.ContextUsage); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(metering), &rec.MeteringUsage)
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *UsageStore) PruneExpired(now time.Time) error {
	if s == nil || s.retentionMonths <= 0 {
		return nil
	}
	loc := s.Location()
	now = now.In(loc)
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, -s.retentionMonths+1, 0)
	if s.db == nil {
		return errors.New("usage store not configured")
	}
	_, err := s.db.Exec(`DELETE FROM usage_records WHERE occurred_at < ?`, formatUsageDBTime(cutoff))
	return err
}

func parseUsageTime(value string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.In(loc), nil
	}
	return time.ParseInLocation(usageTimeFormat, value, loc)
}

func earliestTime(times ...time.Time) time.Time {
	if len(times) == 0 {
		return time.Time{}
	}
	earliest := times[0]
	for _, t := range times[1:] {
		if t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}
