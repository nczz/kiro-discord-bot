package bot

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nczz/kiro-discord-bot/a2a"
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
		record, err := b.a2aPeerCardRecord(time.Now().UTC())
		if err != nil {
			log.Printf("[a2a] build fallback peer card failed: %v", err)
			return
		}
		_, raw, err := a2a.NormalizePeerCardRecordForPublish(record)
		if err != nil {
			log.Printf("[a2a] normalize fallback peer card failed: %v", err)
			return
		}
		if err := msg.Respond(raw); err != nil {
			log.Printf("[a2a] respond fallback peer card failed: %v", err)
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
	record, err := b.a2aPeerCardRecord(now)
	if err != nil {
		return err
	}
	if _, err := a2a.PublishPeerCard(ctx, b.a2aNode, record, a2aPeerCardTTL); err != nil {
		return err
	}
	if err := a2a.PublishHeartbeat(ctx, b.a2aNode, a2a.HeartbeatPayload{AgentID: b.a2aConfig.AgentID, InstanceID: b.a2aInstanceID, Status: "online", StartedAt: b.startedAt.UTC(), Version: b.version}); err != nil {
		return err
	}
	return nil
}

func (b *Bot) a2aPeerCardRecord(now time.Time) (a2a.PeerCardRecord, error) {
	card, err := buildBotA2APeerCard(b.a2aConfig, b.version)
	if err != nil {
		return a2a.PeerCardRecord{}, err
	}
	return a2a.PeerCardRecord{AgentID: b.a2aConfig.AgentID, InstanceID: b.a2aInstanceID, Card: card, ExtendedCard: a2a.ExtendedAgentCard{Runtime: "kiro-discord-bot", TriggerGuidance: "/a2a setup, then /a2a ask", ResultVisibilitySupport: []string{"proxy", "transparent"}, MaxTaskDurationClass: "interactive"}, PublishedAt: now, ExpiresAt: now.Add(a2aPeerCardTTL)}, nil
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
