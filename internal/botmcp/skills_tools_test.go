package botmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	for _, forbidden := range []string{ToolSkillUsageRecord, ToolSkillDraftFromConversation, ToolSkillImportMarkdown, ToolSkillImportURL, ToolSkillImportGitHubRepo, ToolSkillInstallDraft, ToolSkillDiscardDraft, ToolSkillsChannelEnable, ToolSkillsChannelDisable, ToolSkillsChannelRemove, ToolSkillsChannelRestore, ToolSkillsChannelRollback, ToolSkillsServerDraft, ToolSkillsServerDisable, ToolSkillsServerRemove, ToolSkillsServerRestore, ToolSkillsServerRollback} {
		if seen[forbidden] {
			t.Fatalf("write/admin skill tool %s must not be default-safe", forbidden)
		}
	}
}

func TestSkillMCPDraftInstallSearchAndGet(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	project := t.TempDir()
	t.Setenv("MCP_PROXY_ALLOWED_TOOLS_JSON", `["read"]`)
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	draft, err := skillDraft(ctx, dataDir, skillReq(map[string]any{
		"name":             "ERP Excel",
		"scope_type":       skills.ScopeChannelProject,
		"project_cwd":      project,
		"content_markdown": "# When to use\nUse for ERP Excel files.",
		"required_tools":   `["read","python"]`,
		"requested_by":     "alice user_id=user-1",
	}), skills.SourceConversation)
	if err != nil {
		t.Fatalf("skillDraft: %v", err)
	}
	if draft.Status != skills.StatusDraft || draft.ProposedSlug != "erp-excel" {
		t.Fatalf("draft = %+v", draft)
	}
	file, err := skills.Materialize(project, draft.ProposedSlug, draft.ProposedContentMarkdown, false)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	store, err := skills.Open(dataDir)
	if err != nil {
		t.Fatalf("open skills: %v", err)
	}
	defer store.Close()
	installed, err := store.InstallDraftWithMaterialization(ctx, draft.DraftID, "alice", file.RelativePath, file.SHA256)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	raw, err := json.Marshal(installed)
	if err != nil {
		t.Fatalf("marshal install: %v", err)
	}
	if strings.Contains(string(raw), dataDir) || strings.Contains(string(raw), "ch-") {
		t.Fatalf("install response leaked bot data path: %s", raw)
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

func TestSkillInstallDraftRequiresDiscordConfirmation(t *testing.T) {
	if _, err := skillInstallDraft(context.Background(), t.TempDir(), skillReq(map[string]any{"draft_id": "draft-1", "confirmed_by": "alice", "manage_channels": true, "manage_guild": true})); err == nil {
		t.Fatalf("MCP install without Discord confirmation succeeded")
	}
}

func TestSkillDiscardDraftRequiresDiscordConfirmation(t *testing.T) {
	if _, err := skillDiscardDraft(context.Background(), t.TempDir(), skillReq(map[string]any{"draft_id": "draft-1", "confirmed_by": "alice"})); err == nil {
		t.Fatalf("MCP discard without Discord confirmation succeeded")
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

func TestSkillImportURLRejectsPrivateNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("---\nrequired_tools:\n  - python\n---\n# When to use\nUse fetched markdown."))
	}))
	defer srv.Close()
	if _, _, err := fetchSkillURL(context.Background(), srv.URL, 4096); err == nil {
		t.Fatalf("fetchSkillURL accepted private test server URL")
	}
}

func TestSkillImportMarkdownCreatesDraftOnly(t *testing.T) {
	content := "---\nrequired_tools:\n  - python\n---\n# When to use\nUse fetched markdown."
	draft, err := skillDraft(context.Background(), t.TempDir(), skillReq(map[string]any{"name": "Fetched Skill", "scope_type": skills.ScopeGuild, "guild_id": "guild-1", "content_markdown": content, "requested_by": "alice"}), skills.SourceURL)
	if err != nil {
		t.Fatalf("draft from fetched content: %v", err)
	}
	tools, err := skills.RequiredToolsFromJSON(draft.RequiredToolsJSON)
	if err != nil {
		t.Fatalf("required tools json: %v", err)
	}
	if len(tools) != 1 || tools[0] != "python" || draft.Status != skills.StatusDraft {
		t.Fatalf("draft tools/status = %v/%s", tools, draft.Status)
	}
}

func skillReq(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
}
