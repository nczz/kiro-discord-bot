package bot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/channel"
	L "github.com/nczz/kiro-discord-bot/locale"
)

// cmdCtx holds the execution context for a command, abstracting away
// the differences between bang commands, slash commands, channel, and thread.
type cmdCtx struct {
	channelID string       // parent channel (session/memory key)
	targetID  string       // where the command was issued (channelID or threadID)
	inThread  bool         // true if issued inside a thread
	reply     func(string) // unified reply function
	args      string       // optional arguments after the command name
	guildID   string
	userID    string
	username  string
}

// --- Scope-aware commands ---

func (b *Bot) cmdStatus(ctx cmdCtx) {
	if ctx.inThread {
		ctx.reply(b.manager.ThreadStatus(ctx.targetID))
		return
	}
	ctx.reply(b.statusWithSTT(ctx.channelID))
}

func channelOnly(ctx cmdCtx) bool {
	if !ctx.inThread {
		return false
	}
	ctx.reply(L.Get("error.channel_only"))
	return true
}

func isChannelOnlySlashCommand(name string) bool {
	switch name {
	case "start", "cwd", "agent", "resume", "cron", "cron-list", "cron-run", "cron-prompt", "remind":
		return true
	default:
		return false
	}
}

func (b *Bot) cmdAudit(ctx cmdCtx) {
	if b.auditRecorder == nil {
		ctx.reply(L.Get("audit.disabled"))
		return
	}
	if !b.userCanManageAuditTarget(b.discord, ctx.userID, ctx.targetID) {
		ctx.reply(L.Get("audit.forbidden"))
		return
	}
	limit := 20
	args := strings.Fields(ctx.args)
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[len(args)-1]); err == nil && n > 0 {
			limit = n
		}
	}
	events, err := b.auditRecorder.RecentTimeline(context.Background(), ctx.targetID, limit)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	if len(events) == 0 {
		ctx.reply(L.Get("audit.no_records"))
		return
	}
	ctx.reply(formatAuditTimeline(events))
}

func formatAuditTimeline(events []audit.TimelineEvent) string {
	var sb strings.Builder
	sb.WriteString(L.Get("audit.recent_header"))
	for _, evt := range events {
		label := evt.Type
		if evt.Command != "" {
			label += " /" + evt.Command
		}
		if evt.Status != "" {
			label += " [" + evt.Status + "]"
		}
		user := evt.UserID
		if user != "" {
			user = " <@" + user + ">"
		}
		content := strings.TrimSpace(evt.Content)
		if len([]rune(content)) > 120 {
			content = string([]rune(content)[:120]) + "..."
		}
		sb.WriteString(fmt.Sprintf("\n- `%s` `%s`%s", evt.Kind, label, user))
		if evt.MessageID != "" {
			sb.WriteString(" msg:`" + evt.MessageID + "`")
		}
		if content != "" {
			sb.WriteString(" — " + channel.EscapeDiscordMarkdown(content))
		}
	}
	return sb.String()
}

func (b *Bot) cmdPause(ctx cmdCtx) {
	b.manager.Pause(ctx.targetID)
	if !ctx.inThread {
		b.manager.SetThreadMode(ctx.channelID, false)
	}
	ctx.reply(L.Get("pause.on"))
}

func (b *Bot) cmdBack(ctx cmdCtx) {
	b.manager.Back(ctx.targetID)
	if !ctx.inThread {
		b.manager.SetThreadMode(ctx.channelID, true)
	}
	ctx.reply(L.Get("pause.off"))
}

func (b *Bot) cmdSilent(ctx cmdCtx) {
	switch ctx.args {
	case "on":
		b.manager.SetSilent(ctx.targetID, true)
		ctx.reply(L.Get("silent.on"))
	case "off":
		b.manager.SetSilent(ctx.targetID, false)
		ctx.reply(L.Get("silent.off"))
	default:
		if b.manager.IsSilent(ctx.targetID) {
			ctx.reply(L.Get("silent.status.on"))
		} else {
			ctx.reply(L.Get("silent.status.off"))
		}
	}
}

func (b *Bot) cmdThreadMode(ctx cmdCtx) {
	target := ctx.channelID
	switch ctx.args {
	case "on":
		b.manager.SetThreadMode(target, true)
		ctx.reply(L.Get("thread_mode.on"))
	case "off":
		b.manager.SetThreadMode(target, false)
		ctx.reply(L.Get("thread_mode.off"))
	default:
		if b.manager.ThreadModeEnabled(target) {
			ctx.reply(L.Get("thread_mode.status.on"))
		} else {
			ctx.reply(L.Get("thread_mode.status.off"))
		}
	}
}

func (b *Bot) cmdModels(ctx cmdCtx) {
	msg, err := b.manager.ListModels(ctx.channelID)
	if err != nil {
		ctx.reply(commandError(err))
	} else {
		ctx.reply(msg)
	}
}

func (b *Bot) cmdUsage(ctx cmdCtx) {
	userID := usageUserIDFromArgs(ctx.args, ctx.userID)
	limit := 10
	if userID != "" {
		limit = 0
	}
	report, err := b.manager.UsageReport(ctx.guildID, ctx.channelID, userID, limit)
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	ctx.reply(formatUsageReport(report, userID))
}

func usageUserIDFromArgs(args, selfID string) string {
	args = strings.TrimSpace(args)
	if args == "" || strings.EqualFold(args, "top") || strings.EqualFold(args, "all") {
		return ""
	}
	if strings.EqualFold(args, "me") || strings.EqualFold(args, "self") {
		return selfID
	}
	if strings.HasPrefix(args, "<@") && strings.HasSuffix(args, ">") {
		id := strings.TrimSuffix(strings.TrimPrefix(args, "<@"), ">")
		id = strings.TrimPrefix(id, "!")
		if _, err := strconv.ParseUint(id, 10, 64); err == nil {
			return id
		}
	}
	if _, err := strconv.ParseUint(args, 10, 64); err == nil {
		return args
	}
	return ""
}

func formatUsageReport(report channel.UsageReport, userID string) string {
	var sb strings.Builder
	sb.WriteString(L.Get("usage.report.title") + "\n")
	sb.WriteString(L.Getf("usage.report.through", report.GeneratedAt.Format("2006-01-02 15:04"), report.Location.String()) + "\n")
	sb.WriteString(L.Getf("usage.report.range",
		report.DayStart.Format("01-02 15:04"), report.WeekStart.Format("01-02 15:04"), report.MonthStart.Format("01-02 15:04")))
	if len(report.Rows) == 0 {
		if userID != "" {
			sb.WriteString("\n" + L.Get("usage.report.no_user_records"))
		} else {
			sb.WriteString("\n" + L.Get("usage.report.no_records"))
		}
		return sb.String()
	}
	sb.WriteString("\n")
	for i, row := range report.Rows {
		name := row.Username
		if name == "" {
			name = row.UserID
		}
		if name == "" {
			name = L.Get("usage.report.unknown_user")
		}
		if userID == "" {
			if row.UserID == "" {
				sb.WriteString(fmt.Sprintf("`%d.` %s\n", i+1, name))
			} else {
				sb.WriteString(fmt.Sprintf("`%d.` <@%s> %s\n", i+1, row.UserID, name))
			}
		} else if row.UserID == "" {
			sb.WriteString(name + "\n")
		} else {
			sb.WriteString(fmt.Sprintf("<@%s> %s\n", row.UserID, name))
		}
		sb.WriteString(L.Getf("usage.report.row",
			row.DayCredits, row.DayTurns, row.WeekCredits, row.WeekTurns, row.MonthCredits, row.MonthTurns))
		sb.WriteString("\n")
		if row.MonthTurns > row.MeteredMonthTurns {
			sb.WriteString(L.Getf("usage.report.unmetered", row.MonthTurns-row.MeteredMonthTurns) + "\n")
		}
	}
	return sb.String()
}

func (b *Bot) cmdResume(ctx cmdCtx) {
	if channelOnly(ctx) {
		return
	}
	sess, ok := b.manager.GetSession(ctx.targetID)
	if !ok {
		ctx.reply(L.Get("error.no_active_session"))
		return
	}
	_ = sess
	// TODO: resume from agent's last text — requires storing last response
	ctx.reply(L.Get("error.no_response"))
}

func (b *Bot) cmdDoctor(ctx cmdCtx) {
	runCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	replyLong(ctx.reply, b.doctor(runCtx, ctx.channelID, ctx.targetID))
}

func (b *Bot) doctor(ctx context.Context, channelID, targetID string) string {
	var sb strings.Builder
	sb.WriteString(b.manager.Doctor(ctx))

	sb.WriteString("\n**Discord**\n")
	if b.discord == nil {
		sb.WriteString("❌ session: not initialized\n")
	} else {
		if b.discord.State != nil && b.discord.State.User != nil {
			sb.WriteString("✅ bot user: `" + b.discord.State.User.String() + "`\n")
		} else if u, err := b.discord.User("@me"); err != nil {
			sb.WriteString("❌ bot user: " + err.Error() + "\n")
		} else {
			sb.WriteString("✅ bot user: `" + u.String() + "`\n")
		}

		if b.guildID == "" {
			sb.WriteString("⚠️ guild restriction: not configured\n")
		} else if b.discord.State != nil {
			if g, err := b.discord.State.Guild(b.guildID); err == nil && g != nil {
				sb.WriteString("✅ guild: `" + g.Name + "` (`" + b.guildID + "`)\n")
			} else if g, err := b.discord.Guild(b.guildID); err != nil {
				sb.WriteString("❌ guild: " + err.Error() + "\n")
			} else {
				sb.WriteString("✅ guild: `" + g.Name + "` (`" + b.guildID + "`)\n")
			}
		} else if g, err := b.discord.Guild(b.guildID); err != nil {
			sb.WriteString("❌ guild: " + err.Error() + "\n")
		} else {
			sb.WriteString("✅ guild: `" + g.Name + "` (`" + b.guildID + "`)\n")
		}
	}

	sb.WriteString(b.doctorDiscordPermissions(channelID, targetID))
	sb.WriteString(b.doctorBotPeers(targetID))

	sb.WriteString("\n**Discord MCP**\n")
	sb.WriteString(doctorEnvLine("guild allowlist", "MCP_DISCORD_ALLOWED_GUILDS", "not configured"))
	sb.WriteString(doctorEnvLine("channel allowlist", "MCP_DISCORD_ALLOWED_CHANNELS", "not configured"))
	sb.WriteString(doctorEnvLine("download dir", "MCP_DISCORD_DOWNLOAD_DIR", "not restricted"))
	sb.WriteString(doctorEnvLine("read only", "MCP_DISCORD_READ_ONLY", "false"))
	sb.WriteString(doctorEnvLine("write tools", "MCP_DISCORD_ALLOWED_WRITE_TOOLS", "unrestricted"))
	sb.WriteString(doctorEnvLine("destructive writes", "MCP_DISCORD_ALLOW_DESTRUCTIVE", "true"))

	return sb.String()
}

func (b *Bot) doctorDiscordPermissions(channelID, targetID string) string {
	var sb strings.Builder
	if b.discord == nil || b.discord.State == nil || b.discord.State.User == nil {
		return ""
	}
	selfID := b.discord.State.User.ID
	if targetID == "" {
		targetID = channelID
	}
	if targetID != "" {
		sb.WriteString("\n**" + L.Get("doctor.discord.permissions.title") + "**\n")
		sb.WriteString(b.doctorPermissionSet(L.Get("doctor.discord.permissions.current_target"), selfID, targetID, []permissionCheck{
			{name: L.Get("doctor.discord.permissions.view_channel"), bit: discordgo.PermissionViewChannel},
			{name: L.Get("doctor.discord.permissions.send_messages"), bit: discordgo.PermissionSendMessages},
			{name: L.Get("doctor.discord.permissions.read_history"), bit: discordgo.PermissionReadMessageHistory},
			{name: L.Get("doctor.discord.permissions.send_in_threads"), bit: discordgo.PermissionSendMessagesInThreads},
		}))
	}
	if channelID != "" && channelID != targetID {
		sb.WriteString(b.doctorPermissionSet(L.Get("doctor.discord.permissions.parent_channel"), selfID, channelID, []permissionCheck{
			{name: L.Get("doctor.discord.permissions.view_channel"), bit: discordgo.PermissionViewChannel},
			{name: L.Get("doctor.discord.permissions.send_messages"), bit: discordgo.PermissionSendMessages},
			{name: L.Get("doctor.discord.permissions.read_history"), bit: discordgo.PermissionReadMessageHistory},
			{name: L.Get("doctor.discord.permissions.create_public_threads"), bit: discordgo.PermissionCreatePublicThreads},
		}))
	} else if channelID != "" {
		sb.WriteString(b.doctorPermissionSet(L.Get("doctor.discord.permissions.thread_creation"), selfID, channelID, []permissionCheck{
			{name: L.Get("doctor.discord.permissions.create_public_threads"), bit: discordgo.PermissionCreatePublicThreads},
		}))
	}
	return sb.String()
}

type permissionCheck struct {
	name string
	bit  int64
}

func (b *Bot) doctorPermissionSet(label, userID, channelID string, checks []permissionCheck) string {
	perms, err := b.discord.UserChannelPermissions(userID, channelID)
	if err != nil {
		return L.Getf("doctor.discord.permissions.error", label, channelID, err.Error()) + "\n"
	}
	var missing []string
	for _, check := range checks {
		if perms&check.bit == 0 {
			missing = append(missing, check.name)
		}
	}
	if len(missing) > 0 {
		return L.Getf("doctor.discord.permissions.missing", label, channelID, strings.Join(missing, ", ")) + "\n"
	}
	return L.Getf("doctor.discord.permissions.ok", label, channelID) + "\n"
}

func (b *Bot) doctorBotPeers(targetID string) string {
	var sb strings.Builder
	sb.WriteString("\n**" + L.Get("doctor.bot_peers.title") + "**\n")
	peers := b.peerSnapshot()
	if len(peers) == 0 {
		sb.WriteString(L.Get("doctor.bot_peers.not_configured") + "\n")
		sb.WriteString(L.Get("doctor.bot_peers.channel_mode_open") + "\n")
		return sb.String()
	}
	selfID := ""
	if b.discord != nil && b.discord.State != nil && b.discord.State.User != nil {
		selfID = b.discord.State.User.ID
	}
	for _, p := range peers {
		mention := p.Mention()
		if roleMention := p.RoleMention(); roleMention != "" {
			if mention == "" {
				mention = roleMention
			} else {
				mention += " role " + roleMention
			}
		}
		sb.WriteString(L.Getf("doctor.bot_peers.peer", p.Name, mention, p.ID) + "\n")
	}
	if diag, multiBot := b.channelMultiBotTrigger(b.discord, targetID, selfID); multiBot {
		if b.manager != nil && b.manager.HasFullListenOverride(targetID) {
			sb.WriteString(L.Get("doctor.bot_peers.mode_open_override") + "\n")
			return sb.String()
		}
		peerID := diag.Peer.ID
		if peerID == "" {
			peerID = "role-only"
		}
		sb.WriteString(L.Getf("doctor.bot_peers.trigger", diag.Peer.Name, peerID, diag.Presence) + "\n")
		sb.WriteString(L.Get("doctor.bot_peers.mode_multi_bot_mention") + "\n")
	} else {
		if b.multiBotMode(selfID) {
			sb.WriteString(L.Get("doctor.bot_peers.no_channel_trigger") + "\n")
		}
		sb.WriteString(L.Get("doctor.bot_peers.mode_open") + "\n")
	}
	return sb.String()
}

func doctorEnvLine(label, key, unset string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "⚠️ " + label + ": " + unset + "\n"
	}
	return "✅ " + label + ": `" + value + "`\n"
}

// --- Commands with different channel/thread behavior ---

func (b *Bot) cmdReset(ctx cmdCtx) {
	if ctx.inThread {
		if err := b.manager.ResetThreadAgent(ctx.targetID); err != nil {
			ctx.reply(L.Getf("error.reset_failed", commandErrorString(err)))
		} else {
			ctx.reply(L.Get("reset.success"))
		}
		return
	}
	ctx.reply(L.Get("reset.resetting"))
	if err := b.manager.Reset(ctx.channelID); err != nil {
		ctx.reply(L.Getf("error.reset_failed", commandErrorString(err)))
	} else {
		ctx.reply(L.Get("reset.success"))
		ctx.reply(usageMessage())
	}
}

func (b *Bot) cmdCancel(ctx cmdCtx) {
	if ctx.inThread {
		if err := b.manager.CancelThreadAgent(ctx.targetID); err != nil {
			ctx.reply(L.Getf("error.cancel_failed", commandErrorString(err)))
		} else {
			ctx.reply(L.Get("cancel.success"))
		}
		return
	}
	if err := b.manager.Cancel(ctx.channelID); err != nil {
		ctx.reply(L.Getf("error.cancel_failed", commandErrorString(err)))
	} else {
		ctx.reply(L.Get("cancel.success"))
	}
}

func (b *Bot) cmdInterrupt(ctx cmdCtx) {
	if ctx.inThread {
		if err := b.manager.InterruptThreadAgent(ctx.targetID); err != nil {
			ctx.reply(L.Getf("error.interrupt_failed", commandErrorString(err)))
		} else {
			ctx.reply(L.Get("interrupt.success"))
		}
		return
	}
	if err := b.manager.Interrupt(ctx.channelID); err != nil {
		ctx.reply(L.Getf("error.interrupt_failed", commandErrorString(err)))
	} else {
		ctx.reply(L.Get("interrupt.success"))
	}
}

func (b *Bot) cmdCompact(ctx cmdCtx) {
	var resp string
	var err error
	if ctx.inThread {
		resp, err = b.manager.SendCommandThread(ctx.targetID, "/compact")
	} else {
		resp, err = b.manager.SendCommand(ctx.channelID, "/compact")
	}
	if err != nil {
		ctx.reply(commandError(err))
	} else {
		if resp == "" {
			resp = L.Get("compact.success")
		}
		ctx.reply("✅ " + resp)
	}
}

func (b *Bot) cmdClear(ctx cmdCtx) {
	var resp string
	var err error
	if ctx.inThread {
		resp, err = b.manager.SendCommandThread(ctx.targetID, "/clear")
	} else {
		resp, err = b.manager.SendCommand(ctx.channelID, "/clear")
		if err == nil {
			b.manager.ClearHistory(ctx.channelID)
		}
	}
	if err != nil {
		ctx.reply(commandError(err))
	} else {
		if resp == "" {
			resp = L.Get("clear.success")
		}
		ctx.reply("✅ " + resp)
	}
}

func (b *Bot) cmdAgent(ctx cmdCtx) {
	if channelOnly(ctx) {
		return
	}
	if ctx.args == "" {
		// List available modes
		current, modes := b.manager.AgentModes(ctx.channelID)
		if modes == nil {
			ctx.reply(L.Get("agent.no_session"))
			return
		}
		var sb strings.Builder
		sb.WriteString("**Agent Modes**\n")
		for _, m := range modes {
			marker := " "
			if m.ID == current {
				marker = "▸"
			}
			sb.WriteString(fmt.Sprintf("%s `%s` — %s\n", marker, m.ID, m.Description))
		}
		sb.WriteString("\nUsage: `!agent <mode_id>`")
		ctx.reply(sb.String())
		return
	}
	// Switch mode
	if err := b.manager.SwitchMode(ctx.channelID, ctx.args); err != nil {
		ctx.reply(fmt.Sprintf("❌ Mode switch failed: %s", err.Error()))
	} else {
		ctx.reply(fmt.Sprintf("✅ Switched to mode: `%s`", ctx.args))
	}
}

func (b *Bot) cmdModel(ctx cmdCtx) {
	if ctx.args == "" {
		// Show current model
		if ctx.inThread {
			ctx.reply(b.manager.ThreadModel(ctx.targetID))
		} else {
			ctx.reply(b.manager.Model(ctx.channelID))
		}
		return
	}
	// Switch model
	model := ctx.args
	if ctx.inThread {
		ctx.reply(L.Getf("model.switching", model))
		if err := b.manager.ResetThreadAgentWithModel(ctx.targetID, model); err != nil {
			ctx.reply(L.Getf("error.reset_failed", commandErrorString(err)))
		} else {
			ctx.reply(L.Getf("model.switched", model))
		}
		return
	}
	ctx.reply(L.Getf("model.switching", model))
	restarted, err := b.manager.SwitchModel(ctx.channelID, model)
	if err != nil {
		ctx.reply(L.Getf("error.reset_failed", commandErrorString(err)))
	} else if restarted {
		ctx.reply(L.Getf("model.switched", model))
	} else {
		ctx.reply(L.Getf("model.switched", model))
	}
}

// --- Thread-only commands ---

func (b *Bot) cmdClose(ctx cmdCtx) {
	if !ctx.inThread {
		ctx.reply(L.Get("error.thread_only"))
		return
	}
	if b.manager.StopThreadAgent(ctx.targetID) {
		ctx.reply(L.Get("thread_agent.closed"))
	} else {
		ctx.reply(L.Get("thread_agent.not_running"))
	}
}

func (b *Bot) cmdCloseThread(ctx cmdCtx) {
	threadID := parseThreadIDArg(ctx.args)
	if threadID == "" {
		ctx.reply(L.Get("thread_agent.close_thread_usage"))
		return
	}
	parentID, active, ok := b.manager.ThreadAgentDetails(threadID)
	if !ok {
		ctx.reply(L.Getf("thread_agent.not_running_thread", threadID))
		return
	}
	if parentID != ctx.channelID {
		ctx.reply(L.Get("thread_agent.close_thread_forbidden"))
		return
	}
	if active {
		ctx.reply(L.Get("thread_agent.close_thread_active"))
		return
	}
	if err := b.validateManagedThread(ctx, threadID, parentID); err != nil {
		ctx.reply(commandError(err))
		return
	}
	if b.manager.StopThreadAgent(threadID) {
		ctx.reply(L.Getf("thread_agent.closed_thread", threadID))
	} else {
		ctx.reply(L.Getf("thread_agent.not_running_thread", threadID))
	}
}

func (b *Bot) validateManagedThread(ctx cmdCtx, threadID, parentID string) error {
	ch, err := b.discordChannel(threadID)
	if err != nil {
		return fmt.Errorf("validate thread: %w", err)
	}
	if ch == nil || !ch.IsThread() {
		return fmt.Errorf("target is not a thread")
	}
	if ctx.guildID != "" && ch.GuildID != "" && ch.GuildID != ctx.guildID {
		return fmt.Errorf("thread belongs to another guild")
	}
	if ch.ParentID != parentID {
		return fmt.Errorf("thread belongs to another parent channel")
	}
	return nil
}

func (b *Bot) discordChannel(channelID string) (*discordgo.Channel, error) {
	if b.discord == nil {
		return nil, fmt.Errorf("discord session unavailable")
	}
	if b.discord.State != nil {
		if ch, err := b.discord.State.Channel(channelID); err == nil && ch != nil {
			return ch, nil
		}
	}
	return b.discord.Channel(channelID)
}

func parseThreadIDArg(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "<#")
	s = strings.TrimSuffix(s, ">")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s
}

// --- Channel-only commands ---

func (b *Bot) cmdStart(ctx cmdCtx) {
	if channelOnly(ctx) {
		return
	}
	cwd := ctx.args
	if cwd == "" {
		ctx.reply(L.Get("start.usage"))
		return
	}
	ctx.reply(L.Getf("start.starting", cwd))
	if err := b.manager.StartAt(ctx.channelID, cwd); err != nil {
		ctx.reply(commandError(err))
	} else {
		ctx.reply(L.Getf("start.success", cwd))
		ctx.reply(usageMessage())
	}
}

func (b *Bot) cmdCwd(ctx cmdCtx) {
	if channelOnly(ctx) {
		return
	}
	if ctx.args == "" {
		ctx.reply(b.manager.CWD(ctx.channelID))
		return
	}
	if err := b.manager.SetCWD(ctx.channelID, ctx.args); err != nil {
		ctx.reply(commandError(err))
	} else {
		ctx.reply(L.Getf("cwd.set", ctx.args))
	}
}

// --- Memory commands (unified from handleMemoryCommand + handleMemorySlash) ---

func (b *Bot) cmdMemory(ctx cmdCtx) {
	if ctx.inThread {
		ctx.reply(L.Get("memory.parent_scope"))
	}
	action, value := parseActionValue(ctx.args)
	switch action {
	case "", "list":
		entries := b.manager.MemoryList(ctx.channelID)
		if len(entries) == 0 {
			ctx.reply(L.Get("memory.empty"))
			return
		}
		var sb strings.Builder
		sb.WriteString(L.Get("memory.list_header"))
		for i, e := range entries {
			sb.WriteString(fmt.Sprintf("`%d.` %s\n", i+1, e))
		}
		ctx.reply(sb.String())
	case "add":
		if value == "" {
			ctx.reply(L.Get("memory.usage"))
			return
		}
		if err := b.manager.MemoryAdd(ctx.channelID, value); err != nil {
			ctx.reply(L.Getf("error.save_failed", err.Error()))
			return
		}
		ctx.reply(L.Getf("memory.added", value))
	case "remove":
		idx, err := strconv.Atoi(value)
		if err != nil {
			ctx.reply(L.Get("memory.usage"))
			return
		}
		if err := b.manager.MemoryRemove(ctx.channelID, idx-1); err != nil {
			ctx.reply(commandError(err))
			return
		}
		ctx.reply(L.Getf("memory.removed", idx))
	case "clear":
		if err := b.manager.MemoryClear(ctx.channelID); err != nil {
			ctx.reply(commandError(err))
			return
		}
		ctx.reply(L.Get("memory.cleared"))
	default:
		ctx.reply(L.Get("memory.usage"))
	}
}

func (b *Bot) cmdFlashMemory(ctx cmdCtx) {
	if ctx.inThread {
		ctx.reply(L.Get("flashmemory.parent_scope"))
	}
	action, value := parseActionValue(ctx.args)
	switch action {
	case "", "list":
		entries := b.manager.FlashMemoryList(ctx.channelID)
		if len(entries) == 0 {
			ctx.reply(L.Get("flashmemory.empty"))
			return
		}
		var sb strings.Builder
		sb.WriteString(L.Get("flashmemory.list_header"))
		for i, e := range entries {
			sb.WriteString(fmt.Sprintf("`%d.` %s\n", i+1, e))
		}
		ctx.reply(sb.String())
	case "add":
		if value == "" {
			ctx.reply(L.Get("flashmemory.usage"))
			return
		}
		b.manager.FlashMemoryAdd(ctx.channelID, value)
		ctx.reply(L.Getf("flashmemory.added", value))
	case "remove":
		idx, err := strconv.Atoi(value)
		if err != nil {
			ctx.reply(L.Get("flashmemory.usage"))
			return
		}
		if err := b.manager.FlashMemoryRemove(ctx.channelID, idx-1); err != nil {
			ctx.reply(commandError(err))
			return
		}
		ctx.reply(L.Getf("flashmemory.removed", idx))
	case "clear":
		b.manager.FlashMemoryClear(ctx.channelID)
		ctx.reply(L.Get("flashmemory.cleared"))
	default:
		ctx.reply(L.Get("flashmemory.usage"))
	}
}

// parseActionValue splits "add some text" into ("add", "some text").
// For slash commands, pass "action value" directly.
func parseActionValue(args string) (action, value string) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", ""
	}
	parts := strings.SplitN(args, " ", 2)
	action = parts[0]
	if len(parts) > 1 {
		value = strings.TrimSpace(parts[1])
	}
	return
}
