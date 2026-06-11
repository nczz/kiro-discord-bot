package bot

import (
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	L "github.com/nczz/kiro-discord-bot/locale"
)

const (
	steeringCustomPrefix = "steerui"
	steeringModalLimit   = 4000
	steeringDraftLimit   = 600
)

func (b *Bot) cmdSteering(ctx cmdCtx) {
	if !b.userCanManageAuditTarget(b.discord, ctx.userID, ctx.targetID) {
		ctx.reply(L.Get("steering.forbidden"))
		return
	}
	if !b.requireInitializedCommand(ctx, "steering") {
		return
	}
	action := "status"
	fields := strings.Fields(ctx.args)
	if len(fields) > 0 {
		action = strings.ToLower(fields[0])
	}
	switch action {
	case "status":
		ctx.reply(b.steeringStatusMessage(ctx.channelID))
	case "create":
		ctx.reply(L.Get("steering.create.slash_only"))
	case "edit":
		ctx.reply(L.Get("steering.edit.slash_only"))
	default:
		ctx.reply(L.Get("steering.usage"))
	}
}

func (b *Bot) handleSteeringSlash(ds *discordgo.Session, i *discordgo.InteractionCreate, auditCtx cmdCtx) {
	const command = "steering"
	data := i.ApplicationCommandData()
	action := "status"
	if len(data.Options) > 0 {
		action = data.Options[0].Name
	}
	userID, _ := interactionUser(i)
	if !b.userCanManageAuditTarget(ds, userID, auditCtx.targetID) {
		b.respondInteractionForCommand(ds, i, auditCtx, command, L.Get("steering.forbidden"), map[string]any{"rejected_reason": "forbidden"})
		return
	}
	if !b.requireInitializedInteraction(ds, i, auditCtx, command) {
		return
	}
	switch action {
	case "edit":
		b.openSteeringEditModal(ds, i, auditCtx.channelID)
	case "create":
		b.openSteeringCreateModal(ds, i, auditCtx.channelID)
	default:
		b.respondInteractionForCommand(ds, i, auditCtx, command, b.steeringStatusMessage(auditCtx.channelID), map[string]any{"steering_action": "status"})
	}
}

func (b *Bot) handleSteeringComponent(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	parts := strings.Split(data.CustomID, ":")
	if len(parts) < 3 || parts[0] != steeringCustomPrefix {
		return
	}
	action := parts[1]
	channelID := parts[2]
	userID, _ := interactionUser(i)
	if !b.userCanManageAuditTarget(ds, userID, channelID) {
		respondInteractionEphemeral(ds, i, L.Get("steering.forbidden"))
		return
	}
	if !b.manager.ChannelInitialized(channelID) {
		respondInteractionEphemeral(ds, i, L.Getf("setup.required.command", "/steering"))
		return
	}
	switch action {
	case "create":
		b.openSteeringCreateModal(ds, i, channelID)
	case "edit":
		b.openSteeringEditModal(ds, i, channelID)
	default:
		respondInteractionEphemeral(ds, i, L.Get("error.expired"))
	}
}

func (b *Bot) handleSteeringCreateModalSubmit(ds *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	userID, _ := interactionUser(i)
	if !b.userCanManageAuditTarget(ds, userID, channelID) {
		respondInteractionEphemeral(ds, i, L.Get("steering.forbidden"))
		return
	}
	if !b.manager.ChannelInitialized(channelID) {
		respondInteractionEphemeral(ds, i, L.Getf("setup.required.command", "/steering"))
		return
	}
	draft := channel.SteeringDraft{}
	for _, row := range i.ModalSubmitData().Components {
		actionRow, ok := row.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, component := range actionRow.Components {
			input, ok := component.(*discordgo.TextInput)
			if !ok {
				continue
			}
			switch input.CustomID {
			case "background":
				draft.Background = input.Value
			case "working_style":
				draft.WorkingStyle = input.Value
			case "references":
				draft.References = input.Value
			case "constraints":
				draft.Constraints = input.Value
			case "extra":
				draft.Extra = input.Value
			}
		}
	}
	status, err := b.manager.ChannelSteeringStatus(channelID)
	if err != nil {
		respondInteractionEphemeral(ds, i, commandError(err))
		return
	}
	if status.Exists {
		respondInteractionEphemeral(ds, i, steeringExistsMessage(status))
		return
	}
	content := channel.BuildSteeringContent(status.ProjectName, draft)
	status, err = b.manager.WriteChannelSteeringFile(channelID, content)
	if err != nil {
		respondInteractionEphemeral(ds, i, commandError(err))
		return
	}
	respondInteractionEphemeral(ds, i, steeringCreatedMessage(status))
}

func (b *Bot) handleSteeringModalSubmit(ds *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	userID, _ := interactionUser(i)
	if !b.userCanManageAuditTarget(ds, userID, channelID) {
		respondInteractionEphemeral(ds, i, L.Get("steering.forbidden"))
		return
	}
	content := ""
	for _, row := range i.ModalSubmitData().Components {
		actionRow, ok := row.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, component := range actionRow.Components {
			input, ok := component.(*discordgo.TextInput)
			if ok && input.CustomID == "content" {
				content = input.Value
			}
		}
	}
	status, err := b.manager.WriteChannelSteeringFile(channelID, content)
	if err != nil {
		respondInteractionEphemeral(ds, i, commandError(err))
		return
	}
	respondInteractionEphemeral(ds, i, steeringSavedMessage(status))
}

func (b *Bot) openSteeringCreateModal(ds *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	status, err := b.manager.ChannelSteeringStatus(channelID)
	if err != nil {
		respondInteractionEphemeral(ds, i, commandError(err))
		return
	}
	if status.Exists {
		respondInteractionEphemeral(ds, i, steeringExistsMessage(status))
		return
	}
	if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: steeringComponentID("create_modal", channelID),
			Title:    truncateDiscordComponentText(L.Get("steering.create.modal.title"), 45),
			Components: []discordgo.MessageComponent{
				steeringTextInputRow("background", L.Get("steering.create.modal.background"), L.Get("steering.create.modal.background_ph"), true),
				steeringTextInputRow("working_style", L.Get("steering.create.modal.working_style"), L.Get("steering.create.modal.working_style_ph"), false),
				steeringTextInputRow("references", L.Get("steering.create.modal.references"), L.Get("steering.create.modal.references_ph"), false),
				steeringTextInputRow("constraints", L.Get("steering.create.modal.constraints"), L.Get("steering.create.modal.constraints_ph"), false),
				steeringTextInputRow("extra", L.Get("steering.create.modal.extra"), L.Get("steering.create.modal.extra_ph"), false),
			},
		},
	}); err != nil {
		log.Printf("[steering-ui] create modal failed channel=%s: %v", channelID, err)
	}
}

func steeringTextInputRow(customID, label, placeholder string, required bool) discordgo.MessageComponent {
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.TextInput{
			CustomID:    customID,
			Label:       truncateDiscordComponentText(label, 45),
			Placeholder: truncateDiscordComponentText(placeholder, 100),
			Style:       discordgo.TextInputParagraph,
			Required:    required,
			MaxLength:   steeringDraftLimit,
		},
	}}
}

func (b *Bot) openSteeringEditModal(ds *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	status, content, err := b.manager.ReadChannelSteeringFile(channelID)
	if err != nil {
		if _, _, createErr := b.manager.EnsureChannelSteeringFile(channelID); createErr != nil {
			respondInteractionEphemeral(ds, i, commandError(createErr))
			return
		}
		status, content, err = b.manager.ReadChannelSteeringFile(channelID)
		if err != nil {
			respondInteractionEphemeral(ds, i, commandError(err))
			return
		}
	}
	if len([]rune(content)) > steeringModalLimit {
		respondInteractionEphemeral(ds, i, L.Getf("steering.edit.too_large", status.FileName, status.Size))
		return
	}
	if err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: steeringComponentID("modal", channelID),
			Title:    truncateDiscordComponentText(L.Get("steering.modal.title"), 45),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:  "content",
						Label:     truncateDiscordComponentText(L.Getf("steering.modal.content", status.FileName), 45),
						Style:     discordgo.TextInputParagraph,
						Required:  true,
						MaxLength: steeringModalLimit,
						Value:     content,
					},
				}},
			},
		},
	}); err != nil {
		log.Printf("[steering-ui] modal failed channel=%s: %v", channelID, err)
	}
}

func (b *Bot) steeringStatusMessage(channelID string) string {
	status, err := b.manager.ChannelSteeringStatus(channelID)
	if err != nil {
		return commandError(err)
	}
	if status.Exists {
		return steeringExistsMessage(status)
	}
	return L.Getf("steering.status.missing", status.FileName, status.Path)
}

func steeringCreatedMessage(status channel.SteeringFileStatus) string {
	return secrets.RedactEnv(L.Getf("steering.created", status.FileName, status.Path))
}

func steeringExistsMessage(status channel.SteeringFileStatus) string {
	return secrets.RedactEnv(L.Getf("steering.exists", status.FileName, status.Path, status.Size))
}

func steeringSavedMessage(status channel.SteeringFileStatus) string {
	return secrets.RedactEnv(L.Getf("steering.saved", status.FileName, status.Path))
}

func steeringSetupComponents(channelID string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    L.Get("cwd.setup.btn.mcp"),
				Style:    discordgo.SecondaryButton,
				CustomID: cwdComponentID("mcp", channelID),
			},
			discordgo.Button{
				Label:    L.Get("cwd.setup.btn.steering"),
				Style:    discordgo.PrimaryButton,
				CustomID: steeringComponentID("create", channelID),
			},
		}},
	}
}

func steeringComponentID(parts ...string) string {
	return steeringCustomPrefix + ":" + strings.Join(parts, ":")
}

func steeringSlashOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "status", Description: L.Get("cmd.steering.sub.status")},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "create", Description: L.Get("cmd.steering.sub.create")},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "edit", Description: L.Get("cmd.steering.sub.edit")},
	}
}

func steeringArgsFromSlashOptions(options []*discordgo.ApplicationCommandInteractionDataOption) string {
	if len(options) == 0 {
		return "status"
	}
	return fmt.Sprintf("%s", options[0].Name)
}
