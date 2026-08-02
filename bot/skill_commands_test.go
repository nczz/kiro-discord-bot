package bot

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/skills"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestSkillSlashCommandsIncludeReviewFlow(t *testing.T) {
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
	for _, name := range []string{"list", "get", "draft", "preview", "install", "discard", "disable", "restore", "rollback", "history"} {
		if !seen[name] {
			t.Fatalf("/skill missing subcommand %s", name)
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
	components := skillDraftComponents("draft-1", "channel-1")
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok || len(row.Components) != 2 {
		t.Fatalf("draft components = %#v", components)
	}
	install, ok := row.Components[0].(discordgo.Button)
	if !ok || install.Label != "安裝" {
		t.Fatalf("install button = %#v, want zh-TW label", row.Components[0])
	}
}

func TestSkillDraftCommandCreatesReviewButtonsAndInstall(t *testing.T) {
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
	b := &Bot{discord: ds, manager: manager, skillsStore: store}
	var replies []string
	var componentCount int
	ctx := cmdCtx{guildID: "guild-1", channelID: "channel-1", targetID: "channel-1", userID: "manager", username: "alice", reply: func(msg string) { replies = append(replies, msg) }, replyWithComponents: func(msg string, components []discordgo.MessageComponent, _ map[string]any) {
		replies = append(replies, msg)
		componentCount = len(components)
	}}
	b.cmdSkillDraft(ctx, store, "ERP Reconcile", "Use this skill to reconcile ERP spreadsheets.", skills.ScopeChannelProject, "read", "low")
	if len(replies) != 1 || !strings.Contains(replies[0], "Skill draft ready") || componentCount != 1 {
		t.Fatalf("draft reply=%#v components=%d", replies, componentCount)
	}
	resolvedProject := manager.CWDPath("channel-1")
	drafts, err := store.Search(t.Context(), skills.ResolveContext{GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: resolvedProject, EffectiveTools: []string{"read"}}, "ERP", 10)
	if err != nil {
		t.Fatalf("search before install: %v", err)
	}
	if len(drafts) != 0 {
		t.Fatalf("draft should not be visible before install: %+v", drafts)
	}
	previewDraftID := draftIDFromReply(replies[0])
	if previewDraftID == "" {
		t.Fatalf("draft id missing from reply %q", replies[0])
	}
	replies = nil
	b.cmdSkillInstall(ctx, store, previewDraftID, false)
	if len(replies) != 1 || !strings.Contains(replies[0], "Installed skill") {
		t.Fatalf("install reply=%#v", replies)
	}
	results, err := store.Search(t.Context(), skills.ResolveContext{GuildID: "guild-1", ChannelID: "channel-1", ProjectCWD: resolvedProject, EffectiveTools: []string{"read"}}, "ERP", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || !results[0].Executable || results[0].Slug != "erp-reconcile" {
		t.Fatalf("installed results=%+v", results)
	}
}

func TestPlainSkillInstallConfirmationInstallsOnlyActiveDraft(t *testing.T) {
	L.Load("en")
	dataDir := t.TempDir()
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
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{userMemberManageOverwrite("manager", discordgo.PermissionManageChannels)})
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "manager", Username: "alice"}}); err != nil {
		t.Fatalf("MemberAdd manager: %v", err)
	}
	rt := &recordingDiscordTransport{}
	ds.Client = &http.Client{Transport: rt}
	draft, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: "Install Reply SOP", ScopeType: skills.ScopeChannel, GuildID: "guild-1", ChannelID: "channel-1", ContentMarkdown: "# Steps\nReply install confirms this draft.", SourceType: skills.SourceConversation, CreatedBy: "agent"})
	if err != nil {
		t.Fatalf("new draft: %v", err)
	}
	if _, err := store.CreateDraft(t.Context(), draft); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	b := &Bot{discord: ds, manager: manager, skillsStore: store, seen: newSeenMessages()}
	defer b.seen.Stop()
	b.handleMessage(ds, &discordgo.MessageCreate{Message: &discordgo.Message{ID: "msg-install", ChannelID: "channel-1", GuildID: "guild-1", Content: "install", Author: &discordgo.User{ID: "manager", Username: "alice"}}})
	paths, bodies := rt.Snapshot()
	if len(paths) != 2 || !strings.Contains(paths[1], "/channels/channel-1/messages") || !strings.Contains(bodies[1], "Installed skill") {
		t.Fatalf("reply paths=%#v bodies=%#v", paths, bodies)
	}
	got, err := store.GetVisible(t.Context(), skills.ResolveContext{GuildID: "guild-1", ChannelID: "channel-1"}, "install-reply-sop")
	if err != nil {
		t.Fatalf("get visible: %v", err)
	}
	if got.SkillID == "" || got.ScopeType != skills.ScopeChannel {
		t.Fatalf("installed skill=%+v", got)
	}
}

func TestPlainSkillInstallConfirmationWorksInsideThread(t *testing.T) {
	L.Load("zh-TW")
	defer L.Load("en")
	dataDir := t.TempDir()
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
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{userMemberManageOverwrite("manager", discordgo.PermissionManageChannels)})
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "manager", Username: "alice"}}); err != nil {
		t.Fatalf("MemberAdd manager: %v", err)
	}
	rt := &recordingDiscordTransport{}
	ds.Client = &http.Client{Transport: rt}
	draft, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: "Thread Reply SOP", ScopeType: skills.ScopeChannel, GuildID: "guild-1", ChannelID: "channel-1", ContentMarkdown: "# Steps\nReply install confirms from a thread.", SourceType: skills.SourceConversation, CreatedBy: "agent"})
	if err != nil {
		t.Fatalf("new draft: %v", err)
	}
	if _, err := store.CreateDraft(t.Context(), draft); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	b := &Bot{discord: ds, manager: manager, skillsStore: store, seen: newSeenMessages()}
	defer b.seen.Stop()
	registerThreadParent("thread-1", "channel-1")
	b.handleMessage(ds, &discordgo.MessageCreate{Message: &discordgo.Message{ID: "msg-thread-install", ChannelID: "thread-1", GuildID: "guild-1", Content: "安裝", Author: &discordgo.User{ID: "manager", Username: "alice"}}})
	paths, bodies := rt.Snapshot()
	if len(paths) == 0 || !strings.Contains(paths[len(paths)-1], "/channels/thread-1/messages") || !strings.Contains(bodies[len(bodies)-1], "已為") {
		t.Fatalf("reply paths=%#v bodies=%#v", paths, bodies)
	}
	got, err := store.GetVisible(t.Context(), skills.ResolveContext{GuildID: "guild-1", ChannelID: "channel-1"}, "thread-reply-sop")
	if err != nil {
		t.Fatalf("get visible: %v", err)
	}
	if got.SkillID == "" || got.ScopeType != skills.ScopeChannel {
		t.Fatalf("installed skill=%+v", got)
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

func TestSkillDraftReviewIncludesContent(t *testing.T) {
	draft, err := skills.NewDraftFromMarkdown(skills.DraftInput{Name: "Review Me", ScopeType: skills.ScopeChannel, GuildID: "guild-1", ChannelID: "channel-1", ContentMarkdown: "Follow the exact reconciliation checklist.", SourceType: skills.SourceConversation})
	if err != nil {
		t.Fatalf("new draft: %v", err)
	}
	review := skillDraftReview(draft)
	if !strings.Contains(review, "Draft content") || !strings.Contains(review, "reconciliation checklist") {
		t.Fatalf("review omitted draft content: %q", review)
	}
}

func TestSkillInstallAuthorizationUsesDraftChannel(t *testing.T) {
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{userMemberManageOverwrite("manager", discordgo.PermissionManageChannels)})
	if err := ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "manager"}}); err != nil {
		t.Fatalf("MemberAdd manager: %v", err)
	}
	b := &Bot{}
	draft := skills.Draft{GuildID: "guild-1", ChannelID: "channel-2", ProposedScopeType: skills.ScopeChannel}
	ctx := cmdCtx{guildID: "guild-1", channelID: "channel-1", targetID: "channel-1", userID: "manager"}
	if b.userCanManageSkillDraft(ds, ctx, draft) {
		t.Fatal("manager of issuing channel authorized for another draft channel")
	}
}

func draftIDFromReply(reply string) string {
	marker := "Draft: `"
	idx := strings.Index(reply, marker)
	if idx < 0 {
		return ""
	}
	rest := reply[idx+len(marker):]
	end := strings.Index(rest, "`")
	if end < 0 {
		return ""
	}
	return rest[:end]
}
