package bot

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/internal/botmcp"
	L "github.com/nczz/kiro-discord-bot/locale"
)

type a2aSlashPayload struct {
	Subcommand string                `json:"subcommand"`
	Request    botmcp.A2AToolRequest `json:"request"`
}

func a2aSlashOptions() []*discordgo.ApplicationCommandOption {
	str := func(name, desc string, required bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: name, Description: desc, Required: required}
	}
	integer := func(name, desc string, required bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionInteger, Name: name, Description: desc, Required: required}
	}
	boolean := func(name, desc string, required bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionBoolean, Name: name, Description: desc, Required: required}
	}
	return []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "peers", Description: L.Get("cmd.a2a.sub.peers")},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "status", Description: L.Get("cmd.a2a.sub.status"), Options: []*discordgo.ApplicationCommandOption{str("task", L.Get("cmd.a2a.opt.task"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "delegate", Description: L.Get("cmd.a2a.sub.delegate"), Options: []*discordgo.ApplicationCommandOption{str("target_agent", L.Get("cmd.a2a.opt.target_agent"), true), str("skill_id", L.Get("cmd.a2a.opt.skill_id"), true), str("message", L.Get("cmd.a2a.opt.message"), true), str("reason", L.Get("cmd.a2a.opt.reason"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "cancel", Description: L.Get("cmd.a2a.sub.cancel"), Options: []*discordgo.ApplicationCommandOption{str("task", L.Get("cmd.a2a.opt.task"), true), str("reason", L.Get("cmd.a2a.opt.reason"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "reply", Description: L.Get("cmd.a2a.sub.reply"), Options: []*discordgo.ApplicationCommandOption{str("task", L.Get("cmd.a2a.opt.task"), true), str("input", L.Get("cmd.a2a.opt.input"), true)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "authorize", Description: L.Get("cmd.a2a.sub.authorize"), Options: []*discordgo.ApplicationCommandOption{str("task", L.Get("cmd.a2a.opt.task"), true), boolean("approve", L.Get("cmd.a2a.opt.approve"), true), str("deny_reason", L.Get("cmd.a2a.opt.deny_reason"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "enable", Description: L.Get("cmd.a2a.sub.enable"), Options: []*discordgo.ApplicationCommandOption{str("channel_ref", L.Get("cmd.a2a.opt.channel_ref"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "disable", Description: L.Get("cmd.a2a.sub.disable"), Options: []*discordgo.ApplicationCommandOption{str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "ref", Description: L.Get("cmd.a2a.sub.ref"), Options: []*discordgo.ApplicationCommandOption{str("channel_ref", L.Get("cmd.a2a.opt.channel_ref"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "expose", Description: L.Get("cmd.a2a.sub.expose"), Options: []*discordgo.ApplicationCommandOption{str("skill_id", L.Get("cmd.a2a.opt.skill_id"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "unexpose", Description: L.Get("cmd.a2a.sub.unexpose"), Options: []*discordgo.ApplicationCommandOption{str("skill_id", L.Get("cmd.a2a.opt.skill_id"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "accept-from", Description: L.Get("cmd.a2a.sub.accept_from"), Options: []*discordgo.ApplicationCommandOption{str("agent_id", L.Get("cmd.a2a.opt.agent_id"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "deny-from", Description: L.Get("cmd.a2a.sub.deny_from"), Options: []*discordgo.ApplicationCommandOption{str("agent_id", L.Get("cmd.a2a.opt.agent_id"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "delegate-to", Description: L.Get("cmd.a2a.sub.delegate_to"), Options: []*discordgo.ApplicationCommandOption{str("agent_id", L.Get("cmd.a2a.opt.agent_id"), true), str("skill_id", L.Get("cmd.a2a.opt.skill_id"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "undelegate-to", Description: L.Get("cmd.a2a.sub.undelegate_to"), Options: []*discordgo.ApplicationCommandOption{str("agent_id", L.Get("cmd.a2a.opt.agent_id"), true), str("skill_id", L.Get("cmd.a2a.opt.skill_id"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "max-concurrent", Description: L.Get("cmd.a2a.sub.max_concurrent"), Options: []*discordgo.ApplicationCommandOption{integer("value", L.Get("cmd.a2a.opt.value"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "transcript-mode", Description: L.Get("cmd.a2a.sub.transcript_mode"), Options: []*discordgo.ApplicationCommandOption{str("mode", L.Get("cmd.a2a.opt.mode"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "transcript-from", Description: L.Get("cmd.a2a.sub.transcript_from"), Options: []*discordgo.ApplicationCommandOption{str("agent_id", L.Get("cmd.a2a.opt.agent_id"), true), boolean("share", L.Get("cmd.a2a.opt.share"), false), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
	}
}

func a2aArgsFromSlashOptions(options []*discordgo.ApplicationCommandInteractionDataOption, guildID, channelID, userID, username string, manage bool) string {
	payload := a2aSlashPayload{Request: botmcp.A2AToolRequest{GuildID: guildID, ChannelID: channelID, RequestedBy: username, RequestedByID: userID, ManageChannels: manage}}
	if len(options) == 0 {
		payload.Subcommand = "peers"
		raw, _ := json.Marshal(payload)
		return string(raw)
	}
	sub := options[0]
	payload.Subcommand = sub.Name
	for _, opt := range sub.Options {
		switch opt.Name {
		case "task":
			payload.Request.TaskID = strings.TrimSpace(opt.StringValue())
		case "target_agent":
			payload.Request.TargetAgent = opt.StringValue()
		case "skill_id":
			payload.Request.SkillID = opt.StringValue()
		case "message":
			payload.Request.Message = opt.StringValue()
		case "reason":
			payload.Request.Reason = opt.StringValue()
		case "input":
			payload.Request.Input = opt.StringValue()
		case "approve":
			payload.Request.Approve = opt.BoolValue()
		case "deny_reason":
			payload.Request.DenyReason = opt.StringValue()
		case "channel_ref":
			payload.Request.ChannelRef = opt.StringValue()
		case "agent_id":
			assignA2AAgentOption(&payload, sub.Name, opt.StringValue())
		case "value":
			v := int(opt.IntValue())
			payload.Request.MaxConcurrent = &v
		case "mode":
			payload.Request.TranscriptMode = opt.StringValue()
		case "share":
			v := opt.BoolValue()
			payload.Request.ShareDiscordContext = &v
		case "confirmation_token":
			payload.Request.ConfirmationToken = opt.StringValue()
		}
	}
	applyA2ASubcommandDefaults(&payload)
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func assignA2AAgentOption(payload *a2aSlashPayload, subcommand string, value string) {
	switch subcommand {
	case "accept-from", "deny-from":
		payload.Request.AcceptFrom = []string{value}
	case "delegate-to", "undelegate-to":
		payload.Request.DelegateTo = []string{value}
	case "transcript-from":
		payload.Request.CoPresentFrom = []string{value}
	}
}

func applyA2ASubcommandDefaults(payload *a2aSlashPayload) {
	switch payload.Subcommand {
	case "enable":
		v := true
		payload.Request.Enable = &v
	case "disable":
		v := false
		payload.Request.Enable = &v
	case "expose", "unexpose":
		payload.Request.ExposeSkills = []string{payload.Request.SkillID}
	case "delegate-to", "undelegate-to":
		payload.Request.DelegateSkills = []string{payload.Request.SkillID}
	}
}

func (b *Bot) cmdA2A(ctx cmdCtx) {
	if channelOnly(ctx) {
		return
	}
	var payload a2aSlashPayload
	if err := json.Unmarshal([]byte(ctx.args), &payload); err != nil {
		ctx.reply(commandError(err))
		return
	}
	svc, err := botmcp.NewA2AService(botmcp.A2AServiceConfig{DataDir: b.dataDir, Config: botmcp.A2AConfigFromEnv(), Node: b.a2aNode, BoundGuildID: ctx.guildID, BoundChannelID: ctx.channelID, BoundTargetID: ctx.targetID, AuditEnabled: true, AuditRecordContent: true, ConnectNATS: false})
	if err != nil {
		ctx.reply(commandError(err))
		return
	}
	defer svc.Close()
	var resp botmcp.A2AToolResponse
	switch payload.Subcommand {
	case "peers":
		resp, _ = svc.Peers(context.Background(), payload.Request)
	case "status":
		resp, _ = svc.TaskStatus(context.Background(), payload.Request)
	case "delegate":
		resp, _ = svc.Delegate(context.Background(), payload.Request)
	case "cancel":
		resp, _ = svc.Cancel(context.Background(), payload.Request)
	case "reply":
		resp, _ = svc.InputReply(context.Background(), payload.Request)
	case "authorize":
		resp, _ = svc.AuthReply(context.Background(), payload.Request)
	case "enable", "disable", "ref", "expose", "unexpose", "accept-from", "deny-from", "delegate-to", "undelegate-to", "max-concurrent", "transcript-mode", "transcript-from":
		if strings.TrimSpace(payload.Request.ConfirmationToken) == "" {
			resp, _ = svc.PolicyPlan(context.Background(), payload.Request)
		} else {
			resp, _ = svc.PolicyApply(context.Background(), payload.Request)
		}
	default:
		resp = botmcp.A2AToolResponse{OK: false, Message: fmt.Sprintf("unknown A2A subcommand %q", payload.Subcommand)}
	}
	ctx.reply(formatA2AResponse(resp))
}

func a2aConfirmationButtonCustomID(action, channelID, changeID string) string {
	mac := hmac.New(sha256.New, []byte(a2aComponentSecret()))
	mac.Write([]byte(action))
	mac.Write([]byte{0})
	mac.Write([]byte(channelID))
	mac.Write([]byte{0})
	mac.Write([]byte(changeID))
	sig := hex.EncodeToString(mac.Sum(nil))[:24]
	return "a2a:confirm:" + action + ":" + sig
}

func validateA2AConfirmationButtonCustomID(customID, action, channelID, changeID string) bool {
	want := a2aConfirmationButtonCustomID(action, channelID, changeID)
	return hmac.Equal([]byte(customID), []byte(want))
}

func a2aComponentSecret() string {
	if v := strings.TrimSpace(os.Getenv("A2A_CONFIRMATION_SECRET")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("DISCORD_TOKEN")); v != "" {
		return v
	}
	return "kiro-a2a-dev-component-secret"
}

func formatA2AResponse(resp botmcp.A2AToolResponse) string {
	if resp.RequiresConfirmation {
		return L.Getf("a2a.confirmation_required", resp.ConfirmationSummary, resp.ChangeID, resp.ConfirmationToken)
	}
	if !resp.OK {
		return L.Getf("a2a.error", resp.Message)
	}
	if raw, err := json.MarshalIndent(resp, "", "  "); err == nil {
		return "```json\n" + string(raw) + "\n```"
	}
	return resp.Message
}
