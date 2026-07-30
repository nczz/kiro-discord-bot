package bot

import (
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/a2a"
)

func TestBuildBotA2APeerCardUsesStableAgentIDAndGeneralSkill(t *testing.T) {
	card, err := buildBotA2APeerCard(a2a.Config{NATSURL: "nats://nats.example.internal:4222", AgentID: "m5bot-local", AgentName: "M5Bot", AgentDescription: "runtime in /Users/chun with DISCORD_TOKEN"}, "2.29.1-test")
	if err != nil {
		t.Fatalf("buildBotA2APeerCard: %v", err)
	}
	if card.Name != "m5bot-local" {
		t.Fatalf("card name = %q, want stable agent id", card.Name)
	}
	if len(card.Skills) != 1 || card.Skills[0].ID != "general/task" {
		t.Fatalf("card skills = %+v, want general/task", card.Skills)
	}
	text := card.Description + " " + card.SupportedInterfaces[0].URL
	if strings.Contains(text, "/Users") || strings.Contains(text, "DISCORD_TOKEN") {
		t.Fatalf("card leaked private data: %+v", card)
	}
}
