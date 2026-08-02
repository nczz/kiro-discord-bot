package bot

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/skills"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestSkillSlashCommandsExposeInstalledLifecycleOnly(t *testing.T) {
	var skillCmd *discordgo.ApplicationCommand
	for _, cmd := range buildSlashCommands() {
		if cmd.Name == "skill" {
			skillCmd = cmd
			break
		}
	}
	if skillCmd == nil {
		t.Fatal("/skill command not registered")
	}
	seen := map[string]bool{}
	for _, opt := range skillCmd.Options {
		seen[opt.Name] = true
	}
	for _, name := range []string{"list", "get", "create", "disable", "enable", "restore", "rollback", "history"} {
		if !seen[name] {
			t.Fatalf("/skill missing subcommand %s", name)
		}
	}
	for _, name := range []string{"draft", "preview", "install", "discard"} {
		if seen[name] {
			t.Fatalf("/skill still exposes removed subcommand %s", name)
		}
	}
}

func TestSkillUserFacingTextUsesLocale(t *testing.T) {
	L.Load("zh-TW")
	defer L.Load("en")
	var skillCmd *discordgo.ApplicationCommand
	for _, cmd := range buildSlashCommands() {
		if cmd.Name == "skill" {
			skillCmd = cmd
			break
		}
	}
	if skillCmd == nil {
		t.Fatal("/skill command not registered")
	}
	for _, opt := range skillCmd.Options {
		if opt.Name == "history" && opt.Description != L.Get("cmd.skill.sub.history") {
			t.Fatalf("history description = %q, want locale", opt.Description)
		}
	}
	createdComponents := skillCreatedComponents("inst_123456789012345678901234567890123456", "channel-1")
	createdRow, ok := createdComponents[0].(discordgo.ActionsRow)
	if !ok || len(createdRow.Components) != 1 {
		t.Fatalf("created components = %#v", createdComponents)
	}
	enable, ok := createdRow.Components[0].(discordgo.Button)
	if !ok || enable.Label != "啟用" {
		t.Fatalf("enable button = %#v, want zh-TW label", createdRow.Components[0])
	}
	if strings.Contains(enable.CustomID, "draft") || enable.CustomID != "skill:enable:channel-1:inst_123456789012345678901234567890123456" || len(enable.CustomID) > 100 {
		t.Fatalf("enable custom id = %q", enable.CustomID)
	}
}

func TestSkillCreateCommandCreatesDisabledInstallAndEnable(t *testing.T) {
	L.Load("en")
	dataDir := t.TempDir()
	project := t.TempDir()
	store, err := skills.Open(dataDir)
	if err != nil {
		t.Fatalf("open skills: %v", err)
	}
	defer store.Close()
	sessionStore, err := channel.NewSessionStore(dataDir)
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dataDir, GuildID: "guild-1", Store: sessionStore})
	defer manager.StopAll()
	if err := manager.SetCWD("channel-1", project); err != nil {
		t.Fatalf("set cwd: %v", err)
	}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{userMemberManageOverwrite("manager", discordgo.PermissionManageChannels)})
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "manager"}}); err != nil {
		t.Fatalf("MemberAdd manager: %v", err)
	}
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "regular"}}); err != nil {
		t.Fatalf("MemberAdd regular: %v", err)
	}
	b := &Bot{discord: ds, manager: manager, skillsStore: store}
	var replies []string
	var componentCount int
	ctx := cmdCtx{guildID: "guild-1", channelID: "channel-1", targetID: "channel-1", userID: "manager", username: "alice", reply: func(msg string) { replies = append(replies, msg) }, replyWithComponents: func(msg string, components []discordgo.MessageComponent, _ map[string]any) {
		replies = append(replies, msg)
		componentCount = len(components)
	}}
	b.cmdSkillCreate(ctx, store, "ERP Reconcile", "Use this skill to reconcile ERP spreadsheets.", skills.ScopeChannelProject, "read", "low")
	if len(replies) != 1 || !strings.Contains(replies[0], "Created skill") || componentCount != 1 {
		t.Fatalf("create reply=%#v components=%d", replies, componentCount)
	}
	resolvedProject := manager.CWDPath("channel-1")
	results, err := store.Search(t.Context(), skills.ResolveContext{GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: resolvedProject, EffectiveTools: []string{"read"}}, "ERP", 10)
	if err != nil {
		t.Fatalf("search before enable: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("disabled skill should not be effective before enable: %+v", results)
	}
	listed, err := store.ListInstalled(t.Context(), skills.ResolveContext{GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: resolvedProject, EffectiveTools: []string{"read"}}, "ERP", 10)
	if err != nil {
		t.Fatalf("list before enable: %v", err)
	}
	if len(listed) != 1 || listed[0].Enabled || listed[0].Executable {
		t.Fatalf("created skill should be listed disabled before enable: %+v", listed)
	}
	replies = nil
	b.cmdSkillList(ctx, store, "ERP")
	if len(replies) != 1 || !strings.Contains(replies[0], "disabled") {
		t.Fatalf("manager list reply=%#v", replies)
	}
	replies = nil
	b.cmdSkillGet(ctx, store, "erp-reconcile")
	if len(replies) != 1 || !strings.Contains(replies[0], "Enabled: `false`") {
		t.Fatalf("manager get reply=%#v", replies)
	}
	regularReplies := []string{}
	regularCtx := ctx
	regularCtx.userID = "regular"
	regularCtx.username = "bob"
	regularCtx.reply = func(msg string) { regularReplies = append(regularReplies, msg) }
	regularCtx.replyWithComponents = nil
	b.cmdSkillList(regularCtx, store, "ERP")
	if len(regularReplies) != 1 || !strings.Contains(regularReplies[0], L.Get("skill.list.empty")) {
		t.Fatalf("regular list reply=%#v", regularReplies)
	}
	regularReplies = nil
	b.cmdSkillGet(regularCtx, store, "erp-reconcile")
	if len(regularReplies) != 1 || !strings.Contains(regularReplies[0], L.Get("skill.get.not_visible")) {
		t.Fatalf("regular get reply=%#v", regularReplies)
	}
	replies = nil
	b.cmdSkillSetEnabled(ctx, store, "erp-reconcile", "", true, "enable")
	if len(replies) != 1 || !strings.Contains(replies[0], "enabled") {
		t.Fatalf("enable reply=%#v", replies)
	}
	results, err = store.Search(t.Context(), skills.ResolveContext{GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: resolvedProject, EffectiveTools: []string{"read"}}, "ERP", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || !results[0].Executable || results[0].Slug != "erp-reconcile" {
		t.Fatalf("installed results=%+v", results)
	}
}

func TestSkillSlashLifecycleDisableRestoreHistoryRollback(t *testing.T) {
	dataDir := t.TempDir()
	project := t.TempDir()
	store, err := skills.Open(dataDir)
	if err != nil {
		t.Fatalf("open skills: %v", err)
	}
	defer store.Close()
	sessionStore, err := channel.NewSessionStore(dataDir)
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dataDir, GuildID: "guild-1", Store: sessionStore})
	defer manager.StopAll()
	if err := manager.SetCWD("channel-1", project); err != nil {
		t.Fatalf("set cwd: %v", err)
	}
	resolvedProject := manager.CWDPath("channel-1")
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{userMemberManageOverwrite("manager", discordgo.PermissionManageChannels)})
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "manager"}}); err != nil {
		t.Fatalf("MemberAdd manager: %v", err)
	}
	b := &Bot{discord: ds, manager: manager, skillsStore: store}
	var replies []string
	ctx := cmdCtx{guildID: "guild-1", channelID: "channel-1", targetID: "channel-1", userID: "manager", username: "alice", reply: func(msg string) { replies = append(replies, msg) }}
	first, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: "Rollback SOP", ScopeType: skills.ScopeChannelProject, GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: resolvedProject, ContentMarkdown: "# When to use\nUse v1.", SourceType: skills.SourceMarkdown, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("new first draft: %v", err)
	}
	first, err = store.CreateDraft(t.Context(), first)
	if err != nil {
		t.Fatalf("create first draft: %v", err)
	}
	if _, err := store.InstallDraft(t.Context(), first.DraftID, "alice"); err != nil {
		t.Fatalf("install first: %v", err)
	}
	second, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: "Rollback SOP", ScopeType: skills.ScopeChannelProject, GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: resolvedProject, ContentMarkdown: "# When to use\nUse v2.", SourceType: skills.SourceMarkdown, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("new second draft: %v", err)
	}
	second.ProposedVersion = "2.0.0"
	second, err = store.CreateDraft(t.Context(), second)
	if err != nil {
		t.Fatalf("create second draft: %v", err)
	}
	if _, err := store.InstallDraft(t.Context(), second.DraftID, "alice"); err != nil {
		t.Fatalf("install second: %v", err)
	}
	b.cmdSkillSetEnabled(ctx, store, "rollback-sop", "", false, "disable")
	b.cmdSkillSetEnabled(ctx, store, "rollback-sop", "", true, "restore")
	b.cmdSkillRollback(ctx, store, "rollback-sop", "", "1.0.0")
	other, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: "Rollback SOP", ScopeType: skills.ScopeChannelProject, GuildID: "guild-1", ChannelID: "channel-2", ProjectCWD: resolvedProject, ContentMarkdown: "# When to use\nUse elsewhere.", SourceType: skills.SourceMarkdown, CreatedBy: "mallory"})
	if err != nil {
		t.Fatalf("new other draft: %v", err)
	}
	other.ProposedVersion = "3.0.0"
	other, err = store.CreateDraft(t.Context(), other)
	if err != nil {
		t.Fatalf("create other draft: %v", err)
	}
	if _, err := store.InstallDraft(t.Context(), other.DraftID, "mallory"); err != nil {
		t.Fatalf("install other: %v", err)
	}
	b.cmdSkillHistory(ctx, store, "rollback-sop", "")
	if len(replies) != 4 || !strings.Contains(replies[0], "disabled") || !strings.Contains(replies[1], "restored") || !strings.Contains(replies[2], "rolled back") || !strings.Contains(replies[3], "rollback") || strings.Contains(replies[3], "3.0.0") {
		t.Fatalf("lifecycle replies=%#v", replies)
	}
	got, err := store.GetVisible(t.Context(), skills.ResolveContext{GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: manager.CWDPath("channel-1"), EffectiveTools: []string{}}, "rollback-sop")
	if err != nil {
		t.Fatalf("get visible: %v", err)
	}
	if got.Version != "1.0.0" {
		t.Fatalf("version after rollback = %s", got.Version)
	}
}

func TestSkillEnableAuthorizationUsesCreatedSkillChannel(t *testing.T) {
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{userMemberManageOverwrite("manager", discordgo.PermissionManageChannels)})
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "manager"}}); err != nil {
		t.Fatalf("MemberAdd manager: %v", err)
	}
	b := &Bot{}
	draft := skills.Draft{GuildID: "guild-1", ChannelID: "channel-2", ProposedScopeType: skills.ScopeChannel}
	ctx := cmdCtx{guildID: "guild-1", channelID: "channel-1", targetID: "channel-1", userID: "manager"}
	if b.userCanManageSkillDraft(ds, ctx, draft) {
		t.Fatal("manager of issuing channel authorized for another created skill channel")
	}
}
