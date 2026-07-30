package a2a

import (
	"context"
	"encoding/json"
	"time"
)

type TaskExecutionRequest struct {
	MessageID             MessageID
	ClientTaskRef         string
	ContextID             string
	From                  AgentID
	To                    AgentID
	ChannelID             string
	GuildID               string
	ChannelRef            string
	SkillID               string
	Payload               json.RawMessage
	Delivery              DeliveryOptions
	ResultVisibility      string
	DiscordTranscriptMode string
	CreatedAt             time.Time
	ExpiresAt             time.Time
}

type DeliveryOptions struct {
	TimeoutSec            int
	RequiresConfirmation  bool
	DiscordContextJSON    json.RawMessage
	DiscordReplyChannelID string
	DiscordReplyThreadID  string
	ShareDiscordContext   bool
	CoPresentFrom         AgentID
	MaxDelegationDepth    int
}

type A2AAdmissionResult struct {
	Accepted      bool
	AdmissionKey  string
	TaskID        TaskID
	State         TaskState
	Revision      int64
	ExecutorAgent AgentID
	Error         TaskError
}

type A2AAdmission struct {
	AdmissionKey string
	TaskID       TaskID
	State        TaskState
	Revision     int64
	Request      TaskExecutionRequest
}

type TaskExecutionResult struct {
	TaskID    TaskID
	State     TaskState
	Revision  int64
	Content   string
	Artifacts []Artifact
	Error     TaskError
	Metrics   map[string]float64
}

type Artifact struct {
	ID        string
	Name      string
	MediaType string
	Digest    string
	SizeBytes int64
	URI       string
}

type TaskError struct {
	Code    ErrorCode
	Message string
}

type Executor interface {
	AdmitA2ATask(context.Context, TaskExecutionRequest) (A2AAdmissionResult, error)
	RunA2ATask(context.Context, A2AAdmission) (TaskExecutionResult, error)
}
