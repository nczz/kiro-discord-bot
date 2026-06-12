package bot

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/internal/botegress"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
)

type safeEgressTask struct {
	bot      *Bot
	redactor *secrets.Redactor
	mu       sync.Mutex
}

func newSafeEgressTask(bot *Bot) *safeEgressTask {
	return &safeEgressTask{bot: bot, redactor: secrets.FromEnv()}
}

func (t *safeEgressTask) Name() string { return "safe-egress" }

func (t *safeEgressTask) ShouldRun(_ time.Time) bool { return true }

func (t *safeEgressTask) Run() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	actions, err := botegress.ReadPending(t.bot.dataDir)
	if err != nil {
		return err
	}
	for _, action := range actions {
		t.processAndRemove(action)
	}
	return nil
}

func (t *safeEgressTask) DrainChannel(channelID string) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	actions, err := botegress.ReadPending(t.bot.dataDir)
	if err != nil {
		log.Printf("[safe-egress] drain channel=%s read pending: %v", channelID, err)
		return
	}
	for _, action := range actions {
		if strings.TrimSpace(action.ChannelID) != channelID {
			continue
		}
		t.processAndRemove(action)
	}
}

func (t *safeEgressTask) processAndRemove(action botegress.Action) {
	if err := t.process(action); err != nil {
		log.Printf("[safe-egress] action %s failed: %v", action.ID, err)
		t.sendSafeFailure(action, err)
	}
	if err := botegress.RemovePending(t.bot.dataDir, action.ID); err != nil {
		log.Printf("[safe-egress] remove action %s: %v", action.ID, err)
	}
}

func (t *safeEgressTask) process(action botegress.Action) error {
	switch action.Action {
	case botegress.ActionSendMessage:
		content := strings.TrimSpace(t.redactor.Redact(action.Content))
		if content == "" {
			content = "[REDACTED]"
		}
		_, err := channelSendSanitized(t.bot.discord, action.ChannelID, content)
		return err
	case botegress.ActionSendFile:
		return t.sendFile(action)
	default:
		return fmt.Errorf("unknown egress action %q", action.Action)
	}
}

func (t *safeEgressTask) sendFile(action botegress.Action) error {
	tempRoot := filepath.Join(t.bot.dataDir, "egress", "sanitized")
	prepared, err := botegress.PrepareSanitizedFile(action.FilePath, t.redactor, tempRoot)
	if err != nil {
		return err
	}
	defer os.Remove(prepared.Path)
	content := strings.TrimSpace(t.redactor.Redact(action.Content))
	if prepared.SensitivePath {
		if content != "" {
			content += "\n"
		}
		content += "Sensitive path detected; uploaded a sanitized copy."
	}
	file, err := openDiscordFile(prepared.Path, prepared.DisplayName)
	if err != nil {
		return err
	}
	if closer, ok := file.Reader.(*os.File); ok {
		defer closer.Close()
	}
	msg := &discordgo.MessageSend{
		Content:         content,
		Files:           []*discordgo.File{file},
		AllowedMentions: &discordgo.MessageAllowedMentions{},
		Flags:           discordgo.MessageFlagsSuppressEmbeds,
	}
	_, err = t.bot.discord.ChannelMessageSendComplex(action.ChannelID, msg)
	return err
}

func openDiscordFile(path, displayName string) (*discordgo.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open sanitized file: %w", err)
	}
	return &discordgo.File{Name: displayName, Reader: f}, nil
}

func (t *safeEgressTask) sendSafeFailure(action botegress.Action, err error) {
	channelID := strings.TrimSpace(action.ChannelID)
	if channelID == "" {
		return
	}
	msg := "Safe egress blocked: " + botegress.RedactSensitivePaths(t.redactor.Redact(err.Error()))
	_, _ = channelSendSanitized(t.bot.discord, channelID, msg)
}

func channelSendSanitized(ds *discordgo.Session, channelID, content string) (*discordgo.Message, error) {
	if ds == nil || channelID == "" || content == "" {
		return nil, nil
	}
	return ds.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
		Flags:           discordgo.MessageFlagsSuppressEmbeds,
	})
}
