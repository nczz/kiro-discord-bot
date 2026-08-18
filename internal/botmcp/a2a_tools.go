package botmcp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/internal/channelmeta"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
)

const (
	ToolA2APeers            = "bot_a2a_peers"
	ToolA2APolicyGet        = "bot_a2a_policy_get"
	ToolA2ATaskStatus       = "bot_a2a_task_status"
	ToolA2ARuntimePreflight = "bot_a2a_runtime_preflight"
	ToolA2APolicyPlan       = "bot_a2a_policy_plan"
	ToolA2APolicyApply      = "bot_a2a_policy_apply"
	ToolA2ATrustPeer        = "bot_a2a_trust_peer"
	ToolA2ADelegate         = "bot_a2a_delegate"
	ToolA2ACancel           = "bot_a2a_cancel"
	ToolA2AInputReply       = "bot_a2a_input_reply"
	ToolA2AAuthReply        = "bot_a2a_auth_reply"
)

type A2AServiceConfig struct {
	DataDir            string
	Config             a2a.Config
	Node               *a2a.Node
	PeerStore          *a2a.SQLitePeerStore
	PolicyStore        *a2a.SQLitePolicyStore
	TaskStore          *a2a.SQLiteTaskStore
	Publisher          *a2a.Publisher
	BoundGuildID       string
	BoundChannelID     string
	BoundTargetID      string
	ConfirmationSecret string
	AuditDBPath        string
	AuditEnabled       bool
	AuditRecordContent bool
	Now                func() time.Time
	ConnectNATS        bool
}

type A2AService struct {
	cfg         A2AServiceConfig
	peers       *a2a.SQLitePeerStore
	policies    *a2a.SQLitePolicyStore
	tasks       *a2a.SQLiteTaskStore
	node        *a2a.Node
	publisher   *a2a.Publisher
	pendingMu   sync.Mutex
	pendingPlan map[string]pendingPolicyPlan
	closeFns    []func()
}

type pendingPolicyPlan struct {
	BaseChangeID string
	Tool         string
	Policy       a2a.ChannelA2APolicy
	ExpiresAt    time.Time
}

type A2AToolRequest struct {
	GuildID                 string   `json:"guild_id,omitempty"`
	ChannelID               string   `json:"channel_id,omitempty"`
	RequestedBy             string   `json:"requested_by,omitempty"`
	RequestedByID           string   `json:"requested_by_id,omitempty"`
	RequestSource           string   `json:"request_source,omitempty"`
	ManageChannels          bool     `json:"manage_channels,omitempty"`
	TargetAgent             string   `json:"target_agent,omitempty"`
	SkillID                 string   `json:"skill_id,omitempty"`
	Message                 string   `json:"message,omitempty"`
	Reason                  string   `json:"reason,omitempty"`
	TaskID                  string   `json:"task_id,omitempty"`
	MessageID               string   `json:"message_id,omitempty"`
	LocalID                 string   `json:"local_id,omitempty"`
	Input                   string   `json:"input,omitempty"`
	Approve                 bool     `json:"approve,omitempty"`
	DenyReason              string   `json:"deny_reason,omitempty"`
	ChangeID                string   `json:"change_id,omitempty"`
	ConfirmationToken       string   `json:"confirmation_token,omitempty"`
	PolicyAction            string   `json:"policy_action,omitempty"`
	RequiresConfirmation    bool     `json:"requires_confirmation,omitempty"`
	DeliveryMode            string   `json:"delivery_mode,omitempty"`
	TranscriptMode          string   `json:"transcript_mode,omitempty"`
	ResultVisibility        string   `json:"result_visibility,omitempty"`
	ChannelRef              string   `json:"channel_ref,omitempty"`
	TargetChannelID         string   `json:"target_channel_id,omitempty"`
	TargetThreadID          string   `json:"target_thread_id,omitempty"`
	TargetChannelRef        string   `json:"target_channel_ref,omitempty"`
	SetupMode               string   `json:"setup_mode,omitempty"`
	TrustRelationship       string   `json:"relationship,omitempty"`
	Capability              string   `json:"capability,omitempty"`
	Enable                  *bool    `json:"enable,omitempty"`
	AcceptFrom              []string `json:"accept_from,omitempty"`
	AcceptFromRuntimes      []string `json:"accept_from_runtimes,omitempty"`
	AcceptSkills            []string `json:"accept_skills,omitempty"`
	ExposeSkills            []string `json:"expose_skills,omitempty"`
	DelegateTo              []string `json:"delegate_to,omitempty"`
	DelegateSkills          []string `json:"delegate_skills,omitempty"`
	DelegateMediaTypes      []string `json:"delegate_media_types,omitempty"`
	DelegateMaxBytes        int64    `json:"delegate_max_bytes,omitempty"`
	MaxConcurrent           *int     `json:"max_concurrent,omitempty"`
	ShareDiscordContext     *bool    `json:"share_discord_context,omitempty"`
	CoPresentFrom           []string `json:"co_present_from,omitempty"`
	CoPresentFromRuntimes   []string `json:"co_present_from_runtimes,omitempty"`
	CoPresentTargetChannels []string `json:"co_present_target_channels,omitempty"`
	AllowMemoryWrite        *bool    `json:"allow_memory_write,omitempty"`
	Limit                   int      `json:"limit,omitempty"`
}

type A2AToolResponse struct {
	OK                   bool                        `json:"ok"`
	Message              string                      `json:"message"`
	ErrorCode            a2a.ErrorCode               `json:"errorCode,omitempty"`
	RequiresConfirmation bool                        `json:"requiresConfirmation"`
	ConfirmationSummary  string                      `json:"confirmationSummary,omitempty"`
	RiskLabels           []string                    `json:"riskLabels,omitempty"`
	ExpiresAt            string                      `json:"expiresAt,omitempty"`
	ChangeID             string                      `json:"changeId,omitempty"`
	ConfirmationToken    string                      `json:"confirmationToken,omitempty"`
	Policy               *a2a.ChannelA2APolicy       `json:"policy,omitempty"`
	DeliveryReadiness    *A2APolicyDeliveryReadiness `json:"deliveryReadiness,omitempty"`
	RuntimePreflight     *a2a.RuntimeCutoverReport   `json:"runtimePreflight,omitempty"`
	Peers                []A2APeerSummary            `json:"peers,omitempty"`
	PeerPolicy           *A2APeerPolicySummary       `json:"peerPolicy,omitempty"`
	Tasks                []A2ATaskSummary            `json:"tasks,omitempty"`
	Task                 *A2ATaskSummary             `json:"task,omitempty"`
	Metadata             map[string]interface{}      `json:"metadata,omitempty"`
}

type A2APeerSummary struct {
	AgentID           string   `json:"agentId"`
	BotAgentID        string   `json:"botAgentId,omitempty"`
	Name              string   `json:"name"`
	Trusted           bool     `json:"trusted"`
	Online            bool     `json:"online"`
	Stale             bool     `json:"stale"`
	Skills            []string `json:"skills"`
	HiddenSkillCount  int      `json:"hiddenSkillCount,omitempty"`
	InboundAllowed    bool     `json:"inboundAllowed"`
	InboundReason     string   `json:"inboundReason,omitempty"`
	DelegationAllowed bool     `json:"delegationAllowed"`
	ProtocolBinding   string   `json:"protocolBinding,omitempty"`
	ProtocolVersion   string   `json:"protocolVersion,omitempty"`
	SignatureStatus   string   `json:"signatureStatus,omitempty"`
	Runtime           string   `json:"runtime,omitempty"`
	ChannelRef        string   `json:"channelRef,omitempty"`
	Wakeable          bool     `json:"wakeable"`
	DisplayName       string   `json:"displayName,omitempty"`
	DelegationReason  string   `json:"delegationReason,omitempty"`
	DiscordGuildID    string   `json:"discordGuildId,omitempty"`
	DiscordChannelID  string   `json:"discordChannelId,omitempty"`
	DiscordThreadID   string   `json:"discordThreadId,omitempty"`
}

type A2APeerPolicySummary struct {
	Enabled                    bool                       `json:"enabled"`
	CurrentRuntimeAgentID      string                     `json:"currentRuntimeAgentId,omitempty"`
	CurrentChannelRef          string                     `json:"currentChannelRef,omitempty"`
	InboundAllowedRuntimes     []string                   `json:"inboundAllowedRuntimes,omitempty"`
	LegacyInboundAllowedAgents []string                   `json:"legacyInboundAllowedAgents,omitempty"`
	InboundAcceptedSkills      []string                   `json:"inboundAcceptedSkills,omitempty"`
	OutboundDelegateTargets    []A2ADelegateTargetSummary `json:"outboundDelegateTargets,omitempty"`
}

type A2ADelegateTargetSummary struct {
	RuntimeAgentID string `json:"runtimeAgentId,omitempty"`
	AgentID        string `json:"agentId,omitempty"`
	ChannelRef     string `json:"channelRef,omitempty"`
	SkillID        string `json:"skillId,omitempty"`
}

type A2APolicyDeliveryReadiness struct {
	ResultVisibility        string   `json:"resultVisibility"`
	DiscordTranscriptMode   string   `json:"discordTranscriptMode"`
	ShareDiscordContext     bool     `json:"shareDiscordContext"`
	CoPresentReady          bool     `json:"coPresentReady"`
	CoPresentMissing        []string `json:"coPresentMissing,omitempty"`
	CoPresentFrom           []string `json:"coPresentFrom,omitempty"`
	CoPresentFromRuntimes   []string `json:"coPresentFromRuntimes,omitempty"`
	CoPresentTargetChannels []string `json:"coPresentTargetChannels,omitempty"`
	Guidance                string   `json:"guidance"`
}

type A2ATaskEventSummary struct {
	Revision     int64         `json:"revision"`
	EventType    string        `json:"eventType"`
	State        a2a.TaskState `json:"state,omitempty"`
	Content      string        `json:"content,omitempty"`
	ErrorCode    a2a.ErrorCode `json:"errorCode,omitempty"`
	ErrorMessage string        `json:"errorMessage,omitempty"`
	CreatedAt    string        `json:"createdAt,omitempty"`
}

type A2ATaskSummary struct {
	LocalID               string                `json:"localId"`
	TaskID                string                `json:"taskId,omitempty"`
	MessageID             string                `json:"messageId,omitempty"`
	Direction             string                `json:"direction"`
	FromAgent             string                `json:"fromAgent"`
	ToAgent               string                `json:"toAgent"`
	ExecutorAgent         string                `json:"executorAgent,omitempty"`
	ChannelID             string                `json:"channelId,omitempty"`
	ChannelRef            string                `json:"channelRef,omitempty"`
	SkillID               string                `json:"skillId,omitempty"`
	ResultVisibility      string                `json:"resultVisibility,omitempty"`
	DiscordTranscriptMode string                `json:"discordTranscriptMode,omitempty"`
	State                 a2a.TaskState         `json:"state"`
	Revision              int64                 `json:"revision"`
	Terminal              bool                  `json:"terminal"`
	ErrorCode             a2a.ErrorCode         `json:"errorCode,omitempty"`
	ErrorMessage          string                `json:"errorMessage,omitempty"`
	OriginRuntimeRef      a2a.OriginRuntimeRef  `json:"originRuntimeRef,omitempty"`
	CreatedAt             string                `json:"createdAt,omitempty"`
	UpdatedAt             string                `json:"updatedAt,omitempty"`
	Events                []A2ATaskEventSummary `json:"events,omitempty"`
}

type confirmationPayload struct {
	Action    string `json:"action"`
	ChangeID  string `json:"changeId"`
	GuildID   string `json:"guildId"`
	ChannelID string `json:"channelId"`
	UserID    string `json:"userId"`
	Hash      string `json:"hash"`
	ExpiresAt int64  `json:"expiresAt"`
}

func NewA2AService(cfg A2AServiceConfig) (*A2AService, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = dataDir()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.BoundGuildID == "" {
		cfg.BoundGuildID = strings.TrimSpace(os.Getenv("BOT_TOOLS_GUILD_ID"))
	}
	if cfg.BoundChannelID == "" {
		cfg.BoundChannelID = strings.TrimSpace(os.Getenv("BOT_TOOLS_CHANNEL_ID"))
	}
	if cfg.BoundTargetID == "" {
		cfg.BoundTargetID = strings.TrimSpace(os.Getenv("BOT_TOOLS_TARGET_CHANNEL_ID"))
	}
	if cfg.ConfirmationSecret == "" {
		cfg.ConfirmationSecret = firstNonEmpty(os.Getenv("A2A_CONFIRMATION_SECRET"), os.Getenv("DISCORD_TOKEN"))
		if cfg.ConfirmationSecret == "" {
			cfg.ConfirmationSecret = fallbackConfirmationSecret()
		}
	}
	if cfg.AuditDBPath == "" {
		cfg.AuditDBPath = botToolsAuditDBPath()
	}
	s := &A2AService{cfg: cfg, peers: cfg.PeerStore, policies: cfg.PolicyStore, tasks: cfg.TaskStore, node: cfg.Node, publisher: cfg.Publisher, pendingPlan: make(map[string]pendingPolicyPlan)}
	if s.peers == nil {
		store, err := a2a.OpenPeerStore(cfg.DataDir)
		if err != nil {
			return nil, err
		}
		s.peers = store
		s.closeFns = append(s.closeFns, func() { _ = store.Close() })
	}
	if s.policies == nil {
		store, err := a2a.OpenPolicyStore(cfg.DataDir, cfg.Config.AgentID)
		if err != nil {
			s.Close()
			return nil, err
		}
		s.policies = store
		s.closeFns = append(s.closeFns, func() { _ = store.Close() })
	}
	if s.tasks == nil {
		store, err := a2a.OpenTaskStore(cfg.DataDir)
		if err != nil {
			s.Close()
			return nil, err
		}
		s.tasks = store
		s.closeFns = append(s.closeFns, func() { _ = store.Close() })
	}
	return s, nil
}

func NewA2AServiceFromEnv(ctx context.Context) (*A2AService, error) {
	return NewA2AService(A2AServiceConfig{DataDir: dataDir(), Config: a2aConfigFromEnv(), BoundGuildID: os.Getenv("BOT_TOOLS_GUILD_ID"), BoundChannelID: os.Getenv("BOT_TOOLS_CHANNEL_ID"), BoundTargetID: os.Getenv("BOT_TOOLS_TARGET_CHANNEL_ID"), AuditEnabled: botToolsAuditEnabled(), AuditRecordContent: botToolsAuditRecordContent(), ConnectNATS: true})
}

func (s *A2AService) Close() {
	for i := len(s.closeFns) - 1; i >= 0; i-- {
		s.closeFns[i]()
	}
	if s.node != nil && s.cfg.Node == nil {
		s.node.Close()
	}
}

func (s *A2AService) Peers(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if err := s.validateContext(req, false); err != nil {
		return responseError(err), nil
	}
	policy, _ := s.currentPolicy(ctx, req)
	rows, err := s.peers.TrustSummary(ctx, 90*time.Second)
	if err != nil {
		return responseError(fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)), nil
	}
	peers := make([]A2APeerSummary, 0, len(rows))
	runtimeOnly := s.cfg.Config.RuntimeIDMode == a2a.RuntimeIDModeRuntime
	canonicalRefs := map[string]string{}
	if runtimeOnly {
		canonicalRefs = canonicalRuntimeChannelRefs(s.cfg.DataDir, req.GuildID)
	}
	for _, row := range rows {
		if row.AgentID == s.cfg.Config.AgentID || (runtimeOnly && row.Runtime == "kiro-discord-bot") {
			continue
		}
		if runtimeOnly && (row.Stale || !row.Online) {
			continue
		}
		if runtimeOnly {
			if row.Runtime != "channel" && row.Runtime != "thread" {
				continue
			}
			if strings.TrimSpace(req.GuildID) == "" || strings.TrimSpace(row.DiscordGuildID) != strings.TrimSpace(req.GuildID) {
				continue
			}
			if canonicalRef := canonicalRefs[strings.TrimSpace(row.DiscordChannelID)]; canonicalRef != "" && strings.TrimSpace(row.ChannelRef) != canonicalRef {
				continue
			}
		}
		callableSkills, reason := peerDelegationCapability(policy, row, row.SkillIDs, s.cfg.Config.RuntimeIDMode)
		delegationAllowed := len(callableSkills) > 0
		hiddenSkillCount := len(row.SkillIDs) - len(callableSkills)
		if hiddenSkillCount < 0 {
			hiddenSkillCount = 0
		}
		inboundAllowed, inboundReason := peerInboundCapability(policy, row)
		peers = append(peers, A2APeerSummary{AgentID: string(row.AgentID), Name: row.Name, BotAgentID: row.BotAgentID, Trusted: row.Trusted, Online: row.Online, Stale: row.Stale, Skills: callableSkills, HiddenSkillCount: hiddenSkillCount, InboundAllowed: inboundAllowed, InboundReason: inboundReason, DelegationAllowed: delegationAllowed, DelegationReason: reason, Runtime: row.Runtime, ChannelRef: row.ChannelRef, DisplayName: peerDisplayName(row), DiscordGuildID: row.DiscordGuildID, DiscordChannelID: row.DiscordChannelID, DiscordThreadID: row.DiscordThreadID, Wakeable: delegationAllowed && row.Runtime == "channel" && !row.Stale, ProtocolBinding: row.SupportedBinding, ProtocolVersion: row.ProtocolVersion, SignatureStatus: row.SignatureStatus})
	}
	return A2AToolResponse{OK: true, Message: "A2A peers listed", Peers: peers, PeerPolicy: peerPolicySummary(policy), DeliveryReadiness: ptrPolicyDeliveryReadiness(policy)}, nil
}

func peerDisplayName(peer a2a.PeerTrustDisplay) string {
	if strings.TrimSpace(peer.DisplayName) != "" {
		return peer.DisplayName
	}
	if strings.TrimSpace(peer.ChannelRef) != "" {
		return peer.ChannelRef
	}
	if strings.TrimSpace(peer.Description) != "" {
		return peer.Description
	}
	return peer.Name
}
func peerPolicySummary(policy a2a.ChannelA2APolicy) *A2APeerPolicySummary {
	targets := make([]A2ADelegateTargetSummary, 0, len(policy.DelegateTargets))
	for _, target := range policy.DelegateTargets {
		targets = append(targets, A2ADelegateTargetSummary{
			RuntimeAgentID: strings.TrimSpace(target.RuntimeAgentID),
			AgentID:        strings.TrimSpace(target.AgentID),
			ChannelRef:     strings.TrimSpace(target.ChannelRef),
			SkillID:        strings.TrimSpace(target.SkillID),
		})
	}
	summary := &A2APeerPolicySummary{
		Enabled:               policy.Enabled,
		CurrentRuntimeAgentID: strings.TrimSpace(policy.RuntimeAgentID),
		CurrentChannelRef:     strings.TrimSpace(policy.ChannelRef),
	}
	if !policy.Enabled {
		return summary
	}
	summary.InboundAllowedRuntimes = append([]string(nil), policy.AcceptFromRuntimes...)
	summary.LegacyInboundAllowedAgents = append([]string(nil), policy.AcceptFrom...)
	summary.InboundAcceptedSkills = append([]string(nil), policy.AcceptSkills...)
	summary.OutboundDelegateTargets = targets
	return summary
}

func peerInboundCapability(policy a2a.ChannelA2APolicy, peer a2a.PeerTrustDisplay) (bool, string) {
	if !policy.Enabled {
		return false, "channel A2A policy disabled"
	}
	allowed := policy.AcceptFromRuntimes
	if len(allowed) == 0 {
		allowed = policy.AcceptFrom
	}
	if len(allowed) == 0 {
		return false, "no inbound runtime allowlist"
	}
	if stringListAllows(allowed, string(peer.AgentID)) {
		return true, "allowed by current channel inbound policy"
	}
	return false, "not accepted by current channel policy"
}

func peerDelegationCapability(policy a2a.ChannelA2APolicy, peer a2a.PeerTrustDisplay, skills []string, mode a2a.RuntimeIDMode) ([]string, string) {
	visibleSkills := visiblePeerSkills(policy, peer, skills, mode)
	if len(visibleSkills) > 0 {
		return visibleSkills, "allowed"
	}
	if peer.Stale {
		return nil, "peer stale"
	}
	runtimeOnly := mode == a2a.RuntimeIDModeRuntime
	if runtimeOnly && peer.Runtime != "channel" && peer.Runtime != "thread" {
		return nil, "not a runtime peer"
	}
	if !policy.Enabled {
		return nil, "channel A2A policy disabled"
	}
	if runtimeOnly {
		return nil, "missing runtime delegate target"
	}
	return nil, "missing delegate target or skill"
}

func visiblePeerSkills(policy a2a.ChannelA2APolicy, peer a2a.PeerTrustDisplay, skills []string, mode a2a.RuntimeIDMode) []string {
	if !policy.Enabled {
		return nil
	}
	runtimeOnly := mode == a2a.RuntimeIDModeRuntime
	out := make([]string, 0, len(skills))
	for _, skill := range skills {
		agent := string(peer.AgentID)
		if runtimeOnly {
			if policyDelegatesExactRuntime(policy, agent, skill, peer.ChannelRef) || policyDelegatesSameDiscordChannelRuntime(policy, peer, skill) {
				out = append(out, skill)
			}
			continue
		}
		if policyDelegatesRuntime(policy, agent, skill, policy.ChannelRef) {
			out = append(out, skill)
		}
	}
	if runtimeOnly {
		for _, skill := range visiblePolicyTargetSkills(policy, peer) {
			if !skillListAllows(out, skill) {
				out = append(out, skill)
			}
		}
	}
	return out
}

func visiblePolicyTargetSkills(policy a2a.ChannelA2APolicy, peer a2a.PeerTrustDisplay) []string {
	agent := strings.TrimSpace(string(peer.AgentID))
	if agent == "" {
		return nil
	}
	out := make([]string, 0, len(policy.DelegateTargets))
	for _, target := range policy.DelegateTargets {
		targetAgent := strings.TrimSpace(target.RuntimeAgentID)
		if targetAgent == "" {
			targetAgent = strings.TrimSpace(target.AgentID)
		}
		if targetAgent == "" || !stringListAllows([]string{targetAgent}, agent) {
			if strings.TrimSpace(peer.BotAgentID) == "" || strings.TrimSpace(peer.DiscordChannelID) == "" || strings.TrimSpace(peer.DiscordChannelID) != strings.TrimSpace(policy.ChannelID) {
				continue
			}
			if strings.TrimSpace(target.RuntimeAgentID) == "" || !strings.HasPrefix(strings.TrimSpace(target.RuntimeAgentID), strings.TrimSpace(peer.BotAgentID)+"-") {
				continue
			}
		}
		if target.ChannelRef != "" && target.ChannelRef != "*" && strings.TrimSpace(peer.ChannelRef) != "" && target.ChannelRef != peer.ChannelRef {
			continue
		}
		skill := strings.TrimSpace(target.SkillID)
		if skill == "" {
			skill = "task"
		}
		out = appendUnique(out, skill)
	}
	return out
}

func policyDelegatesSameDiscordChannelRuntime(policy a2a.ChannelA2APolicy, peer a2a.PeerTrustDisplay, skill string) bool {
	if strings.TrimSpace(peer.BotAgentID) == "" || strings.TrimSpace(peer.DiscordChannelID) == "" || strings.TrimSpace(peer.DiscordChannelID) != strings.TrimSpace(policy.ChannelID) {
		return false
	}
	for _, target := range policy.DelegateTargets {
		if strings.TrimSpace(target.RuntimeAgentID) == "" || !strings.HasPrefix(strings.TrimSpace(target.RuntimeAgentID), strings.TrimSpace(peer.BotAgentID)+"-") {
			continue
		}
		if skillListAllows([]string{target.SkillID}, skill) {
			return true
		}
	}
	return false
}

func policyDelegatesSameDiscordChannelPeerRow(policy a2a.ChannelA2APolicy, peer a2a.PeerRow, skill string) bool {
	return policyDelegatesSameDiscordChannelRuntime(policy, a2a.PeerTrustDisplay{
		AgentID:          peer.AgentID,
		BotAgentID:       peer.ExtendedCard.BotAgentID,
		DiscordChannelID: peer.ExtendedCard.DiscordChannelID,
	}, skill)
}

func policyDelegatesExactRuntime(policy a2a.ChannelA2APolicy, agent, skill, targetChannelRef string) bool {
	for _, target := range policy.DelegateTargets {
		targetAgent := strings.TrimSpace(target.RuntimeAgentID)
		if targetAgent == "" {
			targetAgent = strings.TrimSpace(target.AgentID)
		}
		if targetAgent == "" || !stringListAllows([]string{targetAgent}, agent) {
			continue
		}
		if !skillListAllows([]string{target.SkillID}, skill) {
			continue
		}
		if target.RuntimeAgentID != "" || target.ChannelRef == "" || targetChannelRef == "" || target.ChannelRef == targetChannelRef || target.ChannelRef == "*" {
			return true
		}
	}
	return false
}

func policyRuntimeTargetChannelRef(policy a2a.ChannelA2APolicy, agent, skill string) string {
	for _, target := range policy.DelegateTargets {
		if strings.TrimSpace(target.RuntimeAgentID) == "" {
			continue
		}
		if !stringListAllows([]string{target.RuntimeAgentID}, agent) {
			continue
		}
		if !skillListAllows([]string{target.SkillID}, skill) {
			continue
		}
		ref := strings.TrimSpace(target.ChannelRef)
		if ref != "" && ref != "*" {
			return ref
		}
	}
	return ""
}

func (s *A2AService) PolicyGet(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if err := s.validateContext(req, false); err != nil {
		return responseError(err), nil
	}
	policy, err := s.currentPolicy(ctx, req)
	if err != nil {
		return responseError(err), nil
	}
	return A2AToolResponse{OK: true, Message: "A2A policy loaded", Policy: &policy, DeliveryReadiness: ptrPolicyDeliveryReadiness(policy)}, nil
}

func (s *A2AService) RuntimePreflight(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if err := s.validateContext(req, true); err != nil {
		return responseError(err), nil
	}
	report, err := s.policies.RuntimeCutoverReadiness(ctx, s.cfg.Config, strings.TrimSpace(req.GuildID))
	if err != nil {
		return responseError(fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)), nil
	}
	message := "A2A runtime cutover preflight passed"
	if !report.Ready {
		message = "A2A runtime cutover preflight blocked"
	}
	return A2AToolResponse{OK: true, Message: message, RuntimePreflight: &report}, nil
}

func (s *A2AService) TaskStatus(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if err := s.validateContext(req, false); err != nil {
		return responseError(err), nil
	}
	if strings.TrimSpace(req.LocalID) != "" {
		row, err := s.tasks.GetByLocalID(ctx, strings.TrimSpace(req.LocalID))
		if err != nil {
			return responseError(taskLookupError(err)), nil
		}
		if err := authorizeTaskStatus(row, req); err != nil {
			return responseError(err), nil
		}
		sum := s.summarizeTaskWithEvents(ctx, row)
		return A2AToolResponse{OK: true, Message: taskStatusMessage(row), Task: &sum, Metadata: taskStatusMetadata(row)}, nil
	}
	if strings.TrimSpace(req.TaskID) != "" {
		row, err := s.lookupOutboundTaskOrMessage(ctx, strings.TrimSpace(req.TaskID))
		if err != nil {
			return responseError(taskLookupError(err)), nil
		}
		if err := authorizeTaskStatus(row, req); err != nil {
			return responseError(err), nil
		}
		sum := s.summarizeTaskWithEvents(ctx, row)
		return A2AToolResponse{OK: true, Message: taskStatusMessage(row), Task: &sum, Metadata: taskStatusMetadata(row)}, nil
	}
	if strings.TrimSpace(req.MessageID) != "" {
		row, err := s.tasks.GetByDirectionMessage(ctx, "outbound", a2a.MessageID(strings.TrimSpace(req.MessageID)))
		if err != nil {
			return responseError(taskLookupError(err)), nil
		}
		if err := authorizeTaskStatus(row, req); err != nil {
			return responseError(err), nil
		}
		sum := s.summarizeTaskWithEvents(ctx, row)
		return A2AToolResponse{OK: true, Message: taskStatusMessage(row), Task: &sum, Metadata: taskStatusMetadata(row)}, nil
	}
	rows, err := s.tasks.ListByChannel(ctx, "outbound", strings.TrimSpace(req.ChannelID), req.Limit)
	if err != nil {
		return responseError(fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)), nil
	}
	tasks := make([]A2ATaskSummary, 0, len(rows))
	for _, row := range rows {
		if err := authorizeTaskStatus(row, req); err != nil {
			continue
		}
		tasks = append(tasks, summarizeTask(row))
	}
	return A2AToolResponse{OK: true, Message: "A2A recent tasks listed", Tasks: tasks}, nil
}

func authorizeTaskStatus(row a2a.TaskRow, req A2AToolRequest) error {
	if strings.TrimSpace(row.GuildID) != "" && strings.TrimSpace(req.GuildID) != "" && strings.TrimSpace(row.GuildID) != strings.TrimSpace(req.GuildID) {
		return fmt.Errorf("%w: task is not visible from this guild", errorCode(a2a.ErrorPolicyDenied))
	}
	if strings.TrimSpace(row.ChannelID) != strings.TrimSpace(req.ChannelID) {
		return fmt.Errorf("%w: task is not visible from this channel", errorCode(a2a.ErrorPolicyDenied))
	}
	if req.ManageChannels {
		return nil
	}
	if strings.TrimSpace(row.ClientTaskRef) == "" || strings.TrimSpace(req.RequestedByID) == "" || strings.TrimSpace(row.ClientTaskRef) != strings.TrimSpace(req.RequestedByID) {
		return fmt.Errorf("%w: requester does not own this task", errorCode(a2a.ErrorPolicyDenied))
	}
	return nil
}

func (s *A2AService) PolicyPlan(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if err := s.validateContext(req, true); err != nil {
		return responseError(err), nil
	}
	policy, err := s.currentPolicy(ctx, req)
	if err != nil {
		return responseError(err), nil
	}
	planned := s.applyPolicyDiff(policy, req)
	changeID := policyChangeID(planned)
	summary := policySummary(policy, planned)
	token := s.confirmationToken("policy_apply", changeID, req, planned)
	exp := s.cfg.Now().UTC().Add(10 * time.Minute)
	if err := s.storePendingPolicyPlan(changeID, policyChangeID(policy), ToolA2APolicyPlan, planned, exp); err != nil {
		return responseError(fmt.Errorf("%w: store pending policy plan: %v", errorCode(a2a.ErrorStoreError), err)), nil
	}
	_ = s.recordAudit(ctx, a2a.AuditPolicyChangePlanned, req, "planned", "", map[string]any{"change_id": changeID})
	return A2AToolResponse{OK: true, Message: "A2A policy change planned", RequiresConfirmation: true, ConfirmationSummary: summary, RiskLabels: policyRiskLabels(policy, planned), ExpiresAt: exp.Format(time.RFC3339), ChangeID: changeID, ConfirmationToken: token, Policy: &planned, DeliveryReadiness: ptrPolicyDeliveryReadiness(planned)}, nil
}

func (s *A2AService) PolicyApply(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if err := s.validateContext(req, true); err != nil {
		return responseError(err), nil
	}
	policy, err := s.currentPolicy(ctx, req)
	if err != nil {
		return responseError(err), nil
	}
	planned := s.applyPolicyDiff(policy, req)
	changeID := policyChangeID(planned)
	if strings.TrimSpace(req.ChangeID) != "" && strings.TrimSpace(req.ChangeID) != changeID {
		if pending, ok := s.pendingPolicyPlan(strings.TrimSpace(req.ChangeID)); ok {
			if pending.BaseChangeID != policyChangeID(policy) {
				return responseError(fmt.Errorf("%w: planned policy is stale; re-run the plan", errorCode(a2a.ErrorPolicyDenied))), nil
			}
			planned = pending.Policy
			changeID = strings.TrimSpace(req.ChangeID)
		}
	}
	if strings.TrimSpace(req.ChangeID) != "" && strings.TrimSpace(req.ChangeID) != changeID {
		return responseError(fmt.Errorf("%w: change_id does not match policy diff", errorCode(a2a.ErrorPolicyDenied))), nil
	}
	if err := s.verifyConfirmation("policy_apply", changeID, req, planned); err != nil {
		_ = s.recordAudit(ctx, a2a.AuditPolicyChangeDenied, req, "denied", err.Error(), map[string]any{"change_id": changeID})
		return responseError(err), nil
	}
	if err := s.policies.Save(ctx, planned, req.RequestedByID); err != nil {
		return responseError(fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)), nil
	}
	s.trustDelegatedPeers(ctx, delegatedPeerAgents(planned))
	_ = s.recordAudit(ctx, a2a.AuditPolicyChangeApplied, req, "applied", "", map[string]any{"change_id": changeID})
	return A2AToolResponse{OK: true, Message: "A2A policy applied", ChangeID: changeID, Policy: &planned}, nil
}

func (s *A2AService) TrustPeer(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if err := s.validateContext(req, true); err != nil {
		return responseError(err), nil
	}
	policy, err := s.currentPolicy(ctx, req)
	if err != nil {
		return responseError(err), nil
	}
	if !simpleInboundTrustRequest(req, policy) || strings.TrimSpace(req.ConfirmationToken) != "" || strings.TrimSpace(req.ChangeID) != "" {
		return responseError(fmt.Errorf("%w: bot_a2a_trust_peer only supports simple inbound receiver consent with target_agent; expert A2A policy planning/apply has been retired from bot-tools", errorCode(a2a.ErrorPolicyDenied))), nil
	}
	if _, ok := s.lookupPeer(ctx, strings.TrimSpace(req.TargetAgent)); !ok {
		return responseError(fmt.Errorf("%w: target peer is unknown", errorCode(a2a.ErrorUnknownAgent))), nil
	}
	planned, err := s.applyTrustPeerDiff(ctx, policy, req)
	if err != nil {
		return responseError(err), nil
	}
	changeID := policyChangeID(planned)
	if err := s.policies.Save(ctx, planned, req.RequestedByID); err != nil {
		return responseError(fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)), nil
	}
	_ = s.recordAudit(ctx, a2a.AuditPolicyChangeApplied, req, "applied", "", map[string]any{"change_id": changeID, "tool": ToolA2ATrustPeer, "mode": "simple_inbound"})
	return A2AToolResponse{OK: true, Message: "A2A peer allowed", ChangeID: changeID, Policy: &planned, DeliveryReadiness: ptrPolicyDeliveryReadiness(planned)}, nil
}

func simpleInboundTrustRequest(req A2AToolRequest, policy a2a.ChannelA2APolicy) bool {
	relationship, err := normalizeTrustRelationship(req.TrustRelationship)
	if err != nil || relationship != "inbound" {
		return false
	}
	mode := normalizeSetupMode(req.SetupMode)
	if mode != "auto" {
		return false
	}
	if v := strings.TrimSpace(req.ChannelRef); v != "" && v != strings.TrimSpace(policy.ChannelRef) {
		return false
	}
	return strings.TrimSpace(req.SkillID) == "" &&
		strings.TrimSpace(req.Capability) == "" &&
		strings.TrimSpace(req.TargetChannelID) == "" &&
		strings.TrimSpace(req.TargetThreadID) == "" &&
		strings.TrimSpace(req.TargetChannelRef) == "" &&
		strings.TrimSpace(req.DeliveryMode) == "" &&
		strings.TrimSpace(req.TranscriptMode) == "" &&
		strings.TrimSpace(req.ResultVisibility) == "" &&
		(strings.TrimSpace(req.PolicyAction) == "" || strings.TrimSpace(req.PolicyAction) == "trust" || strings.TrimSpace(req.PolicyAction) == "allow") &&
		req.MaxConcurrent == nil &&
		req.ShareDiscordContext == nil &&
		req.AllowMemoryWrite == nil &&
		req.DelegateMaxBytes == 0 &&
		len(req.AcceptFrom) == 0 &&
		len(req.AcceptFromRuntimes) == 0 &&
		len(req.AcceptSkills) == 0 &&
		len(req.ExposeSkills) == 0 &&
		len(req.DelegateTo) == 0 &&
		len(req.DelegateSkills) == 0 &&
		len(req.DelegateMediaTypes) == 0 &&
		len(req.CoPresentFrom) == 0 &&
		len(req.CoPresentFromRuntimes) == 0 &&
		len(req.CoPresentTargetChannels) == 0
}

func (s *A2AService) RevokePeer(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if err := s.validateContext(req, true); err != nil {
		return responseError(err), nil
	}
	peer := strings.TrimSpace(req.TargetAgent)
	if err := a2a.ValidateAgentID(a2a.AgentID(peer)); err != nil {
		return responseError(fmt.Errorf("%w: peer_agent is required and must be a valid agent id: %v", errorCode(a2a.ErrorPolicyDenied), err)), nil
	}
	policy, err := s.currentPolicy(ctx, req)
	if err != nil {
		return responseError(err), nil
	}
	policy.AcceptFrom = removeStrings(policy.AcceptFrom, peer)
	policy.AcceptFromRuntimes = removeStrings(policy.AcceptFromRuntimes, peer)
	policy.CoPresentFrom = removeStrings(policy.CoPresentFrom, peer)
	policy.CoPresentFromRuntimes = removeStrings(policy.CoPresentFromRuntimes, peer)
	policy.DelegateTo = removeStrings(policy.DelegateTo, peer)
	policy.DelegateTargets = removeDelegateTargetsForPeer(policy.DelegateTargets, peer)
	if len(policy.AcceptFrom) == 0 && len(policy.AcceptFromRuntimes) == 0 {
		policy.AcceptSkills = nil
	}
	if err := s.policies.Save(ctx, policy, req.RequestedByID); err != nil {
		return responseError(fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)), nil
	}
	changeID := policyChangeID(policy)
	_ = s.recordAudit(ctx, a2a.AuditPolicyChangeApplied, req, "applied", "", map[string]any{"change_id": changeID, "tool": "bot_a2a_revoke_peer", "mode": "simple_revoke"})
	return A2AToolResponse{OK: true, Message: "A2A peer revoked", ChangeID: changeID, Policy: &policy, DeliveryReadiness: ptrPolicyDeliveryReadiness(policy)}, nil
}

func (s *A2AService) Delegate(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if err := s.validateContext(req, false); err != nil {
		return responseError(err), nil
	}
	policy, err := s.currentPolicy(ctx, req)
	if err != nil {
		return responseError(err), nil
	}
	if !policy.Enabled {
		return responseError(fmt.Errorf("%w: channel A2A policy is disabled", errorCode(a2a.ErrorChannelNotEnabled))), nil
	}
	target := a2a.AgentID(strings.TrimSpace(req.TargetAgent))
	if err := a2a.ValidateAgentID(target); err != nil {
		return responseError(fmt.Errorf("%w: %v", errorCode(a2a.ErrorUnknownAgent), err)), nil
	}
	peer, err := s.peers.Get(ctx, target)
	if err != nil {
		return responseError(fmt.Errorf("%w: target peer is unknown", errorCode(a2a.ErrorUnknownAgent))), nil
	}
	targetChannelRef, effectiveSkill, delegated, exactRuntimePolicy, err := s.resolveDelegateTarget(policy, req, peer)
	if err != nil {
		return responseError(err), nil
	}
	authorizationMode := "persistent_policy"
	persistentDelegateTarget := true
	if !delegated && !exactRuntimePolicy {
		if !s.directHumanEphemeralDelegateAllowed(req, peer, targetChannelRef) {
			return responseError(fmt.Errorf("%w: target must be allowed by current channel delegate_targets policy", errorCode(a2a.ErrorUnauthorizedTarget))), nil
		}
		authorizationMode = "ephemeral_user_request"
		persistentDelegateTarget = false
	}
	req.SkillID = effectiveSkill
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return responseError(fmt.Errorf("%w: message is required", errorCode(a2a.ErrorInvalidEnvelope))), nil
	}
	delegationDepth, err := s.nextDelegationDepth()
	if err != nil {
		return responseError(err), nil
	}
	if err := s.checkOutboundQuota(ctx, req); err != nil {
		return responseError(err), nil
	}
	defaults := collaborationDefaultsForPeer(policy.ChannelRef, targetChannelRef, req, peer)
	resultVisibility := defaults.ResultVisibility
	transcriptMode := defaults.TranscriptMode
	deliveryReason := defaults.Reason
	if strings.TrimSpace(req.ResultVisibility) != "" || strings.TrimSpace(req.TranscriptMode) != "" {
		resultVisibility = firstNonEmpty(req.ResultVisibility, resultVisibility)
		transcriptMode = firstNonEmpty(req.TranscriptMode, transcriptMode)
		deliveryReason = "explicit delivery settings"
	}
	switch normalizeExplicitSetupMode(req.SetupMode) {
	case "co_present":
		if !coPresentContextAllowed(policy.ChannelRef, targetChannelRef, req, peer) {
			return responseError(fmt.Errorf("%w: co_present delegation requires the peer to be in the same Discord channel/thread or the same shared runtime channel_ref", errorCode(a2a.ErrorPolicyDenied))), nil
		}
		resultVisibility, transcriptMode = "transparent", "co_present"
		deliveryReason = "explicit co_present setup mode"
	case "safe":
		resultVisibility, transcriptMode = "proxy", "delegator"
		deliveryReason = "explicit safe setup mode"
	case "auto":
		resultVisibility, transcriptMode, deliveryReason = defaults.ResultVisibility, defaults.TranscriptMode, defaults.Reason
	}
	changeID := hashString("delegate:" + req.GuildID + ":" + req.ChannelID + ":" + targetChannelRef + ":" + string(target) + ":" + req.SkillID + ":" + resultVisibility + ":" + transcriptMode + ":" + message)
	needsConfirmation := req.RequiresConfirmation || s.cfg.Config.RequireConfirmationForRemote
	if needsConfirmation && strings.TrimSpace(req.ConfirmationToken) == "" {
		exp := s.cfg.Now().UTC().Add(10 * time.Minute)
		meta := deliveryResponseMetadata(resultVisibility, transcriptMode, deliveryReason, deliveryChannelID(req.ChannelID))
		meta["authorization_mode"] = authorizationMode
		meta["persistent_delegate_target"] = persistentDelegateTarget
		return A2AToolResponse{OK: true, Message: "A2A delegation requires confirmation", RequiresConfirmation: true, ConfirmationSummary: fmt.Sprintf("Delegate %q to %s@%s/%s via %s/%s (%s)", truncateForSummary(message), target, targetChannelRef, req.SkillID, resultVisibility, transcriptMode, deliveryReason), RiskLabels: []string{"remote_task", "data_egress"}, ExpiresAt: exp.Format(time.RFC3339), ChangeID: changeID, ConfirmationToken: s.confirmationToken("delegate", changeID, req, message), Metadata: meta}, nil
	}
	if needsConfirmation {
		if err := s.verifyConfirmation("delegate", changeID, req, message); err != nil {
			return responseError(err), nil
		}
	}
	pub, err := s.publisherFor(ctx)
	if err != nil {
		return responseError(err), nil
	}
	payload, _ := json.Marshal(map[string]string{"kind": "text", "text": message})
	msgID := a2a.MessageID("msg_" + randomToken(12))
	source := sourceAgentForRuntimeMode(s.cfg.Config, policy)
	originRef := s.originRuntimeRef(req, policy, source, msgID)
	delivery := deliveryOptionsForDelegate(req, resultVisibility, transcriptMode, source, s.cfg.Config.TaskTimeoutSec, delegationDepth)
	taskReq := a2a.TaskExecutionRequest{
		MessageID:             msgID,
		ClientTaskRef:         req.RequestedByID,
		ContextID:             "discord:" + req.ChannelID + ":" + string(msgID),
		From:                  source,
		To:                    target,
		ChannelID:             req.ChannelID,
		GuildID:               req.GuildID,
		ChannelRef:            targetChannelRef,
		SkillID:               req.SkillID,
		UserVisibleSummary:    truncateForSummary(message),
		Payload:               payload,
		Delivery:              delivery,
		ResultVisibility:      resultVisibility,
		DiscordTranscriptMode: transcriptMode,
		OriginRequester: a2a.OriginRequester{
			DiscordUserID:   strings.TrimSpace(req.RequestedByID),
			DiscordUsername: strings.TrimSpace(req.RequestedBy),
			DiscordGuildID:  strings.TrimSpace(req.GuildID),
		},
		OriginRuntimeRef: originRef,
	}
	discordTargetID, discordParentChannelID, discordThreadID := auditDiscordFields(req, delivery)
	row, err := pub.SendTask(ctx, taskReq)
	if err != nil {
		meta := a2a.AuditMetadata(a2a.AuditMetadataInput{
			MessageID:              msgID,
			ClientTaskRef:          req.RequestedByID,
			ContextID:              taskReq.ContextID,
			FromAgent:              taskReq.From,
			ToAgent:                taskReq.To,
			ChannelID:              req.ChannelID,
			GuildID:                req.GuildID,
			ChannelRef:             targetChannelRef,
			SkillID:                req.SkillID,
			ResultVisibility:       taskReq.ResultVisibility,
			DiscordTranscriptMode:  taskReq.DiscordTranscriptMode,
			DiscordTargetID:        discordTargetID,
			DiscordParentChannelID: discordParentChannelID,
			DiscordThreadID:        discordThreadID,
			ActorAgentID:           s.cfg.Config.AgentID,
			ActorDiscordUserID:     req.RequestedByID,
			ErrorCode:              a2a.ErrorNATSPublishFailed,
			PayloadSize:            len(payload),
		})
		meta["authorization_mode"] = authorizationMode
		meta["persistent_delegate_target"] = persistentDelegateTarget
		_ = s.recordAudit(ctx, a2a.AuditTaskPublishFailed, req, "error", err.Error(), meta)
		return responseError(fmt.Errorf("%w: %v", errorCode(a2a.ErrorNATSPublishFailed), err)), nil
	}
	meta := a2a.AuditMetadata(a2a.AuditMetadataInput{
		TaskID:                 row.TaskID,
		ClientTaskRef:          row.ClientTaskRef,
		MessageID:              msgID,
		ContextID:              row.ContextID,
		FromAgent:              row.FromAgent,
		ToAgent:                row.ToAgent,
		ExecutorAgent:          row.ExecutorAgent,
		ChannelID:              req.ChannelID,
		GuildID:                req.GuildID,
		ChannelRef:             targetChannelRef,
		SkillID:                req.SkillID,
		State:                  row.State,
		Revision:               row.Revision,
		ResultVisibility:       row.ResultVisibility,
		DiscordTranscriptMode:  row.DiscordTranscriptMode,
		DiscordTargetID:        discordTargetID,
		DiscordParentChannelID: discordParentChannelID,
		DiscordThreadID:        discordThreadID,
		ActorAgentID:           s.cfg.Config.AgentID,
		ActorDiscordUserID:     req.RequestedByID,
		PayloadSize:            len(payload),
	})
	meta["authorization_mode"] = authorizationMode
	meta["persistent_delegate_target"] = persistentDelegateTarget
	_ = s.recordAudit(ctx, a2a.AuditTaskSendRequested, req, "queued", "", meta)
	sum := summarizeTask(row)
	metaOut := deliveryResponseMetadata(resultVisibility, transcriptMode, deliveryReason, delivery.DiscordReplyThreadID)
	metaOut["origin_runtime_ref"] = taskReq.OriginRuntimeRef
	metaOut["success_confirmed"] = false
	metaOut["authorization_mode"] = authorizationMode
	metaOut["persistent_delegate_target"] = persistentDelegateTarget
	metaOut["must_check_status"] = true
	return A2AToolResponse{OK: true, Message: delegateSuccessMessage(resultVisibility, transcriptMode, deliveryReason), Task: &sum, Metadata: metaOut}, nil
}

func (s *A2AService) nextDelegationDepth() (int, error) {
	state, ok := currentTargetState()
	if ok && state.RemoteA2A {
		if state.DelegationDepth <= 0 {
			return 0, fmt.Errorf("%w: A2A delegation depth exhausted", errorCode(a2a.ErrorPolicyDenied))
		}
		return state.DelegationDepth - 1, nil
	}
	return s.cfg.Config.MaxDelegationDepth, nil
}

func sourceAgentForRuntimeMode(cfg a2a.Config, policy a2a.ChannelA2APolicy) a2a.AgentID {
	if cfg.RuntimeIDMode.UsesRuntimeIDs() && strings.TrimSpace(policy.RuntimeAgentID) != "" {
		return a2a.AgentID(strings.TrimSpace(policy.RuntimeAgentID))
	}
	return cfg.AgentID
}

func (s *A2AService) originRuntimeRef(req A2AToolRequest, policy a2a.ChannelA2APolicy, source a2a.AgentID, msgID a2a.MessageID) a2a.OriginRuntimeRef {
	channelRef := strings.TrimSpace(policy.ChannelRef)
	if channelRef == "" {
		channelRef = defaultChannelRef(req, s.cfg.DataDir)
	}
	displayName := channelNameFromMetadata(s.cfg.DataDir, req.ChannelID)
	if strings.TrimSpace(displayName) == "" {
		displayName = channelRef
	}
	return a2a.OriginRuntimeRef{
		RuntimeAgentID:   source,
		BotAgentID:       s.cfg.Config.AgentID,
		ChannelRef:       channelRef,
		DisplayName:      displayName,
		DiscordGuildID:   strings.TrimSpace(req.GuildID),
		DiscordChannelID: strings.TrimSpace(req.ChannelID),
		DiscordThreadID:  strings.TrimSpace(deliveryChannelID(req.ChannelID)),
		MessageID:        string(msgID),
	}
}

func (s *A2AService) Cancel(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	return s.publishTaskControl(ctx, req, a2a.ControlKindCancel, a2a.TaskStateCanceled, map[string]any{"reason": strings.TrimSpace(req.Reason)})
}

func (s *A2AService) InputReply(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if strings.TrimSpace(req.Input) == "" {
		return responseError(fmt.Errorf("%w: input is required", errorCode(a2a.ErrorInputNotExpected))), nil
	}
	return s.publishTaskControl(ctx, req, a2a.ControlKindInputReply, a2a.TaskStateInputRequired, map[string]any{"input": secrets.RedactEnv(req.Input)})
}

func (s *A2AService) AuthReply(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	payload := map[string]any{"approve": req.Approve, "denyReason": strings.TrimSpace(req.DenyReason)}
	return s.publishTaskControl(ctx, req, a2a.ControlKindAuthReply, a2a.TaskStateAuthRequired, payload)
}

func (s *A2AService) publishTaskControl(ctx context.Context, req A2AToolRequest, kind string, expected a2a.TaskState, payload map[string]any) (A2AToolResponse, error) {
	if err := s.validateContext(req, false); err != nil {
		return responseError(err), nil
	}
	row, err := s.lookupOutboundTask(ctx, req)
	if err != nil {
		return responseError(err), nil
	}
	if !req.ManageChannels && row.ClientTaskRef != req.RequestedByID {
		return responseError(fmt.Errorf("%w: requester does not own this task", errorCode(a2a.ErrorCancelNotAllowed))), nil
	}
	if row.Terminal {
		return responseError(fmt.Errorf("%w: task is terminal", errorCode(a2a.ErrorTaskTerminal))), nil
	}
	if expected != "" && expected != a2a.TaskStateCanceled && row.State != expected {
		return responseError(fmt.Errorf("%w: task state is %s", errorCode(a2a.ErrorInputNotExpected), row.State)), nil
	}
	pub, err := s.publisherFor(ctx)
	if err != nil {
		return responseError(err), nil
	}
	if row.FromAgent != "" {
		pub = pub.WithFrom(row.FromAgent)
	}
	raw, _ := json.Marshal(payload)
	to := row.ExecutorAgent
	if to == "" {
		to = row.ToAgent
	}
	if err := pub.PublishControl(ctx, to, row.TaskID, kind, row.Revision+1, a2a.ControlPayload{A2A: raw, ClientTaskRef: row.ClientTaskRef, Reason: req.Reason}); err != nil {
		return responseError(fmt.Errorf("%w: %v", errorCode(a2a.ErrorInternal), err)), nil
	}
	_ = s.recordAudit(ctx, "a2a_control_sent", req, "sent", "", map[string]any{"task_id": row.TaskID, "kind": kind})
	sum := summarizeTask(row)
	return A2AToolResponse{OK: true, Message: "A2A control published", Task: &sum}, nil
}

func (s *A2AService) validateContext(req A2AToolRequest, manager bool) error {
	if strings.TrimSpace(req.GuildID) == "" || strings.TrimSpace(req.ChannelID) == "" || strings.TrimSpace(req.RequestedBy) == "" || strings.TrimSpace(req.RequestedByID) == "" {
		return fmt.Errorf("%w: guild_id, channel_id, requested_by, and requested_by_id are required", errorCode(a2a.ErrorPolicyDenied))
	}
	if s.cfg.BoundGuildID != "" && strings.TrimSpace(req.GuildID) != s.cfg.BoundGuildID {
		return fmt.Errorf("%w: guild_id does not match bound context", errorCode(a2a.ErrorPolicyDenied))
	}
	if s.cfg.BoundChannelID != "" && strings.TrimSpace(req.ChannelID) != s.cfg.BoundChannelID && strings.TrimSpace(req.ChannelID) != s.cfg.BoundTargetID {
		return fmt.Errorf("%w: channel_id does not match bound context", errorCode(a2a.ErrorPolicyDenied))
	}
	if state, ok := currentTargetState(); ok {
		if requesterID := strings.TrimSpace(state.RequesterID); requesterID != "" && strings.TrimSpace(req.RequestedByID) != requesterID {
			return fmt.Errorf("%w: requested_by_id does not match bound requester", errorCode(a2a.ErrorPolicyDenied))
		}
		if requesterName := strings.TrimSpace(state.RequesterName); requesterName != "" && strings.TrimSpace(req.RequestedBy) != requesterName {
			return fmt.Errorf("%w: requested_by does not match bound requester", errorCode(a2a.ErrorPolicyDenied))
		}
	}
	if manager && !req.ManageChannels {
		return fmt.Errorf("%w: ManageChannels is required", errorCode(a2a.ErrorPolicyDenied))
	}
	return nil
}
func (s *A2AService) trustDelegatedPeers(ctx context.Context, agents []string) {
	for _, raw := range agents {
		agent := a2a.AgentID(strings.TrimSpace(raw))
		if err := a2a.ValidateAgentID(agent); err != nil {
			continue
		}
		if err := s.peers.SetTrusted(ctx, agent, true); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("[a2a] trust delegated peer %s failed: %v", agent, err)
		}
	}
}

func delegatedPeerAgents(policy a2a.ChannelA2APolicy) []string {
	agents := append([]string{}, policy.DelegateTo...)
	for _, target := range policy.DelegateTargets {
		if runtimeID := strings.TrimSpace(target.RuntimeAgentID); runtimeID != "" {
			agents = append(agents, runtimeID)
		}
	}
	return agents
}

func (s *A2AService) currentPolicy(ctx context.Context, req A2AToolRequest) (a2a.ChannelA2APolicy, error) {
	policy, err := s.policies.Get(ctx, strings.TrimSpace(req.GuildID), strings.TrimSpace(req.ChannelID))
	if err == nil {
		return policy, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return defaultPolicy(req, s.cfg.Config, s.cfg.DataDir), nil
	}
	return a2a.ChannelA2APolicy{}, fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)
}

func defaultPolicy(req A2AToolRequest, cfg a2a.Config, dataDir string) a2a.ChannelA2APolicy {
	channelRef := defaultChannelRef(req, dataDir)
	policy := a2a.ChannelA2APolicy{GuildID: strings.TrimSpace(req.GuildID), ChannelID: strings.TrimSpace(req.ChannelID), ChannelRef: channelRef, BotAgentID: string(cfg.AgentID), ResultVisibility: "proxy", DiscordTranscriptMode: "delegator"}
	stableKey := runtimeStableKey(req.GuildID, req.ChannelID, "", channelRef)
	if runtime, err := a2a.GenerateRuntimeAgentIDFromAlias(cfg.AgentID, channelRef, stableKey); err == nil {
		policy.RuntimeAgentID = string(runtime)
	}
	return policy
}

func defaultChannelRef(req A2AToolRequest, dataDir string) string {
	if explicit := strings.TrimSpace(req.ChannelRef); explicit != "" {
		return explicit
	}

	if name := channelNameFromMetadata(dataDir, req.ChannelID); name != "" {
		return a2a.RuntimeAlias(name, runtimeStableKey(req.GuildID, req.ChannelID, "", name))
	}
	return a2a.RuntimeAlias("", runtimeStableKey(req.GuildID, req.ChannelID, "", "channel"))
}
func canonicalRuntimeChannelRefs(dataDir, guildID string) map[string]string {
	out := map[string]string{}
	entries, err := channelmeta.List(dataDir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.Type) != "channel" || strings.TrimSpace(entry.GuildID) != strings.TrimSpace(guildID) {
			continue
		}
		ref := a2a.RuntimeAlias(entry.Name, runtimeStableKey(entry.GuildID, entry.ID, "", entry.Name))
		if ref != "" {
			out[strings.TrimSpace(entry.ID)] = ref
		}
	}
	return out
}

func channelNameFromMetadata(dataDir, channelID string) string {
	entries, err := channelmeta.Read(dataDir)
	if err != nil {
		return ""
	}
	if entry, ok := entries[strings.TrimSpace(channelID)]; ok {
		return strings.TrimSpace(entry.Name)
	}
	return ""
}

func runtimeStableKey(guildID, channelID, threadID, alias string) string {
	return strings.Join([]string{strings.TrimSpace(guildID), strings.TrimSpace(channelID), strings.TrimSpace(threadID), strings.TrimSpace(alias)}, "\x00")
}

func (s *A2AService) applyPolicyDiff(policy a2a.ChannelA2APolicy, req A2AToolRequest) a2a.ChannelA2APolicy {
	explicitChannelRef := strings.TrimSpace(req.ChannelRef) != ""
	req = applyA2APolicyRequestDefaults(req, policy)
	if req.Enable != nil {
		policy.Enabled = *req.Enable
	}
	if v := strings.TrimSpace(req.ChannelRef); v != "" {
		policy.ChannelRef = v
	}
	if policy.Enabled {
		policy.Discoverable = true
		if strings.TrimSpace(policy.BotAgentID) == "" {
			policy.BotAgentID = string(s.cfg.Config.AgentID)
		}
		if !explicitChannelRef && s.cfg.Config.RuntimeIDMode == a2a.RuntimeIDModeRuntime {
			if name := channelNameFromMetadata(s.cfg.DataDir, policy.ChannelID); name != "" {
				next := a2a.RuntimeAlias(name, runtimeStableKey(policy.GuildID, policy.ChannelID, "", name))
				if next != "" && next != policy.ChannelRef {
					policy.ChannelRef = next
					policy.RuntimeAgentID = ""
				}
			}
		}
		if strings.TrimSpace(policy.ChannelRef) == "" || strings.HasPrefix(policy.ChannelRef, "discord-") {
			policy.ChannelRef = defaultChannelRef(req, s.cfg.DataDir)
		}
		if strings.TrimSpace(policy.RuntimeAgentID) == "" {
			stableKey := runtimeStableKey(policy.GuildID, policy.ChannelID, "", policy.ChannelRef)
			if runtime, err := a2a.GenerateRuntimeAgentIDFromAlias(s.cfg.Config.AgentID, policy.ChannelRef, stableKey); err == nil {
				policy.RuntimeAgentID = string(runtime)
			}
		}
	}
	policy.AcceptFrom = appendUnique(policy.AcceptFrom, req.AcceptFrom...)
	policy.AcceptFromRuntimes = appendUnique(policy.AcceptFromRuntimes, req.AcceptFromRuntimes...)
	policy.AcceptSkills = appendUnique(policy.AcceptSkills, req.AcceptSkills...)
	for _, skill := range req.ExposeSkills {
		if skill = strings.TrimSpace(skill); skill != "" {
			policy.ExposeSkills = upsertExposeSkill(policy.ExposeSkills, skill)
		}
	}
	if req.PolicyAction == "undelegate-to" {
		policy.DelegateTo = removeStrings(policy.DelegateTo, req.DelegateTo...)
		policy.DelegateSkills = removeStrings(policy.DelegateSkills, req.DelegateSkills...)
		policy.DelegateTargets = removeDelegateTargets(policy.DelegateTargets, req.DelegateTo, strings.TrimSpace(req.TargetChannelRef), req.SkillID)
	} else {
		policy.DelegateTo = appendUnique(policy.DelegateTo, req.DelegateTo...)
		policy.DelegateSkills = appendUnique(policy.DelegateSkills, req.DelegateSkills...)
		if len(req.DelegateTo) > 0 || strings.TrimSpace(req.TargetAgent) != "" {
			agents := req.DelegateTo
			if len(agents) == 0 {
				agents = []string{req.TargetAgent}
			}
			targetSkill := strings.TrimSpace(req.SkillID)
			if targetSkill == "" {
				targetSkill = "task"
			}
			for _, agent := range agents {
				policy.DelegateTargets = upsertDelegateTarget(policy.DelegateTargets, a2a.DelegateTargetPolicy{
					AgentID:    strings.TrimSpace(agent),
					ChannelRef: req.targetRuntimeRef(policy.ChannelRef),
					SkillID:    targetSkill,
				})
			}
		}
	}
	if len(req.DelegateMediaTypes) > 0 {
		policy.DelegateMedia.AllowedMIMETypes = appendUnique(policy.DelegateMedia.AllowedMIMETypes, req.DelegateMediaTypes...)
	}
	if req.DelegateMaxBytes > 0 {
		policy.DelegateMedia.MaxBytes = req.DelegateMaxBytes
	}
	if req.MaxConcurrent != nil {
		policy.MaxConcurrent = *req.MaxConcurrent
	}
	if v := strings.TrimSpace(req.ResultVisibility); v != "" {
		policy.ResultVisibility = v
	}
	if v := strings.TrimSpace(req.TranscriptMode); v != "" {
		policy.DiscordTranscriptMode = v
	}
	if req.ShareDiscordContext != nil {
		policy.ShareDiscordContext = *req.ShareDiscordContext
	}
	policy.CoPresentFrom = appendUnique(policy.CoPresentFrom, req.CoPresentFrom...)
	policy.CoPresentFromRuntimes = appendUnique(policy.CoPresentFromRuntimes, req.CoPresentFromRuntimes...)
	policy.CoPresentTargetChannels = appendUnique(policy.CoPresentTargetChannels, req.CoPresentTargetChannels...)
	if req.AllowMemoryWrite != nil {
		policy.RemoteToolPolicy.AllowMemoryWrite = *req.AllowMemoryWrite
	}
	return policy
}

func applyPolicyDiff(policy a2a.ChannelA2APolicy, req A2AToolRequest) a2a.ChannelA2APolicy {
	cfg := a2a.Config{AgentID: a2a.AgentID(strings.TrimSpace(policy.BotAgentID))}
	if cfg.AgentID == "" {
		cfg.AgentID = "bot"
	}
	return (&A2AService{cfg: A2AServiceConfig{Config: cfg}}).applyPolicyDiff(policy, req)
}

func applyA2APolicyRequestDefaults(req A2AToolRequest, policy a2a.ChannelA2APolicy) A2AToolRequest {
	if strings.TrimSpace(req.TargetAgent) == "" || strings.TrimSpace(req.SkillID) == "" {
		return req
	}
	if req.Enable == nil {
		enable := true
		req.Enable = &enable
	}
	if strings.TrimSpace(req.ChannelRef) == "" {
		req.ChannelRef = policy.ChannelRef
	}
	req.AcceptFrom = appendUnique(req.AcceptFrom, req.TargetAgent)
	req.DelegateTo = appendUnique(req.DelegateTo, req.TargetAgent)
	req.CoPresentFrom = appendUnique(req.CoPresentFrom, req.TargetAgent)
	req.AcceptFromRuntimes = appendUnique(req.AcceptFromRuntimes, req.TargetAgent)
	req.CoPresentFromRuntimes = appendUnique(req.CoPresentFromRuntimes, req.TargetAgent)
	localSkill := string(a2a.SkillSlug(req.SkillID))
	req.AcceptSkills = appendUnique(req.AcceptSkills, localSkill)
	if defaultTaskSkillCompatible(localSkill, "task") {
		req.AcceptSkills = appendUnique(req.AcceptSkills, "task", "general_task")
	}
	req.ExposeSkills = appendUnique(req.ExposeSkills, localSkill)
	req.DelegateSkills = appendUnique(req.DelegateSkills, req.SkillID)
	mode := normalizeSetupMode(req.SetupMode)
	req.SetupMode = mode
	if mode == "co_present" {
		req.TranscriptMode = "co_present"
		req.ResultVisibility = "transparent"
		share := true
		req.ShareDiscordContext = &share
	} else if mode == "safe" {
		req.TranscriptMode = "delegator"
		req.ResultVisibility = "proxy"
		share := false
		req.ShareDiscordContext = &share
	} else if mode == "auto" {
		defaults := collaborationDefaultsForRefs(policy.ChannelRef, req.targetRuntimeRef(""))
		req.TranscriptMode = defaults.TranscriptMode
		req.ResultVisibility = defaults.ResultVisibility
		req.ShareDiscordContext = &defaults.ShareDiscordContext
	}
	return req
}

func (s *A2AService) applyTrustPeerDiff(ctx context.Context, policy a2a.ChannelA2APolicy, req A2AToolRequest) (a2a.ChannelA2APolicy, error) {
	peer := strings.TrimSpace(req.TargetAgent)
	if peer == "" && len(req.DelegateTo) > 0 {
		peer = strings.TrimSpace(req.DelegateTo[0])
	}
	if peer == "" && len(req.AcceptFrom) > 0 {
		peer = strings.TrimSpace(req.AcceptFrom[0])
	}
	if err := a2a.ValidateAgentID(a2a.AgentID(peer)); err != nil {
		return a2a.ChannelA2APolicy{}, fmt.Errorf("%w: peer_agent is required and must be a valid agent id: %v", errorCode(a2a.ErrorPolicyDenied), err)
	}
	relationship, err := normalizeTrustRelationship(req.TrustRelationship)
	if err != nil {
		return a2a.ChannelA2APolicy{}, err
	}
	mode := normalizeSetupMode(req.SetupMode)
	if strings.TrimSpace(req.SetupMode) != "" && mode == "" {
		return a2a.ChannelA2APolicy{}, fmt.Errorf("%w: setup_mode must be auto, safe, or co_present", errorCode(a2a.ErrorPolicyDenied))
	}
	peerRow, peerKnown := s.lookupPeer(ctx, peer)
	targetSkill := strings.TrimSpace(req.SkillID)
	targetChannelRef := ""
	if relationship == "outbound" || relationship == "bidirectional" {
		if targetSkill == "" && peerKnown {
			targetSkill = canonicalPeerDefaultTaskSkill(peerRow, req.targetRuntimeRef(""))
		}
		if targetSkill == "" {
			targetSkill = "task"
		}
		targetChannelRef, err = s.trustPeerTargetChannelRef(ctx, req, peer, targetSkill)
		if err != nil {
			return a2a.ChannelA2APolicy{}, err
		}
		if peerKnown {
			if canonical := canonicalPeerSkill(peerRow, targetSkill, targetChannelRef); canonical != "" {
				targetSkill = canonical
			}
		}
	}
	if mode == "auto" {
		defaults := collaborationDefaultsForRefs(policy.ChannelRef, targetChannelRef)
		if peerKnown {
			defaults = collaborationDefaultsForPeer(policy.ChannelRef, targetChannelRef, req, peerRow)
		}
		if defaults.TranscriptMode == "co_present" {
			mode = "co_present"
		} else {
			mode = "mirror"
		}
	}
	if mode == "co_present" {
		if peerKnown {
			if !coPresentContextAllowed(policy.ChannelRef, targetChannelRef, req, peerRow) {
				return a2a.ChannelA2APolicy{}, fmt.Errorf("%w: co_present trust requires the peer to be in the same Discord channel/thread or the same shared runtime channel_ref", errorCode(a2a.ErrorPolicyDenied))
			}
		} else if strings.TrimSpace(targetChannelRef) == "" || strings.TrimSpace(targetChannelRef) != strings.TrimSpace(policy.ChannelRef) {
			return a2a.ChannelA2APolicy{}, fmt.Errorf("%w: co_present trust requires discovered same-channel peer metadata or an explicit shared runtime channel_ref", errorCode(a2a.ErrorPolicyDenied))
		}
	}
	if relationship == "outbound" && mode == "co_present" {
		return a2a.ChannelA2APolicy{}, fmt.Errorf("%w: co_present trust must be inbound or bidirectional because shared Discord context is an inbound admission grant", errorCode(a2a.ErrorPolicyDenied))
	}
	enable := true
	policy = s.applyPolicyDiff(policy, A2AToolRequest{
		GuildID:        req.GuildID,
		ChannelID:      req.ChannelID,
		RequestedBy:    req.RequestedBy,
		RequestedByID:  req.RequestedByID,
		ManageChannels: req.ManageChannels,
		ChannelRef:     req.ChannelRef,
		Enable:         &enable,
	})
	if req.MaxConcurrent != nil {
		policy.MaxConcurrent = *req.MaxConcurrent
	} else if policy.MaxConcurrent == 0 {
		policy.MaxConcurrent = 1
	}
	policy.RemoteToolPolicy.AllowMemoryWrite = false
	if relationship == "inbound" || relationship == "bidirectional" {
		policy.AcceptFromRuntimes = appendUnique(policy.AcceptFromRuntimes, peer)
	}
	if relationship == "bidirectional" {
		policy.AcceptFrom = appendUnique(policy.AcceptFrom, peer)
	}
	if relationship == "outbound" || relationship == "bidirectional" {
		policy.DelegateTo = appendUnique(policy.DelegateTo, peer)
		policy.DelegateSkills = appendUnique(policy.DelegateSkills, targetSkill)
		policy.DelegateTargets = upsertDelegateTarget(policy.DelegateTargets, a2a.DelegateTargetPolicy{
			RuntimeAgentID: peer,
			ChannelRef:     targetChannelRef,
			SkillID:        targetSkill,
		})
	}
	if mode == "safe" {
		policy.DiscordTranscriptMode = "delegator"
		policy.ResultVisibility = "proxy"
		policy.ShareDiscordContext = false
	} else if mode == "co_present" {
		policy.DiscordTranscriptMode = "co_present"
		policy.ResultVisibility = "transparent"
		policy.ShareDiscordContext = true
		policy.CoPresentFromRuntimes = appendUnique(policy.CoPresentFromRuntimes, peer)
		if relationship == "bidirectional" {
			policy.CoPresentFrom = appendUnique(policy.CoPresentFrom, peer)
		}
	} else {
		policy.DiscordTranscriptMode = "mirror"
		policy.ResultVisibility = "transparent"
		policy.ShareDiscordContext = false
	}
	return policy, nil
}

func (s *A2AService) lookupPeer(ctx context.Context, peer string) (a2a.PeerRow, bool) {
	row, err := s.peers.Get(ctx, a2a.AgentID(peer))
	return row, err == nil
}

func (s *A2AService) trustPeerTargetChannelRef(ctx context.Context, req A2AToolRequest, peer string, skill string) (string, error) {
	if ref := req.targetRuntimeRef(""); ref != "" {
		return ref, nil
	}
	row, err := s.peers.Get(ctx, a2a.AgentID(peer))
	if err == nil {
		if ref := strings.TrimSpace(row.ExtendedCard.ChannelRef); ref != "" {
			return ref, nil
		}
		if ref := skillChannelRef(canonicalPeerSkill(row, skill, "")); ref != "" {
			return ref, nil
		}
	}
	return "", fmt.Errorf("%w: target_channel_ref is required when trusting outbound peer %s before its runtime channel_ref can be inferred from discovery", errorCode(a2a.ErrorPolicyDenied), peer)
}

func normalizeTrustRelationship(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "inbound", "accept", "receive":
		return "inbound", nil
	case "both", "bidirectional":
		return "bidirectional", nil
	case "outbound", "delegate", "send":
		return "outbound", nil
	default:
		return "", fmt.Errorf("%w: relationship must be inbound, outbound, or bidirectional", errorCode(a2a.ErrorPolicyDenied))
	}
}

func normalizeSetupMode(mode string) string {
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
func normalizeExplicitSetupMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return ""
	}
	return normalizeSetupMode(mode)
}

func (req A2AToolRequest) targetRuntimeRef(fallback string) string {
	if v := strings.TrimSpace(req.TargetChannelRef); v != "" {
		return v
	}
	if v := strings.TrimSpace(req.TargetThreadID); v != "" {
		return "discord-" + v
	}
	if v := strings.TrimSpace(req.TargetChannelID); v != "" {
		return "discord-" + v
	}
	return strings.TrimSpace(fallback)
}

func (req A2AToolRequest) hasExplicitTargetRuntimeRef() bool {
	return strings.TrimSpace(req.TargetChannelRef) != "" || strings.TrimSpace(req.TargetChannelID) != "" || strings.TrimSpace(req.TargetThreadID) != ""
}

func (s *A2AService) resolveDelegateTarget(policy a2a.ChannelA2APolicy, req A2AToolRequest, peer a2a.PeerRow) (string, string, bool, bool, error) {
	agent := strings.TrimSpace(req.TargetAgent)
	if agent == "" {
		return "", "", false, false, fmt.Errorf("%w: target_agent is required", errorCode(a2a.ErrorUnknownAgent))
	}
	requestedSkill := strings.TrimSpace(req.SkillID)
	explicitRef := req.targetRuntimeRef("")
	targetRef := explicitRef
	candidates := delegateTargetCandidates(policy, agent, requestedSkill)
	if targetRef == "" {
		if ref, ok, ambiguous := singleDelegateTargetChannelRef(candidates); ambiguous {
			return "", "", false, false, fmt.Errorf("%w: target_channel_ref is ambiguous for %s; specify target_channel_ref or skill_id", errorCode(a2a.ErrorUnauthorizedTarget), agent)
		} else if ok {
			targetRef = ref
		}
	}
	if targetRef == "" {
		targetRef = strings.TrimSpace(peer.ExtendedCard.ChannelRef)
	}
	if targetRef == "" && requestedSkill != "" {
		targetRef = skillChannelRef(requestedSkill)
	}
	if targetRef == "" && !ordinaryExplicitRuntimeTarget(req, peer) {
		targetRef = strings.TrimSpace(policy.ChannelRef)
	}

	effectiveSkill := ""
	if requestedSkill != "" {
		effectiveSkill = canonicalPeerSkill(peer, requestedSkill, targetRef)
		if effectiveSkill == "" {
			effectiveSkill = requestedSkill
		}
	} else if skill, ok, ambiguous := singleDelegateTargetSkill(candidates); ambiguous {
		return "", "", false, false, fmt.Errorf("%w: skill_id is ambiguous for %s; specify skill_id", errorCode(a2a.ErrorUnknownSkill), agent)
	} else if ok {
		effectiveSkill = canonicalPeerSkill(peer, skill, targetRef)
		if effectiveSkill == "" {
			effectiveSkill = strings.TrimSpace(skill)
		}
	} else {
		effectiveSkill = canonicalPeerDefaultTaskSkill(peer, targetRef)
		if effectiveSkill == "" {
			effectiveSkill = "task"
		}
	}
	if targetRef == "" {
		targetRef = skillChannelRef(effectiveSkill)
	}
	if ref := policyRuntimeTargetChannelRef(policy, agent, effectiveSkill); ref != "" {
		targetRef = ref
	}
	exact := policyDelegatesExactRuntime(policy, agent, effectiveSkill, targetRef)
	delegated := policyDelegatesRuntime(policy, agent, effectiveSkill, targetRef) || policyDelegatesSameDiscordChannelPeerRow(policy, peer, effectiveSkill)
	return targetRef, effectiveSkill, delegated, exact, nil
}

func delegateTargetCandidates(policy a2a.ChannelA2APolicy, agent, requestedSkill string) []a2a.DelegateTargetPolicy {
	var out []a2a.DelegateTargetPolicy
	for _, target := range policy.DelegateTargets {
		targetAgent := strings.TrimSpace(target.RuntimeAgentID)
		if targetAgent == "" {
			targetAgent = strings.TrimSpace(target.AgentID)
		}
		if targetAgent != strings.TrimSpace(agent) {
			continue
		}
		if strings.TrimSpace(requestedSkill) != "" && !skillListAllows([]string{target.SkillID}, requestedSkill) {
			continue
		}
		out = append(out, target)
	}
	return out
}

func singleDelegateTargetChannelRef(candidates []a2a.DelegateTargetPolicy) (string, bool, bool) {
	ref := ""
	for _, target := range candidates {
		next := strings.TrimSpace(target.ChannelRef)
		if next == "" || next == "*" {
			continue
		}
		if ref == "" {
			ref = next
			continue
		}
		if ref != next {
			return "", false, true
		}
	}
	return ref, ref != "", false
}

func singleDelegateTargetSkill(candidates []a2a.DelegateTargetPolicy) (string, bool, bool) {
	skill := ""
	for _, target := range candidates {
		next := strings.TrimSpace(target.SkillID)
		if next == "" {
			continue
		}
		if skill == "" {
			skill = next
			continue
		}
		if !skillListAllows([]string{skill}, next) || !skillListAllows([]string{next}, skill) {
			return "", false, true
		}
	}
	return skill, skill != "", false
}

func ordinaryExplicitRuntimeTarget(req A2AToolRequest, peer a2a.PeerRow) bool {
	if strings.TrimSpace(req.TargetAgent) == "" {
		return false
	}
	if strings.TrimSpace(peer.ExtendedCard.Runtime) == "channel" || strings.TrimSpace(peer.ExtendedCard.Runtime) == "thread" {
		return true
	}
	return strings.TrimSpace(peer.ExtendedCard.ChannelRef) != ""
}

func skillChannelRef(skillID string) string {
	before, _, ok := strings.Cut(strings.TrimSpace(skillID), "/")
	if !ok {
		return ""
	}
	return strings.TrimSpace(before)
}

type collaborationDefaults struct {
	ResultVisibility    string
	TranscriptMode      string
	ShareDiscordContext bool
	Reason              string
}

func collaborationDefaultsForRefs(sourceChannelRef, targetChannelRef string) collaborationDefaults {
	if strings.TrimSpace(targetChannelRef) != "" && strings.TrimSpace(targetChannelRef) == strings.TrimSpace(sourceChannelRef) {
		return collaborationDefaults{ResultVisibility: "transparent", TranscriptMode: "co_present", ShareDiscordContext: true, Reason: "same runtime channel_ref"}
	}
	return collaborationDefaults{ResultVisibility: "transparent", TranscriptMode: "mirror", ShareDiscordContext: false, Reason: "different runtime channel_ref or missing peer Discord metadata"}
}

func collaborationDefaultsForPeer(sourceChannelRef, targetChannelRef string, req A2AToolRequest, peer a2a.PeerRow) collaborationDefaults {
	if sameDiscordConversation(req, peer) {
		return collaborationDefaults{ResultVisibility: "transparent", TranscriptMode: "co_present", ShareDiscordContext: true, Reason: "same Discord channel verified from peer runtime card"}
	}
	return collaborationDefaults{ResultVisibility: "transparent", TranscriptMode: "mirror", ShareDiscordContext: false, Reason: "different Discord channel or missing peer metadata"}
}

func coPresentContextAllowed(sourceChannelRef, targetChannelRef string, req A2AToolRequest, peer a2a.PeerRow) bool {
	return sameDiscordConversation(req, peer) || (strings.TrimSpace(targetChannelRef) != "" && strings.TrimSpace(targetChannelRef) == strings.TrimSpace(sourceChannelRef))
}

func runtimeDeliveryDefaults(sourceChannelRef, targetChannelRef string) (string, string) {
	defaults := collaborationDefaultsForRefs(sourceChannelRef, targetChannelRef)
	return defaults.ResultVisibility, defaults.TranscriptMode
}

func auditDiscordFields(req A2AToolRequest, delivery a2a.DeliveryOptions) (string, string, string) {
	targetID := delivery.DiscordReplyThreadID
	if targetID == "" {
		targetID = delivery.DiscordReplyChannelID
	}
	parentChannelID := delivery.DiscordReplyChannelID
	threadID := delivery.DiscordReplyThreadID
	if strings.TrimSpace(threadID) == "" && strings.TrimSpace(req.TargetThreadID) != "" {
		threadID = strings.TrimSpace(req.TargetThreadID)
	}
	if strings.TrimSpace(parentChannelID) == "" && strings.TrimSpace(req.ChannelID) != "" {
		parentChannelID = strings.TrimSpace(req.ChannelID)
	}
	if strings.TrimSpace(targetID) == "" {
		if strings.TrimSpace(threadID) != "" {
			targetID = threadID
		} else {
			targetID = parentChannelID
		}
	}
	return strings.TrimSpace(targetID), strings.TrimSpace(parentChannelID), strings.TrimSpace(threadID)
}
func runtimeDeliveryDefaultsForPeer(sourceChannelRef, targetChannelRef string, req A2AToolRequest, peer a2a.PeerRow) (string, string, string) {
	defaults := collaborationDefaultsForPeer(sourceChannelRef, targetChannelRef, req, peer)
	return defaults.ResultVisibility, defaults.TranscriptMode, defaults.Reason
}

func sameDiscordConversation(req A2AToolRequest, peer a2a.PeerRow) bool {
	card := peer.ExtendedCard
	guildID := strings.TrimSpace(req.GuildID)
	channelID := strings.TrimSpace(req.ChannelID)
	peerChannelID := strings.TrimSpace(card.DiscordChannelID)
	peerThreadID := strings.TrimSpace(card.DiscordThreadID)
	if guildID == "" || channelID == "" || strings.TrimSpace(card.DiscordGuildID) != guildID {
		return false
	}
	if peerThreadID != "" && channelID == peerThreadID {
		return true
	}
	if peerChannelID != channelID {
		return false
	}
	localThreadID := strings.TrimSpace(deliveryChannelID(req.ChannelID))
	if peerThreadID == "" || localThreadID == "" || localThreadID == channelID {
		return true
	}
	return peerThreadID == localThreadID
}

func deliveryOptionsForDelegate(req A2AToolRequest, resultVisibility, transcriptMode string, source a2a.AgentID, timeoutSec, delegationDepth int) a2a.DeliveryOptions {
	delivery := a2a.DeliveryOptions{
		TimeoutSec:            timeoutSec,
		DiscordReplyChannelID: req.ChannelID,
		DiscordReplyThreadID:  deliveryChannelID(req.ChannelID),
		MaxDelegationDepth:    delegationDepth,
	}
	if resultVisibility == "transparent" && transcriptMode == "co_present" && strings.TrimSpace(req.GuildID) != "" && strings.TrimSpace(req.ChannelID) != "" {
		delivery.ShareDiscordContext = true
		delivery.CoPresentFrom = source
		ctx := a2a.DiscordContext{
			GuildID:   strings.TrimSpace(req.GuildID),
			ChannelID: strings.TrimSpace(req.ChannelID),
			ThreadID:  delivery.DiscordReplyThreadID,
		}
		delivery.DiscordContext = &ctx
		if raw, err := json.Marshal(ctx); err == nil {
			delivery.DiscordContextJSON = raw
		}
	}
	return delivery
}

func delegateSuccessMessage(resultVisibility, transcriptMode, reason string) string {
	if resultVisibility == "transparent" && transcriptMode == "co_present" {
		return "A2A request queued; executor owns the shared Discord thread, so this bot must not repost the result and should use task status to confirm terminal state"
	}
	if transcriptMode == "mirror" {
		return "A2A request queued; mirror mode may show executor transcript updates and task status confirms terminal state"
	}
	return "A2A request queued; durable task status must be checked before claiming the executor accepted or completed it"
}

func deliveryResponseMetadata(resultVisibility, transcriptMode, reason, discordThreadID string) map[string]interface{} {
	meta := map[string]interface{}{
		"result_visibility":       resultVisibility,
		"discord_transcript_mode": transcriptMode,
		"delivery_reason":         reason,
	}
	if strings.TrimSpace(discordThreadID) != "" {
		meta["discord_thread_id"] = strings.TrimSpace(discordThreadID)
	}
	if resultVisibility == "transparent" && transcriptMode == "co_present" {
		meta["follow_up_guidance"] = "executor owns the shared Discord transcript; do not repost, summarize, or paraphrase the result unless the user explicitly asks"
	}
	return meta
}

func (s *A2AService) directHumanEphemeralDelegateAllowed(req A2AToolRequest, peer a2a.PeerRow, targetChannelRef string) bool {
	if strings.TrimSpace(req.RequestedByID) == "" || strings.TrimSpace(targetChannelRef) == "" {
		return false
	}
	if peer.Stale {
		return false
	}
	runtimeKind := strings.TrimSpace(peer.ExtendedCard.Runtime)
	if runtimeKind != "channel" && runtimeKind != "thread" {
		return false
	}
	peerChannelRef := strings.TrimSpace(peer.ExtendedCard.ChannelRef)
	if peerChannelRef != "" && peerChannelRef != strings.TrimSpace(targetChannelRef) {
		return false
	}
	source := strings.TrimSpace(req.RequestSource)
	if state, ok := currentTargetState(); ok {
		if state.RemoteA2A {
			return false
		}
		if source == "" {
			source = strings.TrimSpace(state.Source)
		}
	}
	switch source {
	case "message", "thread", "slash":
		return true
	default:
		return false
	}
}

func policyDelegatesRuntime(policy a2a.ChannelA2APolicy, agent, skill, targetChannelRef string) bool {
	for _, target := range policy.DelegateTargets {
		targetAgent := strings.TrimSpace(target.RuntimeAgentID)
		if targetAgent == "" {
			targetAgent = strings.TrimSpace(target.AgentID)
		}
		if !stringListAllows([]string{targetAgent}, agent) {
			continue
		}
		if !skillListAllows([]string{target.SkillID}, skill) {
			continue
		}
		if target.RuntimeAgentID != "" || target.ChannelRef == "" || targetChannelRef == "" || target.ChannelRef == targetChannelRef || target.ChannelRef == "*" {
			return true
		}
	}
	return legacyDelegateAllowsTaskDefault(policy, agent, skill, targetChannelRef)
}

func legacyDelegateAllowsTaskDefault(policy a2a.ChannelA2APolicy, agent, skill, targetChannelRef string) bool {
	if strings.TrimSpace(targetChannelRef) != strings.TrimSpace(policy.ChannelRef) {
		return false
	}
	if !stringListAllows(policy.DelegateTo, agent) {
		return false
	}
	if skillListAllows(policy.DelegateSkills, skill) {
		return true
	}
	return a2a.SkillSlug(skill) == "task" && len(policy.DelegateSkills) == 0
}

func upsertDelegateTarget(targets []a2a.DelegateTargetPolicy, next a2a.DelegateTargetPolicy) []a2a.DelegateTargetPolicy {
	next.SkillID = strings.TrimSpace(next.SkillID)
	if next.SkillID == "" {
		next.SkillID = "task"
	}
	if next.AgentID == "" && next.RuntimeAgentID == "" {
		return targets
	}
	for i, target := range targets {
		if target.AgentID == next.AgentID && target.RuntimeAgentID == next.RuntimeAgentID && target.ChannelRef == next.ChannelRef && target.SkillID == next.SkillID {
			targets[i] = next
			return targets
		}
	}
	return append(targets, next)
}
func removeStrings(values []string, remove ...string) []string {
	if len(values) == 0 || len(remove) == 0 {
		return values
	}
	blocked := make(map[string]bool, len(remove))
	for _, value := range remove {
		if value = strings.TrimSpace(value); value != "" {
			blocked[value] = true
		}
	}
	out := values[:0]
	for _, value := range values {
		if !blocked[strings.TrimSpace(value)] {
			out = append(out, value)
		}
	}
	return out
}

func removeDelegateTargetsForPeer(targets []a2a.DelegateTargetPolicy, peer string) []a2a.DelegateTargetPolicy {
	if len(targets) == 0 {
		return targets
	}
	peer = strings.TrimSpace(peer)
	out := targets[:0]
	for _, target := range targets {
		if strings.TrimSpace(target.RuntimeAgentID) == peer || strings.TrimSpace(target.AgentID) == peer {
			continue
		}
		out = append(out, target)
	}
	return out
}

func removeDelegateTargets(targets []a2a.DelegateTargetPolicy, agents []string, channelRef, skillID string) []a2a.DelegateTargetPolicy {
	if len(targets) == 0 {
		return targets
	}
	agentSet := make(map[string]bool, len(agents))
	for _, agent := range agents {
		if agent = strings.TrimSpace(agent); agent != "" {
			agentSet[agent] = true
		}
	}
	channelRef = strings.TrimSpace(channelRef)
	skillID = strings.TrimSpace(skillID)
	out := targets[:0]
	for _, target := range targets {
		if agentSet[strings.TrimSpace(target.AgentID)] && (channelRef == "" || strings.TrimSpace(target.ChannelRef) == channelRef) && strings.TrimSpace(target.SkillID) == skillID {
			continue
		}
		out = append(out, target)
	}
	return out
}

func (s *A2AService) checkOutboundQuota(ctx context.Context, req A2AToolRequest) error {
	capGlobal := s.cfg.Config.MaxPendingTasks
	if capGlobal > 0 {
		count, err := s.tasks.CountOpenOutbound(ctx, "")
		if err != nil {
			return fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)
		}
		if count >= capGlobal {
			return fmt.Errorf("%w: A2A_MAX_PENDING_TASKS exceeded", errorCode(a2a.ErrorOverloaded))
		}
	}
	capChannel := s.cfg.Config.MaxOutboundTasksPerChannel
	if capChannel > 0 {
		count, err := s.tasks.CountOpenOutbound(ctx, req.ChannelID)
		if err != nil {
			return fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)
		}
		if count >= capChannel {
			return fmt.Errorf("%w: A2A_MAX_OUTBOUND_TASKS_PER_CHANNEL exceeded", errorCode(a2a.ErrorOverloaded))
		}
	}
	return nil
}

func (s *A2AService) lookupOutboundTaskOrMessage(ctx context.Context, id string) (a2a.TaskRow, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return a2a.TaskRow{}, sql.ErrNoRows
	}
	byTask, taskErr := s.tasks.GetByDirectionTaskID(ctx, "outbound", a2a.TaskID(id))
	byMessage, messageErr := s.tasks.GetByDirectionMessage(ctx, "outbound", a2a.MessageID(id))
	if taskErr == nil && messageErr == nil {
		if byTask.LocalID != byMessage.LocalID {
			return a2a.TaskRow{}, fmt.Errorf("%w: task_id/message_id is ambiguous", errorCode(a2a.ErrorTaskNotFound))
		}
		return byTask, nil
	}
	if taskErr == nil {
		return byTask, nil
	}
	if messageErr == nil {
		return byMessage, nil
	}
	if !errors.Is(taskErr, sql.ErrNoRows) {
		return a2a.TaskRow{}, taskErr
	}
	return a2a.TaskRow{}, messageErr
}

func (s *A2AService) lookupOutboundTask(ctx context.Context, req A2AToolRequest) (a2a.TaskRow, error) {
	if id := strings.TrimSpace(req.LocalID); id != "" {
		row, err := s.tasks.GetByLocalID(ctx, id)
		if err != nil {
			return a2a.TaskRow{}, taskLookupError(err)
		}
		return row, nil
	}
	if id := strings.TrimSpace(req.TaskID); id != "" {
		row, err := s.lookupOutboundTaskOrMessage(ctx, id)
		if err != nil {
			return a2a.TaskRow{}, taskLookupError(err)
		}
		return row, nil
	}
	if id := strings.TrimSpace(req.MessageID); id != "" {
		row, err := s.tasks.GetByDirectionMessage(ctx, "outbound", a2a.MessageID(id))
		if err != nil {
			return a2a.TaskRow{}, taskLookupError(err)
		}
		return row, nil
	}
	return a2a.TaskRow{}, fmt.Errorf("%w: task_id, message_id, or local_id is required", errorCode(a2a.ErrorTaskNotFound))
}

func (s *A2AService) publisherFor(ctx context.Context) (*a2a.Publisher, error) {
	if s.publisher != nil {
		return s.publisher, nil
	}
	if s.cfg.Config.AgentID == "" || !s.cfg.Config.Enabled() {
		return nil, fmt.Errorf("%w: A2A NATS is disabled", errorCode(a2a.ErrorChannelNotEnabled))
	}
	if s.node == nil {
		if !s.cfg.ConnectNATS {
			return nil, fmt.Errorf("%w: A2A NATS node is unavailable", errorCode(a2a.ErrorInternal))
		}
		node, err := a2a.Connect(ctx, a2a.NodeConfig{Config: s.cfg.Config})
		if err != nil {
			return nil, err
		}
		s.node = node
	}
	s.publisher = a2a.NewPublisher(s.node, s.tasks, s.cfg.Config.AgentID, s.cfg.Config.MaxEventRatePerMin)
	return s.publisher, nil
}

func (s *A2AService) confirmationToken(action, changeID string, req A2AToolRequest, payload any) string {
	exp := s.cfg.Now().UTC().Add(10 * time.Minute).Unix()
	hash := payloadHash(payload)
	cp := confirmationPayload{Action: action, ChangeID: changeID, GuildID: req.GuildID, ChannelID: req.ChannelID, UserID: req.RequestedByID, Hash: hash, ExpiresAt: exp}
	raw, _ := json.Marshal(cp)
	mac := hmac.New(sha256.New, []byte(s.cfg.ConfirmationSecret))
	mac.Write(raw)
	return hex.EncodeToString(raw) + "." + hex.EncodeToString(mac.Sum(nil))
}

func (s *A2AService) verifyConfirmation(action, changeID string, req A2AToolRequest, payload any) error {
	rawHex, sigHex, ok := strings.Cut(strings.TrimSpace(req.ConfirmationToken), ".")
	if !ok {
		return fmt.Errorf("%w: confirmation token is required", errorCode(a2a.ErrorPolicyDenied))
	}
	raw, err := hex.DecodeString(rawHex)
	if err != nil {
		return fmt.Errorf("%w: invalid confirmation token", errorCode(a2a.ErrorPolicyDenied))
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return fmt.Errorf("%w: invalid confirmation token", errorCode(a2a.ErrorPolicyDenied))
	}
	mac := hmac.New(sha256.New, []byte(s.cfg.ConfirmationSecret))
	mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return fmt.Errorf("%w: confirmation token signature mismatch", errorCode(a2a.ErrorPolicyDenied))
	}
	var cp confirmationPayload
	if err := json.Unmarshal(raw, &cp); err != nil {
		return fmt.Errorf("%w: invalid confirmation token", errorCode(a2a.ErrorPolicyDenied))
	}
	if cp.Action != action || cp.ChangeID != changeID || cp.GuildID != req.GuildID || cp.ChannelID != req.ChannelID || cp.UserID != req.RequestedByID || cp.Hash != payloadHash(payload) {
		return fmt.Errorf("%w: confirmation token scope mismatch", errorCode(a2a.ErrorPolicyDenied))
	}
	if s.cfg.Now().UTC().Unix() > cp.ExpiresAt {
		return fmt.Errorf("%w: confirmation token expired", errorCode(a2a.ErrorPolicyDenied))
	}
	return nil
}

func (s *A2AService) recordAudit(ctx context.Context, eventType string, req A2AToolRequest, status string, errText string, metadata map[string]any) error {
	if !s.cfg.AuditEnabled {
		return nil
	}
	store, err := audit.Open(audit.Config{DBPath: s.cfg.AuditDBPath, DataDir: s.cfg.DataDir, RecordContent: s.cfg.AuditRecordContent})
	if err != nil {
		return err
	}
	defer store.Close()
	return store.RecordBotEvent(ctx, audit.BotEvent{Type: eventType, GuildID: req.GuildID, ChannelID: req.ChannelID, TargetID: req.ChannelID, UserID: req.RequestedByID, Username: req.RequestedBy, Command: eventType, Source: "bot_a2a", Status: status, Error: errText, Metadata: metadata, OccurredAt: s.cfg.Now()})
}

var (
	fallbackConfirmationOnce  sync.Once
	fallbackConfirmationValue string
)

func fallbackConfirmationSecret() string {
	fallbackConfirmationOnce.Do(func() {
		var b [32]byte
		if _, err := rand.Read(b[:]); err != nil {
			panic(err)
		}
		fallbackConfirmationValue = hex.EncodeToString(b[:])
	})
	return fallbackConfirmationValue
}

func A2AConfigFromEnv() a2a.Config {
	return a2aConfigFromEnv()
}

func a2aConfigFromEnv() a2a.Config {
	return a2a.Config{
		NATSURL:                      os.Getenv("NATS_URL"),
		NATSCredsFile:                os.Getenv("NATS_CREDS_FILE"),
		NATSToken:                    os.Getenv("NATS_TOKEN"),
		NATSTLSCAFile:                os.Getenv("NATS_TLS_CA_FILE"),
		AgentID:                      a2a.AgentID(os.Getenv("A2A_AGENT_ID")),
		RuntimeIDMode:                a2a.RuntimeIDMode(os.Getenv("A2A_RUNTIME_ID_MODE")),
		AgentName:                    os.Getenv("A2A_AGENT_NAME"),
		AgentDescription:             os.Getenv("A2A_AGENT_DESCRIPTION"),
		TaskTimeoutSec:               envIntLocal("A2A_TASK_TIMEOUT_SEC", 3600),
		MaxDelegationDepth:           envIntLocal("A2A_MAX_DELEGATION_DEPTH", 1),
		AutoDelegateEnabled:          envBoolLocal("A2A_AUTO_DELEGATE_ENABLED", false),
		RequireConfirmationForRemote: envBoolLocal("A2A_REQUIRE_CONFIRMATION_FOR_REMOTE", true),
		ProductionSecurity:           envBoolLocal("A2A_PRODUCTION_SECURITY", false),
		TaskRetentionDays:            envIntLocal("A2A_TASK_RETENTION_DAYS", 30),
		ObjectRetentionDays:          envIntLocal("A2A_OBJECT_RETENTION_DAYS", 30),
		MaxPendingTasks:              envIntLocal("A2A_MAX_PENDING_TASKS", 100),
		MaxOutboundTasksPerChannel:   envIntLocal("A2A_MAX_OUTBOUND_TASKS_PER_CHANNEL", 10),
		MaxInboundTasksPerChannel:    envIntLocal("A2A_MAX_INBOUND_TASKS_PER_CHANNEL", 10),
		MaxEventRatePerMin:           envIntLocal("A2A_MAX_EVENT_RATE_PER_MIN", 120),
	}
}

func envIntLocal(key string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return def
	}
	return v
}
func envBoolLocal(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

type codedError struct {
	code a2a.ErrorCode
	msg  string
}

func (e codedError) Error() string            { return e.msg }
func errorCode(code a2a.ErrorCode) codedError { return codedError{code: code, msg: string(code)} }
func responseError(err error) A2AToolResponse {
	var ce codedError
	code := a2a.ErrorInternal
	if errors.As(err, &ce) {
		code = ce.code
	}
	return A2AToolResponse{OK: false, Message: err.Error(), ErrorCode: code}
}
func taskLookupError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: task not found", errorCode(a2a.ErrorTaskNotFound))
	}
	return fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)
}

func summarizeTask(row a2a.TaskRow) A2ATaskSummary {
	return A2ATaskSummary{LocalID: row.LocalID, TaskID: string(row.TaskID), MessageID: string(row.MessageID), Direction: row.Direction, FromAgent: string(row.FromAgent), ToAgent: string(row.ToAgent), ExecutorAgent: string(row.ExecutorAgent), ChannelID: row.ChannelID, ChannelRef: row.ChannelRef, SkillID: row.SkillID, ResultVisibility: row.ResultVisibility, DiscordTranscriptMode: row.DiscordTranscriptMode, State: row.State, Revision: row.Revision, Terminal: row.Terminal, ErrorCode: row.Error.Code, ErrorMessage: row.Error.Message, CreatedAt: row.CreatedAt.Format(time.RFC3339), UpdatedAt: row.UpdatedAt.Format(time.RFC3339), OriginRuntimeRef: row.OriginRuntimeRef}
}

func taskStatusMessage(row a2a.TaskRow) string {
	if suppressTaskStatusResultContent(row) {
		return "A2A task loaded; executor already owns the shared Discord transcript, so result text is omitted here. Do not post a follow-up, summary, or paraphrase unless the user explicitly asks"
	}
	return "A2A task loaded"
}

func taskStatusMetadata(row a2a.TaskRow) map[string]interface{} {
	if !suppressTaskStatusResultContent(row) {
		return nil
	}
	meta := map[string]interface{}{
		"follow_up_guidance": "executor owns the shared Discord transcript; do not repost, summarize, or paraphrase the result unless the user explicitly asks",
	}
	if strings.TrimSpace(row.DiscordContextJSON) != "" {
		var dc a2a.DiscordContext
		if json.Unmarshal([]byte(row.DiscordContextJSON), &dc) == nil && strings.TrimSpace(dc.ThreadID) != "" {
			meta["discord_thread_id"] = strings.TrimSpace(dc.ThreadID)
		}
	}
	return meta
}

func suppressTaskStatusResultContent(row a2a.TaskRow) bool {
	return strings.TrimSpace(row.ResultVisibility) == "transparent" && strings.TrimSpace(row.DiscordTranscriptMode) == "co_present"
}

func (s *A2AService) summarizeTaskWithEvents(ctx context.Context, row a2a.TaskRow) A2ATaskSummary {
	sum := summarizeTask(row)
	if row.TaskID == "" {
		return sum
	}
	events, err := s.tasks.ReplayEvents(ctx, row.TaskID, 0)
	if err != nil {
		return sum
	}
	sum.Events = make([]A2ATaskEventSummary, 0, len(events))
	suppressResultContent := suppressTaskStatusResultContent(row)
	for _, event := range events {
		eventSum := summarizeTaskEvent(event)
		if suppressResultContent && event.EventType == a2a.EventKindResult {
			eventSum.Content = ""
		}
		sum.Events = append(sum.Events, eventSum)
	}
	return sum
}

func summarizeTaskEvent(event a2a.EventRow) A2ATaskEventSummary {
	sum := A2ATaskEventSummary{Revision: event.Revision, EventType: event.EventType, State: event.State, CreatedAt: event.CreatedAt.Format(time.RFC3339)}
	var payload a2a.TaskEventPayload
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err == nil {
		if payload.State != "" {
			sum.State = payload.State
		}
		if payload.Content != "" {
			sum.Content = truncateForSummary(payload.Content)
		}
		if payload.Error.Code != "" {
			sum.ErrorCode = payload.Error.Code
			sum.ErrorMessage = payload.Error.Message
		}
		if payload.Result != nil {
			if payload.Result.State != "" {
				sum.State = payload.Result.State
			}
			if payload.Result.Content != "" {
				sum.Content = truncateForSummary(payload.Result.Content)
			}
			if payload.Result.Error.Code != "" {
				sum.ErrorCode = payload.Result.Error.Code
				sum.ErrorMessage = payload.Result.Error.Message
			}
		}
		return sum
	}
	var result a2a.TaskExecutionResult
	if err := json.Unmarshal([]byte(event.PayloadJSON), &result); err == nil {
		if result.State != "" {
			sum.State = result.State
		}
		if result.Content != "" {
			sum.Content = truncateForSummary(result.Content)
		}
		if result.Error.Code != "" {
			sum.ErrorCode = result.Error.Code
			sum.ErrorMessage = result.Error.Message
		}
	}
	return sum
}
func stringListAllows(list []string, value string) bool {
	value = strings.TrimSpace(value)
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "*" || item == value || (strings.HasSuffix(item, "*") && strings.HasPrefix(value, strings.TrimSuffix(item, "*"))) {
			return true
		}
	}
	return false
}

func appendUnique(base []string, add ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(base)+len(add))
	for _, item := range append(base, add...) {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func upsertExposeSkill(skills []a2a.SkillPolicy, id string) []a2a.SkillPolicy {
	for i := range skills {
		if skills[i].ID == id {
			return skills
		}
	}
	return append(skills, a2a.SkillPolicy{ID: id, InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}})
}

func peerHasSkill(peer a2a.PeerRow, skillID string) bool {
	slug := a2a.SkillSlug(skillID)
	for _, skill := range peer.Card.Skills {
		if skill.ID == skillID || a2a.SkillSlug(skill.ID) == slug || defaultTaskSkillCompatible(skill.ID, skillID) {
			return true
		}
	}
	return false
}

func canonicalPeerSkill(peer a2a.PeerRow, skillID, targetChannelRef string) string {
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return canonicalPeerDefaultTaskSkill(peer, targetChannelRef)
	}
	slug := a2a.SkillSlug(skillID)
	for _, skill := range peer.Card.Skills {
		if skill.ID == skillID {
			return skill.ID
		}
		if strings.TrimSpace(targetChannelRef) != "" && skill.ID == strings.TrimSpace(targetChannelRef)+"/"+slug {
			return skill.ID
		}
	}
	for _, skill := range peer.Card.Skills {
		if a2a.SkillSlug(skill.ID) == slug || defaultTaskSkillCompatible(skill.ID, skillID) {
			return skill.ID
		}
	}
	return ""
}

func canonicalPeerDefaultTaskSkill(peer a2a.PeerRow, targetChannelRef string) string {
	prefix := strings.TrimSpace(targetChannelRef)
	if prefix != "" {
		for _, skill := range peer.Card.Skills {
			if strings.TrimSpace(skill.ID) == prefix+"/task" || strings.TrimSpace(skill.ID) == prefix+"/general_task" {
				return skill.ID
			}
		}
	}
	for _, want := range []string{"task", "general_task"} {
		for _, skill := range peer.Card.Skills {
			if strings.TrimSpace(skill.ID) == want {
				return skill.ID
			}
		}
	}
	for _, skill := range peer.Card.Skills {
		if defaultTaskSkillCompatible(skill.ID, "task") {
			return skill.ID
		}
	}
	return ""
}

func defaultTaskSkillCompatible(a, b string) bool {
	aSlug := a2a.SkillSlug(a)
	bSlug := a2a.SkillSlug(b)
	return (aSlug == "task" || aSlug == "general_task") && (bSlug == "task" || bSlug == "general_task")
}

func (s *A2AService) storePendingPolicyPlan(changeID, baseChangeID, tool string, policy a2a.ChannelA2APolicy, expiresAt time.Time) error {
	changeID = strings.TrimSpace(changeID)
	if changeID == "" {
		return nil
	}
	plan := pendingPolicyPlan{BaseChangeID: baseChangeID, Tool: tool, Policy: policy, ExpiresAt: expiresAt}
	s.pendingMu.Lock()
	now := s.cfg.Now().UTC()
	for id, existing := range s.pendingPlan {
		if !existing.ExpiresAt.IsZero() && !existing.ExpiresAt.After(now) {
			delete(s.pendingPlan, id)
		}
	}
	s.pendingPlan[changeID] = plan
	s.pendingMu.Unlock()
	return s.persistPendingPolicyPlan(changeID, plan)
}

func (s *A2AService) pendingPolicyPlan(changeID string) (pendingPolicyPlan, bool) {
	changeID = strings.TrimSpace(changeID)
	if changeID == "" {
		return pendingPolicyPlan{}, false
	}
	s.pendingMu.Lock()
	plan, ok := s.pendingPlan[changeID]
	if ok && !plan.ExpiresAt.IsZero() && !plan.ExpiresAt.After(s.cfg.Now().UTC()) {
		delete(s.pendingPlan, changeID)
		ok = false
	}
	s.pendingMu.Unlock()
	if ok {
		return plan, true
	}
	plan, err := s.loadPendingPolicyPlan(changeID)
	if err != nil || (!plan.ExpiresAt.IsZero() && !plan.ExpiresAt.After(s.cfg.Now().UTC())) {
		return pendingPolicyPlan{}, false
	}
	s.pendingMu.Lock()
	s.pendingPlan[changeID] = plan
	s.pendingMu.Unlock()
	return plan, true
}

func (s *A2AService) pendingPolicyDB() (*sql.DB, error) {
	dir := filepath.Join(s.cfg.DataDir, "a2a")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "pending_policy_plans.sqlite"))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS pending_policy_plans (
		change_id TEXT PRIMARY KEY,
		base_change_id TEXT NOT NULL,
		tool TEXT NOT NULL,
		policy_json TEXT NOT NULL,
		expires_at TEXT NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`DELETE FROM pending_policy_plans WHERE expires_at <= ?`, s.cfg.Now().UTC().Format(time.RFC3339)); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (s *A2AService) persistPendingPolicyPlan(changeID string, plan pendingPolicyPlan) error {
	raw, err := json.Marshal(plan.Policy)
	if err != nil {
		return err
	}
	db, err := s.pendingPolicyDB()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO pending_policy_plans(change_id, base_change_id, tool, policy_json, expires_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(change_id) DO UPDATE SET base_change_id=excluded.base_change_id, tool=excluded.tool, policy_json=excluded.policy_json, expires_at=excluded.expires_at`,
		changeID, plan.BaseChangeID, plan.Tool, string(raw), plan.ExpiresAt.UTC().Format(time.RFC3339))
	return err
}

func (s *A2AService) loadPendingPolicyPlan(changeID string) (pendingPolicyPlan, error) {
	db, err := s.pendingPolicyDB()
	if err != nil {
		return pendingPolicyPlan{}, err
	}
	defer db.Close()
	var plan pendingPolicyPlan
	var raw, expires string
	err = db.QueryRow(`SELECT base_change_id, tool, policy_json, expires_at FROM pending_policy_plans WHERE change_id=?`, changeID).Scan(&plan.BaseChangeID, &plan.Tool, &raw, &expires)
	if err != nil {
		return pendingPolicyPlan{}, err
	}
	if err := json.Unmarshal([]byte(raw), &plan.Policy); err != nil {
		return pendingPolicyPlan{}, err
	}
	if t, err := time.Parse(time.RFC3339, expires); err == nil {
		plan.ExpiresAt = t
	}
	return plan, nil
}

func skillListAllows(list []string, skill string) bool {
	if stringListAllows(list, skill) {
		return true
	}
	slug := a2a.SkillSlug(skill)
	for _, item := range list {
		if a2a.SkillSlug(item) == slug || defaultTaskSkillCompatible(item, skill) {
			return true
		}
	}
	return false
}
func policyChangeID(policy a2a.ChannelA2APolicy) string { return hashString(payloadHash(policy)) }
func payloadHash(payload any) string                    { raw, _ := json.Marshal(payload); return hashString(string(raw)) }
func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}
func truncateForSummary(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
func randomToken(n int) string {
	if n <= 0 {
		n = 12
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}
func ptrPolicyDeliveryReadiness(policy a2a.ChannelA2APolicy) *A2APolicyDeliveryReadiness {
	readiness := policyDeliveryReadiness(policy)
	return &readiness
}

func policyDeliveryReadiness(policy a2a.ChannelA2APolicy) A2APolicyDeliveryReadiness {
	missing := coPresentMissing(policy)
	guidance := "transparent/co_present is ready for configured co-present senders; still verify the opposite bot's inbound policy before claiming bidirectional readiness"
	if len(missing) > 0 {
		guidance = "safe/proxy only for co-present purposes: trusted/accept_from alone is not transparent/co_present readiness; update the target bot policy before claiming direct same-thread replies"
	}
	return A2APolicyDeliveryReadiness{
		ResultVisibility:        strings.TrimSpace(policy.ResultVisibility),
		DiscordTranscriptMode:   strings.TrimSpace(policy.DiscordTranscriptMode),
		ShareDiscordContext:     policy.ShareDiscordContext,
		CoPresentReady:          len(missing) == 0,
		CoPresentMissing:        missing,
		CoPresentFrom:           append([]string{}, policy.CoPresentFrom...),
		CoPresentFromRuntimes:   append([]string{}, policy.CoPresentFromRuntimes...),
		CoPresentTargetChannels: append([]string{}, policy.CoPresentTargetChannels...),
		Guidance:                guidance,
	}
}

func coPresentMissing(policy a2a.ChannelA2APolicy) []string {
	var missing []string
	if !policy.Enabled {
		missing = append(missing, "enabled=true")
	}
	if len(policy.AcceptFrom) == 0 && len(policy.AcceptFromRuntimes) == 0 {
		missing = append(missing, "accept_from or accept_from_runtimes")
	}
	if len(policy.AcceptSkills) > 0 && !skillListAllows(policy.AcceptSkills, "task") && !skillListAllows(policy.AcceptSkills, "general/task") {
		missing = append(missing, "accept_skills includes task/general_task or is empty to allow all capabilities")
	}
	if strings.TrimSpace(policy.ResultVisibility) != "transparent" {
		missing = append(missing, "result_visibility=transparent")
	}
	if strings.TrimSpace(policy.DiscordTranscriptMode) != "co_present" {
		missing = append(missing, "discord_transcript_mode=co_present")
	}
	if !policy.ShareDiscordContext {
		missing = append(missing, "share_discord_context=true")
	}
	if len(policy.CoPresentFrom) == 0 && len(policy.CoPresentFromRuntimes) == 0 {
		missing = append(missing, "co_present_from or co_present_from_runtimes")
	}
	return missing
}

func policySummary(old, next a2a.ChannelA2APolicy) string {
	readiness := policyDeliveryReadiness(next)
	return fmt.Sprintf("A2A policy for channel %s: enabled %v→%v, ref %q→%q, delegate_to=%v, delegate_skills=%v, delivery=%s/%s, co_present_ready=%v, co_present_missing=%v", next.ChannelID, old.Enabled, next.Enabled, old.ChannelRef, next.ChannelRef, next.DelegateTo, next.DelegateSkills, readiness.ResultVisibility, readiness.DiscordTranscriptMode, readiness.CoPresentReady, readiness.CoPresentMissing)
}
func policyRiskLabels(old, next a2a.ChannelA2APolicy) []string {
	labels := []string{"policy_change"}
	if !old.Enabled && next.Enabled {
		labels = append(labels, "enable_a2a")
	}
	if len(next.DelegateTo) > len(old.DelegateTo) || len(next.DelegateSkills) > len(old.DelegateSkills) {
		labels = append(labels, "remote_delegation")
	}
	if next.ShareDiscordContext {
		labels = append(labels, "discord_context")
	}
	return labels
}

func a2aToolResult(resp A2AToolResponse) (*mcp.CallToolResult, error) {
	raw, _ := json.MarshalIndent(resp, "", "  ")
	if !resp.OK {
		return mcp.NewToolResultError(string(raw)), nil
	}
	return mcp.NewToolResultText(string(raw)), nil
}
func (s *A2AService) normalizeBoundContext(req A2AToolRequest) A2AToolRequest {
	bound := strings.TrimSpace(s.cfg.BoundChannelID)
	target := strings.TrimSpace(s.cfg.BoundTargetID)
	if bound != "" && target != "" && target != bound && strings.TrimSpace(req.ChannelID) == target {
		req.ChannelID = bound
	}
	return req
}

func a2aRequestFromMCP(req mcp.CallToolRequest) A2AToolRequest {
	var out A2AToolRequest
	raw, _ := json.Marshal(req.GetArguments())
	_ = json.Unmarshal(raw, &out)
	if out.GuildID == "" {
		out.GuildID = req.GetString("guild_id", "")
	}
	if out.ChannelID == "" {
		out.ChannelID = req.GetString("channel_id", "")
	}
	if out.RequestedBy == "" {
		out.RequestedBy = req.GetString("requested_by", "")
	}
	if out.RequestedByID == "" {
		out.RequestedByID = req.GetString("requested_by_id", "")
	}
	if state, ok := currentTargetState(); ok {
		if requesterID := strings.TrimSpace(state.RequesterID); requesterID != "" && strings.TrimSpace(out.RequestedByID) == "" {
			out.RequestedByID = requesterID
		}
		if requesterName := strings.TrimSpace(state.RequesterName); requesterName != "" && strings.TrimSpace(out.RequestedBy) == "" {
			out.RequestedBy = requesterName
		}
		if source := strings.TrimSpace(state.Source); source != "" && strings.TrimSpace(out.RequestSource) == "" {
			out.RequestSource = source
		}
	}
	return out
}
