package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/channel"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestBotA2APeerDoctorSurface(t *testing.T) {
	L.Load("en")
	ctx := context.Background()
	store, err := a2a.OpenPeerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.UpsertExtendedCard(ctx, "eve-local", botTestA2APeerCard("eve-local"), a2a.ExtendedAgentCard{CredentialIssuer: "issuer", CredentialFingerprint: "aa:bb", SignatureStatus: "verified"}, true, "instance", "online", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	m := channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir(), A2A: a2a.Config{NATSURL: "nats://nats.example.internal:4222", AgentID: "adam-n200"}, A2APeerStore: store})
	defer m.StopAll()
	b := &Bot{manager: m}
	got := b.doctor(ctx, "channel-1", "channel-1")
	a2aSection := got[strings.Index(got, "**A2A Peers**"):]
	if idx := strings.Index(a2aSection, "\n**MCP"); idx >= 0 {
		a2aSection = a2aSection[:idx]
	}
	if idx := strings.Index(a2aSection, "\nNo MCP"); idx >= 0 {
		a2aSection = a2aSection[:idx]
	}
	for _, want := range []string{"**A2A Peers**", "eve-local", "trusted", "compatible", "backend/code-review"} {
		if !strings.Contains(got, want) {
			t.Fatalf("bot doctor missing %q:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"/Users", "DISCORD_TOKEN", "mcp.json"} {
		if strings.Contains(a2aSection, notWant) {
			t.Fatalf("bot A2A peer section leaked %q:\n%s", notWant, a2aSection)
		}
	}
}

func botTestA2APeerCard(agent a2a.AgentID) a2a.AgentCard {
	return a2a.AgentCard{Name: string(agent), Description: "Kiro Discord Bot A2A runtime /Users/eve DISCORD_TOKEN", Version: "2.30.0", SupportedInterfaces: []a2a.A2AInterface{{URL: "nats://nats.example.internal:4222", ProtocolBinding: a2a.ProtocolBindingNATS, ProtocolVersion: a2a.ProtocolVersion}}, Capabilities: map[string]bool{"streaming": false, "pushNotifications": false, "extendedAgentCard": true}, DefaultInputModes: []string{"text/plain", "application/json"}, DefaultOutputModes: []string{"text/plain", "application/json"}, Skills: []a2a.AgentSkill{{ID: "backend/code-review", Name: "Code Review", Description: "Review backend changes.", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}}}}
}
