package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type HeartbeatPayload struct {
	AgentID     AgentID   `json:"agentId"`
	InstanceID  string    `json:"instanceId"`
	Status      string    `json:"status"`
	ActiveTasks int       `json:"activeTasks"`
	StartedAt   time.Time `json:"startedAt"`
	Version     string    `json:"version"`
}

func PublishHeartbeat(ctx context.Context, node *Node, payload HeartbeatPayload) error {
	if node == nil || !node.IsEnabled() {
		return fmt.Errorf("A2A node is disabled")
	}
	if node.NATSConn() == nil {
		return fmt.Errorf("A2A NATS connection is not initialized")
	}
	payload, raw, err := NormalizeHeartbeat(payload)
	if err != nil {
		return err
	}
	if err := node.NATSConn().Publish(HeartbeatSubject(payload.AgentID, payload.InstanceID), raw); err != nil {
		return err
	}
	return node.NATSConn().FlushWithContext(ctx)
}

func NormalizeHeartbeat(payload HeartbeatPayload) (HeartbeatPayload, []byte, error) {
	if err := ValidateAgentID(payload.AgentID); err != nil {
		return HeartbeatPayload{}, nil, err
	}
	payload.InstanceID = sanitizePublicText(payload.InstanceID)
	if payload.InstanceID == "" {
		return HeartbeatPayload{}, nil, fmt.Errorf("instanceId is required")
	}
	if payload.Status == "" {
		payload.Status = "online"
	}
	payload.Status = sanitizePublicText(payload.Status)
	if payload.ActiveTasks < 0 {
		return HeartbeatPayload{}, nil, fmt.Errorf("activeTasks cannot be negative")
	}
	if payload.StartedAt.IsZero() {
		payload.StartedAt = time.Now().UTC()
	}
	if payload.Version == "" {
		payload.Version = "unknown"
	}
	payload.Version = sanitizePublicText(payload.Version)
	raw, err := json.Marshal(payload)
	if err != nil {
		return HeartbeatPayload{}, nil, err
	}
	return payload, raw, nil
}
