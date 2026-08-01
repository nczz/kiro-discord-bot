package a2a

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestAgentCardSanitizer(t *testing.T) {
	card, err := BuildPublicAgentCard(Config{NATSURL: "nats://user:credential@nats.example.internal:4222?auth=credential", AgentID: "adam-n200", AgentDescription: "runtime in /Users/example/project with DISCORD_TOKEN 123456789012345678"}, "2.30.0", []AgentSkill{{ID: "backend/code-review", Name: "Code Review", Description: "reads /data/private", Examples: []string{"sensitive prompt"}, InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}}})
	if err != nil {
		t.Fatalf("BuildPublicAgentCard: %v", err)
	}
	raw, _ := json.Marshal(card)
	text := string(raw)
	for _, forbidden := range []string{"/Users", "/data", "DISCORD_TOKEN", "123456789012345678", "sensitive prompt", "user:credential", "auth=credential"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public card leaked %q: %s", forbidden, text)
		}
	}
	if card.Capabilities["streaming"] || card.Capabilities["pushNotifications"] || !card.Capabilities["extendedAgentCard"] {
		t.Fatalf("unexpected capabilities: %+v", card.Capabilities)
	}
}

func TestExtendedCard(t *testing.T) {
	card := phase4Card("eve-local")
	ext, err := BuildExtendedAgentCard(card, ExtendedAgentCard{ChannelRef: "backend", Runtime: "cwd /var/private", TriggerGuidance: "use mcp.json token", ResultVisibilitySupport: []string{"proxy", "transparent"}, CredentialIssuer: "issuer", CredentialFingerprint: "aa:bb", PublicKeyFingerprint: "cc:dd", SignatureStatus: "verified"})
	if err != nil {
		t.Fatalf("BuildExtendedAgentCard: %v", err)
	}
	raw, _ := json.Marshal(ext)
	if strings.Contains(string(raw), "/var") || strings.Contains(string(raw), "mcp.json") || strings.Contains(string(raw), "token") {
		t.Fatalf("extended card leaked private data: %s", raw)
	}
	if ext.ChannelRef != "backend" || ext.CredentialFingerprint != "aa:bb" || ext.SignatureStatus != "verified" {
		t.Fatalf("unexpected extended card: %+v", ext)
	}
}

func TestBuildRuntimeAgentCardIncludesDiscordIdentifiers(t *testing.T) {
	runtime := RuntimeRecord{
		RuntimeAgentID: "adam-n200-main",
		BotAgentID:     "adam-n200",
		GuildID:        "111111111111111111",
		ChannelID:      "222222222222222222",
		ThreadID:       "333333333333333333",
		ChannelRef:     "main",
		DisplayName:    "Main",
		RuntimeKind:    "channel",
		Enabled:        true,
		Discoverable:   true,
	}
	card, ext, err := BuildRuntimeAgentCard(Config{NATSURL: "nats://nats.example.internal:4222", AgentID: "adam-n200"}, runtime, "2.30.0", []AgentSkill{{ID: "task", Name: "Task", Description: "task"}})
	if err != nil {
		t.Fatalf("BuildRuntimeAgentCard: %v", err)
	}
	if card.Name != "adam-n200-main" || card.Description != "Main" || len(card.Skills) != 1 || card.Skills[0].ID != "main/task" {
		t.Fatalf("runtime card = %+v, want runtime-scoped identity and skill", card)
	}
	if ext.ChannelRef != "main" || ext.Runtime != "channel" || ext.BotAgentID != "adam-n200" || ext.DisplayName != "Main" || ext.DiscordGuildID != "111111111111111111" || ext.DiscordChannelID != "222222222222222222" || ext.DiscordThreadID != "333333333333333333" {
		t.Fatalf("extended runtime card = %+v, want Discord identifiers", ext)
	}
}

func TestRuntimeAliasUsesChannelNameOrStableHash(t *testing.T) {
	if got := RuntimeAlias("Backend Support", "guild\x00channel"); !strings.HasPrefix(got, "backend-support-") || got == "backend-support" {
		t.Fatalf("RuntimeAlias ascii = %q, want readable alias with stable disambiguator", got)
	}
	if got := RuntimeAlias("support-222222222222222222-room", "guild\x00channel"); got == "" || strings.Contains(got, "222222222222222222") || !strings.HasPrefix(got, "ch-") {
		t.Fatalf("RuntimeAlias snowflake = %q, want hashed public alias", got)
	}
	if got := RuntimeAlias("隨口問", "guild\x00channel"); got == "" || strings.Contains(got, "111111") || !strings.HasPrefix(got, "ch-") {
		t.Fatalf("RuntimeAlias unicode = %q, want hashed public alias", got)
	}
	id, err := GenerateRuntimeAgentIDFromAlias("m5bot", "Backend Support", "guild\x00channel")
	if err != nil || id != "m5bot-backend-support" {
		t.Fatalf("GenerateRuntimeAgentIDFromAlias = %q/%v, want m5bot-backend-support", id, err)
	}
	id, err = GenerateRuntimeAgentIDFromAlias("m5bot", "support-222222222222222222-room", "guild\x00channel")
	if err != nil || strings.Contains(string(id), "222222222222222222") || !strings.HasPrefix(string(id), "m5bot-rt-") {
		t.Fatalf("GenerateRuntimeAgentIDFromAlias snowflake = %q/%v, want hashed runtime id", id, err)
	}
}

func TestPeerKV(t *testing.T) {
	node := connectedTestNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	kv, err := EnsurePeerKV(ctx, node, 150*time.Millisecond)
	if err != nil {
		t.Fatalf("EnsurePeerKV: %v", err)
	}
	status, err := kv.Status(ctx)
	if err != nil {
		t.Fatalf("KV status: %v", err)
	}
	if status.Bucket() != PeerKVBucket || status.Config().TTL != 150*time.Millisecond {
		t.Fatalf("unexpected KV config: %+v", status.Config())
	}
}

func TestPeerWatch(t *testing.T) {
	node := connectedTestNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := OpenPeerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := PublishPeerCard(ctx, node, PeerCardRecord{AgentID: "eve-local", InstanceID: "instance-a", Card: phase4Card("eve-local"), ExpiresAt: time.Now().Add(time.Minute)}, time.Minute); err != nil {
		t.Fatalf("PublishPeerCard: %v", err)
	}
	kv, err := EnsurePeerKV(ctx, node, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := kv.Get(ctx, "eve-local")
	if err != nil {
		t.Fatalf("KV get: %v", err)
	}
	if err := ApplyPeerKVEntry(ctx, store, entry); err != nil {
		t.Fatalf("ApplyPeerKVEntry: %v", err)
	}
	peer, err := store.Get(ctx, "eve-local")
	if err != nil {
		t.Fatalf("peer store get: %v", err)
	}
	if peer.InstanceID != "instance-a" || peer.ProtocolBinding != ProtocolBindingNATS {
		t.Fatalf("unexpected peer row: %+v", peer)
	}
	if err := kv.Delete(ctx, "eve-local"); err != nil {
		t.Fatalf("KV delete: %v", err)
	}
	deleted, err := kv.Get(ctx, "eve-local")
	if err == nil {
		if err := ApplyPeerKVEntry(ctx, store, deleted); err != nil {
			t.Fatalf("apply delete marker: %v", err)
		}
	} else {
		if err := store.MarkStale(ctx, "eve-local"); err != nil {
			t.Fatalf("MarkStale fallback: %v", err)
		}
	}
	summary, err := store.TrustSummary(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary) != 1 || !summary[0].Stale {
		t.Fatalf("delete did not mark stale: %+v", summary)
	}
}

func TestHeartbeat(t *testing.T) {
	node := connectedTestNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	payload := HeartbeatPayload{AgentID: "adam-n200", InstanceID: "host-123", Status: "online", ActiveTasks: 2, Version: "2.30.0"}
	payload, raw, err := NormalizeHeartbeat(payload)
	if err != nil {
		t.Fatalf("NormalizeHeartbeat: %v", err)
	}
	if !strings.Contains(string(raw), "host-123") || payload.StartedAt.IsZero() {
		t.Fatalf("unexpected heartbeat: %+v raw=%s", payload, raw)
	}
	if err := PublishHeartbeat(ctx, node, payload); err != nil {
		t.Fatalf("PublishHeartbeat: %v", err)
	}
	store, err := OpenPeerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.UpsertCard(ctx, "adam-n200", phase4Card("adam-n200"), true, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkHeartbeat(ctx, payload, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("MarkHeartbeat: %v", err)
	}
	summary, err := store.TrustSummary(ctx, time.Hour)
	if err != nil || len(summary) != 1 || !summary[0].Online {
		t.Fatalf("heartbeat summary err=%v summary=%+v", err, summary)
	}
}

func TestPeerRequestReplyFallback(t *testing.T) {
	srv := runEmbeddedNATS(t)
	responder, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer responder.Close()
	_, err = responder.Subscribe(PeerFallbackInbox, func(msg *nats.Msg) {
		record := PeerCardRecord{AgentID: "eve-local", InstanceID: "fallback", Card: phase4Card("eve-local"), ExpiresAt: time.Now().Add(time.Minute)}
		_, raw, _ := normalizePeerCardRecord(record)
		_ = responder.Publish(msg.Reply, raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := responder.Flush(); err != nil {
		t.Fatal(err)
	}
	client, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	records, err := CollectPeerCardsFallback(ctx, client, PeerFallbackInbox, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("CollectPeerCardsFallback: %v", err)
	}
	if len(records) != 1 || records[0].AgentID != "eve-local" {
		t.Fatalf("unexpected fallback records: %+v", records)
	}
	if _, err := CollectPeerCardsFallback(ctx, client, "a2a.v1.card.no_responders", 20*time.Millisecond); err == nil {
		t.Fatal("fallback without responders did not report timeout")
	}
}

func TestPeerTrustSummary(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPeerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.UpsertExtendedCard(ctx, "eve-local", phase4Card("eve-local"), ExtendedAgentCard{CredentialIssuer: "issuer", CredentialFingerprint: "aa:bb", SignatureStatus: "verified"}, true, "instance", "online", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("UpsertExtendedCard: %v", err)
	}
	summary, err := store.TrustSummary(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary) != 1 || !summary[0].Trusted || !summary[0].Online || summary[0].CredentialIssuer != "issuer" || summary[0].SupportedBinding != ProtocolBindingNATS || summary[0].ProtocolVersion != ProtocolVersion || !summary[0].Compatibility.Supported {
		t.Fatalf("unexpected trust summary: %+v", summary)
	}
}

func TestApplyPeerCardRecordPreservesLocalTrust(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPeerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.UpsertCard(ctx, "eve-local", phase4Card("eve-local"), true, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Upsert trusted peer: %v", err)
	}
	if err := ApplyPeerCardRecord(ctx, store, PeerCardRecord{AgentID: "eve-local", InstanceID: "instance-b", Card: phase4Card("eve-local"), ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatalf("ApplyPeerCardRecord: %v", err)
	}
	peer, err := store.Get(ctx, "eve-local")
	if err != nil {
		t.Fatal(err)
	}
	if !peer.Trusted {
		t.Fatal("peer-card refresh revoked local trust")
	}
}

func TestVersionCompatibility(t *testing.T) {
	card := phase4Card("eve-local")
	if got := CheckVersionCompatibility(card); !got.Supported {
		t.Fatalf("compatible card rejected: %+v", got)
	}
	card.SupportedInterfaces[0].ProtocolVersion = "9.9"
	if got := CheckVersionCompatibility(card); got.Supported || len(got.Reasons) == 0 || !strings.Contains(got.Reasons[0], "unsupported protocol version") {
		t.Fatalf("unsupported version hidden: %+v", got)
	}
	if err := ValidatePeerCard(card); err != nil {
		t.Fatalf("ValidatePeerCard should report compatibility separately, got %v", err)
	}
}

func TestStalePeer(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPeerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.UpsertCard(ctx, "eve-local", phase4Card("eve-local"), false, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	summary, err := store.TrustSummary(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary) != 1 || !summary[0].Stale || summary[0].Online {
		t.Fatalf("expired peer not stale: %+v", summary)
	}
}

func phase4Card(agent AgentID) AgentCard {
	return AgentCard{Name: string(agent), Description: "Kiro Discord Bot A2A runtime", Version: "2.30.0", SupportedInterfaces: []A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: ProtocolBindingNATS, ProtocolVersion: ProtocolVersion}}, Capabilities: map[string]bool{"streaming": false, "pushNotifications": false, "extendedAgentCard": true}, DefaultInputModes: []string{"text/plain", "application/json"}, DefaultOutputModes: []string{"text/plain", "application/json"}, Skills: []AgentSkill{{ID: "backend/code-review", Name: "Code Review", Description: "Review backend changes.", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}}}}
}
