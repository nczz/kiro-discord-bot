package skills

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const sqliteTimeFormat = time.RFC3339Nano

type Store struct {
	db *sql.DB
}

func Open(dataDir string) (*Store, error) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "./data"
	}
	return OpenPath(filepath.Join(dataDir, "skills", "skills.sqlite"))
}

func OpenPath(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("skills db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrate(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func migrate(ctx context.Context, db *sql.DB) error {
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
	for _, stmt := range migrations() {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS skills_schema_versions(component TEXT PRIMARY KEY, version INTEGER NOT NULL, applied_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS skills (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			skill_id TEXT NOT NULL UNIQUE,
			canonical_slug TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			current_version TEXT NOT NULL DEFAULT '',
			source_type TEXT NOT NULL,
			source_ref TEXT NOT NULL DEFAULT '',
			risk_level TEXT NOT NULL DEFAULT 'low',
			status TEXT NOT NULL DEFAULT 'active',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS skill_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			skill_id TEXT NOT NULL,
			version TEXT NOT NULL,
			content_markdown TEXT NOT NULL,
			content_sha256 TEXT NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			UNIQUE(skill_id, version)
		)`,
		`CREATE TABLE IF NOT EXISTS skill_installs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			install_id TEXT NOT NULL UNIQUE,
			skill_id TEXT NOT NULL,
			version TEXT NOT NULL,
			scope_type TEXT NOT NULL,
			guild_id TEXT NOT NULL DEFAULT '',
			channel_id TEXT NOT NULL DEFAULT '',
			project_cwd_hash TEXT NOT NULL DEFAULT '',
			project_cwd TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			override_policy TEXT NOT NULL DEFAULT 'inherit',
			materialized_path TEXT NOT NULL DEFAULT '',
			materialized_sha256 TEXT NOT NULL DEFAULT '',
			installed_by TEXT NOT NULL DEFAULT '',
			installed_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_skill_installs_scope ON skill_installs(skill_id, scope_type, guild_id, channel_id, project_cwd_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_skill_installs_scope_lookup ON skill_installs(scope_type, guild_id, channel_id, project_cwd_hash)`,
		`CREATE TABLE IF NOT EXISTS skill_drafts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			draft_id TEXT NOT NULL UNIQUE,
			proposed_skill_id TEXT NOT NULL DEFAULT '',
			proposed_slug TEXT NOT NULL,
			proposed_name TEXT NOT NULL,
			proposed_description TEXT NOT NULL DEFAULT '',
			proposed_version TEXT NOT NULL DEFAULT '1.0.0',
			proposed_scope_type TEXT NOT NULL,
			guild_id TEXT NOT NULL DEFAULT '',
			channel_id TEXT NOT NULL DEFAULT '',
			project_cwd_hash TEXT NOT NULL DEFAULT '',
			project_cwd TEXT NOT NULL DEFAULT '',
			source_type TEXT NOT NULL,
			source_ref TEXT NOT NULL DEFAULT '',
			source_message_refs_json TEXT NOT NULL DEFAULT '[]',
			proposed_content_markdown TEXT NOT NULL,
			required_tools_json TEXT NOT NULL DEFAULT '[]',
			risk_report_json TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL DEFAULT 'draft',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS skill_tool_requirements (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			skill_id TEXT NOT NULL,
			version TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			required INTEGER NOT NULL DEFAULT 1,
			min_version TEXT NOT NULL DEFAULT '',
			permission_level TEXT NOT NULL DEFAULT 'read',
			UNIQUE(skill_id, version, tool_name)
		)`,
		`CREATE TABLE IF NOT EXISTS skill_usage_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			usage_id TEXT NOT NULL UNIQUE,
			skill_id TEXT NOT NULL,
			version TEXT NOT NULL,
			guild_id TEXT NOT NULL DEFAULT '',
			channel_id TEXT NOT NULL DEFAULT '',
			thread_id TEXT NOT NULL DEFAULT '',
			project_cwd_hash TEXT NOT NULL DEFAULT '',
			message_id TEXT NOT NULL DEFAULT '',
			agent_session_id TEXT NOT NULL DEFAULT '',
			selected_by TEXT NOT NULL DEFAULT 'agent',
			used_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_skill_usage_skill ON skill_usage_events(skill_id, version)`,
		`CREATE TABLE IF NOT EXISTS skill_mutation_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			action TEXT NOT NULL,
			skill_id TEXT NOT NULL DEFAULT '',
			install_id TEXT NOT NULL DEFAULT '',
			draft_id TEXT NOT NULL DEFAULT '',
			scope_type TEXT NOT NULL DEFAULT '',
			guild_id TEXT NOT NULL DEFAULT '',
			channel_id TEXT NOT NULL DEFAULT '',
			target_channel_id TEXT NOT NULL DEFAULT '',
			project_cwd_hash TEXT NOT NULL DEFAULT '',
			actor_user_id TEXT NOT NULL DEFAULT '',
			actor_username TEXT NOT NULL DEFAULT '',
			source_message_id TEXT NOT NULL DEFAULT '',
			source_interaction_id TEXT NOT NULL DEFAULT '',
			agent_session_id TEXT NOT NULL DEFAULT '',
			mcp_server_name TEXT NOT NULL DEFAULT '',
			mcp_tool_name TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			status_before TEXT NOT NULL DEFAULT '',
			status_after TEXT NOT NULL DEFAULT '',
			version_before TEXT NOT NULL DEFAULT '',
			version_after TEXT NOT NULL DEFAULT '',
			content_sha_before TEXT NOT NULL DEFAULT '',
			content_sha_after TEXT NOT NULL DEFAULT '',
			materialized_path TEXT NOT NULL DEFAULT '',
			materialized_sha256 TEXT NOT NULL DEFAULT '',
			result_status TEXT NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			previous_event_hash TEXT NOT NULL DEFAULT '',
			event_hash TEXT NOT NULL,
			occurred_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_skill_mutation_skill_time ON skill_mutation_events(skill_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_skill_mutation_scope_time ON skill_mutation_events(guild_id, channel_id, target_channel_id, occurred_at)`,
		`INSERT INTO skills_schema_versions(component, version, applied_at) VALUES('skills', 1, strftime('%Y-%m-%dT%H:%M:%fZ','now')) ON CONFLICT(component) DO UPDATE SET version=excluded.version, applied_at=excluded.applied_at`,
	}
}

func (s *Store) CreateDraft(ctx context.Context, draft Draft) (Draft, error) {
	if s == nil || s.db == nil {
		return Draft{}, fmt.Errorf("skills store is unavailable")
	}
	now := time.Now().UTC()
	draft = normalizeDraft(draft, now)
	if err := validateDraft(draft); err != nil {
		return Draft{}, err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO skill_drafts(draft_id, proposed_skill_id, proposed_slug, proposed_name, proposed_description, proposed_version, proposed_scope_type, guild_id, channel_id, project_cwd_hash, project_cwd, source_type, source_ref, source_message_refs_json, proposed_content_markdown, required_tools_json, risk_report_json, status, created_by, created_at, expires_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, draft.DraftID, draft.ProposedSkillID, draft.ProposedSlug, draft.ProposedName, draft.ProposedDescription, draft.ProposedVersion, draft.ProposedScopeType, draft.GuildID, draft.ChannelID, draft.ProjectCWDHash, draft.ProjectCWD, draft.SourceType, draft.SourceRef, draft.SourceMessageRefsJSON, draft.ProposedContentMarkdown, draft.RequiredToolsJSON, draft.RiskReportJSON, draft.Status, draft.CreatedBy, formatTime(draft.CreatedAt), formatOptionalTime(draft.ExpiresAt))
	if err != nil {
		return Draft{}, err
	}
	return draft, nil
}

func normalizeDraft(d Draft, now time.Time) Draft {
	d.DraftID = strings.TrimSpace(d.DraftID)
	if d.DraftID == "" {
		d.DraftID = "draft_" + uuid.NewString()
	}
	d.ProposedSlug = NormalizeSlug(firstNonEmpty(d.ProposedSlug, d.ProposedName, d.ProposedSkillID))
	d.ProposedSkillID = strings.TrimSpace(d.ProposedSkillID)
	if d.ProposedSkillID == "" {
		d.ProposedSkillID = d.ProposedSlug
	}
	d.ProposedName = strings.TrimSpace(d.ProposedName)
	if d.ProposedName == "" {
		d.ProposedName = d.ProposedSlug
	}
	d.ProposedDescription = strings.TrimSpace(d.ProposedDescription)
	d.ProposedVersion = normalizeVersion(d.ProposedVersion)
	d.ProposedScopeType = NormalizeScope(d.ProposedScopeType)
	d.GuildID = strings.TrimSpace(d.GuildID)
	d.ChannelID = strings.TrimSpace(d.ChannelID)
	d.ProjectCWD = strings.TrimSpace(d.ProjectCWD)
	if d.ProjectCWDHash == "" {
		d.ProjectCWDHash = ProjectCWDHash(d.ProjectCWD)
	}
	d.SourceType = NormalizeSourceType(d.SourceType)
	d.SourceRef = strings.TrimSpace(d.SourceRef)
	if strings.TrimSpace(d.SourceMessageRefsJSON) == "" {
		d.SourceMessageRefsJSON = "[]"
	}
	d.ProposedContentMarkdown = strings.TrimSpace(d.ProposedContentMarkdown)
	if strings.TrimSpace(d.RequiredToolsJSON) == "" {
		d.RequiredToolsJSON = "[]"
	}
	if strings.TrimSpace(d.RiskReportJSON) == "" {
		d.RiskReportJSON = "{}"
	}
	if strings.TrimSpace(d.Status) == "" {
		d.Status = StatusDraft
	}
	d.CreatedBy = strings.TrimSpace(d.CreatedBy)
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	return d
}

func validateDraft(d Draft) error {
	if err := ValidateSlug(d.ProposedSlug); err != nil {
		return err
	}
	if d.ProposedContentMarkdown == "" {
		return fmt.Errorf("skill content is required")
	}
	if looksLikeRawHTMLDocument(d.ProposedContentMarkdown) {
		return fmt.Errorf("skill content must be curated markdown, not raw HTML")
	}
	if err := ValidateScope(d.ProposedScopeType); err != nil {
		return err
	}
	if err := validateScopeFields(d.ProposedScopeType, d.GuildID, d.ChannelID, d.ProjectCWD, d.ProjectCWDHash); err != nil {
		return err
	}
	if !json.Valid([]byte(d.RequiredToolsJSON)) {
		return fmt.Errorf("required tools must be JSON")
	}
	if !json.Valid([]byte(d.RiskReportJSON)) {
		return fmt.Errorf("risk report must be JSON")
	}
	if !json.Valid([]byte(d.SourceMessageRefsJSON)) {
		return fmt.Errorf("source message refs must be JSON")
	}
	return nil
}

func looksLikeRawHTMLDocument(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	return strings.HasPrefix(lower, "<!doctype html") ||
		strings.HasPrefix(lower, "<html") ||
		strings.HasPrefix(lower, "<head") ||
		strings.HasPrefix(lower, "<body")
}

func validateScopeFields(scope, guildID, channelID, projectCWD, cwdHash string) error {
	projectCWD = strings.TrimSpace(projectCWD)
	cwdHash = strings.TrimSpace(cwdHash)
	switch NormalizeScope(scope) {
	case ScopeGuild:
		if strings.TrimSpace(guildID) == "" {
			return fmt.Errorf("guild scope requires guild_id")
		}
	case ScopeChannel:
		if strings.TrimSpace(guildID) == "" || strings.TrimSpace(channelID) == "" {
			return fmt.Errorf("channel scope requires guild_id and channel_id")
		}
	case ScopeProject:
		if projectCWD == "" || cwdHash == "" || ProjectCWDHash(projectCWD) != cwdHash {
			return fmt.Errorf("project scope requires project_cwd and matching project_cwd_hash")
		}
	case ScopeChannelProject:
		if strings.TrimSpace(guildID) == "" || strings.TrimSpace(channelID) == "" || projectCWD == "" || cwdHash == "" || ProjectCWDHash(projectCWD) != cwdHash {
			return fmt.Errorf("channel_project scope requires guild_id, channel_id, and project_cwd")
		}
	default:
		return fmt.Errorf("unsupported skill scope %q", scope)
	}
	return nil
}

func (s *Store) GetDraft(ctx context.Context, draftID string) (Draft, error) {
	return scanDraft(s.db.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM skill_drafts WHERE draft_id=?`, strings.TrimSpace(draftID)))
}

func (s *Store) ActiveDrafts(ctx context.Context, rc ResolveContext, limit int) ([]Draft, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("skills store is unavailable")
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rc = normalizeResolveContext(rc)
	now := time.Now().UTC()
	rows, err := s.db.QueryContext(ctx, `SELECT `+draftColumns+` FROM skill_drafts
		WHERE status=?
		ORDER BY created_at DESC`, StatusDraft)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Draft{}
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		if !d.ExpiresAt.IsZero() && now.After(d.ExpiresAt) {
			continue
		}
		if scopeMatches(d.ProposedScopeType, d.GuildID, d.ChannelID, d.ProjectCWDHash, rc) {
			out = append(out, d)
			if len(out) >= limit {
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

const draftColumns = "draft_id, proposed_skill_id, proposed_slug, proposed_name, proposed_description, proposed_version, proposed_scope_type, guild_id, channel_id, project_cwd_hash, project_cwd, source_type, source_ref, source_message_refs_json, proposed_content_markdown, required_tools_json, risk_report_json, status, created_by, created_at, expires_at"

type draftScanner interface {
	Scan(dest ...any) error
}

func scanDraft(scanner draftScanner) (Draft, error) {
	var d Draft
	var created, expires string
	err := scanner.Scan(&d.DraftID, &d.ProposedSkillID, &d.ProposedSlug, &d.ProposedName, &d.ProposedDescription, &d.ProposedVersion, &d.ProposedScopeType, &d.GuildID, &d.ChannelID, &d.ProjectCWDHash, &d.ProjectCWD, &d.SourceType, &d.SourceRef, &d.SourceMessageRefsJSON, &d.ProposedContentMarkdown, &d.RequiredToolsJSON, &d.RiskReportJSON, &d.Status, &d.CreatedBy, &created, &expires)
	if err != nil {
		return Draft{}, err
	}
	d.CreatedAt = parseTime(created)
	d.ExpiresAt = parseTime(expires)
	return d, nil
}

func (s *Store) InstallDraft(ctx context.Context, draftID, confirmedBy string) (Install, error) {
	if s == nil || s.db == nil {
		return Install{}, fmt.Errorf("skills store is unavailable")
	}
	draft, err := s.GetDraft(ctx, draftID)
	if err != nil {
		return Install{}, err
	}
	if draft.Status != StatusDraft {
		return Install{}, fmt.Errorf("draft %s is %s", draft.DraftID, draft.Status)
	}
	if !draft.ExpiresAt.IsZero() && time.Now().UTC().After(draft.ExpiresAt) {
		return Install{}, fmt.Errorf("draft %s expired", draft.DraftID)
	}
	return s.installDraft(ctx, draft, strings.TrimSpace(confirmedBy), "", "", MutationActor{ActorUsername: strings.TrimSpace(confirmedBy)}, "install draft", true, "skill_installed", "install")
}

func (s *Store) InstallDraftWithMaterialization(ctx context.Context, draftID, confirmedBy, materializedPath, materializedSHA256 string) (Install, error) {
	draft, err := s.GetDraft(ctx, draftID)
	if err != nil {
		return Install{}, err
	}
	if draft.Status != StatusDraft {
		return Install{}, fmt.Errorf("draft %s is %s", draft.DraftID, draft.Status)
	}
	if !draft.ExpiresAt.IsZero() && time.Now().UTC().After(draft.ExpiresAt) {
		return Install{}, fmt.Errorf("draft %s expired", draft.DraftID)
	}
	return s.installDraft(ctx, draft, strings.TrimSpace(confirmedBy), materializedPath, materializedSHA256, MutationActor{ActorUsername: strings.TrimSpace(confirmedBy)}, "install draft", true, "skill_installed", "install")
}

func (s *Store) InstallDraftWithMaterializationAndAudit(ctx context.Context, draftID string, actor MutationActor, reason, materializedPath, materializedSHA256 string) (Install, error) {
	if s == nil || s.db == nil {
		return Install{}, fmt.Errorf("skills store is unavailable")
	}
	draft, err := s.GetDraft(ctx, draftID)
	if err != nil {
		return Install{}, err
	}
	if draft.Status != StatusDraft {
		return Install{}, fmt.Errorf("draft %s is %s", draft.DraftID, draft.Status)
	}
	if !draft.ExpiresAt.IsZero() && time.Now().UTC().After(draft.ExpiresAt) {
		return Install{}, fmt.Errorf("draft %s expired", draft.DraftID)
	}
	return s.installDraft(ctx, draft, firstNonEmpty(actor.ActorUsername, actor.ActorUserID), materializedPath, materializedSHA256, actor, reason, true, "skill_installed", "install")
}

func (s *Store) installDraft(ctx context.Context, draft Draft, confirmedBy, materializedPath, materializedSHA256 string, actor MutationActor, reason string, enabled bool, eventType, action string) (Install, error) {
	now := time.Now().UTC()
	required, err := RequiredToolsFromJSON(draft.RequiredToolsJSON)
	if err != nil {
		return Install{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Install{}, err
	}
	defer tx.Rollback()
	skillID := firstNonEmpty(draft.ProposedSkillID, draft.ProposedSlug)
	if !enabled {
		var existingSkillID string
		err = tx.QueryRowContext(ctx, `SELECT skill_id FROM skills WHERE skill_id=?`, skillID).Scan(&existingSkillID)
		if err == nil {
			return Install{}, fmt.Errorf("skill %s already exists; use the review install flow or a new slug/version", skillID)
		}
		if err != sql.ErrNoRows {
			return Install{}, err
		}
	}
	before := s.installSnapshotTx(ctx, tx, skillID, draft.ProposedScopeType, draft.GuildID, draft.ChannelID, draft.ProjectCWDHash)
	claim, err := tx.ExecContext(ctx, `UPDATE skill_drafts SET status=? WHERE draft_id=? AND status=?`, StatusInstalled, draft.DraftID, StatusDraft)
	if err != nil {
		return Install{}, err
	}
	claimed, err := claim.RowsAffected()
	if err != nil {
		return Install{}, err
	}
	if claimed == 0 {
		return Install{}, fmt.Errorf("draft %s is not an active draft", draft.DraftID)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO skills(skill_id, canonical_slug, name, description, current_version, source_type, source_ref, risk_level, status, created_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(skill_id) DO UPDATE SET canonical_slug=excluded.canonical_slug, name=excluded.name, description=excluded.description, current_version=excluded.current_version, source_type=excluded.source_type, source_ref=excluded.source_ref, risk_level=excluded.risk_level, status='active', updated_at=excluded.updated_at`, skillID, draft.ProposedSlug, draft.ProposedName, draft.ProposedDescription, draft.ProposedVersion, draft.SourceType, draft.SourceRef, riskLevelFromJSON(draft.RiskReportJSON), StatusActive, confirmedBy, formatTime(now), formatTime(now))
	if err != nil {
		return Install{}, err
	}
	contentSHA := ContentSHA256(draft.ProposedContentMarkdown)
	_, err = tx.ExecContext(ctx, `INSERT INTO skill_versions(skill_id, version, content_markdown, content_sha256, metadata_json, created_by, created_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(skill_id, version) DO UPDATE SET content_markdown=excluded.content_markdown, content_sha256=excluded.content_sha256, metadata_json=excluded.metadata_json`, skillID, draft.ProposedVersion, draft.ProposedContentMarkdown, contentSHA, draft.RiskReportJSON, confirmedBy, formatTime(now))
	if err != nil {
		return Install{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM skill_tool_requirements WHERE skill_id=? AND version=?`, skillID, draft.ProposedVersion); err != nil {
		return Install{}, err
	}
	for _, tool := range required {
		if _, err = tx.ExecContext(ctx, `INSERT INTO skill_tool_requirements(skill_id, version, tool_name, required, min_version, permission_level) VALUES(?,?,?,?,?,?)`, skillID, draft.ProposedVersion, tool, 1, "", "read"); err != nil {
			return Install{}, err
		}
	}
	install := Install{
		InstallID:          "inst_" + uuid.NewString(),
		SkillID:            skillID,
		Version:            draft.ProposedVersion,
		ScopeType:          draft.ProposedScopeType,
		GuildID:            draft.GuildID,
		ChannelID:          draft.ChannelID,
		ProjectCWDHash:     draft.ProjectCWDHash,
		ProjectCWD:         draft.ProjectCWD,
		Enabled:            enabled,
		OverridePolicy:     installOverridePolicy(enabled),
		MaterializedPath:   strings.TrimSpace(materializedPath),
		MaterializedSHA256: strings.TrimSpace(materializedSHA256),
		InstalledBy:        confirmedBy,
		InstalledAt:        now,
		UpdatedAt:          now,
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO skill_installs(install_id, skill_id, version, scope_type, guild_id, channel_id, project_cwd_hash, project_cwd, enabled, override_policy, materialized_path, materialized_sha256, installed_by, installed_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(skill_id, scope_type, guild_id, channel_id, project_cwd_hash) DO UPDATE SET version=excluded.version, project_cwd=excluded.project_cwd, enabled=excluded.enabled, override_policy=excluded.override_policy, materialized_path=excluded.materialized_path, materialized_sha256=excluded.materialized_sha256, installed_by=excluded.installed_by, updated_at=excluded.updated_at`, install.InstallID, install.SkillID, install.Version, install.ScopeType, install.GuildID, install.ChannelID, install.ProjectCWDHash, install.ProjectCWD, boolInt(install.Enabled), install.OverridePolicy, install.MaterializedPath, install.MaterializedSHA256, install.InstalledBy, formatTime(install.InstalledAt), formatTime(install.UpdatedAt))
	if err != nil {
		return Install{}, err
	}
	if before.installID != "" {
		install.InstallID = before.installID
	}
	if err = s.recordMutationTx(ctx, tx, MutationEvent{
		EventType:          eventType,
		Action:             action,
		SkillID:            install.SkillID,
		InstallID:          install.InstallID,
		DraftID:            draft.DraftID,
		ScopeType:          install.ScopeType,
		GuildID:            install.GuildID,
		ChannelID:          install.ChannelID,
		TargetChannelID:    firstNonEmpty(actor.TargetChannelID, install.ChannelID),
		ProjectCWDHash:     install.ProjectCWDHash,
		ActorUserID:        actor.ActorUserID,
		ActorUsername:      firstNonEmpty(actor.ActorUsername, confirmedBy),
		SourceMessageID:    actor.SourceMessageID,
		InteractionID:      actor.InteractionID,
		AgentSessionID:     actor.AgentSessionID,
		MCPServerName:      actor.MCPServerName,
		MCPToolName:        actor.MCPToolName,
		Reason:             reason,
		StatusBefore:       before.status,
		StatusAfter:        installStatusAfter(enabled),
		VersionBefore:      before.version,
		VersionAfter:       install.Version,
		ContentSHABefore:   before.contentSHA,
		ContentSHAAfter:    contentSHA,
		MaterializedPath:   install.MaterializedPath,
		MaterializedSHA256: install.MaterializedSHA256,
		ResultStatus:       "ok",
		MetadataJSON:       "{}",
	}); err != nil {
		return Install{}, err
	}
	if err = tx.Commit(); err != nil {
		return Install{}, err
	}
	return install, nil
}

func (s *Store) CreateDisabledInstallFromDraftWithMaterializationAndAudit(ctx context.Context, draftID string, actor MutationActor, reason, materializedPath, materializedSHA256 string) (Install, error) {
	if s == nil || s.db == nil {
		return Install{}, fmt.Errorf("skills store is unavailable")
	}
	draft, err := s.GetDraft(ctx, draftID)
	if err != nil {
		return Install{}, err
	}
	if draft.Status != StatusDraft {
		return Install{}, fmt.Errorf("draft %s is %s", draft.DraftID, draft.Status)
	}
	if !draft.ExpiresAt.IsZero() && time.Now().UTC().After(draft.ExpiresAt) {
		return Install{}, fmt.Errorf("draft %s expired", draft.DraftID)
	}
	return s.installDraft(ctx, draft, firstNonEmpty(actor.ActorUsername, actor.ActorUserID), materializedPath, materializedSHA256, actor, reason, false, "skill_created", "create")
}

func (s *Store) GetInstallByID(ctx context.Context, installID string) (Install, error) {
	if s == nil || s.db == nil {
		return Install{}, fmt.Errorf("skills store is unavailable")
	}
	var install Install
	var enabled int
	var installedAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT install_id, skill_id, version, scope_type, guild_id, channel_id, project_cwd_hash, project_cwd, enabled, override_policy, materialized_path, materialized_sha256, installed_by, installed_at, updated_at
		FROM skill_installs WHERE install_id=?`, strings.TrimSpace(installID)).Scan(&install.InstallID, &install.SkillID, &install.Version, &install.ScopeType, &install.GuildID, &install.ChannelID, &install.ProjectCWDHash, &install.ProjectCWD, &enabled, &install.OverridePolicy, &install.MaterializedPath, &install.MaterializedSHA256, &install.InstalledBy, &installedAt, &updatedAt)
	if err != nil {
		return Install{}, err
	}
	install.Enabled = intBool(enabled)
	install.InstalledAt = parseTime(installedAt)
	install.UpdatedAt = parseTime(updatedAt)
	return install, nil
}

func installOverridePolicy(enabled bool) string {
	if enabled {
		return OverrideInherit
	}
	return OverrideDisable
}

func installStatusAfter(enabled bool) string {
	if enabled {
		return StatusActive
	}
	return StatusDisabled
}

type installSnapshot struct {
	installID  string
	status     string
	version    string
	contentSHA string
}

func (s *Store) installSnapshotTx(ctx context.Context, tx *sql.Tx, skillID, scopeType, guildID, channelID, cwdHash string) installSnapshot {
	var snap installSnapshot
	var enabled int
	err := tx.QueryRowContext(ctx, `SELECT i.install_id, i.version, i.enabled, v.content_sha256
		FROM skill_installs i
		LEFT JOIN skill_versions v ON v.skill_id=i.skill_id AND v.version=i.version
		WHERE i.skill_id=? AND i.scope_type=? AND i.guild_id=? AND i.channel_id=? AND i.project_cwd_hash=?`,
		strings.TrimSpace(skillID), NormalizeScope(scopeType), strings.TrimSpace(guildID), strings.TrimSpace(channelID), strings.TrimSpace(cwdHash)).Scan(&snap.installID, &snap.version, &enabled, &snap.contentSHA)
	if err != nil {
		return installSnapshot{}
	}
	if intBool(enabled) {
		snap.status = StatusActive
	} else {
		snap.status = StatusDisabled
	}
	return snap
}

func (s *Store) recordMutationTx(ctx context.Context, tx *sql.Tx, ev MutationEvent) error {
	now := time.Now().UTC()
	if ev.EventID == "" {
		ev.EventID = "mut_" + uuid.NewString()
	}
	if ev.EventType == "" {
		ev.EventType = "skill_" + strings.TrimSpace(ev.Action)
	}
	if ev.ResultStatus == "" {
		ev.ResultStatus = "ok"
	}
	if strings.TrimSpace(ev.MetadataJSON) == "" {
		ev.MetadataJSON = "{}"
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = now
	}
	prev := ""
	_ = tx.QueryRowContext(ctx, `SELECT event_hash FROM skill_mutation_events ORDER BY id DESC LIMIT 1`).Scan(&prev)
	ev.PreviousEventHash = prev
	ev.EventHash = mutationEventHash(ev)
	_, err := tx.ExecContext(ctx, `INSERT INTO skill_mutation_events(event_id, event_type, action, skill_id, install_id, draft_id, scope_type, guild_id, channel_id, target_channel_id, project_cwd_hash, actor_user_id, actor_username, source_message_id, source_interaction_id, agent_session_id, mcp_server_name, mcp_tool_name, reason, status_before, status_after, version_before, version_after, content_sha_before, content_sha_after, materialized_path, materialized_sha256, result_status, error_code, error_message, metadata_json, previous_event_hash, event_hash, occurred_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.EventID, ev.EventType, ev.Action, ev.SkillID, ev.InstallID, ev.DraftID, ev.ScopeType, ev.GuildID, ev.ChannelID, ev.TargetChannelID, ev.ProjectCWDHash, ev.ActorUserID, ev.ActorUsername, ev.SourceMessageID, ev.InteractionID, ev.AgentSessionID, ev.MCPServerName, ev.MCPToolName, ev.Reason, ev.StatusBefore, ev.StatusAfter, ev.VersionBefore, ev.VersionAfter, ev.ContentSHABefore, ev.ContentSHAAfter, ev.MaterializedPath, ev.MaterializedSHA256, ev.ResultStatus, ev.ErrorCode, ev.ErrorMessage, ev.MetadataJSON, ev.PreviousEventHash, ev.EventHash, formatTime(ev.OccurredAt))
	return err
}

func mutationEventHash(ev MutationEvent) string {
	copy := ev
	copy.EventHash = ""
	raw, _ := json.Marshal(copy)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Store) SetInstallEnabled(ctx context.Context, rc ResolveContext, skillID, scopeType string, enabled bool, actor MutationActor, reason, action string) (MutationEvent, error) {
	if s == nil || s.db == nil {
		return MutationEvent{}, fmt.Errorf("skills store is unavailable")
	}
	rc = normalizeResolveContext(rc)
	scopeType = NormalizeScope(scopeType)
	guildID, channelID, cwdHash, err := scopeFieldsFromContext(scopeType, rc)
	if err != nil {
		return MutationEvent{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MutationEvent{}, err
	}
	defer tx.Rollback()
	before := s.installSnapshotTx(ctx, tx, skillID, scopeType, guildID, channelID, cwdHash)
	if before.version == "" {
		return MutationEvent{}, sql.ErrNoRows
	}
	now := time.Now().UTC()
	override := OverrideDisable
	statusAfter := StatusDisabled
	if enabled {
		override = OverrideInherit
		statusAfter = StatusActive
	}
	res, err := tx.ExecContext(ctx, `UPDATE skill_installs SET enabled=?, override_policy=?, installed_by=?, updated_at=? WHERE skill_id=? AND scope_type=? AND guild_id=? AND channel_id=? AND project_cwd_hash=?`,
		boolInt(enabled), override, firstNonEmpty(actor.ActorUsername, actor.ActorUserID), formatTime(now), strings.TrimSpace(skillID), scopeType, guildID, channelID, cwdHash)
	if err != nil {
		return MutationEvent{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return MutationEvent{}, err
	}
	if n == 0 {
		return MutationEvent{}, sql.ErrNoRows
	}
	ev := MutationEvent{
		EventType:        "skill_" + action,
		Action:           action,
		SkillID:          strings.TrimSpace(skillID),
		InstallID:        before.installID,
		ScopeType:        scopeType,
		GuildID:          guildID,
		ChannelID:        channelID,
		TargetChannelID:  firstNonEmpty(actor.TargetChannelID, rc.TargetID, channelID),
		ProjectCWDHash:   cwdHash,
		ActorUserID:      actor.ActorUserID,
		ActorUsername:    actor.ActorUsername,
		SourceMessageID:  actor.SourceMessageID,
		InteractionID:    actor.InteractionID,
		AgentSessionID:   actor.AgentSessionID,
		MCPServerName:    actor.MCPServerName,
		MCPToolName:      actor.MCPToolName,
		Reason:           reason,
		StatusBefore:     before.status,
		StatusAfter:      statusAfter,
		VersionBefore:    before.version,
		VersionAfter:     before.version,
		ContentSHABefore: before.contentSHA,
		ContentSHAAfter:  before.contentSHA,
		ResultStatus:     "ok",
		MetadataJSON:     "{}",
	}
	if err := s.recordMutationTx(ctx, tx, ev); err != nil {
		return MutationEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return MutationEvent{}, err
	}
	return s.lastMutationEvent(ctx, ev.SkillID)
}

func (s *Store) RollbackInstall(ctx context.Context, rc ResolveContext, skillID, scopeType, version string, actor MutationActor, reason string) (MutationEvent, error) {
	if s == nil || s.db == nil {
		return MutationEvent{}, fmt.Errorf("skills store is unavailable")
	}
	rc = normalizeResolveContext(rc)
	scopeType = NormalizeScope(scopeType)
	guildID, channelID, cwdHash, err := scopeFieldsFromContext(scopeType, rc)
	if err != nil {
		return MutationEvent{}, err
	}
	var afterSHA string
	if err := s.db.QueryRowContext(ctx, `SELECT content_sha256 FROM skill_versions WHERE skill_id=? AND version=?`, strings.TrimSpace(skillID), strings.TrimSpace(version)).Scan(&afterSHA); err != nil {
		return MutationEvent{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MutationEvent{}, err
	}
	defer tx.Rollback()
	before := s.installSnapshotTx(ctx, tx, skillID, scopeType, guildID, channelID, cwdHash)
	if before.version == "" {
		return MutationEvent{}, sql.ErrNoRows
	}
	res, err := tx.ExecContext(ctx, `UPDATE skill_installs SET version=?, enabled=1, override_policy=?, installed_by=?, updated_at=? WHERE skill_id=? AND scope_type=? AND guild_id=? AND channel_id=? AND project_cwd_hash=?`,
		strings.TrimSpace(version), OverrideInherit, firstNonEmpty(actor.ActorUsername, actor.ActorUserID), formatTime(time.Now().UTC()), strings.TrimSpace(skillID), scopeType, guildID, channelID, cwdHash)
	if err != nil {
		return MutationEvent{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return MutationEvent{}, err
	}
	if n == 0 {
		return MutationEvent{}, sql.ErrNoRows
	}
	ev := MutationEvent{EventType: "skill_rollback", Action: "rollback", SkillID: strings.TrimSpace(skillID), InstallID: before.installID, ScopeType: scopeType, GuildID: guildID, ChannelID: channelID, TargetChannelID: firstNonEmpty(actor.TargetChannelID, rc.TargetID, channelID), ProjectCWDHash: cwdHash, ActorUserID: actor.ActorUserID, ActorUsername: actor.ActorUsername, SourceMessageID: actor.SourceMessageID, InteractionID: actor.InteractionID, AgentSessionID: actor.AgentSessionID, MCPServerName: actor.MCPServerName, MCPToolName: actor.MCPToolName, Reason: reason, StatusBefore: before.status, StatusAfter: StatusActive, VersionBefore: before.version, VersionAfter: strings.TrimSpace(version), ContentSHABefore: before.contentSHA, ContentSHAAfter: afterSHA, ResultStatus: "ok", MetadataJSON: "{}"}
	if err := s.recordMutationTx(ctx, tx, ev); err != nil {
		return MutationEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return MutationEvent{}, err
	}
	return s.lastMutationEvent(ctx, strings.TrimSpace(skillID))
}

func (s *Store) MutationHistory(ctx context.Context, skillID string, limit int) ([]MutationEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("skills store is unavailable")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event_id, event_type, action, skill_id, install_id, draft_id, scope_type, guild_id, channel_id, target_channel_id, project_cwd_hash, actor_user_id, actor_username, source_message_id, source_interaction_id, agent_session_id, mcp_server_name, mcp_tool_name, reason, status_before, status_after, version_before, version_after, content_sha_before, content_sha_after, materialized_path, materialized_sha256, result_status, error_code, error_message, metadata_json, previous_event_hash, event_hash, occurred_at FROM skill_mutation_events WHERE skill_id=? ORDER BY id DESC LIMIT ?`, strings.TrimSpace(skillID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MutationEvent
	for rows.Next() {
		ev, err := scanMutationEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) MutationHistoryForContext(ctx context.Context, rc ResolveContext, skillID, scopeType string, limit int) ([]MutationEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("skills store is unavailable")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rc = normalizeResolveContext(rc)
	scopeType = NormalizeScope(scopeType)
	guildID, channelID, cwdHash, err := scopeFieldsFromContext(scopeType, rc)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event_id, event_type, action, skill_id, install_id, draft_id, scope_type, guild_id, channel_id, target_channel_id, project_cwd_hash, actor_user_id, actor_username, source_message_id, source_interaction_id, agent_session_id, mcp_server_name, mcp_tool_name, reason, status_before, status_after, version_before, version_after, content_sha_before, content_sha_after, materialized_path, materialized_sha256, result_status, error_code, error_message, metadata_json, previous_event_hash, event_hash, occurred_at FROM skill_mutation_events WHERE skill_id=? AND scope_type=? AND guild_id=? AND channel_id=? AND project_cwd_hash=? ORDER BY id DESC LIMIT ?`, strings.TrimSpace(skillID), scopeType, guildID, channelID, cwdHash, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MutationEvent
	for rows.Next() {
		ev, err := scanMutationEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) lastMutationEvent(ctx context.Context, skillID string) (MutationEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_id, event_type, action, skill_id, install_id, draft_id, scope_type, guild_id, channel_id, target_channel_id, project_cwd_hash, actor_user_id, actor_username, source_message_id, source_interaction_id, agent_session_id, mcp_server_name, mcp_tool_name, reason, status_before, status_after, version_before, version_after, content_sha_before, content_sha_after, materialized_path, materialized_sha256, result_status, error_code, error_message, metadata_json, previous_event_hash, event_hash, occurred_at FROM skill_mutation_events WHERE skill_id=? ORDER BY id DESC LIMIT 1`, strings.TrimSpace(skillID))
	if err != nil {
		return MutationEvent{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return MutationEvent{}, sql.ErrNoRows
	}
	return scanMutationEvent(rows)
}

type mutationScanner interface {
	Scan(dest ...any) error
}

func scanMutationEvent(rows mutationScanner) (MutationEvent, error) {
	var ev MutationEvent
	var occurred string
	err := rows.Scan(&ev.EventID, &ev.EventType, &ev.Action, &ev.SkillID, &ev.InstallID, &ev.DraftID, &ev.ScopeType, &ev.GuildID, &ev.ChannelID, &ev.TargetChannelID, &ev.ProjectCWDHash, &ev.ActorUserID, &ev.ActorUsername, &ev.SourceMessageID, &ev.InteractionID, &ev.AgentSessionID, &ev.MCPServerName, &ev.MCPToolName, &ev.Reason, &ev.StatusBefore, &ev.StatusAfter, &ev.VersionBefore, &ev.VersionAfter, &ev.ContentSHABefore, &ev.ContentSHAAfter, &ev.MaterializedPath, &ev.MaterializedSHA256, &ev.ResultStatus, &ev.ErrorCode, &ev.ErrorMessage, &ev.MetadataJSON, &ev.PreviousEventHash, &ev.EventHash, &occurred)
	ev.OccurredAt = parseTime(occurred)
	return ev, err
}

func scopeFieldsFromContext(scope string, rc ResolveContext) (string, string, string, error) {
	switch NormalizeScope(scope) {
	case ScopeGuild:
		if rc.GuildID == "" {
			return "", "", "", fmt.Errorf("guild scope requires guild context")
		}
		return rc.GuildID, "", "", nil
	case ScopeChannel:
		if rc.GuildID == "" || firstNonEmpty(rc.ParentChannelID, rc.ChannelID) == "" {
			return "", "", "", fmt.Errorf("channel scope requires guild and channel context")
		}
		return rc.GuildID, firstNonEmpty(rc.ParentChannelID, rc.ChannelID), "", nil
	case ScopeProject:
		if rc.ProjectCWDHash == "" {
			return "", "", "", fmt.Errorf("project scope requires project context")
		}
		return "", "", rc.ProjectCWDHash, nil
	case ScopeChannelProject:
		if rc.GuildID == "" || firstNonEmpty(rc.ParentChannelID, rc.ChannelID) == "" || rc.ProjectCWDHash == "" {
			return "", "", "", fmt.Errorf("channel_project scope requires guild, channel, and project context")
		}
		return rc.GuildID, firstNonEmpty(rc.ParentChannelID, rc.ChannelID), rc.ProjectCWDHash, nil
	default:
		return "", "", "", fmt.Errorf("unsupported skill scope %q", scope)
	}
}

func (s *Store) Resolve(ctx context.Context, rc ResolveContext) ([]ResolvedSkill, error) {
	return s.resolve(ctx, rc, false)
}

func (s *Store) resolve(ctx context.Context, rc ResolveContext, includeDisabled bool) ([]ResolvedSkill, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("skills store is unavailable")
	}
	rc = normalizeResolveContext(rc)
	rows, err := s.db.QueryContext(ctx, `SELECT sk.skill_id, sk.canonical_slug, sk.name, sk.description, sk.risk_level, i.version, i.scope_type, i.guild_id, i.channel_id, i.project_cwd_hash, i.enabled, i.override_policy, i.materialized_path, v.content_markdown
		FROM skill_installs i
		JOIN skills sk ON sk.skill_id=i.skill_id
		JOIN skill_versions v ON v.skill_id=i.skill_id AND v.version=i.version
		WHERE sk.status='active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type candidate struct {
		ResolvedSkill
		installGuildID   string
		installChannelID string
		installCWDHash   string
		enabled          bool
		override         string
		prec             int
	}
	selected := map[string]candidate{}
	for rows.Next() {
		var c candidate
		var enabled int
		if err := rows.Scan(&c.SkillID, &c.Slug, &c.Name, &c.Description, &c.RiskLevel, &c.Version, &c.ScopeType, &c.installGuildID, &c.installChannelID, &c.installCWDHash, &enabled, &c.override, &c.MaterializedPath, &c.ContentMarkdown); err != nil {
			return nil, err
		}
		c.enabled = intBool(enabled)
		if !scopeMatches(c.ScopeType, c.installGuildID, c.installChannelID, c.installCWDHash, rc) {
			continue
		}
		c.prec = scopePrecedence(c.ScopeType)
		old, ok := selected[c.Slug]
		if !ok || c.prec > old.prec || (c.prec == old.prec && c.Version > old.Version) {
			selected[c.Slug] = c
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ResolvedSkill, 0, len(selected))
	for _, c := range selected {
		c.Enabled = c.enabled && c.override != OverrideDisable
		if !includeDisabled && !c.Enabled {
			continue
		}
		tools, err := s.RequiredTools(ctx, c.SkillID, c.Version)
		if err != nil {
			return nil, err
		}
		c.RequiredTools = tools
		c.MissingTools = missingTools(tools, rc)
		c.Executable = c.Enabled && len(c.MissingTools) == 0
		out = append(out, c.ResolvedSkill)
	}
	sortResolved(out)
	return out, nil
}

func (s *Store) Search(ctx context.Context, rc ResolveContext, query string, limit int) ([]ResolvedSkill, error) {
	all, err := s.Resolve(ctx, rc)
	if err != nil {
		return nil, err
	}
	return filterSkillList(all, query, limit), nil
}

func (s *Store) ListInstalled(ctx context.Context, rc ResolveContext, query string, limit int) ([]ResolvedSkill, error) {
	all, err := s.resolve(ctx, rc, true)
	if err != nil {
		return nil, err
	}
	return filterSkillList(all, query, limit), nil
}

func filterSkillList(all []ResolvedSkill, query string, limit int) []ResolvedSkill {
	query = strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	out := make([]ResolvedSkill, 0, len(all))
	for _, skill := range all {
		if query != "" && !strings.Contains(strings.ToLower(skill.Slug+" "+skill.Name+" "+skill.Description+" "+skill.ContentMarkdown), query) {
			continue
		}
		// Search/list returns metadata only.
		skill.ContentMarkdown = ""
		out = append(out, skill)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Store) GetVisible(ctx context.Context, rc ResolveContext, skillID string) (ResolvedSkill, error) {
	all, err := s.Resolve(ctx, rc)
	if err != nil {
		return ResolvedSkill{}, err
	}
	skillID = strings.TrimSpace(skillID)
	for _, skill := range all {
		if skill.SkillID == skillID || skill.Slug == skillID {
			return skill, nil
		}
	}
	return ResolvedSkill{}, sql.ErrNoRows
}

func (s *Store) GetInstalled(ctx context.Context, rc ResolveContext, skillID string) (ResolvedSkill, error) {
	all, err := s.resolve(ctx, rc, true)
	if err != nil {
		return ResolvedSkill{}, err
	}
	skillID = strings.TrimSpace(skillID)
	for _, skill := range all {
		if skill.SkillID == skillID || skill.Slug == skillID {
			return skill, nil
		}
	}
	return ResolvedSkill{}, sql.ErrNoRows
}

func (s *Store) RequiredTools(ctx context.Context, skillID, version string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tool_name FROM skill_tool_requirements WHERE skill_id=? AND version=? AND required=1`, strings.TrimSpace(skillID), strings.TrimSpace(version))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tools []string
	for rows.Next() {
		var tool string
		if err := rows.Scan(&tool); err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return normalizeToolNames(tools), nil
}

func RequiredToolsJSON(tools []string) string {
	raw, _ := json.Marshal(normalizeToolNames(tools))
	return string(raw)
}

func RequiredToolsFromJSON(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var tools []string
	if err := json.Unmarshal([]byte(raw), &tools); err != nil {
		return nil, err
	}
	return normalizeToolNames(tools), nil
}

func (s *Store) DiscardDraft(ctx context.Context, draftID string) error {
	_, err := s.DiscardDraftWithAudit(ctx, draftID, MutationActor{}, "discard draft")
	return err
}

func (s *Store) DiscardDraftWithAudit(ctx context.Context, draftID string, actor MutationActor, reason string) (MutationEvent, error) {
	if s == nil || s.db == nil {
		return MutationEvent{}, fmt.Errorf("skills store is unavailable")
	}
	draft, err := s.GetDraft(ctx, draftID)
	if err != nil {
		return MutationEvent{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MutationEvent{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE skill_drafts SET status=? WHERE draft_id=? AND status=?`, StatusRejected, strings.TrimSpace(draftID), StatusDraft)
	if err != nil {
		return MutationEvent{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return MutationEvent{}, err
	}
	if n == 0 {
		return MutationEvent{}, fmt.Errorf("draft %s is not an active draft", strings.TrimSpace(draftID))
	}
	ev := MutationEvent{
		EventType:       "skill_discarded",
		Action:          "discard",
		SkillID:         firstNonEmpty(draft.ProposedSkillID, draft.ProposedSlug),
		DraftID:         draft.DraftID,
		ScopeType:       draft.ProposedScopeType,
		GuildID:         draft.GuildID,
		ChannelID:       draft.ChannelID,
		TargetChannelID: firstNonEmpty(actor.TargetChannelID, draft.ChannelID),
		ProjectCWDHash:  draft.ProjectCWDHash,
		ActorUserID:     actor.ActorUserID,
		ActorUsername:   actor.ActorUsername,
		SourceMessageID: actor.SourceMessageID,
		InteractionID:   actor.InteractionID,
		AgentSessionID:  actor.AgentSessionID,
		MCPServerName:   actor.MCPServerName,
		MCPToolName:     actor.MCPToolName,
		Reason:          reason,
		StatusBefore:    StatusDraft,
		StatusAfter:     StatusRejected,
		VersionAfter:    draft.ProposedVersion,
		ContentSHAAfter: ContentSHA256(draft.ProposedContentMarkdown),
		ResultStatus:    "ok",
		MetadataJSON:    "{}",
	}
	if err := s.recordMutationTx(ctx, tx, ev); err != nil {
		return MutationEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return MutationEvent{}, err
	}
	return s.lastMutationEvent(ctx, ev.SkillID)
}

func (s *Store) RecordUsage(ctx context.Context, ev UsageEvent) (UsageEvent, error) {
	if s == nil || s.db == nil {
		return UsageEvent{}, fmt.Errorf("skills store is unavailable")
	}
	now := time.Now().UTC()
	ev.UsageID = strings.TrimSpace(ev.UsageID)
	if ev.UsageID == "" {
		ev.UsageID = "use_" + uuid.NewString()
	}
	ev.SkillID = strings.TrimSpace(ev.SkillID)
	ev.Version = strings.TrimSpace(ev.Version)
	ev.GuildID = strings.TrimSpace(ev.GuildID)
	ev.ChannelID = strings.TrimSpace(ev.ChannelID)
	ev.ThreadID = strings.TrimSpace(ev.ThreadID)
	if ev.ProjectCWDHash == "" {
		ev.ProjectCWDHash = ProjectCWDHash("")
	}
	ev.MessageID = strings.TrimSpace(ev.MessageID)
	ev.AgentSessionID = strings.TrimSpace(ev.AgentSessionID)
	ev.SelectedBy = strings.TrimSpace(ev.SelectedBy)
	if ev.SelectedBy == "" {
		ev.SelectedBy = "agent"
	}
	if ev.UsedAt.IsZero() {
		ev.UsedAt = now
	}
	if ev.SkillID == "" || ev.Version == "" {
		return UsageEvent{}, fmt.Errorf("usage requires skill_id and version")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO skill_usage_events(usage_id, skill_id, version, guild_id, channel_id, thread_id, project_cwd_hash, message_id, agent_session_id, selected_by, used_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, ev.UsageID, ev.SkillID, ev.Version, ev.GuildID, ev.ChannelID, ev.ThreadID, ev.ProjectCWDHash, ev.MessageID, ev.AgentSessionID, ev.SelectedBy, formatTime(ev.UsedAt))
	if err != nil {
		return UsageEvent{}, err
	}
	return ev, nil
}

func normalizeResolveContext(rc ResolveContext) ResolveContext {
	rc.GuildID = strings.TrimSpace(rc.GuildID)
	rc.ChannelID = strings.TrimSpace(rc.ChannelID)
	rc.ParentChannelID = strings.TrimSpace(rc.ParentChannelID)
	rc.TargetID = strings.TrimSpace(rc.TargetID)
	rc.ProjectCWD = strings.TrimSpace(rc.ProjectCWD)
	if rc.ProjectCWDHash == "" {
		rc.ProjectCWDHash = ProjectCWDHash(rc.ProjectCWD)
	}
	rc.EffectiveTools = normalizeToolNames(rc.EffectiveTools)
	rc.RuntimeTools = normalizeToolNames(rc.RuntimeTools)
	return rc
}

func scopeMatches(scope, guildID, channelID, cwdHash string, rc ResolveContext) bool {
	switch NormalizeScope(scope) {
	case ScopeGuild:
		return guildID != "" && guildID == rc.GuildID
	case ScopeChannel:
		return guildID != "" && channelID != "" && guildID == rc.GuildID && channelMatchesContext(channelID, rc)
	case ScopeProject:
		return cwdHash != "" && cwdHash == rc.ProjectCWDHash
	case ScopeChannelProject:
		return guildID != "" && channelID != "" && cwdHash != "" && guildID == rc.GuildID && channelMatchesContext(channelID, rc) && cwdHash == rc.ProjectCWDHash
	default:
		return false
	}
}

func channelMatchesContext(installedChannelID string, rc ResolveContext) bool {
	if installedChannelID == "" {
		return false
	}
	if installedChannelID == rc.ChannelID {
		return true
	}
	return rc.ParentChannelID != "" && installedChannelID == rc.ParentChannelID
}

func missingTools(required []string, rc ResolveContext) []string {
	if len(required) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(rc.EffectiveTools))
	for _, tool := range rc.EffectiveTools {
		allowed[tool] = true
	}
	runtime := make(map[string]bool, len(rc.RuntimeTools))
	for _, tool := range rc.RuntimeTools {
		runtime[strings.ToLower(strings.TrimSpace(tool))] = true
	}
	var missing []string
	for _, tool := range required {
		if runtimeToolName(tool) {
			if !runtimeToolSatisfied(tool, runtime) {
				missing = append(missing, tool)
			}
			continue
		}
		if rc.ReadOnlyPolicy && destructiveToolName(tool) {
			missing = append(missing, tool)
			continue
		}
		if !rc.AllowAllTools && !allowed[tool] {
			missing = append(missing, tool)
		}
	}
	return normalizeToolNames(missing)
}

var (
	defaultRuntimeToolCapabilitiesOnce sync.Once
	defaultRuntimeToolCapabilities     []string
)

func DefaultRuntimeToolCapabilities() []string {
	defaultRuntimeToolCapabilitiesOnce.Do(func() {
		defaultRuntimeToolCapabilities = runtimeToolCapabilities(exec.LookPath)
	})
	return append([]string(nil), defaultRuntimeToolCapabilities...)
}

var runtimeToolCandidates = []string{"sh", "bash", "zsh", "curl", "python", "python3", "node", "npm", "git", "go"}

func runtimeToolCapabilities(lookup func(string) (string, error)) []string {
	seen := map[string]bool{}
	var tools []string
	for _, candidate := range runtimeToolCandidates {
		if _, err := lookup(candidate); err != nil {
			continue
		}
		seen[candidate] = true
		tools = append(tools, candidate)
	}
	if seen["sh"] || seen["bash"] || seen["zsh"] {
		tools = append(tools, "shell")
	}
	if seen["python"] || seen["python3"] {
		tools = append(tools, "python")
	}
	return normalizeToolNames(tools)
}

func runtimeToolName(tool string) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	if tool == "shell" {
		return true
	}
	for _, candidate := range runtimeToolCandidates {
		if tool == candidate {
			return true
		}
	}
	return false
}

func runtimeToolSatisfied(tool string, runtime map[string]bool) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	if tool == "" {
		return true
	}
	if runtime[tool] {
		return true
	}
	switch tool {
	case "shell":
		return runtime["shell"] || runtime["bash"] || runtime["sh"] || runtime["zsh"]
	case "python":
		return runtime["python"] || runtime["python3"]
	default:
		return false
	}
}

func destructiveToolName(tool string) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	for _, token := range []string{"write", "delete", "remove", "clear", "send", "create", "update", "install", "uninstall", "apply", "bash", "shell"} {
		if strings.Contains(tool, token) {
			return true
		}
	}
	return false
}

func sortResolved(skills []ResolvedSkill) {
	for i := 1; i < len(skills); i++ {
		for j := i; j > 0 && skills[j-1].Slug > skills[j].Slug; j-- {
			skills[j-1], skills[j] = skills[j], skills[j-1]
		}
	}
}

func riskLevelFromJSON(raw string) string {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err == nil {
		if v, ok := data["risk_level"].(string); ok {
			return normalizeRisk(v)
		}
	}
	return "low"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func intBool(v int) bool { return v != 0 }

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(sqliteTimeFormat)
}

func formatOptionalTime(t time.Time) string { return formatTime(t) }

func parseTime(s string) time.Time {
	if strings.TrimSpace(s) == "" {
		return time.Time{}
	}
	t, _ := time.Parse(sqliteTimeFormat, strings.TrimSpace(s))
	return t
}
