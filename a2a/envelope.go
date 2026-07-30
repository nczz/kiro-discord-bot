package a2a

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Envelope struct {
	Version   string          `json:"version"`
	Binding   string          `json:"binding"`
	MessageID MessageID       `json:"messageId"`
	From      AgentID         `json:"from"`
	To        AgentID         `json:"to,omitempty"`
	Type      EnvelopeType    `json:"type"`
	TaskID    TaskID          `json:"taskId,omitempty"`
	Revision  int64           `json:"revision,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"createdAt,omitempty"`
	ExpiresAt string          `json:"expiresAt,omitempty"`
}

func ValidateEnvelope(env Envelope, subject Subject) error {
	if env.Version != Version {
		return fmt.Errorf("version must be %q", Version)
	}
	if env.Binding != Binding {
		return fmt.Errorf("binding must be %q", Binding)
	}
	if err := ValidateMessageID(env.MessageID); err != nil {
		return fmt.Errorf("messageId: %w", err)
	}
	if err := ValidateAgentID(env.From); err != nil {
		return fmt.Errorf("from: %w", err)
	}
	if env.To != "" {
		if err := ValidateAgentID(env.To); err != nil {
			return fmt.Errorf("to: %w", err)
		}
	}
	if !IsKnownEnvelopeType(env.Type) {
		return fmt.Errorf("unknown envelope type %q", env.Type)
	}
	if len(env.Payload) > MaxEnvelopePayloadBytes {
		return fmt.Errorf("payload exceeds %d bytes", MaxEnvelopePayloadBytes)
	}
	if env.Revision < 0 {
		return fmt.Errorf("revision must not be negative")
	}
	created, hasCreated, err := parseEnvelopeTime("createdAt", env.CreatedAt)
	if err != nil {
		return err
	}
	expires, hasExpires, err := parseEnvelopeTime("expiresAt", env.ExpiresAt)
	if err != nil {
		return err
	}
	if hasCreated && created.After(time.Now().Add(5*time.Minute)) {
		return fmt.Errorf("createdAt is too far in the future")
	}
	if hasCreated && hasExpires && !expires.After(created) {
		return fmt.Errorf("expiresAt must be after createdAt")
	}
	if hasExpires && expires.Before(time.Now().Add(-1*time.Minute)) {
		return fmt.Errorf("envelope is expired")
	}

	switch subject.Kind {
	case SubjectKindTask:
		if env.Type != EnvelopeTypeTask {
			return fmt.Errorf("envelope type %q does not match task subject", env.Type)
		}
		if env.From != subject.From || env.To != subject.To || env.MessageID != subject.MessageID {
			return fmt.Errorf("task envelope does not match subject")
		}
	case SubjectKindControl:
		if env.Type != EnvelopeTypeControl {
			return fmt.Errorf("envelope type %q does not match control subject", env.Type)
		}
		if env.From != subject.From || env.To != subject.Executor || env.TaskID != subject.TaskID {
			return fmt.Errorf("control envelope does not match subject")
		}
	case SubjectKindEvent:
		if env.Type != EnvelopeTypeEvent {
			return fmt.Errorf("envelope type %q does not match event subject", env.Type)
		}
		if env.From != subject.Executor || env.To != subject.Delegator {
			return fmt.Errorf("event envelope does not match subject")
		}
		if strings.HasPrefix(subject.TaskKey, "msg_") {
			if MessageID(strings.TrimPrefix(subject.TaskKey, "msg_")) != env.MessageID {
				return fmt.Errorf("pre-accept event message id does not match subject")
			}
		} else if env.TaskID != TaskID(subject.TaskKey) {
			return fmt.Errorf("event task id does not match subject")
		}
	case SubjectKindCard:
		if env.Type != EnvelopeTypeCard {
			return fmt.Errorf("envelope type %q does not match card subject", env.Type)
		}
		if env.From != subject.Agent {
			return fmt.Errorf("card envelope does not match subject")
		}
	case SubjectKindHeartbeat:
		if env.Type != EnvelopeTypeHeartbeat {
			return fmt.Errorf("envelope type %q does not match heartbeat subject", env.Type)
		}
		if env.From != subject.Agent {
			return fmt.Errorf("heartbeat envelope does not match subject")
		}
	default:
		return fmt.Errorf("unknown subject kind %q", subject.Kind)
	}
	return nil
}

func parseEnvelopeTime(name, value string) (time.Time, bool, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%s must be RFC3339: %w", name, err)
	}
	return parsed, true, nil
}
