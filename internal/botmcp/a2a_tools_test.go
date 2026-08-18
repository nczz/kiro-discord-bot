package botmcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/internal/channelmeta"
)

func runBotMCPEmbeddedNATS(t *testing.T) *natsserver.Server {
	t.Helper()
	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
		NoSigs:    true,
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("new nats server: %v", err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		srv.Shutdown()
		t.Fatal("nats server did not become ready")
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.WaitForShutdown()
	})
	return srv
}

func TestA2AServiceDefaultConfirmationSecretIsProcessRandom(t *testing.T) {
	t.Setenv("A2A_CONFIRMATION_SECRET", "")
	t.Setenv("DISCORD_TOKEN", "")
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:     t.TempDir(),
		Config:      a2a.Config{AgentID: "adam-n200"},
		ConnectNATS: false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	if svc.cfg.ConfirmationSecret == "" {
		t.Fatal("default confirmation secret is empty")
	}
	if svc.cfg.ConfirmationSecret == "kiro-a2a-dev-confirmation-secret" {
		t.Fatal("default confirmation secret used the old hardcoded literal")
	}
}

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

	planReq := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, Enable: &enable, ChannelRef: "case/alpha", AcceptFromRuntimes: []string{"peer-n100-alpha"}, AcceptSkills: []string{"task"}, ExposeSkills: []string{"search-case"}, DelegateTo: []string{"peer-n100"}, DelegateSkills: []string{"summarize-case"}, CoPresentFromRuntimes: []string{"peer-n100-alpha"}, CoPresentTargetChannels: []string{"channel-2"}}
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
	if !applied.OK || applied.Policy == nil || !applied.Policy.Enabled || applied.Policy.ChannelRef != "case/alpha" || !stringListAllows(applied.Policy.AcceptFromRuntimes, "peer-n100-alpha") || !stringListAllows(applied.Policy.CoPresentFromRuntimes, "peer-n100-alpha") || len(applied.Policy.CoPresentTargetChannels) != 1 || applied.Policy.CoPresentTargetChannels[0] != "channel-2" {
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
		BoundGuildID:       "111111111111111111",
		BoundChannelID:     "222222222222222222",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{
		GuildID:            "111111111111111111",
		ChannelID:          "222222222222222222",
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
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-n100-support", runtimeCard, a2a.ExtendedAgentCard{Runtime: "channel", BotAgentID: "peer-n100", ChannelRef: "support", DisplayName: "Support Room", DiscordGuildID: "111111111111111111", DiscordChannelID: "222222222222222222", DiscordThreadID: "333333333333333333"}, false, "peer-host-peer-n100-support", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert runtime card: %v", err)
	}
	migratedCard := a2a.AgentCard{Name: "peer-n100-ch-abcd1234", Description: "support runtime renamed", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "ch-abcd1234/task", Name: "Task", Description: "task"}}}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-n100-ch-abcd1234", migratedCard, a2a.ExtendedAgentCard{Runtime: "channel", BotAgentID: "peer-n100", ChannelRef: "ch-abcd1234", DisplayName: "Support Room", DiscordGuildID: "111111111111111111", DiscordChannelID: "222222222222222222"}, false, "peer-host-peer-n100-ch-abcd1234", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert migrated runtime card: %v", err)
	}
	resp, err := svc.Peers(ctx, A2AToolRequest{GuildID: "111111111111111111", ChannelID: "222222222222222222", RequestedBy: "alice", RequestedByID: "user-1"})
	if err != nil {
		t.Fatalf("Peers: %v", err)
	}
	byAgent := map[string]A2APeerSummary{}
	for _, peer := range resp.Peers {
		byAgent[peer.AgentID] = peer
	}
	if _, ok := byAgent["peer-n100"]; ok {
		t.Fatalf("base peer leaked into runtime peers = %+v", byAgent["peer-n100"])
	}
	runtime := byAgent["peer-n100-support"]
	if !runtime.DelegationAllowed || !runtime.Wakeable || runtime.Runtime != "channel" || runtime.ChannelRef != "support" {
		t.Fatalf("runtime peer = %+v, want wakeable callable channel runtime", runtime)
	}
	if runtime.DisplayName != "Support Room" || runtime.BotAgentID != "peer-n100" || runtime.DelegationReason != "allowed" || runtime.DiscordGuildID != "111111111111111111" || runtime.DiscordChannelID != "222222222222222222" || runtime.DiscordThreadID != "333333333333333333" {
		t.Fatalf("runtime identity = %+v, want Discord identifiers and display name", runtime)
	}
	if len(runtime.Skills) != 1 || runtime.Skills[0] != "support/task" {
		t.Fatalf("runtime skills = %v, want canonical runtime skill", runtime.Skills)
	}
	migrated := byAgent["peer-n100-ch-abcd1234"]
	if !migrated.DelegationAllowed || migrated.DelegationReason != "allowed" || len(migrated.Skills) != 1 || migrated.Skills[0] != "ch-abcd1234/task" {
		t.Fatalf("migrated runtime peer = %+v, want same-channel legacy delegation to remain allowed", migrated)
	}
}

func TestA2AToolsPeersUsesPolicyTaskWhenPeerCardHasNoSkills(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", RuntimeIDMode: a2a.RuntimeIDModeRuntime, RequireConfirmationForRemote: true},
		BoundGuildID:       "111111111111111111",
		BoundChannelID:     "222222222222222222",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{
		GuildID:        "111111111111111111",
		ChannelID:      "222222222222222222",
		Enabled:        true,
		Discoverable:   true,
		RuntimeAgentID: "adam-n200-main",
		BotAgentID:     "adam-n200",
		ChannelRef:     "main",
		DelegateTargets: []a2a.DelegateTargetPolicy{{
			RuntimeAgentID: "peer-n100-empty",
			ChannelRef:     "empty",
			SkillID:        "task",
		}},
	}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	card := a2a.AgentCard{Name: "peer-n100-empty", Description: "runtime without advertised skills", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-n100-empty", card, a2a.ExtendedAgentCard{Runtime: "channel", BotAgentID: "peer-n100", ChannelRef: "empty", DisplayName: "Empty Skills", DiscordGuildID: "111111111111111111", DiscordChannelID: "222222222222222222"}, false, "peer-host-peer-n100-empty", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert runtime card: %v", err)
	}

	resp, err := svc.Peers(ctx, A2AToolRequest{GuildID: "111111111111111111", ChannelID: "222222222222222222", RequestedBy: "alice", RequestedByID: "user-1"})
	if err != nil {
		t.Fatalf("Peers: %v", err)
	}
	if len(resp.Peers) != 1 {
		t.Fatalf("Peers = %+v, want one runtime peer", resp.Peers)
	}
	summary := resp.Peers[0]
	if !summary.DelegationAllowed || !summary.Wakeable || summary.DelegationReason != "allowed" || summary.HiddenSkillCount != 0 {
		t.Fatalf("peer summary = %+v, want policy-authorized default task delegation", summary)
	}
	if len(summary.Skills) != 1 || summary.Skills[0] != "task" {
		t.Fatalf("peer skills = %+v, want policy default task even without peer card skills", summary.Skills)
	}
	delegated, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "111111111111111111", ChannelID: "222222222222222222", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "peer-n100-empty", Message: "ping"})
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if !delegated.RequiresConfirmation || !strings.Contains(delegated.ConfirmationSummary, "peer-n100-empty@empty/task") {
		t.Fatalf("Delegate = %+v, want default task confirmation from policy target", delegated)
	}
}

func TestA2AToolsPeersReportsCurrentChannelPerspective(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:      t.TempDir(),
		Config:       a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime, TaskTimeoutSec: 60},
		BoundGuildID: "111111111111111111",
		ConnectNATS:  false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{
		GuildID:            "111111111111111111",
		ChannelID:          "222222222222222222",
		Enabled:            true,
		Discoverable:       true,
		RuntimeAgentID:     "local-bot-channel-1",
		BotAgentID:         "local-bot",
		ChannelRef:         "main",
		AcceptFromRuntimes: []string{"peer-bot-support"},
		AcceptSkills:       []string{"task"},
		DelegateTargets: []a2a.DelegateTargetPolicy{{
			RuntimeAgentID: "peer-bot-support",
			ChannelRef:     "support",
			SkillID:        "support/task",
		}},
	}, "manager"); err != nil {
		t.Fatalf("Save channel-1 policy: %v", err)
	}
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{
		GuildID:        "111111111111111111",
		ChannelID:      "333333333333333333",
		Enabled:        false,
		Discoverable:   false,
		RuntimeAgentID: "local-bot-channel-2",
		BotAgentID:     "local-bot",
		ChannelRef:     "other",
	}, "manager"); err != nil {
		t.Fatalf("Save channel-2 policy: %v", err)
	}
	card := a2a.AgentCard{Name: "peer-bot-support", Description: "support runtime", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "support/task", Name: "Task", Description: "task"}}}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-bot-support", card, a2a.ExtendedAgentCard{Runtime: "channel", BotAgentID: "peer-bot", ChannelRef: "support", DisplayName: "Support", DiscordGuildID: "111111111111111111", DiscordChannelID: "444444444444444444"}, false, "peer-bot-support", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert runtime peer: %v", err)
	}

	fromMain, err := svc.Peers(ctx, A2AToolRequest{GuildID: "111111111111111111", ChannelID: "222222222222222222", RequestedBy: "alice", RequestedByID: "user-1"})
	if err != nil {
		t.Fatalf("Peers main: %v", err)
	}
	fromOther, err := svc.Peers(ctx, A2AToolRequest{GuildID: "111111111111111111", ChannelID: "333333333333333333", RequestedBy: "alice", RequestedByID: "user-1"})
	if err != nil {
		t.Fatalf("Peers other: %v", err)
	}
	if fromMain.PeerPolicy == nil || !fromMain.PeerPolicy.Enabled || fromMain.PeerPolicy.CurrentRuntimeAgentID != "local-bot-channel-1" || !containsString(fromMain.PeerPolicy.InboundAllowedRuntimes, "peer-bot-support") || len(fromMain.PeerPolicy.OutboundDelegateTargets) != 1 {
		t.Fatalf("main peer policy = %+v, want channel-1 inbound and outbound perspective", fromMain.PeerPolicy)
	}
	if len(fromMain.Peers) != 1 || !fromMain.Peers[0].InboundAllowed || !fromMain.Peers[0].DelegationAllowed {
		t.Fatalf("main peer summary = %+v, want inbound and outbound allowed from channel-1 perspective", fromMain.Peers)
	}
	if fromOther.PeerPolicy == nil || fromOther.PeerPolicy.Enabled || fromOther.PeerPolicy.CurrentRuntimeAgentID != "local-bot-channel-2" || len(fromOther.PeerPolicy.InboundAllowedRuntimes) != 0 || len(fromOther.PeerPolicy.OutboundDelegateTargets) != 0 {
		t.Fatalf("other peer policy = %+v, want channel-2-local empty policy perspective", fromOther.PeerPolicy)
	}
	if len(fromOther.Peers) != 1 || fromOther.Peers[0].InboundAllowed || fromOther.Peers[0].DelegationAllowed {
		t.Fatalf("other peer summary = %+v, want peer not inbound-authorized for channel-2", fromOther.Peers)
	}
}

func TestA2AToolsPeersRuntimeModeHidesStaleRuntimeRows(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir: t.TempDir(),
		Config:  a2a.Config{AgentID: "adam-n200", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	card := a2a.AgentCard{Name: "peer-n100-ch-support", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "ch-support/task", Name: "Task"}}}
	activeCard := card
	activeCard.Name = "peer-n100-ch-active"
	ext := a2a.ExtendedAgentCard{Runtime: "channel", BotAgentID: "peer-n100", ChannelRef: "ch-support", DisplayName: "Support", DiscordGuildID: "111111111111111111", DiscordChannelID: "222222222222222222"}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-n100-ch-support", card, ext, false, "peer-n100-ch-support", "online", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("UpsertExtendedCard stale: %v", err)
	}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-n100-ch-active", activeCard, ext, false, "peer-n100-ch-active", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("UpsertExtendedCard active: %v", err)
	}
	resp, err := svc.Peers(ctx, A2AToolRequest{GuildID: "111111111111111111", ChannelID: "222222222222222222", RequestedBy: "alice", RequestedByID: "user-1"})
	if err != nil {
		t.Fatalf("Peers: %v", err)
	}
	foundActive := false
	for _, peer := range resp.Peers {
		if peer.AgentID == "peer-n100-ch-support" {
			t.Fatalf("stale runtime peer leaked: %+v", resp.Peers)
		}
		if peer.AgentID == "peer-n100-ch-active" {
			foundActive = true
		}
	}
	if !foundActive {
		t.Fatalf("active runtime peer missing: %+v", resp.Peers)
	}
}

func TestA2AToolsDefaultPolicyUsesDiscordChannelNameAlias(t *testing.T) {
	dataDir := t.TempDir()
	if err := channelmeta.Upsert(dataDir, channelmeta.Entry{ID: "channel-1", GuildID: "guild-1", Name: "Backend Support", Type: "channel"}); err != nil {
		t.Fatalf("channel metadata: %v", err)
	}
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:        dataDir,
		Config:         a2a.Config{AgentID: "adam-n200", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:   "guild-1",
		BoundChannelID: "channel-1",
		ConnectNATS:    false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	policy, err := svc.currentPolicy(context.Background(), A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1"})
	if err != nil {
		t.Fatalf("currentPolicy: %v", err)
	}
	if !strings.HasPrefix(policy.ChannelRef, "backend-support-") || !strings.HasPrefix(policy.RuntimeAgentID, "adam-n200-backend-support-") {
		t.Fatalf("default policy = %+v, want disambiguated channel-name runtime alias", policy)
	}
	if err := channelmeta.Upsert(dataDir, channelmeta.Entry{ID: "channel-2", GuildID: "guild-1", Name: "Backend Support", Type: "channel"}); err != nil {
		t.Fatalf("channel metadata 2: %v", err)
	}
	other, err := svc.currentPolicy(context.Background(), A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-2"})
	if err != nil {
		t.Fatalf("currentPolicy other: %v", err)
	}
	if other.ChannelRef == policy.ChannelRef || other.RuntimeAgentID == policy.RuntimeAgentID || !strings.HasPrefix(other.ChannelRef, "backend-support-") {
		t.Fatalf("default policy collision: first=%+v second=%+v", policy, other)
	}
}

func TestA2AToolsOriginRuntimeRefUsesServerBoundChannel(t *testing.T) {
	dataDir := t.TempDir()
	if err := channelmeta.Upsert(dataDir, channelmeta.Entry{ID: "channel-1", GuildID: "guild-1", Name: "隨口問", Type: "channel"}); err != nil {
		t.Fatalf("channel metadata: %v", err)
	}
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:        dataDir,
		Config:         a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:   "guild-1",
		BoundChannelID: "channel-1",
		ConnectNATS:    false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	policy, err := svc.currentPolicy(context.Background(), A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1"})
	if err != nil {
		t.Fatalf("currentPolicy: %v", err)
	}
	source := sourceAgentForRuntimeMode(svc.cfg.Config, policy)
	ref := svc.originRuntimeRef(A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1"}, policy, source, "msg_origin")
	if ref.RuntimeAgentID != source || ref.BotAgentID != "local-bot" || ref.ChannelRef != policy.ChannelRef || ref.DisplayName != "隨口問" || ref.DiscordGuildID != "guild-1" || ref.DiscordChannelID != "channel-1" || ref.MessageID != "msg_origin" {
		t.Fatalf("origin runtime ref = %+v, source=%s policy=%+v", ref, source, policy)
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
	if policy.ResultVisibility != "transparent" || policy.DiscordTranscriptMode != "mirror" || policy.ShareDiscordContext {
		t.Fatalf("cross-runtime auto defaults = visibility %q transcript %q share %v, want transparent/mirror/false", policy.ResultVisibility, policy.DiscordTranscriptMode, policy.ShareDiscordContext)
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

func TestA2AToolsPolicySetupDefaultsWithoutModeUsesUnknownMirror(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", TaskTimeoutSec: 60, MaxDelegationDepth: 1},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	req := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "peer-n100", SkillID: "general/task", ChannelRef: "m5-main"}
	planned, err := svc.PolicyPlan(ctx, req)
	if err != nil {
		t.Fatalf("PolicyPlan: %v", err)
	}
	if !planned.OK || planned.Policy == nil {
		t.Fatalf("PolicyPlan = %+v, want planned policy", planned)
	}
	policy := planned.Policy
	if policy.ResultVisibility != "transparent" || policy.DiscordTranscriptMode != "mirror" || policy.ShareDiscordContext {
		t.Fatalf("implicit unknown setup defaults = visibility %q transcript %q share %v, want transparent/mirror/false", policy.ResultVisibility, policy.DiscordTranscriptMode, policy.ShareDiscordContext)
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

func TestA2AToolsDelegateRejectsNonRuntimePeerBeforePublishing(t *testing.T) {
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
	if got.OK || got.ErrorCode != a2a.ErrorUnauthorizedTarget || !strings.Contains(got.Message, "delegate_targets") {
		t.Fatalf("Delegate to non-runtime peer = %+v, want unauthorized_target delegate_targets guidance", got)
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
	explicitWrong, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "peer-n100-support", TargetChannelRef: "discord-channel-1", SkillID: "task", Message: "ping"})
	if err != nil {
		t.Fatalf("Delegate explicit wrong target ref: %v", err)
	}
	if !strings.Contains(explicitWrong.ConfirmationSummary, "peer-n100-support@support/support/task") {
		t.Fatalf("Delegate explicit wrong target ref summary = %q, want policy target channel_ref", explicitWrong.ConfirmationSummary)
	}
}

func TestA2AToolsPeersFiltersLocalAgent(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:        t.TempDir(),
		Config:         a2a.Config{AgentID: "local-bot", TaskTimeoutSec: 60},
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
	if _, err := svc.peers.UpsertCard(ctx, "local-bot", card("local-bot"), true, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert self: %v", err)
	}
	if _, err := svc.peers.UpsertCard(ctx, "remote-bot", card("remote-bot"), true, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}
	got, err := svc.Peers(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1"})
	if err != nil {
		t.Fatalf("Peers: %v", err)
	}
	if len(got.Peers) != 1 || got.Peers[0].AgentID != "remote-bot" {
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
	req.SetupMode = "auto"
	req.ConfirmationToken = planned.ConfirmationToken
	replayed, err := svc.Delegate(ctx, req)
	if err != nil {
		t.Fatalf("Delegate replay: %v", err)
	}
	if replayed.OK || replayed.ErrorCode != a2a.ErrorPolicyDenied || !strings.Contains(replayed.Message, "confirmation") {
		t.Fatalf("Delegate replay = %+v, want confirmation policy denial", replayed)
	}
}

func TestA2AToolsDelegateWithoutModeUsesSameChannelCoPresentDefault(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", NATSURL: "nats://a2a.example.internal:4222", TaskTimeoutSec: 60, MaxDelegationDepth: 1, RequireConfirmationForRemote: true},
		BoundGuildID:       "111111111111111111",
		BoundChannelID:     "222222222222222222",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{GuildID: "111111111111111111", ChannelID: "222222222222222222", Enabled: true, RuntimeAgentID: "adam-n200-m5-main", BotAgentID: "adam-n200", ChannelRef: "m5-main", DelegateTargets: []a2a.DelegateTargetPolicy{{AgentID: "peer-n100", ChannelRef: "m5-main", SkillID: "general/task"}}, ResultVisibility: "proxy", DiscordTranscriptMode: "delegator"}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	card := a2a.AgentCard{Name: "peer-n100", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "general/task", Name: "General"}}}
	peerExt := a2a.ExtendedAgentCard{DiscordGuildID: "111111111111111111", DiscordChannelID: "222222222222222222", ChannelRef: "m5-main"}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-n100", card, peerExt, true, "peer-instance", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}
	req := A2AToolRequest{GuildID: "111111111111111111", ChannelID: "222222222222222222", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "peer-n100", SkillID: "general/task", Message: "ping"}
	planned, err := svc.Delegate(ctx, req)
	if err != nil {
		t.Fatalf("Delegate plan: %v", err)
	}
	if !planned.RequiresConfirmation || planned.Metadata["result_visibility"] != "transparent" || planned.Metadata["discord_transcript_mode"] != "co_present" {
		t.Fatalf("Delegate plan metadata = %+v, want same-channel transparent/co_present", planned.Metadata)
	}
}

func TestA2AToolsDelegateDefaultsTreatThreadIDAsSameConversation(t *testing.T) {
	peer := a2a.PeerRow{ExtendedCard: a2a.ExtendedAgentCard{
		DiscordGuildID:   "guild-1",
		DiscordChannelID: "channel-1",
		DiscordThreadID:  "thread-1",
	}}
	req := A2AToolRequest{GuildID: "guild-1", ChannelID: "thread-1"}

	if !sameDiscordConversation(req, peer) {
		t.Fatal("thread channel_id was not treated as the peer card's Discord thread")
	}
	visibility, transcriptMode, reason := runtimeDeliveryDefaultsForPeer("", "", req, peer)
	if visibility != "transparent" || transcriptMode != "co_present" || !strings.Contains(reason, "same Discord") {
		t.Fatalf("thread defaults = %s/%s/%s, want transparent/co_present same Discord", visibility, transcriptMode, reason)
	}
}

func TestA2AToolsDelegateOmittedSkillReasonInfersCrossChannelMirror(t *testing.T) {
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
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{GuildID: "guild-1", ChannelID: "channel-1", Enabled: true, RuntimeAgentID: "adam-n200-m5-main", BotAgentID: "adam-n200", ChannelRef: "m5-main", DelegateTargets: []a2a.DelegateTargetPolicy{{RuntimeAgentID: "peer-n100-erp", ChannelRef: "erp-support", SkillID: "erp-support/general_task"}}}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	card := a2a.AgentCard{Name: "peer-n100-erp", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "erp-support/general_task", Name: "General"}}}
	peerExt := a2a.ExtendedAgentCard{Runtime: "channel", ChannelRef: "erp-support", DiscordGuildID: "guild-1", DiscordChannelID: "channel-2"}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "peer-n100-erp", card, peerExt, true, "peer-instance", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}
	planned, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "peer-n100-erp", Message: "ping"})
	if err != nil {
		t.Fatalf("Delegate plan: %v", err)
	}
	if !planned.RequiresConfirmation || planned.Metadata["result_visibility"] != "transparent" || planned.Metadata["discord_transcript_mode"] != "mirror" {
		t.Fatalf("Delegate plan metadata = %+v, want cross-channel transparent/mirror", planned.Metadata)
	}
	if !strings.Contains(planned.ConfirmationSummary, "peer-n100-erp@erp-support/erp-support/general_task") {
		t.Fatalf("Delegate confirmation summary = %q, want inferred channel_ref and canonical skill", planned.ConfirmationSummary)
	}
}

func TestA2AToolsDelegateAllowsDirectHumanEphemeralRuntimeTarget(t *testing.T) {
	ctx := context.Background()
	srv := runBotMCPEmbeddedNATS(t)
	node, err := a2a.Connect(ctx, a2a.NodeConfig{Config: a2a.Config{NATSURL: srv.ClientURL(), AgentID: "adam-n200"}})
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	defer node.Close()
	if err := a2a.EnsureStreams(ctx, node); err != nil {
		t.Fatalf("EnsureStreams: %v", err)
	}
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", RuntimeIDMode: a2a.RuntimeIDModeRuntime, NATSURL: srv.ClientURL(), TaskTimeoutSec: 60, MaxDelegationDepth: 1},
		Node:               node,
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{GuildID: "guild-1", ChannelID: "channel-1", Enabled: true, RuntimeAgentID: "adam-n200-lobby", BotAgentID: "adam-n200", ChannelRef: "lobby"}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	card := a2a.AgentCard{Name: "d80-kanboard", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: srv.ClientURL(), ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "kanboard/general_task", Name: "General"}}}
	peerExt := a2a.ExtendedAgentCard{Runtime: "channel", BotAgentID: "d80-chunbot", ChannelRef: "kanboard", DiscordGuildID: "guild-1", DiscordChannelID: "channel-2"}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "d80-kanboard", card, peerExt, true, "peer-instance", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}
	got, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", RequestSource: "message", TargetAgent: "d80-kanboard", Message: "create a kanboard task"})
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if !got.OK || got.RequiresConfirmation || got.Task == nil {
		t.Fatalf("Delegate = %+v, want queued without confirmation", got)
	}
	if got.Metadata["authorization_mode"] != "ephemeral_user_request" || got.Metadata["persistent_delegate_target"] != false {
		t.Fatalf("Delegate metadata = %+v, want ephemeral one-shot authorization", got.Metadata)
	}
	policy, err := svc.policies.Get(ctx, "guild-1", "channel-1")
	if err != nil {
		t.Fatalf("Get policy: %v", err)
	}
	if len(policy.DelegateTargets) != 0 {
		t.Fatalf("DelegateTargets = %+v, want one-shot request not persistent policy mutation", policy.DelegateTargets)
	}
	rejected, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "cron", RequestedByID: "system", RequestSource: "cron", TargetAgent: "d80-kanboard", Message: "create a kanboard task"})
	if err != nil {
		t.Fatalf("Delegate cron: %v", err)
	}
	if rejected.OK || rejected.ErrorCode != a2a.ErrorUnauthorizedTarget {
		t.Fatalf("Delegate cron = %+v, want unauthorized target without persistent delegate_targets", rejected)
	}
	remoteState := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(remoteState, []byte(`{"target_channel_id":"channel-1","remote_a2a":true,"source":"message"}`), 0644); err != nil {
		t.Fatalf("write remote target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", remoteState)
	remoteRejected, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", RequestSource: "message", TargetAgent: "d80-kanboard", Message: "create a kanboard task"})
	if err != nil {
		t.Fatalf("Delegate remote: %v", err)
	}
	if remoteRejected.OK || remoteRejected.ErrorCode != a2a.ErrorUnauthorizedTarget {
		t.Fatalf("Delegate remote = %+v, want unauthorized target without persistent delegate_targets", remoteRejected)
	}
}

func TestA2AToolsDelegateAmbiguousOrUnknownTargetErrors(t *testing.T) {
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
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{GuildID: "guild-1", ChannelID: "channel-1", Enabled: true, RuntimeAgentID: "adam-n200-m5-main", BotAgentID: "adam-n200", ChannelRef: "m5-main", DelegateTargets: []a2a.DelegateTargetPolicy{{AgentID: "peer-n100", ChannelRef: "support-a", SkillID: "support-a/task"}, {AgentID: "peer-n100", ChannelRef: "support-b", SkillID: "support-b/task"}}}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	card := a2a.AgentCard{Name: "peer-n100", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "support-a/task", Name: "Task A"}, {ID: "support-b/task", Name: "Task B"}}}
	if _, err := svc.peers.UpsertCard(ctx, "peer-n100", card, true, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}
	ambiguous, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "peer-n100", Message: "ping"})
	if err != nil {
		t.Fatalf("Delegate ambiguous: %v", err)
	}
	if ambiguous.OK || !strings.Contains(ambiguous.Message, "ambiguous") {
		t.Fatalf("Delegate ambiguous = %+v, want ambiguity error", ambiguous)
	}
	unknown, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "missing-peer", Message: "ping"})
	if err != nil {
		t.Fatalf("Delegate unknown: %v", err)
	}
	if unknown.OK || unknown.ErrorCode != a2a.ErrorUnknownAgent {
		t.Fatalf("Delegate unknown = %+v, want unknown_agent", unknown)
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
	row, err := svc.tasks.CreateOutbound(ctx, a2a.TaskRow{TaskID: "task_status", MessageID: "msg_status", ClientTaskRef: "owner-1", FromAgent: "adam-n200", ToAgent: "peer-n100", ChannelID: "channel-1", GuildID: "guild-1", ChannelRef: "d80-main", SkillID: "d80-main/task", State: a2a.TaskStateRejected, Terminal: true, Error: a2a.TaskError{Code: a2a.ErrorChannelNotEnabled, Message: "channel_ref is not enabled"}})
	if err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	if err := svc.tasks.AppendEvent(ctx, a2a.EventRow{TaskID: row.TaskID, Revision: 1, EventType: a2a.EventKindRejected, State: a2a.TaskStateRejected, PayloadJSON: `{"taskId":"task_status","state":"TASK_STATE_REJECTED","content":"executor rejected target channel","error":{"Code":"channel_not_enabled","Message":"channel_ref is not enabled"},"revision":1}`}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
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
	if !got.OK || got.Task == nil || got.Task.TaskID != string(row.TaskID) || !got.Task.Terminal || got.Task.State != a2a.TaskStateRejected || got.Task.ChannelRef != "d80-main" || got.Task.ErrorCode != a2a.ErrorChannelNotEnabled {
		t.Fatalf("TaskStatus manager = %+v, want task", got)
	}
	messageReq := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "owner", RequestedByID: "owner-1", TaskID: "msg_status"}
	byMessage, err := svc.TaskStatus(ctx, messageReq)
	if err != nil {
		t.Fatalf("TaskStatus by message id: %v", err)
	}
	if !byMessage.OK || byMessage.Task == nil || byMessage.Task.MessageID != "msg_status" || len(byMessage.Task.Events) != 1 || byMessage.Task.Events[0].Content != "executor rejected target channel" {
		t.Fatalf("TaskStatus by message id = %+v, want task events", byMessage)
	}
	crossGuildReq := A2AToolRequest{GuildID: "guild-2", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, LocalID: row.LocalID}
	crossGuild, err := svc.TaskStatus(ctx, crossGuildReq)
	if err != nil {
		t.Fatalf("TaskStatus cross guild manager: %v", err)
	}
	if crossGuild.OK || crossGuild.ErrorCode != a2a.ErrorPolicyDenied {
		t.Fatalf("TaskStatus cross guild manager = %+v, want policy_denied", crossGuild)
	}
	ownerReq := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "owner", RequestedByID: "owner-1"}
	listed, err := svc.TaskStatus(ctx, ownerReq)
	if err != nil {
		t.Fatalf("TaskStatus owner list: %v", err)
	}
	if !listed.OK || len(listed.Tasks) != 1 || listed.Tasks[0].LocalID != row.LocalID || listed.Tasks[0].ChannelRef != "d80-main" || listed.Tasks[0].ErrorMessage != "channel_ref is not enabled" {
		t.Fatalf("TaskStatus owner list = %+v, want authoritative terminal task state", listed)
	}
}

func TestA2AToolsTaskStatusOmitsCoPresentResultText(t *testing.T) {
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
	row, err := svc.tasks.CreateOutbound(ctx, a2a.TaskRow{
		TaskID:                "task_copresent",
		MessageID:             "msg_copresent",
		ClientTaskRef:         "owner-1",
		FromAgent:             "adam-n200",
		ToAgent:               "peer-n100",
		ChannelID:             "channel-1",
		GuildID:               "guild-1",
		State:                 a2a.TaskStateCompleted,
		ResultVisibility:      "transparent",
		DiscordTranscriptMode: "co_present",
		Terminal:              true,
		DiscordContextJSON:    `{"guildId":"guild-1","channelId":"channel-1","threadId":"thread-1"}`,
	})
	if err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	if err := svc.tasks.AppendEvent(ctx, a2a.EventRow{TaskID: row.TaskID, Revision: 1, EventType: a2a.EventKindResult, State: a2a.TaskStateCompleted, PayloadJSON: `{"taskId":"task_copresent","state":"TASK_STATE_COMPLETED","content":"executor posted final text","revision":1}`}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	got, err := svc.TaskStatus(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "owner", RequestedByID: "owner-1", LocalID: row.LocalID})
	if err != nil {
		t.Fatalf("TaskStatus: %v", err)
	}
	if !got.OK || got.Task == nil || len(got.Task.Events) != 1 {
		t.Fatalf("TaskStatus = %+v, want task event", got)
	}
	if got.Task.Events[0].Content != "" || !strings.Contains(got.Message, "Do not post a follow-up") || got.Metadata["discord_thread_id"] != "thread-1" {
		t.Fatalf("TaskStatus co-present result = %+v metadata=%+v message=%q, want omitted content with follow-up guidance", got.Task.Events[0], got.Metadata, got.Message)
	}
}

func TestA2AToolsDelegateSuccessMessageRequiresStatusCheck(t *testing.T) {
	got := delegateSuccessMessage("proxy", "delegator", "policy/default delivery settings")
	for _, want := range []string{"request queued", "task status", "before claiming"} {
		if !strings.Contains(got, want) {
			t.Fatalf("delegateSuccessMessage proxy = %q, missing %q", got, want)
		}
	}
	coPresent := delegateSuccessMessage("transparent", "co_present", "same Discord channel")
	for _, want := range []string{"executor owns the shared Discord thread", "must not repost", "task status"} {
		if !strings.Contains(coPresent, want) {
			t.Fatalf("delegateSuccessMessage co_present = %q, missing %q", coPresent, want)
		}
	}
}

func TestA2AToolsAutoDeliveryUsesCoPresentForSameDiscordRuntime(t *testing.T) {
	req := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1"}
	peer := a2a.PeerRow{ExtendedCard: a2a.ExtendedAgentCard{DiscordGuildID: "guild-1", DiscordChannelID: "channel-1"}}
	visibility, mode, reason := runtimeDeliveryDefaultsForPeer("m5-main", "d80-main", req, peer)
	if visibility != "transparent" || mode != "co_present" || !strings.Contains(reason, "same Discord channel") {
		t.Fatalf("runtimeDeliveryDefaultsForPeer = %s/%s (%s), want transparent/co_present same-channel reason", visibility, mode, reason)
	}
	delivery := deliveryOptionsForDelegate(req, visibility, mode, "local-bot-m5-main", 60, 1)
	if !delivery.ShareDiscordContext || delivery.CoPresentFrom != "local-bot-m5-main" || delivery.DiscordContext == nil || len(delivery.DiscordContextJSON) == 0 {
		t.Fatalf("deliveryOptionsForDelegate = %+v, want shared Discord context from runtime", delivery)
	}
}

func TestA2AToolsAuditDiscordFieldsUsesThreadTarget(t *testing.T) {
	delivery := a2a.DeliveryOptions{
		DiscordReplyChannelID: "parent-channel",
		DiscordReplyThreadID:  "thread-1",
	}
	targetID, parentChannelID, threadID := auditDiscordFields(A2AToolRequest{ChannelID: "caller-channel"}, delivery)
	if targetID != "thread-1" || parentChannelID != "parent-channel" || threadID != "thread-1" {
		t.Fatalf("auditDiscordFields = %q/%q/%q, want thread target with parent channel", targetID, parentChannelID, threadID)
	}
}

func TestA2AToolsTaskStatusManagerCannotCrossGuild(t *testing.T) {
	err := authorizeTaskStatus(a2a.TaskRow{GuildID: "guild-1", ChannelID: "channel-1", ClientTaskRef: "owner-1"}, A2AToolRequest{GuildID: "guild-2", ChannelID: "channel-2", RequestedByID: "manager-1", ManageChannels: true})
	if err == nil || !strings.Contains(err.Error(), string(a2a.ErrorPolicyDenied)) {
		t.Fatalf("authorizeTaskStatus cross guild = %v, want policy_denied", err)
	}
}

func TestA2AToolsTaskStatusManagerCannotCrossChannel(t *testing.T) {
	err := authorizeTaskStatus(a2a.TaskRow{GuildID: "guild-1", ChannelID: "channel-2", ClientTaskRef: "owner-1"}, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedByID: "manager-1", ManageChannels: true})
	if err == nil || !strings.Contains(err.Error(), string(a2a.ErrorPolicyDenied)) {
		t.Fatalf("authorizeTaskStatus cross channel = %v, want policy_denied", err)
	}
}

func TestA2AToolsTaskLookupRejectsTaskMessageAmbiguity(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{DataDir: t.TempDir(), Config: a2a.Config{AgentID: "adam-n200", TaskTimeoutSec: 60}, ConnectNATS: false})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	if _, err := svc.tasks.CreateOutbound(ctx, a2a.TaskRow{TaskID: "task_real", MessageID: "shared_lookup", ClientTaskRef: "owner-1", FromAgent: "adam-n200", ToAgent: "peer-n100", ChannelID: "channel-1", GuildID: "guild-1", State: a2a.TaskStateSubmitted}); err != nil {
		t.Fatalf("CreateOutbound A: %v", err)
	}
	if _, err := svc.tasks.CreateOutbound(ctx, a2a.TaskRow{TaskID: "shared_lookup", MessageID: "other_message", ClientTaskRef: "owner-1", FromAgent: "adam-n200", ToAgent: "peer-n100", ChannelID: "channel-1", GuildID: "guild-1", State: a2a.TaskStateSubmitted}); err != nil {
		t.Fatalf("CreateOutbound B: %v", err)
	}
	_, err = svc.lookupOutboundTaskOrMessage(ctx, "shared_lookup")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("lookupOutboundTaskOrMessage ambiguity = %v, want ambiguity error", err)
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

func TestA2AToolsTrustPeerDefaultsInboundConsent(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime, RequireConfirmationForRemote: true},
		BoundGuildID:       "111111111111111111",
		BoundChannelID:     "222222222222222222",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
		Now:                func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	peerCard := a2a.AgentCard{Name: "remote-bot-ch-2cbaf623", Description: "runtime", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "ch-2cbaf623/general_task", Name: "Task", Description: "task"}, {ID: "ch-2cbaf623/review", Name: "Review", Description: "review"}}}
	peerExt := a2a.ExtendedAgentCard{ChannelRef: "ch-2cbaf623", Runtime: "channel", DiscordGuildID: "111111111111111111", DiscordChannelID: "222222222222222222"}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "remote-bot-ch-2cbaf623", peerCard, peerExt, false, "peer-instance", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}

	req := A2AToolRequest{GuildID: "111111111111111111", ChannelID: "222222222222222222", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "remote-bot-ch-2cbaf623", PolicyAction: "trust"}
	applied, err := svc.TrustPeer(ctx, req)
	if err != nil {
		t.Fatalf("TrustPeer apply: %v", err)
	}
	if !applied.OK || applied.RequiresConfirmation || applied.Policy == nil {
		t.Fatalf("TrustPeer apply = %+v, want direct applied policy", applied)
	}
	policy := applied.Policy
	if !policy.Enabled || !policy.Discoverable || policy.ResultVisibility != "transparent" || policy.DiscordTranscriptMode != "co_present" || !policy.ShareDiscordContext {
		t.Fatalf("applied delivery policy = %+v, want same-Discord co_present", policy)
	}
	if !stringListAllows(policy.AcceptFromRuntimes, "remote-bot-ch-2cbaf623") || len(policy.AcceptSkills) != 0 {
		t.Fatalf("applied inbound policy = %+v, want runtime consent without skill restriction", policy)
	}
	if len(policy.AcceptFrom) != 0 || len(policy.DelegateTargets) != 0 || len(policy.DelegateTo) != 0 || len(policy.DelegateSkills) != 0 {
		t.Fatalf("applied relationship = %+v, want inbound-only consent without outbound delegation", policy)
	}
	if policy.MaxConcurrent != 1 || policy.RemoteToolPolicy.AllowMemoryWrite {
		t.Fatalf("planned receiver safety = max %d memory %v, want max=1 memory=false", policy.MaxConcurrent, policy.RemoteToolPolicy.AllowMemoryWrite)
	}
	if err := policy.ValidateInboundRuntime("remote-bot-ch-2cbaf623", "ch-2cbaf623/review"); err != nil {
		t.Fatalf("applied policy inbound validation for exposed capability: %v", err)
	}
	peer, err := svc.peers.Get(ctx, "remote-bot-ch-2cbaf623")
	if err != nil {
		t.Fatalf("trusted peer lookup: %v", err)
	}
	if peer.Trusted {
		t.Fatalf("trusted peer = %+v, want inbound consent without outbound trust marker", peer)
	}
	stored, err := svc.policies.Get(ctx, req.GuildID, req.ChannelID)
	if err != nil {
		t.Fatalf("stored policy lookup: %v", err)
	}
	if !stringListAllows(stored.AcceptFromRuntimes, "remote-bot-ch-2cbaf623") || len(stored.AcceptSkills) != 0 {
		t.Fatalf("stored inbound policy = %+v, want direct persisted runtime consent", stored)
	}
	peers, err := svc.Peers(ctx, A2AToolRequest{GuildID: "111111111111111111", ChannelID: "222222222222222222", RequestedBy: "alice", RequestedByID: "user-1"})
	if err != nil {
		t.Fatalf("Peers after inbound trust: %v", err)
	}
	if len(peers.Peers) != 1 {
		t.Fatalf("Peers after inbound trust = %+v, want one runtime peer", peers.Peers)
	}
	if peers.PeerPolicy == nil || peers.PeerPolicy.CurrentRuntimeAgentID == "" || !containsString(peers.PeerPolicy.InboundAllowedRuntimes, "remote-bot-ch-2cbaf623") {
		t.Fatalf("Peers peer policy = %+v, want current runtime inbound consent summary", peers.PeerPolicy)
	}
	summary := peers.Peers[0]
	if !summary.InboundAllowed || summary.InboundReason != "allowed by current channel inbound policy" {
		t.Fatalf("peer summary inbound = %+v, want receiver consent visible", summary)
	}
	if summary.DelegationAllowed || summary.Wakeable || summary.HiddenSkillCount != 2 {
		t.Fatalf("peer summary = %+v, want inbound consent without outbound delegation capability", summary)
	}
	if len(summary.Skills) != 0 {
		t.Fatalf("peer summary skills = %+v, want hidden until channel policy delegates them", summary.Skills)
	}
	if summary.DelegationReason != "missing runtime delegate target" {
		t.Fatalf("peer delegation reason = %q, want missing delegate target guidance", summary.DelegationReason)
	}

	delegated, err := svc.Delegate(ctx, A2AToolRequest{GuildID: "111111111111111111", ChannelID: "222222222222222222", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "remote-bot-ch-2cbaf623", Message: "ping"})
	if err != nil {
		t.Fatalf("Delegate after inbound trust: %v", err)
	}
	if delegated.OK || delegated.ErrorCode != a2a.ErrorUnauthorizedTarget || !strings.Contains(delegated.Message, "delegate_targets") {
		t.Fatalf("Delegate after inbound trust = %+v, want unauthorized without local outbound policy", delegated)
	}
}

func TestA2AToolsRevokePeerRemovesSimpleInboundGrant(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
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
		ChannelRef:         "ch-main",
		RuntimeAgentID:     "local-bot-ch-main",
		BotAgentID:         "local-bot",
		AcceptFromRuntimes: []string{"remote-bot-ch-2cbaf623"},
		AcceptSkills:       []string{"task"},
		DelegateTo:         []string{"remote-bot-ch-2cbaf623"},
		DelegateTargets:    []a2a.DelegateTargetPolicy{{RuntimeAgentID: "remote-bot-ch-2cbaf623", SkillID: "general/task"}},
	}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}

	resp, err := svc.RevokePeer(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "remote-bot-ch-2cbaf623"})
	if err != nil {
		t.Fatalf("RevokePeer: %v", err)
	}
	if !resp.OK || resp.Policy == nil {
		t.Fatalf("RevokePeer = %+v, want applied policy", resp)
	}
	if len(resp.Policy.AcceptFromRuntimes) != 0 || len(resp.Policy.AcceptSkills) != 0 || len(resp.Policy.DelegateTo) != 0 || len(resp.Policy.DelegateTargets) != 0 {
		t.Fatalf("revoked policy = %+v, want peer grants removed", resp.Policy)
	}
}

func TestA2AToolsTrustPeerSimpleInboundRequiresKnownPeer(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	resp, err := svc.TrustPeer(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "unknown-peer"})
	if err != nil {
		t.Fatalf("TrustPeer unknown simple inbound: %v", err)
	}

	if resp.OK || resp.ErrorCode != a2a.ErrorUnknownAgent {
		t.Fatalf("TrustPeer unknown simple inbound = %+v, want unknown_agent", resp)
	}
}
func TestA2AToolsTrustPeerSimpleInboundDoesNotWidenSameSenderSkills(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{GuildID: "guild-1", ChannelID: "channel-1", Enabled: true, ChannelRef: "ch-main", RuntimeAgentID: "local-bot-ch-main", BotAgentID: "local-bot", AcceptFromRuntimes: []string{"existing-peer"}, AcceptSkills: []string{"task"}}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	card := a2a.AgentCard{Name: "existing-peer", Description: "runtime", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "existing/review", Name: "Review", Description: "review"}}}
	if _, err := svc.peers.UpsertCard(ctx, "existing-peer", card, false, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}

	resp, err := svc.TrustPeer(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "existing-peer", PolicyAction: "trust"})
	if err != nil {
		t.Fatalf("TrustPeer same sender restricted skills: %v", err)
	}
	if !resp.OK {
		t.Fatalf("TrustPeer same sender restricted skills = %+v, want ok without widening skills", resp)
	}
	stored, err := svc.policies.Get(ctx, "guild-1", "channel-1")
	if err != nil {
		t.Fatalf("stored policy lookup: %v", err)
	}
	if len(stored.AcceptSkills) != 1 || stored.AcceptSkills[0] != "task" || len(stored.AcceptFromRuntimes) != 1 || stored.AcceptFromRuntimes[0] != "existing-peer" {
		t.Fatalf("stored policy skills/runtimes not preserved: %+v", stored)
	}
}

func TestA2AToolsTrustPeerSimpleInboundDoesNotWidenExistingSenderSkills(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{GuildID: "guild-1", ChannelID: "channel-1", Enabled: true, ChannelRef: "ch-main", RuntimeAgentID: "local-bot-ch-main", BotAgentID: "local-bot", AcceptFromRuntimes: []string{"existing-peer"}, AcceptSkills: []string{"task"}}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	card := a2a.AgentCard{Name: "new-peer", Description: "runtime", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "new/review", Name: "Review", Description: "review"}}}
	if _, err := svc.peers.UpsertCard(ctx, "new-peer", card, false, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}

	resp, err := svc.TrustPeer(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "new-peer", PolicyAction: "trust"})
	if err != nil {
		t.Fatalf("TrustPeer restricted existing sender: %v", err)
	}
	if !resp.OK {
		t.Fatalf("TrustPeer restricted existing sender = %+v, want ok without widening skills", resp)
	}
	stored, err := svc.policies.Get(ctx, "guild-1", "channel-1")
	if err != nil {
		t.Fatalf("stored policy lookup: %v", err)
	}
	if len(stored.AcceptSkills) != 1 || stored.AcceptSkills[0] != "task" || len(stored.AcceptFromRuntimes) != 2 || stored.AcceptFromRuntimes[0] != "existing-peer" || stored.AcceptFromRuntimes[1] != "new-peer" {
		t.Fatalf("stored policy should add only runtime and preserve skill restriction: %+v", stored)
	}
}

func TestA2AToolsTrustPeerRejectsExpertSkillField(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	card := a2a.AgentCard{Name: "remote-bot-ch-2cbaf623", Description: "runtime", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "ch-2cbaf623/task", Name: "Task", Description: "task"}}}
	if _, err := svc.peers.UpsertCard(ctx, "remote-bot-ch-2cbaf623", card, false, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}

	resp, err := svc.TrustPeer(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "remote-bot-ch-2cbaf623", SkillID: "task"})
	if err != nil {
		t.Fatalf("TrustPeer expert skill err: %v", err)
	}
	if resp.OK || resp.ErrorCode != a2a.ErrorPolicyDenied || resp.RequiresConfirmation {
		t.Fatalf("TrustPeer expert skill = %+v, want retired expert policy error without confirmation", resp)
	}
}

func TestA2AToolsTrustPeerDefaultUnknownLocationUsesMirror(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	peerCard := a2a.AgentCard{Name: "remote-bot-ch-2cbaf623", Description: "runtime", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "ch-2cbaf623/task", Name: "Task", Description: "task"}}}
	if _, err := svc.peers.UpsertCard(ctx, "remote-bot-ch-2cbaf623", peerCard, false, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}
	planned, err := svc.TrustPeer(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "remote-bot-ch-2cbaf623"})
	if err != nil {
		t.Fatalf("TrustPeer plan: %v", err)
	}
	if !planned.OK || planned.Policy == nil {
		t.Fatalf("TrustPeer plan = %+v, want planned policy", planned)
	}
	if planned.Policy.ResultVisibility != "transparent" || planned.Policy.DiscordTranscriptMode != "mirror" || planned.Policy.ShareDiscordContext {
		t.Fatalf("unknown-location trust defaults = %+v, want transparent/mirror", planned.Policy)
	}
}

func TestA2AToolsTrustPeerRejectsOutboundOnlyCoPresent(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	resp, err := svc.TrustPeer(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "remote-bot-ch-2cbaf623", TrustRelationship: "outbound", SetupMode: "co_present", TargetChannelRef: "ch-2cbaf623"})
	if err != nil {
		t.Fatalf("TrustPeer outbound co_present err: %v", err)
	}
	if resp.OK || resp.ErrorCode != a2a.ErrorPolicyDenied {
		t.Fatalf("TrustPeer outbound co_present = %+v, want policy_denied", resp)
	}
}

func TestA2AToolsTrustPeerRejectsOutboundOnlyAutoCoPresent(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:       "111111111111111111",
		BoundChannelID:     "222222222222222222",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	peerCard := a2a.AgentCard{Name: "remote-bot-ch-2cbaf623", Description: "runtime", Version: "1.0.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Skills: []a2a.AgentSkill{{ID: "ch-2cbaf623/task", Name: "Task", Description: "task"}}}
	peerExt := a2a.ExtendedAgentCard{ChannelRef: "ch-2cbaf623", DiscordGuildID: "111111111111111111", DiscordChannelID: "222222222222222222"}
	if _, err := svc.peers.UpsertExtendedCard(ctx, "remote-bot-ch-2cbaf623", peerCard, peerExt, false, "peer-instance", "online", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Upsert peer: %v", err)
	}

	resp, err := svc.TrustPeer(ctx, A2AToolRequest{GuildID: "111111111111111111", ChannelID: "222222222222222222", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, TargetAgent: "remote-bot-ch-2cbaf623", TrustRelationship: "outbound", SetupMode: "auto", TargetChannelRef: "ch-2cbaf623"})
	if err != nil {
		t.Fatalf("TrustPeer outbound auto err: %v", err)
	}
	if resp.OK || resp.ErrorCode != a2a.ErrorPolicyDenied {
		t.Fatalf("TrustPeer outbound auto same-Discord = %+v, want policy_denied", resp)
	}
}

func TestA2AToolsThreadBoundContextUsesParentChannel(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:        t.TempDir(),
		Config:         a2a.Config{AgentID: "adam-n200", TaskTimeoutSec: 60},
		BoundGuildID:   "guild-1",
		BoundChannelID: "channel-1",
		BoundTargetID:  "thread-1",
		ConnectNATS:    false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{GuildID: "guild-1", ChannelID: "channel-1", Enabled: true, ChannelRef: "ch-parent", RuntimeAgentID: "adam-n200-ch-parent", BotAgentID: "adam-n200"}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	row, err := svc.tasks.CreateOutbound(ctx, a2a.TaskRow{
		TaskID:        "task_thread",
		MessageID:     "msg_thread",
		ClientTaskRef: "owner-1",
		FromAgent:     "adam-n200-ch-parent",
		ToAgent:       "peer-n100",
		ChannelID:     "channel-1",
		GuildID:       "guild-1",
		ChannelRef:    "ch-parent",
		State:         a2a.TaskStateCompleted,
		Terminal:      true,
	})
	if err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}

	req := svc.normalizeBoundContext(A2AToolRequest{GuildID: "guild-1", ChannelID: "thread-1", RequestedBy: "owner", RequestedByID: "owner-1", LocalID: row.LocalID})
	if req.ChannelID != "channel-1" {
		t.Fatalf("normalized ChannelID = %q, want parent channel", req.ChannelID)
	}
	policy, err := svc.currentPolicy(ctx, req)
	if err != nil {
		t.Fatalf("currentPolicy: %v", err)
	}
	if !policy.Enabled || policy.ChannelRef != "ch-parent" {
		t.Fatalf("currentPolicy = %+v, want parent channel policy", policy)
	}
	got, err := svc.TaskStatus(ctx, req)
	if err != nil {
		t.Fatalf("TaskStatus: %v", err)
	}
	if !got.OK || got.Task == nil || got.Task.LocalID != row.LocalID {
		t.Fatalf("TaskStatus = %+v, want parent-channel task visible from thread target", got)
	}
}

func TestA2AToolsPolicyGetIncludesDeliveryReadiness(t *testing.T) {
	ctx := context.Background()
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:        t.TempDir(),
		Config:         a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime},
		BoundGuildID:   "guild-1",
		BoundChannelID: "channel-1",
		ConnectNATS:    false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()
	if err := svc.policies.Save(ctx, a2a.ChannelA2APolicy{
		GuildID:               "guild-1",
		ChannelID:             "channel-1",
		Enabled:               true,
		ChannelRef:            "ch-parent",
		RuntimeAgentID:        "local-bot-ch-parent",
		BotAgentID:            "local-bot",
		AcceptFrom:            []string{"remote-bot-ch-parent"},
		AcceptSkills:          []string{"task"},
		ResultVisibility:      "proxy",
		DiscordTranscriptMode: "delegator",
		ShareDiscordContext:   false,
		CoPresentFrom:         []string{"remote-bot-ch-parent"},
	}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}

	resp, err := svc.PolicyGet(ctx, A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "owner", RequestedByID: "owner-1"})
	if err != nil {
		t.Fatalf("PolicyGet: %v", err)
	}
	if !resp.OK || resp.DeliveryReadiness == nil {
		t.Fatalf("PolicyGet = %+v, want delivery readiness", resp)
	}
	if resp.DeliveryReadiness.CoPresentReady {
		t.Fatalf("CoPresentReady = true, want blocked for safe/proxy policy")
	}
	for _, want := range []string{"result_visibility=transparent", "discord_transcript_mode=co_present", "share_discord_context=true"} {
		if !stringListAllows(resp.DeliveryReadiness.CoPresentMissing, want) {
			t.Fatalf("CoPresentMissing = %v, missing %q", resp.DeliveryReadiness.CoPresentMissing, want)
		}
	}
	if !strings.Contains(resp.DeliveryReadiness.Guidance, "trusted/accept_from alone") {
		t.Fatalf("Guidance = %q, want explicit trust warning", resp.DeliveryReadiness.Guidance)
	}
}

func TestA2APolicyDeliveryReadinessRequiresInboundAuthorization(t *testing.T) {
	ready := policyDeliveryReadiness(a2a.ChannelA2APolicy{
		ResultVisibility:      "transparent",
		DiscordTranscriptMode: "co_present",
		ShareDiscordContext:   true,
		CoPresentFromRuntimes: []string{"peer-runtime"},
	})
	if ready.CoPresentReady {
		t.Fatalf("CoPresentReady = true without enabled/accept policy: %+v", ready)
	}
	for _, want := range []string{"enabled=true", "accept_from or accept_from_runtimes"} {
		if !stringListAllows(ready.CoPresentMissing, want) {
			t.Fatalf("CoPresentMissing = %v, missing %q", ready.CoPresentMissing, want)
		}
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
