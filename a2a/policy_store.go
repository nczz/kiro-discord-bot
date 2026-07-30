package a2a

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime"
	"regexp"
	"strings"
	"time"
)

type ChannelA2APolicy struct {
	GuildID               string              `json:"guildId"`
	ChannelID             string              `json:"channelId"`
	Enabled               bool                `json:"enabled"`
	ChannelRef            string              `json:"channelRef"`
	AcceptFrom            []string            `json:"acceptFrom"`
	AcceptSkills          []string            `json:"acceptSkills"`
	ExposeSkills          []SkillPolicy       `json:"exposeSkills"`
	DelegateTo            []string            `json:"delegateTo"`
	DelegateSkills        []string            `json:"delegateSkills"`
	DelegateMedia         DelegateMediaPolicy `json:"delegateMedia"`
	MaxConcurrent         int                 `json:"maxConcurrent"`
	ResultVisibility      string              `json:"resultVisibility"`
	DiscordTranscriptMode string              `json:"discordTranscriptMode"`
	ShareDiscordContext   bool                `json:"shareDiscordContext"`
	CoPresentFrom         []string            `json:"coPresentFrom"`
	AutoDelegateEnabled   bool                `json:"autoDelegateEnabled"`
	RemoteToolPolicy      RemoteToolPolicy    `json:"remoteToolPolicy"`
	CreatedAt             time.Time           `json:"createdAt"`
	UpdatedAt             time.Time           `json:"updatedAt"`
	UpdatedBy             string              `json:"updatedBy"`
}

type SkillPolicy struct {
	ID          string   `json:"id"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

type DelegateMediaPolicy struct {
	AllowedMIMETypes []string `json:"allowedMimeTypes,omitempty"`
	MaxBytes         int64    `json:"maxBytes,omitempty"`
	AllowObjectRefs  bool     `json:"allowObjectRefs,omitempty"`
}

type RemoteToolPolicy struct {
	AllowMemoryWrite bool `json:"allow_memory_write"`
}

type SQLitePolicyStore struct {
	db      *sql.DB
	agentID AgentID
}

func OpenPolicyStore(dataDir string, agentID AgentID) (*SQLitePolicyStore, error) {
	if agentID != "" {
		if err := ValidateAgentID(agentID); err != nil {
			return nil, err
		}
	}
	db, err := openA2ASQLite(dataDir, "policy.sqlite", policyStoreMigrations())
	if err != nil {
		return nil, err
	}
	return &SQLitePolicyStore{db: db, agentID: agentID}, nil
}

func (s *SQLitePolicyStore) Close() error { return closeSQL(s.db) }

func policyStoreMigrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS channel_a2a_policy (
		guild_id TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 0,
		channel_ref TEXT NOT NULL,
		accept_from_json TEXT NOT NULL DEFAULT '[]',
		accept_skills_json TEXT NOT NULL DEFAULT '[]',
		expose_skills_json TEXT NOT NULL DEFAULT '[]',
		delegate_to_json TEXT NOT NULL DEFAULT '[]',
		delegate_skills_json TEXT NOT NULL DEFAULT '[]',
		delegate_media_json TEXT NOT NULL DEFAULT '{}',
		max_concurrent INTEGER NOT NULL DEFAULT 0,
		result_visibility TEXT NOT NULL DEFAULT 'proxy',
		discord_transcript_mode TEXT NOT NULL DEFAULT 'delegator',
		share_discord_context INTEGER NOT NULL DEFAULT 0,
		co_present_from_json TEXT NOT NULL DEFAULT '[]',
		auto_delegate_enabled INTEGER NOT NULL DEFAULT 0,
		remote_tool_policy_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		updated_by TEXT NOT NULL,
		PRIMARY KEY (guild_id, channel_id)
	)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_a2a_policy_ref ON channel_a2a_policy(channel_ref) WHERE enabled=1 AND channel_ref <> ''`,
	}
}

func (s *SQLitePolicyStore) Save(ctx context.Context, p ChannelA2APolicy, updatedBy string) error {
	if err := validateChannelA2APolicy(p); err != nil {
		return err
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	p.UpdatedBy = strings.TrimSpace(updatedBy)
	if p.UpdatedBy == "" {
		return fmt.Errorf("updated_by is required")
	}
	acceptFrom, _ := json.Marshal(p.AcceptFrom)
	acceptSkills, _ := json.Marshal(p.AcceptSkills)
	exposeSkills, _ := json.Marshal(p.ExposeSkills)
	delegateTo, _ := json.Marshal(p.DelegateTo)
	delegateSkills, _ := json.Marshal(p.DelegateSkills)
	delegateMedia, _ := json.Marshal(p.DelegateMedia)
	coPresent, _ := json.Marshal(p.CoPresentFrom)
	remote, _ := json.Marshal(p.RemoteToolPolicy)
	_, err := s.db.ExecContext(ctx, `INSERT INTO channel_a2a_policy(guild_id, channel_id, enabled, channel_ref, accept_from_json, accept_skills_json, expose_skills_json, delegate_to_json, delegate_skills_json, delegate_media_json, max_concurrent, result_visibility, discord_transcript_mode, share_discord_context, co_present_from_json, auto_delegate_enabled, remote_tool_policy_json, created_at, updated_at, updated_by)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(guild_id, channel_id) DO UPDATE SET enabled=excluded.enabled, channel_ref=excluded.channel_ref, accept_from_json=excluded.accept_from_json, accept_skills_json=excluded.accept_skills_json, expose_skills_json=excluded.expose_skills_json, delegate_to_json=excluded.delegate_to_json, delegate_skills_json=excluded.delegate_skills_json, delegate_media_json=excluded.delegate_media_json, max_concurrent=excluded.max_concurrent, result_visibility=excluded.result_visibility, discord_transcript_mode=excluded.discord_transcript_mode, share_discord_context=excluded.share_discord_context, co_present_from_json=excluded.co_present_from_json, auto_delegate_enabled=excluded.auto_delegate_enabled, remote_tool_policy_json=excluded.remote_tool_policy_json, updated_at=excluded.updated_at, updated_by=excluded.updated_by`, p.GuildID, p.ChannelID, boolInt(p.Enabled), p.ChannelRef, string(acceptFrom), string(acceptSkills), string(exposeSkills), string(delegateTo), string(delegateSkills), string(delegateMedia), p.MaxConcurrent, p.ResultVisibility, p.DiscordTranscriptMode, boolInt(p.ShareDiscordContext), string(coPresent), boolInt(p.AutoDelegateEnabled), string(remote), p.CreatedAt.Format(sqliteTimeFormat), p.UpdatedAt.Format(sqliteTimeFormat), p.UpdatedBy)
	return err
}

func (s *SQLitePolicyStore) Get(ctx context.Context, guildID, channelID string) (ChannelA2APolicy, error) {
	var p ChannelA2APolicy
	var enabled, share, auto int
	var acceptFrom, acceptSkills, exposeSkills, delegateTo, delegateSkills, delegateMedia, coPresent, remote, created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT guild_id, channel_id, enabled, channel_ref, accept_from_json, accept_skills_json, expose_skills_json, delegate_to_json, delegate_skills_json, delegate_media_json, max_concurrent, result_visibility, discord_transcript_mode, share_discord_context, co_present_from_json, auto_delegate_enabled, remote_tool_policy_json, created_at, updated_at, updated_by FROM channel_a2a_policy WHERE guild_id=? AND channel_id=?`, guildID, channelID).Scan(&p.GuildID, &p.ChannelID, &enabled, &p.ChannelRef, &acceptFrom, &acceptSkills, &exposeSkills, &delegateTo, &delegateSkills, &delegateMedia, &p.MaxConcurrent, &p.ResultVisibility, &p.DiscordTranscriptMode, &share, &coPresent, &auto, &remote, &created, &updated, &p.UpdatedBy)
	if err != nil {
		return ChannelA2APolicy{}, err
	}
	p.Enabled = intBool(enabled)
	p.ShareDiscordContext = intBool(share)
	p.AutoDelegateEnabled = intBool(auto)
	_ = json.Unmarshal([]byte(acceptFrom), &p.AcceptFrom)
	_ = json.Unmarshal([]byte(acceptSkills), &p.AcceptSkills)
	_ = json.Unmarshal([]byte(exposeSkills), &p.ExposeSkills)
	_ = json.Unmarshal([]byte(delegateTo), &p.DelegateTo)
	_ = json.Unmarshal([]byte(delegateSkills), &p.DelegateSkills)
	_ = json.Unmarshal([]byte(delegateMedia), &p.DelegateMedia)
	_ = json.Unmarshal([]byte(coPresent), &p.CoPresentFrom)
	_ = json.Unmarshal([]byte(remote), &p.RemoteToolPolicy)
	p.CreatedAt, _ = time.Parse(sqliteTimeFormat, created)
	p.UpdatedAt, _ = time.Parse(sqliteTimeFormat, updated)
	return p, nil
}

func validateChannelA2APolicy(p ChannelA2APolicy) error {
	if strings.TrimSpace(p.GuildID) == "" || strings.TrimSpace(p.ChannelID) == "" {
		return fmt.Errorf("guild_id and channel_id are required")
	}
	if p.Enabled && strings.TrimSpace(p.ChannelRef) == "" {
		return fmt.Errorf("channel_ref is required when enabled")
	}
	if p.ResultVisibility == "" {
		p.ResultVisibility = "proxy"
	}
	if p.ResultVisibility != "proxy" && p.ResultVisibility != "transparent" {
		return fmt.Errorf("result_visibility must be proxy or transparent")
	}
	if p.DiscordTranscriptMode == "" {
		p.DiscordTranscriptMode = "delegator"
	}
	if p.DiscordTranscriptMode != "delegator" && p.DiscordTranscriptMode != "mirror" && p.DiscordTranscriptMode != "co_present" {
		return fmt.Errorf("discord_transcript_mode invalid")
	}
	if p.ShareDiscordContext && p.DiscordTranscriptMode != "co_present" {
		return fmt.Errorf("share_discord_context requires co_present transcript mode")
	}
	if p.MaxConcurrent < 0 || p.MaxConcurrent > 64 {
		return fmt.Errorf("max_concurrent must be 0..64")
	}
	for _, list := range [][]string{p.AcceptFrom, p.DelegateTo, p.CoPresentFrom} {
		for _, id := range list {
			if id == "*" {
				continue
			}
			if err := ValidateAgentID(AgentID(id)); err != nil {
				return err
			}
		}
	}
	for _, skill := range p.AcceptSkills {
		if !skillSlugPattern.MatchString(skill) {
			return fmt.Errorf("accept skill %q is not a slug", skill)
		}
	}
	for _, skill := range p.DelegateSkills {
		if !skillPattern.MatchString(skill) {
			return fmt.Errorf("delegate skill %q is invalid", skill)
		}
	}
	for _, skill := range p.ExposeSkills {
		if !skillSlugPattern.MatchString(skill.ID) {
			return fmt.Errorf("exposed skill id %q is not subject-safe", skill.ID)
		}
		for _, mode := range append(skill.InputModes, skill.OutputModes...) {
			if _, _, err := mime.ParseMediaType(mode); err != nil {
				return fmt.Errorf("invalid MIME mode %q", mode)
			}
		}
	}
	if p.DelegateMedia.MaxBytes < 0 {
		return fmt.Errorf("delegate media max bytes cannot be negative")
	}
	for _, mt := range p.DelegateMedia.AllowedMIMETypes {
		if _, _, err := mime.ParseMediaType(mt); err != nil {
			return fmt.Errorf("invalid delegate media MIME %q", mt)
		}
	}
	return nil
}

var skillSlugPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var skillPattern = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_-]{0,63}/)?[A-Za-z0-9][A-Za-z0-9_-]{0,63}(\*)?$`)
