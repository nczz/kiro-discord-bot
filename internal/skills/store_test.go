package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreResolveScopePrecedenceAndRequiredTools(t *testing.T) {
	ctx := context.Background()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open skills store: %v", err)
	}
	defer store.Close()
	project := t.TempDir()
	base := draftFor(t, "ERP Excel", ScopeGuild, "guild-1", "", "", []string{"read"})
	if _, err := store.CreateDraft(ctx, base); err != nil {
		t.Fatalf("create guild draft: %v", err)
	}
	if _, err := store.InstallDraft(ctx, base.DraftID, "admin"); err != nil {
		t.Fatalf("install guild draft: %v", err)
	}
	channel := draftFor(t, "ERP Excel", ScopeChannel, "guild-1", "channel-1", "", []string{"read", "python"})
	channel.ProposedContentMarkdown = strings.ReplaceAll(channel.ProposedContentMarkdown, "ERP Excel", "ERP Excel Channel")
	if _, err := store.CreateDraft(ctx, channel); err != nil {
		t.Fatalf("create channel draft: %v", err)
	}
	if _, err := store.InstallDraft(ctx, channel.DraftID, "manager"); err != nil {
		t.Fatalf("install channel draft: %v", err)
	}
	projectDraft := draftFor(t, "Project Only", ScopeProject, "", "", project, nil)
	if _, err := store.CreateDraft(ctx, projectDraft); err != nil {
		t.Fatalf("create project draft: %v", err)
	}
	if _, err := store.InstallDraft(ctx, projectDraft.DraftID, "manager"); err != nil {
		t.Fatalf("install project draft: %v", err)
	}

	resolved, err := store.Resolve(ctx, ResolveContext{GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: project, EffectiveTools: []string{"read"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved len = %d, want 2: %+v", len(resolved), resolved)
	}
	erp := mustFindSkill(t, resolved, "erp-excel")
	if erp.ScopeType != ScopeChannel {
		t.Fatalf("ERP scope = %s, want channel", erp.ScopeType)
	}
	if erp.Executable || len(erp.MissingTools) != 1 || erp.MissingTools[0] != "python" {
		t.Fatalf("ERP executable/missing = %v/%v, want missing python", erp.Executable, erp.MissingTools)
	}
	proj := mustFindSkill(t, resolved, "project-only")
	if proj.ScopeType != ScopeProject || !proj.Executable {
		t.Fatalf("project skill = %+v, want executable project scope", proj)
	}
}

func TestSearchOmitsContentAndGetVisibleIncludesContent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open skills store: %v", err)
	}
	defer store.Close()
	d := draftFor(t, "Invoice Reconcile", ScopeGuild, "guild-1", "", "", nil)
	d.ProposedContentMarkdown += "\nsecret searchable details"
	if _, err := store.CreateDraft(ctx, d); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := store.InstallDraft(ctx, d.DraftID, "admin"); err != nil {
		t.Fatalf("install draft: %v", err)
	}
	results, err := store.Search(ctx, ResolveContext{GuildID: "guild-1"}, "secret", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("search len = %d, want 1", len(results))
	}
	if results[0].ContentMarkdown != "" {
		t.Fatalf("search exposed content: %q", results[0].ContentMarkdown)
	}
	got, err := store.GetVisible(ctx, ResolveContext{GuildID: "guild-1"}, d.ProposedSlug)
	if err != nil {
		t.Fatalf("get visible: %v", err)
	}
	if !strings.Contains(got.ContentMarkdown, "secret searchable details") {
		t.Fatalf("get did not include content: %q", got.ContentMarkdown)
	}
}

func TestMaterializeRejectsUnsafeSlugAndDetectsDrift(t *testing.T) {
	project := t.TempDir()
	if _, err := Materialize(project, "../bad", "content", false); err == nil {
		t.Fatalf("unsafe slug accepted")
	}
	file, err := Materialize(project, "safe-skill", "content", false)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if file.RelativePath != filepath.ToSlash(filepath.Join(".kiro-bot", "skills", "safe-skill", "SKILL.md")) {
		t.Fatalf("relative path = %q", file.RelativePath)
	}
	if err := os.WriteFile(file.Path, []byte("changed"), 0644); err != nil {
		t.Fatalf("change materialized file: %v", err)
	}
	if _, err := Materialize(project, "safe-skill", "content", false); !errors.Is(err, ErrMaterializedDrift) {
		t.Fatalf("drift err = %v, want ErrMaterializedDrift", err)
	}
}

func TestNewDraftFromMarkdownNormalizesSections(t *testing.T) {
	d, err := NewDraftFromMarkdown(DraftInput{Name: "Excel Helper", ScopeType: ScopeChannelProject, GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: t.TempDir(), SourceType: SourceMarkdown, ContentMarkdown: "# Notes\nDo it.", RequiredTools: []string{"python"}, TTL: time.Hour})
	if err != nil {
		t.Fatalf("new draft: %v", err)
	}
	for _, want := range []string{"# When to use", "# Preconditions", "# Procedure", "# Safety", "# Output contract", "required_tools: [python]"} {
		if !strings.Contains(d.ProposedContentMarkdown, want) {
			t.Fatalf("draft content missing %q:\n%s", want, d.ProposedContentMarkdown)
		}
	}
	if d.ProposedSlug != "excel-helper" || d.ProjectCWDHash == "" || d.ExpiresAt.IsZero() {
		t.Fatalf("draft normalization failed: %+v", d)
	}
}

func TestRecordUsageRequiresSkillAndVersion(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open skills store: %v", err)
	}
	defer store.Close()
	if _, err := store.RecordUsage(context.Background(), UsageEvent{}); err == nil {
		t.Fatalf("usage without skill/version accepted")
	}
	ev, err := store.RecordUsage(context.Background(), UsageEvent{SkillID: "skill", Version: "1.0.0", GuildID: "guild-1", ChannelID: "channel-1"})
	if err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if ev.UsageID == "" || ev.SelectedBy != "agent" || ev.UsedAt.IsZero() {
		t.Fatalf("usage defaults missing: %+v", ev)
	}
}

func TestInstallDraftClaimsDraftOnceAndAuditsPersistedInstallID(t *testing.T) {
	ctx := context.Background()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open skills store: %v", err)
	}
	defer store.Close()
	first := draftFor(t, "Claimed SOP", ScopeChannel, "guild-1", "channel-1", "", nil)
	if _, err := store.CreateDraft(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	install, err := store.InstallDraft(ctx, first.DraftID, "alice")
	if err != nil {
		t.Fatalf("install first: %v", err)
	}
	if _, err := store.InstallDraft(ctx, first.DraftID, "alice"); err == nil {
		t.Fatalf("second install of consumed draft succeeded")
	}
	second := draftFor(t, "Claimed SOP", ScopeChannel, "guild-1", "channel-1", "", nil)
	second.ProposedVersion = "2.0.0"
	if _, err := store.CreateDraft(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	updated, err := store.InstallDraft(ctx, second.DraftID, "alice")
	if err != nil {
		t.Fatalf("install second: %v", err)
	}
	if updated.InstallID != install.InstallID {
		t.Fatalf("overwrite install id = %s, want persisted %s", updated.InstallID, install.InstallID)
	}
	history, err := store.MutationHistoryForContext(ctx, ResolveContext{GuildID: "guild-1", ChannelID: "channel-1"}, "claimed-sop", ScopeChannel, 5)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) < 2 || history[0].InstallID != install.InstallID {
		t.Fatalf("history install ids = %+v, want persisted id %s", history, install.InstallID)
	}
}

func TestCreateDisabledInstallRequiresExplicitEnable(t *testing.T) {
	ctx := context.Background()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open skills store: %v", err)
	}
	defer store.Close()
	d := draftFor(t, "Disabled Create SOP", ScopeChannel, "guild-1", "channel-1", "", []string{"read"})
	if _, err := store.CreateDraft(ctx, d); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	install, err := store.CreateDisabledInstallFromDraftWithMaterializationAndAudit(ctx, d.DraftID, MutationActor{ActorUsername: "alice"}, "test create", "", "")
	if err != nil {
		t.Fatalf("create disabled install: %v", err)
	}
	if install.Enabled {
		t.Fatalf("created install enabled = true")
	}
	rc := ResolveContext{GuildID: "guild-1", ChannelID: "channel-1", EffectiveTools: []string{"read"}}
	if resolved, err := store.Resolve(ctx, rc); err != nil || len(resolved) != 0 {
		t.Fatalf("resolve disabled = %+v, err=%v", resolved, err)
	}
	listed, err := store.ListInstalled(ctx, rc, "disabled", 10)
	if err != nil {
		t.Fatalf("list installed: %v", err)
	}
	if len(listed) != 1 || listed[0].Enabled || listed[0].Executable {
		t.Fatalf("listed disabled skill = %+v", listed)
	}
	if _, err := store.SetInstallEnabled(ctx, rc, "disabled-create-sop", ScopeChannel, true, MutationActor{ActorUsername: "alice"}, "test enable", "enable"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	resolved, err := store.Resolve(ctx, rc)
	if err != nil {
		t.Fatalf("resolve enabled: %v", err)
	}
	if len(resolved) != 1 || !resolved[0].Enabled || !resolved[0].Executable {
		t.Fatalf("resolved enabled = %+v", resolved)
	}
}

func TestCreateDisabledInstallDoesNotReplaceExistingActiveContent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open skills store: %v", err)
	}
	defer store.Close()
	active, err := NewDraftFromMarkdown(DraftInput{Name: "Shared SOP", ScopeType: ScopeChannel, GuildID: "guild-1", ChannelID: "channel-1", SourceType: SourceConversation, ContentMarkdown: "# When to use\nOld approved content.", RequiredTools: []string{"read"}, CreatedBy: "tester"})
	if err != nil {
		t.Fatalf("active draft: %v", err)
	}
	active, err = store.CreateDraft(ctx, active)
	if err != nil {
		t.Fatalf("create active draft: %v", err)
	}
	if _, err := store.InstallDraft(ctx, active.DraftID, "alice"); err != nil {
		t.Fatalf("install active draft: %v", err)
	}
	replacement, err := NewDraftFromMarkdown(DraftInput{Name: "Shared SOP", ScopeType: ScopeChannel, GuildID: "guild-1", ChannelID: "channel-1", SourceType: SourceConversation, ContentMarkdown: "# When to use\nNew unapproved content.", RequiredTools: []string{"read"}, CreatedBy: "tester"})
	if err != nil {
		t.Fatalf("replacement draft: %v", err)
	}
	replacement, err = store.CreateDraft(ctx, replacement)
	if err != nil {
		t.Fatalf("create replacement draft: %v", err)
	}
	if _, err := store.CreateDisabledInstallFromDraftWithMaterializationAndAudit(ctx, replacement.DraftID, MutationActor{ActorUsername: "alice"}, "test create", "", ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("create duplicate disabled err = %v", err)
	}
	visible, err := store.GetVisible(ctx, ResolveContext{GuildID: "guild-1", ChannelID: "channel-1", EffectiveTools: []string{"read"}}, "shared-sop")
	if err != nil {
		t.Fatalf("get visible: %v", err)
	}
	if strings.Contains(visible.ContentMarkdown, "New unapproved") {
		t.Fatalf("unapproved content became visible: %q", visible.ContentMarkdown)
	}
}

func draftFor(t *testing.T, name, scope, guild, channel, project string, tools []string) Draft {
	t.Helper()
	d, err := NewDraftFromMarkdown(DraftInput{Name: name, ScopeType: scope, GuildID: guild, ChannelID: channel, ProjectCWD: project, SourceType: SourceConversation, ContentMarkdown: "# When to use\n" + name, RequiredTools: tools, CreatedBy: "tester"})
	if err != nil {
		t.Fatalf("draftFor %s: %v", name, err)
	}
	return d
}

func mustFindSkill(t *testing.T, skills []ResolvedSkill, slug string) ResolvedSkill {
	t.Helper()
	for _, skill := range skills {
		if skill.Slug == slug {
			return skill
		}
	}
	t.Fatalf("skill %q not found in %+v", slug, skills)
	return ResolvedSkill{}
}
