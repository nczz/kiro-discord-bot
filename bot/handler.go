package bot

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	L "github.com/nczz/kiro-discord-bot/locale"
	"github.com/nczz/kiro-discord-bot/stt"
)

func usageMessage() string { return L.Get("usage_message") }

// threadParentCache caches thread→parent channel mappings to avoid repeated API calls.
var (
	threadParentMu    sync.RWMutex
	threadParentCache = make(map[string]string) // threadID → parentChannelID, "" = not a thread
)

// seenMessages is a TTL-based set to deduplicate Discord MESSAGE_CREATE events
// that may be replayed during gateway reconnections.
type seenMessages struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func newSeenMessages() *seenMessages {
	s := &seenMessages{entries: make(map[string]time.Time)}
	go func() {
		for range time.Tick(60 * time.Second) {
			s.mu.Lock()
			cutoff := time.Now().Add(-5 * time.Minute)
			for id, t := range s.entries {
				if t.Before(cutoff) {
					delete(s.entries, id)
				}
			}
			s.mu.Unlock()
		}
	}()
	return s
}

// Mark returns true if the message ID was already seen (duplicate).
func (s *seenMessages) Mark(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.entries[id]; dup {
		return true
	}
	s.entries[id] = time.Now()
	return false
}

// resolveThreadParent returns the parent channel ID if channelID is a thread, or "" if not.
func resolveThreadParent(ds *discordgo.Session, channelID string) string {
	threadParentMu.RLock()
	parent, cached := threadParentCache[channelID]
	threadParentMu.RUnlock()
	if cached {
		return parent
	}

	ch, err := ds.Channel(channelID)
	if err != nil {
		return ""
	}

	parentID := ""
	if ch.IsThread() {
		parentID = ch.ParentID
	}

	threadParentMu.Lock()
	threadParentCache[channelID] = parentID
	threadParentMu.Unlock()
	return parentID
}

// registerThreadParent caches a known thread→parent mapping (called when bot creates a thread).
func registerThreadParent(threadID, parentChannelID string) {
	threadParentMu.Lock()
	threadParentCache[threadID] = parentChannelID
	threadParentMu.Unlock()
}

func (b *Bot) statusWithSTT(channelID string) string {
	s := b.manager.Status(channelID)
	if b.sttClient != nil {
		s += "\nSTT: `" + b.sttClient.Model() + "`"
	}
	return s
}

// warnIfAttachmentsLarge checks total attachment size and sends a warning if it may exceed the scanner buffer.
func (b *Bot) warnIfAttachmentsLarge(ds *discordgo.Session, channelID string, paths []string) {
	if len(paths) == 0 {
		return
	}
	limit := b.manager.MaxScannerBuffer()
	if limit <= 0 {
		return
	}
	var total int64
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil {
			total += fi.Size()
		}
	}
	// base64 expansion ≈ ×1.37, use ×1.5 as safety margin
	if int64(float64(total)*1.5) > int64(limit) {
		mb := total / (1024 * 1024)
		ds.ChannelMessageSend(channelID, L.Getf("warn.attachments_large", mb, limit/(1024*1024)))
	}
}


// transcribeAudioFiles detects audio files in paths, transcribes them via STT,
// and returns (transcribed text, remaining non-audio paths).
func (b *Bot) transcribeAudioFiles(paths []string, attachments []*discordgo.MessageAttachment) (string, []string) {
	if b.sttClient == nil || len(paths) == 0 {
		return "", paths
	}

	// Build duration lookup from Discord attachment metadata
	durMap := make(map[string]float64)
	for _, att := range attachments {
		if att.DurationSecs > 0 {
			durMap[att.Filename] = att.DurationSecs
		}
	}

	var transcripts []string
	var remaining []string
	for _, p := range paths {
		if !stt.IsAudioFile(p) {
			remaining = append(remaining, p)
			continue
		}
		// Check duration limit
		base := filepath.Base(p)
		// Strip timestamp prefix (20060102-150405-) to match original filename
		if idx := strings.Index(base, "-"); idx > 0 {
			if idx2 := strings.Index(base[idx+1:], "-"); idx2 > 0 {
				base = base[idx+1+idx2+1:]
			}
		}
		if dur, ok := durMap[base]; ok && b.sttMaxDuration > 0 && dur > float64(b.sttMaxDuration) {
			log.Printf("[stt] skip %s: duration %.0fs > max %ds", base, dur, b.sttMaxDuration)
			remaining = append(remaining, p)
			continue
		}

		text, err := b.sttClient.Transcribe(p)
		if err != nil {
			log.Printf("[stt] transcribe %s: %v", filepath.Base(p), err)
			remaining = append(remaining, p) // fallback: keep file path
			continue
		}
		if text != "" {
			transcripts = append(transcripts, text)
			log.Printf("[stt] transcribed %s (%d chars)", filepath.Base(p), len(text))
		}
	}

	return strings.Join(transcripts, "\n"), remaining
}

// downloadAttachments saves message attachments to DATA_DIR/ch-<channelID>/attachments/ and returns local paths.
func (b *Bot) downloadAttachments(channelID string, attachments []*discordgo.MessageAttachment) []string {
	if len(attachments) == 0 {
		return nil
	}
	attDir := filepath.Join(b.dataDir, "ch-"+channelID, "attachments")
	_ = os.MkdirAll(attDir, 0755)

	ts := time.Now().Format("20060102-150405")
	var paths []string
	for _, att := range attachments {
		resp, err := b.downloadClient.Get(att.URL)
		if err != nil {
			log.Printf("[attach] download %s: %v (url=%s)", att.Filename, err, att.URL)
			continue
		}
		dst := filepath.Join(attDir, ts+"-"+att.Filename)
		f, err := os.Create(dst)
		if err != nil {
			resp.Body.Close()
			log.Printf("[attach] create %s: %v", dst, err)
			continue
		}
		_, err = io.Copy(f, resp.Body)
		resp.Body.Close()
		f.Close()
		if err != nil {
			log.Printf("[attach] write %s: %v", dst, err)
			continue
		}
		abs, _ := filepath.Abs(dst)
		paths = append(paths, abs)
	}
	return paths
}

// buildPrompt combines user text with attachment paths into an effective prompt.
func buildPrompt(text string, attachments []string, channelID, guildID, username string) string {
	return buildPromptThread(text, attachments, channelID, "", guildID, username)
}

func buildPromptThread(text string, attachments []string, channelID, threadID, guildID, username string) string {
	var sb strings.Builder
	sb.WriteString("[Discord bot environment] Your responses are automatically forwarded to a Discord thread. Each message is split at 2000 chars. Tool execution details are also shown.\n")
	if threadID != "" {
		sb.WriteString(fmt.Sprintf("[Discord context] channel_id=%s thread_id=%s guild_id=%s user=%s\n\n", channelID, threadID, guildID, username))
	} else {
		sb.WriteString(fmt.Sprintf("[Discord context] channel_id=%s guild_id=%s user=%s\n\n", channelID, guildID, username))
	}
	if len(attachments) > 0 {
		sb.WriteString("[Attached files]\n")
		for _, p := range attachments {
			sb.WriteString(fmt.Sprintf("- %s\n", p))
		}
		sb.WriteString("\n")
	}
	if text != "" {
		sb.WriteString(text)
	} else if len(attachments) > 0 {
		sb.WriteString("Please review the attached file(s).")
	}
	return sb.String()
}

func (b *Bot) handleMessage(ds *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from other guilds
	if !b.isMyGuild(m.GuildID) {
		return
	}
	// Ignore bot's own messages
	if m.Author.ID == ds.State.User.ID {
		return
	}

	// Deduplicate: skip if this message ID was already processed (gateway reconnect replay)
	if b.seen.Mark(m.ID) {
		return
	}

	content := strings.TrimSpace(m.Content)
	hasAttachments := len(m.Attachments) > 0
	if content == "" && !hasAttachments {
		return
	}

	botMention := "<@" + ds.State.User.ID + ">"
	isMentioned := strings.Contains(content, botMention)
	isCommand := strings.HasPrefix(content, "!")

	// In pause mode, only respond to commands or mentions
	if b.manager.IsPaused(m.ChannelID) && !isCommand && !isMentioned {
		return
	}

	// Strip mention prefix if present
	if isMentioned {
		content = strings.TrimSpace(strings.ReplaceAll(content, botMention, ""))
	}

	// Check if message is from a thread — route to thread agent
	parentChannelID := resolveThreadParent(ds, m.ChannelID)
	if parentChannelID != "" {
		b.handleThreadMessage(ds, m, content, parentChannelID)
		return
	}

	// Commands
	switch {
	case content == "!resume":
		sess, ok := b.manager.GetSession(m.ChannelID)
		if !ok {
			ds.ChannelMessageSendReply(m.ChannelID, L.Get("error.no_active_session"), &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
			return
		}
		_ = sess
		// TODO: resume from agent's last text — requires storing last response in session or agent
		ds.ChannelMessageSendReply(m.ChannelID, L.Get("error.no_response"), &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})

	case content == "!pause":
		b.manager.Pause(m.ChannelID)
		ds.ChannelMessageSendReply(m.ChannelID, L.Get("pause.on"), &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})

	case content == "!back":
		b.manager.Back(m.ChannelID)
		ds.ChannelMessageSendReply(m.ChannelID, L.Get("pause.off"), &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})

	case content == "!reset":
		if err := b.manager.Reset(m.ChannelID); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.reset_failed", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Get("reset.success"))
		ds.ChannelMessageSend(m.ChannelID, usageMessage())

	case content == "!status":
		ds.ChannelMessageSend(m.ChannelID, b.statusWithSTT(m.ChannelID))

	case content == "!cancel":
		if err := b.manager.Cancel(m.ChannelID); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.cancel_failed", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Get("cancel.success"))

	case content == "!compact":
		resp, err := b.manager.SendCommand(m.ChannelID, "/compact")
		if err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
			return
		}
		if resp == "" {
			resp = L.Get("compact.success")
		}
		ds.ChannelMessageSend(m.ChannelID, "✅ "+resp)

	case content == "!clear":
		resp, err := b.manager.SendCommand(m.ChannelID, "/clear")
		if err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
			return
		}
		if resp == "" {
			resp = L.Get("clear.success")
		}
		ds.ChannelMessageSend(m.ChannelID, "✅ "+resp)

	case content == "!cwd":
		ds.ChannelMessageSend(m.ChannelID, b.manager.CWD(m.ChannelID))

	case strings.HasPrefix(content, "!cwd "):
		newCwd := strings.TrimSpace(strings.TrimPrefix(content, "!cwd "))
		if err := b.manager.SetCWD(m.ChannelID, newCwd); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Getf("cwd.set", newCwd))

	case strings.HasPrefix(content, "!start "):
		cwd := strings.TrimSpace(strings.TrimPrefix(content, "!start "))
		if cwd == "" {
			ds.ChannelMessageSend(m.ChannelID, L.Get("start.usage"))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Getf("start.starting", cwd))
		if err := b.manager.StartAt(m.ChannelID, cwd); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Getf("start.success", cwd))
		ds.ChannelMessageSend(m.ChannelID, usageMessage())

	case content == "!model":
		ds.ChannelMessageSend(m.ChannelID, b.manager.Model(m.ChannelID))

	case content == "!models":
		msg, err := b.manager.ListModels(m.ChannelID)
		if err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, msg)

	case strings.HasPrefix(content, "!model "):
		model := strings.TrimSpace(strings.TrimPrefix(content, "!model "))
		if err := b.manager.SetModel(m.ChannelID, model); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Getf("model.switching", model))
		if err := b.manager.Restart(m.ChannelID); err != nil {
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.reset_failed", err.Error()))
			return
		}
		ds.ChannelMessageSend(m.ChannelID, L.Getf("model.switched", model))

	case strings.HasPrefix(content, "!memory"):
		b.handleMemoryCommand(ds, m.ChannelID, m.ChannelID, strings.TrimSpace(strings.TrimPrefix(content, "!memory")))

	case strings.HasPrefix(content, "!flashmemory"):
		b.handleFlashMemoryCommand(ds, m.ChannelID, m.ChannelID, strings.TrimSpace(strings.TrimPrefix(content, "!flashmemory")))

	case strings.HasPrefix(content, "!cron"):
		b.handleCronTextCommand(ds, m.ChannelID, m.GuildID, m.Author.ID, content)

	case strings.HasPrefix(content, "!remind "):
		b.handleRemindText(ds, m.ChannelID, m.GuildID, m.Author.ID, m.Author.Username, strings.TrimPrefix(content, "!remind "))

	default:
		// Immediate feedback
		_ = ds.MessageReactionAdd(m.ChannelID, m.ID, "⏳")

		// Download attachments if any
		localPaths := b.downloadAttachments(m.ChannelID, m.Attachments)
		b.warnIfAttachmentsLarge(ds, m.ChannelID, localPaths)

		// Transcribe audio files (voice messages + audio attachments)
		var transcript string
		if t, rest := b.transcribeAudioFiles(localPaths, m.Attachments); t != "" {
			transcript = t
			content = L.Get("stt.prefix") + t + "\n" + content
			localPaths = rest
		}

		prompt := buildPrompt(content, localPaths, m.ChannelID, m.GuildID, m.Author.Username)

		job := &channel.Job{
			ChannelID:   m.ChannelID,
			MessageID:   m.ID,
			Prompt:      prompt,
			UserID:      m.Author.ID,
			Username:    m.Author.Username,
			Attachments: localPaths,
			Transcript:  transcript,
		}
		if err := b.manager.Enqueue(ds, job); err != nil {
			ds.MessageReactionRemove(m.ChannelID, m.ID, "⏳", "@me")
			ds.ChannelMessageSend(m.ChannelID, L.Getf("error.generic", err.Error()))
		}
	}
}

// handleThreadUpdate handles Discord thread archive/unarchive events.
func (b *Bot) handleThreadUpdate(ds *discordgo.Session, t *discordgo.ThreadUpdate) {
	if t.ThreadMetadata != nil && t.ThreadMetadata.Archived {
		if b.manager.HasThreadAgent(t.ID) {
			log.Printf("[handler] thread %s archived, stopping thread agent", t.ID)
			b.manager.StopThreadAgent(t.ID)
		}
	}
}

// handleThreadMessage handles messages sent inside a thread, routing to a dedicated thread agent.
func (b *Bot) handleThreadMessage(ds *discordgo.Session, m *discordgo.MessageCreate, content, parentChannelID string) {
	threadID := m.ChannelID

	// Thread-specific commands
	switch {
	case content == "!status":
		ds.ChannelMessageSend(threadID, b.statusWithSTT(parentChannelID))
		return
	case content == "!cancel":
		if err := b.manager.CancelThreadAgent(threadID); err != nil {
			ds.ChannelMessageSend(threadID, L.Getf("error.cancel_failed", err.Error()))
		} else {
			ds.ChannelMessageSend(threadID, L.Get("cancel.success"))
		}
		return
	case content == "!close":
		b.manager.StopThreadAgent(threadID)
		ds.ChannelMessageSend(threadID, L.Get("thread_agent.closed"))
		return
	case strings.HasPrefix(content, "!memory"):
		b.handleMemoryCommand(ds, threadID, parentChannelID, strings.TrimSpace(strings.TrimPrefix(content, "!memory")))
		return
	case strings.HasPrefix(content, "!flashmemory"):
		b.handleFlashMemoryCommand(ds, threadID, parentChannelID, strings.TrimSpace(strings.TrimPrefix(content, "!flashmemory")))
		return
	}

	// Immediate feedback
	_ = ds.MessageReactionAdd(threadID, m.ID, "⏳")

	// Build prompt and enqueue to thread agent
	localPaths := b.downloadAttachments(threadID, m.Attachments)
	b.warnIfAttachmentsLarge(ds, threadID, localPaths)

	// Transcribe audio files
	var transcript string
	if t, rest := b.transcribeAudioFiles(localPaths, m.Attachments); t != "" {
		transcript = t
		content = L.Get("stt.prefix") + t + "\n" + content
		localPaths = rest
	}

	prompt := buildPromptThread(content, localPaths, parentChannelID, threadID, m.GuildID, m.Author.Username)

	job := &channel.Job{
		ChannelID:   threadID,
		MessageID:   m.ID,
		Prompt:      prompt,
		UserID:      m.Author.ID,
		Username:    m.Author.Username,
		Attachments: localPaths,
		ThreadID:    threadID,
		Transcript:  transcript,
	}
	if err := b.manager.EnqueueThread(ds, job, parentChannelID); err != nil {
		ds.MessageReactionRemove(threadID, m.ID, "⏳", "@me")
		ds.ChannelMessageSend(threadID, L.Getf("error.generic", err.Error()))
	}
}

func buildSlashCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{Name: "start", Description: L.Get("cmd.start.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "cwd", Description: L.Get("cmd.start.opt.cwd"), Required: true},
		}},
		{Name: "reset", Description: L.Get("cmd.reset.desc")},
		{Name: "status", Description: L.Get("cmd.status.desc")},
		{Name: "cancel", Description: L.Get("cmd.cancel.desc")},
		{Name: "cwd", Description: L.Get("cmd.cwd.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "path", Description: L.Get("cmd.cwd.opt.path"), Required: false},
		}},
		{Name: "pause", Description: L.Get("cmd.pause.desc")},
		{Name: "back", Description: L.Get("cmd.back.desc")},
		{Name: "model", Description: L.Get("cmd.model.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "model", Description: L.Get("cmd.model.opt.model"), Required: false},
		}},
		{Name: "models", Description: L.Get("cmd.models.desc")},
		{Name: "cron", Description: L.Get("cmd.cron.desc")},
		{Name: "cron-list", Description: L.Get("cmd.cron_list.desc")},
		{Name: "compact", Description: L.Get("cmd.compact.desc")},
		{Name: "clear", Description: L.Get("cmd.clear.desc")},
		{Name: "cron-run", Description: L.Get("cmd.cron_run.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: L.Get("cmd.cron_run.opt.name"), Required: true},
		}},
		{Name: "remind", Description: L.Get("cmd.remind.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "time", Description: L.Get("cmd.remind.opt.time"), Required: true},
			{Type: discordgo.ApplicationCommandOptionString, Name: "content", Description: L.Get("cmd.remind.opt.content"), Required: true},
			{Type: discordgo.ApplicationCommandOptionBoolean, Name: "agent", Description: L.Get("cmd.remind.opt.agent"), Required: false},
		}},
		{Name: "memory", Description: L.Get("cmd.memory.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "action", Description: L.Get("cmd.memory.opt.action"), Required: true,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "list", Value: "list"},
					{Name: "add", Value: "add"},
					{Name: "remove", Value: "remove"},
					{Name: "clear", Value: "clear"},
				}},
			{Type: discordgo.ApplicationCommandOptionString, Name: "value", Description: L.Get("cmd.memory.opt.value"), Required: false},
		}},
		{Name: "flashmemory", Description: L.Get("cmd.flashmemory.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "action", Description: L.Get("cmd.flashmemory.opt.action"), Required: true,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "list", Value: "list"},
					{Name: "add", Value: "add"},
					{Name: "remove", Value: "remove"},
					{Name: "clear", Value: "clear"},
				}},
			{Type: discordgo.ApplicationCommandOptionString, Name: "value", Description: L.Get("cmd.flashmemory.opt.value"), Required: false},
		}},
	}
}

func (b *Bot) registerSlashCommands() {
	guildID := b.guildID
	// Clear global commands first
	if _, err := b.discord.ApplicationCommandBulkOverwrite(b.discord.State.User.ID, "", []*discordgo.ApplicationCommand{}); err != nil {
		log.Printf("[slash] clear global commands: %v", err)
	}
	created, err := b.discord.ApplicationCommandBulkOverwrite(b.discord.State.User.ID, guildID, buildSlashCommands())
	if err != nil {
		log.Printf("[slash] bulk overwrite error: %v", err)
		return
	}
	for _, cmd := range created {
		log.Printf("[slash] registered /%s (id=%s)", cmd.Name, cmd.ID)
	}
}

func (b *Bot) handleInteraction(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	// Ignore interactions from other guilds
	if !b.isMyGuild(i.GuildID) {
		return
	}
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleSlashCommand(ds, i)
	case discordgo.InteractionModalSubmit:
		customID := i.ModalSubmitData().CustomID
		if customID == "cron_add_modal" {
			b.handleCronModalSubmit(ds, i)
		} else if strings.HasPrefix(customID, "cron_edit_modal_") {
			b.handleCronEditSubmit(ds, i, strings.TrimPrefix(customID, "cron_edit_modal_"))
		}
	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		if strings.HasPrefix(customID, "cron_") {
			b.handleCronButton(ds, i)
		}
	}
}

func (b *Bot) handleSlashCommand(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	log.Printf("[interaction] /%s from %s", data.Name, i.ChannelID)
	channelID := i.ChannelID
	if parent := resolveThreadParent(ds, channelID); parent != "" {
		channelID = parent
	}

	// Commands that need their own response type (not deferred)
	switch data.Name {
	case "cron":
		b.handleCronModal(ds, i)
		return
	case "cron-list":
		b.handleCronList(ds, i)
		return
	case "cron-run":
		name := data.Options[0].StringValue()
		b.handleCronRun(ds, i, name)
		return
	case "remind":
		timeStr := data.Options[0].StringValue()
		content := data.Options[1].StringValue()
		useAgent := false
		if len(data.Options) > 2 {
			useAgent = data.Options[2].BoolValue()
		}
		b.handleRemind(ds, i, timeStr, content, useAgent)
		return
	}

	// All other commands: acknowledge immediately to avoid 3-second timeout
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	reply := func(msg string) {
		_, _ = ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: msg})
	}

	go func() {
		switch data.Name {
		case "start":
			cwd := data.Options[0].StringValue()
			reply(L.Getf("start.starting", cwd))
			if err := b.manager.StartAt(channelID, cwd); err != nil {
				reply(L.Getf("error.generic", err.Error()))
			} else {
				reply(L.Getf("start.success", cwd))
				reply(usageMessage())
			}
		case "reset":
			reply(L.Get("reset.resetting"))
			if err := b.manager.Reset(channelID); err != nil {
				reply(L.Getf("error.generic", err.Error()))
			} else {
				reply(L.Get("reset.success"))
				reply(usageMessage())
			}
		case "status":
			reply(b.statusWithSTT(channelID))
		case "cancel":
			if err := b.manager.Cancel(channelID); err != nil {
				reply(L.Getf("error.generic", err.Error()))
			} else {
				reply(L.Get("cancel.success"))
			}
		case "compact":
			resp, err := b.manager.SendCommand(channelID, "/compact")
			if err != nil {
				reply(L.Getf("error.generic", err.Error()))
			} else {
				if resp == "" {
					resp = L.Get("compact.success")
				}
				reply("✅ " + resp)
			}
		case "clear":
			resp, err := b.manager.SendCommand(channelID, "/clear")
			if err != nil {
				reply(L.Getf("error.generic", err.Error()))
			} else {
				if resp == "" {
					resp = L.Get("clear.success")
				}
				reply("✅ " + resp)
			}
		case "cwd":
			if len(data.Options) > 0 {
				newCwd := data.Options[0].StringValue()
				if err := b.manager.SetCWD(channelID, newCwd); err != nil {
					reply(L.Getf("error.generic", err.Error()))
				} else {
					reply(L.Getf("cwd.set", newCwd))
				}
			} else {
				reply(b.manager.CWD(channelID))
			}
		case "pause":
			b.manager.Pause(channelID)
			reply(L.Get("pause.on"))
		case "back":
			b.manager.Back(channelID)
			reply(L.Get("pause.off"))
		case "model":
			if len(data.Options) > 0 {
				model := data.Options[0].StringValue()
				if err := b.manager.SetModel(channelID, model); err != nil {
					reply(L.Getf("error.generic", err.Error()))
					return
				}
				reply(L.Getf("model.switching", model))
				if err := b.manager.Restart(channelID); err != nil {
					reply(L.Getf("error.reset_failed", err.Error()))
				} else {
					reply(L.Getf("model.switched", model))
				}
			} else {
				reply(b.manager.Model(channelID))
			}
		case "models":
			msg, err := b.manager.ListModels(channelID)
			if err != nil {
				reply(L.Getf("error.generic", err.Error()))
			} else {
				reply(msg)
			}
		case "memory":
			action := data.Options[0].StringValue()
			value := ""
			if len(data.Options) > 1 {
				value = data.Options[1].StringValue()
			}
			b.handleMemorySlash(reply, channelID, action, value)
		case "flashmemory":
			action := data.Options[0].StringValue()
			value := ""
			if len(data.Options) > 1 {
				value = data.Options[1].StringValue()
			}
			b.handleFlashMemorySlash(reply, channelID, action, value)
		}
	}()
}

// handleMemoryCommand dispatches !memory subcommands.
// replyTo is where to send the response, channelID is the memory key (parent channel).
func (b *Bot) handleMemoryCommand(ds *discordgo.Session, replyTo, channelID, args string) {
	switch {
	case args == "" || args == "list":
		entries := b.manager.MemoryList(channelID)
		if len(entries) == 0 {
			ds.ChannelMessageSend(replyTo, L.Get("memory.empty"))
			return
		}
		var sb strings.Builder
		sb.WriteString(L.Get("memory.list_header"))
		for i, e := range entries {
			sb.WriteString(fmt.Sprintf("`%d.` %s\n", i+1, e))
		}
		ds.ChannelMessageSend(replyTo, sb.String())

	case strings.HasPrefix(args, "add "):
		entry := strings.TrimSpace(strings.TrimPrefix(args, "add "))
		if entry == "" {
			ds.ChannelMessageSend(replyTo, L.Get("memory.usage"))
			return
		}
		if err := b.manager.MemoryAdd(channelID, entry); err != nil {
			ds.ChannelMessageSend(replyTo, L.Getf("error.save_failed", err.Error()))
			return
		}
		ds.ChannelMessageSend(replyTo, L.Getf("memory.added", entry))

	case strings.HasPrefix(args, "remove "):
		idxStr := strings.TrimSpace(strings.TrimPrefix(args, "remove "))
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			ds.ChannelMessageSend(replyTo, L.Get("memory.usage"))
			return
		}
		if err := b.manager.MemoryRemove(channelID, idx-1); err != nil {
			ds.ChannelMessageSend(replyTo, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(replyTo, L.Getf("memory.removed", idx))

	case args == "clear":
		if err := b.manager.MemoryClear(channelID); err != nil {
			ds.ChannelMessageSend(replyTo, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(replyTo, L.Get("memory.cleared"))

	default:
		ds.ChannelMessageSend(replyTo, L.Get("memory.usage"))
	}
}

// handleFlashMemoryCommand dispatches !flashmemory subcommands.
func (b *Bot) handleFlashMemoryCommand(ds *discordgo.Session, replyTo, channelID, args string) {
	switch {
	case args == "" || args == "list":
		entries := b.manager.FlashMemoryList(channelID)
		if len(entries) == 0 {
			ds.ChannelMessageSend(replyTo, L.Get("flashmemory.empty"))
			return
		}
		var sb strings.Builder
		sb.WriteString(L.Get("flashmemory.list_header"))
		for i, e := range entries {
			sb.WriteString(fmt.Sprintf("`%d.` %s\n", i+1, e))
		}
		ds.ChannelMessageSend(replyTo, sb.String())

	case strings.HasPrefix(args, "add "):
		entry := strings.TrimSpace(strings.TrimPrefix(args, "add "))
		if entry == "" {
			ds.ChannelMessageSend(replyTo, L.Get("flashmemory.usage"))
			return
		}
		b.manager.FlashMemoryAdd(channelID, entry)
		ds.ChannelMessageSend(replyTo, L.Getf("flashmemory.added", entry))

	case strings.HasPrefix(args, "remove "):
		idxStr := strings.TrimSpace(strings.TrimPrefix(args, "remove "))
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			ds.ChannelMessageSend(replyTo, L.Get("flashmemory.usage"))
			return
		}
		if err := b.manager.FlashMemoryRemove(channelID, idx-1); err != nil {
			ds.ChannelMessageSend(replyTo, L.Getf("error.generic", err.Error()))
			return
		}
		ds.ChannelMessageSend(replyTo, L.Getf("flashmemory.removed", idx))

	case args == "clear":
		b.manager.FlashMemoryClear(channelID)
		ds.ChannelMessageSend(replyTo, L.Get("flashmemory.cleared"))

	default:
		ds.ChannelMessageSend(replyTo, L.Get("flashmemory.usage"))
	}
}

// handleMemorySlash handles /memory slash command via reply func.
func (b *Bot) handleMemorySlash(reply func(string), channelID, action, value string) {
	value = strings.TrimSpace(value)
	switch action {
	case "list":
		entries := b.manager.MemoryList(channelID)
		if len(entries) == 0 {
			reply(L.Get("memory.empty"))
			return
		}
		var sb strings.Builder
		sb.WriteString(L.Get("memory.list_header"))
		for i, e := range entries {
			sb.WriteString(fmt.Sprintf("`%d.` %s\n", i+1, e))
		}
		reply(sb.String())
	case "add":
		if value == "" {
			reply(L.Get("memory.usage"))
			return
		}
		if err := b.manager.MemoryAdd(channelID, value); err != nil {
			reply(L.Getf("error.save_failed", err.Error()))
			return
		}
		reply(L.Getf("memory.added", value))
	case "remove":
		idx, err := strconv.Atoi(value)
		if err != nil {
			reply(L.Get("memory.usage"))
			return
		}
		if err := b.manager.MemoryRemove(channelID, idx-1); err != nil {
			reply(L.Getf("error.generic", err.Error()))
			return
		}
		reply(L.Getf("memory.removed", idx))
	case "clear":
		if err := b.manager.MemoryClear(channelID); err != nil {
			reply(L.Getf("error.generic", err.Error()))
			return
		}
		reply(L.Get("memory.cleared"))
	}
}

// handleFlashMemorySlash handles /flashmemory slash command via reply func.
func (b *Bot) handleFlashMemorySlash(reply func(string), channelID, action, value string) {
	value = strings.TrimSpace(value)
	switch action {
	case "list":
		entries := b.manager.FlashMemoryList(channelID)
		if len(entries) == 0 {
			reply(L.Get("flashmemory.empty"))
			return
		}
		var sb strings.Builder
		sb.WriteString(L.Get("flashmemory.list_header"))
		for i, e := range entries {
			sb.WriteString(fmt.Sprintf("`%d.` %s\n", i+1, e))
		}
		reply(sb.String())
	case "add":
		if value == "" {
			reply(L.Get("flashmemory.usage"))
			return
		}
		b.manager.FlashMemoryAdd(channelID, value)
		reply(L.Getf("flashmemory.added", value))
	case "remove":
		idx, err := strconv.Atoi(value)
		if err != nil {
			reply(L.Get("flashmemory.usage"))
			return
		}
		if err := b.manager.FlashMemoryRemove(channelID, idx-1); err != nil {
			reply(L.Getf("error.generic", err.Error()))
			return
		}
		reply(L.Getf("flashmemory.removed", idx))
	case "clear":
		b.manager.FlashMemoryClear(channelID)
		reply(L.Get("flashmemory.cleared"))
	}
}
