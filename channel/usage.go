package channel

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
)

const usageTimeFormat = time.RFC3339

// UsageRecord is an append-only audit entry for one completed agent turn.
type UsageRecord struct {
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

func NewUsageStore(dataDir, timezone string, retentionMonths int) *UsageStore {
	loc := resolveUsageLocation(timezone)
	s := &UsageStore{
		dir:             filepath.Join(dataDir, "usage"),
		location:        loc,
		retentionMonths: retentionMonths,
	}
	if retentionMonths > 0 {
		if err := s.PruneExpired(time.Now().In(loc)); err != nil {
			log.Printf("[usage] prune failed: %v", err)
		}
	}
	return s
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
	if s == nil {
		return nil
	}
	now := time.Now().In(s.Location())
	if record.Timestamp == "" {
		record.Timestamp = now.Format(usageTimeFormat)
	}
	record.Credits, record.MeteringSupported = creditsFromMetering(record.MeteringUsage)
	record.CostUSD, _ = costFromMetering(record.MeteringUsage)
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
	path := filepath.Join(s.dir, t.Format("2006-01")+".jsonl")
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	s.mu.Lock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		s.mu.Unlock()
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	prune := false
	if writeErr == nil && closeErr == nil && s.retentionMonths > 0 {
		month := t.Format("2006-01")
		if s.lastPruneMonth != month {
			s.lastPruneMonth = month
			prune = true
		}
	}
	s.mu.Unlock()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if prune {
		return s.PruneExpired(t)
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
	var records []UsageRecord
	for _, path := range s.monthFiles(start, end) {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			var rec UsageRecord
			if json.Unmarshal(scanner.Bytes(), &rec) == nil {
				records = append(records, rec)
			}
		}
		if err := scanner.Err(); err != nil {
			f.Close()
			return nil, err
		}
		f.Close()
	}
	return records, nil
}

func (s *UsageStore) monthFiles(start, end time.Time) []string {
	loc := s.Location()
	cur := time.Date(start.In(loc).Year(), start.In(loc).Month(), 1, 0, 0, 0, 0, loc)
	last := time.Date(end.In(loc).Year(), end.In(loc).Month(), 1, 0, 0, 0, 0, loc)
	var files []string
	for !cur.After(last) {
		files = append(files, filepath.Join(s.dir, cur.Format("2006-01")+".jsonl"))
		cur = cur.AddDate(0, 1, 0)
	}
	return files
}

func (s *UsageStore) PruneExpired(now time.Time) error {
	if s == nil || s.retentionMonths <= 0 {
		return nil
	}
	loc := s.Location()
	now = now.In(loc)
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, -s.retentionMonths+1, 0)
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		month := strings.TrimSuffix(entry.Name(), ".jsonl")
		t, err := time.ParseInLocation("2006-01", month, loc)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil {
				return fmt.Errorf("prune usage %s: %w", entry.Name(), err)
			}
		}
	}
	return nil
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
