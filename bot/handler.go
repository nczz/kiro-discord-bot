package bot

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func usageMessage() string { return L.Get("usage_message") }


// downloadAttachments saves message attachments to DATA_DIR/ch-<channelID>/attachments/ and returns local paths.
func (b *Bot) downloadAttachments(channelID string, attachments []*discordgo.MessageAttachment) []string {
	if len(attachments) == 0 {
		return nil
	}
	attDir := filepath.Join(b.dataDir, "ch-"+channelID, "attachments")
	_ = os.MkdirAll(attDir, 0755)

	ts := time.Now().Format("20060102-150405")
	var paths []string
	for _, att := range attachments {
		resp, err := http.Get(att.URL)
		if err != nil {
			log.Printf("[attach] download %s: %v", att.Filename, err)
			continue
		}
		dst := filepath.Join(attDir, ts+"-"+att.Filename)
		f, err := os.Create(dst)
		if err != nil {
			resp.Body.Close()
			log.Printf("[attach] create %s: %v", dst, err)
			continue
		}
		_, err = io.Copy(f, resp.Body)
		resp.Body.Close()
		f.Close()
		if err != nil {
			log.Printf("[attach] write %s: %v", dst, err)
			continue
		}
		abs, _ := filepath.Abs(dst)
		paths = append(paths, abs)
	}
	return paths
}

// buildPrompt combines user text with attachment paths into an effective prompt.
func buildPrompt(text string, attachments []string, channelID, guildID string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Discord context] channel_id=%s guild_id=%s\n\n", channelID, guildID))
	if len(attachments) > 0 {
		sb.WriteString("[Attached files]\n")
		for _, p := range attachments {
			sb.WriteString(fmt.Sprintf("- %s\n", p))
		}
		sb.WriteString("\n")
	}
	if text != "" {
		sb.WriteString(text)
	} else if len(attachments) > 0 {
		sb.WriteString("Please review the attached file(s).")
	}
	return sb.String()
}

func (b *Bot) handleMessage(ds *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from other guilds
	if b.guildID != "" && m.GuildID != b.guildID {
		return
	}
	// Ignore bot's own messages
	if m.Author.ID == ds.State.User.ID {
		return
	}

	content := strings.TrimSpace(m.Content)
	hasAttachments := len(m.Attachments) > 0
	if content == "" && !hasAttachments {
		return
	}

	botMention := "<@" + ds.State.User.ID + ">"
	isMentioned := strings.Contains(content, botMention)
	isCommand := strings.HasPrefix(content, "!")

	// In pause mode, only respond to commands or mentions
	if b.manager.IsPaused(m.ChannelID) && !isCommand && !isMentioned {
		return
	}

	// Strip mention prefix if present
	if isMentioned {
		content = strings.TrimSpace(strings.ReplaceAll(content, botMention, ""))
	}

	// Commands
	switch {
	case content == "!resume":
		sess, ok := b.manager.GetSession(m.ChannelID)
		if !ok {
			ds.ChannelMessageSendReply(m.ChannelID, L.Get("error.no_active_session"), &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
			return
		}
		agent, err := b.manager.GetAgentStatus(sess.AgentName)
		if err != nil || agent.LastText == "" {
			ds.ChannelMessageSendReply(m.ChannelID, L.Get("error.no_response"), &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
			return
		}
		ds.ChannelMessageSendReply(m.ChannelID, agent.LastText, &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})

	case content == "!pause":
		b.manager.Pause(m.ChannelID)
		ds.ChannelMessageSendReply(m.ChannelID, L.Get("pause.on"), &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})

	case content == "!back":
		b.manager.Back(m.ChannelID)
		ds.ChannelMessageSendReply(m.ChannelID, L.Get("pause.off"), &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})

	case content == "!reset":
		if err := b.manager.Reset(m.ChannelID); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.reset_failed", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Get("reset.success"))
		ds.ChannelMessageSend(m.ChannelID, usageMessage())

	case content == "!status":
		ds.ChannelMessageSend(m.ChannelID, b.manager.Status(m.ChannelID))

	case content == "!cancel":
		if err := b.manager.Cancel(m.ChannelID); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.cancel_failed", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Get("cancel.success"))

	case content == "!cwd":
		ds.ChannelMessageSend(m.ChannelID, b.manager.CWD(m.ChannelID))

	case strings.HasPrefix(content, "!cwd "):
		newCwd := strings.TrimSpace(strings.TrimPrefix(content, "!cwd "))
		if err := b.manager.SetCWD(m.ChannelID, newCwd); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Getf("cwd.set", newCwd))

	case strings.HasPrefix(content, "!start "):
		cwd := strings.TrimSpace(strings.TrimPrefix(content, "!start "))
		if cwd == "" {
			ds.ChannelMessageSend(m.ChannelID, L.Get("start.usage"))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Getf("start.starting", cwd))
		if err := b.manager.StartAt(m.ChannelID, cwd); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Getf("start.success", cwd))
		ds.ChannelMessageSend(m.ChannelID, usageMessage())

	case content == "!model":
		ds.ChannelMessageSend(m.ChannelID, b.manager.Model(m.ChannelID))

	case content == "!models":
		msg, err := b.manager.ListModels()
		if err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, msg)

	case strings.HasPrefix(content, "!model "):
		model := strings.TrimSpace(strings.TrimPrefix(content, "!model "))
		if err := b.manager.SetModel(m.ChannelID, model); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Getf("model.switching", model))
		if err := b.manager.Restart(m.ChannelID); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.reset_failed", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Getf("model.switched", model))

	case strings.HasPrefix(content, "!cron"):
		b.handleCronTextCommand(ds, m.ChannelID, m.GuildID, m.Author.ID, content)

	case strings.HasPrefix(content, "!remind "):
		b.handleRemindText(ds, m.ChannelID, m.GuildID, m.Author.ID, m.Author.Username, strings.TrimPrefix(content, "!remind "))

	default:
		// Download attachments if any
		localPaths := b.downloadAttachments(m.ChannelID, m.Attachments)
		prompt := buildPrompt(content, localPaths, m.ChannelID, m.GuildID)

		job := &channel.Job{
			ChannelID:   m.ChannelID,
			MessageID:   m.ID,
			Prompt:      prompt,
			UserID:      m.Author.ID,
			Username:    m.Author.Username,
			Attachments: localPaths,
		}
		if err := b.manager.Enqueue(ds, job); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
		}
	}
}

var slashCommands = []*discordgo.ApplicationCommand{
	{Name: "start", Description: L.Get("cmd.start.desc"), Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "cwd", Description: L.Get("cmd.start.opt.cwd"), Required: true},
	}},
	{Name: "reset", Description: L.Get("cmd.reset.desc")},
	{Name: "status", Description: L.Get("cmd.status.desc")},
	{Name: "cancel", Description: L.Get("cmd.cancel.desc")},
	{Name: "cwd", Description: L.Get("cmd.cwd.desc"), Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "path", Description: L.Get("cmd.cwd.opt.path"), Required: false},
	}},
	{Name: "pause", Description: L.Get("cmd.pause.desc")},
	{Name: "back", Description: L.Get("cmd.back.desc")},
	{Name: "model", Description: L.Get("cmd.model.desc"), Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "model", Description: L.Get("cmd.model.opt.model"), Required: false},
	}},
	{Name: "models", Description: L.Get("cmd.models.desc")},
	{Name: "cron", Description: L.Get("cmd.cron.desc")},
	{Name: "cron-list", Description: L.Get("cmd.cron_list.desc")},
	{Name: "cron-run", Description: L.Get("cmd.cron_run.desc"), Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: L.Get("cmd.cron_run.opt.name"), Required: true},
	}},
	{Name: "remind", Description: L.Get("cmd.remind.desc"), Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "time", Description: L.Get("cmd.remind.opt.time"), Required: true},
		{Type: discordgo.ApplicationCommandOptionString, Name: "content", Description: L.Get("cmd.remind.opt.content"), Required: true},
		{Type: discordgo.ApplicationCommandOptionBoolean, Name: "agent", Description: L.Get("cmd.remind.opt.agent"), Required: false},
	}},
}

func (b *Bot) registerSlashCommands() {
	guildID := b.guildID
	// Clear global commands first
	if _, err := b.discord.ApplicationCommandBulkOverwrite(b.discord.State.User.ID, "", []*discordgo.ApplicationCommand{}); err != nil {
		log.Printf("[slash] clear global commands: %v", err)
	}
	created, err := b.discord.ApplicationCommandBulkOverwrite(b.discord.State.User.ID, guildID, slashCommands)
	if err != nil {
		log.Printf("[slash] bulk overwrite error: %v", err)
		return
	}
	for _, cmd := range created {
		log.Printf("[slash] registered /%s (id=%s)", cmd.Name, cmd.ID)
	}
}

func (b *Bot) handleInteraction(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	// Ignore interactions from other guilds
	if b.guildID != "" && i.GuildID != b.guildID {
		return
	}
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleSlashCommand(ds, i)
	case discordgo.InteractionModalSubmit:
		if i.ModalSubmitData().CustomID == "cron_add_modal" {
			b.handleCronModalSubmit(ds, i)
		}
	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		if strings.HasPrefix(customID, "cron_") {
			b.handleCronButton(ds, i)
		}
	}
}

func (b *Bot) handleSlashCommand(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	log.Printf("[interaction] /%s from %s", data.Name, i.ChannelID)
	channelID := i.ChannelID

	// Commands that need their own response type (not deferred)
	switch data.Name {
	case "cron":
		b.handleCronModal(ds, i)
		return
	case "cron-list":
		b.handleCronList(ds, i)
		return
	case "cron-run":
		name := data.Options[0].StringValue()
		b.handleCronRun(ds, i, name)
		return
	case "remind":
		timeStr := data.Options[0].StringValue()
		content := data.Options[1].StringValue()
		useAgent := false
		if len(data.Options) > 2 {
			useAgent = data.Options[2].BoolValue()
		}
		b.handleRemind(ds, i, timeStr, content, useAgent)
		return
	}

	// All other commands: acknowledge immediately to avoid 3-second timeout
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	reply := func(msg string) {
		_, _ = ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: msg})
	}

	go func() {
		switch data.Name {
		case "start":
			cwd := data.Options[0].StringValue()
			reply(L.Getf("start.starting", cwd))
			if err := b.manager.StartAt(channelID, cwd); err != nil {
				reply(L.Getf("error.generic", err.Error()))
			} else {
				reply(L.Getf("start.success", cwd))
				reply(usageMessage())
			}
		case "reset":
			reply(L.Get("reset.resetting"))
			if err := b.manager.Reset(channelID); err != nil {
				reply(L.Getf("error.generic", err.Error()))
			} else {
				reply(L.Get("reset.success"))
				reply(usageMessage())
			}
		case "status":
			reply(b.manager.Status(channelID))
		case "cancel":
			if err := b.manager.Cancel(channelID); err != nil {
				reply(L.Getf("error.generic", err.Error()))
			} else {
				reply(L.Get("cancel.success"))
			}
		case "cwd":
			if len(data.Options) > 0 {
				newCwd := data.Options[0].StringValue()
				if err := b.manager.SetCWD(channelID, newCwd); err != nil {
					reply(L.Getf("error.generic", err.Error()))
				} else {
					reply(L.Getf("cwd.set", newCwd))
				}
			} else {
				reply(b.manager.CWD(channelID))
			}
		case "pause":
			b.manager.Pause(channelID)
			reply(L.Get("pause.on"))
		case "back":
			b.manager.Back(channelID)
			reply(L.Get("pause.off"))
		case "model":
			if len(data.Options) > 0 {
				model := data.Options[0].StringValue()
				if err := b.manager.SetModel(channelID, model); err != nil {
					reply(L.Getf("error.generic", err.Error()))
					return
				}
				reply(L.Getf("model.switching", model))
				if err := b.manager.Restart(channelID); err != nil {
					reply(L.Getf("error.reset_failed", err.Error()))
				} else {
					reply(L.Getf("model.switched", model))
				}
			} else {
				reply(b.manager.Model(channelID))
			}
		case "models":
			msg, err := b.manager.ListModels()
			if err != nil {
				reply(L.Getf("error.generic", err.Error()))
			} else {
				reply(msg)
			}
		}
	}()
}
