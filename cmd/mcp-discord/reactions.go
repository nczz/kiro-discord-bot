package main

import "fmt"

type discordReactionUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Bot        bool   `json:"bot"`
}

type discordReactionPage struct {
	ChannelID string                `json:"channel_id"`
	MessageID string                `json:"message_id"`
	Emoji     string                `json:"emoji"`
	After     string                `json:"after"`
	NextAfter string                `json:"next_after"`
	Users     []discordReactionUser `json:"users"`
}

func discordReactionPageJSON(channelID, messageID, emoji, after string, limit int) (string, error) {
	users, err := dg.MessageReactions(channelID, messageID, emoji, limit, "", after)
	if err != nil {
		return "", fmt.Errorf("list message reactions: %w", err)
	}
	records := make([]discordReactionUser, 0, len(users))
	for _, user := range users {
		if user == nil {
			continue
		}
		records = append(records, discordReactionUser{
			ID:         user.ID,
			Username:   user.Username,
			GlobalName: user.GlobalName,
			Bot:        user.Bot,
		})
	}
	nextAfter := ""
	if len(users) == limit && len(records) > 0 {
		nextAfter = records[len(records)-1].ID
	}
	return marshalDiscordJSON(discordReactionPage{
		ChannelID: channelID,
		MessageID: messageID,
		Emoji:     emoji,
		After:     after,
		NextAfter: nextAfter,
		Users:     records,
	})
}
