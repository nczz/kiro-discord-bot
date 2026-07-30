package a2a

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	EventKindAccepted = "accepted"
	EventKindRejected = "rejected"
	EventKindStatus   = "status"
	EventKindArtifact = "artifact"
	EventKindResult   = "result"

	ControlKindStatusRequest = "status"
	ControlKindCancel        = "cancel"
	ControlKindInputReply    = "input_reply"
	ControlKindAuthReply     = "auth_reply"
)

type SendMessagePayload struct {
	A2A                   json.RawMessage        `json:"a2a"`
	ChannelRef            string                 `json:"channelRef"`
	SkillID               string                 `json:"skillId"`
	UserVisibleSummary    string                 `json:"userVisibleSummary,omitempty"`
	ClientTaskRef         string                 `json:"clientTaskRef,omitempty"`
	ContextID             string                 `json:"contextId,omitempty"`
	Delivery              TransportDelivery      `json:"delivery,omitempty"`
	AuditMetadata         map[string]string      `json:"auditMetadata,omitempty"`
	AdditionalSafetyAttrs map[string]interface{} `json:"-"`
}

type TransportDelivery struct {
	TimeoutSec            int             `json:"timeoutSec,omitempty"`
	RequiresConfirmation  bool            `json:"requiresConfirmation,omitempty"`
	DiscordContext        *DiscordContext `json:"discordContext,omitempty"`
	DiscordReplyChannelID string          `json:"discordReplyChannelId,omitempty"`
	DiscordReplyThreadID  string          `json:"discordReplyThreadId,omitempty"`
	ShareDiscordContext   bool            `json:"shareDiscordContext,omitempty"`
	CoPresentFrom         AgentID         `json:"coPresentFrom,omitempty"`
	MaxDelegationDepth    int             `json:"maxDelegationDepth,omitempty"`
	ResultVisibility      string          `json:"resultVisibility,omitempty"`
	DiscordTranscriptMode string          `json:"discordTranscriptMode,omitempty"`
}

type ControlPayload struct {
	A2A           json.RawMessage `json:"a2a,omitempty"`
	MessageID     MessageID       `json:"messageId,omitempty"`
	ClientTaskRef string          `json:"clientTaskRef,omitempty"`
	Reason        string          `json:"reason,omitempty"`
	Revision      int64           `json:"revision,omitempty"`
}

type TaskEventPayload struct {
	MessageID     MessageID              `json:"messageId,omitempty"`
	ClientTaskRef string                 `json:"clientTaskRef,omitempty"`
	TaskID        TaskID                 `json:"taskId,omitempty"`
	State         TaskState              `json:"state,omitempty"`
	Content       string                 `json:"content,omitempty"`
	Error         TaskError              `json:"error,omitempty"`
	Revision      int64                  `json:"revision,omitempty"`
	Result        *TaskExecutionResult   `json:"result,omitempty"`
	Artifact      *TaskExecutionArtifact `json:"artifact,omitempty"`
}

func taskRequestFromEnvelope(env Envelope, subject Subject) (TaskExecutionRequest, error) {
	var payload SendMessagePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return TaskExecutionRequest{}, fmt.Errorf("decode send_message payload: %w", err)
	}
	if len(payload.A2A) == 0 || string(payload.A2A) == "null" {
		return TaskExecutionRequest{}, fmt.Errorf("payload.a2a is required")
	}
	if strings.TrimSpace(payload.ChannelRef) == "" {
		return TaskExecutionRequest{}, fmt.Errorf("channelRef is required")
	}
	if strings.TrimSpace(payload.SkillID) == "" {
		return TaskExecutionRequest{}, fmt.Errorf("skillId is required")
	}
	delivery := DeliveryOptions{
		TimeoutSec:            payload.Delivery.TimeoutSec,
		RequiresConfirmation:  payload.Delivery.RequiresConfirmation,
		DiscordContext:        payload.Delivery.DiscordContext,
		DiscordReplyChannelID: strings.TrimSpace(payload.Delivery.DiscordReplyChannelID),
		DiscordReplyThreadID:  strings.TrimSpace(payload.Delivery.DiscordReplyThreadID),
		ShareDiscordContext:   payload.Delivery.ShareDiscordContext,
		CoPresentFrom:         payload.Delivery.CoPresentFrom,
		MaxDelegationDepth:    payload.Delivery.MaxDelegationDepth,
	}
	if delivery.DiscordContext != nil {
		raw, _ := json.Marshal(delivery.DiscordContext)
		delivery.DiscordContextJSON = raw
	}
	created, _, _ := parseEnvelopeTime("createdAt", env.CreatedAt)
	expires, _, _ := parseEnvelopeTime("expiresAt", env.ExpiresAt)
	return TaskExecutionRequest{
		MessageID:             env.MessageID,
		ClientTaskRef:         strings.TrimSpace(payload.ClientTaskRef),
		ContextID:             strings.TrimSpace(firstNonEmpty(payload.ContextID, string(env.TaskID))),
		From:                  subject.From,
		To:                    subject.To,
		ChannelRef:            strings.TrimSpace(payload.ChannelRef),
		SkillID:               strings.TrimSpace(payload.SkillID),
		UserVisibleSummary:    strings.TrimSpace(payload.UserVisibleSummary),
		Payload:               payload.A2A,
		Delivery:              delivery,
		ResultVisibility:      strings.TrimSpace(payload.Delivery.ResultVisibility),
		DiscordTranscriptMode: strings.TrimSpace(payload.Delivery.DiscordTranscriptMode),
		CreatedAt:             created,
		ExpiresAt:             expires,
		AuditMetadata:         payload.AuditMetadata,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func newEnvelope(from, to AgentID, typ EnvelopeType, messageID MessageID, taskID TaskID, revision int64, payload []byte) Envelope {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return Envelope{Version: Version, Binding: Binding, MessageID: messageID, From: from, To: to, Type: typ, TaskID: taskID, Revision: revision, Payload: payload, CreatedAt: now}
}
