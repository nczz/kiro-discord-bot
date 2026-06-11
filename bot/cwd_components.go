package bot

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	L "github.com/nczz/kiro-discord-bot/locale"
)

const (
	cwdCustomPrefix = "cwdui"
	cwdMaxOptions   = 25
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
		return sb.String(), []discordgo.MessageComponent{cwdActionButtons(channelID)}
	}

	projects, err := b.manager.ListDefaultProjects()
	if err != nil {
		sb.WriteString("\n")
		sb.WriteString(commandError(err))
		return sb.String(), []discordgo.MessageComponent{cwdActionButtons(channelID)}
	}
	if len(projects) == 0 {
		sb.WriteString("\n")
		sb.WriteString(L.Get("cwd.setup.no_projects"))
		return sb.String(), []discordgo.MessageComponent{cwdActionButtons(channelID)}
	}
	sb.WriteString("\n")
	sb.WriteString(L.Getf("cwd.setup.project_count", len(projects)))
	options := cwdProjectOptions(projects)
	if len(options) == 0 {
		sb.WriteString("\n")
		sb.WriteString(L.Get("cwd.setup.no_selectable_projects"))
		return sb.String(), []discordgo.MessageComponent{cwdActionButtons(channelID)}
	}
	return sb.String(), []discordgo.MessageComponent{
		cwdProjectSelect(channelID, options),
		cwdActionButtons(channelID),
	}
}

func cwdProjectOptions(projects []channel.ProjectOption) []discordgo.SelectMenuOption {
	options := []discordgo.SelectMenuOption{}
	for _, project := range projects {
		if len(options) >= cwdMaxOptions {
			break
		}
		value := project.Relative
		if strings.TrimSpace(value) == "" || len(value) > 100 {
			continue
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:       truncateDiscordComponentText(project.Name, 100),
			Description: truncateDiscordComponentText(project.Description, 100),
			Value:       value,
		})
	}
	return options
}

func cwdProjectSelect(channelID string, options []discordgo.SelectMenuOption) discordgo.MessageComponent {
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.SelectMenu{
			MenuType:    discordgo.StringSelectMenu,
			CustomID:    cwdComponentID("select", channelID),
			Placeholder: L.Get("cwd.setup.select_placeholder"),
			MaxValues:   1,
			Options:     options,
		},
	}}
}

func cwdActionButtons(channelID string) discordgo.MessageComponent {
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{
			Label:    L.Get("cwd.setup.btn.new"),
			Style:    discordgo.PrimaryButton,
			CustomID: cwdComponentID("new", channelID),
		},
		discordgo.Button{
			Label:    L.Get("cwd.setup.btn.refresh"),
			Style:    discordgo.SecondaryButton,
			CustomID: cwdComponentID("refresh", channelID),
		},
	}}
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
		b.updateCWDComponentPanel(ds, i, channelID, "")
	case "select":
		if len(data.Values) == 0 {
			respondInteractionEphemeral(ds, i, L.Get("error.expired"))
			return
		}
		rel := data.Values[0]
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

func (b *Bot) updateCWDComponentPanel(ds *discordgo.Session, i *discordgo.InteractionCreate, channelID, prefix string) {
	content, components := b.buildCWDPanel(channelID, prefix)
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

func (b *Bot) allowSetupPrompt(key string) bool {
	b.setupPromptMu.Lock()
	if b.setupPromptCooldown == nil {
		b.setupPromptCooldown = newSetupPromptCooldown(nil)
	}
	cooldown := b.setupPromptCooldown
	b.setupPromptMu.Unlock()
	return cooldown.Allow(key)
}
