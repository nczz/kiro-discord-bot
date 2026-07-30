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
	"github.com/nczz/kiro-discord-bot/a2a"
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
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "setup", Description: L.Get("cmd.a2a.sub.setup"), Options: []*discordgo.ApplicationCommandOption{str("peer_agent", L.Get("cmd.a2a.opt.peer_agent"), true), str("skill_id", L.Get("cmd.a2a.opt.skill_id_optional"), false), str("channel_ref", L.Get("cmd.a2a.opt.channel_ref_optional"), false), str("mode", L.Get("cmd.a2a.opt.setup_mode"), false), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "ask", Description: L.Get("cmd.a2a.sub.ask"), Options: []*discordgo.ApplicationCommandOption{str("peer_agent", L.Get("cmd.a2a.opt.peer_agent"), true), str("message", L.Get("cmd.a2a.opt.message"), true), str("skill_id", L.Get("cmd.a2a.opt.skill_id_optional"), false), str("reason", L.Get("cmd.a2a.opt.reason_optional"), false), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
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
			assignA2ATaskOption(&payload, opt.StringValue())
		case "target_agent":
			payload.Request.TargetAgent = opt.StringValue()
		case "peer_agent":
			payload.Request.TargetAgent = opt.StringValue()
			assignA2AAgentOption(&payload, sub.Name, opt.StringValue())
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
	case "setup":
		payload.Request.AcceptFrom = []string{value}
		payload.Request.DelegateTo = []string{value}
		payload.Request.CoPresentFrom = []string{value}
	case "transcript-from":
		payload.Request.CoPresentFrom = []string{value}
	}
}

func applyA2ASubcommandDefaults(payload *a2aSlashPayload) {
	switch payload.Subcommand {
	case "setup":
		enable := true
		payload.Request.Enable = &enable
		if strings.TrimSpace(payload.Request.SkillID) == "" {
			payload.Request.SkillID = "general/task"
		}
		if strings.TrimSpace(payload.Request.ChannelRef) == "" {
			payload.Request.ChannelRef = "discord-" + strings.TrimSpace(payload.Request.ChannelID)
		}
		localSkill := a2a.SkillSlug(payload.Request.SkillID)
		payload.Request.AcceptSkills = []string{localSkill}
		payload.Request.ExposeSkills = []string{localSkill}
		payload.Request.DelegateSkills = []string{payload.Request.SkillID}
		mode := normalizeA2ATranscriptMode(payload.Request.TranscriptMode)
		if mode == "" {
			mode = "delegator"
		}
		payload.Request.TranscriptMode = mode
		if mode == "co_present" {
			share := true
			payload.Request.ShareDiscordContext = &share
		}
	case "ask":
		if strings.TrimSpace(payload.Request.SkillID) == "" {
			payload.Request.SkillID = "general/task"
		}
		if strings.TrimSpace(payload.Request.Reason) == "" {
			payload.Request.Reason = "user_request"
		}
	case "transcript-mode":
		if mode := normalizeA2ATranscriptMode(payload.Request.TranscriptMode); mode != "" {
			payload.Request.TranscriptMode = mode
			share := mode == "co_present"
			payload.Request.ShareDiscordContext = &share
		}
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

func assignA2ATaskOption(payload *a2aSlashPayload, value string) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "local_") {
		payload.Request.LocalID = value
		return
	}
	payload.Request.TaskID = value
}

func normalizeA2ATranscriptMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "", "safe", "proxy", "delegator":
		return "delegator"
	case "mirror":
		return "mirror"
	case "co-present", "copresent", "co_present":
		return "co_present"
	default:
		return strings.TrimSpace(mode)
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
	case "delegate", "ask":
		resp, _ = svc.Delegate(context.Background(), payload.Request)
	case "cancel":
		resp, _ = svc.Cancel(context.Background(), payload.Request)
	case "reply":
		resp, _ = svc.InputReply(context.Background(), payload.Request)
	case "authorize":
		resp, _ = svc.AuthReply(context.Background(), payload.Request)
	case "setup", "enable", "disable", "ref", "expose", "unexpose", "accept-from", "deny-from", "delegate-to", "undelegate-to", "max-concurrent", "transcript-mode", "transcript-from":
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
		return formatA2AConfirmation(resp)
	}
	if !resp.OK {
		return formatA2AError(resp)
	}
	switch {
	case resp.Task != nil:
		return formatA2ATask(*resp.Task, resp.Message)
	case resp.Tasks != nil:
		return formatA2ATaskList(resp.Tasks)
	case resp.Peers != nil:
		return formatA2APeers(resp.Peers, resp.Policy)
	case resp.Policy != nil:
		return formatA2APolicy(resp.Message, *resp.Policy)
	default:
		return a2aBulletTitle(resp.Message)
	}
}

func formatA2AConfirmation(resp botmcp.A2AToolResponse) string {
	var sb strings.Builder
	sb.WriteString(L.Getf("a2a.confirmation_required", resp.ConfirmationSummary, resp.ChangeID, resp.ConfirmationToken))
	if len(resp.RiskLabels) > 0 {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.risk_labels", strings.Join(resp.RiskLabels, ", ")))
	}
	if resp.ExpiresAt != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.expires_at", resp.ExpiresAt))
	}
	sb.WriteString("\n")
	sb.WriteString(L.Get("a2a.confirmation_hint"))
	if resp.Policy != nil {
		sb.WriteString("\n\n")
		sb.WriteString(formatA2APolicy(L.Get("a2a.policy.preview"), *resp.Policy))
	}
	return sb.String()
}

func formatA2AError(resp botmcp.A2AToolResponse) string {
	msg := L.Getf("a2a.error", resp.Message)
	lower := strings.ToLower(resp.Message)
	var hint string
	switch {
	case strings.Contains(lower, "target peer is unknown"):
		hint = L.Get("a2a.remedy.peer_unknown")
	case strings.Contains(lower, "channel a2a policy is disabled"), strings.Contains(lower, "channel_ref is not enabled"):
		hint = L.Get("a2a.remedy.channel_disabled")
	case strings.Contains(lower, "not delegated"), strings.Contains(lower, "skill is not delegated"):
		hint = L.Get("a2a.remedy.not_delegated")
	case strings.Contains(lower, "does not expose skill"), strings.Contains(lower, "unknown_skill"):
		hint = L.Get("a2a.remedy.unknown_skill")
	case strings.Contains(lower, "managechannels"), strings.Contains(lower, "manager required"):
		hint = L.Get("a2a.remedy.manager_required")
	}
	if hint == "" {
		return msg
	}
	return msg + "\n" + hint
}

func formatA2APeers(peers []botmcp.A2APeerSummary, policy *a2a.ChannelA2APolicy) string {
	var sb strings.Builder
	sb.WriteString(L.Get("a2a.peers.title"))
	sb.WriteString("\n")
	if len(peers) == 0 {
		sb.WriteString(L.Get("a2a.peers.empty"))
		sb.WriteString("\n")
		sb.WriteString(L.Get("a2a.peers.empty_hint"))
	} else {
		for _, peer := range peers {
			state := L.Get("a2a.state.offline")
			if peer.Online {
				state = L.Get("a2a.state.online")
			} else if peer.Stale {
				state = L.Get("a2a.state.stale")
			}
			trust := L.Get("a2a.trust.untrusted")
			if peer.Trusted {
				trust = L.Get("a2a.trust.trusted")
			}
			allowed := L.Get("a2a.delegate.not_allowed")
			if peer.DelegationAllowed {
				allowed = L.Get("a2a.delegate.allowed")
			}
			skills := strings.Join(peer.Skills, ", ")
			if skills == "" {
				skills = L.Get("a2a.none")
			}
			sb.WriteString(L.Getf("a2a.peers.row_human", peer.AgentID, peer.Name, state, trust, allowed, skills))
			sb.WriteString("\n")
		}
	}
	if policy != nil {
		sb.WriteString("\n")
		sb.WriteString(formatA2APolicy(L.Get("a2a.policy.current"), *policy))
	}
	return sb.String()
}

func formatA2APolicy(title string, policy a2a.ChannelA2APolicy) string {
	var sb strings.Builder
	sb.WriteString(a2aBulletTitle(title))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.enabled", yesNo(policy.Enabled)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.channel_ref", valueOrNone(policy.ChannelRef)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.mode", valueOrNone(policy.DiscordTranscriptMode)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.result_visibility", valueOrNone(policy.ResultVisibility)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.accept_from", joinOrNone(policy.AcceptFrom)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.accept_skills", joinOrNone(policy.AcceptSkills)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.expose_skills", joinSkillPoliciesOrNone(policy.ExposeSkills)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.delegate_to", joinOrNone(policy.DelegateTo)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.delegate_skills", joinOrNone(policy.DelegateSkills)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.max_concurrent", policy.MaxConcurrent))
	if policy.ShareDiscordContext {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.policy.co_present_from", joinOrNone(policy.CoPresentFrom)))
	}
	return sb.String()
}

func formatA2ATask(task botmcp.A2ATaskSummary, message string) string {
	var sb strings.Builder
	sb.WriteString(a2aBulletTitle(firstNonEmptyA2A(message, L.Get("a2a.task.title"))))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.task.state", task.State))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.task.local_id", valueOrNone(task.LocalID)))
	if task.TaskID != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.task.task_id", task.TaskID))
	}
	if task.ToAgent != "" || task.ExecutorAgent != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.task.route", valueOrNone(task.FromAgent), valueOrNone(firstNonEmptyA2A(task.ExecutorAgent, task.ToAgent))))
	}
	if task.SkillID != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.task.skill", task.SkillID))
	}
	if task.ErrorMessage != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.task.error", task.ErrorMessage))
	}
	sb.WriteString("\n")
	sb.WriteString(L.Get("a2a.task.actions_hint"))
	return sb.String()
}

func formatA2ATaskList(tasks []botmcp.A2ATaskSummary) string {
	var sb strings.Builder
	sb.WriteString(L.Get("a2a.tasks.title"))
	if len(tasks) == 0 {
		sb.WriteString("\n")
		sb.WriteString(L.Get("a2a.tasks.empty"))
		return sb.String()
	}
	for _, task := range tasks {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.tasks.row", valueOrNone(firstNonEmptyA2A(task.LocalID, task.TaskID)), task.State, valueOrNone(firstNonEmptyA2A(task.ExecutorAgent, task.ToAgent)), valueOrNone(task.SkillID)))
	}
	return sb.String()
}

func a2aBulletTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "A2A"
	}
	return "**" + title + "**"
}

func joinOrNone(values []string) string {
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, "`"+value+"`")
		}
	}
	if len(out) == 0 {
		return L.Get("a2a.none")
	}
	return strings.Join(out, ", ")
}

func joinSkillPoliciesOrNone(values []a2a.SkillPolicy) string {
	var out []string
	for _, value := range values {
		skill := strings.TrimSpace(value.ID)
		if skill != "" {
			out = append(out, "`"+skill+"`")
		}
	}
	if len(out) == 0 {
		return L.Get("a2a.none")
	}
	return strings.Join(out, ", ")
}

func valueOrNone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return L.Get("a2a.none")
	}
	return "`" + value + "`"
}

func yesNo(v bool) string {
	if v {
		return L.Get("a2a.yes")
	}
	return L.Get("a2a.no")
}

func firstNonEmptyA2A(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
