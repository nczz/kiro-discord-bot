package bot

import (
	"log"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/internal/channelmeta"
)

func (b *Bot) recordChannelMetadata(ds *discordgo.Session, channelID, guildID string) {
	if b == nil || ds == nil || channelID == "" {
		return
	}
	ch, err := ds.State.Channel(channelID)
	if err != nil || ch == nil {
		return
	}
	entry := channelmeta.Entry{
		ID:              ch.ID,
		GuildID:         firstNonEmpty(ch.GuildID, guildID),
		Name:            ch.Name,
		Type:            channelMetadataType(ch),
		ParentChannelID: ch.ParentID,
	}
	if err := channelmeta.Upsert(b.dataDir, entry); err != nil {
		log.Printf("[channel-meta] upsert channel=%s: %v", ch.ID, err)
	}
	if ch.IsThread() && ch.ParentID != "" {
		b.recordChannelMetadata(ds, ch.ParentID, entry.GuildID)
	}
}

func channelMetadataType(ch *discordgo.Channel) string {
	if ch == nil {
		return ""
	}
	if ch.IsThread() {
		return "thread"
	}
	switch ch.Type {
	case discordgo.ChannelTypeGuildText:
		return "channel"
	default:
		return strconv.Itoa(int(ch.Type))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
