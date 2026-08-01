package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/internal/channelmeta"
)

func TestBuildBotA2APeerCardUsesStableAgentIDAndGeneralSkill(t *testing.T) {
	card, err := buildBotA2APeerCard(a2a.Config{NATSURL: "nats://nats.example.internal:4222", AgentID: "local-bot", AgentName: "AlphaBot", AgentDescription: "runtime in /Users/chun with DISCORD_TOKEN"}, "2.29.1-test")
	if err != nil {
		t.Fatalf("buildBotA2APeerCard: %v", err)
	}
	if card.Name != "local-bot" {
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

func TestRuntimePeerCardUsesRuntimeIDAndPolicySkills(t *testing.T) {
	b := &Bot{
		a2aConfig:   a2a.Config{NATSURL: "nats://nats.example.internal:4222", AgentID: "local-bot"},
		version:     "2.29.1-test",
		startedAt:   nowForA2ATest(),
		dataDir:     t.TempDir(),
		guildID:     "guild-1",
		manualPeers: nil,
	}
	if err := channelmeta.Upsert(b.dataDir, channelmeta.Entry{ID: "channel-1", GuildID: "guild-1", Name: "Support Room", Type: "channel"}); err != nil {
		t.Fatalf("channel metadata: %v", err)
	}
	record, err := b.a2aRuntimePeerCardRecord(nowForA2ATest(), a2a.ChannelA2APolicy{
		GuildID:        "guild-1",
		ChannelID:      "channel-1",
		Enabled:        true,
		Discoverable:   true,
		RuntimeAgentID: "local-bot-support",
		BotAgentID:     "local-bot",
		ChannelRef:     "support",
		ExposeSkills:   []a2a.SkillPolicy{{ID: "review", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}}},
	})
	if err != nil {
		t.Fatalf("a2aRuntimePeerCardRecord: %v", err)
	}
	if record.AgentID != "local-bot-support" || record.Card.Name != "local-bot-support" || record.ExtendedCard.ChannelRef != "support" || record.ExtendedCard.DisplayName != "Support Room" || record.ExtendedCard.BotAgentID != "local-bot" {
		t.Fatalf("runtime record = %+v", record)
	}
	if len(record.Card.Skills) != 1 || record.Card.Skills[0].ID != "support/review" {
		t.Fatalf("runtime skills = %+v", record.Card.Skills)
	}
}

func TestRuntimePeerCardForInactiveMetadataChannelHasNoSkills(t *testing.T) {
	b := &Bot{
		a2aConfig: a2a.Config{NATSURL: "nats://nats.example.internal:4222", AgentID: "local-bot"},
		version:   "2.29.1-test",
		startedAt: nowForA2ATest(),
		dataDir:   t.TempDir(),
	}
	if err := channelmeta.Upsert(b.dataDir, channelmeta.Entry{ID: "channel-2", GuildID: "guild-1", Name: "大廳", Type: "channel"}); err != nil {
		t.Fatalf("channel metadata: %v", err)
	}
	record, err := b.a2aRuntimePeerCardRecord(nowForA2ATest(), a2a.ChannelA2APolicy{
		GuildID:               "guild-1",
		ChannelID:             "channel-2",
		Enabled:               false,
		Discoverable:          false,
		RuntimeAgentID:        "local-bot-ch-1234abcd",
		BotAgentID:            "local-bot",
		ChannelRef:            "ch-1234abcd",
		ResultVisibility:      "proxy",
		DiscordTranscriptMode: "delegator",
	})
	if err != nil {
		t.Fatalf("a2aRuntimePeerCardRecord: %v", err)
	}
	if len(record.Card.Skills) != 0 || record.ExtendedCard.DisplayName != "大廳" {
		t.Fatalf("inactive runtime card = %+v", record)
	}
}

func nowForA2ATest() time.Time { return time.Unix(1700000000, 0).UTC() }
