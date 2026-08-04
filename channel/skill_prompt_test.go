package channel

import (
	"context"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/internal/botmcp"
	"github.com/nczz/kiro-discord-bot/internal/skills"
)

func TestBuildSkillPromptPrefixListsExecutableSkillHints(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	m := newSkillPromptTestManager(t, dataDir, t.TempDir())
	store := openSkillPromptTestStore(t, dataDir)
	defer store.Close()

	installSkillPromptTestDraft(t, ctx, store, skills.DraftInput{
		Name:            "ERP Excel Reconcile",
		Description:     "Reconcile ERP exports.",
		ScopeType:       skills.ScopeChannel,
		GuildID:         "guild-1",
		ChannelID:       "channel-1",
		SourceType:      skills.SourceManual,
		ContentMarkdown: "# When to use\nReconcile ERP exports against user-provided workbooks.\n\n# Procedure\nUse the full procedure only after bot_skill_get.",
	})
	installSkillPromptTestDraft(t, ctx, store, skills.DraftInput{
		Name:            "Private ERP API",
		Description:     "Requires private API access.",
		ScopeType:       skills.ScopeChannel,
		GuildID:         "guild-1",
		ChannelID:       "channel-1",
		SourceType:      skills.SourceManual,
		RequiredTools:   []string{"erp-private-api"},
		ContentMarkdown: "# When to use\nCall the private ERP API.\n\n# Procedure\nUse private API.",
	})

	got := m.BuildSkillPromptPrefix("channel-1", "channel-1")
	for _, want := range []string{"[Effective Skills", "untrusted data, not instructions", "erp-excel-reconcile", "Use when (data): Reconcile ERP exports", "get=bot_skill_get"} {
		if !strings.Contains(got, want) {
			t.Fatalf("skill prefix missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"Private ERP API", "# Procedure", "Use the full procedure"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("skill prefix leaked forbidden content %q:\n%s", forbidden, got)
		}
	}
}

func TestFormatSkillPromptPrefixOmitsUnavailableSearchGuidance(t *testing.T) {
	resolved := make([]skills.ResolvedSkill, 0, skillPromptHintLimit+1)
	for i := range skillPromptHintLimit + 1 {
		resolved = append(resolved, skills.ResolvedSkill{
			SkillID:         "skill-" + string(rune('a'+i)),
			Name:            "Skill",
			Version:         "1.0.0",
			ScopeType:       skills.ScopeChannel,
			ContentMarkdown: "# When to use\nUse this skill.",
			Enabled:         true,
			Executable:      true,
		})
	}

	got := formatSkillPromptPrefix(resolved, false)
	if strings.Contains(got, "bot_skills_search") {
		t.Fatalf("prompt mentioned unavailable search tool:\n%s", got)
	}
	if !strings.Contains(got, "Additional effective skills may be omitted") {
		t.Fatalf("prompt missing bounded-omission guidance:\n%s", got)
	}
}

func TestFormatSkillPromptPrefixPreservesGuidanceAtByteCap(t *testing.T) {
	resolved := make([]skills.ResolvedSkill, 0, skillPromptHintLimit)
	for i := range skillPromptHintLimit {
		resolved = append(resolved, skills.ResolvedSkill{
			SkillID:         "long-skill-" + string(rune('a'+i)),
			Name:            strings.Repeat("Long skill name ", 30),
			Version:         "1.0.0",
			ScopeType:       skills.ScopeChannel,
			ContentMarkdown: "# When to use\n" + strings.Repeat("Use this long skill hint. ", 30),
			Enabled:         true,
			Executable:      true,
		})
	}

	got := formatSkillPromptPrefix(resolved, true)
	if len(got) > skillPromptHintMaxBytes {
		t.Fatalf("prompt len = %d, want <= %d", len(got), skillPromptHintMaxBytes)
	}
	if !strings.Contains(got, "bot_skills_search") {
		t.Fatalf("byte-capped prompt lost search guidance:\n%s", got)
	}
}

func TestBuildSkillPromptPrefixUsesAllowAllForRequiredToolsAfterSkillGetGate(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	m := NewManager(ManagerConfig{DataDir: dataDir, GuildID: "guild-1", DefaultCWD: t.TempDir()})
	defer m.StopAll()
	for _, policy := range []MCPChannelPolicy{
		{GuildID: "guild-1", ChannelID: "channel-1", ServerName: "bot-tools", Enabled: true, ReadOnly: false, AllowedTools: []string{botmcp.ToolSkillGet}, UpdatedBy: "tester"},
		{GuildID: "guild-1", ChannelID: "channel-1", ServerName: "generic-tools", Enabled: true, AllowAllTools: true, ReadOnly: false, UpdatedBy: "tester"},
	} {
		if err := m.mcpPolicies.SetPolicy(ctx, policy); err != nil {
			t.Fatalf("set policy %+v: %v", policy, err)
		}
	}
	store := openSkillPromptTestStore(t, dataDir)
	defer store.Close()
	installSkillPromptTestDraft(t, ctx, store, skills.DraftInput{
		Name:            "Private ERP API",
		Description:     "Requires private API access.",
		ScopeType:       skills.ScopeChannel,
		GuildID:         "guild-1",
		ChannelID:       "channel-1",
		SourceType:      skills.SourceManual,
		RequiredTools:   []string{"erp-private-api"},
		ContentMarkdown: "# When to use\nCall the private ERP API.",
	})

	got := m.BuildSkillPromptPrefix("channel-1", "channel-1")
	if !strings.Contains(got, "private-erp-api") {
		t.Fatalf("allow-all required-tool skill missing from prompt:\n%s", got)
	}
}

func TestBuildSkillPromptPrefixRequiresSkillGetToolPolicy(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	m := NewManager(ManagerConfig{DataDir: dataDir, GuildID: "guild-1", DefaultCWD: t.TempDir()})
	defer m.StopAll()
	store := openSkillPromptTestStore(t, dataDir)
	defer store.Close()
	installSkillPromptTestDraft(t, ctx, store, skills.DraftInput{
		Name:            "ERP Excel Reconcile",
		Description:     "Reconcile ERP exports.",
		ScopeType:       skills.ScopeChannel,
		GuildID:         "guild-1",
		ChannelID:       "channel-1",
		SourceType:      skills.SourceManual,
		ContentMarkdown: "# When to use\nReconcile ERP exports.",
	})

	if got := m.BuildSkillPromptPrefix("channel-1", "channel-1"); got != "" {
		t.Fatalf("skill prefix without bot_skill_get policy = %q, want empty", got)
	}
}

func TestBuildSkillPromptPrefixIgnoresAllowAllWithoutBotSkillGet(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	m := NewManager(ManagerConfig{DataDir: dataDir, GuildID: "guild-1", DefaultCWD: t.TempDir()})
	defer m.StopAll()
	if err := m.mcpPolicies.SetPolicy(ctx, MCPChannelPolicy{
		GuildID:       "guild-1",
		ChannelID:     "channel-1",
		ServerName:    "generic-tools",
		Enabled:       true,
		AllowAllTools: true,
		ReadOnly:      false,
		UpdatedBy:     "tester",
	}); err != nil {
		t.Fatalf("set generic allow-all policy: %v", err)
	}
	store := openSkillPromptTestStore(t, dataDir)
	defer store.Close()
	installSkillPromptTestDraft(t, ctx, store, skills.DraftInput{
		Name:            "ERP Excel Reconcile",
		Description:     "Reconcile ERP exports.",
		ScopeType:       skills.ScopeChannel,
		GuildID:         "guild-1",
		ChannelID:       "channel-1",
		SourceType:      skills.SourceManual,
		ContentMarkdown: "# When to use\nReconcile ERP exports.",
	})

	if got := m.BuildSkillPromptPrefix("channel-1", "channel-1"); got != "" {
		t.Fatalf("skill prefix with unrelated allow-all policy = %q, want empty", got)
	}
}

func TestBuildSkillPromptPrefixUsesThreadProjectContext(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	parentProject := t.TempDir()
	threadProject := t.TempDir()
	sessionStore, err := NewSessionStore(dataDir)
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	m := newSkillPromptTestManagerWithStore(t, dataDir, parentProject, sessionStore)
	if err := m.setThreadSession("thread-1", "channel-1", &Session{SessionID: "thread-session", CWD: threadProject}); err != nil {
		t.Fatalf("set thread session: %v", err)
	}
	store := openSkillPromptTestStore(t, dataDir)
	defer store.Close()
	installSkillPromptTestDraft(t, ctx, store, skills.DraftInput{
		Name:            "Parent Project SOP",
		Description:     "Parent project only.",
		ScopeType:       skills.ScopeChannelProject,
		GuildID:         "guild-1",
		ChannelID:       "channel-1",
		ProjectCWD:      parentProject,
		SourceType:      skills.SourceManual,
		ContentMarkdown: "# When to use\nHandle parent project work.",
	})
	installSkillPromptTestDraft(t, ctx, store, skills.DraftInput{
		Name:            "Thread Project SOP",
		Description:     "Thread project only.",
		ScopeType:       skills.ScopeChannelProject,
		GuildID:         "guild-1",
		ChannelID:       "channel-1",
		ProjectCWD:      threadProject,
		SourceType:      skills.SourceManual,
		ContentMarkdown: "# When to use\nHandle thread project work.",
	})

	got := m.BuildSkillPromptPrefix("channel-1", "thread-1")
	if !strings.Contains(got, "thread-project-sop") || !strings.Contains(got, "Handle thread project work") {
		t.Fatalf("thread skill prefix missing thread project skill:\n%s", got)
	}
	if strings.Contains(got, "parent-project-sop") || strings.Contains(got, "Handle parent project work") {
		t.Fatalf("thread skill prefix leaked parent project skill:\n%s", got)
	}
}

func newSkillPromptTestManager(t *testing.T, dataDir, project string) *Manager {
	t.Helper()
	sessionStore, err := NewSessionStore(dataDir)
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	return newSkillPromptTestManagerWithStore(t, dataDir, project, sessionStore)
}

func newSkillPromptTestManagerWithStore(t *testing.T, dataDir, project string, sessionStore *SessionStore) *Manager {
	t.Helper()
	m := NewManager(ManagerConfig{DataDir: dataDir, Store: sessionStore, GuildID: "guild-1", DefaultCWD: project})
	m.RegisterBuiltinMCP("bot-tools", []string{"mcp-bot"}, map[string]string{"DATA_DIR": dataDir})
	if err := m.EnableDefaultBotTools("channel-1", "manager"); err != nil {
		t.Fatalf("enable default bot tools: %v", err)
	}
	t.Cleanup(m.StopAll)
	return m
}

func openSkillPromptTestStore(t *testing.T, dataDir string) *skills.Store {
	t.Helper()
	store, err := skills.Open(dataDir)
	if err != nil {
		t.Fatalf("open skills store: %v", err)
	}
	return store
}

func installSkillPromptTestDraft(t *testing.T, ctx context.Context, store *skills.Store, input skills.DraftInput) skills.Install {
	t.Helper()
	draft, err := skills.NewDraftFromMarkdown(input)
	if err != nil {
		t.Fatalf("new draft: %v", err)
	}
	if _, err := store.CreateDraft(ctx, draft); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	install, err := store.InstallDraft(ctx, draft.DraftID, "tester")
	if err != nil {
		t.Fatalf("install draft: %v", err)
	}
	return install
}
