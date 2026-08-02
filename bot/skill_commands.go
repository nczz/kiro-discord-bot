package bot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	"github.com/nczz/kiro-discord-bot/internal/skills"
	L "github.com/nczz/kiro-discord-bot/locale"
)

const (
	skillComponentPrefix = "skill"
	skillDraftTTL        = 72 * time.Hour
)

func skillMutationActorFromCmd(ctx cmdCtx, source string) skills.MutationActor {
	return skills.MutationActor{
		GuildID:         ctx.guildID,
		ChannelID:       ctx.channelID,
		TargetChannelID: ctx.targetID,
		ActorUserID:     ctx.userID,
		ActorUsername:   firstNonEmptySkill(ctx.username, ctx.userID),
		SourceMessageID: ctx.messageID,
		InteractionID:   ctx.interactionID,
		MCPServerName:   source,
	}
}

func isPlainSkillInstallConfirmation(content string) bool {
	switch strings.ToLower(strings.TrimSpace(content)) {
	case "install", "安裝":
		return true
	default:
		return false
	}
}

func (b *Bot) handlePlainSkillInstallConfirmation(ctx cmdCtx, content string) bool {
	if !isPlainSkillInstallConfirmation(content) {
		return false
	}
	if b == nil || b.skillsStore == nil {
		ctx.reply(L.Get("skill.error.store_unavailable"))
		return true
	}
	drafts, err := b.skillsStore.ActiveDrafts(context.Background(), b.skillResolveContext(ctx), 2)
	if err != nil {
		ctx.reply(commandError(err))
		return true
	}
	if len(drafts) == 0 {
		ctx.reply(L.Get("skill.confirm.no_draft"))
		return true
	}
	if len(drafts) > 1 {
		ctx.reply(L.Get("skill.confirm.ambiguous"))
		return true
	}
	b.installSkillDraft(ctx, b.skillsStore, drafts[0].DraftID, false, "message", "Discord message skill install confirmation")
	return true
}

func skillSlashOptions() []*discordgo.ApplicationCommandOption {
	str := func(name, desc string, required bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: name, Description: desc, Required: required}
	}
	return []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "list", Description: L.Get("cmd.skill.sub.list"), Options: []*discordgo.ApplicationCommandOption{str("query", L.Get("cmd.skill.opt.query"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "get", Description: L.Get("cmd.skill.sub.get"), Options: []*discordgo.ApplicationCommandOption{str("skill_id", L.Get("cmd.skill.opt.skill_id"), true)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "create", Description: L.Get("cmd.skill.sub.create"), Options: []*discordgo.ApplicationCommandOption{str("name", L.Get("cmd.skill.opt.name"), true), str("content", L.Get("cmd.skill.opt.content"), true), str("scope", L.Get("cmd.skill.opt.scope"), false), str("required_tools", L.Get("cmd.skill.opt.required_tools"), false), str("risk", L.Get("cmd.skill.opt.risk"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "draft", Description: L.Get("cmd.skill.sub.draft"), Options: []*discordgo.ApplicationCommandOption{str("name", L.Get("cmd.skill.opt.name"), true), str("content", L.Get("cmd.skill.opt.content"), true), str("scope", L.Get("cmd.skill.opt.scope"), false), str("required_tools", L.Get("cmd.skill.opt.required_tools"), false), str("risk", L.Get("cmd.skill.opt.risk"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "preview", Description: L.Get("cmd.skill.sub.preview"), Options: []*discordgo.ApplicationCommandOption{str("draft_id", L.Get("cmd.skill.opt.draft_id"), true)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "install", Description: L.Get("cmd.skill.sub.install"), Options: []*discordgo.ApplicationCommandOption{str("draft_id", L.Get("cmd.skill.opt.draft_id"), true)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "discard", Description: L.Get("cmd.skill.sub.discard"), Options: []*discordgo.ApplicationCommandOption{str("draft_id", L.Get("cmd.skill.opt.draft_id"), true)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "disable", Description: L.Get("cmd.skill.sub.disable"), Options: []*discordgo.ApplicationCommandOption{str("skill_id", L.Get("cmd.skill.opt.skill_id"), true), str("scope", L.Get("cmd.skill.opt.scope"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "enable", Description: L.Get("cmd.skill.sub.enable"), Options: []*discordgo.ApplicationCommandOption{str("skill_id", L.Get("cmd.skill.opt.skill_id"), true), str("scope", L.Get("cmd.skill.opt.scope"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "restore", Description: L.Get("cmd.skill.sub.restore"), Options: []*discordgo.ApplicationCommandOption{str("skill_id", L.Get("cmd.skill.opt.skill_id"), true), str("scope", L.Get("cmd.skill.opt.scope"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "rollback", Description: L.Get("cmd.skill.sub.rollback"), Options: []*discordgo.ApplicationCommandOption{str("skill_id", L.Get("cmd.skill.opt.skill_id"), true), str("version", L.Get("cmd.skill.opt.version"), true), str("scope", L.Get("cmd.skill.opt.scope"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "history", Description: L.Get("cmd.skill.sub.history"), Options: []*discordgo.ApplicationCommandOption{str("skill_id", L.Get("cmd.skill.opt.skill_id"), true), str("scope", L.Get("cmd.skill.opt.scope"), false)}},
	}
}

func skillArgsFromSlashOptions(options []*discordgo.ApplicationCommandInteractionDataOption) string {
	if len(options) == 0 {
		return "list"
	}
	sub := options[0]
	parts := []string{sub.Name}
	for _, opt := range sub.Options {
		if value := strings.TrimSpace(opt.StringValue()); value != "" {
			parts = append(parts, shellQuoteSkillArg(value))
		}
	}
	return strings.Join(parts, " ")
}

func shellQuoteSkillArg(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\n\r\"'") {
		return fmt.Sprintf("%q", s)
	}
	return s
}
func (b *Bot) handleSkillSlash(options []*discordgo.ApplicationCommandInteractionDataOption, ctx cmdCtx) {
	store := b.skillsStore
	if store == nil {
		ctx.reply(L.Get("skill.error.store_unavailable"))
		return
	}
	if len(options) == 0 {
		b.cmdSkillList(ctx, store, "")
		return
	}
	sub := options[0]
	opt := func(name string) string {
		for _, item := range sub.Options {
			if item.Name == name {
				return strings.TrimSpace(item.StringValue())
			}
		}
		return ""
	}
	switch sub.Name {
	case "list", "search":
		b.cmdSkillList(ctx, store, opt("query"))
	case "get":
		b.cmdSkillGet(ctx, store, opt("skill_id"))
	case "draft":
		b.cmdSkillDraft(ctx, store, opt("name"), opt("content"), firstNonEmptySkill(opt("scope"), skills.ScopeChannelProject), opt("required_tools"), firstNonEmptySkill(opt("risk"), "low"))
	case "preview":
		b.cmdSkillPreview(ctx, store, opt("draft_id"))
	case "create":
		b.cmdSkillCreate(ctx, store, opt("name"), opt("content"), firstNonEmptySkill(opt("scope"), skills.ScopeChannelProject), opt("required_tools"), firstNonEmptySkill(opt("risk"), "low"))
	case "install":
		b.cmdSkillInstall(ctx, store, opt("draft_id"), false)
	case "discard":
		b.cmdSkillDiscard(ctx, store, opt("draft_id"))
	case "disable":
		b.cmdSkillSetEnabled(ctx, store, opt("skill_id"), opt("scope"), false, "disable")
	case "enable":
		b.cmdSkillSetEnabled(ctx, store, opt("skill_id"), opt("scope"), true, "enable")
	case "restore":
		b.cmdSkillSetEnabled(ctx, store, opt("skill_id"), opt("scope"), true, "restore")
	case "rollback":
		b.cmdSkillRollback(ctx, store, opt("skill_id"), opt("scope"), opt("version"))
	case "history":
		b.cmdSkillHistory(ctx, store, opt("skill_id"), opt("scope"))
	default:
		ctx.reply(L.Get("skill.error.unknown_action"))
	}
}

func (b *Bot) cmdSkill(ctx cmdCtx) {
	store := b.skillsStore
	if store == nil {
		ctx.reply(L.Get("skill.error.store_unavailable"))
		return
	}
	fields := strings.Fields(ctx.args)
	action := "list"
	if len(fields) > 0 {
		action = strings.ToLower(fields[0])
	}
	switch action {
	case "", "list", "search":
		query := strings.TrimSpace(strings.TrimPrefix(ctx.args, fieldsFirst(ctx.args)))
		b.cmdSkillList(ctx, store, query)
	case "get":
		if len(fields) < 2 {
			ctx.reply(L.Get("skill.usage.get"))
			return
		}
		b.cmdSkillGet(ctx, store, fields[1])
	case "create", "draft":
		args, err := parseSkillArgs(strings.TrimSpace(strings.TrimPrefix(ctx.args, fieldsFirst(ctx.args))))
		if err != nil || len(args) < 2 {
			ctx.reply(L.Get("skill.usage.create"))
			return
		}
		name, content := args[0], args[1]
		scope, required, risk := skills.ScopeChannelProject, "", "low"
		if len(args) > 2 && args[2] != "" {
			scope = args[2]
		}
		if len(args) > 3 {
			required = args[3]
		}
		if len(args) > 4 && args[4] != "" {
			risk = args[4]
		}
		if action == "draft" {
			b.cmdSkillDraft(ctx, store, name, content, scope, required, risk)
		} else {
			b.cmdSkillCreate(ctx, store, name, content, scope, required, risk)
		}
	case "preview":
		if len(fields) < 2 {
			ctx.reply(L.Get("skill.usage.preview"))
			return
		}
		b.cmdSkillPreview(ctx, store, fields[1])
	case "install":
		if len(fields) < 2 {
			ctx.reply(L.Get("skill.usage.install"))
			return
		}
		b.cmdSkillInstall(ctx, store, fields[1], false)
	case "discard", "reject":
		if len(fields) < 2 {
			ctx.reply(L.Get("skill.usage.discard"))
			return
		}
		b.cmdSkillDiscard(ctx, store, fields[1])
	case "disable":
		if len(fields) < 2 {
			ctx.reply(L.Get("skill.usage.disable"))
			return
		}
		scope := ""
		if len(fields) > 2 {
			scope = fields[2]
		}
		b.cmdSkillSetEnabled(ctx, store, fields[1], scope, false, "disable")
	case "enable", "restore":
		if len(fields) < 2 {
			usageKey := "skill.usage.enable"
			if action == "restore" {
				usageKey = "skill.usage.restore"
			}
			ctx.reply(L.Get(usageKey))
			return
		}
		scope := ""
		if len(fields) > 2 {
			scope = fields[2]
		}
		b.cmdSkillSetEnabled(ctx, store, fields[1], scope, true, action)
	case "rollback":
		if len(fields) < 3 {
			ctx.reply(L.Get("skill.usage.rollback"))
			return
		}
		scope := ""
		if len(fields) > 3 {
			scope = fields[3]
		}
		b.cmdSkillRollback(ctx, store, fields[1], scope, fields[2])
	case "history":
		if len(fields) < 2 {
			ctx.reply(L.Get("skill.usage.history"))
			return
		}
		scope := ""
		if len(fields) > 2 {
			scope = fields[2]
		}
		b.cmdSkillHistory(ctx, store, fields[1], scope)
	default:
		ctx.reply(L.Get("skill.usage.general"))
	}
}

func fieldsFirst(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func parseSkillArgs(raw string) ([]string, error) {
	var args []string
	for raw = strings.TrimSpace(raw); raw != ""; raw = strings.TrimSpace(raw) {
		if raw[0] != '"' {
			idx := strings.IndexAny(raw, " \t\n\r")
			if idx < 0 {
				args = append(args, raw)
				break
			}
			args = append(args, raw[:idx])
			raw = raw[idx+1:]
			continue
		}
		value, rest, err := parseQuotedSkillArg(raw)
		if err != nil {
			return nil, err
		}
		args = append(args, value)
		raw = rest
	}
	return args, nil
}

func parseQuotedSkillArg(raw string) (string, string, error) {
	var sb strings.Builder
	escaped := false
	for i := 1; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			switch ch {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			default:
				sb.WriteByte(ch)
			}
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return sb.String(), raw[i+1:], nil
		}
		sb.WriteByte(ch)
	}
	return "", "", errors.New("unterminated quoted argument")
}

func (b *Bot) cmdSkillList(ctx cmdCtx, store *skills.Store, query string) {
	rc := b.skillResolveContext(ctx)
	var (
		results []skills.ResolvedSkill
		err     error
	)
	if b.userCanManageAuditTarget(b.discord, ctx.userID, ctx.targetID) {
		results, err = store.ListInstalled(context.Background(), rc, query, 10)
	} else {
		results, err = store.Search(context.Background(), rc, query, 10)
	}
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	if len(results) == 0 {
		ctx.reply(L.Get("skill.list.empty"))
		return
	}
	var sb strings.Builder
	sb.WriteString(L.Get("skill.list.header") + "\n")
	for _, skill := range results {
		state := L.Get("skill.list.ready")
		if !skill.Enabled {
			state = L.Get("skill.list.disabled")
		} else if !skill.Executable {
			state = L.Getf("skill.list.missing_tools", strings.Join(skill.MissingTools, ", "))
		}
		fmt.Fprintf(&sb, "- `%s` v%s (%s, %s): %s\n", skill.Slug, skill.Version, skill.ScopeType, state, skill.Description)
	}
	ctx.reply(sb.String())
}

func (b *Bot) cmdSkillGet(ctx cmdCtx, store *skills.Store, skillID string) {
	rc := b.skillResolveContext(ctx)
	skill, err := store.GetVisible(context.Background(), rc, skillID)
	if errors.Is(err, sql.ErrNoRows) && b.userCanManageAuditTarget(b.discord, ctx.userID, ctx.targetID) {
		skill, err = store.GetInstalled(context.Background(), rc, skillID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		ctx.reply(L.Get("skill.get.not_visible"))
		return
	}
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	content := strings.TrimSpace(skill.ContentMarkdown)
	if content == "" {
		content = skill.Description
	}
	ctx.reply(L.Getf("skill.get.details", skill.Name, skill.Slug, skill.Version, skill.Enabled, skill.Executable, strings.Join(skill.MissingTools, ", "), content))
}

func (b *Bot) commandSkillDraft(ctx cmdCtx, name, content, scope, required, risk string) (skills.Draft, bool) {
	if !b.userCanManageAuditTarget(b.discord, ctx.userID, ctx.targetID) {
		ctx.reply(L.Get("skill.permission.channel_draft"))
		return skills.Draft{}, false
	}
	projectCWD := ""
	if scope == skills.ScopeProject || scope == skills.ScopeChannelProject || scope == "" {
		projectCWD = b.targetSkillCWD(ctx)
		if strings.TrimSpace(projectCWD) == "" && (scope == skills.ScopeProject || scope == skills.ScopeChannelProject || scope == "") {
			ctx.reply(L.Get("skill.error.project_cwd_required"))
			return skills.Draft{}, false
		}
	}
	draft, err := skills.NewDraftFromMarkdown(skills.DraftInput{
		Name:            name,
		Description:     content,
		ScopeType:       firstNonEmptySkill(scope, skills.ScopeChannelProject),
		GuildID:         ctx.guildID,
		ChannelID:       ctx.channelID,
		ProjectCWD:      projectCWD,
		SourceType:      skills.SourceConversation,
		SourceRef:       ctx.messageID,
		ContentMarkdown: content,
		RequiredTools:   parseSkillToolCSV(required),
		RiskLevel:       risk,
		CreatedBy:       firstNonEmptySkill(ctx.username, ctx.userID),
		TTL:             skillDraftTTL,
	})
	if err != nil {
		ctx.reply(commandError(err))
		return skills.Draft{}, false
	}
	return draft, true
}

func (b *Bot) cmdSkillDraft(ctx cmdCtx, store *skills.Store, name, content, scope, required, risk string) {
	draft, ok := b.commandSkillDraft(ctx, name, content, scope, required, risk)
	if !ok {
		return
	}
	var err error
	draft, err = store.CreateDraft(context.Background(), draft)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	ctx.sendReplyWithComponents(skillDraftReview(draft), skillDraftComponents(draft.DraftID, ctx.channelID), map[string]any{"skill_draft_id": draft.DraftID, "has_components": true})
}

func (b *Bot) cmdSkillCreate(ctx cmdCtx, store *skills.Store, name, content, scope, required, risk string) {
	draft, ok := b.commandSkillDraft(ctx, name, content, scope, required, risk)
	if !ok {
		return
	}
	var err error
	draft, err = store.CreateDraft(context.Background(), draft)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	materializedPath, materializedSHA, err := materializeSkillDraftIfNeeded(draft, false)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	install, err := store.CreateDisabledInstallFromDraftWithMaterializationAndAudit(context.Background(), draft.DraftID, skillMutationActorFromCmd(ctx, "slash"), "Discord /skill create request", materializedPath, materializedSHA)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	ctx.sendReplyWithComponents(skillCreatedSummary(draft, install), skillCreatedComponents(draft.DraftID, ctx.channelID), map[string]any{"skill_id": install.SkillID, "has_components": true})
}

func (b *Bot) cmdSkillPreview(ctx cmdCtx, store *skills.Store, draftID string) {
	draft, err := store.GetDraft(context.Background(), draftID)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	ctx.sendReplyWithComponents(skillDraftReview(draft), skillDraftComponents(draft.DraftID, ctx.channelID), map[string]any{"skill_draft_id": draft.DraftID, "has_components": true})
}

func (b *Bot) cmdSkillInstall(ctx cmdCtx, store *skills.Store, draftID string, overwrite bool) {
	b.installSkillDraft(ctx, store, draftID, overwrite, "slash", "Discord /skill install confirmation")
}

func (b *Bot) installSkillDraft(ctx cmdCtx, store *skills.Store, draftID string, overwrite bool, source, reason string) {
	draft, err := store.GetDraft(context.Background(), draftID)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	if !b.userCanManageSkillDraft(b.discord, ctx, draft) {
		ctx.reply(L.Get("skill.permission.draft_scope"))
		return
	}
	materializedPath, materializedSHA, err := materializeSkillDraftIfNeeded(draft, overwrite)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	install, err := store.InstallDraftWithMaterializationAndAudit(context.Background(), draft.DraftID, skillMutationActorFromCmd(ctx, source), reason, materializedPath, materializedSHA)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	ctx.reply(L.Getf("skill.install.success", install.SkillID, install.Version, install.ScopeType))
}

func (b *Bot) cmdSkillDiscard(ctx cmdCtx, store *skills.Store, draftID string) {
	draft, err := store.GetDraft(context.Background(), draftID)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	if !b.userCanManageSkillDraft(b.discord, ctx, draft) {
		ctx.reply(L.Get("skill.permission.draft_scope"))
		return
	}
	if _, err := store.DiscardDraftWithAudit(context.Background(), draftID, skillMutationActorFromCmd(ctx, "slash"), "Discord /skill discard confirmation"); err != nil {
		ctx.reply(commandError(err))
		return
	}
	ctx.reply(L.Getf("skill.discard.success", draftID))
}

func (b *Bot) cmdSkillSetEnabled(ctx cmdCtx, store *skills.Store, skillID, scopeArg string, enabled bool, action string) {
	scope := b.skillLifecycleScope(ctx, scopeArg)
	if !b.userCanManageSkillScope(ctx, scope) {
		ctx.reply(L.Get("skill.permission.scope"))
		return
	}
	ev, err := store.SetInstallEnabled(context.Background(), b.skillResolveContext(ctx), skillID, scope, enabled, skillMutationActorFromCmd(ctx, "slash"), "Discord /skill "+action+" confirmation", action)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	successKey := "skill.disable.success"
	if enabled && action == "enable" {
		successKey = "skill.enable.success"
	} else if enabled {
		successKey = "skill.restore.success"
	}
	ctx.reply(L.Getf(successKey, ev.SkillID, scope, ev.EventID))
}

func (b *Bot) cmdSkillRollback(ctx cmdCtx, store *skills.Store, skillID, scopeArg, version string) {
	scope := b.skillLifecycleScope(ctx, scopeArg)
	if !b.userCanManageSkillScope(ctx, scope) {
		ctx.reply(L.Get("skill.permission.scope"))
		return
	}
	ev, err := store.RollbackInstall(context.Background(), b.skillResolveContext(ctx), skillID, scope, version, skillMutationActorFromCmd(ctx, "slash"), "Discord /skill rollback confirmation")
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	ctx.reply(L.Getf("skill.rollback.success", ev.SkillID, ev.VersionBefore, ev.VersionAfter, scope, ev.EventID))
}

func (b *Bot) cmdSkillHistory(ctx cmdCtx, store *skills.Store, skillID, scopeArg string) {
	scope := b.skillLifecycleScope(ctx, scopeArg)
	if !b.userCanManageSkillScope(ctx, scope) {
		ctx.reply(L.Get("skill.permission.history"))
		return
	}
	events, err := store.MutationHistoryForContext(context.Background(), b.skillResolveContext(ctx), skillID, scope, 10)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	if len(events) == 0 {
		ctx.reply(L.Get("skill.history.empty"))
		return
	}
	var sb strings.Builder
	sb.WriteString(L.Get("skill.history.header") + "\n")
	for _, ev := range events {
		fmt.Fprintf(&sb, L.Get("skill.history.item")+"\n", ev.Action, ev.VersionBefore, ev.VersionAfter, ev.StatusAfter, firstNonEmptySkill(ev.ActorUsername, ev.ActorUserID), ev.OccurredAt.Format(time.RFC3339), ev.EventID)
	}
	ctx.reply(sb.String())
}

func (b *Bot) skillLifecycleScope(ctx cmdCtx, scopeArg string) string {
	if scope := skills.NormalizeScope(scopeArg); scope != "" {
		return scope
	}
	if strings.TrimSpace(b.targetSkillCWD(ctx)) != "" {
		return skills.ScopeChannelProject
	}
	return skills.ScopeChannel
}

func (b *Bot) userCanManageSkillScope(ctx cmdCtx, scope string) bool {
	switch skills.NormalizeScope(scope) {
	case skills.ScopeGuild:
		return b.userCanManageUsageGuild(b.discord, ctx.userID, ctx.targetID)
	case skills.ScopeChannel, skills.ScopeProject, skills.ScopeChannelProject:
		return b.userCanManageAuditTarget(b.discord, ctx.userID, ctx.targetID)
	default:
		return false
	}
}

func materializeSkillDraftIfNeeded(draft skills.Draft, overwrite bool) (string, string, error) {
	if draft.ProposedScopeType != skills.ScopeProject && draft.ProposedScopeType != skills.ScopeChannelProject {
		return "", "", nil
	}
	cleanCWD, err := skills.ValidateProjectCWD(draft.ProjectCWD, allowedCwdRootsFromEnv())
	if err != nil {
		return "", "", err
	}
	file, err := skills.Materialize(cleanCWD, draft.ProposedSlug, draft.ProposedContentMarkdown, overwrite)
	if err != nil {
		if errors.Is(err, skills.ErrMaterializedDrift) {
			return "", "", fmt.Errorf("%s", L.Get("skill.error.materialized_drift"))
		}
		return "", "", err
	}
	return file.RelativePath, file.SHA256, nil
}

func skillCreatedSummary(draft skills.Draft, install skills.Install) string {
	tools, _ := skills.RequiredToolsFromJSON(draft.RequiredToolsJSON)
	toolText := L.Get("skill.tool.none")
	if len(tools) > 0 {
		toolText = strings.Join(tools, ", ")
	}
	return L.Getf("skill.create.success", install.SkillID, install.Version, install.ScopeType, skillsRiskFromDraft(draft), toolText)
}

func skillDraftSummary(draft skills.Draft) string {
	tools, _ := skills.RequiredToolsFromJSON(draft.RequiredToolsJSON)
	toolText := L.Get("skill.tool.none")
	if len(tools) > 0 {
		toolText = strings.Join(tools, ", ")
	}
	return L.Getf("skill.draft.summary", draft.DraftID, draft.ProposedName, draft.ProposedScopeType, skillsRiskFromDraft(draft), toolText, draft.DraftID)
}

func skillDraftReview(draft skills.Draft) string {
	content := truncateDiscordMessageContent(draft.ProposedContentMarkdown, 1200)
	if content == "" {
		content = L.Get("skill.draft.empty")
	}
	return skillDraftSummary(draft) + "\n\n" + L.Get("skill.draft.content_header") + "\n" + content
}

func (b *Bot) userCanManageSkillDraft(ds *discordgo.Session, ctx cmdCtx, draft skills.Draft) bool {
	if draft.GuildID != "" && ctx.guildID != "" && draft.GuildID != ctx.guildID {
		return false
	}
	targetID := firstNonEmptySkill(draft.ChannelID, ctx.channelID, ctx.targetID)
	switch draft.ProposedScopeType {
	case skills.ScopeGuild:
		return b.userCanManageUsageGuild(ds, ctx.userID, firstNonEmptySkill(targetID, ctx.targetID))
	case skills.ScopeChannel, skills.ScopeProject, skills.ScopeChannelProject:
		return b.userCanManageAuditTarget(ds, ctx.userID, targetID)
	default:
		return false
	}
}

func skillsRiskFromDraft(draft skills.Draft) string {
	if strings.Contains(draft.RiskReportJSON, "critical") {
		return "critical"
	}
	if strings.Contains(draft.RiskReportJSON, "high") {
		return "high"
	}
	if strings.Contains(draft.RiskReportJSON, "medium") {
		return "medium"
	}
	return "low"
}

func skillDraftComponents(draftID, channelID string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: L.Get("skill.button.install"), Style: discordgo.SuccessButton, CustomID: strings.Join([]string{skillComponentPrefix, "install", channelID, draftID}, ":")},
		discordgo.Button{Label: L.Get("skill.button.discard"), Style: discordgo.DangerButton, CustomID: strings.Join([]string{skillComponentPrefix, "discard", channelID, draftID}, ":")},
	}}}
}

func skillCreatedComponents(draftID, channelID string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: L.Get("skill.button.enable"), Style: discordgo.SuccessButton, CustomID: strings.Join([]string{skillComponentPrefix, "enable", channelID, draftID}, ":")},
	}}}
}

func (b *Bot) handleSkillComponent(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	parts := strings.Split(i.MessageComponentData().CustomID, ":")
	if len(parts) != 4 || parts[0] != skillComponentPrefix {
		return
	}
	action, channelID, draftID := parts[1], parts[2], parts[3]
	userID, username := interactionUser(i)
	store := b.skillsStore
	if store == nil {
		respondInteractionEphemeral(ds, i, L.Get("skill.error.store_unavailable"))
		return
	}
	draft, err := store.GetDraft(context.Background(), draftID)
	if err != nil {
		respondInteractionEphemeral(ds, i, commandError(err))
		return
	}
	if !b.userCanManageSkillDraft(ds, cmdCtx{guildID: i.GuildID, channelID: channelID, targetID: channelID, userID: userID}, draft) {
		respondInteractionEphemeral(ds, i, L.Get("skill.permission.draft_scope"))
		return
	}
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredMessageUpdate})
	content := ""
	components := []discordgo.MessageComponent(nil)
	switch action {
	case "discard":
		if _, err := store.DiscardDraftWithAudit(context.Background(), draftID, skills.MutationActor{GuildID: i.GuildID, ChannelID: channelID, TargetChannelID: channelID, ActorUserID: userID, ActorUsername: username, InteractionID: i.ID}, "Discord review-button discard confirmation"); err != nil {
			content = commandError(err)
		} else {
			content = L.Getf("skill.discard.success", draftID)
		}
	case "install":
		materializedPath, materializedSHA, err := materializeSkillDraftIfNeeded(draft, false)
		if err != nil {
			content = commandError(err)
		} else if install, err := store.InstallDraftWithMaterializationAndAudit(context.Background(), draft.DraftID, skills.MutationActor{GuildID: i.GuildID, ChannelID: channelID, TargetChannelID: channelID, ActorUserID: userID, ActorUsername: username, InteractionID: i.ID}, "Discord review-button install confirmation", materializedPath, materializedSHA); err != nil {
			content = commandError(err)
		} else {
			content = L.Getf("skill.install.success", install.SkillID, install.Version, install.ScopeType)
		}
	case "enable":
		ctx := cmdCtx{guildID: i.GuildID, channelID: channelID, targetID: channelID, userID: userID, username: username}
		skillID := firstNonEmptySkill(draft.ProposedSkillID, draft.ProposedSlug)
		ev, err := store.SetInstallEnabled(context.Background(), b.skillResolveContext(ctx), skillID, draft.ProposedScopeType, true, skills.MutationActor{GuildID: i.GuildID, ChannelID: channelID, TargetChannelID: channelID, ActorUserID: userID, ActorUsername: username, InteractionID: i.ID}, "Discord enable-button confirmation", "enable")
		if err != nil {
			content = commandError(err)
		} else {
			content = L.Getf("skill.enable.success", ev.SkillID, ev.ScopeType, ev.EventID)
		}
	default:
		content = L.Get("error.expired")
	}
	content = secrets.RedactEnv(content)
	if _, err := ds.InteractionResponseEdit(i.Interaction, webhookEdit(content, components)); err != nil {
		log.Printf("[skill-ui] interaction edit failed action=%s channel=%s draft=%s: %v", action, channelID, draftID, err)
	}
}

func (b *Bot) skillResolveContext(ctx cmdCtx) skills.ResolveContext {
	effective, allowAll, readOnly := b.effectiveMCPTools(ctx.channelID)
	return skills.ResolveContext{GuildID: ctx.guildID, ChannelID: ctx.channelID, ParentChannelID: ctx.channelID, TargetID: ctx.targetID, ProjectCWD: b.targetSkillCWD(ctx), EffectiveTools: effective, AllowAllTools: allowAll, ReadOnlyPolicy: readOnly}
}

func (b *Bot) targetSkillCWD(ctx cmdCtx) string {
	if b == nil || b.manager == nil {
		return ""
	}
	return b.manager.TargetCWDPath(ctx.targetID, ctx.channelID)
}

func (b *Bot) effectiveMCPTools(channelID string) ([]string, bool, bool) {
	if b == nil || b.manager == nil {
		return nil, false, true
	}
	views, err := b.manager.MCPServerViews(channelID)
	if err != nil {
		return nil, false, true
	}
	seen := map[string]struct{}{}
	readOnly := true
	allowAll := false
	for _, view := range views {
		p := view.Policy
		if !p.Enabled {
			continue
		}
		if p.AllowAllTools {
			allowAll = true
		}
		if !p.ReadOnly {
			readOnly = false
			for _, tool := range p.EffectiveTools() {
				seen[tool] = struct{}{}
			}
		}
	}
	tools := make([]string, 0, len(seen))
	for tool := range seen {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	return tools, allowAll, readOnly
}

func parseSkillToolCSV(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func allowedCwdRootsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("ALLOWED_CWD_ROOTS"))
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ':' || r == ';' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstNonEmptySkill(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
