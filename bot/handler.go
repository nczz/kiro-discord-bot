package bot

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
)

const usageMessage = `🤖 **Agent 已就緒！** 以下是可用指令：

` + "```" + `
!status   — 查詢 agent 狀態
!cancel   — 取消目前執行中的任務
!cwd          — 查詢目前工作目錄
!cwd <目錄>   — 設定新工作目錄（下次 !reset 生效）
!reset    — 重啟 agent session
!start <目錄> — 綁定新的專案目錄
!pause    — 切換為 @mention 模式（僅回應 @提及）
!back     — 恢復完整監聽模式
!resume   — 重新顯示上次被截斷的回應
` + "```" + `
直接在頻道輸入訊息即可與 agent 對話。`

// downloadAttachments saves message attachments to DATA_DIR/<agentName>/ and returns local paths.
func (b *Bot) downloadAttachments(channelID string, attachments []*discordgo.MessageAttachment) []string {
	if len(attachments) == 0 {
		return nil
	}
	sess, ok := b.manager.GetSession(channelID)
	agentDir := filepath.Join(b.dataDir, "ch-"+channelID)
	if ok && sess.AgentName != "" {
		agentDir = filepath.Join(b.dataDir, sess.AgentName)
	}
	_ = os.MkdirAll(agentDir, 0755)

	var paths []string
	for _, att := range attachments {
		resp, err := http.Get(att.URL)
		if err != nil {
			log.Printf("[attach] download %s: %v", att.Filename, err)
			continue
		}
		dst := filepath.Join(agentDir, att.Filename)
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
			ds.ChannelMessageSendReply(m.ChannelID, "❌ No active session", &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
			return
		}
		agent, err := b.manager.GetAgentStatus(sess.AgentName)
		if err != nil || agent.LastText == "" {
			ds.ChannelMessageSendReply(m.ChannelID, "❌ No response to resume", &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
			return
		}
		ds.ChannelMessageSendReply(m.ChannelID, agent.LastText, &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})

	case content == "!pause":
		b.manager.Pause(m.ChannelID)
		ds.ChannelMessageSendReply(m.ChannelID, "⏸️ 暫停監聽，改為 @mention 模式", &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})

	case content == "!back":
		b.manager.Back(m.ChannelID)
		ds.ChannelMessageSendReply(m.ChannelID, "▶️ 恢復完整監聽", &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})

	case content == "!reset":
		if err := b.manager.Reset(m.ChannelID); err != nil {
			ds.ChannelMessageSend(m.ChannelID, "❌ Reset failed: "+err.Error())
			return
		}
		ds.ChannelMessageSend(m.ChannelID, "✅ Session reset. Next message starts a new agent.")
		ds.ChannelMessageSend(m.ChannelID, usageMessage)

	case content == "!status":
		ds.ChannelMessageSend(m.ChannelID, b.manager.Status(m.ChannelID))

	case content == "!cancel":
		if err := b.manager.Cancel(m.ChannelID); err != nil {
			ds.ChannelMessageSend(m.ChannelID, "❌ Cancel failed: "+err.Error())
			return
		}
		ds.ChannelMessageSend(m.ChannelID, "⚠️ Cancel requested.")

	case content == "!cwd":
		ds.ChannelMessageSend(m.ChannelID, b.manager.CWD(m.ChannelID))

	case strings.HasPrefix(content, "!cwd "):
		newCwd := strings.TrimSpace(strings.TrimPrefix(content, "!cwd "))
		if err := b.manager.SetCWD(m.ChannelID, newCwd); err != nil {
			ds.ChannelMessageSend(m.ChannelID, "❌ "+err.Error())
			return
		}
		ds.ChannelMessageSend(m.ChannelID, "✅ CWD 已設定為 `"+newCwd+"`，下次 `!reset` 時生效。")

	case strings.HasPrefix(content, "!start "):
		cwd := strings.TrimSpace(strings.TrimPrefix(content, "!start "))
		if cwd == "" {
			ds.ChannelMessageSend(m.ChannelID, "Usage: `!start /path/to/project`")
			return
		}
		ds.ChannelMessageSend(m.ChannelID, "⏳ Starting agent at `"+cwd+"`...")
		if err := b.manager.StartAt(m.ChannelID, cwd); err != nil {
			ds.ChannelMessageSend(m.ChannelID, "❌ "+err.Error())
			return
		}
		ds.ChannelMessageSend(m.ChannelID, "✅ Agent started at `"+cwd+"`")
		ds.ChannelMessageSend(m.ChannelID, usageMessage)

	default:
		// Download attachments if any
		localPaths := b.downloadAttachments(m.ChannelID, m.Attachments)
		prompt := buildPrompt(content, localPaths, m.ChannelID, m.GuildID)

		job := &channel.Job{
			ChannelID: m.ChannelID,
			MessageID: m.ID,
			Prompt:    prompt,
		}
		if err := b.manager.Enqueue(ds, job); err != nil {
			ds.ChannelMessageSend(m.ChannelID, "❌ "+err.Error())
		}
	}
}

var slashCommands = []*discordgo.ApplicationCommand{
	{Name: "start", Description: "綁定專案目錄並啟動 agent", Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "cwd", Description: "專案目錄路徑", Required: true},
	}},
	{Name: "reset", Description: "重啟 agent"},
	{Name: "status", Description: "查詢目前 agent 狀態"},
	{Name: "cancel", Description: "取消目前執行中的任務"},
	{Name: "cwd", Description: "查詢或設定工作目錄", Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "path", Description: "新的工作目錄（留空則查詢）", Required: false},
	}},
	{Name: "pause", Description: "暫停監聽，改為 @mention 模式"},
	{Name: "back", Description: "恢復完整監聽所有訊息"},
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
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	data := i.ApplicationCommandData()
	log.Printf("[interaction] /%s from %s", data.Name, i.ChannelID)
	channelID := i.ChannelID

	respond := func(msg string) {
		_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: msg},
		})
	}
	followup := func(msg string) {
		_, _ = ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: msg})
	}

	switch data.Name {
	case "start":
		cwd := data.Options[0].StringValue()
		respond("⏳ Starting agent at `" + cwd + "`...")
		go func() {
			if err := b.manager.StartAt(channelID, cwd); err != nil {
				followup("❌ " + err.Error())
			} else {
				followup("✅ Agent started at `" + cwd + "`")
				followup(usageMessage)
			}
		}()
	case "reset":
		respond("⏳ Resetting...")
		go func() {
			if err := b.manager.Reset(channelID); err != nil {
				followup("❌ " + err.Error())
			} else {
				followup("✅ Session reset.")
				followup(usageMessage)
			}
		}()
	case "status":
		respond(b.manager.Status(channelID))
	case "cancel":
		if err := b.manager.Cancel(channelID); err != nil {
			respond("❌ " + err.Error())
		} else {
			respond("⚠️ Cancel requested.")
		}
	case "cwd":
		if len(data.Options) > 0 {
			newCwd := data.Options[0].StringValue()
			if err := b.manager.SetCWD(channelID, newCwd); err != nil {
				respond("❌ " + err.Error())
			} else {
				respond("✅ CWD 已設定為 `" + newCwd + "`，下次 `/reset` 時生效。")
			}
		} else {
			respond(b.manager.CWD(channelID))
		}
	case "pause":
		b.manager.Pause(channelID)
		respond("⏸️ 暫停監聽，改為 @mention 模式")
	case "back":
		b.manager.Back(channelID)
		respond("▶️ 恢復完整監聽")
	}
}
