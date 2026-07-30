package channel

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/a2a"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestManagerA2APeerTrustSummary(t *testing.T) {
	L.Load("en")
	ctx := context.Background()
	store, err := a2a.OpenPeerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.UpsertExtendedCard(ctx, "eve-local", testA2APeerCard("eve-local"), a2a.ExtendedAgentCard{CredentialIssuer: "issuer", CredentialFingerprint: "aa:bb", SignatureStatus: "verified"}, true, "instance", "online", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(ManagerConfig{DataDir: t.TempDir(), A2A: a2a.Config{NATSURL: "nats://nats.example.internal:4222", AgentID: "adam-n200"}, A2APeerStore: store})
	defer m.StopAll()
	got := m.A2APeerTrustSummary(ctx)
	for _, want := range []string{"**A2A Peers**", "eve-local", "trusted", "compatible", "issuer", "aa:bb", "backend/code-review"} {
		if !strings.Contains(got, want) {
			t.Fatalf("A2A peer summary missing %q:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"/Users", "DISCORD_TOKEN", "mcp.json"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("A2A peer summary leaked %q:\n%s", notWant, got)
		}
	}
}

func TestManagerA2APeerSummaryDisabledNoop(t *testing.T) {
	L.Load("en")
	m := NewManager(ManagerConfig{DataDir: t.TempDir(), A2A: a2a.Config{}})
	defer m.StopAll()
	if got := m.A2APeerTrustSummary(context.Background()); got != "" {
		t.Fatalf("disabled A2A peer summary = %q, want empty", got)
	}
}

func testA2APeerCard(agent a2a.AgentID) a2a.AgentCard {
	return a2a.AgentCard{Name: string(agent), Description: "Kiro Discord Bot A2A runtime /Users/eve DISCORD_TOKEN", Version: "2.30.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Capabilities: map[string]bool{"streaming": false, "pushNotifications": false, "extendedAgentCard": true}, DefaultInputModes: []string{"text/plain", "application/json"}, DefaultOutputModes: []string{"text/plain", "application/json"}, Skills: []a2a.AgentSkill{{ID: "backend/code-review", Name: "Code Review", Description: "Review backend changes.", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}}}}
}
