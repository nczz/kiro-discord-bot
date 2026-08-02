package skills

import "time"

const (
	ScopeGuild          = "guild"
	ScopeChannel        = "channel"
	ScopeProject        = "project"
	ScopeChannelProject = "channel_project"

	SourceConversation = "conversation"
	SourceMarkdown     = "markdown"
	SourceURL          = "url"
	SourceGitHubRepo   = "github_repo"
	SourceManual       = "manual"
	SourceBuiltin      = "builtin"

	StatusActive    = "active"
	StatusDraft     = "draft"
	StatusInstalled = "installed"
	StatusDisabled  = "disabled"
	StatusRemoved   = "removed"
	StatusRejected  = "rejected"
	StatusExpired   = "expired"

	OverrideInherit  = "inherit"
	OverrideOverride = "override"
	OverrideDisable  = "disable"
)

type Skill struct {
	SkillID        string    `json:"skill_id"`
	CanonicalSlug  string    `json:"canonical_slug"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	CurrentVersion string    `json:"current_version"`
	SourceType     string    `json:"source_type"`
	SourceRef      string    `json:"source_ref,omitempty"`
	RiskLevel      string    `json:"risk_level"`
	Status         string    `json:"status"`
	CreatedBy      string    `json:"created_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SkillVersion struct {
	SkillID         string    `json:"skill_id"`
	Version         string    `json:"version"`
	ContentMarkdown string    `json:"content_markdown"`
	ContentSHA256   string    `json:"content_sha256"`
	MetadataJSON    string    `json:"metadata_json"`
	CreatedBy       string    `json:"created_by,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type Install struct {
	InstallID          string    `json:"install_id"`
	SkillID            string    `json:"skill_id"`
	Version            string    `json:"version"`
	ScopeType          string    `json:"scope_type"`
	GuildID            string    `json:"guild_id,omitempty"`
	ChannelID          string    `json:"channel_id,omitempty"`
	ProjectCWDHash     string    `json:"project_cwd_hash,omitempty"`
	ProjectCWD         string    `json:"project_cwd,omitempty"`
	Enabled            bool      `json:"enabled"`
	OverridePolicy     string    `json:"override_policy"`
	MaterializedPath   string    `json:"materialized_path,omitempty"`
	MaterializedSHA256 string    `json:"materialized_sha256,omitempty"`
	InstalledBy        string    `json:"installed_by,omitempty"`
	InstalledAt        time.Time `json:"installed_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Draft struct {
	DraftID                 string    `json:"draft_id"`
	ProposedSkillID         string    `json:"proposed_skill_id,omitempty"`
	ProposedSlug            string    `json:"proposed_slug"`
	ProposedName            string    `json:"proposed_name"`
	ProposedDescription     string    `json:"proposed_description,omitempty"`
	ProposedVersion         string    `json:"proposed_version"`
	ProposedScopeType       string    `json:"proposed_scope_type"`
	GuildID                 string    `json:"guild_id,omitempty"`
	ChannelID               string    `json:"channel_id,omitempty"`
	ProjectCWDHash          string    `json:"project_cwd_hash,omitempty"`
	ProjectCWD              string    `json:"project_cwd,omitempty"`
	SourceType              string    `json:"source_type"`
	SourceRef               string    `json:"source_ref,omitempty"`
	SourceMessageRefsJSON   string    `json:"source_message_refs_json"`
	ProposedContentMarkdown string    `json:"proposed_content_markdown"`
	RequiredToolsJSON       string    `json:"required_tools_json"`
	RiskReportJSON          string    `json:"risk_report_json"`
	Status                  string    `json:"status"`
	CreatedBy               string    `json:"created_by,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
	ExpiresAt               time.Time `json:"expires_at,omitempty"`
}

type ToolRequirement struct {
	SkillID         string `json:"skill_id"`
	Version         string `json:"version"`
	ToolName        string `json:"tool_name"`
	Required        bool   `json:"required"`
	MinVersion      string `json:"min_version,omitempty"`
	PermissionLevel string `json:"permission_level"`
}

type ResolveContext struct {
	GuildID         string   `json:"guild_id,omitempty"`
	ChannelID       string   `json:"channel_id,omitempty"`
	ParentChannelID string   `json:"parent_channel_id,omitempty"`
	TargetID        string   `json:"target_id,omitempty"`
	ProjectCWD      string   `json:"project_cwd,omitempty"`
	ProjectCWDHash  string   `json:"project_cwd_hash,omitempty"`
	EffectiveTools  []string `json:"effective_tools,omitempty"`
	AllowAllTools   bool     `json:"allow_all_tools,omitempty"`
	ReadOnlyPolicy  bool     `json:"read_only_policy,omitempty"`
}

type ResolvedSkill struct {
	SkillID          string   `json:"skill_id"`
	Slug             string   `json:"slug"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Version          string   `json:"version"`
	ScopeType        string   `json:"scope_type"`
	RiskLevel        string   `json:"risk_level"`
	ContentMarkdown  string   `json:"content_markdown,omitempty"`
	RequiredTools    []string `json:"required_tools"`
	MissingTools     []string `json:"missing_tools"`
	Executable       bool     `json:"executable"`
	MaterializedPath string   `json:"materialized_path,omitempty"`
}

type UsageEvent struct {
	UsageID        string    `json:"usage_id"`
	SkillID        string    `json:"skill_id"`
	Version        string    `json:"version"`
	GuildID        string    `json:"guild_id,omitempty"`
	ChannelID      string    `json:"channel_id,omitempty"`
	ThreadID       string    `json:"thread_id,omitempty"`
	ProjectCWDHash string    `json:"project_cwd_hash,omitempty"`
	MessageID      string    `json:"message_id,omitempty"`
	AgentSessionID string    `json:"agent_session_id,omitempty"`
	SelectedBy     string    `json:"selected_by"`
	UsedAt         time.Time `json:"used_at"`
}

type MutationActor struct {
	GuildID          string `json:"guild_id,omitempty"`
	ChannelID        string `json:"channel_id,omitempty"`
	TargetChannelID  string `json:"target_channel_id,omitempty"`
	ProjectCWDHash   string `json:"project_cwd_hash,omitempty"`
	ActorUserID      string `json:"actor_user_id,omitempty"`
	ActorUsername    string `json:"actor_username,omitempty"`
	SourceMessageID  string `json:"source_message_id,omitempty"`
	InteractionID    string `json:"source_interaction_id,omitempty"`
	AgentSessionID   string `json:"agent_session_id,omitempty"`
	MCPServerName    string `json:"mcp_server_name,omitempty"`
	MCPToolName      string `json:"mcp_tool_name,omitempty"`
	CanManageChannel bool   `json:"can_manage_channel,omitempty"`
	CanManageGuild   bool   `json:"can_manage_guild,omitempty"`
}

type MutationEvent struct {
	EventID            string    `json:"event_id"`
	EventType          string    `json:"event_type"`
	Action             string    `json:"action"`
	SkillID            string    `json:"skill_id,omitempty"`
	InstallID          string    `json:"install_id,omitempty"`
	DraftID            string    `json:"draft_id,omitempty"`
	ScopeType          string    `json:"scope_type,omitempty"`
	GuildID            string    `json:"guild_id,omitempty"`
	ChannelID          string    `json:"channel_id,omitempty"`
	TargetChannelID    string    `json:"target_channel_id,omitempty"`
	ProjectCWDHash     string    `json:"project_cwd_hash,omitempty"`
	ActorUserID        string    `json:"actor_user_id,omitempty"`
	ActorUsername      string    `json:"actor_username,omitempty"`
	SourceMessageID    string    `json:"source_message_id,omitempty"`
	InteractionID      string    `json:"source_interaction_id,omitempty"`
	AgentSessionID     string    `json:"agent_session_id,omitempty"`
	MCPServerName      string    `json:"mcp_server_name,omitempty"`
	MCPToolName        string    `json:"mcp_tool_name,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	StatusBefore       string    `json:"status_before,omitempty"`
	StatusAfter        string    `json:"status_after,omitempty"`
	VersionBefore      string    `json:"version_before,omitempty"`
	VersionAfter       string    `json:"version_after,omitempty"`
	ContentSHABefore   string    `json:"content_sha_before,omitempty"`
	ContentSHAAfter    string    `json:"content_sha_after,omitempty"`
	MaterializedPath   string    `json:"materialized_path,omitempty"`
	MaterializedSHA256 string    `json:"materialized_sha256,omitempty"`
	ResultStatus       string    `json:"result_status"`
	ErrorCode          string    `json:"error_code,omitempty"`
	ErrorMessage       string    `json:"error_message,omitempty"`
	MetadataJSON       string    `json:"metadata_json,omitempty"`
	PreviousEventHash  string    `json:"previous_event_hash,omitempty"`
	EventHash          string    `json:"event_hash"`
	OccurredAt         time.Time `json:"occurred_at"`
}
