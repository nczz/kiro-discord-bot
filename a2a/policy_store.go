package a2a

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"regexp"
	"strings"
	"time"
)

type ChannelA2APolicy struct {
	GuildID                 string                 `json:"guildId"`
	ChannelID               string                 `json:"channelId"`
	Enabled                 bool                   `json:"enabled"`
	Discoverable            bool                   `json:"discoverable"`
	RuntimeAgentID          string                 `json:"runtimeAgentId,omitempty"`
	BotAgentID              string                 `json:"botAgentId,omitempty"`
	ChannelRef              string                 `json:"channelRef"`
	AcceptFrom              []string               `json:"acceptFrom"`
	AcceptFromRuntimes      []string               `json:"acceptFromRuntimes,omitempty"`
	AcceptSkills            []string               `json:"acceptSkills"`
	ExposeSkills            []SkillPolicy          `json:"exposeSkills"`
	DelegateTo              []string               `json:"delegateTo"`
	DelegateSkills          []string               `json:"delegateSkills"`
	DelegateMedia           DelegateMediaPolicy    `json:"delegateMedia"`
	DelegateTargets         []DelegateTargetPolicy `json:"delegateTargets,omitempty"`
	MaxConcurrent           int                    `json:"maxConcurrent"`
	ResultVisibility        string                 `json:"resultVisibility"`
	DiscordTranscriptMode   string                 `json:"discordTranscriptMode"`
	ShareDiscordContext     bool                   `json:"shareDiscordContext"`
	CoPresentFrom           []string               `json:"coPresentFrom"`
	CoPresentFromRuntimes   []string               `json:"coPresentFromRuntimes,omitempty"`
	CoPresentTargetChannels []string               `json:"coPresentTargetChannels,omitempty"`
	AutoDelegateEnabled     bool                   `json:"autoDelegateEnabled"`
	RemoteToolPolicy        RemoteToolPolicy       `json:"remoteToolPolicy"`
	CreatedAt               time.Time              `json:"createdAt"`
	UpdatedAt               time.Time              `json:"updatedAt"`
	UpdatedBy               string                 `json:"updatedBy"`
}

type SkillPolicy struct {
	ID          string   `json:"id"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

type DelegateTargetPolicy struct {
	RuntimeAgentID string `json:"runtimeAgentId,omitempty"`
	AgentID        string `json:"agentId,omitempty"`
	ChannelRef     string `json:"channelRef,omitempty"`
	SkillID        string `json:"skillId"`
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
		remote_tool_policy_json TEXT NOT NULL DEFAULT '{"allow_memory_write":false}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		updated_by TEXT NOT NULL,
		PRIMARY KEY (guild_id, channel_id)
	)`,
		`ALTER TABLE channel_a2a_policy ADD COLUMN delegate_targets_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE channel_a2a_policy ADD COLUMN discoverable INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE channel_a2a_policy ADD COLUMN runtime_agent_id TEXT`,
		`ALTER TABLE channel_a2a_policy ADD COLUMN bot_agent_id TEXT`,
		`ALTER TABLE channel_a2a_policy ADD COLUMN accept_from_runtimes_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE channel_a2a_policy ADD COLUMN co_present_from_runtimes_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE channel_a2a_policy ADD COLUMN co_present_target_channels_json TEXT NOT NULL DEFAULT '[]'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_a2a_policy_ref ON channel_a2a_policy(channel_ref) WHERE enabled=1 AND channel_ref <> ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_a2a_policy_runtime ON channel_a2a_policy(runtime_agent_id) WHERE runtime_agent_id IS NOT NULL AND runtime_agent_id <> ''`,
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existingRuntime sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT runtime_agent_id FROM channel_a2a_policy WHERE guild_id=? AND channel_id=?`, p.GuildID, p.ChannelID).Scan(&existingRuntime); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	acceptFrom, _ := json.Marshal(p.AcceptFrom)
	acceptFromRuntimes, _ := json.Marshal(p.AcceptFromRuntimes)
	acceptSkills, _ := json.Marshal(p.AcceptSkills)
	exposeSkills, _ := json.Marshal(p.ExposeSkills)
	delegateTo, _ := json.Marshal(p.DelegateTo)
	delegateSkills, _ := json.Marshal(p.DelegateSkills)
	delegateTargets, _ := json.Marshal(p.DelegateTargets)
	delegateMedia, _ := json.Marshal(p.DelegateMedia)
	coPresent, _ := json.Marshal(p.CoPresentFrom)
	coPresentRuntimes, _ := json.Marshal(p.CoPresentFromRuntimes)
	coPresentTargetChannels, _ := json.Marshal(p.CoPresentTargetChannels)
	remote, _ := json.Marshal(p.RemoteToolPolicy)
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_a2a_policy(guild_id, channel_id, enabled, discoverable, runtime_agent_id, bot_agent_id, channel_ref, accept_from_json, accept_from_runtimes_json, accept_skills_json, expose_skills_json, delegate_to_json, delegate_skills_json, delegate_targets_json, delegate_media_json, max_concurrent, result_visibility, discord_transcript_mode, share_discord_context, co_present_from_json, co_present_from_runtimes_json, co_present_target_channels_json, auto_delegate_enabled, remote_tool_policy_json, created_at, updated_at, updated_by)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(guild_id, channel_id) DO UPDATE SET enabled=excluded.enabled, discoverable=excluded.discoverable, runtime_agent_id=excluded.runtime_agent_id, bot_agent_id=excluded.bot_agent_id, channel_ref=excluded.channel_ref, accept_from_json=excluded.accept_from_json, accept_from_runtimes_json=excluded.accept_from_runtimes_json, accept_skills_json=excluded.accept_skills_json, expose_skills_json=excluded.expose_skills_json, delegate_to_json=excluded.delegate_to_json, delegate_skills_json=excluded.delegate_skills_json, delegate_targets_json=excluded.delegate_targets_json, delegate_media_json=excluded.delegate_media_json, max_concurrent=excluded.max_concurrent, result_visibility=excluded.result_visibility, discord_transcript_mode=excluded.discord_transcript_mode, share_discord_context=excluded.share_discord_context, co_present_from_json=excluded.co_present_from_json, co_present_from_runtimes_json=excluded.co_present_from_runtimes_json, co_present_target_channels_json=excluded.co_present_target_channels_json, auto_delegate_enabled=excluded.auto_delegate_enabled, remote_tool_policy_json=excluded.remote_tool_policy_json, updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		p.GuildID, p.ChannelID, boolInt(p.Enabled), boolInt(p.Discoverable), nullEmpty(p.RuntimeAgentID), nullEmpty(p.BotAgentID), p.ChannelRef, string(acceptFrom), string(acceptFromRuntimes), string(acceptSkills), string(exposeSkills), string(delegateTo), string(delegateSkills), string(delegateTargets), string(delegateMedia), p.MaxConcurrent, p.ResultVisibility, p.DiscordTranscriptMode, boolInt(p.ShareDiscordContext), string(coPresent), string(coPresentRuntimes), string(coPresentTargetChannels), boolInt(p.AutoDelegateEnabled), string(remote), p.CreatedAt.Format(sqliteTimeFormat), p.UpdatedAt.Format(sqliteTimeFormat), p.UpdatedBy)
	if err != nil {
		return err
	}
	oldRuntime := strings.TrimSpace(existingRuntime.String)
	newRuntime := strings.TrimSpace(p.RuntimeAgentID)
	if existingRuntime.Valid && oldRuntime != "" && newRuntime != "" && oldRuntime != newRuntime {
		if err := rewriteRuntimeIDReferences(ctx, tx, oldRuntime, newRuntime); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func rewriteRuntimeIDReferences(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, oldRuntime, newRuntime string) error {
	oldJSON, err := json.Marshal(oldRuntime)
	if err != nil {
		return err
	}
	newJSON, err := json.Marshal(newRuntime)
	if err != nil {
		return err
	}
	_, err = execer.ExecContext(ctx, `UPDATE channel_a2a_policy SET
		accept_from_json=replace(accept_from_json, ?, ?),
		accept_from_runtimes_json=replace(accept_from_runtimes_json, ?, ?),
		delegate_to_json=replace(delegate_to_json, ?, ?),
		delegate_targets_json=replace(delegate_targets_json, ?, ?),
		co_present_from_json=replace(co_present_from_json, ?, ?),
		co_present_from_runtimes_json=replace(co_present_from_runtimes_json, ?, ?)`,
		string(oldJSON), string(newJSON),
		string(oldJSON), string(newJSON),
		string(oldJSON), string(newJSON),
		string(oldJSON), string(newJSON),
		string(oldJSON), string(newJSON),
		string(oldJSON), string(newJSON))
	return err
}

func nullEmpty(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

func (s *SQLitePolicyStore) Get(ctx context.Context, guildID, channelID string) (ChannelA2APolicy, error) {
	var p ChannelA2APolicy
	var enabled, discoverable, share, auto int
	var runtimeID, botID sql.NullString
	var acceptFrom, acceptFromRuntimes, acceptSkills, exposeSkills, delegateTo, delegateSkills, delegateTargets, delegateMedia, coPresent, coPresentRuntimes, coPresentTargetChannels, remote, created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT guild_id, channel_id, enabled, discoverable, runtime_agent_id, bot_agent_id, channel_ref, accept_from_json, accept_from_runtimes_json, accept_skills_json, expose_skills_json, delegate_to_json, delegate_skills_json, delegate_targets_json, delegate_media_json, max_concurrent, result_visibility, discord_transcript_mode, share_discord_context, co_present_from_json, co_present_from_runtimes_json, co_present_target_channels_json, auto_delegate_enabled, remote_tool_policy_json, created_at, updated_at, updated_by FROM channel_a2a_policy WHERE guild_id=? AND channel_id=?`, guildID, channelID).Scan(&p.GuildID, &p.ChannelID, &enabled, &discoverable, &runtimeID, &botID, &p.ChannelRef, &acceptFrom, &acceptFromRuntimes, &acceptSkills, &exposeSkills, &delegateTo, &delegateSkills, &delegateTargets, &delegateMedia, &p.MaxConcurrent, &p.ResultVisibility, &p.DiscordTranscriptMode, &share, &coPresent, &coPresentRuntimes, &coPresentTargetChannels, &auto, &remote, &created, &updated, &p.UpdatedBy)
	if err != nil {
		return ChannelA2APolicy{}, err
	}
	p.Enabled = intBool(enabled)
	p.Discoverable = intBool(discoverable)
	p.RuntimeAgentID = strings.TrimSpace(runtimeID.String)
	p.BotAgentID = strings.TrimSpace(botID.String)
	p.ShareDiscordContext = intBool(share)
	p.AutoDelegateEnabled = intBool(auto)
	_ = json.Unmarshal([]byte(acceptFrom), &p.AcceptFrom)
	_ = json.Unmarshal([]byte(acceptFromRuntimes), &p.AcceptFromRuntimes)
	_ = json.Unmarshal([]byte(acceptSkills), &p.AcceptSkills)
	_ = json.Unmarshal([]byte(exposeSkills), &p.ExposeSkills)
	_ = json.Unmarshal([]byte(delegateTo), &p.DelegateTo)
	_ = json.Unmarshal([]byte(delegateSkills), &p.DelegateSkills)
	_ = json.Unmarshal([]byte(delegateTargets), &p.DelegateTargets)
	_ = json.Unmarshal([]byte(delegateMedia), &p.DelegateMedia)
	_ = json.Unmarshal([]byte(coPresent), &p.CoPresentFrom)
	_ = json.Unmarshal([]byte(coPresentRuntimes), &p.CoPresentFromRuntimes)
	_ = json.Unmarshal([]byte(coPresentTargetChannels), &p.CoPresentTargetChannels)
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
	if p.Enabled {
		if err := ValidateAgentID(AgentID(strings.TrimSpace(p.RuntimeAgentID))); err != nil {
			return fmt.Errorf("runtime_agent_id is required when enabled: %w", err)
		}
		if err := ValidateAgentID(AgentID(strings.TrimSpace(p.BotAgentID))); err != nil {
			return fmt.Errorf("bot_agent_id is required when enabled: %w", err)
		}
	}
	if p.Discoverable && !p.Enabled {
		return fmt.Errorf("discoverable policy must be enabled")
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
	for _, list := range [][]string{p.AcceptFrom, p.DelegateTo, p.CoPresentFrom, p.AcceptFromRuntimes, p.CoPresentFromRuntimes} {
		for _, id := range list {
			if id == "*" {
				continue
			}
			if err := ValidateAgentID(AgentID(id)); err != nil {
				return err
			}
		}
	}
	for _, id := range p.CoPresentTargetChannels {
		if !validCoPresentTargetChannel(id) {
			return fmt.Errorf("co-present target channel %q is invalid", id)
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
	for _, target := range p.DelegateTargets {
		if strings.TrimSpace(target.RuntimeAgentID) != "" {
			if err := ValidateAgentID(AgentID(target.RuntimeAgentID)); err != nil {
				return err
			}
		} else {
			if target.AgentID != "*" {
				if err := ValidateAgentID(AgentID(target.AgentID)); err != nil {
					return err
				}
			}
			if strings.TrimSpace(target.ChannelRef) == "" {
				return fmt.Errorf("delegate target channel_ref is required")
			}
		}
		if !skillPattern.MatchString(target.SkillID) {
			return fmt.Errorf("delegate target skill %q is invalid", target.SkillID)
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
var coPresentTargetChannelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func validCoPresentTargetChannel(id string) bool {
	id = strings.TrimSpace(id)
	return id == "*" || coPresentTargetChannelPattern.MatchString(id)
}

func (s *SQLitePolicyStore) GetEnabledByChannelRef(ctx context.Context, channelRef string) (ChannelA2APolicy, error) {
	channelRef = strings.TrimSpace(channelRef)
	if !skillSlugPattern.MatchString(channelRef) {
		return ChannelA2APolicy{}, fmt.Errorf("channel_ref is invalid")
	}
	var guildID, channelID string
	err := s.db.QueryRowContext(ctx, `SELECT guild_id, channel_id FROM channel_a2a_policy WHERE enabled=1 AND channel_ref=?`, channelRef).Scan(&guildID, &channelID)
	if err != nil {
		return ChannelA2APolicy{}, err
	}
	return s.Get(ctx, guildID, channelID)
}

func (s *SQLitePolicyStore) ListDiscoverable(ctx context.Context) ([]ChannelA2APolicy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT guild_id, channel_id FROM channel_a2a_policy WHERE enabled=1 AND discoverable=1 AND runtime_agent_id IS NOT NULL AND runtime_agent_id <> '' ORDER BY runtime_agent_id`)
	if err != nil {
		return nil, err
	}
	var keys []struct {
		guildID   string
		channelID string
	}
	for rows.Next() {
		var key struct {
			guildID   string
			channelID string
		}
		if err := rows.Scan(&key.guildID, &key.channelID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	policies := make([]ChannelA2APolicy, 0, len(keys))
	for _, key := range keys {
		policy, err := s.Get(ctx, key.guildID, key.channelID)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	return policies, nil
}

func (p ChannelA2APolicy) ValidateInbound(from AgentID, skillID string) error {
	return p.ValidateInboundRuntime(from, skillID)
}

func (p ChannelA2APolicy) ValidateInboundRuntime(fromRuntime AgentID, skillID string) error {
	if !p.Enabled {
		return fmt.Errorf("%w: channel is not enabled", errorCodeError(ErrorChannelNotEnabled))
	}
	if err := ValidateAgentID(fromRuntime); err != nil {
		return fmt.Errorf("%w: %v", errorCodeError(ErrorUnauthorizedSender), err)
	}
	allowed := p.AcceptFrom
	if len(p.AcceptFromRuntimes) > 0 {
		allowed = p.AcceptFromRuntimes
	}
	if !stringListAllowsAgent(allowed, fromRuntime) {
		return fmt.Errorf("%w: sender %s is not accepted", errorCodeError(ErrorSenderNotAllowed), fromRuntime)
	}
	slug := SkillSlug(skillID)
	if !skillSlugPattern.MatchString(slug) {
		return fmt.Errorf("%w: skill %q is invalid", errorCodeError(ErrorUnknownSkill), skillID)
	}
	if !stringListAllowsValue(p.AcceptSkills, slug) {
		return fmt.Errorf("%w: skill %s is not accepted", errorCodeError(ErrorSkillNotAllowed), slug)
	}
	return nil
}

func SkillSlug(skillID string) string {
	skillID = strings.TrimSpace(skillID)
	if idx := strings.LastIndex(skillID, "/"); idx >= 0 {
		return strings.TrimSpace(skillID[idx+1:])
	}
	return skillID
}

func stringListAllowsAgent(list []string, id AgentID) bool {
	return stringListAllowsValue(list, string(id))
}

func stringListAllowsValue(list []string, value string) bool {
	value = strings.TrimSpace(value)
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "*" || item == value {
			return true
		}
	}
	return false
}

type errorCodeError ErrorCode

func (e errorCodeError) Error() string { return string(e) }
