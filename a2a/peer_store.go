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
	AgentID               AgentID
	Card                  AgentCard
	CardJSON              string
	ExtendedCard          ExtendedAgentCard
	ExtendedCardJSON      string
	FirstSeenAt           time.Time
	LastSeenAt            time.Time
	ExpiresAt             time.Time
	Trusted               bool
	Stale                 bool
	InstanceID            string
	Status                string
	SignatureStatus       string
	CredentialIssuer      string
	CredentialFingerprint string
	PublicKeyFingerprint  string
	ProtocolBinding       string
	ProtocolVersion       string
}

type PeerTrustDisplay struct {
	AgentID               AgentID
	Name                  string
	Description           string
	SkillIDs              []string
	Stale                 bool
	Online                bool
	Trusted               bool
	LastSeenAt            time.Time
	ExpiresAt             time.Time
	SignatureStatus       string
	CredentialIssuer      string
	CredentialFingerprint string
	PublicKeyFingerprint  string
	SupportedBinding      string
	ProtocolVersion       string
	Compatibility         PeerCompatibility
	Runtime               string
	ChannelRef            string
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
		extended_card_json TEXT NOT NULL DEFAULT '{}',
		first_seen_at TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		expires_at TEXT,
		trusted INTEGER NOT NULL DEFAULT 0,
		stale INTEGER NOT NULL DEFAULT 0,
		instance_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'offline',
		signature_status TEXT NOT NULL DEFAULT '',
		credential_issuer TEXT NOT NULL DEFAULT '',
		credential_fingerprint TEXT NOT NULL DEFAULT '',
		public_key_fingerprint TEXT NOT NULL DEFAULT '',
		protocol_binding TEXT NOT NULL DEFAULT '',
		protocol_version TEXT NOT NULL DEFAULT ''
	)`,
		`ALTER TABLE a2a_peers ADD COLUMN extended_card_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE a2a_peers ADD COLUMN stale INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE a2a_peers ADD COLUMN instance_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE a2a_peers ADD COLUMN status TEXT NOT NULL DEFAULT 'offline'`,
		`ALTER TABLE a2a_peers ADD COLUMN signature_status TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE a2a_peers ADD COLUMN credential_issuer TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE a2a_peers ADD COLUMN credential_fingerprint TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE a2a_peers ADD COLUMN public_key_fingerprint TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE a2a_peers ADD COLUMN protocol_binding TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE a2a_peers ADD COLUMN protocol_version TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_peers_last_seen ON a2a_peers(last_seen_at)`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_peers_stale ON a2a_peers(stale, expires_at)`,
	}
}

func (s *SQLitePeerStore) UpsertCard(ctx context.Context, agent AgentID, card AgentCard, trusted bool, expiresAt time.Time) (PeerRow, error) {
	return s.UpsertExtendedCard(ctx, agent, card, ExtendedAgentCard{}, trusted, "", "online", expiresAt)
}

func (s *SQLitePeerStore) UpsertExtendedCard(ctx context.Context, agent AgentID, card AgentCard, ext ExtendedAgentCard, trusted bool, instanceID string, status string, expiresAt time.Time) (PeerRow, error) {
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
	if err := ValidatePeerCard(card); err != nil {
		return PeerRow{}, err
	}
	ext, err := BuildExtendedAgentCard(card, ext)
	if err != nil {
		return PeerRow{}, err
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return PeerRow{}, err
	}
	extendedRaw, err := json.Marshal(ext)
	if err != nil {
		return PeerRow{}, err
	}
	if strings.TrimSpace(status) == "" {
		status = "online"
	}
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(90 * time.Second)
	}
	if expiresAt.Before(time.Now().UTC()) {
		status = "stale"
	}
	iface := primaryInterface(card)
	now := time.Now().UTC().Format(sqliteTimeFormat)
	_, err = s.db.ExecContext(ctx, `INSERT INTO a2a_peers(agent_id, card_json, extended_card_json, first_seen_at, last_seen_at, expires_at, trusted, stale, instance_id, status, signature_status, credential_issuer, credential_fingerprint, public_key_fingerprint, protocol_binding, protocol_version)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(agent_id) DO UPDATE SET card_json=excluded.card_json, extended_card_json=excluded.extended_card_json, last_seen_at=excluded.last_seen_at, expires_at=excluded.expires_at, trusted=excluded.trusted, stale=excluded.stale, instance_id=excluded.instance_id, status=excluded.status, signature_status=excluded.signature_status, credential_issuer=excluded.credential_issuer, credential_fingerprint=excluded.credential_fingerprint, public_key_fingerprint=excluded.public_key_fingerprint, protocol_binding=excluded.protocol_binding, protocol_version=excluded.protocol_version`,
		agent, string(raw), string(extendedRaw), now, now, nullTime(expiresAt), boolInt(trusted), boolInt(status == "stale"), sanitizePublicText(instanceID), sanitizePublicText(status), ext.SignatureStatus, ext.CredentialIssuer, ext.CredentialFingerprint, ext.PublicKeyFingerprint, iface.ProtocolBinding, iface.ProtocolVersion)
	if err != nil {
		return PeerRow{}, err
	}
	return s.Get(ctx, agent)
}

func (s *SQLitePeerStore) Get(ctx context.Context, agent AgentID) (PeerRow, error) {
	return scanPeer(s.db.QueryRowContext(ctx, `SELECT agent_id, card_json, extended_card_json, first_seen_at, last_seen_at, expires_at, trusted, stale, instance_id, status, signature_status, credential_issuer, credential_fingerprint, public_key_fingerprint, protocol_binding, protocol_version FROM a2a_peers WHERE agent_id=?`, agent))
}

func (s *SQLitePeerStore) ListPeers(ctx context.Context) ([]PeerRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT agent_id, card_json, extended_card_json, first_seen_at, last_seen_at, expires_at, trusted, stale, instance_id, status, signature_status, credential_issuer, credential_fingerprint, public_key_fingerprint, protocol_binding, protocol_version FROM a2a_peers ORDER BY agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var peers []PeerRow
	for rows.Next() {
		row, err := scanPeerRows(rows)
		if err != nil {
			return nil, err
		}
		peers = append(peers, row)
	}
	return peers, rows.Err()
}

func (s *SQLitePeerStore) SetTrusted(ctx context.Context, agent AgentID, trusted bool) error {
	if err := ValidateAgentID(agent); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE a2a_peers SET trusted=?, last_seen_at=last_seen_at WHERE agent_id=?`, boolInt(trusted), agent)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLitePeerStore) MarkStale(ctx context.Context, agent AgentID) error {
	_, err := s.db.ExecContext(ctx, `UPDATE a2a_peers SET stale=1, status='stale', expires_at=?, last_seen_at=last_seen_at WHERE agent_id=?`, time.Now().UTC().Format(sqliteTimeFormat), agent)
	return err
}

func (s *SQLitePeerStore) MarkHeartbeat(ctx context.Context, payload HeartbeatPayload, expiresAt time.Time) error {
	if err := ValidateAgentID(payload.AgentID); err != nil {
		return err
	}
	status := sanitizePublicText(payload.Status)
	if status == "" {
		status = "online"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE a2a_peers SET last_seen_at=?, expires_at=?, stale=?, instance_id=?, status=? WHERE agent_id=?`,
		time.Now().UTC().Format(sqliteTimeFormat), nullTime(expiresAt), boolInt(status == "stale"), sanitizePublicText(payload.InstanceID), status, payload.AgentID)
	return err
}

func (s *SQLitePeerStore) TrustDisplay(ctx context.Context, staleAfter time.Duration) ([]PeerTrustDisplay, error) {
	return s.TrustSummary(ctx, staleAfter)
}

func (s *SQLitePeerStore) TrustSummary(ctx context.Context, staleAfter time.Duration) ([]PeerTrustDisplay, error) {
	peers, err := s.ListPeers(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]PeerTrustDisplay, 0, len(peers))
	for _, peer := range peers {
		display := PeerTrustDisplay{
			AgentID:               peer.AgentID,
			Name:                  peer.Card.Name,
			Description:           peer.Card.Description,
			Trusted:               peer.Trusted,
			LastSeenAt:            peer.LastSeenAt,
			ExpiresAt:             peer.ExpiresAt,
			SignatureStatus:       peer.SignatureStatus,
			CredentialIssuer:      peer.CredentialIssuer,
			CredentialFingerprint: peer.CredentialFingerprint,
			PublicKeyFingerprint:  peer.PublicKeyFingerprint,
			SupportedBinding:      peer.ProtocolBinding,
			ProtocolVersion:       peer.ProtocolVersion,
			Compatibility:         CheckVersionCompatibility(peer.Card),
			Runtime:               peer.ExtendedCard.Runtime,
			ChannelRef:            peer.ExtendedCard.ChannelRef,
		}
		for _, skill := range peer.Card.Skills {
			display.SkillIDs = append(display.SkillIDs, skill.ID)
		}
		display.Stale = peer.Stale || peer.Status == "stale" || (staleAfter > 0 && now.Sub(peer.LastSeenAt) > staleAfter) || (!peer.ExpiresAt.IsZero() && now.After(peer.ExpiresAt))
		display.Online = !display.Stale && peer.Status != "offline"
		out = append(out, display)
	}
	return out, nil
}

func scanPeer(row *sql.Row) (PeerRow, error) {
	var p PeerRow
	var first, last string
	var expires sql.NullString
	var trusted, stale int
	err := row.Scan(&p.AgentID, &p.CardJSON, &p.ExtendedCardJSON, &first, &last, &expires, &trusted, &stale, &p.InstanceID, &p.Status, &p.SignatureStatus, &p.CredentialIssuer, &p.CredentialFingerprint, &p.PublicKeyFingerprint, &p.ProtocolBinding, &p.ProtocolVersion)
	if err != nil {
		return PeerRow{}, err
	}
	return finishPeerScan(p, first, last, expires, trusted, stale), nil
}

type peerRowsScanner interface {
	Scan(...any) error
}

func scanPeerRows(rows peerRowsScanner) (PeerRow, error) {
	var p PeerRow
	var first, last string
	var expires sql.NullString
	var trusted, stale int
	err := rows.Scan(&p.AgentID, &p.CardJSON, &p.ExtendedCardJSON, &first, &last, &expires, &trusted, &stale, &p.InstanceID, &p.Status, &p.SignatureStatus, &p.CredentialIssuer, &p.CredentialFingerprint, &p.PublicKeyFingerprint, &p.ProtocolBinding, &p.ProtocolVersion)
	if err != nil {
		return PeerRow{}, err
	}
	return finishPeerScan(p, first, last, expires, trusted, stale), nil
}

func finishPeerScan(p PeerRow, first, last string, expires sql.NullString, trusted int, stale int) PeerRow {
	_ = json.Unmarshal([]byte(p.CardJSON), &p.Card)
	_ = json.Unmarshal([]byte(p.ExtendedCardJSON), &p.ExtendedCard)
	p.FirstSeenAt, _ = time.Parse(sqliteTimeFormat, first)
	p.LastSeenAt, _ = time.Parse(sqliteTimeFormat, last)
	if expires.Valid {
		p.ExpiresAt, _ = time.Parse(sqliteTimeFormat, expires.String)
	}
	p.Trusted = intBool(trusted)
	p.Stale = intBool(stale)
	return p
}

func primaryInterface(card AgentCard) A2AInterface {
	if len(card.SupportedInterfaces) == 0 {
		return A2AInterface{}
	}
	return card.SupportedInterfaces[0]
}

var absolutePathPattern = regexp.MustCompile(`(/Users/|/var/|/etc/|/data/|[A-Za-z]:\\)[^\s]+`)
var secretWordPattern = regexp.MustCompile(`(?i)(token|secret|password|api[_-]?key|mcp\.json|discord[_-]?token|credential)`)
var internalURLPattern = regexp.MustCompile(`(?i)(https?|wss?)://(localhost|127\.0\.0\.1|10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[01])\.)[^\s]+`)
var discordIDPattern = regexp.MustCompile(`\b\d{17,20}\b`)

func sanitizePublicText(s string) string {
	s = absolutePathPattern.ReplaceAllString(s, "[REDACTED]")
	s = internalURLPattern.ReplaceAllString(s, "[REDACTED]")
	s = secretWordPattern.ReplaceAllString(s, "[REDACTED]")
	s = discordIDPattern.ReplaceAllString(s, "[REDACTED]")
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
