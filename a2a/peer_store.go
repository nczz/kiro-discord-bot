package a2a

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type AgentCard struct {
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	Version             string          `json:"version"`
	SupportedInterfaces []A2AInterface  `json:"supportedInterfaces"`
	Capabilities        map[string]bool `json:"capabilities"`
	DefaultInputModes   []string        `json:"defaultInputModes"`
	DefaultOutputModes  []string        `json:"defaultOutputModes"`
	Skills              []AgentSkill    `json:"skills"`
}

type A2AInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion"`
}
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

type PeerRow struct {
	AgentID     AgentID
	Card        AgentCard
	CardJSON    string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
	ExpiresAt   time.Time
	Trusted     bool
}
type PeerTrustDisplay struct {
	AgentID     AgentID
	Name        string
	Description string
	SkillIDs    []string
	Stale       bool
	Trusted     bool
	LastSeenAt  time.Time
}

type SQLitePeerStore struct{ db *sql.DB }

func OpenPeerStore(dataDir string) (*SQLitePeerStore, error) {
	db, err := openA2ASQLite(dataDir, "peers.sqlite", peerStoreMigrations())
	if err != nil {
		return nil, err
	}
	return &SQLitePeerStore{db: db}, nil
}
func (s *SQLitePeerStore) Close() error { return closeSQL(s.db) }

func peerStoreMigrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS a2a_peers (
		agent_id TEXT PRIMARY KEY,
		card_json TEXT NOT NULL,
		first_seen_at TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		expires_at TEXT,
		trusted INTEGER NOT NULL DEFAULT 0
	)`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_peers_last_seen ON a2a_peers(last_seen_at)`,
	}
}

func (s *SQLitePeerStore) UpsertCard(ctx context.Context, agent AgentID, card AgentCard, trusted bool, expiresAt time.Time) (PeerRow, error) {
	if err := ValidateAgentID(agent); err != nil {
		return PeerRow{}, err
	}
	card = SanitizeAgentCard(card)
	if strings.TrimSpace(card.Name) == "" {
		card.Name = string(agent)
	}
	if card.Name != string(agent) {
		return PeerRow{}, fmt.Errorf("card name must match agent id")
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return PeerRow{}, err
	}
	now := time.Now().UTC().Format(sqliteTimeFormat)
	_, err = s.db.ExecContext(ctx, `INSERT INTO a2a_peers(agent_id, card_json, first_seen_at, last_seen_at, expires_at, trusted) VALUES(?,?,?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET card_json=excluded.card_json, last_seen_at=excluded.last_seen_at, expires_at=excluded.expires_at, trusted=excluded.trusted`, agent, string(raw), now, now, nullTime(expiresAt), boolInt(trusted))
	if err != nil {
		return PeerRow{}, err
	}
	return s.Get(ctx, agent)
}

func (s *SQLitePeerStore) Get(ctx context.Context, agent AgentID) (PeerRow, error) {
	var row PeerRow
	var raw, first, last, expires string
	var trusted int
	var expiresNull sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT agent_id, card_json, first_seen_at, last_seen_at, expires_at, trusted FROM a2a_peers WHERE agent_id=?`, agent).Scan(&row.AgentID, &raw, &first, &last, &expiresNull, &trusted)
	if err != nil {
		return PeerRow{}, err
	}
	row.CardJSON = raw
	_ = json.Unmarshal([]byte(raw), &row.Card)
	row.FirstSeenAt, _ = time.Parse(sqliteTimeFormat, first)
	row.LastSeenAt, _ = time.Parse(sqliteTimeFormat, last)
	if expiresNull.Valid {
		expires = expiresNull.String
		row.ExpiresAt, _ = time.Parse(sqliteTimeFormat, expires)
	}
	row.Trusted = intBool(trusted)
	return row, nil
}

func (s *SQLitePeerStore) TrustDisplay(ctx context.Context, staleAfter time.Duration) ([]PeerTrustDisplay, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT agent_id, card_json, last_seen_at, expires_at, trusted FROM a2a_peers ORDER BY agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PeerTrustDisplay
	now := time.Now().UTC()
	for rows.Next() {
		var agent AgentID
		var raw, last string
		var expires sql.NullString
		var trusted int
		if err := rows.Scan(&agent, &raw, &last, &expires, &trusted); err != nil {
			return nil, err
		}
		var card AgentCard
		_ = json.Unmarshal([]byte(raw), &card)
		seen, _ := time.Parse(sqliteTimeFormat, last)
		d := PeerTrustDisplay{AgentID: agent, Name: card.Name, Description: card.Description, LastSeenAt: seen, Trusted: intBool(trusted)}
		for _, skill := range card.Skills {
			d.SkillIDs = append(d.SkillIDs, skill.ID)
		}
		if staleAfter > 0 && now.Sub(seen) > staleAfter {
			d.Stale = true
		}
		if expires.Valid {
			exp, _ := time.Parse(sqliteTimeFormat, expires.String)
			if !exp.IsZero() && now.After(exp) {
				d.Stale = true
			}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func SanitizeAgentCard(card AgentCard) AgentCard {
	card.Name = sanitizePublicText(card.Name)
	card.Description = sanitizePublicText(card.Description)
	for i := range card.SupportedInterfaces {
		card.SupportedInterfaces[i].URL = sanitizeInterfaceURL(card.SupportedInterfaces[i].URL)
	}
	for i := range card.Skills {
		card.Skills[i].ID = sanitizeSkillID(card.Skills[i].ID)
		card.Skills[i].Name = sanitizePublicText(card.Skills[i].Name)
		card.Skills[i].Description = sanitizePublicText(card.Skills[i].Description)
		card.Skills[i].Examples = nil
	}
	return card
}

var absolutePathPattern = regexp.MustCompile(`(/Users/|/var/|/etc/|/data/|[A-Za-z]:\\)[^\s]+`)
var secretWordPattern = regexp.MustCompile(`(?i)(token|secret|password|api[_-]?key|mcp\.json|discord[_-]?token)`)
var internalURLPattern = regexp.MustCompile(`(?i)https?://(localhost|127\.0\.0\.1|10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[01])\.)[^\s]+`)

func sanitizePublicText(s string) string {
	s = absolutePathPattern.ReplaceAllString(s, "[REDACTED]")
	s = internalURLPattern.ReplaceAllString(s, "[REDACTED]")
	s = secretWordPattern.ReplaceAllString(s, "[REDACTED]")
	return strings.TrimSpace(s)
}
func sanitizeInterfaceURL(s string) string {
	if internalURLPattern.MatchString(s) {
		return ""
	}
	return sanitizePublicText(s)
}
func sanitizeSkillID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "..", "")
	return s
}
