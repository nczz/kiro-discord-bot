package bot

import "github.com/bwmarrin/discordgo"

type commandVisibility int

const (
	commandVisibilityPublic commandVisibility = iota
	commandVisibilityPrivate
)

var guildInteractionContexts = []discordgo.InteractionContextType{discordgo.InteractionContextGuild}

func commandDefaultMemberPermissions(name string) *int64 {
	switch name {
	case "audit", "mcp", "steering", "cwd", "start", "agent", "cron", "cron-list", "cron-run", "cron-prompt", "memory", "flashmemory", "clear":
		perms := int64(discordgo.PermissionManageChannels)
		return &perms
	default:
		return nil
	}
}

func applySlashCommandPolicy(cmd *discordgo.ApplicationCommand) *discordgo.ApplicationCommand {
	if cmd == nil {
		return nil
	}
	cmd.Contexts = &guildInteractionContexts
	cmd.DefaultMemberPermissions = commandDefaultMemberPermissions(cmd.Name)
	return cmd
}

func commandResponseVisibility(name string, args string) commandVisibility {
	switch name {
	case "help", "status", "usage", "doctor", "audit", "mcp", "steering", "cwd", "models", "agent",
		"memory", "flashmemory", "cron-list", "cron-run", "cron-prompt", "remind":
		return commandVisibilityPrivate
	default:
		return commandVisibilityPublic
	}
}

func commandInteractionFlags(visibility commandVisibility) discordgo.MessageFlags {
	if visibility == commandVisibilityPrivate {
		return discordgo.MessageFlagsEphemeral
	}
	return 0
}

func commandVisibilityMetadata(visibility commandVisibility) map[string]any {
	if visibility == commandVisibilityPrivate {
		return map[string]any{"ephemeral": true}
	}
	return nil
}

func mergeMetadata(base map[string]any, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	merged := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}
