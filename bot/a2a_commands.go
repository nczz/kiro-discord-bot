package bot

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/botmcp"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	L "github.com/nczz/kiro-discord-bot/locale"
)

type a2aSlashPayload struct {
	Subcommand string                `json:"subcommand"`
	Request    botmcp.A2AToolRequest `json:"request"`
}

const (
	a2aComponentPrefix        = "a2a"
	a2aPolicyComponentSection = "policy"
	a2aPolicyConfirmTTL       = 10 * time.Minute
)

type a2aPolicyConfirmationEntry struct {
	Payload   a2aSlashPayload
	ExpiresAt time.Time
}

type a2aPolicyConfirmationStore struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]a2aPolicyConfirmationEntry
}

func newA2APolicyConfirmationStore(now func() time.Time) *a2aPolicyConfirmationStore {
	if now == nil {
		now = time.Now
	}
	return &a2aPolicyConfirmationStore{now: now, entries: make(map[string]a2aPolicyConfirmationEntry)}
}

func (s *a2aPolicyConfirmationStore) Put(payload a2aSlashPayload, resp botmcp.A2AToolResponse) string {
	if s == nil {
		return ""
	}
	payload.Request.ChangeID = resp.ChangeID
	payload.Request.ConfirmationToken = resp.ConfirmationToken
	expires := s.now().Add(a2aPolicyConfirmTTL)
	if resp.ExpiresAt != "" {
		if parsed, err := time.Parse(time.RFC3339, resp.ExpiresAt); err == nil {
			expires = parsed
		}
	}
	id := randomA2AConfirmationID()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, entry := range s.entries {
		if !entry.ExpiresAt.After(now) {
			delete(s.entries, key)
		}
	}
	s.entries[id] = a2aPolicyConfirmationEntry{Payload: payload, ExpiresAt: expires}
	return id
}

func (s *a2aPolicyConfirmationStore) Take(id string) (a2aPolicyConfirmationEntry, bool) {
	if s == nil {
		return a2aPolicyConfirmationEntry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok {
		return a2aPolicyConfirmationEntry{}, false
	}
	if !entry.ExpiresAt.After(s.now()) {
		delete(s.entries, id)
		return a2aPolicyConfirmationEntry{}, false
	}
	delete(s.entries, id)
	return entry, true
}

func (s *a2aPolicyConfirmationStore) Get(id string) (a2aPolicyConfirmationEntry, bool) {
	if s == nil {
		return a2aPolicyConfirmationEntry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok || !entry.ExpiresAt.After(s.now()) {
		if ok {
			delete(s.entries, id)
		}
		return a2aPolicyConfirmationEntry{}, false
	}
	return entry, true
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
	target := func() *discordgo.ApplicationCommandOption {
		return str("target_channel_ref", L.Get("cmd.a2a.opt.target_channel_ref_optional"), false)
	}
	return []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "peers", Description: L.Get("cmd.a2a.sub.peers")},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "setup", Description: L.Get("cmd.a2a.sub.setup"), Options: []*discordgo.ApplicationCommandOption{str("peer_agent", L.Get("cmd.a2a.opt.peer_agent"), true), str("skill_id", L.Get("cmd.a2a.opt.skill_id_optional"), false), str("channel_ref", L.Get("cmd.a2a.opt.channel_ref_optional"), false), target(), str("mode", L.Get("cmd.a2a.opt.setup_mode"), false), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "trust", Description: L.Get("cmd.a2a.sub.trust"), Options: []*discordgo.ApplicationCommandOption{str("peer_agent", L.Get("cmd.a2a.opt.peer_agent"), true), str("relationship", L.Get("cmd.a2a.opt.relationship"), false), str("mode", L.Get("cmd.a2a.opt.setup_mode"), false), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "ask", Description: L.Get("cmd.a2a.sub.ask"), Options: []*discordgo.ApplicationCommandOption{str("peer_agent", L.Get("cmd.a2a.opt.peer_agent"), true), str("message", L.Get("cmd.a2a.opt.message"), true), str("skill_id", L.Get("cmd.a2a.opt.skill_id_optional"), false), target(), str("reason", L.Get("cmd.a2a.opt.reason_optional"), false), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "status", Description: L.Get("cmd.a2a.sub.status"), Options: []*discordgo.ApplicationCommandOption{str("task", L.Get("cmd.a2a.opt.task"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "delegate", Description: L.Get("cmd.a2a.sub.delegate"), Options: []*discordgo.ApplicationCommandOption{str("target_agent", L.Get("cmd.a2a.opt.target_agent"), true), str("skill_id", L.Get("cmd.a2a.opt.skill_id"), true), str("message", L.Get("cmd.a2a.opt.message"), true), str("reason", L.Get("cmd.a2a.opt.reason"), true), target(), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
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
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "delegate-to", Description: L.Get("cmd.a2a.sub.delegate_to"), Options: []*discordgo.ApplicationCommandOption{str("agent_id", L.Get("cmd.a2a.opt.agent_id"), true), str("skill_id", L.Get("cmd.a2a.opt.skill_id"), true), target(), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "undelegate-to", Description: L.Get("cmd.a2a.sub.undelegate_to"), Options: []*discordgo.ApplicationCommandOption{str("agent_id", L.Get("cmd.a2a.opt.agent_id"), true), str("skill_id", L.Get("cmd.a2a.opt.skill_id"), true), target(), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "max-concurrent", Description: L.Get("cmd.a2a.sub.max_concurrent"), Options: []*discordgo.ApplicationCommandOption{integer("value", L.Get("cmd.a2a.opt.value"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "transcript-mode", Description: L.Get("cmd.a2a.sub.transcript_mode"), Options: []*discordgo.ApplicationCommandOption{str("mode", L.Get("cmd.a2a.opt.mode"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "transcript-from", Description: L.Get("cmd.a2a.sub.transcript_from"), Options: []*discordgo.ApplicationCommandOption{str("agent_id", L.Get("cmd.a2a.opt.agent_id"), true), boolean("share", L.Get("cmd.a2a.opt.share"), false), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "transcript-target", Description: L.Get("cmd.a2a.sub.transcript_target"), Options: []*discordgo.ApplicationCommandOption{str("channel_id", L.Get("cmd.a2a.opt.channel_id"), true), str("confirmation_token", L.Get("cmd.a2a.opt.confirmation_token"), false)}},
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
	payload.Request.PolicyAction = sub.Name
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
			if sub.Name == "setup" || sub.Name == "trust" {
				payload.Request.SetupMode = opt.StringValue()
			} else {
				payload.Request.TranscriptMode = opt.StringValue()
			}
		case "target_channel_ref":
			payload.Request.TargetChannelRef = opt.StringValue()
		case "target_channel_id":
			payload.Request.TargetChannelID = opt.StringValue()
		case "target_thread_id":
			payload.Request.TargetThreadID = opt.StringValue()
		case "channel_id":
			payload.Request.CoPresentTargetChannels = []string{opt.StringValue()}
		case "share":
			v := opt.BoolValue()
			payload.Request.ShareDiscordContext = &v
		case "confirmation_token":
			payload.Request.ConfirmationToken = opt.StringValue()
		case "relationship":
			payload.Request.TrustRelationship = opt.StringValue()
		case "capability":
			payload.Request.Capability = opt.StringValue()
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
			payload.Request.SkillID = "task"
		}
		if strings.TrimSpace(payload.Request.ChannelRef) == "" {
			payload.Request.ChannelRef = "discord-" + strings.TrimSpace(payload.Request.ChannelID)
		}
		localSkill := a2a.SkillSlug(payload.Request.SkillID)
		payload.Request.AcceptSkills = []string{localSkill}
		payload.Request.ExposeSkills = []string{localSkill}
		payload.Request.DelegateSkills = []string{payload.Request.SkillID}
		if strings.TrimSpace(payload.Request.TargetChannelRef) == "" {
			payload.Request.TargetChannelRef = payload.Request.ChannelRef
		}
		mode := normalizeA2ASetupMode(payload.Request.SetupMode)
		payload.Request.SetupMode = mode
		switch {
		case mode == "co_present" || (mode == "auto" && payload.Request.TargetChannelRef == payload.Request.ChannelRef):
			payload.Request.TranscriptMode = "co_present"
			payload.Request.ResultVisibility = "transparent"
			share := true
			payload.Request.ShareDiscordContext = &share
		default:
			payload.Request.TranscriptMode = "delegator"
			payload.Request.ResultVisibility = "proxy"
		}
	case "ask":
		if strings.TrimSpace(payload.Request.SkillID) == "" {
			payload.Request.SkillID = "general/task"
		}
		if strings.TrimSpace(payload.Request.Reason) == "" {
			payload.Request.Reason = "user_request"
		}
	case "trust":
		if strings.TrimSpace(payload.Request.SkillID) == "" {
			payload.Request.SkillID = "task"
		}
		if strings.TrimSpace(payload.Request.TrustRelationship) == "" {
			payload.Request.TrustRelationship = "bidirectional"
		}
		if strings.TrimSpace(payload.Request.SetupMode) == "" {
			payload.Request.SetupMode = "auto"
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
	switch {
	case strings.HasPrefix(value, "local_"):
		payload.Request.LocalID = value
	default:
		payload.Request.TaskID = value
	}
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

func normalizeA2ASetupMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "", "auto":
		return "auto"
	case "co-present", "copresent", "co_present":
		return "co_present"
	case "safe", "proxy", "delegator":
		return "safe"
	default:
		return strings.TrimSpace(mode)
	}
}

func (b *Bot) cmdA2A(ctx cmdCtx) {
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
	case "trust":
		resp, _ = svc.TrustPeer(context.Background(), payload.Request)
	case "setup", "enable", "disable", "ref", "expose", "unexpose", "accept-from", "deny-from", "delegate-to", "undelegate-to", "max-concurrent", "transcript-mode", "transcript-from", "transcript-target":
		if strings.TrimSpace(payload.Request.ConfirmationToken) == "" {
			resp, _ = svc.PolicyPlan(context.Background(), payload.Request)
		} else {
			resp, _ = svc.PolicyApply(context.Background(), payload.Request)
		}
	default:
		resp = botmcp.A2AToolResponse{OK: false, Message: fmt.Sprintf("unknown A2A subcommand %q", payload.Subcommand)}
	}
	if resp.OK && resp.RequiresConfirmation && ctx.replyWithComponents != nil {
		components := b.a2aPolicyConfirmationComponents(payload, resp)
		ctx.sendReplyWithComponents(formatA2AResponse(resp), components, map[string]any{"a2a_confirmation": "button"})
		return
	}
	ctx.reply(formatA2AResponse(resp))
}

func (b *Bot) a2aPolicyConfirmationComponents(payload a2aSlashPayload, resp botmcp.A2AToolResponse) []discordgo.MessageComponent {
	if b.a2aConfirmations == nil || strings.TrimSpace(resp.ConfirmationToken) == "" || strings.TrimSpace(resp.ChangeID) == "" {
		return nil
	}
	stateID := b.a2aConfirmations.Put(payload, resp)
	if stateID == "" {
		return nil
	}
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    L.Get("a2a.confirm.apply"),
				Style:    discordgo.SuccessButton,
				CustomID: a2aPolicyConfirmationButtonCustomID("apply", stateID, payload.Request.ChannelID, resp.ChangeID),
			},
			discordgo.Button{
				Label:    L.Get("a2a.confirm.cancel"),
				Style:    discordgo.SecondaryButton,
				CustomID: a2aPolicyConfirmationButtonCustomID("cancel", stateID, payload.Request.ChannelID, resp.ChangeID),
			},
		}},
	}
}

func (b *Bot) handleA2AComponent(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	parts := strings.Split(data.CustomID, ":")
	if len(parts) != 5 || parts[0] != a2aComponentPrefix || parts[1] != a2aPolicyComponentSection {
		return
	}
	action, stateID := parts[2], parts[3]
	entry, ok := b.a2aConfirmations.Get(stateID)
	if !ok {
		b.respondA2AComponentUpdate(ds, i, L.Get("a2a.confirm.expired"), nil)
		return
	}
	req := entry.Payload.Request
	if i.GuildID != req.GuildID || i.ChannelID != req.ChannelID || !validateA2APolicyConfirmationButtonCustomID(data.CustomID, action, stateID, req.ChannelID, req.ChangeID) {
		respondInteractionEphemeral(ds, i, L.Get("a2a.confirm.invalid"))
		return
	}
	userID, username := interactionUser(i)
	if userID != req.RequestedByID {
		respondInteractionEphemeral(ds, i, L.Get("a2a.confirm.original_user_only"))
		return
	}
	if !b.userCanManageAuditTarget(ds, userID, i.ChannelID) {
		respondInteractionEphemeral(ds, i, L.Get("a2a.remedy.manager_required"))
		return
	}
	switch action {
	case "cancel":
		_, _ = b.a2aConfirmations.Take(stateID)
		b.respondA2AComponentUpdate(ds, i, L.Get("a2a.confirm.cancelled"), nil)
	case "apply":
		entry, ok = b.a2aConfirmations.Take(stateID)
		if !ok {
			b.respondA2AComponentUpdate(ds, i, L.Get("a2a.confirm.expired"), nil)
			return
		}
		entry.Payload.Request.ManageChannels = true
		entry.Payload.Request.RequestedBy = username
		entry.Payload.Request.RequestedByID = userID
		resp := b.applyA2APolicyConfirmation(entry.Payload, i.GuildID, i.ChannelID)
		b.respondA2AComponentUpdate(ds, i, formatA2AResponse(resp), nil)
	default:
		respondInteractionEphemeral(ds, i, L.Get("a2a.confirm.invalid"))
	}
}

func (b *Bot) applyA2APolicyConfirmation(payload a2aSlashPayload, guildID, channelID string) botmcp.A2AToolResponse {
	svc, err := botmcp.NewA2AService(botmcp.A2AServiceConfig{DataDir: b.dataDir, Config: botmcp.A2AConfigFromEnv(), Node: b.a2aNode, BoundGuildID: guildID, BoundChannelID: channelID, BoundTargetID: channelID, AuditEnabled: true, AuditRecordContent: true, ConnectNATS: false})
	if err != nil {
		return botmcp.A2AToolResponse{OK: false, Message: err.Error()}
	}
	defer svc.Close()
	if payload.Subcommand == "trust" {
		resp, err := svc.TrustPeer(context.Background(), payload.Request)
		if err != nil {
			return botmcp.A2AToolResponse{OK: false, Message: err.Error()}
		}
		return resp
	}
	resp, err := svc.PolicyApply(context.Background(), payload.Request)
	if err != nil {
		return botmcp.A2AToolResponse{OK: false, Message: err.Error()}
	}
	return resp
}

func (b *Bot) respondA2AComponentUpdate(ds *discordgo.Session, i *discordgo.InteractionCreate, content string, components []discordgo.MessageComponent) {
	content = truncateDiscordMessageContent(secrets.RedactEnv(content), mcpContentLimit)
	err := ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:         content,
			Components:      components,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		log.Printf("[a2a-ui] interaction update failed channel=%s content_len=%d components=%d: %v", i.ChannelID, len(content), len(components), err)
	}
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

func a2aPolicyConfirmationButtonCustomID(action, stateID, channelID, changeID string) string {
	mac := macA2APolicyConfirmation(action, stateID, channelID, changeID)
	return strings.Join([]string{a2aComponentPrefix, a2aPolicyComponentSection, action, stateID, mac}, ":")
}

func validateA2APolicyConfirmationButtonCustomID(customID, action, stateID, channelID, changeID string) bool {
	want := a2aPolicyConfirmationButtonCustomID(action, stateID, channelID, changeID)
	return hmac.Equal([]byte(customID), []byte(want))
}

func macA2APolicyConfirmation(action, stateID, channelID, changeID string) string {
	mac := hmac.New(sha256.New, []byte(a2aComponentSecret()))
	for _, part := range []string{action, stateID, channelID, changeID} {
		mac.Write([]byte(part))
		mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil))[:24]
}

func randomA2AConfirmationID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
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
	sb.WriteString(L.Getf("a2a.confirmation_required", resp.ConfirmationSummary, resp.ChangeID))
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
			label := firstNonEmpty(peer.DisplayName, peer.Name, peer.AgentID)
			reason := firstNonEmpty(peer.DelegationReason, allowed)
			sb.WriteString(L.Getf("a2a.peers.row_human", label, peer.AgentID, valueOrNone(peer.BotAgentID), valueOrNone(peer.ChannelRef), state, trust, allowed, reason, skills))
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
	sb.WriteString(L.Getf("a2a.policy.runtime_agent", valueOrNone(policy.RuntimeAgentID), yesNo(policy.Discoverable)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.bot_agent", valueOrNone(policy.BotAgentID)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.mode", valueOrNone(policy.DiscordTranscriptMode)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.result_visibility", valueOrNone(policy.ResultVisibility)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.trusted_task", joinTrustedTaskRuntimes(policy)))
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
	sb.WriteString(L.Getf("a2a.policy.delegate_targets", joinDelegateTargetsOrNone(policy.DelegateTargets)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.max_concurrent", policy.MaxConcurrent))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.co_present_ready", coPresentReadiness(policy)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.co_present_from", joinOrNone(policy.CoPresentFrom)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.co_present_from_runtimes", joinOrNone(policy.CoPresentFromRuntimes)))
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.policy.co_present_target_channels", joinOrNone(policy.CoPresentTargetChannels)))
	return sb.String()
}

func coPresentReadiness(policy a2a.ChannelA2APolicy) string {
	var missing []string
	if policy.ResultVisibility != "transparent" {
		missing = append(missing, "result_visibility=transparent")
	}
	if policy.DiscordTranscriptMode != "co_present" {
		missing = append(missing, "discord_transcript_mode=co_present")
	}
	if !policy.ShareDiscordContext {
		missing = append(missing, "share_discord_context=true")
	}
	if len(policy.CoPresentFrom) == 0 && len(policy.CoPresentFromRuntimes) == 0 {
		missing = append(missing, "co_present_from_runtimes")
	}
	if len(missing) == 0 {
		return L.Get("a2a.policy.co_present.ready")
	}
	return L.Getf("a2a.policy.co_present.blocked", strings.Join(missing, ", "))
}

func formatA2AEventDetail(value string) string {
	value = strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(value))
	if value == "" {
		return ""
	}
	return channel.EscapeDiscordMarkdown(discordmention.EscapeRaw(value))
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
	if task.MessageID != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.task.message_id", task.MessageID))
	}
	if task.ToAgent != "" || task.ExecutorAgent != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.task.route", valueOrNone(task.FromAgent), valueOrNone(firstNonEmptyA2A(task.ExecutorAgent, task.ToAgent))))
	}
	if task.SkillID != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.task.skill", task.SkillID))
	}
	if task.ChannelRef != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.task.channel_ref", task.ChannelRef))
	}
	sb.WriteString("\n")
	sb.WriteString(L.Getf("a2a.task.delivery", valueOrNone(task.ResultVisibility), valueOrNone(task.DiscordTranscriptMode), task.Revision, task.Terminal))
	if task.UpdatedAt != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.task.updated_at", task.UpdatedAt))
	}
	if len(task.Events) > 0 {
		sb.WriteString("\n")
		sb.WriteString(L.Get("a2a.task.events_title"))
		for _, event := range task.Events {
			var details []string
			if event.Content != "" {
				details = append(details, "content="+formatA2AEventDetail(event.Content))
			}
			if event.ErrorMessage != "" {
				details = append(details, "error="+formatA2AEventDetail(event.ErrorMessage))
			}
			content := ""
			if len(details) > 0 {
				content = " " + strings.Join(details, " ")
			}
			sb.WriteString("\n")
			sb.WriteString(L.Getf("a2a.task.event_row", event.Revision, event.EventType, event.State, content))
		}
	}
	if task.ErrorMessage != "" {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("a2a.task.error", task.ErrorMessage))
	}
	if remedy := a2aTaskRemedy(task); remedy != "" {
		sb.WriteString("\n")
		sb.WriteString(remedy)
	}
	sb.WriteString("\n")
	sb.WriteString(L.Get("a2a.task.actions_hint"))
	return sb.String()
}

func a2aTaskRemedy(task botmcp.A2ATaskSummary) string {
	switch task.ErrorCode {
	case a2a.ErrorSenderNotAllowed:
		return L.Get("a2a.remedy.sender_not_allowed")
	case a2a.ErrorChannelNotEnabled:
		return L.Get("a2a.remedy.channel_disabled")
	case a2a.ErrorSkillNotAllowed, a2a.ErrorUnauthorizedTarget:
		return L.Get("a2a.remedy.not_delegated")
	default:
		return ""
	}
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

func joinTrustedTaskRuntimes(policy a2a.ChannelA2APolicy) string {
	seen := map[string]bool{}
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, "`"+value+"`")
	}
	for _, agent := range policy.AcceptFromRuntimes {
		if taskSkillAllowedForDisplay(policy.AcceptSkills) {
			add(agent + " inbound default_task")
		}
	}
	for _, target := range policy.DelegateTargets {
		agent := strings.TrimSpace(target.RuntimeAgentID)
		if agent == "" {
			agent = strings.TrimSpace(target.AgentID)
		}
		if agent != "" && a2a.SkillSlug(target.SkillID) == "task" {
			add(agent + " outbound default_task")
		}
	}
	if len(out) == 0 {
		return L.Get("a2a.none")
	}
	return strings.Join(out, ", ")
}

func taskSkillAllowedForDisplay(skills []string) bool {
	if len(skills) == 0 {
		return true
	}
	for _, skill := range skills {
		if a2a.SkillSlug(skill) == "task" {
			return true
		}
	}
	return false
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

func joinDelegateTargetsOrNone(values []a2a.DelegateTargetPolicy) string {
	var out []string
	for _, value := range values {
		agent := strings.TrimSpace(value.RuntimeAgentID)
		if agent == "" {
			agent = strings.TrimSpace(value.AgentID)
		}
		channelRef := strings.TrimSpace(value.ChannelRef)
		skill := strings.TrimSpace(value.SkillID)
		if agent != "" && skill != "" {
			if channelRef != "" {
				out = append(out, "`"+agent+" @ "+channelRef+" / "+skill+"`")
			} else {
				out = append(out, "`"+agent+" / "+skill+"`")
			}
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
