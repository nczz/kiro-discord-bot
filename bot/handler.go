package bot

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	"github.com/nczz/kiro-discord-bot/internal/textutil"
	"github.com/nczz/kiro-discord-bot/internal/timectx"
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
	case "resume", "session", "pause", "back", "silent", "thread", "reset", "status", "usage", "doctor", "audit", "mcp", "skill", "steering", "cancel", "interrupt",
		"close-thread", "compact", "clear", "cwd", "start", "agent", "engine", "model", "models", "memory", "flashmemory", "cron", "help":
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

// downloadAttachments saves message attachments under the active project CWD and returns local paths.
func (b *Bot) downloadAttachments(projectCWD, messageID string, attachments []*discordgo.MessageAttachment) []string {
	if len(attachments) == 0 {
		return nil
	}
	attDir := filepath.Join(projectCWD, ".kiro-bot", "attachments", safeAttachmentFilename(messageID))
	if err := os.MkdirAll(attDir, 0755); err != nil {
		log.Printf("[attach] create attachment dir %s: %v", attDir, err)
		return nil
	}

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

type attachmentManifestEntry struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Filename  string `json:"filename"`
	MIME      string `json:"mime,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

func attachmentManifest(attachments []string) string {
	if len(attachments) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[Attached files manifest]\n")
	for i, path := range attachments {
		entry := attachmentManifestEntry{
			ID:       fmt.Sprintf("att-%d", i+1),
			Path:     path,
			Filename: filepath.Base(path),
			MIME:     mime.TypeByExtension(strings.ToLower(filepath.Ext(path))),
		}
		if info, err := os.Stat(path); err == nil {
			entry.SizeBytes = info.Size()
		}
		if strings.HasPrefix(entry.MIME, "image/") {
			if width, height := imageDimensions(path); width > 0 && height > 0 {
				entry.Width = width
				entry.Height = height
			}
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		sb.Write(raw)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

func imageDimensions(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// buildPrompt combines user text with attachment paths into an effective prompt.
func buildPrompt(text string, attachments []string, channelID, guildID, username, peerContext string) string {
	return buildPromptThread(text, attachments, channelID, "", guildID, username, peerContext)
}

func buildPromptThread(text string, attachments []string, channelID, threadID, guildID, username, peerContext string) string {
	return buildPromptThreadWithMentions(text, attachments, channelID, threadID, guildID, username, "", peerContext, nil)
}

func buildPromptThreadWithMentions(text string, attachments []string, channelID, threadID, guildID, username, userID, peerContext string, mentionRefs []discordmention.Ref) string {
	var sb strings.Builder
	sb.WriteString("[Discord bot environment] Your responses are automatically forwarded to a Discord thread. Each message is split at 2000 chars. Tool execution details are also shown.\n")
	sb.WriteString("Do not call bot_send_message for ordinary replies or final answers; write the reply normally and the bot will redact, split, and deliver it. bot_send_message is not default-enabled; if it is available, use it only when the user explicitly asks you to send a separate extra Discord message, notify another target, or hand off to another bot.\n")
	sb.WriteString("Do not inspect or mutate raw bot state files or databases to answer ordinary user requests. Use the exposed bot MCP tools such as bot_a2a_policy_get, bot_a2a_policy_plan, bot_a2a_policy_apply, bot_a2a_peers, bot_a2a_delegate, and bot_a2a_task_status; if the required tool is unavailable, report that the channel MCP policy must be updated instead of editing database files directly.\n")
	sb.WriteString("Do not write raw Discord angle-bracket mention strings or guess Discord IDs. To mention a user, use one of the exact Discord mention reference placeholders listed below; unlisted users cannot be mentioned. If the user asks to tag, mention, notify, or ping a named person who is not listed and discord_resolve_mentions is available, call it once with the extracted names first, then use only the returned placeholders. Do not use discord_list_members or message history to infer mention IDs.\n")
	sb.WriteString("For cron management tools, use channel_id as the owning parent channel ID; use thread_id only for thread-targeted Discord messages.\n")
	sb.WriteString("When users say 本頻道, 這個頻道, 目前頻道, this channel, here, or current session, interpret that as the current Discord target from this context. In a thread, 本討論串/this thread means thread_id; parent channel or whole-channel history means channel_id and includes child threads. If users ask about prior discussion, use bot_query_channel_history when available instead of claiming Discord history is inaccessible. For broad or exhaustive history requests, keep paginating bot_query_channel_history with offset=next_offset until has_more=false before synthesizing the answer.\n")
	sb.WriteString("For one-time delayed reminders, use bot_create_reminder; for recurring schedules, use bot_create_cron.\n")
	sb.WriteString("To change, disable, or resume an existing recurring schedule, first use bot_list_cron, then bot_update_cron with only the requested fields. Use enabled=false to disable without deleting; deletion requires bot_delete_cron.\n")
	sb.WriteString("For A2A delegation status, progress, or task outcome questions, call bot_a2a_task_status and treat its TaskStore state as authoritative. Do not answer A2A progress from audit timeline rows alone; audit events show history and can lag or omit terminal task state. If bot_a2a_task_status says a transparent/co_present result is omitted because the executor owns the shared Discord transcript, do not post a follow-up, summary, or paraphrase of that result unless the user explicitly asks.\n")
	sb.WriteString("For persistent channel memory, use bot_memory_list to inspect existing rules. Use bot_memory_add only when the user explicitly asks the bot to remember a Discord-channel preference or behavior rule for future turns; summarize it as one durable behavior rule, reject secrets or policy-bypass instructions, and include requester/reason audit fields. bot_memory_add is not a knowledge base, project knowledge store, document corpus, or searchable index: do not use it for requests phrased as 知識庫, knowledge base, KB, 文件索引, project knowledge, update docs, or add to corpus. If the user asks to update a knowledge base and no knowledge-base-specific write tool is available, say that this bot cannot write that knowledge base from Discord and ask for the target KB/source/update workflow. Do not store ordinary conversation as memory. Use bot_memory_remove or bot_memory_clear only when explicitly requested and available.\n")
	sb.WriteString("Attached Discord files are listed as a manifest with local paths and metadata. Do not infer image contents from filenames or metadata alone; inspect the relevant path before visual claims. For bulk image operations, process file paths in batches instead of asking for all images as prompt content.\n")
	sb.WriteString("For time/date/weekday questions, use the [Current datetime] block below for current facts. For calculated date ranges, relative periods, month/week boundaries, specific weekdays, nth week of a month, or schedule-sensitive answers, translate the user's date phrase into structured bot_resolve_date_range fields and call that tool when available. Examples: 明天 => range_type=day offset=1; 下個月第二週 => range_type=month_week offset=1 week_index=2; 過去7天 => range_type=relative_days days=7 direction=past. The structured fields are language-neutral; do not pass natural-language date text to the MCP tool. Do not calculate weekdays, month boundaries, or relative ranges from model memory when bot time tools are available. State the timezone used for user-visible date/time answers.\n")
	sb.WriteString(timectx.PromptBlock(time.Now(), os.Getenv("CRON_TIMEZONE")))
	userIDPart := ""
	if strings.TrimSpace(userID) != "" {
		userIDPart = fmt.Sprintf(" user_id=%s", strings.TrimSpace(userID))
	}
	if threadID != "" {
		sb.WriteString(fmt.Sprintf("[Discord context] channel_id=%s thread_id=%s guild_id=%s user=%s%s\n\n", channelID, threadID, guildID, username, userIDPart))
	} else {
		sb.WriteString(fmt.Sprintf("[Discord context] channel_id=%s guild_id=%s user=%s%s\n\n", channelID, guildID, username, userIDPart))
	}
	if block := discordmention.PromptBlock(mentionRefs); block != "" {
		sb.WriteString(block)
		sb.WriteString("\n")
	}
	if peerContext != "" {
		sb.WriteString(peerContext)
		sb.WriteString("\n")
	}
	if manifest := attachmentManifest(attachments); manifest != "" {
		sb.WriteString(manifest)
	}
	if text != "" {
		sb.WriteString(text)
	} else if len(attachments) > 0 {
		sb.WriteString("Please review the attached file(s).")
	}
	return sb.String()
}

func mentionRefsForMessage(m *discordgo.MessageCreate, selfID string) []discordmention.Ref {
	if m == nil || m.Message == nil {
		return nil
	}
	seen := make(map[string]bool)
	var refs []discordmention.Ref
	addUser := func(u *discordgo.User) {
		if u == nil || strings.TrimSpace(u.ID) == "" || u.ID == selfID || seen[u.ID] {
			return
		}
		seen[u.ID] = true
		refs = append(refs, discordmention.UserRef(u.ID, u.Username))
	}
	addUser(m.Author)
	for _, u := range m.Mentions {
		addUser(u)
	}
	return refs
}

func appendMentionRefs(base []discordmention.Ref, extra ...discordmention.Ref) []discordmention.Ref {
	seen := make(map[string]bool)
	out := make([]discordmention.Ref, 0, len(base)+len(extra))
	add := func(ref discordmention.Ref) {
		key := ref.Kind + ":" + ref.ID
		if ref.ID == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, ref)
	}
	for _, ref := range base {
		add(ref)
	}
	for _, ref := range extra {
		add(ref)
	}
	return out
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
		for _, payload := range splitOversizedReply(secrets.RedactEnv(msg), nil) {
			sent, err := sendDiscordText(ds, m.ChannelID, payload.content, &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
			if isKnownCommand {
				b.recordCommandResponseDelivery(auditCtx, bangCommand, "message", "sent", payload.content, payload.metadata, sent, err)
			}
		}
	}
	replyWithMetadata := func(msg string, metadata map[string]any) {
		for _, payload := range splitOversizedReply(secrets.RedactEnv(msg), metadata) {
			sent, err := sendDiscordText(ds, m.ChannelID, payload.content, &discordgo.MessageReference{MessageID: m.ID, ChannelID: m.ChannelID})
			if isKnownCommand {
				b.recordCommandResponseDelivery(auditCtx, bangCommand, "message", "sent", payload.content, payload.metadata, sent, err)
			}
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
	case content == "!session" || strings.HasPrefix(content, "!session "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!session"))
		b.cmdSession(ctx)
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
		ctx.reply(L.Get("usage.slash_only"))
	case content == "!doctor":
		b.cmdDoctor(ctx)
	case content == "!audit" || strings.HasPrefix(content, "!audit "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!audit"))
		b.cmdAudit(ctx)
	case content == "!mcp" || strings.HasPrefix(content, "!mcp "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!mcp"))
		b.cmdMCP(ctx)
	case content == "!skill" || strings.HasPrefix(content, "!skill "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!skill"))
		b.cmdSkill(ctx)
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
	case content == "!engine":
		b.cmdEngine(ctx)
	case strings.HasPrefix(content, "!engine "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!engine "))
		b.cmdEngine(ctx)
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

		projectCWD := b.manager.CWDPath(m.ChannelID)
		localPaths := b.downloadAttachments(projectCWD, m.ID, m.Attachments)
		b.warnIfAttachmentsLarge(ds, m.ChannelID, localPaths)

		// Transcribe audio files (voice messages + audio attachments)
		var transcript string
		if t, rest := b.transcribeAudioFiles(localPaths, m.Attachments); t != "" {
			transcript = t
			content = L.Get("stt.prefix") + t + "\n" + content
			localPaths = rest
		}

		mentionRefs := appendMentionRefs(mentionRefsForMessage(m, selfID), b.peerMentionRefs(selfID)...)
		prompt := buildPromptThreadWithMentions(content, localPaths, m.ChannelID, "", m.GuildID, m.Author.Username, m.Author.ID, b.peerPromptContext(selfID), mentionRefs)
		deliveryMode := channel.DeliveryThread
		if !b.manager.ThreadModeEnabled(m.ChannelID) {
			deliveryMode = channel.DeliveryInline
		}

		job := &channel.Job{
			ChannelID:    m.ChannelID,
			GuildID:      m.GuildID,
			MessageID:    m.ID,
			Prompt:       prompt,
			UserID:       m.Author.ID,
			Username:     m.Author.Username,
			Attachments:  localPaths,
			Transcript:   transcript,
			Source:       "message",
			DeliveryMode: deliveryMode,
			MentionRefs:  mentionRefs,
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
		for _, payload := range splitOversizedReply(secrets.RedactEnv(msg), nil) {
			sent, err := sendDiscordText(ds, threadID, payload.content, nil)
			if isKnownCommand {
				b.recordCommandResponseDelivery(auditCtx, bangCommand, "thread_message", "sent", payload.content, payload.metadata, sent, err)
			}
		}
	}
	replyWithMetadata := func(msg string, metadata map[string]any) {
		for _, payload := range splitOversizedReply(secrets.RedactEnv(msg), metadata) {
			sent, err := sendDiscordText(ds, threadID, payload.content, nil)
			if isKnownCommand {
				b.recordCommandResponseDelivery(auditCtx, bangCommand, "thread_message", "sent", payload.content, payload.metadata, sent, err)
			}
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
		ctx.reply(L.Get("usage.slash_only"))
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
	case content == "!engine" || strings.HasPrefix(content, "!engine "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!engine"))
		b.cmdEngine(ctx)
		return
	case content == "!resume":
		b.cmdResume(ctx)
		return
	case content == "!session" || strings.HasPrefix(content, "!session "):
		ctx.args = strings.TrimSpace(strings.TrimPrefix(content, "!session"))
		b.cmdSession(ctx)
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

	localPaths := b.downloadAttachments(b.manager.TargetCWDPath(threadID, parentChannelID), m.ID, m.Attachments)
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
	mentionRefs := appendMentionRefs(mentionRefsForMessage(m, selfID), b.peerMentionRefs(selfID)...)
	prompt := buildPromptThreadWithMentions(content, localPaths, parentChannelID, threadID, m.GuildID, m.Author.Username, m.Author.ID, b.peerPromptContext(selfID), mentionRefs)

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
		MentionRefs:     mentionRefs,
	}
	if err := b.manager.EnqueueThread(ds, job, parentChannelID); err != nil {
		ds.MessageReactionRemove(threadID, m.ID, "⏳", "@me")
		_, _ = sendDiscordText(ds, threadID, commandError(err), nil)
	}
}

func buildSlashCommands() []*discordgo.ApplicationCommand {
	return buildSlashCommandsWithA2A(true)
}

func buildSlashCommandsWithA2A(a2aEnabled bool) []*discordgo.ApplicationCommand {
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
		{Name: "usage-history", Description: L.Get("cmd.usage_history.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: L.Get("cmd.usage_history.opt.user"), Required: false},
			{Type: discordgo.ApplicationCommandOptionString, Name: "period", Description: L.Get("cmd.usage_history.opt.period"), Required: false, Choices: []*discordgo.ApplicationCommandOptionChoice{{Name: "7d", Value: "7d"}, {Name: "30d", Value: "30d"}, {Name: "this-month", Value: "this-month"}, {Name: "last-month", Value: "last-month"}}},
			{Type: discordgo.ApplicationCommandOptionString, Name: "status", Description: L.Get("cmd.usage_history.opt.status"), Required: false, Choices: []*discordgo.ApplicationCommandOptionChoice{{Name: "all", Value: "all"}, {Name: "success", Value: "success"}, {Name: "failed", Value: "error"}}},
			{Type: discordgo.ApplicationCommandOptionString, Name: "source", Description: L.Get("cmd.usage_history.opt.source"), Required: false, Choices: []*discordgo.ApplicationCommandOptionChoice{{Name: "all", Value: "all"}, {Name: "message", Value: "message"}, {Name: "command", Value: "command"}, {Name: "cron", Value: "cron"}}},
		}},
		{Name: "doctor", Description: L.Get("cmd.doctor.desc")},
		{Name: "audit", Description: L.Get("cmd.audit.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "limit", Description: L.Get("cmd.audit.opt.limit"), Required: false},
			{Type: discordgo.ApplicationCommandOptionString, Name: "prompt", Description: L.Get("cmd.audit.opt.prompt"), Required: false},
		}},
		{Name: "mcp", Description: L.Get("cmd.mcp.desc"), Options: mcpSlashOptions()},
		{Name: "skill", Description: L.Get("cmd.skill.desc"), Options: skillSlashOptions()},
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
		{Name: "engine", Description: L.Get("cmd.engine.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "engine", Description: L.Get("cmd.engine.opt.engine"), Required: false, Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "kiro", Value: "kiro"},
				{Name: "omp", Value: "omp"},
			}},
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
		{Name: "session", Description: L.Get("cmd.session.desc"), Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "list", Description: L.Get("cmd.session.list.desc")},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "resume", Description: L.Get("cmd.session.resume.desc")},
		}},
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
	if a2aEnabled {
		commands = append(commands, &discordgo.ApplicationCommand{Name: "a2a", Description: L.Get("cmd.a2a.desc"), Options: a2aSlashOptions()})
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
	created, err := b.discord.ApplicationCommandBulkOverwrite(b.discord.State.User.ID, guildID, buildSlashCommandsWithA2A(b.a2aConfig.Enabled()))
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
		} else if strings.HasPrefix(customID, usageHistoryCustomPrefix+":") {
			b.handleUsageHistoryComponent(ds, i)
		} else if strings.HasPrefix(customID, a2aComponentPrefix+":") {
			b.handleA2AComponent(ds, i)
		} else if strings.HasPrefix(customID, skillComponentPrefix+":") {
			b.handleSkillComponent(ds, i)
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
	case "usage-history":
		status, errText := b.handleUsageHistory(ds, i, auditCtx)
		b.recordCommandCompleted(auditCtx, data.Name, "slash", status, errText)
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
	responder := newDeferredSlashResponder(ds, i.Interaction, commandInteractionFlags(visibility))
	reply := func(msg string) {
		for _, payload := range splitOversizedReply(secrets.RedactEnv(msg), visibilityMetadata) {
			sent, err := responder.Send(payload.content)
			b.recordCommandResponseDelivery(auditCtx, data.Name, "slash", "sent", payload.content, payload.metadata, sent, err)
		}
	}
	replyWithMetadata := func(msg string, metadata map[string]any) {
		metadata = mergeMetadata(metadata, visibilityMetadata)
		for _, payload := range splitOversizedReply(secrets.RedactEnv(msg), metadata) {
			sent, err := responder.Send(payload.content)
			b.recordCommandResponseDelivery(auditCtx, data.Name, "slash", "sent", payload.content, payload.metadata, sent, err)
		}
	}
	replyWithComponents := func(msg string, components []discordgo.MessageComponent, metadata map[string]any) {
		metadata = mergeMetadata(metadata, visibilityMetadata)
		payloads := splitOversizedReply(secrets.RedactEnv(msg), metadata)
		for idx, payload := range payloads {
			sendComponents := []discordgo.MessageComponent(nil)
			if idx == 0 {
				sendComponents = components
			}
			sent, err := responder.SendWithComponents(payload.content, sendComponents)
			b.recordCommandResponseDelivery(auditCtx, data.Name, "slash", "sent", payload.content, payload.metadata, sent, err)
		}
	}
	ctx := cmdCtx{channelID: channelID, targetID: rawChannelID, inThread: inThread, reply: reply, replyWithMetadata: replyWithMetadata, replyWithComponents: replyWithComponents, guildID: i.GuildID, userID: userID, username: username, interactionID: i.ID}

	go func() {
		completionStatus := "completed"
		completionError := ""
		defer func() {
			b.recordCommandCompleted(auditCtx, data.Name, "slash", completionStatus, completionError)
		}()
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
			requestedUserID := ""
			if len(data.Options) > 0 {
				if u := data.Options[0].UserValue(ds); u != nil {
					requestedUserID = u.ID
				}
			}
			args, ok := b.usageReportArgsForRequester(ds, userID, rawChannelID, requestedUserID)
			if !ok {
				completionStatus = "rejected"
				completionError = "usage_report_forbidden"
				ctx.reply(L.Get("usage.report.forbidden"))
				return
			}
			ctx.args = args
			b.cmdUsage(ctx)
		case "doctor":
			b.cmdDoctor(ctx)
		case "audit":
			for _, opt := range data.Options {
				switch opt.Name {
				case "limit":
					ctx.args = fmt.Sprintf("%d", opt.IntValue())
				case "prompt":
					ctx.args = opt.StringValue()
				}
			}
			b.cmdAudit(ctx)
		case "mcp":
			ctx.args = mcpArgsFromSlashOptions(data.Options)
			if strings.TrimSpace(ctx.args) == "manage" {
				b.sendMCPManagePanel(ds, i, ctx)
				return
			}
			b.cmdMCP(ctx)
		case "skill":
			b.handleSkillSlash(data.Options, ctx)
		case "a2a":
			if !b.a2aConfig.Enabled() {
				ctx.reply(L.Get("a2a.disabled"))
				return
			}
			ctx.args = a2aArgsFromSlashOptions(data.Options, i.GuildID, rawChannelID, userID, username, b.userCanManageAuditTarget(ds, userID, rawChannelID))
			b.cmdA2A(ctx)
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
		case "engine":
			if len(data.Options) > 0 {
				ctx.args = data.Options[0].StringValue()
			}
			b.cmdEngine(ctx)
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
		case "session":
			if len(data.Options) > 0 {
				sub := data.Options[0]
				ctx.args = sub.Name
			}
			b.cmdSession(ctx)
		}
	}()
}
