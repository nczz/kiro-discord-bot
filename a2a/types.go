package a2a

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	Version = "v1"
	Binding = "urn:kiro-discord-bot:a2a:nats:v1"
)

const MaxEnvelopePayloadBytes = 1024 * 1024

var (
	agentIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	taskIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
	tokenPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	digitsPattern  = regexp.MustCompile(`^\d+$`)
	pidPattern     = regexp.MustCompile(`(?i)(^|[-_])pid[-_]?\d+($|[-_])`)
	longSuffix     = regexp.MustCompile(`[-_]\d{10,}$`)
)

type AgentID string

type TaskID string

type MessageID string

type TaskState string

type ErrorCode string

type EnvelopeType string

const (
	TaskStateSubmitted     TaskState = "TASK_STATE_SUBMITTED"
	TaskStateWorking       TaskState = "TASK_STATE_WORKING"
	TaskStateInputRequired TaskState = "TASK_STATE_INPUT_REQUIRED"
	TaskStateAuthRequired  TaskState = "TASK_STATE_AUTH_REQUIRED"
	TaskStateCompleted     TaskState = "TASK_STATE_COMPLETED"
	TaskStateFailed        TaskState = "TASK_STATE_FAILED"
	TaskStateCanceled      TaskState = "TASK_STATE_CANCELED"
	TaskStateRejected      TaskState = "TASK_STATE_REJECTED"
)

const (
	EnvelopeTypeTask      EnvelopeType = "task"
	EnvelopeTypeControl   EnvelopeType = "control"
	EnvelopeTypeEvent     EnvelopeType = "event"
	EnvelopeTypeCard      EnvelopeType = "card"
	EnvelopeTypeHeartbeat EnvelopeType = "heartbeat"
)

type Config struct {
	NATSURL                      string
	NATSCredsFile                string
	NATSToken                    string
	NATSTLSCAFile                string
	AgentID                      AgentID
	AgentName                    string
	AgentDescription             string
	TaskTimeoutSec               int
	MaxDelegationDepth           int
	AutoDelegateEnabled          bool
	RequireConfirmationForRemote bool
	ProductionSecurity           bool
	TaskRetentionDays            int
	ObjectRetentionDays          int
	MaxPendingTasks              int
	MaxOutboundTasksPerChannel   int
	MaxInboundTasksPerChannel    int
	MaxEventRatePerMin           int
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.NATSURL) != "" && strings.TrimSpace(string(c.AgentID)) != ""
}

func (c Config) ValidateStartup() error {
	if strings.TrimSpace(c.NATSURL) == "" {
		return nil
	}
	if err := ValidateAgentID(c.AgentID); err != nil {
		return fmt.Errorf("A2A_AGENT_ID is required when NATS_URL is set: %w", err)
	}
	if c.ProductionSecurity && strings.TrimSpace(c.NATSCredsFile) == "" {
		return fmt.Errorf("A2A_PRODUCTION_SECURITY=true requires NATS_CREDS_FILE; token-only or unauthenticated production A2A is not allowed")
	}
	return nil
}

func IsTerminalState(state TaskState) bool {
	switch state {
	case TaskStateCompleted, TaskStateFailed, TaskStateCanceled, TaskStateRejected:
		return true
	default:
		return false
	}
}

func IsKnownTaskState(state TaskState) bool {
	switch state {
	case TaskStateSubmitted, TaskStateWorking, TaskStateInputRequired, TaskStateAuthRequired, TaskStateCompleted, TaskStateFailed, TaskStateCanceled, TaskStateRejected:
		return true
	default:
		return false
	}
}

func IsKnownEnvelopeType(t EnvelopeType) bool {
	switch t {
	case EnvelopeTypeTask, EnvelopeTypeControl, EnvelopeTypeEvent, EnvelopeTypeCard, EnvelopeTypeHeartbeat:
		return true
	default:
		return false
	}
}

func ValidateAgentID(id AgentID) error {
	s := string(id)
	if !agentIDPattern.MatchString(s) {
		return fmt.Errorf("agent id must match [A-Za-z0-9_-]{1,64}")
	}
	if digitsPattern.MatchString(s) || pidPattern.MatchString(s) || longSuffix.MatchString(s) {
		return fmt.Errorf("agent id must be stable and must not contain PID, boot timestamp, or random suffix")
	}
	return nil
}

func ValidateTaskID(id TaskID) error {
	if !taskIDPattern.MatchString(string(id)) {
		return fmt.Errorf("task id must match [A-Za-z0-9_-]{1,96}")
	}
	return nil
}

func ValidateMessageID(id MessageID) error {
	if !tokenPattern.MatchString(string(id)) {
		return fmt.Errorf("message id must match [A-Za-z0-9_-]{1,128}")
	}
	return nil
}

func validateSubjectToken(name, value string) error {
	if !tokenPattern.MatchString(value) {
		return fmt.Errorf("%s must match [A-Za-z0-9_-]{1,128}", name)
	}
	return nil
}
