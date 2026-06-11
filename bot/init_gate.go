package bot

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func commandRequiresInitializedChannel(name, args string) bool {
	switch name {
	case "start", "steering", "reset", "compact", "clear", "cron", "cron-prompt", "cron-run":
		return true
	case "model", "agent":
		return strings.TrimSpace(args) != ""
	case "memory", "flashmemory":
		action, _ := parseActionValue(args)
		return action != "" && action != "list"
	case "mcp":
		fields := strings.Fields(args)
		if len(fields) == 0 {
			return false
		}
		switch strings.ToLower(fields[0]) {
		case "manage", "enable", "disable", "allow-tool", "deny-tool", "reload":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (b *Bot) requireInitializedCommand(ctx cmdCtx, command string) bool {
	if b.manager.ChannelInitialized(ctx.channelID) {
		return true
	}
	ctx.reply(L.Getf("setup.required.command", command))
	return false
}

func (b *Bot) requireInitializedInteraction(ds *discordgo.Session, i *discordgo.InteractionCreate, auditCtx cmdCtx, command string) bool {
	if b.manager.ChannelInitialized(auditCtx.channelID) {
		return true
	}
	b.respondInteractionForCommand(ds, i, auditCtx, command, L.Getf("setup.required.command", "/"+command), map[string]any{"rejected_reason": "channel_uninitialized"})
	return false
}

func slashInitGateArgs(data discordgo.ApplicationCommandInteractionData) string {
	switch data.Name {
	case "mcp":
		return mcpArgsFromSlashOptions(data.Options)
	case "steering":
		return steeringArgsFromSlashOptions(data.Options)
	case "model", "agent":
		if len(data.Options) > 0 {
			return data.Options[0].StringValue()
		}
	case "memory", "flashmemory":
		action := ""
		value := ""
		for _, opt := range data.Options {
			switch opt.Name {
			case "action":
				action = opt.StringValue()
			case "value":
				value = opt.StringValue()
			}
		}
		return strings.TrimSpace(action + " " + value)
	}
	return ""
}
