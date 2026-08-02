package botmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/nczz/kiro-discord-bot/internal/skills"
)

const (
	ToolSkillsSearch                    = "bot_skills_search"
	ToolSkillsEffectiveList             = "bot_skills_effective_list"
	ToolSkillGet                        = "bot_skill_get"
	ToolSkillUsageRecord                = "bot_skill_usage_record"
	ToolSkillCreateDraft                = "bot_skill_create_draft"
	ToolSkillPreviewDraft               = "bot_skill_preview_draft"
	ToolSkillInstallDraft               = "bot_skill_install_draft"
	ToolSkillDiscardDraft               = "bot_skill_discard_draft"
	ToolSkillsChannelEnable             = "bot_skills_channel_enable"
	ToolSkillsChannelDisable            = "bot_skills_channel_disable"
	ToolSkillsChannelRemove             = "bot_skills_channel_remove"
	ToolSkillsChannelRestore            = "bot_skills_channel_restore"
	ToolSkillsChannelRollback           = "bot_skills_channel_rollback"
	ToolSkillsServerSearch              = "bot_skills_server_search"
	ToolSkillsServerGet                 = "bot_skills_server_get"
	ToolSkillsServerInventory           = "bot_skills_server_inventory"
	ToolSkillsServerEffectiveForChannel = "bot_skills_server_effective_for_channel"
	ToolSkillsServerDisable             = "bot_skills_server_disable"
	ToolSkillsServerRemove              = "bot_skills_server_remove"
	ToolSkillsServerRestore             = "bot_skills_server_restore"
	ToolSkillsServerRollback            = "bot_skills_server_rollback"
)

func registerSkillTools(s *server.MCPServer) {
	s.AddTool(skillSearchTool(ToolSkillsSearch, "Search effective scoped skills for the current Discord guild/channel/project without returning full skill content. Use before applying a reusable skill."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillSearch(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillSearchTool(ToolSkillsEffectiveList, "List effective scoped skills for the current Discord guild/channel/project without returning full skill content."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillEffectiveList(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillGetTool(), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillGet(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillUsageTool(), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillUsageRecord(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillCreateDraftTool(), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillCreateDraft(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillPreviewDraftTool(), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillPreviewDraft(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillInstallDraftTool(), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillInstallDraft(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillDiscardDraftTool(), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillDiscardDraft(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillChannelLifecycleTool(ToolSkillsChannelEnable, "Enable a previously installed channel/project skill for this authenticated Discord channel context."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillChannelSetEnabled(ctx, dataDir(), req, true, "enable")
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillChannelLifecycleTool(ToolSkillsChannelDisable, "Disable an installed channel/project skill for this authenticated Discord channel context. This is reversible."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillChannelSetEnabled(ctx, dataDir(), req, false, "disable")
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillChannelLifecycleTool(ToolSkillsChannelRemove, "Soft-remove an installed channel/project skill for this authenticated Discord channel context. This is reversible through restore."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillChannelSetEnabled(ctx, dataDir(), req, false, "remove")
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillChannelLifecycleTool(ToolSkillsChannelRestore, "Restore a disabled or removed channel/project skill for this authenticated Discord channel context."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillChannelSetEnabled(ctx, dataDir(), req, true, "restore")
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillChannelRollbackTool(), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillChannelRollback(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillServerSearchTool(ToolSkillsServerSearch, "Search Discord server-wide skills without returning full content."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillServerSearch(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillServerGetTool(), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillServerGet(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillServerSearchTool(ToolSkillsServerInventory, "List Discord server-wide skills without full content."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillServerInventory(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillSearchTool(ToolSkillsServerEffectiveForChannel, "List skills effective for a specific channel in this Discord server without returning full content."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillEffectiveList(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillChannelLifecycleTool(ToolSkillsServerDisable, "Disable a Discord server-wide skill. Requires authenticated Discord server management context."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillServerSetEnabled(ctx, dataDir(), req, false, "disable")
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillChannelLifecycleTool(ToolSkillsServerRemove, "Soft-remove a Discord server-wide skill. Requires authenticated Discord server management context."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillServerSetEnabled(ctx, dataDir(), req, false, "remove")
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillChannelLifecycleTool(ToolSkillsServerRestore, "Restore a Discord server-wide skill. Requires authenticated Discord server management context."), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillServerSetEnabled(ctx, dataDir(), req, true, "restore")
		return skillJSONResult(result, err), nil
	})
	s.AddTool(skillServerRollbackTool(), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := skillServerRollback(ctx, dataDir(), req)
		return skillJSONResult(result, err), nil
	})
}

func skillSearchTool(name, desc string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(desc),
		mcp.WithString("query", mcp.Description("Optional search query. Omit to list all effective skills.")),
		mcp.WithString("guild_id", mcp.Description("Discord guild/server ID. Defaults to bound bot-tools guild.")),
		mcp.WithString("channel_id", mcp.Description("Discord parent channel ID. Defaults to bound bot-tools parent channel.")),
		mcp.WithString("project_cwd", mcp.Description("Validated project working directory for project-scoped skill resolution. Do not pass bot DATA_DIR paths.")),
		mcp.WithNumber("limit", mcp.Description("Maximum results, 1-50. Defaults to 10.")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillGetTool() mcp.Tool {
	return mcp.NewTool(ToolSkillGet,
		mcp.WithDescription("Read one effective scoped skill's full SKILL.md content after scope visibility and tool requirement resolution."),
		mcp.WithString("skill_id", mcp.Required(), mcp.Description("Skill ID or slug returned by bot_skills_search.")),
		mcp.WithString("guild_id", mcp.Description("Discord guild/server ID. Defaults to bound bot-tools guild.")),
		mcp.WithString("channel_id", mcp.Description("Discord parent channel ID. Defaults to bound bot-tools parent channel.")),
		mcp.WithString("project_cwd", mcp.Description("Validated project working directory for project-scoped skill resolution. Do not pass bot DATA_DIR paths.")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillServerSearchTool(name, desc string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(desc),
		mcp.WithString("query", mcp.Description("Optional search query. Omit to list server skills.")),
		mcp.WithString("guild_id", mcp.Description("Discord guild/server ID. Defaults to bound bot-tools guild.")),
		mcp.WithNumber("limit", mcp.Description("Maximum results, 1-50. Defaults to 10.")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillServerGetTool() mcp.Tool {
	return mcp.NewTool(ToolSkillsServerGet,
		mcp.WithDescription("Read one visible Discord server-wide skill's full SKILL.md content."),
		mcp.WithString("skill_id", mcp.Required(), mcp.Description("Server skill ID or slug returned by bot_skills_server_search.")),
		mcp.WithString("guild_id", mcp.Description("Discord guild/server ID. Defaults to bound bot-tools guild.")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillUsageTool() mcp.Tool {
	return mcp.NewTool(ToolSkillUsageRecord,
		mcp.WithDescription("Record that the agent used a specific skill version for audit. This does not install, update, or enable skills."),
		mcp.WithString("skill_id", mcp.Required(), mcp.Description("Skill ID used.")),
		mcp.WithString("version", mcp.Required(), mcp.Description("Skill version used.")),
		mcp.WithString("guild_id", mcp.Description("Discord guild/server ID. Defaults to bound bot-tools guild.")),
		mcp.WithString("channel_id", mcp.Description("Discord parent channel ID. Defaults to bound bot-tools parent channel.")),
		mcp.WithString("thread_id", mcp.Description("Optional Discord thread target ID.")),
		mcp.WithString("project_cwd", mcp.Description("Project working directory used for project-scoped audit hashing. Not echoed back.")),
		mcp.WithString("message_id", mcp.Description("Discord message ID for correlation.")),
		mcp.WithString("agent_session_id", mcp.Description("Agent session ID for correlation.")),
		mcp.WithString("selected_by", mcp.Description("agent or user. Defaults to agent.")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillDraftTool(name, desc string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(desc),
		mcp.WithString("name", mcp.Required(), mcp.Description("Human-readable skill name.")),
		mcp.WithString("slug", mcp.Description("Optional lowercase skill slug. If omitted, derived from name.")),
		mcp.WithString("description", mcp.Description("Short skill description.")),
		mcp.WithString("scope_type", mcp.Required(), mcp.Description("guild, channel, project, or channel_project.")),
		mcp.WithString("guild_id", mcp.Description("Discord guild/server ID. Defaults to bound bot-tools guild.")),
		mcp.WithString("channel_id", mcp.Description("Discord parent channel ID. Defaults to bound bot-tools parent channel.")),
		mcp.WithString("project_cwd", mcp.Description("Project CWD for project/channel_project scope. Must pass allowed roots during install.")),
		mcp.WithString("content_markdown", mcp.Required(), mcp.Description("Agent-curated clean skill markdown. Inspect external URLs/Gists/repos yourself first; do not pass raw HTML or unreviewed page source.")),
		mcp.WithString("required_tools", mcp.Description("Comma-separated or JSON array MCP tool names required by this skill.")),
		mcp.WithString("source_type", mcp.Description("Optional audit provenance: conversation, markdown, url, github_repo, or manual. Defaults to conversation.")),
		mcp.WithString("source_ref", mcp.Description("Optional source reference, such as a Discord message range, URL, Gist, or repository.")),
		mcp.WithString("source_message_ids", mcp.Description("Optional JSON array or comma-separated Discord message IDs used as source.")),
		mcp.WithString("risk_level", mcp.Description("low, medium, high, or critical. Defaults to low.")),
		mcp.WithString("requested_by", mcp.Required(), mcp.Description("Requester identity from Discord context for audit.")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillCreateDraftTool() mcp.Tool {
	return skillDraftTool(ToolSkillCreateDraft, "Create one inactive skill draft from agent-curated markdown. Use this for every user request to create/install a skill, including URLs, Gists, repositories, files, and prior conversation: inspect sources with normal agent tools first, then submit only the clean final skill markdown. This tool never fetches URLs, executes source content, installs, or enables the skill.")
}

func skillPreviewDraftTool() mcp.Tool {
	return mcp.NewTool(ToolSkillPreviewDraft,
		mcp.WithDescription("Preview an inactive skill draft by ID without installing it."),
		mcp.WithString("draft_id", mcp.Required(), mcp.Description("Draft ID returned by bot_skill_create_draft.")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillInstallDraftTool() mcp.Tool {
	return mcp.NewTool(ToolSkillInstallDraft,
		mcp.WithDescription("Install a previously reviewed skill draft after explicit human confirmation. This never grants missing MCP tools."),
		mcp.WithString("draft_id", mcp.Required(), mcp.Description("Draft ID to install.")),
		mcp.WithString("confirmed_by", mcp.Required(), mcp.Description("Discord user identity that confirmed the install.")),
		mcp.WithBoolean("manage_channels", mcp.Required(), mcp.Description("Server-provided permission result; must be true for channel/project/channel_project install in MCP-only flows.")),
		mcp.WithBoolean("manage_guild", mcp.Description("Server-provided permission result; must be true for guild install in MCP-only flows.")),
		mcp.WithBoolean("overwrite_materialized", mcp.Description("Set true only when the user confirmed replacing a drifted project-local SKILL.md.")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillDiscardDraftTool() mcp.Tool {
	return mcp.NewTool(ToolSkillDiscardDraft,
		mcp.WithDescription("Reject an inactive skill draft. This does not remove installed skills."),
		mcp.WithString("draft_id", mcp.Required(), mcp.Description("Draft ID to reject.")),
		mcp.WithString("confirmed_by", mcp.Required(), mcp.Description("Discord user identity that rejected the draft.")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillChannelLifecycleTool(name, desc string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(desc),
		mcp.WithString("skill_id", mcp.Required(), mcp.Description("Installed skill ID or slug.")),
		mcp.WithString("scope_type", mcp.Description("channel or channel_project. Defaults to channel_project when project_cwd is present, otherwise channel.")),
		mcp.WithString("reason", mcp.Description("Short user-visible reason for the change.")),
		mcp.WithString("project_cwd", mcp.Description("Project CWD for channel_project scope. Defaults to bound bot-tools project CWD.")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillChannelRollbackTool() mcp.Tool {
	return mcp.NewTool(ToolSkillsChannelRollback,
		mcp.WithDescription("Rollback an installed channel/project skill to an existing version for this authenticated Discord channel context."),
		mcp.WithString("skill_id", mcp.Required(), mcp.Description("Installed skill ID or slug.")),
		mcp.WithString("version", mcp.Required(), mcp.Description("Existing version to restore.")),
		mcp.WithString("scope_type", mcp.Description("channel or channel_project. Defaults to channel_project when project_cwd is present, otherwise channel.")),
		mcp.WithString("reason", mcp.Description("Short user-visible reason for the rollback.")),
		mcp.WithString("project_cwd", mcp.Description("Project CWD for channel_project scope. Defaults to bound bot-tools project CWD.")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillServerRollbackTool() mcp.Tool {
	return mcp.NewTool(ToolSkillsServerRollback,
		mcp.WithDescription("Rollback a Discord server-wide skill to an existing version. Requires authenticated Discord server management context."),
		mcp.WithString("skill_id", mcp.Required(), mcp.Description("Installed server skill ID or slug.")),
		mcp.WithString("version", mcp.Required(), mcp.Description("Existing version to restore.")),
		mcp.WithString("reason", mcp.Description("Short user-visible reason for the rollback.")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func skillSearch(ctx context.Context, dataDir string, req mcp.CallToolRequest) (map[string]any, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	results, err := store.Search(ctx, skillResolveContext(req), req.GetString("query", ""), req.GetInt("limit", 10))
	if err != nil {
		return nil, err
	}
	return map[string]any{"results": sanitizeSkillResults(results)}, nil
}

func skillEffectiveList(ctx context.Context, dataDir string, req mcp.CallToolRequest) (map[string]any, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	results, err := store.Resolve(ctx, skillResolveContext(req))
	if err != nil {
		return nil, err
	}
	for i := range results {
		results[i].ContentMarkdown = ""
	}
	return map[string]any{"results": sanitizeSkillResults(results)}, nil
}

func skillGet(ctx context.Context, dataDir string, req mcp.CallToolRequest) (skills.ResolvedSkill, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return skills.ResolvedSkill{}, err
	}
	defer store.Close()
	return store.GetVisible(ctx, skillResolveContext(req), req.GetString("skill_id", ""))
}

func skillUsageRecord(ctx context.Context, dataDir string, req mcp.CallToolRequest) (skills.UsageEvent, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return skills.UsageEvent{}, err
	}
	defer store.Close()
	rc := skillResolveContext(req)
	return store.RecordUsage(ctx, skills.UsageEvent{SkillID: req.GetString("skill_id", ""), Version: req.GetString("version", ""), GuildID: rc.GuildID, ChannelID: rc.ChannelID, ThreadID: req.GetString("thread_id", ""), ProjectCWDHash: rc.ProjectCWDHash, MessageID: req.GetString("message_id", ""), AgentSessionID: req.GetString("agent_session_id", ""), SelectedBy: req.GetString("selected_by", "")})
}

func skillCreateDraft(ctx context.Context, dataDir string, req mcp.CallToolRequest) (skills.Draft, error) {
	sourceType := strings.TrimSpace(req.GetString("source_type", ""))
	if sourceType == "" {
		sourceType = skills.SourceConversation
	} else {
		sourceType = skills.NormalizeSourceType(sourceType)
	}
	return skillDraft(ctx, dataDir, req, sourceType)
}

func skillDraft(ctx context.Context, dataDir string, req mcp.CallToolRequest, sourceType string) (skills.Draft, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return skills.Draft{}, err
	}
	defer store.Close()
	rc := skillResolveContext(req)
	draft, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: req.GetString("name", ""), Slug: req.GetString("slug", ""), Description: req.GetString("description", ""), ScopeType: req.GetString("scope_type", ""), GuildID: rc.GuildID, ChannelID: rc.ChannelID, ProjectCWD: strings.TrimSpace(req.GetString("project_cwd", "")), SourceType: sourceType, SourceRef: req.GetString("source_ref", ""), SourceMessageRefs: parseStringList(req.GetString("source_message_ids", "")), ContentMarkdown: req.GetString("content_markdown", ""), RequiredTools: parseStringList(req.GetString("required_tools", "")), RiskLevel: req.GetString("risk_level", ""), CreatedBy: req.GetString("requested_by", ""), TTL: skillDraftTTL()})
	if err != nil {
		return skills.Draft{}, err
	}
	return store.CreateDraft(ctx, draft)
}

func skillPreviewDraft(ctx context.Context, dataDir string, req mcp.CallToolRequest) (skills.Draft, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return skills.Draft{}, err
	}
	defer store.Close()
	return store.GetDraft(ctx, req.GetString("draft_id", ""))
}

func skillInstallDraft(ctx context.Context, dataDir string, req mcp.CallToolRequest) (map[string]any, error) {
	_ = ctx
	_ = dataDir
	_ = req
	return nil, fmt.Errorf("a channel manager must confirm installation from the visible skill draft review message before this draft becomes active")
}

func skillDiscardDraft(ctx context.Context, dataDir string, req mcp.CallToolRequest) (map[string]any, error) {
	_ = ctx
	_ = dataDir
	_ = req
	return nil, fmt.Errorf("a channel manager must confirm discard from the visible skill draft review message before this draft is rejected")
}

func skillChannelSetEnabled(ctx context.Context, dataDir string, req mcp.CallToolRequest, enabled bool, action string) (map[string]any, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	rc := skillResolveContext(req)
	rc.ProjectCWD = firstNonEmptySkill(req.GetString("project_cwd", ""), rc.ProjectCWD)
	scope := channelSkillScope(req, rc)
	actor, err := authenticatedChannelSkillActor(req, action)
	if err != nil {
		return nil, err
	}
	ev, err := store.SetInstallEnabled(ctx, rc, req.GetString("skill_id", ""), scope, enabled, actor, req.GetString("reason", ""), action)
	if err != nil {
		return nil, err
	}
	return map[string]any{"event": ev, "status": "ok"}, nil
}

func skillChannelRollback(ctx context.Context, dataDir string, req mcp.CallToolRequest) (map[string]any, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	rc := skillResolveContext(req)
	rc.ProjectCWD = firstNonEmptySkill(req.GetString("project_cwd", ""), rc.ProjectCWD)
	scope := channelSkillScope(req, rc)
	actor, err := authenticatedChannelSkillActor(req, "rollback")
	if err != nil {
		return nil, err
	}
	ev, err := store.RollbackInstall(ctx, rc, req.GetString("skill_id", ""), scope, req.GetString("version", ""), actor, req.GetString("reason", ""))
	if err != nil {
		return nil, err
	}
	return map[string]any{"event": ev, "status": "ok"}, nil
}

func channelSkillScope(req mcp.CallToolRequest, rc skills.ResolveContext) string {
	if scope := skills.NormalizeScope(req.GetString("scope_type", "")); scope == skills.ScopeChannel || scope == skills.ScopeChannelProject {
		return scope
	}
	if strings.TrimSpace(rc.ProjectCWD) != "" || strings.TrimSpace(rc.ProjectCWDHash) != "" {
		return skills.ScopeChannelProject
	}
	return skills.ScopeChannel
}

func authenticatedChannelSkillActor(req mcp.CallToolRequest, toolName string) (skills.MutationActor, error) {
	state, ok := currentTargetState()
	if !ok || strings.TrimSpace(state.RequesterID) == "" {
		return skills.MutationActor{}, fmt.Errorf("skill lifecycle tools require authenticated Discord request context")
	}
	if !state.CanManageChannel {
		return skills.MutationActor{}, fmt.Errorf("skill lifecycle tools require Discord channel management permission for the current target")
	}
	return skills.MutationActor{
		GuildID:         firstNonEmptySkill(os.Getenv("BOT_TOOLS_GUILD_ID"), req.GetString("guild_id", "")),
		ChannelID:       firstNonEmptySkill(os.Getenv("BOT_TOOLS_CHANNEL_ID"), req.GetString("channel_id", "")),
		TargetChannelID: firstNonEmptySkill(state.TargetChannelID, os.Getenv("BOT_TOOLS_TARGET_CHANNEL_ID"), req.GetString("target_id", "")),
		ActorUserID:     state.RequesterID,
		ActorUsername:   state.RequesterName,
		SourceMessageID: req.GetString("message_id", ""),
		AgentSessionID:  req.GetString("agent_session_id", ""),
		MCPServerName:   "bot-tools",
		MCPToolName:     toolName,
	}, nil
}

func skillServerResolveContext(req mcp.CallToolRequest) skills.ResolveContext {
	allowed, allowAll := proxyAllowedTools()
	return skills.ResolveContext{
		GuildID:        firstNonEmptySkill(os.Getenv("BOT_TOOLS_GUILD_ID"), req.GetString("guild_id", "")),
		EffectiveTools: allowed,
		AllowAllTools:  allowAll,
	}
}

func skillServerSearch(ctx context.Context, dataDir string, req mcp.CallToolRequest) (map[string]any, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	results, err := store.Search(ctx, skillServerResolveContext(req), req.GetString("query", ""), req.GetInt("limit", 10))
	if err != nil {
		return nil, err
	}
	return map[string]any{"results": sanitizeSkillResults(results)}, nil
}

func skillServerInventory(ctx context.Context, dataDir string, req mcp.CallToolRequest) (map[string]any, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	results, err := store.Resolve(ctx, skillServerResolveContext(req))
	if err != nil {
		return nil, err
	}
	for i := range results {
		results[i].ContentMarkdown = ""
	}
	return map[string]any{"results": sanitizeSkillResults(results)}, nil
}

func skillServerGet(ctx context.Context, dataDir string, req mcp.CallToolRequest) (skills.ResolvedSkill, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return skills.ResolvedSkill{}, err
	}
	defer store.Close()
	return store.GetVisible(ctx, skillServerResolveContext(req), req.GetString("skill_id", ""))
}

func skillServerSetEnabled(ctx context.Context, dataDir string, req mcp.CallToolRequest, enabled bool, action string) (map[string]any, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	actor, err := authenticatedServerSkillActor(req, action)
	if err != nil {
		return nil, err
	}
	rc := skillServerResolveContext(req)
	ev, err := store.SetInstallEnabled(ctx, rc, req.GetString("skill_id", ""), skills.ScopeGuild, enabled, actor, req.GetString("reason", ""), action)
	if err != nil {
		return nil, err
	}
	return map[string]any{"event": ev, "status": "ok"}, nil
}

func skillServerRollback(ctx context.Context, dataDir string, req mcp.CallToolRequest) (map[string]any, error) {
	store, err := openSkillsStore(dataDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	actor, err := authenticatedServerSkillActor(req, ToolSkillsServerRollback)
	if err != nil {
		return nil, err
	}
	rc := skillServerResolveContext(req)
	ev, err := store.RollbackInstall(ctx, rc, req.GetString("skill_id", ""), skills.ScopeGuild, req.GetString("version", ""), actor, req.GetString("reason", ""))
	if err != nil {
		return nil, err
	}
	return map[string]any{"event": ev, "status": "ok"}, nil
}

func authenticatedServerSkillActor(req mcp.CallToolRequest, toolName string) (skills.MutationActor, error) {
	state, ok := currentTargetState()
	if !ok || strings.TrimSpace(state.RequesterID) == "" {
		return skills.MutationActor{}, fmt.Errorf("server skill management requires authenticated Discord request context")
	}
	if !state.CanManageGuild {
		return skills.MutationActor{}, fmt.Errorf("server skill management requires Discord server management permission")
	}
	return skills.MutationActor{
		GuildID:         firstNonEmptySkill(os.Getenv("BOT_TOOLS_GUILD_ID"), req.GetString("guild_id", "")),
		ChannelID:       firstNonEmptySkill(os.Getenv("BOT_TOOLS_CHANNEL_ID"), req.GetString("channel_id", "")),
		TargetChannelID: firstNonEmptySkill(state.TargetChannelID, os.Getenv("BOT_TOOLS_TARGET_CHANNEL_ID"), req.GetString("target_id", "")),
		ActorUserID:     state.RequesterID,
		ActorUsername:   state.RequesterName,
		SourceMessageID: req.GetString("message_id", ""),
		AgentSessionID:  req.GetString("agent_session_id", ""),
		MCPServerName:   "bot-tools",
		MCPToolName:     toolName,
	}, nil
}

func openSkillsStore(dataDir string) (*skills.Store, error) {
	if !skillsEnabled() {
		return nil, fmt.Errorf("skills are disabled")
	}
	if path := strings.TrimSpace(os.Getenv("SKILLS_DB_PATH")); path != "" {
		return skills.OpenPath(path)
	}
	return skills.Open(dataDir)
}

func skillResolveContext(req mcp.CallToolRequest) skills.ResolveContext {
	allowed, allowAll := proxyAllowedTools()
	return skills.ResolveContext{
		GuildID:        firstNonEmptySkill(os.Getenv("BOT_TOOLS_GUILD_ID"), req.GetString("guild_id", "")),
		ChannelID:      firstNonEmptySkill(os.Getenv("BOT_TOOLS_CHANNEL_ID"), req.GetString("channel_id", "")),
		TargetID:       firstNonEmptySkill(os.Getenv("BOT_TOOLS_TARGET_CHANNEL_ID"), req.GetString("target_id", "")),
		ProjectCWD:     firstNonEmptySkill(os.Getenv("BOT_TOOLS_PROJECT_CWD"), req.GetString("project_cwd", "")),
		EffectiveTools: allowed,
		AllowAllTools:  allowAll,
	}
}

func proxyAllowedTools() ([]string, bool) {
	allowAll, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("BOT_TOOLS_CHANNEL_ALLOW_ALL_TOOLS")))
	var tools []string
	if raw := os.Getenv("BOT_TOOLS_CHANNEL_ALLOWED_TOOLS_JSON"); strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &tools)
		normalized, _ := skills.RequiredToolsFromJSON(skills.RequiredToolsJSON(tools))
		return normalized, allowAll
	}
	allowAll, _ = strconv.ParseBool(strings.TrimSpace(os.Getenv("MCP_PROXY_ALLOW_ALL_TOOLS")))
	_ = json.Unmarshal([]byte(os.Getenv("MCP_PROXY_ALLOWED_TOOLS_JSON")), &tools)
	return tools, allowAll
}

func allowedCwdRoots() []string {
	raw := strings.TrimSpace(os.Getenv("ALLOWED_CWD_ROOTS"))
	if raw == "" {
		return nil
	}
	split := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ':' || r == ';' || r == '\n' })
	out := make([]string, 0, len(split))
	for _, item := range split {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return out
}

func authorizeSkillInstall(d skills.Draft, manageChannels, manageGuild bool) error {
	switch d.ProposedScopeType {
	case skills.ScopeGuild:
		if !manageGuild && !manageChannels {
			return fmt.Errorf("guild skill install requires server management permission")
		}
	case skills.ScopeChannel, skills.ScopeProject, skills.ScopeChannelProject:
		if !manageChannels {
			return fmt.Errorf("%s skill install requires channel management permission", d.ProposedScopeType)
		}
	default:
		return fmt.Errorf("unsupported skill scope %q", d.ProposedScopeType)
	}
	return nil
}

func skillJSONResult(v any, err error) *mcp.CallToolResult {
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	return mcp.NewToolResultText(string(raw))
}

func sanitizeSkillResults(results []skills.ResolvedSkill) []skills.ResolvedSkill {
	out := make([]skills.ResolvedSkill, len(results))
	for i, result := range results {
		out[i] = sanitizeSkillResult(result)
	}
	return out
}

func sanitizeSkillResult(result skills.ResolvedSkill) skills.ResolvedSkill {
	result.ContentMarkdown = ""
	if result.MaterializedPath != "" {
		result.MaterializedPath = filepath.ToSlash(result.MaterializedPath)
	}
	return result
}

func skillsEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("SKILLS_ENABLED"))
	if raw == "" {
		return true
	}
	ok, err := strconv.ParseBool(raw)
	return err == nil && ok
}

func skillMaterializeEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("SKILL_MATERIALIZE"))
	if raw == "" {
		return true
	}
	ok, err := strconv.ParseBool(raw)
	return err == nil && ok
}

func skillDraftTTL() time.Duration {
	hours, err := strconv.Atoi(strings.TrimSpace(os.Getenv("SKILL_DRAFT_TTL_HOURS")))
	if err != nil || hours <= 0 {
		hours = 72
	}
	return time.Duration(hours) * time.Hour
}

func parseStringList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var items []string
	if strings.HasPrefix(raw, "[") && json.Unmarshal([]byte(raw), &items) == nil {
		return items
	}
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ';' })
}

func firstNonEmptySkill(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func setStringArg(req *mcp.CallToolRequest, key, value string) {
	args := req.GetArguments()
	if args == nil {
		args = map[string]any{}
	}
	args[key] = value
}
