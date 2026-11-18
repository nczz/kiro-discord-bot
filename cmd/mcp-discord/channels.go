package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bwmarrin/discordgo"
)

type discordGuildChannelInventory struct {
	GuildID               string                 `json:"guild_id"`
	ActiveThreadsIncluded bool                   `json:"active_threads_included"`
	Channels              []discordChannelRecord `json:"channels"`
}

type discordChannelRecord struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	TypeID   int    `json:"type_id"`
	ParentID string `json:"parent_id"`
	Position int    `json:"position"`
}

func discordGuildChannelInventoryJSON(guildID string, includeActiveThreads bool) (string, error) {
	channels, err := dg.GuildChannels(guildID)
	if err != nil {
		return "", fmt.Errorf("list guild channels: %w", err)
	}
	if includeActiveThreads {
		threads, err := dg.GuildThreadsActive(guildID)
		if err != nil {
			return "", fmt.Errorf("list active guild threads: %w", err)
		}
		channels = append(channels, threads.Threads...)
	}

	records := make([]discordChannelRecord, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		records = append(records, discordChannelRecord{
			ID:       channel.ID,
			Name:     channel.Name,
			Type:     discordChannelTypeName(channel.Type),
			TypeID:   int(channel.Type),
			ParentID: channel.ParentID,
			Position: channel.Position,
		})
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Position != records[j].Position {
			return records[i].Position < records[j].Position
		}
		if records[i].ParentID != records[j].ParentID {
			return records[i].ParentID < records[j].ParentID
		}
		if records[i].TypeID != records[j].TypeID {
			return records[i].TypeID < records[j].TypeID
		}
		return records[i].ID < records[j].ID
	})

	raw, err := json.MarshalIndent(discordGuildChannelInventory{
		GuildID:               guildID,
		ActiveThreadsIncluded: includeActiveThreads,
		Channels:              records,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode guild channel inventory: %w", err)
	}
	return string(raw), nil
}

func discordChannelTypeName(channelType discordgo.ChannelType) string {
	switch channelType {
	case discordgo.ChannelTypeGuildText:
		return "text"
	case discordgo.ChannelTypeDM:
		return "dm"
	case discordgo.ChannelTypeGuildVoice:
		return "voice"
	case discordgo.ChannelTypeGroupDM:
		return "group_dm"
	case discordgo.ChannelTypeGuildCategory:
		return "category"
	case discordgo.ChannelTypeGuildNews:
		return "announcement"
	case discordgo.ChannelTypeGuildStore:
		return "store"
	case discordgo.ChannelTypeGuildNewsThread:
		return "announcement_thread"
	case discordgo.ChannelTypeGuildPublicThread:
		return "public_thread"
	case discordgo.ChannelTypeGuildPrivateThread:
		return "private_thread"
	case discordgo.ChannelTypeGuildStageVoice:
		return "stage_voice"
	case discordgo.ChannelTypeGuildDirectory:
		return "directory"
	case discordgo.ChannelTypeGuildForum:
		return "forum"
	case discordgo.ChannelTypeGuildMedia:
		return "media"
	default:
		return "unknown"
	}
}
