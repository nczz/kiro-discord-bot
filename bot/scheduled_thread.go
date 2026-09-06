package bot

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	"strings"
	"time"
)

type scheduledThreadRequest struct {
	ParentChannelID  string
	ExistingThreadID string
	CreatedByID      string
	ThreadName       string
	Location         *time.Location
}

func (b *Bot) prepareScheduledThread(ds *discordgo.Session, req scheduledThreadRequest) (string, bool, error) {
	loc := req.Location
	if loc == nil {
		loc = time.Now().Location()
	}
	archiveDur := 1440
	if b != nil && b.manager != nil {
		if configured := b.manager.ThreadArchive(); configured > 0 {
			archiveDur = configured
		}
	}
	separator := fmt.Sprintf("── %s ──", time.Now().In(loc).Format("01/02 15:04"))

	threadID := req.ExistingThreadID
	if threadID != "" && !scheduledThreadBelongsToParent(ds, threadID, req.ParentChannelID) {
		threadID = ""
	}
	if threadID != "" {
		if _, err := sendDiscordText(ds, threadID, separator, nil); err != nil {
			if _, uerr := ds.ChannelEditComplex(threadID, &discordgo.ChannelEdit{Archived: boolPtr(false), Locked: boolPtr(false)}); uerr != nil {
				threadID = ""
			} else if _, err2 := sendDiscordText(ds, threadID, separator, nil); err2 != nil {
				threadID = ""
			}
		}
	}

	createdThread := false
	if threadID == "" {
		thread, err := ds.ThreadStart(req.ParentChannelID, truncateDiscordThreadName(req.ThreadName), discordgo.ChannelTypeGuildPublicThread, archiveDur)
		if err != nil {
			return "", false, fmt.Errorf("create thread: %w", err)
		}
		threadID = thread.ID
		createdThread = true
		_, _ = sendDiscordText(ds, threadID, separator, nil)
	}

	if createdThread && req.CreatedByID != "" {
		_ = ds.ThreadMemberAdd(threadID, req.CreatedByID)
	}

	return threadID, createdThread, nil
}

func scheduledThreadBelongsToParent(ds *discordgo.Session, threadID, parentChannelID string) bool {
	if ds == nil || threadID == "" || parentChannelID == "" {
		return false
	}
	ch, err := ds.State.Channel(threadID)
	if err != nil || ch == nil {
		ch, err = ds.Channel(threadID)
		if err != nil {
			return false
		}
	}
	return ch != nil && ch.IsThread() && ch.ParentID == parentChannelID
}

func scheduledThreadName(prefix, name string) string {
	return truncateDiscordThreadName(channel.EscapeDiscordMarkdown(prefix + name))
}

func truncateDiscordThreadName(name string) string {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) <= 100 {
		return name
	}
	return string(runes[:100])
}
