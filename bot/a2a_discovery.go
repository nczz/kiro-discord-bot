package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/internal/channelmeta"
)

const a2aPeerCardTTL = 3 * time.Minute

type a2aDiscoveryTask struct {
	bot *Bot
}

func (t *a2aDiscoveryTask) Name() string { return "a2a-discovery" }

func (t *a2aDiscoveryTask) ShouldRun(time.Time) bool {
	return t != nil && t.bot != nil && t.bot.a2aNode != nil && t.bot.a2aNode.IsEnabled()
}

func (t *a2aDiscoveryTask) Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return t.bot.publishA2APeerPresence(ctx)
}

func (b *Bot) startA2APeerDiscovery(ctx context.Context) {
	if b == nil || b.a2aNode == nil || !b.a2aNode.IsEnabled() || b.a2aPeerStore == nil {
		return
	}
	publishCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if err := b.publishA2APeerPresence(publishCtx); err != nil {
		log.Printf("[a2a] publish peer card failed: %v", err)
	}
	cancel()
	if err := b.startA2APeerFallbackResponder(); err != nil {
		log.Printf("[a2a] peer fallback responder disabled: %v", err)
	}
	go b.watchA2APeerCards(ctx)
}

func (b *Bot) watchA2APeerCards(ctx context.Context) {
	for {
		if err := a2a.WatchPeerCards(ctx, b.a2aNode, b.a2aPeerStore, a2aPeerCardTTL); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[a2a] peer-card watcher error: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (b *Bot) startA2APeerFallbackResponder() error {
	if b == nil || b.a2aNode == nil || b.a2aNode.NATSConn() == nil {
		return nil
	}
	sub, err := b.a2aNode.NATSConn().Subscribe(a2a.PeerFallbackInbox, func(msg *nats.Msg) {
		records, err := b.a2aPeerCardRecords(time.Now().UTC())
		if err != nil {
			log.Printf("[a2a] build fallback peer cards failed: %v", err)
			return
		}
		for _, record := range records {
			_, raw, err := a2a.NormalizePeerCardRecordForPublish(record)
			if err != nil {
				log.Printf("[a2a] normalize fallback peer card failed: %v", err)
				return
			}
			if err := msg.Respond(raw); err != nil {
				log.Printf("[a2a] respond fallback peer card failed: %v", err)
			}
		}
	})
	if err != nil {
		return err
	}
	b.a2aPeerFallbackSub = sub
	return nil
}

func (b *Bot) publishA2APeerPresence(ctx context.Context) error {
	if b == nil || b.a2aNode == nil || !b.a2aNode.IsEnabled() {
		return nil
	}
	now := time.Now().UTC()
	records, err := b.a2aPeerCardRecords(now)
	if err != nil {
		return err
	}
	for _, record := range records {
		if _, err := a2a.PublishPeerCard(ctx, b.a2aNode, record, a2aPeerCardTTL); err != nil {
			return err
		}
		if err := a2a.PublishHeartbeat(ctx, b.a2aNode, a2a.HeartbeatPayload{AgentID: record.AgentID, InstanceID: record.InstanceID, Status: "online", StartedAt: b.startedAt.UTC(), Version: b.version}); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) a2aPeerCardRecord(now time.Time) (a2a.PeerCardRecord, error) {
	records, err := b.a2aPeerCardRecords(now)
	if err != nil {
		return a2a.PeerCardRecord{}, err
	}
	if len(records) == 0 {
		return a2a.PeerCardRecord{}, fmt.Errorf("no A2A peer card records")
	}
	return records[0], nil
}

func (b *Bot) a2aPeerCardRecords(now time.Time) ([]a2a.PeerCardRecord, error) {
	mode, err := a2a.NormalizeRuntimeIDMode(b.a2aConfig.RuntimeIDMode.String())
	if err != nil {
		return nil, err
	}
	var records []a2a.PeerCardRecord
	if mode != a2a.RuntimeIDModeRuntime {
		card, err := buildBotA2APeerCard(b.a2aConfig, b.version)
		if err != nil {
			return nil, err
		}
		records = append(records, a2a.PeerCardRecord{AgentID: b.a2aConfig.AgentID, InstanceID: b.a2aInstanceID, Card: card, ExtendedCard: a2a.ExtendedAgentCard{Runtime: "kiro-discord-bot", TriggerGuidance: "/a2a setup, then /a2a ask", ResultVisibilitySupport: []string{"proxy", "transparent"}, MaxTaskDurationClass: "interactive"}, PublishedAt: now, ExpiresAt: now.Add(a2aPeerCardTTL)})
	}
	if b.manager == nil {
		return records, nil
	}
	if mode.UsesRuntimeIDs() {
		policies, err := b.manager.A2ADiscoverablePolicies(context.Background())
		if err != nil {
			return nil, err
		}
		for _, policy := range policies {
			runtimeID := a2a.AgentID(policy.RuntimeAgentID)
			if err := b.manager.EnsureA2ATransportRuntime(context.Background(), runtimeID); err != nil {
				log.Printf("[a2a] start runtime transport %s failed: %v", runtimeID, err)
				continue
			}
			if !b.manager.A2ATransportAccepts(runtimeID) {
				continue
			}
			record, err := b.a2aRuntimePeerCardRecord(now, policy)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
	}
	return records, nil
}

func (b *Bot) a2aRuntimePeerCardRecord(now time.Time, policy a2a.ChannelA2APolicy) (a2a.PeerCardRecord, error) {
	runtime := a2a.RuntimeRecord{
		RuntimeAgentID: a2a.AgentID(policy.RuntimeAgentID),
		BotAgentID:     a2a.AgentID(policy.BotAgentID),
		GuildID:        policy.GuildID,
		ChannelID:      policy.ChannelID,
		ChannelRef:     policy.ChannelRef,
		DisplayName:    b.a2aRuntimeDisplayName(policy),
		RuntimeKind:    "channel",
		Enabled:        policy.Enabled,
		Discoverable:   policy.Discoverable,
	}
	card, ext, err := a2a.BuildRuntimeAgentCard(b.a2aConfig, runtime, b.version, agentSkillsFromPolicy(policy))
	if err != nil {
		return a2a.PeerCardRecord{}, err
	}
	return a2a.PeerCardRecord{AgentID: runtime.RuntimeAgentID, InstanceID: b.a2aInstanceID + "-" + string(runtime.RuntimeAgentID), Card: card, ExtendedCard: ext, PublishedAt: now, ExpiresAt: now.Add(a2aPeerCardTTL)}, nil
}

func (b *Bot) a2aRuntimeDisplayName(policy a2a.ChannelA2APolicy) string {
	if b != nil {
		if entries, err := channelmeta.Read(b.dataDir); err == nil {
			if entry, ok := entries[strings.TrimSpace(policy.ChannelID)]; ok && strings.TrimSpace(entry.Name) != "" {
				return strings.TrimSpace(entry.Name)
			}
		}
	}
	if strings.TrimSpace(policy.ChannelRef) != "" {
		return strings.TrimSpace(policy.ChannelRef)
	}
	return strings.TrimSpace(policy.RuntimeAgentID)
}

func agentSkillsFromPolicy(policy a2a.ChannelA2APolicy) []a2a.AgentSkill {
	if len(policy.ExposeSkills) == 0 {
		return []a2a.AgentSkill{{ID: "task", Name: "General task", Description: "General Discord-bound task execution", InputModes: []string{"text/plain", "application/json"}, OutputModes: []string{"text/plain", "application/json"}}}
	}
	skills := make([]a2a.AgentSkill, 0, len(policy.ExposeSkills))
	for _, skill := range policy.ExposeSkills {
		input := skill.InputModes
		if len(input) == 0 {
			input = []string{"text/plain", "application/json"}
		}
		output := skill.OutputModes
		if len(output) == 0 {
			output = []string{"text/plain", "application/json"}
		}
		skills = append(skills, a2a.AgentSkill{ID: skill.ID, Name: a2a.SkillSlug(skill.ID), Description: "Discord runtime task execution", InputModes: input, OutputModes: output})
	}
	return skills
}

func buildBotA2APeerCard(cfg a2a.Config, version string) (a2a.AgentCard, error) {
	if cfg.AgentID == "" {
		return a2a.AgentCard{}, fmt.Errorf("A2A agent id is required")
	}
	friendlyName := cfg.AgentName
	cfg.AgentName = string(cfg.AgentID)
	if cfg.AgentDescription == "" && friendlyName != "" {
		cfg.AgentDescription = friendlyName + " A2A Discord bot"
	}
	return a2a.BuildPublicAgentCard(cfg, version, []a2a.AgentSkill{{ID: "general/task", Name: "General task", Description: "General Discord-bound task execution", InputModes: []string{"text/plain", "application/json"}, OutputModes: []string{"text/plain", "application/json"}}})
}
