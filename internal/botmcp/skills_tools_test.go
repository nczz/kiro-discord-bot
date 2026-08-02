package botmcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nczz/kiro-discord-bot/internal/skills"
)

func TestDefaultSafeToolNamesIncludesOnlyReadSkillTools(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range DefaultSafeToolNames() {
		seen[tool] = true
	}
	for _, want := range []string{ToolSkillsSearch, ToolSkillsEffectiveList, ToolSkillGet, ToolSkillsServerSearch, ToolSkillsServerGet, ToolSkillsServerInventory, ToolSkillsServerEffectiveForChannel} {
		if !seen[want] {
			t.Fatalf("default safe tools missing %s", want)
		}
	}
	for _, forbidden := range []string{ToolSkillUsageRecord, ToolSkillCreate, ToolSkillsChannelInventory, ToolSkillsChannelEnable, ToolSkillsChannelDisable, ToolSkillsChannelRemove, ToolSkillsChannelRestore, ToolSkillsChannelRollback, ToolSkillsServerDisable, ToolSkillsServerRemove, ToolSkillsServerRestore, ToolSkillsServerRollback} {
		if seen[forbidden] {
			t.Fatalf("write/admin skill tool %s must not be default-safe", forbidden)
		}
	}
}

func TestSkillMCPCreateInventoryEnableSearchAndGet(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	project := t.TempDir()
	t.Setenv("MCP_PROXY_ALLOWED_TOOLS_JSON", `["read"]`)
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	statePath := filepath.Join(t.TempDir(), "target.json")
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1","requester_id":"user-1","requester_name":"alice","can_manage_channel":true}`), 0644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	result, err := skillCreate(ctx, dataDir, skillReq(map[string]any{
		"name":             "ERP Excel",
		"scope_type":       skills.ScopeChannelProject,
		"project_cwd":      project,
		"content_markdown": "# When to use\nUse for ERP Excel files.",
		"required_tools":   `["read","python"]`,
		"requested_by":     "alice user_id=user-1",
	}))
	if err != nil {
		t.Fatalf("skillCreate: %v", err)
	}
	if result["status"] != "created_disabled" || result["enabled"] != false || result["skill_id"] != "erp-excel" {
		t.Fatalf("create result = %+v", result)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal create result: %v", err)
	}
	if strings.Contains(string(raw), dataDir) || strings.Contains(string(raw), "ch-") || strings.Contains(string(raw), "draft_id") {
		t.Fatalf("create response leaked internal details: %s", raw)
	}
	inventory, err := skillChannelInventory(ctx, dataDir, skillReq(map[string]any{"query": "ERP", "project_cwd": project}))
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	listed := inventory["results"].([]skills.ResolvedSkill)
	if len(listed) != 1 || listed[0].Enabled || listed[0].Executable || listed[0].ContentMarkdown != "" {
		t.Fatalf("inventory results = %+v", listed)
	}
	if _, err := skillChannelSetEnabled(ctx, dataDir, skillReq(map[string]any{"skill_id": "erp-excel", "project_cwd": project, "reason": "test enable"}), true, "enable"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	search, err := skillSearch(ctx, dataDir, skillReq(map[string]any{"query": "ERP", "project_cwd": project}))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	results := search["results"].([]skills.ResolvedSkill)
	if len(results) != 1 || results[0].ContentMarkdown != "" || results[0].Executable || len(results[0].MissingTools) != 1 || results[0].MissingTools[0] != "python" {
		t.Fatalf("search results = %+v", results)
	}
	got, err := skillGet(ctx, dataDir, skillReq(map[string]any{"skill_id": "erp-excel", "project_cwd": project}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(got.ContentMarkdown, "ERP Excel") || got.Executable {
		t.Fatalf("get = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(project, ".kiro-bot", "skills", "erp-excel", "SKILL.md")); err != nil {
		t.Fatalf("materialized skill missing: %v", err)
	}
}

func TestSkillReadToolsBindToBotToolsChannel(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := skills.Open(dataDir)
	if err != nil {
		t.Fatalf("open skills: %v", err)
	}
	defer store.Close()
	draft, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: "Private Channel Two", ScopeType: skills.ScopeChannel, GuildID: "guild-1", ChannelID: "channel-2", ContentMarkdown: "# When to use\nUse only elsewhere.", SourceType: skills.SourceMarkdown, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("new draft: %v", err)
	}
	draft, err = store.CreateDraft(ctx, draft)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := store.InstallDraft(ctx, draft.DraftID, "alice"); err != nil {
		t.Fatalf("install: %v", err)
	}
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	if _, err := skillGet(ctx, dataDir, skillReq(map[string]any{"skill_id": "private-channel-two", "guild_id": "guild-1", "channel_id": "channel-2"})); err == nil {
		t.Fatalf("skillGet crossed bound channel scope")
	}
}

func TestSkillChannelLifecycleRequiresAuthenticatedManagerAndAudits(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := skills.Open(dataDir)
	if err != nil {
		t.Fatalf("open skills: %v", err)
	}
	defer store.Close()
	draft, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: "Channel SOP", ScopeType: skills.ScopeChannel, GuildID: "guild-1", ChannelID: "channel-1", ContentMarkdown: "# When to use\nUse in this channel.", SourceType: skills.SourceMarkdown, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("new draft: %v", err)
	}
	draft, err = store.CreateDraft(ctx, draft)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := store.InstallDraft(ctx, draft.DraftID, "alice"); err != nil {
		t.Fatalf("install: %v", err)
	}
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	statePath := filepath.Join(t.TempDir(), "target.json")
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1","requester_id":"user-1","requester_name":"alice","can_manage_channel":true}`), 0644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if _, err := skillChannelSetEnabled(ctx, dataDir, skillReq(map[string]any{"skill_id": "channel-sop", "reason": "test disable"}), false, "disable"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := skillGet(ctx, dataDir, skillReq(map[string]any{"skill_id": "channel-sop"})); err == nil {
		t.Fatalf("disabled skill remained visible")
	}
	history, err := store.MutationHistory(ctx, "channel-sop", 5)
	if err != nil {
		t.Fatalf("mutation history: %v", err)
	}
	if len(history) == 0 || history[0].Action != "disable" || history[0].ActorUserID != "user-1" || history[0].PreviousEventHash == "" {
		t.Fatalf("mutation history missing audited disable: %+v", history)
	}
}

func TestSkillServerManagementRequiresGuildManager(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "target.json")
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1","requester_id":"user-1","requester_name":"alice","can_manage_channel":true}`), 0644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if _, err := skillServerSetEnabled(context.Background(), t.TempDir(), skillReq(map[string]any{"skill_id": "guild-sop"}), false, "disable"); err == nil {
		t.Fatalf("server management allowed without guild management permission")
	}
}

func TestSkillServerReadAndManageUsesGuildScope(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := skills.Open(dataDir)
	if err != nil {
		t.Fatalf("open skills: %v", err)
	}
	defer store.Close()
	draft, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: "Guild SOP", ScopeType: skills.ScopeGuild, GuildID: "guild-1", ContentMarkdown: "# When to use\nUse across the server.", SourceType: skills.SourceMarkdown, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("new draft: %v", err)
	}
	draft, err = store.CreateDraft(ctx, draft)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := store.InstallDraft(ctx, draft.DraftID, "alice"); err != nil {
		t.Fatalf("install: %v", err)
	}
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")
	statePath := filepath.Join(t.TempDir(), "target.json")
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1","requester_id":"user-1","requester_name":"alice","can_manage_guild":true}`), 0644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	search, err := skillServerSearch(ctx, dataDir, skillReq(map[string]any{"query": "Guild"}))
	if err != nil {
		t.Fatalf("server search: %v", err)
	}
	if got := search["results"].([]skills.ResolvedSkill); len(got) != 1 || got[0].ScopeType != skills.ScopeGuild {
		t.Fatalf("server search results=%+v", got)
	}
	if _, err := skillServerGet(ctx, dataDir, skillReq(map[string]any{"skill_id": "guild-sop"})); err != nil {
		t.Fatalf("server get: %v", err)
	}
	if _, err := skillServerSetEnabled(ctx, dataDir, skillReq(map[string]any{"skill_id": "guild-sop"}), false, "disable"); err != nil {
		t.Fatalf("server disable: %v", err)
	}
	if _, err := skillServerGet(ctx, dataDir, skillReq(map[string]any{"skill_id": "guild-sop"})); err == nil {
		t.Fatalf("disabled server skill remained visible")
	}
}

func TestSkillCreateUsesCuratedMarkdownOnly(t *testing.T) {
	content := "---\nrequired_tools:\n  - python\n---\n# When to use\nUse fetched markdown."
	dataDir := t.TempDir()
	result, err := skillCreate(context.Background(), dataDir, skillReq(map[string]any{"name": "Fetched Skill", "scope_type": skills.ScopeGuild, "guild_id": "guild-1", "content_markdown": content, "source_type": "url", "source_ref": "https://gist.github.com/example", "requested_by": "alice"}))
	if err != nil {
		t.Fatalf("create from curated content: %v", err)
	}
	if result["status"] != "created_disabled" || result["enabled"] != false || result["skill_id"] != "fetched-skill" || result["scope_type"] != skills.ScopeGuild {
		t.Fatalf("create result = %+v", result)
	}
	if _, ok := result["project_cwd"]; ok {
		t.Fatalf("create result = %+v", result)
	}
	store, err := skills.Open(dataDir)
	if err != nil {
		t.Fatalf("open skills: %v", err)
	}
	defer store.Close()
	install, err := store.GetInstalled(context.Background(), skills.ResolveContext{GuildID: "guild-1"}, result["skill_id"].(string))
	if err != nil {
		t.Fatalf("get installed: %v", err)
	}
	tools, err := store.RequiredTools(context.Background(), install.SkillID, install.Version)
	if err != nil {
		t.Fatalf("required tools: %v", err)
	}
	if len(tools) != 1 || tools[0] != "python" || install.Enabled || install.ScopeType != skills.ScopeGuild {
		t.Fatalf("install tools/enabled/scope = %v/%t/%s", tools, install.Enabled, install.ScopeType)
	}
	if visible, err := store.Resolve(context.Background(), skills.ResolveContext{GuildID: "guild-1", EffectiveTools: []string{"python"}}); err != nil || len(visible) != 0 {
		t.Fatalf("created disabled skill visible to agents = %+v, err=%v", visible, err)
	}
}

func TestSkillCreateFailureDoesNotLeaveActiveStagingRow(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	project := t.TempDir()
	store, err := skills.Open(dataDir)
	if err != nil {
		t.Fatalf("open skills: %v", err)
	}
	active, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: "Duplicate SOP", ScopeType: skills.ScopeChannelProject, GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: project, ContentMarkdown: "# When to use\nUse existing.", SourceType: skills.SourceConversation, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("new active skill: %v", err)
	}
	active, err = store.CreateDraft(ctx, active)
	if err != nil {
		t.Fatalf("create active skill staging row: %v", err)
	}
	if _, err := store.InstallDraft(ctx, active.DraftID, "alice"); err != nil {
		t.Fatalf("install active skill: %v", err)
	}
	store.Close()

	_, err = skillCreate(ctx, dataDir, skillReq(map[string]any{"name": "Duplicate SOP", "scope_type": skills.ScopeChannelProject, "guild_id": "guild-1", "channel_id": "channel-1", "project_cwd": project, "content_markdown": "# When to use\nUse duplicate.", "source_type": "conversation", "requested_by": "bob"}))
	if err == nil {
		t.Fatal("duplicate skill create unexpectedly succeeded")
	}
	store, err = skills.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen skills: %v", err)
	}
	defer store.Close()
	activeRows, err := store.ActiveDrafts(ctx, skills.ResolveContext{GuildID: "guild-1"}, 10)
	if err != nil {
		t.Fatalf("active staging rows: %v", err)
	}
	if len(activeRows) != 0 {
		t.Fatalf("failed create left active staging rows: %+v", activeRows)
	}
	if _, err := os.Stat(filepath.Join(project, ".kiro-bot", "skills", "duplicate-sop", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("failed create materialized file exists after cleanup: %v", err)
	}
}

func TestSkillCreateRejectsRawHTML(t *testing.T) {
	_, err := skillCreate(context.Background(), t.TempDir(), skillReq(map[string]any{"name": "HTML Skill", "scope_type": skills.ScopeGuild, "guild_id": "guild-1", "content_markdown": "<!doctype html><html><body>not a skill</body></html>", "source_type": "url", "source_ref": "https://gist.github.com/example", "requested_by": "alice"}))
	if err == nil || !strings.Contains(err.Error(), "curated markdown") {
		t.Fatalf("raw HTML was not rejected: %v", err)
	}
}

func TestSkillCreateAllowsMarkdownHTMLExamples(t *testing.T) {
	content := "# Procedure\nAdd this snippet when needed:\n\n```html\n<script src=\"/assets/app.js\"></script>\n<body>example</body>\n```"
	if _, err := skillCreate(context.Background(), t.TempDir(), skillReq(map[string]any{"name": "HTML Example Skill", "scope_type": skills.ScopeGuild, "guild_id": "guild-1", "content_markdown": content, "source_type": "conversation", "requested_by": "alice"})); err != nil {
		t.Fatalf("curated markdown with HTML example rejected: %v", err)
	}
}

func skillReq(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
}
