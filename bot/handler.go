package bot

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/jianghongjun/kiro-discord-bot/channel"
)

func (b *Bot) handleMessage(ds *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore bot's own messages
	if m.Author.ID == ds.State.User.ID {
		return
	}

	content := strings.TrimSpace(m.Content)
	if content == "" {
		return
	}

	// Commands
	switch {
	case content == "!reset":
		if err := b.manager.Reset(m.ChannelID); err != nil {
			ds.ChannelMessageSend(m.ChannelID, "❌ Reset failed: "+err.Error())
			return
		}
		ds.ChannelMessageSend(m.ChannelID, "✅ Session reset. Next message starts a new agent.")

	case content == "!status":
		ds.ChannelMessageSend(m.ChannelID, b.manager.Status(m.ChannelID))

	case content == "!cancel":
		if err := b.manager.Cancel(m.ChannelID); err != nil {
			ds.ChannelMessageSend(m.ChannelID, "❌ Cancel failed: "+err.Error())
			return
		}
		ds.ChannelMessageSend(m.ChannelID, "⚠️ Cancel requested.")

	case strings.HasPrefix(content, "!cwd "):
		cwd := strings.TrimPrefix(content, "!cwd ")
		if err := b.manager.SetCWD(m.ChannelID, cwd); err != nil {
			ds.ChannelMessageSend(m.ChannelID, "❌ "+err.Error())
			return
		}
		ds.ChannelMessageSend(m.ChannelID, "✅ CWD set to `"+cwd+"` (takes effect on next `!reset`)")

	default:
		// Regular prompt → enqueue
		job := &channel.Job{
			ChannelID: m.ChannelID,
			MessageID: m.ID,
			Prompt:    content,
		}
		if err := b.manager.Enqueue(ds, job); err != nil {
			ds.ChannelMessageSend(m.ChannelID, "❌ "+err.Error())
		}
	}
}
