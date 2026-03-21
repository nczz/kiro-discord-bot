package channel

import "github.com/bwmarrin/discordgo"

// swapReaction removes oldEmoji and adds newEmoji on a message.
func swapReaction(ds *discordgo.Session, channelID, messageID, oldEmoji, newEmoji string) {
	_ = ds.MessageReactionRemove(channelID, messageID, oldEmoji, "@me")
	_ = ds.MessageReactionAdd(channelID, messageID, newEmoji)
}
