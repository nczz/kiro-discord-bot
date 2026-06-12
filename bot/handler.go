package bot

import (
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	"github.com/nczz/kiro-discord-bot/internal/textutil"
	L "github.com/nczz/kiro-discord-bot/locale"
	"github.com/nczz/kiro-discord-bot/stt"
)

func usageMessage() string { return L.Get("usage_message") }

func shouldIgnoreMessage(m *discordgo.MessageCreate, selfID string) bool {
	if m == nil || m.Message == nil || m.Author == nil {
		return true
	}
	return m.Author.ID == selfID
}

func isSelfMentioned(content, selfID string) bool {
	return strings.Contains(content, "<@"+selfID+">") || strings.Contains(content, "<@!"+selfID+">")
}

func messageMentionsUser(m *discordgo.MessageCreate, content, userID string) bool {
	if userID == "" {
		return false
	}
	if isSelfMentioned(content, userID) {
		return true
	}
	if m == nil || m.Message == nil {
		return false
	}
	for _, u := range m.Mentions {
		if u != nil && u.ID == userID {
			return true
		}
	}
	return false
}

func stripSelfMentions(content, selfID string) string {
	content = strings.ReplaceAll(content, "<@"+selfID+">", "")
	content = strings.ReplaceAll(content, "<@!"+selfID+">", "")
	return strings.TrimSpace(content)
}

func isBotGeneratedNonResult(content string) bool {
	content = strings.TrimSpace(strings.ReplaceAll(content, "\u200b", ""))
	if content == "" {
		return true
	}
	lower := strings.ToLower(content)
	nonResultPrefixes := []string{
		"🔄", "⏳", "❌", "⚠️", "💭",
		"processing", "bot running", "thread queue full", "transport closed",
	}
	for _, prefix := range nonResultPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func messageHasReaction(m *discordgo.Message, emoji string) bool {
	if m == nil {
		return false
	}
	for _, r := range m.Reactions {
		if r != nil && r.Count > 0 && r.Emoji != nil && r.Emoji.Name == emoji {
			return true
		}
	}
	return false
}

func messageReactionState(m *discordgo.Message) string {
	switch {
	case messageHasReaction(m, "✅"):
		return "done"
	case messageHasReaction(m, "🔄"), messageHasReaction(m, "⏳"):
		return "running"
	case messageHasReaction(m, "❌"), messageHasReaction(m, "⚠️"):
		return "failed"
	default:
		return "unknown"
	}
}

func ctxForAudit(channelID, targetID string, inThread bool, guildID, userID, username string) cmdCtx {
	return cmdCtx{
		channelID: channelID,
		targetID:  targetID,
		inThread:  inThread,
		guildID:   guildID,
		userID:    userID,
		username:  username,
	}
}

func isKnownBangCommand(name, content string) bool {
	if name == "" {
		return false
	}
	switch name {
	case "resume", "pause", "back", "silent", "thread", "reset", "status", "usage", "doctor", "audit", "mcp", "steering", "cancel", "interrupt",
		"close-thread", "compact", "clear", "cwd", "start", "agent", "model", "models", "memory", "flashmemory", "cron", "help":
		return true
	case "remind":
		return strings.HasPrefix(strings.TrimSpace(content), "!remind ")
	default:
		return false
	}
}

func (b *Bot) shouldAcceptBotResultMention(ds *discordgo.Session, m *discordgo.MessageCreate, content, selfID, parentChannelID string) bool {
	if m.Author == nil || !m.Author.Bot || m.Author.ID == selfID {
		return false
	}
	if !b.messageMentionsSelf(m, content, selfID) {
		log.Printf("[bot-gate] ignored bot msg reason=no_mention source=%s channel=%s msg=%s", m.Author.ID, m.ChannelID, m.ID)
		return false
	}
	if parentChannelID == "" {
		log.Printf("[bot-gate] ignored bot mention reason=not_thread source=%s channel=%s msg=%s", m.Author.ID, m.ChannelID, m.ID)
		return false
	}
	if isBotGeneratedNonResult(b.stripOwnMentions(content, selfID)) {
		log.Printf("[bot-gate] ignored bot mention reason=non_result source=%s thread=%s msg=%s", m.Author.ID, m.ChannelID, m.ID)
		return false
	}

	origin, err := ds.ChannelMessage(parentChannelID, m.ChannelID)
	if err != nil {
		log.Printf("[handler] ignore bot mention: fetch thread origin channel=%s msg=%s: %v", parentChannelID, m.ChannelID, err)
		return false
	}
	switch state := messageReactionState(origin); state {
	case "done":
		log.Printf("[bot-gate] accepted bot result mention source=%s thread=%s msg=%s origin=%s", m.Author.ID, m.ChannelID, m.ID, origin.ID)
		return true
	case "running", "failed":
		log.Printf("[bot-gate] ignored bot mention reason=origin_%s source=%s thread=%s msg=%s origin=%s", state, m.Author.ID, m.ChannelID, m.ID, origin.ID)
	default:
		log.Printf("[bot-gate] ignored bot mention reason=origin_not_done source=%s thread=%s msg=%s origin=%s", m.Author.ID, m.ChannelID, m.ID, origin.ID)
	}
	return false
}

// threadParentCache caches thread→parent channel mappings to avoid repeated API calls.
// Evicts all entries when capacity is reached (simple reset strategy).
var (
	threadParentMu       sync.RWMutex
	threadParentCache    = make(map[string]string) // threadID → parentChannelID, "" = not a thread
	threadParentCacheMax = 1000
)

// seenMessages is a TTL-based set to deduplicate Discord MESSAGE_CREATE events
// that may be replayed during gateway reconnections.
type seenMessages struct {
	mu      sync.Mutex
	entries map[string]time.Time
	stopCh  chan struct{}
}

func newSeenMessages() *seenMessages {
	s := &seenMessages{entries: make(map[string]time.Time), stopCh: make(chan struct{})}
	ticker := time.NewTicker(60 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.mu.Lock()
				cutoff := time.Now().Add(-5 * time.Minute)
				for id, t := range s.entries {
					if t.Before(cutoff) {
						delete(s.entries, id)
					}
				}
				s.mu.Unlock()
			}
		}
	}()
	return s
}

func (s *seenMessages) Stop() {
	close(s.stopCh)
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
	if len(threadParentCache) >= threadParentCacheMax {
		threadParentCache = make(map[string]string)
	}
	threadParentCache[channelID] = parentID
	threadParentMu.Unlock()
	return parentID
}

// registerThreadParent caches a known thread→parent mapping (called when bot creates a thread).
func registerThreadParent(threadID, parentChannelID string) {
	threadParentMu.Lock()
	if len(threadParentCache) >= threadParentCacheMax {
		threadParentCache = make(map[string]string)
	}
	threadParentCache[threadID] = parentChannelID
	threadParentMu.Unlock()
}

func (b *Bot) statusWithRuntime(s string) string {
	s += "\n" + L.Getf("status.bot_uptime", textutil.FormatUptime(time.Since(b.startedAt)))
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
		_, _ = sendDiscordText(ds, channelID, L.Getf("warn.attachments_large", mb, limit/(1024*1024)), nil)
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
		if b.attachmentMaxBytes > 0 && att.Size > int(b.attachmentMaxBytes) {
			log.Printf("[attach] skip %s: size %d > max %d", att.Filename, att.Size, b.attachmentMaxBytes)
			continue
		}
		resp, err := b.downloadClient.Get(att.URL)
		if err != nil {
			log.Printf("[attach] download %s: %v (url=%s)", att.Filename, err, att.URL)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			log.Printf("[attach] download %s: HTTP %d", att.Filename, resp.StatusCode)
			continue
		}
		if b.attachmentMaxBytes > 0 && resp.ContentLength > b.attachmentMaxBytes {
			resp.Body.Close()
			log.Printf("[attach] skip %s: content-length %d > max %d", att.Filename, resp.ContentLength, b.attachmentMaxBytes)
			continue
		}
		dst := filepath.Join(attDir, ts+"-"+safeAttachmentFilename(att.Filename))
		f, err := os.Create(dst)
		if err != nil {
			resp.Body.Close()
			log.Printf("[attach] create %s: %v", dst, err)
			continue
		}
		reader := io.Reader(resp.Body)
		if b.attachmentMaxBytes > 0 {
			reader = io.LimitReader(resp.Body, b.attachmentMaxBytes+1)
		}
		n, err := io.Copy(f, reader)
		resp.Body.Close()
		f.Close()
		if err != nil {
			log.Printf("[attach] write %s: %v", dst, err)
			continue
		}
		if b.attachmentMaxBytes > 0 && n > b.attachmentMaxBytes {
			_ = os.Remove(dst)
			log.Printf("[attach] skip %s: downloaded %d > max %d", att.Filename, n, b.attachmentMaxBytes)
			continue
		}
		abs, _ := filepath.Abs(dst)
		paths = append(paths, abs)
	}
	return paths
}

func safeAttachmentFilename(name string) string {
	decoder := new(mime.WordDecoder)
	if decoded, err := decoder.DecodeHeader(name); err == nil && decoded != "" {
		name = decoded
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == '_' || r == '.' || r == ' ':
			return r
		case r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= 'a' && r <= 'z':
			return r
		default:
			return '_'
		}
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" {
		return "attachment"
	}
	return name
}

// buildPrompt combines user text with attachment paths into an effective prompt.
func buildPrompt(text string, attachments []string, channelID, guildID, username, peerContext string) string {
	return buildPromptThread(text, attachments, channelID, "", guildID, username, peerContext)
}

func buildPromptThread(text string, attachments []string, channelID, threadID, guildID, username, peerContext string) string {
	var sb strings.Builder
	sb.WriteString("[Discord bot environment] Your responses are automatically forwarded to a Discord thread. Each message is split at 2000 chars. Tool execution details are also shown.\n")
	sb.WriteString("Do not call bot_send_message for ordinary replies or final answers; write the reply normally and the bot will redact, split, and deliver it. bot_send_message is not default-enabled; if it is available, use it only when the user explicitly asks you to send a separate extra Discord message, notify another target, or hand off to another bot.\n")
	sb.WriteString("For cron management tools, use channel_id as the owning parent channel ID; use thread_id only for thread-targeted Discord messages.\n")
	if threadID != "" {
		sb.WriteString(fmt.Sprintf("[Discord context] channel_id=%s thread_id=%s guild_id=%s user=%s\n\n", channelID, threadID, guildID, username))
	} else {
		sb.WriteString(fmt.Sprintf("[Discord context] channel_id=%s guild_id=%s user=%s\n\n", channelID, guildID, username))
	}
	if peerContext != "" {
		sb.WriteString(peerContext)
		sb.WriteString("\n")
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
	b.recordChannelMetadata(ds, m.ChannelID, m.GuildID)
	selfID := ds.State.User.ID
	if shouldIgnoreMessage(m, selfID) {
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

	isMentioned := b.messageMentionsSelf(m, content, selfID)
	isCommand := strings.HasPrefix(content, "!")

	parentChannelID := resolveThreadParent(ds, m.ChannelID)
	handoff := false
	if m.Author.Bot {
		handoff = b.shouldAcceptBotResultMention(ds, m, content, selfID, parentChannelID)
		if !handoff {
			return
		}
	}

	if !m.Author.Bot && !isMentioned && b.messageMentionsOtherPeer(m, content, selfID) {
		log.Printf("[handler] ignored human msg reason=other_peer_mentioned channel=%s thread=%t msg=%s", m.ChannelID, parentChannelID != "", m.ID)
		return
	}
	if !m.Author.Bot && isMentioned && !b.humanMessageAddressesSelf(m, content, selfID) {
		log.Printf("[handler] ignored human msg reason=self_mentioned_as_task_target channel=%s thread=%t msg=%s", m.ChannelID, parentChannelID != "", m.ID)
		return
	}

	if !m.Author.Bot && !isCommand && !isMentioned {
		if mentionRequired, mentionReason := b.requiresHumanMention(ds, m.ChannelID, parentChannelID, selfID); mentionRequired {
			log.Printf("[handler] ignored human msg reason=%s channel=%s thread=%t msg=%s", mentionReason, m.ChannelID, parentChannelID != "", m.ID)
			return
		}
	}

	// In pause mode, only respond to commands or mentions.
	if !m.Author.Bot && b.manager.IsPaused(m.ChannelID) && !isCommand && !isMentioned {
		return
	}

	// Strip mention prefix if present
	if isMentioned {
		content = b.stripOwnMentions(content, selfID)
		if !m.Author.Bot {
			content = b.stripLeadingPeerMentions(content)
		}
	}

	// Check if message is from a thread — route to thread agent
	if parentChannelID != "" {
		b.handleThreadMessage(ds, m, content, parentChannelID, handoff)
		return
	}

	// Commands
	bangCommand := commandNameFromBang(content)
	isKnownCommand := isKnownBangCommand(bangCommand, content)
	auditCtx := ctxForAudit(m.ChannelID, m.ChannelID, false, m.GuildID, m.Author.ID, m.Author.Username)
	auditCtx.messageID = m.ID
	reply := func(msg string) {
		sent, err := sendDiscordText(ds, m.ChannelID, msg, &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
		if isKnownCommand {
			b.recordCommandResponseDelivery(auditCtx, bangCommand, "message", "sent", msg, nil, sent, err)
		}
	}
	replyWithMetadata := func(msg string, metadata map[string]any) {
		sent, err := sendDiscordText(ds, m.ChannelID, msg, &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
		if isKnownCommand {
			b.recordCommandResponseDelivery(auditCtx, bangCommand, "message", "sent", msg, metadata, sent, err)
		}
	}
	ctx := cmdCtx{
		channelID:         m.ChannelID,
		targetID:          m.ChannelID,
		inThread:          false,
		reply:             reply,
		replyWithMetadata: replyWithMetadata,
		guildID:           m.GuildID,
		userID:            m.Author.ID,
		username:          m.Author.Username,
		messageID:         m.ID,
	}
	if isKnownCommand {
		b.recordCommandInvoked(ctx, bangCommand, "message", m.ID, "")
	}
	if isKnownCommand {
		gateArgs := strings.TrimSpace(strings.TrimPrefix(content, "!"+bangCommand))
		if bangCommand == "remind" {
			if strings.HasPrefix(gateArgs, "--agent ") && !b.requireInitializedCommand(ctx, "!remind --agent") {
				b.recordCommandCompleted(ctx, bangCommand, "message", "rejected", "channel_uninitialized")
				return
			}
		} else if commandRequiresInitializedChannel(bangCommand, gateArgs) && !b.requireInitializedCommand(ctx, "!"+bangCommand) {
			b.recordCommandCompleted(ctx, bangCommand, "message", "rejected", "channel_uninitialized")
			return
		}
	}
	if isKnownCommand {
		defer b.recordCommandCompleted(ctx, bangCommand, "message", "completed", "")
	}

	switch {
	case content == "!resume":
		b.cmdResume(ctx)
	case content == "!pause":
		b.cmdPause(ctx)
	case content == "!back":
		b.cmdBack(ctx)
	case content == "!silent", content == "!silent on", content == "!silent off":
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!silent"))
		b.cmdSilent(ctx)
	case content == "!thread", content == "!thread on", content == "!thread off":
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!thread"))
		b.cmdThreadMode(ctx)
	case content == "!reset":
		b.cmdReset(ctx)
	case content == "!help":
		b.cmdHelp(ctx)
	case content == "!status":
		b.cmdStatus(ctx)
	case content == "!usage" || strings.HasPrefix(content, "!usage "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!usage"))
		b.cmdUsage(ctx)
	case content == "!doctor":
		b.cmdDoctor(ctx)
	case content == "!audit" || strings.HasPrefix(content, "!audit "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!audit"))
		b.cmdAudit(ctx)
	case content == "!mcp" || strings.HasPrefix(content, "!mcp "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!mcp"))
		b.cmdMCP(ctx)
	case content == "!steering" || strings.HasPrefix(content, "!steering "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!steering"))
		b.cmdSteering(ctx)
	case content == "!cancel":
		b.cmdCancel(ctx)
	case content == "!interrupt":
		b.cmdInterrupt(ctx)
	case content == "!close-thread" || strings.HasPrefix(content, "!close-thread "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!close-thread"))
		b.cmdCloseThread(ctx)
	case content == "!compact":
		b.cmdCompact(ctx)
	case content == "!clear":
		b.cmdClear(ctx)
	case content == "!cwd":
		b.cmdCwd(ctx)
	case strings.HasPrefix(content, "!cwd "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!cwd "))
		b.cmdCwd(ctx)
	case strings.HasPrefix(content, "!start "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!start "))
		b.cmdStart(ctx)
	case content == "!agent":
		b.cmdAgent(ctx)
	case strings.HasPrefix(content, "!agent "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!agent "))
		b.cmdAgent(ctx)
	case content == "!model":
		b.cmdModel(ctx)
	case content == "!models":
		b.cmdModels(ctx)
	case strings.HasPrefix(content, "!model "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!model "))
		b.cmdModel(ctx)
	case strings.HasPrefix(content, "!memory"):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!memory"))
		b.cmdMemory(ctx)
	case strings.HasPrefix(content, "!flashmemory"):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!flashmemory"))
		b.cmdFlashMemory(ctx)
	case strings.HasPrefix(content, "!cron"):
		b.handleCronTextCommand(ds, m.ChannelID, m.GuildID, m.Author.ID, content)
	case strings.HasPrefix(content, "!remind "):
		b.handleRemindText(ds, m.ChannelID, m.GuildID, m.Author.ID, m.Author.Username, strings.TrimPrefix(content, "!remind "))
	default:
		if !b.manager.ChannelInitialized(m.ChannelID) {
			b.sendChannelSetupPrompt(ds, m)
			return
		}

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

		prompt := buildPrompt(content, localPaths, m.ChannelID, m.GuildID, m.Author.Username, b.peerPromptContext(selfID))
		deliveryMode := channel.DeliveryThread
		if !b.manager.ThreadModeEnabled(m.ChannelID) {
			deliveryMode = channel.DeliveryInline
		}

		job := &channel.Job{
			ChannelID:         m.ChannelID,
			GuildID:           m.GuildID,
			MessageID:         m.ID,
			Prompt:            prompt,
			UserID:            m.Author.ID,
			Username:          m.Author.Username,
			Attachments:       localPaths,
			Transcript:        transcript,
			Source:            "message",
			DeliveryMode:      deliveryMode,
		}
		job.ThreadMentionOnly, _ = b.requiresHumanMention(ds, m.ChannelID, "", selfID)
		if err := b.manager.Enqueue(ds, job); err != nil {
			ds.MessageReactionRemove(m.ChannelID, m.ID, "⏳", "@me")
			_, _ = sendDiscordText(ds, m.ChannelID, L.Getf("error.generic", err.Error()), nil)
		}
	}
}

// handleThreadUpdate handles Discord thread archive/unarchive events.
func (b *Bot) handleThreadUpdate(ds *discordgo.Session, t *discordgo.ThreadUpdate) {
	if t.ThreadMetadata != nil && t.ThreadMetadata.Archived {
		if b.manager.HasThreadAgent(t.ID) {
			stopped, deferred := b.manager.MarkThreadArchived(t.ID)
			if deferred {
				log.Printf("[handler] thread %s archived while agent is active; scheduled stop after current job", t.ID)
				if t.ParentID != "" {
					_, _ = sendDiscordText(ds, t.ParentID, L.Getf("thread_agent.archive_deferred", t.ID), nil)
				}
			} else if stopped {
				log.Printf("[handler] thread %s archived, stopping thread agent", t.ID)
			}
		}
	}
}

// handleThreadMessage handles messages sent inside a thread, routing to a dedicated thread agent.
func (b *Bot) handleThreadMessage(ds *discordgo.Session, m *discordgo.MessageCreate, content, parentChannelID string, handoff bool) {
	threadID := m.ChannelID
	bangCommand := commandNameFromBang(content)
	isKnownCommand := isKnownBangCommand(bangCommand, content)
	auditCtx := ctxForAudit(parentChannelID, threadID, true, m.GuildID, m.Author.ID, m.Author.Username)
	auditCtx.messageID = m.ID
	reply := func(msg string) {
		sent, err := sendDiscordText(ds, threadID, msg, nil)
		if isKnownCommand {
			b.recordCommandResponseDelivery(auditCtx, bangCommand, "thread_message", "sent", msg, nil, sent, err)
		}
	}
	replyWithMetadata := func(msg string, metadata map[string]any) {
		sent, err := sendDiscordText(ds, threadID, msg, nil)
		if isKnownCommand {
			b.recordCommandResponseDelivery(auditCtx, bangCommand, "thread_message", "sent", msg, metadata, sent, err)
		}
	}
	ctx := cmdCtx{
		channelID:         parentChannelID,
		targetID:          threadID,
		inThread:          true,
		reply:             reply,
		replyWithMetadata: replyWithMetadata,
		guildID:           m.GuildID,
		userID:            m.Author.ID,
		username:          m.Author.Username,
		messageID:         m.ID,
	}
	if isKnownCommand {
		b.recordCommandInvoked(ctx, bangCommand, "thread_message", m.ID, "")
	}
	if isKnownCommand && !isChannelOnlySlashCommand(bangCommand) {
		gateArgs := strings.TrimSpace(strings.TrimPrefix(content, "!"+bangCommand))
		if commandRequiresInitializedChannel(bangCommand, gateArgs) && !b.requireInitializedCommand(ctx, "!"+bangCommand) {
			b.recordCommandCompleted(ctx, bangCommand, "thread_message", "rejected", "channel_uninitialized")
			return
		}
	}
	if isKnownCommand && isChannelOnlySlashCommand(bangCommand) {
		ctx.reply(L.Get("error.channel_only"))
		b.recordCommandCompleted(ctx, bangCommand, "thread_message", "rejected", "channel_only")
		return
	}
	if isKnownCommand {
		defer b.recordCommandCompleted(ctx, bangCommand, "thread_message", "completed", "")
	}

	// Thread-specific commands
	switch {
	case content == "!status":
		b.cmdStatus(ctx)
		return
	case content == "!usage" || strings.HasPrefix(content, "!usage "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!usage"))
		b.cmdUsage(ctx)
		return
	case content == "!doctor":
		b.cmdDoctor(ctx)
		return
	case content == "!audit" || strings.HasPrefix(content, "!audit "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!audit"))
		b.cmdAudit(ctx)
		return
	case content == "!mcp" || strings.HasPrefix(content, "!mcp "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!mcp"))
		b.cmdMCP(ctx)
		return
	case content == "!cancel":
		b.cmdCancel(ctx)
		return
	case content == "!interrupt":
		b.cmdInterrupt(ctx)
		return
	case content == "!close":
		b.cmdClose(ctx)
		return
	case content == "!close-thread" || strings.HasPrefix(content, "!close-thread "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!close-thread"))
		b.cmdCloseThread(ctx)
		return
	case content == "!pause":
		b.cmdPause(ctx)
		return
	case content == "!back":
		b.cmdBack(ctx)
		return
	case content == "!silent", content == "!silent on", content == "!silent off":
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!silent"))
		b.cmdSilent(ctx)
		return
	case content == "!thread", content == "!thread on", content == "!thread off":
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!thread"))
		b.cmdThreadMode(ctx)
		return
	case content == "!compact":
		b.cmdCompact(ctx)
		return
	case content == "!clear":
		b.cmdClear(ctx)
		return
	case content == "!reset":
		b.cmdReset(ctx)
		return
	case content == "!help":
		b.cmdHelp(ctx)
		return
	case content == "!model":
		b.cmdModel(ctx)
		return
	case strings.HasPrefix(content, "!model "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!model "))
		b.cmdModel(ctx)
		return
	case content == "!models":
		b.cmdModels(ctx)
		return
	case content == "!cwd" || strings.HasPrefix(content, "!cwd "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!cwd"))
		b.cmdCwd(ctx)
		return
	case content == "!steering" || strings.HasPrefix(content, "!steering "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!steering"))
		b.cmdSteering(ctx)
		return
	case content == "!start" || strings.HasPrefix(content, "!start "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!start"))
		b.cmdStart(ctx)
		return
	case content == "!agent" || strings.HasPrefix(content, "!agent "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!agent"))
		b.cmdAgent(ctx)
		return
	case content == "!resume":
		b.cmdResume(ctx)
		return
	case strings.HasPrefix(content, "!cron") || strings.HasPrefix(content, "!remind "):
		ctx.reply(L.Get("error.channel_only"))
		return
	case strings.HasPrefix(content, "!memory"):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!memory"))
		b.cmdMemory(ctx)
		return
	case strings.HasPrefix(content, "!flashmemory"):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!flashmemory"))
		b.cmdFlashMemory(ctx)
		return
	}

	if !b.manager.ChannelInitialized(parentChannelID) {
		b.sendThreadSetupPrompt(ds, m, parentChannelID)
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

	selfID := ""
	if ds.State != nil && ds.State.User != nil {
		selfID = ds.State.User.ID
	}
	prompt := buildPromptThread(content, localPaths, parentChannelID, threadID, m.GuildID, m.Author.Username, b.peerPromptContext(selfID))

	job := &channel.Job{
		ChannelID:       threadID,
		ParentChannelID: parentChannelID,
		GuildID:         m.GuildID,
		MessageID:       m.ID,
		Prompt:          prompt,
		UserID:          m.Author.ID,
		Username:        m.Author.Username,
		Attachments:     localPaths,
		ThreadID:        threadID,
		Transcript:      transcript,
		Handoff:         handoff,
		Source:          "thread",
	}
	if err := b.manager.EnqueueThread(ds, job, parentChannelID); err != nil {
		ds.MessageReactionRemove(threadID, m.ID, "⏳", "@me")
		_, _ = sendDiscordText(ds, threadID, commandError(err), nil)
	}
}

func buildSlashCommands() []*discordgo.ApplicationCommand {
	commands := []*discordgo.ApplicationCommand{
		{Name: "start", Description: L.Get("cmd.start.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "cwd", Description: L.Get("cmd.start.opt.cwd"), Required: true},
		}},
		{Name: "reset", Description: L.Get("cmd.reset.desc")},
		{Name: "help", Description: L.Get("cmd.help.desc")},
		{Name: "status", Description: L.Get("cmd.status.desc")},
		{Name: "usage", Description: L.Get("cmd.usage.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: L.Get("cmd.usage.opt.user"), Required: false},
		}},
		{Name: "doctor", Description: L.Get("cmd.doctor.desc")},
		{Name: "audit", Description: L.Get("cmd.audit.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "limit", Description: L.Get("cmd.audit.opt.limit"), Required: false},
		}},
		{Name: "mcp", Description: L.Get("cmd.mcp.desc"), Options: mcpSlashOptions()},
		{Name: "steering", Description: L.Get("cmd.steering.desc"), Options: steeringSlashOptions()},
		{Name: "cancel", Description: L.Get("cmd.cancel.desc")},
		{Name: "interrupt", Description: L.Get("cmd.interrupt.desc")},
		{Name: "cwd", Description: L.Get("cmd.cwd.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "path", Description: L.Get("cmd.cwd.opt.path"), Required: false},
		}},
		{Name: "pause", Description: L.Get("cmd.pause.desc")},
		{Name: "back", Description: L.Get("cmd.back.desc")},
		{Name: "silent", Description: L.Get("cmd.silent.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "mode", Description: L.Get("cmd.silent.opt.mode"), Required: false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				}},
		}},
		{Name: "thread", Description: L.Get("cmd.thread.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "mode", Description: L.Get("cmd.thread.opt.mode"), Required: false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				}},
		}},
		{Name: "model", Description: L.Get("cmd.model.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "model", Description: L.Get("cmd.model.opt.model"), Required: false},
		}},
		{Name: "models", Description: L.Get("cmd.models.desc")},
		{Name: "agent", Description: L.Get("cmd.agent.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "mode", Description: L.Get("cmd.agent.opt.mode"), Required: false},
		}},
		{Name: "cron", Description: L.Get("cmd.cron.desc")},
		{Name: "cron-list", Description: L.Get("cmd.cron_list.desc")},
		{Name: "compact", Description: L.Get("cmd.compact.desc")},
		{Name: "clear", Description: L.Get("cmd.clear.desc")},
		{Name: "close", Description: L.Get("cmd.close.desc")},
		{Name: "close-thread", Description: L.Get("cmd.close_thread.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "thread_id", Description: L.Get("cmd.close_thread.opt.thread_id"), Required: true},
		}},
		{Name: "resume", Description: L.Get("cmd.resume.desc")},
		{Name: "cron-run", Description: L.Get("cmd.cron_run.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: L.Get("cmd.cron_run.opt.name"), Required: true, Autocomplete: true},
		}},
		{Name: "cron-prompt", Description: L.Get("cmd.cron_prompt.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "description", Description: L.Get("cmd.cron_prompt.opt"), Required: true},
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
	for _, cmd := range commands {
		applySlashCommandPolicy(cmd)
	}
	return commands
}

func mcpSlashOptions() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "manage", Description: L.Get("cmd.mcp.sub.manage")},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "status", Description: L.Get("cmd.mcp.sub.status"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "server", Description: L.Get("cmd.mcp.opt.server"), Required: false},
		}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "enable", Description: L.Get("cmd.mcp.sub.enable"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "server", Description: L.Get("cmd.mcp.opt.server"), Required: true},
		}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "disable", Description: L.Get("cmd.mcp.sub.disable"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "server", Description: L.Get("cmd.mcp.opt.server"), Required: true},
		}},
	}
}

func mcpArgsFromSlashOptions(options []*discordgo.ApplicationCommandInteractionDataOption) string {
	if len(options) == 0 {
		return "status"
	}
	sub := options[0]
	args := []string{sub.Name}
	for _, opt := range sub.Options {
		switch opt.Type {
		case discordgo.ApplicationCommandOptionBoolean:
			if opt.BoolValue() {
				args = append(args, "on")
			} else {
				args = append(args, "off")
			}
		default:
			if s := strings.TrimSpace(opt.StringValue()); s != "" {
				args = append(args, s)
			}
		}
	}
	return strings.Join(args, " ")
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

func (b *Bot) handleAutocomplete(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.slashCommandAllowedInTarget(ds, i.ChannelID) {
		_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionApplicationCommandAutocompleteResult,
			Data: &discordgo.InteractionResponseData{Choices: []*discordgo.ApplicationCommandOptionChoice{}},
		})
		return
	}
	data := i.ApplicationCommandData()
	if data.Name != "cron-run" {
		return
	}
	// Get typed value
	var typed string
	for _, opt := range data.Options {
		if opt.Name == "name" && opt.Focused {
			typed = strings.ToLower(opt.StringValue())
		}
	}
	// List jobs for this channel, filter by typed prefix
	jobs := b.cronStore.ListByChannel(i.ChannelID)
	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, job := range jobs {
		if typed == "" || strings.Contains(strings.ToLower(job.Name), typed) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  secrets.RedactEnv(job.Name),
				Value: job.Name,
			})
		}
		if len(choices) >= 25 { // Discord max
			break
		}
	}
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	})
}
func (b *Bot) handleInteraction(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	// Ignore interactions from other guilds
	if !b.isMyGuild(i.GuildID) {
		return
	}
	b.recordChannelMetadata(ds, i.ChannelID, i.GuildID)
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleSlashCommand(ds, i)
	case discordgo.InteractionApplicationCommandAutocomplete:
		b.handleAutocomplete(ds, i)
	case discordgo.InteractionModalSubmit:
		customID := i.ModalSubmitData().CustomID
		if customID == "cron_add_modal" {
			b.handleCronModalSubmit(ds, i)
		} else if strings.HasPrefix(customID, "cron_edit_modal_") {
			b.handleCronEditSubmit(ds, i, strings.TrimPrefix(customID, "cron_edit_modal_"))
		} else if strings.HasPrefix(customID, cwdCustomPrefix+":newmodal:") {
			b.handleCWDModalSubmit(ds, i, strings.TrimPrefix(customID, cwdCustomPrefix+":newmodal:"))
		} else if strings.HasPrefix(customID, steeringCustomPrefix+":create_modal:") {
			b.handleSteeringCreateModalSubmit(ds, i, strings.TrimPrefix(customID, steeringCustomPrefix+":create_modal:"))
		} else if strings.HasPrefix(customID, steeringCustomPrefix+":modal:") {
			b.handleSteeringModalSubmit(ds, i, strings.TrimPrefix(customID, steeringCustomPrefix+":modal:"))
		}
	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		if strings.HasPrefix(customID, mcpCustomPrefix+":") {
			b.handleMCPComponent(ds, i)
		} else if strings.HasPrefix(customID, steeringCustomPrefix+":") {
			b.handleSteeringComponent(ds, i)
		} else if strings.HasPrefix(customID, cwdCustomPrefix+":") {
			b.handleCWDComponent(ds, i)
		} else if strings.HasPrefix(customID, "cronp_") {
			b.handleCronPromptButton(ds, i)
		} else if strings.HasPrefix(customID, "cron_") {
			b.handleCronButton(ds, i)
		}
	}
}

func interactionUser(i *discordgo.InteractionCreate) (string, string) {
	if i == nil || i.Interaction == nil {
		return "", ""
	}
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID, i.Member.User.Username
	}
	if i.User != nil {
		return i.User.ID, i.User.Username
	}
	return "", ""
}

func (b *Bot) handleSlashCommand(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	log.Printf("[interaction] /%s from %s", data.Name, i.ChannelID)
	rawChannelID := i.ChannelID
	userID, username := interactionUser(i)
	auditCtx := ctxForAudit(rawChannelID, rawChannelID, false, i.GuildID, userID, username)
	auditCtx.interactionID = i.ID
	b.recordCommandInvoked(auditCtx, data.Name, "slash", "", i.ID)
	if !b.slashCommandAllowedInTarget(ds, rawChannelID) {
		log.Printf("[interaction] rejected /%s reason=bot_not_in_channel channel=%s", data.Name, rawChannelID)
		msg := L.Get("error.bot_not_in_channel")
		err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content:         msg,
				AllowedMentions: &discordgo.MessageAllowedMentions{},
				Flags:           discordgo.MessageFlagsEphemeral,
			},
		})
		b.recordInteractionResponseDelivery(auditCtx, data.Name, "rejected", msg, discordgo.InteractionResponseChannelMessageWithSource, map[string]any{"ephemeral": true}, err)
		b.recordCommandCompleted(auditCtx, data.Name, "slash", "rejected", "bot_not_in_channel")
		return
	}
	threadParent := resolveThreadParent(ds, rawChannelID)
	channelID := rawChannelID
	if threadParent != "" {
		channelID = threadParent
	}
	inThread := threadParent != ""
	auditCtx.channelID = channelID
	auditCtx.targetID = rawChannelID
	auditCtx.inThread = inThread

	if inThread && isChannelOnlySlashCommand(data.Name) {
		msg := L.Get("error.channel_only")
		err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content:         msg,
				AllowedMentions: &discordgo.MessageAllowedMentions{},
				Flags:           discordgo.MessageFlagsEphemeral,
			},
		})
		b.recordInteractionResponseDelivery(auditCtx, data.Name, "rejected", msg, discordgo.InteractionResponseChannelMessageWithSource, map[string]any{"ephemeral": true}, err)
		b.recordCommandCompleted(auditCtx, data.Name, "slash", "rejected", "channel_only")
		return
	}

	// Commands that need their own response type (not deferred)
	switch data.Name {
	case "cron":
		if !b.requireInitializedInteraction(ds, i, auditCtx, data.Name) {
			b.recordCommandCompleted(auditCtx, data.Name, "slash", "rejected", "channel_uninitialized")
			return
		}
		b.handleCronModal(ds, i, auditCtx)
		b.recordCommandCompleted(auditCtx, data.Name, "slash", "completed", "")
		return
	case "cron-list":
		b.handleCronList(ds, i, auditCtx)
		b.recordCommandCompleted(auditCtx, data.Name, "slash", "completed", "")
		return
	case "cron-run":
		if !b.requireInitializedInteraction(ds, i, auditCtx, data.Name) {
			b.recordCommandCompleted(auditCtx, data.Name, "slash", "rejected", "channel_uninitialized")
			return
		}
		name := data.Options[0].StringValue()
		b.handleCronRun(ds, i, auditCtx, name)
		b.recordCommandCompleted(auditCtx, data.Name, "slash", "completed", "")
		return
	case "cron-prompt":
		if !b.requireInitializedInteraction(ds, i, auditCtx, data.Name) {
			b.recordCommandCompleted(auditCtx, data.Name, "slash", "rejected", "channel_uninitialized")
			return
		}
		desc := data.Options[0].StringValue()
		b.handleCronPrompt(ds, i, auditCtx, desc)
		b.recordCommandCompleted(auditCtx, data.Name, "slash", "completed", "")
		return
	case "remind":
		timeStr := data.Options[0].StringValue()
		content := data.Options[1].StringValue()
		useAgent := false
		if len(data.Options) > 2 {
			useAgent = data.Options[2].BoolValue()
		}
		if useAgent && !b.requireInitializedInteraction(ds, i, auditCtx, data.Name) {
			b.recordCommandCompleted(auditCtx, data.Name, "slash", "rejected", "channel_uninitialized")
			return
		}
		b.handleRemind(ds, i, auditCtx, timeStr, content, useAgent)
		b.recordCommandCompleted(auditCtx, data.Name, "slash", "completed", "")
		return
	case "cwd":
		b.handleCWDSlash(ds, i, auditCtx)
		b.recordCommandCompleted(auditCtx, data.Name, "slash", "completed", "")
		return
	case "steering":
		b.handleSteeringSlash(ds, i, auditCtx)
		b.recordCommandCompleted(auditCtx, data.Name, "slash", "completed", "")
		return
	}

	if commandRequiresInitializedChannel(data.Name, slashInitGateArgs(data)) && !b.requireInitializedInteraction(ds, i, auditCtx, data.Name) {
		b.recordCommandCompleted(auditCtx, data.Name, "slash", "rejected", "channel_uninitialized")
		return
	}

	// All other commands: acknowledge immediately to avoid 3-second timeout
	argsForVisibility := ""
	if data.Name == "mcp" {
		argsForVisibility = mcpArgsFromSlashOptions(data.Options)
	} else if data.Name == "steering" {
		argsForVisibility = steeringArgsFromSlashOptions(data.Options)
	}
	visibility := commandResponseVisibility(data.Name, argsForVisibility)
	err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: commandInteractionFlags(visibility)},
	})
	visibilityMetadata := commandVisibilityMetadata(visibility)
	b.recordInteractionResponseDelivery(auditCtx, data.Name, "deferred", "", discordgo.InteractionResponseDeferredChannelMessageWithSource, visibilityMetadata, err)
	reply := func(msg string) {
		msg = secrets.RedactEnv(msg)
		sent, err := ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content:         msg,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
			Flags:           commandInteractionFlags(visibility),
		})
		b.recordCommandResponseDelivery(auditCtx, data.Name, "slash", "sent", msg, visibilityMetadata, sent, err)
	}
	replyWithMetadata := func(msg string, metadata map[string]any) {
		msg = secrets.RedactEnv(msg)
		sent, err := ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content:         msg,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
			Flags:           commandInteractionFlags(visibility),
		})
		b.recordCommandResponseDelivery(auditCtx, data.Name, "slash", "sent", msg, mergeMetadata(metadata, visibilityMetadata), sent, err)
	}
	ctx := cmdCtx{channelID: channelID, targetID: rawChannelID, inThread: inThread, reply: reply, replyWithMetadata: replyWithMetadata, guildID: i.GuildID, userID: userID, username: username, interactionID: i.ID}

	go func() {
		defer b.recordCommandCompleted(auditCtx, data.Name, "slash", "completed", "")
		// Extract args from slash command options
		switch data.Name {
		case "start":
			ctx.args = data.Options[0].StringValue()
			b.cmdStart(ctx)
		case "reset":
			b.cmdReset(ctx)
		case "help":
			b.cmdHelp(ctx)
		case "status":
			b.cmdStatus(ctx)
		case "usage":
			if len(data.Options) > 0 {
				if u := data.Options[0].UserValue(ds); u != nil {
					ctx.args = u.ID
				}
			}
			b.cmdUsage(ctx)
		case "doctor":
			b.cmdDoctor(ctx)
		case "audit":
			if len(data.Options) > 0 {
				ctx.args = fmt.Sprintf("%d", data.Options[0].IntValue())
			}
			b.cmdAudit(ctx)
		case "mcp":
			ctx.args = mcpArgsFromSlashOptions(data.Options)
			if strings.TrimSpace(ctx.args) == "manage" {
				b.sendMCPManagePanel(ds, i, ctx)
				return
			}
			b.cmdMCP(ctx)
		case "steering":
			ctx.args = steeringArgsFromSlashOptions(data.Options)
			b.cmdSteering(ctx)
		case "cancel":
			b.cmdCancel(ctx)
		case "interrupt":
			b.cmdInterrupt(ctx)
		case "compact":
			b.cmdCompact(ctx)
		case "clear":
			b.cmdClear(ctx)
		case "cwd":
			if len(data.Options) > 0 {
				ctx.args = data.Options[0].StringValue()
			}
			b.cmdCwd(ctx)
		case "pause":
			b.cmdPause(ctx)
		case "back":
			b.cmdBack(ctx)
		case "silent":
			if len(data.Options) > 0 {
				ctx.args = data.Options[0].StringValue()
			}
			b.cmdSilent(ctx)
		case "thread":
			if len(data.Options) > 0 {
				ctx.args = data.Options[0].StringValue()
			}
			b.cmdThreadMode(ctx)
		case "model":
			if len(data.Options) > 0 {
				ctx.args = data.Options[0].StringValue()
			}
			b.cmdModel(ctx)
		case "models":
			b.cmdModels(ctx)
		case "agent":
			if len(data.Options) > 0 {
				ctx.args = data.Options[0].StringValue()
			}
			b.cmdAgent(ctx)
		case "memory":
			action := data.Options[0].StringValue()
			value := ""
			if len(data.Options) > 1 {
				value = data.Options[1].StringValue()
			}
			ctx.args = action + " " + value
			b.cmdMemory(ctx)
		case "flashmemory":
			action := data.Options[0].StringValue()
			value := ""
			if len(data.Options) > 1 {
				value = data.Options[1].StringValue()
			}
			ctx.args = action + " " + value
			b.cmdFlashMemory(ctx)
		case "close":
			b.cmdClose(ctx)
		case "close-thread":
			ctx.args = data.Options[0].StringValue()
			b.cmdCloseThread(ctx)
		case "resume":
			b.cmdResume(ctx)
		}
	}()
}
