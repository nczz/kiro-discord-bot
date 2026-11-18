package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type discordMemberRecord struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	GlobalName  string `json:"global_name"`
	Nickname    string `json:"nickname"`
	DisplayName string `json:"display_name"`
	Bot         bool   `json:"bot"`
}

type discordMemberPage struct {
	GuildID   string                `json:"guild_id"`
	After     string                `json:"after"`
	NextAfter string                `json:"next_after"`
	Members   []discordMemberRecord `json:"members"`
}

type discordMemberSearchResult struct {
	GuildID string                `json:"guild_id"`
	Query   string                `json:"query"`
	Members []discordMemberRecord `json:"members"`
}

type discordUserRecord struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	GlobalName    string `json:"global_name"`
	Discriminator string `json:"discriminator"`
	Bot           bool   `json:"bot"`
	System        bool   `json:"system"`
	Avatar        string `json:"avatar"`
}

type discordGuildMemberDetails struct {
	GuildID  string   `json:"guild_id"`
	Nickname string   `json:"nickname"`
	Roles    []string `json:"roles"`
	JoinedAt string   `json:"joined_at"`
	Pending  bool     `json:"pending"`
}

type discordUserDetails struct {
	User   discordUserRecord          `json:"user"`
	Member *discordGuildMemberDetails `json:"member,omitempty"`
}

func discordListMembersJSON(guildID, after string, limit int) (string, error) {
	members, err := dg.GuildMembers(guildID, after, limit)
	if err != nil {
		return "", fmt.Errorf("list guild members: %w", err)
	}
	records := discordMemberRecords(members)
	nextAfter := ""
	if len(members) == limit && len(records) > 0 {
		nextAfter = records[len(records)-1].ID
	}
	return marshalDiscordJSON(discordMemberPage{
		GuildID:   guildID,
		After:     after,
		NextAfter: nextAfter,
		Members:   records,
	})
}

func discordMemberRecords(members []*discordgo.Member) []discordMemberRecord {
	records := make([]discordMemberRecord, 0, len(members))
	for _, member := range members {
		if member == nil || member.User == nil {
			continue
		}
		records = append(records, discordMemberRecord{
			ID:          member.User.ID,
			Username:    member.User.Username,
			GlobalName:  member.User.GlobalName,
			Nickname:    member.Nick,
			DisplayName: member.DisplayName(),
			Bot:         member.User.Bot,
		})
	}
	return records
}

func discordUserDetailsJSON(userID, guildID, channelID string) (string, error) {
	user, err := dg.User(strings.TrimSpace(userID))
	if err != nil {
		return "", fmt.Errorf("get Discord user: %w", err)
	}
	if user == nil {
		return "", fmt.Errorf("get Discord user: empty response")
	}
	result := discordUserDetails{User: discordUserRecord{
		ID:            user.ID,
		Username:      user.Username,
		GlobalName:    user.GlobalName,
		Discriminator: user.Discriminator,
		Bot:           user.Bot,
		System:        user.System,
		Avatar:        user.Avatar,
	}}

	guildID, err = discordOptionalGuildScope(guildID, channelID)
	if err != nil {
		return "", err
	}
	if guildID != "" {
		member, err := dg.GuildMember(guildID, user.ID)
		if err != nil {
			return "", fmt.Errorf("get guild member details: %w", err)
		}
		if member == nil {
			return "", fmt.Errorf("get guild member details: empty response")
		}
		joinedAt := ""
		if !member.JoinedAt.IsZero() {
			joinedAt = member.JoinedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		result.Member = &discordGuildMemberDetails{
			GuildID:  guildID,
			Nickname: member.Nick,
			Roles:    append([]string(nil), member.Roles...),
			JoinedAt: joinedAt,
			Pending:  member.Pending,
		}
	}
	return marshalDiscordJSON(result)
}

func discordOptionalGuildScope(guildID, channelID string) (string, error) {
	guildID = strings.TrimSpace(guildID)
	channelID = strings.TrimSpace(channelID)
	if guildID != "" || channelID == "" {
		return guildID, nil
	}
	channel, err := dg.Channel(channelID)
	if err != nil {
		return "", fmt.Errorf("resolve channel guild: %w", err)
	}
	if channel == nil {
		return "", fmt.Errorf("resolve channel guild: empty response")
	}
	if strings.TrimSpace(channel.GuildID) == "" {
		return "", fmt.Errorf("channel %s has no guild_id", channelID)
	}
	return channel.GuildID, nil
}

func marshalDiscordJSON(value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode Discord result: %w", err)
	}
	return string(raw), nil
}
