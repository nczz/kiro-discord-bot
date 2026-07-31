package botmcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/a2a"
)

func TestA2AToolsPolicyPlanPolicyApply(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", TaskTimeoutSec: 60, MaxDelegationDepth: 1},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
		Now:                func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	enable := true
	unauthorized, err := svc.PolicyPlan(context.Background(), A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", Enable: &enable, ChannelRef: "case/alpha"})
	if err != nil {
		t.Fatalf("PolicyPlan unauthorized err: %v", err)
	}
	if unauthorized.OK || unauthorized.ErrorCode != a2a.ErrorPolicyDenied {
		t.Fatalf("PolicyPlan unauthorized = %+v, want policy_denied", unauthorized)
	}

	peerCard := a2a.AgentCard{Name: "peer-n100", Description: "peer", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "case/summarize", Name: "Summarize", Description: "summarize"}}}
	if _, err := svc.peers.UpsertCard(context.Background(), "peer-n100", peerCard, false, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert untrusted peer: %v", err)
	}

	planReq := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, Enable: &enable, ChannelRef: "case/alpha", ExposeSkills: []string{"search-case"}, DelegateTo: []string{"peer-n100"}, DelegateSkills: []string{"summarize-case"}}
	planned, err := svc.PolicyPlan(context.Background(), planReq)
	if err != nil {
		t.Fatalf("PolicyPlan manager err: %v", err)
	}
	if !planned.OK || !planned.RequiresConfirmation || planned.ConfirmationToken == "" || planned.ChangeID == "" {
		t.Fatalf("PolicyPlan manager = %+v, want confirmation payload", planned)
	}
	if planned.Policy == nil || !planned.Policy.Enabled || planned.Policy.ChannelRef != "case/alpha" {
		t.Fatalf("planned policy = %+v, want enabled channel ref", planned.Policy)
	}

	applyReq := planReq
	applyReq.ConfirmationToken = planned.ConfirmationToken
	applied, err := svc.PolicyApply(context.Background(), applyReq)
	if err != nil {
		t.Fatalf("PolicyApply: %v", err)
	}
	if !applied.OK || applied.Policy == nil || !applied.Policy.Enabled || applied.Policy.ChannelRef != "case/alpha" {
		t.Fatalf("PolicyApply = %+v, want persisted policy", applied)
	}
	peer, err := svc.peers.Get(context.Background(), "peer-n100")
	if err != nil {
		t.Fatalf("Get trusted peer: %v", err)
	}
	if !peer.Trusted {
		t.Fatal("PolicyApply did not trust confirmed delegated peer")
	}
}

func TestA2AToolsPeersRuntimeModeListsWakeableRuntimeAndHidesLegacyBot(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{
		GuildID:            "guild-1",
		ChannelID:          "channel-1",
		Enabled:            true,
		Discoverable:       true,
		RuntimeAgentID:     "adam-n200-main",
		BotAgentID:         "adam-n200",
		ChannelRef:         "main",
		AcceptFromRuntimes: []string{"peer-n100-support"},
		AcceptSkills:       []string{"task"},
		DelegateTo:         []string{"peer-n100"},
		DelegateSkills:     []string{"general/task"},
		DelegateTargets: []a2a.DelegateTargetPolicy{{
			RuntimeAgentID: "peer-n100-support",
			AgentID:        "peer-n100",
			ChannelRef:     "support",
			SkillID:        "task",
		}},
	}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	baseCard := a2a.AgentCard{Name: "peer-n100", Description: "bot host", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "general/task", Name: "General", Description: "general"}}}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-n100", baseCard, a2a.ExtendedAgentCard{Runtime: "kiro-discord-bot"}, true, "peer-host", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert base card: %v", err)
	}
	runtimeCard := a2a.AgentCard{Name: "peer-n100-support", Description: "support runtime", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "support/task", Name: "Task", Description: "task"}}}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-n100-support", runtimeCard, a2a.ExtendedAgentCard{Runtime: "channel", ChannelRef: "support", DiscordGuildID: "1495737767827865620", DiscordChannelID: "1495737768905670719", DiscordThreadID: "1532710952477261854"}, false, "peer-host-peer-n100-support", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert runtime card: %v", err)
	}

	resp, err := svc.Peers(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1"})
	if err != nil {
		t.Fatalf("Peers: %v", err)
	}
	byAgent := map[string]A2APeerSummary{}
	for _, peer := range resp.Peers {
		byAgent[peer.AgentID] = peer
	}
	if base := byAgent["peer-n100"]; base.DelegationAllowed || base.Wakeable || base.HiddenSkillCount == 0 {
		t.Fatalf("base peer = %+v, want hidden non-callable bot host in runtime mode", base)
	}
	runtime := byAgent["peer-n100-support"]
	if !runtime.DelegationAllowed || !runtime.Wakeable || runtime.Runtime != "channel" || runtime.ChannelRef != "support" {
		t.Fatalf("runtime peer = %+v, want wakeable callable channel runtime", runtime)
	}
	if runtime.DisplayName != "support" || runtime.DiscordGuildID != "1495737767827865620" || runtime.DiscordChannelID != "1495737768905670719" || runtime.DiscordThreadID != "1532710952477261854" {
		t.Fatalf("runtime identity = %+v, want Discord identifiers and display name", runtime)
	}
	if len(runtime.Skills) != 1 || runtime.Skills[0] != "support/task" {
		t.Fatalf("runtime skills = %v, want canonical runtime skill", runtime.Skills)
	}
}

func TestA2AToolsPolicySetupDefaultsRuntimeTarget(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", TaskTimeoutSec: 60, MaxDelegationDepth: 1},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
		Now:                func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	req := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "peer-n100", SkillID: "general/task", ChannelRef: "m5-main", TargetChannelRef: "erp-support", SetupMode: "auto"}
	planned, err := svc.PolicyPlan(ctx, req)
	if err != nil {
		t.Fatalf("PolicyPlan: %v", err)
	}
	if !planned.OK || planned.Policy == nil {
		t.Fatalf("PolicyPlan = %+v, want planned policy", planned)
	}
	policy := planned.Policy
	if !policy.Enabled {
		t.Fatalf("setup defaults did not enable policy: %+v", policy)
	}
	if policy.ResultVisibility != "proxy" || policy.DiscordTranscriptMode != "delegator" || policy.ShareDiscordContext {
		t.Fatalf("cross-runtime auto defaults = visibility %q transcript %q share %v, want proxy/delegator/false", policy.ResultVisibility, policy.DiscordTranscriptMode, policy.ShareDiscordContext)
	}
	if len(policy.DelegateTargets) != 1 || policy.DelegateTargets[0].AgentID != "peer-n100" || policy.DelegateTargets[0].ChannelRef != "erp-support" || policy.DelegateTargets[0].SkillID != "general/task" {
		t.Fatalf("delegate targets = %+v, want peer-n100 @ erp-support / general/task", policy.DelegateTargets)
	}
	applyReq := req
	applyReq.ConfirmationToken = planned.ConfirmationToken
	applied, err := svc.PolicyApply(ctx, applyReq)
	if err != nil {
		t.Fatalf("PolicyApply: %v", err)
	}
	if !applied.OK || applied.Policy == nil || len(applied.Policy.DelegateTargets) != 1 {
		t.Fatalf("PolicyApply = %+v, want persisted runtime target", applied)
	}
}

func TestA2AToolsLegacyDelegatePolicyCannotCrossRuntime(t *testing.T) {
	policy := a2a.ChannelA2APolicy{ChannelRef: "m5-main", DelegateTo: []string{"peer-n100"}, DelegateSkills: []string{"general/task"}}
	if !policyDelegatesRuntime(policy, "peer-n100", "general/task", "m5-main") {
		t.Fatal("legacy same-runtime delegation was denied")
	}
	if policyDelegatesRuntime(policy, "peer-n100", "general/task", "erp-support") {
		t.Fatal("legacy delegation allowed cross-runtime target")
	}
}

func TestA2AToolsBoundContextDelegateQuotaCancelInputReplyAuthReply(t *testing.T) {
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", MaxOutboundTasksPerChannel: 1, TaskTimeoutSec: 60},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	base := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "peer-n100", SkillID: "summarize-case", Message: "summarize this"}
	if got, _ := svc.Peers(context.Background(), A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-2", RequestedBy: "alice", RequestedByID: "user-1"}); got.OK || got.ErrorCode != a2a.ErrorPolicyDenied {
		t.Fatalf("wrong bound channel response = %+v, want policy_denied", got)
	}
	if got, _ := svc.Delegate(context.Background(), base); got.OK || got.ErrorCode != a2a.ErrorChannelNotEnabled {
		t.Fatalf("Delegate disabled policy response = %+v, want channel_not_enabled", got)
	}

	if _, err := svc.tasks.CreateOutbound(context.Background(), a2a.TaskRow{MessageID: "msg_quota_1", FromAgent: "adam-n200", ToAgent: "peer-n100", ChannelID: "channel-1", State: a2a.TaskStateSubmitted}); err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	if err := svc.checkOutboundQuota(context.Background(), base); err == nil || !strings.Contains(err.Error(), string(a2a.ErrorOverloaded)) {
		t.Fatalf("checkOutboundQuota err = %v, want overloaded", err)
	}

	base.TaskID = "task_missing"
	base.Input = "requested input"
	base.Approve = true
	for name, call := range map[string]func(context.Context, A2AToolRequest) (A2AToolResponse, error){
		"cancel":      svc.Cancel,
		"input_reply": svc.InputReply,
		"auth_reply":  svc.AuthReply,
	} {
		got, err := call(context.Background(), base)
		if err != nil {
			t.Fatalf("%s err: %v", name, err)
		}
		if got.OK || got.ErrorCode != a2a.ErrorTaskNotFound {
			t.Fatalf("%s response = %+v, want task_not_found", name, got)
		}
	}
}

func TestA2AToolsDelegateRejectsRevokedPeerBeforePublishing(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{NATSURL: "nats://127.0.0.1:4222", AgentID: "adam-n200", TaskTimeoutSec: 60, MaxDelegationDepth: 1, RequireConfirmationForRemote: false},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{
		GuildID:               "guild-1",
		ChannelID:             "channel-1",
		Enabled:               true,
		RuntimeAgentID:        "adam-n200-case-alpha",
		BotAgentID:            "adam-n200",
		ChannelRef:            "case/alpha",
		DelegateTo:            []string{"peer-n100"},
		DelegateSkills:        []string{"case/summarize"},
		ResultVisibility:      "proxy",
		DiscordTranscriptMode: "delegator",
	}, "manager-1"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	card := a2a.AgentCard{
		Name:        "peer-n100",
		Description: "peer",
		Version:     "1.0.0",
		SupportedInterfaces: []a2a.A2AInterface{{
			URL:             "nats://nats.example.internal:4222",
			ProtocolBinding: a2a.ProtocolBindingNATS,
			ProtocolVersion: a2a.ProtocolVersion,
		}},
		Skills: []a2a.AgentSkill{{ID: "case/summarize", Name: "Summarize", Description: "summarize"}},
	}
	if _, err := svc.peers.UpsertCard(ctx, "peer-n100", card, true, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert trusted peer: %v", err)
	}
	if _, err := svc.peers.UpsertCard(ctx, "peer-n100", card, false, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert revoked peer: %v", err)
	}

	got, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "peer-n100", SkillID: "case/summarize", Message: "summarize this"})
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if got.OK || got.ErrorCode != a2a.ErrorPolicyDenied || !strings.Contains(got.Message, "not trusted") {
		t.Fatalf("Delegate after peer revocation = %+v, want policy_denied not trusted", got)
	}
}

func TestA2AToolsDelegateAllowsUntrustedExactRuntimePolicy(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", NATSURL: "nats://a2a.example.internal:4222", TaskTimeoutSec: 60, MaxDelegationDepth: 1, RequireConfirmationForRemote: true},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{
		GuildID:        "guild-1",
		ChannelID:      "channel-1",
		Enabled:        true,
		RuntimeAgentID: "adam-n200-m5-main",
		BotAgentID:     "adam-n200",
		ChannelRef:     "m5-main",
		DelegateTargets: []a2a.DelegateTargetPolicy{{
			RuntimeAgentID: "peer-n100-support",
			AgentID:        "peer-n100",
			ChannelRef:     "support",
			SkillID:        "task",
		}},
	}, "manager-1"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	card := a2a.AgentCard{Name: "peer-n100-support", Description: "support runtime", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "support/task", Name: "Task", Description: "task"}}}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-n100-support", card, a2a.ExtendedAgentCard{Runtime: "channel", ChannelRef: "support"}, false, "peer-host-peer-n100-support", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert runtime peer: %v", err)
	}

	got, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "peer-n100-support", SkillID: "task", Message: "ping"})
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if !got.OK || !got.RequiresConfirmation || got.ErrorCode != "" || got.ConfirmationToken == "" {
		t.Fatalf("Delegate exact runtime policy = %+v, want confirmation instead of trust denial", got)
	}
	if !strings.Contains(got.ConfirmationSummary, "peer-n100-support@support/support/task") {
		t.Fatalf("Delegate confirmation summary = %q, want policy target channel_ref", got.ConfirmationSummary)
	}
}

func TestA2AToolsPeersFiltersLocalAgent(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:        t.TempDir(),
		Config:         a2a.Config{AgentID: "m5bot-local", TaskTimeoutSec: 60},
		BoundGuildID:   "guild-1",
		BoundChannelID: "channel-1",
		ConnectNATS:    false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	card := func(agent string) a2a.AgentCard {
		return a2a.AgentCard{Name: agent, Description: "peer", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "general/task", Name: "General", Description: "general"}}}
	}
	if _, err := svc.peers.UpsertCard(ctx, "m5bot-local", card("m5bot-local"), true, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert self: %v", err)
	}
	if _, err := svc.peers.UpsertCard(ctx, "d80-chunbot", card("d80-chunbot"), true, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}
	got, err := svc.Peers(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1"})
	if err != nil {
		t.Fatalf("Peers: %v", err)
	}
	if len(got.Peers) != 1 || got.Peers[0].AgentID != "d80-chunbot" {
		t.Fatalf("Peers = %+v, want remote peer only", got.Peers)
	}
	if len(got.Peers[0].Skills) != 0 || got.Peers[0].HiddenSkillCount != 1 || got.Peers[0].DelegationAllowed {
		t.Fatalf("peer skills = %+v hidden=%d allowed=%v, want hidden until channel policy delegates them", got.Peers[0].Skills, got.Peers[0].HiddenSkillCount, got.Peers[0].DelegationAllowed)
	}
}

func TestA2AToolsDelegateConfirmationBindsDeliveryMode(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", NATSURL: "nats://a2a.example.internal:4222", TaskTimeoutSec: 60, MaxDelegationDepth: 1, RequireConfirmationForRemote: true},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{GuildID: "guild-1", ChannelID: "channel-1", Enabled: true, RuntimeAgentID: "adam-n200-m5-main", BotAgentID: "adam-n200", ChannelRef: "m5-main", DelegateTargets: []a2a.DelegateTargetPolicy{{AgentID: "peer-n100", ChannelRef: "erp-support", SkillID: "general/task"}}}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	card := a2a.AgentCard{Name: "peer-n100", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "general/task", Name: "General"}}}
	if _, err := svc.peers.UpsertCard(ctx, "peer-n100", card, true, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}
	req := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "peer-n100", TargetChannelRef: "erp-support", SkillID: "general/task", Message: "ping", SetupMode: "safe"}
	planned, err := svc.Delegate(ctx, req)
	if err != nil {
		t.Fatalf("Delegate plan: %v", err)
	}
	if !planned.RequiresConfirmation || planned.ConfirmationToken == "" {
		t.Fatalf("Delegate plan = %+v, want confirmation token", planned)
	}
	req.SetupMode = "co_present"
	req.ConfirmationToken = planned.ConfirmationToken
	replayed, err := svc.Delegate(ctx, req)
	if err != nil {
		t.Fatalf("Delegate replay: %v", err)
	}
	if replayed.OK || replayed.ErrorCode != a2a.ErrorPolicyDenied || !strings.Contains(replayed.Message, "confirmation") {
		t.Fatalf("Delegate replay = %+v, want confirmation policy denial", replayed)
	}
}

func TestA2AToolsNestedDelegationDepthExhausted(t *testing.T) {
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:        t.TempDir(),
		Config:         a2a.Config{AgentID: "adam-n200", NATSURL: "nats://a2a.example.internal:4222", TaskTimeoutSec: 60, MaxDelegationDepth: 3},
		BoundGuildID:   "guild-1",
		BoundChannelID: "channel-1",
		ConnectNATS:    false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	statePath := filepath.Join(t.TempDir(), "target.json")
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1","remote_a2a":true,"delegation_depth":0}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	if depth, err := svc.nextDelegationDepth(); err == nil || depth != 0 || !strings.Contains(err.Error(), string(a2a.ErrorPolicyDenied)) {
		t.Fatalf("nextDelegationDepth exhausted = depth %d err %v, want policy_denied", depth, err)
	}
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1","remote_a2a":true,"delegation_depth":2}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	if depth, err := svc.nextDelegationDepth(); err != nil || depth != 1 {
		t.Fatalf("nextDelegationDepth nested = depth %d err %v, want 1", depth, err)
	}
}

func TestA2AToolsUndelegateRemovesRuntimeTarget(t *testing.T) {
	policy := a2a.ChannelA2APolicy{
		ChannelRef:      "m5-main",
		DelegateTo:      []string{"peer-n100"},
		DelegateSkills:  []string{"general/task"},
		DelegateTargets: []a2a.DelegateTargetPolicy{{AgentID: "peer-n100", ChannelRef: "erp-support", SkillID: "general/task"}},
	}
	got := applyPolicyDiff(policy, A2AToolRequest{PolicyAction: "undelegate-to", DelegateTo: []string{"peer-n100"}, DelegateSkills: []string{"general/task"}, SkillID: "general/task"})
	if len(got.DelegateTo) != 0 || len(got.DelegateSkills) != 0 || len(got.DelegateTargets) != 0 {
		t.Fatalf("undelegated policy = %+v, want empty delegate lists", got)
	}
}

func TestA2AToolsTaskStatusRequiresOwnerOrManager(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:        t.TempDir(),
		Config:         a2a.Config{AgentID: "adam-n200", TaskTimeoutSec: 60},
		BoundGuildID:   "guild-1",
		BoundChannelID: "channel-1",
		ConnectNATS:    false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	row, err := svc.tasks.CreateOutbound(ctx, a2a.TaskRow{MessageID: "msg_status", ClientTaskRef: "owner-1", FromAgent: "adam-n200", ToAgent: "peer-n100", ChannelID: "channel-1", GuildID: "guild-1", State: a2a.TaskStateSubmitted})
	if err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	req := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "mallory", RequestedByID: "mallory", LocalID: row.LocalID}
	got, err := svc.TaskStatus(ctx, req)
	if err != nil {
		t.Fatalf("TaskStatus: %v", err)
	}
	if got.OK || got.ErrorCode != a2a.ErrorPolicyDenied {
		t.Fatalf("TaskStatus non-owner = %+v, want policy_denied", got)
	}
	req.ManageChannels = true
	got, err = svc.TaskStatus(ctx, req)
	if err != nil {
		t.Fatalf("TaskStatus manager: %v", err)
	}
	if !got.OK || got.Task == nil || got.Task.TaskID != string(row.TaskID) {
		t.Fatalf("TaskStatus manager = %+v, want task", got)
	}
	ownerReq := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "owner", RequestedByID: "owner-1"}
	listed, err := svc.TaskStatus(ctx, ownerReq)
	if err != nil {
		t.Fatalf("TaskStatus owner list: %v", err)
	}
	if !listed.OK || len(listed.Tasks) != 1 || listed.Tasks[0].LocalID != row.LocalID {
		t.Fatalf("TaskStatus owner list = %+v, want only owned task", listed)
	}
}

func TestA2AToolsRuntimePreflightRequiresManagerAndReportsBlockers(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:        t.TempDir(),
		Config:         a2a.Config{AgentID: "adam-n200", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:   "guild-1",
		BoundChannelID: "channel-1",
		ConnectNATS:    false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	unauthorized, err := svc.RuntimePreflight(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "user", RequestedByID: "user-1"})
	if err != nil {
		t.Fatalf("RuntimePreflight unauthorized err: %v", err)
	}
	if unauthorized.OK || unauthorized.ErrorCode != a2a.ErrorPolicyDenied {
		t.Fatalf("RuntimePreflight unauthorized = %+v, want policy denied", unauthorized)
	}
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{
		GuildID:             "guild-1",
		ChannelID:           "channel-1",
		Enabled:             true,
		Discoverable:        true,
		RuntimeAgentID:      "adam-n200-main",
		BotAgentID:          "adam-n200",
		ChannelRef:          "main",
		AcceptFrom:          []string{"eve-local"},
		AcceptSkills:        []string{"task"},
		DelegateTargets:     []a2a.DelegateTargetPolicy{{AgentID: "eve-local", ChannelRef: "support", SkillID: "general/task"}},
		ResultVisibility:    "proxy",
		DelegateMedia:       a2a.DelegateMediaPolicy{},
		RemoteToolPolicy:    a2a.RemoteToolPolicy{},
		MaxConcurrent:       0,
		ShareDiscordContext: false,
	}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	resp, err := svc.RuntimePreflight(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true})
	if err != nil {
		t.Fatalf("RuntimePreflight: %v", err)
	}
	if !resp.OK || resp.RuntimePreflight == nil || resp.RuntimePreflight.Ready || resp.RuntimePreflight.BlockerCount == 0 {
		t.Fatalf("RuntimePreflight = %+v, want blocker report", resp)
	}
}

func TestA2AToolsAnnotations(t *testing.T) {
	readTool := a2aReadTool(ToolA2APeers, "peers")
	if readTool.Annotations.ReadOnlyHint == nil || !*readTool.Annotations.ReadOnlyHint {
		t.Fatalf("read tool readOnlyHint = %+v, want true", readTool.Annotations.ReadOnlyHint)
	}

	writeTool := a2aWriteTool(ToolA2APolicyApply, "apply", true, true)
	if writeTool.Annotations.ReadOnlyHint == nil || *writeTool.Annotations.ReadOnlyHint {
		t.Fatalf("write tool readOnlyHint = %+v, want false", writeTool.Annotations.ReadOnlyHint)
	}
	if writeTool.Annotations.DestructiveHint == nil || !*writeTool.Annotations.DestructiveHint {
		t.Fatalf("policy apply destructiveHint = %+v, want true", writeTool.Annotations.DestructiveHint)
	}
	if writeTool.Annotations.OpenWorldHint == nil || *writeTool.Annotations.OpenWorldHint {
		t.Fatalf("policy apply openWorldHint = %+v, want false", writeTool.Annotations.OpenWorldHint)
	}
}
