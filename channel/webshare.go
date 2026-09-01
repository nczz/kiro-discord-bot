package channel

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
)

const WebShareSource = "webshare"

// WebSharePrompt describes browser-originated agent work for a Discord target.
type WebSharePrompt struct {
	GuildID         string
	ChannelID       string
	ParentChannelID string
	ThreadID        string
	MessageID       string
	Prompt          string
	UserID          string
	Username        string
	Attachments     []string
	MentionRefs     []discordmention.Ref
	DeliveryMode    DeliveryMode
	FinalReply      func(string)
}

// WebShareEnqueue queues browser-originated work from the Discord-visible
// WebShare prompt record.
func (m *Manager) WebShareEnqueue(ds *discordgo.Session, p WebSharePrompt) error {
	if m == nil {
		return fmt.Errorf("manager unavailable")
	}
	job, err := newWebShareChannelJob(p)
	if err != nil {
		return err
	}
	return m.Enqueue(ds, job)
}

// WebShareEnqueueThread queues browser-originated work on a scoped thread agent.
func (m *Manager) WebShareEnqueueThread(ds *discordgo.Session, p WebSharePrompt) error {
	if m == nil {
		return fmt.Errorf("manager unavailable")
	}
	job, parentID, err := newWebShareThreadJob(p)
	if err != nil {
		return err
	}
	return m.EnqueueThread(ds, job, parentID)
}

func newWebShareChannelJob(p WebSharePrompt) (*Job, error) {
	channelID := strings.TrimSpace(p.ChannelID)
	if channelID == "" {
		channelID = strings.TrimSpace(p.ParentChannelID)
	}
	if channelID == "" {
		return nil, fmt.Errorf("channel id is required")
	}
	delivery := p.DeliveryMode
	if delivery == "" {
		delivery = DeliveryThread
	}
	return &Job{
		ChannelID:        channelID,
		GuildID:          strings.TrimSpace(p.GuildID),
		MessageID:        strings.TrimSpace(p.MessageID),
		Prompt:           p.Prompt,
		UserID:           strings.TrimSpace(p.UserID),
		Username:         strings.TrimSpace(p.Username),
		Attachments:      append([]string(nil), p.Attachments...),
		Source:           WebShareSource,
		DeliveryMode:     delivery,
		MentionRefs:      append([]discordmention.Ref(nil), p.MentionRefs...),
		FinalReply:       p.FinalReply,
		BotToolsTargetID: strings.TrimSpace(p.ThreadID),
	}, nil
}

func newWebShareThreadJob(p WebSharePrompt) (*Job, string, error) {
	threadID := strings.TrimSpace(p.ThreadID)
	parentID := strings.TrimSpace(p.ParentChannelID)
	if threadID == "" || parentID == "" {
		return nil, "", fmt.Errorf("thread id and parent channel id are required")
	}
	return &Job{
		ChannelID:       threadID,
		ParentChannelID: parentID,
		GuildID:         strings.TrimSpace(p.GuildID),
		MessageID:       strings.TrimSpace(p.MessageID),
		Prompt:          p.Prompt,
		UserID:          strings.TrimSpace(p.UserID),
		Username:        strings.TrimSpace(p.Username),
		Attachments:     append([]string(nil), p.Attachments...),
		ThreadID:        threadID,
		Source:          WebShareSource,
		MentionRefs:     append([]discordmention.Ref(nil), p.MentionRefs...),
		FinalReply:      p.FinalReply,
	}, parentID, nil
}
