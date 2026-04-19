package bot

import (
	"fmt"
	"strconv"
	"strings"

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
}

// --- Commands with identical channel/thread behavior ---

func (b *Bot) cmdStatus(ctx cmdCtx) {
	ctx.reply(b.statusWithSTT(ctx.channelID))
}

func (b *Bot) cmdPause(ctx cmdCtx) {
	b.manager.Pause(ctx.targetID)
	ctx.reply(L.Get("pause.on"))
}

func (b *Bot) cmdBack(ctx cmdCtx) {
	b.manager.Back(ctx.targetID)
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

func (b *Bot) cmdModels(ctx cmdCtx) {
	msg, err := b.manager.ListModels(ctx.channelID)
	if err != nil {
		ctx.reply(L.Getf("error.generic", err.Error()))
	} else {
		ctx.reply(msg)
	}
}

func (b *Bot) cmdResume(ctx cmdCtx) {
	sess, ok := b.manager.GetSession(ctx.targetID)
	if !ok {
		ctx.reply(L.Get("error.no_active_session"))
		return
	}
	_ = sess
	// TODO: resume from agent's last text — requires storing last response
	ctx.reply(L.Get("error.no_response"))
}

// --- Commands with different channel/thread behavior ---

func (b *Bot) cmdReset(ctx cmdCtx) {
	if ctx.inThread {
		if err := b.manager.ResetThreadAgent(ctx.targetID); err != nil {
			ctx.reply(L.Getf("error.reset_failed", err.Error()))
		} else {
			ctx.reply(L.Get("reset.success"))
		}
		return
	}
	ctx.reply(L.Get("reset.resetting"))
	if err := b.manager.Reset(ctx.channelID); err != nil {
		ctx.reply(L.Getf("error.reset_failed", err.Error()))
	} else {
		ctx.reply(L.Get("reset.success"))
		ctx.reply(usageMessage())
	}
}

func (b *Bot) cmdCancel(ctx cmdCtx) {
	if ctx.inThread {
		if err := b.manager.CancelThreadAgent(ctx.targetID); err != nil {
			ctx.reply(L.Getf("error.cancel_failed", err.Error()))
		} else {
			ctx.reply(L.Get("cancel.success"))
		}
		return
	}
	if err := b.manager.Cancel(ctx.channelID); err != nil {
		ctx.reply(L.Getf("error.cancel_failed", err.Error()))
	} else {
		ctx.reply(L.Get("cancel.success"))
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
		ctx.reply(L.Getf("error.generic", err.Error()))
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
		ctx.reply(L.Getf("error.generic", err.Error()))
	} else {
		if resp == "" {
			resp = L.Get("clear.success")
		}
		ctx.reply("✅ " + resp)
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
			ctx.reply(L.Getf("error.reset_failed", err.Error()))
		} else {
			ctx.reply(L.Getf("model.switched", model))
		}
		return
	}
	if err := b.manager.SetModel(ctx.channelID, model); err != nil {
		ctx.reply(L.Getf("error.generic", err.Error()))
		return
	}
	ctx.reply(L.Getf("model.switching", model))
	if err := b.manager.Restart(ctx.channelID); err != nil {
		ctx.reply(L.Getf("error.reset_failed", err.Error()))
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
	b.manager.StopThreadAgent(ctx.targetID)
	ctx.reply(L.Get("thread_agent.closed"))
}

// --- Channel-only commands ---

func (b *Bot) cmdStart(ctx cmdCtx) {
	cwd := ctx.args
	if cwd == "" {
		ctx.reply(L.Get("start.usage"))
		return
	}
	ctx.reply(L.Getf("start.starting", cwd))
	if err := b.manager.StartAt(ctx.channelID, cwd); err != nil {
		ctx.reply(L.Getf("error.generic", err.Error()))
	} else {
		ctx.reply(L.Getf("start.success", cwd))
		ctx.reply(usageMessage())
	}
}

func (b *Bot) cmdCwd(ctx cmdCtx) {
	if ctx.args == "" {
		ctx.reply(b.manager.CWD(ctx.channelID))
		return
	}
	if err := b.manager.SetCWD(ctx.channelID, ctx.args); err != nil {
		ctx.reply(L.Getf("error.generic", err.Error()))
	} else {
		ctx.reply(L.Getf("cwd.set", ctx.args))
	}
}

// --- Memory commands (unified from handleMemoryCommand + handleMemorySlash) ---

func (b *Bot) cmdMemory(ctx cmdCtx) {
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
			ctx.reply(L.Getf("error.generic", err.Error()))
			return
		}
		ctx.reply(L.Getf("memory.removed", idx))
	case "clear":
		if err := b.manager.MemoryClear(ctx.channelID); err != nil {
			ctx.reply(L.Getf("error.generic", err.Error()))
			return
		}
		ctx.reply(L.Get("memory.cleared"))
	default:
		ctx.reply(L.Get("memory.usage"))
	}
}

func (b *Bot) cmdFlashMemory(ctx cmdCtx) {
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
			ctx.reply(L.Getf("error.generic", err.Error()))
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
