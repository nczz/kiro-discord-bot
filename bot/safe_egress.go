package bot

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/internal/botegress"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	L "github.com/nczz/kiro-discord-bot/locale"
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

func (t *safeEgressTask) DrainChannel(channelID string) int {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	actions, err := botegress.ReadPending(t.bot.dataDir)
	if err != nil {
		log.Printf("[safe-egress] drain channel=%s read pending: %v", channelID, err)
		return 0
	}
	delivered := 0
	for _, action := range actions {
		if strings.TrimSpace(action.ChannelID) != channelID {
			continue
		}
		if t.processAndRemove(action) && isDiscordEgressAction(action.Action) {
			delivered++
		}
	}
	return delivered
}

func (t *safeEgressTask) processAndRemove(action botegress.Action) bool {
	defer t.removeTransientFile(action)
	delivered := true
	if err := t.process(action); err != nil {
		delivered = false
		log.Printf("[safe-egress] action %s failed: %v", action.ID, err)
		if isDiscordEgressAction(action.Action) {
			t.sendSafeFailure(action, err)
		}
	}
	if err := botegress.RemovePending(t.bot.dataDir, action.ID); err != nil {
		log.Printf("[safe-egress] remove action %s: %v", action.ID, err)
	}
	return delivered
}

func (t *safeEgressTask) removeTransientFile(action botegress.Action) {
	if !action.RemoveFileAfterSend || strings.TrimSpace(action.FilePath) == "" || t == nil || t.bot == nil {
		return
	}
	root := filepath.Join(t.bot.dataDir, "egress", "incoming")
	absRoot, err := filepath.Abs(root)
	if err != nil {
		log.Printf("[safe-egress] resolve transient root: %v", err)
		return
	}
	absPath, err := filepath.Abs(action.FilePath)
	if err != nil {
		log.Printf("[safe-egress] resolve transient file %s: %v", action.FilePath, err)
		return
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		log.Printf("[safe-egress] refusing transient cleanup outside incoming dir: %s", action.FilePath)
		return
	}
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		log.Printf("[safe-egress] remove transient file %s: %v", action.FilePath, err)
	}
	if dir := filepath.Dir(absPath); dir != absRoot {
		_ = os.Remove(dir)
	}
}

func (t *safeEgressTask) process(action botegress.Action) error {
	switch action.Action {
	case botegress.ActionSendMessage:
		content := strings.TrimSpace(t.redactor.Redact(action.Content))
		if content == "" {
			content = "[REDACTED]"
		}
		_, err := channelSendSanitized(t.bot.discord, action.ChannelID, content, action.MentionRefs)
		return err
	case botegress.ActionSendFile:
		return t.sendFile(action)
	case botegress.ActionMemoryAdd, botegress.ActionMemoryRemove, botegress.ActionMemoryClear:
		return t.processMemory(action)
	default:
		return fmt.Errorf("unknown bot action %q", action.Action)
	}
}

func isDiscordEgressAction(action string) bool {
	return action == botegress.ActionSendMessage || action == botegress.ActionSendFile
}

func (t *safeEgressTask) processMemory(action botegress.Action) error {
	if t == nil || t.bot == nil || t.bot.manager == nil {
		err := fmt.Errorf("memory manager is unavailable")
		t.recordMemoryAction(action, "error", err)
		return err
	}
	switch action.Action {
	case botegress.ActionMemoryAdd:
		entry := strings.TrimSpace(action.MemoryEntry)
		if entry == "" {
			err := fmt.Errorf("memory_entry is required")
			t.recordMemoryAction(action, "error", err)
			return err
		}
		if redacted := t.redactor.Redact(entry); redacted != entry {
			err := fmt.Errorf("memory_entry appears to contain a secret and was not stored")
			t.recordMemoryAction(action, "error", err)
			return err
		}
		if err := t.bot.manager.MemoryAdd(action.ChannelID, entry); err != nil {
			t.recordMemoryAction(action, "error", err)
			return err
		}
	case botegress.ActionMemoryRemove:
		if err := t.bot.manager.MemoryRemove(action.ChannelID, action.MemoryIndex-1); err != nil {
			t.recordMemoryAction(action, "error", err)
			return err
		}
	case botegress.ActionMemoryClear:
		if err := t.bot.manager.MemoryClear(action.ChannelID); err != nil {
			t.recordMemoryAction(action, "error", err)
			return err
		}
	default:
		err := fmt.Errorf("unknown memory action %q", action.Action)
		t.recordMemoryAction(action, "error", err)
		return err
	}
	t.recordMemoryAction(action, "applied", nil)
	return nil
}

func (t *safeEgressTask) recordMemoryAction(action botegress.Action, status string, actionErr error) {
	if t == nil || t.bot == nil {
		return
	}
	metadata := map[string]any{
		"action":       action.Action,
		"action_id":    action.ID,
		"entry_len":    len(action.MemoryEntry),
		"memory_index": action.MemoryIndex,
		"requested_by": action.RequestedBy,
		"reason":       action.Reason,
	}
	errText := ""
	if actionErr != nil {
		errText = actionErr.Error()
	}
	content := ""
	if action.Action == botegress.ActionMemoryAdd {
		content = strings.TrimSpace(t.redactor.Redact(action.MemoryEntry))
	}
	t.bot.recordBotAuditEvent(audit.BotEvent{
		Type:      "bot_memory_updated",
		GuildID:   t.bot.guildID,
		ChannelID: action.ChannelID,
		TargetID:  action.ChannelID,
		Command:   action.Action,
		Source:    "bot_tools_mcp",
		Status:    status,
		Content:   content,
		Error:     errText,
		Metadata:  metadata,
	})
}

func (t *safeEgressTask) sendFile(action botegress.Action) error {
	tempRoot := filepath.Join(t.bot.dataDir, "egress", "sanitized")
	prepared, err := botegress.PrepareSanitizedFile(action.FilePath, t.redactor, tempRoot)
	if err != nil {
		return err
	}
	defer os.Remove(prepared.Path)
	rawContent := strings.TrimSpace(t.redactor.Redact(action.Content))
	if prepared.SensitivePath {
		if rawContent != "" {
			rawContent += "\n"
		}
		rawContent += L.Get("egress.sensitive_path_notice")
	}
	content, _ := discordmention.Render(rawContent, action.MentionRefs)
	file, err := openDiscordFile(prepared.Path, prepared.DisplayName)
	if err != nil {
		return err
	}
	if closer, ok := file.Reader.(*os.File); ok {
		defer closer.Close()
	}
	if utf8.RuneCountInString(content) > discordReplyLimit {
		if _, err := channelSendSanitized(t.bot.discord, action.ChannelID, rawContent, action.MentionRefs); err != nil {
			return err
		}
		content = ""
	}
	msg := &discordgo.MessageSend{
		Content:         content,
		Files:           []*discordgo.File{file},
		AllowedMentions: discordmention.AllowedMentionsForRendered(content, action.MentionRefs),
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
	reason := egressReasonMessage(err.Error())
	msg := L.Getf("egress.blocked", botegress.RedactSensitivePaths(t.redactor.Redact(reason)))
	_, _ = channelSendSanitized(t.bot.discord, channelID, msg, nil)
}

func egressReasonMessage(raw string) string {
	for _, r := range egressReasonKeys {
		if strings.Contains(raw, r.match) {
			return L.Get(r.key)
		}
	}
	return raw
}

var egressReasonKeys = []struct {
	match string
	key   string
}{
	{"file type is not safely redactable as text", "egress.reason.not_text"},
	{"exceeds sanitizable size limit", "egress.reason.too_large"},
	{"image exceeds upload size limit", "egress.reason.image_too_large"},
	{"image dimensions exceed upload limit", "egress.reason.image_too_large"},
	{"invalid image file", "egress.reason.invalid_image"},
	{"directories cannot be sent as files", "egress.reason.is_directory"},
	{"file_path is required", "egress.reason.path_required"},
	{"extract readable text", "egress.reason.extract_failed"},
	{"unsupported extractable format", "egress.reason.unsupported_format"},
}

func channelSendSanitized(ds *discordgo.Session, channelID, content string, mentionRefs []discordmention.Ref) (int, error) {
	if ds == nil || channelID == "" || content == "" {
		return 0, nil
	}
	rendered, _ := discordmention.Render(content, mentionRefs)
	parts := splitDiscordMessage(rendered, discordReplyLimit)
	if len(parts) == 0 {
		return 0, nil
	}
	sent := 0
	var firstErr error
	for i, part := range parts {
		if len(parts) > 1 {
			part = formatReplyPart(i, len(parts), part)
		}
		_, err := ds.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
			Content:         part,
			AllowedMentions: discordmention.AllowedMentionsForRendered(part, mentionRefs),
			Flags:           discordgo.MessageFlagsSuppressEmbeds,
		})
		if err != nil {
			log.Printf("[safe-egress] send channel=%s part=%d/%d failed: %v", channelID, i+1, len(parts), err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sent++
	}
	return sent, firstErr
}
