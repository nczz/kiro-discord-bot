package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	L "github.com/nczz/kiro-discord-bot/locale"
)

const (
	cwdCustomPrefix   = "cwdui"
	cwdPageSize       = 25
	cwdProjectTokenID = "p_"
)

func (b *Bot) handleCWDSlash(ds *discordgo.Session, i *discordgo.InteractionCreate, auditCtx cmdCtx) {
	const command = "cwd"
	path := ""
	data := i.ApplicationCommandData()
	if len(data.Options) > 0 {
		path = strings.TrimSpace(data.Options[0].StringValue())
	}
	if path != "" {
		b.respondInteractionForCommand(ds, i, auditCtx, command, b.applyCWDPath(ds, auditCtx, path), map[string]any{"cwd_mode": "path"})
		return
	}
	err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	b.recordInteractionResponseDelivery(auditCtx, command, "deferred", "", discordgo.InteractionResponseDeferredChannelMessageWithSource, map[string]any{"ephemeral": true}, err)
	b.sendCWDPanel(ds, i, auditCtx, "")
}

func (b *Bot) applyCWDPath(ds *discordgo.Session, ctx cmdCtx, path string) string {
	if !b.userCanManageAuditTarget(ds, ctx.userID, ctx.targetID) {
		return L.Get("cwd.forbidden")
	}
	if err := b.manager.SetCWD(ctx.channelID, path); err != nil {
		return commandError(err)
	}
	return L.Getf("cwd.set", path)
}

func (b *Bot) sendCWDPanel(ds *discordgo.Session, i *discordgo.InteractionCreate, ctx cmdCtx, prefix string) {
	content, components := b.buildCWDPanel(ctx.channelID, prefix)
	content = secrets.RedactEnv(content)
	sent, err := ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content:    content,
		Components: components,
		Flags:      discordgo.MessageFlagsEphemeral,
	})
	b.recordCommandResponseDelivery(ctx, "cwd", "slash", "sent", content, map[string]any{"has_components": len(components) > 0, "ephemeral": true, "cwd_ui": "panel"}, sent, err)
}

func (b *Bot) buildCWDPanel(channelID, prefix string) (string, []discordgo.MessageComponent) {
	return b.buildCWDPanelPage(channelID, prefix, 0)
}

func (b *Bot) buildCWDPanelPage(channelID, prefix string, page int) (string, []discordgo.MessageComponent) {
	var sb strings.Builder
	if prefix != "" {
		sb.WriteString(prefix)
		sb.WriteString("\n\n")
	}
	sb.WriteString(L.Get("cwd.setup.header"))
	sb.WriteString("\n")
	sb.WriteString(b.manager.CWD(channelID))
	sb.WriteString("\n")
	if root, err := b.manager.DefaultProjectRoot(); err == nil {
		sb.WriteString(L.Getf("cwd.setup.default_root", filepath.Base(root)))
	} else {
		sb.WriteString(commandError(err))
		return sb.String(), []discordgo.MessageComponent{cwdActionButtons(channelID, 0, 0)}
	}

	projects, err := b.manager.ListDefaultProjects()
	if err != nil {
		sb.WriteString("\n")
		sb.WriteString(commandError(err))
		return sb.String(), []discordgo.MessageComponent{cwdActionButtons(channelID, 0, 0)}
	}
	if len(projects) == 0 {
		sb.WriteString("\n")
		sb.WriteString(L.Get("cwd.setup.no_projects"))
		return sb.String(), []discordgo.MessageComponent{cwdActionButtons(channelID, 0, 0)}
	}
	sb.WriteString("\n")
	sb.WriteString(L.Getf("cwd.setup.project_count", len(projects)))
	page = normalizeCWDProjectPage(page, len(projects))
	start, end := cwdProjectPageBounds(len(projects), page)
	sb.WriteString("\n")
	sb.WriteString(L.Getf("cwd.setup.project_page", start+1, end, len(projects)))
	options := cwdProjectOptions(projects[start:end])
	if len(options) == 0 {
		sb.WriteString("\n")
		sb.WriteString(L.Get("cwd.setup.no_selectable_projects"))
		return sb.String(), []discordgo.MessageComponent{cwdActionButtons(channelID, page, len(projects))}
	}
	return sb.String(), []discordgo.MessageComponent{
		cwdProjectSelect(channelID, page, options),
		cwdActionButtons(channelID, page, len(projects)),
	}
}

func cwdProjectOptions(projects []channel.ProjectOption) []discordgo.SelectMenuOption {
	options := []discordgo.SelectMenuOption{}
	for _, project := range projects {
		if len(options) >= cwdPageSize {
			break
		}
		if strings.TrimSpace(project.Relative) == "" {
			continue
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:       truncateDiscordComponentText(project.Name, 100),
			Description: truncateDiscordComponentText(project.Description, 100),
			Value:       cwdProjectToken(project.Relative),
		})
	}
	return options
}

func cwdProjectSelect(channelID string, page int, options []discordgo.SelectMenuOption) discordgo.MessageComponent {
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.SelectMenu{
			MenuType:    discordgo.StringSelectMenu,
			CustomID:    cwdComponentID("select", channelID, strconv.Itoa(page)),
			Placeholder: L.Get("cwd.setup.select_placeholder"),
			MaxValues:   1,
			Options:     options,
		},
	}}
}

func cwdActionButtons(channelID string, page, total int) discordgo.MessageComponent {
	components := []discordgo.MessageComponent{
		discordgo.Button{
			Label:    L.Get("cwd.setup.btn.new"),
			Style:    discordgo.PrimaryButton,
			CustomID: cwdComponentID("new", channelID),
		},
		discordgo.Button{
			Label:    L.Get("cwd.setup.btn.refresh"),
			Style:    discordgo.SecondaryButton,
			CustomID: cwdComponentID("refresh", channelID, strconv.Itoa(page)),
		},
	}
	if total > cwdPageSize {
		_, end := cwdProjectPageBounds(total, page)
		components = append(components,
			discordgo.Button{
				Label:    L.Get("cwd.setup.btn.prev"),
				Style:    discordgo.SecondaryButton,
				CustomID: cwdComponentID("page", channelID, strconv.Itoa(page-1)),
				Disabled: page <= 0,
			},
			discordgo.Button{
				Label:    L.Get("cwd.setup.btn.next"),
				Style:    discordgo.SecondaryButton,
				CustomID: cwdComponentID("page", channelID, strconv.Itoa(page+1)),
				Disabled: end >= total,
			},
		)
	}
	return discordgo.ActionsRow{Components: components}
}

func (b *Bot) handleCWDComponent(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	parts := strings.Split(data.CustomID, ":")
	if len(parts) < 3 || parts[0] != cwdCustomPrefix {
		return
	}
	action := parts[1]
	channelID := parts[2]
	userID, _ := interactionUser(i)
	if !b.userCanManageAuditTarget(ds, userID, channelID) {
		respondInteractionEphemeral(ds, i, L.Get("cwd.forbidden"))
		return
	}

	switch action {
	case "open":
		if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
		}); err != nil {
			log.Printf("[cwd-ui] open defer failed: %v", err)
			return
		}
		ctx := ctxForAudit(channelID, channelID, false, i.GuildID, userID, "")
		ctx.interactionID = i.ID
		b.sendCWDPanel(ds, i, ctx, "")
	case "mcp":
		if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
		}); err != nil {
			log.Printf("[cwd-ui] mcp defer failed: %v", err)
			return
		}
		ctx := ctxForAudit(channelID, channelID, false, i.GuildID, userID, "")
		ctx.interactionID = i.ID
		b.sendMCPManagePanel(ds, i, ctx)
	case "refresh":
		b.updateCWDComponentPanel(ds, i, channelID, "", parseCWDPage(parts, 3))
	case "page":
		b.updateCWDComponentPanel(ds, i, channelID, "", parseCWDPage(parts, 3))
	case "select":
		if len(data.Values) == 0 {
			respondInteractionEphemeral(ds, i, L.Get("error.expired"))
			return
		}
		rel := b.resolveCWDProjectToken(data.Values[0])
		if rel == "" {
			respondInteractionEphemeral(ds, i, L.Get("error.expired"))
			return
		}
		b.confirmCWDSelection(ds, i, channelID, rel, data.Values[0], parseCWDPage(parts, 3))
	case "confirm":
		if len(parts) < 4 {
			respondInteractionEphemeral(ds, i, L.Get("error.expired"))
			return
		}
		rel := b.resolveCWDProjectToken(parts[3])
		if rel == "" {
			respondInteractionEphemeral(ds, i, L.Get("error.expired"))
			return
		}
		root, err := b.manager.DefaultProjectRoot()
		if err != nil {
			respondInteractionEphemeral(ds, i, commandError(err))
			return
		}
		path := filepath.Join(root, rel)
		real, err := b.manager.InitializeChannelCWD(channelID, path)
		if err != nil {
			respondInteractionEphemeral(ds, i, commandError(err))
			return
		}
		b.completeCWDSetupInteraction(ds, i, channelID, L.Getf("cwd.setup.initialized", filepath.Base(real)))
	case "back":
		b.updateCWDComponentPanel(ds, i, channelID, "", parseCWDPage(parts, 3))
	case "new":
		if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: &discordgo.InteractionResponseData{
				CustomID: cwdComponentID("newmodal", channelID),
				Title:    L.Get("cwd.setup.modal.title"),
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "project_name",
							Label:       L.Get("cwd.setup.modal.name"),
							Placeholder: L.Get("cwd.setup.modal.name_ph"),
							Style:       discordgo.TextInputShort,
							Required:    true,
							MaxLength:   80,
						},
					}},
				},
			},
		}); err != nil {
			log.Printf("[cwd-ui] new modal failed: %v", err)
		}
	}
}

func (b *Bot) confirmCWDSelection(ds *discordgo.Session, i *discordgo.InteractionCreate, channelID, rel, token string, page int) {
	root, err := b.manager.DefaultProjectRoot()
	if err != nil {
		respondInteractionEphemeral(ds, i, commandError(err))
		return
	}
	path := filepath.Join(root, rel)
	content := secrets.RedactEnv(L.Getf("cwd.setup.confirm", rel, path))
	if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Components: []discordgo.MessageComponent{
				cwdConfirmButtons(channelID, token, page),
			},
		},
	}); err != nil {
		log.Printf("[cwd-ui] confirm selection update failed channel=%s: %v", channelID, err)
	}
}

func cwdConfirmButtons(channelID, token string, page int) discordgo.MessageComponent {
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{
			Label:    L.Get("cwd.setup.btn.confirm"),
			Style:    discordgo.PrimaryButton,
			CustomID: cwdComponentID("confirm", channelID, token, strconv.Itoa(page)),
		},
		discordgo.Button{
			Label:    L.Get("cwd.setup.btn.back"),
			Style:    discordgo.SecondaryButton,
			CustomID: cwdComponentID("back", channelID, strconv.Itoa(page)),
		},
	}}
}

func (b *Bot) updateCWDComponentPanel(ds *discordgo.Session, i *discordgo.InteractionCreate, channelID, prefix string, page int) {
	content, components := b.buildCWDPanelPage(channelID, prefix, page)
	content = secrets.RedactEnv(content)
	if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Components: components,
		},
	}); err != nil {
		log.Printf("[cwd-ui] update failed action_channel=%s: %v", channelID, err)
	}
}

func (b *Bot) handleCWDModalSubmit(ds *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	userID, _ := interactionUser(i)
	if !b.userCanManageAuditTarget(ds, userID, channelID) {
		respondInteractionEphemeral(ds, i, L.Get("cwd.forbidden"))
		return
	}
	name := ""
	for _, row := range i.ModalSubmitData().Components {
		if actionRow, ok := row.(*discordgo.ActionsRow); ok {
			for _, component := range actionRow.Components {
				if input, ok := component.(*discordgo.TextInput); ok && input.CustomID == "project_name" {
					name = input.Value
				}
			}
		}
	}
	project, err := b.manager.CreateDefaultProject(name)
	if err != nil {
		respondInteractionEphemeral(ds, i, commandError(err))
		return
	}
	real, err := b.manager.InitializeChannelCWD(channelID, project.Path)
	if err != nil {
		respondInteractionEphemeral(ds, i, commandError(err))
		return
	}
	content := secrets.RedactEnv(cwdSetupCompleteMessage(L.Getf("cwd.setup.created", filepath.Base(real))))
	if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Components: cwdSetupCompleteComponents(channelID),
			Flags:      discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		log.Printf("[cwd-ui] modal submit response failed: %v", err)
	}
}

func (b *Bot) completeCWDSetupInteraction(ds *discordgo.Session, i *discordgo.InteractionCreate, channelID, msg string) {
	msg = secrets.RedactEnv(cwdSetupCompleteMessage(msg))
	if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    msg,
			Components: cwdSetupCompleteComponents(channelID),
		},
	}); err != nil {
		log.Printf("[cwd-ui] complete setup update failed: %v", err)
	}
}

func cwdSetupCompleteMessage(msg string) string {
	return strings.TrimSpace(msg) + "\n\n" + L.Get("cwd.setup.next_steps")
}

func cwdSetupCompleteComponents(channelID string) []discordgo.MessageComponent {
	return steeringSetupComponents(channelID)
}

func (b *Bot) sendChannelSetupPrompt(ds *discordgo.Session, m *discordgo.MessageCreate) {
	if m == nil || m.Author == nil {
		return
	}
	if !b.allowSetupPrompt("channel:" + m.ChannelID) {
		return
	}
	if b.userCanManageAuditTarget(ds, m.Author.ID, m.ChannelID) {
		_, _ = ds.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Content: secrets.RedactEnv(L.Get("cwd.setup.channel_uninitialized_admin")),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    L.Get("cwd.setup.btn.open"),
						Style:    discordgo.PrimaryButton,
						CustomID: cwdComponentID("open", m.ChannelID),
					},
				}},
			},
			Reference: &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID},
		})
		return
	}
	_, _ = ds.ChannelMessageSendReply(m.ChannelID, secrets.RedactEnv(L.Get("cwd.setup.channel_uninitialized_user")), &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
}

func (b *Bot) sendThreadSetupPrompt(ds *discordgo.Session, m *discordgo.MessageCreate, parentChannelID string) {
	if m == nil || m.Author == nil {
		return
	}
	if !b.allowSetupPrompt("thread:" + m.ChannelID + ":parent:" + parentChannelID) {
		return
	}
	if b.userCanManageAuditTarget(ds, m.Author.ID, parentChannelID) {
		_, _ = ds.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Content: secrets.RedactEnv(L.Get("cwd.setup.thread_uninitialized_admin")),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    L.Get("cwd.setup.btn.open"),
						Style:    discordgo.PrimaryButton,
						CustomID: cwdComponentID("open", parentChannelID),
					},
				}},
			},
			Reference: &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID},
		})
		return
	}
	_, _ = ds.ChannelMessageSendReply(m.ChannelID, secrets.RedactEnv(L.Get("cwd.setup.thread_uninitialized_user")), &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
}

func cwdComponentID(parts ...string) string {
	return cwdCustomPrefix + ":" + strings.Join(parts, ":")
}

func parseCWDPage(parts []string, index int) int {
	if len(parts) <= index {
		return 0
	}
	page, err := strconv.Atoi(parts[index])
	if err != nil {
		return 0
	}
	return page
}

func normalizeCWDProjectPage(page, total int) int {
	if page < 0 || total <= 0 {
		return 0
	}
	last := (total - 1) / cwdPageSize
	if page > last {
		return last
	}
	return page
}

func cwdProjectPageBounds(total, page int) (int, int) {
	page = normalizeCWDProjectPage(page, total)
	start := page * cwdPageSize
	if start > total {
		start = total
	}
	end := start + cwdPageSize
	if end > total {
		end = total
	}
	return start, end
}

func cwdProjectToken(relative string) string {
	sum := sha256.Sum256([]byte(relative))
	return cwdProjectTokenID + hex.EncodeToString(sum[:])[:16]
}

func (b *Bot) resolveCWDProjectToken(token string) string {
	projects, err := b.manager.ListDefaultProjects()
	if err != nil {
		return ""
	}
	for _, project := range projects {
		if token == cwdProjectToken(project.Relative) || token == project.Relative {
			return project.Relative
		}
	}
	return ""
}

func (b *Bot) allowSetupPrompt(key string) bool {
	b.setupPromptMu.Lock()
	if b.setupPromptCooldown == nil {
		b.setupPromptCooldown = newSetupPromptCooldown(nil)
	}
	cooldown := b.setupPromptCooldown
	b.setupPromptMu.Unlock()
	return cooldown.Allow(key)
}
