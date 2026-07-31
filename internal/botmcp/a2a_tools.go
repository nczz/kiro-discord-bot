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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
)

const (
	ToolA2APeers            = "bot_a2a_peers"
	ToolA2APolicyGet        = "bot_a2a_policy_get"
	ToolA2ATaskStatus       = "bot_a2a_task_status"
	ToolA2ARuntimePreflight = "bot_a2a_runtime_preflight"
	ToolA2APolicyPlan       = "bot_a2a_policy_plan"
	ToolA2APolicyApply      = "bot_a2a_policy_apply"
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
	cfg       A2AServiceConfig
	peers     *a2a.SQLitePeerStore
	policies  *a2a.SQLitePolicyStore
	tasks     *a2a.SQLiteTaskStore
	node      *a2a.Node
	publisher *a2a.Publisher
	closeFns  []func()
}

type A2AToolRequest struct {
	GuildID              string   `json:"guild_id,omitempty"`
	ChannelID            string   `json:"channel_id,omitempty"`
	RequestedBy          string   `json:"requested_by,omitempty"`
	RequestedByID        string   `json:"requested_by_id,omitempty"`
	ManageChannels       bool     `json:"manage_channels,omitempty"`
	TargetAgent          string   `json:"target_agent,omitempty"`
	SkillID              string   `json:"skill_id,omitempty"`
	Message              string   `json:"message,omitempty"`
	Reason               string   `json:"reason,omitempty"`
	TaskID               string   `json:"task_id,omitempty"`
	LocalID              string   `json:"local_id,omitempty"`
	Input                string   `json:"input,omitempty"`
	Approve              bool     `json:"approve,omitempty"`
	DenyReason           string   `json:"deny_reason,omitempty"`
	ChangeID             string   `json:"change_id,omitempty"`
	ConfirmationToken    string   `json:"confirmation_token,omitempty"`
	PolicyAction         string   `json:"policy_action,omitempty"`
	RequiresConfirmation bool     `json:"requires_confirmation,omitempty"`
	DeliveryMode         string   `json:"delivery_mode,omitempty"`
	TranscriptMode       string   `json:"transcript_mode,omitempty"`
	ResultVisibility     string   `json:"result_visibility,omitempty"`
	ChannelRef           string   `json:"channel_ref,omitempty"`
	TargetChannelID      string   `json:"target_channel_id,omitempty"`
	TargetThreadID       string   `json:"target_thread_id,omitempty"`
	TargetChannelRef     string   `json:"target_channel_ref,omitempty"`
	SetupMode            string   `json:"setup_mode,omitempty"`
	Enable               *bool    `json:"enable,omitempty"`
	AcceptFrom           []string `json:"accept_from,omitempty"`
	AcceptSkills         []string `json:"accept_skills,omitempty"`
	ExposeSkills         []string `json:"expose_skills,omitempty"`
	DelegateTo           []string `json:"delegate_to,omitempty"`
	DelegateSkills       []string `json:"delegate_skills,omitempty"`
	DelegateMediaTypes   []string `json:"delegate_media_types,omitempty"`
	DelegateMaxBytes     int64    `json:"delegate_max_bytes,omitempty"`
	MaxConcurrent        *int     `json:"max_concurrent,omitempty"`
	ShareDiscordContext  *bool    `json:"share_discord_context,omitempty"`
	CoPresentFrom        []string `json:"co_present_from,omitempty"`
	AllowMemoryWrite     *bool    `json:"allow_memory_write,omitempty"`
	Limit                int      `json:"limit,omitempty"`
}

type A2AToolResponse struct {
	OK                   bool                      `json:"ok"`
	Message              string                    `json:"message"`
	ErrorCode            a2a.ErrorCode             `json:"errorCode,omitempty"`
	RequiresConfirmation bool                      `json:"requiresConfirmation"`
	ConfirmationSummary  string                    `json:"confirmationSummary,omitempty"`
	RiskLabels           []string                  `json:"riskLabels,omitempty"`
	ExpiresAt            string                    `json:"expiresAt,omitempty"`
	ChangeID             string                    `json:"changeId,omitempty"`
	ConfirmationToken    string                    `json:"confirmationToken,omitempty"`
	Policy               *a2a.ChannelA2APolicy     `json:"policy,omitempty"`
	RuntimePreflight     *a2a.RuntimeCutoverReport `json:"runtimePreflight,omitempty"`
	Peers                []A2APeerSummary          `json:"peers,omitempty"`
	Tasks                []A2ATaskSummary          `json:"tasks,omitempty"`
	Task                 *A2ATaskSummary           `json:"task,omitempty"`
	Metadata             map[string]interface{}    `json:"metadata,omitempty"`
}

type A2APeerSummary struct {
	AgentID           string   `json:"agentId"`
	Name              string   `json:"name"`
	Trusted           bool     `json:"trusted"`
	Online            bool     `json:"online"`
	Stale             bool     `json:"stale"`
	Skills            []string `json:"skills"`
	HiddenSkillCount  int      `json:"hiddenSkillCount,omitempty"`
	DelegationAllowed bool     `json:"delegationAllowed"`
	ProtocolBinding   string   `json:"protocolBinding,omitempty"`
	ProtocolVersion   string   `json:"protocolVersion,omitempty"`
	SignatureStatus   string   `json:"signatureStatus,omitempty"`
	Runtime           string   `json:"runtime,omitempty"`
	ChannelRef        string   `json:"channelRef,omitempty"`
	Wakeable          bool     `json:"wakeable"`
	DisplayName       string   `json:"displayName,omitempty"`
	DiscordGuildID    string   `json:"discordGuildId,omitempty"`
	DiscordChannelID  string   `json:"discordChannelId,omitempty"`
	DiscordThreadID   string   `json:"discordThreadId,omitempty"`
}

type A2ATaskSummary struct {
	LocalID       string        `json:"localId"`
	TaskID        string        `json:"taskId,omitempty"`
	Direction     string        `json:"direction"`
	FromAgent     string        `json:"fromAgent"`
	ToAgent       string        `json:"toAgent"`
	ExecutorAgent string        `json:"executorAgent,omitempty"`
	ChannelID     string        `json:"channelId,omitempty"`
	SkillID       string        `json:"skillId,omitempty"`
	State         a2a.TaskState `json:"state"`
	Revision      int64         `json:"revision"`
	Terminal      bool          `json:"terminal"`
	ErrorCode     a2a.ErrorCode `json:"errorCode,omitempty"`
	ErrorMessage  string        `json:"errorMessage,omitempty"`
	UpdatedAt     string        `json:"updatedAt,omitempty"`
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
		cfg.ConfirmationSecret = firstNonEmpty(os.Getenv("A2A_CONFIRMATION_SECRET"), os.Getenv("DISCORD_TOKEN"), "kiro-a2a-dev-confirmation-secret")
	}
	if cfg.AuditDBPath == "" {
		cfg.AuditDBPath = botToolsAuditDBPath()
	}
	s := &A2AService{cfg: cfg, peers: cfg.PeerStore, policies: cfg.PolicyStore, tasks: cfg.TaskStore, node: cfg.Node, publisher: cfg.Publisher}
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
	for _, row := range rows {
		if row.AgentID == s.cfg.Config.AgentID {
			continue
		}
		visibleSkills := visiblePeerSkills(policy, string(row.AgentID), row.SkillIDs, s.cfg.Config.RuntimeIDMode)
		delegationAllowed := len(visibleSkills) > 0
		peers = append(peers, A2APeerSummary{AgentID: string(row.AgentID), Name: row.Name, Trusted: row.Trusted, Online: row.Online, Stale: row.Stale, Skills: visibleSkills, HiddenSkillCount: len(row.SkillIDs) - len(visibleSkills), DelegationAllowed: delegationAllowed, Runtime: row.Runtime, ChannelRef: row.ChannelRef, DisplayName: peerDisplayName(row), DiscordGuildID: row.DiscordGuildID, DiscordChannelID: row.DiscordChannelID, DiscordThreadID: row.DiscordThreadID, Wakeable: delegationAllowed && row.Runtime == "channel" && !row.Stale, ProtocolBinding: row.SupportedBinding, ProtocolVersion: row.ProtocolVersion, SignatureStatus: row.SignatureStatus})
	}
	return A2AToolResponse{OK: true, Message: "A2A peers listed", Peers: peers}, nil
}

func peerDisplayName(peer a2a.PeerTrustDisplay) string {
	if strings.TrimSpace(peer.ChannelRef) != "" {
		return peer.ChannelRef
	}
	if strings.TrimSpace(peer.Description) != "" {
		return peer.Description
	}
	return peer.Name
}

func visiblePeerSkills(policy a2a.ChannelA2APolicy, agent string, skills []string, mode a2a.RuntimeIDMode) []string {
	if !policy.Enabled {
		return nil
	}
	runtimeOnly := mode == a2a.RuntimeIDModeRuntime
	out := make([]string, 0, len(skills))
	for _, skill := range skills {
		if runtimeOnly {
			if policyDelegatesExactRuntime(policy, agent, skill) {
				out = append(out, skill)
			}
			continue
		}
		if policyDelegatesRuntime(policy, agent, skill, policy.ChannelRef) {
			out = append(out, skill)
		}
	}
	return out
}

func policyDelegatesExactRuntime(policy a2a.ChannelA2APolicy, agent, skill string) bool {
	for _, target := range policy.DelegateTargets {
		if strings.TrimSpace(target.RuntimeAgentID) == "" {
			continue
		}
		if !stringListAllows([]string{target.RuntimeAgentID}, agent) {
			continue
		}
		if skillListAllows([]string{target.SkillID}, skill) {
			return true
		}
	}
	return false
}

func (s *A2AService) PolicyGet(ctx context.Context, req A2AToolRequest) (A2AToolResponse, error) {
	if err := s.validateContext(req, false); err != nil {
		return responseError(err), nil
	}
	policy, err := s.currentPolicy(ctx, req)
	if err != nil {
		return responseError(err), nil
	}
	return A2AToolResponse{OK: true, Message: "A2A policy loaded", Policy: &policy}, nil
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
		sum := summarizeTask(row)
		return A2AToolResponse{OK: true, Message: "A2A task loaded", Task: &sum}, nil
	}
	if strings.TrimSpace(req.TaskID) != "" {
		row, err := s.tasks.GetByDirectionTaskID(ctx, "outbound", a2a.TaskID(strings.TrimSpace(req.TaskID)))
		if err != nil {
			return responseError(taskLookupError(err)), nil
		}
		if err := authorizeTaskStatus(row, req); err != nil {
			return responseError(err), nil
		}
		sum := summarizeTask(row)
		return A2AToolResponse{OK: true, Message: "A2A task loaded", Task: &sum}, nil
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
	if req.ManageChannels {
		return nil
	}
	if strings.TrimSpace(row.ChannelID) != strings.TrimSpace(req.ChannelID) {
		return fmt.Errorf("%w: task is not visible from this channel", errorCode(a2a.ErrorPolicyDenied))
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
	_ = s.recordAudit(ctx, a2a.AuditPolicyChangePlanned, req, "planned", "", map[string]any{"change_id": changeID})
	return A2AToolResponse{OK: true, Message: "A2A policy change planned", RequiresConfirmation: true, ConfirmationSummary: summary, RiskLabels: policyRiskLabels(policy, planned), ExpiresAt: exp.Format(time.RFC3339), ChangeID: changeID, ConfirmationToken: token, Policy: &planned}, nil
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
	targetChannelRef := req.targetRuntimeRef(policy.ChannelRef)
	effectiveSkill := canonicalPeerSkill(peer, req.SkillID, targetChannelRef)
	if effectiveSkill == "" {
		return responseError(fmt.Errorf("%w: target peer does not expose skill", errorCode(a2a.ErrorUnknownSkill))), nil
	}
	if !req.hasExplicitTargetRuntimeRef() && !policyDelegatesRuntime(policy, string(target), effectiveSkill, targetChannelRef) {
		if inferred := skillChannelRef(effectiveSkill); inferred != "" {
			targetChannelRef = inferred
		}
	}
	if !policyDelegatesRuntime(policy, string(target), effectiveSkill, targetChannelRef) {
		return responseError(fmt.Errorf("%w: target runtime is not delegated by channel policy", errorCode(a2a.ErrorUnauthorizedTarget))), nil
	}
	if !peer.Trusted && !policyDelegatesExactRuntime(policy, string(target), effectiveSkill) {
		return responseError(fmt.Errorf("%w: target peer is not trusted", errorCode(a2a.ErrorPolicyDenied))), nil
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
	resultVisibility := firstNonEmpty(req.ResultVisibility, policy.ResultVisibility, "proxy")
	transcriptMode := firstNonEmpty(req.TranscriptMode, policy.DiscordTranscriptMode, "delegator")
	switch normalizeSetupMode(req.SetupMode) {
	case "co_present":
		resultVisibility, transcriptMode = "transparent", "co_present"
	case "safe":
		resultVisibility, transcriptMode = "proxy", "delegator"
	case "auto":
		resultVisibility, transcriptMode = runtimeDeliveryDefaults(policy.ChannelRef, targetChannelRef)
	}
	changeID := hashString("delegate:" + req.GuildID + ":" + req.ChannelID + ":" + targetChannelRef + ":" + string(target) + ":" + req.SkillID + ":" + resultVisibility + ":" + transcriptMode + ":" + message)
	needsConfirmation := req.RequiresConfirmation || s.cfg.Config.RequireConfirmationForRemote
	if needsConfirmation && strings.TrimSpace(req.ConfirmationToken) == "" {
		exp := s.cfg.Now().UTC().Add(10 * time.Minute)
		return A2AToolResponse{OK: true, Message: "A2A delegation requires confirmation", RequiresConfirmation: true, ConfirmationSummary: fmt.Sprintf("Delegate %q to %s@%s/%s", truncateForSummary(message), target, targetChannelRef, req.SkillID), RiskLabels: []string{"remote_task", "data_egress"}, ExpiresAt: exp.Format(time.RFC3339), ChangeID: changeID, ConfirmationToken: s.confirmationToken("delegate", changeID, req, message)}, nil
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
	taskReq := a2a.TaskExecutionRequest{MessageID: msgID, ClientTaskRef: req.RequestedByID, ContextID: "discord:" + req.ChannelID + ":" + string(msgID), From: source, To: target, ChannelID: req.ChannelID, GuildID: req.GuildID, ChannelRef: targetChannelRef, SkillID: req.SkillID, UserVisibleSummary: truncateForSummary(message), Payload: payload, Delivery: a2a.DeliveryOptions{TimeoutSec: s.cfg.Config.TaskTimeoutSec, DiscordReplyChannelID: req.ChannelID, DiscordReplyThreadID: deliveryChannelID(req.ChannelID), MaxDelegationDepth: delegationDepth}, ResultVisibility: resultVisibility, DiscordTranscriptMode: transcriptMode, OriginRequester: a2a.OriginRequester{DiscordUserID: strings.TrimSpace(req.RequestedByID), DiscordUsername: strings.TrimSpace(req.RequestedBy), DiscordGuildID: strings.TrimSpace(req.GuildID)}}
	row, err := pub.SendTask(ctx, taskReq)
	if err != nil {
		meta := a2a.AuditMetadata(a2a.AuditMetadataInput{MessageID: msgID, ClientTaskRef: req.RequestedByID, ContextID: taskReq.ContextID, FromAgent: taskReq.From, ToAgent: taskReq.To, ChannelID: req.ChannelID, GuildID: req.GuildID, ChannelRef: targetChannelRef, SkillID: req.SkillID, ResultVisibility: taskReq.ResultVisibility, DiscordTranscriptMode: taskReq.DiscordTranscriptMode, ActorAgentID: s.cfg.Config.AgentID, ActorDiscordUserID: req.RequestedByID, ErrorCode: a2a.ErrorNATSPublishFailed, PayloadSize: len(payload)})
		_ = s.recordAudit(ctx, a2a.AuditTaskPublishFailed, req, "error", err.Error(), meta)
		return responseError(fmt.Errorf("%w: %v", errorCode(a2a.ErrorNATSPublishFailed), err)), nil
	}
	meta := a2a.AuditMetadata(a2a.AuditMetadataInput{TaskID: row.TaskID, ClientTaskRef: row.ClientTaskRef, MessageID: msgID, ContextID: row.ContextID, FromAgent: row.FromAgent, ToAgent: row.ToAgent, ExecutorAgent: row.ExecutorAgent, ChannelID: req.ChannelID, GuildID: req.GuildID, ChannelRef: targetChannelRef, SkillID: req.SkillID, State: row.State, Revision: row.Revision, ResultVisibility: row.ResultVisibility, DiscordTranscriptMode: row.DiscordTranscriptMode, ActorAgentID: s.cfg.Config.AgentID, ActorDiscordUserID: req.RequestedByID, PayloadSize: len(payload)})
	_ = s.recordAudit(ctx, a2a.AuditTaskSendRequested, req, "queued", "", meta)
	sum := summarizeTask(row)
	return A2AToolResponse{OK: true, Message: "A2A task sent", Task: &sum}, nil
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
		return defaultPolicy(req, s.cfg.Config), nil
	}
	return a2a.ChannelA2APolicy{}, fmt.Errorf("%w: %v", errorCode(a2a.ErrorStoreError), err)
}

func defaultPolicy(req A2AToolRequest, cfg a2a.Config) a2a.ChannelA2APolicy {
	channelRef := strings.TrimSpace(req.ChannelRef)
	if channelRef == "" {
		channelRef = "discord-" + strings.TrimSpace(req.ChannelID)
	}
	policy := a2a.ChannelA2APolicy{GuildID: strings.TrimSpace(req.GuildID), ChannelID: strings.TrimSpace(req.ChannelID), ChannelRef: channelRef, BotAgentID: string(cfg.AgentID), ResultVisibility: "proxy", DiscordTranscriptMode: "delegator"}
	if runtime, err := a2a.GenerateRuntimeAgentID(cfg.AgentID, channelRef); err == nil {
		policy.RuntimeAgentID = string(runtime)
	}
	return policy
}

func (s *A2AService) applyPolicyDiff(policy a2a.ChannelA2APolicy, req A2AToolRequest) a2a.ChannelA2APolicy {
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
		if strings.TrimSpace(policy.RuntimeAgentID) == "" {
			if runtime, err := a2a.GenerateRuntimeAgentID(s.cfg.Config.AgentID, policy.ChannelRef); err == nil {
				policy.RuntimeAgentID = string(runtime)
			}
		}
	}
	policy.AcceptFrom = appendUnique(policy.AcceptFrom, req.AcceptFrom...)
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
		if (len(req.DelegateTo) > 0 || strings.TrimSpace(req.TargetAgent) != "") && strings.TrimSpace(req.SkillID) != "" {
			agents := req.DelegateTo
			if len(agents) == 0 {
				agents = []string{req.TargetAgent}
			}
			for _, agent := range agents {
				policy.DelegateTargets = upsertDelegateTarget(policy.DelegateTargets, a2a.DelegateTargetPolicy{
					AgentID:    strings.TrimSpace(agent),
					ChannelRef: req.targetRuntimeRef(policy.ChannelRef),
					SkillID:    strings.TrimSpace(req.SkillID),
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
	localSkill := string(a2a.SkillSlug(req.SkillID))
	req.AcceptSkills = appendUnique(req.AcceptSkills, localSkill)
	req.ExposeSkills = appendUnique(req.ExposeSkills, localSkill)
	req.DelegateSkills = appendUnique(req.DelegateSkills, req.SkillID)
	mode := normalizeSetupMode(req.SetupMode)
	req.SetupMode = mode
	if mode == "co_present" || (mode == "auto" && req.targetRuntimeRef(policy.ChannelRef) == policy.ChannelRef) {
		req.TranscriptMode = "co_present"
		req.ResultVisibility = "transparent"
		share := true
		req.ShareDiscordContext = &share
	} else if mode == "safe" || mode == "auto" {
		req.TranscriptMode = "delegator"
		req.ResultVisibility = "proxy"
		share := false
		req.ShareDiscordContext = &share
	}
	return req
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

func skillChannelRef(skillID string) string {
	before, _, ok := strings.Cut(strings.TrimSpace(skillID), "/")
	if !ok {
		return ""
	}
	return strings.TrimSpace(before)
}

func runtimeDeliveryDefaults(sourceChannelRef, targetChannelRef string) (string, string) {
	if strings.TrimSpace(targetChannelRef) == "" || strings.TrimSpace(targetChannelRef) == strings.TrimSpace(sourceChannelRef) {
		return "transparent", "co_present"
	}
	return "proxy", "delegator"
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
	return strings.TrimSpace(targetChannelRef) == strings.TrimSpace(policy.ChannelRef) && stringListAllows(policy.DelegateTo, agent) && skillListAllows(policy.DelegateSkills, skill)
}

func upsertDelegateTarget(targets []a2a.DelegateTargetPolicy, next a2a.DelegateTargetPolicy) []a2a.DelegateTargetPolicy {
	if next.SkillID == "" || (next.AgentID == "" && next.RuntimeAgentID == "") {
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

func (s *A2AService) lookupOutboundTask(ctx context.Context, req A2AToolRequest) (a2a.TaskRow, error) {
	if id := strings.TrimSpace(req.LocalID); id != "" {
		row, err := s.tasks.GetByLocalID(ctx, id)
		if err != nil {
			return a2a.TaskRow{}, taskLookupError(err)
		}
		return row, nil
	}
	if id := strings.TrimSpace(req.TaskID); id != "" {
		row, err := s.tasks.GetByDirectionTaskID(ctx, "outbound", a2a.TaskID(id))
		if err != nil {
			return a2a.TaskRow{}, taskLookupError(err)
		}
		return row, nil
	}
	return a2a.TaskRow{}, fmt.Errorf("%w: task_id or local_id is required", errorCode(a2a.ErrorTaskNotFound))
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
	return A2ATaskSummary{LocalID: row.LocalID, TaskID: string(row.TaskID), Direction: row.Direction, FromAgent: string(row.FromAgent), ToAgent: string(row.ToAgent), ExecutorAgent: string(row.ExecutorAgent), ChannelID: row.ChannelID, SkillID: row.SkillID, State: row.State, Revision: row.Revision, Terminal: row.Terminal, ErrorCode: row.Error.Code, ErrorMessage: row.Error.Message, UpdatedAt: row.UpdatedAt.Format(time.RFC3339)}
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
		if skill.ID == skillID || a2a.SkillSlug(skill.ID) == slug {
			return true
		}
	}
	return false
}

func canonicalPeerSkill(peer a2a.PeerRow, skillID, targetChannelRef string) string {
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
		if a2a.SkillSlug(skill.ID) == slug {
			return skill.ID
		}
	}
	return ""
}

func skillListAllows(list []string, skill string) bool {
	if stringListAllows(list, skill) {
		return true
	}
	slug := a2a.SkillSlug(skill)
	for _, item := range list {
		if a2a.SkillSlug(item) == slug {
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
func policySummary(old, next a2a.ChannelA2APolicy) string {
	return fmt.Sprintf("A2A policy for channel %s: enabled %v→%v, ref %q→%q, delegate_to=%v, delegate_skills=%v", next.ChannelID, old.Enabled, next.Enabled, old.ChannelRef, next.ChannelRef, next.DelegateTo, next.DelegateSkills)
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
	}
	return out
}
