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

func (b *Bot) syncGuildChannelMetadata(ds *discordgo.Session, guildID string) {
	if b == nil || ds == nil || guildID == "" {
		return
	}
	channels, err := ds.GuildChannels(guildID)
	if err != nil {
		log.Printf("[channel-meta] list guild channels guild=%s: %v", guildID, err)
		return
	}
	entries := make([]channelmeta.Entry, 0, len(channels))
	for _, ch := range channels {
		entry, ok := b.channelMetadataEntryForCurrentBot(ds, guildID, ch)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	if err := channelmeta.ReplaceGuildChannels(b.dataDir, guildID, entries); err != nil {
		log.Printf("[channel-meta] sync guild channels guild=%s: %v", guildID, err)
	}
}

func (b *Bot) channelMetadataEntryForCurrentBot(ds *discordgo.Session, guildID string, ch *discordgo.Channel) (channelmeta.Entry, bool) {
	if b == nil || ds == nil || ch == nil || channelMetadataType(ch) != "channel" {
		return channelmeta.Entry{}, false
	}
	if ds.State == nil || ds.State.User == nil || ds.State.User.ID == "" {
		return channelmeta.Entry{}, false
	}
	if err := ds.State.ChannelAdd(ch); err != nil {
		log.Printf("[channel-meta] cache channel=%s: %v", ch.ID, err)
	}
	if !b.peerCanRespondInTarget(ds, ds.State.User.ID, ch.ID) {
		return channelmeta.Entry{}, false
	}
	return channelmeta.Entry{
		ID:              ch.ID,
		GuildID:         firstNonEmpty(ch.GuildID, guildID),
		Name:            ch.Name,
		Type:            "channel",
		ParentChannelID: ch.ParentID,
	}, true
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
