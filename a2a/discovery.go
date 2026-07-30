package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	PeerKVBucket      = "A2A_PEERS"
	PeerFallbackInbox = "a2a.v1.card.discover"
	MaxPeerCardBytes  = 64 * 1024
)

type PeerCardRecord struct {
	AgentID      AgentID           `json:"agentId"`
	InstanceID   string            `json:"instanceId"`
	Card         AgentCard         `json:"card"`
	ExtendedCard ExtendedAgentCard `json:"extendedCard,omitempty"`
	ExpiresAt    time.Time         `json:"expiresAt"`
	PublishedAt  time.Time         `json:"publishedAt"`
	Version      string            `json:"version"`
}

func EnsurePeerKV(ctx context.Context, node *Node, ttl time.Duration) (jetstream.KeyValue, error) {
	if node == nil || !node.IsEnabled() {
		return nil, fmt.Errorf("A2A node is disabled")
	}
	js := node.JetStream()
	if js == nil {
		return nil, fmt.Errorf("A2A JetStream is not initialized")
	}
	if ttl <= 0 {
		ttl = 90 * time.Second
	}
	return js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: PeerKVBucket, TTL: ttl, History: 1, MaxValueSize: MaxPeerCardBytes})
}

func PublishPeerCard(ctx context.Context, node *Node, record PeerCardRecord, ttl time.Duration) (uint64, error) {
	kv, err := EnsurePeerKV(ctx, node, ttl)
	if err != nil {
		return 0, err
	}
	record, payload, err := normalizePeerCardRecord(record)
	if err != nil {
		return 0, err
	}
	return kv.Put(ctx, string(record.AgentID), payload)
}

func WatchPeerCards(ctx context.Context, node *Node, store *SQLitePeerStore, ttl time.Duration) error {
	if store == nil {
		return fmt.Errorf("peer store is required")
	}
	kv, err := EnsurePeerKV(ctx, node, ttl)
	if err != nil {
		return err
	}
	watcher, err := kv.WatchAll(ctx)
	if err != nil {
		return err
	}
	defer watcher.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case entry := <-watcher.Updates():
			if entry == nil {
				continue
			}
			if err := ApplyPeerKVEntry(ctx, store, entry); err != nil {
				return err
			}
		}
	}
}

func ApplyPeerKVEntry(ctx context.Context, store *SQLitePeerStore, entry jetstream.KeyValueEntry) error {
	if store == nil || entry == nil {
		return nil
	}
	agent := AgentID(entry.Key())
	if entry.Operation() == jetstream.KeyValueDelete || entry.Operation() == jetstream.KeyValuePurge {
		return store.MarkStale(ctx, agent)
	}
	var record PeerCardRecord
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		return err
	}
	if record.AgentID == "" {
		record.AgentID = agent
	}
	if record.AgentID != agent {
		return fmt.Errorf("peer KV key %s does not match record agent %s", agent, record.AgentID)
	}
	return ApplyPeerCardRecord(ctx, store, record)
}

func ApplyPeerCardRecord(ctx context.Context, store *SQLitePeerStore, record PeerCardRecord) error {
	if store == nil {
		return fmt.Errorf("peer store is required")
	}
	record, _, err := normalizePeerCardRecord(record)
	if err != nil {
		return err
	}
	_, err = store.UpsertExtendedCard(ctx, record.AgentID, record.Card, record.ExtendedCard, false, record.InstanceID, "online", record.ExpiresAt)
	return err
}

func CollectPeerCardsFallback(ctx context.Context, nc *nats.Conn, subject string, deadline time.Duration) ([]PeerCardRecord, error) {
	if nc == nil {
		return nil, fmt.Errorf("NATS connection is required")
	}
	if subject == "" {
		subject = PeerFallbackInbox
	}
	if deadline <= 0 {
		deadline = time.Second
	}
	inbox := nats.NewInbox()
	sub, err := nc.SubscribeSync(inbox)
	if err != nil {
		return nil, err
	}
	defer sub.Unsubscribe()
	if err := nc.PublishRequest(subject, inbox, []byte("peer-card-request")); err != nil {
		if errors.Is(err, nats.ErrNoResponders) {
			return nil, fmt.Errorf("peer discovery unsupported: no responders")
		}
		return nil, err
	}
	until := time.Now().Add(deadline)
	var records []PeerCardRecord
	for {
		remaining := time.Until(until)
		if remaining <= 0 {
			if len(records) == 0 {
				return nil, fmt.Errorf("peer discovery timeout: no peer cards received")
			}
			return records, nil
		}
		msg, err := sub.NextMsg(remaining)
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				if len(records) == 0 {
					return nil, fmt.Errorf("peer discovery timeout: no peer cards received")
				}
				return records, nil
			}
			return nil, err
		}
		var record PeerCardRecord
		if err := json.Unmarshal(msg.Data, &record); err != nil {
			return nil, err
		}
		record, _, err = normalizePeerCardRecord(record)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
}

func normalizePeerCardRecord(record PeerCardRecord) (PeerCardRecord, []byte, error) {
	if record.AgentID == "" {
		record.AgentID = AgentID(record.Card.Name)
	}
	if err := ValidateAgentID(record.AgentID); err != nil {
		return PeerCardRecord{}, nil, err
	}
	if record.Card.Name == "" {
		record.Card.Name = string(record.AgentID)
	}
	record.Card = SanitizeAgentCard(record.Card)
	if err := ValidatePeerCard(record.Card); err != nil {
		return PeerCardRecord{}, nil, err
	}
	ext, err := BuildExtendedAgentCard(record.Card, record.ExtendedCard)
	if err != nil {
		return PeerCardRecord{}, nil, err
	}
	record.ExtendedCard = ext
	if record.PublishedAt.IsZero() {
		record.PublishedAt = time.Now().UTC()
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.PublishedAt.Add(90 * time.Second)
	}
	if record.Version == "" {
		record.Version = ProtocolVersion
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return PeerCardRecord{}, nil, err
	}
	if len(payload) > MaxPeerCardBytes {
		return PeerCardRecord{}, nil, fmt.Errorf("peer card exceeds max size")
	}
	return record, payload, nil
}
